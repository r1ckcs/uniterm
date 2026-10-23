package tcprtt

import (
	"time"

	"golang.org/x/sys/unix"
)

// Supported reports whether Of can work on this platform.
const Supported = true

// TCP_INFO: tcpi_rtt is the smoothed RTT in microseconds.
func sockRTT(fd uintptr) (time.Duration, error) {
	info, err := unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	if err != nil {
		return 0, err
	}
	return time.Duration(info.Rtt) * time.Microsecond, nil
}
