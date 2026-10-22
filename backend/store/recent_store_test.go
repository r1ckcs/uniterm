package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecentStore_Record_Deduplicates(t *testing.T) {
	dir := t.TempDir()
	s := NewRecentStore(dir)

	s.Record("a")
	s.Record("b")
	s.Record("a") // duplicate — should move to front

	ids := s.GetAll()
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique ids, got %d: %v", len(ids), ids)
	}
	if ids[0] != "a" {
		t.Errorf("expected 'a' first (most recent), got %v", ids)
	}
	if ids[1] != "b" {
		t.Errorf("expected 'b' second, got %v", ids)
	}
}

func TestRecentStore_Record_TrimsToMax(t *testing.T) {
	dir := t.TempDir()
	s := NewRecentStore(dir)

	for i := 0; i < 25; i++ {
		s.Record(string(rune('a' + i)))
	}

	ids := s.GetAll()
	if len(ids) != 20 {
		t.Fatalf("expected 20 ids (maxRecent), got %d", len(ids))
	}
}

func TestRecentStore_Load_MissingFile(t *testing.T) {
	dir := t.TempDir()
	s := NewRecentStore(dir)

	ids, err := s.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list, got %v", ids)
	}
}

func TestRecentStore_Load_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "recent.json")
	os.WriteFile(filePath, []byte("not valid json"), 0644)

	s := NewRecentStore(dir)
	ids, err := s.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list for corrupted file, got %v", ids)
	}
}

func TestRecentStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	s := NewRecentStore(dir)

	s.Record("conn-1")
	s.Record("conn-2")
	// Record is debounced; flush via Close before reading from a fresh store.
	s.Close()

	// Create a new store pointing to same dir — should read persisted data
	s2 := NewRecentStore(dir)
	ids, _ := s2.Load()

	if len(ids) != 2 {
		t.Fatalf("expected 2 ids loaded from disk, got %d: %v", len(ids), ids)
	}
	if ids[0] != "conn-2" {
		t.Errorf("expected 'conn-2' first, got %v", ids)
	}
}

func TestRecentStore_AccessTimes(t *testing.T) {
	dir := t.TempDir()
	// An entry from a build that kept no times.
	if err := os.WriteFile(filepath.Join(dir, "recent.json"), []byte(`["old"]`), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewRecentStore(dir)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 22, 13, 4, 5, 0, time.UTC)
	s.now = func() time.Time { return t0 }
	s.Record("a")
	got := s.GetAccess()
	if len(got) != 2 || got[0].ID != "a" || got[0].At != "2026-10-22T13:04:05Z" || got[1].ID != "old" || got[1].At != "" {
		t.Fatalf("got %+v", got)
	}
	b, _ := json.Marshal(got)
	if string(b) != `[{"id":"a","at":"2026-10-22T13:04:05Z"},{"id":"old","at":""}]` {
		t.Fatalf("json %s", b)
	}
	s.Close()

	// recent.json keeps the old format; the times live beside it.
	ids, _ := os.ReadFile(filepath.Join(dir, "recent.json"))
	if string(ids) != `["a","old"]` {
		t.Fatalf("recent.json %s", ids)
	}
	s2 := NewRecentStore(dir)
	defer s2.Close()
	if _, err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s2.GetAccess(); len(got) != 2 || got[0].At != "2026-10-22T13:04:05Z" {
		t.Fatalf("after reload %+v", got)
	}
}

func TestRecentStore_AccessFileNullOrCorrupt(t *testing.T) {
	for _, body := range []string{"null", "{bad"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "recent.json"), []byte(`["a"]`), 0644)
		os.WriteFile(filepath.Join(dir, "recent-access.json"), []byte(body), 0600)
		s := NewRecentStore(dir)
		if _, err := s.Load(); err != nil {
			t.Fatal(err)
		}
		s.Record("b") // must not panic on a nil map
		if got := s.GetAccess(); len(got) != 2 || got[1].ID != "a" || got[1].At != "" {
			t.Fatalf("%s: %+v", body, got)
		}
		s.Close()
	}
}

func TestRecentStore_FilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "recent.json"), []byte(`["x"]`), 0644) // older build
	s := NewRecentStore(dir)
	s.Load()
	s.Record("a")
	s.Close()
	for _, name := range []string{"recent.json", "recent-access.json"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v", name, fi.Mode().Perm())
		}
	}
}
