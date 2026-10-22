package vault

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrNotWriter: this device's role can't append ops.
var ErrNotWriter = errors.New("vault: device is not a writer")

// Vault is one device's open view of a vault: the verified roster, the
// epoch keys this device can unwrap, and the log. Safe for concurrent use.
type Vault struct {
	mu      sync.Mutex
	store   Store
	dev     DeviceKey
	self    string
	roster  *verifiedRoster
	trust   TrustState
	keys    map[uint64][]byte
	lamport uint64
}

// Create starts a new vault in an empty store with dev as its only admin
// (genesis roster, epoch 1). Persist v.Trust(); RosterHash of the genesis
// (v.Trust().Hash) is the anchor other devices are given when they join.
func Create(s Store, vaultID string, dev DeviceKey, owner, name string) (*Vault, error) {
	if !ValidName(vaultID) || strings.Contains(vaultID, "/") {
		return nil, fmt.Errorf("vault: invalid vault id %q", vaultID)
	}
	vk, err := newVaultKey()
	if err != nil {
		return nil, err
	}
	self := MemberOf(dev, owner, name, RoleAdmin)
	r := &Roster{
		Format: rosterFormat, VaultID: vaultID, Counter: 1, Epoch: 1,
		KeyChecks: []string{keyCheck(vaultID, 1, vk)},
		Members:   []Member{self},
	}
	if err := r.validate(vaultID); err != nil {
		return nil, err
	}
	if err := putWrap(s, self, vaultID, 1, vk); err != nil {
		return nil, err
	}
	vr, err := writeRoster(s, dev, r)
	if err != nil {
		return nil, err
	}
	return &Vault{
		store: s, dev: dev, self: self.DeviceID, roster: vr,
		trust: TrustState{VaultID: vaultID, Counter: 1, Hash: vr.hash},
		keys:  map[uint64][]byte{1: vk},
	}, nil
}

// Open verifies the roster chain from trust and unwraps this device's epoch
// keys. Fails with ErrRollback/ErrFork if storage went back or diverged,
// and ErrNotMember if this device was removed.
func Open(s Store, dev DeviceKey, trust TrustState) (*Vault, error) {
	v := &Vault{store: s, dev: dev, self: IDOf(dev), trust: trust}
	if err := v.Refresh(); err != nil {
		return nil, err
	}
	return v, nil
}

// Refresh re-reads the roster (newer versions, new epoch keys).
func (v *Vault) Refresh() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refreshLocked()
}

func (v *Vault) refreshLocked() error {
	r, trust, err := loadRoster(v.store, v.trust)
	if err != nil {
		return err
	}
	if _, ok := r.member(v.self); !ok {
		return ErrNotMember
	}
	keys := map[uint64][]byte{}
	for e := uint64(1); e <= r.Epoch; e++ {
		if vk, ok := v.keys[e]; ok {
			keys[e] = vk
			continue
		}
		wrapped, _, err := v.store.Get(wrapName(v.self, e))
		if errors.Is(err, ErrNotFound) && e < r.Epoch {
			continue // joined after this epoch
		}
		if err != nil {
			return fmt.Errorf("vault: key of epoch %d: %w", e, err)
		}
		vk, err := unwrapKey(v.dev, &r.Roster, e, wrapped)
		if err != nil {
			return err
		}
		keys[e] = vk
	}
	v.roster, v.trust, v.keys = r, trust, keys
	return nil
}

// Trust is the state to persist locally after Create/Open/Refresh.
func (v *Vault) Trust() TrustState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.trust
}

// Roster returns a copy of the current verified roster.
func (v *Vault) Roster() Roster {
	v.mu.Lock()
	defer v.mu.Unlock()
	r := v.roster.Roster
	r.KeyChecks = append([]string(nil), r.KeyChecks...)
	r.Members = cloneMembers(r.Members)
	return r
}

func putWrap(s Store, m Member, vaultID string, epoch uint64, vk []byte) error {
	wrapped, err := wrapKey(m.KexPub, vaultID, epoch, vk)
	if err != nil {
		return err
	}
	_, err = s.Create(wrapName(m.DeviceID, epoch), wrapped)
	return err
}

func (v *Vault) requireAdminLocked() error {
	if m, ok := v.roster.member(v.self); !ok || m.Role != RoleAdmin {
		return ErrNotAdmin
	}
	return nil
}

// nextRosterLocked writes the successor of the current roster.
func (v *Vault) nextRosterLocked(epoch uint64, checks []string, members []Member) error {
	r := &Roster{
		Format: rosterFormat, VaultID: v.roster.VaultID, Counter: v.roster.Counter + 1,
		Prev: v.roster.hash, Epoch: epoch, KeyChecks: checks, Members: members,
	}
	if err := r.validate(r.VaultID); err != nil {
		return err
	}
	if err := checkSuccessor(v.roster, &verifiedRoster{Roster: *r, hash: ""}); err != nil {
		return err
	}
	vr, err := writeRoster(v.store, v.dev, r)
	if err != nil {
		return err
	}
	v.roster = vr
	v.trust = TrustState{VaultID: r.VaultID, Counter: vr.Counter, Hash: vr.hash}
	return nil
}

