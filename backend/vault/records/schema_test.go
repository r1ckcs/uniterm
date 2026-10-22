package records

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

// An op from a build before Schema 2 (no "schema" key) can't know color:
// its null is the stores dropping an unknown key, not a user clearing it.
func TestOldSchemaCannotClearLateFields(t *testing.T) {
	create := []Change{{Kind: Connection, ID: "c1", Create: true, Fields: Fields{
		"name": json.RawMessage(`"olt"`), "color": json.RawMessage(`"orange"`), "icon": json.RawMessage(`"olt"`),
	}}}
	oldOp := vault.Op{Name: "o2", Author: "B", Payload: []byte(
		`{"format":1,"at":"2026-10-22T15:00:00Z","changes":[{"kind":"connection","id":"c1","fields":{"name":"olt-pjt","color":null,"icon":null}}]}`)}
	onlyLate := vault.Op{Name: "o3", Author: "B", Payload: []byte(
		`{"format":1,"at":"2026-10-22T15:01:00Z","changes":[{"kind":"connection","id":"c1","fields":{"color":"red"}}]}`)}
	s := Replay([]vault.Op{op(t, "o1", "A", create), oldOp, onlyLate})
	f := s.Records[Key{Connection, "c1"}].Fields
	if string(f["name"]) != `"olt-pjt"` || string(f["color"]) != `"orange"` || string(f["icon"]) != `"olt"` {
		t.Fatalf("fields = %s %s %s", f["name"], f["color"], f["icon"])
	}
	if last := s.Records[Key{Connection, "c1"}].Last; last.Author != "B" || strings.Join(last.Fields, ",") != "name" {
		t.Fatalf("badge = %+v (the ignored-only op must not count)", last)
	}

	// The current schema can clear it.
	clear := []Change{{Kind: Connection, ID: "c1", Fields: Fields{"color": json.RawMessage("null")}}}
	s = Replay([]vault.Op{op(t, "o1", "A", create), op(t, "o2", "B", clear)})
	if _, ok := s.Records[Key{Connection, "c1"}].Fields["color"]; ok {
		t.Fatal("a schema-2 null did not clear color")
	}
}

func TestEncodeWritesSchema(t *testing.T) {
	p, err := Encode(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decode(p)
	if err != nil || d.Schema != payloadSchema {
		t.Fatalf("schema %d %v", d.Schema, err)
	}
}

func TestKeepAndJSONFields(t *testing.T) {
	type inner struct {
		Extra string `json:"extra"`
	}
	type rec struct {
		inner
		ID     string `json:"id"`
		Name   string `json:"name,omitempty"`
		Secret string `json:"-"`
		Plain  string
	}
	got := JSONFields(&rec{})
	for _, want := range []string{"id", "name", "extra", "Plain"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if got["-"] || got["Secret"] || len(got) != 4 {
		t.Errorf("got %v", got)
	}
	s := Snapshot{{Connection, "c1"}: Fields{"name": json.RawMessage(`"a"`), "color": json.RawMessage(`"red"`)}}
	k := s.Keep(func(_ Kind, f string) bool { return f != "color" })
	if _, ok := k[Key{Connection, "c1"}]["color"]; ok || len(s[Key{Connection, "c1"}]) != 2 {
		t.Fatalf("keep %v, original %v", k, s)
	}
}
