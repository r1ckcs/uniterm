package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ys-ll/uniterm/backend/credentials"
	"github.com/ys-ll/uniterm/backend/log"
	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/sync"
	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/records"
	"github.com/ys-ll/uniterm/backend/vault/service"
)

// initVault starts the credential vault service for dataDir (after the
// stores and the credential store are wired). See CONTRATO-cofre-f1.md.
func (a *App) initVault(dataDir string) {
	if a.vaultService != nil {
		a.vaultService.Close()
		a.vaultService = nil
	}
	dir := filepath.Join(dataDir, "vault")
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	opts := service.Options{
		Dir:        dir,
		SecretsKey: hex.EncodeToString(sum[:4]),
		Bridge:     &storeBridge{a: a},
		Emit:       func(name string, data any) { a.emit(name, data) },
		DeviceName: deviceName(),
	}
	fileBackend, testMode, err := service.FileBackendFromEnv(dir)
	switch {
	case err != nil:
		log.Writef("vault: %v", err)
		return
	case testMode:
		log.Writef("vault: test mode, storage %s", fileBackend.Path)
		opts.Backend = fileBackend
		opts.Secrets = &service.FileSecrets{Path: filepath.Join(dir, "secrets.json")}
	default:
		opts.Backend = newDriveBackend(dataDir, sync.NewKeychain(), opts.SecretsKey, a.openURL)
		opts.Secrets = sync.NewKeychain()
	}
	svc, err := service.New(opts)
	if err != nil {
		log.Writef("vault: %v", err)
		return
	}
	a.vaultService = svc
	onSave := func() { svc.LocalChanged() }
	if a.connectionStore != nil {
		a.connectionStore.SetOnSave(onSave)
	}
	if a.identityStore != nil {
		a.identityStore.SetOnSave(onSave)
	}
	svc.Start()
}

// openURL shows url in the system browser (never a webview).
func (a *App) openURL(url string) error {
	if a.app == nil {
		return fmt.Errorf("%s: app not started", service.CodeInternal)
	}
	return a.app.Browser.OpenURL(url)
}

func deviceName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "FlexTerm"
	}
	h = strings.TrimSuffix(h, ".local")
	if r := []rune(h); len(r) > 64 {
		h = string(r[:64])
	}
	return h
}

// storeBridge maps the connection and identity stores to vault records.
type storeBridge struct{ a *App }

func (b *storeBridge) Local() (records.Snapshot, error) {
	snap := records.Snapshot{}
	if b.a.connectionStore != nil {
		data, err := b.a.connectionStore.Load()
		if err != nil {
			return nil, err
		}
		for _, g := range data.Groups {
			if err := addRecord(snap, records.Group, g); err != nil {
				return nil, err
			}
		}
		for _, c := range data.Connections {
			c.SavePasswordOnSuccess = false
			if err := addRecord(snap, records.Connection, c); err != nil {
				return nil, err
			}
		}
	}
	if b.a.identityStore != nil {
		data, err := b.a.identityStore.Load()
		if err != nil {
			return nil, err
		}
		for _, id := range data.Identities {
			if err := addRecord(snap, records.Identity, id); err != nil {
				return nil, err
			}
		}
	}
	return snap, nil
}

// addRecord adds v's fields, leaving out values still encrypted with this
// device's local key (enc:v1: is useless on another device; it shows up
// when the local credential store is locked).
func addRecord(snap records.Snapshot, kind records.Kind, v any) error {
	id, f, err := records.ToFields(v)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	for name, raw := range f {
		var s string
		if json.Unmarshal(raw, &s) == nil && credentials.IsEncrypted(s) {
			delete(f, name)
		}
	}
	snap[records.Key{Kind: kind, ID: id}] = f
	return nil
}

// Apply writes the vault's content, keeping the local order of what was
// already there and appending new records by name.
func (b *storeBridge) Apply(snap records.Snapshot) error {
	if b.a.connectionStore != nil {
		cur, err := b.a.connectionStore.Load()
		if err != nil {
			return err
		}
		var data session.ConnectionStoreData
		data.Groups, err = merged(snap, records.Group, idsOf(cur.Groups, func(g session.ConnectionGroup) string { return g.ID }),
			func(g session.ConnectionGroup) string { return g.Name })
		if err != nil {
			return err
		}
		data.Connections, err = merged(snap, records.Connection, idsOf(cur.Connections, func(c session.ConnectionConfig) string { return c.ID }),
			func(c session.ConnectionConfig) string { return c.Name })
		if err != nil {
			return err
		}
		if err := b.a.connectionStore.Save(data); err != nil {
			return err
		}
		if fresh, err := b.a.connectionStore.Load(); err == nil {
			b.a.emit("store:connections:changed", fresh)
		}
	}
	if b.a.identityStore != nil {
		cur, err := b.a.identityStore.Load()
		if err != nil {
			return err
		}
		ids, err := merged(snap, records.Identity, idsOf(cur.Identities, func(i session.Identity) string { return i.ID }),
			func(i session.Identity) string { return i.Name })
		if err != nil {
			return err
		}
		if err := b.a.identityStore.Save(session.IdentityStoreData{Identities: ids}); err != nil {
			return err
		}
		if fresh, err := b.a.identityStore.Load(); err == nil {
			b.a.emit("store:identities:changed", fresh)
		}
	}
	return nil
}