// AddMember (admin) adds a device approved out of band and wraps the epoch
// keys this admin holds for it, so it can read the log written so far
// (until snapshots exist, phase 2).
func (v *Vault) AddMember(m Member) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireAdminLocked(); err != nil {
		return err
	}
	if _, ok := v.roster.member(m.DeviceID); ok {
		return fmt.Errorf("vault: %s is already a member", m.DeviceID)
	}
	members := append(cloneMembers(v.roster.Members), m)
	probe := v.roster.Roster
	probe.Members = members
	if err := probe.validate(probe.VaultID); err != nil {
		return err
	}
	for e, vk := range v.keys {
		if err := putWrap(v.store, m, v.roster.VaultID, e, vk); err != nil {
			return err
		}
	}
	return v.nextRosterLocked(v.roster.Epoch, append([]string(nil), v.roster.KeyChecks...), members)
}

// Revoke (admin) removes a device and rotates the vault key: the new epoch's
// key is wrapped only for the devices that stay. What the removed device
// already decrypted is not undone; those secrets must be changed at their
// source.
func (v *Vault) Revoke(deviceID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireAdminLocked(); err != nil {
		return err
	}
	var members []Member
	found := false
	for _, m := range cloneMembers(v.roster.Members) {
		if m.DeviceID == deviceID {
			found = true
			continue
		}
		members = append(members, m)
	}
	if !found {
		return fmt.Errorf("vault: %s is not a member", deviceID)
	}
	epoch := v.roster.Epoch + 1
	vk, err := newVaultKey()
	if err != nil {
		return err
	}
	checks := append(append([]string(nil), v.roster.KeyChecks...), keyCheck(v.roster.VaultID, epoch, vk))
	probe := Roster{Format: rosterFormat, VaultID: v.roster.VaultID, Counter: 1, Epoch: epoch, KeyChecks: checks, Members: members}
	if err := probe.validate(probe.VaultID); err != nil {
		return err
	}
	for _, m := range members {
		if err := putWrap(v.store, m, v.roster.VaultID, epoch, vk); err != nil {
			return err
		}
	}
	if err := v.nextRosterLocked(epoch, checks, members); err != nil {
		return err
	}
	v.keys[epoch] = vk
	return nil
}

// Append signs, encrypts (current epoch) and stores one op. Several
// devices append concurrently without coordination: each writes only
// under its own ops/<deviceID>/ prefix, and a sequence number taken by a
// concurrent writer of the same device is skipped.
func (v *Vault) Append(payload []byte) (Op, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	m, ok := v.roster.member(v.self)
	if !ok {
		return Op{}, ErrNotMember
	}
	if !m.Role.canWrite() {
		return Op{}, ErrNotWriter
	}
	names, err := v.store.List("ops/" + v.self + "/")
	if err != nil {
		return Op{}, err
	}
	var seq uint64
	for _, n := range names {
		if a, s, ok := parseOpName(n); ok && a == v.self && s > seq {
			seq = s
		}
	}
	epoch := v.roster.Epoch
	vk := v.keys[epoch]
	v.lamport++
	for range 1000 {
		seq++
		obj, err := sealOp(v.dev, v.roster.VaultID, seq, epoch, v.lamport, vk, payload)
		if err != nil {
			return Op{}, err
		}
		name := opName(v.self, seq)
		_, err = v.store.Create(name, obj)
		if errors.Is(err, ErrExists) {
			continue
		}
		if err != nil {
			return Op{}, err
		}
		return Op{Name: name, Author: v.self, Seq: seq, Epoch: epoch, Lamport: v.lamport, Payload: payload}, nil
	}
	return Op{}, errors.New("vault: could not allocate an op sequence number")
}

// ReadLog returns every op that verifies against the current roster,
// ordered by (Lamport, author, seq), and the objects it refused.
func (v *Vault) ReadLog() ([]Op, []Rejected, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	names, err := v.store.List("ops/")
	if err != nil {
		return nil, nil, err
	}
	var ops []Op
	var rejected []Rejected
	for _, name := range names {
		data, _, err := v.store.Get(name)
		if err != nil {
			return nil, nil, err
		}
		op, err := openOp(&v.roster.Roster, v.keys, name, data)
		if err != nil {
			rejected = append(rejected, Rejected{Name: name, Reason: err.Error()})
			continue
		}
		if op.Lamport > v.lamport {
			v.lamport = op.Lamport
		}
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if a.Lamport != b.Lamport {
			return a.Lamport < b.Lamport
		}
		if a.Author != b.Author {
			return a.Author < b.Author
		}
		return a.Seq < b.Seq
	})
	return ops, rejected, nil
}
