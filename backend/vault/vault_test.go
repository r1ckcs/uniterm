package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func newKey(t *testing.T) *SoftwareDeviceKey {
	t.Helper()
	k, err := NewSoftwareDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newFileStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// team: admin A (vault creator), writer W, reader R. W and R join from the
// genesis anchor, like devices approved out of band.
type team struct {
	store      Store
	genesis    string
	a, w, r    *SoftwareDeviceKey
	va, vw, vr *Vault
}

func newTeam(t *testing.T, s Store) *team {
	t.Helper()
	tm := &team{store: s, a: newKey(t), w: newKey(t), r: newKey(t)}
	var err error
	if tm.va, err = Create(s, NewMemState(State{}), "pessoal", tm.a, "henrique", "mac-a"); err != nil {
		t.Fatal(err)
	}
	tm.genesis = tm.va.Trust().Hash
	must(t, tm.va.AddMember(MemberOf(tm.w, "henrique", "mac-w", RoleWriter)))
	must(t, tm.va.AddMember(MemberOf(tm.r, "colega", "android-r", RoleReader)))
	tm.vw = tm.join(t, tm.w)
	tm.vr = tm.join(t, tm.r)
	return tm
}

// join opens the vault as dev from the genesis anchor.
func (tm *team) join(t *testing.T, dev DeviceKey) *Vault {
	t.Helper()
	v, err := tm.open(dev)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (tm *team) open(dev DeviceKey) (*Vault, error) {
	st := NewMemState(State{})
	if err := Join(st, "pessoal", tm.genesis); err != nil {
		return nil, err
	}
	return Open(tm.store, st, dev)
}

// openAt opens dev with a given trust state (a device that had accepted it).
func openAt(s Store, dev DeviceKey, trust TrustState) (*Vault, error) {
	return Open(s, NewMemState(State{Trust: trust, NextSeq: 1}), dev)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func logOf(t *testing.T, v *Vault) Log {
	t.Helper()
	log, err := v.ReadLog()
	must(t, err)
	return log
}

func payloads(ops []Op) string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = string(op.Payload)
	}
	return strings.Join(out, ",")
}

func stores(t *testing.T) map[string]func() Store {
	return map[string]func() Store{
		"file": func() Store { return newFileStore(t) },
		"mem":  func() Store { return NewMemStore() },
	}
}

// --- round trip ---

func TestRoundTrip(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			tm := newTeam(t, mk())
			_, err := tm.va.Append([]byte(`{"host":"10.0.0.1"}`))
			must(t, err)
			_, err = tm.vw.Append([]byte(`{"host":"10.0.0.2"}`))
			must(t, err)
			for who, v := range map[string]*Vault{"admin": tm.va, "writer": tm.vw, "reader": tm.vr} {
				log := logOf(t, v)
				if len(log.Rejected) != 0 || len(log.Warnings) != 0 || len(log.Ops) != 2 ||
					!strings.Contains(payloads(log.Ops), "10.0.0.1") || !strings.Contains(payloads(log.Ops), "10.0.0.2") {
					t.Errorf("%s: %+v", who, log)
				}
			}
			if tr := tm.vr.Trust(); tr.Counter != 3 || tr.Hash == tm.genesis {
				t.Errorf("trust = %+v", tr)
			}
			entries, _ := tm.store.List("")
			for _, e := range entries {
				data, _, _ := tm.store.Get(e.ID)
				if bytes.Contains(data, []byte("10.0.0.")) {
					t.Errorf("%s holds plaintext", e.Name)
				}
			}
		})
	}
}

func TestStatePersistsAcrossOpen(t *testing.T) {
	s := newFileStore(t)
	a := newKey(t)
	st := FileState{Path: filepath.Join(t.TempDir(), "state", "pessoal.json")}
	v, err := Create(s, st, "pessoal", a, "h", "mac")
	must(t, err)
	_, err = v.Append([]byte("x"))
	must(t, err)
	saved, err := st.Load()
	must(t, err)
	if saved.NextSeq != 2 || saved.Lamport != 1 || saved.Trust != v.Trust() {
		t.Fatalf("state = %+v", saved)
	}
	v2, err := Open(s, st, a)
	must(t, err)
	op, err := v2.Append([]byte("y"))
	must(t, err)
	if op.Seq != 2 || op.Lamport != 2 {
		t.Fatalf("second op = %+v", op)
	}
	if _, err := (FileState{Path: filepath.Join(t.TempDir(), "none.json")}).Load(); !errors.Is(err, ErrNoState) {
		t.Fatalf("missing state = %v", err)
	}
}

func TestSoftwareDeviceKeyMarshal(t *testing.T) {
	k := newKey(t)
	data, err := k.Marshal()
	must(t, err)
	k2, err := UnmarshalSoftwareDeviceKey(data)
	must(t, err)
	if IDOf(k2) != IDOf(k) {
		t.Fatal("device id changed across marshal")
	}
	s := NewMemStore()
	v, err := Create(s, NewMemState(State{}), "pessoal", k, "h", "mac")
	must(t, err)
	if _, err := openAt(s, k2, v.Trust()); err != nil {
		t.Fatal(err)
	}
}

