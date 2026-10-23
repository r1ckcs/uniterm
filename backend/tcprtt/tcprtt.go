// Package tcprtt reads the round-trip time the operating system already keeps
// for a connected TCP socket (the kernel's smoothed RTT). It sends nothing and
// dials nothing: it is one getsockopt/ioctl on a socket the caller owns.
// See CONTRATO-latencia.md.
package tcprtt

import (
	"errors"
	"net"
	"syscall"
	"time"
)

// ErrUnsupported means this platform has no TCP RTT query.
var ErrUnsupported = errors.New("tcprtt: unsupported platform")

// ErrNotTCP means the connection has no TCP socket underneath.
var ErrNotTCP = errors.New("tcprtt: not a TCP socket")

// maxUnwrap bounds the NetConn() chain (proxy wrappers around the socket).
const maxUnwrap = 8

// Of returns the kernel's smoothed RTT for c. Wrappers that expose the
// socket through NetConn() (tls.Conn, the proxy dialers) are unwrapped.
// Calling it on a closed connection returns an error, never a value of a
// reused descriptor: the runtime holds a reference on the fd during Control.
func Of(c net.Conn) (time.Duration, error) {
	for i := 0; i < maxUnwrap && c != nil; i++ {
		if _, ok := c.(*net.TCPConn); ok {
			break
		}
		u, ok := c.(interface{ NetConn() net.Conn })
		if !ok {
			break
		}
		c = u.NetConn()
	}
	tcp, ok := c.(*net.TCPConn)
	if !ok {
		return 0, ErrNotTCP
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return 0, err
	}
	return query(raw)
}

func query(raw syscall.RawConn) (time.Duration, error) {
	var rtt time.Duration
	var qerr error
	if err := raw.Control(func(fd uintptr) { rtt, qerr = sockRTT(fd) }); err != nil {
		return 0, err
	}
	return rtt, qerr
}
