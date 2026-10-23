// Package latency feeds the terminal footer with each visible session's
// round-trip time: the kernel's smoothed RTT of the session's first TCP leg
// (backend/tcprtt). Nothing is sent and nothing is dialed to measure it, so
// it cannot disturb the SSH keepalive or a legacy device.
// See CONTRATO-latencia.md.
package latency

import (
	"errors"
	"math"
	"net"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/tcprtt"
)

// States.
const (
	StateOK          = "ok"
	StateUnavailable = "unavailable"
)

// Reasons for StateUnavailable.
const (
	ReasonType       = "type"       // protocol without a TCP RTT (telnet, serial, local…)
	ReasonMosh       = "mosh"       // UDP transport; its RTT is not exposed
	ReasonPlatform   = "platform"   // OS without a TCP RTT query
	ReasonConnecting = "connecting" // not connected yet
	ReasonClosed     = "closed"     // session gone or socket closed
	ReasonError      = "error"      // the query failed
)

// Interval is the shortest time between two samples of one session.
const Interval = 5 * time.Second

// MaxWatch caps the watched sessions (visible tabs and split panes).
const MaxWatch = 32

// Sample is one session's latency.
type Sample struct {
	State  string  `json:"state"`
	RttMs  float64 `json:"rttMs"`            // ok only; 0.1 ms resolution
	Via    string  `json:"via,omitempty"`    // ok only: direct | proxy | jump
	At     string  `json:"at,omitempty"`     // RFC 3339 of the sample
	Reason string  `json:"reason,omitempty"` // unavailable only
}

// Options wires the monitor to the app.
type Options struct {
	// Lookup returns the session's first TCP leg, or a reason when it has
	// none to measure.
	Lookup func(id string) (session.FirstHop, string)
	// Emit receives every watched session at each round.
	Emit func(map[string]Sample)
	RTT  func(net.Conn) (time.Duration, error) // default tcprtt.Of
	Now  func() time.Time
}

type entry struct {
	sample Sample
	taken  time.Time
}

// Monitor samples the watched sessions every Interval, only while some are
// watched.
type Monitor struct {
	o        Options
	interval time.Duration

	mu      sync.Mutex
	watched []string
	cache   map[string]entry

	poke chan struct{}
	quit chan struct{}
	once sync.Once
}

func New(o Options) *Monitor { return newMonitor(o, Interval) }

func newMonitor(o Options, interval time.Duration) *Monitor {
	if o.RTT == nil {
		o.RTT = tcprtt.Of
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := &Monitor{
		o:        o,
		interval: interval,
		cache:    map[string]entry{},
		poke:     make(chan struct{}, 1),
		quit:     make(chan struct{}),
	}
	go m.loop()
	return m
}

// Close stops the loop.
func (m *Monitor) Close() { m.once.Do(func() { close(m.quit) }) }

// Watch replaces the watched set (the visible terminal tabs; empty stops
// sampling) and returns their current samples. A session sampled less than
// Interval ago returns its cached sample.
func (m *Monitor) Watch(ids []string) map[string]Sample {
	seen := map[string]bool{}
	var w []string
	for _, id := range ids {
		if id == "" || seen[id] || len(w) == MaxWatch {
			continue
		}
		seen[id] = true
		w = append(w, id)
	}
	m.mu.Lock()
	m.watched = w
	for id := range m.cache {
		if !seen[id] {
			delete(m.cache, id)
		}
	}
	out := m.sampleLocked(false)
	m.mu.Unlock()
	select {
	case m.poke <- struct{}{}:
	default:
	}
	return out
}

func (m *Monitor) loop() {
	t := time.NewTimer(m.interval)
	defer t.Stop()
	for {
		select {
		case <-m.quit:
			return
		case <-m.poke:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(m.interval)
		case <-t.C:
			m.mu.Lock()
			var out map[string]Sample
			if len(m.watched) > 0 {
				out = m.sampleLocked(true)
			}
			m.mu.Unlock()
			if out != nil && m.o.Emit != nil {
				m.o.Emit(out)
			}
			t.Reset(m.interval)
		}
	}
}

// sampleLocked samples the watched sessions. Without force, a sample younger
// than the interval is reused.
func (m *Monitor) sampleLocked(force bool) map[string]Sample {
	now := m.o.Now()
	out := make(map[string]Sample, len(m.watched))
	for _, id := range m.watched {
		if e, ok := m.cache[id]; ok && !force && now.Sub(e.taken) < m.interval {
			out[id] = e.sample
			continue
		}
		s := m.sample(id, now)
		m.cache[id] = entry{sample: s, taken: now}
		out[id] = s
	}
	return out
}

func (m *Monitor) sample(id string, now time.Time) Sample {
	at := now.UTC().Format(time.RFC3339)
	hop, reason := m.o.Lookup(id)
	if reason == "" && hop.Conn == nil {
		reason = ReasonClosed
	}
	if reason != "" {
		return Sample{State: StateUnavailable, Reason: reason, At: at}
	}
	rtt, err := m.o.RTT(hop.Conn)
	if err != nil {
		return Sample{State: StateUnavailable, Reason: reasonOf(err), At: at}
	}
	ms := math.Round(float64(rtt)/float64(time.Millisecond)*10) / 10
	return Sample{State: StateOK, RttMs: ms, Via: hop.Via, At: at}
}

func reasonOf(err error) string {
	switch {
	case errors.Is(err, tcprtt.ErrUnsupported):
		return ReasonPlatform
	case errors.Is(err, tcprtt.ErrNotTCP):
		return ReasonType
	case errors.Is(err, net.ErrClosed):
		return ReasonClosed
	}
	return ReasonError
}
