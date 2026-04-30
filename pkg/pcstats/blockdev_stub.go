//go:build !linux

package pcstats

import "errors"

// getBlockDeviceSize is a no-op stub on non-Linux platforms. The BLKGETSIZE64
// ioctl is Linux-specific; on other platforms the code path that calls this
// helper is unreachable at runtime (pgcacher refuses to start on non-Linux),
// but the stub exists so the package compiles cleanly for development on
// macOS/BSD.
func getBlockDeviceSize(path string) (int64, error) {
	return 0, errors.New("block device size lookup is only supported on Linux")
}
