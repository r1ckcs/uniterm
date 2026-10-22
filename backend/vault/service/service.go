// Package service runs the credential vault for the app: sign-in, the
// phases the screens show (CONTRATO-cofre-f1.md), joining and recovery,
// and syncing the connection and identity stores through the vault log.
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/join"
	"github.com/ys-ll/uniterm/backend/vault/records"
	"github.com/ys-ll/uniterm/backend/vault/recovery"
)

// Options configures a Service.
type Options struct {
	// Dir holds the local vault files (state, queue, cache, config). It is
	// per data dir, so a second data dir is a second device.
	Dir     string
	Backend Backend
	Secrets Secrets
	// SecretsKey namespaces the keychain entries (per data dir).
	SecretsKey string
	Bridge     Bridge
	// Emit sends an event to the UI.
	Emit func(name string, data any)
	// DeviceName is the default name for this device (host name).
	DeviceName string
	// Owner is recorded in the roster with the device.
	Owner string
	// SyncEvery is the background sync interval (60 s if zero).
	SyncEvery time.Duration
	// Debounce is the delay between a local save and its sync (2 s if zero).
	Debounce time.Duration
	// PollEvery is how often join requests are polled (2 s if zero).
	PollEvery time.Duration
}

// config is what the service persists besides the vault state.
type config struct {
	WelcomeAnswered bool   `json:"welcomeAnswered"`
	Account         string `json:"account,omitempty"`
	DeviceName      string `json:"deviceName,omitempty"`
	// Merged: the first sync merged this device's existing stores.
	Merged bool `json:"merged"`
	// Revoked: this device was removed from the vault.
	Revoked bool `json:"revoked,omitempty"`
}

// queued is a local change waiting to be (or confirmed as) in the log.
type queued struct {
	Payload []byte `json:"payload"`
	// Name of the op once appended; cleared to re-append it when the log
	// refused it (written under an epoch that was closed meanwhile).
	Name string `json:"name,omitempty"`
}

// Service is the app's vault. Safe for concurrent use.
type Service struct {
	o Options

	mu       sync.Mutex
	cfg      config
	phase    Phase
	lastErr  *ErrorInfo
	dev      *vault.SoftwareDeviceKey
	store    *vault.CachedStore
	v        *vault.Vault
	queue    []queued
	lastSync time.Time
	syncing  bool
	cancel   context.CancelFunc // sign-in in progress
	stop     chan struct{}      // background loops of the current session
	kick     chan struct{}

	// first device: the vault drafted in memory until the words are confirmed
	draft      *vault.MemStore
	draftState *vault.MemState
	draftV     *vault.Vault
	words      []string
	check      []int // 1-based positions
	// regen: the words replace the recovery key of the open vault s.v
	regen bool

	// new device
	joiner       *join.Joiner
	joinStatus   join.Status
	joinAttempts int

	// approving device
	approvals map[string]*join.Approval
	attempts  map[string]int
	seenReqs  map[string]bool

	changed  map[string]Change
	lastSeen map[string]time.Time

	// background goroutines, waited for by Close
	wg     sync.WaitGroup
	closed bool
}

// spawn runs f in a goroutine that Close waits for. Caller holds s.mu.
func (s *Service) spawn(f func()) {
	if s.closed {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		f()
	}()
}

// New loads the local files; call Start to resume a saved session.
func New(o Options) (*Service, error) {
	if o.SyncEvery == 0 {
		o.SyncEvery = time.Minute
	}
	if o.Debounce == 0 {
		o.Debounce = 2 * time.Second
	}
	if o.PollEvery == 0 {
		o.PollEvery = 2 * time.Second
	}
	if o.Emit == nil {
		o.Emit = func(string, any) {}
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	s := &Service{o: o, phase: PhaseOff, approvals: map[string]*join.Approval{},
		attempts: map[string]int{}, seenReqs: map[string]bool{}, kick: make(chan struct{}, 1)}
	if err := readJSON(s.path("service.json"), &s.cfg); err != nil {
		return nil, err
	}
	if err := readJSON(s.path("queue.json"), &s.queue); err != nil {
		return nil, err
	}
	dev, err := s.deviceKey()
	if err != nil {
		return nil, err
	}
	s.dev = dev
	return s, nil
}

func (s *Service) path(name string) string { return filepath.Join(s.o.Dir, name) }

func (s *Service) secretName(name string) string { return "vault/" + s.o.SecretsKey + "/" + name }

// deviceKey loads this device's key, creating it on first use. It stays
// across sign-outs: signing in again does not need a new approval.
func (s *Service) deviceKey() (*vault.SoftwareDeviceKey, error) {
	if enc, err := s.o.Secrets.Get(s.secretName("device-key")); err == nil && enc != "" {
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("vault: device key: %w", err)
		}
		return vault.UnmarshalSoftwareDeviceKey(raw)
	}
	k, err := vault.NewSoftwareDeviceKey()
	if err != nil {
		return nil, err
	}
	raw, err := k.Marshal()
	if err != nil {
		return nil, err
	}
	if err := s.o.Secrets.Set(s.secretName("device-key"), base64.StdEncoding.EncodeToString(raw)); err != nil {
		return nil, err
	}
	return k, nil
}

