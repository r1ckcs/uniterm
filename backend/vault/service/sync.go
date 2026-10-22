package service

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/join"
	"github.com/ys-ll/uniterm/backend/vault/records"
	"github.com/ys-ll/uniterm/backend/vault/recovery"
)

// Sync runs one round: refresh the roster, capture local edits as an op,
// push the queue, read the log, write the merged result into the stores.
func (s *Service) Sync() (SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseReady || s.v == nil {
		return SyncResult{}, newError(CodeWrongPhase, "the vault is not open")
	}
	s.syncing = true
	s.emitStatusLocked()
	res, err := s.syncLocked()
	s.syncing = false
	if err != nil {
		e := coded(err)
		switch {
		case errors.Is(err, vault.ErrNotMember):
			_ = s.revokedLocked()
			s.emitStatusLocked()
			return res, newError(CodeRevoked, "this device was removed from the vault")
		case e.Code == CodeIntegrity:
			s.stopLocked()
			s.phase = PhaseError
		}
		return res, s.failLocked(err)
	}
	s.lastSync = time.Now()
	s.lastErr = nil
	s.emitStatusLocked()
	return res, nil
}

func (s *Service) syncLocked() (SyncResult, error) {
	var res SyncResult
	v := s.v
	if err := v.Refresh(); err != nil {
		return res, err
	}
	if s.isAdminLocked() {
		if err := v.SupersedeLosers(); err != nil && !errors.Is(err, vault.ErrLostRace) && !errors.Is(err, vault.ErrUnavailable) {
			return res, err
		}
	}
	key, err := s.digestKey()
	if err != nil {
		return res, err
	}
	var base records.Digest
	if err := readJSON(s.path("baseline.json"), &base); err != nil {
		return res, err
	}

	// 1. Local edits since the stores were last written become one op.
	before, err := v.ReadLog()
	if err != nil {
		return res, err
	}
	local, err := s.o.Bridge.Local()
	if err != nil {
		return res, err
	}
	var changes []records.Change
	if !s.cfg.Merged {
		// First sync of this device: bring its existing records in, but the
		// vault wins on ids it already has, and nothing is deleted.
		vaultNow := s.replayLocked(before.Ops)
		for _, c := range records.LocalChanges(records.Digest{}, key, local) {
			if _, ok := vaultNow.Records[records.Key{Kind: c.Kind, ID: c.ID}]; !ok {
				changes = append(changes, c)
			}
		}
	} else {
		changes = records.LocalChanges(base, key, local)
	}
	if len(changes) > 0 {
		if !s.canWriteLocked() {
			return res, newError(CodeReadOnly, "this device can only read the vault")
		}
		payload, err := records.Encode(time.Now(), changes)
		if err != nil {
			return res, err
		}
		s.queue = append(s.queue, queued{Payload: payload})
		if err := writeJSON(s.path("queue.json"), s.queue); err != nil {
			return res, err
		}
	}
	if !s.cfg.Merged {
		s.cfg.Merged = true
		if err := s.saveConfigLocked(); err != nil {
			return res, err
		}
	}

	// 2. Push the queue; offline, it stays for the next round.
	offline := false
	pushed, err := s.flushLocked()
	if errors.Is(err, vault.ErrUnavailable) {
		offline = true
	} else if err != nil {
		return res, err
	}
	res.Pushed = pushed

	// 3. Read the log; confirm or re-queue what was pushed.
	log, err := v.ReadLog()
	if err != nil {
		return res, err
	}
	if s.reconcileLocked(log) && !offline {
		// Refused under a closed epoch: write again under the current one.
		n, err := s.flushLocked()
		if err != nil && !errors.Is(err, vault.ErrUnavailable) {
			return res, err
		}
		res.Pushed += n
		if log, err = v.ReadLog(); err != nil {
			return res, err
		}
		s.reconcileLocked(log)
	}
	res.Warnings = log.Warnings
	for _, r := range log.Rejected {
		if a, _, ok := splitOpName(r.Name); !ok || a != vault.IDOf(s.dev) {
			res.Rejected++
		}
	}

	// 4. The merged result (log + what is still queued) goes to the stores.
	merged := s.replayLocked(log.Ops)
	for _, q := range s.queue {
		merged.Apply("queued", vault.IDOf(s.dev), q.Payload)
	}
	// The baseline is what the stores hold after the write, read back: they
	// drop fields this build doesn't know and values it doesn't accept (a
	// newer palette), and neither may come back next round as a null.
	// applied.json keeps the last snapshot written, so a snapshot the
	// stores can't hold verbatim is not written again every round.
	snap := merged.Snapshot()
	kept := snap
	if fk, ok := s.o.Bridge.(FieldKeeper); ok {
		kept = snap.Keep(fk.Keeps)
	}
	keptD := kept.Digest(key)
	var applied records.Digest
	if err := readJSON(s.path("applied.json"), &applied); err != nil {
		return res, err
	}
	if d := records.LocalChanges(local.Digest(key), key, kept); len(d) > 0 && !sameDigest(keptD, applied) {
		res.Pulled = len(d)
		if err := s.o.Bridge.Apply(snap); err != nil {
			return res, err
		}
		if local, err = s.o.Bridge.Local(); err != nil {
			return res, err
		}
	}
	if err := writeJSON(s.path("applied.json"), keptD); err != nil {
		return res, err
	}
	if err := writeJSON(s.path("baseline.json"), local.Digest(key)); err != nil {
		return res, err
	}
	s.updateMetaLocked(merged)
	s.pollRequestsLocked()
	return res, nil
}

