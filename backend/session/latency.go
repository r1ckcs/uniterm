package session

import "net"

// What a session's latency measures: the first TCP leg, which ends at the
// host itself (direct), at the proxy, or at the jump host.
const (
	ViaDirect = "direct"
	ViaProxy  = "proxy"
	ViaJump   = "jump"
)

// FirstHop is the socket of a session's first TCP leg. The kernel keeps an
// RTT estimate for it (backend/tcprtt); nothing is ever sent to measure it.
type FirstHop struct {
	Conn net.Conn
	Via  string
}

// SetFirstHop records the jump host tunnel's socket before Connect: the
// session itself only dials 127.0.0.1, whose RTT would say nothing.
func (s *SSHSession) SetFirstHop(h FirstHop) {
	s.mu.Lock()
	s.presetHop = &h
	s.mu.Unlock()
}

// FirstHop returns the first TCP leg of a connected session. Channel clones
// share their source's transport, hence its leg.
func (s *SSHSession) FirstHop() (FirstHop, bool) {
	s.mu.RLock()
	ref := s.clientRef
	s.mu.RUnlock()
	if ref == nil || ref.hop.Conn == nil {
		return FirstHop{}, false
	}
	return ref.hop, true
}