// digestKey is the local HMAC key of the baseline digest.
func (s *Service) digestKey() ([]byte, error) {
	name := s.secretName("digest-key")
	if enc, err := s.o.Secrets.Get(name); err == nil && enc != "" {
		return base64.StdEncoding.DecodeString(enc)
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, s.o.Secrets.Set(name, base64.StdEncoding.EncodeToString(k))
}

func (s *Service) deviceName() string {
	if s.cfg.DeviceName != "" {
		return s.cfg.DeviceName
	}
	if s.o.DeviceName != "" {
		return s.o.DeviceName
	}
	return "FlexTerm"
}

// Start resumes a saved sign-in (no browser) in the background.
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Revoked {
		s.phase = PhaseRevoked
		return
	}
	if _, ok := s.o.Backend.Resume(); !ok || s.cfg.Account == "" {
		return
	}
	s.spawn(s.enter)
}

// Close stops the background work and waits for it (a sync in progress
// finishes first); the service is unusable afterwards.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.stopLocked()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) stopLocked() {
	if s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
	if s.joiner != nil {
		s.joiner.Cancel()
		s.joiner = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// --- status ---

// Status is the current state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() Status {
	st := Status{
		Phase:         s.phase,
		PromptWelcome: !s.cfg.WelcomeAnswered && s.cfg.Account == "",
		TestMode:      s.o.Backend.TestMode(),
		Device: DeviceInfo{ID: vault.IDOf(s.dev), Name: s.deviceName(),
			Fingerprint: join.Fingerprint(vault.IDOf(s.dev))},
		Syncing:    s.syncing,
		PendingOps: len(s.queue),
		Error:      s.lastErr,
	}
	if s.cfg.Account != "" {
		st.Account = &Account{Email: s.cfg.Account}
	}
	if s.store != nil {
		st.Offline = s.store.Offline()
	}
	if !s.lastSync.IsZero() {
		st.LastSyncAt = s.lastSync.Format(time.RFC3339)
	}
	if s.v != nil {
		for _, m := range s.v.Roster().Members {
			if m.DeviceID == st.Device.ID {
				st.Device.Role = m.Role
			}
			if recovery.IsRecoveryMember(m) {
				st.RecoveryKeys++
			}
		}
	}
	if s.phase == PhaseShowRecovery {
		st.RecoveryRegen = s.regen
		for _, p := range s.check {
			st.RecoveryCheck = append(st.RecoveryCheck, p-1)
		}
	}
	if s.phase == PhaseAwaitingApproval && s.joiner != nil {
		js := s.joinStatus
		st.PendingJoin = &PendingJoin{
			Code6: js.Code, Approved: js.Phase == join.PhaseApproved,
			AttemptsLeft: join.MaxAttempts - s.joinAttempts,
			Fingerprint:  st.Device.Fingerprint,
			ExpiresAt:    time.UnixMilli(js.ExpiresAt).Format(time.RFC3339),
		}
	}
	return st
}

func (s *Service) emitStatusLocked() {
	s.o.Emit("vault:status", s.statusLocked())
}

func (s *Service) setPhaseLocked(p Phase) {
	s.phase = p
	s.emitStatusLocked()
}

// failLocked records err for the status and returns it as a coded error.
func (s *Service) failLocked(err error) error {
	e := coded(err)
	s.lastErr = &ErrorInfo{Code: e.Code, Message: e.Error()}
	s.emitStatusLocked()
	return e
}

func (s *Service) saveConfigLocked() error { return writeJSON(s.path("service.json"), s.cfg) }

// --- welcome, sign-in, sign-out ---

// Skip answers the welcome screen with "not now".
func (s *Service) Skip() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.WelcomeAnswered = true
	if err := s.saveConfigLocked(); err != nil {
		return asError(err)
	}
	s.emitStatusLocked()
	return nil
}

