package service

import (
	"context"

	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/records"
)

// Phase is where this device stands with the vault (CONTRATO-cofre-f1.md §2).
type Phase string

const (
	PhaseOff              Phase = "off"
	PhaseSigningIn        Phase = "signingIn"
	PhaseCreating         Phase = "creating"
	PhaseShowRecovery     Phase = "showRecovery"
	PhaseAwaitingApproval Phase = "awaitingApproval"
	PhaseReady            Phase = "ready"
	PhaseRevoked          Phase = "revoked"
	PhaseError            Phase = "error"
)

// Status is the single state the screens render; sent on every change as
// the vault:status event.
type Status struct {
	Phase         Phase        `json:"phase"`
	PromptWelcome bool         `json:"promptWelcome"`
	TestMode      bool         `json:"testMode"`
	Account       *Account     `json:"account,omitempty"`
	Device        DeviceInfo   `json:"device"`
	PendingJoin   *PendingJoin `json:"pendingJoin,omitempty"`
	RecoveryCheck []int        `json:"recoveryCheck,omitempty"`
	LastSyncAt    string       `json:"lastSyncAt,omitempty"`
	Syncing       bool         `json:"syncing"`
	Offline       bool         `json:"offline"`
	PendingOps    int          `json:"pendingOps"`
	Error         *ErrorInfo   `json:"error,omitempty"`
}

type Account struct {
	Email string `json:"email"`
}

type DeviceInfo struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Fingerprint string     `json:"fingerprint"`
	Role        vault.Role `json:"role,omitempty"`
}

type PendingJoin struct {
	Code6        string `json:"code6,omitempty"`
	Approved     bool   `json:"approved"`
	AttemptsLeft int    `json:"attemptsLeft"`
	Fingerprint  string `json:"fingerprint"`
	ExpiresAt    string `json:"expiresAt"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Device is one roster member as the devices list shows it.
type Device struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Fingerprint string     `json:"fingerprint"`
	Role        vault.Role `json:"role"`
	IsThis      bool       `json:"isThis"`
	IsRecovery  bool       `json:"isRecovery"`
	LastSeenAt  string     `json:"lastSeenAt,omitempty"`
}

// JoinRequest is a pending request as the approving (admin) device sees it.
// Name comes from the other device: render as text only.
type JoinRequest struct {
	RequestID    string `json:"requestId"`
	Name         string `json:"name"`
	Fingerprint  string `json:"fingerprint"`
	ExpiresAt    string `json:"expiresAt"`
	Ready        bool   `json:"ready"`
	AttemptsLeft int    `json:"attemptsLeft"`
}

// Change is the "changed by" badge of one connection.
type Change struct {
	DeviceID   string   `json:"deviceId"`
	DeviceName string   `json:"deviceName"`
	ChangedAt  string   `json:"changedAt"`
	Fields     []string `json:"fields"`
}

type SyncResult struct {
	Pulled   int      `json:"pulled"`
	Pushed   int      `json:"pushed"`
	Rejected int      `json:"rejected"`
	Warnings []string `json:"warnings"`
}

type Exposed struct {
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	Host         string `json:"host"`
}

type RevokeResult struct {
	Exposed []Exposed `json:"exposed"`
}

// Bridge connects the vault to the app's stores.
type Bridge interface {
	// Local returns the stores' current content (secrets in the clear).
	Local() (records.Snapshot, error)
	// Apply writes the vault's content into the stores and tells the UI.
	Apply(records.Snapshot) error
}

// Secrets keeps small secrets: the device key, the OAuth refresh token.
// The OS keychain, or a 0600 file in test mode.
type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// Backend is where the vault lives and how this device signs in to it.
type Backend interface {
	// TestMode: a local folder instead of Google Drive.
	TestMode() bool
	// SignIn authenticates (opens the browser for Drive) and returns the
	// account. Cancelled through ctx.
	SignIn(ctx context.Context) (account string, err error)
	// Resume returns the account of a saved sign-in, if any.
	Resume() (account string, ok bool)
	// Store opens the remote store of the signed-in account.
	Store() (vault.Store, error)
	// SignOut forgets the saved sign-in (the token, not the vault).
	SignOut() error
}