func (s *Service) replayLocked(ops []vault.Op) *records.State {
	st := records.Replay(ops)
	return st
}

// flushLocked appends the queued payloads not yet in the log.
func (s *Service) flushLocked() (int, error) {
	n := 0
	for i := range s.queue {
		if s.queue[i].Name != "" {
			continue
		}
		op, err := s.v.Append(s.queue[i].Payload)
		if errors.Is(err, vault.ErrNotWriter) {
			// Lost write access: drop it, the stores get the vault's version.
			s.queue = slices.Delete(s.queue, i, i+1)
			_ = writeJSON(s.path("queue.json"), s.queue)
			return n, newError(CodeReadOnly, "this device can only read the vault")
		}
		if err != nil {
			_ = writeJSON(s.path("queue.json"), s.queue)
			return n, err
		}
		s.queue[i].Name = op.Name
		n++
	}
	return n, writeJSON(s.path("queue.json"), s.queue)
}

// reconcileLocked drops queued ops the log accepted and un-names those it
// refused (so they are appended again). Reports whether any was refused.
func (s *Service) reconcileLocked(log vault.Log) bool {
	accepted := map[string]bool{}
	for _, op := range log.Ops {
		accepted[op.Name] = true
	}
	refused := map[string]bool{}
	for _, r := range log.Rejected {
		refused[r.Name] = true
	}
	again := false
	kept := s.queue[:0]
	for _, q := range s.queue {
		switch {
		case q.Name != "" && accepted[q.Name]:
			continue
		case q.Name != "" && refused[q.Name]:
			q.Name = ""
			again = true
		}
		kept = append(kept, q)
	}
	s.queue = kept
	_ = writeJSON(s.path("queue.json"), s.queue)
	return again
}

func splitOpName(name string) (string, string, bool) {
	// ops/<author>/<seq>
	if len(name) < 5 || name[:4] != "ops/" {
		return "", "", false
	}
	rest := name[4:]
	for i := range len(rest) {
		if rest[i] == '/' {
			return rest[:i], rest[i+1:], true
		}
	}
	return "", "", false
}

func (s *Service) memberLocked(id string) (vault.Member, bool) {
	r := s.v.Roster()
	for _, m := range slices.Concat(r.Members, r.Former) {
		if m.DeviceID == id {
			return m, true
		}
	}
	return vault.Member{}, false
}

func (s *Service) isAdminLocked() bool {
	m, ok := s.memberLocked(vault.IDOf(s.dev))
	return ok && m.Role == vault.RoleAdmin
}

func (s *Service) canWriteLocked() bool {
	m, ok := s.memberLocked(vault.IDOf(s.dev))
	return ok && (m.Role == vault.RoleAdmin || m.Role == vault.RoleWriter)
}