// --- roster: signatures, rollback, fork ---

// tamperFile rewrites a stored signed object's body in a FileStore.
func tamperFile(t *testing.T, s *FileStore, name string, edit func(body []byte) []byte) {
	t.Helper()
	data, _, err := s.Get(name)
	must(t, err)
	var obj signed
	must(t, json.Unmarshal(data, &obj))
	obj.Body = edit(obj.Body)
	out, _ := json.Marshal(obj)
	_, err = s.Put(name, out)
	must(t, err)
}

func TestTamperedRosterIgnored(t *testing.T) {
	s := newFileStore(t)
	tm := newTeam(t, s)
	at3 := tm.vr.Trust()
	// Promote the reader to admin in roster 3 without re-signing.
	tamperFile(t, s, rosterName(3), func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"role":"reader"`), []byte(`"role":"admin"`), 1)
	})
	// A joining client ignores the bad version and stops before it…
	v, err := tm.open(tm.w)
	must(t, err)
	if v.Trust().Counter != 2 || len(logOf(t, v).Warnings) == 0 {
		t.Fatalf("trust=%+v warnings=%v", v.Trust(), logOf(t, v).Warnings)
	}
	// …so the reader, added in 3, is not a member for it,
	if _, err := tm.open(tm.r); !errors.Is(err, ErrNotMember) {
		t.Fatalf("reader Open = %v, want ErrNotMember", err)
	}
	// and a client that had accepted the real 3 sees the change as a fork.
	if _, err := openAt(s, tm.r, at3); !errors.Is(err, ErrFork) {
		t.Fatalf("Open at 3 = %v, want ErrFork", err)
	}
}

func TestRosterSignedByNonAdminIgnored(t *testing.T) {
	tm := newTeam(t, NewMemStore())
	// The writer signs a roster 4 making itself admin: a valid signature,
	// but the writer is not an admin of roster 3.
	r := tm.vw.Roster()
	r.Counter, r.Prev = 4, tm.vw.Trust().Hash
	for i := range r.Members {
		if r.Members[i].DeviceID == IDOf(tm.w) {
			r.Members[i].Role = RoleAdmin
		}
	}
	_, err := writeRoster(tm.store, tm.w, &r)
	must(t, err)
	must(t, tm.va.Refresh())
	if tm.va.Trust().Counter != 3 {
		t.Fatalf("accepted a roster signed by a writer: %+v", tm.va.Trust())
	}
}

func TestRevokeWithoutRotationIgnored(t *testing.T) {
	tm := newTeam(t, NewMemStore())
	// An admin-signed roster that drops a member but keeps the epoch.
	r := tm.va.Roster()
	r.Counter, r.Prev = 4, tm.va.Trust().Hash
	r.Members = r.Members[:2]
	_, err := writeRoster(tm.store, tm.a, &r)
	must(t, err)
	v, err := tm.open(tm.w)
	must(t, err)
	if v.Trust().Counter != 3 {
		t.Fatalf("accepted removal without rotation: %+v", v.Trust())
	}
}

func TestRosterRollbackRefused(t *testing.T) {
	s := newFileStore(t)
	tm := newTeam(t, s)
	trusted := tm.vr.Trust() // counter 3
	must(t, s.Delete(rosterName(3)))
	if _, err := openAt(s, tm.r, trusted); !errors.Is(err, ErrRollback) {
		t.Fatalf("Open after rollback = %v, want ErrRollback", err)
	}
}

func TestRosterForkRefused(t *testing.T) {
	s := newFileStore(t)
	tm := newTeam(t, s)
	trusted := tm.vr.Trust() // counter 3
	// A different roster 3, validly signed by the admin (e.g. a stolen admin
	// key rewriting history): same counter, different hash.
	r := tm.va.Roster()
	r.Members[1].Name = "renamed"
	must(t, s.Delete(rosterName(3)))
	_, err := writeRoster(s, tm.a, &r)
	must(t, err)
	if _, err := openAt(s, tm.r, trusted); !errors.Is(err, ErrFork) {
		t.Fatalf("Open on forked roster = %v, want ErrFork", err)
	}
}

// --- A: junk candidates must not block anyone ---

func TestJunkCandidatesDoNotBlock(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	// Anyone with write access to the folder adds garbage under the next
	// roster name and next to existing ones.
	for _, n := range []string{rosterName(3), rosterName(4), rosterName(9)} {
		_, err := s.Create(n, []byte(`{"body":"AAAA","signer":"x","sig":"AAAA"}`))
		must(t, err)
	}
	v, err := tm.open(tm.r)
	must(t, err)
	if v.Trust().Counter != 3 {
		t.Fatalf("trust = %+v", v.Trust())
	}
	if w := logOf(t, v).Warnings; len(w) == 0 {
		t.Error("junk candidates not reported")
	}
	// The admin still moves the roster forward, past the squatted name.
	n := newKey(t)
	must(t, tm.va.AddMember(MemberOf(n, "h", "new", RoleReader)))
	must(t, v.Refresh())
	if v.Trust().Counter != 4 {
		t.Fatalf("after add: %+v", v.Trust())
	}
}

func TestSquattedWrapName(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	// The joining device's id is public (join request): someone plants a
	// wrap of a key they know under its name before the admin approves it.
	n := newKey(t)
	evil, _ := newVaultKey()
	planted, err := wrapKey(n.AgreementPublicKey(), "pessoal", 1, evil)
	must(t, err)
	_, err = s.Create(wrapName(IDOf(n), 1), planted)
	must(t, err)
	must(t, tm.va.AddMember(MemberOf(n, "h", "new", RoleWriter)))
	v := tm.join(t, n)
	// The device took the admin's key, not the planted one: the others read it.
	_, err = v.Append([]byte("hello"))
	must(t, err)
	if got := payloads(logOf(t, tm.va).Ops); got != "hello" {
		t.Fatalf("ops = %q", got)
	}
}

func TestPlantedWrapRefused(t *testing.T) {
	// Replacing a device's only wrap with one of a key the attacker knows is
	// caught by the roster's key check: the device never uses that key.
	s := newFileStore(t)
	tm := newTeam(t, s)
	evil, _ := newVaultKey()
	wrapped, err := wrapKey(tm.w.AgreementPublicKey(), "pessoal", 1, evil)
	must(t, err)
	_, err = s.Put(wrapName(IDOf(tm.w), 1), wrapped)
	must(t, err)
	if _, err := tm.open(tm.w); !errors.Is(err, errBadWrap) {
		t.Fatalf("Open with planted wrap = %v, want errBadWrap", err)
	}
}

// --- B: admins racing on one counter ---

func TestAdminRace(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	a2 := newKey(t)
	must(t, tm.va.AddMember(MemberOf(a2, "reserva", "mac-reserva", RoleAdmin)))
	va2 := tm.join(t, a2)
	// Both admins, at counter 4, add a device at the same time.
	x, y := newKey(t), newKey(t)
	must(t, tm.va.AddMember(MemberOf(x, "h", "x", RoleReader)))
	if err := va2.AddMember(MemberOf(y, "h", "y", RoleReader)); !errors.Is(err, ErrLostRace) {
		t.Fatalf("second admin = %v, want ErrLostRace", err)
	}
	// Every client picked the same version (the first one)…
	vr := tm.join(t, tm.r)
	if vr.Trust() != tm.va.Trust() || va2.Trust() != tm.va.Trust() {
		t.Fatalf("clients disagree: %+v %+v %+v", vr.Trust(), va2.Trust(), tm.va.Trust())
	}
	if w := logOf(t, vr).Warnings; len(w) == 0 || !strings.Contains(strings.Join(w, ";"), "raced") {
		t.Errorf("race not reported: %v", w)
	}
	// …and the loser redoes its change on top of it.
	must(t, va2.AddMember(MemberOf(y, "h", "y", RoleReader)))
	must(t, vr.Refresh())
	if _, ok := (&Roster{Members: vr.Roster().Members}).member(IDOf(y)); !ok {
		t.Fatal("redone change missing")
	}
}

// --- revocation and epochs (C) ---

func TestRevokedDeviceCannotReadNewEpoch(t *testing.T) {
	tm := newTeam(t, newFileStore(t))
	_, err := tm.vw.Append([]byte("before"))
	must(t, err)
	must(t, tm.va.Revoke(IDOf(tm.r)))
	if got := tm.va.Roster().Epoch; got != 2 {
		t.Fatalf("epoch after revoke = %d", got)
	}
	must(t, tm.vw.Refresh())
	_, err = tm.vw.Append([]byte("after"))
	must(t, err)

	// The revoked device's client refuses to open the vault…
	if _, err := openAt(tm.store, tm.r, tm.vr.Trust()); !errors.Is(err, ErrNotMember) {
		t.Fatalf("revoked Open = %v, want ErrNotMember", err)
	}
	// …and a modified client gets nothing either: no wrap of epoch 2 is
	// addressed to it, the others' wraps don't open with its key, and its
	// epoch-1 key doesn't decrypt epoch-2 ops.
	if c, _ := candidates(tm.store, wrapName(IDOf(tm.r), 2)); len(c) != 0 {
		t.Fatal("wrap for revoked device exists")
	}
	r2 := tm.va.Roster()
	for _, id := range []string{IDOf(tm.a), IDOf(tm.w)} {
		wrapped, _, err := tm.store.Get(wrapName(id, 2))
		must(t, err)
		if _, err := unwrapKey(tm.r, &r2, 2, wrapped); err == nil {
			t.Fatalf("revoked device opened %s's wrap", id)
		}
	}
	stolen := map[uint64][]byte{1: tm.vr.keys[1], 2: tm.vr.keys[1]}
	name := opName(IDOf(tm.w), 2)
	data, _, _ := tm.store.Get(name)
	if _, _, err := openOp(&r2, stolen, name, data); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("epoch-2 op with the epoch-1 key: %v", err)
	}

	for _, v := range []*Vault{tm.va, tm.vw} {
		if log := logOf(t, v); payloads(log.Ops) != "before,after" || len(log.Rejected) != 0 {
			t.Fatalf("%+v", log)
		}
	}
}

func TestRevokedWriterKeepsOldOpsButCannotAddMore(t *testing.T) {
	tm := newTeam(t, NewMemStore())
	_, err := tm.vw.Append([]byte("w1"))
	must(t, err)
	_, err = tm.vw.Append([]byte("w2"))
	must(t, err)
	must(t, tm.va.Revoke(IDOf(tm.w)))
	if c := tm.va.Roster().Closed[0].Seqs[IDOf(tm.w)]; c.Max != 2 || len(c.Holes) != 0 {
		t.Fatalf("cutoff = %d", c)
	}
	// Its ops from before the rotation still count…
	log := logOf(t, tm.va)
	if payloads(log.Ops) != "w1,w2" || len(log.Rejected) != 0 {
		t.Fatalf("%+v", log)
	}
	// …but it can't add a backdated one with the epoch-1 key it still has.
	obj, err := sealOp(tm.w, "pessoal", 3, 1, 3, RosterRef{}, tm.vw.keys[1], []byte("honeypot"))
	must(t, err)
	_, err = tm.store.Create(opName(IDOf(tm.w), 3), obj)
	must(t, err)
	log = logOf(t, tm.va)
	if payloads(log.Ops) != "w1,w2" || len(log.Rejected) != 1 || !strings.Contains(log.Rejected[0].Reason, "is not one of epoch") {
		t.Fatalf("%+v", log)
	}
}

func TestOldEpochOpAfterRotationRefused(t *testing.T) {
	// A remaining writer with a stale (or compromised) client writes under
	// the retired key, which the removed device knows: refused, so nothing
	// new is ever readable with the old key.
	tm := newTeam(t, NewMemStore())
	must(t, tm.va.Revoke(IDOf(tm.r)))
	_, err := tm.vw.Append([]byte("stale")) // vw not refreshed: epoch 1
	must(t, err)
	log := logOf(t, tm.va)
	if len(log.Ops) != 0 || len(log.Rejected) != 1 || !strings.Contains(log.Rejected[0].Reason, "is not one of epoch") {
		t.Fatalf("%+v", log)
	}
	must(t, tm.vw.Refresh())
	op, err := tm.vw.Append([]byte("fresh"))
	must(t, err)
	if op.Epoch != 2 || payloads(logOf(t, tm.va).Ops) != "fresh" {
		t.Fatalf("op=%+v", op)
	}
}

func TestReaderOpRefused(t *testing.T) {
	tm := newTeam(t, NewMemStore())
	if _, err := tm.vr.Append([]byte("x")); !errors.Is(err, ErrNotWriter) {
		t.Fatalf("reader Append = %v, want ErrNotWriter", err)
	}
	// A modified client signs and stores the op anyway: everyone refuses it.
	obj, err := sealOp(tm.r, "pessoal", 1, 1, 1, RosterRef{}, tm.vr.keys[1], []byte(`{"host":"honeypot"}`))
	must(t, err)
	_, err = tm.store.Create(opName(IDOf(tm.r), 1), obj)
	must(t, err)
	for _, v := range []*Vault{tm.va, tm.vw, tm.vr} {
		log := logOf(t, v)
		if len(log.Ops) != 0 || len(log.Rejected) != 1 || !strings.Contains(log.Rejected[0].Reason, "not a writer") {
			t.Fatalf("%+v", log)
		}
	}
}

// --- ops: tampering, moving, equivocation (F), Lamport (D), freeze (E) ---

func TestTamperedOpRefused(t *testing.T) {
	s := newFileStore(t)
	tm := newTeam(t, s)
	op, err := tm.vw.Append([]byte("ok"))
	must(t, err)
	_, err = tm.vw.Append([]byte("other"))
	must(t, err)
	tamperFile(t, s, op.Name, func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"lamport":1`), []byte(`"lamport":9`), 1)
	})
	log := logOf(t, tm.va)
	if payloads(log.Ops) != "other" || len(log.Rejected) != 1 || log.Rejected[0].Name != op.Name {
		t.Fatalf("%+v", log)
	}
}

