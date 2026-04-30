package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/dustin/go-humanize"
	"github.com/rfyiamcool/pgcacher/pkg/pcstats"
)

type option struct {
	pid, worker, depth, limit             int
	top, terse, json, unicode             bool
	plain, bname, enhancedNs, verbose     bool
	statBlockdev                          bool
	leastSize, excludeFiles, includeFiles string
	container                             string
}

var globalOption = new(option)

func init() {
	// basic params
	flag.IntVar(&globalOption.pid, "pid", 0, "show all open maps for the given pid")
	flag.IntVar(&globalOption.limit, "limit", 500, "limit the number of files displayed")
	flag.BoolVar(&globalOption.top, "top", false, "scan the open files of all processes, show the top few files that occupy the most memory space in the page cache.")
	flag.IntVar(&globalOption.depth, "depth", 0, "set the depth of dirs to scan")
	flag.IntVar(&globalOption.worker, "worker", 2, "concurrency workers")
	flag.StringVar(&globalOption.leastSize, "least-size", "0mb", "ignore files smaller than the leastSize, such as 10MB and 15GB")
	flag.StringVar(&globalOption.excludeFiles, "exclude-files", "", "exclude the specified files by wildcard, such as 'a*c?d' and '*xiaorui*,rfyiamcool'")
	flag.StringVar(&globalOption.includeFiles, "include-files", "", "only include the specified files by wildcard, such as 'a*c?d' and '*xiaorui?cc,rfyiamcool'")
	flag.BoolVar(&globalOption.statBlockdev, "statblockdev", false, "include page cache of block devices (/dev/sdX, /dev/nvmeXnY) held by scanned processes; may be SLOW since device size is read via BLKGETSIZE64 ioctl")

	// show params
	flag.BoolVar(&globalOption.terse, "terse", false, "show terse output")
	flag.BoolVar(&globalOption.json, "json", false, "return data in JSON format")
	flag.BoolVar(&globalOption.unicode, "unicode", false, "return data with unicode box characters")
	flag.BoolVar(&globalOption.plain, "plain", false, "return data with no box characters")
	flag.BoolVar(&globalOption.bname, "bname", false, "convert paths to basename to narrow the output")

	// container support params
	flag.BoolVar(&globalOption.enhancedNs, "enhanced-ns", false, "use enhanced namespace switching for better container support (similar to nsenter)")
	flag.BoolVar(&globalOption.verbose, "verbose", false, "enable verbose logging for debugging namespace operations")
	flag.StringVar(&globalOption.container, "container", "", "container ID (>=12 hex chars) to analyze; resolves to host PID via /proc/<pid>/cgroup, no docker/nsenter required")
}

func main() {
	// prepare phase
	flag.Parse()
	if runtime.GOOS != "linux" {
		log.Fatalf("pgcacher only support running on Linux !!!")
	}
	leastSize, err := humanize.ParseBytes(globalOption.leastSize)
	if err != nil {
		log.Fatalf("invalid -least-size value %q: %v", globalOption.leastSize, err)
	}

	// resolve -container to a host PID; implies -enhanced-ns since we're
	// crossing namespaces. Conflicts with an explicit -pid.
	if globalOption.container != "" {
		if globalOption.pid != 0 {
			log.Fatalf("-container and -pid are mutually exclusive")
		}
		resolved, err := pcstats.ResolveContainerPID(globalOption.container)
		if err != nil {
			log.Fatalf("failed to resolve container %q: %v", globalOption.container, err)
		}
		if globalOption.verbose {
			log.Printf("container %s -> host pid %d", globalOption.container, resolved)
		}
		globalOption.pid = resolved
		globalOption.enhancedNs = true
	}

	// running phase
	files := flag.Args()
	files = walkDirs(files, globalOption.depth)

	// init pgcacher obj
	pg := pgcacher{
		files:     files,
		leastSize: int64(leastSize),
		option:    globalOption,
	}

	if globalOption.top {
		pg.handleTop()
		os.Exit(0)
	}

	if globalOption.pid != 0 {
		pg.appendProcessFiles(globalOption.pid)
	}

	if len(pg.files) == 0 {
		fmt.Println("the files is null ???")
		flag.Usage()
		os.Exit(1)
	}

	pg.filterFiles()
	stats := pg.getPageCacheStats()
	pg.output(stats, pg.option.limit)
}
