#!/bin/bash

# Webhook Test Script
# Comprehensive end-to-end testing of the webhook endpoint

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# Directory paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
TEST_DIR="$PROJECT_ROOT/test"
REPOS_DIR="$TEST_DIR/test_repos"
LOGS_DIR="$TEST_DIR/test_logs"

# Configuration files
TEST_CONFIG="$TEST_DIR/test_config.json"
WEBHOOK_PID_FILE="$TEST_DIR/webhook_server.pid"

# Test configuration
WEBHOOK_URL="http://localhost:$(jq -r '.port' "$TEST_CONFIG")"
WEBHOOK_SECRET=$(jq -r '.secret' "$TEST_CONFIG")
TARGET_REPO_URL=$(jq -r '.target_repo_url' "$TEST_CONFIG")
SELF_UPDATE_REPO_URL=$(jq -r '.self_update_repo_url' "$TEST_CONFIG")

# Logging
TEST_LOG_FILE="$LOGS_DIR/webhook_test_$(date +%Y%m%d_%H%M%S).log"
TEST_RESULTS=()

log() {
    echo -e "${GREEN}[TEST]${NC} $1" | tee -a "$TEST_LOG_FILE"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$TEST_LOG_FILE" >&2
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" | tee -a "$TEST_LOG_FILE"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $1" | tee -a "$TEST_LOG_FILE"
}

test_result() {
    local test_name="$1"
    local status="$2"
    local details="$3"
    
    local status_icon
    case "$status" in
        "PASS") status_icon="✅" ;;
        "FAIL") status_icon="❌" ;;
        "SKIP") status_icon="⏭️" ;;
        *) status_icon="❓" ;;
    esac
    
    echo -e "${status_icon} ${test_name}: ${status}" | tee -a "$TEST_LOG_FILE"
    if [[ -n "$details" ]]; then
        echo -e "   ${details}" | tee -a "$TEST_LOG_FILE"
    fi
    
    TEST_RESULTS+=("$test_name:$status:$details")
}

# Generate HMAC-SHA256 signature
generate_signature() {
    local payload="$1"
    local secret="$2"
    
    echo -n "$payload" | openssl dgst -sha256 -hmac "$secret" | sed 's/^.* //'
}

# Generate GitHub webhook payload
generate_payload() {
    local repo_url="$1"
    local repo_name="$2"
    local branch="${3:-main}"
    local commit_hash="$4"
    local commit_message="${5:-"Test commit"}"
    
    cat <<EOF
{
  "ref": "refs/heads/${branch}",
  "repository": {
    "name": "${repo_name}",
    "clone_url": "${repo_url}"
  },
  "head_commit": {
    "id": "${commit_hash}",
    "message": "${commit_message}"
  }
}
EOF
}

