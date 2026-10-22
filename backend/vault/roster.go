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
	"maps"
	"slices"
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

func (r Role) valid() bool { return r == RoleAdmin || r == RoleWriter || r == RoleReader }

// Member is one device in the roster. Roles belong to a person (Owner) in
// the design; each of the person's devices carries the role. Owner and
// Name are plaintext metadata visible to the storage provider, and come
// from remote devices: render them as text only.
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

func (m Member) equal(o Member) bool {
	return m.DeviceID == o.DeviceID && m.Owner == o.Owner && m.Name == o.Name && m.Role == o.Role &&
		bytes.Equal(m.SignPub, o.SignPub) && bytes.Equal(m.KexPub, o.KexPub)
}

// EpochClose is written when an epoch's key is retired: for each author,
// the highest op seq the rotating admin had accepted. An op under that
// epoch is valid only up to it, so nobody who still knows the old key (the
// removed device included) can add content under it afterwards, while the
// ops written before the rotation stay readable.
type EpochClose struct {
	Epoch uint64            `json:"epoch"`
	Seqs  map[string]uint64 `json:"seqs"`
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
	// Closed[e-1] closes epoch e (one entry per past epoch).
	Closed  []EpochClose `json:"closed"`
	Members []Member     `json:"members"`
	// Former are removed devices, kept so their ops written before removal
	// still verify (within Closed cutoffs). Append-only.
	Former []Member `json:"former"`
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
	// ErrLostRace: another admin wrote a roster version at the same counter
	// first; refresh and redo the change on top of it.
	ErrLostRace = errors.New("vault: another admin changed the roster first")
)

// TrustState is the newest roster version a device accepted. A joining
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

func (r *Roster) former(id string) (Member, bool) {
	for _, m := range r.Former {
		if m.DeviceID == id {
			return m, true
		}
	}
	return Member{}, false
}

// cutoff is the last seq of author valid under epoch e (a closed epoch).
func (r *Roster) cutoff(e uint64, author string) uint64 {
	if e == 0 || e > uint64(len(r.Closed)) {
		return 0
	}
	return r.Closed[e-1].Seqs[author]
}

