package tcprtt

import (
	"testing"
	"unsafe"
)

// TCP_INFO_v0 is 88 bytes; a wrong layout makes the ioctl reject the buffer.
func TestTCPInfoV0Layout(t *testing.T) {
	var i tcpInfoV0
	if s := unsafe.Sizeof(i); s != 88 {
		t.Fatalf("sizeof = %d, want 88", s)
	}
	if o := unsafe.Offsetof(i.RttUs); o != 20 {
		t.Fatalf("RttUs offset = %d, want 20", o)
	}
}