func TestMovedOpRefused(t *testing.T) {
	tm := newTeam(t, NewMemStore())
	op, err := tm.vw.Append([]byte("x"))
	must(t, err)
	c, _ := candidates(tm.store, op.Name)
	data, _, _ := tm.store.Get(c[0].ID)
	_, err = tm.store.Create(opName(IDOf(tm.w), 7), data)
	must(t, err)
	if log := logOf(t, tm.va); len(log.Ops) != 1 || len(log.Rejected) != 1 {
		t.Fatalf("%+v", log)
	}
}

func TestEquivocationRefused(t *testing.T) {
	tm := newTeam(t, NewMemStore())
	_, err := tm.vw.Append([]byte("real"))
	must(t, err)
	// Same author and seq, different content, both validly signed (e.g. the
	// device restored from a backup and reused the number).
	obj, err := sealOp(tm.w, "pessoal", 1, 1, 1, RosterRef{}, tm.vw.keys[1], []byte("other"))
	must(t, err)
	_, err = tm.store.Create(opName(IDOf(tm.w), 1), obj)
	must(t, err)
	log := logOf(t, tm.va)
	if len(log.Ops) != 0 || len(log.Rejected) != 2 || !strings.Contains(strings.Join(log.Warnings, ";"), "two different ops") {
		t.Fatalf("%+v", log)
	}
	// An identical duplicate (same object listed twice) is not equivocation.
	tm2 := newTeam(t, NewMemStore())
	op, err := tm2.vw.Append([]byte("x"))
	must(t, err)
	c, _ := candidates(tm2.store, op.Name)
	data, _, _ := tm2.store.Get(c[0].ID)
	_, err = tm2.store.Create(op.Name, data)
	must(t, err)
	if log := logOf(t, tm2.va); payloads(log.Ops) != "x" || len(log.Rejected) != 0 {
		t.Fatalf("%+v", log)
	}
}

