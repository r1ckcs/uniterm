// Package drive is a vault.Store on Google Drive (REST v3, drive.file
// scope). Drive has no create-if-absent and allows repeated names, which is
// what the vault's candidate model expects: Create always adds a file, and
// List returns every file under a name. Objects live flat in one folder the
// app creates; the vault name is kept in appProperties (n), with its first
// segment (d) to list one directory per request.
package drive

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

// FolderName is the folder the app creates in the user's Drive.
const FolderName = "FlexTerm Vault"

const folderMime = "application/vnd.google-apps.folder"

// Endpoints (variables for tests).
var (
	APIBase    = "https://www.googleapis.com/drive/v3"
	UploadBase = "https://www.googleapis.com/upload/drive/v3"
)

// maxObject bounds a download (vault objects are small).
const maxObject = 8 << 20

// Tokens hands out access tokens; Invalidate after a 401.
type Tokens interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

// Error is a Drive failure with a vault error code.
type Error struct {
	Code   string
	Status int
	Msg    string
	err    error
}

func (e *Error) Error() string     { return e.Msg }
func (e *Error) VaultCode() string { return e.Code }
func (e *Error) Unwrap() error     { return e.err }

// Store is a vault folder on Drive. Safe for concurrent use.
type Store struct {
	client *http.Client
	tokens Tokens
	// Timeout per request.
	Timeout time.Duration

	mu     sync.Mutex
	folder string
	// listings cached briefly per directory: one refresh lists the same
	// directory many times (one candidates() per roster version and wrap).
	cache map[string]cached
}

type cached struct {
	at      time.Time
	entries []vault.Entry
}

// listTTL is how long a directory listing is reused (a variable for tests).
var listTTL = 3 * time.Second

// New opens the store; folderID may be "" (found or created on first use).
func New(client *http.Client, tokens Tokens, folderID string) *Store {
	return &Store{client: client, tokens: tokens, folder: folderID, Timeout: 30 * time.Second, cache: map[string]cached{}}
}

// FolderID is the vault folder (to remember between runs).
func (s *Store) FolderID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.folder
}

// do sends a request with a fresh token, retrying once after a 401.
func (s *Store) do(build func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
		tok, err := s.tokens.Token(ctx)
		if err != nil {
			cancel()
			return nil, err
		}
		req, err := build()
		if err != nil {
			cancel()
			return nil, err
		}
		req = req.WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := s.client.Do(req)
		if err != nil {
			cancel()
			return nil, &Error{Code: "vault.drive.offline", Msg: "drive: " + err.Error(), err: vault.ErrUnavailable}
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			drain(resp)
			cancel()
			s.tokens.Invalidate()
			continue
		}
		if resp.StatusCode >= 300 {
			defer cancel()
			return nil, apiError(resp)
		}
		resp.Body = &cancelBody{resp.Body, cancel}
		return resp, nil
	}
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelBody) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
}

func apiError(resp *http.Response) error {
	defer drain(resp)
	var body struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	reason := ""
	if len(body.Error.Errors) > 0 {
		reason = body.Error.Errors[0].Reason
	}
	msg := fmt.Sprintf("drive: HTTP %d %s", resp.StatusCode, reason)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return &Error{Code: "vault.internal", Status: 404, Msg: msg, err: vault.ErrNotFound}
	case resp.StatusCode == http.StatusUnauthorized:
		return &Error{Code: "vault.drive.auth", Status: 401, Msg: msg}
	case reason == "storageQuotaExceeded":
		return &Error{Code: "vault.drive.quota", Status: resp.StatusCode, Msg: msg}
	case resp.StatusCode == 429 || resp.StatusCode >= 500 ||
		reason == "rateLimitExceeded" || reason == "userRateLimitExceeded":
		return &Error{Code: "vault.drive.quota", Status: resp.StatusCode, Msg: msg, err: vault.ErrUnavailable}
	}
	return &Error{Code: "vault.internal", Status: resp.StatusCode, Msg: msg}
}

type file struct {
	ID            string            `json:"id"`
	CreatedTime   string            `json:"createdTime"`
	AppProperties map[string]string `json:"appProperties"`
}

