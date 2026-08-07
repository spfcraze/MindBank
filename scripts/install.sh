#!/bin/bash
# MindBank — one-command installer
#
#   curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/scripts/install.sh | bash
#
# What it does:
#   1. Checks prerequisites (git, Go, Docker)
#   2. Clones MindBank to $MIND_BANK_HOME (default ~/mindbank)
#   3. Starts the Postgres/pgvector container
#   4. Builds mindbank-api + mindbank-mcp
#   5. Runs migrations, starts both services
#   6. Prints the dashboard URL and next steps

set -e

MIND_BANK_HOME="${MIND_BANK_HOME:-$HOME/mindbank}"
GITHUB_REPO="${GITHUB_REPO:-https://github.com/spfcraze/MindBank.git}"
MB_PORT="${MB_PORT:-8095}"
MCP_PORT="${MCP_HTTP_PORT:-8096}"

say() { printf "\033[1;36m[mindbank]\033[0m %s\n" "$*"; }
die() { printf "\033[1;31m[mindbank] ERROR:\033[0m %s\n" "$*" >&2; exit 1; }

# ---- Prerequisites ----
command -v git >/dev/null 2>&1 || die "git is required (apt install git)"
command -v go   >/dev/null 2>&1 || die "Go 1.25+ is required (https://go.dev/dl)"
command -v docker >/dev/null 2>&1 || die "Docker is required for the Postgres/pgvector database"

go_ok=$(go version 2>/dev/null | grep -oE 'go1\.(2[5-9]|[3-9][0-9])' | head -1)
[ -n "$go_ok" ] || die "Go 1.25+ required (found: $(go version 2>/dev/null || echo none))"

# ---- Clone / update ----
if [ -d "$MIND_BANK_HOME/.git" ]; then
  say "Updating existing install at $MIND_BANK_HOME"
  git -C "$MIND_BANK_HOME" pull --ff-only || say "Update skipped (uncommitted changes?)"
else
  say "Cloning MindBank into $MIND_BANK_HOME"
  mkdir -p "$(dirname "$MIND_BANK_HOME")"
  git clone --depth 1 "$GITHUB_REPO" "$MIND_BANK_HOME"
fi
cd "$MIND_BANK_HOME"

# ---- Environment ----
if [ ! -f .env ] && [ -f .env.example ]; then
  cp .env.example .env
  say "Created .env from .env.example (edit for your setup)"
fi
# shellcheck disable=SC1091
[ -f .env ] && set -a && . ./.env && set +a

# ---- Database ----
if ! docker compose ps 2>/dev/null | grep -q "mindbank-postgres"; then
  say "Starting Postgres/pgvector container (port ${MB_PG_PORT:-5436})..."
  docker compose up -d --wait
  say "Postgres ready."
else
  say "Postgres already running."
fi

# ---- Build ----
say "Building mindbank-api + mindbank-mcp (this can take a minute)..."
go build -o mindbank-api ./cmd/mindbank
go build -o mindbank-mcp ./cmd/mindbank-mcp

# ---- Run ----
say "Starting API on :${MB_PORT} and MCP on :${MCP_PORT} ..."
export MB_DB_DSN="${MB_DB_DSN:-postgres://mindbank:mindbank@localhost:5436/mindbank?sslmode=disable}"
export MB_PORT MCP_HTTP_PORT=${MCP_PORT} MCP_TRANSPORT=http

nohup ./mindbank-api >> mindbank.log 2>&1 &
echo $! > .mindbank-api.pid
sleep 1
nohup ./mindbank-mcp --http >> mindbank-mcp.log 2>&1 &
echo $! > .mindbank-mcp.pid

# Wait for health
for _ in $(seq 1 30); do
  if curl -sf "http://localhost:${MB_PORT}/api/v1/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

say ""
say "========================================================"
say " MindBank is running"
say "   Dashboard: http://localhost:${MB_PORT}"
say "   MCP:       http://localhost:${MCP_PORT}/mcp"
say "   Logs:      tail -f $MIND_BANK_HOME/mindbank.log"
say ""
say " Stop:  kill \$(cat $MIND_BANK_HOME/.mindbank-api.pid) \$(cat $MIND_BANK_HOME/.mindbank-mcp.pid)"
say "========================================================"
