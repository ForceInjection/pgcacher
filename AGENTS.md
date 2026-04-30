# AGENTS.md

This file provides guidance to Qoder (qoder.com) when working with code in this repository.

## Project Overview

`pgcacher` is a Linux CLI tool for inspecting page cache statistics of files and processes. It uses the `mincore(2)` syscall via `mmap` to determine which file pages are resident in the kernel page cache. It is an enhanced alternative to `pcstat`/`hcache` with better process file discovery (reads both `/proc/{pid}/fd` and `/proc/{pid}/maps`), concurrency support, and container namespace awareness.

**Linux-only runtime.** The binary exits immediately on non-Linux systems (`main.go:49`). However, the code compiles on macOS/BSD via build-tag stubs (`mnt_ns_unix.go`, `enhanced_ns_stub.go`, `refresh_darwin.go`).

## Build and Test Commands

```bash
# Build (cross-compiles Linux amd64 binary)
make build
# Equivalent to:
GOOS=linux GOARCH=amd64 go build .

# Run tests
go test ./...

# Run a single test
go test -run TestWildcardMatch -v

# Build for local platform (useful for compilation checks on macOS)
go build .
```

The Makefile uses `GOPROXY=https://goproxy.cn` (China proxy). When building locally outside China, you can run `go build .` directly.

## Architecture

### Entry Point and Core Flow

`main.go` -> parses flags into `globalOption` struct -> creates `pgcacher` struct -> dispatches to one of two modes:

1. **Top mode** (`-top`): `handleTop()` scans all system processes via `psutils.Processes()`, collects their open files, computes page cache stats, and displays the top N.
2. **PID/file mode**: For a given `-pid`, resolves open files via `/proc/{pid}/fd` + `/proc/{pid}/maps`. For file args, walks directories to `depth`. Then computes page cache stats and outputs.

### Package Layout

- **Root package (`main`)**: CLI entry point (`main.go`), core logic struct and methods (`pgcacher.go`), output formatting (`formats.go`), and wildcard matching / directory walking helpers.
- **`pkg/pcstats`**: Low-level page cache inspection.
  - `mincore.go` - `mmap` + `mincore(2)` syscall to probe which pages are cached.
  - `pcstatus.go` - `GetPcStatus()` opens a file, calls mincore, returns `PcStatus` struct.
  - `mnt_ns_linux.go` / `mnt_ns_unix.go` - Mount namespace switching (Linux uses `setns` syscall with proper file descriptors; other Unix is a no-op stub).
  - `enhanced_ns.go` / `enhanced_ns_stub.go` - Multi-namespace switching (`mnt` + `pid`) for container support via `SwitchToContainerContext()`. Linux-only build tag; stub on other platforms.
- **`pkg/psutils`**: Process enumeration by reading `/proc`.
  - `scan.go` - Reads `/proc` directory entries, parses `/proc/{pid}/stat`.
  - `refresh_linux.go` / `refresh_darwin.go` - Platform-specific process stat refresh (darwin is a no-op).
  - `process.go` - `Process` interface and `ProcessSlice` sort by RSS.

### Concurrency Model

A worker pool pattern is used throughout: items are fed into a buffered channel, N goroutines (`-worker` flag, default 2) consume from it. `getProcessFiles` uses `runtime.LockOSThread` to pin goroutines to OS threads during namespace switches, since `setns` is thread-scoped. This applies to:

- Reading `/proc/{pid}/fd` symlinks (`getProcessFdFiles`)
- Computing page cache stats for files (`getPageCacheStats`)
- Scanning all process files in top mode (`handleTop`)

**Namespace safety contract:** `setns(2)` affects only the calling OS thread, so every goroutine that calls `pcstats.SwitchMountNs` or `pcstats.SwitchToContainerContext` **must** call `runtime.LockOSThread()` before the switch and `runtime.UnlockOSThread()` (or exit the goroutine without unlocking, so the Go runtime discards the thread) afterwards. Without this pin, the Go scheduler can migrate the goroutine to a different OS thread and leak a modified namespace into unrelated work. `pg.option` is treated as immutable after `init()` so workers can read it without synchronization; writes to shared collections such as `pg.files` are guarded by a mutex and published via `wg.Wait()` before subsequent reads.

### Output Formats

`PcStatusList` (sorted by cached pages descending) supports five output modes selected by flags: `-json`, `-terse`, `-unicode`, `-plain`, or default ASCII table. All formatting is in `formats.go`.

### Key External Dependencies

- `golang.org/x/sys/unix` - syscalls (`mmap`, `mincore`, `setns`, `Munmap`)
- `github.com/dustin/go-humanize` - parsing human-readable size strings (`-least-size` flag)
- `github.com/stretchr/testify` - test assertions

## Platform Considerations

- Build tags split Linux vs BSD/Darwin implementations in `pkg/pcstats` and `pkg/psutils`. The Darwin stubs are no-ops, so the binary compiles but does nothing useful on macOS.
- `enhanced_ns.go` has a `//go:build linux` tag. The `enhanced_ns_stub.go` provides no-op implementations for non-Linux platforms.
- The `go.mod` declares `go 1.21`. The builtin `min` function is used (no custom definition needed).
