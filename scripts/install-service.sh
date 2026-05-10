#!/bin/bash
# Install MindBank systemd user service for auto-start
# Usage: bash scripts/install-service.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MINDBANK_DIR="$(dirname "$SCRIPT_DIR")"

# Load environment from .env
cd "$MINDBANK_DIR"
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi

SERVICE_DIR="$HOME/.config/systemd/user"
mkdir -p "$SERVICE_DIR"

echo "Installing MindBank systemd user service..."

# Create the API service
cat > "$SERVICE_DIR/mindbank-api.service" << EOF
[Unit]
Description=MindBank API Server
After=docker.service network.target
Wants=docker.service

[Service]
Type=simple
WorkingDirectory=$MINDBANK_DIR
Environment="MB_DB_DSN=$MB_DB_DSN"
Environment="MB_PORT=${MB_PORT:-8095}"
Environment="MB_OLLAMA_URL=${MB_OLLAMA_URL:-http://localhost:11434}"
Environment="MB_EMBED_MODEL=${MB_EMBED_MODEL:-nomic-embed-text}"
Environment="MB_LOG_LEVEL=${MB_LOG_LEVEL:-info}"
ExecStartPre=/bin/bash -c 'until docker exec mindbank-postgres pg_isready -U mindbank; do sleep 1; done'
ExecStart=$MINDBANK_DIR/mindbank-api
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF

# Create the MCP service
cat > "$SERVICE_DIR/mindbank-mcp.service" << EOF
[Unit]
Description=MindBank MCP Server
After=mindbank-api.service
Requires=mindbank-api.service

[Service]
Type=simple
WorkingDirectory=$MINDBANK_DIR
Environment="MCP_TRANSPORT=http"
Environment="MCP_HTTP_PORT=${MCP_HTTP_PORT:-8096}"
Environment="MB_DB_DSN=$MB_DB_DSN"
ExecStart=$MINDBANK_DIR/mindbank-mcp
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF

# Create a combined target
cat > "$SERVICE_DIR/mindbank.target" << EOF
[Unit]
Description=MindBank — Graph Memory for AI Agents
After=docker.service network.target

[Install]
WantedBy=default.target
EOF

# Reload systemd
systemctl --user daemon-reload

# Enable services
systemctl --user enable mindbank-api.service
systemctl --user enable mindbank-mcp.service
systemctl --user enable mindbank.target

echo ""
echo "Systemd services installed:"
echo "  mindbank-api.service  — API server (port ${MB_PORT:-8095})"
echo "  mindbank-mcp.service  — MCP server (port ${MCP_HTTP_PORT:-8096})"
echo "  mindbank.target       — Combined target"
echo ""
echo "Commands:"
echo "  Start:   systemctl --user start mindbank.target"
echo "  Stop:    systemctl --user stop mindbank.target"
echo "  Status:  systemctl --user status mindbank-api.service mindbank-mcp.service"
echo "  Logs:    journalctl --user -u mindbank-api.service -f"
echo "           journalctl --user -u mindbank-mcp.service -f"
echo ""
echo "To start now: systemctl --user start mindbank.target"
