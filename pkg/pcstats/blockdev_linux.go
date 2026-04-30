//go:build linux

package pcstats

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// BLKGETSIZE64 returns the block-device size in bytes as an unsigned 64-bit
// integer. The constant value is defined in <linux/fs.h>:
//
//	#define BLKGETSIZE64 _IOR(0x12, 114, size_t)
//
// which expands to 0x80081272 on little-endian Linux for sizeof(size_t)==8.
// This is necessary because fstat(2) reports Size==0 for block-special files.
const blkGetSize64 = 0x80081272

// getBlockDeviceSize returns the size in bytes of a block device by issuing
// the BLKGETSIZE64 ioctl. The caller must ensure path actually refers to a
// block-special file (see IsBlockDevice).
func getBlockDeviceSize(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var size uint64
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		f.Fd(),
		uintptr(blkGetSize64),
		uintptr(unsafe.Pointer(&size)),
	)
	if errno != 0 {
		return 0, errno
	}
	return int64(size), nil
}
