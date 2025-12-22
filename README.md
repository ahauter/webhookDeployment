# BinaryDeploy - Self-Updating Git Webhook Server

A Go webhook server that automatically deploys applications and updates itself through GitHub webhooks. 
The system uses a dual-repository architecture with repository-based configuration files and includes comprehensive process management.

## Architecture Overview

BinaryDeploy consists of four core components:

1. **Webhook Server** - Listens for GitHub push events with signature verification
2. **Process Manager** - Manages application lifecycle with persistent process handling
3. **Target Deployment System** - Builds and manages your applications
4. **Self-Update System** - Automatically updates the webhook server itself

### Dual-Repository System

```
┌─────────────────┐    Webhook    ┌─────────────────┐
│   Your App      │ ─────────────► │  BinaryDeploy   │
│ Repository      │               │  Webhook Server │
│ (deploy.config) │               │                 │
└─────────────────┘               └─────┬───────────┘
                                         │
┌─────────────────┐    Webhook          │
│ BinaryDeploy    │ ────────────────►   │
│ Updater Repo    │                   │
│ (deploy.config) │                   │
└─────────────────┘                   │
                                         ▼
                             ┌─────────────────┐
                             │ Process Manager │
                             │                 │
                             └─────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21 or higher
- Git

### 1. Clone and Build

```bash
git clone https://github.com/your-username/binaryDeploy.git
cd binaryDeploy
go mod tidy
go build -o binaryDeploy .
```

### 2. Configure Webhook Server

Create `config.json`:

```json
{
  "port": "8080",
  "secret": "your-webhook-secret-here",
  "target_repo_url": "https://github.com/your-username/myapp.git",
  "self_update_repo_url": "https://github.com/your-username/binaryDeploy-updater.git",
  "deploy_dir": "./deployments",
  "self_update_dir": "./self-update",
  "allowed_branches": ["main", "master"],
  "log_file": "./binaryDeploy.log"
}
```

### 3. Run the Server

```bash
./binaryDeploy
```

#### Command Line Options
```bash
./binaryDeploy              # Start webhook server
./binaryDeploy --version    # Show version information
./binaryDeploy --help       # Show help message
```

The server will start listening on the configured port (default: 8080) for webhook events and write structured JSON logs to `binaryDeploy.log`.

## Repository Setup

### Target Application Repository

Add a `deploy.config` file to your application repository:

```
# My Application Deployment Configuration
build_command=go build -o myapp .
run_command=./myapp
working_dir=./
environment=production
port=8080
restart_delay=5
max_restarts=3
```

### Self-Update Repository

Create a separate repository (e.g., `binaryDeploy-updater`) with:

```
# BinaryDeploy Self-Update Configuration
build_command=go build -o binaryDeploy .
restart_command=systemctl restart webhook
backup_binary=/opt/binaryDeploy/binaryDeploy.backup
```

**Important**: Add `deploy.config` to `.gitignore` in both repositories to prevent secrets exposure!

## Configuration Reference

### config.json (Webhook Server)

| Field | Type | Description |
|-------|------|-------------|
| `port` | string | Server port (default: 8080) |
| `secret` | string | GitHub webhook secret for verification |
| `target_repo_url` | string | URL to your application repository |
| `self_update_repo_url` | string | URL to binaryDeploy updates repository |
| `deploy_dir` | string | Directory for target application deployments |
| `self_update_dir` | string | Directory for self-update operations |
| `allowed_branches` | array | Branches that trigger deployments |
| `log_file` | string | Path to structured JSON log file (default: "./binaryDeploy.log") |

### deploy.config (Application Repository)

| Field | Required | Description |
|-------|----------|-------------|
| `build_command` | Yes | Command to build your application |
| `run_command` | Yes | Command to run your application |
| `working_dir` | No | Working directory for commands (default: "./") |
| `environment` | No | Environment setting (e.g., "production") |
| `port` | No | Application port (default: 8080) |
| `restart_delay` | No | Delay between restart attempts in seconds |
| `max_restarts` | No | Maximum restart attempts |

### deploy.config (Self-Update Repository)

| Field | Required | Description |
|-------|----------|-------------|
| `build_command` | Yes | Command to build new binaryDeploy binary |
| `restart_command` | No | Command to restart webhook service |
| `backup_binary` | No | Path for backup binary (default: "./binaryDeploy.backup") |

## Process Management

### Expected Behavior

BinaryDeploy is designed for **process persistence** and **failure resilience**. When the webhook server fails or shuts down, your deployed applications continue running.

#### Process Lifecycle

1. **Deployment**: Webhook triggers `ProcessManager.StartProcess()` 
2. **Runtime**: Application runs independently of the webhook server
3. **Redeployment**: New webhook automatically replaces the existing process
4. **Server Failure**: Applications remain operational until next webhook

#### Failure Scenarios

- **Webhook Server Crash**: Applications continue running uninterrupted
- **Network Outage**: Deployed apps keep serving traffic  
- **Webhook Server Restart**: Existing processes are detected and managed
- **Process Replacement**: New deployments automatically stop old processes

#### Test Behavior

The test suite expects and verifies this behavior:
- E2E tests check that `target-app` processes persist after server shutdown
- Tests then clean up these "orphaned" processes to maintain test hygiene

#### Manual Process Management

If you need to manually clean up processes:

```bash
# Find target-app processes
pgrep -f target-app

# Find binaryDeploy processes  
pgrep -f binaryDeploy

# Clean up specific processes
# Careful not to kill tmux processes unintentionally
kill -9 <PID>
```

## Development & Testing

### Running Tests

BinaryDeploy includes a comprehensive test suite covering deployment flows, security scenarios, and failure handling.

```bash
# Run all tests
go test ./test/...

# Run tests with verbose output
go test -v ./test/...

# Run specific test suites
go test -v ./test/ -run TestTargetAppDeploymentFlow
go test -v ./test/ -run TestWebhookSecurity
go test -v ./test/ -run TestWebhookLogic

# Run tests with coverage
go test -cover ./test/...
```

### Test Features

- **End-to-End Deployment Tests**: Verify complete deployment workflows
- **Security Tests**: Validate webhook signature verification and payload validation
- **Failure Scenario Tests**: Test behavior under various failure conditions
- **Process Persistence Tests**: Verify application processes continue running after webhook server shutdown
- **Self-Update Tests**: Validate automatic binaryDeploy updates
- **Integration Tests**: Test component interactions in realistic environments

### Project Structure

```
binaryDeploy/
├── main.go                    # Main webhook server
├── config/
│   └── deploy_config.go      # Configuration parsing
├── processmanager/
│   ├── manager.go            # Process lifecycle management
│   └── manager_test.go       # Process manager tests
├── updater/
│   └── self_update.go        # Self-update functionality
├── test/                     # Comprehensive test suite
│   ├── e2e_*_test.go        # End-to-end tests
│   ├── integration_test.go   # Integration tests
│   ├── security_test.go      # Security validation
│   └── helpers.go           # Test utilities
├── config.json              # Server configuration
└── README.md
```

## Monitoring & Troubleshooting

### Logs

BinaryDeploy writes structured JSON logs to the configured log file. Use `jq` for easy parsing:

```bash
# View recent logs
tail -f binaryDeploy.log | jq .

# Filter by log level
grep '"level":"error"' binaryDeploy.log | jq .

# View deployment events
grep 'deployment' binaryDeploy.log | jq .
```

### Health Monitoring

The server exposes a simple health endpoint:

```bash
curl http://localhost:8080/
```
