package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/rfyiamcool/pgcacher/pkg/pcstats"
)

// Test program for enhanced namespace switching functionality
func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <target_pid> [verbose]\n", os.Args[0])
		fmt.Println("\nThis program tests the enhanced namespace switching functionality.")
		fmt.Println("It will attempt to switch to the target process's namespaces.")
		fmt.Println("\nExample:")
		fmt.Printf("  %s 1234 true    # Test with PID 1234, verbose output\n", os.Args[0])
		fmt.Printf("  %s 1234         # Test with PID 1234, quiet output\n", os.Args[0])
		os.Exit(1)
	}

	// Parse target PID
	targetPid, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatalf("Invalid PID: %s", os.Args[1])
	}

	// Parse verbose flag
	verbose := false
	if len(os.Args) > 2 {
		verbose = os.Args[2] == "true"
	}

	fmt.Printf("Testing enhanced namespace switching for PID %d\n", targetPid)
	fmt.Printf("Verbose mode: %t\n", verbose)
	fmt.Println("----------------------------------------")

	// Test 1: Basic mount namespace switching
	fmt.Println("Test 1: Enhanced mount namespace switching")
	err = pcstats.EnhancedSwitchMountNs(targetPid, verbose)
	if err != nil {
		fmt.Printf("❌ Enhanced mount namespace switching failed: %v\n", err)
	} else {
		fmt.Println("✅ Enhanced mount namespace switching succeeded")
	}
	fmt.Println()

	// Test 2: Full container context switching
	fmt.Println("Test 2: Full container context switching (mount + pid namespaces)")
	err = pcstats.SwitchToContainerContext(targetPid, verbose)
	if err != nil {
		fmt.Printf("❌ Container context switching failed: %v\n", err)
	} else {
		fmt.Println("✅ Container context switching succeeded")
	}
	fmt.Println()

	// Test 3: Individual namespace switching
	fmt.Println("Test 3: Individual namespace switching")
	switcher := pcstats.NewEnhancedNsSwitcher(targetPid, verbose)

	// Test different namespace types
	namespaces := []pcstats.NamespaceType{
		pcstats.MountNS,
		pcstats.PidNS,
		pcstats.NetNS,
		pcstats.IpcNS,
		pcstats.UtsNS,
	}

	for _, ns := range namespaces {
		err := switcher.SwitchNamespace(ns)
		if err != nil {
			fmt.Printf("❌ %s namespace switching failed: %v\n", ns, err)
		} else {
			fmt.Printf("✅ %s namespace switching succeeded\n", ns)
		}
	}
	fmt.Println()

	// Test 4: Compare with basic namespace switching
	fmt.Println("Test 4: Comparison with basic namespace switching")
	fmt.Println("Testing basic SwitchMountNs function...")
	pcstats.SwitchMountNs(targetPid)
	fmt.Println("✅ Basic mount namespace switching completed (no error checking)")
	fmt.Println()

	fmt.Println("========================================")
	fmt.Println("Enhanced namespace switching test completed.")
	fmt.Println("")
	fmt.Println("Note: Some namespace switches may fail due to:")
	fmt.Println("- Insufficient permissions (need CAP_SYS_ADMIN)")
	fmt.Println("- Target process in same namespace")
	fmt.Println("- Kernel doesn't support certain namespace types")
	fmt.Println("")
	fmt.Println("For container environments, try:")
	fmt.Printf("  sudo %s <container_main_pid> true\n", os.Args[0])
}
