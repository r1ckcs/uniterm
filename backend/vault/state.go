package vault

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// State is what a device keeps locally per vault, outside the vault folder
// (it must not travel with the storage it guards).
type State struct {
	// Trust is the pinned roster version (anti-rollback): the newest
	// accepted one that is at least PinDelay old (see Vault.refreshLocked).
	Trust TrustState `json:"trust"`
	// NextSeq is this device's next op sequence number. It is reserved here
	// before the op is written, never derived from the storage listing
	// alone: a stale listing or a restored backup would otherwise reuse a
	// number for a different op (equivocation).
	NextSeq uint64 `json:"nextSeq"`
	// Lamport is the highest logical clock this device used or accepted.
	Lamport uint64 `json:"lamport"`
}

// ErrNoState: no local state yet (the device has not joined this vault).
var ErrNoState = errors.New("vault: no local state")

// StateStore persists State.
type StateStore interface {
	Load() (State, error)
	Save(State) error
}

// FileState keeps State in a JSON file (0600).
type FileState struct{ Path string }

func (f FileState) Load() (State, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, ErrNoState
	}
	if err != nil {
		return State{}, err
	}
	var st State
	err = json.Unmarshal(data, &st)
	return st, err
}

func (f FileState) Save(st State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	tmp, err := writeTemp(f.Path, data)
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// MemState keeps State in memory (tests, or a caller with its own storage).
type MemState struct {
	mu  sync.Mutex
	st  State
	set bool
}

// NewMemState starts with st (e.g. a joining device's genesis anchor).
func NewMemState(st State) *MemState { return &MemState{st: st, set: true} }

func (m *MemState) Load() (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.set {
		return State{}, ErrNoState
	}
	return m.st, nil
}

func (m *MemState) Save(st State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st, m.set = st, true
	return nil
}
