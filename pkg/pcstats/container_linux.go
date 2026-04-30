//go:build linux

package pcstats

/*
 * Native container -> host PID resolution via /proc/<pid>/cgroup scanning.
 *
 * No external binary (docker / crictl / nsenter) is required. Works with any
 * runtime that embeds the container ID into the cgroup path: Docker,
 * containerd, CRI-O, Podman, Kubernetes. Supports cgroup v1 and v2.
 */

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// minContainerIDLen is the shortest container ID we accept. Docker's short
// form is 12 hex chars; anything shorter risks false-positive substring
// matches against unrelated cgroup paths.
const minContainerIDLen = 12

// validateContainerID returns nil if id is a plausible container ID:
// at least minContainerIDLen lowercase hex characters. Returns a
// descriptive error otherwise.
func validateContainerID(id string) error {
	if len(id) < minContainerIDLen {
		return fmt.Errorf("container id %q is too short (need at least %d hex chars)", id, minContainerIDLen)
	}
	for _, r := range id {
		isDigit := r >= '0' && r <= '9'
		isLowerHex := r >= 'a' && r <= 'f'
		if !isDigit && !isLowerHex {
			return fmt.Errorf("container id %q contains non-hex character %q", id, r)
		}
	}
	return nil
}

// cgroupContainsID reports whether a single /proc/<pid>/cgroup line
// references the given container id in its cgroup path. The last
// colon-separated field of every cgroup line is the path; we match id
// as a substring of that path. Handles both cgroup v1 and v2 formats.
func cgroupContainsID(line, id string) bool {
	if len(id) < minContainerIDLen {
		return false
	}
	// cgroup line format:
	//   v1: "<hierarchy-ID>:<controller-list>:<cgroup-path>"
	//   v2: "0::<cgroup-path>"
	// Split on ':' and take the last field as the path. SplitN with n=3
	// preserves colons that might appear in the path itself (rare but
	// theoretically possible).
	parts := strings.SplitN(line, ":", 3)
	if len(parts) < 3 {
		return false
	}
	return strings.Contains(parts[2], id)
}

// ResolveContainerPID walks /proc and returns the lowest host PID whose
// cgroup path references the given container id. Any process inside the
// target container is sufficient for the subsequent setns switch, since
// they all share the same namespaces; choosing the lowest PID is simply
// deterministic.
func ResolveContainerPID(id string) (int, error) {
	if err := validateContainerID(id); err != nil {
		return 0, err
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc: %v", err)
	}

	var matches []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pidMatchesContainer(pid, id) {
			matches = append(matches, pid)
		}
	}

	if len(matches) == 0 {
		return 0, fmt.Errorf("no process found for container %s", id)
	}

	sort.Ints(matches)
	return matches[0], nil
}

// pidMatchesContainer reads /proc/<pid>/cgroup and reports whether any
// line references id. Unreadable cgroup files (process exited, EACCES)
// are silently skipped.
func pidMatchesContainer(pid int, id string) bool {
	f, err := os.Open(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if cgroupContainsID(scanner.Text(), id) {
			return true
		}
	}
	return false
}
