package session

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// weakRSAKey512 builds an RSA key below crypto/rsa's minimum by hand
// (rsa.GenerateKey refuses since Go 1.24).
func weakRSAKey512(t *testing.T) (n, d *big.Int, e int) {
	t.Helper()
	e = 65537
	E := big.NewInt(int64(e))
	one := big.NewInt(1)
	for {
		p, err := rand.Prime(rand.Reader, 256)
		if err != nil {
			t.Fatal(err)
		}
		q, err := rand.Prime(rand.Reader, 256)
		if err != nil {
			t.Fatal(err)
		}
		n = new(big.Int).Mul(p, q)
		if p.Cmp(q) == 0 || n.BitLen() != 512 {
			continue
		}
		phi := new(big.Int).Mul(new(big.Int).Sub(p, one), new(big.Int).Sub(q, one))
		d = new(big.Int).ModInverse(E, phi)
		if d != nil {
			return n, d, e
		}
	}
}

// weakSigner signs PKCS #1 v1.5 by hand with a sub-1024-bit key, the way an
// old switch does.
type weakSigner struct {
	n, d *big.Int
	e    int
}

func newWeakSigner(t *testing.T) *weakSigner {
	n, d, e := weakRSAKey512(t)
	return &weakSigner{n: n, d: d, e: e}
}

func (w *weakSigner) PublicKey() ssh.PublicKey {
	pk, err := ssh.NewPublicKey(&rsa.PublicKey{N: w.n, E: w.e})
	if err != nil {
		panic(err)
	}
	return pk
}

func (w *weakSigner) Sign(r io.Reader, data []byte) (*ssh.Signature, error) {
	return w.SignWithAlgorithm(r, data, ssh.KeyAlgoRSA)
}

func (w *weakSigner) SignWithAlgorithm(_ io.Reader, data []byte, algo string) (*ssh.Signature, error) {
	var digest, prefix []byte
	switch algo {
	case "", ssh.KeyAlgoRSA:
		algo = ssh.KeyAlgoRSA
		s := sha1.Sum(data)
		digest, prefix = s[:], prefixSHA1
	case ssh.KeyAlgoRSASHA256:
		s := sha256.Sum256(data)
		digest, prefix = s[:], prefixSHA256
	default:
		return nil, fmt.Errorf("weak signer: %s not possible with a 512-bit key", algo)
	}
	k := (w.n.BitLen() + 7) / 8
	em := make([]byte, k)
	em[1] = 1
	tLen := len(prefix) + len(digest)
	for i := 2; i < k-tLen-1; i++ {
		em[i] = 0xff
	}
	copy(em[k-tLen:], prefix)
	copy(em[k-len(digest):], digest)
	s := new(big.Int).Exp(new(big.Int).SetBytes(em), w.d, w.n)
	return &ssh.Signature{Format: algo, Blob: s.FillBytes(make([]byte, k))}, nil
}

// startWeakSSHServer serves SSH with the given host key; accepts the
// password "pw" and closes after auth.
func startWeakSSHServer(t *testing.T, host *weakSigner, algos []string) string {
	t.Helper()
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if string(pw) == "pw" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	signer, err := ssh.NewSignerWithAlgorithms(host, algos)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					ch.Reject(ssh.Prohibited, "test server")
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// isolatedKnownHosts points the app's known_hosts at a temp file for the
// test (the user's ~/.ssh/known_hosts is only read, and holds no entry for
// a random loopback port).
func isolatedKnownHosts(t *testing.T) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "known_hosts")
	hostKeyCfgMu.RLock()
	prev := knownHostsFile
	hostKeyCfgMu.RUnlock()
	SetKnownHostsPath(p)
	t.Cleanup(func() { SetKnownHostsPath(prev) })
}

func dialLegacyTest(addr string, p *SSHAlgoConfig, prompt HostKeyPrompter) error {
	sets, err := resolveSSHDialAlgorithms(p)
	if err != nil {
		return err
	}
	factory := func() (*ssh.ClientConfig, func(), error) {
		return &ssh.ClientConfig{
			User:            "admin",
			Auth:            []ssh.AuthMethod{ssh.Password("pw")},
			Timeout:         5 * time.Second,
			HostKeyCallback: hostKeyCallback(prompt),
		}, func() {}, nil
	}
	client, err := dialSSHWithAuthRetry(addr, sets, factory, factory, func() (net.Conn, error) {
		return net.DialTimeout("tcp", addr, 5*time.Second)
	})
	if err != nil {
		return wrapSSHAlgorithmError(sshAlgoModeOf(p), err)
	}
	client.Close()
	return nil
}

