package pcstats

/*
 * Copyright 2014-2017 A. Tobey <tobert@gmail.com> @AlTobey
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// not available before Go 1.4
const CLONE_NEWNS = 0x00020000 /* mount namespace */

// if the pid is in a different mount namespace (e.g. Docker)
// the paths will be all wrong, so try to enter that namespace
func SwitchMountNs(pid int) {
	myns := getMountNs(os.Getpid())
	pidns := getMountNs(pid)

	if myns == 0 || pidns == 0 || myns == pidns {
		return
	}

	nsPath := fmt.Sprintf("/proc/%d/ns/mnt", pid)
	nsFile, err := os.Open(nsPath)
	if err != nil {
		log.Printf("failed to open namespace file %s: %v", nsPath, err)
		return
	}
	defer nsFile.Close()

	if err := setns(int(nsFile.Fd()), CLONE_NEWNS); err != nil {
		log.Printf("setns failed for pid %d: %v", pid, err)
	}
}

func getMountNs(pid int) int {
	fname := fmt.Sprintf("/proc/%d/ns/mnt", pid)
	nss, err := os.Readlink(fname)

	// probably permission denied or namespaces not compiled into the kernel
	// ignore any errors so ns support doesn't break normal usage
	if err != nil || nss == "" {
		return 0
	}

	nss = strings.TrimPrefix(nss, "mnt:[")
	nss = strings.TrimSuffix(nss, "]")
	ns, err := strconv.Atoi(nss)

	// not a number? weird ...
	if err != nil {
		log.Fatalf("strconv.Atoi('%s') failed: %s\n", nss, err)
	}

	return ns
}

// SameMountNamespace reports whether the given pid shares the mount namespace
// of the calling process. Callers can use this to short-circuit namespace
// switch scaffolding (LockOSThread + setns) when the target already lives in
// our namespace, which is the common case for non-containerised host pids.
//
// If either namespace id cannot be read (e.g. the pid vanished or /proc is
// not visible) the function conservatively returns true so the caller keeps
// reading files from its current namespace rather than attempting a setns
// that is certain to fail.
func SameMountNamespace(pid int) bool {
	selfNs := getMountNs(os.Getpid())
	targetNs := getMountNs(pid)
	if selfNs == 0 || targetNs == 0 {
		return true
	}
	return selfNs == targetNs
}

// setns switches the current thread into the namespace identified by fd.
//
// For CLONE_NEWNS, the kernel rejects the call (EINVAL) unless the calling
// thread has its own fs_struct (i.e. it does not share filesystem state with
// any other thread). Go's runtime creates worker threads with CLONE_FS, so we
// must explicitly unshare(CLONE_FS) first. Callers must have pinned the
// goroutine with runtime.LockOSThread() and should let the goroutine exit
// without unlocking once a mount namespace switch has succeeded, so the Go
// runtime discards the now-polluted thread instead of returning it to the pool.
func setns(fd int, cloneFlag uintptr) error {
	if cloneFlag == CLONE_NEWNS {
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