// updateMetaLocked refreshes the "changed by" badges and last-seen times.
func (s *Service) updateMetaLocked(st *records.State) {
	self := vault.IDOf(s.dev)
	changed := map[string]Change{}
	for k, r := range st.Records {
		if k.Kind != records.Connection || r.Last.Author == "" || r.Last.Author == self {
			continue
		}
		name := r.Last.Author
		if m, ok := s.memberLocked(r.Last.Author); ok {
			name = m.Name
		}
		changed[k.ID] = Change{DeviceID: r.Last.Author, DeviceName: name,
			ChangedAt: r.Last.At.Format(time.RFC3339), Fields: slices.Clone(r.Last.Fields)}
	}
	s.lastSeen = maps.Clone(st.LastAt)
	if !maps.EqualFunc(changed, s.changed, func(a, b Change) bool {
		return a.DeviceID == b.DeviceID && a.ChangedAt == b.ChangedAt && slices.Equal(a.Fields, b.Fields)
	}) {
		s.changed = changed
		s.o.Emit("vault:changed", maps.Clone(changed))
	}
}

// ChangedMap is the "changed by" badge per connection id.
func (s *Service) ChangedMap() map[string]Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.changed)
}

// --- devices ---

// Devices lists the roster.
func (s *Service) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v == nil {
		return nil, newError(CodeWrongPhase, "the vault is not open")
	}
	self := vault.IDOf(s.dev)
	var out []Device
	for _, m := range s.v.Roster().Members {
		d := Device{ID: m.DeviceID, Name: m.Name, Fingerprint: join.Fingerprint(m.DeviceID),
			Role: m.Role, IsThis: m.DeviceID == self, IsRecovery: recovery.IsRecoveryMember(m)}
		if t, ok := s.lastSeen[m.DeviceID]; ok {
			d.LastSeenAt = t.Format(time.RFC3339)
		}
		out = append(out, d)
	}
	return out, nil
}

// Revoke removes a device and rotates the key. It returns the connections
// that device could read: their secrets must be changed at the source.
func (s *Service) Revoke(id string) (RevokeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v == nil {
		return RevokeResult{}, newError(CodeWrongPhase, "the vault is not open")
	}
	if id == vault.IDOf(s.dev) {
		return RevokeResult{}, newError(CodeRevokeSelf, "a device can't revoke itself")
	}
	target, _ := s.memberLocked(id)
	if recovery.IsRecoveryMember(target) {
		return RevokeResult{}, newError(CodeInternal, "the recovery key is replaced, not revoked")
	}
	var err error
	for range 3 {
		if err = s.v.Revoke(id); !errors.Is(err, vault.ErrLostRace) {
			break
		}
		_ = s.v.Refresh()
	}
	if err != nil {
		return RevokeResult{}, asError(err)
	}
	log, err := s.v.ReadLog()
	if err != nil {
		return RevokeResult{}, asError(err)
	}
	res := RevokeResult{Exposed: []Exposed{}, RecoveryStale: target.Role == vault.RoleAdmin}
	for k, r := range s.replayLocked(log.Ops).Records {
		if k.Kind != records.Connection {
			continue
		}
		var c struct {
			Name string `json:"name"`
			Host string `json:"host"`
		}
		_ = records.FromFields(k.ID, r.Fields, &c)
		res.Exposed = append(res.Exposed, Exposed{ConnectionID: k.ID, Name: c.Name, Host: c.Host})
	}
	slices.SortFunc(res.Exposed, func(a, b Exposed) int {
		if a.Name < b.Name {
			return -1
		} else if a.Name > b.Name {
			return 1
		}
		return 0
	})
	s.emitStatusLocked()
	s.kickSync()
	return res, nil
}

// --- approving join requests (admin) ---

// pollRequestsLocked lists join requests and announces new ones.
func (s *Service) pollRequestsLocked() {
	if !s.isAdminLocked() {
		return
	}
	reqs, err := join.List(s.store)
	if err != nil {
		return
	}
	for _, r := range reqs {
		if s.seenReqs[r.ID] {
			continue
		}
		s.seenReqs[r.ID] = true
		s.o.Emit("vault:joinRequest", s.requestLocked(r, nil))
	}
}