// SignIn starts the sign-in in the background; the result arrives as
// vault:status.
func (s *Service) SignIn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseOff && s.phase != PhaseError {
		return newError(CodeWrongPhase, "sign-in from phase %s", s.phase)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.lastErr = nil
	s.cfg.WelcomeAnswered = true
	s.setPhaseLocked(PhaseSigningIn)
	s.spawn(func() {
		account, err := s.o.Backend.SignIn(ctx)
		s.mu.Lock()
		s.cancel = nil
		if err != nil {
			s.phase = PhaseOff
			_ = s.failLocked(err)
			s.mu.Unlock()
			return
		}
		s.cfg.Account = account
		_ = s.saveConfigLocked()
		s.mu.Unlock()
		s.enter()
	})
	return nil
}

// CancelSignIn aborts a sign-in in progress.
func (s *Service) CancelSignIn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.phase == PhaseSigningIn {
		s.lastErr = &ErrorInfo{Code: CodeOAuthCancelled, Message: CodeOAuthCancelled + ": cancelled"}
		s.setPhaseLocked(PhaseOff)
	}
	return nil
}

// SignOut forgets the sign-in and stops syncing. The local copy and the
// device key stay, so signing in again resumes without a new approval.
func (s *Service) SignOut() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	if err := s.o.Backend.SignOut(); err != nil {
		return asError(err)
	}
	s.cfg.Account = ""
	s.store, s.v, s.draft, s.draftV, s.words = nil, nil, nil, nil, nil
	s.lastErr = nil
	if err := s.saveConfigLocked(); err != nil {
		return asError(err)
	}
	if s.cfg.Revoked {
		s.setPhaseLocked(PhaseRevoked)
	} else {
		s.setPhaseLocked(PhaseOff)
	}
	return nil
}

// Rename sets this device's name (used for the next roster entry or join
// request; an existing roster entry keeps the name it was approved with).
func (s *Service) Rename(name string) error {
	if name == "" || len([]rune(name)) > join.MaxNameLen {
		return newError(CodeInternal, "device name must have 1 to %d characters", join.MaxNameLen)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.DeviceName = name
	if err := s.saveConfigLocked(); err != nil {
		return asError(err)
	}
	s.emitStatusLocked()
	return nil
}

// enter opens the store after sign-in and picks the phase: open the vault
// (member), draft one (empty storage) or ask to join.
func (s *Service) enter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if err := s.enterLocked(); err != nil {
		if s.phase == PhaseSigningIn || s.phase == PhaseCreating {
			s.phase = PhaseOff
		}
		_ = s.failLocked(err)
	}
}

func (s *Service) enterLocked() error {
	remote, err := s.o.Backend.Store()
	if err != nil {
		return err
	}
	cs, err := vault.NewCachedStore(remote, s.path("cache"))
	if err != nil {
		return err
	}
	s.store = cs
	state := vault.FileState{Path: s.path("state.json")}
	if _, err := state.Load(); err == nil {
		return s.openLocked()
	} else if !errors.Is(err, vault.ErrNoState) {
		return err
	}
	rosters, err := cs.List("roster/")
	if err != nil {
		return err
	}
	if cs.Offline() {
		return newError(CodeOffline, "storage unavailable")
	}
	if len(rosters) == 0 {
		return s.draftLocked()
	}
	return s.startJoinLocked()
}

// openLocked opens the vault from the saved state and starts syncing.
func (s *Service) openLocked() error {
	v, err := vault.Open(s.store, vault.FileState{Path: s.path("state.json")}, s.dev)
	if errors.Is(err, vault.ErrNotMember) {
		return s.revokedLocked()
	}
	if err != nil {
		if errors.Is(err, vault.ErrRollback) || errors.Is(err, vault.ErrFork) {
			s.phase = PhaseError
		}
		return err
	}
	s.v = v
	s.lastErr = nil
	s.setPhaseLocked(PhaseReady)
	s.startLoopsLocked()
	return nil
}

func (s *Service) revokedLocked() error {
	s.stopLocked()
	s.v = nil
	s.cfg.Revoked = true
	_ = s.saveConfigLocked()
	s.phase = PhaseRevoked
	return newError(CodeRevoked, "this device was removed from the vault")
}

// --- first device: draft, words, confirm ---

func (s *Service) draftLocked() error {
	s.setPhaseLocked(PhaseCreating)
	draft := vault.NewMemStore()
	st := vault.NewMemState(vault.State{})
	id, err := newVaultID()
	if err != nil {
		return err
	}
	v, err := vault.Create(draft, st, id, s.dev, s.o.Owner, s.deviceName())
	if err != nil {
		return err
	}
	words, err := recovery.Setup(draft, v)
	if err != nil {
		return err
	}
	check, err := recovery.Challenge(3)
	if err != nil {
		return err
	}
	s.draft, s.draftState, s.draftV, s.words, s.check = draft, st, v, words, check
	s.setPhaseLocked(PhaseShowRecovery)
	return nil
}

