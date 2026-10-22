package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/appname"
	"github.com/ys-ll/uniterm/backend/log"
)

// release mirrors the GitHub release payload subset: tag_name, body and an
// assets list of {name, browser_download_url}.
type release struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type cacheEntry struct {
	Release release `json:"release"`
	Source  string  `json:"source"`
	// Repo pins the entry to the repository it was fetched from, so a cache
	// written by an upstream build (ys-ll/uniterm) is never served here.
	Repo      string    `json:"repo"`
	Timestamp time.Time `json:"timestamp"`
	// ETag captured from the API response so a follow-up call inside the
	// disk-cache TTL window can send If-None-Match and let the host answer
	// 304 Not Modified, skipping the body decode entirely (F-409).
	ETag string `json:"etag,omitempty"`
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// parseSemver splits a version string into its numeric core parts and its
// optional pre-release identifiers, following SemVer 2.0.0. Build metadata
// (anything after '+') is discarded because it does not affect precedence.
func parseSemver(v string) (core []int, pre []string) {
	v = normalizeVersion(v)
	if idx := strings.Index(v, "+"); idx >= 0 {
		v = v[:idx]
	}
	var prePart string
	if idx := strings.Index(v, "-"); idx >= 0 {
		prePart = v[idx+1:]
		v = v[:idx]
	}
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(p)
		core = append(core, n)
	}
	if prePart != "" {
		pre = strings.Split(prePart, ".")
	}
	return core, pre
}

