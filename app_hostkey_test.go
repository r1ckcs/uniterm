package main

import (
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/session"
)

// pendingID waits for the dialog prompt to register and returns its id.
func pendingID(t *testing.T) string {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		hostKeyPromptsMu.Lock()
		for id := range hostKeyPrompts {
			hostKeyPromptsMu.Unlock()
			return id
		}
		hostKeyPromptsMu.Unlock()
	}
	t.Fatal("no pending prompt")
	return ""
}

// Only ResolveHostKeyChanged answers a changed-key dialog, so a frontend
// that only knows the unknown-key dialog can never replace a key.
func TestHostKeyChangedDialogNeedsItsBinding(t *testing.T) {
	a := &App{}
	done := make(chan session.HostKeyAnswer, 1)
	go func() {
		ans, _ := a.hostKeyDialogPrompt(session.HostKeyPrompt{Host: "srv:22", Changed: true, CanReplace: true})
		done <- ans
	}()
	id := pendingID(t)
	if err := a.ResolveHostKeyPrompt(id, true); err == nil {
		t.Fatal("the unknown-key binding answered a changed-key dialog")
	}
	if err := a.ResolveHostKeyChanged(id, true); err != nil {
		t.Fatal(err)
	}
	if ans := <-done; ans != session.HostKeyReplace {
		t.Fatalf("answer = %v, want Replace", ans)
	}

	go func() {
		ans, _ := a.hostKeyDialogPrompt(session.HostKeyPrompt{Host: "new:22"})
		done <- ans
	}()
	id = pendingID(t)
	if err := a.ResolveHostKeyChanged(id, true); err == nil {
		t.Fatal("the changed-key binding answered an unknown-key dialog")
	}
	if err := a.ResolveHostKeyPrompt(id, true); err != nil {
		t.Fatal(err)
	}
	if ans := <-done; ans != session.HostKeyTrust {
		t.Fatalf("answer = %v, want Trust", ans)
	}
}

// A changed key that cannot be replaced is a notice: no wait, rejected.
func TestHostKeyChangedNoticeDoesNotBlock(t *testing.T) {
	a := &App{}
	start := time.Now()
	ans, err := a.hostKeyDialogPrompt(session.HostKeyPrompt{Host: "srv:22", Changed: true, Blocker: "pattern"})
	if err != nil || ans != session.HostKeyReject || time.Since(start) > time.Second {
		t.Fatalf("notice: %v %v after %v", ans, err, time.Since(start))
	}
	hostKeyPromptsMu.Lock()
	n := len(hostKeyPrompts)
	hostKeyPromptsMu.Unlock()
	if n != 0 {
		t.Fatalf("notice left %d pending prompts", n)
	}
}
