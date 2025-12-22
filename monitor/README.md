# Monitor Module

This module provides a web-based monitoring interface for the Binary Deploy system.

## Features

- **Real-time Process Monitoring**: View the status of managed processes including PID, uptime, and restart count
- **Server Information**: Display server configuration and repository settings
- **Auto-refresh Dashboard**: HTML dashboard that updates every 5 seconds
- **JSON API**: RESTful endpoint for programmatic access to status information

## Usage

The monitor module is automatically integrated into the main binary. Access the monitoring interface at:

- **Web Dashboard**: `http://localhost:8080/monitor`
- **JSON API**: `http://localhost:8080/status`

## API Endpoints

### GET /status
Returns JSON with current system status:

```json
{
  "server": {
    "port": "8080",
    "target_repo": "https://github.com/user/app.git",
    "self_update_repo": "https://github.com/user/binaryDeploy-updater.git",
    "allowed_branches": ["main", "develop"]
  },
  "process": {
    "running": true,
    "pid": 12345,
    "uptime": "2h30m45s",
    "command": "./myapp",
    "working_dir": "/path/to/deployments/repo",
    "restart_count": 1,
    "config": {
      "build_command": "go build -o myapp .",
      "run_command": "./myapp",
      "working_dir": "./",
      "environment": "production",
      "max_restarts": 3,
      "restart_delay": 5
    }
  },
  "timestamp": "2025-12-21T10:30:00Z"
}
```

### GET /monitor
Serves the HTML monitoring dashboard with real-time updates.

## Architecture

The monitor module consists of:

- `handler.go`: HTTP handlers for serving the dashboard and JSON API
- `handler_test.go`: Unit tests for the handler functionality
- Clean separation from the main application logic

## Integration

To use the monitor in your application:

```go
import "binaryDeploy/monitor"

// Create monitor handler
serverConfig := &monitor.ServerConfig{
    Port:            "8080",
    TargetRepoURL:   "https://github.com/user/app.git",
    AllowedBranches: []string{"main"},
}

monitorHandler := monitor.NewHandler(processManager, serverConfig)

// Register routes
mux := http.NewServeMux()
monitorHandler.RegisterRoutes(mux)
```

The module is designed to be extensible - you can easily add new monitoring endpoints or expand the existing functionality.