// comparePre compares two pre-release identifier slices per SemVer 2.0.0 §11.4.
// Returns -1, 0, or 1. A version WITHOUT a pre-release has higher precedence
// than one WITH a pre-release; callers must handle that case before calling.
func comparePre(a, b []string) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		if i >= len(a) {
			return -1
		}
		if i >= len(b) {
			return 1
		}
		an, aErr := strconv.Atoi(a[i])
		bn, bErr := strconv.Atoi(b[i])
		aNumeric := aErr == nil
		bNumeric := bErr == nil
		switch {
		case aNumeric && bNumeric:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aNumeric && !bNumeric:
			return -1
		case !aNumeric && bNumeric:
			return 1
		default:
			if a[i] != b[i] {
				if a[i] < b[i] {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

func versionGreater(latest, current string) bool {
	lc, lp := parseSemver(latest)
	cc, cp := parseSemver(current)
	for i := 0; i < len(lc) || i < len(cc); i++ {
		var ln, cn int
		if i < len(lc) {
			ln = lc[i]
		}
		if i < len(cc) {
			cn = cc[i]
		}
		if ln > cn {
			return true
		}
		if ln < cn {
			return false
		}
	}
	// Core versions equal: a version without a pre-release outranks one with.
	if len(lp) == 0 && len(cp) == 0 {
		return false
	}
	if len(lp) == 0 {
		return true
	}
	if len(cp) == 0 {
		return false
	}
	return comparePre(lp, cp) > 0
}

func shouldUpdate(current, latest string) bool {
	if current == "dev" {
		return true
	}
	return versionGreater(latest, current)
}

const cacheTTL = 5 * time.Minute

// updateRepo is the only repository this fork checks for updates. The
// upstream sources (github.com/ys-ll/uniterm and its gitee.com/ys-l mirror)
// were removed on purpose: an upstream release would replace the binary and
// silently undo every fix listed in SECURITY-FIXES.md.
const updateRepo = "r1ckcs/uniterm"

// SourceGitHub is the single update source. Any other value (including the
// legacy "gitee" and "auto" settings) is normalized to it.
const SourceGitHub = "github"

// releasePublicKey is the base64 ed25519 public key that must sign
// checksums.txt (as checksums.txt.sig) before any asset is offered for
// install. It is empty until the fork publishes signed releases; release
// builds inject it with
//
//	-ldflags "-X github.com/ys-ll/uniterm/backend/update.releasePublicKey=<base64>"
//
// With no key compiled in, updates can still be detected (and the release
// page opened) but nothing is ever downloaded or installed.
var releasePublicKey = ""

// legacyCacheFiles are the upstream-era caches (keyed by source only). They
// may hold an upstream release (e.g. v1.10.1) and are removed on first use.
var legacyCacheFiles = []string{"update_cache_github.json", "update_cache_gitee.json"}

// sharedClient is a package-level *http.Client with keep-alive enabled.
// Reusing it avoids the per-call TCP+TLS handshake to api.github.com on
// every Check() (F-409).
var (
	sharedClientOnce sync.Once
	sharedClient     *http.Client
)

func sharedHTTPClient() *http.Client {
	sharedClientOnce.Do(func() {
		sharedClient = &http.Client{Timeout: 10 * time.Second}
	})
	return sharedClient
}

// configDir is os.UserConfigDir, swappable in tests.
var configDir = os.UserConfigDir

func normalizeSource(string) string { return SourceGitHub }

func cachePath() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, appname.DirName, "update_cache_"+strings.ReplaceAll(updateRepo, "/", "-")+".json")
}

// removeLegacyCaches deletes the upstream-era cache files (the first FlexTerm
// start copies them over from the uniTerm folder along with everything else).
func removeLegacyCaches() {
	dir, err := configDir()
	if err != nil {
		return
	}
	for _, name := range legacyCacheFiles {
		_ = os.Remove(filepath.Join(dir, appname.DirName, name))
	}
}

func loadCache() *cacheEntry {
	path := cachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if entry.Repo != updateRepo || time.Since(entry.Timestamp) > cacheTTL {
		return nil
	}
	return &entry
}

func saveCache(entry *cacheEntry) {
	path := cachePath()
	if path == "" {
		return
	}
	entry.Repo = updateRepo
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.Marshal(entry)
	_ = os.WriteFile(path, data, 0600)
}

// apiBaseOverride returns UNITERM_UPDATE_API_BASE, which replaces the API
// base for the local end-to-end autotest (autotest_update.go); production
// builds leave it unset. The base must serve a GitHub-style
// GET <base>/releases/latest payload.
func apiBaseOverride() string {
	return strings.TrimRight(os.Getenv("UNITERM_UPDATE_API_BASE"), "/")
}

// sourceAPIURL returns the latest-release API endpoint of the fork.
func sourceAPIURL(string) string {
	if base := apiBaseOverride(); base != "" {
		return base + "/releases/latest"
	}
	return "https://api.github.com/repos/" + updateRepo + "/releases/latest"
}

// sourceReleaseURL returns the human-facing release page of the fork.
func sourceReleaseURL(string) string {
	return "https://github.com/" + updateRepo + "/releases/latest"
}

// assetURLAllowed accepts only download URLs under the fork's own GitHub
// release path (plus the loopback autotest server when the API base is
// overridden). GitHub redirects these to its asset CDN over https.
func assetURLAllowed(raw string) bool {
	if strings.HasPrefix(raw, "https://github.com/"+updateRepo+"/releases/download/") {
		return true
	}
	if apiBaseOverride() == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && allowInsecureUpdateURL(u)
}

// fetchRelease fetches the fork's latest release, honouring the disk cache +
// ETag/304 protocol. It returns (nil, nil) when the repository has no
// published release yet (GitHub answers 404): that is "no update", never a
// reason to look elsewhere.
func fetchRelease() (*release, error) {
	apiURL := sourceAPIURL(SourceGitHub)

	if cached := loadCache(); cached != nil {
		log.Writef("[update] returning disk-cached release, repo=%s, age=%s", updateRepo, time.Since(cached.Timestamp))
		return &cached.Release, nil
	}

	log.Writef("[update] fetching latest release, repo=%s", updateRepo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", appname.Name)

	resp, err := sharedHTTPClient().Do(req)
	if err != nil {
		log.Writef("[update] api request error: %v", err)
		return nil, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Writef("[update] %s has no published release", updateRepo)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		log.Writef("[update] decode error: %v", err)
		return nil, fmt.Errorf("decode response: %w", err)
	}

	log.Writef("[update] latest=%s, assets=%d (repo=%s)", rel.TagName, len(rel.Assets), updateRepo)

	saveCache(&cacheEntry{
		Release:   rel,
		Source:    SourceGitHub,
		Timestamp: time.Now(),
		ETag:      resp.Header.Get("ETag"),
	})

	return &rel, nil
}

// fetchAssetBytes downloads a small release asset (checksums or signature).
func fetchAssetBytes(ctx context.Context, rel *release, name string) ([]byte, bool) {
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		if !assetURLAllowed(a.BrowserDownloadURL) {
			log.Writef("[update] %s url outside %s releases; ignored", name, updateRepo)
			return nil, false
		}
		req, err := http.NewRequestWithContext(ctx, "GET", a.BrowserDownloadURL, nil)
		if err != nil {
			return nil, false
		}
		req.Header.Set("User-Agent", appname.Name)
		resp, err := downloadHTTPClient().Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			log.Writef("[update] %s fetch failed: %v", name, err)
			return nil, false
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return nil, false
		}
		return data, true
	}
	return nil, false
}

// EmbeddedReleasePublicKey returns the release key compiled into this build
// ("" when none). The release tooling uses it to prove the -ldflags -X path
// is right: a wrong path silently leaves the key empty.
func EmbeddedReleasePublicKey() string { return strings.TrimSpace(releasePublicKey) }

// ParsePublicKey decodes a base64 ed25519 public key (32 raw bytes).
func ParsePublicKey(b64 string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("release public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release public key: want %d bytes, got %d", ed25519.PublicKeySize, len(key))
	}
	return ed25519.PublicKey(key), nil
}

// VerifyChecksumsSignature checks sig (raw 64 bytes or base64 text) over
// data against the base64 public key pubB64. An empty or malformed key never
// verifies.
func VerifyChecksumsSignature(pubB64 string, data, sig []byte) bool {
	key, err := ParsePublicKey(pubB64)
	if err != nil {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
		if err != nil {
			return false
		}
		sig = dec
	}
	return len(sig) == ed25519.SignatureSize && ed25519.Verify(key, data, sig)
}

