package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/rfyiamcool/pgcacher/pkg/pcstats"
	"github.com/rfyiamcool/pgcacher/pkg/psutils"
)

type emptyNull struct{}

type pgcacher struct {
	files     []string
	leastSize int64
	option    *option
}

func (pg *pgcacher) ignoreFile(file string) bool {
	if pg.option.excludeFiles != "" && wildcardMatchMulti(file, pg.option.excludeFiles) {
		return true
	}

	if pg.option.includeFiles != "" && !wildcardMatchMulti(file, pg.option.includeFiles) {
		return true
	}

	return false
}

func (pg *pgcacher) filterFiles() {
	sset := make(map[string]emptyNull, len(pg.files))
	for _, file := range pg.files {
		file = strings.Trim(file, " ")
		if pg.ignoreFile(file) {
			continue
		}
		sset[file] = emptyNull{}
	}

	// remove duplication.
	dups := make([]string, 0, len(sset))
	for fname := range sset {
		dups = append(dups, fname)
	}
	pg.files = dups
}

func (pg *pgcacher) appendProcessFiles(pid int) {
	pg.files = append(pg.files, pg.getProcessFiles(pid)...)
}

func (pg *pgcacher) getProcessFiles(pid int) []string {
	// Fast path: if we are not using enhanced (pid-namespace) switching and the
	// target pid already shares our mount namespace, no setns is required.
	// Reading directly from the caller's goroutine avoids spawning a throwaway
	// OS thread for every pid in top mode, which is the common case for
	// non-containerised host processes.
	if !pg.option.enhancedNs && pcstats.SameMountNamespace(pid) {
		return pg.readProcessFiles(pid)
	}

	// Slow path: a mount (and possibly pid) namespace switch may happen.
	//
	// setns(CLONE_NEWNS) mutates the current OS thread's namespace view. If we
	// simply LockOSThread + UnlockOSThread around the switch, the Go runtime
	// returns the polluted thread to its scheduler pool and any later
	// goroutine scheduled onto it would observe the container's /proc and
	// filesystem instead of the host's.
	//
	// To contain the pollution we run the switch and the subsequent /proc
	// reads on a dedicated throwaway goroutine that locks its OS thread and
	// never unlocks it. When the goroutine exits, the Go runtime terminates
	// the locked thread instead of recycling it, so the foreign namespace
	// cannot leak to unrelated work.
	ch := make(chan []string, 1)
	go func() {
		runtime.LockOSThread()
		// Intentionally no runtime.UnlockOSThread here — see comment above.

		if pg.option.enhancedNs {
			if err := pcstats.SwitchToContainerContext(pid, pg.option.verbose); err != nil {
				if pg.option.verbose {
					log.Printf("Enhanced namespace switching failed, falling back to basic mode: %v", err)
				}
				pcstats.SwitchMountNs(pid)
			}
		} else {
			pcstats.SwitchMountNs(pid)
		}

		ch <- pg.readProcessFiles(pid)
	}()
	return <-ch
}

// readProcessFiles reads the open-file descriptors and memory map entries of
// pid out of /proc and returns their combined target paths. It does not touch
// namespaces, so it is safe to call either from the caller's goroutine (fast
// path) or from a dedicated throwaway thread after a setns (slow path).
func (pg *pgcacher) readProcessFiles(pid int) []string {
	processFiles := pg.getProcessFdFiles(pid)
	processMapFiles := pg.getProcessMaps(pid)

	var files []string
	files = append(files, processFiles...)
	files = append(files, processMapFiles...)
	return files
}

