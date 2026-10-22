package vault

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrNotWriter: this device's role can't append ops.
var ErrNotWriter = errors.New("vault: device is not a writer")

// Vault is one device's open view of a vault: the verified roster, the
// epoch keys this device can unwrap, and the log. Safe for concurrent use
// within a process; one Vault per device and vault (the local State is not
// shared between processes).
type Vault struct {
	mu       sync.Mutex
	store    Store
	state    StateStore
	st       State
	dev      DeviceKey
	self     string
	roster   *verifiedRoster
	cur      TrustState // newest accepted version (st.Trust is the pinned one)
	hashes   map[uint64]string
	warnings []string
	keys     map[uint64][]byte
}

// Create starts a new vault in an empty store with dev as its only admin
// (genesis roster, epoch 1), saving the local state. The genesis hash
// (Trust().Hash) is the anchor other devices are given when they join.
func Create(s Store, state StateStore, vaultID string, dev DeviceKey, owner, name string) (*Vault, error) {
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
	st := State{Trust: TrustState{VaultID: vaultID, Counter: 1, Hash: vr.hash}, NextSeq: 1}
	if err := state.Save(st); err != nil {
		return nil, err
	}
	return &Vault{
		store: s, state: state, st: st, cur: st.Trust, dev: dev, self: self.DeviceID, roster: vr,
		hashes: map[uint64]string{1: vr.hash}, keys: map[uint64][]byte{1: vk},
	}, nil
}

// Join saves the local state of a device approved into a vault: the trust
// anchor is the genesis hash the admin showed out of band. Then Open.
func Join(state StateStore, vaultID, genesisHash string) error {
	return state.Save(State{Trust: TrustState{VaultID: vaultID, Counter: 1, Hash: genesisHash}, NextSeq: 1})
}

// Open verifies the roster chain from the local trust state and unwraps
// this device's epoch keys. Fails with ErrRollback/ErrFork if storage went
// back or diverged, and ErrNotMember if this device was removed.
func Open(s Store, state StateStore, dev DeviceKey) (*Vault, error) {
	st, err := state.Load()
	if err != nil {
		return nil, err
	}
	v := &Vault{store: s, state: state, st: st, dev: dev, self: IDOf(dev)}
	if err := v.Refresh(); err != nil {
		return nil, err
	}
	return v, nil
}

// PinDelay is how old a roster version must be before it becomes the
// persisted trust anchor. Until then a client uses it in memory but loads
// from the older pinned version every time, so a version that loses an
// admin race (seen alone through a lagging listing) is replaced by the
// winner once the listing catches up, instead of being pinned and making
// the client refuse the winner forever. The price: a rollback of versions
// younger than this goes unnoticed across restarts.
const PinDelay = 5 * time.Minute

// Refresh re-reads the roster (newer versions, new epoch keys).
func (v *Vault) Refresh() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refreshLocked()
}

func (v *Vault) refreshLocked() error {
	ch, err := loadRoster(v.store, v.st.Trust)
	if err != nil {
		return err
	}
	if v.cur.Counter > ch.trust.Counter {
		return fmt.Errorf("%w: storage has %d, accepted %d", ErrRollback, ch.trust.Counter, v.cur.Counter)
	}
	r := ch.cur
	if _, ok := r.member(v.self); !ok {
		return ErrNotMember
	}
	keys := map[uint64][]byte{}
	for e := uint64(1); e <= r.Epoch; e++ {
		if vk, ok := v.keys[e]; ok {
			keys[e] = vk
			continue
		}
		vk, err := loadWrap(v.store, v.dev, &r.Roster, e)
		if errors.Is(err, ErrNotFound) && e < r.Epoch {
			continue // joined after this epoch
		}
		if err != nil {
			return fmt.Errorf("vault: key of epoch %d: %w", e, err)
		}
		keys[e] = vk
	}
	st := v.st
	horizon := time.Now().Add(-PinDelay).UnixNano()
	for c := ch.trust.Counter; c > st.Trust.Counter; c-- {
		if ch.created[c] <= horizon {
			st.Trust = TrustState{VaultID: ch.trust.VaultID, Counter: c, Hash: ch.hashes[c]}
			break
		}
	}
	if st != v.st {
		if err := v.state.Save(st); err != nil {
			return err
		}
	}
	v.st, v.cur, v.roster, v.hashes, v.warnings, v.keys = st, ch.trust, r, ch.hashes, ch.warnings, keys
	return nil
}

