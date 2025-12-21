#!/bin/bash

# Cleanup Test Environment Script
# Cleans up the test environment after testing

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Directory paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
TEST_DIR="$PROJECT_ROOT/test"
WEBHOOK_PID_FILE="$TEST_DIR/webhook_server.pid"

# Logging
log() {
    echo -e "${GREEN}[CLEANUP]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Stop webhook server
stop_webhook_server() {
    log "Stopping webhook server..."
    
    if [[ ! -f "$WEBHOOK_PID_FILE" ]]; then
        warn "Webhook server PID file not found"
        return 0
    fi
    
    local pid=$(cat "$WEBHOOK_PID_FILE")
    
    if kill -0 "$pid" 2>/dev/null; then
        log "Terminating webhook server (PID: $pid)..."
        
        # Try graceful shutdown first
        kill "$pid" 2>/dev/null || true
        
        # Wait up to 5 seconds for graceful shutdown
        local count=0
        while [[ $count -lt 5 ]]; do
            if ! kill -0 "$pid" 2>/dev/null; then
                break
            fi
            sleep 1
            ((count++))
        done
        
        # Force kill if still running
        if kill -0 "$pid" 2>/dev/null; then
            warn "Force killing webhook server..."
            kill -9 "$pid" 2>/dev/null || true
        fi
        
        log "✓ Webhook server stopped"
    else
        warn "Webhook server process not found (PID: $pid)"
    fi
    
    # Remove PID file
    rm -f "$WEBHOOK_PID_FILE"
}

# Clean up test directories
cleanup_directories() {
    log "Cleaning up test directories..."
    
    local dirs_to_clean=(
        "$PROJECT_ROOT/test_deployments"
        "$PROJECT_ROOT/test_self_update"
        "$TEST_DIR/test_repos"
    )
    
    for dir in "${dirs_to_clean[@]}"; do
        if [[ -d "$dir" ]]; then
            log "Removing directory: $dir"
            rm -rf "$dir"
        else
            info "Directory not found: $dir"
        fi
    done
    
    log "✓ Test directories cleaned"
}

# Archive test logs
archive_logs() {
    local logs_dir="$TEST_DIR/test_logs"
    local archive_dir="$TEST_DIR/archived_logs"
    
    if [[ ! -d "$logs_dir" ]] || [[ -z "$(ls -A "$logs_dir" 2>/dev/null)" ]]; then
        info "No test logs to archive"
        return 0
    fi
    
    log "Archiving test logs..."
    
    # Create archive directory if it doesn't exist
    mkdir -p "$archive_dir"
    
    # Create timestamped archive
    local timestamp=$(date +%Y%m%d_%H%M%S)
    local archive_file="$archive_dir/test_logs_$timestamp.tar.gz"
    
    tar -czf "$archive_file" -C "$TEST_DIR" test_logs/
    
    # Remove original logs
    rm -rf "$logs_dir"
    
    log "✓ Test logs archived to: $archive_file"
}

# Check for any remaining processes
check_remaining_processes() {
    log "Checking for remaining processes..."
    
    # Check for any remaining binaryDeploy processes
    local remaining_processes=$(pgrep -f "binaryDeploy" || true)
    
    if [[ -n "$remaining_processes" ]]; then
        warn "Found remaining binaryDeploy processes:"
        echo "$remaining_processes" | while read -r pid; do
            local cmd=$(ps -p "$pid" -o cmd= 2>/dev/null || echo "Process not found")
            warn "  PID $pid: $cmd"
        done
        
        read -p "Do you want to kill these processes? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "$remaining_processes" | xargs kill -9 2>/dev/null || true
            log "✓ Remaining processes killed"
        else
            warn "Remaining processes left running"
        fi
    else
        log "✓ No remaining processes found"
    fi
}

# Clean up temporary files
cleanup_temp_files() {
    log "Cleaning up temporary files..."
    
    # Remove any temp files that might be left behind
    local temp_patterns=(
        "/tmp/binaryDeploy_*"
        "/tmp/webhook_test_*"
        "/tmp/setup_*"
    )
    
    for pattern in "${temp_patterns[@]}"; do
        for file in $pattern; do
            if [[ -e "$file" ]]; then
                rm -f "$file"
                info "Removed temporary file: $file"
            fi
        done
    done
    
    log "✓ Temporary files cleaned"
}

# Show cleanup summary
show_summary() {
    log ""
    log "🧹 Cleanup completed!"
    log ""
    log "Cleanup Actions Performed:"
    echo "  ✓ Webhook server stopped"
    echo "  ✓ Test directories removed"
    echo "  ✓ Temporary files cleaned"
    echo "  ✓ Logs archived (if any existed)"
    log ""
    info "Environment is now clean and ready for next test run"
}

# Force cleanup option
force_cleanup() {
    log "Performing force cleanup..."
    
    # Kill any binaryDeploy processes forcefully
    pkill -f "binaryDeploy" 2>/dev/null || true
    
    # Remove all test-related directories
    local test_dirs=(
        "$PROJECT_ROOT/test_deployments"
        "$PROJECT_ROOT/test_self_update"
        "$TEST_DIR/test_repos"
        "$TEST_DIR/test_logs"
        "$TEST_DIR/archived_logs"
    )
    
    for dir in "${test_dirs[@]}"; do
        if [[ -d "$dir" ]]; then
            rm -rf "$dir"
            info "Force removed: $dir"
        fi
    done
    
    # Remove PID file
    rm -f "$WEBHOOK_PID_FILE"
    
    log "✓ Force cleanup completed"
}

# Main cleanup function
main() {
    local force=false
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --force|-f)
                force=true
                shift
                ;;
            --help|-h)
                echo "Usage: $0 [--force]"
                echo ""
                echo "Options:"
                echo "  --force, -f    Force cleanup (kills all processes, removes all test data)"
                echo "  --help, -h     Show this help message"
                exit 0
                ;;
            *)
                error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
    
    log "Starting test environment cleanup..."
    
    if [[ "$force" == true ]]; then
        force_cleanup
        show_summary
        return 0
    fi
    
    # Normal cleanup sequence
    stop_webhook_server
    cleanup_directories
    
    # Ask about logs
    if [[ -d "$TEST_DIR/test_logs" ]] && [[ -n "$(ls -A "$TEST_DIR/test_logs" 2>/dev/null)" ]]; then
        read -p "Do you want to archive test logs? (Y/n): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Nn]$ ]]; then
            info "Test logs left in place"
        else
            archive_logs
        fi
    fi
    
    cleanup_temp_files
    check_remaining_processes
    show_summary
}

# Run main function with all arguments
main "$@"