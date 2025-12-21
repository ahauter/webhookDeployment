#!/bin/bash

# Mock Git Server Script
# Simulates an external git server (GitHub) for testing webhook functionality

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test repository paths
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOS_DIR="$TEST_DIR/test_repos"
TARGET_APP_REPO="$REPOS_DIR/test_target_app"
BINARYDEPLOY_UPDATE_REPO="$REPOS_DIR/test_binarydeploy_updater"

# Logging
log() {
    echo -e "${GREEN}[MOCK_GIT]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Initialize test repositories
setup_test_repos() {
    log "Setting up test repositories..."
    
    # Clean up existing repos
    rm -rf "$REPOS_DIR"
    mkdir -p "$REPOS_DIR"
    
    # Setup target app repository
    setup_target_app_repo
    
    # Setup binaryDeploy update repository
    setup_binarydeploy_update_repo
    
    log "Test repositories setup complete"
}

# Setup target app repository
setup_target_app_repo() {
    log "Setting up target app repository..."
    
    mkdir -p "$TARGET_APP_REPO"
    cd "$TARGET_APP_REPO"
    
    # Initialize git repo
    git init -q
    git config user.email "test@example.com"
    git config user.name "Test User"
    
    # Create initial files
    cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Hello from target app!")
}
EOF

    # Create deploy.config
    cat > deploy.config << 'EOF'
# Target App Test Configuration
build_command=echo "Building test app" && go build -o test_app_binary .
run_command=./test_app_binary
working_dir=./
environment=test
port=8082
restart_delay=2
max_restarts=3
EOF

    # Create go.mod
    cat > go.mod << 'EOF'
module test_target_app

go 1.21
EOF

    # Initial commit
    git add .
    git commit -q -m "Initial commit"
    
    log "Target app repository initialized at $TARGET_APP_REPO"
}

# Setup binaryDeploy update repository
setup_binarydeploy_update_repo() {
    log "Setting up binaryDeploy update repository..."
    
    mkdir -p "$BINARYDEPLOY_UPDATE_REPO"
    cd "$BINARYDEPLOY_UPDATE_REPO"
    
    # Initialize git repo
    git init -q
    git config user.email "test@example.com"
    git config user.name "Test User"
    
    # Create a mock binaryDeploy source
    cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Updated binaryDeploy test version!")
}
EOF

    # Create deploy.config for self-update
    cat > deploy.config << 'EOF'
# BinaryDeploy Self-Update Test Configuration
build_command=echo "Building binaryDeploy test" && go build -o binaryDeploy_test .
restart_command=echo "Mock restart command executed for binaryDeploy"
backup_binary=./test/binaryDeploy_test.backup
EOF

    # Create go.mod
    cat > go.mod << 'EOF'
module test_binarydeploy_updater

go 1.21
EOF

    # Initial commit
    git add .
    git commit -q -m "Initial commit - binaryDeploy updater"
    
    log "BinaryDeploy update repository initialized at $BINARYDEPLOY_UPDATE_REPO"
}

# Simulate a push to target app repository
push_to_target_app() {
    local message="${1:-"Test commit to target app"}"
    local branch="${2:-main}"
    
    log "Simulating push to target app (branch: $branch)..."
    
    cd "$TARGET_APP_REPO"
    
    # Make a change to simulate update
    echo "// Updated at $(date)" >> main.go
    
    # Commit the change
    git add .
    git commit -q -m "$message"
    
    # Get the commit hash
    local commit_hash=$(git rev-parse HEAD)
    
    log "Push completed. Commit hash: $commit_hash"
    echo "$commit_hash"
}

# Simulate a push to binaryDeploy update repository
push_to_binarydeploy_update() {
    local message="${1:-"Test commit to binaryDeploy updater"}"
    local branch="${2:-main}"
    
    log "Simulating push to binaryDeploy update (branch: $branch)..."
    
    cd "$BINARYDEPLOY_UPDATE_REPO"
    
    # Make a change to simulate update
    echo "// BinaryDeploy updated at $(date)" >> main.go
    
    # Commit the change
    git add .
    git commit -q -m "$message"
    
    # Get the commit hash
    local commit_hash=$(git rev-parse HEAD)
    
    log "Push completed. Commit hash: $commit_hash"
    echo "$commit_hash"
}

