#!/bin/bash

# Setup Test Environment Script
# Prepares the environment for webhook testing

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
REPOS_DIR="$TEST_DIR/test_repos"
LOGS_DIR="$TEST_DIR/test_logs"
DEPLOY_DIR="$PROJECT_ROOT/test_deployments"
SELF_UPDATE_DIR="$PROJECT_ROOT/test_self_update"

# Configuration
WEBHOOK_SERVER_BINARY="$PROJECT_ROOT/binaryDeploy"
TEST_CONFIG="$TEST_DIR/test_config.json"
WEBHOOK_PID_FILE="$TEST_DIR/webhook_server.pid"
LOG_FILE="$LOGS_DIR/setup_$(date +%Y%m%d_%H%M%S).log"

# Logging
log() {
    echo -e "${GREEN}[SETUP]${NC} $1" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$LOG_FILE" >&2
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" | tee -a "$LOG_FILE"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $1" | tee -a "$LOG_FILE"
}

# Check if required tools are installed
check_dependencies() {
    log "Checking dependencies..."
    
    local deps=("curl" "jq" "openssl" "git" "nc")
    local missing=()
    
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            missing+=("$dep")
        else
            info "✓ $dep found"
        fi
    done
    
    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing dependencies: ${missing[*]}"
        error "Please install the missing tools and try again."
        exit 1
    fi
    
    log "All dependencies found"
}

# Check if Go is available for building
check_go_dependencies() {
    log "Checking Go dependencies..."
    
    if ! command -v go &> /dev/null; then
        error "Go is not installed or not in PATH"
        exit 1
    fi
    
    info "✓ Go found: $(go version)"
    
    # Check if we can build the project
    cd "$PROJECT_ROOT"
    if ! go mod tidy &>/dev/null; then
        warn "go mod tidy had issues, but continuing..."
    fi
    
    if ! go build -o binaryDeploy . &>/dev/null; then
        error "Failed to build binaryDeploy"
        exit 1
    fi
    
    log "✓ binaryDeploy built successfully"
}

# Create necessary directories
create_directories() {
    log "Creating test directories..."
    
    mkdir -p "$REPOS_DIR" "$LOGS_DIR" "$DEPLOY_DIR" "$SELF_UPDATE_DIR"
    
    # Set appropriate permissions
    chmod 755 "$TEST_DIR" "$REPOS_DIR" "$LOGS_DIR" "$DEPLOY_DIR" "$SELF_UPDATE_DIR"
    
    log "Test directories created"
}

# Validate test configuration
validate_config() {
    log "Validating test configuration..."
    
    if [[ ! -f "$TEST_CONFIG" ]]; then
        error "Test configuration file not found: $TEST_CONFIG"
        exit 1
    fi
    
    # Validate JSON format
    if ! jq empty "$TEST_CONFIG" 2>/dev/null; then
        error "Test configuration file is not valid JSON: $TEST_CONFIG"
        exit 1
    fi
    
    # Extract configuration values
    local port=$(jq -r '.port' "$TEST_CONFIG")
    local target_repo=$(jq -r '.target_repo_url' "$TEST_CONFIG")
    local self_update_repo=$(jq -r '.self_update_repo_url' "$TEST_CONFIG")
    
    info "Configuration validated:"
    info "  Port: $port"
    info "  Target Repo: $target_repo"
    info "  Self Update Repo: $self_update_repo"
    
    log "✓ Configuration validation passed"
}

# Setup test repositories using mock git server
setup_repositories() {
    log "Setting up test repositories..."
    
    "$SCRIPT_DIR/mock_git_server.sh" setup
    
    # Create additional test branches
    "$SCRIPT_DIR/mock_git_server.sh" create-branch "$REPOS_DIR/test_target_app" "test-branch"
    "$SCRIPT_DIR/mock_git_server.sh" create-branch "$REPOS_DIR/test_binarydeploy_updater" "test-branch"
    
    log "✓ Test repositories setup complete"
}

# Check if port is available
check_port() {
    local port="$1"
    if nc -z localhost "$port" 2>/dev/null; then
        warn "Port $port is already in use"
        return 1
    fi
    return 0
}

