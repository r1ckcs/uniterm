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
	err := verifyHostKey(app, "", "example.com:22", key, func(HostKeyPrompt) (bool, error) {
		prompted = true
		return false, nil
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
	err := verifyHostKey(app, "", "new.example:22", key, func(p HostKeyPrompt) (bool, error) {
		got = p
		return false, nil
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
	accept := func(HostKeyPrompt) (bool, error) { return true, nil }
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

func TestVerifyHostKeyChangedHardFails(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	old := newEd25519Key(t)
	writeKnown(t, app, "srv:22", old)
	attacker := newEd25519Key(t)
	prompted := false
	err := verifyHostKey(app, "", "srv:22", attacker, func(HostKeyPrompt) (bool, error) {
		prompted = true
		return true, nil
	}, true)
	if err == nil {
		t.Fatal("changed key must fail even when the prompt would accept")
	}
	if prompted {
		t.Fatal("changed key must never prompt")
	}
	if !strings.Contains(err.Error(), ssh.FingerprintSHA256(attacker)) {
		t.Fatalf("error should carry the new fingerprint: %v", err)
	}
}

func TestVerifyHostKeyOtherTypePrompts(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	writeKnown(t, app, "srv:22", newRSAKey(t))
	ed := newEd25519Key(t)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "srv:22", ed, func(p HostKeyPrompt) (bool, error) {
		got = p
		return false, nil
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
