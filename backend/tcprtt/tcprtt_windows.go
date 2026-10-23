package tcprtt

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Supported reports whether Of can work on this platform.
const Supported = true

// sioTCPInfo is SIO_TCP_INFO = _WSAIORW(IOC_VENDOR, 39) (Windows 10 1703+).
const sioTCPInfo = 0xD8000027

// tcpInfoV0 mirrors TCP_INFO_v0 (mstcpip.h). Only RttUs is read; the rest
// keeps the size right so the ioctl accepts the buffer.
type tcpInfoV0 struct {
	State             int32
	Mss               uint32
	ConnectionTimeMs  uint64
	TimestampsEnabled uint8
	_                 [3]byte
	RttUs             uint32
	MinRttUs          uint32
	BytesInFlight     uint32
	Cwnd              uint32
	SndWnd            uint32
	RcvWnd            uint32
	RcvBuf            uint32
	BytesOut          uint64
	BytesIn           uint64
	BytesReordered    uint32
	BytesRetrans      uint32
	FastRetrans       uint32
	DupAcksIn         uint32
	TimeoutEpisodes   uint32
	SynRetrans        uint8
	_                 [3]byte
}

func sockRTT(fd uintptr) (time.Duration, error) {
	var version uint32 // TCP_INFO_v0
	var info tcpInfoV0
	var n uint32
	err := windows.WSAIoctl(windows.Handle(fd), sioTCPInfo,
		(*byte)(unsafe.Pointer(&version)), uint32(unsafe.Sizeof(version)),
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
		&n, nil, 0)
	if err != nil {
		return 0, err
	}
	return time.Duration(info.RttUs) * time.Microsecond, nil
}
