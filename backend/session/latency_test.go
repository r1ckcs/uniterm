package session

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"

	"github.com/ys-ll/uniterm/backend/tcprtt"
)

// A real Connect records its own socket (direct), the kernel answers for it,
// and a channel clone reports the same leg.
func TestSSHFirstHopDirectAndClone(t *testing.T) {
	addr, _ := startProbeTrackingServer(t, false)
	trustProbeTestHost(t, addr)
	s := NewSSHSession("hop-" + t.Name())
	if _, ok := s.FirstHop(); ok {
		t.Fatal("FirstHop before Connect")
	}
	if err := s.Connect(probeTestConfig(addr)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Disconnect()

	hop, ok := s.FirstHop()
	if !ok || hop.Via != ViaDirect || hop.Conn == nil {
		t.Fatalf("hop = %+v, %v", hop, ok)
	}
	if got := hop.Conn.RemoteAddr().String(); got != addr {
		t.Fatalf("hop remote = %s, want %s", got, addr)
	}
	if tcprtt.Supported {
		if _, err := tcprtt.Of(hop.Conn); err != nil {
			t.Fatalf("rtt: %v", err)
		}
	}

	clone := NewSSHChannelSession("hop-clone", s)
	if err := clone.Connect(probeTestConfig(addr)); err != nil {
		t.Fatalf("clone connect: %v", err)
	}
	defer clone.Disconnect()
	if ch, ok := clone.FirstHop(); !ok || ch.Conn != hop.Conn || ch.Via != ViaDirect {
		t.Fatalf("clone hop = %+v, %v", ch, ok)
	}
}

// With a jump host the session dials 127.0.0.1; the app presets the tunnel's
// socket so the footer measures the jump leg, not loopback.
func TestSSHFirstHopPresetForJump(t *testing.T) {
	addr, _ := startProbeTrackingServer(t, false)
	trustProbeTestHost(t, addr)
	jumpA, jumpB := net.Pipe()
	defer jumpA.Close()
	defer jumpB.Close()

	s := NewSSHSession("hop-" + t.Name())
	s.SetFirstHop(FirstHop{Conn: jumpA, Via: ViaJump})
	if _, ok := s.FirstHop(); ok {
		t.Fatal("preset hop visible before Connect")
	}
	if err := s.Connect(probeTestConfig(addr)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Disconnect()
	if hop, ok := s.FirstHop(); !ok || hop.Conn != jumpA || hop.Via != ViaJump {
		t.Fatalf("hop = %+v, %v", hop, ok)
	}
}

// startSOCKS5 is a no-auth SOCKS5 server that pipes CONNECT to the target.
func startSOCKS5(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 262)
				if _, err := io.ReadFull(c, buf[:2]); err != nil {
					return
				}
				if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
					return
				}
				c.Write([]byte{5, 0})
				if _, err := io.ReadFull(c, buf[:4]); err != nil || buf[3] != 1 {
					return
				}
				if _, err := io.ReadFull(c, buf[:6]); err != nil {
					return
				}
				target := net.JoinHostPort(net.IP(buf[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(buf[4:6]))))
				up, err := net.Dial("tcp", target)
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	return ln.Addr().String()
}

// Behind a SOCKS proxy the proxy package hides the socket; dialFirstHop
// exposes it so the RTT of the leg to the proxy can be read.
func TestDialFirstHopSOCKSExposesSocket(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		c, err := target.Accept()
		if err == nil {
			io.Copy(io.Discard, c)
		}
	}()
	proxyAddr := startSOCKS5(t)
	host, portStr, _ := net.SplitHostPort(proxyAddr)
	port, _ := strconv.Atoi(portStr)

	c, err := dialFirstHop(target.Addr().String(), &SocksProxy{Kind: "socks5", Host: host, Port: port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	u, ok := c.(interface{ NetConn() net.Conn })
	if !ok {
		t.Fatalf("%T hides its socket", c)
	}
	if got := u.NetConn().RemoteAddr().String(); got != proxyAddr {
		t.Fatalf("socket remote = %s, want the proxy %s", got, proxyAddr)
	}
	if _, err := c.Write([]byte("ping")); err != nil { // traffic still flows through the proxy conn
		t.Fatalf("write: %v", err)
	}
	if tcprtt.Supported {
		if _, err := tcprtt.Of(c); err != nil {
			t.Fatalf("rtt: %v", err)
		}
	}
}

// The jump host tunnel keeps its own socket for the footer and forgets it
// on Stop.
func TestTunnelFirstHop(t *testing.T) {
	addr, _ := startProbeTrackingServer(t, false)
	trustProbeTestHost(t, addr)
	ts := NewTunnelService()
	defer ts.Shutdown()
	if _, err := ts.Start("sid", probeTestConfig(addr), "127.0.0.1", 9, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	hop, ok := ts.FirstHop("sid")
	if !ok || hop.Via != ViaJump || hop.Conn.RemoteAddr().String() != addr {
		t.Fatalf("hop = %+v, %v", hop, ok)
	}
	ts.Stop("sid")
	if _, ok := ts.FirstHop("sid"); ok {
		t.Fatal("hop after Stop")
	}
}