func idsOf[T any](list []T, id func(T) string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = id(v)
	}
	return out
}

func merged[T any](snap records.Snapshot, kind records.Kind, order []string, name func(T) string) ([]T, error) {
	out := []T{}
	seen := map[string]bool{}
	for _, id := range order {
		f, ok := snap[records.Key{Kind: kind, ID: id}]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		var v T
		if err := records.FromFields(id, f, &v); err != nil {
			return nil, fmt.Errorf("vault: %s %s: %w", kind, id, err)
		}
		out = append(out, v)
	}
	var fresh []T
	for k, f := range snap {
		if k.Kind != kind || seen[k.ID] {
			continue
		}
		var v T
		if err := records.FromFields(k.ID, f, &v); err != nil {
			return nil, fmt.Errorf("vault: %s %s: %w", kind, k.ID, err)
		}
		fresh = append(fresh, v)
	}
	slices.SortStableFunc(fresh, func(a, b T) int { return strings.Compare(name(a), name(b)) })
	return append(out, fresh...), nil
}

// --- bindings (CONTRATO-cofre-f1.md §1) ---

func (a *App) vault() (*service.Service, error) {
	if a.vaultService == nil {
		return nil, fmt.Errorf("%s: vault service not initialized", service.CodeInternal)
	}
	return a.vaultService, nil
}

// VaultGetStatus returns the vault state the screens render.
func (a *App) VaultGetStatus() (service.Status, error) {
	v, err := a.vault()
	if err != nil {
		return service.Status{Phase: service.PhaseOff}, nil
	}
	return v.Status(), nil
}

// VaultSkip answers the welcome screen with "not now".
func (a *App) VaultSkip() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.Skip()
}

// VaultSignIn opens the system browser to sign in (result via vault:status).
func (a *App) VaultSignIn() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.SignIn()
}

func (a *App) VaultCancelSignIn() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.CancelSignIn()
}

// VaultRecoveryWords returns the 24 words (phase showRecovery only).
func (a *App) VaultRecoveryWords() ([]string, error) {
	v, err := a.vault()
	if err != nil {
		return nil, err
	}
	return v.RecoveryWords()
}

// VaultConfirmRecovery checks 3 of the words (0-based idx) and creates the vault.
func (a *App) VaultConfirmRecovery(idx []int, words []string) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.ConfirmRecovery(idx, words)
}

// VaultRegenerateRecovery (admin) starts replacing the recovery phrase:
// phase showRecovery with recoveryRegen; confirm with VaultConfirmRecovery.
func (a *App) VaultRegenerateRecovery() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.RegenerateRecovery()
}

// VaultCancelRecovery abandons a replacement started by
// VaultRegenerateRecovery (the old phrase keeps working).
func (a *App) VaultCancelRecovery() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.CancelRecovery()
}

func (a *App) VaultRecoverWithWords(words []string) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.RecoverWithWords(words)
}

func (a *App) VaultListDevices() ([]service.Device, error) {
	v, err := a.vault()
	if err != nil {
		return nil, err
	}
	return v.Devices()
}

func (a *App) VaultRevokeDevice(id string) (service.RevokeResult, error) {
	v, err := a.vault()
	if err != nil {
		return service.RevokeResult{}, err
	}
	return v.Revoke(id)
}

func (a *App) VaultRenameThisDevice(name string) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.Rename(name)
}

func (a *App) VaultSyncNow() (service.SyncResult, error) {
	v, err := a.vault()
	if err != nil {
		return service.SyncResult{}, err
	}
	return v.Sync()
}

func (a *App) VaultSignOut() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.SignOut()
}

func (a *App) VaultListJoinRequests() ([]service.JoinRequest, error) {
	v, err := a.vault()
	if err != nil {
		return nil, err
	}
	return v.JoinRequests()
}

func (a *App) VaultBeginApproval(requestID string) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.BeginApproval(requestID)
}

// VaultApproveJoin checks the 6-digit code and returns the 4-digit
// confirmation to show.
func (a *App) VaultApproveJoin(requestID, code6 string, role string) (string, error) {
	v, err := a.vault()
	if err != nil {
		return "", err
	}
	return v.ApproveJoin(requestID, code6, vault.Role(role))
}

func (a *App) VaultRejectJoin(requestID string) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.RejectJoin(requestID)
}

func (a *App) VaultCompleteJoin(code4 string) error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.CompleteJoin(code4)
}

func (a *App) VaultCancelJoin() error {
	v, err := a.vault()
	if err != nil {
		return err
	}
	return v.CancelJoin()
}

func (a *App) VaultChangedMap() (map[string]service.Change, error) {
	v, err := a.vault()
	if err != nil {
		return nil, err
	}
	return v.ChangedMap(), nil
}
