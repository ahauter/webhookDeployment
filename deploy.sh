#!/bin/bash

# Deployment script for webhook-triggered builds
# This script can be used as an alternative to inline commands in config.json

set -e  # Exit on any error

LOG_FILE="/var/log/webhook-deploy.log"
REPO_DIR="$1"
APP_NAME="your-app"

# Function to log messages
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

log "Starting deployment process..."

# Change to repository directory
cd "$REPO_DIR"

# Pull latest changes
log "Pulling latest changes..."
git pull origin main

# Build the application
log "Building application..."
go build -o "$APP_NAME" .

# Stop existing service (if running)
log "Stopping existing service..."
if systemctl is-active --quiet "$APP_NAME"; then
    systemctl stop "$APP_NAME"
fi

# Copy new binary to /usr/local/bin
log "Installing new binary..."
sudo cp "$APP_NAME" /usr/local/bin/
sudo chmod +x /usr/local/bin/"$APP_NAME"

# Start the service
log "Starting service..."
systemctl start "$APP_NAME"

log "Deployment completed successfully!"