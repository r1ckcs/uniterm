package service

import (
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/records"
	"github.com/ys-ll/uniterm/backend/vault/recovery"
)

// memBridge stands in for the app's stores.
type memBridge struct {
	mu   sync.Mutex
	snap records.Snapshot
}

func (b *memBridge) Local() (records.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.snap), nil
}

func (b *memBridge) Apply(s records.Snapshot) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snap = maps.Clone(s)
	return nil
}

type conn struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Password string `json:"password,omitempty"`
}

func (b *memBridge) put(t *testing.T, c conn) {
	t.Helper()
	id, f, err := records.ToFields(c)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.snap == nil {
		b.snap = records.Snapshot{}
	}
	b.snap[records.Key{Kind: records.Connection, ID: id}] = f
}

func (b *memBridge) get(t *testing.T, id string) (conn, bool) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	f, ok := b.snap[records.Key{Kind: records.Connection, ID: id}]
	var c conn
	if ok {
		if err := records.FromFields(id, f, &c); err != nil {
			t.Fatal(err)
		}
	}
	return c, ok
}

type device struct {
	*Service
	bridge *memBridge
	events chan string
}

func newDevice(t *testing.T, shared, name string) *device {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(StoreEnv, "file:"+shared)
	be, ok, err := FileBackendFromEnv(dir)
	if err != nil || !ok {
		t.Fatal(err)
	}
	d := &device{bridge: &memBridge{}, events: make(chan string, 1000)}
	d.Service, err = New(Options{
		Dir: filepath.Join(dir, "vault"), Backend: be, Secrets: &FileSecrets{Path: filepath.Join(dir, "secrets.json")},
		Bridge: d.bridge, DeviceName: name, Owner: "henrique",
		SyncEvery: time.Hour, Debounce: time.Millisecond, PollEvery: 10 * time.Millisecond,
		Emit: func(n string, _ any) {
			select {
			case d.events <- n:
			default:
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// create brings up the first device through the 24 words.
func create(t *testing.T, a *device) []string {
	t.Helper()
	must(t, a.SignIn())
	waitFor(t, "showRecovery", func() bool { return a.Status().Phase == PhaseShowRecovery })
	words, err := a.RecoveryWords()
	must(t, err)
	if len(words) != 24 {
		t.Fatalf("%d words", len(words))
	}
	idx := a.Status().RecoveryCheck
	answers := make([]string, len(idx))
	for i, p := range idx {
		answers[i] = words[p]
	}
	wrong := slices3(answers)
	wrong[0] = "zzz"
	if err := a.ConfirmRecovery(idx, wrong); code(err) != CodeRecoveryMismatch {
		t.Fatalf("wrong words = %v", err)
	}
	must(t, a.ConfirmRecovery(idx, answers))
	if a.Status().Phase != PhaseReady || a.Status().Device.Role != vault.RoleAdmin {
		t.Fatalf("after confirm: %+v", a.Status())
	}
	return words
}

func slices3(s []string) []string { return append([]string(nil), s...) }

// approve runs the join dialog on admin a for new device b.
func approve(t *testing.T, a, b *device, role vault.Role) {
	t.Helper()
	must(t, b.SignIn())
	waitFor(t, "awaitingApproval", func() bool { return b.Status().Phase == PhaseAwaitingApproval })
	if pj := b.Status().PendingJoin; pj == nil || pj.Code6 != "" {
		t.Fatalf("code before approval began: %+v", pj)
	}
	var reqs []JoinRequest
	waitFor(t, "request listed", func() bool {
		var err error
		reqs, err = a.JoinRequests()
		return err == nil && len(reqs) == 1
	})
	if reqs[0].Name != b.Status().Device.Name || reqs[0].Fingerprint != b.Status().Device.Fingerprint {
		t.Fatalf("request %+v vs %+v", reqs[0], b.Status().Device)
	}
	must(t, a.BeginApproval(reqs[0].RequestID))
	waitFor(t, "code shown", func() bool { pj := b.Status().PendingJoin; return pj != nil && pj.Code6 != "" })
	code6 := b.Status().PendingJoin.Code6
	bad := "000000"
	if bad == code6 {
		bad = "111111"
	}
	var conf string
	waitFor(t, "approver ready", func() bool {
		_, err := a.ApproveJoin(reqs[0].RequestID, bad, role)
		return code(err) == CodeJoinBadCode
	})
	conf, err := a.ApproveJoin(reqs[0].RequestID, code6, role)
	must(t, err)
	waitFor(t, "approved", func() bool { pj := b.Status().PendingJoin; return pj != nil && pj.Approved })
	if err := b.CompleteJoin("0000"); code(err) != CodeJoinBadCode && conf != "0000" {
		t.Fatalf("wrong confirmation = %v", err)
	}
	must(t, b.CompleteJoin(conf))
	if b.Status().Phase != PhaseReady {
		t.Fatalf("b = %+v", b.Status())
	}
}

func TestFlowCreateApproveSyncRevoke(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.1", Password: "s3nha"})
	b.bridge.put(t, conn{ID: "b-only", Name: "lab", Host: "192.168.0.1"})
	if !a.Status().PromptWelcome || a.Status().Phase != PhaseOff || !a.Status().TestMode {
		t.Fatalf("first launch: %+v", a.Status())
	}
	create(t, a)
	_, err := a.Sync()
	must(t, err)
	approve(t, a, b, vault.RoleWriter)

	// B's first sync brings the vault in and adds its own record.
	_, err = b.Sync()
	must(t, err)
	if c, ok := b.bridge.get(t, "c1"); !ok || c.Password != "s3nha" {
		t.Fatalf("b c1 = %+v %v", c, ok)
	}
	if _, ok := b.bridge.get(t, "b-only"); !ok {
		t.Fatal("b lost its own record")
	}
	_, err = a.Sync()
	must(t, err)
	if _, ok := a.bridge.get(t, "b-only"); !ok {
		t.Fatal("a did not get b's record")
	}
	if len(a.ChangedMap()) != 1 || a.ChangedMap()["b-only"].DeviceName != "mac-b" {
		t.Fatalf("a changed map = %+v", a.ChangedMap())
	}

	// B changes the host; A gets it with the badge.
	b.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.99", Password: "s3nha"})
	_, err = b.Sync()
	must(t, err)
	_, err = a.Sync()
	must(t, err)
	if c, _ := a.bridge.get(t, "c1"); c.Host != "10.0.0.99" {
		t.Fatalf("a c1 = %+v", c)
	}
	ch := a.ChangedMap()["c1"]
	if ch.DeviceName != "mac-b" || strings.Join(ch.Fields, ",") != "host" {
		t.Fatalf("badge = %+v", ch)
	}
	// A's own edit after that is not sent back as a revert of B's.
	a.bridge.put(t, conn{ID: "c1", Name: "olt-renomeada", Host: "10.0.0.99", Password: "s3nha"})
	_, err = a.Sync()
	must(t, err)
	_, err = b.Sync()
	must(t, err)
	if c, _ := b.bridge.get(t, "c1"); c.Name != "olt-renomeada" || c.Host != "10.0.0.99" {
		t.Fatalf("b c1 = %+v", c)
	}

	devs, err := a.Devices()
	must(t, err)
	var bID string
	recov := 0
	for _, d := range devs {
		if d.Name == "mac-b" {
			bID = d.ID
		}
		if d.IsRecovery {
			recov++
		}
	}
	if len(devs) != 3 || recov != 1 || bID == "" {
		t.Fatalf("devices = %+v", devs)
	}
	if _, err := a.Revoke(a.Status().Device.ID); code(err) != CodeRevokeSelf {
		t.Fatalf("self revoke = %v", err)
	}
	res, err := a.Revoke(bID)
	must(t, err)
	if res.RecoveryStale {
		t.Fatal("revoking a writer marked the recovery key stale")
	}
	if len(res.Exposed) != 2 {
		t.Fatalf("exposed = %+v", res.Exposed)
	}
	if _, err := b.Sync(); code(err) != CodeRevoked || b.Status().Phase != PhaseRevoked {
		t.Fatalf("b after revoke: %v %+v", err, b.Status())
	}
	_, err = a.Sync()
	must(t, err)
}

func TestRecoverWithWords(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "h", Password: "p"})
	words := create(t, a)
	_, err := a.Sync()
	must(t, err)

	c := newDevice(t, shared, "mac-novo")
	must(t, c.SignIn())
	waitFor(t, "awaitingApproval", func() bool { return c.Status().Phase == PhaseAwaitingApproval })
	bad := slices3(words)
	bad[3], bad[4] = bad[4], bad[3]
	if err := c.RecoverWithWords(bad); code(err) != CodeRecoveryBadWords && code(err) != CodeRecoveryOther {
		t.Fatalf("bad words = %v", err)
	}
	must(t, c.RecoverWithWords(words))
	if c.Status().Phase != PhaseReady {
		t.Fatalf("c = %+v", c.Status())
	}
	_, err = c.Sync()
	must(t, err)
	if got, ok := c.bridge.get(t, "c1"); !ok || got.Password != "p" {
		t.Fatalf("c c1 = %+v", got)
	}
}

func TestSkipAndResume(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	must(t, a.Skip())
	if a.Status().PromptWelcome {
		t.Fatal("still prompting after Skip")
	}
	create(t, a)
	// Restart: same data dir resumes without the browser.
	a.Close()
	again, err := New(a.o)
	must(t, err)
	defer again.Close()
	again.Start()
	waitFor(t, "resumed", func() bool { return again.Status().Phase == PhaseReady })
	must(t, again.SignOut())
	if again.Status().Phase != PhaseOff || again.Status().Account != nil {
		t.Fatalf("after sign-out: %+v", again.Status())
	}
}

func TestQueueRewritesOpRefusedAfterRotation(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	c := newDevice(t, shared, "mac-c")
	create(t, a)
	approve(t, a, b, vault.RoleWriter)
	approve(t, a, c, vault.RoleWriter)
	for _, d := range []*device{a, b, c} {
		_, err := d.Sync()
		must(t, err)
	}
	// B writes with the roster it has; meanwhile A revokes C (new epoch).
	devs, _ := a.Devices()
	for _, d := range devs {
		if d.Name == "mac-c" {
			_, err := a.Revoke(d.ID)
			must(t, err)
		}
	}
	payload, err := records.Encode(time.Now(), []records.Change{{Kind: records.Connection, ID: "late",
		Create: true, Fields: records.Fields{"name": []byte(`"tardia"`), "host": []byte(`"h"`)}}})
	must(t, err)
	b.mu.Lock()
	b.queue = append(b.queue, queued{Payload: payload})
	_, err = b.flushLocked() // stale roster: written under the closed epoch
	b.mu.Unlock()
	must(t, err)
	b.bridge.put(t, conn{ID: "late", Name: "tardia", Host: "h"})

	res, err := b.Sync()
	must(t, err)
	if res.Pushed == 0 || b.Status().PendingOps != 0 {
		t.Fatalf("not re-appended: %+v, pending %d", res, b.Status().PendingOps)
	}
	_, err = a.Sync()
	must(t, err)
	if got, ok := a.bridge.get(t, "late"); !ok || got.Name != "tardia" {
		t.Fatalf("a did not get the rewritten op: %+v %v", got, ok)
	}
}

func TestRevokingAdminMarksRecoveryStale(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	create(t, a)
	approve(t, a, b, vault.RoleAdmin)
	res, err := a.Revoke(b.Status().Device.ID)
	must(t, err)
	if !res.RecoveryStale {
		t.Fatal("revoking an admin should suggest a new recovery key")
	}
}

func recoveryMember(t *testing.T, d *device) string {
	t.Helper()
	devs, err := d.Devices()
	must(t, err)
	id := ""
	for _, x := range devs {
		if x.IsRecovery {
			if id != "" {
				t.Fatalf("two recovery members: %+v", devs)
			}
			id = x.ID
		}
	}
	if id == "" {
		t.Fatal("no recovery member")
	}
	return id
}

func confirmShown(t *testing.T, d *device) []string {
	t.Helper()
	words, err := d.RecoveryWords()
	must(t, err)
	idx := d.Status().RecoveryCheck
	answers := make([]string, len(idx))
	for i, p := range idx {
		answers[i] = words[p]
	}
	must(t, d.ConfirmRecovery(idx, answers))
	return words
}

func TestRegenerateRecoveryAfterRevokingAdmin(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "h", Password: "p"})
	oldWords := create(t, a)
	_, err := a.Sync()
	must(t, err)
	approve(t, a, b, vault.RoleAdmin)
	oldRecov := recoveryMember(t, a)
	res, err := a.Revoke(b.Status().Device.ID)
	must(t, err)
	if !res.RecoveryStale {
		t.Fatal("RecoveryStale not set")
	}
	revokedAt := a.v.Trust().Counter

	// Non-admins can't; outside ready it's the wrong phase.
	if err := b.RegenerateRecovery(); code(err) != CodeRevoked || b.Status().Phase != PhaseRevoked {
		t.Fatalf("revoked device regenerate = %v, %+v", err, b.Status().Phase)
	}

	// Cancel: nothing written, the old key stays.
	must(t, a.RegenerateRecovery())
	if st := a.Status(); st.Phase != PhaseShowRecovery || !st.RecoveryRegen || len(st.RecoveryCheck) != 3 {
		t.Fatalf("regen status = %+v", st)
	}
	trust := a.v.Trust()
	must(t, a.CancelRecovery())
	if a.Status().Phase != PhaseReady || a.v.Trust() != trust || recoveryMember(t, a) != oldRecov {
		t.Fatal("cancel changed the vault")
	}

	// Wrong words: still nothing written.
	must(t, a.RegenerateRecovery())
	idx := a.Status().RecoveryCheck
	if err := a.ConfirmRecovery(idx, []string{"zoo", "zoo", "zoo"}); code(err) != CodeRecoveryMismatch {
		t.Fatalf("wrong words = %v", err)
	}
	if a.v.Trust() != trust {
		t.Fatal("wrong words wrote something")
	}
	if a.Status().RecoveryKeys != 1 {
		t.Fatalf("recovery keys before = %d", a.Status().RecoveryKeys)
	}
	newWords := confirmShown(t, a)
	if a.Status().RecoveryKeys != 1 {
		t.Fatalf("recovery keys after = %d", a.Status().RecoveryKeys)
	}
	if a.Status().Phase != PhaseReady || a.Status().RecoveryRegen {
		t.Fatalf("after confirm: %+v", a.Status())
	}
	if recoveryMember(t, a) == oldRecov || strings.Join(newWords, " ") == strings.Join(oldWords, " ") {
		t.Fatal("recovery key not replaced")
	}

	// The old phrase no longer opens the vault; the new one does, from an
	// anchor after the admin's revocation.
	c := newDevice(t, shared, "mac-c")
	must(t, c.SignIn())
	waitFor(t, "awaitingApproval", func() bool { return c.Status().Phase == PhaseAwaitingApproval })
	if err := c.RecoverWithWords(oldWords); err == nil {
		t.Fatal("old phrase still works")
	}
	must(t, c.RecoverWithWords(newWords))
	if c.Status().Phase != PhaseReady {
		t.Fatalf("c = %+v", c.Status())
	}
	if got := c.v.Trust().Counter; got <= revokedAt {
		t.Fatalf("recovered at %d, not after the revocation at %d", got, revokedAt)
	}
	_, err = c.Sync()
	must(t, err)
	if got, ok := c.bridge.get(t, "c1"); !ok || got.Password != "p" {
		t.Fatalf("c c1 = %+v", got)
	}
}

func TestRegenerateNeedsReadyAdmin(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	if err := a.RegenerateRecovery(); code(err) != CodeWrongPhase {
		t.Fatalf("before the vault = %v", err)
	}
	if err := a.CancelRecovery(); code(err) != CodeWrongPhase {
		t.Fatalf("cancel without regen = %v", err)
	}
	b := newDevice(t, shared, "mac-b")
	create(t, a)
	approve(t, a, b, vault.RoleWriter)
	if err := b.RegenerateRecovery(); code(err) != CodeNotAdmin {
		t.Fatalf("writer = %v", err)
	}
	// Creation's showRecovery can't be cancelled as a replacement.
	n := newDevice(t, t.TempDir(), "mac-n")
	must(t, n.SignIn())
	waitFor(t, "showRecovery", func() bool { return n.Status().Phase == PhaseShowRecovery })
	if n.Status().RecoveryRegen {
		t.Fatal("creation flagged as regen")
	}
	if err := n.CancelRecovery(); code(err) != CodeWrongPhase {
		t.Fatalf("cancel during creation = %v", err)
	}
}

func TestTwoRecoveryKeysAreReported(t *testing.T) {
	// The app closed between adding the new key and revoking the old.
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	create(t, a)
	_, k, err := recovery.Generate()
	must(t, err)
	must(t, a.v.AddMember(vault.MemberOf(k, "", recovery.RecoveryName, vault.RoleAdmin)))
	if n := a.Status().RecoveryKeys; n != 2 {
		t.Fatalf("recovery keys = %d, want 2", n)
	}
	must(t, a.RegenerateRecovery())
	confirmShown(t, a)
	if n := a.Status().RecoveryKeys; n != 1 {
		t.Fatalf("after regenerating: %d", n)
	}
}