func TestWeakHostKeyRefusedWithoutLegacyOption(t *testing.T) {
	isolatedKnownHosts(t)
	addr := startWeakSSHServer(t, newWeakSigner(t), []string{ssh.KeyAlgoRSA})
	prompted := false
	for _, p := range []*SSHAlgoConfig{nil, {Mode: SSHAlgoModeCompatible}, {Mode: SSHAlgoModeSecure}} {
		err := dialLegacyTest(addr, p, func(HostKeyPrompt) (bool, error) { prompted = true; return true, nil })
		w, ok := isWeakHostKeyError(err)
		if !ok || w.Bits != 512 {
			if p != nil && p.Mode == SSHAlgoModeSecure && err != nil {
				continue // secure mode doesn't even offer ssh-rsa (SHA-1)
			}
			t.Fatalf("mode %+v: err = %v, want the legacy host key error", p, err)
		}
		if !strings.Contains(err.Error(), "legacy host key (RSA 512 bits)") {
			t.Fatalf("error text the UI matches is missing: %v", err)
		}
	}
	if prompted {
		t.Fatal("the user was asked to trust a key whose signature was never verified")
	}
}

func TestLegacyOptionConnectsAndKeepsKnownHosts(t *testing.T) {
	isolatedKnownHosts(t)
	for _, algo := range []string{ssh.KeyAlgoRSA, ssh.KeyAlgoRSASHA256} {
		t.Run(algo, func(t *testing.T) {
			host := newWeakSigner(t)
			addr := startWeakSSHServer(t, host, []string{algo})
			legacy := &SSHAlgoConfig{LegacyDevice: true}

			var got HostKeyPrompt
			accept := func(p HostKeyPrompt) (bool, error) { got = p; return true, nil }
			if err := dialLegacyTest(addr, legacy, accept); err != nil {
				t.Fatalf("legacy connection failed: %v", err)
			}
			if !got.Weak || got.KeyBits != 512 {
				t.Fatalf("first-visit prompt = %+v, want weak 512-bit", got)
			}
			// Known now: no prompt the second time.
			if err := dialLegacyTest(addr, legacy, func(HostKeyPrompt) (bool, error) {
				return false, errors.New("must not prompt for a known key")
			}); err != nil {
				t.Fatalf("second connection: %v", err)
			}
			// Legacy + secure is treated as compatible.
			if err := dialLegacyTest(addr, &SSHAlgoConfig{Mode: SSHAlgoModeSecure, LegacyDevice: true}, accept); err != nil {
				t.Fatalf("legacy+secure: %v", err)
			}
		})
	}
}

func TestLegacyOptionStillRefusesChangedKey(t *testing.T) {
	isolatedKnownHosts(t)
	known := newWeakSigner(t)
	impostor := newWeakSigner(t)
	addr := startWeakSSHServer(t, impostor, []string{ssh.KeyAlgoRSA})
	// The address was trusted with another (also weak) key before.
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, known.PublicKey())
	if err := os.WriteFile(appKnownHostsPath(), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := dialLegacyTest(addr, &SSHAlgoConfig{LegacyDevice: true}, func(HostKeyPrompt) (bool, error) { return true, nil })
	if err == nil {
		t.Fatal("a changed host key was accepted on a legacy connection")
	}
}

