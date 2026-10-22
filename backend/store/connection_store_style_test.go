package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ys-ll/uniterm/backend/session"
)

func TestConnectionStore_StyleRoundTripAndJunk(t *testing.T) {
	dir := t.TempDir()
	s := &ConnectionStore{configDir: dir, passwordStore: fakeCipherStore{}}
	in := session.ConnectionStoreData{
		Groups: []session.ConnectionGroup{{ID: "g1", Name: "PJT", Color: "orange", Icon: "router"},
			{ID: "g2", Name: "x", Color: "url(evil)", Icon: "x.svg"}},
		Connections: []session.ConnectionConfig{{ID: "c1", Type: "ssh", Color: "#F46414", Icon: "olt"},
			{ID: "c2", Type: "ssh", Color: "none", Icon: "<svg>"}},
	}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	if in.Groups[1].Color != "url(evil)" {
		t.Fatal("Save changed the caller's groups")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, storeFileName))
	for _, junk := range []string{"evil", "x.svg", "<svg>", "F46414"} {
		if strings.Contains(string(raw), junk) {
			t.Fatalf("%q reached the file: %s", junk, raw)
		}
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	g, c := got.Groups, got.Connections
	if g[0].Color != "orange" || g[0].Icon != "router" || g[1].Color != "" || g[1].Icon != "" ||
		c[0].Color != "#f46414" || c[0].Icon != "olt" || c[1].Color != "none" || c[1].Icon != "" {
		t.Fatalf("got %+v %+v", g, c)
	}
}

// Files from before the fields load with none; a hand edit is checked too.
func TestConnectionStore_StyleOldFileAndHandEdit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, storeFileName), []byte(`{"groups":[{"id":"g","name":"a"}],
		"connections":[{"id":"c1","name":"x","type":"ssh","host":"h","port":22,"user":"u","authType":"agent"},
		{"id":"c2","name":"y","type":"ssh","host":"h","port":22,"user":"u","authType":"agent","color":"red;x","icon":"../../etc"}]}`), 0600)
	s := &ConnectionStore{configDir: dir, passwordStore: fakeCipherStore{}}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Connections {
		if c.Color != "" || c.Icon != "" {
			t.Fatalf("%s: %q %q", c.ID, c.Color, c.Icon)
		}
	}
	if got.Groups[0].Color != "" {
		t.Fatal(got.Groups[0])
	}
}
