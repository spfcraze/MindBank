#!/bin/bash
# MindBank Complete Setup — One-command install for new users
# Usage: curl -sSL https://raw.githubusercontent.com/.../setup.sh | bash
# Or:    bash setup.sh
#
# Sets up: Postgres (Docker), API server, and calls install-plugin.sh
# for Claude Desktop, Claude Code CLI, and/or Hermes Agent integration.

set -e

MINDBANK_DIR="${MINDBANK_DIR:-$HOME/mindbank}"
MINDBANK_PORT="${MINDBANK_PORT:-8095}"
MINDBANK_PG_PORT="${MINDBANK_PG_PORT:-5434}"

echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║  MindBank — Graph Memory for AI Agents            ║"
echo "║  Complete Setup                                   ║"
echo "╚═══════════════════════════════════════════════════╝"
echo ""

# ---- Step 1: Check prerequisites ----
echo "[1/7] Checking prerequisites..."
MISSING=""
command -v docker &>/dev/null || MISSING="$MISSING docker"
command -v curl &>/dev/null || MISSING="$MISSING curl"

if [ -n "$MISSING" ]; then
    echo "  ERROR: Missing:$MISSING"
    echo "  Install Docker: https://docs.docker.com/engine/install/"
    exit 1
fi

# Detect available AI clients
HAS_CLAUDE=false
HAS_HERMES=false
HAS_GO=false
command -v claude &>/dev/null && HAS_CLAUDE=true
command -v hermes &>/dev/null && HAS_HERMES=true
command -v go &>/dev/null && HAS_GO=true

echo "  Prerequisites OK"
echo "  Detected: docker ✓ curl ✓"
[ "$HAS_CLAUDE" = true ] && echo "  Detected: claude ✓"
[ "$HAS_HERMES" = true ] && echo "  Detected: hermes ✓"
[ "$HAS_GO" = true ] && echo "  Detected: go ✓"

if [ "$HAS_CLAUDE" = false ] && [ "$HAS_HERMES" = false ]; then
    echo ""
    echo "  WARNING: No AI client detected (claude/hermes)."
    echo "  MindBank will be set up but not integrated."
    echo ""
    read -p "Continue? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then exit 1; fi
fi

# ---- Step 2: Create directory structure ----
echo "[2/7] Creating directory structure..."
mkdir -p "$MINDBANK_DIR"/{migrations,scripts,internal,cmd,plugins/memory/mindbank}
echo "  OK"

# ---- Step 3: Generate secure credentials ----
echo "[3/7] Generating credentials..."
DB_PASS=$(openssl rand -hex 16 2>/dev/null || head -c 32 /dev/urandom | base64 | head -c 32)
API_KEY=$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | base64 | head -c 64)
echo "  Generated DB password and API key"

# ---- Step 4: Create docker-compose.yml ----
echo "[4/7] Creating Docker Compose config..."
cat > "$MINDBANK_DIR/docker-compose.yml" << EOF
version: '3.8'
services:
  postgres:
    image: pgvector/pgvector:pg16
    container_name: mindbank-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: mindbank
      POSTGRES_USER: mindbank
      POSTGRES_PASSWORD: ${DB_PASS}
    ports:
      - "${MINDBANK_PG_PORT}:5432"
    volumes:
      - mindbank-pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U mindbank"]
      interval: 5s
      timeout: 5s
      retries: 5
volumes:
  mindbank-pgdata:
EOF
echo "  OK"

# ---- Step 5: Start Postgres ----
echo "[5/7] Starting Postgres..."
cd "$MINDBANK_DIR"
docker compose up -d postgres
echo "  Waiting for Postgres to be healthy..."
for i in $(seq 1 30); do
    if docker exec mindbank-postgres pg_isready -U mindbank &>/dev/null; then
        echo "  Postgres ready"
        break
    fi
    sleep 1
done