func TestSeqNotReusedWhenListingIsStale(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	op, err := tm.vw.Append([]byte("first"))
	must(t, err)
	// The listing doesn't show the op yet (or it was hidden): the next
	// append must not take the same number.
	c, _ := candidates(s, op.Name)
	must(t, s.Delete(c[0].ID))
	op2, err := tm.vw.Append([]byte("second"))
	must(t, err)
	if op2.Seq != 2 || op2.Lamport <= op.Lamport {
		t.Fatalf("second op = %+v", op2)
	}
}

func TestLamportRules(t *testing.T) {
	forge := func(tm *team, seq, lamport uint64, p string) {
		t.Helper()
		obj, err := sealOp(tm.w, "pessoal", seq, 1, lamport, RosterRef{}, tm.vw.keys[1], []byte(p))
		must(t, err)
		_, err = tm.store.Create(opName(IDOf(tm.w), seq), obj)
		must(t, err)
	}
	cases := []struct {
		name   string
		ops    [][2]uint64 // seq, lamport
		accept string
		reason string
	}{
		{"beyond the cap", [][2]uint64{{1, 1}, {2, maxLamport + 1}}, "s1", "out of range"},
		{"max uint64", [][2]uint64{{1, ^uint64(0)}}, "", "out of range"},
		{"jump past everyone", [][2]uint64{{1, 1}, {2, 3 + maxLamportJump}}, "s1", "jumps too far"},
		{"clock goes back", [][2]uint64{{1, 5}, {2, 3}}, "s1", "does not grow"},
		{"within slack", [][2]uint64{{1, 1}, {2, maxLamportJump}}, "s1,s2", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tm := newTeam(t, NewMemStore())
			_, err := tm.va.Append([]byte("a")) // another author's clock: 1
			must(t, err)
			for _, o := range c.ops {
				forge(tm, o[0], o[1], fmt.Sprintf("s%d", o[0]))
			}
			log := logOf(t, tm.va)
			got := strings.TrimPrefix(strings.ReplaceAll(","+payloads(log.Ops), ",a", ""), ",")
			if got != c.accept {
				t.Errorf("accepted %q, want %q (%+v)", got, c.accept, log.Rejected)
			}
			if c.reason != "" && (len(log.Rejected) != 1 || !strings.Contains(log.Rejected[0].Reason, c.reason)) {
				t.Errorf("rejected = %+v, want %q", log.Rejected, c.reason)
			}
		})
	}
	// A single writer is never capped by the jump rule.
	tm := newTeam(t, NewMemStore())
	for i := range 5 {
		_, err := tm.va.Append(fmt.Appendf(nil, "%d", i))
		must(t, err)
	}
	if got := payloads(logOf(t, tm.vr).Ops); got != "0,1,2,3,4" {
		t.Fatalf("ops = %q", got)
	}
}

