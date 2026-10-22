package records

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/vault"
)

type conn struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	Password string `json:"password,omitempty"`
}

func snap(t *testing.T, cs ...conn) Snapshot {
	t.Helper()
	s := Snapshot{}
	for _, c := range cs {
		id, f, err := ToFields(c)
		if err != nil {
			t.Fatal(err)
		}
		s[Key{Connection, id}] = f
	}
	return s
}

func op(t *testing.T, name, author string, changes []Change) vault.Op {
	t.Helper()
	p, err := Encode(time.Date(2026, 10, 22, 14, 30, 0, 0, time.UTC), changes)
	if err != nil {
		t.Fatal(err)
	}
	return vault.Op{Name: name, Author: author, Payload: p}
}

func get(t *testing.T, s *State, id string) conn {
	t.Helper()
	r := s.Records[Key{Connection, id}]
	if r == nil {
		t.Fatalf("%s missing", id)
	}
	var c conn
	if err := FromFields(id, r.Fields, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPerFieldLastWriterWins(t *testing.T) {
	base := snap(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.1", Port: 22})
	create := Diff(Snapshot{}, base)
	// A changes the host, B (concurrently) the port: both survive.
	a := Diff(base, snap(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.9", Port: 22}))
	b := Diff(base, snap(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.1", Port: 2222}))
	// C, later in the order, also sets the host: it wins.
	c := Diff(base, snap(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.7", Port: 22}))
	s := Replay([]vault.Op{op(t, "o1", "A", create), op(t, "o2", "A", a), op(t, "o3", "B", b), op(t, "o4", "C", c)})
	got := get(t, s, "c1")
	if got.Host != "10.0.0.7" || got.Port != 2222 || got.Name != "olt" {
		t.Fatalf("merged = %+v", got)
	}
	last := s.Records[Key{Connection, "c1"}].Last
	if last.Author != "C" || len(last.Fields) != 1 || last.Fields[0] != "host" {
		t.Fatalf("stamp = %+v", last)
	}
}

func TestDeleteWinsOverConcurrentEdit(t *testing.T) {
	base := snap(t, conn{ID: "c1", Name: "x", Host: "h"})
	edit := Diff(base, snap(t, conn{ID: "c1", Name: "x", Host: "h2"}))
	del := Diff(base, Snapshot{})
	if len(del) != 1 || !del[0].Delete {
		t.Fatalf("delete diff = %+v", del)
	}
	s := Replay([]vault.Op{op(t, "1", "A", Diff(Snapshot{}, base)), op(t, "2", "A", del), op(t, "3", "B", edit)})
	if len(s.Records) != 0 {
		t.Fatalf("edit resurrected a deleted record: %+v", s.Records)
	}
	// A later Create brings it back whole.
	s.Apply("4", "A", mustEncode(t, Diff(Snapshot{}, base)))
	if get(t, s, "c1").Host != "h" {
		t.Fatal("re-create failed")
	}
}

func TestClearedFieldDoesNotChurn(t *testing.T) {
	with := snap(t, conn{ID: "c1", Name: "x", Host: "h", Password: "s3nha"})
	without := snap(t, conn{ID: "c1", Name: "x", Host: "h"})
	s := Replay([]vault.Op{op(t, "1", "A", Diff(Snapshot{}, with)), op(t, "2", "A", Diff(with, without))})
	if d := Diff(s.Snapshot(), without); len(d) != 0 {
		t.Fatalf("diff after clearing a field = %+v", d)
	}
	if get(t, s, "c1").Password != "" {
		t.Fatal("password not cleared")
	}
}

func TestBadPayloadSkipped(t *testing.T) {
	s := Replay([]vault.Op{
		{Name: "junk", Author: "A", Payload: []byte("{")},
		{Name: "id-field", Author: "A", Payload: rawPayload(t, Change{Kind: Connection, ID: "c", Create: true, Fields: Fields{"id": json.RawMessage(`"other"`)}})},
		{Name: "kind", Author: "A", Payload: rawPayload(t, Change{Kind: "settings", ID: "c", Create: true})},
	})
	if len(s.Skipped) != 3 || len(s.Records) != 0 {
		t.Fatalf("skipped = %v, records = %v", s.Skipped, s.Records)
	}
}

func mustEncode(t *testing.T, ch []Change) []byte {
	t.Helper()
	p, err := Encode(time.Now(), ch)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// rawPayload skips Encode's validation (a malicious writer's payload).
func rawPayload(t *testing.T, c Change) []byte {
	t.Helper()
	data, err := json.Marshal(Payload{Format: payloadFormat, Changes: []Change{c}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLocalChangesIgnoreAppliedRemoteEdits(t *testing.T) {
	key := []byte("k")
	// The device last wrote v1 into its stores; a remote edit (host) came in
	// and was applied, so the digest is of the applied state.
	applied := snap(t, conn{ID: "c1", Name: "x", Host: "remote"}, conn{ID: "c2", Name: "y", Host: "h"})
	base := applied.Digest(key)
	// Then the user changed c1's name, deleted c2 and added c3.
	cur := snap(t, conn{ID: "c1", Name: "novo", Host: "remote"}, conn{ID: "c3", Name: "z", Host: "h3"})
	got := LocalChanges(base, key, cur)
	if len(got) != 3 {
		t.Fatalf("changes = %+v", got)
	}
	if c := got[0]; c.ID != "c1" || len(c.Fields) != 1 || string(c.Fields["name"]) != `"novo"` {
		t.Fatalf("c1 = %+v (must not resend host)", c)
	}
	if !got[1].Delete || got[1].ID != "c2" || !got[2].Create || got[2].ID != "c3" {
		t.Fatalf("c2/c3 = %+v %+v", got[1], got[2])
	}
	if d := LocalChanges(cur.Digest(key), key, cur); len(d) != 0 {
		t.Fatalf("no-op = %+v", d)
	}
	if bytes.Contains(mustJSON(t, base), []byte("remote")) {
		t.Fatal("digest leaks values")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
