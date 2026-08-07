#!/bin/bash
# MindBank Unified Start Script
# Starts both the API server (port 8095) and MCP server (port 8096)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MINDBANK_DIR="$(dirname "$SCRIPT_DIR")"

# Load environment variables from .env
cd "$MINDBANK_DIR"
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi

# Ensure MB_DB_DSN is set
if [ -z "$MB_DB_DSN" ]; then
    echo "ERROR: MB_DB_DSN not set. Please check your .env file."
    exit 1
fi

# Check if Postgres is running
if ! docker compose ps | grep -q "mindbank-postgres"; then
    echo "Starting Postgres..."
    docker compose up -d --wait
fi

# Check if Ollama is running
if ! curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
    echo "WARNING: Ollama not detected at localhost:11434. Embeddings will fail."
fi

# Kill existing mindbank processes
pkill -f "./mindbank-api" 2>/dev/null || true
pkill -f "./mindbank-mcp" 2>/dev/null || true
sleep 1

echo "Starting MindBank API on port ${MB_PORT:-8095}..."
export MB_DB_DSN
nohup ./mindbank-api > /tmp/mindbank-api.log 2>&1 &
API_PID=$!
echo "API PID: $API_PID"

# Wait for API to be ready
for i in {1..30}; do
    if curl -s http://localhost:${MB_PORT:-8095}/api/v1/health > /dev/null 2>&1; then
        echo "MindBank API ready."
        break
    fi
    sleep 1
done

# Start MCP server in HTTP mode
echo "Starting MindBank MCP on port ${MCP_HTTP_PORT:-8096}..."
export MCP_TRANSPORT=http
export MCP_HTTP_PORT=${MCP_HTTP_PORT:-8096}
export MB_DB_DSN
nohup ./mindbank-mcp --http > /tmp/mindbank-mcp.log 2>&1 &
MCP_PID=$!
echo "MCP PID: $MCP_PID"

# Wait for MCP to be ready
for i in {1..30}; do
    if curl -s http://localhost:${MCP_HTTP_PORT:-8096}/mcp > /dev/null 2>&1; then
        echo "MindBank MCP ready."
        break
    fi
    sleep 1
done

echo ""
echo "MindBank is running:"
echo "  API:     http://localhost:${MB_PORT:-8095}"
echo "  MCP:     http://localhost:${MCP_HTTP_PORT:-8096}/mcp"
echo "  Dashboard: http://localhost:${MB_PORT:-8095}"
echo ""
echo "Logs:"
echo "  API:  tail -f /tmp/mindbank-api.log"
echo "  MCP:  tail -f /tmp/mindbank-mcp.log"
echo ""
echo "To stop: pkill -f 'mindbank-api|mindbank-mcp'"
