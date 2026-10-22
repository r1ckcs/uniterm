// Package recovery is the vault's recovery key: 24 words (BIP-39 English
// list, 256 random bits) from which a device identity is derived. That
// identity is an admin member of the roster, so the vault key of every
// epoch is wrapped for it like for any device (rotations included), and
// whoever holds the words can let a new device in after all devices are
// lost. The words are shown once and never stored.
package recovery

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ys-ll/uniterm/backend/vault"
)

// RecoveryName is the roster name of the recovery member. Join requests
// can't use it (see join), so the UI can trust it to mean "recovery key".
const RecoveryName = "uniterm-recovery-key"

// IsRecoveryMember reports whether m is a recovery key, not a device.
func IsRecoveryMember(m vault.Member) bool { return m.Name == RecoveryName }

// Key is the device identity derived from a recovery phrase. It holds the
// private keys in memory only while recovering or setting up.
type Key struct {
	sign *ecdsa.PrivateKey
	kex  *ecdh.PrivateKey
}

var _ vault.DeviceKey = (*Key)(nil)

func (k *Key) SigningPublicKey() []byte {
	b, err := k.sign.PublicKey.Bytes()
	if err != nil {
		panic(err) // P-256 keys always encode
	}
	return b
}

func (k *Key) AgreementPublicKey() []byte { return k.kex.PublicKey().Bytes() }

func (k *Key) SignDigest(digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, k.sign, digest)
}

func (k *Key) KeyExchanger() ecdh.KeyExchanger { return k.kex }

// DeviceID is the recovery member's id in the roster.
func (k *Key) DeviceID() string { return vault.IDOf(k) }

// deriveScalar expands the entropy into a valid P-256 private scalar. A
// 32-byte output is out of range with probability ~2^-32; the counter
// retries then, deterministically.
func deriveScalar(entropy []byte, label string, parse func([]byte) error) error {
	for i := 0; i < 16; i++ {
		b, err := hkdf.Key(sha256.New, entropy, []byte("uniterm-vault/v1 recovery"), label+"/"+strconv.Itoa(i), 32)
		if err != nil {
			return err
		}
		if parse(b) == nil {
			return nil
		}
	}
	return errors.New("recovery: could not derive a key")
}

func keyFromEntropy(entropy []byte) (*Key, error) {
	k := &Key{}
	if err := deriveScalar(entropy, "sign", func(b []byte) (err error) {
		k.sign, err = ecdsa.ParseRawPrivateKey(elliptic.P256(), b)
		return err
	}); err != nil {
		return nil, err
	}
	if err := deriveScalar(entropy, "kex", func(b []byte) (err error) {
		k.kex, err = ecdh.P256().NewPrivateKey(b)
		return err
	}); err != nil {
		return nil, err
	}
	return k, nil
}

// Generate makes a new random phrase and its key.
func Generate() ([]string, *Key, error) {
	entropy := make([]byte, entropySize)
	if _, err := rand.Read(entropy); err != nil {
		return nil, nil, err
	}
	k, err := keyFromEntropy(entropy)
	if err != nil {
		return nil, nil, err
	}
	return entropyToWords(entropy), k, nil
}

// KeyFromWords checks a phrase (WordError, ErrChecksum) and derives its key.
func KeyFromWords(words []string) (*Key, error) {
	entropy, err := wordsToEntropy(words)
	if err != nil {
		return nil, err
	}
	return keyFromEntropy(entropy)
}

