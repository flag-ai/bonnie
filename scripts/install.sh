#!/usr/bin/env bash
set -euo pipefail

# DEPRECATED: This standalone install script is superseded by the KARR-managed
# install flow. Use the "Generate Install Command" button in the KARR UI to get
# a one-command installer that automatically registers the agent.
# See: flag-commons/install/ package.

# BONNIE Install Script
# Downloads the latest release binary and sets up a systemd service.

REPO="flag-ai/bonnie"
INSTALL_DIR="/usr/local/bin"
SERVICE_USER="bonnie"
CONFIG_DIR="/etc/bonnie"

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [ "$OS" != "linux" ]; then
    echo "This install script is for Linux only."
    exit 1
fi

echo "Installing BONNIE for ${OS}/${ARCH}..."

# Get latest release tag
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST" ]; then
    echo "Failed to determine latest release."
    exit 1
fi

echo "Latest release: ${LATEST}"

# Download binary
BINARY_URL="https://github.com/${REPO}/releases/download/${LATEST}/bonnie-${OS}-${ARCH}"
echo "Downloading ${BINARY_URL}..."
curl -fsSL -o /tmp/bonnie "$BINARY_URL"
chmod +x /tmp/bonnie

# Install binary
sudo mv /tmp/bonnie "${INSTALL_DIR}/bonnie"
echo "Installed bonnie to ${INSTALL_DIR}/bonnie"

# Create service user if it doesn't exist
if ! id -u "$SERVICE_USER" &>/dev/null; then
    sudo useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
    sudo usermod -aG docker "$SERVICE_USER"
    echo "Created service user: ${SERVICE_USER}"
fi

# Create config directory
sudo mkdir -p "$CONFIG_DIR"
if [ ! -f "${CONFIG_DIR}/bonnie.env" ]; then
    cat <<'ENVEOF' | sudo tee "${CONFIG_DIR}/bonnie.env" > /dev/null
# BONNIE Configuration
BONNIE_AUTH_TOKEN=change-me
BONNIE_LISTEN_ADDR=:7777
BONNIE_POLL_INTERVAL=10
LOG_LEVEL=info
LOG_FORMAT=json
ENVEOF
    sudo chmod 600 "${CONFIG_DIR}/bonnie.env"
    sudo chown "$SERVICE_USER":"$SERVICE_USER" "${CONFIG_DIR}/bonnie.env"
    echo "Created config at ${CONFIG_DIR}/bonnie.env — edit BONNIE_AUTH_TOKEN before starting!"
fi

# Create systemd service
cat <<'SVCEOF' | sudo tee /etc/systemd/system/bonnie.service > /dev/null
[Unit]
Description=BONNIE GPU Host Agent
Documentation=https://github.com/flag-ai/bonnie
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
User=bonnie
Group=bonnie
EnvironmentFile=/etc/bonnie/bonnie.env
ExecStart=/usr/local/bin/bonnie
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/run/docker.sock

[Install]
WantedBy=multi-user.target
SVCEOF

echo "Created systemd service: bonnie.service"

# Reload and enable
sudo systemctl daemon-reload
sudo systemctl enable bonnie.service

echo ""
echo "Installation complete!"
echo ""
echo "Next steps:"
echo "  1. Edit /etc/bonnie/bonnie.env and set BONNIE_AUTH_TOKEN"
echo "  2. Start the service: sudo systemctl start bonnie"
echo "  3. Check status: sudo systemctl status bonnie"
echo "  4. View logs: journalctl -u bonnie -f"