func newVaultID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("v%x", b), nil
}

// RecoveryWords returns the 24 words (only while they are being shown).
func (s *Service) RecoveryWords() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseShowRecovery {
		return nil, newError(CodeWrongPhase, "no recovery words to show")
	}
	return slices.Clone(s.words), nil
}

// ConfirmRecovery checks the words at idx (0-based, the recoveryCheck
// positions) and, if right, publishes the drafted vault.
func (s *Service) ConfirmRecovery(idx []int, answers []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseShowRecovery {
		return newError(CodeWrongPhase, "nothing to confirm")
	}
	pos := make([]int, len(idx))
	for i, p := range idx {
		pos[i] = p + 1
	}
	if len(idx) != len(s.check) || !slices.Equal(pos, s.check) || !recovery.Check(s.words, pos, answers) {
		return newError(CodeRecoveryMismatch, "the words do not match")
	}
	if s.regen {
		return s.finishRegenLocked()
	}
	if err := s.publishDraftLocked(); err != nil {
		return s.failLocked(err)
	}
	s.words, s.check, s.draft, s.draftV, s.draftState = nil, nil, nil, nil, nil
	return s.openLocked()
}

// RegenerateRecovery (admin) starts replacing the recovery key: the new
// words are shown and confirmed like at creation (phase showRecovery with
// recoveryRegen), and only then the new key and its anchor are written and
// the old key revoked. Until then the old phrase keeps working; cancelling
// or quitting changes nothing. Needed after revoking an admin
// (RevokeResult.RecoveryStale): the old anchor predates the revocation.
func (s *Service) RegenerateRecovery() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase == PhaseRevoked {
		return newError(CodeRevoked, "this device was removed from the vault")
	}
	if s.phase != PhaseReady || s.v == nil {
		return newError(CodeWrongPhase, "the vault is not open")
	}
	// Decide on the current roster (this device may have been revoked or
	// demoted since the last sync).
	if err := s.v.Refresh(); errors.Is(err, vault.ErrNotMember) {
		_ = s.revokedLocked()
		s.emitStatusLocked()
		return newError(CodeRevoked, "this device was removed from the vault")
	} else if err != nil {
		return s.failLocked(err)
	}
	if !s.isAdminLocked() {
		return newError(CodeNotAdmin, "only an admin replaces the recovery key")
	}
	words, _, err := recovery.Generate()
	if err != nil {
		return asError(err)
	}
	check, err := recovery.Challenge(3)
	if err != nil {
		return asError(err)
	}
	s.words, s.check, s.regen = words, check, true
	s.lastErr = nil
	s.setPhaseLocked(PhaseShowRecovery)
	return nil
}

// CancelRecovery abandons a recovery key replacement (the old key stays).
func (s *Service) CancelRecovery() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseShowRecovery || !s.regen {
		return newError(CodeWrongPhase, "no recovery key replacement in progress")
	}
	s.words, s.check, s.regen = nil, nil, false
	s.setPhaseLocked(PhaseReady)
	return nil
}

func (s *Service) finishRegenLocked() error {
	var err error
	for range 3 {
		if err = recovery.SetupWithWords(s.store, s.v, s.words); !errors.Is(err, vault.ErrLostRace) {
			break
		}
		_ = s.v.Refresh()
	}
	if err != nil {
		// Still in showRecovery with the same words: confirming again
		// resumes where it stopped.
		return s.failLocked(err)
	}
	s.words, s.check, s.regen = nil, nil, false
	s.lastErr = nil
	s.setPhaseLocked(PhaseReady)
	s.kickSync()
	return nil
}

// publishDraftLocked copies every drafted object (rosters, wraps, the
// recovery anchor) to the real store and saves the drafted trust state.
func (s *Service) publishDraftLocked() error {
	existing, err := s.store.List("roster/")
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return errors.New("vault: another device created a vault meanwhile; sign in again to join it")
	}
	entries, err := s.draft.List("")
	if err != nil {
		return err
	}
	// Rosters last, so a device listing midway never sees a roster whose
	// wraps are missing.
	slices.SortStableFunc(entries, func(a, b vault.Entry) int {
		ra, rb := len(a.Name) > 7 && a.Name[:7] == "roster/", len(b.Name) > 7 && b.Name[:7] == "roster/"
		switch {
		case ra == rb:
			return 0
		case ra:
			return 1
		}
		return -1
	})
	for _, e := range entries {
		data, _, err := s.draft.Get(e.ID)
		if err != nil {
			return err
		}
		if _, err := s.store.Create(e.Name, data); err != nil {
			return err
		}
	}
	st, err := s.draftState.Load()
	if err != nil {
		return err
	}
	return vault.FileState{Path: s.path("state.json")}.Save(st)
}

