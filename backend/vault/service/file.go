package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ys-ll/uniterm/backend/vault"
)

// StoreEnv selects a local folder instead of Google Drive:
// UNITERM_VAULT_STORE=file:/absolute/path (testing without a Google project).
const StoreEnv = "UNITERM_VAULT_STORE"

// FileBackendFromEnv returns the test backend when StoreEnv is set.
func FileBackendFromEnv(dir string) (*FileBackend, bool, error) {
	spec := os.Getenv(StoreEnv)
	if spec == "" {
		return nil, false, nil
	}
	p, ok := strings.CutPrefix(spec, "file:")
	if !ok || !filepath.IsAbs(p) {
		return nil, true, fmt.Errorf("%s must be file:/absolute/path, got %q", StoreEnv, spec)
	}
	return &FileBackend{Path: filepath.Clean(p), marker: filepath.Join(dir, "file-signed-in")}, true, nil
}

// FileBackend is the test backend: the vault lives in a local folder (two
// app instances with their own UNITERM_DATA_DIR share it like two devices
// share a Drive folder). Signing in needs no browser.
type FileBackend struct {
	Path   string
	marker string
}

func (f *FileBackend) TestMode() bool { return true }

func (f *FileBackend) account() string { return "file:" + f.Path }

func (f *FileBackend) SignIn(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.WriteFile(f.marker, []byte(f.Path), 0o600); err != nil {
		return "", err
	}
	return f.account(), nil
}

func (f *FileBackend) Resume() (string, bool) {
	data, err := os.ReadFile(f.marker)
	if err != nil || string(data) != f.Path {
		return "", false
	}
	return f.account(), true
}

func (f *FileBackend) Store() (vault.Store, error) { return vault.NewFileStore(f.Path) }

func (f *FileBackend) SignOut() error {
	if err := os.Remove(f.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// FileSecrets keeps secrets in a 0600 JSON file (test mode only: the real
// app uses the keychain). Two test "devices" on one Mac must not share the
// keychain entry of the device key.
type FileSecrets struct {
	Path string
	mu   sync.Mutex
}

func (f *FileSecrets) load() (map[string]string, error) {
	m := map[string]string{}
	return m, readJSON(f.Path, &m)
}

func (f *FileSecrets) Get(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := m[key]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}

func (f *FileSecrets) Set(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key] = value
	return writeJSON(f.Path, m)
}

func (f *FileSecrets) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	delete(m, key)
	return writeJSON(f.Path, m)
}
