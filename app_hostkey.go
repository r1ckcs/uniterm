package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/session"
)

// hostKeyPromptTimeout bounds how long a dial waits for the user to answer
// the unknown-host-key dialog before rejecting.
const hostKeyPromptTimeout = 120 * time.Second

var (
	hostKeyPromptsMu sync.Mutex
	hostKeyPrompts   = map[string]chan bool{}
)

// hostKeyDialogPrompt backs session.SetHostKeyPrompter for dial paths that
// have no terminal (SFTP, SCP, monitor, tunnels, containers): it emits
// ssh:hostkey-prompt and blocks for ResolveHostKeyPrompt or the timeout.
func (a *App) hostKeyDialogPrompt(p session.HostKeyPrompt) (bool, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return false, err
	}
	id := hex.EncodeToString(b[:])
	ch := make(chan bool, 1)
	hostKeyPromptsMu.Lock()
	hostKeyPrompts[id] = ch
	hostKeyPromptsMu.Unlock()
	defer func() {
		hostKeyPromptsMu.Lock()
		delete(hostKeyPrompts, id)
		hostKeyPromptsMu.Unlock()
	}()

	a.emit("ssh:hostkey-prompt", map[string]interface{}{
		"id":                 id,
		"host":               p.Host,
		"keyType":            p.KeyType,
		"fingerprint":        p.Fingerprint,
		"otherKeyTypesKnown": p.OtherKeyTypesKnown,
	})
	select {
	case ok := <-ch:
		return ok, nil
	case <-time.After(hostKeyPromptTimeout):
		return false, fmt.Errorf("no answer within %s", hostKeyPromptTimeout)
	}
}

// ResolveHostKeyPrompt is the Wails binding the frontend calls when the user
// answers the unknown-host-key dialog.
func (a *App) ResolveHostKeyPrompt(id string, accept bool) error {
	hostKeyPromptsMu.Lock()
	ch, ok := hostKeyPrompts[id]
	hostKeyPromptsMu.Unlock()
	if !ok {
		return fmt.Errorf("no pending host key prompt %s", id)
	}
	select {
	case ch <- accept:
	default:
	}
	return nil
}
