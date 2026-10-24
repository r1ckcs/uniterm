package session

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func knownLine(host string, key ssh.PublicKey) string {
	return knownhosts.Line([]string{knownhosts.Normalize(host)}, key)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func answer(a HostKeyAnswer, err error) HostKeyPrompter {
	return func(HostKeyPrompt) (HostKeyAnswer, error) { return a, err }
}

// Replacing removes every entry of that host:port (any key type, hashed
// too), keeps everything else, writes the new key, 0600.
func TestChangedKeyReplaceAccepted(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	oldEd, oldRSA, other, next := newEd25519Key(t), newRSAKey(t), newEd25519Key(t), newEd25519Key(t)
	hashed := knownhosts.HashHostname(knownhosts.Normalize("srv:22"))
	writeLines(t, app,
		"# my comment",
		knownLine("srv:22", oldEd),
		knownLine("other:22", other),
		"srv,other2 "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(oldRSA)))+" note",
		hashed+" "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(oldEd))),
		knownLine("srv:2222", oldEd), // other port: another identity, kept
	)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "srv:22", next, func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReplace, nil
	}, true)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if !got.Changed || !got.CanReplace || got.Blocker != "" {
		t.Fatalf("prompt = %+v", got)
	}
	out := readFile(t, app)
	for _, want := range []string{"# my comment", knownLine("other:22", other), "other2 ssh-rsa", " note", knownLine("srv:2222", oldEd), knownLine("srv:22", next)} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, hashed) || strings.Contains(out, knownLine("srv:22", oldEd)) || strings.Contains(out, "srv,") {
		t.Errorf("old srv entries left:\n%s", out)
	}
	if fi, _ := os.Stat(app); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
	if err := verifyHostKey(app, "", "srv:22", next, nil, false); err != nil {
		t.Fatalf("new key not trusted: %v", err)
	}
	// Neither old key is trusted any more, whatever its type.
	if err := verifyHostKey(app, "", "srv:22", oldRSA, nil, false); err == nil {
		t.Fatal("old RSA key still trusted")
	}
	if err := verifyHostKey(app, "", "srv:22", oldEd, nil, false); err == nil {
		t.Fatal("old ed25519 key still trusted")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".known_hosts-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestChangedKeyRejectedOrTimeoutKeepsFile(t *testing.T) {
	for name, p := range map[string]HostKeyPrompter{
		"reject":  answer(HostKeyReject, nil),
		"trust":   answer(HostKeyTrust, nil),
		"timeout": answer(HostKeyReplace, errors.New("no answer within 2m0s")), // an error wins over any answer
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			app := filepath.Join(dir, "known_hosts")
			writeLines(t, app, knownLine("srv:22", newEd25519Key(t)))
			before := readFile(t, app)
			if err := verifyHostKey(app, "", "srv:22", newEd25519Key(t), p, true); err == nil {
				t.Fatal("connection accepted")
			}
			if readFile(t, app) != before {
				t.Fatal("known_hosts changed")
			}
		})
	}
}

// Replace is only an answer to a changed key: it never trusts an unknown one.
func TestUnknownKeyReplaceAnswerRejects(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	if err := verifyHostKey(app, "", "new:22", newEd25519Key(t), answer(HostKeyReplace, nil), true); err == nil {
		t.Fatal("Replace accepted an unknown key")
	}
	if _, err := os.Stat(app); !os.IsNotExist(err) {
		t.Fatal("unknown key persisted")
	}
}

func TestRevokedKeyNeverPrompts(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	bad := newEd25519Key(t)
	writeLines(t, app,
		"@revoked * "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(bad))),
		knownLine("srv:22", newEd25519Key(t)),
	)
	prompted := false
	err := verifyHostKey(app, "", "srv:22", bad, func(HostKeyPrompt) (HostKeyAnswer, error) {
		prompted = true
		return HostKeyReplace, nil
	}, true)
	if err == nil || prompted {
		t.Fatalf("revoked: err=%v prompted=%v", err, prompted)
	}
}

