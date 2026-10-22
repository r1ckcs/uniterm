package appname

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestCopyLegacyDirCopiesTreeAndKeepsOld(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "uniTerm"), filepath.Join(base, "FlexTerm")
	write(t, filepath.Join(oldDir, "connections.json"), `{"a":1}`, 0o600)
	write(t, filepath.Join(oldDir, "vaults", "v1", "state.json"), "s", 0o600)
	write(t, filepath.Join(oldDir, "bootstrap.json"), "b", 0o644)

	copied, err := CopyLegacyDir(oldDir, newDir)
	if err != nil || !copied {
		t.Fatalf("CopyLegacyDir = %v, %v", copied, err)
	}
	for _, rel := range []string{"connections.json", "vaults/v1/state.json", "bootstrap.json"} {
		a, _ := os.ReadFile(filepath.Join(oldDir, rel))
		b, err := os.ReadFile(filepath.Join(newDir, rel))
		if err != nil || string(a) != string(b) {
			t.Fatalf("%s not copied: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(oldDir, "connections.json")); err != nil {
		t.Fatal("old folder must be kept (copy, not move)")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(newDir, "connections.json"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("secret file mode = %v, want 0600", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 2 {
		t.Fatalf("staging dir left behind: %v", entries)
	}
}

func TestCopyLegacyDirNeverTouchesExistingNewDir(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "uniTerm"), filepath.Join(base, "FlexTerm")
	write(t, filepath.Join(oldDir, "connections.json"), "old", 0o600)
	write(t, filepath.Join(newDir, "connections.json"), "new", 0o600)

	if copied, err := CopyLegacyDir(oldDir, newDir); copied || err != nil {
		t.Fatalf("CopyLegacyDir = %v, %v; want no-op", copied, err)
	}
	if b, _ := os.ReadFile(filepath.Join(newDir, "connections.json")); string(b) != "new" {
		t.Fatal("existing FlexTerm data was overwritten")
	}
}

func TestCopyLegacyDirNothingToMigrate(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "FlexTerm")
	if copied, err := CopyLegacyDir(filepath.Join(base, "uniTerm"), newDir); copied || err != nil {
		t.Fatalf("CopyLegacyDir = %v, %v", copied, err)
	}
	if _, err := os.Stat(newDir); err == nil {
		t.Fatal("new dir must not be created when there is nothing to copy")
	}
}

func TestCopyLegacyDirKeepsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "uniTerm"), filepath.Join(base, "FlexTerm")
	write(t, filepath.Join(oldDir, "a.txt"), "x", 0o600)
	if err := os.Symlink("a.txt", filepath.Join(oldDir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyLegacyDir(oldDir, newDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(newDir, "link")); err != nil || got != "a.txt" {
		t.Fatalf("symlink = %q, %v", got, err)
	}
}
