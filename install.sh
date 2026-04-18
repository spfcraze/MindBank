#!/bin/bash
# MindBank Bootstrap Installer
# One-command install: curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/install.sh | bash
#
# What this does:
#   1. Clones MindBank to ~/mindbank
#   2. Installs Ollama + embedding model (if needed)
#   3. Runs setup wizard (Docker Postgres, build, plugin menu)
#   4. Starts MindBank API
#   5. Opens dashboard
#
# After install: http://localhost:8095

set -e

MINDBANK_DIR="${MINDBANK_DIR:-$HOME/mindbank}"
GITHUB_REPO="spfcraze/MindBank"
MINDBANK_PORT="${MINDBANK_PORT:-8095}"

echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║  MindBank — One-Command Install                   ║"
echo "╚═══════════════════════════════════════════════════╝"
echo ""

# ---- Check prerequisites ----
MISSING=""
command -v git &>/dev/null || MISSING="$MISSING git"
command -v docker &>/dev/null || MISSING="$MISSING docker"
command -v curl &>/dev/null || MISSING="$MISSING curl"

if [ -n "$MISSING" ]; then
    echo "ERROR: Missing required tools:$MISSING"
    echo ""
    echo "Install them first:"
    echo "  git:    https://git-scm.com/"
    echo "  docker: https://docs.docker.com/engine/install/"
    echo "  curl:   apt install curl / brew install curl"
    exit 1
fi

# ---- Step 1: Clone repo ----
echo "[1/5] Cloning MindBank..."
if [ -d "$MINDBANK_DIR/.git" ]; then
    echo "  Already installed at: $MINDBANK_DIR"
    echo "  Pulling latest..."
    cd "$MINDBANK_DIR"
    git pull --ff-only 2>/dev/null || echo "  (pull failed, continuing with existing)"
else
    git clone "https://github.com/${GITHUB_REPO}.git" "$MINDBANK_DIR"
    echo "  Cloned to: $MINDBANK_DIR"
fi
echo ""

# ---- Step 2: Install Ollama ----
echo "[2/5] Setting up Ollama..."
if command -v ollama &>/dev/null; then
    echo "  Ollama already installed ✓"
else
    echo "  Installing Ollama..."
    curl -fsSL https://ollama.ai/install.sh | sh
    echo "  Ollama installed ✓"
fi

# Pull embedding model
if ollama list 2>/dev/null | grep -q "nomic-embed-text"; then
    echo "  Embedding model already pulled ✓"
else
    echo "  Pulling nomic-embed-text model (one-time download, ~270MB)..."
    ollama pull nomic-embed-text
    echo "  Model ready ✓"
fi
echo ""

# ---- Step 3: Check Go ----
echo "[3/5] Checking Go..."
if command -v go &>/dev/null; then
    echo "  Go $(go version | awk '{print $3}') ✓"
else
    echo "  WARNING: Go not found."
    echo "  Install: https://go.dev/dl/"
    echo "  MindBank needs Go to build from source."
    echo ""
    read -p "  Continue without Go? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then exit 1; fi
fi
echo ""

# ---- Step 4: Run setup wizard ----
echo "[4/5] Running setup wizard..."
cd "$MINDBANK_DIR"
bash scripts/setup.sh
echo ""

# ---- Step 5: Start MindBank ----
echo "[5/5] Starting MindBank API..."
cd "$MINDBANK_DIR"

# Load env if exists
if [ -f ".env" ]; then
    source .env
fi

export MB_DB_DSN="${MB_DB_DSN:-postgres://mindbank:mindbank_secret@localhost:5434/mindbank?sslmode=disable}"
export MB_OLLAMA_URL="${MB_OLLAMA_URL:-http://localhost:11434}"
export MB_PORT="${MB_PORT:-$MINDBANK_PORT}"

# Kill existing if running
OLD_PID=$(pgrep -f "mindbank-api" 2>/dev/null || pgrep -f "./mindbank-api" 2>/dev/null || true)
if [ -n "$OLD_PID" ]; then
    echo "  Stopping old process (PID: $OLD_PID)..."
    kill "$OLD_PID" 2>/dev/null || true
    sleep 2
fi

# Check if binary exists
if [ ! -f "mindbank-api" ]; then
    echo "  ERROR: mindbank-api binary not found."
    echo "  Build it: cd ~/mindbank && make build"
    exit 1
fi

# Start
nohup ./mindbank-api >> /tmp/mindbank.log 2>&1 &
NEW_PID=$!
sleep 2

# Verify
if kill -0 "$NEW_PID" 2>/dev/null; then
    if curl -sf "http://localhost:${MB_PORT}/api/v1/health" &>/dev/null; then
        echo "  Started PID: $NEW_PID ✓"
        echo "  Health check: OK ✓"
    else
        echo "  WARNING: Process started but health check failed."
        echo "  Check logs: tail /tmp/mindbank.log"
    fi
else
    echo "  ERROR: Failed to start."
    echo "  Check logs: tail /tmp/mindbank.log"
    exit 1
fi

# ---- Done ----
echo ""
echo "╔═══════════════════════════════════════════════════╗"
echo "║  Install Complete!                                ║"
echo "╚═══════════════════════════════════════════════════╝"
echo ""
echo "  Dashboard:  http://localhost:${MB_PORT}"
echo "  Updates:    http://localhost:${MB_PORT}/updates"
echo ""
echo "  Useful commands:"
echo "    make health     — check status"
echo "    make update     — check for updates"
echo "    make stop       — stop everything"
echo "    make run        — restart"
echo ""
echo "  Logs: tail -f /tmp/mindbank.log"
echo ""