// The old key only in ~/.ssh/known_hosts (Henrique's addendum, 24/10/2026):
// replacing works right away without touching that file. The new key goes
// to the app file, which takes precedence for the host, so the old key
// left in the user's file no longer passes.
func TestChangedKeyInUserFileReplacedInAppFile(t *testing.T) {
	for host, cmd := range map[string]string{
		"srv:22":        "ssh-keygen -R srv",
		"10.0.0.5:2222": "ssh-keygen -R '[10.0.0.5]:2222'",
	} {
		dir := t.TempDir()
		app := filepath.Join(dir, "data", "known_hosts")
		user := filepath.Join(dir, "user_known_hosts")
		old, next := newEd25519Key(t), newEd25519Key(t)
		writeLines(t, user, knownLine(host, old))
		userBefore := readFile(t, user)
		var got HostKeyPrompt
		err := verifyHostKey(app, user, host, next, func(p HostKeyPrompt) (HostKeyAnswer, error) {
			got = p
			return HostKeyReplace, nil
		}, true)
		if err != nil {
			t.Fatalf("%s: replace: %v", host, err)
		}
		if !got.Changed || !got.CanReplace || !got.UserFileKept || got.RemoveCommand != cmd || got.OldKeys[0].File != "user" {
			t.Fatalf("%s: prompt = %+v", host, got)
		}
		if readFile(t, user) != userBefore {
			t.Fatal("user known_hosts edited")
		}
		if fi, err := os.Stat(app); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("app file: %v %v", fi, err)
		}
		if err := verifyHostKey(app, user, host, next, nil, false); err != nil {
			t.Fatalf("%s: new key not trusted: %v", host, err)
		}
		// The old key is still in the user's file but no longer passes.
		if err := verifyHostKey(app, user, host, old, nil, false); err == nil || !strings.Contains(err.Error(), "HAS CHANGED") {
			t.Fatalf("%s: old key from the user file: %v", host, err)
		}
	}
}

// Without an app entry for the host the user's file still decides alone.
func TestUserFileStillTrustedWithoutAppEntry(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	user := filepath.Join(dir, "user_known_hosts")
	k := newEd25519Key(t)
	writeLines(t, app, knownLine("other:22", newEd25519Key(t)))
	writeLines(t, user, knownLine("srv:22", k))
	if err := verifyHostKey(app, user, "srv:22", k, nil, false); err != nil {
		t.Fatal(err)
	}
	// @revoked in the user file still wins over a trusted app entry.
	writeLines(t, app, knownLine("srv:22", k))
	writeLines(t, user, "@revoked * "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))))
	if err := verifyHostKey(app, user, "srv:22", k, answer(HostKeyReplace, nil), true); err == nil {
		t.Fatal("revoked key in the user file passed")
	}
}

// A wildcard line cannot be "this host's entry": no replace is offered.
func TestChangedKeyPatternLineIsNotReplaced(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	writeLines(t, app, "*.example.com "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(newEd25519Key(t)))))
	before := readFile(t, app)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "a.example.com:22", newEd25519Key(t), func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReplace, nil
	}, true)
	if err == nil || got.CanReplace || got.Blocker != "pattern" {
		t.Fatalf("err=%v prompt=%+v", err, got)
	}
	if readFile(t, app) != before {
		t.Fatal("pattern file changed")
	}
}

// Paths that cannot ask (cwd probe, status, the no-prompt callback) never
// replace, even with a prompter that would.
func TestChangedKeyNoPromptNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	writeLines(t, app, knownLine("srv:22", newEd25519Key(t)))
	before := readFile(t, app)
	called := false
	replace := func(HostKeyPrompt) (HostKeyAnswer, error) { called = true; return HostKeyReplace, nil }
	err := verifyHostKey(app, "", "srv:22", newEd25519Key(t), replace, false)
	if err == nil || called || !strings.Contains(err.Error(), "HAS CHANGED") {
		t.Fatalf("no-prompt: err=%v called=%v", err, called)
	}
	// The real no-prompt callback with a global prompter installed.
	SetKnownHostsPath(app)
	SetHostKeyPrompter(replace)
	t.Cleanup(func() { SetKnownHostsPath(""); SetHostKeyPrompter(nil) })
	if err := hostKeyCallbackNoPrompt()("srv:22", nil, newEd25519Key(t)); err == nil || called {
		t.Fatalf("hostKeyCallbackNoPrompt: err=%v called=%v", err, called)
	}
	if readFile(t, app) != before {
		t.Fatal("known_hosts changed")
	}
}

