package vault

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemStore is an in-memory Store with Drive's semantics: names are not
// unique and Create always adds a candidate. It exists to exercise the
// candidate handling before the Drive backend does; Now and Hide simulate
// server clocks and a listing that lags behind.
type MemStore struct {
	mu      sync.Mutex
	seq     int64
	last    int64
	objects map[string]memObject
	hidden  map[string]bool
	// Now is the server clock used for Created (time.Now if nil).
	Now func() time.Time
}

type memObject struct {
	entry Entry
	data  []byte
}

func NewMemStore() *MemStore {
	return &MemStore{objects: map[string]memObject{}, hidden: map[string]bool{}}
}

// Hide leaves an object out of List until Show (a lagging listing).
func (m *MemStore) Hide(id string) { m.mu.Lock(); m.hidden[id] = true; m.mu.Unlock() }

// Show undoes Hide.
func (m *MemStore) Show(id string) { m.mu.Lock(); delete(m.hidden, id); m.mu.Unlock() }

func (m *MemStore) List(prefix string) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Entry
	for _, o := range m.objects {
		if strings.HasPrefix(o.entry.Name, prefix) && !m.hidden[o.entry.ID] {
			out = append(out, o.entry)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Created < out[j].Created
	})
	return out, nil
}

func (m *MemStore) Get(id string) ([]byte, Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[id]
	if !ok {
		return nil, "", ErrNotFound
	}
	return append([]byte(nil), o.data...), contentVersion(o.data), nil
}

func (m *MemStore) Create(name string, data []byte) (Entry, error) {
	if !ValidName(name) {
		return Entry{}, ErrBadName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	created := max(now().UnixNano(), m.last+1)
	m.last = created
	e := Entry{ID: "m" + strconv.FormatInt(m.seq, 10), Name: name, Created: created}
	m.objects[e.ID] = memObject{entry: e, data: append([]byte(nil), data...)}
	return e, nil
}

func (m *MemStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, id)
	return nil
}

// Replace overwrites an object's content in place (tests: tampering).
func (m *MemStore) Replace(id string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.objects[id]
	o.data = append([]byte(nil), data...)
	m.objects[id] = o
}
