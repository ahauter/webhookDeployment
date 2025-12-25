#!/bin/bash

# Test webhook script for binaryDeploy
# This sends a mock GitHub push webhook to trigger deployments

WEBHOOK_URL="http://localhost:8080/webhook"
SECRET="test-secret-for-chex"  # Set this if your deployment uses a webhook secret

# Mock GitHub push payload (compact format for consistent signature)
# Use self-update repo URL to test self-update webhook
PAYLOAD='{"ref":"refs/heads/main","repository":{"name":"binaryDeploy-updater","url":"https://github.com/ahauter/binaryDeploy-updater.git"},"head_commit":{"id":"abc123def456789","message":"Test commit for deployment"}}'

echo "Testing webhook trigger..."
echo "URL: $WEBHOOK_URL"
echo "Payload: $PAYLOAD"

# Calculate signature if secret is provided
if [ -n "$SECRET" ]; then
    SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')
    HEADER="X-Hub-Signature-256: sha256=$SIGNATURE"
    echo "Using signature: $HEADER"
    echo "Expected hash: $SIGNATURE"
    curl -v -X POST \
         -H "Content-Type: application/json" \
         -H "$HEADER" \
         -d "$PAYLOAD" \
         "$WEBHOOK_URL"
else
    echo "No signature required"
    curl -v -X POST \
         -H "Content-Type: application/json" \
         -d "$PAYLOAD" \
         "$WEBHOOK_URL"
fi

echo ""
echo "Webhook sent! Check the monitor page at http://localhost:8080/ to see the update status."