//go:build !darwin && !linux && !windows

package tcprtt

import "time"

// Supported reports whether Of can work on this platform.
const Supported = false

func sockRTT(uintptr) (time.Duration, error) { return 0, ErrUnsupported }
