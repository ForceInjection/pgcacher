#!/bin/bash

# pgcacher-container.sh
# A helper script to run pgcacher in containerized environments
# This script provides nsenter-like functionality for pgcacher

set -e

# Default values
CONTAINER_ID=""
CONTAINER_NAME=""
TARGET_PID=""
PGCACHER_ARGS=""
VERBOSE=false
USE_ENHANCED_NS=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print usage information
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

A helper script to run pgcacher in containerized environments.
Provides functionality similar to: nsenter --target <pid> -p -m ./pgcacher -pid=<target_pid>

Options:
    -c, --container-id ID       Docker container ID
    -n, --container-name NAME   Docker container name
    -p, --target-pid PID        Target process PID inside container (optional)
    -a, --args ARGS             Additional arguments to pass to pgcacher
    -e, --enhanced-ns           Use enhanced namespace switching (experimental)
    -v, --verbose               Enable verbose output
    -h, --help                  Show this help message

Examples:
    # Analyze page cache for a container by ID
    $0 -c abc123def456
    
    # Analyze page cache for a container by name
    $0 -n my-app-container
    
    # Analyze specific process inside container
    $0 -c abc123def456 -p 123 -v
    
    # Use enhanced namespace switching (experimental)
    $0 -c abc123def456 -e -v
    
    # Pass additional arguments to pgcacher
    $0 -c abc123def456 -a "-limit 100 -json"

EOF
}

# Print colored output
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Get container PID from Docker container ID or name
get_container_pid() {
    local container="$1"
    local pid
    
    if ! command_exists docker; then
        print_error "Docker command not found. Please install Docker."
        exit 1
    fi
    
    pid=$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null)
    if [ $? -ne 0 ] || [ -z "$pid" ] || [ "$pid" = "0" ]; then
        print_error "Failed to get PID for container: $container"
        print_error "Make sure the container exists and is running."
        exit 1
    fi
    
    echo "$pid"
}

# Get target process PID inside container
get_target_pid_in_container() {
    local container_pid="$1"
    local target_pid="$2"
    
    if [ -n "$target_pid" ]; then
        echo "$target_pid"
        return
    fi
    
    # If no specific PID provided, use PID 1 (init process in container)
    echo "1"
}

# Run pgcacher with nsenter
run_with_nsenter() {
    local container_pid="$1"
    local target_pid="$2"
    local pgcacher_args="$3"
    
    if ! command_exists nsenter; then
        print_error "nsenter command not found. Please install util-linux package."
        exit 1
    fi
    
    print_info "Using nsenter to switch namespaces..."
    print_info "Container PID: $container_pid"
    print_info "Target PID: $target_pid"
    
    # Build the commands as arrays to avoid injection via word splitting
    local -a nsenter_cmd=(nsenter --target "$container_pid" -p -m)
    local -a pgcacher_cmd=(./pgcacher "-pid=$target_pid")
    if [ -n "$pgcacher_args" ]; then
        read -ra extra_args <<< "$pgcacher_args"
        pgcacher_cmd+=("${extra_args[@]}")
    fi
    
    if [ "$VERBOSE" = true ]; then
        print_info "Executing: ${nsenter_cmd[*]} ${pgcacher_cmd[*]}"
    fi
    
    # Execute the command
    "${nsenter_cmd[@]}" "${pgcacher_cmd[@]}"
}

# Run pgcacher with enhanced namespace switching
run_with_enhanced_ns() {
    local target_pid="$1"
    local pgcacher_args="$2"
    
    print_info "Using enhanced namespace switching..."
    print_info "Target PID: $target_pid"
    
    # Build the pgcacher command as an array
    local -a pgcacher_cmd=(./pgcacher "-pid=$target_pid" -enhanced-ns)
    if [ -n "$pgcacher_args" ]; then
        read -ra extra_args <<< "$pgcacher_args"
        pgcacher_cmd+=("${extra_args[@]}")
    fi
    
    if [ "$VERBOSE" = true ]; then
        pgcacher_cmd+=(-verbose)
        print_info "Executing: ${pgcacher_cmd[*]}"
    fi
    
    # Execute the command
    "${pgcacher_cmd[@]}"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--container-id)
            CONTAINER_ID="$2"
            shift 2
            ;;
        -n|--container-name)
            CONTAINER_NAME="$2"
            shift 2
            ;;
        -p|--target-pid)
            TARGET_PID="$2"
            shift 2
            ;;
        -a|--args)
            PGCACHER_ARGS="$2"
            shift 2
            ;;
        -e|--enhanced-ns)
            USE_ENHANCED_NS=true
            shift
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate input
if [ -z "$CONTAINER_ID" ] && [ -z "$CONTAINER_NAME" ]; then
    print_error "Either container ID or container name must be specified."
    usage
    exit 1
fi

# Determine container identifier
CONTAINER="${CONTAINER_ID:-$CONTAINER_NAME}"

# Check if pgcacher binary exists
if [ ! -f "./pgcacher" ]; then
    print_error "pgcacher binary not found in current directory."
    print_error "Please make sure you're running this script from the pgcacher directory."
    exit 1
fi

# Get container PID
print_info "Getting container PID for: $CONTAINER"
CONTAINER_PID=$(get_container_pid "$CONTAINER")
print_success "Container PID: $CONTAINER_PID"

# Get target PID inside container
TARGET_PID_IN_CONTAINER=$(get_target_pid_in_container "$CONTAINER_PID" "$TARGET_PID")

# Choose execution method
if [ "$USE_ENHANCED_NS" = true ]; then
    print_info "Using enhanced namespace switching mode (experimental)"
    run_with_enhanced_ns "$TARGET_PID_IN_CONTAINER" "$PGCACHER_ARGS"
else
    print_info "Using nsenter mode (recommended)"
    run_with_nsenter "$CONTAINER_PID" "$TARGET_PID_IN_CONTAINER" "$PGCACHER_ARGS"
fi

print_success "pgcacher execution completed."