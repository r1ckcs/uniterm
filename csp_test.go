package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The CSP must stay on: these fail if the header, a key directive or the
// wiring in main.go is removed, or if index.html gains an inline script.

func TestCSPMiddlewareSetsHeader(t *testing.T) {
	h := cspMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html></html>"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
}

func TestCSPPolicy(t *testing.T) {
	want := map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self' 'wasm-unsafe-eval'",
		"object-src":      "'none'",
		"base-uri":        "'none'",
		"form-action":     "'none'",
		"frame-ancestors": "'none'",
	}
	for name, v := range want {
		got, ok := cspDirective(contentSecurityPolicy, name)
		if !ok || got != v {
			t.Errorf("%s = %q (present %v), want %q", name, got, ok, v)
		}
	}
	for _, name := range []string{"script-src", "default-src", "connect-src", "img-src"} {
		v, _ := cspDirective(contentSecurityPolicy, name)
		for _, bad := range []string{"'unsafe-eval'", "'unsafe-inline'", "*", "http:", "https:"} {
			for _, src := range strings.Fields(v) {
				if src == bad {
					t.Errorf("%s allows %s", name, bad)
				}
			}
		}
	}
}

func TestCSPWiredIntoAssetServer(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`AssetOptions\{[^}]*Middleware:\s*cspMiddleware`).Match(src) {
		t.Fatal("main.go: the asset server no longer uses cspMiddleware")
	}
}

func TestIndexHTMLHasNoInlineScript(t *testing.T) {
	src, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range regexp.MustCompile(`(?is)<script\b[^>]*>`).FindAll(src, -1) {
		if !regexp.MustCompile(`(?i)\bsrc\s*=`).Match(tag) {
			t.Errorf("inline script in index.html (blocked by script-src 'self'): %s", tag)
		}
	}
	if regexp.MustCompile(`(?i)\son[a-z]+\s*=`).Match(src) {
		t.Error("inline event handler in index.html")
	}
}
