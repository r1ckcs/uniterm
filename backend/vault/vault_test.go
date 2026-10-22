package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func newKey(t *testing.T) *SoftwareDeviceKey {
	t.Helper()
	k, err := NewSoftwareDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// team: admin A (vault creator), writer W, reader R, all opened from the
// genesis anchor like joining devices.
type team struct {
	store      *FileStore
	genesis    TrustState
	a, w, r    *SoftwareDeviceKey
	va, vw, vr *Vault
}

func newTeam(t *testing.T) *team {
	t.Helper()
	tm := &team{store: newStore(t), a: newKey(t), w: newKey(t), r: newKey(t)}
	var err error
	if tm.va, err = Create(tm.store, "pessoal", tm.a, "henrique", "mac-a"); err != nil {
		t.Fatal(err)
	}
	tm.genesis = tm.va.Trust()
	if err := tm.va.AddMember(MemberOf(tm.w, "henrique", "mac-w", RoleWriter)); err != nil {
		t.Fatal(err)
	}
	if err := tm.va.AddMember(MemberOf(tm.r, "colega", "android-r", RoleReader)); err != nil {
		t.Fatal(err)
	}
	if tm.vw, err = Open(tm.store, tm.w, tm.genesis); err != nil {
		t.Fatal(err)
	}
	if tm.vr, err = Open(tm.store, tm.r, tm.genesis); err != nil {
		t.Fatal(err)
	}
	return tm
}

func payloads(ops []Op) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = string(op.Payload)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	tm := newTeam(t)
	if _, err := tm.va.Append([]byte(`{"host":"10.0.0.1"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.vw.Append([]byte(`{"host":"10.0.0.2"}`)); err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]*Vault{"admin": tm.va, "writer": tm.vw, "reader": tm.vr} {
		ops, rejected, err := v.ReadLog()
		if err != nil || len(rejected) != 0 {
			t.Fatalf("%s: ReadLog err=%v rejected=%v", name, err, rejected)
		}
		got := strings.Join(payloads(ops), " ")
		if !strings.Contains(got, "10.0.0.1") || !strings.Contains(got, "10.0.0.2") || len(ops) != 2 {
			t.Errorf("%s: ops = %s", name, got)
		}
	}
	// The trust state advances to the newest roster (genesis + 2 adds).
	if tr := tm.vr.Trust(); tr.Counter != 3 || tr.Hash == tm.genesis.Hash {
		t.Errorf("trust = %+v", tr)
	}
	// Reopening from the persisted state works; nothing in storage is plaintext.
	if _, err := Open(tm.store, tm.w, tm.vw.Trust()); err != nil {
		t.Fatal(err)
	}
	names, _ := tm.store.List("")
	for _, n := range names {
		data, _, _ := tm.store.Get(n)
		if bytes.Contains(data, []byte("10.0.0.")) {
			t.Errorf("%s holds plaintext", n)
		}
	}
}

func TestSoftwareDeviceKeyMarshal(t *testing.T) {
	k := newKey(t)
	data, err := k.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := UnmarshalSoftwareDeviceKey(data)
	if err != nil {
		t.Fatal(err)
	}
	if IDOf(k2) != IDOf(k) {
		t.Fatal("device id changed across marshal")
	}
	// The restored key opens the vault the original created.
	s := newStore(t)
	v, err := Create(s, "pessoal", k, "h", "mac")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(s, k2, v.Trust()); err != nil {
		t.Fatal(err)
	}
}

// tamper rewrites a stored signed object's body through edit.
func tamper(t *testing.T, s Store, name string, edit func(body []byte) []byte) {
	t.Helper()
	data, _, err := s.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	var obj signed
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatal(err)
	}
	obj.Body = edit(obj.Body)
	out, _ := json.Marshal(obj)
	if _, err := s.Put(name, out); err != nil {
		t.Fatal(err)
	}
}

func TestTamperedRosterRefused(t *testing.T) {
	tm := newTeam(t)
	// Promote the reader to admin in the newest roster without re-signing.
	tamper(t, tm.store, rosterName(3), func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"role":"reader"`), []byte(`"role":"admin"`), 1)
	})
	if _, err := Open(tm.store, tm.r, tm.genesis); !errors.Is(err, errBadSignature) {
		t.Fatalf("Open with tampered roster = %v, want bad signature", err)
	}
}

func TestRosterSignedByNonAdminRefused(t *testing.T) {
	tm := newTeam(t)
	// The writer signs a roster 4 that makes it admin: valid signature, but
	// the writer is not an admin of roster 3.
	r := tm.vw.Roster()
	r.Counter, r.Prev = 4, tm.vw.Trust().Hash
	for i := range r.Members {
		if r.Members[i].DeviceID == IDOf(tm.w) {
			r.Members[i].Role = RoleAdmin
		}
	}
	if _, err := writeRoster(tm.store, tm.w, &r); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(tm.store, tm.a, tm.genesis); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("Open = %v, want ErrNotAdmin", err)
	}
}