# Send webhook request
send_webhook() {
    local payload="$1"
    local secret="$2"
    local expected_status="${3:-200}"
    
    # Generate signature
    local signature=$(generate_signature "$payload" "$secret")
    
    # Send request and capture response
    local response_file=$(mktemp)
    local http_code=$(curl -s -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -H "X-Hub-Signature-256: sha256=${signature}" \
        -d "$payload" \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    local response_body=$(cat "$response_file")
    rm -f "$response_file"
    
    # Check if status code matches expected
    if [[ "$http_code" == "$expected_status" ]]; then
        return 0
    else
        echo "HTTP $http_code: $response_body" >&2
        return 1
    fi
}

# Test 1: Happy Path - Target App Deployment
test_target_app_deployment() {
    log "Testing target app deployment (happy path)..."
    
    # Convert file:// URL to path for mock git server
    local target_repo_path=$(echo "$TARGET_REPO_URL" | sed 's|^file://||')
    
    # Get repository state
    local repo_state=$("$SCRIPT_DIR/mock_git_server.sh" get-state "$target_repo_path" "test_target_app" "main" | tail -n +1)
    local commit_hash=$(echo "$repo_state" | grep "commit_hash:" | cut -d: -f2)
    local repo_name=$(echo "$repo_state" | grep "repo_name:" | cut -d: -f2)
    
    # Make a change to the repository
    "$SCRIPT_DIR/mock_git_server.sh" push-target "Test deployment commit" "main" >/dev/null
    
    # Get updated state
    repo_state=$("$SCRIPT_DIR/mock_git_server.sh" get-state "$target_repo_path" "test_target_app" "main" | tail -n +1)
    commit_hash=$(echo "$repo_state" | grep "commit_hash:" | cut -d: -f2)
    
    # Generate payload
    local payload=$(generate_payload "$TARGET_REPO_URL" "$repo_name" "main" "$commit_hash" "Test deployment commit")
    
    # Send webhook
    if send_webhook "$payload" "$WEBHOOK_SECRET" "200"; then
        test_result "Target App Deployment" "PASS" "Successfully triggered deployment"
    else
        test_result "Target App Deployment" "FAIL" "Failed to trigger deployment"
    fi
}

# Test 2: Happy Path - Self Update Deployment
test_self_update_deployment() {
    log "Testing self-update deployment (happy path)..."
    
    # Convert file:// URL to path for mock git server
    local self_update_repo_path=$(echo "$SELF_UPDATE_REPO_URL" | sed 's|^file://||')
    
    # Get repository state
    local repo_state=$("$SCRIPT_DIR/mock_git_server.sh" get-state "$self_update_repo_path" "test_binarydeploy_updater" "main" | tail -n +1)
    local commit_hash=$(echo "$repo_state" | grep "commit_hash:" | cut -d: -f2)
    local repo_name=$(echo "$repo_state" | grep "repo_name:" | cut -d: -f2)
    
    # Make a change to the repository
    "$SCRIPT_DIR/mock_git_server.sh" push-binarydeploy "Test self-update commit" "main" >/dev/null
    
    # Get updated state
    repo_state=$("$SCRIPT_DIR/mock_git_server.sh" get-state "$self_update_repo_path" "test_binarydeploy_updater" "main" | tail -n +1)
    commit_hash=$(echo "$repo_state" | grep "commit_hash:" | cut -d: -f2)
    
    # Generate payload
    local payload=$(generate_payload "$SELF_UPDATE_REPO_URL" "$repo_name" "main" "$commit_hash" "Test self-update commit")
    
    # Send webhook
    if send_webhook "$payload" "$WEBHOOK_SECRET" "200"; then
        test_result "Self Update Deployment" "PASS" "Successfully triggered self-update"
    else
        test_result "Self Update Deployment" "FAIL" "Failed to trigger self-update"
    fi
}

# Test 3: Invalid Signature
test_invalid_signature() {
    log "Testing invalid signature rejection..."
    
    # Generate payload
    local payload=$(generate_payload "$TARGET_REPO_URL" "test_target_app" "main" "abc123" "Test invalid signature")
    
    # Send with wrong secret
    if send_webhook "$payload" "wrong-secret" "401"; then
        test_result "Invalid Signature" "PASS" "Correctly rejected invalid signature"
    else
        test_result "Invalid Signature" "FAIL" "Did not reject invalid signature as expected"
    fi
}

# Test 4: Missing Signature
test_missing_signature() {
    log "Testing missing signature rejection..."
    
    local payload=$(generate_payload "$TARGET_REPO_URL" "test_target_app" "main" "abc123" "Test missing signature")
    
    # Send request without signature
    local response_file=$(mktemp)
    local http_code=$(curl -s -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -d "$payload" \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    rm -f "$response_file"
    
    if [[ "$http_code" == "401" ]]; then
        test_result "Missing Signature" "PASS" "Correctly rejected missing signature"
    else
        test_result "Missing Signature" "FAIL" "Expected 401, got $http_code"
    fi
}

# Test 5: Branch Filter - Unauthorized Branch
test_unauthorized_branch() {
    log "Testing branch filter (unauthorized branch)..."
    
    # Create payload for unauthorized branch
    local payload=$(generate_payload "$TARGET_REPO_URL" "test_target_app" "unauthorized-branch" "abc123" "Test unauthorized branch")
    
    # Send webhook
    local response_file=$(mktemp)
    local signature=$(generate_signature "$payload" "$WEBHOOK_SECRET")
    local http_code=$(curl -s -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -H "X-Hub-Signature-256: sha256=${signature}" \
        -d "$payload" \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    local response_body=$(cat "$response_file")
    rm -f "$response_file"
    
    # Should return 200 but with message about branch not configured
    if [[ "$http_code" == "200" ]] && echo "$response_body" | grep -q "not configured for auto-deployment"; then
        test_result "Branch Filter (Unauthorized)" "PASS" "Correctly rejected unauthorized branch"
    else
        test_result "Branch Filter (Unauthorized)" "FAIL" "Expected 200 with rejection message, got $http_code: $response_body"
    fi
}

# Test 6: Branch Filter - Authorized Branch
test_authorized_branch() {
    log "Testing branch filter (authorized branch)..."
    
    # Convert file:// URL to path for mock git server
    local target_repo_path=$(echo "$TARGET_REPO_URL" | sed 's|^file://||')
    
    # Test branch should already exist from setup, skip creation if it exists
    if ! cd "$target_repo_path" && git rev-parse --verify "test-branch" >/dev/null 2>&1; then
        "$SCRIPT_DIR/mock_git_server.sh" create-branch "$target_repo_path" "test-branch" >/dev/null
    fi
    
    # Get repository state for test-branch
    local repo_state=$("$SCRIPT_DIR/mock_git_server.sh" get-state "$target_repo_path" "test_target_app" "test-branch" | tail -n +1)
    local commit_hash=$(echo "$repo_state" | grep "commit_hash:" | cut -d: -f2)
    
    # Generate payload for authorized branch
    local payload=$(generate_payload "$TARGET_REPO_URL" "test_target_app" "test-branch" "$commit_hash" "Test authorized branch")
    
    # Send webhook
    if send_webhook "$payload" "$WEBHOOK_SECRET" "200"; then
        test_result "Branch Filter (Authorized)" "PASS" "Successfully processed authorized branch"
    else
        test_result "Branch Filter (Authorized)" "FAIL" "Failed to process authorized branch"
    fi
}

# Test 7: Unknown Repository
test_unknown_repository() {
    log "Testing unknown repository handling..."
    
    # Generate payload for unknown repository
    local payload=$(generate_payload "file:///unknown/repo" "unknown_repo" "main" "abc123" "Test unknown repository")
    
    # Send webhook
    local response_file=$(mktemp)
    local signature=$(generate_signature "$payload" "$WEBHOOK_SECRET")
    local http_code=$(curl -s -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -H "X-Hub-Signature-256: sha256=${signature}" \
        -d "$payload" \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    local response_body=$(cat "$response_file")
    rm -f "$response_file"
    
    # Should return 200 but with message about repository not configured
    if [[ "$http_code" == "200" ]] && echo "$response_body" | grep -q "Repository not configured for deployment"; then
        test_result "Unknown Repository" "PASS" "Correctly handled unknown repository"
    else
        test_result "Unknown Repository" "FAIL" "Expected 200 with message, got $http_code: '$response_body'"
    fi
}

# Test 8: Invalid JSON Payload
test_invalid_json() {
    log "Testing invalid JSON payload..."
    
    # Send invalid JSON
    local response_file=$(mktemp)
    local signature=$(generate_signature '{invalid json}' "$WEBHOOK_SECRET")
    local http_code=$(curl -s -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -H "X-Hub-Signature-256: sha256=${signature}" \
        -d '{invalid json}' \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    rm -f "$response_file"
    
    if [[ "$http_code" == "400" ]]; then
        test_result "Invalid JSON Payload" "PASS" "Correctly rejected invalid JSON"
    else
        test_result "Invalid JSON Payload" "FAIL" "Expected 400, got $http_code"
    fi
}

# Test 9: Wrong HTTP Method
test_wrong_http_method() {
    log "Testing wrong HTTP method..."
    
    local payload=$(generate_payload "$TARGET_REPO_URL" "test_target_app" "main" "abc123" "Test GET method")
    
    # Send GET request instead of POST
    local response_file=$(mktemp)
    local http_code=$(curl -s -w "%{http_code}" \
        -X GET \
        -H "Content-Type: application/json" \
        -d "$payload" \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    rm -f "$response_file"
    
    if [[ "$http_code" == "405" ]]; then
        test_result "Wrong HTTP Method" "PASS" "Correctly rejected wrong HTTP method"
    else
        test_result "Wrong HTTP Method" "FAIL" "Expected 405, got $http_code"
    fi
}

# Test 10: Empty Body
test_empty_body() {
    log "Testing empty request body..."
    
    # Send request with empty body
    local response_file=$(mktemp)
    local signature=$(generate_signature '' "$WEBHOOK_SECRET")
    local http_code=$(curl -s -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -H "X-Hub-Signature-256: sha256=${signature}" \
        -d '' \
        "$WEBHOOK_URL/webhook" \
        -o "$response_file" \
        2>/dev/null || echo "000")
    
    rm -f "$response_file"
    
    if [[ "$http_code" == "400" ]]; then
        test_result "Empty Body" "PASS" "Correctly rejected empty body"
    else
        test_result "Empty Body" "FAIL" "Expected 400, got $http_code"
    fi
}

# Check prerequisites
check_prerequisites() {
    log "Checking test prerequisites..."
    
    # Check if webhook server is running
    if ! nc -z localhost "$(jq -r '.port' "$TEST_CONFIG")" 2>/dev/null; then
        error "Webhook server is not running"
        error "Please run: $SCRIPT_DIR/setup_test_env.sh"
        exit 1
    fi
    
    # Check if test repositories exist
    if [[ ! -d "$REPOS_DIR/test_target_app" ]] || [[ ! -d "$REPOS_DIR/test_binarydeploy_updater" ]]; then
        error "Test repositories not found"
        error "Please run: $SCRIPT_DIR/setup_test_env.sh"
        exit 1
    fi
    
    log "✓ Prerequisites check passed"
}

# Generate test report
generate_report() {
    log ""
    log "🏁 Test execution completed!"
    log ""
    
    local total_tests=${#TEST_RESULTS[@]}
    local passed=0
    local failed=0
    
    echo -e "${PURPLE}=== TEST SUMMARY ===${NC}" | tee -a "$TEST_LOG_FILE"
    echo "Total Tests: $total_tests" | tee -a "$TEST_LOG_FILE"
    echo "Log File: $TEST_LOG_FILE" | tee -a "$TEST_LOG_FILE"
    echo "" | tee -a "$TEST_LOG_FILE"
    
    for result in "${TEST_RESULTS[@]}"; do
        local test_name=$(echo "$result" | cut -d: -f1)
        local status=$(echo "$result" | cut -d: -f2)
        local details=$(echo "$result" | cut -d: -f3-)
        
        case "$status" in
            "PASS")
                echo -e "✅ ${test_name}" | tee -a "$TEST_LOG_FILE"
                ((passed++))
                ;;
            "FAIL")
                echo -e "❌ ${test_name}" | tee -a "$TEST_LOG_FILE"
                if [[ -n "$details" ]]; then
                    echo "   $details" | tee -a "$TEST_LOG_FILE"
                fi
                ((failed++))
                ;;
            "SKIP")
                echo -e "⏭️ ${test_name}" | tee -a "$TEST_LOG_FILE"
                ;;
        esac
    done
    
    echo "" | tee -a "$TEST_LOG_FILE"
    echo -e "Results: ${GREEN}$passed${NC} passed, ${RED}$failed${NC} failed" | tee -a "$TEST_LOG_FILE"
    
    if [[ $failed -eq 0 ]]; then
        echo -e "${GREEN}🎉 All tests passed!${NC}" | tee -a "$TEST_LOG_FILE"
        return 0
    else
        echo -e "${RED}💥 Some tests failed!${NC}" | tee -a "$TEST_LOG_FILE"
        return 1
    fi
}

# Main test execution
main() {
    log "Starting webhook endpoint testing..."
    log "Test log: $TEST_LOG_FILE"
    
    # Check prerequisites
    check_prerequisites
    
    # Wait a moment for server to be fully ready
    sleep 2
    
    # Run tests
    test_target_app_deployment
    test_self_update_deployment
    test_invalid_signature
    test_missing_signature
    test_unauthorized_branch
    test_authorized_branch
    test_unknown_repository
    test_invalid_json
    test_wrong_http_method
    test_empty_body
    
    # Generate report
    generate_report
}

# Cleanup on exit
cleanup_on_exit() {
    # Any necessary cleanup can be added here
    :
}

# Set trap for cleanup
trap cleanup_on_exit EXIT

# Run main function
main "$@"