package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func testKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func buildSigtool(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "sigtool")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	args := []string{"build", "-trimpath", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	cmd := exec.Command("go", append(args, ".")...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// A wrong -X path is silently ignored by the linker, leaving the key empty
// and every update refused. Prove the path the Taskfiles use really lands in
// update.releasePublicKey, and that binkey reads it back from the binary.
func TestLdflagsEmbedsReleaseKey(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	key := testKey(t)
	bin := buildSigtool(t, "-s -w -X "+keySymbol+"="+key)
	out, err := exec.Command(bin, "embedded").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != key {
		t.Fatalf("embedded key = %q, want %q", got, key)
	}
	if ok, err := BinaryHasKey(bin, key); err != nil || !ok {
		t.Fatalf("BinaryHasKey = %v, %v; want true", ok, err)
	}

	plain := buildSigtool(t, "")
	out, err = exec.Command(plain, "embedded").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "" {
		t.Fatalf("build without -X must embed no key, got %q", got)
	}
	if ok, _ := BinaryHasKey(plain, key); ok {
		t.Fatal("BinaryHasKey on a build without the key must be false")
	}
}

func TestTaskfilesEmbedKeyWithTheRightSymbol(t *testing.T) {
	root := repoRoot(t)
	for _, f := range []string{"build/darwin/Taskfile.yml", "build/linux/Taskfile.yml", "build/windows/Taskfile.yml"} {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "-X "+keySymbol+"={{.RELEASE_PUBKEY}}") {
			t.Errorf("%s does not embed RELEASE_PUBKEY into %s", f, keySymbol)
		}
	}
}

func opensslAtLeast3(t *testing.T) {
	t.Helper()
	out, err := exec.Command("openssl", "version").Output()
	if err != nil {
		t.Skip("openssl not available")
	}
	v := string(out)
	if strings.HasPrefix(v, "OpenSSL 1.") || strings.HasPrefix(v, "LibreSSL") || !strings.HasPrefix(v, "OpenSSL") {
		t.Skipf("need OpenSSL >= 3, have %s", strings.TrimSpace(v))
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
}

// End to end with the real offline scripts: keygen → checksums → sign →
// verify, and the verification the updater uses accepts OpenSSL's output.
func TestOfflineScriptsInterop(t *testing.T) {
	opensslAtLeast3(t)
	root := repoRoot(t)
	keyDir := t.TempDir()
	env := append(os.Environ(), "UNITERM_RELEASE_KEY_PASS=test-only-passphrase")

	gen := exec.Command("sh", filepath.Join(root, "scripts/release/offline-keygen.sh"), keyDir)
	gen.Env = env
	genOut, err := gen.CombinedOutput()
	if err != nil {
		t.Fatalf("keygen: %v\n%s", err, genOut)
	}
	keyInfo, err := os.Stat(filepath.Join(keyDir, "uniterm-release.key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && keyInfo.Mode().Perm() != 0600 {
		t.Errorf("private key mode = %v, want 0600", keyInfo.Mode().Perm())
	}
	if pem, _ := os.ReadFile(filepath.Join(keyDir, "uniterm-release.key")); !strings.Contains(string(pem), "ENCRYPTED") {
		t.Error("private key must be passphrase-encrypted")
	}
	pubData, err := os.ReadFile(filepath.Join(keyDir, "release-pubkey.txt"))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := readKeyArg(string(pubData))
	if err != nil {
		t.Fatalf("keygen public key unreadable: %v", err)
	}
	fp, _ := Fingerprint(pub)
	if !strings.Contains(string(genOut), "Fingerprint : "+fp) {
		t.Errorf("keygen fingerprint differs from sigtool's %q:\n%s", fp, genOut)
	}

	rel := t.TempDir()
	for name, body := range map[string]string{
		"uniterm-darwin-arm64-v1.9.6.zip":   "zip payload",
		"uniterm-linux-amd64-v1.9.6.tar.gz": "tar payload",
	} {
		if err := os.WriteFile(filepath.Join(rel, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmdChecksums([]string{rel}); err != nil {
		t.Fatal(err)
	}
	sign := exec.Command("sh", filepath.Join(root, "scripts/release/offline-sign.sh"),
		filepath.Join(keyDir, "uniterm-release.key"), filepath.Join(rel, "checksums.txt"))
	sign.Env = env
	sign.Stdin = strings.NewReader("y\n")
	if out, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}

	if err := Verify(pub, rel); err != nil {
		t.Fatalf("verify of a freshly signed release failed: %v", err)
	}
	if err := Verify(testKey(t), rel); err == nil {
		t.Fatal("verify must fail with another key")
	}
	if err := Verify("", rel); err == nil {
		t.Fatal("verify must fail with no key")
	}

	// An asset changed after signing, or added without re-signing, fails.
	if err := os.WriteFile(filepath.Join(rel, "uniterm-linux-amd64-v1.9.6.tar.gz"), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, rel); err == nil {
		t.Fatal("tampered asset must fail verification")
	}
	if err := os.WriteFile(filepath.Join(rel, "uniterm-linux-amd64-v1.9.6.tar.gz"), []byte("tar payload"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel, "uniterm-windows-amd64-portable-v1.9.6.zip"), []byte("extra"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, rel); err == nil {
		t.Fatal("unlisted asset must fail verification")
	}
}

func TestOfflineSignRefusesForeignLines(t *testing.T) {
	opensslAtLeast3(t)
	root := repoRoot(t)
	dir := t.TempDir()
	sums := filepath.Join(dir, "checksums.txt")
	bad := strings.Repeat("a", 64) + "  ../../etc/passwd\n"
	if err := os.WriteFile(sums, []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", filepath.Join(root, "scripts/release/offline-sign.sh"), filepath.Join(dir, "nokey"), sums)
	cmd.Stdin = strings.NewReader("y\n")
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "refusing") {
		t.Fatalf("sign must refuse a malformed checksums.txt, got err=%v\n%s", err, out)
	}
	if _, err := os.Stat(sums + ".sig"); !os.IsNotExist(err) {
		t.Fatal("no signature may be written")
	}
}
