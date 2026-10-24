package session

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newEd25519Key(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newRSAKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func writeKnown(t *testing.T, path, host string, key ssh.PublicKey) {
	t.Helper()
	line := knownhosts.Line([]string{knownhosts.Normalize(host)}, key) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyHostKeyKnown(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	key := newEd25519Key(t)
	writeKnown(t, app, "example.com:22", key)
	prompted := false
	err := verifyHostKey(app, "", "example.com:22", key, func(HostKeyPrompt) (HostKeyAnswer, error) {
		prompted = true
		return HostKeyReject, nil
	}, true)
	if err != nil || prompted {
		t.Fatalf("known key: err=%v prompted=%v", err, prompted)
	}
}

func TestVerifyHostKeyKnownFromUserFileNonStandardPort(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user_known_hosts")
	key := newEd25519Key(t)
	writeKnown(t, user, "10.0.0.5:2222", key)
	if err := verifyHostKey(filepath.Join(dir, "missing"), user, "10.0.0.5:2222", key, nil, false); err != nil {
		t.Fatalf("key in user known_hosts must be accepted: %v", err)
	}
	// Same host, different port is a different identity.
	if err := verifyHostKey(filepath.Join(dir, "missing"), user, "10.0.0.5:22", key, nil, false); err == nil {
		t.Fatal("different port must not match")
	}
}

func TestVerifyHostKeyUnknownRejected(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	key := newEd25519Key(t)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "new.example:22", key, func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReject, nil
	}, true)
	if err == nil {
		t.Fatal("rejected unknown key must fail")
	}
	if got.Fingerprint != ssh.FingerprintSHA256(key) || got.Host != "new.example:22" || got.KeyType != key.Type() {
		t.Fatalf("prompt payload wrong: %+v", got)
	}
	if _, statErr := os.Stat(app); !os.IsNotExist(statErr) {
		t.Fatal("rejected key must not be persisted")
	}
}

func TestVerifyHostKeyUnknownNoPromptFailsWithFingerprint(t *testing.T) {
	dir := t.TempDir()
	key := newEd25519Key(t)
	err := verifyHostKey(filepath.Join(dir, "known_hosts"), "", "h:22", key, nil, false)
	if err == nil || !strings.Contains(err.Error(), ssh.FingerprintSHA256(key)) {
		t.Fatalf("want error with fingerprint, got %v", err)
	}
}

func TestVerifyHostKeyUnknownAcceptedPersists(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "sub", "known_hosts")
	key := newEd25519Key(t)
	accept := func(HostKeyPrompt) (HostKeyAnswer, error) { return HostKeyTrust, nil }
	if err := verifyHostKey(app, "", "h.example:2200", key, accept, true); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(app)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts perm = %v, want 0600", fi.Mode().Perm())
	}
	// Second connection: no prompt needed.
	if err := verifyHostKey(app, "", "h.example:2200", key, nil, false); err != nil {
		t.Fatalf("persisted key not trusted: %v", err)
	}
}

// A changed key asks, but the unknown-key answer (Trust) never replaces it:
// a prompter that does not know about changed keys cannot let one through.
func TestVerifyHostKeyChangedTrustDoesNotReplace(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	old := newEd25519Key(t)
	writeKnown(t, app, "srv:22", old)
	before, _ := os.ReadFile(app)
	attacker := newEd25519Key(t)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "srv:22", attacker, func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyTrust, nil
	}, true)
	if err == nil {
		t.Fatal("Trust must not accept a changed key")
	}
	if !got.Changed || !got.CanReplace || len(got.OldKeys) != 1 ||
		got.OldKeys[0].Fingerprint != ssh.FingerprintSHA256(old) || got.OldKeys[0].File != "app" ||
		got.Fingerprint != ssh.FingerprintSHA256(attacker) {
		t.Fatalf("changed prompt payload: %+v", got)
	}
	if after, _ := os.ReadFile(app); string(after) != string(before) {
		t.Fatal("rejected change modified known_hosts")
	}
}

func TestVerifyHostKeyOtherTypePrompts(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	writeKnown(t, app, "srv:22", newRSAKey(t))
	ed := newEd25519Key(t)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "srv:22", ed, func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReject, nil
	}, true)
	if err == nil || !got.OtherKeyTypesKnown {
		t.Fatalf("different key type should prompt with warning; err=%v prompt=%+v", err, got)
	}
}

func TestVerifyHostKeyIgnoresMalformedUserFile(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	user := filepath.Join(dir, "user_known_hosts")
	key := newEd25519Key(t)
	writeKnown(t, app, "h:22", key)
	if err := os.WriteFile(user, []byte("this is not a known_hosts line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyHostKey(app, user, "h:22", key, nil, false); err != nil {
		t.Fatalf("malformed user file must not break trusted app entries: %v", err)
	}
}
