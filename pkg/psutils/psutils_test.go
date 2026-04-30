package psutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnixProcess_Interface(t *testing.T) {
	p := &UnixProcess{
		pid:    42,
		ppid:   1,
		rss:    1024,
		binary: "test",
	}
	// Verify the Process interface is satisfied
	var proc Process = p
	assert.Equal(t, 42, proc.Pid())
	assert.Equal(t, 1, proc.PPid())
	assert.Equal(t, 1024, proc.RSS())
	assert.Equal(t, "test", proc.Executable())
}

func TestProcessSlice_Sort(t *testing.T) {
	ps := ProcessSlice{
		&UnixProcess{pid: 1, rss: 100},
		&UnixProcess{pid: 2, rss: 300},
		&UnixProcess{pid: 3, rss: 200},
	}

	assert.Equal(t, 3, ps.Len())

	// Less sorts by RSS descending
	assert.True(t, ps.Less(1, 0))  // 300 > 100
	assert.False(t, ps.Less(0, 1)) // 100 < 300

	ps.Swap(0, 1)
	assert.Equal(t, 2, ps[0].Pid())
	assert.Equal(t, 1, ps[1].Pid())
}
