#!/bin/bash
# Run this ONCE on a fresh GCP e2-micro VM to set up the environment.
# Usage: ssh into VM, then: sudo bash setup-vm.sh

set -euo pipefail

echo "==> Creating service user..."
id -u guest-stay &>/dev/null || useradd --system --no-create-home --shell /usr/sbin/nologin guest-stay

echo "==> Creating app directory..."
mkdir -p /opt/guest-stay/{templates,static}
chown -R guest-stay:guest-stay /opt/guest-stay

echo "==> Installing Caddy..."
apt-get update -y
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt-get update -y
apt-get install -y caddy

echo "==> Installing Caddyfile..."
cp /opt/guest-stay/deploy/Caddyfile /etc/caddy/Caddyfile
systemctl restart caddy
systemctl enable caddy

echo "==> Installing systemd service..."
cp /opt/guest-stay/deploy/guest-stay.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable guest-stay

echo "==> Opening firewall ports (HTTP/HTTPS)..."
# GCP firewall rules are set via gcloud, but also allow via iptables if ufw is present
if command -v ufw &>/dev/null; then
    ufw allow 80/tcp
    ufw allow 443/tcp
fi

echo ""
echo "==> Setup complete!"
echo ""
echo "Next steps:"
echo "  1. Upload app files:  ./deploy.sh"
echo "  2. Create /opt/guest-stay/.env with production values"
echo "  3. Start the app:     sudo systemctl start guest-stay"
echo "  4. Check status:      sudo systemctl status guest-stay"
echo "  5. View logs:         sudo journalctl -u guest-stay -f"
