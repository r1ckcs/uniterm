// Package records turns the app's stores (connections, groups, identities)
// into vault op payloads and back. Each record is a set of top-level JSON
// fields; an op carries only the fields that changed, and replaying the log
// in its total order (Lamport, author, seq) is a per-field last-writer-wins
// merge: two devices editing different fields of one connection both keep
// their edit, and on the same field the later op wins on every device.
package records

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

// Kind is the store a record belongs to.
type Kind string

const (
	Connection Kind = "connection"
	Group      Kind = "group"
	Identity   Kind = "identity"
)

func (k Kind) valid() bool { return k == Connection || k == Group || k == Identity }

// Key identifies a record.
type Key struct {
	Kind Kind
	ID   string
}

// Fields is a record's top-level JSON fields (without "id").
type Fields map[string]json.RawMessage

// Change is one record's share of an op.
type Change struct {
	Kind Kind   `json:"kind"`
	ID   string `json:"id"`
	// Create carries the whole record. Only a Create brings a record into
	// existence, so an edit that raced a delete can't resurrect half a
	// connection.
	Create bool `json:"create,omitempty"`
	// Delete leaves a tombstone: later edits are ignored, a later Create
	// (same id) brings it back.
	Delete bool   `json:"delete,omitempty"`
	Fields Fields `json:"fields,omitempty"`
}

// Payload is the plaintext of one op: the changes of one local save.
type Payload struct {
	Format int `json:"format"`
	// At is the author's wall clock, for "changed by X on DD/MM" only;
	// ordering uses the signed Lamport clock.
	At      time.Time `json:"at"`
	Changes []Change  `json:"changes"`
}

const payloadFormat = 1

// Limits on what one op may carry (a writer can't blow up other clients).
const (
	maxChanges    = 5000
	maxFields     = 200
	maxIDLen      = 128
	maxFieldName  = 64
	maxFieldBytes = 256 << 10
)

// Encode builds an op payload.
func Encode(at time.Time, changes []Change) ([]byte, error) {
	p := Payload{Format: payloadFormat, At: at.UTC(), Changes: changes}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// Decode parses and validates an op payload.
func Decode(data []byte) (Payload, error) {
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("records: payload: %w", err)
	}
	return p, p.validate()
}

func (p *Payload) validate() error {
	if p.Format != payloadFormat {
		return fmt.Errorf("records: payload format %d not supported", p.Format)
	}
	if len(p.Changes) > maxChanges {
		return errors.New("records: too many changes in one op")
	}
	for _, c := range p.Changes {
		if !c.Kind.valid() || c.ID == "" || len(c.ID) > maxIDLen {
			return fmt.Errorf("records: bad change %q/%q", c.Kind, c.ID)
		}
		if c.Create && c.Delete || c.Delete && len(c.Fields) > 0 {
			return fmt.Errorf("records: change %s mixes create/delete", c.ID)
		}
		if len(c.Fields) > maxFields {
			return fmt.Errorf("records: change %s has too many fields", c.ID)
		}
		for f, v := range c.Fields {
			if f == "" || f == "id" || len(f) > maxFieldName || len(v) > maxFieldBytes || !json.Valid(v) {
				return fmt.Errorf("records: change %s has a bad field %q", c.ID, f)
			}
		}
	}
	return nil
}

// Stamp says who last changed a record, and which fields.
type Stamp struct {
	Author string
	At     time.Time
	Fields []string
}

// Record is a live record and its last change.
type Record struct {
	Fields Fields
	Last   Stamp
}

// State is the vault content after replaying the log.
type State struct {
	Records map[Key]*Record
	deleted map[Key]bool
	// Skipped names ops whose payload did not parse (applied as nothing).
	Skipped []string
}

// NewState is an empty state.
func NewState() *State {
	return &State{Records: map[Key]*Record{}, deleted: map[Key]bool{}}
}

// Replay applies ops in the given order (the log's total order).
func Replay(ops []vault.Op) *State {
	s := NewState()
	for _, op := range ops {
		s.Apply(op.Name, op.Author, op.Payload)
	}
	return s
}

// Apply merges one op's payload.
func (s *State) Apply(name, author string, payload []byte) {
	p, err := Decode(payload)
	if err != nil {
		s.Skipped = append(s.Skipped, name)
		return
	}
	for _, c := range p.Changes {
		k := Key{c.Kind, c.ID}
		stamp := Stamp{Author: author, At: p.At, Fields: slices.Sorted(maps.Keys(c.Fields))}
		switch {
		case c.Delete:
			delete(s.Records, k)
			s.deleted[k] = true
		case c.Create:
			delete(s.deleted, k)
			r := s.Records[k]
			if r == nil {
				r = &Record{Fields: Fields{}}
				s.Records[k] = r
			}
			r.Fields.merge(c.Fields)
			r.Last = stamp
		default:
			r := s.Records[k]
			if r == nil || len(c.Fields) == 0 {
				continue // deleted, or never created
			}
			r.Fields.merge(c.Fields)
			r.Last = stamp
		}
	}
}

// merge sets the changed fields; null removes one.
func (f Fields) merge(changed Fields) {
	for name, v := range changed {
		if string(v) == "null" {
			delete(f, name)
		} else {
			f[name] = v
		}
	}
}

// Snapshot is the plain content of a state (or of the local stores).
type Snapshot map[Key]Fields

// Snapshot returns the live records' fields.
func (s *State) Snapshot() Snapshot {
	out := Snapshot{}
	for k, r := range s.Records {
		out[k] = maps.Clone(r.Fields)
	}
	return out
}

// Diff is the changes that turn base into cur: new records as Create,
// changed fields only, missing records as Delete. A field missing from cur
// is sent as null (the stores omit empty fields). Sorted for determinism.
func Diff(base, cur Snapshot) []Change {
	var out []Change
	for k, f := range cur {
		old, ok := base[k]
		if !ok {
			out = append(out, Change{Kind: k.Kind, ID: k.ID, Create: true, Fields: maps.Clone(f)})
			continue
		}
		changed := Fields{}
		for name, v := range f {
			if !bytes.Equal(old[name], v) {
				changed[name] = v
			}
		}
		for name := range old {
			if _, ok := f[name]; !ok {
				changed[name] = json.RawMessage("null")
			}
		}
		if len(changed) > 0 {
			out = append(out, Change{Kind: k.Kind, ID: k.ID, Fields: changed})
		}
	}
	for k := range base {
		if _, ok := cur[k]; !ok {
			out = append(out, Change{Kind: k.Kind, ID: k.ID, Delete: true})
		}
	}
	slices.SortFunc(out, func(a, b Change) int {
		if a.Kind != b.Kind {
			return cmpStr(string(a.Kind), string(b.Kind))
		}
		return cmpStr(a.ID, b.ID)
	})
	return out
}

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ToFields splits a JSON object into fields (dropping "id" and nulls).
func ToFields(v any) (string, Fields, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return "", nil, err
	}
	var id string
	if raw, ok := all["id"]; ok {
		if err := json.Unmarshal(raw, &id); err != nil {
			return "", nil, fmt.Errorf("records: id: %w", err)
		}
	}
	delete(all, "id")
	for name, v := range all {
		if string(v) == "null" {
			delete(all, name)
		}
	}
	return id, all, nil
}

// FromFields rebuilds a JSON object (with "id") into v.
func FromFields(id string, f Fields, v any) error {
	all := maps.Clone(f)
	if all == nil {
		all = Fields{}
	}
	for name, raw := range all {
		if string(raw) == "null" {
			delete(all, name)
		}
	}
	idJSON, _ := json.Marshal(id)
	all["id"] = idJSON
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
