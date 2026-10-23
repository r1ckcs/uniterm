package tcprtt

import (
	"time"

	"golang.org/x/sys/unix"
)

// Supported reports whether Of can work on this platform.
const Supported = true

// TCP_CONNECTION_INFO: tcpi_srtt is the smoothed RTT in milliseconds.
func sockRTT(fd uintptr) (time.Duration, error) {
	info, err := unix.GetsockoptTCPConnectionInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_CONNECTION_INFO)
	if err != nil {
		return 0, err
	}
	return time.Duration(info.Srtt) * time.Millisecond, nil
}
