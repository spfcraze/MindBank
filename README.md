# MindBank — Graph Memory for AI Agents

Permanent, searchable, relationship-aware memory for Claude, Hermes, and any AI agent.

## Quick Install

```bash
curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/install.sh | bash
```

Done. Dashboard: http://localhost:8095

## What It Does

- **Hybrid search**: Full-text (PostgreSQL tsvector) + semantic (pgvector) with Reciprocal Rank Fusion
- **Graph memory**: Nodes + edges with temporal versioning (never lose history)
- **Local embeddings**: nomic-embed-text via Ollama (no API keys, no cloud)
- **Per-project namespaces**: Memories auto-isolated by working directory
- **Wake-up context**: Pre-computed snapshot of important memories on session start
- **MCP server**: Works with Claude Desktop, Claude Code CLI, and Hermes Agent

## Prerequisites

- **PostgreSQL 16** with pgvector extension
- **Go 1.23+**
- **Ollama** (for local embeddings)
- **Docker** (for Postgres — recommended)

## Install

### 1. Clone and set up

```bash
git clone https://github.com/spfcraze/MindBank.git ~/mindbank
cd ~/mindbank
```

### 2. Run setup wizard

```bash
make setup
```

Or directly:

```bash
bash scripts/setup.sh
```

This will:
- Start Postgres via Docker (port 5434)
- Generate secure credentials
- Build the API server and MCP server
- Run `install-plugin.sh` to connect your AI client

### 3. Start Ollama + pull embedding model

```bash
curl -fsSL https://ollama.ai/install.sh | sh
ollama pull nomic-embed-text
```

### 4. Start MindBank

```bash
# Load environment
source .env

# Start API server
./mindbank-api
```

Or with Make:

```bash
make run
```

### 5. Verify

```bash
curl http://localhost:8095/api/v1/health
```

Expected:

```json
{"status":"ok","postgres":"connected","ollama":"connected","version":"0.1.0"}
```

### 6. Open dashboard

```
http://localhost:8095
```

## Connect Your AI Agent

The `install-plugin.sh` script detects your AI client and configures it:

```bash
bash scripts/install-plugin.sh
```

Shows:

```
Detected AI clients:

  MCP server: /home/user/mindbank/mindbank-mcp ✓

  [1] Claude Desktop   found
  [2] Claude Code CLI  found
  [3] Hermes Agent     found

Enter choices (e.g. 1 3 or 'all'):
```

Or use flags:

```bash
bash scripts/install-plugin.sh --claude-desktop
bash scripts/install-plugin.sh --claude-code
bash scripts/install-plugin.sh --hermes
bash scripts/install-plugin.sh --all
```

### Claude Desktop

Config: `~/.config/claude/claude_desktop_config.json` (Linux) or `~/Library/Application Support/Claude/` (macOS)

### Claude Code CLI

Config: `~/.claude/mcp.json`

### Hermes Agent

Plugin: `~/.hermes/hermes-agent/plugins/memory/mindbank/__init__.py`

Memories auto-isolate by working directory. Edit `~/.hermes/mindbank-namespaces.json` to customize:

```json
{"my-project": "custom-ns", "other-dir": "other-ns"}
```

## Updating

### From the dashboard

Visit `http://localhost:8095/updates` — shows current vs latest version, one-click update.

### From the command line

```bash
make update
```

Or:

```bash
bash scripts/update.sh
```

Flags:

