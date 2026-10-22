//go:build !android

package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvDataDirOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "aparelho-b")
	t.Setenv(DataDirEnv, dir)
	dd, err := ResolveDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if dd.Path != dir || dd.FirstRun || dd.Upgrade {
		t.Fatalf("ResolveDataDir = %+v, want %s", dd, dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}

func TestEnvDataDirMustBeAbsolute(t *testing.T) {
	t.Setenv(DataDirEnv, "relativo/b")
	if _, err := ResolveDataDir(); err == nil {
		t.Fatal("relative override accepted")
	}
}
