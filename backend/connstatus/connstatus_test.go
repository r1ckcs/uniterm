package connstatus

import (
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/session"
)

// listener accepts and counts connections, never writing a byte.
func listener(t *testing.T) (port int, accepted *atomic.Int32, got chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	accepted = &atomic.Int32{}
	got = make(chan []byte, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() {
				defer c.Close()
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				got <- buf[:n]
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, accepted, got
}

// closedPort returns a port nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func ssh(id string, port int) session.ConnectionConfig {
	return session.ConnectionConfig{ID: id, Type: "ssh", Host: "127.0.0.1", Port: port, User: "root", Password: "secret"}
}

type harness struct {
	m     *Monitor
	mu    sync.Mutex
	conns []session.ConnectionConfig
	on    bool
	emits chan map[string]Status
}

func newHarness(t *testing.T, conns []session.ConnectionConfig, dial Dialer) *harness {
	t.Helper()
	h := &harness{conns: conns, on: true, emits: make(chan map[string]Status, 64)}
	h.m = New(Options{
		Connections: func() ([]session.ConnectionConfig, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return append([]session.ConnectionConfig(nil), h.conns...), nil
		},
		Proxies: func() map[string]bool { return map[string]bool{"px-1": true, "px-off": false} },
		Enabled: func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.on
		},
		Emit: func(s map[string]Status) { h.emits <- s },
		Dial: dial,
	})
	t.Cleanup(h.m.Close)
	return h
}

// waitFor returns the first emitted map satisfying ok.
func (h *harness) waitFor(t *testing.T, ok func(map[string]Status) bool) map[string]Status {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-h.emits:
			if ok(s) {
				return s
			}
		case <-deadline:
			t.Fatalf("timeout; now %+v", h.m.Get())
		}
	}
}

func TestOpenPortIsOKAndNothingIsSent(t *testing.T) {
	port, accepted, got := listener(t)
	h := newHarness(t, []session.ConnectionConfig{ssh("a", port)}, nil)
	h.m.SetActive(true)
	s := h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateOK })
	if s["a"].LatencyMs == nil || s["a"].CheckedAt == nil {
		t.Fatalf("ok without latency/checkedAt: %+v", s["a"])
	}
	select {
	case b := <-got:
		if len(b) != 0 {
			t.Fatalf("probe sent %q", b)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not close the connection")
	}
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d", accepted.Load())
	}
}

func TestClosedPortIsDownWithSince(t *testing.T) {
	h := newHarness(t, []session.ConnectionConfig{ssh("a", closedPort(t))}, nil)
	h.m.SetActive(true)
	s := h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateDown })
	if s["a"].Since == nil {
		t.Fatalf("down without since: %+v", s["a"])
	}
}

func TestTimeout(t *testing.T) {
	old := Timeout
	Timeout = 100 * time.Millisecond
	t.Cleanup(func() { Timeout = old })
	hang := func(ctx context.Context, network, addr string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	h := newHarness(t, []session.ConnectionConfig{ssh("a", 22)}, hang)
	start := time.Now()
	h.m.SetActive(true)
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateDown })
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("timeout took %v", d)
	}
}

func TestJumpHostAndProxyAreNeverDialed(t *testing.T) {
	var dials atomic.Int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return nil, &net.OpError{Op: "dial"}
	}
	viaTunnel := ssh("tunnel", 22)
	viaTunnel.TunnelSSHConnID = "bastion"
	viaProxy := ssh("proxy", 22)
	viaProxy.ProxyId = "px-1"
	viaSystem := ssh("system", 22)
	viaSystem.ProxyId = session.ProxyIDSystem
	viaMissing := ssh("missing", 22)
	viaMissing.ProxyId = "px-gone" // unknown proxy: still not direct
	h := newHarness(t, []session.ConnectionConfig{viaTunnel, viaProxy, viaSystem, viaMissing}, dial)
	h.m.SetActive(true)
	s := h.waitFor(t, func(s map[string]Status) bool { return len(s) == 4 })
	for id, st := range s {
		if st.State != StateJump {
			t.Errorf("%s: %+v, want jump", id, st)
		}
	}
	h.m.CheckNow()
	time.Sleep(200 * time.Millisecond)
	if dials.Load() != 0 {
		t.Fatalf("dialed %d times", dials.Load())
	}
}

func TestDisabledProxyDialsDirect(t *testing.T) {
	port, _, _ := listener(t)
	c := ssh("a", port)
	c.ProxyId = "px-off" // a disabled proxy: the app dials direct, so do we
	h := newHarness(t, []session.ConnectionConfig{c}, nil)
	h.m.SetActive(true)
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateOK })
}

