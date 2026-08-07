# MindBank Quickstart

The fastest way to a running MindBank.

## One command

```bash
curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/scripts/install.sh | bash
```

The installer checks for **Git**, **Go 1.25+**, and **Docker**, then clones, builds, and starts everything. Open **http://localhost:8095**.

## What the installer does

1. Clones the repo to `~/mindbank`
2. Creates `.env` from `.env.example`
3. Starts Postgres + pgvector (`docker compose up -d --wait`)
4. Builds `mindbank-api` + `mindbank-mcp`
5. Starts both servers (API :8095, MCP :8096)
6. Prints the health-check URL

## After install

```bash
# Verify
curl http://localhost:8095/api/v1/health

# Check the dashboard
open http://localhost:8095          # macOS
xdg-open http://localhost:8095      # Linux

# Stop
kill $(cat ~/mindbank/.mindbank-api.pid) $(cat ~/mindbank/.mindbank-mcp.pid)

# Restart
cd ~/mindbank && bash scripts/start.sh
```

## Optional: embeddings + LLM mining

```bash
ollama pull nomic-embed-text     # required for semantic search
ollama pull qwen3-coder:latest   # optional: LLM session mining
```

## Connect an agent (MCP)

```yaml
mcp_servers:
  mindbank:
    url: http://127.0.0.1:8096/mcp
    enabled: true
```

See the [README](README.md) for the full feature walkthrough and the MCP tool list.
