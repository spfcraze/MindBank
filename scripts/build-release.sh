#!/bin/bash
# MindBank Release Builder — produces a CLEAN public release folder.
# The release is a source tarball ready for GitHub: no tests, no personal
# paths, no secrets, no internal audit docs, no build artifacts.
#
# Usage: bash scripts/build-release.sh [version]
# Output: release/MindBank-<version>/

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MINDBANK_DIR="$(dirname "$SCRIPT_DIR")"
VERSION="${1:-$(cat "$MINDBANK_DIR/VERSION" 2>/dev/null || echo 'dev')}"
RELEASE_DIR="$MINDBANK_DIR/release/MindBank-$VERSION"

echo "Building clean public release MindBank-$VERSION ..."

# ---- Clean + create ----
rm -rf "$RELEASE_DIR"
mkdir -p "$RELEASE_DIR"

# ---- Copy source with strict excludes ----
echo "[1/5] Copying source (excludes: tests, plans, secrets, artifacts)..."
rsync -a \
  --exclude='.git' --exclude='.claude' --exclude='.update_tarball_url' \
  --exclude='backups' --exclude='release' --exclude='ready' --exclude='bin' \
  --exclude='*.log' --exclude='*.log.*' \
  --exclude='tests' --exclude='*_test.go' --exclude='*.test' \
  --exclude='docs/plans' --exclude='docs/bugs' \
  --exclude='.env' --exclude='.env.dsn' --exclude='.env.*' \
  --exclude='mindbank-api' --exclude='mindbank-mcp' --exclude='mindbank-api.new' \
  --exclude='restart.sh' --exclude='start_mindbank.sh' --exclude='start_server.sh' \
  --exclude='web' \
  --exclude='internal/handler/static/*.bak' --exclude='internal/handler/static/*.backup.*' \
  --exclude='internal/handler/static/graph-legacy.html' --exclude='internal/handler/static/index-legacy.html' \
  --exclude='internal/handler/static/observer-tab-legacy.js' --exclude='internal/handler/static/graph-glyphs.js' \
  --exclude='internal/handler/static/brain3d-inline.js' --exclude='internal/handler/static/brain3d.js.bak.glyph' \
  --exclude='internal/handler/static/static' \
  --exclude='internal/handler/static/mindbank-api' \
  --exclude='scripts/__pycache__' --exclude='__pycache__' --exclude='*.pyc' \
  --exclude='cleanup-labels' --exclude='backfill-ns' --exclude='mindbank' --exclude='scheduler' \
  "$MINDBANK_DIR/" "$RELEASE_DIR/"

# ---- Scrub any residual personal paths in included files ----
echo "[2/5] Scrubbing personal paths..."
if grep -rlI '/home/rat' "$RELEASE_DIR" --include='*.go' --include='*.sh' --include='*.py' --include='*.md' 2>/dev/null | while read -r f; do
  sed -i 's#$HOME/mindbank#$HOME/mindbank#g; s#$HOME/hermes-agent#$HOME/hermes-agent#g; s#$HOME/.hermes#$HOME/.hermes#g' "$f"
done; then :; fi
LEFTOVER=$(grep -rlI '/home/rat' "$RELEASE_DIR" --include='*.go' --include='*.sh' --include='*.py' --include='*.md' 2>/dev/null || true)
if [ -n "$LEFTOVER" ]; then
  echo "WARNING: personal paths remain in:"; echo "$LEFTOVER"
fi

# ---- Write clean .env.example + docker-compose ----
echo "[3/5] Writing clean .env.example + docker-compose.yml..."
cat > "$RELEASE_DIR/.env.example" << 'EOF'
# MindBank configuration — copy to .env and adjust.
# Single-command install (scripts/install.sh) sets these for you.

MB_DB_DSN=postgres://mindbank:mindbank@localhost:5436/mindbank?sslmode=disable
MB_PG_PORT=5436
MB_PORT=8095
MB_OLLAMA_URL=http://localhost:11434
MB_EMBED_MODEL=nomic-embed-text
MB_LLM_MODEL=qwen3-coder:latest
MCP_HTTP_PORT=8096
MB_LOG_LEVEL=info
# Set to enable API auth:
# MB_API_KEY=changeme
EOF

cat > "$RELEASE_DIR/docker-compose.yml" << 'EOF'
services:
  mindbank-postgres:
    image: pgvector/pgvector:pg16
    restart: unless-stopped
    environment:
      POSTGRES_DB: mindbank
      POSTGRES_USER: mindbank
      POSTGRES_PASSWORD: ${MB_POSTGRES_PASSWORD:-mindbank}
    volumes:
      - mindbank-pgdata:/var/lib/postgresql/data
    ports:
      - "${MB_PG_PORT:-5436}:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U mindbank -d mindbank"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  mindbank-pgdata:
EOF

# ---- Permissions ----
echo "[4/5] Setting permissions..."
chmod +x "$RELEASE_DIR/scripts/"*.sh 2>/dev/null || true

# ---- Sanity checks ----
echo "[5/5] Verifying release cleanliness..."
if ls "$RELEASE_DIR"/mindbank-api "$RELEASE_DIR"/mindbank-mcp >/dev/null 2>&1; then
  echo "ERROR: build artifacts leaked into release"; exit 1
fi
if find "$RELEASE_DIR" -name '*_test.go' | grep -q .; then
  echo "ERROR: test files leaked into release"; exit 1
fi
if grep -rlI '/home/rat' "$RELEASE_DIR" --exclude='build-release.sh' 2>/dev/null | grep -q .; then
  echo "ERROR: personal paths leaked into release"; exit 1
fi
if grep -rlI -E 'ghp_[A-Za-z0-9]{36,}|sk-[A-Za-z0-9]{32,}|AKIA[0-9A-Z]{16}[A-Za-z0-9+/]{8,}' "$RELEASE_DIR" --exclude='build-release.sh' --exclude='privacy-patterns.md' 2>/dev/null | grep -q .; then
  echo "ERROR: possible secret leaked into release"; exit 1
fi

echo ""
echo "Release ready: $RELEASE_DIR"
echo "Next: cd release/MindBank-$VERSION && go build -o mindbank-api ./cmd/mindbank && bash scripts/install.sh"
