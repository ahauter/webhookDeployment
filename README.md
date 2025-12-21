# BinaryDeploy - Git Webhook Server

A simple Go webhook server that listens for git push events and automatically builds and restarts applications.

## Features

- GitHub webhook support with HMAC signature verification
- Configurable deployment commands
- Branch filtering (only deploy from specific branches)
- Graceful shutdown handling
- Systemd service support
- Concurrent deployment execution

## Quick Start

1. **Install dependencies**:
   ```bash
   go mod tidy
   ```

2. **Build the binary**:
   ```bash
   go build -o binaryDeploy .
   ```

3. **Configure deployment**:
   Edit `config.json` with your repository settings:
   ```json
   {
     "port": "8080",
     "secret": "your-webhook-secret-here",
     "repo_url": "https://github.com/your-username/your-repo.git",
     "deploy_dir": "./deployments",
     "build_command": "go build -o app .",
     "restart_command": "systemctl restart your-app",
     "allowed_branches": ["main", "master"]
   }
   ```

4. **Run the server**:
   ```bash
   ./binaryDeploy
   ```

## Configuration Options

- `port`: Server port (default: 8080)
- `secret`: GitHub webhook secret for signature verification
- `repo_url`: Git repository URL to clone
- `deploy_dir`: Directory where repository will be cloned
- `build_command`: Command to build your application
- `restart_command`: Command to restart your service
- `allowed_branches`: List of branches that trigger deployment

## Setup as Systemd Service

1. **Copy files to system location**:
   ```bash
   sudo mkdir -p /opt/binaryDeploy
   sudo cp binaryDeploy config.json /opt/binaryDeploy/
   sudo chmod +x /opt/binaryDeploy/binaryDeploy
   ```

2. **Install systemd service**:
   ```bash
   sudo cp webhook.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable webhook
   sudo systemctl start webhook
   ```

3. **Check status**:
   ```bash
   sudo systemctl status webhook
   ```

## GitHub Webhook Setup

1. Go to your GitHub repository
2. Navigate to Settings > Webhooks
3. Click "Add webhook"
4. Set Payload URL: `http://your-server:8080/webhook`
5. Set Content type: `application/json`
6. Set Secret: same as in `config.json`
7. Select "Just the `push` event"
8. Click "Add webhook"

## Security Notes

- Always use a strong webhook secret
- Consider using HTTPS in production
- Run the service with minimal privileges (as configured in systemd service)
- Limit allowed branches to prevent unwanted deployments

## Troubleshooting

- Check logs: `sudo journalctl -u webhook -f`
- Verify webhook URL is accessible from GitHub
- Ensure the service has permissions to access the deploy directory
- Test build and restart commands manually first