func (pg *pgcacher) getProcessMaps(pid int) []string {
	fname := fmt.Sprintf("/proc/%d/maps", pid)

	f, err := os.Open(fname)
	if err != nil {
		log.Printf("could not read dir %s, err: %s", fname, err.Error())
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	out := make([]string, 0, 20)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 6 && strings.HasPrefix(parts[5], "/") {
			// found something that looks like a file
			out = append(out, parts[5])
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("reading '%s' failed: %s", fname, err)
	}

	return out
}

func (pg *pgcacher) getProcessFdFiles(pid int) []string {
	dpath := fmt.Sprintf("/proc/%d/fd", pid)

	files, err := os.ReadDir(dpath)
	if err != nil {
		log.Printf("could not read dir %s, err: %s", dpath, err.Error())
		return nil
	}

	var (
		out = make([]string, 0, len(files))
		mu  = sync.Mutex{}
	)

	readlink := func(file fs.DirEntry) {
		fpath := fmt.Sprintf("%s/%s", dpath, file.Name())
		target, err := os.Readlink(fpath)
		if err != nil {
			log.Printf("can not read link '%s', err: %v\n", fpath, err)
			return
		}
		if !strings.HasPrefix(target, "/") { // ignore socket or pipe.
			return
		}
		if strings.HasPrefix(target, "/dev") {
			// character devices (ttys, /dev/null, /dev/urandom, ...) have no
			// page cache; skip them unconditionally. Block devices (/dev/sdX,
			// /dev/nvmeXnY, loop*, dm-*) DO carry page cache for raw I/O, but
			// stat'ing every /dev/* fd and then issuing an ioctl for size is
			// expensive and surprising, so we only include them when the user
			// opts in via -statblockdev.
			if !pg.option.statBlockdev {
				return
			}
			isBlock, err := pcstats.IsBlockDevice(fpath)
			if err != nil {
				log.Printf("cannot determine if %q is a block device, err: %v\n", fpath, err)
				return
			}
			if !isBlock {
				return
			}
		}
		if pg.ignoreFile(target) {
			return
		}

		mu.Lock()
		out = append(out, target)
		mu.Unlock()
	}

	// fill files to channel.
	queue := make(chan fs.DirEntry, len(files))
	for _, file := range files {
		queue <- file
	}
	close(queue)

	// handle files concurrently.
	wg := sync.WaitGroup{}
	for i := 0; i < pg.option.worker; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for file := range queue {
				readlink(file)
			}
		}()
	}
	wg.Wait()

	return out
}

var errLessThanSize = errors.New("the file size is less than the leastSize")

func (pg *pgcacher) getPageCacheStats() PcStatusList {
	var (
		mu = sync.Mutex{}
		wg = sync.WaitGroup{}

		stats = make(PcStatusList, 0, len(pg.files))
	)

	// fill files to queue.
	queue := make(chan string, len(pg.files))
	for _, fname := range pg.files {
		queue <- fname
	}
	close(queue)

	ignoreFunc := func(file *os.File) error {
		fs, err := file.Stat()
		if err != nil {
			return err
		}
		// Block devices report Size()==0 from fstat; real size is resolved later
		// via BLKGETSIZE64 inside GetPcStatus. Skip the -least-size filter here to
		// avoid dropping every block device when -statblockdev is combined with a
		// positive threshold.
		mode := fs.Mode()
		if mode&os.ModeDevice != 0 && mode&os.ModeCharDevice == 0 {
			return nil
		}
		if pg.leastSize != 0 && fs.Size() < pg.leastSize {
			return errLessThanSize
		}
		return nil
	}

	analyse := func(fname string) {
		status, err := pcstats.GetPcStatus(fname, ignoreFunc)
		if err == errLessThanSize {
			return
		}
		if err != nil {
			log.Printf("skipping %q: %v", fname, err)
			return
		}

		// only get filename, trim full dir path of the file.
		if pg.option.bname {
			status.Name = path.Base(fname)
		}

		// append
		mu.Lock()
		stats = append(stats, status)
		mu.Unlock()
	}

	// analyse page cache stats of files concurrently.
	for i := 0; i < pg.option.worker; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for fname := range queue {
				analyse(fname)
			}
		}()
	}
	wg.Wait()

	sort.Sort(PcStatusList(stats))
	return stats
}

