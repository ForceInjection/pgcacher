package pcstats

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFileMincore_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp("", "pcstats_test_empty")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	result, err := GetFileMincore(f, 0)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetPcStatus_NonExistent(t *testing.T) {
	noopFilter := func(f *os.File) error { return nil }
	_, err := GetPcStatus("/nonexistent/path/xyz123", noopFilter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not open file")
}

func TestGetPcStatus_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	noopFilter := func(f *os.File) error { return nil }
	_, err := GetPcStatus(tmpDir, noopFilter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

// TestIsBlockDevice_RegularFile verifies a plain on-disk file is not
// classified as a block device.
func TestIsBlockDevice_RegularFile(t *testing.T) {
	f, err := os.CreateTemp("", "pcstats_test_regular")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	f.Close()

	isBlock, err := IsBlockDevice(f.Name())
	assert.NoError(t, err)
	assert.False(t, isBlock, "regular temp file must not be reported as block device")
}

// TestIsBlockDevice_Directory verifies a directory is not reported as a
// block device.
func TestIsBlockDevice_Directory(t *testing.T) {
	isBlock, err := IsBlockDevice(t.TempDir())
	assert.NoError(t, err)
	assert.False(t, isBlock, "directory must not be reported as block device")
}

// TestIsBlockDevice_NonExistent verifies the helper surfaces stat errors
// rather than silently returning false.
func TestIsBlockDevice_NonExistent(t *testing.T) {
	_, err := IsBlockDevice("/nonexistent/path/xyz123")
	assert.Error(t, err)
}

// TestIsBlockDevice_CharDevice verifies that a character device (/dev/null)
// is NOT classified as a block device. This is the critical distinction
// made by the IsBlockDevice helper: only real block devices carry page
// cache, so char devices must not trip the -statblockdev gate.
//
// /dev/null is a char device on Linux, macOS, and all BSDs, so this test
// can run on any developer workstation without elevated privileges.
func TestIsBlockDevice_CharDevice(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("/dev/null not available on this platform")
	}
	isBlock, err := IsBlockDevice("/dev/null")
	assert.NoError(t, err)
	assert.False(t, isBlock, "/dev/null is a character device, must not be reported as block device")
}