func (s *Service) requestLocked(r join.Request, a *join.Approval) JoinRequest {
	jr := JoinRequest{RequestID: r.ID, Name: r.Name, Fingerprint: r.Fingerprint,
		AttemptsLeft: join.MaxAttempts - s.attempts[r.ID]}
	if a != nil {
		jr.ExpiresAt = time.UnixMilli(a.ExpiresAt()).Format(time.RFC3339)
		jr.Ready, _ = a.Ready()
	} else {
		jr.ExpiresAt = time.Now().Add(join.TTL).Format(time.RFC3339)
	}
	return jr
}

// pollApprovals tells the dialog when the new device shows its code.
func (s *Service) pollApprovals() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, a := range s.approvals {
		ready, err := a.Ready()
		if err != nil {
			delete(s.approvals, id)
			continue
		}
		if ready && !s.seenReqs["ready:"+id] {
			s.seenReqs["ready:"+id] = true
			s.o.Emit("vault:joinRequest", s.requestLocked(a.Request(), a))
		}
	}
}

// JoinRequests lists the pending requests (to reopen the dialog).
func (s *Service) JoinRequests() ([]JoinRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v == nil {
		return nil, newError(CodeWrongPhase, "the vault is not open")
	}
	reqs, err := join.List(s.store)
	if err != nil {
		return nil, asError(err)
	}
	out := []JoinRequest{}
	for _, r := range reqs {
		out = append(out, s.requestLocked(r, s.approvals[r.ID]))
	}
	return out, nil
}

func (s *Service) findRequestLocked(id string) (join.Request, error) {
	reqs, err := join.List(s.store)
	if err != nil {
		return join.Request{}, err
	}
	for _, r := range reqs {
		if r.ID == id {
			return r, nil
		}
	}
	return join.Request{}, newError(CodeJoinNotFound, "join request %s is gone", id)
}

// BeginApproval answers a request; the new device then shows its code.
func (s *Service) BeginApproval(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v == nil {
		return newError(CodeWrongPhase, "the vault is not open")
	}
	if !s.isAdminLocked() {
		return newError(CodeNotAdmin, "only an admin approves devices")
	}
	if _, ok := s.approvals[id]; ok {
		return nil
	}
	r, err := s.findRequestLocked(id)
	if err != nil {
		return asError(err)
	}
	a, err := join.Begin(s.store, s.v, s.dev, r)
	if err != nil {
		return asError(err)
	}
	s.approvals[id] = a
	return nil
}

// ApproveJoin checks the code typed from the new device and adds it with
// role; returns the 4-digit confirmation to show.
func (s *Service) ApproveJoin(id, code6 string, role vault.Role) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok {
		return "", newError(CodeJoinNotFound, "approval %s was not started", id)
	}
	if role != vault.RoleAdmin && role != vault.RoleWriter && role != vault.RoleReader {
		return "", newError(CodeInternal, "invalid role %q", role)
	}
	conf, err := a.Confirm(code6, role)
	var wrong *join.WrongCodeError
	if errors.As(err, &wrong) {
		s.attempts[id] = join.MaxAttempts - wrong.Remaining
		s.o.Emit("vault:joinRequest", s.requestLocked(a.Request(), a))
		return "", asError(err)
	}
	if errors.Is(err, join.ErrTooManyAttempts) || errors.Is(err, join.ErrExpired) || errors.Is(err, join.ErrRefused) {
		delete(s.approvals, id)
		return "", asError(err)
	}
	if err != nil {
		// Transient (network, lost race): keep the approval. The new device
		// already answered this offer, so a new one would never be ready;
		// retrying with the same code works.
		return "", asError(err)
	}
	delete(s.approvals, id)
	s.emitStatusLocked()
	s.kickSync()
	return conf, nil
}

// RejectJoin refuses a request.
func (s *Service) RejectJoin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return newError(CodeWrongPhase, "the vault is not open")
	}
	if a, ok := s.approvals[id]; ok {
		delete(s.approvals, id)
		return asError(a.Cancel())
	}
	return asError(join.Discard(s.store, id))
}

var _ = json.Valid

func sameDigest(a, b records.Digest) bool {
	return maps.EqualFunc(a, b, func(x, y map[string]string) bool { return maps.Equal(x, y) })
}