func validMember(m Member) error {
	if !m.Role.valid() {
		return fmt.Errorf("vault: member %s has invalid role %q", m.DeviceID, m.Role)
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
	return nil
}

// validate checks a roster on its own (keys well formed, ids derived from
// them, at least one admin, one close per past epoch).
func (r *Roster) validate(vaultID string) error {
	if r.Format != rosterFormat {
		return fmt.Errorf("vault: roster format %d not supported", r.Format)
	}
	if r.VaultID != vaultID {
		return fmt.Errorf("vault: roster is for vault %q, not %q", r.VaultID, vaultID)
	}
	if r.Counter == 0 || r.Epoch == 0 || uint64(len(r.KeyChecks)) != r.Epoch || uint64(len(r.Closed)) != r.Epoch-1 {
		return errors.New("vault: roster counter/epoch malformed")
	}
	for i, c := range r.Closed {
		if c.Epoch != uint64(i+1) {
			return errors.New("vault: roster epoch closes out of order")
		}
	}
	seen := map[string]bool{}
	admins := 0
	for _, list := range [][]Member{r.Members, r.Former} {
		for _, m := range list {
			if seen[m.DeviceID] {
				return fmt.Errorf("vault: device %s listed twice", m.DeviceID)
			}
			seen[m.DeviceID] = true
			if err := validMember(m); err != nil {
				return err
			}
		}
	}
	for _, m := range r.Members {
		if m.Role == RoleAdmin {
			admins++
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
	if authority == nil {
		authority = &vr.Roster
	}
	signer, ok := authority.member(obj.Signer)
	if !ok || signer.Role != RoleAdmin {
		return nil, fmt.Errorf("vault: roster %d signed by %s, not an admin: %w", vr.Counter, obj.Signer, ErrNotAdmin)
	}
	if err := verify(signer.SignPub, "roster", obj.Body, obj.Sig); err != nil {
		return nil, fmt.Errorf("vault: roster %d: %w", vr.Counter, err)
	}
	return vr, nil
}

// checkSuccessor enforces the chain rules between two signed versions.
func checkSuccessor(prev, next *verifiedRoster) error {
	if next.Counter != prev.Counter+1 || next.Prev != prev.hash {
		return fmt.Errorf("vault: roster %d does not follow %d", next.Counter, prev.Counter)
	}
	rotated := false
	switch next.Epoch {
	case prev.Epoch:
	case prev.Epoch + 1:
		rotated = true
	default:
		return fmt.Errorf("vault: roster %d jumps from epoch %d to %d", next.Counter, prev.Epoch, next.Epoch)
	}
	for i, c := range prev.KeyChecks {
		if next.KeyChecks[i] != c {
			return fmt.Errorf("vault: roster %d rewrites the key check of epoch %d", next.Counter, i+1)
		}
	}
	for i, c := range prev.Closed {
		if !maps.Equal(next.Closed[i].Seqs, c.Seqs) {
			return fmt.Errorf("vault: roster %d rewrites the close of epoch %d", next.Counter, i+1)
		}
	}
	if rotated && len(prev.Closed) > 0 {
		// Cutoffs only move forward.
		last, closing := prev.Closed[len(prev.Closed)-1], next.Closed[len(next.Closed)-1]
		for a, s := range last.Seqs {
			if closing.Seqs[a] < s {
				return fmt.Errorf("vault: roster %d moves %s's cutoff back", next.Counter, a)
			}
		}
	}
	// Former is append-only, and only receives devices removed now.
	if len(next.Former) < len(prev.Former) {
		return fmt.Errorf("vault: roster %d drops former members", next.Counter)
	}
	for i, m := range prev.Former {
		if !next.Former[i].equal(m) {
			return fmt.Errorf("vault: roster %d rewrites former member %s", next.Counter, m.DeviceID)
		}
	}
	for _, m := range next.Former[len(prev.Former):] {
		old, ok := prev.member(m.DeviceID)
		if !ok || !old.equal(m) {
			return fmt.Errorf("vault: roster %d lists %s as former without removing it", next.Counter, m.DeviceID)
		}
	}
	for _, m := range prev.Members {
		if _, ok := next.member(m.DeviceID); ok {
			continue
		}
		// Removing a device without a new key would leave it able to read
		// everything written afterwards.
		if !rotated {
			return fmt.Errorf("vault: roster %d removes %s without rotating the key", next.Counter, m.DeviceID)
		}
		if _, ok := next.former(m.DeviceID); !ok {
			return fmt.Errorf("vault: roster %d removes %s without keeping it as former", next.Counter, m.DeviceID)
		}
	}
	return nil
}

// rosterChain is the verified roster plus what loading it learned.
type rosterChain struct {
	cur   *verifiedRoster
	trust TrustState
	// hashes of the accepted versions from the trust anchor on.
	hashes map[uint64]string
	// warnings about ignored candidates or admin races.
	warnings []string
}

// loadRoster verifies the chain in s from the trusted version up to the
// newest valid one. Candidates that don't verify are ignored (anyone with
// write access to the folder can add some), and the chain stops at the
// first counter without a valid candidate. When admins raced and two valid
// versions exist at one counter, the earliest created wins (then the lower
// hash), the same choice on every client.
func loadRoster(s Store, trust TrustState) (*rosterChain, error) {
	if trust.Counter == 0 || trust.Hash == "" {
		return nil, errors.New("vault: empty trust state")
	}
	entries, err := s.List("roster/")
	if err != nil {
		return nil, err
	}
	byCounter := map[uint64][]Entry{}
	var newest uint64
	for _, e := range entries {
		if c, ok := parseRosterName(e.Name); ok {
			byCounter[c] = append(byCounter[c], e)
			newest = max(newest, c)
		}
	}
	if len(byCounter[trust.Counter]) == 0 {
		return nil, fmt.Errorf("%w: accepted version %d is gone (storage has up to %d)", ErrRollback, trust.Counter, newest)
	}

	// The trusted version is trusted by its hash: its signature was checked
	// when it was accepted, or (genesis anchor of a joining device) the hash
	// came from an admin out of band. A genesis must still be self-signed.
	var cur *verifiedRoster
	for _, e := range byCounter[trust.Counter] {
		data, _, err := s.Get(e.ID)
		if err != nil {
			return nil, err
		}
		if vr, _, err := parseRoster(data, trust.VaultID); err == nil && vr.hash == trust.Hash {
			if vr.Counter == 1 {
				if _, err := decodeRoster(data, trust.VaultID, nil); err != nil || vr.Prev != "" || vr.Epoch != 1 {
					return nil, errors.New("vault: genesis roster malformed")
				}
			}
			cur = vr
			break
		}
	}
	if cur == nil {
		return nil, fmt.Errorf("%w at version %d", ErrFork, trust.Counter)
	}
	if cur.Counter != trust.Counter {
		return nil, fmt.Errorf("vault: roster %d claims counter %d", trust.Counter, cur.Counter)
	}

	ch := &rosterChain{hashes: map[uint64]string{cur.Counter: cur.hash}}
	for c := trust.Counter + 1; c <= newest; c++ {
		type cand struct {
			vr      *verifiedRoster
			created int64
		}
		var valid []cand
		for _, e := range byCounter[c] {
			data, _, err := s.Get(e.ID)
			if err != nil {
				return nil, err
			}
			next, err := decodeRoster(data, trust.VaultID, &cur.Roster)
			if err == nil {
				err = checkSuccessor(cur, next)
			}
			if err == nil {
				valid = append(valid, cand{next, e.Created})
			}
		}
		if n := len(byCounter[c]) - len(valid); n > 0 {
			ch.warnings = append(ch.warnings, fmt.Sprintf("roster %d: ignored %d candidate(s) that do not follow version %d", c, n, c-1))
		}
		if len(valid) == 0 {
			break
		}
		if len(valid) > 1 {
			ch.warnings = append(ch.warnings, fmt.Sprintf("roster %d: %d valid versions (admins raced); took the earliest", c, len(valid)))
			slices.SortFunc(valid, func(a, b cand) int {
				if a.created != b.created {
					return int(min(max(a.created-b.created, -1), 1))
				}
				return strings.Compare(a.vr.hash, b.vr.hash)
			})
		}
		cur = valid[0].vr
		ch.hashes[cur.Counter] = cur.hash
	}
	ch.cur = cur
	ch.trust = TrustState{VaultID: trust.VaultID, Counter: cur.Counter, Hash: cur.hash}
	return ch, nil
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

func cloneRoster(r Roster) Roster {
	r.KeyChecks = slices.Clone(r.KeyChecks)
	closed := make([]EpochClose, len(r.Closed))
	for i, c := range r.Closed {
		closed[i] = EpochClose{Epoch: c.Epoch, Seqs: maps.Clone(c.Seqs)}
	}
	r.Closed = closed
	r.Members = cloneMembers(r.Members)
	r.Former = cloneMembers(r.Former)
	return r
}
