// Package oauth signs a desktop app in to Google for Drive access: the
// system browser (never a webview), a loopback redirect on 127.0.0.1 at a
// random port, PKCE S256 and a random state. The scope is drive.file only
// (the app sees just the files it created). The refresh token goes to the
// keychain through TokenStore; it never reaches a file or a log.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Scope is the only scope requested.
const Scope = "https://www.googleapis.com/auth/drive.file"

// Endpoints (variables for tests).
var (
	AuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL = "https://oauth2.googleapis.com/token"
)

// LoginTimeout is how long the loopback waits for the browser.
const LoginTimeout = 5 * time.Minute

// Error is a sign-in failure with a vault error code.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string     { return e.Msg }
func (e *Error) VaultCode() string { return e.Code }

var (
	ErrCancelled     = &Error{"vault.oauth.cancelled", "sign-in cancelled"}
	ErrDenied        = &Error{"vault.oauth.denied", "Google refused the sign-in"}
	ErrTimeout       = &Error{"vault.oauth.timeout", "no answer from the browser"}
	ErrNoClientID    = &Error{"vault.oauth.noClientId", "no Google OAuth client configured (google-oauth.json)"}
	ErrStateMismatch = &Error{"vault.oauth.stateMismatch", "sign-in answer with a wrong state"}
	// ErrRevoked: the refresh token no longer works (sign in again).
	ErrRevoked = &Error{"vault.drive.auth", "Google access was revoked or expired"}
)

// Config is the OAuth client of the app ("Desktop app" type). For desktop
// clients Google issues a client secret that is not confidential; PKCE is
// what protects the code.
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Build-time defaults (-ldflags "-X .../oauth.DefaultClientID=...").
var DefaultClientID, DefaultClientSecret string

// ConfigFileName is read from the app's default data dir (the caller passes
// it, from the bootstrap): either Google's downloaded file
// ({"installed": {...}}) or the flat form. Never in the repository.
const ConfigFileName = "google-oauth.json"

// LoadConfig finds the client: the config file in dir, then the ldflags
// defaults.
func LoadConfig(dir string) (Config, error) {
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, ConfigFileName)); err == nil {
			var wrapped struct {
				Installed *Config `json:"installed"`
				Config
			}
			if err := json.Unmarshal(data, &wrapped); err != nil {
				return Config{}, fmt.Errorf("%s: %w", ConfigFileName, err)
			}
			c := wrapped.Config
			if wrapped.Installed != nil {
				c = *wrapped.Installed
			}
			if c.ClientID != "" {
				return c, nil
			}
		}
	}
	if DefaultClientID != "" {
		return Config{ClientID: DefaultClientID, ClientSecret: DefaultClientSecret}, nil
	}
	return Config{}, ErrNoClientID
}

// Token is what a sign-in yields.
type Token struct {
	Access  string
	Refresh string
	Expiry  time.Time
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login runs the browser sign-in. open shows a URL in the system browser.
func Login(ctx context.Context, c Config, client *http.Client, open func(string) error) (Token, error) {
	if c.ClientID == "" {
		return Token{}, ErrNoClientID
	}
	state, err := randomString()
	if err != nil {
		return Token{}, err
	}
	verifier, err := randomString()
	if err != nil {
		return Token{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// Loopback on the IPv4 literal only: never 0.0.0.0, never "localhost"
	// (which may resolve elsewhere).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Token{}, err
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	var once sync.Once
	finish := func(r result) { once.Do(func() { done <- r }) }
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") == "" && q.Get("code") == "" && q.Get("error") == "" {
			http.NotFound(w, r) // favicon, probes
			return
		}
		got := q.Get("state")
		if subtle.ConstantTimeCompare([]byte(got), []byte(state)) != 1 {
			page(w, "Falha no login", "Resposta inválida. Volte ao FlexTerm e tente de novo.")
			finish(result{err: ErrStateMismatch})
			return
		}
		if e := q.Get("error"); e != "" {
			page(w, "Login cancelado", "Volte ao FlexTerm.")
			if e == "access_denied" {
				finish(result{err: ErrCancelled})
			} else {
				finish(result{err: &Error{ErrDenied.Code, "Google: " + e}})
			}
			return
		}
		page(w, "Pronto", "Login concluído. Pode fechar esta aba e voltar ao FlexTerm.")
		finish(result{code: q.Get("code")})
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	v := url.Values{
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {Scope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	if err := open(AuthURL + "?" + v.Encode()); err != nil {
		return Token{}, err
	}

	timer := time.NewTimer(LoginTimeout)
	defer timer.Stop()
	var res result
	select {
	case <-ctx.Done():
		return Token{}, ErrCancelled
	case <-timer.C:
		return Token{}, ErrTimeout
	case res = <-done:
	}
	if res.err != nil {
		return Token{}, res.err
	}
	return exchange(ctx, client, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirect},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}, "")
}

func page(w http.ResponseWriter, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>FlexTerm</title><body style="font-family:system-ui;margin:3em"><h2>%s</h2><p>%s</p>`,
		html.EscapeString(title), html.EscapeString(msg))
}

// Refresh gets a new access token from the refresh token.
func Refresh(ctx context.Context, c Config, client *http.Client, refresh string) (Token, error) {
	return exchange(ctx, client, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}, refresh)
}

func exchange(ctx context.Context, client *http.Client, form url.Values, refresh string) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, &Error{"vault.drive.offline", "token endpoint: " + err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, err
	}
	var tr tokenResponse
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		if tr.Error == "invalid_grant" {
			return Token{}, ErrRevoked
		}
		// Never echo the body: it may carry tokens.
		return Token{}, &Error{ErrDenied.Code, fmt.Sprintf("token endpoint: HTTP %d %s", resp.StatusCode, tr.Error)}
	}
	t := Token{Access: tr.AccessToken, Refresh: tr.RefreshToken, Expiry: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)}
	if t.Refresh == "" {
		t.Refresh = refresh
	}
	if t.Refresh == "" {
		return Token{}, errors.New("oauth: Google returned no refresh token")
	}
	return t, nil
}

// TokenSource hands out a valid access token, refreshing it when needed.
type TokenSource struct {
	Config  Config
	Client  *http.Client
	Refresh string
	mu      sync.Mutex
	tok     Token
}

// Token returns an access token valid for at least a minute.
func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.tok.Access != "" && time.Until(ts.tok.Expiry) > time.Minute {
		return ts.tok.Access, nil
	}
	t, err := Refresh(ctx, ts.Config, ts.Client, ts.Refresh)
	if err != nil {
		return "", err
	}
	ts.tok = t
	return t.Access, nil
}

// Invalidate drops the cached access token (after a 401).
func (ts *TokenSource) Invalidate() {
	ts.mu.Lock()
	ts.tok = Token{}
	ts.mu.Unlock()
}
