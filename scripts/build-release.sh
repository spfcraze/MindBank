#!/bin/bash
# MindBank Release Builder
# Creates a self-contained release folder ready for manual GitHub upload
# Usage: bash scripts/build-release.sh [version]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MINDBANK_DIR="$(dirname "$SCRIPT_DIR")"
VERSION="${1:-$(git -C "$MINDBANK_DIR" describe --tags --always 2>/dev/null || echo 'dev')}"
RELEASE_DIR="$MINDBANK_DIR/release/MindBank-$VERSION"

echo "Building MindBank release v$VERSION..."
echo ""

# Clean previous release
rm -rf "$RELEASE_DIR"
mkdir -p "$RELEASE_DIR"

# ---- Copy core source ----
echo "[1/8] Copying source code..."
rsync -a \
    --exclude='.git' \
    --exclude='release' \
    --exclude='*.log' \
    --exclude='tmp' \
    --exclude='mindbank-api' \
    --exclude='mindbank-mcp' \
    --exclude='__pycache__' \
    --exclude='*.pyc' \
    "$MINDBANK_DIR/" "$RELEASE_DIR/"

# ---- Copy pre-built binaries ----
echo "[2/8] Copying pre-built binaries..."
if [ -f "$MINDBANK_DIR/mindbank-api" ]; then
    cp "$MINDBANK_DIR/mindbank-api" "$RELEASE_DIR/"
    echo "  mindbank-api ✓"
else
    echo "  WARNING: mindbank-api binary not found. Build with: go build -o mindbank-api ./cmd/mindbank"
fi

if [ -f "$MINDBANK_DIR/mindbank-mcp" ]; then
    cp "$MINDBANK_DIR/mindbank-mcp" "$RELEASE_DIR/"
    echo "  mindbank-mcp ✓"
else
    echo "  WARNING: mindbank-mcp binary not found. Build with: go build -o mindbank-mcp ./cmd/mindbank-mcp"
fi

# ---- Create .env template ----
echo "[3/8] Creating .env template..."
cat > "$RELEASE_DIR/.env" << 'EOF'
# MindBank Configuration
# This file is sourced by start scripts. Do NOT commit real passwords to git.
# For local development, the default password below works with the Docker Postgres setup.

# Database password (used by docker-compose.yml)
MB_POSTGRES_PASSWORD=mindbank

# Full database DSN (used by mindbank-api and mindbank-mcp binaries)
MB_DB_DSN=postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable

# Service ports
MB_PG_PORT=5434
MB_OLLAMA_PORT=11434
MB_PORT=8095

# MCP server port (Hermes integration)
MCP_HTTP_PORT=8096

# Logging
MB_LOG_LEVEL=info

# Embedding model
MB_EMBED_MODEL=nomic-embed-text

# Ollama URL
MB_OLLAMA_URL=http://localhost:11434
EOF

# ---- Ensure scripts are executable ----
echo "[4/8] Setting executable permissions..."
chmod +x "$RELEASE_DIR/scripts/"*.sh 2>/dev/null || true

# ---- Create docker-compose.yml ----
echo "[5/8] Creating docker-compose.yml..."
cat > "$RELEASE_DIR/docker-compose.yml" << 'EOF'
version: '3.8'
services:
  postgres:
    image: pgvector/pgvector:pg16
    container_name: mindbank-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: mindbank
      POSTGRES_USER: mindbank
      POSTGRES_PASSWORD: mindbank
    ports:
      - "5434:5432"
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

# ---- Create quick-start guide ----
echo "[6/8] Creating QUICKSTART.md..."
cat > "$RELEASE_DIR/QUICKSTART.md" << 'EOF'
# MindBank Quick Start

## Prerequisites
- Docker & Docker Compose
- curl
- Ollama (for embeddings)

## 1. Start Postgres
```bash
docker compose up -d
```

## 2. Start MindBank (API + MCP)
```bash
bash scripts/start.sh
```

This starts:
- API server on http://localhost:8095
- MCP server on http://localhost:8096/mcp

## 3. Verify
```bash
curl http://localhost:8095/api/v1/health
curl http://localhost:8096/mcp
```

## 4. Open Dashboard
http://localhost:8095

## 5. Auto-Start on Boot (optional)
```bash
bash scripts/install-service.sh
systemctl --user start mindbank.target
```

## Configuration
Edit `.env` to change passwords, ports, or model settings.

## Edge Types (23 total)
The system supports 23 typed edges for rich graph relationships:
- Causal: depends_on, learned_from, decided_by, produced, supports, contradicts
- Epistemic: tested_by, invalidated_by, derived_from, assumed
- Temporal: superseded_by, refined_by, merged_into
- Agent: created_by, reviewed_by, executed_by
- Failure: failed_due_to, precondition_for, incompatible_with
- Structural: contains, relates_to, temporal_next, mentions, participated_in
EOF

# ---- Create release notes ----
echo "[7/8] Creating RELEASE_NOTES.md..."
cat > "$RELEASE_DIR/RELEASE_NOTES.md" << EOF
# MindBank v$VERSION

## What's Fixed
- **Password/DSN mismatch**: .env now correctly sets MB_POSTGRES_PASSWORD and MB_DB_DSN
- **MCP auto-start**: scripts/start.sh now launches both API (8095) and MCP (8096) servers
- **Systemd services**: install-service.sh creates mindbank-api.service + mindbank-mcp.service
- **Setup script**: setup.sh generates correct .env with all required variables
- **23 edge types**: Full epistemic, temporal, agent, and failure edge support

## Files Added/Updated
- scripts/start.sh — Unified start script (API + MCP)
- scripts/install-service.sh — Systemd user service installer
- scripts/setup.sh — One-command setup with correct .env generation
- scripts/install-plugin.sh — Fixed default DSN
- scripts/mindbank.service — Updated template
- scripts/mindbank-mcp.service — New MCP service template
- .env — Complete configuration template
- README.md — Updated install instructions

## Quick Start
\`\`\`bash
bash scripts/setup.sh    # One-time setup
bash scripts/start.sh    # Start everything
\`\`\`
EOF

# ---- Create archive ----
echo "[8/8] Creating release archive..."
cd "$MINDBANK_DIR/release"
tar czf "MindBank-$VERSION.tar.gz" "MindBank-$VERSION"

echo ""
echo "Release built: $RELEASE_DIR"
echo "Archive:       $MINDBANK_DIR/release/MindBank-$VERSION.tar.gz"
echo ""
echo "Contents:"
ls -la "$RELEASE_DIR" | tail -n +2 | awk '{print "  " $9 " (" $5 " bytes)"}'
echo ""
echo "Upload the contents of $RELEASE_DIR to GitHub manually."
