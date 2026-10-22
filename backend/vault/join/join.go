// Package join lets a new device into a vault with the approval of an admin
// device, through the vault's own storage (join/<request>/...). The user
// types on the admin device the 6-digit code the new device shows.
//
// Six digits are only 20 bits, so the code can't simply be a hash of the
// public keys: whoever can write to storage could grind keys until one
// matches. The exchange is commit-then-reveal, like Bluetooth's numeric
// comparison:
//
//  1. new device: request = keys, name, commit = H(nonceN)   (nonceN kept)
//  2. admin:      offer   = H(request), trust anchor, nonceA  (after 1)
//  3. new device: reveal  = nonceN, for that offer            (after 2)
//  4. both:       code    = H(request, offer, nonceN) mod 10^6
//
// The new device fixed nonceN before seeing nonceA, and the admin picked
// nonceA after the commit was fixed, so a man in the middle gets one
// 1-in-a-million guess per approval the user starts, and three wrong codes
// end it. The code also covers the offer, which carries the roster version
// the new device anchors its trust on: storage can't swap in a look-alike
// vault either.
package join

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ys-ll/uniterm/backend/vault"
)

// TTL is how long a request (on the new device) and an approval (on the
// admin device) stay usable, each by its own clock: storage timestamps are
// writable by anyone and play no part.
const TTL = 10 * time.Minute

// MaxAttempts is how many codes the admin may type before the approval is
// refused for good.
const MaxAttempts = 3

// MaxNameLen bounds owner and device names (runes).
const MaxNameLen = 64

// reservedName is recovery.RecoveryName (not imported: recovery imports
// nothing from here, and the UI trusts that name to mean the recovery key).
const reservedName = "uniterm-recovery-key"

var (
	ErrExpired         = errors.New("join: the request expired; start again")
	ErrTooManyAttempts = errors.New("join: too many wrong codes; the request was refused")
	ErrNotReady        = errors.New("join: the new device has not answered yet")
	ErrRefused         = errors.New("join: the request was refused")
	ErrBadRequest      = errors.New("join: malformed request")
)

// WrongCodeError: the typed code doesn't match; Remaining attempts left.
type WrongCodeError struct{ Remaining int }

func (e *WrongCodeError) Error() string {
	return fmt.Sprintf("join: wrong code (%d attempt(s) left)", e.Remaining)
}

// ErrWrongCode matches any *WrongCodeError with errors.Is.
var ErrWrongCode = errors.New("join: wrong code")

func (e *WrongCodeError) Is(target error) bool { return target == ErrWrongCode }

// requestBody is what the new device publishes. Its hash names it, so a
// candidate stored under the name with any other content is ignored.
type requestBody struct {
	Format  int    `json:"format"`
	SignPub []byte `json:"signPub"`
	KexPub  []byte `json:"kexPub"`
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	Commit  []byte `json:"commit"` // H(nonceN)
}

type offerBody struct {
	Format    int              `json:"format"`
	RequestID string           `json:"requestId"`
	Trust     vault.TrustState `json:"trust"`
	Admin     string           `json:"admin"` // approving device id
	Nonce     []byte           `json:"nonce"`
}

func hashID(domain string, body []byte) string {
	sum := sha256.Sum256(append([]byte("uniterm-vault/v1 join-"+domain+"\x00"), body...))
	return hex.EncodeToString(sum[:16])
}

func commitOf(nonce []byte) []byte {
	sum := sha256.Sum256(append([]byte("uniterm-vault/v1 join-commit\x00"), nonce...))
	return sum[:]
}

func requestName(id string) string        { return "join/" + id + "/request" }
func offerName(req, offer string) string  { return "join/" + req + "/offer-" + offer }
func revealName(req, offer string) string { return "join/" + req + "/reveal-" + offer }
func refusedName(req string) string       { return "join/" + req + "/refused" }

// codeOf is the 6-digit code the new device shows and the admin types.
func codeOf(req, offer, nonceN []byte) string {
	return digits("code", 1_000_000, "%06d", req, offer, nonceN)
}

// confirmationOf is the 4-digit number the admin device shows after a
// correct code and the new device asks for before trusting the approval.
func confirmationOf(req, offer, nonceN []byte) string {
	return digits("confirm", 10_000, "%04d", req, offer, nonceN)
}

func digits(domain string, mod uint64, format string, req, offer, nonceN []byte) string {
	h := sha256.New()
	h.Write([]byte("uniterm-vault/v1 join-" + domain + "\x00"))
	for _, f := range [][]byte{req, offer, nonceN} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(f)))
		h.Write(n[:])
		h.Write(f)
	}
	v := binary.BigEndian.Uint64(h.Sum(nil)[:8])
	return fmt.Sprintf(format, v%mod)
}

// equalCode compares a typed code in constant time (spaces ignored).
func equalCode(typed, want string) bool {
	typed = strings.Join(strings.Fields(typed), "")
	return subtle.ConstantTimeCompare([]byte(typed), []byte(want)) == 1
}

// validText refuses names that could mislead when rendered: control and
// format characters (bidi overrides, zero-width), invalid UTF-8, overlong.
func validText(s string, allowEmpty bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxNameLen || strings.TrimSpace(s) != s {
		return false
	}
	if s == "" {
		return allowEmpty
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}

func (b *requestBody) validate() error {
	if b.Format != 1 || len(b.Commit) != sha256.Size {
		return ErrBadRequest
	}
	if _, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b.SignPub); err != nil {
		return ErrBadRequest
	}
	if _, err := ecdh.P256().NewPublicKey(b.KexPub); err != nil {
		return ErrBadRequest
	}
	if !validText(b.Name, false) || !validText(b.Owner, true) || b.Name == reservedName {
		return fmt.Errorf("%w: invalid device or owner name", ErrBadRequest)
	}
	return nil
}

// Fingerprint formats a device id for display ("1a2b 3c4d …").
func Fingerprint(deviceID string) string {
	var parts []string
	for i := 0; i+4 <= len(deviceID) && i < 16; i += 4 {
		parts = append(parts, deviceID[i:i+4])
	}
	return strings.Join(parts, " ")
}

// readValid returns the bodies of the candidates under name whose content
// hashes to id (anyone can add candidates; only the genuine one matches).
func readValid(s vault.Store, name, domain, id string) ([][]byte, error) {
	entries, err := s.List(name)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, e := range entries {
		if e.Name != name {
			continue
		}
		data, _, err := s.Get(e.ID)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if id == "" || hashID(domain, data) == id {
			out = append(out, data)
		}
	}
	return out, nil
}

func exists(s vault.Store, name string) (bool, error) {
	entries, err := s.List(name)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// cleanup deletes every object of a request (best effort).
func cleanup(s vault.Store, reqID string) {
	entries, err := s.List("join/" + reqID + "/")
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = s.Delete(e.ID)
	}
}

func randomNonce() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}
