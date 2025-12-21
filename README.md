# BinaryDeploy - Self-Updating Git Webhook Server

A Go webhook server that automatically deploys applications and updates itself through GitHub webhooks. 
The system uses a dual-repository architecture with repository-based configuration files.

## Architecture Overview

BinaryDeploy consists of three core components:

1. **Webhook Server** - Listens for GitHub push events
2. **Target Deployment System** - Builds and manages your applications
3. **Self-Update System** - Automatically updates the webhook server itself

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
                            │ (coming soon)  │
                            └─────────────────┘
```

##  Quick Start

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
  "allowed_branches": ["main", "master"]
}
```

### 3. Run the Server

```bash
./binaryDeploy
```

The server will start listening on port 8080 for webhook events.

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

