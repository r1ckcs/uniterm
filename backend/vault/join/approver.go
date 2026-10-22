package join

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

// Request is a pending join request as the admin device lists it. Name and
// Owner come from the requesting device: render them as text only.
type Request struct {
	ID          string `json:"id"`
	DeviceID    string `json:"deviceId"`
	Fingerprint string `json:"fingerprint"`
	Owner       string `json:"owner"`
	Name        string `json:"name"`

	body []byte
	rb   requestBody
}

// List returns the genuine, well-formed requests not yet refused.
func List(s vault.Store) ([]Request, error) {
	entries, err := s.List("join/")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Request
	for _, e := range entries {
		id, ok := strings.CutPrefix(e.Name, "join/")
		if !ok {
			continue
		}
		id, ok = strings.CutSuffix(id, "/request")
		if !ok || seen[id] || strings.Contains(id, "/") {
			continue
		}
		data, _, err := s.Get(e.ID)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var rb requestBody
		if hashID("request", data) != id || json.Unmarshal(data, &rb) != nil || rb.validate() != nil {
			continue
		}
		if refused, err := exists(s, refusedName(id)); err != nil {
			return nil, err
		} else if refused {
			continue
		}
		seen[id] = true
		dev := vault.DeviceID(rb.SignPub, rb.KexPub)
		out = append(out, Request{ID: id, DeviceID: dev, Fingerprint: Fingerprint(dev),
			Owner: rb.Owner, Name: rb.Name, body: data, rb: rb})
	}
	return out, nil
}

// Discard refuses a request without approving it (spam, unknown device).
func Discard(s vault.Store, id string) error {
	if !vault.ValidName(requestName(id)) {
		return ErrBadRequest
	}
	if _, err := s.Create(refusedName(id), []byte("refused")); err != nil && !errors.Is(err, vault.ErrExists) {
		return err
	}
	return nil
}

// Approval is one admin device's attempt to approve one request. It lives
// in memory only: attempts and the deadline can't be reset by storage.
type Approval struct {
	mu       sync.Mutex
	s        vault.Store
	v        *vault.Vault
	req      Request
	offer    []byte
	offerID  string
	nonceN   []byte
	deadline time.Time
	attempts int
	done     bool
	now      func() time.Time
}

// Begin answers req with an offer anchored on the roster version v trusts
// now. dev must be this device's key and an admin of v.
func Begin(s vault.Store, v *vault.Vault, dev vault.DeviceKey, req Request) (*Approval, error) {
	return begin(s, v, dev, req, time.Now)
}

func begin(s vault.Store, v *vault.Vault, dev vault.DeviceKey, req Request, now func() time.Time) (*Approval, error) {
	if req.body == nil {
		return nil, ErrBadRequest
	}
	r := v.Roster()
	self := vault.IDOf(dev)
	isAdmin := false
	for _, m := range r.Members {
		if m.DeviceID == self && m.Role == vault.RoleAdmin {
			isAdmin = true
		}
		if m.DeviceID == req.DeviceID {
			return nil, errors.New("join: that device is already in the vault")
		}
	}
	if !isAdmin {
		return nil, vault.ErrNotAdmin
	}
	for _, m := range r.Former {
		if m.DeviceID == req.DeviceID {
			return nil, errors.New("join: that device was revoked and can't rejoin")
		}
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(offerBody{Format: 1, RequestID: req.ID, Trust: v.Trust(), Admin: self, Nonce: nonce})
	if err != nil {
		return nil, err
	}
	offerID := hashID("offer", data)
	if _, err := s.Create(offerName(req.ID, offerID), data); err != nil {
		return nil, err
	}
	return &Approval{s: s, v: v, req: req, offer: data, offerID: offerID,
		deadline: now().Add(TTL), now: now}, nil
}

// Request is the request being approved (for the dialog: name, fingerprint).
func (a *Approval) Request() Request { return a.req }

// ExpiresAt is the approval deadline (Unix ms, this device's clock).
func (a *Approval) ExpiresAt() int64 { return a.deadline.UnixMilli() }

// Ready reports whether the new device answered this offer, i.e. it now
// shows the code.
func (a *Approval) Ready() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkLocked(); err != nil {
		return false, err
	}
	return a.readyLocked()
}

func (a *Approval) checkLocked() error {
	if a.done {
		return ErrRefused
	}
	if a.now().After(a.deadline) {
		return ErrExpired
	}
	return nil
}

func (a *Approval) readyLocked() (bool, error) {
	if a.nonceN != nil {
		return true, nil
	}
	cands, err := readValid(a.s, revealName(a.req.ID, a.offerID), "", "")
	if err != nil {
		return false, err
	}
	for _, n := range cands {
		if equalBytes(commitOf(n), a.req.rb.Commit) {
			a.nonceN = n
			return true, nil
		}
	}
	return false, nil
}

func equalBytes(a, b []byte) bool { return string(a) == string(b) }

// Confirm checks the code the user typed (the one the new device shows) and,
// if it matches, adds the device with role and returns the 4-digit
// confirmation to show; the user types it on the new device to finish. A
// wrong code costs one of MaxAttempts; after the last one the request is
// refused for good.
func (a *Approval) Confirm(code string, role vault.Role) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkLocked(); err != nil {
		return "", err
	}
	if ok, err := a.readyLocked(); err != nil {
		return "", err
	} else if !ok {
		return "", ErrNotReady
	}
	if !equalCode(code, codeOf(a.req.body, a.offer, a.nonceN)) {
		a.attempts++
		if a.attempts >= MaxAttempts {
			a.done = true
			_ = Discard(a.s, a.req.ID)
			return "", ErrTooManyAttempts
		}
		return "", &WrongCodeError{Remaining: MaxAttempts - a.attempts}
	}
	m := vault.Member{DeviceID: a.req.DeviceID, Owner: a.req.Owner, Name: a.req.Name,
		SignPub: a.req.rb.SignPub, KexPub: a.req.rb.KexPub, Role: role}
	var err error
	for range 3 {
		if err = a.v.AddMember(m); !errors.Is(err, vault.ErrLostRace) {
			break
		}
	}
	if err != nil {
		return "", err
	}
	a.done = true
	return confirmationOf(a.req.body, a.offer, a.nonceN), nil
}

// Cancel refuses the request (the user closed the dialog).
func (a *Approval) Cancel() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return nil
	}
	a.done = true
	return Discard(a.s, a.req.ID)
}
