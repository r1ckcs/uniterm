// Package records turns the app's stores (connections, groups, identities)
// into vault op payloads and back. Each record is a set of top-level JSON
// fields; an op carries only the fields that changed, and replaying the log
// in its total order (Lamport, author, seq) is a per-field last-writer-wins
// merge: two devices editing different fields of one connection both keep
// their edit, and on the same field the later op wins on every device.
package records

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
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
	// Schema says which fields the writer knows (see lateFields). Absent
	// (0) in ops of builds from before it existed. Old builds ignore it,
	// so it is not part of Format: bumping Format would make them skip
	// every new op.
	Schema int `json:"schema,omitempty"`
}

const payloadFormat = 1

// payloadSchema is the Schema this build writes.
const payloadSchema = 2

// lateFields are fields added after builds that sync already existed,
// with the Schema that introduced them. A writer below that Schema cannot
// know the field: it drops it from its stores and its next diff sends it
// as null. So such an op's mention of the field (set or null) is ignored;
// nothing is lost, as that writer could never set it.
var lateFields = map[string]int{
	"color": 2, // connection and group, CONTRATO-cor-icone.md
	"icon":  2,
}

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
	p := Payload{Format: payloadFormat, Schema: payloadSchema, At: at.UTC(), Changes: changes}
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
	// LastAt is each author's newest op time (the devices list).
	LastAt map[string]time.Time
}

// NewState is an empty state.
func NewState() *State {
	return &State{Records: map[Key]*Record{}, deleted: map[Key]bool{}, LastAt: map[string]time.Time{}}
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
	if p.At.After(s.LastAt[author]) {
		s.LastAt[author] = p.At
	}
	for _, c := range p.Changes {
		c.Fields = knownTo(p.Schema, c.Fields)
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

// knownTo drops the late fields a writer of schema could not know.
func knownTo(schema int, f Fields) Fields {
	var out Fields
	for name, since := range lateFields {
		if _, ok := f[name]; ok && schema < since {
			if out == nil {
				out = maps.Clone(f)
			}
			delete(out, name)
		}
	}
	if out == nil {
		return f
	}
	return out
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

// Keep returns a copy of s holding only the fields keep accepts: the ones
// the local stores can hold. A field this build doesn't know is dropped by
// the stores on write; leaving it out of the baseline is what keeps the
// next diff from sending it back as null.
func (s Snapshot) Keep(keep func(kind Kind, field string) bool) Snapshot {
	out := make(Snapshot, len(s))
	for k, f := range s {
		g := make(Fields, len(f))
		for name, v := range f {
			if keep(k.Kind, name) {
				g[name] = v
			}
		}
		out[k] = g
	}
	return out
}

// JSONFields lists the JSON field names of struct v (or pointer to it),
// "id" included; "-" fields are left out.
func JSONFields(v any) map[string]bool {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := map[string]bool{}
	for i := range t.NumField() {
		sf := t.Field(i)
		if !sf.IsExported() && !sf.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if sf.Anonymous && name == "" && sf.Type.Kind() == reflect.Struct {
			maps.Copy(out, JSONFields(reflect.Zero(sf.Type).Interface()))
			continue
		}
		if name == "" {
			name = sf.Name
		}
		out[name] = true
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
	sortChanges(out)
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

// Digest is a snapshot reduced to keyed field hashes: what a device last
// wrote into its stores, kept on disk to tell its own edits from remote
// ones it has applied, without keeping secrets in the clear.
type Digest map[string]map[string]string

func digestKey(k Key) string { return string(k.Kind) + "/" + k.ID }

// Digest hashes every field with key (HMAC-SHA256, so weak passwords
// can't be guessed from the file without the device's key).
func (s Snapshot) Digest(key []byte) Digest {
	out := Digest{}
	for k, f := range s {
		m := map[string]string{}
		for name, v := range f {
			h := hmac.New(sha256.New, key)
			h.Write([]byte(name))
			h.Write([]byte{0})
			h.Write(v)
			m[name] = hex.EncodeToString(h.Sum(nil))
		}
		out[digestKey(k)] = m
	}
	return out
}

// LocalChanges is what this device changed in cur since it last wrote
// base (a Digest made with the same key). Remote edits applied since then
// are in base, so they are not sent back as reverts.
func LocalChanges(base Digest, key []byte, cur Snapshot) []Change {
	curD := cur.Digest(key)
	var out []Change
	for k, f := range cur {
		old, ok := base[digestKey(k)]
		if !ok {
			out = append(out, Change{Kind: k.Kind, ID: k.ID, Create: true, Fields: maps.Clone(f)})
			continue
		}
		changed := Fields{}
		for name, v := range f {
			if old[name] != curD[digestKey(k)][name] {
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
	for dk := range base {
		kind, id, _ := cutKey(dk)
		if _, ok := cur[Key{kind, id}]; !ok {
			out = append(out, Change{Kind: kind, ID: id, Delete: true})
		}
	}
	sortChanges(out)
	return out
}

func cutKey(s string) (Kind, string, bool) {
	for i := range len(s) {
		if s[i] == '/' {
			return Kind(s[:i]), s[i+1:], true
		}
	}
	return "", s, false
}

func sortChanges(out []Change) {
	slices.SortFunc(out, func(a, b Change) int {
		if a.Kind != b.Kind {
			return cmpStr(string(a.Kind), string(b.Kind))
		}
		return cmpStr(a.ID, b.ID)
	})
}
