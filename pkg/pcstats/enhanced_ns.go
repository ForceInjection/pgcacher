//go:build linux

package pcstats

/*
 * Enhanced namespace switching functionality
 * Provides nsenter-like capabilities for container environments
 */

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Namespace types constants
const (
	CLONE_NEWPID  = 0x20000000 // pid namespace
	CLONE_NEWNET  = 0x40000000 // network namespace
	CLONE_NEWIPC  = 0x08000000 // ipc namespace
	CLONE_NEWUTS  = 0x04000000 // uts namespace
	CLONE_NEWUSER = 0x10000000 // user namespace
)

// NamespaceType represents different types of namespaces
type NamespaceType string

const (
	MountNS NamespaceType = "mnt"
	PidNS   NamespaceType = "pid"
	NetNS   NamespaceType = "net"
	IpcNS   NamespaceType = "ipc"
	UtsNS   NamespaceType = "uts"
	UserNS  NamespaceType = "user"
)

// EnhancedNsSwitcher provides enhanced namespace switching capabilities
type EnhancedNsSwitcher struct {
	targetPid int
	verbose   bool
}

// NewEnhancedNsSwitcher creates a new enhanced namespace switcher
func NewEnhancedNsSwitcher(targetPid int, verbose bool) *EnhancedNsSwitcher {
	return &EnhancedNsSwitcher{
		targetPid: targetPid,
		verbose:   verbose,
	}
}

// SwitchToContainerNamespaces switches to container's mount and pid namespaces
// This provides functionality similar to: nsenter --target <pid> -p -m
func (ens *EnhancedNsSwitcher) SwitchToContainerNamespaces() error {
	// Switch to mount namespace first
	if err := ens.SwitchNamespace(MountNS); err != nil {
		if ens.verbose {
			log.Printf("Warning: failed to switch mount namespace: %v", err)
		}
		// Continue even if mount namespace switch fails
	}

	// Switch to pid namespace for process visibility
	if err := ens.SwitchNamespace(PidNS); err != nil {
		if ens.verbose {
			log.Printf("Warning: failed to switch pid namespace: %v", err)
		}
		// Continue even if pid namespace switch fails
	}

	return nil
}

// SwitchNamespace switches to a specific namespace type
func (ens *EnhancedNsSwitcher) SwitchNamespace(nsType NamespaceType) error {
	myNs := ens.getNamespace(os.Getpid(), nsType)
	targetNs := ens.getNamespace(ens.targetPid, nsType)

	if myNs == targetNs {
		if ens.verbose {
			log.Printf("Already in the same %s namespace", nsType)
		}
		return nil
	}

	if targetNs == 0 {
		return fmt.Errorf("failed to get %s namespace for pid %d", nsType, ens.targetPid)
	}

	// Open the namespace file
	nsPath := fmt.Sprintf("/proc/%d/ns/%s", ens.targetPid, nsType)
	nsFile, err := os.Open(nsPath)
	if err != nil {
		return fmt.Errorf("failed to open namespace file %s: %v", nsPath, err)
	}
	defer nsFile.Close()

	// Get the file descriptor
	nsfd := int(nsFile.Fd())

	// Determine the clone flag for this namespace type
	cloneFlag := ens.getCloneFlag(nsType)
	if cloneFlag == 0 {
		return fmt.Errorf("unsupported namespace type: %s", nsType)
	}

	// Switch to the namespace
	if err := ens.setns(nsfd, cloneFlag); err != nil {
		return fmt.Errorf("failed to switch to %s namespace: %v", nsType, err)
	}

	if ens.verbose {
		log.Printf("Successfully switched to %s namespace of pid %d", nsType, ens.targetPid)
	}

	return nil
}

// getNamespace gets the namespace ID for a given pid and namespace type
func (ens *EnhancedNsSwitcher) getNamespace(pid int, nsType NamespaceType) int {
	fname := fmt.Sprintf("/proc/%d/ns/%s", pid, nsType)
	nss, err := os.Readlink(fname)

	// Ignore errors to maintain compatibility
	if err != nil || nss == "" {
		if ens.verbose {
			log.Printf("Failed to read namespace %s for pid %d: %v", nsType, pid, err)
		}
		return 0
	}

	// Parse namespace ID from the symlink target
	// Format: "mnt:[4026531840]"
	prefix := fmt.Sprintf("%s:[", nsType)
	nss = strings.TrimPrefix(nss, prefix)
	nss = strings.TrimSuffix(nss, "]")
	ns, err := strconv.Atoi(nss)

	if err != nil {
		if ens.verbose {
			log.Printf("Failed to parse namespace ID '%s': %v", nss, err)
		}
		return 0
	}

	return ns
}

// getCloneFlag returns the appropriate clone flag for a namespace type
func (ens *EnhancedNsSwitcher) getCloneFlag(nsType NamespaceType) uintptr {
	switch nsType {
	case MountNS:
		return 0x00020000 // CLONE_NEWNS
	case PidNS:
		return CLONE_NEWPID
	case NetNS:
		return CLONE_NEWNET
	case IpcNS:
		return CLONE_NEWIPC
	case UtsNS:
		return CLONE_NEWUTS
	case UserNS:
		return CLONE_NEWUSER
	default:
		return 0
	}
}

// setns performs the actual namespace switch using the setns system call.
//
// When switching into a mount namespace (CLONE_NEWNS), the kernel requires
// the calling thread to NOT share its fs_struct with any other task. Go's
// runtime creates OS threads with CLONE_FS by default, so a plain setns would
// return EINVAL even on a goroutine that pinned itself via runtime.LockOSThread.
// Calling unshare(CLONE_FS) first gives the locked thread a private fs_struct,
// which satisfies the kernel's precondition for setns(CLONE_NEWNS).
//
// The caller is expected to have already pinned the goroutine via
// runtime.LockOSThread() and must NOT call runtime.UnlockOSThread() after
// this succeeds: once the thread has entered a foreign mount namespace the
// runtime must discard it rather than return it to the pool.
func (ens *EnhancedNsSwitcher) setns(fd int, cloneFlag uintptr) error {
	if cloneFlag == 0x00020000 { // CLONE_NEWNS
		if err := unix.Unshare(unix.CLONE_FS); err != nil {
			return fmt.Errorf("unshare(CLONE_FS) before setns(CLONE_NEWNS) failed: %v", err)
		}
	}
	ret, _, err := unix.Syscall(unix.SYS_SETNS, uintptr(fd), cloneFlag, 0)
	if ret != 0 {
		return fmt.Errorf("syscall SYS_SETNS failed: %v", err)
	}
	return nil
}

// GetContainerPidFromDockerContainer gets the main process PID of a Docker container
func GetContainerPidFromDockerContainer(containerID string) (int, error) {
	// This would typically use Docker API or inspect command
	// For now, we'll provide a placeholder implementation
	return 0, fmt.Errorf("container PID detection not implemented yet")
}

// EnhancedSwitchMountNs provides enhanced mount namespace switching with better error handling
func EnhancedSwitchMountNs(pid int, verbose bool) error {
	switcher := NewEnhancedNsSwitcher(pid, verbose)
	return switcher.SwitchNamespace(MountNS)
}

// SwitchToContainerContext switches to container's mount and pid namespaces
// This is the main function that provides nsenter-like functionality
func SwitchToContainerContext(pid int, verbose bool) error {
	switcher := NewEnhancedNsSwitcher(pid, verbose)
	return switcher.SwitchToContainerNamespaces()
}