// verifyChecksumsSignature checks sig against the compiled-in release key.
// No key → never verified.
func verifyChecksumsSignature(data, sig []byte) bool {
	return VerifyChecksumsSignature(releasePublicKey, data, sig)
}

// fetchChecksums downloads checksums.txt and checksums.txt.sig from the
// release and returns name → sha256 only when the signature verifies against
// releasePublicKey. Anything else (no key compiled in, unsigned release, bad
// signature) yields an empty map, so no asset carries a hash and Download
// refuses every candidate: a checksum published next to the asset it covers
// proves integrity, not origin.
func fetchChecksums(rel *release) map[string]string {
	if strings.TrimSpace(releasePublicKey) == "" {
		log.Writef("[update] no release signing key compiled in; install disabled")
		return map[string]string{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, ok := fetchAssetBytes(ctx, rel, "checksums.txt")
	if !ok {
		return map[string]string{}
	}
	sig, ok := fetchAssetBytes(ctx, rel, "checksums.txt.sig")
	if !ok {
		log.Writef("[update] release has no checksums.txt.sig; refusing its checksums")
		return map[string]string{}
	}
	if !verifyChecksumsSignature(data, sig) {
		log.Writef("[update] checksums.txt signature invalid; refusing its checksums")
		return map[string]string{}
	}
	sums := parseChecksums(data)
	log.Writef("[update] parsed %d signed checksums", len(sums))
	return sums
}

// classifyAsset buckets an asset name by its update payload kind. Any .zip is
// treated as portable (Windows portable zip and the macOS bare-binary zip);
// .dmg/.deb/.rpm are manual-install artifacts and land in "other".
func classifyAsset(name string) string {
	switch {
	case strings.Contains(name, "-installer-") && strings.HasSuffix(name, ".exe"):
		return "installer"
	case strings.HasSuffix(name, ".zip"):
		return "portable"
	case strings.HasSuffix(name, ".tar.gz"):
		return "binary-tar.gz"
	}
	return "other"
}

// assetMatchesPlatform reports whether the asset targets the given OS/arch.
// Asset names are anchored to the uniterm-<os>-<arch>- prefix.
func assetMatchesPlatform(osName, arch, name string) bool {
	switch osName {
	case "windows", "linux", "darwin":
	default:
		return false
	}
	return strings.HasPrefix(name, "uniterm-"+osName+"-"+arch+"-")
}

// kindPreference returns the asset kinds usable for a channel, best first.
func kindPreference(ch Channel) []string {
	switch ch {
	case ChannelInstaller:
		return []string{"installer", "portable"}
	case ChannelPortable:
		return []string{"portable", "binary-tar.gz"}
	default:
		return nil
	}
}

// candidatesFor orders the installable assets for a platform and channel,
// preferred kind first. Only assets with a signed checksum and a download URL
// under the fork's releases are returned.
func candidatesFor(osName, arch string, ch Channel, rel *release, sums map[string]string) []UpdateAsset {
	var out []UpdateAsset
	if rel == nil {
		return nil
	}
	for _, kind := range kindPreference(ch) {
		for _, a := range rel.Assets {
			if classifyAsset(a.Name) != kind || !assetMatchesPlatform(osName, arch, a.Name) {
				continue
			}
			if sums[a.Name] == "" || !assetURLAllowed(a.BrowserDownloadURL) {
				continue
			}
			out = append(out, UpdateAsset{
				Name:   a.Name,
				URL:    a.BrowserDownloadURL,
				SHA256: sums[a.Name],
				Source: SourceGitHub,
			})
		}
	}
	return out
}

// Check compares the current version against the fork's latest release. The
// source argument is kept for API compatibility and ignored: there is a single
// source and no fallback mirror. Assets are only listed when the release's
// checksums are signed by releasePublicKey.
func Check(currentVersion, source string) (*UpdateInfo, error) {
	source = normalizeSource(source)
	removeLegacyCaches()

	log.Writef("[update] Check called, current=%s, repo=%s", currentVersion, updateRepo)

	rel, err := fetchRelease()
	if err != nil {
		return nil, err
	}

	result := UpdateInfo{
		Current:    currentVersion,
		ReleaseURL: sourceReleaseURL(source),
	}
	if rel == nil || rel.TagName == "" {
		return &result, nil
	}
	result.Latest = rel.TagName
	result.Changelog = rel.Body
	result.HasUpdate = shouldUpdate(currentVersion, rel.TagName)

	if !result.HasUpdate {
		return &result, nil
	}

	sums := fetchChecksums(rel)
	result.Assets = candidatesFor(runtime.GOOS, runtime.GOARCH, DetectChannel(), rel, sums)
	log.Writef("[update] %d verified candidate asset(s) for channel %s", len(result.Assets), DetectChannel())

	return &result, nil
}