func TestHiddenRosterDetected(t *testing.T) {
	s := newFileStore(t)
	tm := newTeam(t, s)
	// The admin moves to roster 4 and writes an op citing it…
	must(t, tm.va.AddMember(MemberOf(newKey(t), "h", "n", RoleReader)))
	_, err := tm.va.Append([]byte("x"))
	must(t, err)
	// …but storage hides roster 4 from a client that only knows 3.
	must(t, s.Delete(rosterName(4)))
	v, err := openAt(s, tm.w, tm.vw.Trust())
	must(t, err)
	log := logOf(t, v)
	if !strings.Contains(strings.Join(log.Warnings, ";"), "hiding roster") {
		t.Fatalf("freeze not detected: %+v", log)
	}
}

// --- concurrency ---

func TestConcurrentWriters(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			tm := newTeam(t, mk())
			w2 := newKey(t)
			must(t, tm.va.AddMember(MemberOf(w2, "colega", "mac-w2", RoleWriter)))
			vw2 := tm.join(t, w2)
			must(t, tm.vw.Refresh())
			must(t, tm.vr.Refresh())
			// Three devices, and two goroutines per device handle.
			const n = 15
			var wg sync.WaitGroup
			errs := make(chan error, 6*n)
			for i, v := range []*Vault{tm.va, tm.vw, vw2} {
				for g := range 2 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := range n {
							if _, err := v.Append(fmt.Appendf(nil, "d%d-g%d-%d", i, g, j)); err != nil {
								errs <- err
							}
						}
					}()
				}
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatal(err)
			}
			log := logOf(t, tm.vr)
			if len(log.Rejected) != 0 || len(log.Ops) != 6*n {
				t.Fatalf("got %d ops, rejected %v", len(log.Ops), log.Rejected)
			}
			if payloads(logOf(t, tm.va).Ops) != payloads(log.Ops) {
				t.Fatal("readers disagree on log order")
			}
		})
	}
}

