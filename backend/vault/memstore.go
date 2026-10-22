package vault

import (
	"sort"
	"strconv"
	"strings"
	"sync"
)

// MemStore is an in-memory Store with Drive's semantics: names are not
// unique and Create always adds a candidate. It exists to exercise the
// candidate handling before the Drive backend does.
type MemStore struct {
	mu      sync.Mutex
	seq     int64
	objects map[string]memObject
}

type memObject struct {
	entry Entry
	data  []byte
}

func NewMemStore() *MemStore { return &MemStore{objects: map[string]memObject{}} }

func (m *MemStore) List(prefix string) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Entry
	for _, o := range m.objects {
		if strings.HasPrefix(o.entry.Name, prefix) {
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
	e := Entry{ID: "m" + strconv.FormatInt(m.seq, 10), Name: name, Created: m.seq}
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
