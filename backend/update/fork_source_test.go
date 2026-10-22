package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingTransport answers every request with 404 and remembers the URLs.
type recordingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{"message":"Not Found"}`)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

// isolate points the cache at a temp dir and swaps the HTTP clients.
func isolate(t *testing.T, rt http.RoundTripper) string {
	t.Helper()
	dir := t.TempDir()
	prevDir, prevCache := configDir, userCacheDir
	configDir = func() (string, error) { return dir, nil }
	userCacheDir = func() (string, error) { return dir, nil }
	sharedHTTPClient()
	downloadHTTPClient()
	prevShared, prevDownload := sharedClient, downloadClient
	if rt != nil {
		sharedClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}
		downloadClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}
	}
	prevKey := releasePublicKey
	t.Cleanup(func() {
		configDir, userCacheDir = prevDir, prevCache
		sharedClient, downloadClient = prevShared, prevDownload
		releasePublicKey = prevKey
	})
	return dir
}

func TestSourceURLsPointOnlyAtFork(t *testing.T) {
	t.Setenv("UNITERM_UPDATE_API_BASE", "")
	for _, src := range []string{"", "auto", "github", "gitee", "anything"} {
		for _, u := range []string{sourceAPIURL(src), sourceReleaseURL(src)} {
			if !strings.Contains(u, "r1ckcs/uniterm") {
				t.Errorf("source %q: %s does not point at the fork", src, u)
			}
			if strings.Contains(u, "ys-ll") || strings.Contains(u, "gitee") || strings.Contains(u, "ys-l/") {
				t.Errorf("source %q: %s points at upstream", src, u)
			}
		}
	}
}

// Check must never contact ys-ll/uniterm or Gitee, whatever source the
// (possibly legacy) settings ask for, and a fork without releases is simply
// "no update" — no fallback anywhere.
func TestCheckNeverContactsUpstream(t *testing.T) {
	t.Setenv("UNITERM_UPDATE_API_BASE", "")
	rt := &recordingTransport{}
	isolate(t, rt)

	for _, src := range []string{"github", "gitee", "auto", ""} {
		info, err := Check("1.9.5+security", src)
		if err != nil {
			t.Fatalf("source %q: no release must not be an error, got %v", src, err)
		}
		if info.HasUpdate || len(info.Assets) != 0 || info.Latest != "" {
			t.Fatalf("source %q: want no update, got %+v", src, info)
		}
	}
	if len(rt.urls) == 0 {
		t.Fatal("expected at least one request to the fork")
	}
	for _, u := range rt.urls {
		if u != "https://api.github.com/repos/r1ckcs/uniterm/releases/latest" {
			t.Errorf("unexpected request %s", u)
		}
	}
}

// A cache written by the upstream build (e.g. the v1.10.1 the installed app
// already saw) must be ignored and removed, never offered.
func TestUpstreamCacheIsIgnoredAndRemoved(t *testing.T) {
	t.Setenv("UNITERM_UPDATE_API_BASE", "")
	dir := isolate(t, &recordingTransport{})
	stale := cacheEntry{
		Release: release{TagName: "v1.10.1", Assets: []releaseAsset{{
			Name:               "uniterm-" + runtime.GOOS + "-" + runtime.GOARCH + "-v1.10.1.zip",
			BrowserDownloadURL: "https://github.com/ys-ll/uniterm/releases/download/v1.10.1/x.zip",
		}}},
		Source:    "github",
		Timestamp: time.Now(),
	}
	data, _ := json.Marshal(stale)
	if err := os.MkdirAll(filepath.Join(dir, "uniTerm"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(legacyCacheFiles, filepath.Base(cachePath())) {
		// The last one simulates a same-named file without the repo pin.
		if err := os.WriteFile(filepath.Join(dir, "uniTerm", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	info, err := Check("1.9.5+security", "gitee")
	if err != nil {
		t.Fatal(err)
	}
	if info.HasUpdate || info.Latest != "" {
		t.Fatalf("stale upstream cache was served: %+v", info)
	}
	for _, name := range legacyCacheFiles {
		if _, err := os.Stat(filepath.Join(dir, "uniTerm", name)); !os.IsNotExist(err) {
			t.Errorf("legacy cache %s was not removed", name)
		}
	}
}

// fakeRelease serves a fork-shaped release over loopback. sig selects what is
// published as checksums.txt.sig: "" omits it.
func fakeRelease(t *testing.T, sig func(sums []byte) []byte) (payloadName string) {
	t.Helper()
	payloadName = "uniterm-" + runtime.GOOS + "-" + runtime.GOARCH + "-v99.0.0.tar.gz"
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		payloadName = "uniterm-" + runtime.GOOS + "-" + runtime.GOARCH + "-portable-v99.0.0.zip"
	}
	payload := []byte("not really a binary")
	h := sha256.Sum256(payload)
	sums := []byte(hex.EncodeToString(h[:]) + "  " + payloadName + "\n")

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	assets := []releaseAsset{
		{Name: payloadName, BrowserDownloadURL: srv.URL + "/dl/" + payloadName},
		{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/dl/checksums.txt"},
	}
	var sigBytes []byte
	if sig != nil {
		sigBytes = sig(sums)
		assets = append(assets, releaseAsset{Name: "checksums.txt.sig", BrowserDownloadURL: srv.URL + "/dl/checksums.txt.sig"})
	}
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(release{TagName: "v99.0.0", Assets: assets})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/dl/") {
		case payloadName:
			w.Write(payload)
		case "checksums.txt":
			w.Write(sums)
		case "checksums.txt.sig":
			w.Write(sigBytes)
		default:
			http.NotFound(w, r)
		}
	})
	t.Setenv("UNITERM_UPDATE_API_BASE", srv.URL)
	return payloadName
}

func TestUnverifiedReleaseIsNeverInstallable(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	cases := []struct {
		name    string
		key     string
		sig     func([]byte) []byte
		wantAny bool
	}{
		{"no key compiled in, signed release", "", func(b []byte) []byte { return ed25519.Sign(priv, b) }, false},
		{"key set, release without signature", pubB64, nil, false},
		{"key set, signature by another key", pubB64, func(b []byte) []byte { return ed25519.Sign(otherPriv, b) }, false},
		{"key set, garbage signature", pubB64, func([]byte) []byte { return []byte("not a signature") }, false},
		{"key set, valid raw signature", pubB64, func(b []byte) []byte { return ed25519.Sign(priv, b) }, true},
		{"key set, valid base64 signature", pubB64, func(b []byte) []byte {
			return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b)) + "\n")
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t, nil)
			name := fakeRelease(t, c.sig)
			releasePublicKey = c.key

			info, err := Check("1.9.5+security", "github")
			if err != nil {
				t.Fatal(err)
			}
			if !info.HasUpdate {
				t.Fatalf("v99 should be reported as an update: %+v", info)
			}
			if got := len(info.Assets) > 0; got != c.wantAny {
				t.Fatalf("installable assets = %v, want %v (%+v)", got, c.wantAny, info.Assets)
			}
			if !c.wantAny {
				// Even an asset forged by the caller (e.g. the webview) with
				// no signed hash must be refused before any download.
				m := NewManager()
				_, err := m.Download([]UpdateAsset{{Name: name, URL: fmt.Sprintf("%s/dl/%s", os.Getenv("UNITERM_UPDATE_API_BASE"), name)}}, nil)
				if err == nil || m.Pending() != nil {
					t.Fatalf("download without signed checksum must fail, err=%v", err)
				}
				return
			}
			m := NewManager()
			if _, err := m.Download(info.Assets, nil); err != nil && !strings.Contains(err.Error(), "extract") {
				t.Fatalf("verified asset should pass download+hash, got %v", err)
			}
		})
	}
}

func TestDownloadRefusesOffRepoURL(t *testing.T) {
	t.Setenv("UNITERM_UPDATE_API_BASE", "")
	rt := &recordingTransport{}
	isolate(t, rt)
	m := NewManager()
	_, err := m.Download([]UpdateAsset{{
		Name:   "uniterm-linux-amd64-v1.10.1.tar.gz",
		URL:    "https://github.com/ys-ll/uniterm/releases/download/v1.10.1/uniterm-linux-amd64-v1.10.1.tar.gz",
		SHA256: strings.Repeat("a", 64),
	}}, nil)
	if err == nil {
		t.Fatal("upstream asset URL must be refused")
	}
	if len(rt.urls) != 0 {
		t.Fatalf("refused asset must not be fetched, got %v", rt.urls)
	}
}

// A plain build (no -ldflags key, as `go test` and dev builds are) must ship
// with no release key, so even a correctly signed release is not installable.
func TestDefaultBuildHasNoKeyAndRefusesSignedRelease(t *testing.T) {
	if got := EmbeddedReleasePublicKey(); got != "" {
		t.Fatalf("default build embeds a release key %q", got)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	isolate(t, nil)
	fakeRelease(t, func(b []byte) []byte { return ed25519.Sign(priv, b) })
	info, err := Check("1.9.5+security", "github")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Assets) != 0 {
		t.Fatalf("no key compiled in: nothing may be installable, got %+v", info.Assets)
	}
	if VerifyChecksumsSignature("", []byte("x"), ed25519.Sign(priv, []byte("x"))) {
		t.Fatal("an empty key must never verify")
	}
}
