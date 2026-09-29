package main

import (
	"crypto/rand"
	"testing"

	"github.com/ys-ll/uniterm/backend/credentials"
	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/store"
)

type keyCipher struct{ key []byte }

func (k keyCipher) Encrypt(p string) (string, error) { return credentials.EncryptField(p, k.key) }
func (k keyCipher) Decrypt(e string) (string, error) { return credentials.DecryptField(e, k.key) }

// fakeSession satisfies session.Session through embedding and reports a
// typed password like SSHSession does.
type fakeSession struct {
	session.Session
	id    string
	typed string
}

func (f fakeSession) ID() string           { return f.id }
func (f fakeSession) AuthPassword() string { return f.typed }

func newOfferApp(t *testing.T, conns ...session.ConnectionConfig) *App {
	t.Helper()
	cs, err := store.NewConnectionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	cs.SetPasswordStore(keyCipher{key})
	if err := cs.Save(session.ConnectionStoreData{Connections: conns}); err != nil {
		t.Fatal(err)
	}
	passwordOffersMu.Lock()
	passwordOffers = map[string]pendingPasswordOffer{}
	passwordOffersMu.Unlock()
	return &App{connectionStore: cs}
}

func savedPassword(t *testing.T, a *App, id string) session.ConnectionConfig {
	t.Helper()
	data, err := a.connectionStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range data.Connections {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("connection %s not found", id)
	return session.ConnectionConfig{}
}

func pending(sessionID string) bool {
	passwordOffersMu.Lock()
	defer passwordOffersMu.Unlock()
	_, ok := passwordOffers[sessionID]
	return ok
}

func TestPasswordOfferAfterTypedLogin(t *testing.T) {
	a := newOfferApp(t, session.ConnectionConfig{ID: "c1", Type: "ssh", AuthType: "password", Host: "h", User: "u"})
	a.maybeOfferPasswordSave(fakeSession{id: "s1", typed: "typed-pw"}, session.ConnectionConfig{ID: "c1", Type: "ssh"})
	if !pending("s1") {
		t.Fatal("expected an offer after a typed password login")
	}
	if err := a.ResolvePasswordOffer("s1", "save"); err != nil {
		t.Fatal(err)
	}
	if got := savedPassword(t, a, "c1").Password; got != "typed-pw" {
		t.Fatalf("saved password = %q", got)
	}
	if pending("s1") {
		t.Fatal("offer not cleared")
	}
}

func TestPasswordOfferOnlyWhenDifferent(t *testing.T) {
	a := newOfferApp(t, session.ConnectionConfig{ID: "c1", Type: "ssh", AuthType: "password", Password: "same"})
	a.maybeOfferPasswordSave(fakeSession{id: "s1"}, session.ConnectionConfig{ID: "c1", Type: "ssh", Password: "same"})
	if pending("s1") {
		t.Fatal("no offer expected when the saved password was used")
	}
	// Saved one failed, a new one was typed: offer to update.
	a.maybeOfferPasswordSave(fakeSession{id: "s2", typed: "new"}, session.ConnectionConfig{ID: "c1", Type: "ssh", Password: "same"})
	if !pending("s2") {
		t.Fatal("expected an update offer")
	}
}

func TestPasswordOfferNeverAndNo(t *testing.T) {
	a := newOfferApp(t, session.ConnectionConfig{ID: "c1", Type: "sftp", AuthType: "password", Password: "old"})
	a.maybeOfferPasswordSave(fakeSession{id: "s1"}, session.ConnectionConfig{ID: "c1", Type: "sftp", Password: "new"})
	if err := a.ResolvePasswordOffer("s1", "no"); err != nil {
		t.Fatal(err)
	}
	if got := savedPassword(t, a, "c1").Password; got != "old" {
		t.Fatalf("'no' changed the password to %q", got)
	}
	a.maybeOfferPasswordSave(fakeSession{id: "s2"}, session.ConnectionConfig{ID: "c1", Type: "sftp", Password: "new"})
	if err := a.ResolvePasswordOffer("s2", "never"); err != nil {
		t.Fatal(err)
	}
	c := savedPassword(t, a, "c1")
	if !c.PasswordSaveNever || c.Password != "old" {
		t.Fatalf("never: %+v", c)
	}
	a.maybeOfferPasswordSave(fakeSession{id: "s3"}, session.ConnectionConfig{ID: "c1", Type: "sftp", Password: "new"})
	if pending("s3") {
		t.Fatal("offered despite 'never'")
	}
}

func TestPasswordSaveOnSuccessSkipsTheQuestion(t *testing.T) {
	a := newOfferApp(t, session.ConnectionConfig{ID: "c1", Type: "ssh", AuthType: "password", PasswordSaveNever: true})
	a.maybeOfferPasswordSave(fakeSession{id: "s1"}, session.ConnectionConfig{ID: "c1", Type: "ssh", Password: "pw", SavePasswordOnSuccess: true})
	if pending("s1") {
		t.Fatal("no question expected with 'Save & connect'")
	}
	c := savedPassword(t, a, "c1")
	if c.Password != "pw" || c.PasswordSaveNever || c.SavePasswordOnSuccess {
		t.Fatalf("auto-save result: %+v", c)
	}
}

func TestPasswordOfferScope(t *testing.T) {
	a := newOfferApp(t,
		session.ConnectionConfig{ID: "k", Type: "ssh", AuthType: "key"},
		session.ConnectionConfig{ID: "i", Type: "ssh", AuthType: "identity"},
		session.ConnectionConfig{ID: "r", Type: "rdp", AuthType: "password"},
	)
	a.maybeOfferPasswordSave(fakeSession{id: "s1", typed: "x"}, session.ConnectionConfig{ID: "k", Type: "ssh"})
	a.maybeOfferPasswordSave(fakeSession{id: "s2", typed: "x"}, session.ConnectionConfig{ID: "i", Type: "ssh", AuthType: "password"})
	a.maybeOfferPasswordSave(fakeSession{id: "s3"}, session.ConnectionConfig{ID: "r", Type: "rdp", Password: "x"})
	a.maybeOfferPasswordSave(fakeSession{id: "s4", typed: "x"}, session.ConnectionConfig{ID: "quick", Type: "ssh"})
	for _, s := range []string{"s1", "s2", "s3", "s4"} {
		if pending(s) {
			t.Errorf("%s: unexpected offer", s)
		}
	}
}
