package main

import (
	"testing"

	"github.com/ys-ll/uniterm/backend/latency"
	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/tcprtt"
)

func TestLatencyHopReasons(t *testing.T) {
	a := &App{sessionManager: session.NewSessionManager()}
	a.sessionManager.Add(session.NewSSHSession("ssh"))
	a.sessionManager.Add(session.NewMoshSession("mosh"))
	a.sessionManager.Add(session.NewTelnetSession("telnet"))

	sshWant := latency.ReasonClosed // never connected: disconnected
	if !tcprtt.Supported {
		sshWant = latency.ReasonPlatform
	}
	for id, want := range map[string]string{
		"ssh":    sshWant,
		"mosh":   latency.ReasonMosh,
		"telnet": latency.ReasonType,
		"gone":   latency.ReasonClosed,
	} {
		if _, got := a.latencyHop(id); got != want {
			t.Errorf("%s: reason %q, want %q", id, got, want)
		}
	}
	if got := a.SessionLatencyWatch([]string{"ssh"}); len(got) != 0 {
		t.Fatalf("watch before init = %+v", got)
	}
	a.initLatency()
	defer a.latency.Close()
	if got := a.SessionLatencyWatch([]string{"telnet"})["telnet"]; got.State != latency.StateUnavailable || got.Reason != latency.ReasonType {
		t.Fatalf("telnet sample = %+v", got)
	}
}