// Trust is the newest roster version accepted.
func (v *Vault) Trust() TrustState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cur
}

// Roster returns a copy of the current verified roster.
func (v *Vault) Roster() Roster {
	v.mu.Lock()
	defer v.mu.Unlock()
	return cloneRoster(v.roster.Roster)
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

// nextRosterLocked writes next as the successor of the current roster, then
// re-reads the listing and keeps it only if it is the version every client
// will pick at that counter (another admin may have written one too).
// Running a single "primary" admin for roster changes, with the spare
// stepping in only when the primary is gone, avoids the race altogether.
func (v *Vault) nextRosterLocked(next Roster) error {
	next.Format, next.VaultID = rosterFormat, v.roster.VaultID
	next.Counter, next.Prev = v.roster.Counter+1, v.roster.hash
	if err := next.validate(next.VaultID); err != nil {
		return err
	}
	if err := checkSuccessor(v.roster, &verifiedRoster{Roster: next}); err != nil {
		return err
	}
	vr, err := writeRoster(v.store, v.dev, &next)
	if errors.Is(err, ErrExists) {
		return ErrLostRace
	}
	if err != nil {
		return err
	}
	if err := v.refreshLocked(); err != nil {
		return err
	}
	if v.hashes[vr.Counter] != vr.hash {
		return ErrLostRace
	}
	return nil
}

// AddMember (admin) adds a device approved out of band and wraps the epoch
// keys this admin holds for it, so it can read the log written so far.
// That includes old secrets changed before it joined; phase 2 (snapshots)
// wraps only the current epoch.
func (v *Vault) AddMember(m Member) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireAdminLocked(); err != nil {
		return err
	}
	if err := validMember(m); err != nil {
		return err
	}
	if _, ok := v.roster.member(m.DeviceID); ok {
		return fmt.Errorf("vault: %s is already a member", m.DeviceID)
	}
	if _, ok := v.roster.former(m.DeviceID); ok {
		return fmt.Errorf("vault: %s was removed; a removed device can't rejoin", m.DeviceID)
	}
	next := cloneRoster(v.roster.Roster)
	next.Members = append(next.Members, m)
	for e, vk := range v.keys {
		if err := putWrap(v.store, m, v.roster.VaultID, e, vk); err != nil {
			return err
		}
	}
	return v.nextRosterLocked(next)
}

