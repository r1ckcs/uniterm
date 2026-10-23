package main

import (
	"github.com/ys-ll/uniterm/backend/latency"
	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/tcprtt"
)

// initLatency starts the terminal footer's latency monitor, idle until the
// frontend watches a tab. See CONTRATO-latencia.md.
func (a *App) initLatency() {
	if a.latency != nil {
		return
	}
	a.latency = latency.New(latency.Options{
		Lookup: a.latencyHop,
		Emit:   func(m map[string]latency.Sample) { a.emit("session:latency", m) },
	})
}

// latencyHop finds a session's first TCP leg, or why it has none.
func (a *App) latencyHop(id string) (session.FirstHop, string) {
	if a.sessionManager == nil {
		return session.FirstHop{}, latency.ReasonClosed
	}
	s, ok := a.sessionManager.Get(id)
	if !ok {
		return session.FirstHop{}, latency.ReasonClosed
	}
	switch s := s.(type) {
	case *session.SSHSession:
		if !tcprtt.Supported {
			return session.FirstHop{}, latency.ReasonPlatform
		}
		switch s.Status() {
		case session.StatusConnected:
		case session.StatusConnecting:
			return session.FirstHop{}, latency.ReasonConnecting
		default:
			return session.FirstHop{}, latency.ReasonClosed
		}
		if hop, ok := s.FirstHop(); ok {
			return hop, ""
		}
		return session.FirstHop{}, latency.ReasonConnecting
	case *session.MoshSession:
		return session.FirstHop{}, latency.ReasonMosh
	}
	return session.FirstHop{}, latency.ReasonType
}

// SessionLatencyWatch sets the sessions whose latency the footer shows (the
// visible terminal tabs, window focused; empty stops) and returns their
// current samples. Updates follow on "session:latency" every 5 s.
func (a *App) SessionLatencyWatch(ids []string) map[string]latency.Sample {
	if a.latency == nil {
		return map[string]latency.Sample{}
	}
	return a.latency.Watch(ids)
}
