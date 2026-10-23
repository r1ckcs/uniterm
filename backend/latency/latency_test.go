package latency

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ys-ll/uniterm/backend/session"
	"github.com/ys-ll/uniterm/backend/tcprtt"
)

type fakeConn struct{ net.Conn }

type fixture struct {
	mu      sync.Mutex
	hops    map[string]session.FirstHop
	reasons map[string]string
	calls   map[string]int
	rtt     time.Duration
	err     error
	emits   chan map[string]Sample
}

func newFixture() *fixture {
	return &fixture{
		hops:    map[string]session.FirstHop{},
		reasons: map[string]string{},
		calls:   map[string]int{},
		rtt:     23456 * time.Microsecond,
		emits:   make(chan map[string]Sample, 64),
	}
}

func (f *fixture) add(id, via string) {
	f.hops[id] = session.FirstHop{Conn: fakeConn{}, Via: via}
}

func (f *fixture) options() Options {
	return Options{
		Lookup: func(id string) (session.FirstHop, string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if r, ok := f.reasons[id]; ok {
				return session.FirstHop{}, r
			}
			h, ok := f.hops[id]
			if !ok {
				return session.FirstHop{}, ReasonClosed
			}
			f.calls[id]++
			return h, ""
		},
		RTT: func(c net.Conn) (time.Duration, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.rtt, f.err
		},
		Emit: func(m map[string]Sample) { f.emits <- m },
	}
}

