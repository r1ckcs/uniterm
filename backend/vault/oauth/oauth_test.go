package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeGoogle checks the PKCE pair at the token endpoint.
func fakeGoogle(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var challenge string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "the-code" ||
				!strings.HasPrefix(r.Form.Get("redirect_uri"), "http://127.0.0.1:") {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at2", "expires_in": 3600})
		}
	}))
	t.Cleanup(srv.Close)
	old := TokenURL
	TokenURL = srv.URL
	t.Cleanup(func() { TokenURL = old })
	return srv, &challenge
}

// browser simulates the user approving: it follows the auth URL's
// redirect_uri with code and state (tamper rewrites the query).
func browser(t *testing.T, challenge *string, tamper func(url.Values)) func(string) error {
	return func(auth string) error {
		u, err := url.Parse(auth)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("scope") != Scope || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" {
			t.Errorf("auth URL = %s", auth)
		}
		*challenge = q.Get("code_challenge")
		back := url.Values{"code": {"the-code"}, "state": {q.Get("state")}}
		if tamper != nil {
			tamper(back)
		}
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?" + back.Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginPKCE(t *testing.T) {
	srv, ch := fakeGoogle(t)
	tok, err := Login(context.Background(), Config{ClientID: "id"}, srv.Client(), browser(t, ch, nil))
	if err != nil || tok.Access != "at" || tok.Refresh != "rt" {
		t.Fatalf("login = %+v %v", tok, err)
	}
	ts := &TokenSource{Config: Config{ClientID: "id"}, Client: srv.Client(), Refresh: "rt"}
	if a, err := ts.Token(context.Background()); err != nil || a != "at2" {
		t.Fatalf("refresh = %q %v", a, err)
	}
	ts.Refresh = "revoked"
	ts.Invalidate()
	if _, err := ts.Token(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked refresh = %v", err)
	}
}

func TestLoginWrongState(t *testing.T) {
	srv, ch := fakeGoogle(t)
	_, err := Login(context.Background(), Config{ClientID: "id"}, srv.Client(),
		browser(t, ch, func(v url.Values) { v.Set("state", "forged") }))
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("forged state = %v", err)
	}
}

func TestLoginDeniedAndCancelled(t *testing.T) {
	srv, ch := fakeGoogle(t)
	_, err := Login(context.Background(), Config{ClientID: "id"}, srv.Client(),
		browser(t, ch, func(v url.Values) { v.Del("code"); v.Set("error", "access_denied") }))
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("access_denied = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err = Login(ctx, Config{ClientID: "id"}, srv.Client(), func(string) error { return nil })
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancel = %v", err)
	}
	if _, err := Login(ctx, Config{}, srv.Client(), nil); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("no client = %v", err)
	}
}

func TestLoopbackOnlyOnIPv4Literal(t *testing.T) {
	srv, ch := fakeGoogle(t)
	var redirect string
	open := browser(t, ch, nil)
	_, err := Login(context.Background(), Config{ClientID: "id"}, srv.Client(), func(auth string) error {
		u, _ := url.Parse(auth)
		redirect = u.Query().Get("redirect_uri")
		return open(auth)
	})
	if err != nil || !strings.HasPrefix(redirect, "http://127.0.0.1:") {
		t.Fatalf("redirect = %q %v", redirect, err)
	}
}

func TestLoadConfigForms(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := writeFile(dir+"/"+ConfigFileName, s); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"installed":{"client_id":"a.apps","client_secret":"s"}}`)
	if c, err := LoadConfig(dir); err != nil || c.ClientID != "a.apps" || c.ClientSecret != "s" {
		t.Fatalf("installed form = %+v %v", c, err)
	}
	write(`{"client_id":"b"}`)
	if c, _ := LoadConfig(dir); c.ClientID != "b" {
		t.Fatalf("flat form = %+v", c)
	}
	if _, err := LoadConfig(t.TempDir()); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("missing = %v", err)
	}
}
