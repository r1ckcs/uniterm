package main

import (
	"net/http"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// contentSecurityPolicy is sent with every asset the WebView loads (see
// cspMiddleware). The page renders text from SSH servers, other vault
// devices and AI models; if any of it ever slips past escaping, this keeps
// it from running script. Why each source is allowed:
//
//   - script-src 'self': only the bundle. No inline script (index.html
//     loads boot.js) and no 'unsafe-eval'. 'wasm-unsafe-eval' lets
//     WebAssembly compile (the xterm image addon's sixel decoder); it does
//     not allow eval of JavaScript.
//   - style-src 'unsafe-inline': xterm, CodeMirror and Element Plus insert
//     <style> elements and style attributes at run time, and Vue binds
//     :style (the --conn-* tokens, theme colors). Inline CSS can't run
//     script; the values that reach it are validated (session.NormalizeColor).
//   - img-src data: blob:: VNC/SPICE snapshots and xterm inline images.
//     No remote images (AI markdown renders image links as links).
//   - font-src data:: fonts the bundler inlines.
//   - connect-src ws:: the VNC proxy (ws://127.0.0.1:<port>, also Vite's
//     HMR in `wails3 dev`) and SPICE, which today connects straight to the
//     host's websocket port (backend/session/spice_session.go). The Wails
//     runtime itself is 'self'.
//   - object-src, base-uri, form-action, frame-ancestors 'none'; frames
//     fall back to default-src 'self' (none are used).
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' ws:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// cspMiddleware adds the Content-Security-Policy header to every asset
// server response (the Wails v3 asset server takes no CSP option; a header
// is used instead of a <meta> because meta can't carry frame-ancestors).
func cspMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

var _ application.Middleware = cspMiddleware

// cspDirective returns the sources of one directive of policy ("" if absent).
func cspDirective(policy, name string) (string, bool) {
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if n, v, _ := strings.Cut(d, " "); n == name {
			return v, true
		}
	}
	return "", false
}
