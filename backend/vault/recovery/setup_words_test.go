package recovery

import (
	"testing"

	"github.com/ys-ll/uniterm/backend/vault"
)

// SetupWithWords resumes after a partial failure with the same words.
func TestSetupWithWordsIsResumable(t *testing.T) {
	s := vault.NewMemStore()
	admin, _ := vault.NewSoftwareDeviceKey()
	v, err := vault.Create(s, vault.NewMemState(vault.State{}), "pessoal", admin, "h", "mac")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Setup(s, v)
	if err != nil {
		t.Fatal(err)
	}
	words, _, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the new key was added (before the revocation).
	k, _ := KeyFromWords(words)
	if err := v.AddMember(vault.MemberOf(k, "", RecoveryName, vault.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	if err := SetupWithWords(s, v, words); err != nil {
		t.Fatalf("resume = %v", err)
	}
	n := 0
	for _, m := range v.Roster().Members {
		if IsRecoveryMember(m) {
			n++
			if m.DeviceID != k.DeviceID() {
				t.Fatal("old key kept")
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d recovery members", n)
	}
	dev, _ := vault.NewSoftwareDeviceKey()
	tr, err := Recover(s, words, dev, "h", "novo")
	if err != nil {
		t.Fatalf("recover with the new words = %v", err)
	}
	if tr.Counter < v.Trust().Counter-1 {
		t.Fatalf("anchored at %d, vault at %d", tr.Counter, v.Trust().Counter)
	}
	dev2, _ := vault.NewSoftwareDeviceKey()
	if _, err := Recover(s, first, dev2, "h", "velho"); err == nil {
		t.Fatal("old words still recover")
	}
}