// --- stores ---

func TestFileStore(t *testing.T) {
	s := newFileStore(t)
	e, err := s.Create("ops/a/000000000001", []byte("x"))
	must(t, err)
	if _, err := s.Create("ops/a/000000000001", []byte("y")); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create = %v, want ErrExists", err)
	}
	data, ver, err := s.Get(e.ID)
	if err != nil || string(data) != "x" || ver == "" {
		t.Fatalf("Get = %q %q %v", data, ver, err)
	}
	if ver2, _ := s.Put(e.ID, []byte("z")); ver2 == ver {
		t.Error("version did not change with the content")
	}
	if _, _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing = %v", err)
	}
	for _, bad := range []string{"", "../x", "a/../b", "/abs", "a//b", ".hidden", "a/.vault-tmp-1", `a\b`, "a b"} {
		if _, err := s.Create(bad, nil); !errors.Is(err, ErrBadName) {
			t.Errorf("Create(%q) = %v, want ErrBadName", bad, err)
		}
	}
	if l, _ := s.List("ops/"); len(l) != 1 || l[0].Name != "ops/a/000000000001" {
		t.Errorf("List = %v", l)
	}
}

func TestMemStoreKeepsCandidates(t *testing.T) {
	s := NewMemStore()
	e1, _ := s.Create("roster/000000000001", []byte("a"))
	e2, _ := s.Create("roster/000000000001", []byte("b"))
	_, _ = s.Create("roster/000000000001x", []byte("c"))
	c, err := candidates(s, "roster/000000000001")
	must(t, err)
	if len(c) != 2 || c[0].ID != e1.ID || c[1].ID != e2.ID || c[0].Created >= c[1].Created {
		t.Fatalf("candidates = %+v", c)
	}
}

// --- review 2: holes below a cutoff, premature pinning ---

// forgeOp stores an op signed by dev under (seq, epoch) with key vk.
func forgeOp(t *testing.T, s Store, dev DeviceKey, seq, epoch, lamport uint64, vk []byte, p string) {
	t.Helper()
	obj, err := sealOp(dev, "pessoal", seq, epoch, lamport, RosterRef{}, vk, []byte(p))
	must(t, err)
	_, err = s.Create(opName(IDOf(dev), seq), obj)
	must(t, err)
}

// dropOp removes every candidate of an op (a write that never landed).
func dropOp(t *testing.T, s Store, name string) {
	t.Helper()
	c, err := candidates(s, name)
	must(t, err)
	for _, e := range c {
		must(t, s.Delete(e.ID))
	}
}

func TestRevokedCannotFillHoleBelowCutoff(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	for _, p := range []string{"w1", "w2", "w3"} {
		_, err := tm.vw.Append([]byte(p))
		must(t, err)
	}
	dropOp(t, s, opName(IDOf(tm.w), 2)) // seq 2 never landed
	must(t, tm.va.Revoke(IDOf(tm.w)))
	if c := tm.va.Roster().Closed[0].Seqs[IDOf(tm.w)]; c.Max != 3 || !slices.Equal(c.Holes, []uint64{2}) {
		t.Fatalf("cut = %+v", c)
	}
	forgeOp(t, s, tm.w, 2, 1, 2, tm.vw.keys[1], "honeypot")
	log := logOf(t, tm.va)
	if payloads(log.Ops) != "w1,w3" || len(log.Rejected) != 1 || !strings.Contains(log.Rejected[0].Reason, "is not one of epoch") {
		t.Fatalf("%+v", log)
	}
}

