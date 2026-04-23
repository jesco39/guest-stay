#!/bin/bash
# Deploy guest-stay to the GCP Compute Engine VM.
# Usage: ./deploy.sh <host-or-url> [ssh-user]
#
# Examples:
#   ./deploy.sh https://guest-stay.jesco39.com
#   ./deploy.sh guest-stay.jesco39.com
#   ./deploy.sh 34.56.78.90
#   ./deploy.sh 34.56.78.90 someone-else

set -euo pipefail

RAW_TARGET="${1:?Usage: ./deploy.sh <host-or-url> [ssh-user]}"
REMOTE_USER="${2:-jesco}"
APP_DIR="/opt/guest-stay"

# Accept bare hostname, full URL, or IP. Strip scheme + path if a URL was passed.
HOST=$(echo "$RAW_TARGET" | sed -E 's#^[a-z]+://##; s#/.*$##')

# known_hosts is pinned to the VM's IP, not its DNS name, so always connect by IP.
if [[ "$HOST" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    REMOTE_HOST="$HOST"
else
    REMOTE_HOST=$(dig +short "$HOST" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | tail -n1)
    if [ -z "$REMOTE_HOST" ]; then
        echo "Could not resolve $HOST to an IP" >&2
        exit 1
    fi
    echo "==> Resolved $HOST -> $REMOTE_HOST"
fi

REMOTE="$REMOTE_USER@$REMOTE_HOST"

# The gcloud-managed key isn't a default SSH identity, so plain ssh won't try it
# unless it's in the agent. Load it if present (no-op if already loaded).
GCE_KEY="$HOME/.ssh/google_compute_engine"
if [ -f "$GCE_KEY" ]; then
    ssh-add "$GCE_KEY" >/dev/null 2>&1 || true
fi

echo "==> Cross-compiling for Linux amd64..."
cd "$(dirname "$0")"
GOOS=linux GOARCH=amd64 go build -o guest-stay-linux .

echo "==> Uploading files to ${REMOTE}:${APP_DIR}..."
# Upload binary
scp guest-stay-linux "${REMOTE}:/tmp/guest-stay"
ssh "$REMOTE" "sudo mv /tmp/guest-stay ${APP_DIR}/guest-stay && sudo chmod +x ${APP_DIR}/guest-stay"
rm -f guest-stay-linux

# Upload templates and static files
scp -r templates "${REMOTE}:/tmp/guest-stay-templates"
scp -r static "${REMOTE}:/tmp/guest-stay-static"
ssh "$REMOTE" "sudo rm -rf ${APP_DIR}/templates ${APP_DIR}/static && \
    sudo mv /tmp/guest-stay-templates ${APP_DIR}/templates && \
    sudo mv /tmp/guest-stay-static ${APP_DIR}/static"

# Upload deploy config files
scp -r deploy "${REMOTE}:/tmp/guest-stay-deploy"
ssh "$REMOTE" "sudo rm -rf ${APP_DIR}/deploy && sudo mv /tmp/guest-stay-deploy ${APP_DIR}/deploy"

# Upload credentials.json if it exists locally
if [ -f credentials.json ]; then
    scp credentials.json "${REMOTE}:/tmp/guest-stay-credentials.json"
    ssh "$REMOTE" "sudo mv /tmp/guest-stay-credentials.json ${APP_DIR}/credentials.json"
fi

# Fix ownership
ssh "$REMOTE" "sudo chown -R guest-stay:guest-stay ${APP_DIR}"

# Restart the service
echo "==> Restarting guest-stay service..."
ssh "$REMOTE" "sudo systemctl restart guest-stay"

# Check status
echo "==> Service status:"
ssh "$REMOTE" "sudo systemctl status guest-stay --no-pager" || true

echo ""
echo "==> Deploy complete! https://guest-stay.jesco39.com"
