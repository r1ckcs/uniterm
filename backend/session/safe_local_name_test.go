package session

import "testing"

func TestSafeRemoteName(t *testing.T) {
	bad := []string{"", ".", "..", "../x", "a/b", `..\x`, `a\b`, "x\x00y", "/etc/passwd"}
	for _, n := range bad {
		if safeRemoteName(n) {
			t.Errorf("safeRemoteName(%q) = true", n)
		}
	}
	for _, n := range []string{"notes.txt", ".bashrc", "a..b", "日本語.md"} {
		if !safeRemoteName(n) {
			t.Errorf("safeRemoteName(%q) = false", n)
		}
	}
	if _, err := safeLocalRelPath("/tmp/dl", "docs/../../x"); err == nil {
		t.Error("safeLocalRelPath accepted traversal")
	}
	if p, err := safeLocalRelPath("/tmp/dl", "docs/a.txt"); err != nil || p != "/tmp/dl/docs/a.txt" {
		t.Errorf("safeLocalRelPath = %q, %v", p, err)
	}
}