func TestCurrentWriterCannotFillHoleUnderOldEpoch(t *testing.T) {
	// A remaining writer (buggy or compromised) puts new content in its own
	// hole under the retired key, which the removed reader knows.
	s := NewMemStore()
	tm := newTeam(t, s)
	for _, p := range []string{"w1", "w2", "w3"} {
		_, err := tm.vw.Append([]byte(p))
		must(t, err)
	}
	dropOp(t, s, opName(IDOf(tm.w), 2))
	must(t, tm.va.Revoke(IDOf(tm.r)))
	forgeOp(t, s, tm.w, 2, 1, 2, tm.vw.keys[1], "readable by the revoked reader")
	log := logOf(t, tm.va)
	if payloads(log.Ops) != "w1,w3" || len(log.Rejected) != 1 {
		t.Fatalf("%+v", log)
	}
}

func TestOldSeqRelabeledWithLaterClosedEpochRefused(t *testing.T) {
	// seq 1 belongs to epoch 1; after epoch 2 is closed too, an op 1 under
	// epoch 2 must not pass as one of epoch 2's.
	s := NewMemStore()
	tm := newTeam(t, s)
	x := newKey(t)
	must(t, tm.va.AddMember(MemberOf(x, "h", "x", RoleReader)))
	_, err := tm.vw.Append([]byte("e1"))
	must(t, err)
	must(t, tm.va.Revoke(IDOf(tm.r))) // closes epoch 1: w max 1
	must(t, tm.vw.Refresh())
	_, err = tm.vw.Append([]byte("e2"))
	must(t, err)
	must(t, tm.va.Revoke(IDOf(x))) // closes epoch 2: w (1, 2]
	dropOp(t, s, opName(IDOf(tm.w), 1))
	forgeOp(t, s, tm.w, 1, 2, 1, tm.vw.keys[2], "relabeled")
	log := logOf(t, tm.va)
	if payloads(log.Ops) != "e2" || len(log.Rejected) != 1 || !strings.Contains(log.Rejected[0].Reason, "is not one of epoch 2") {
		t.Fatalf("%+v", log)
	}
}

func TestLoserSeenAloneIsNotPinned(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	a2 := newKey(t)
	must(t, tm.va.AddMember(MemberOf(a2, "reserva", "mac-reserva", RoleAdmin)))
	va2 := tm.join(t, a2)
	// A client that has pinned counter 4 (old enough).
	pinned := NewMemState(State{Trust: tm.va.Trust(), NextSeq: 1})
	// Both admins write roster 5; the client's listing only shows the loser.
	must(t, tm.va.AddMember(MemberOf(newKey(t), "h", "x", RoleReader)))
	winner, _ := candidates(s, rosterName(5))
	s.Hide(winner[0].ID)
	if err := va2.AddMember(MemberOf(newKey(t), "h", "y", RoleReader)); err != nil {
		t.Fatal(err) // with the winner hidden, the loser thinks it won
	}
	v, err := Open(s, pinned, tm.r)
	must(t, err)
	loser := v.Trust()
	if loser.Counter != 5 {
		t.Fatalf("view = %+v", loser)
	}
	if st, _ := pinned.Load(); st.Trust.Counter != 4 {
		t.Fatalf("pinned a fresh version: %+v", st.Trust)
	}
	// The listing catches up: the client moves to the winner, no ErrFork.
	s.Show(winner[0].ID)
	must(t, v.Refresh())
	if v.Trust() == loser || v.Trust() != tm.va.Trust() {
		t.Fatalf("view = %+v, want the winner %+v", v.Trust(), tm.va.Trust())
	}
}

func TestOldVersionIsPinned(t *testing.T) {
	s := NewMemStore()
	s.Now = func() time.Time { return time.Now().Add(-2 * PinDelay) }
	tm := newTeam(t, s)
	st := NewMemState(State{})
	must(t, Join(st, "pessoal", tm.genesis))
	_, err := Open(s, st, tm.r)
	must(t, err)
	if got, _ := st.Load(); got.Trust.Counter != 3 {
		t.Fatalf("pinned = %+v", got.Trust)
	}
	// A rollback below the pinned version is caught across restarts.
	c, _ := candidates(s, rosterName(3))
	must(t, s.Delete(c[0].ID))
	if _, err := Open(s, st, tm.r); !errors.Is(err, ErrRollback) {
		t.Fatalf("Open = %v, want ErrRollback", err)
	}
}

// --- Supersedes (re-anchoring off a dead branch) ---

// pinnedLoser sets up an admin race at roster 5 whose loser a client has
// pinned (its listing showed only the loser for longer than PinDelay).
func pinnedLoser(t *testing.T) (s *MemStore, tm *team, va2 *Vault, pinned *MemState, loser TrustState) {
	t.Helper()
	s = NewMemStore()
	tm = newTeam(t, s)
	a2 := newKey(t)
	must(t, tm.va.AddMember(MemberOf(a2, "reserva", "mac-reserva", RoleAdmin)))
	va2 = tm.join(t, a2)
	must(t, tm.va.AddMember(MemberOf(newKey(t), "h", "x", RoleReader)))
	winner, _ := candidates(s, rosterName(5))
	s.Hide(winner[0].ID)
	must(t, va2.AddMember(MemberOf(newKey(t), "h", "y", RoleReader)))
	loser = va2.Trust()
	s.Show(winner[0].ID)
	pinned = NewMemState(State{Trust: loser, NextSeq: 1})
	return s, tm, va2, pinned, loser
}