# ---- Step 6: Create config ----
echo "[6/7] Creating configuration..."
cat > "$MINDBANK_DIR/.env" << EOF
# MindBank Configuration
MB_PORT=${MINDBANK_PORT}
MB_DB_DSN=postgres://mindbank:${DB_PASS}@localhost:${MINDBANK_PG_PORT}/mindbank?sslmode=disable
MB_OLLAMA_URL=http://localhost:11434
MB_EMBED_MODEL=nomic-embed-text
MB_LOG_LEVEL=info
MB_API_KEY=${API_KEY}
EOF
echo "  OK"

# ---- Step 7: Build MCP server + install integrations ----
echo "[7/7] Installing AI agent integrations..."

# Build MCP binary if Go is available
if [ "$HAS_GO" = true ] && [ -f "$MINDBANK_DIR/cmd/mindbank-mcp/main.go" ]; then
    echo "  Building MCP server binary..."
    cd "$MINDBANK_DIR"
    go build -o mindbank-mcp ./cmd/mindbank-mcp
    echo "  Built: $MINDBANK_DIR/mindbank-mcp"
fi

# Run install-plugin.sh if it exists (shows interactive menu)
if [ -f "$MINDBANK_DIR/scripts/install-plugin.sh" ]; then
    echo ""
    export MINDBANK_URL="http://localhost:${MINDBANK_PORT}/api/v1"
    export MB_DB_DSN="postgres://mindbank:${DB_PASS}@localhost:${MINDBANK_PG_PORT}/mindbank?sslmode=disable"
    export MINDBANK_MCP_BIN="$MINDBANK_DIR/mindbank-mcp"
    bash "$MINDBANK_DIR/scripts/install-plugin.sh"
elif [ -f "$MINDBANK_DIR/plugins/memory/mindbank/__init__.py" ]; then
    # Fallback: manual hermes install if install-plugin.sh missing
    echo "  install-plugin.sh not found, doing manual hermes install..."
    HERMES_HOME="${HERMES_HOME:-$HOME/.hermes}"
    if command -v hermes &>/dev/null; then
        mkdir -p "$HERMES_HOME/hermes-agent/plugins/memory/mindbank"
        cp "$MINDBANK_DIR/plugins/memory/mindbank/__init__.py" \
           "$HERMES_HOME/hermes-agent/plugins/memory/mindbank/__init__.py"
        cat > "$HERMES_HOME/mindbank.json" << EOF
{
  "api_url": "http://localhost:${MINDBANK_PORT}/api/v1",
  "namespace": ""
}
EOF
        echo "  Hermes plugin installed"
    else
        echo "  Skipped (no install script and hermes not found)"
    fi
else
    echo "  Skipped (no plugin source found)"
fi

# ---- Done ----
echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║  Setup Complete!                                  ║"
echo "╚═══════════════════════════════════════════════════╝"
echo ""
echo "  Next steps:"
echo ""
echo "  1. Start MindBank API:"
echo "     cd $MINDBANK_DIR && source .env && ./mindbank-api"
echo ""
echo "  2. Verify it's running:"
echo "     curl http://localhost:${MINDBANK_PORT}/api/v1/health"
echo ""
echo "  3. Open the dashboard:"
echo "     http://localhost:${MINDBANK_PORT}"
echo ""
echo "  4. For auto-start on boot:"
echo "     cp $MINDBANK_DIR/scripts/mindbank.service ~/.config/systemd/user/"
echo "     systemctl --user enable --now mindbank"
echo ""
echo "  API Key: ${API_KEY}"
echo "  (Set MB_API_KEY env var to require auth)"
echo ""

if [ "$HAS_CLAUDE" = true ]; then
    echo "  Claude: restart Claude Desktop or Claude Code to use MindBank tools."
    echo ""
fi
if [ "$HAS_HERMES" = true ]; then
    echo "  Hermes: cd /your/project && hermes chat"
    echo "  Memories auto-isolated by working directory."
    echo ""
fi
