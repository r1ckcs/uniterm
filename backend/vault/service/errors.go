package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/join"
	"github.com/ys-ll/uniterm/backend/vault/recovery"
)

// Stable error codes (CONTRATO-cofre-f1.md §4). The message of every error
// the service returns starts with one, then ": " and an English detail.
const (
	CodeOAuthCancelled     = "vault.oauth.cancelled"
	CodeOAuthDenied        = "vault.oauth.denied"
	CodeOAuthTimeout       = "vault.oauth.timeout"
	CodeOAuthNoClientID    = "vault.oauth.noClientId"
	CodeOAuthStateMismatch = "vault.oauth.stateMismatch"
	CodeOffline            = "vault.drive.offline"
	CodeDriveAuth          = "vault.drive.auth"
	CodeDriveQuota         = "vault.drive.quota"
	CodeWrongPhase         = "vault.state.wrongPhase"
	CodeRecoveryMismatch   = "vault.recovery.mismatch"
	CodeRecoveryBadWords   = "vault.recovery.badWords"
	CodeRecoveryOther      = "vault.recovery.notThisVault"
	CodeJoinBadCode        = "vault.join.badCode"
	CodeJoinTooMany        = "vault.join.tooManyAttempts"
	CodeJoinExpired        = "vault.join.expired"
	CodeJoinNotReady       = "vault.join.notReady"
	CodeJoinNotFound       = "vault.join.notFound"
	CodeNotAdmin           = "vault.perm.notAdmin"
	CodeReadOnly           = "vault.perm.readOnly"
	CodeRevokeSelf         = "vault.revoke.self"
	CodeIntegrity          = "vault.integrity"
	CodeRevoked            = "vault.revoked"
	CodeInternal           = "vault.internal"
)

// Error is a coded error.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func newError(code, format string, args ...any) *Error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// Coded errors from other layers (the OAuth and Drive packages) carry their
// code by implementing this.
type coder interface{ VaultCode() string }

// asError maps any error to a coded one (nil stays nil).
func asError(err error) error {
	if err == nil {
		return nil
	}
	return coded(err)
}

// coded is asError for a non-nil error, typed.
func coded(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: codeOf(err), Err: err}
}

func codeOf(err error) string {
	var c coder
	var wrong *join.WrongCodeError
	var word *recovery.WordError
	switch {
	case errors.As(err, &c):
		return c.VaultCode()
	case errors.Is(err, context.Canceled):
		return CodeOAuthCancelled
	case errors.Is(err, vault.ErrUnavailable):
		return CodeOffline
	case errors.Is(err, vault.ErrRollback), errors.Is(err, vault.ErrFork):
		return CodeIntegrity
	case errors.Is(err, vault.ErrNotMember):
		return CodeRevoked
	case errors.Is(err, vault.ErrNotAdmin):
		return CodeNotAdmin
	case errors.Is(err, vault.ErrNotWriter):
		return CodeReadOnly
	case errors.As(err, &wrong):
		return CodeJoinBadCode
	case errors.Is(err, join.ErrTooManyAttempts):
		return CodeJoinTooMany
	case errors.Is(err, join.ErrExpired):
		return CodeJoinExpired
	case errors.Is(err, join.ErrNotReady):
		return CodeJoinNotReady
	case errors.Is(err, join.ErrRefused):
		return CodeJoinNotFound
	case errors.As(err, &word), errors.Is(err, recovery.ErrChecksum):
		return CodeRecoveryBadWords
	case errors.Is(err, recovery.ErrNoAnchor):
		return CodeRecoveryOther
	}
	return CodeInternal
}