func TestSupersededLoserReanchors(t *testing.T) {
	_, tm, _, pinned, loser := pinnedLoser(t)
	v, err := Open(tm.store, pinned, tm.r)
	must(t, err)
	if v.Trust() != loser {
		t.Fatalf("before supersede: %+v, want the pinned loser", v.Trust())
	}
	// The winning admin sees the race and marks the loser.
	must(t, tm.va.Refresh())
	must(t, tm.va.SupersedeLosers())
	if got := tm.va.Roster().Supersedes; len(got) != 1 || got[0] != (Superseded{loser.Counter, loser.Hash}) {
		t.Fatalf("supersedes = %+v", got)
	}
	must(t, v.Refresh())
	if v.Trust() != tm.va.Trust() {
		t.Fatalf("after supersede: %+v, want %+v", v.Trust(), tm.va.Trust())
	}
	if st, _ := pinned.Load(); st.Trust == loser {
		t.Fatal("still pinned to the loser")
	}
	if !strings.Contains(strings.Join(logOf(t, v).Warnings, ";"), "superseded") {
		t.Error("re-anchoring not reported")
	}
	// Nothing left to mark: no new version.
	before := tm.va.Trust()
	must(t, tm.va.SupersedeLosers())
	if tm.va.Trust() != before {
		t.Fatal("SupersedeLosers wrote a version with nothing to mark")
	}
}

func TestLostRaceRedoSupersedes(t *testing.T) {
	// The losing admin's redo marks its own loser.
	s := NewMemStore()
	tm := newTeam(t, s)
	a2 := newKey(t)
	must(t, tm.va.AddMember(MemberOf(a2, "reserva", "mac-reserva", RoleAdmin)))
	va2 := tm.join(t, a2)
	must(t, tm.va.AddMember(MemberOf(newKey(t), "h", "x", RoleReader)))
	y := MemberOf(newKey(t), "h", "y", RoleReader)
	if err := va2.AddMember(y); !errors.Is(err, ErrLostRace) {
		t.Fatalf("= %v, want ErrLostRace", err)
	}
	must(t, va2.AddMember(y))
	if got := va2.Roster().Supersedes; len(got) != 1 || got[0].Counter != 5 {
		t.Fatalf("supersedes = %+v", got)
	}
}

func TestStalledWithoutSupersedeStays(t *testing.T) {
	// Roster 6 written by an admin that had not refreshed since the race
	// does not mark the loser: the pinned client stays on its branch (and
	// the version after it, which does not follow, doesn't move it either)
	// until SupersedeLosers runs.
	_, tm, _, pinned, loser := pinnedLoser(t)
	must(t, tm.va.AddMember(MemberOf(newKey(t), "h", "z", RoleReader)))
	if len(tm.va.Roster().Supersedes) != 0 {
		t.Fatal("test premise: roster 6 should not supersede yet")
	}
	v, err := Open(tm.store, pinned, tm.r)
	must(t, err)
	if v.Trust() != loser {
		t.Fatalf("moved without a supersede: %+v", v.Trust())
	}
	must(t, tm.va.SupersedeLosers())
	must(t, v.Refresh())
	if v.Trust() != tm.va.Trust() || v.Trust().Counter != 7 {
		t.Fatalf("after supersede: %+v, want %+v", v.Trust(), tm.va.Trust())
	}
}

func TestSupersedeNeedsAdminSignature(t *testing.T) {
	s, tm, _, pinned, loser := pinnedLoser(t)
	// A writer forges roster 6 on the winning chain claiming the loser is dead.
	must(t, tm.va.Refresh())
	next := cloneRoster(tm.va.Roster())
	next.Counter, next.Prev = 6, tm.va.Trust().Hash
	next.Supersedes = []Superseded{{loser.Counter, loser.Hash}}
	if _, err := writeRoster(s, tm.w, &next); err != nil {
		t.Fatal(err)
	}
	v, err := Open(s, pinned, tm.r)
	must(t, err)
	if v.Trust() != loser {
		t.Fatalf("moved on a forged supersede: %+v", v.Trust())
	}
}

func TestSupersedeOwnAncestorRefused(t *testing.T) {
	s := NewMemStore()
	tm := newTeam(t, s)
	// An admin can't declare a version of its own chain dead.
	next := cloneRoster(tm.va.Roster())
	next.Counter, next.Prev = 4, tm.va.Trust().Hash
	g, _ := candidates(s, rosterName(2))
	data, _, _ := s.Get(g[0].ID)
	vr, _, err := parseRoster(data, "pessoal")
	must(t, err)
	next.Supersedes = []Superseded{{2, vr.hash}}
	if _, err := writeRoster(s, tm.a, &next); err != nil {
		t.Fatal(err)
	}
	v := tm.join(t, tm.r)
	if v.Trust().Counter != 3 {
		t.Fatalf("accepted a version superseding its own ancestor: %+v", v.Trust())
	}
}
