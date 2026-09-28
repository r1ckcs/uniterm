package update

import (
	"os"
	"strings"
	"sync"
)

// Manager holds in-memory state for one in-progress update (download → apply).
type Manager struct {
	mu      sync.Mutex
	pending *PendingUpdate
	// offered holds the assets returned by the last backend-side Check.
	// Download only accepts candidates from this list, so the frontend
	// (or script injected into it) cannot supply its own URL + hash.
	offered []UpdateAsset
}

// SetOffered records the assets from the most recent update check.
func (m *Manager) SetOffered(assets []UpdateAsset) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.offered = append([]UpdateAsset(nil), assets...)
}

// FilterOffered returns the subset of assets that exactly match (name, URL,
// checksum) an asset from the last backend-side Check.
func (m *Manager) FilterOffered(assets []UpdateAsset) []UpdateAsset {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []UpdateAsset
	for _, a := range assets {
		for _, o := range m.offered {
			if a.Name == o.Name && a.URL == o.URL && strings.EqualFold(a.SHA256, o.SHA256) {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

// PendingUpdate is a downloaded, verified, staged update ready to apply.
type PendingUpdate struct {
	Asset     UpdateAsset
	Kind      string // classifyAsset(name): "installer" | "portable" | "binary-tar.gz"
	StageDir  string // staging directory holding the downloaded artifact + extracted payload
	FilePath  string // the downloaded artifact itself
	NewBinary string // extracted replacement executable (empty for "installer")
}

// NewManager returns an empty update Manager.
func NewManager() *Manager { return &Manager{} }

// Pending returns the currently staged update, or nil.
func (m *Manager) Pending() *PendingUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

// Clear discards the staged update and removes its staging directory.
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil && m.pending.StageDir != "" {
		// Installer payloads are kept for the detached updater script; only
		// extraction dirs are removed here.
		if m.pending.Kind != "installer" {
			_ = os.RemoveAll(m.pending.StageDir)
		}
	}
	m.pending = nil
}

func (m *Manager) setPending(p *PendingUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = p
}