```bash
bash scripts/update.sh --check       # check only, no changes
bash scripts/update.sh --yes         # skip confirmation
bash scripts/update.sh --force       # force same-version update
bash scripts/update.sh --no-restart  # update without restarting API
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/health` | Health check (postgres + ollama status) |
| **Nodes** | | |
| POST | `/api/v1/nodes` | Create a node |
| GET | `/api/v1/nodes` | List nodes (?workspace, ?namespace, ?type) |
| GET | `/api/v1/nodes/{id}` | Get node by ID (bumps access count) |
| PUT | `/api/v1/nodes/{id}` | Update node (creates new temporal version) |
| DELETE | `/api/v1/nodes/{id}` | Soft-delete node (valid_to set) |
| GET | `/api/v1/nodes/{id}/neighbors` | Connected nodes (?depth=N for multi-hop) |
| GET | `/api/v1/nodes/{id}/path/{target}` | Shortest path between nodes |
| GET | `/api/v1/nodes/{id}/history` | Temporal version history |
| **Edges** | | |
| POST | `/api/v1/edges` | Create an edge |
| GET | `/api/v1/edges` | List edges (?type=...) |
| DELETE | `/api/v1/edges/{id}` | Delete an edge |
| **Batch** | | |
| POST | `/api/v1/nodes/batch` | Batch create nodes (array) |
| POST | `/api/v1/edges/batch` | Batch create edges (array) |
| **Maintenance** | | |
| POST | `/api/v1/nodes/{id}/relink` | Relink edges after node update |
| POST | `/api/v1/nodes/{id}/purge` | Purge old temporal versions |
| POST | `/api/v1/connect` | Auto-connect nodes by similarity |
| POST | `/api/v1/nodes/dedup` | Deduplicate similar nodes |
| POST | `/api/v1/export` | Export graph to JSON |
| POST | `/api/v1/import` | Import graph from JSON |
| **Search** | | |
| GET | `/api/v1/search?q=...` | Full-text search (ts_rank_cd) |
| POST | `/api/v1/search/semantic` | Semantic search (pgvector) |
| POST | `/api/v1/search/hybrid` | Hybrid search (FTS + vector RRF) |
| **Embeddings** | | |
| POST | `/api/v1/embeddings/generate` | Generate embedding |
| GET | `/api/v1/embeddings/stats` | Embedding client metrics |
| **Sessions** | | |
| POST | `/api/v1/sessions` | Create session |
| GET | `/api/v1/sessions` | List sessions (?workspace, ?active) |
| GET | `/api/v1/sessions/{id}` | Get session |
| POST | `/api/v1/sessions/{id}/messages` | Add messages (+ auto-extraction) |
| GET | `/api/v1/sessions/{id}/context` | Get token-limited context |
| POST | `/api/v1/sessions/{id}/close` | Close session |
| **Ask + Snapshot** | | |
| POST | `/api/v1/ask` | Natural language query → structured context |
| GET | `/api/v1/snapshot` | Pre-computed wake-up context |
| POST | `/api/v1/snapshot/rebuild` | Regenerate snapshot |
| **Updates** | | |
| GET | `/api/v1/updates/check` | Check GitHub for updates |
| POST | `/api/v1/updates/run` | Run update (background) |
| GET | `/api/v1/updates/status/{id}` | Poll update progress |

## Architecture

- **Temporal versioning**: Never delete. `valid_from`/`valid_to` + version chains (dual history path).
- **Hybrid search**: FTS (tsvector/ts_rank_cd) + semantic (pgvector HNSW) with Reciprocal Rank Fusion.
- **Local embeddings**: nomic-embed-text:v1.5 via Ollama (768 dims, ~270MB, no API keys).
- **Graph traversal**: Recursive CTEs for N-hop neighbors + BFS shortest path.
- **Per-project namespaces**: Isolated or unified graph modes.
- **Importance scoring**: 5-factor (recency 30%, frequency 25%, connectivity 20%, explicit 15%, type 10%).
- **Rule-based extraction**: Auto-extracts decisions, questions, preferences, problems, advice, URLs, IPs from messages.
- **LLM-based extraction**: Optional background LLM extraction (pluggable, OpenAI-compatible).
- **Ask API**: Natural language query → hybrid search → structured context (no LLM cost on mindbank side).
- **Snapshots**: Pre-computed wake-up context of top-N important nodes.
- **MCP server**: Stdio MCP protocol — 6 tools for any AI agent to query mindbank.
- **Typed errors**: BUSY/UNAVAILABLE/BAD_QUERY — callers can retry or bail intelligently.

## MCP Tools (for AI agents)

| Tool | Description |
|------|-------------|
| `mindbank_store` | Save facts, decisions, questions, preferences |
| `mindbank_search` | Hybrid FTS + semantic search |
| `mindbank_ask` | Natural language question → context |
| `mindbank_snapshot` | Get pre-computed wake-up context |
| `mindbank_neighbors` | Get connected nodes (graph traversal) |

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `MB_PORT` | 8095 | HTTP server port |
| `MB_DB_DSN` | `postgres://mindbank:***@localhost:5432/mindbank?sslmode=disable` | PostgreSQL DSN |
| `MB_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `MB_EMBED_MODEL` | `nomic-embed-text` | Embedding model name |
| `MB_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |

## Make Targets

| Target | Description |
|--------|-------------|
| `make setup` | Run setup wizard (Docker + install) |
| `make build` | Build API server |
| `make build-mcp` | Build MCP server |
| `make run` | Build + start Postgres + start API |
| `make stop` | Stop API + Postgres |
| `make update` | Check for updates and apply |
| `make version` | Show current version |
| `make health` | Quick health check |
| `make test` | Run tests |
| `make vet` | Run go vet |
| `make clean` | Remove binaries + Docker volumes |

## License

MIT
