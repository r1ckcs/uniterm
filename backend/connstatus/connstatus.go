// Package connstatus tells the start screen whether each saved connection
// answers: a plain TCP connect to the connection's own host and port, closed
// as soon as it succeeds. No banner is read, nothing is sent, no credential
// is used, and nothing outside the saved connections is ever dialed.
// Connections behind a jump host or proxy are never dialed directly.
// See CONTRATO-status.md.
package connstatus

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ys-ll/uniterm/backend/session"
)

// States.
const (
	StateOK        = "ok"
	StateDown      = "down"
	StateJump      = "jump"
	StateUnchecked = "unchecked"
)

// Reasons for StateUnchecked.
const (
	ReasonDisabled = "disabled"
	ReasonPending  = "pending"
	ReasonType     = "type"
	ReasonHost     = "host"
)

// Status is one connection's reachability.
type Status struct {
	State     string     `json:"state"`
	LatencyMs *int64     `json:"latencyMs,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

// Defaults (variables for tests).
var (
	Interval    = 60 * time.Second
	Timeout     = 3 * time.Second
	Concurrency = 16
	MaxBackoff  = 10 * time.Minute
	MinManual   = 10 * time.Second
)

// defaultPorts mirrors defaultPort in frontend/src/utils/connectionTypes.ts.
var defaultPorts = map[string]int{
	"ssh": 22, "telnet": 23, "mosh": 22, "tcp": 23, "sftp": 22, "scp": 22,
	"ftp": 21, "smb": 445, "rdp": 3389, "vnc": 5900, "spice": 5900,
	"x11-desktop": 22, "redis": 6379, "mongodb": 27017, "elasticsearch": 9200,
}

var dbPorts = map[string]int{
	"mysql": 3306, "postgres": 5432, "oracle": 1521, "sqlserver": 1433, "rqlite": 4001,
}

// Target is what a connection resolves to: an address to dial, or a fixed
// state when it must not be dialed.
type Target struct {
	Addr   string // host:port; empty when State is set
	State  string
	Reason string
}

// TargetOf decides how c is checked. proxyActive reports whether a saved
// proxy id is honored on connect (a disabled one dials direct). An unknown
// proxy id is treated as a proxy too: never dial what may be meant to go
// through one.
func TargetOf(c session.ConnectionConfig, proxyActive func(id string) bool) Target {
	port, ok := defaultPorts[c.Type]
	if c.Type == "database" {
		port, ok = dbPorts[c.DBType]
	}
	if !ok || (c.Type == "redis" && c.RedisMode == "sentinel") {
		return Target{State: StateUnchecked, Reason: ReasonType}
	}
	if c.TunnelSSHConnID != "" {
		return Target{State: StateJump}
	}
	if c.ProxyId != "" && (c.ProxyId == session.ProxyIDSystem || proxyActive == nil || proxyActive(c.ProxyId)) {
		return Target{State: StateJump}
	}
	if c.Port != 0 {
		port = c.Port
	}
	host := strings.TrimSpace(c.Host)
	if port < 1 || port > 65535 || !validHost(host) {
		return Target{State: StateUnchecked, Reason: ReasonHost}
	}
	return Target{Addr: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))}
}

// validHost accepts a hostname or an IP literal, nothing that looks like a
// URL, a list or a host:port.
func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
		return !ip.IsUnspecified() && !ip.IsMulticast()
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
		default:
			return false
		}
	}
	return !strings.HasPrefix(h, "-") && !strings.HasPrefix(h, ".")
}

// Dialer opens a TCP connection (net.Dialer.DialContext in production).
type Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

// Options wires a Monitor.
type Options struct {
	// Connections lists the saved connections (read at each round).
	Connections func() ([]session.ConnectionConfig, error)
	// Proxies maps each saved proxy id to whether it is honored on connect
	// (read once per round).
	Proxies func() map[string]bool
	// Enabled reads the "check reachability" setting.
	Enabled func() bool
	// Emit sends the full map after a change.
	Emit func(map[string]Status)
	Dial Dialer
	Now  func() time.Time
}

type entry struct {
	status   Status
	fails    int
	next     time.Time // earliest next check
	lastAddr string
}

// Monitor keeps the reachability of every saved connection while active.
type Monitor struct {
	opt Options

	mu      sync.Mutex
	entries map[string]*entry
	active  bool
	running bool
	lastRun time.Time
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	closed  bool
}

// New starts a Monitor (idle until SetActive(true)).
func New(opt Options) *Monitor {
	if opt.Dial == nil {
		d := &net.Dialer{}
		opt.Dial = d.DialContext
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	m := &Monitor{opt: opt, entries: map[string]*entry{}, wake: make(chan struct{}, 1),
		stop: make(chan struct{}), done: make(chan struct{})}
	go m.loop()
	return m
}

// Close stops the monitor and waits for a running round.
func (m *Monitor) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.done
		return
	}
	m.closed = true
	close(m.stop)
	m.mu.Unlock()
	<-m.done
}

// SetActive starts (true) or pauses (false) the checks: the start screen is
// visible and the window has focus.
func (m *Monitor) SetActive(active bool) {
	m.mu.Lock()
	m.active = active
	m.mu.Unlock()
	if active {
		m.poke()
	}
}

// CheckNow runs a round ignoring backoff, unless one ran under MinManual ago.
func (m *Monitor) CheckNow() {
	m.mu.Lock()
	if m.opt.Now().Sub(m.lastRun) < MinManual {
		m.mu.Unlock()
		return
	}
	for _, e := range m.entries {
		e.next = time.Time{}
	}
	m.mu.Unlock()
	m.poke()
}

// Changed is called when connections or settings change: the map is
// rebuilt at once (new ids, jump/type states, disabled) and new targets are
// checked if active.
func (m *Monitor) Changed() {
	m.poke()
}

func (m *Monitor) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Get returns the full map.
func (m *Monitor) Get() map[string]Status {
	m.mu.Lock()
	empty := len(m.entries) == 0
	m.mu.Unlock()
	if empty {
		m.sync()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Monitor) snapshotLocked() map[string]Status {
	out := make(map[string]Status, len(m.entries))
	for id, e := range m.entries {
		out[id] = e.status
	}
	return out
}

func (m *Monitor) loop() {
	defer close(m.done)
	t := time.NewTimer(Interval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
		case <-t.C:
		}
		m.round()
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(m.untilNext())
	}
}

// untilNext is how long to sleep: until the earliest due entry, at most
// Interval.
func (m *Monitor) untilNext() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.opt.Now()
	d := Interval
	for _, e := range m.entries {
		if e.lastAddr == "" || e.next.IsZero() {
			continue
		}
		if w := e.next.Sub(now); w < d {
			d = w
		}
	}
	if d < time.Second {
		d = time.Second
	}
	return d
}

// sync rebuilds the map from the saved connections and returns the
// addresses due for a check (each one once) and whether anything changed.
func (m *Monitor) sync() (due []string, changed bool) {
	var conns []session.ConnectionConfig
	if m.opt.Connections != nil {
		if cs, err := m.opt.Connections(); err == nil {
			conns = cs
		}
	}
	enabled := m.opt.Enabled == nil || m.opt.Enabled()
	var proxies map[string]bool
	if m.opt.Proxies != nil {
		proxies = m.opt.Proxies()
	}
	proxyActive := func(id string) bool {
		on, known := proxies[id]
		return on || !known
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.opt.Now()
	seen := map[string]bool{}
	dueSet := map[string]bool{}
	for _, c := range conns {
		if c.ID == "" || c.Type == "workspace" {
			continue
		}
		seen[c.ID] = true
		e := m.entries[c.ID]
		if e == nil {
			e = &entry{status: Status{State: StateUnchecked, Reason: ReasonPending}}
			m.entries[c.ID] = e
			changed = true
		}
		tg := TargetOf(c, proxyActive)
		var want Status
		switch {
		case tg.Addr == "":
			want = Status{State: tg.State, Reason: tg.Reason}
			e.lastAddr, e.fails, e.next = "", 0, time.Time{}
		case !enabled:
			want = Status{State: StateUnchecked, Reason: ReasonDisabled}
			e.lastAddr, e.fails, e.next = "", 0, time.Time{}
		default:
			if e.lastAddr != tg.Addr {
				// New or edited target: forget the old result.
				e.lastAddr, e.fails, e.next = tg.Addr, 0, time.Time{}
				want = Status{State: StateUnchecked, Reason: ReasonPending}
			} else {
				want = e.status
			}
			if m.active && !now.Before(e.next) {
				dueSet[tg.Addr] = true
			}
		}
		if !sameStatus(e.status, want) {
			e.status = want
			changed = true
		}
	}
	for id := range m.entries {
		if !seen[id] {
			delete(m.entries, id)
			changed = true
		}
	}
	for a := range dueSet {
		due = append(due, a)
	}
	return due, changed
}

func (m *Monitor) round() {
	due, changed := m.sync()
	if len(due) > 0 {
		m.mu.Lock()
		m.lastRun = m.opt.Now()
		m.mu.Unlock()
		results := m.probeAll(due)
		if m.apply(results) {
			changed = true
		}
	}
	if changed && m.opt.Emit != nil {
		m.mu.Lock()
		snap := m.snapshotLocked()
		m.mu.Unlock()
		m.opt.Emit(snap)
	}
}

type result struct {
	at      time.Time
	ok      bool
	latency time.Duration
}

// probeAll dials each address once, at most Concurrency at a time. A
// closing monitor cancels the dials in flight.
func (m *Monitor) probeAll(addrs []string) map[string]result {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-m.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	n := Concurrency
	if n < 1 {
		n = 1
	}
	sem := make(chan struct{}, n)
	var mu sync.Mutex
	out := make(map[string]result, len(addrs))
	var wg sync.WaitGroup
	for _, a := range addrs {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			r := m.probe(ctx, a)
			mu.Lock()
			out[a] = r
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func (m *Monitor) probe(ctx context.Context, addr string) result {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	start := m.opt.Now()
	conn, err := m.opt.Dial(ctx, "tcp", addr)
	if err != nil {
		return result{at: start}
	}
	lat := m.opt.Now().Sub(start)
	_ = conn.Close()
	return result{at: start, ok: true, latency: lat}
}

// apply stores the results on every connection pointing at each address.
func (m *Monitor) apply(results map[string]result) (changed bool) {
	select {
	case <-m.stop:
		return false
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		r, ok := results[e.lastAddr]
		if e.lastAddr == "" || !ok {
			continue
		}
		at := r.at
		prev := e.status
		changed = true // checkedAt (and latency) move on every answer
		if r.ok {
			ms := r.latency.Milliseconds()
			e.status = Status{State: StateOK, LatencyMs: &ms, CheckedAt: &at}
			e.fails = 0
			e.next = at.Add(Interval)
		} else {
			since := at
			if prev.State == StateDown && prev.Since != nil {
				since = *prev.Since
			}
			e.status = Status{State: StateDown, Since: &since, CheckedAt: &at}
			e.fails++
			e.next = at.Add(backoff(e.fails))
		}
	}
	return changed
}

// backoff: 1, 2, 4, 8 minutes after consecutive failures, capped.
func backoff(fails int) time.Duration {
	d := Interval
	for i := 1; i < fails && d < MaxBackoff; i++ {
		d *= 2
	}
	if d > MaxBackoff {
		d = MaxBackoff
	}
	return d
}

func sameStatus(a, b Status) bool {
	return a.State == b.State && a.Reason == b.Reason &&
		eqTime(a.Since, b.Since) && eqTime(a.CheckedAt, b.CheckedAt) && eqInt(a.LatencyMs, b.LatencyMs)
}

func eqTime(a, b *time.Time) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
}

func eqInt(a, b *int64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
