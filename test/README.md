# Webhook Testing System

Comprehensive end-to-end testing suite for the binaryDeploy webhook endpoint.

## Quick Start

```bash
# 1. Set up the test environment
./test/setup_test_env.sh

# 2. Run all webhook tests
./test/test_webhook.sh

# 3. Clean up the test environment
./test/cleanup_test_env.sh
```

## Test Components

### Scripts

- **`setup_test_env.sh`** - Sets up test environment, repositories, and starts webhook server
- **`test_webhook.sh`** - Runs comprehensive webhook endpoint tests
- **`cleanup_test_env.sh`** - Cleans up test environment
- **`mock_git_server.sh`** - Simulates external git repositories

### Configuration

- **`test_config.json`** - Test-specific webhook server configuration
- **`test_repos/`** - Local git repositories for testing
- **`test_logs/`** - Test execution logs

## Test Scenarios

The test suite covers:

1. **Happy Paths**
   - Target app deployment (valid webhook)
   - Self-update deployment (valid webhook)

2. **Security Tests**
   - Invalid HMAC signature rejection
   - Missing signature rejection
   - Invalid JSON payload handling

3. **Logic Tests**
   - Branch filtering (authorized/unauthorized)
   - Unknown repository handling
   - Wrong HTTP method rejection
   - Empty request body handling

## Test Architecture

### Repository Structure
```
test/
├── test_config.json              # Test server config (port 8081)
├── test_repos/                   # Local git repositories
│   ├── test_target_app/          # Mock target app repo
│   └── test_binarydeploy_updater/ # Mock self-update repo
├── test_logs/                    # Test execution logs
├── setup_test_env.sh             # Environment setup
├── test_webhook.sh               # Main test script
├── cleanup_test_env.sh           # Environment cleanup
└── mock_git_server.sh            # Git server simulator
```

### How It Works

1. **Local Git Repositories**: Uses `file://` URLs to simulate external repositories
2. **Realistic Webhooks**: Generates GitHub-compatible webhook payloads
3. **Proper Security**: Computes HMAC-SHA256 signatures using the same algorithm
4. **Isolated Environment**: Uses port 8081 and separate directories
5. **Comprehensive Coverage**: Tests success, failure, and edge cases

## Usage Details

### Running Tests

```bash
# Run all tests
./test/test_webhook.sh

# Check test results
ls -la test/test_logs/
cat test/test_logs/webhook_test_*.log
```

### Manual Testing

```bash
# Set up test repos
./test/mock_git_server.sh setup

# Make commits to simulate pushes
./test/mock_git_server.sh push-target "Test commit" main
./test/mock_git_server.sh push-binarydeploy "Update" main

# Get repository state for payload generation
./test/mock_git_server.sh get-state /path/to/repo repo-name main
```

### Cleanup Options

```bash
# Normal cleanup (asks about logs)
./test/cleanup_test_env.sh

# Force cleanup (removes everything)
./test/cleanup_test_env.sh --force
```

## Dependencies

Required tools:
- `curl` - HTTP requests
- `jq` - JSON manipulation
- `openssl` - HMAC signature generation
- `git` - Repository management
- `nc` (netcat) - Port checking

## Test Output

```
✅ Target App Deployment: PASS
✅ Self Update Deployment: PASS
✅ Invalid Signature: PASS
✅ Missing Signature: PASS
✅ Branch Filter (Unauthorized): PASS
✅ Branch Filter (Authorized): PASS
✅ Unknown Repository: PASS
✅ Invalid JSON Payload: PASS
✅ Wrong HTTP Method: PASS
✅ Empty Body: PASS

Results: 10 passed, 0 failed
🎉 All tests passed!
```

## Troubleshooting

### Port Already in Use
```bash
# Check what's using port 8081
lsof -i :8081

# Stop the service
./test/cleanup_test_env.sh --force
```

### Tests Fail to Start
```bash
# Rebuild the binary
go build -o binaryDeploy .

# Re-setup environment
./test/setup_test_env.sh
```

### Repository Issues
```bash
# Clean and recreate repositories
./test/mock_git_server.sh cleanup
./test/mock_git_server.sh setup
```

## Configuration

Modify `test/test_config.json` to change:
- Server port
- Webhook secret
- Repository URLs
- Allowed branches
- Deployment directories

All test scripts read their configuration from this file.