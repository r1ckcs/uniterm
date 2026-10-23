package tcprtt

import (
	"errors"
	"net"
	"testing"
	"time"
)

func loopbackPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	acc := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		acc <- c
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-acc
	t.Cleanup(func() { client.Close(); server.Close() })
	// One exchange so the kernel has an RTT sample beyond the handshake.
	if _, err := client.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	return client, server
}

type wrapped struct {
	net.Conn
	inner net.Conn
}

func (w wrapped) NetConn() net.Conn { return w.inner }

func TestOfLoopback(t *testing.T) {
	if !Supported {
		t.Skip("platform without TCP RTT")
	}
	c, _ := loopbackPair(t)
	rtt, err := Of(c)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if rtt < 0 || rtt > time.Second {
		t.Fatalf("loopback rtt = %v", rtt)
	}
}

func TestOfUnwrapsNetConn(t *testing.T) {
	if !Supported {
		t.Skip("platform without TCP RTT")
	}
	c, _ := loopbackPair(t)
	w := wrapped{Conn: c, inner: c} // embedded Conn alone would hide the socket
	if _, err := Of(wrapped{Conn: w, inner: w}); err != nil {
		t.Fatalf("Of(wrapped): %v", err)
	}
}

func TestOfNotTCP(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if _, err := Of(a); !errors.Is(err, ErrNotTCP) {
		t.Fatalf("pipe: err = %v, want ErrNotTCP", err)
	}
	if _, err := Of(nil); !errors.Is(err, ErrNotTCP) {
		t.Fatalf("nil: err = %v, want ErrNotTCP", err)
	}
}

func TestOfClosed(t *testing.T) {
	c, _ := loopbackPair(t)
	c.Close()
	if _, err := Of(c); err == nil {
		t.Fatal("closed conn returned no error")
	}
}
