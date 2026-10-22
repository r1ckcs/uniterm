package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// CachedStore keeps a local copy of a remote store so a device reads its
// vault offline and fetches each object once. Objects are cached by id
// when first read or written: signed objects are verified on every read,
// so a cached copy is as good as the remote one (and a remote copy edited
// in place later is not picked up). The last successful listing is kept
// and served while the remote is unavailable.
type CachedStore struct {
	Remote Store
	dir    string
	mu      sync.Mutex
	listed  []Entry // merged listings, replaced per prefix on each success
	offline bool
}

// Offline reports whether the last List had to be answered from the cache.
func (c *CachedStore) Offline() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offline
}

const cacheIndex = "index.json"

// NewCachedStore caches remote under dir (created 0700).
func NewCachedStore(remote Store, dir string) (*CachedStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o700); err != nil {
		return nil, err
	}
	c := &CachedStore{Remote: remote, dir: dir}
	if data, err := os.ReadFile(filepath.Join(dir, cacheIndex)); err == nil {
		_ = json.Unmarshal(data, &c.listed)
	}
	return c, nil
}

func (c *CachedStore) objectPath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(c.dir, "objects", hex.EncodeToString(sum[:]))
}

// List asks the remote; while it is unavailable, it answers from the last
// listing (plus objects created since) without an error, so reading the
// vault keeps working; Offline() tells the caller.
func (c *CachedStore) List(prefix string) ([]Entry, error) {
	entries, err := c.Remote.List(prefix)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offline = err != nil && errors.Is(err, ErrUnavailable)
	if err == nil {
		kept := slices.DeleteFunc(slices.Clone(c.listed), func(e Entry) bool { return strings.HasPrefix(e.Name, prefix) })
		c.listed = append(kept, entries...)
		c.saveIndexLocked()
		return entries, nil
	}
	if !errors.Is(err, ErrUnavailable) {
		return nil, err
	}
	var out []Entry
	for _, e := range c.listed {
		if strings.HasPrefix(e.Name, prefix) {
			out = append(out, e)
		}
	}
	sortEntries(out)
	return out, nil
}

func (c *CachedStore) saveIndexLocked() {
	data, err := json.Marshal(c.listed)
	if err != nil {
		return
	}
	if tmp, err := writeTemp(filepath.Join(c.dir, cacheIndex), data); err == nil {
		_ = os.Rename(tmp, filepath.Join(c.dir, cacheIndex))
	}
}

func (c *CachedStore) Get(id string) ([]byte, Version, error) {
	if data, err := os.ReadFile(c.objectPath(id)); err == nil {
		return data, contentVersion(data), nil
	}
	data, ver, err := c.Remote.Get(id)
	if err != nil {
		return nil, "", err
	}
	c.put(id, data)
	return data, ver, nil
}

func (c *CachedStore) put(id string, data []byte) {
	p := c.objectPath(id)
	if tmp, err := writeTemp(p, data); err == nil {
		_ = os.Chmod(tmp, 0o600)
		_ = os.Rename(tmp, p)
	}
}

func (c *CachedStore) Create(name string, data []byte) (Entry, error) {
	e, err := c.Remote.Create(name, data)
	if err != nil {
		return e, err
	}
	c.put(e.ID, data)
	c.mu.Lock()
	c.listed = append(c.listed, e)
	c.saveIndexLocked()
	c.mu.Unlock()
	return e, nil
}

func (c *CachedStore) Delete(id string) error {
	if err := c.Remote.Delete(id); err != nil {
		return err
	}
	_ = os.Remove(c.objectPath(id))
	c.mu.Lock()
	c.listed = slices.DeleteFunc(c.listed, func(e Entry) bool { return e.ID == id })
	c.saveIndexLocked()
	c.mu.Unlock()
	return nil
}

func sortEntries(out []Entry) {
	slices.SortFunc(out, func(a, b Entry) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return int(min(max(a.Created-b.Created, -1), 1))
	})
}
