package sync

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeychainFallsBackToLegacyServiceAndCopiesForward(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(legacyKeychainService, "encryption-key", "abc"); err != nil {
		t.Fatal(err)
	}
	k := NewKeychain()
	got, err := k.Get("encryption-key")
	if err != nil || got != "abc" {
		t.Fatalf("Get = %q, %v; want legacy value", got, err)
	}
	if v, err := keyring.Get(keychainService, "encryption-key"); err != nil || v != "abc" {
		t.Fatalf("not copied to the new service: %q, %v", v, err)
	}
	if v, err := keyring.Get(legacyKeychainService, "encryption-key"); err != nil || v != "abc" {
		t.Fatalf("legacy item must stay: %q, %v", v, err)
	}
}

func TestKeychainPrefersNewService(t *testing.T) {
	keyring.MockInit()
	_ = keyring.Set(legacyKeychainService, "k", "old")
	_ = keyring.Set(keychainService, "k", "new")
	if got, _ := NewKeychain().Get("k"); got != "new" {
		t.Fatalf("Get = %q, want new", got)
	}
}

func TestKeychainDeleteDoesNotResurrectLegacy(t *testing.T) {
	keyring.MockInit()
	_ = keyring.Set(legacyKeychainService, "k", "old")
	k := NewKeychain()
	if _, err := k.Get("k"); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get("k"); err == nil {
		t.Fatalf("deleted key came back from the legacy service: %q", v)
	}
	if v, _ := keyring.Get(legacyKeychainService, "k"); v != "old" {
		t.Fatal("legacy item must not be deleted")
	}
}

func TestKeychainMissingEverywhere(t *testing.T) {
	keyring.MockInit()
	if _, err := NewKeychain().Get("nope"); err == nil {
		t.Fatal("want error for a key in neither service")
	}
}
