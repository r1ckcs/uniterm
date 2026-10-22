package vault

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Role is a member's permission in one vault.
type Role string

const (
	// RoleAdmin signs rosters (add, revoke, rotate) and writes.
	RoleAdmin Role = "admin"
	// RoleWriter appends ops.
	RoleWriter Role = "writer"
	// RoleReader decrypts but its ops are rejected.
	RoleReader Role = "reader"
)

func (r Role) canWrite() bool { return r == RoleAdmin || r == RoleWriter }

// Member is one device in the roster. Roles belong to a person (Owner) in
// the design; each of the person's devices carries the role.
type Member struct {
	DeviceID string `json:"deviceId"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	SignPub  []byte `json:"signPub"`
	KexPub   []byte `json:"kexPub"`
	Role     Role   `json:"role"`
}

// MemberOf describes a device for a roster entry.
func MemberOf(k DeviceKey, owner, name string, role Role) Member {
	return Member{
		DeviceID: IDOf(k), Owner: owner, Name: name,
		SignPub: k.SigningPublicKey(), KexPub: k.AgreementPublicKey(), Role: role,
	}
}

// Roster is one version of the vault's device list. Versions form a hash
// chain: each is signed by an admin of the previous one and names its hash,
// and the counter only grows. Clients remember the newest version they
// accepted (TrustState) and refuse anything older or diverging.
type Roster struct {
	Format  int    `json:"format"`
	VaultID string `json:"vaultId"`
	Counter uint64 `json:"counter"`
	// Prev is RosterHash of version Counter-1 ("" for the genesis).
	Prev string `json:"prev"`
	// Epoch is the current vault-key epoch (starts at 1).
	Epoch uint64 `json:"epoch"`
	// KeyChecks[e-1] commits to the vault key of epoch e (see keyCheck), so
	// a wrap planted by someone without admin rights is detected on open.
	KeyChecks []string `json:"keyChecks"`
	Members   []Member `json:"members"`
}

const rosterFormat = 1

var (
	// ErrRollback: storage offers an older roster than one already accepted.
	ErrRollback = errors.New("vault: roster rollback refused")
	// ErrFork: storage offers a different roster at a version already accepted.
	ErrFork = errors.New("vault: roster diverges from the accepted one")
	// ErrNotMember: this device is not in the current roster.
	ErrNotMember = errors.New("vault: device is not a member")
	// ErrNotAdmin: the operation needs an admin.
	ErrNotAdmin = errors.New("vault: device is not an admin")
)

// TrustState is what a device persists locally (never in the vault folder)
// to verify the roster chain: the newest version it accepted. A joining
// device starts from {VaultID, 1, genesis hash}, the hash it got out of
// band from the approving admin.
type TrustState struct {
	VaultID string `json:"vaultId"`
	Counter uint64 `json:"counter"`
	Hash    string `json:"hash"`
}

// RosterHash identifies a roster version (hash of its signed body).
func RosterHash(body []byte) string {
	sum := sha256.Sum256(append([]byte("uniterm-vault/v1 roster-hash\x00"), body...))
	return hex.EncodeToString(sum[:])
}

func rosterName(counter uint64) string { return fmt.Sprintf("roster/%012d", counter) }

func parseRosterName(name string) (uint64, bool) {
	s, ok := strings.CutPrefix(name, "roster/")
	if !ok || len(s) != 12 {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil && n > 0
}

// signed is the stored form of every signed object: the exact body bytes,
// who signed them, and the signature over (domain, body).
type signed struct {
	Body   []byte `json:"body"`
	Signer string `json:"signer"`
	Sig    []byte `json:"sig"`
}

func seal(k DeviceKey, domain string, v any) ([]byte, []byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	sig, err := sign(k, domain, body)
	if err != nil {
		return nil, nil, err
	}
	obj, err := json.Marshal(signed{Body: body, Signer: IDOf(k), Sig: sig})
	return obj, body, err
}

func (r *Roster) member(id string) (Member, bool) {
	for _, m := range r.Members {
		if m.DeviceID == id {
			return m, true
		}
	}
	return Member{}, false
}

// validate checks a roster on its own (keys well formed, ids derived from
// them, at least one admin).
func (r *Roster) validate(vaultID string) error {
	if r.Format != rosterFormat {
		return fmt.Errorf("vault: roster format %d not supported", r.Format)
	}
	if r.VaultID != vaultID {
		return fmt.Errorf("vault: roster is for vault %q, not %q", r.VaultID, vaultID)
	}
	if r.Counter == 0 || r.Epoch == 0 || uint64(len(r.KeyChecks)) != r.Epoch {
		return errors.New("vault: roster counter/epoch malformed")
	}
	seen := map[string]bool{}
	admins := 0
	for _, m := range r.Members {
		if seen[m.DeviceID] {
			return fmt.Errorf("vault: duplicate member %s", m.DeviceID)
		}
		seen[m.DeviceID] = true
		if m.Role != RoleAdmin && m.Role != RoleWriter && m.Role != RoleReader {
			return fmt.Errorf("vault: member %s has invalid role %q", m.DeviceID, m.Role)
		}
		if m.Role == RoleAdmin {
			admins++
		}
		if _, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), m.SignPub); err != nil {
			return fmt.Errorf("vault: member %s signing key: %w", m.DeviceID, err)
		}
		if _, err := ecdh.P256().NewPublicKey(m.KexPub); err != nil {
			return fmt.Errorf("vault: member %s agreement key: %w", m.DeviceID, err)
		}
		if DeviceID(m.SignPub, m.KexPub) != m.DeviceID {
			return fmt.Errorf("vault: member %s id does not match its keys", m.DeviceID)
		}
	}
	if admins == 0 {
		return errors.New("vault: roster has no admin")
	}
	return nil
}

// verifiedRoster is a roster whose chain was checked.
type verifiedRoster struct {
	Roster
	hash string
}

// parseRoster decodes and validates a stored roster object without checking
// who signed it.
func parseRoster(data []byte, vaultID string) (*verifiedRoster, signed, error) {
	var obj signed
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, obj, fmt.Errorf("vault: roster: %w", err)
	}
	var r Roster
	if err := json.Unmarshal(obj.Body, &r); err != nil {
		return nil, obj, fmt.Errorf("vault: roster body: %w", err)
	}
	if err := r.validate(vaultID); err != nil {
		return nil, obj, err
	}
	return &verifiedRoster{Roster: r, hash: RosterHash(obj.Body)}, obj, nil
}

// decodeRoster parses a stored roster object and checks its signature by an
// admin of authority (the previous version, or itself for the genesis).
func decodeRoster(data []byte, vaultID string, authority *Roster) (*verifiedRoster, error) {
	vr, obj, err := parseRoster(data, vaultID)
	if err != nil {
		return nil, err
	}
	r := vr.Roster
	if authority == nil {
		authority = &r
	}
	signer, ok := authority.member(obj.Signer)
	if !ok || signer.Role != RoleAdmin {
		return nil, fmt.Errorf("vault: roster %d signed by %s, not an admin: %w", r.Counter, obj.Signer, ErrNotAdmin)
	}
	if err := verify(signer.SignPub, "roster", obj.Body, obj.Sig); err != nil {
		return nil, fmt.Errorf("vault: roster %d: %w", r.Counter, err)
	}
	return vr, nil
}

// checkSuccessor enforces the chain rules between two signed versions.
func checkSuccessor(prev, next *verifiedRoster) error {
	if next.Counter != prev.Counter+1 || next.Prev != prev.hash {
		return fmt.Errorf("vault: roster %d does not follow %d", next.Counter, prev.Counter)
	}
	switch next.Epoch {
	case prev.Epoch:
		// Removing a device without a new key would leave it able to read
		// everything written afterwards.
		for _, m := range prev.Members {
			if _, ok := next.member(m.DeviceID); !ok {
				return fmt.Errorf("vault: roster %d removes %s without rotating the key", next.Counter, m.DeviceID)
			}
		}
	case prev.Epoch + 1:
	default:
		return fmt.Errorf("vault: roster %d jumps from epoch %d to %d", next.Counter, prev.Epoch, next.Epoch)
	}
	for i, c := range prev.KeyChecks {
		if next.KeyChecks[i] != c {
			return fmt.Errorf("vault: roster %d rewrites the key check of epoch %d", next.Counter, i+1)
		}
	}
	return nil
}

// loadRoster verifies the chain in s from the trusted version up to the
// newest one and returns it with the trust state to persist.
func loadRoster(s Store, trust TrustState) (*verifiedRoster, TrustState, error) {
	if trust.Counter == 0 || trust.Hash == "" {
		return nil, trust, errors.New("vault: empty trust state")
	}
	names, err := s.List("roster/")
	if err != nil {
		return nil, trust, err
	}
	var newest uint64
	for _, n := range names {
		if c, ok := parseRosterName(n); ok && c > newest {
			newest = c
		}
	}
	if newest < trust.Counter {
		return nil, trust, fmt.Errorf("%w: storage has %d, accepted %d", ErrRollback, newest, trust.Counter)
	}

	data, _, err := s.Get(rosterName(trust.Counter))
	if errors.Is(err, ErrNotFound) {
		return nil, trust, fmt.Errorf("%w: roster %d is gone", ErrRollback, trust.Counter)
	}
	if err != nil {
		return nil, trust, err
	}
	// The trusted version is trusted by its hash: its signature was checked
	// when it was accepted, or (genesis anchor of a joining device) the hash
	// came from an admin out of band. A genesis must still be self-signed.
	cur, _, err := parseRoster(data, trust.VaultID)
	if err != nil {
		return nil, trust, err
	}
	if cur.hash != trust.Hash {
		return nil, trust, fmt.Errorf("%w at version %d", ErrFork, trust.Counter)
	}
	if cur.Counter == 1 {
		if _, err := decodeRoster(data, trust.VaultID, nil); err != nil {
			return nil, trust, err
		}
		if cur.Prev != "" || cur.Epoch != 1 {
			return nil, trust, errors.New("vault: genesis roster malformed")
		}
	}
	if cur.Counter != trust.Counter {
		return nil, trust, fmt.Errorf("vault: roster %d claims counter %d", trust.Counter, cur.Counter)
	}

	for c := trust.Counter + 1; c <= newest; c++ {
		data, _, err := s.Get(rosterName(c))
		if err != nil {
			return nil, trust, fmt.Errorf("vault: roster %d: %w", c, err)
		}
		next, err := decodeRoster(data, trust.VaultID, &cur.Roster)
		if err != nil {
			return nil, trust, err
		}
		if err := checkSuccessor(cur, next); err != nil {
			return nil, trust, err
		}
		cur = next
	}
	return cur, TrustState{VaultID: trust.VaultID, Counter: cur.Counter, Hash: cur.hash}, nil
}

// writeRoster signs r with the admin key and creates its version object.
func writeRoster(s Store, admin DeviceKey, r *Roster) (*verifiedRoster, error) {
	obj, body, err := seal(admin, "roster", r)
	if err != nil {
		return nil, err
	}
	if _, err := s.Create(rosterName(r.Counter), obj); err != nil {
		return nil, err
	}
	return &verifiedRoster{Roster: *r, hash: RosterHash(body)}, nil
}

func cloneMembers(ms []Member) []Member {
	out := make([]Member, len(ms))
	for i, m := range ms {
		m.SignPub = bytes.Clone(m.SignPub)
		m.KexPub = bytes.Clone(m.KexPub)
		out[i] = m
	}
	return out
}
