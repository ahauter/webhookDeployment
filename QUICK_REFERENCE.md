# Quick Reference

## File Structure
```
binaryDeploy/
├── main.go
├── config.json
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
sudo cp binaryDeploy config.json /opt/binaryDeploy/
sudo systemctl enable --now webhook
```

## Configuration Templates

### config.json
```json
{
  "port": "8080",
  "secret": "generate-secure-random-string",
  "target_repo_url": "https://github.com/user/app.git",
  "self_update_repo_url": "https://github.com/user/binaryDeploy-updater.git",
  "deploy_dir": "./deployments",
  "self_update_dir": "./self-update",
  "allowed_branches": ["main"]
}
```

### Application Repository deploy.config
```
# App deployment
build_command=go build -o app .
run_command=./app
working_dir=./
port=8080
```

### Self-Update Repository deploy.config
```
# BinaryDeploy self-update
build_command=go build -o binaryDeploy .
restart_command=systemctl restart webhook
backup_binary=/opt/binaryDeploy/binaryDeploy.backup
```

## Common Workflow
1. Push to app repo → App rebuilds
2. Push to binaryDeploy-updater repo → binaryDeploy updates itself
3. Both use same webhook URL
4. Automatic rollback on any failure