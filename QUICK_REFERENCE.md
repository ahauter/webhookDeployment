# Quick Reference

## File Structure
```
binaryDeploy/
├── main.go
├── deploy.config
├── README.md
├── config/
│   └── deploy_config.go
└── updater/
    └── self_update.go
```

## Essential Commands
```bash
# Build
go build -o binaryDeploy .

# Run (development)
./binaryDeploy

# Install (production)
sudo mkdir -p /opt/binaryDeploy
sudo cp binaryDeploy deploy.config /opt/binaryDeploy/
sudo systemctl enable --now webhook
```

## Configuration Template

### deploy.config (Single Configuration File)
```
# Application Configuration (required)
target_repo_url=https://github.com/user/app.git
allowed_branches=main
secret=generate-secure-random-string

# Application Deployment Settings
build_command=go build -o app .
run_command=./app
working_dir=./
environment=production
port=8080
restart_delay=5
max_restarts=3

# BinaryDeploy Configuration (optional)
# port=8080
# log_file=./binaryDeploy.log
# deploy_dir=./deployments
# self_update_dir=./self-update
# self_update_repo_url=https://github.com/ahauter/binaryDeploy-updater.git
```

## Common Workflow
1. Create deploy.config with your settings
2. Run binaryDeploy → webhook server starts
3. Push to configured repo → Automatic deployment
4. Push to binaryDeploy-updater repo → binaryDeploy updates itself
5. Automatic rollback on any failure

## Migration from config.json
Old config.json fields map to deploy.config:
- `port` → `port` (binary webhook port)
- `secret` → `secret`
- `target_repo_url` → `target_repo_url`
- `self_update_repo_url` → `self_update_repo_url`
- `deploy_dir` → `deploy_dir`
- `self_update_dir` → `self_update_dir`
- `allowed_branches` → `allowed_branches` (comma-separated instead of array)