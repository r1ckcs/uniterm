package session

import (
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Legacy devices (old switches, routers) still ship RSA host keys below the
// 1024-bit minimum crypto/rsa enforces since Go 1.24. A connection marked as
// a legacy device verifies those host key signatures with the PKCS #1 v1.5
// check below instead; everything else (TLS, the updater, Drive, the other
// SSH connections) keeps the standard library's minimum. The known_hosts
// check still runs on the key afterwards.

// minLegacyRSABits is the smallest RSA host key a legacy connection accepts.
// Below 512 bits there is no device worth reaching.
const minLegacyRSABits = 512

// WeakHostKeyError reports a host key the standard library refuses as too
// weak; the connection must be marked as a legacy device to use it.
type WeakHostKeyError struct {
	KeyType string
	Bits    int
}

func (e *WeakHostKeyError) Error() string {
	return fmt.Sprintf("ssh: legacy host key (%s %d bits): enable \"Legacy device\" for this connection", e.KeyType, e.Bits)
}

// legacyHostKeyVerifier is ssh.ClientConfig.HostKeySignatureVerifier for a
// legacy connection: RSA keys between minLegacyRSABits and 1023 bits are
// verified here, every other key by the library as usual.
func legacyHostKeyVerifier(hostKey ssh.PublicKey, data []byte, sig *ssh.Signature) error {
	if pub, ok := weakRSAKey(hostKey); ok {
		return verifyWeakRSA(pub, data, sig)
	}
	return hostKey.Verify(data, sig)
}

// strictHostKeyVerifier is the verifier of every other connection: the
// library's check, with its "keys are insecure" failure turned into a
// WeakHostKeyError that names the fix.
func strictHostKeyVerifier(hostKey ssh.PublicKey, data []byte, sig *ssh.Signature) error {
	err := hostKey.Verify(data, sig)
	if err == nil {
		return nil
	}
	if pub, ok := weakRSAKey(hostKey); ok {
		return &WeakHostKeyError{KeyType: "RSA", Bits: pub.N.BitLen()}
	}
	return err
}

// hostKeyVerifier picks the verifier for a connection.
func hostKeyVerifier(legacy bool) func(ssh.PublicKey, []byte, *ssh.Signature) error {
	if legacy {
		return legacyHostKeyVerifier
	}
	return strictHostKeyVerifier
}

func weakRSAKey(k ssh.PublicKey) (*rsa.PublicKey, bool) {
	if k.Type() != ssh.KeyAlgoRSA {
		return nil, false
	}
	ck, ok := k.(ssh.CryptoPublicKey)
	if !ok {
		return nil, false
	}
	pub, ok := ck.CryptoPublicKey().(*rsa.PublicKey)
	if !ok || pub.N.BitLen() >= 1024 {
		return nil, false
	}
	return pub, true
}

// DigestInfo prefixes (RFC 8017 §9.2, note 1).
var (
	prefixSHA1   = []byte{0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x0e, 0x03, 0x02, 0x1a, 0x05, 0x00, 0x04, 0x14}
	prefixSHA256 = []byte{0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20}
	prefixSHA512 = []byte{0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40}
)

var errWeakRSASignature = errors.New("ssh: legacy RSA host key signature does not verify")

// verifyWeakRSA is RSASSA-PKCS1-v1_5 verification (RFC 8017 §8.2.2) for a
// small RSA key, with the hash the SSH signature format names (RFC 4253
// ssh-rsa: SHA-1; RFC 8332 rsa-sha2-256/512). Like OpenSSH and x/crypto,
// it accepts signatures shorter than the modulus (missing leading zeros).
func verifyWeakRSA(pub *rsa.PublicKey, data []byte, sig *ssh.Signature) error {
	bits := pub.N.BitLen()
	if bits < minLegacyRSABits {
		return &WeakHostKeyError{KeyType: "RSA", Bits: bits}
	}
	if pub.E < 3 || pub.E&1 == 0 {
		return errors.New("ssh: legacy RSA host key has an invalid exponent")
	}
	var h hash.Hash
	var prefix []byte
	switch sig.Format {
	case ssh.KeyAlgoRSA:
		h, prefix = sha1.New(), prefixSHA1
	case ssh.KeyAlgoRSASHA256:
		h, prefix = sha256.New(), prefixSHA256
	case ssh.KeyAlgoRSASHA512:
		h, prefix = sha512.New(), prefixSHA512
	default:
		return fmt.Errorf("ssh: signature type %s for key type %s", sig.Format, ssh.KeyAlgoRSA)
	}
	h.Write(data)
	digest := h.Sum(nil)

	k := (bits + 7) / 8
	tLen := len(prefix) + len(digest)
	if k < tLen+11 || len(sig.Blob) == 0 || len(sig.Blob) > k {
		return errWeakRSASignature
	}
	s := new(big.Int).SetBytes(sig.Blob)
	if s.Cmp(pub.N) >= 0 {
		return errWeakRSASignature
	}
	m := new(big.Int).Exp(s, big.NewInt(int64(pub.E)), pub.N)
	em := m.FillBytes(make([]byte, k))

	want := make([]byte, k)
	want[1] = 0x01
	for i := 2; i < k-tLen-1; i++ {
		want[i] = 0xff
	}
	copy(want[k-tLen:], prefix)
	copy(want[k-len(digest):], digest)
	if subtle.ConstantTimeCompare(em, want) != 1 {
		return errWeakRSASignature
	}
	return nil
}

// isWeakHostKeyError reports whether err (possibly from deep in a dial) is a
// refused weak host key, also when only its text survived wrapping.
func isWeakHostKeyError(err error) (*WeakHostKeyError, bool) {
	if err == nil {
		return nil, false
	}
	var w *WeakHostKeyError
	if errors.As(err, &w) {
		return w, true
	}
	msg := err.Error()
	if i := strings.Index(msg, "legacy host key (RSA "); i >= 0 {
		var bits int
		fmt.Sscanf(msg[i+len("legacy host key (RSA "):], "%d", &bits)
		return &WeakHostKeyError{KeyType: "RSA", Bits: bits}, true
	}
	if strings.Contains(msg, "-bit keys are insecure") {
		var bits int
		if i := strings.Index(msg, "crypto/rsa: "); i >= 0 {
			fmt.Sscanf(msg[i+len("crypto/rsa: "):], "%d-bit", &bits)
		}
		return &WeakHostKeyError{KeyType: "RSA", Bits: bits}, true
	}
	return nil, false
}