# Get repository state for webhook payload generation
get_repo_state() {
    local repo_path="$1"
    local repo_name="$2"
    local branch="${3:-main}"
    
    if [[ ! -d "$repo_path" ]]; then
        error "Repository not found: $repo_path"
        return 1
    fi
    
    cd "$repo_path"
    
    # Get latest commit hash
    local commit_hash=$(git rev-parse HEAD)
    
    # Get current branch
    local current_branch=$(git rev-parse --abbrev-ref HEAD)
    
    # Use provided branch if different from current
    if [[ "$branch" != "$current_branch" ]]; then
        # Check if branch exists
        if git rev-parse --verify "$branch" >/dev/null 2>&1; then
            commit_hash=$(git rev-parse "$branch")
        else
            warn "Branch $branch not found, using current branch $current_branch"
            branch="$current_branch"
        fi
    fi
    
    # Output JSON-ready data
    echo "commit_hash:$commit_hash"
    echo "branch:$branch"
    echo "repo_name:$repo_name"
    echo "repo_url:file://$repo_path"
}

# Create a new branch in a repository
create_branch() {
    local repo_path="$1"
    local branch_name="$2"
    
    log "Creating branch $branch_name in $(basename "$repo_path")..."
    
    cd "$repo_path"
    
    # Create and checkout new branch
    git checkout -q -b "$branch_name"
    
    # Make a small change
    echo "// Change on branch $branch_name at $(date)" >> main.go
    
    # Commit the change
    git add .
    git commit -q -m "Add feature on $branch_name"
    
    # Switch back to main
    git checkout -q main
    
    log "Branch $branch_name created successfully"
}

# Cleanup test repositories
cleanup_repos() {
    log "Cleaning up test repositories..."
    rm -rf "$REPOS_DIR"
    log "Cleanup complete"
}

# Show repository status
show_repo_status() {
    log "Repository Status:"
    echo "Target App: $TARGET_APP_REPO"
    echo "BinaryDeploy Update: $BINARYDEPLOY_UPDATE_REPO"
    
    if [[ -d "$TARGET_APP_REPO" ]]; then
        cd "$TARGET_APP_REPO"
        echo "Target App - Branch: $(git rev-parse --abbrev-ref HEAD), Commit: $(git rev-parse --short HEAD)"
    fi
    
    if [[ -d "$BINARYDEPLOY_UPDATE_REPO" ]]; then
        cd "$BINARYDEPLOY_UPDATE_REPO"
        echo "BinaryDeploy Update - Branch: $(git rev-parse --abbrev-ref HEAD), Commit: $(git rev-parse --short HEAD)"
    fi
}

# Main function
main() {
    case "${1:-help}" in
        setup)
            setup_test_repos
            ;;
        push-target)
            push_to_target_app "${2:-}" "${3:-main}"
            ;;
        push-binarydeploy)
            push_to_binarydeploy_update "${2:-}" "${3:-main}"
            ;;
        get-state)
            get_repo_state "$2" "$3" "${4:-main}"
            ;;
        create-branch)
            create_branch "$2" "$3"
            ;;
        cleanup)
            cleanup_repos
            ;;
        status)
            show_repo_status
            ;;
        help|*)
            echo "Usage: $0 {setup|push-target|push-binarydeploy|get-state|create-branch|cleanup|status}"
            echo ""
            echo "Commands:"
            echo "  setup                    - Initialize test repositories"
            echo "  push-target [msg] [branch] - Simulate push to target app repo"
            echo "  push-binarydeploy [msg] [branch] - Simulate push to binaryDeploy update repo"
            echo "  get-state <path> <name> [branch] - Get repository state for webhook payload"
            echo "  create-branch <path> <branch> - Create new branch in repository"
            echo "  cleanup                  - Remove test repositories"
            echo "  status                   - Show repository status"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"