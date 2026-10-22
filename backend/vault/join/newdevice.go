package join

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

// Phase is where a new device's request stands.
type Phase string

const (
	// PhaseWaiting: no admin device has picked the request up yet.
	PhaseWaiting Phase = "waiting"
	// PhaseCode: show Code; the user types it on the admin device.
	PhaseCode Phase = "code"
	// PhaseApproved: the roster lists this device. Ask for the 4-digit
	// confirmation the admin device now shows, then Complete.
	PhaseApproved Phase = "approved"
	// PhaseRefused: refused on the admin device (wrong codes) or here.
	PhaseRefused Phase = "refused"
	// PhaseExpired: TTL passed; start again.
	PhaseExpired Phase = "expired"
)

// Status is what the new device's screen shows.
type Status struct {
	Phase       Phase  `json:"phase"`
	Code        string `json:"code,omitempty"` // 6 digits, PhaseCode and later
	Fingerprint string `json:"fingerprint"`    // this device, as the admin sees it
	RequestID   string `json:"requestId"`
	ExpiresAt   int64  `json:"expiresAt"` // Unix ms, this device's clock
}

// Joiner is a new device's pending request. Safe for concurrent use.
type Joiner struct {
	mu       sync.Mutex
	s        vault.Store
	dev      vault.DeviceKey
	body     []byte
	id       string
	nonce    []byte
	deadline time.Time
	offer    []byte
	trust    vault.TrustState
	code     string
	phase    Phase
	attempts int
	now      func() time.Time
}

// Start publishes a join request for dev. owner and name are shown to the
// approving admin and stored in plaintext in the roster: no control or
// bidi characters, at most MaxNameLen runes.
func Start(s vault.Store, dev vault.DeviceKey, owner, name string) (*Joiner, error) {
	return start(s, dev, owner, name, time.Now)
}

func start(s vault.Store, dev vault.DeviceKey, owner, name string, now func() time.Time) (*Joiner, error) {
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	rb := requestBody{Format: 1, SignPub: dev.SigningPublicKey(), KexPub: dev.AgreementPublicKey(),
		Owner: owner, Name: name, Commit: commitOf(nonce)}
	if err := rb.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(rb)
	if err != nil {
		return nil, err
	}
	id := hashID("request", body)
	if _, err := s.Create(requestName(id), body); err != nil {
		return nil, err
	}
	return &Joiner{s: s, dev: dev, body: body, id: id, nonce: nonce,
		deadline: now().Add(TTL), phase: PhaseWaiting, now: now}, nil
}

func (j *Joiner) statusLocked() Status {
	return Status{Phase: j.phase, Code: j.code, Fingerprint: Fingerprint(vault.IDOf(j.dev)),
		RequestID: j.id, ExpiresAt: j.deadline.UnixMilli()}
}

func (j *Joiner) endLocked(p Phase) {
	j.phase = p
	j.nonce = nil
	cleanup(j.s, j.id)
}

// Poll advances the request: picks up the admin's offer and answers it
// (PhaseCode), then watches the roster for this device (PhaseApproved).
func (j *Joiner) Poll() (Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch j.phase {
	case PhaseRefused, PhaseExpired, PhaseApproved:
		return j.statusLocked(), nil
	}
	if j.now().After(j.deadline) {
		j.endLocked(PhaseExpired)
		return j.statusLocked(), nil
	}
	if refused, err := exists(j.s, refusedName(j.id)); err != nil {
		return j.statusLocked(), err
	} else if refused {
		j.endLocked(PhaseRefused)
		return j.statusLocked(), nil
	}
	if j.offer == nil {
		if err := j.answerOfferLocked(); err != nil || j.offer == nil {
			return j.statusLocked(), err
		}
	}
	_, err := vault.Open(j.s, vault.NewMemState(vault.State{Trust: j.trust, NextSeq: 1}), j.dev)
	switch {
	case err == nil:
		j.phase = PhaseApproved
	case errors.Is(err, vault.ErrNotMember), errors.Is(err, vault.ErrFork), errors.Is(err, vault.ErrNotFound):
		// Not approved yet, or the offer points at a roster this storage
		// doesn't hold (a foreign offer): keep waiting until TTL.
	default:
		return j.statusLocked(), err
	}
	return j.statusLocked(), nil
}

// answerOfferLocked takes the first genuine offer for this request (its
// name carries its hash) and reveals the nonce for it. Another device or
// storage itself may add offers; picking one that the user is not
// approving only stalls the request, it can't let anything in.
func (j *Joiner) answerOfferLocked() error {
	prefix := "join/" + j.id + "/offer-"
	entries, err := j.s.List(prefix)
	if err != nil {
		return err
	}
	slices.SortStableFunc(entries, func(a, b vault.Entry) int {
		if a.Created != b.Created {
			return int(min(max(a.Created-b.Created, -1), 1))
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, e := range entries {
		offerID, ok := strings.CutPrefix(e.Name, prefix)
		if !ok {
			continue
		}
		data, _, err := j.s.Get(e.ID)
		if errors.Is(err, vault.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		var ob offerBody
		if hashID("offer", data) != offerID || json.Unmarshal(data, &ob) != nil || ob.Format != 1 ||
			ob.RequestID != j.id || len(ob.Nonce) != 32 || ob.Trust.Counter == 0 || ob.Trust.Hash == "" ||
			ob.Trust.VaultID == "" {
			continue
		}
		if _, err := j.s.Create(revealName(j.id, offerID), j.nonce); err != nil && !errors.Is(err, vault.ErrExists) {
			return err
		}
		j.offer, j.trust = data, ob.Trust
		j.code = codeOf(j.body, data, j.nonce)
		j.phase = PhaseCode
		return nil
	}
	return nil
}

// Complete finishes once Poll reported PhaseApproved: confirmation is the
// 4-digit number the admin device showed after accepting the code. It
// proves to this device that the user saw the approval on the device they
// trust; without it, whoever controls storage could approve this device
// into a look-alike vault of their own. Saves the trust anchor in state
// and opens the vault.
func (j *Joiner) Complete(state vault.StateStore, confirmation string) (*vault.Vault, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch j.phase {
	case PhaseApproved:
	case PhaseExpired:
		return nil, ErrExpired
	case PhaseRefused:
		return nil, ErrRefused
	default:
		return nil, ErrNotReady
	}
	if j.now().After(j.deadline) {
		j.endLocked(PhaseExpired)
		return nil, ErrExpired
	}
	if !equalCode(confirmation, confirmationOf(j.body, j.offer, j.nonce)) {
		j.attempts++
		if j.attempts >= MaxAttempts {
			j.endLocked(PhaseRefused)
			return nil, ErrTooManyAttempts
		}
		return nil, &WrongCodeError{Remaining: MaxAttempts - j.attempts}
	}
	if err := state.Save(vault.State{Trust: j.trust, NextSeq: 1}); err != nil {
		return nil, err
	}
	v, err := vault.Open(j.s, state, j.dev)
	if err != nil {
		return nil, err
	}
	j.nonce = nil
	cleanup(j.s, j.id)
	return v, nil
}

// Cancel withdraws the request and deletes its objects.
func (j *Joiner) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.phase != PhaseExpired {
		j.endLocked(PhaseRefused)
	}
}