func (pg *pgcacher) output(stats PcStatusList, limit int) {
	limit = min(len(stats), limit)
	stats = stats[:limit]

	if pg.option.json {
		stats.FormatJson()
	} else if pg.option.terse {
		stats.FormatTerse()
	} else if pg.option.unicode {
		stats.FormatUnicode()
	} else if pg.option.plain {
		stats.FormatPlain()
	} else {
		stats.FormatText()
	}
}

func (pg *pgcacher) handleTop() {
	// get all active process.
	procs, err := psutils.Processes()
	if err != nil || len(procs) == 0 {
		log.Fatalf("failed to get processes, err: %v", err)
	}

	ps := make([]psutils.Process, 0, 50)
	for _, proc := range procs {
		if proc.RSS() == 0 {
			continue
		}

		ps = append(ps, proc)
	}

	var (
		wg    = sync.WaitGroup{}
		mu    = sync.Mutex{}
		queue = make(chan psutils.Process, len(ps))
	)

	for _, process := range ps {
		queue <- process
	}
	close(queue)

	// append open fd of each process.
	for i := 0; i < pg.option.worker; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for process := range queue {
				files := pg.getProcessFiles(process.Pid())

				mu.Lock()
				pg.files = append(pg.files, files...)
				mu.Unlock()
			}

		}()
	}
	// wg.Wait() establishes a happens-before edge: all mutex-protected
	// appends to pg.files by the worker goroutines are guaranteed to be
	// visible after this point, so filterFiles can safely read pg.files.
	wg.Wait()

	// filter files
	pg.filterFiles()

	// get page cache stats of files.
	stats := pg.getPageCacheStats()

	// print
	pg.output(stats, pg.option.limit)
}

func wildcardMatchMulti(s, patterns string) bool {
	for _, p := range strings.Split(patterns, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if wildcardMatch(s, p) {
			return true
		}
	}
	return false
}

func wildcardMatch(s string, p string) bool {
	if strings.Contains(s, p) {
		return true
	}

	runeInput := []rune(s)
	runePattern := []rune(p)

	lenInput := len(runeInput)
	lenPattern := len(runePattern)

	isMatchingMatrix := make([][]bool, lenInput+1)

	for i := range isMatchingMatrix {
		isMatchingMatrix[i] = make([]bool, lenPattern+1)
	}

	isMatchingMatrix[0][0] = true
	for i := 1; i < lenInput; i++ {
		isMatchingMatrix[i][0] = false
	}

	if lenPattern > 0 {
		if runePattern[0] == '*' {
			isMatchingMatrix[0][1] = true
		}
	}

	for j := 2; j <= lenPattern; j++ {
		if runePattern[j-1] == '*' {
			isMatchingMatrix[0][j] = isMatchingMatrix[0][j-1]
		}
	}

	for i := 1; i <= lenInput; i++ {
		for j := 1; j <= lenPattern; j++ {

			if runePattern[j-1] == '*' {
				isMatchingMatrix[i][j] = isMatchingMatrix[i-1][j] || isMatchingMatrix[i][j-1]
			}

			if runePattern[j-1] == '?' || runeInput[i-1] == runePattern[j-1] {
				isMatchingMatrix[i][j] = isMatchingMatrix[i-1][j-1]
			}
		}
	}

	return isMatchingMatrix[lenInput][lenPattern]
}

func walkDirs(dirs []string, maxDepth int) []string {
	if len(dirs) == 0 {
		return dirs
	}

	var files []string
	for _, dir := range dirs {
		fs, err := os.Stat(dir)
		if err != nil {
			files = append(files, dir)
			continue
		}

		// is dir
		if fs.IsDir() {
			files = append(files, walkDir(dir, 0, maxDepth)...)
			continue
		}

		// is file
		files = append(files, dir)
	}

	return files
}

func walkDir(dir string, depth int, maxDepth int) []string {
	if depth > maxDepth {
		return nil
	}

	var files []string
	ofiles, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, file := range ofiles {
		curdir := path.Join(dir, file.Name())
		if file.IsDir() {
			files = append(files, walkDir(curdir, depth+1, maxDepth)...)
			continue
		}

		files = append(files, curdir)
	}

	return files
}