# Start webhook server for testing
start_webhook_server() {
    log "Starting webhook server for testing..."
    
    # Check if server is already running
    if [[ -f "$WEBHOOK_PID_FILE" ]]; then
        local pid=$(cat "$WEBHOOK_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            warn "Webhook server is already running (PID: $pid)"
            return 0
        else
            warn "Stale PID file found, removing..."
            rm -f "$WEBHOOK_PID_FILE"
        fi
    fi
    
    # Get port from config
    local port=$(jq -r '.port' "$TEST_CONFIG")
    
    if ! check_port "$port"; then
        error "Port $port is not available"
        error "Please stop the service using this port or change the test configuration"
        exit 1
    fi
    
    # Start server in background with test config
    cd "$PROJECT_ROOT"
    
    # Backup original config and use test config
    if [[ -f "config.json" ]]; then
        mv config.json config.json.backup
    fi
    cp "$TEST_CONFIG" config.json
    ./binaryDeploy &
    local server_pid=$!
    
    # Save PID
    echo "$server_pid" > "$WEBHOOK_PID_FILE"
    
    # Wait for server to start
    local max_wait=10
    local wait_count=0
    
    while [[ $wait_count -lt $max_wait ]]; do
        if kill -0 "$server_pid" 2>/dev/null && nc -z localhost "$port" 2>/dev/null; then
            log "✓ Webhook server started successfully (PID: $server_pid, Port: $port)"
            return 0
        fi
        
        sleep 1
        ((wait_count++))
    done
    
    error "Failed to start webhook server"
    if kill -0 "$server_pid" 2>/dev/null; then
        kill "$server_pid" 2>/dev/null || true
    fi
    rm -f "$WEBHOOK_PID_FILE"
    exit 1
}

# Create a simple test to verify server is working
verify_server() {
    log "Verifying webhook server..."
    
    local port=$(jq -r '.port' "$TEST_CONFIG")
    
    # Test basic endpoint
    local basic_response=$(curl -s "http://localhost:$port/" 2>/dev/null || echo "failed")
    if echo "$basic_response" | grep -q "Webhook server is running"; then
        log "✓ Webhook server is responding correctly"
    else
        error "Webhook server is not responding correctly"
        error "Response received: '$basic_response'"
        return 1
    fi
    
    # Test webhook endpoint (should fail with missing signature)
    local response_file=$(mktemp)
    local http_code=$(curl -s -w "%{http_code}" -X POST "http://localhost:$port/webhook" -d '{}' -o "$response_file" || echo "000")
    local response_body=$(cat "$response_file")
    rm -f "$response_file"
    
    if [[ "$http_code" == "401" ]]; then
        log "✓ Webhook endpoint is properly secured"
    else
        warn "Webhook endpoint returned unexpected status: $http_code (expected 401 for missing signature)"
        warn "Response body: $response_body"
    fi
}

# Main setup function
main() {
    log "Starting test environment setup..."
    log "Log file: $LOG_FILE"
    
    # Run setup steps
    check_dependencies
    check_go_dependencies
    create_directories
    validate_config
    setup_repositories
    start_webhook_server
    verify_server
    
    log ""
    log "🎉 Test environment setup complete!"
    log ""
    log "Test Environment Details:"
    info "  Webhook Server: http://localhost:$(jq -r '.port' "$TEST_CONFIG")"
    info "  Test Repositories: $REPOS_DIR"
    info "  Logs Directory: $LOGS_DIR"
    info "  Server PID: $(cat "$WEBHOOK_PID_FILE")"
    info "  Config File: $TEST_CONFIG"
    log ""
    log "Next steps:"
    echo "  1. Run tests: $SCRIPT_DIR/test_webhook.sh"
    echo "  2. View logs: ls -la $LOGS_DIR"
    echo "  3. Cleanup: $SCRIPT_DIR/cleanup_test_env.sh"
    log ""
}

# Cleanup on exit
cleanup_on_exit() {
    local exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        error "Setup failed with exit code $exit_code"
        error "Check log file for details: $LOG_FILE"
    fi
    
    # Restore original config if backup exists
    cd "$PROJECT_ROOT"
    if [[ -f "config.json.backup" ]]; then
        mv config.json.backup config.json
    fi
}

# Set trap for cleanup
trap cleanup_on_exit EXIT

# Run main function
main "$@"