func TestConcurrencyIsLimitedAndTargetsDeduped(t *testing.T) {
	old := Concurrency
	Concurrency = 3
	t.Cleanup(func() { Concurrency = old })
	var cur, peak, total atomic.Int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		total.Add(1)
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		return nil, &net.OpError{Op: "dial"}
	}
	var conns []session.ConnectionConfig
	for i := 0; i < 20; i++ {
		conns = append(conns, ssh("c"+strconv.Itoa(i), 1000+i))
	}
	conns = append(conns, ssh("dup", 1000)) // same host:port as c0
	h := newHarness(t, conns, dial)
	h.m.SetActive(true)
	h.waitFor(t, func(s map[string]Status) bool {
		for _, st := range s {
			if st.State != StateDown {
				return false
			}
		}
		return len(s) == 21
	})
	if peak.Load() > 3 {
		t.Fatalf("peak concurrency %d", peak.Load())
	}
	if total.Load() != 20 {
		t.Fatalf("dialed %d times, want 20 (dup shares c0)", total.Load())
	}
}

func TestBackoffSkipsFailingTargets(t *testing.T) {
	var dials atomic.Int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return nil, &net.OpError{Op: "dial"}
	}
	h := newHarness(t, []session.ConnectionConfig{ssh("a", 22)}, dial)
	h.m.SetActive(true)
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateDown })
	// Reactivating (or a save) does not re-dial before the backoff ends.
	h.m.SetActive(false)
	h.m.SetActive(true)
	h.m.Changed()
	time.Sleep(200 * time.Millisecond)
	if dials.Load() != 1 {
		t.Fatalf("dialed %d times during backoff", dials.Load())
	}
	if backoff(1) != Interval || backoff(2) != 2*Interval || backoff(4) != 8*Interval || backoff(9) != MaxBackoff {
		t.Fatalf("backoff %v %v %v %v", backoff(1), backoff(2), backoff(4), backoff(9))
	}
}

func TestInactiveNeverDials(t *testing.T) {
	var dials atomic.Int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return nil, &net.OpError{Op: "dial"}
	}
	h := newHarness(t, []session.ConnectionConfig{ssh("a", 22)}, dial)
	h.m.Changed()
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].Reason == ReasonPending })
	time.Sleep(100 * time.Millisecond)
	if dials.Load() != 0 {
		t.Fatalf("dialed %d times while inactive", dials.Load())
	}
}

func TestDisabledSettingMarksAllUnchecked(t *testing.T) {
	port, _, _ := listener(t)
	h := newHarness(t, []session.ConnectionConfig{ssh("a", port)}, nil)
	h.m.SetActive(true)
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateOK })
	h.mu.Lock()
	h.on = false
	h.mu.Unlock()
	h.m.Changed()
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].Reason == ReasonDisabled })
}

func TestEditedHostForgetsOldResult(t *testing.T) {
	port, _, _ := listener(t)
	h := newHarness(t, []session.ConnectionConfig{ssh("a", port)}, nil)
	h.m.SetActive(true)
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateOK })
	h.mu.Lock()
	h.conns = []session.ConnectionConfig{ssh("a", closedPort(t)), ssh("b", port)}
	h.mu.Unlock()
	h.m.Changed()
	h.waitFor(t, func(s map[string]Status) bool { return s["a"].State == StateDown && s["b"].State == StateOK })
	h.mu.Lock()
	h.conns = h.conns[1:]
	h.mu.Unlock()
	h.m.Changed()
	h.waitFor(t, func(s map[string]Status) bool { _, ok := s["a"]; return !ok })
}

func TestTargetOf(t *testing.T) {
	cases := []struct {
		c    session.ConnectionConfig
		addr string
		st   string
		why  string
	}{
		{session.ConnectionConfig{Type: "ssh", Host: "h.example"}, "h.example:22", "", ""},
		{session.ConnectionConfig{Type: "telnet", Host: "10.0.0.1"}, "10.0.0.1:23", "", ""},
		{session.ConnectionConfig{Type: "database", DBType: "postgres", Host: "db"}, "db:5432", "", ""},
		{session.ConnectionConfig{Type: "ssh", Host: "::1", Port: 2222}, "[::1]:2222", "", ""},
		{session.ConnectionConfig{Type: "ssh", Host: "[fe80::1]"}, "[fe80::1]:22", "", ""},
		{session.ConnectionConfig{Type: "local"}, "", StateUnchecked, ReasonType},
		{session.ConnectionConfig{Type: "s3", Host: "s3.amazonaws.com"}, "", StateUnchecked, ReasonType},
		{session.ConnectionConfig{Type: "database", DBType: "sqlite", Host: "x"}, "", StateUnchecked, ReasonType},
		{session.ConnectionConfig{Type: "redis", RedisMode: "sentinel", Host: "r"}, "", StateUnchecked, ReasonType},
		{session.ConnectionConfig{Type: "ssh", Host: ""}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "mongodb", Host: "mongodb://a,b/x"}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "ssh", Host: "a b"}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "ssh", Host: "h:22"}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "ssh", Host: "0.0.0.0"}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "ssh", Host: "224.0.0.1"}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "ssh", Host: "h", Port: 70000}, "", StateUnchecked, ReasonHost},
		{session.ConnectionConfig{Type: "ssh", Host: "-oProxyCommand=x"}, "", StateUnchecked, ReasonHost},
	}
	for _, c := range cases {
		tg := TargetOf(c.c, nil)
		if tg.Addr != c.addr || tg.State != c.st || tg.Reason != c.why {
			t.Errorf("%+v: got %+v", c.c, tg)
		}
	}
}