// query lists files matching q (all pages).
func (s *Store) query(q string) ([]file, error) {
	var out []file
	page := ""
	for {
		v := url.Values{
			"q":        {q},
			"fields":   {"nextPageToken,files(id,createdTime,appProperties)"},
			"pageSize": {"1000"},
			"spaces":   {"drive"},
		}
		if page != "" {
			v.Set("pageToken", page)
		}
		resp, err := s.do(func() (*http.Request, error) {
			return http.NewRequest(http.MethodGet, APIBase+"/files?"+v.Encode(), nil)
		})
		if err != nil {
			return nil, err
		}
		var r struct {
			NextPageToken string `json:"nextPageToken"`
			Files         []file `json:"files"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&r)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("drive: list: %w", err)
		}
		out = append(out, r.Files...)
		if r.NextPageToken == "" {
			return out, nil
		}
		page = r.NextPageToken
	}
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// ensureFolder finds the vault folder (the oldest the app created, if a
// race made several) or creates it.
func (s *Store) ensureFolder() (string, error) {
	s.mu.Lock()
	f := s.folder
	s.mu.Unlock()
	if f != "" {
		return f, nil
	}
	files, err := s.query("mimeType=" + quote(folderMime) + " and name=" + quote(FolderName) + " and trashed=false")
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		meta, _ := json.Marshal(map[string]any{"name": FolderName, "mimeType": folderMime})
		resp, err := s.do(func() (*http.Request, error) {
			req, err := http.NewRequest(http.MethodPost, APIBase+"/files?fields=id,createdTime", bytes.NewReader(meta))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
			}
			return req, err
		})
		if err != nil {
			return "", err
		}
		var created file
		err = json.NewDecoder(resp.Body).Decode(&created)
		resp.Body.Close()
		if err != nil {
			return "", err
		}
		// Re-list: if another device created one too, both pick the oldest.
		if files, err = s.query("mimeType=" + quote(folderMime) + " and name=" + quote(FolderName) + " and trashed=false"); err != nil {
			return "", err
		}
		if len(files) == 0 {
			files = []file{created}
		}
	}
	slices.SortFunc(files, func(a, b file) int {
		if c := strings.Compare(a.CreatedTime, b.CreatedTime); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	s.mu.Lock()
	s.folder = files[0].ID
	s.mu.Unlock()
	return files[0].ID, nil
}

func dirOf(name string) string {
	d, _, _ := strings.Cut(name, "/")
	return d
}

func (s *Store) List(prefix string) ([]vault.Entry, error) {
	folder, err := s.ensureFolder()
	if err != nil {
		return nil, err
	}
	// A prefix with a complete first segment lists one directory.
	dir := ""
	if strings.Contains(prefix, "/") {
		dir = dirOf(prefix)
	}
	s.mu.Lock()
	c, ok := s.cache[dir]
	s.mu.Unlock()
	if !ok || time.Since(c.at) > listTTL {
		q := quote(folder) + " in parents and trashed=false"
		if dir != "" {
			q += " and appProperties has { key='d' and value=" + quote(dir) + " }"
		}
		files, err := s.query(q)
		if err != nil {
			return nil, err
		}
		c = cached{at: time.Now()}
		for _, f := range files {
			name := f.AppProperties["n"]
			if !vault.ValidName(name) {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, f.CreatedTime)
			if err != nil {
				continue
			}
			c.entries = append(c.entries, vault.Entry{ID: f.ID, Name: name, Created: t.UnixNano()})
		}
		s.mu.Lock()
		s.cache[dir] = c
		s.mu.Unlock()
	}
	var out []vault.Entry
	for _, e := range c.entries {
		if strings.HasPrefix(e.Name, prefix) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b vault.Entry) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return int(min(max(a.Created-b.Created, -1), 1))
	})
	return out, nil
}

func (s *Store) invalidate(name string) {
	s.mu.Lock()
	delete(s.cache, dirOf(name))
	delete(s.cache, "")
	s.mu.Unlock()
}

func (s *Store) Get(id string) ([]byte, vault.Version, error) {
	resp, err := s.do(func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, APIBase+"/files/"+url.PathEscape(id)+"?alt=media", nil)
	})
	if err != nil {
		var de *Error
		if errors.As(err, &de) && de.Status == 404 {
			return nil, "", vault.ErrNotFound
		}
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxObject+1))
	if err != nil {
		return nil, "", &Error{Code: "vault.drive.offline", Msg: "drive: " + err.Error(), err: vault.ErrUnavailable}
	}
	if len(data) > maxObject {
		return nil, "", fmt.Errorf("drive: object %s too large", id)
	}
	return data, vault.Version(id), nil
}

func (s *Store) Create(name string, data []byte) (vault.Entry, error) {
	if !vault.ValidName(name) {
		return vault.Entry{}, vault.ErrBadName
	}
	folder, err := s.ensureFolder()
	if err != nil {
		return vault.Entry{}, err
	}
	// The visible title is random: Google sees no structure in the names
	// (the vault name is in appProperties, which only this app reads).
	title := make([]byte, 12)
	if _, err := rand.Read(title); err != nil {
		return vault.Entry{}, err
	}
	meta, _ := json.Marshal(map[string]any{
		"name":          hex.EncodeToString(title),
		"parents":       []string{folder},
		"mimeType":      "application/octet-stream",
		"appProperties": map[string]string{"n": name, "d": dirOf(name)},
	})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}}
	pw, _ := mw.CreatePart(h)
	pw.Write(meta)
	h = textproto.MIMEHeader{"Content-Type": {"application/octet-stream"}}
	pw, _ = mw.CreatePart(h)
	pw.Write(data)
	mw.Close()
	body := buf.Bytes()
	resp, err := s.do(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, UploadBase+"/files?uploadType=multipart&fields=id,createdTime", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
		}
		return req, err
	})
	if err != nil {
		return vault.Entry{}, err
	}
	defer resp.Body.Close()
	var f file
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return vault.Entry{}, fmt.Errorf("drive: create: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, f.CreatedTime)
	if err != nil {
		t = time.Now()
	}
	s.invalidate(name)
	return vault.Entry{ID: f.ID, Name: name, Created: t.UnixNano()}, nil
}

func (s *Store) Delete(id string) error {
	resp, err := s.do(func() (*http.Request, error) {
		return http.NewRequest(http.MethodDelete, APIBase+"/files/"+url.PathEscape(id), nil)
	})
	if err != nil {
		var de *Error
		if errors.As(err, &de) && de.Status == 404 {
			return nil
		}
		return err
	}
	drain(resp)
	s.mu.Lock()
	s.cache = map[string]cached{}
	s.mu.Unlock()
	return nil
}

// Email is the signed-in account (for the settings screen).
func (s *Store) Email() (string, error) {
	resp, err := s.do(func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, APIBase+"/about?fields=user(emailAddress)", nil)
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		User struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r); err != nil {
		return "", err
	}
	return r.User.EmailAddress, nil
}