// --- new device: join or recover ---

func (s *Service) startJoinLocked() error {
	j, err := join.Start(s.store, s.dev, s.o.Owner, s.deviceName())
	if err != nil {
		return err
	}
	s.joiner, s.joinAttempts = j, 0
	s.joinStatus, _ = j.Poll()
	s.setPhaseLocked(PhaseAwaitingApproval)
	stop := make(chan struct{})
	s.stop = stop
	s.spawn(func() { s.pollJoin(j, stop) })
	return nil
}

func (s *Service) pollJoin(j *join.Joiner, stop chan struct{}) {
	t := time.NewTicker(s.o.PollEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		st, err := j.Poll()
		s.mu.Lock()
		if s.joiner != j {
			s.mu.Unlock()
			return
		}
		switch {
		case err != nil:
			s.lastErr = &ErrorInfo{Code: coded(err).Code, Message: err.Error()}
		case st.Phase == join.PhaseExpired || st.Phase == join.PhaseRefused:
			// A fresh request with a new code.
			s.joiner = nil
			if err := s.startJoinLocked(); err != nil {
				_ = s.failLocked(err)
			}
			s.mu.Unlock()
			return
		}
		if st != s.joinStatus || err != nil {
			s.joinStatus = st
			s.emitStatusLocked()
		}
		s.mu.Unlock()
	}
}

// CompleteJoin finishes an approved join with the 4-digit confirmation the
// approving device shows.
func (s *Service) CompleteJoin(code4 string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseAwaitingApproval || s.joiner == nil {
		return newError(CodeWrongPhase, "no join in progress")
	}
	v, err := s.joiner.Complete(vault.FileState{Path: s.path("state.json")}, code4)
	var wrong *join.WrongCodeError
	if errors.As(err, &wrong) {
		s.joinAttempts = join.MaxAttempts - wrong.Remaining
		s.emitStatusLocked()
		return asError(err)
	}
	if err != nil {
		return asError(err)
	}
	s.stopLocked()
	s.v = v
	s.lastErr = nil
	s.setPhaseLocked(PhaseReady)
	s.startLoopsLocked()
	return nil
}

// CancelJoin withdraws the join request (still signed in).
func (s *Service) CancelJoin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseAwaitingApproval {
		return newError(CodeWrongPhase, "no join in progress")
	}
	s.stopLocked()
	s.setPhaseLocked(PhaseOff)
	return nil
}

// RecoverWithWords lets this device in with the recovery phrase.
func (s *Service) RecoverWithWords(words []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseAwaitingApproval {
		return newError(CodeWrongPhase, "recovery is for a device waiting to join")
	}
	t, err := recovery.Recover(s.store, words, s.dev, s.o.Owner, s.deviceName())
	if err != nil {
		return asError(err)
	}
	// Withdraw the pending join request (join/<req> is not left behind).
	s.stopLocked()
	if err := (vault.FileState{Path: s.path("state.json")}).Save(vault.State{Trust: t, NextSeq: 1}); err != nil {
		return asError(err)
	}
	if err := s.openLocked(); err != nil {
		return s.failLocked(err)
	}
	return nil
}

// --- background loops ---

func (s *Service) startLoopsLocked() {
	if s.stop != nil {
		close(s.stop)
	}
	stop := make(chan struct{})
	s.stop = stop
	s.spawn(func() { s.loop(stop) })
	s.kickSync()
}

func (s *Service) kickSync() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// LocalChanged is called after the app saved connections or identities.
func (s *Service) LocalChanged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseReady {
		return
	}
	stop := s.stop
	s.spawn(func() {
		select {
		case <-time.After(s.o.Debounce):
			s.kickSync()
		case <-stop:
		}
	})
}

func (s *Service) loop(stop chan struct{}) {
	t := time.NewTicker(s.o.SyncEvery)
	defer t.Stop()
	poll := time.NewTicker(s.o.PollEvery)
	defer poll.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			_, _ = s.Sync()
		case <-s.kick:
			_, _ = s.Sync()
		case <-poll.C:
			s.pollApprovals()
		}
	}
}

// --- helpers ---

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// A unique temp name: two writers (a closing and a new Service on the
	// same dir) must not rename each other's file away.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}

var _ = records.Connection
