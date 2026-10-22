package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	"github.com/ys-ll/uniterm/backend/store"
	"github.com/ys-ll/uniterm/backend/sync"
	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/drive"
	"github.com/ys-ll/uniterm/backend/vault/oauth"
)

// driveBackend signs in to Google (system browser) and keeps the vault in
// the app's Drive folder. The refresh token lives only in the keychain.
type driveBackend struct {
	dir    string // this data dir's vault files (drive folder id)
	kc     *sync.Keychain
	key    string // keychain entry of the refresh token
	open   func(string) error
	client *http.Client
	mu     gosync.Mutex
	store  *drive.Store
}

func newDriveBackend(dataDir string, kc *sync.Keychain, ns string, open func(string) error) *driveBackend {
	return &driveBackend{
		dir: filepath.Join(dataDir, "vault"), kc: kc, key: "vault/" + ns + "/google-refresh",
		open: open, client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (d *driveBackend) TestMode() bool { return false }

// config reads google-oauth.json from the default data dir (the bootstrap's
// answer, never a fixed path), then the ldflags defaults.
func (d *driveBackend) config() (oauth.Config, error) {
	dir, _ := store.DefaultDataDir()
	return oauth.LoadConfig(dir)
}

func (d *driveBackend) SignIn(ctx context.Context) (string, error) {
	cfg, err := d.config()
	if err != nil {
		return "", err
	}
	tok, err := oauth.Login(ctx, cfg, d.client, d.open)
	if err != nil {
		return "", err
	}
	if err := d.kc.Set(d.key, tok.Refresh); err != nil {
		return "", err
	}
	st := d.newStore(cfg, tok.Refresh)
	email, err := st.Email()
	if err != nil {
		return "", err
	}
	return email, nil
}

func (d *driveBackend) Resume() (string, bool) {
	rt, err := d.kc.Get(d.key)
	return "", err == nil && rt != ""
}

func (d *driveBackend) newStore(cfg oauth.Config, refresh string) *drive.Store {
	d.mu.Lock()
	defer d.mu.Unlock()
	folder, _ := os.ReadFile(filepath.Join(d.dir, "drive-folder"))
	d.store = drive.New(d.client, &oauth.TokenSource{Config: cfg, Client: d.client, Refresh: refresh}, strings.TrimSpace(string(folder)))
	return d.store
}

func (d *driveBackend) Store() (vault.Store, error) {
	cfg, err := d.config()
	if err != nil {
		return nil, err
	}
	rt, err := d.kc.Get(d.key)
	if err != nil || rt == "" {
		return nil, oauth.ErrRevoked
	}
	st := d.newStore(cfg, rt)
	return &folderRemember{Store: st, path: filepath.Join(d.dir, "drive-folder")}, nil
}

// SignOut forgets the refresh token here and asks Google to revoke it.
func (d *driveBackend) SignOut() error {
	rt, _ := d.kc.Get(d.key)
	if err := d.kc.Delete(d.key); err != nil && rt != "" {
		return err
	}
	if rt != "" {
		go func() {
			req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/revoke",
				strings.NewReader(url.Values{"token": {rt}}.Encode()))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if resp, err := d.client.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
	}
	return nil
}

// folderRemember saves the Drive folder id once known, so later runs don't
// search for it (and never pick a look-alike folder created later).
type folderRemember struct {
	*drive.Store
	path string
	once gosync.Once
}

func (f *folderRemember) List(prefix string) ([]vault.Entry, error) {
	es, err := f.Store.List(prefix)
	if id := f.FolderID(); err == nil && id != "" {
		f.once.Do(func() {
			_ = os.MkdirAll(filepath.Dir(f.path), 0o700)
			_ = os.WriteFile(f.path, []byte(id), 0o600)
		})
	}
	return es, err
}