// Challenge picks n distinct 1-based positions for the "type words #a, #b,
// #c" confirmation after the phrase is shown.
func Challenge(n int) ([]int, error) {
	if n < 1 || n > WordCount {
		return nil, fmt.Errorf("recovery: challenge of %d words", n)
	}
	picked := map[int]bool{}
	var out []int
	for len(out) < n {
		r, err := rand.Int(rand.Reader, big.NewInt(WordCount))
		if err != nil {
			return nil, err
		}
		p := int(r.Int64()) + 1
		if !picked[p] {
			picked[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// Check reports whether answers[i] is the word at positions[i] of words
// (case and surrounding spaces ignored, the 4-letter short form accepted).
func Check(words []string, positions []int, answers []string) bool {
	if len(positions) == 0 || len(positions) != len(answers) {
		return false
	}
	ok := true
	for i, p := range positions {
		if p < 1 || p > len(words) {
			return false
		}
		want, okW := lookup(strings.ToLower(words[p-1]))
		got, okA := lookup(strings.ToLower(strings.TrimSpace(answers[i])))
		ok = ok && okW && okA && want == got
	}
	return ok
}

// anchor is what the recovery key signs: the vault and a roster version it
// accepted. A recovering device has no other trust anchor, and storage
// alone could hand it a look-alike vault built by an attacker (the
// recovery public keys are in the plaintext roster); only the holder of
// the words can sign this.
type anchor struct {
	Format int              `json:"format"`
	Trust  vault.TrustState `json:"trust"`
}

type signedAnchor struct {
	Body []byte `json:"body"`
	Sig  []byte `json:"sig"`
}

func anchorDigest(body []byte) []byte {
	h := sha256.New()
	h.Write([]byte("uniterm-vault/v1 recovery-anchor\x00"))
	h.Write(body)
	return h.Sum(nil)
}

func anchorPrefix(deviceID string) string { return "recovery/" + deviceID + "." }

// writeAnchor signs and stores the vault's current trust state. One object
// per roster counter, so a later anchor never collides with an earlier one.
func writeAnchor(s vault.Store, k *Key, t vault.TrustState) error {
	body, err := json.Marshal(anchor{Format: 1, Trust: t})
	if err != nil {
		return err
	}
	sig, err := k.SignDigest(anchorDigest(body))
	if err != nil {
		return err
	}
	obj, err := json.Marshal(signedAnchor{Body: body, Sig: sig})
	if err != nil {
		return err
	}
	_, err = s.Create(anchorPrefix(k.DeviceID())+strconv.FormatUint(t.Counter, 10), obj)
	if errors.Is(err, vault.ErrExists) {
		return nil // same counter already anchored (an earlier attempt)
	}
	return err
}

// ErrNoAnchor: the store holds no anchor signed by this phrase (wrong
// phrase, another vault, or the anchors were deleted).
var ErrNoAnchor = errors.New("recovery: this phrase does not open the vault in this storage")

// anchors returns the trust states signed by k, newest first. Candidates
// that don't verify (anyone can write to storage) are skipped.
func anchors(s vault.Store, k *Key) ([]vault.TrustState, error) {
	entries, err := s.List(anchorPrefix(k.DeviceID()))
	if err != nil {
		return nil, err
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), k.SigningPublicKey())
	if err != nil {
		return nil, err
	}
	var out []vault.TrustState
	for _, e := range entries {
		data, _, err := s.Get(e.ID)
		if err != nil {
			return nil, err
		}
		var sa signedAnchor
		var a anchor
		if json.Unmarshal(data, &sa) != nil || !ecdsa.VerifyASN1(pub, anchorDigest(sa.Body), sa.Sig) ||
			json.Unmarshal(sa.Body, &a) != nil || a.Format != 1 || a.Trust.Counter == 0 || a.Trust.Hash == "" {
			continue
		}
		out = append(out, a.Trust)
	}
	for i := 1; i < len(out); i++ { // newest counter first
		for j := i; j > 0 && out[j].Counter > out[j-1].Counter; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// Setup gives a vault a new recovery key and returns its words, to be
// shown once. It needs only the store and the open vault (an admin's), so
// it can run on a draft store that is copied to the real one after the
// user confirms the words. An existing recovery key is revoked (with key
// rotation) once the new one is in place: an old paper copy stops working.
func Setup(s vault.Store, v *vault.Vault) ([]string, error) {
	words, k, err := Generate()
	if err != nil {
		return nil, err
	}
	var old []string
	for _, m := range v.Roster().Members {
		if IsRecoveryMember(m) {
			old = append(old, m.DeviceID)
		}
	}
	if err := v.AddMember(vault.MemberOf(k, "", RecoveryName, vault.RoleAdmin)); err != nil {
		return nil, err
	}
	for _, id := range old {
		if err := v.Revoke(id); err != nil {
			return nil, fmt.Errorf("recovery: new key added, but revoking the old one failed: %w", err)
		}
	}
	if err := writeAnchor(s, k, v.Trust()); err != nil {
		return nil, err
	}
	return words, nil
}

// Recover lets dev into the vault in s with the recovery phrase: it opens
// the vault as the recovery member (from the anchor the phrase signed),
// adds dev as an admin, and re-anchors on the new version. It returns the
// trust state dev starts from: save vault.State{Trust: t, NextSeq: 1} in
// dev's state store, then vault.Open.
func Recover(s vault.Store, words []string, dev vault.DeviceKey, owner, name string) (vault.TrustState, error) {
	k, err := KeyFromWords(words)
	if err != nil {
		return vault.TrustState{}, err
	}
	if name == RecoveryName {
		return vault.TrustState{}, errors.New("recovery: reserved device name")
	}
	list, err := anchors(s, k)
	if err != nil {
		return vault.TrustState{}, err
	}
	if len(list) == 0 {
		return vault.TrustState{}, ErrNoAnchor
	}
	var rv *vault.Vault
	var openErr error
	for _, t := range list {
		rv, openErr = vault.Open(s, vault.NewMemState(vault.State{Trust: t, NextSeq: 1}), k)
		if openErr == nil {
			break
		}
	}
	if openErr != nil {
		return vault.TrustState{}, fmt.Errorf("recovery: %w", openErr)
	}
	m := vault.MemberOf(dev, owner, name, vault.RoleAdmin)
	if _, ok := memberOf(rv.Roster(), m.DeviceID); !ok {
		if err := rv.AddMember(m); err != nil {
			return vault.TrustState{}, err
		}
	}
	t := rv.Trust()
	// Keeps recovery working if old roster versions are ever compacted.
	if err := writeAnchor(s, k, t); err != nil {
		return vault.TrustState{}, err
	}
	return t, nil
}

func memberOf(r vault.Roster, id string) (vault.Member, bool) {
	for _, m := range r.Members {
		if m.DeviceID == id {
			return m, true
		}
	}
	return vault.Member{}, false
}
