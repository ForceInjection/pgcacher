//go:build linux

package pcstats

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestGetCloneFlag covers the namespace → CLONE_* mapping. This is pure
// lookup logic, safe to run on any Linux host without privileges.
func TestGetCloneFlag(t *testing.T) {
	ens := NewEnhancedNsSwitcher(os.Getpid(), false)

	cases := []struct {
		nsType NamespaceType
		want   uintptr
	}{
		{MountNS, 0x00020000}, // CLONE_NEWNS
		{PidNS, CLONE_NEWPID},
		{NetNS, CLONE_NEWNET},
		{IpcNS, CLONE_NEWIPC},
		{UtsNS, CLONE_NEWUTS},
		{UserNS, CLONE_NEWUSER},
		{NamespaceType("bogus"), 0},
	}
	for _, c := range cases {
		t.Run(string(c.nsType), func(t *testing.T) {
			got := ens.getCloneFlag(c.nsType)
			if got != c.want {
				t.Fatalf("getCloneFlag(%q) = %#x, want %#x", c.nsType, got, c.want)
			}
		})
	}
}

// TestGetNamespaceSelf parses /proc/self/ns/<type> symlinks of our own process.
// Every Linux process has a mount and pid namespace, so both must resolve to a
// non-zero inode id. This exercises getNamespace's symlink parsing end-to-end.
func TestGetNamespaceSelf(t *testing.T) {
	ens := NewEnhancedNsSwitcher(os.Getpid(), false)

	for _, nt := range []NamespaceType{MountNS, PidNS} {
		id := ens.getNamespace(os.Getpid(), nt)
		if id == 0 {
			t.Fatalf("getNamespace(self, %q) = 0; expected a non-zero ns id", nt)
		}
	}
}

// TestSwitchToSameNamespaceNoop verifies that asking to switch into the
// namespace we are already in is a no-op (no error, no syscall needed).
// This guards against accidental regressions that would attempt setns onto
// our own ns fd and fail in obscure ways.
func TestSwitchToSameNamespaceNoop(t *testing.T) {
	ens := NewEnhancedNsSwitcher(os.Getpid(), false)
	if err := ens.SwitchNamespace(MountNS); err != nil {
		t.Fatalf("SwitchNamespace(self, mnt) returned error: %v", err)
	}
	if err := ens.SwitchNamespace(PidNS); err != nil {
		t.Fatalf("SwitchNamespace(self, pid) returned error: %v", err)
	}
}

// TestSwitchNamespaceMissingTarget confirms a non-existent target pid yields
// a clear error from getNamespace, rather than a zero-value traversal that
// would silently fall through.
func TestSwitchNamespaceMissingTarget(t *testing.T) {
	// pid 0 never exists in /proc; readlink will fail → getNamespace returns 0.
	ens := NewEnhancedNsSwitcher(0, false)
	err := ens.SwitchNamespace(MountNS)
	if err == nil {
		t.Fatal("expected error switching into pid=0 namespace, got nil")
	}
}

// TestSetnsRejectsInvalidFd documents that setns with an invalid fd must
// return an error without panicking. With the EINVAL fix in place the
// CLONE_NEWNS path first runs unshare(CLONE_FS) (must succeed on any Linux)
// and then the kernel rejects the bogus fd with EBADF.
func TestSetnsRejectsInvalidFd(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ens := NewEnhancedNsSwitcher(os.Getpid(), false)
	err := ens.setns(-1, 0x00020000) // CLONE_NEWNS
	if err == nil {
		t.Fatal("expected setns(-1, CLONE_NEWNS) to fail, got nil")
	}
	// The failure must come from the kernel's fd check, not from our
	// pre-syscall unshare step — that would mean the EINVAL fix itself broke.
	if strings.Contains(err.Error(), "unshare(CLONE_FS)") {
		t.Fatalf("unshare(CLONE_FS) unexpectedly failed; fix is regressing: %v", err)
	}
}

// TestUnshareCLONE_FSOnLockedThread pins this goroutine to an OS thread and
// verifies unix.Unshare(CLONE_FS) succeeds without root. This is the
// precondition the kernel enforces for setns(CLONE_NEWNS): once the calling
// thread owns a private fs_struct it is allowed to join a foreign mount
// namespace. If this ever starts failing on a supported kernel, the
// -container / -enhanced-ns feature will surface EINVAL again.
//
// The goroutine intentionally does NOT call runtime.UnlockOSThread: a thread
// whose fs_struct has been unshared is still safe to reuse (CLONE_FS only
// isolates cwd/umask), but keeping it locked mirrors the production contract
// that threads touched by namespace surgery should be retired by the runtime.
func TestUnshareCLONE_FSOnLockedThread(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		// no UnlockOSThread: let the runtime discard this thread on goroutine exit.
		done <- unix.Unshare(unix.CLONE_FS)
	}()
	if err := <-done; err != nil {
		t.Fatalf("unshare(CLONE_FS) on locked OS thread failed: %v", err)
	}
}

// TestSameMountNamespaceSelf verifies the fast-path helper reports true when
// comparing a pid to itself. This is the hot path in top mode where we want
// to skip the throwaway-goroutine thread-pollution scaffolding for any host
// pid that shares our mount namespace.
func TestSameMountNamespaceSelf(t *testing.T) {
	if !SameMountNamespace(os.Getpid()) {
		t.Fatal("SameMountNamespace(self) returned false; expected true")
	}
}

// TestSameMountNamespaceMissingPid documents the conservative behaviour for
// an unreadable target: when /proc/<pid>/ns/mnt cannot be resolved (pid 0,
// vanished pid, permission denied) the helper returns true so callers stay
// in their own namespace instead of attempting a setns that is certain to
// fail.
func TestSameMountNamespaceMissingPid(t *testing.T) {
	if !SameMountNamespace(0) {
		t.Fatal("SameMountNamespace(0) returned false; expected conservative true")
	}
}
