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
// a host key dialog before rejecting.
const hostKeyPromptTimeout = 120 * time.Second

// hostKeyPending is one dialog waiting for its answer. changed selects the
// binding that may answer it: ResolveHostKeyPrompt (unknown key, Trust) or
// ResolveHostKeyChanged (changed key, Replace). A frontend that only knows
// the unknown-key dialog can therefore never replace a changed key.
type hostKeyPending struct {
	ch      chan session.HostKeyAnswer
	changed bool
}

var (
	hostKeyPromptsMu sync.Mutex
	hostKeyPrompts   = map[string]hostKeyPending{}
)

// hostKeyDialogPrompt backs session.SetHostKeyPrompter for dial paths that
// have no terminal (SFTP, SCP, monitor, tunnels, containers): it emits
// ssh:hostkey-prompt (unknown key) or ssh:hostkey-changed (changed key) and
// blocks for the answer or the timeout; on Replace the same dial goes on and
// connects. A changed key the app cannot replace (a pattern line) is a
// notice: emitted without an id and rejected at once.
func (a *App) hostKeyDialogPrompt(p session.HostKeyPrompt) (session.HostKeyAnswer, error) {
	if p.Changed && !p.CanReplace {
		a.emit("ssh:hostkey-changed", hostKeyChangedPayload("", p))
		return session.HostKeyReject, nil
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return session.HostKeyReject, err
	}
	id := hex.EncodeToString(b[:])
	ch := make(chan session.HostKeyAnswer, 1)
	hostKeyPromptsMu.Lock()
	hostKeyPrompts[id] = hostKeyPending{ch: ch, changed: p.Changed}
	hostKeyPromptsMu.Unlock()
	defer func() {
		hostKeyPromptsMu.Lock()
		delete(hostKeyPrompts, id)
		hostKeyPromptsMu.Unlock()
	}()

	if p.Changed {
		a.emit("ssh:hostkey-changed", hostKeyChangedPayload(id, p))
	} else {
		a.emit("ssh:hostkey-prompt", map[string]interface{}{
			"id":                 id,
			"host":               p.Host,
			"keyType":            p.KeyType,
			"fingerprint":        p.Fingerprint,
			"otherKeyTypesKnown": p.OtherKeyTypesKnown,
			"keyBits":            p.KeyBits,
			"weak":               p.Weak,
		})
	}
	select {
	case ans := <-ch:
		return ans, nil
	case <-time.After(hostKeyPromptTimeout):
		return session.HostKeyReject, fmt.Errorf("no answer within %s", hostKeyPromptTimeout)
	}
}

func hostKeyChangedPayload(id string, p session.HostKeyPrompt) map[string]interface{} {
	return map[string]interface{}{
		"id":            id,
		"host":          p.Host,
		"keyType":       p.KeyType,
		"fingerprint":   p.Fingerprint,
		"keyBits":       p.KeyBits,
		"weak":          p.Weak,
		"oldKeys":       p.OldKeys,
		"canReplace":    p.CanReplace,
		"blocker":       p.Blocker,
		"userFileKept":  p.UserFileKept,
		"removeCommand": p.RemoveCommand,
	}
}

func resolveHostKey(id string, changed bool, ans session.HostKeyAnswer) error {
	hostKeyPromptsMu.Lock()
	pending, ok := hostKeyPrompts[id]
	hostKeyPromptsMu.Unlock()
	if !ok {
		return fmt.Errorf("no pending host key prompt %s", id)
	}
	if pending.changed != changed {
		return fmt.Errorf("host key prompt %s is answered by the other binding", id)
	}
	select {
	case pending.ch <- ans:
	default:
	}
	return nil
}

// ResolveHostKeyPrompt is the Wails binding the frontend calls when the user
// answers the unknown-host-key dialog.
func (a *App) ResolveHostKeyPrompt(id string, accept bool) error {
	ans := session.HostKeyReject
	if accept {
		ans = session.HostKeyTrust
	}
	return resolveHostKey(id, false, ans)
}

// ResolveHostKeyChanged answers the changed-host-key dialog: replace=true
// only from the explicit "Replace key" action. See CONTRATO-chave-trocada.md.
func (a *App) ResolveHostKeyChanged(id string, replace bool) error {
	ans := session.HostKeyReject
	if replace {
		ans = session.HostKeyReplace
	}
	return resolveHostKey(id, true, ans)
}

// ForgetHostKey removes every trusted key of host:port from the app's
// known_hosts (the connection menu's "Forget host key"). The user's
// ~/.ssh/known_hosts is never edited; the result says if it still has one.
func (a *App) ForgetHostKey(host string, port int) (session.ForgetResult, error) {
	if port == 0 {
		port = 22
	}
	return session.ForgetHostKey(host, port)
}