// Parallel dials of one host (terminal + SFTP + monitor) ask once, whether
// the answer is replace or cancel; a dial after a cancel asks again.
func TestChangedKeyParallelAsksOnce(t *testing.T) {
	for _, ans := range []HostKeyAnswer{HostKeyReplace, HostKeyReject} {
		dir := t.TempDir()
		app := filepath.Join(dir, "known_hosts")
		writeLines(t, app, knownLine("srv:22", newEd25519Key(t)))
		next := newEd25519Key(t)
		var calls atomic.Int32
		p := func(HostKeyPrompt) (HostKeyAnswer, error) {
			calls.Add(1)
			time.Sleep(100 * time.Millisecond) // the others queue behind the prompt
			return ans, nil
		}
		var wg sync.WaitGroup
		errs := make([]error, 6)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = verifyHostKey(app, "", "srv:22", next, p, true)
			}(i)
		}
		wg.Wait()
		if n := calls.Load(); n != 1 {
			t.Fatalf("answer %v: prompted %d times, want 1", ans, n)
		}
		for i, err := range errs {
			if (err == nil) != (ans == HostKeyReplace) {
				t.Fatalf("answer %v: dial %d err = %v", ans, i, err)
			}
		}
		if ans == HostKeyReject {
			verifyHostKey(app, "", "srv:22", next, p, true)
			if n := calls.Load(); n != 2 {
				t.Fatalf("a new dial after cancel prompted %d times in total, want 2", n)
			}
		}
	}
}

func TestRemoveCommandOnlyForSafeHosts(t *testing.T) {
	for host, want := range map[string]string{
		"srv.example:22":  "ssh-keygen -R srv.example",
		"[fe80::1]:22":    "ssh-keygen -R fe80::1",
		"[fe80::1]:2200":  "ssh-keygen -R '[fe80::1]:2200'",
		"a;rm -rf ~:22":   "",
		"$(id):22":        "",
		"-oProxy=x:22":    "",
		"host'quote:2222": "",
	} {
		if got := removeCommand(host); got != want {
			t.Errorf("%q: %q, want %q", host, got, want)
		}
	}
}

func TestForgetHostKey(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	user := filepath.Join(dir, "user_known_hosts")
	keep := knownLine("other:22", newEd25519Key(t))
	writeLines(t, app, knownLine("srv:2222", newEd25519Key(t)), knownLine("srv:2222", newRSAKey(t)), keep)
	writeLines(t, user, knownLine("srv:2222", newEd25519Key(t)))
	userBefore := readFile(t, user)
	res, err := forgetHostKey(app, user, "srv:2222")
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 || !res.InUserFile || res.RemoveCommand != "ssh-keygen -R '[srv]:2222'" {
		t.Fatalf("res = %+v", res)
	}
	if got := readFile(t, app); got != keep+"\n" {
		t.Fatalf("app file = %q", got)
	}
	if readFile(t, user) != userBefore {
		t.Fatal("user file edited")
	}
	if res, err := forgetHostKey(filepath.Join(dir, "missing"), "", "srv:22"); err != nil || res.Removed != 0 {
		t.Fatalf("missing file: %+v %v", res, err)
	}
	if _, err := ForgetHostKey("", 22); err == nil {
		t.Fatal("empty host accepted")
	}
}

// The inline prompt: only the typed word "replace" replaces; Enter, "yes"
// and Ctrl+C cancel; a key the app cannot replace never reads input.
func TestTerminalChangedKeyPrompt(t *testing.T) {
	p := HostKeyPrompt{Host: "srv:22", KeyType: "ssh-ed25519", Fingerprint: "SHA256:new", Changed: true, CanReplace: true,
		OldKeys: []HostKeyOld{{KeyType: "ssh-ed25519", Fingerprint: "SHA256:old", File: "app"}}}
	for input, want := range map[string]HostKeyAnswer{
		"\r":             HostKeyReject,
		"yes\r":          HostKeyReject,
		"REPLACE\r":      HostKeyReject,
		"replace\r":      HostKeyReplace,
		"replx\breace\r": HostKeyReject,
		"\x03":           HostKeyReject,
	} {
		s := NewSSHSession("t")
		var out strings.Builder
		var mu sync.Mutex
		s.SetOnDataCallback(func(b []byte) { mu.Lock(); out.Write(b); mu.Unlock() })
		ch := make(chan []byte, 1)
		s.authAnswerCh = ch
		ch <- []byte(input)
		got, _ := s.terminalHostKeyPrompt(p)
		if got != want {
			t.Errorf("%q: %v, want %v", input, got, want)
		}
		mu.Lock()
		text := out.String()
		mu.Unlock()
		if !strings.Contains(text, "SHA256:old") || !strings.Contains(text, "SHA256:new") || !strings.Contains(text, "HAS CHANGED") {
			t.Errorf("%q: warning lacks fingerprints:\n%s", input, text)
		}
	}

	blocked := p
	blocked.CanReplace, blocked.Blocker = false, "pattern"
	s := NewSSHSession("t")
	var out strings.Builder
	s.SetOnDataCallback(func(b []byte) { out.Write(b) })
	ch := make(chan []byte, 1)
	s.authAnswerCh = ch
	ch <- []byte("replace\r")
	if got, _ := s.terminalHostKeyPrompt(blocked); got != HostKeyReject {
		t.Fatalf("blocked: %v", got)
	}
	if len(ch) != 1 {
		t.Fatal("blocked notice read the terminal input")
	}
	if !strings.Contains(out.String(), "pattern line") {
		t.Fatalf("notice lacks the reason:\n%s", out.String())
	}

	kept := p
	kept.UserFileKept, kept.RemoveCommand = true, "ssh-keygen -R srv"
	s = NewSSHSession("t")
	out.Reset()
	s.SetOnDataCallback(func(b []byte) { out.Write(b) })
	ch = make(chan []byte, 1)
	s.authAnswerCh = ch
	ch <- []byte("replace\r")
	if got, _ := s.terminalHostKeyPrompt(kept); got != HostKeyReplace {
		t.Fatalf("user file kept: %v", got)
	}
	if !strings.Contains(out.String(), "ssh-keygen -R srv") || !strings.Contains(out.String(), "takes precedence") {
		t.Fatalf("user-file notice:\n%s", out.String())
	}
}

func newECDSAKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Ferrolho's review 1 (24/10/2026): OpenSSH records every key type of a host
// (UpdateHostKeys), and a fake server picks the type. After replacing the
// ed25519 key, the old ecdsa key left in ~/.ssh/known_hosts must not pass:
// the app file decides per host, and that key reads as changed.
func TestReplacedHostOldKeyOfOtherTypeInUserFileDoesNotPass(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	user := filepath.Join(dir, "user_known_hosts")
	oldEd, oldEc, next := newEd25519Key(t), newECDSAKey(t), newEd25519Key(t)
	writeLines(t, user, knownLine("srv:22", oldEd), knownLine("srv:22", oldEc))
	if err := verifyHostKey(app, user, "srv:22", next, answer(HostKeyReplace, nil), true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := verifyHostKey(app, user, "srv:22", oldEc, nil, false); err == nil {
		t.Fatal("old ecdsa key from the user file passed after the replace")
	}
	var got HostKeyPrompt
	err := verifyHostKey(app, user, "srv:22", oldEc, func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyTrust, nil
	}, true)
	if err == nil || !got.Changed || len(got.OldKeys) != 1 || got.OldKeys[0].Fingerprint != ssh.FingerprintSHA256(next) {
		t.Fatalf("old ecdsa key: err=%v prompt=%+v, want the red changed prompt", err, got)
	}
	// Another ecdsa key while the user file still has the old one: changed,
	// with the user file's key as the old one.
	err = verifyHostKey(app, user, "srv:22", newECDSAKey(t), func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReject, nil
	}, true)
	if err == nil || !got.Changed || got.OldKeys[0].Fingerprint != ssh.FingerprintSHA256(oldEc) || got.OldKeys[0].File != "user" {
		t.Fatalf("new ecdsa: err=%v prompt=%+v", err, got)
	}
	// A key of a type neither file knows is a normal first-visit question
	// (OtherKeyTypesKnown), never a silent pass.
	err = verifyHostKey(app, user, "srv:22", newRSAKey(t), func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReject, nil
	}, true)
	if err == nil || got.Changed || !got.OtherKeyTypesKnown {
		t.Fatalf("unknown rsa: err=%v prompt=%+v", err, got)
	}
}

// Ferrolho's review 2: a wildcard line of another key type in the app file
// would survive the replace and keep its old key trusted: no replace.
func TestChangedKeyOtherTypePatternBlocks(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	writeLines(t, app,
		knownLine("a.example.com:22", newEd25519Key(t)),
		"*.example.com "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(newECDSAKey(t)))),
	)
	before := readFile(t, app)
	var got HostKeyPrompt
	err := verifyHostKey(app, "", "a.example.com:22", newEd25519Key(t), func(p HostKeyPrompt) (HostKeyAnswer, error) {
		got = p
		return HostKeyReplace, nil
	}, true)
	if err == nil || got.CanReplace || got.Blocker != "pattern" || readFile(t, app) != before {
		t.Fatalf("err=%v prompt=%+v", err, got)
	}
}

// Ferrolho's review 3: "[srv]:22" written by hand is srv's own entry.
func TestChangedKeyBracketedDefaultPortIsReplaced(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "known_hosts")
	writeLines(t, app, "[srv]:22 "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(newEd25519Key(t)))))
	next := newEd25519Key(t)
	if err := verifyHostKey(app, "", "srv:22", next, answer(HostKeyReplace, nil), true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if out := readFile(t, app); strings.Contains(out, "[srv]:22") || !strings.Contains(out, knownLine("srv:22", next)) {
		t.Fatalf("file:\n%s", out)
	}
}
