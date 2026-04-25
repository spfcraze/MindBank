#!/bin/bash
# MindBank startup script
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[mindbank]${NC} $1"; }
warn() { echo -e "${YELLOW}[mindbank]${NC} $1"; }
err()  { echo -e "${RED}[mindbank]${NC} $1"; }

# Config
MB_DSN="postgres://mindbank:mindbank_secret@localhost:5434/mindbank?sslmode=disable"
MB_PORT=8095
MB_PIDFILE="$DIR/.mindbank.pid"
MB_LOGFILE="${DIR}/mindbank.log"

# BUGFIX: Use PID file instead of pkill -f to avoid killing unrelated processes
stop_mindbank() {
    if [ -f "$MB_PIDFILE" ]; then
        OLD_PID=$(cat "$MB_PIDFILE")
        if kill -0 "$OLD_PID" 2>/dev/null; then
            log "Stopping existing MindBank (PID $OLD_PID)..."
            kill "$OLD_PID" 2>/dev/null
            # Wait for process to exit
            for i in $(seq 1 10); do
                if ! kill -0 "$OLD_PID" 2>/dev/null; then
                    break
                fi
                sleep 0.5
            done
            # Force kill if still running
            if kill -0 "$OLD_PID" 2>/dev/null; then
                warn "Force killing MindBank..."
                kill -9 "$OLD_PID" 2>/dev/null || true
            fi
        fi
        rm -f "$MB_PIDFILE"
    fi
}

# Sync dashboard files to embedded static dir before building
sync_static() {
    log "Syncing web/dashboard/ to internal/handler/static/..."
    local files=("index.html" "graph.html" "observer-tab.js")
    local copied=0
    for f in "${files[@]}"; do
        if [ -f "web/dashboard/$f" ]; then
            cp "web/dashboard/$f" "internal/handler/static/$f"
            copied=$((copied + 1))
        fi
    done
    log "Synced $copied file(s)."
}

# Check if binary exists or web/dashboard is newer, build if needed
needs_build=false
if [ ! -f ./mindbank-api ]; then
    needs_build=true
else
    for f in web/dashboard/index.html web/dashboard/graph.html web/dashboard/observer-tab.js; do
        if [ -f "$f" ] && [ "$f" -nt ./mindbank-api ]; then
            needs_build=true
            break
        fi
    done
fi

if [ "$needs_build" = true ]; then
    sync_static
    log "Building mindbank-api binary..."
    go build -o mindbank-api ./cmd/mindbank
else
    log "Binary is up to date."
fi

# Start Postgres via Docker
log "Starting Postgres (pgvector)..."
docker compose up -d 2>/dev/null

# BUGFIX: Better Postgres health check — use exit code instead of grep
log "Waiting for Postgres to be healthy..."
for i in $(seq 1 30); do
    if docker compose exec -T mindbank-postgres pg_isready -U mindbank -d mindbank >/dev/null 2>&1; then
        log "Postgres is ready."
        break
    fi
    if [ "$i" -eq 30 ]; then
        err "Postgres failed to start after 30 seconds."
        err "Check: docker compose logs mindbank-postgres"
        exit 1
    fi
    sleep 1
done

# Check Ollama (non-fatal — embeddings just won't work)
if curl -s --max-time 3 http://localhost:11434/api/tags 2>/dev/null | grep -q "models"; then
    log "Ollama is running."
else
    warn "Ollama not running. Embeddings will fail until Ollama is started."
    warn "  Install: curl -fsSL https://ollama.ai/install.sh | sh"
    warn "  Pull model: ollama pull nomic-embed-text"
fi

# BUGFIX: Stop existing mindbank by PID file, not pkill
stop_mindbank

# BUGFIX: Log to file so "tail -f /tmp/mindbank.log" actually works
log "Starting MindBank API on port $MB_PORT..."
# BUGFIX: Unset BASH_ENV to prevent RTK aliases from leaking into
# mindbank-api's subprocess calls (e.g., git, docker, find).
# This fixes the "MindBank process has BASH_ENV" test failure.
env -u BASH_ENV \
MB_DB_DSN="$MB_DSN" \
MB_OLLAMA_URL="http://localhost:11434" \
MB_PORT="$MB_PORT" \
MB_LOG_LEVEL=info \
./mindbank-api >> "$MB_LOGFILE" 2>&1 &

MB_PID=$!
echo "$MB_PID" > "$MB_PIDFILE"
sleep 2

# Check if process is still running
if ! kill -0 "$MB_PID" 2>/dev/null; then
    err "MindBank process died immediately."
    err "Check logs: tail -20 $MB_LOGFILE"
    rm -f "$MB_PIDFILE"
    exit 1
fi

# Health check
HEALTH=$(curl -s --max-time 5 http://localhost:$MB_PORT/api/v1/health 2>/dev/null)
if echo "$HEALTH" | grep -q '"status":"ok"'; then
    log "MindBank is healthy!"
    echo "$HEALTH" | sed 's/,/,\n  /g' | sed 's/{/{\n  /' | sed 's/}/\n}/'
else
    err "MindBank failed health check."
    err "Response: $HEALTH"
    err "Check logs: tail -20 $MB_LOGFILE"
    stop_mindbank
    exit 1
fi

log "PID: $MB_PID (saved to $MB_PIDFILE)"
log "Logs: tail -f $MB_LOGFILE"
log "API:  http://localhost:$MB_PORT/api/v1"
log "UI:   http://localhost:$MB_PORT/"
log ""
log "To stop: ./stop.sh"