func TestVerifyWeakRSA(t *testing.T) {
	w := newWeakSigner(t)
	pub := &rsa.PublicKey{N: w.n, E: w.e}
	data := []byte("exchange hash")
	for _, algo := range []string{ssh.KeyAlgoRSA, ssh.KeyAlgoRSASHA256} {
		sig, err := w.SignWithAlgorithm(nil, data, algo)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyWeakRSA(pub, data, sig); err != nil {
			t.Fatalf("%s: valid signature refused: %v", algo, err)
		}
		if err := verifyWeakRSA(pub, []byte("other hash"), sig); err == nil {
			t.Fatalf("%s: signature over other data accepted", algo)
		}
		bad := *sig
		bad.Blob = append([]byte(nil), sig.Blob...)
		bad.Blob[len(bad.Blob)-1] ^= 1
		if err := verifyWeakRSA(pub, data, &bad); err == nil {
			t.Fatalf("%s: tampered signature accepted", algo)
		}
		// Signature claimed under another hash.
		other := *sig
		if algo == ssh.KeyAlgoRSA {
			other.Format = ssh.KeyAlgoRSASHA256
		} else {
			other.Format = ssh.KeyAlgoRSA
		}
		if err := verifyWeakRSA(pub, data, &other); err == nil {
			t.Fatalf("%s: signature accepted under the wrong hash", algo)
		}
	}
	// Short signature (leading zero bytes dropped) is accepted, like OpenSSH.
	for i := 0; i < 200; i++ {
		d := []byte(fmt.Sprintf("h%d", i))
		sig, _ := w.SignWithAlgorithm(nil, d, ssh.KeyAlgoRSA)
		if sig.Blob[0] == 0 {
			short := *sig
			short.Blob = sig.Blob[1:]
			if err := verifyWeakRSA(pub, d, &short); err != nil {
				t.Fatalf("short signature refused: %v", err)
			}
			break
		}
	}
	// Below the legacy floor.
	tiny := &rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), 400), E: 65537}
	if _, ok := verifyWeakRSA(tiny, data, &ssh.Signature{Format: ssh.KeyAlgoRSA, Blob: []byte{1}}).(*WeakHostKeyError); !ok {
		t.Fatal("a 401-bit key must be refused even on a legacy connection")
	}
	// SHA-512 can't fit a 512-bit modulus.
	if err := verifyWeakRSA(pub, data, &ssh.Signature{Format: ssh.KeyAlgoRSASHA512, Blob: []byte{1}}); err == nil {
		t.Fatal("rsa-sha2-512 with a 512-bit key accepted")
	}
}

// The legacy path is per connection: the rest of the process (TLS, x509,
// the updater, Drive) still refuses RSA below 1024 bits.
func TestProcessStillRefusesWeakRSA(t *testing.T) {
	w := newWeakSigner(t)
	pub := &rsa.PublicKey{N: w.n, E: w.e}
	sig, _ := w.SignWithAlgorithm(nil, []byte("m"), ssh.KeyAlgoRSASHA256)
	digest := sha256.Sum256([]byte("m"))
	err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig.Blob)
	if err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("crypto/rsa accepted a 512-bit key: %v", err)
	}
	if strings.Contains(os.Getenv("GODEBUG"), "rsa1024min") {
		t.Fatal("GODEBUG relaxes rsa1024min for the whole process")
	}
	// No //go:debug or go.mod godebug line relaxes it either.
	root := filepath.Join("..", "..")
	for _, f := range []string{"go.mod", "main.go"} {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err == nil && strings.Contains(string(data), "rsa1024min") {
			t.Fatalf("%s relaxes rsa1024min process-wide", f)
		}
	}
}

// The option is saved with the connection (Go struct, not only the TS type)
// and dropped from the JSON when off.
func TestLegacyDeviceSurvivesJSON(t *testing.T) {
	in := ConnectionConfig{Type: "ssh", SSHAlgorithms: &SSHAlgoConfig{Mode: SSHAlgoModeCompatible, LegacyDevice: true}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"legacyDevice":true`) {
		t.Fatalf("legacyDevice not saved: %s", data)
	}
	var out ConnectionConfig
	if err := json.Unmarshal(data, &out); err != nil || out.SSHAlgorithms == nil || !out.SSHAlgorithms.LegacyDevice {
		t.Fatalf("legacyDevice lost on load: %+v %v", out.SSHAlgorithms, err)
	}
	off, _ := json.Marshal(SSHAlgoConfig{Mode: SSHAlgoModeCompatible})
	if strings.Contains(string(off), "legacyDevice") {
		t.Fatalf("legacyDevice written when off: %s", off)
	}
}