func TestTamperedOpRefused(t *testing.T) {
	tm := newTeam(t)
	op, err := tm.vw.Append([]byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.vw.Append([]byte("other")); err != nil {
		t.Fatal(err)
	}
	// Lamport clock changed in the body: signature no longer matches.
	tamper(t, tm.store, op.Name, func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"lamport":1`), []byte(`"lamport":9`), 1)
	})
	ops, rejected, err := tm.va.ReadLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || string(ops[0].Payload) != "other" || len(rejected) != 1 || rejected[0].Name != op.Name {
		t.Fatalf("ops=%v rejected=%v", payloads(ops), rejected)
	}
}

func TestMovedOpRefused(t *testing.T) {
	tm := newTeam(t)
	op, err := tm.vw.Append([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	data, _, _ := tm.store.Get(op.Name)
	// Same valid object replayed under another sequence number.
	if _, err := tm.store.Create(opName(IDOf(tm.w), 7), data); err != nil {
		t.Fatal(err)
	}
	ops, rejected, _ := tm.va.ReadLog()
	if len(ops) != 1 || len(rejected) != 1 {
		t.Fatalf("ops=%d rejected=%v", len(ops), rejected)
	}
}

func TestRosterRollbackRefused(t *testing.T) {
	tm := newTeam(t)
	trusted := tm.vr.Trust() // counter 3
	// Storage drops the newest version (deleted, or an old copy restored).
	if err := tm.store.Delete(rosterName(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(tm.store, tm.r, trusted); !errors.Is(err, ErrRollback) {
		t.Fatalf("Open after rollback = %v, want ErrRollback", err)
	}
}

func TestRosterForkRefused(t *testing.T) {
	tm := newTeam(t)
	trusted := tm.vr.Trust() // counter 3
	// A different roster 3, validly signed by the admin (e.g. a stolen
	// admin key replaying history): same counter, different hash.
	r := tm.va.Roster()
	r.Members = r.Members[:2]
	r.Epoch++
	r.KeyChecks = append(r.KeyChecks, strings.Repeat("0", 64))
	if err := tm.store.Delete(rosterName(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := writeRoster(tm.store, tm.a, &r); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(tm.store, tm.r, trusted); !errors.Is(err, ErrFork) {
		t.Fatalf("Open on forked roster = %v, want ErrFork", err)
	}
}

func TestRevokedDeviceCannotReadNewEpoch(t *testing.T) {
	tm := newTeam(t)
	if _, err := tm.vw.Append([]byte("before")); err != nil {
		t.Fatal(err)
	}
	if err := tm.va.Revoke(IDOf(tm.r)); err != nil {
		t.Fatal(err)
	}
	if got := tm.va.Roster().Epoch; got != 2 {
		t.Fatalf("epoch after revoke = %d", got)
	}
	if err := tm.vw.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.vw.Append([]byte("after")); err != nil {
		t.Fatal(err)
	}

	// The revoked device's client refuses to open the vault…
	if _, err := Open(tm.store, tm.r, tm.vr.Trust()); !errors.Is(err, ErrNotMember) {
		t.Fatalf("revoked Open = %v, want ErrNotMember", err)
	}
	// …and a modified client gets nothing either: no wrap of epoch 2 is
	// addressed to it, the others' wraps don't open with its key, and its
	// epoch-1 key doesn't decrypt epoch-2 ops.
	if _, _, err := tm.store.Get(wrapName(IDOf(tm.r), 2)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrap for revoked device exists: %v", err)
	}
	r2 := tm.va.Roster()
	for _, id := range []string{IDOf(tm.a), IDOf(tm.w)} {
		wrapped, _, err := tm.store.Get(wrapName(id, 2))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := unwrapKey(tm.r, &r2, 2, wrapped); err == nil {
			t.Fatalf("revoked device opened %s's wrap", id)
		}
	}
	oldKeys := map[uint64][]byte{1: tm.vr.keys[1], 2: tm.vr.keys[1]}
	names, _ := tm.store.List("ops/" + IDOf(tm.w) + "/")
	data, _, _ := tm.store.Get(names[len(names)-1])
	if _, err := openOp(&r2, oldKeys, names[len(names)-1], data); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("epoch-2 op with the epoch-1 key: %v", err)
	}

	// Remaining members read both epochs.
	for _, v := range []*Vault{tm.va, tm.vw} {
		ops, rejected, err := v.ReadLog()
		if err != nil || len(rejected) != 0 || strings.Join(payloads(ops), ",") != "before,after" {
			t.Fatalf("ops=%v rejected=%v err=%v", payloads(ops), rejected, err)
		}
	}
}

func TestRevokedWriterOpsRefused(t *testing.T) {
	tm := newTeam(t)
	if err := tm.va.Revoke(IDOf(tm.w)); err != nil {
		t.Fatal(err)
	}
	// The revoked writer still knows the epoch-1 key and signs a backdated op.
	obj, err := sealOp(tm.w, "pessoal", 1, 1, 1, tm.vw.keys[1], []byte("honeypot"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.store.Create(opName(IDOf(tm.w), 1), obj); err != nil {
		t.Fatal(err)
	}
	ops, rejected, _ := tm.va.ReadLog()
	if len(ops) != 0 || len(rejected) != 1 || !strings.Contains(rejected[0].Reason, "not a member") {
		t.Fatalf("ops=%v rejected=%v", payloads(ops), rejected)
	}
}

func TestReaderOpRefused(t *testing.T) {
	tm := newTeam(t)
	if _, err := tm.vr.Append([]byte("x")); !errors.Is(err, ErrNotWriter) {
		t.Fatalf("reader Append = %v, want ErrNotWriter", err)
	}
	// A modified client signs and stores the op anyway: everyone refuses it.
	obj, err := sealOp(tm.r, "pessoal", 1, 1, 1, tm.vr.keys[1], []byte(`{"host":"honeypot"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.store.Create(opName(IDOf(tm.r), 1), obj); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*Vault{tm.va, tm.vw, tm.vr} {
		ops, rejected, _ := v.ReadLog()
		if len(ops) != 0 || len(rejected) != 1 || !strings.Contains(rejected[0].Reason, "not a writer") {
			t.Fatalf("ops=%v rejected=%v", payloads(ops), rejected)
		}
	}
}

func TestPlantedWrapRefused(t *testing.T) {
	// Someone with write access to storage (but no admin key) replaces a
	// device's wrap with one of a key they know: the roster's key check
	// catches it, so the device never encrypts with an attacker's key.
	tm := newTeam(t)
	evil, _ := newVaultKey()
	wrapped, err := wrapKey(tm.w.AgreementPublicKey(), "pessoal", 1, evil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.store.Put(wrapName(IDOf(tm.w), 1), wrapped); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(tm.store, tm.w, tm.genesis); !errors.Is(err, errBadWrap) {
		t.Fatalf("Open with planted wrap = %v, want errBadWrap", err)
	}
}

func TestRevokeWithoutRotationRefused(t *testing.T) {
	tm := newTeam(t)
	// An admin-signed roster that drops a member but keeps the epoch.
	r := tm.va.Roster()
	r.Counter, r.Prev = 4, tm.va.Trust().Hash
	r.Members = r.Members[:2]
	if _, err := writeRoster(tm.store, tm.a, &r); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(tm.store, tm.w, tm.genesis); err == nil || !strings.Contains(err.Error(), "without rotating") {
		t.Fatalf("Open = %v, want refusal", err)
	}
}

func TestConcurrentWriters(t *testing.T) {
	tm := newTeam(t)
	// A second handle on the writer's device (another window or process)
	// competes for the same sequence numbers.
	vw2, err := Open(tm.store, tm.w, tm.genesis)
	if err != nil {
		t.Fatal(err)
	}
	const n = 25
	var wg sync.WaitGroup
	errs := make(chan error, 3*n)
	for i, v := range []*Vault{tm.va, tm.vw, vw2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range n {
				if _, err := v.Append(fmt.Appendf(nil, "w%d-%d", i, j)); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	ops, rejected, err := tm.vr.ReadLog()
	if err != nil || len(rejected) != 0 {
		t.Fatalf("err=%v rejected=%v", err, rejected)
	}
	if len(ops) != 3*n {
		t.Fatalf("got %d ops, want %d (an append was lost)", len(ops), 3*n)
	}
	seen := map[string]bool{}
	for _, op := range ops {
		if seen[string(op.Payload)] {
			t.Fatalf("duplicate %s", op.Payload)
		}
		seen[string(op.Payload)] = true
	}
	// Two readers see the same order.
	ops2, _, _ := tm.va.ReadLog()
	if strings.Join(payloads(ops), ",") != strings.Join(payloads(ops2), ",") {
		t.Fatal("readers disagree on log order")
	}
}

func TestFileStore(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create("ops/a/000000000001", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("ops/a/000000000001", []byte("y")); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create = %v, want ErrExists", err)
	}
	data, ver, err := s.Get("ops/a/000000000001")
	if err != nil || string(data) != "x" || ver == "" {
		t.Fatalf("Get = %q %q %v", data, ver, err)
	}
	ver2, _ := s.Put("ops/a/000000000001", []byte("z"))
	if ver2 == ver {
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
	names, _ := s.List("ops/")
	if len(names) != 1 {
		t.Errorf("List = %v", names)
	}
}
