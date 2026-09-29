package main

import (
	"fmt"
	stdsync "sync"
	"time"

	"github.com/ys-ll/uniterm/backend/log"
	"github.com/ys-ll/uniterm/backend/session"
)

// MobaXterm-style password saving for SSH-family connections: the password
// is typed at login (terminal "Password:" prompt or the credential dialog)
// and, only after the login succeeds, the user is asked whether to save it —
// or to replace a saved one that no longer works. The typed password stays
// in this process; the webview only learns that an offer exists.

// passwordOfferTypes are the connection types that take part.
var passwordOfferTypes = map[string]bool{"ssh": true, "sftp": true, "scp": true, "mosh": true}

const passwordOfferTTL = 10 * time.Minute

type pendingPasswordOffer struct {
	connID   string
	password string
	created  time.Time
}

// PasswordOffer is what the frontend receives (no secret).
type PasswordOffer struct {
	SessionID    string `json:"sessionId"`
	ConnectionID string `json:"connectionId"`
	Name         string `json:"name"`
	User         string `json:"user"`
	Host         string `json:"host"`
	// Update is true when a different password is already saved (the saved
	// one failed and the user typed a new one).
	Update bool `json:"update"`
}

var (
	passwordOffersMu stdsync.Mutex
	passwordOffers   = map[string]pendingPasswordOffer{}
)

// passwordTyper is implemented by sessions that can report a password typed
// during login (SSHSession).
type passwordTyper interface{ AuthPassword() string }

// maybeOfferPasswordSave runs after a successful Connect. It compares the
// password the login actually used with the saved one and, when they differ,
// either saves right away (the user chose "Save & connect" for this attempt)
// or asks the frontend.
func (a *App) maybeOfferPasswordSave(s session.Session, config session.ConnectionConfig) {
	if a.connectionStore == nil || config.ID == "" || !passwordOfferTypes[config.Type] {
		return
	}
	used := config.Password
	if pt, ok := s.(passwordTyper); ok {
		if typed := pt.AuthPassword(); typed != "" {
			used = typed
		}
	}
	if used == "" {
		return
	}
	data, err := a.connectionStore.Load()
	if err != nil {
		return
	}
	var saved *session.ConnectionConfig
	for i := range data.Connections {
		if data.Connections[i].ID == config.ID {
			saved = &data.Connections[i]
			break
		}
	}
	// Quick connects (not in the store) and non-password auth are out of scope.
	if saved == nil || (saved.AuthType != "" && saved.AuthType != "password") {
		return
	}
	if saved.Password == used {
		return
	}
	if config.SavePasswordOnSuccess {
		if err := a.persistConnectionPassword(config.ID, used, false); err != nil {
			log.Writef("[password-offer] auto-save failed for %s: %v", config.ID, err)
		}
		return
	}
	if saved.PasswordSaveNever {
		return
	}

	passwordOffersMu.Lock()
	now := time.Now()
	for id, o := range passwordOffers {
		if now.Sub(o.created) > passwordOfferTTL {
			delete(passwordOffers, id)
		}
	}
	passwordOffers[s.ID()] = pendingPasswordOffer{connID: config.ID, password: used, created: now}
	passwordOffersMu.Unlock()

	a.emit("session:password-offer", PasswordOffer{
		SessionID:    s.ID(),
		ConnectionID: saved.ID,
		Name:         saved.Name,
		User:         saved.User,
		Host:         saved.Host,
		Update:       saved.Password != "",
	})
}

// ResolvePasswordOffer answers a pending offer: "save" stores the typed
// password, "never" stops asking for this connection, anything else drops
// the offer.
func (a *App) ResolvePasswordOffer(sessionID, action string) error {
	passwordOffersMu.Lock()
	offer, ok := passwordOffers[sessionID]
	delete(passwordOffers, sessionID)
	passwordOffersMu.Unlock()
	if !ok {
		return fmt.Errorf("no pending password offer for this session")
	}
	switch action {
	case "save":
		return a.persistConnectionPassword(offer.connID, offer.password, false)
	case "never":
		return a.persistConnectionPassword(offer.connID, "", true)
	default:
		return nil
	}
}

// persistConnectionPassword saves a connection's password (encrypted by the
// store) or, with never=true, marks it as "never ask again" and keeps the
// saved password untouched.
func (a *App) persistConnectionPassword(connID, password string, never bool) error {
	data, err := a.connectionStore.Load()
	if err != nil {
		return err
	}
	for i := range data.Connections {
		c := &data.Connections[i]
		if c.ID != connID {
			continue
		}
		if never {
			c.PasswordSaveNever = true
		} else {
			c.Password = password
			c.PasswordSaveNever = false
		}
		return a.SaveConnections(data)
	}
	return fmt.Errorf("connection %s not found", connID)
}
