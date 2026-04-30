package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout runs fn and returns whatever it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	origStdout := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

// --- ConvertUnit tests ---

func TestConvertUnit(t *testing.T) {
	assert.Equal(t, "0B", ConvertUnit(0))
	assert.Equal(t, "500B", ConvertUnit(500))
	assert.Equal(t, "1.000K", ConvertUnit(1024))
	assert.Equal(t, "1.500K", ConvertUnit(1536))
	assert.Equal(t, "1.000M", ConvertUnit(1048576))
	assert.Equal(t, "1.000G", ConvertUnit(1073741824))
	assert.Equal(t, "1.000T", ConvertUnit(1099511627776))
	assert.Equal(t, "1.000P", ConvertUnit(1125899906842624))
}

// --- safePercent tests ---

func TestSafePercent(t *testing.T) {
	assert.Equal(t, 0.0, safePercent(0, 0))
	assert.Equal(t, 50.0, safePercent(50, 100))
	assert.Equal(t, 100.0, safePercent(100, 100))
	assert.InDelta(t, 33.333, safePercent(1, 3), 0.001)
}

// --- Empty stats should not panic (division by zero fix) ---

func TestFormatText_Empty(t *testing.T) {
	out := captureStdout(t, func() {
		PcStatusList{}.FormatText()
	})
	assert.Contains(t, out, "Sum")
	assert.Contains(t, out, "0.000")
}

func TestFormatUnicode_Empty(t *testing.T) {
	out := captureStdout(t, func() {
		PcStatusList{}.FormatUnicode()
	})
	assert.Contains(t, out, "Sum")
	assert.Contains(t, out, "0.000")
}

func TestFormatPlain_Empty(t *testing.T) {
	out := captureStdout(t, func() {
		PcStatusList{}.FormatPlain()
	})
	assert.Contains(t, out, "Sum")
	assert.Contains(t, out, "0.000")
}

func TestFormatJson_Empty(t *testing.T) {
	out := captureStdout(t, func() {
		PcStatusList{}.FormatJson()
	})
	assert.Equal(t, "[]\n", out)
}

func TestFormatTerse_Empty(t *testing.T) {
	out := captureStdout(t, func() {
		PcStatusList{}.FormatTerse()
	})
	assert.Equal(t, "name,size,timestamp,mtime,pages,cached,percent\n", out)
}

// --- CSV escaping ---

func TestFormatTerse_CommaInFilename(t *testing.T) {
	stats := PcStatusList{
		{
			Name:      "file,with,commas.txt",
			Size:      1024,
			Timestamp: time.Now(),
			Mtime:     time.Now(),
			Pages:     1,
			Cached:    1,
			Percent:   100.0,
		},
	}
	out := captureStdout(t, func() {
		stats.FormatTerse()
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	// The filename should be quoted
	assert.True(t, strings.HasPrefix(lines[1], "\"file,with,commas.txt\""))
}

func TestFormatTerse_QuoteInFilename(t *testing.T) {
	stats := PcStatusList{
		{
			Name:      `file"quote.txt`,
			Size:      1024,
			Timestamp: time.Now(),
			Mtime:     time.Now(),
			Pages:     1,
			Cached:    1,
			Percent:   100.0,
		},
	}
	out := captureStdout(t, func() {
		stats.FormatTerse()
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	// Internal quotes should be doubled per RFC 4180
	assert.Contains(t, lines[1], `"file""quote.txt"`)
}

// --- Sort order ---

func TestPcStatusList_SortByCachedDesc(t *testing.T) {
	stats := PcStatusList{
		{Name: "small", Cached: 10},
		{Name: "large", Cached: 1000},
		{Name: "medium", Cached: 100},
	}
	// PcStatusList sorts by Cached descending
	assert.True(t, stats.Len() == 3)
	stats.Swap(0, 1)
	assert.Equal(t, "large", stats[0].Name)

	// Use the sort interface
	stats = PcStatusList{
		{Name: "small", Cached: 10},
		{Name: "large", Cached: 1000},
		{Name: "medium", Cached: 100},
	}
	// Less returns true when j.Cached < i.Cached (descending)
	assert.True(t, stats.Less(1, 0))  // 1000 > 10
	assert.False(t, stats.Less(0, 1)) // 10 < 1000
}

// --- maxNameLen ---

func TestMaxNameLen(t *testing.T) {
	stats := PcStatusList{
		{Name: "ab"},
		{Name: "abcdefghij"},
	}
	assert.Equal(t, 10, stats.maxNameLen())

	// minimum is 5
	short := PcStatusList{{Name: "a"}}
	assert.Equal(t, 5, short.maxNameLen())

	// empty list
	assert.Equal(t, 5, PcStatusList{}.maxNameLen())
}
