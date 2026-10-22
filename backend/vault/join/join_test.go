package join

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type admin struct {
	s   vault.Store
	v   *vault.Vault
	dev *vault.SoftwareDeviceKey
}

func newAdmin(t *testing.T) admin {
	t.Helper()
	s := vault.NewMemStore()
	dev, err := vault.NewSoftwareDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Create(s, vault.NewMemState(vault.State{}), "pessoal", dev, "henrique", "Mac mini")
	if err != nil {
		t.Fatal(err)
	}
	return admin{s, v, dev}
}

func newDev(t *testing.T) *vault.SoftwareDeviceKey {
	t.Helper()
	d, err := vault.NewSoftwareDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func poll(t *testing.T, j *Joiner, want Phase) Status {
	t.Helper()
	st, err := j.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != want {
		t.Fatalf("phase = %s, want %s", st.Phase, want)
	}
	return st
}

func onlyRequest(t *testing.T, s vault.Store) Request {
	t.Helper()
	reqs, err := List(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	return reqs[0]
}

func TestJoinHappyPath(t *testing.T) {
	a := newAdmin(t)
	if _, err := a.v.Append([]byte("ssh: senha")); err != nil {
		t.Fatal(err)
	}
	nd := newDev(t)
	j, err := Start(a.s, nd, "henrique", "MacBook")
	if err != nil {
		t.Fatal(err)
	}
	st := poll(t, j, PhaseWaiting)
	if st.Code != "" {
		t.Fatal("no code before an offer")
	}

	req := onlyRequest(t, a.s)
	if req.Name != "MacBook" || req.DeviceID != vault.IDOf(nd) || req.Fingerprint != st.Fingerprint {
		t.Fatalf("listed request %+v", req)
	}
	ap, err := Begin(a.s, a.v, a.dev, req)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := ap.Ready(); err != nil || ok {
		t.Fatalf("ready before the reveal: %v %v", ok, err)
	}
	if _, err := ap.Confirm("000000", vault.RoleWriter); !errors.Is(err, ErrNotReady) {
		t.Fatalf("confirm before ready: %v", err)
	}

	st = poll(t, j, PhaseCode)
	if len(st.Code) != 6 {
		t.Fatalf("code %q", st.Code)
	}
	if ok, err := ap.Ready(); err != nil || !ok {
		t.Fatalf("ready: %v %v", ok, err)
	}
	conf, err := ap.Confirm(st.Code[:3]+" "+st.Code[3:], vault.RoleWriter)
	if err != nil {
		t.Fatal(err)
	}
	if len(conf) != 4 {
		t.Fatalf("confirmation %q", conf)
	}

	poll(t, j, PhaseApproved)
	state := vault.NewMemState(vault.State{})
	if _, err := j.Complete(state, "zzzz"); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("wrong confirmation: %v", err)
	}
	nv, err := j.Complete(state, conf)
	if err != nil {
		t.Fatal(err)
	}
	log, err := nv.ReadLog()
	if err != nil || len(log.Ops) != 1 || string(log.Ops[0].Payload) != "ssh: senha" {
		t.Fatalf("new device log: %+v %v", log.Ops, err)
	}
	if _, err := nv.Append([]byte("do novo")); err != nil {
		t.Fatal(err)
	}
	if left, _ := a.s.List("join/"); len(left) != 0 {
		t.Fatalf("join objects left behind: %d", len(left))
	}
}

func TestWrongCodesRefuse(t *testing.T) {
	a := newAdmin(t)
	j, _ := Start(a.s, newDev(t), "", "Intruso")
	ap, err := Begin(a.s, a.v, a.dev, onlyRequest(t, a.s))
	if err != nil {
		t.Fatal(err)
	}
	st := poll(t, j, PhaseCode)
	wrong := "000000"
	if st.Code == wrong {
		wrong = "111111"
	}
	for i := 1; i < MaxAttempts; i++ {
		var wce *WrongCodeError
		if _, err := ap.Confirm(wrong, vault.RoleAdmin); !errors.As(err, &wce) || wce.Remaining != MaxAttempts-i {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := ap.Confirm(wrong, vault.RoleAdmin); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("last attempt: %v", err)
	}
	// Even the right code is refused now.
	if _, err := ap.Confirm(st.Code, vault.RoleAdmin); err == nil {
		t.Fatal("right code accepted after the attempts ran out")
	}
	poll(t, j, PhaseRefused)
	if len(a.v.Roster().Members) != 1 {
		t.Fatal("device added despite refusal")
	}
	if reqs, _ := List(a.s); len(reqs) != 0 {
		t.Fatal("refused request still listed")
	}
}

func TestExpiry(t *testing.T) {
	a := newAdmin(t)
	c := &clock{time.Unix(1_800_000_000, 0)}
	j, err := start(a.s, newDev(t), "", "Lento", c.now)
	if err != nil {
		t.Fatal(err)
	}
	ap, err := begin(a.s, a.v, a.dev, onlyRequest(t, a.s), c.now)
	if err != nil {
		t.Fatal(err)
	}
	st := poll(t, j, PhaseCode)
	c.t = c.t.Add(TTL + time.Second)
	if _, err := ap.Confirm(st.Code, vault.RoleWriter); !errors.Is(err, ErrExpired) {
		t.Fatalf("admin side: %v", err)
	}
	poll(t, j, PhaseExpired)
	if len(a.v.Roster().Members) != 1 {
		t.Fatal("expired request added")
	}
}

// Whoever controls storage sits in the middle: it shows the admin its own
// request (same name), and answers the real new device with an offer of its
// own pointing at a look-alike vault. The code the user reads on the new
// device does not open the admin's approval, and the look-alike approval
// can't be completed without the confirmation only the real admin shows.
func TestMITMFailsBothWays(t *testing.T) {
	a := newAdmin(t)
	victim := newDev(t)
	j, err := Start(a.s, victim, "henrique", "MacBook")
	if err != nil {
		t.Fatal(err)
	}
	realReq := onlyRequest(t, a.s)

	// The attacker's own request, dressed as the victim.
	attackerDev := newDev(t)
	ja, err := Start(a.s, attackerDev, "henrique", "MacBook")
	if err != nil {
		t.Fatal(err)
	}
	var fakeReq Request
	reqs, _ := List(a.s)
	for _, r := range reqs {
		if r.ID != realReq.ID {
			fakeReq = r
		}
	}
	ap, err := Begin(a.s, a.v, a.dev, fakeReq) // the user picked the wrong "MacBook"
	if err != nil {
		t.Fatal(err)
	}
	poll(t, ja, PhaseCode) // attacker reveals for the admin's offer

	// The look-alike vault, offered to the victim.
	fake := newAdmin(t)
	if _, err := Begin(a.s, fake.v, fake.dev, realReq); err != nil { // offer lands in the real join/ dir
		t.Fatal(err)
	}
	st := poll(t, j, PhaseCode)

	// The user types the victim's code on the real admin: refused (one in a
	// million it matches by chance).
	if _, err := ap.Confirm(st.Code, vault.RoleAdmin); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("victim's code opened the attacker's approval: %v", err)
	}

	// The attacker approves the victim into the look-alike vault (copying
	// its roster into the real store would be the same: the victim anchors
	// on the offer's trust). Without the real admin's confirmation, the
	// victim never completes.
	if err := fake.v.AddMember(vault.MemberOf(victim, "henrique", "MacBook", vault.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	entries, _ := fake.s.List("")
	for _, e := range entries {
		if strings.HasPrefix(e.Name, "join/") {
			continue
		}
		data, _, _ := fake.s.Get(e.ID)
		if _, err := a.s.Create(e.Name, data); err != nil {
			t.Fatal(err)
		}
	}
	poll(t, j, PhaseApproved) // in the look-alike vault
	// The attacker can compute the right number too; what it can't do is
	// make the real admin display it, so the user has nothing to type.
	right := confirmationOf(j.body, j.offer, j.nonce)
	var guesses []string
	for _, g := range []string{"0000", "1234", "9999", "4321"} {
		if g != right && len(guesses) < MaxAttempts {
			guesses = append(guesses, g)
		}
	}
	for _, conf := range guesses {
		if _, err := j.Complete(vault.NewMemState(vault.State{}), conf); err == nil {
			t.Fatal("completed without approval")
		}
	}
	if _, err := j.Complete(vault.NewMemState(vault.State{}), "0000"); !errors.Is(err, ErrRefused) {
		t.Fatalf("after the attempts: %v", err)
	}
	if err := a.v.Refresh(); err != nil {
		t.Fatalf("real vault after the attack: %v", err)
	}
	if len(a.v.Roster().Members) != 1 {
		t.Fatal("attacker device added to the real vault")
	}
}

// A request candidate whose content doesn't hash to its name is ignored,
// so storage can't swap the keys behind a listed request.
func TestTamperedRequestIgnored(t *testing.T) {
	a := newAdmin(t)
	if _, err := Start(a.s, newDev(t), "", "MacBook"); err != nil {
		t.Fatal(err)
	}
	req := onlyRequest(t, a.s)
	forged := strings.Replace(string(req.body), `"name":"MacBook"`, `"name":"Outro"`, 1)
	if _, err := a.s.Create(requestName(req.ID), []byte(forged)); err != nil {
		t.Fatal(err)
	}
	reqs, _ := List(a.s)
	if len(reqs) != 1 || reqs[0].Name != "MacBook" {
		t.Fatalf("listed %+v", reqs)
	}
}

func TestNamesValidated(t *testing.T) {
	s := vault.NewMemStore()
	for _, name := range []string{"", " MacBook", "Mac‮Book", "Mac\x00", "uniterm-recovery-key", strings.Repeat("a", MaxNameLen+1), "Mac​Book"} {
		if _, err := Start(s, newDev(t), "", name); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if _, err := Start(s, newDev(t), "Henrique", "MacBook Pro de Henrique — 2026"); err != nil {
		t.Fatal(err)
	}
}

func TestBeginGuards(t *testing.T) {
	a := newAdmin(t)
	writerDev := newDev(t)
	if err := a.v.AddMember(vault.MemberOf(writerDev, "h", "iPhone", vault.RoleWriter)); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(a.s, newDev(t), "", "MacBook"); err != nil {
		t.Fatal(err)
	}
	req := onlyRequest(t, a.s)
	if _, err := Begin(a.s, a.v, writerDev, req); !errors.Is(err, vault.ErrNotAdmin) {
		t.Fatalf("non-admin began an approval: %v", err)
	}
	if err := Discard(a.s, req.ID); err != nil {
		t.Fatal(err)
	}
	if reqs, _ := List(a.s); len(reqs) != 0 {
		t.Fatal("discarded request still listed")
	}
	if _, err := Begin(a.s, a.v, a.dev, Request{ID: "x"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("unlisted request: %v", err)
	}
}

func TestCodesAreUniformDigits(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		n, _ := randomNonce()
		c := codeOf([]byte("r"), []byte("o"), n)
		if len(c) != 6 || strings.Trim(c, "0123456789") != "" {
			t.Fatalf("code %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatalf("only %d distinct codes in 200", len(seen))
	}
}
