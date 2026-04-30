//go:build !linux

package pcstats

import "fmt"

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

// EnhancedNsSwitcher is a no-op on non-Linux platforms.
type EnhancedNsSwitcher struct{}

func NewEnhancedNsSwitcher(targetPid int, verbose bool) *EnhancedNsSwitcher {
	return &EnhancedNsSwitcher{}
}

func (ens *EnhancedNsSwitcher) SwitchToContainerNamespaces() error {
	return fmt.Errorf("namespace switching is not supported on this platform")
}

func (ens *EnhancedNsSwitcher) SwitchNamespace(nsType NamespaceType) error {
	return fmt.Errorf("namespace switching is not supported on this platform")
}

func EnhancedSwitchMountNs(pid int, verbose bool) error {
	return fmt.Errorf("namespace switching is not supported on this platform")
}

func SwitchToContainerContext(pid int, verbose bool) error {
	return fmt.Errorf("namespace switching is not supported on this platform")
}

func GetContainerPidFromDockerContainer(containerID string) (int, error) {
	return 0, fmt.Errorf("container PID detection not supported on this platform")
}

// ResolveContainerPID is a no-op on non-Linux platforms. /proc/<pid>/cgroup
// scanning requires the Linux procfs, so this always returns an error.
func ResolveContainerPID(id string) (int, error) {
	return 0, fmt.Errorf("container resolution is not supported on this platform")
}