// Revoke (admin) removes a device and rotates the vault key: the closing
// epoch gets the cutoffs (highest accepted seq per author, the removed
// device included), and the new key is wrapped only for the devices that
// stay. What the removed device already decrypted is not undone; those
// secrets must be changed at their source.
//
// Ops a writer appends under the old epoch after the cutoffs were taken
// are refused; its client must re-append them under the new epoch.
func (v *Vault) Revoke(deviceID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireAdminLocked(); err != nil {
		return err
	}
	if deviceID == v.self {
		return errors.New("vault: an admin can't revoke itself; ask another admin")
	}
	next := cloneRoster(v.roster.Roster)
	found := false
	next.Members = next.Members[:0]
	for _, m := range cloneMembers(v.roster.Members) {
		if m.DeviceID == deviceID {
			next.Former = append(next.Former, m)
			found = true
			continue
		}
		next.Members = append(next.Members, m)
	}
	if !found {
		return fmt.Errorf("vault: %s is not a member", deviceID)
	}

	log, err := readLog(v.store, &v.roster.Roster, v.keys, v.hashes)
	if err != nil {
		return err
	}
	// The closing epoch's cut per author: every seq it holds an accepted op
	// under this epoch, the gaps listed as holes.
	closing := v.roster.Epoch
	used := map[string][]uint64{}
	for _, op := range log.Ops {
		if op.Epoch == closing {
			used[op.Author] = append(used[op.Author], op.Seq)
		}
	}
	seqs := map[string]Cut{}
	if n := len(v.roster.Closed); n > 0 {
		for a, c := range v.roster.Closed[n-1].Seqs {
			seqs[a] = Cut{Max: c.Max}
		}
	}
	for a, list := range used {
		slices.Sort(list)
		from := seqs[a].Max
		c := Cut{Max: list[len(list)-1]}
		for s, i := from+1, 0; s <= c.Max; s++ {
			if i < len(list) && list[i] == s {
				i++
				continue
			}
			c.Holes = append(c.Holes, s)
		}
		if len(c.Holes) > maxHoles {
			return fmt.Errorf("vault: %s left %d unused seqs in epoch %d", a, len(c.Holes), closing)
		}
		seqs[a] = c
	}

	epoch := v.roster.Epoch + 1
	vk, err := newVaultKey()
	if err != nil {
		return err
	}
	next.Epoch = epoch
	next.KeyChecks = append(next.KeyChecks, keyCheck(v.roster.VaultID, epoch, vk))
	next.Closed = append(next.Closed, EpochClose{Epoch: closing, Seqs: seqs})
	if err := next.validate(next.VaultID); err != nil {
		return err
	}
	for _, m := range next.Members {
		if err := putWrap(v.store, m, v.roster.VaultID, epoch, vk); err != nil {
			return err
		}
	}
	// The new key reaches v.keys by unwrapping this device's own wrap when
	// nextRosterLocked re-reads the roster.
	return v.nextRosterLocked(next)
}

// Append signs, encrypts (current epoch) and stores one op. The sequence
// number and clock are reserved in the local state before the write, so a
// stale listing or a crash can't make this device reuse a number.
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
	// The listing only ever raises the local counter (state lost or
	// restored from an old backup).
	entries, err := v.store.List("ops/" + v.self + "/")
	if err != nil {
		return Op{}, err
	}
	seq := max(v.st.NextSeq, 1)
	for _, e := range entries {
		if a, s, ok := parseOpName(e.Name); ok && a == v.self && s >= seq {
			seq = s + 1
		}
	}
	epoch := v.roster.Epoch
	seq = max(seq, v.roster.cut(epoch-1, v.self).Max+1)
	vk := v.keys[epoch]
	seen := RosterRef{Counter: v.roster.Counter, Hash: v.roster.hash}
	for range 1000 {
		st := v.st
		st.NextSeq, st.Lamport = seq+1, st.Lamport+1
		if st.Lamport > maxLamport {
			return Op{}, errors.New("vault: logical clock exhausted")
		}
		if err := v.state.Save(st); err != nil {
			return Op{}, err
		}
		v.st = st
		obj, err := sealOp(v.dev, v.roster.VaultID, seq, epoch, st.Lamport, seen, vk, payload)
		if err != nil {
			return Op{}, err
		}
		name := opName(v.self, seq)
		_, err = v.store.Create(name, obj)
		if errors.Is(err, ErrExists) {
			seq++
			continue
		}
		if err != nil {
			return Op{}, err
		}
		return Op{Name: name, Author: v.self, Seq: seq, Epoch: epoch, Lamport: st.Lamport, Payload: payload}, nil
	}
	return Op{}, errors.New("vault: could not allocate an op sequence number")
}

// ReadLog returns the verified log, what it refused and why, and warnings
// (roster candidates ignored, admin races, hidden roster versions).
func (v *Vault) ReadLog() (Log, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	log, err := readLog(v.store, &v.roster.Roster, v.keys, v.hashes)
	if err != nil {
		return log, err
	}
	log.Warnings = append(append([]string(nil), v.warnings...), log.Warnings...)
	// Later appends order after everything seen.
	var top uint64
	for _, op := range log.Ops {
		top = max(top, op.Lamport)
	}
	if top > v.st.Lamport {
		st := v.st
		st.Lamport = top
		if err := v.state.Save(st); err != nil {
			return log, err
		}
		v.st = st
	}
	return log, nil
}