func (f *fixture) count(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func TestWatchReturnsSamples(t *testing.T) {
	f := newFixture()
	f.add("a", session.ViaDirect)
	f.add("b", session.ViaJump)
	f.reasons["t"] = ReasonType
	m := New(f.options())
	defer m.Close()

	got := m.Watch([]string{"a", "b", "t", "gone"})
	if s := got["a"]; s.State != StateOK || s.RttMs != 23.5 || s.Via != "direct" || s.At == "" || s.Reason != "" {
		t.Fatalf("a = %+v", s)
	}
	if s := got["b"]; s.State != StateOK || s.Via != "jump" {
		t.Fatalf("b = %+v", s)
	}
	if s := got["t"]; s.State != StateUnavailable || s.Reason != ReasonType || s.RttMs != 0 || s.Via != "" {
		t.Fatalf("t = %+v", s)
	}
	if s := got["gone"]; s.State != StateUnavailable || s.Reason != ReasonClosed {
		t.Fatalf("gone = %+v", s)
	}
	if _, err := time.Parse(time.RFC3339, got["a"].At); err != nil {
		t.Fatalf("at: %v", err)
	}
}

// Switching tabs back and forth must not sample one session more than once
// per Interval.
func TestWatchReusesFreshSample(t *testing.T) {
	f := newFixture()
	f.add("a", session.ViaDirect)
	now := time.Unix(1000, 0)
	o := f.options()
	o.Now = func() time.Time { return now }
	m := New(o)
	defer m.Close()

	for i := 0; i < 10; i++ {
		m.Watch([]string{"a"})
	}
	if n := f.count("a"); n != 1 {
		t.Fatalf("sampled %d times within the interval, want 1", n)
	}
	now = now.Add(Interval)
	m.Watch([]string{"a"})
	if n := f.count("a"); n != 2 {
		t.Fatalf("after the interval: sampled %d times, want 2", n)
	}
	// Leaving and coming back within the interval reuses the sample too.
	f.add("b", session.ViaDirect)
	m.Watch([]string{"b"})
	m.Watch([]string{"a"})
	if n := f.count("a"); n != 2 {
		t.Fatalf("after [b], [a]: sampled %d times, want 2", n)
	}
	now = now.Add(Interval)
	m.Watch([]string{"b"}) // a is stale and unwatched: dropped
	m.Watch([]string{"a"})
	if n := f.count("a"); n != 3 {
		t.Fatalf("after the interval: sampled %d times, want 3", n)
	}
}

func TestLoopSamplesOnlyWatched(t *testing.T) {
	f := newFixture()
	f.add("a", session.ViaDirect)
	f.add("hidden", session.ViaDirect)
	m := newMonitor(f.options(), 10*time.Millisecond)
	defer m.Close()

	m.Watch([]string{"a"})
	for i := 0; i < 3; i++ {
		select {
		case got := <-f.emits:
			if len(got) != 1 || got["a"].State != StateOK {
				t.Fatalf("emit = %+v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no emit while watching")
		}
	}
	if n := f.count("hidden"); n != 0 {
		t.Fatalf("hidden tab sampled %d times", n)
	}

	m.Watch(nil)
	drain(f.emits, 30*time.Millisecond)
	before := f.count("a")
	select {
	case got := <-f.emits:
		t.Fatalf("emit with nothing watched: %+v", got)
	case <-time.After(60 * time.Millisecond):
	}
	if f.count("a") != before {
		t.Fatal("sampled with nothing watched")
	}
}

func TestCloseStopsLoop(t *testing.T) {
	f := newFixture()
	f.add("a", session.ViaDirect)
	m := newMonitor(f.options(), 10*time.Millisecond)
	m.Watch([]string{"a"})
	m.Close()
	m.Close() // idempotent
	drain(f.emits, 30*time.Millisecond)
	select {
	case <-f.emits:
		t.Fatal("emit after Close")
	case <-time.After(60 * time.Millisecond):
	}
}

func TestWatchCapAndDedupe(t *testing.T) {
	f := newFixture()
	var ids []string
	for i := 0; i < MaxWatch+10; i++ {
		id := fmt.Sprint("s", i)
		f.add(id, session.ViaDirect)
		ids = append(ids, id, id, "")
	}
	m := New(f.options())
	defer m.Close()
	got := m.Watch(ids)
	if len(got) != MaxWatch {
		t.Fatalf("watched %d, want %d", len(got), MaxWatch)
	}
	if f.count("s0") != 1 {
		t.Fatalf("duplicate id sampled %d times", f.count("s0"))
	}
}

func TestErrorReasons(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{tcprtt.ErrUnsupported, ReasonPlatform},
		{tcprtt.ErrNotTCP, ReasonType},
		{fmt.Errorf("x: %w", net.ErrClosed), ReasonClosed},
		{errors.New("boom"), ReasonError},
	} {
		f := newFixture()
		f.add("a", session.ViaDirect)
		f.err = tc.err
		m := New(f.options())
		s := m.Watch([]string{"a"})["a"]
		m.Close()
		if s.State != StateUnavailable || s.Reason != tc.want {
			t.Errorf("%v: %+v, want %s", tc.err, s, tc.want)
		}
	}
	// Connected but without a socket counts as closed, not as a zero RTT.
	f := newFixture()
	f.hops["a"] = session.FirstHop{Via: session.ViaDirect}
	m := New(f.options())
	defer m.Close()
	if s := m.Watch([]string{"a"})["a"]; s.Reason != ReasonClosed {
		t.Fatalf("nil conn: %+v", s)
	}
}

// The real query on a real socket: no byte reaches the other end, and a
// closed socket reads as "closed".
func TestRealSocketSendsNothing(t *testing.T) {
	if !tcprtt.Supported {
		t.Skip("platform without TCP RTT")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var received atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		for {
			n, err := c.Read(buf)
			received.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	hop := session.FirstHop{Conn: c, Via: session.ViaDirect}
	m := newMonitor(Options{
		Lookup: func(string) (session.FirstHop, string) { return hop, "" },
		Emit:   func(map[string]Sample) {},
	}, 5*time.Millisecond)
	defer m.Close()
	if s := m.Watch([]string{"a"})["a"]; s.State != StateOK {
		t.Fatalf("live socket: %+v", s)
	}
	time.Sleep(50 * time.Millisecond) // several rounds
	c.Close()
	<-done
	if n := received.Load(); n != 0 {
		t.Fatalf("server received %d bytes", n)
	}
	m.Watch(nil)
	time.Sleep(20 * time.Millisecond) // let the cached sample go stale
	if s := m.Watch([]string{"a"})["a"]; s.State != StateUnavailable || s.Reason != ReasonClosed {
		t.Fatalf("closed socket: %+v", s)
	}
}

func drain(ch chan map[string]Sample, d time.Duration) {
	end := time.After(d)
	for {
		select {
		case <-ch:
		case <-end:
			return
		}
	}
}
