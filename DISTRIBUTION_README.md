# MindBank — AI Memory Bank for Hermes

Graph-structured persistent memory for AI agents. Never lose a decision, fact, or conversation again.

```
98% recall | <5ms latency | temporal versioning | neural graph visualization
```

## What is MindBank?

MindBank replaces the flat text block in your AI agent's memory with a searchable, graph-structured knowledge base. Every decision, fact, preference, and problem is stored as a node with typed connections — like a brain's neural network.

### Features

- **Graph-structured memory** — nodes (decisions, facts, problems) connected by typed edges
- **Semantic search** — hybrid FTS + vector search with 98% recall
- **Temporal versioning** — never lose data, full version history
- **Neural graph visualization** — 2D Canvas brain-like mindmap with physics
- **MCP integration** — works with Hermes Agent and any MCP-compatible AI
- **Per-project namespaces** — isolate or connect projects
- **Local-first** — no API keys, no cloud, everything on your machine

### How it compares

| Feature | Flat Memory | MindBank |
|---------|-------------|----------|
| Capacity | ~550 tokens | Unlimited structured nodes |
| Search | None | FTS + semantic (98% recall) |
| Temporal | None | Full version chains |
| Graph | None | 1-5 hop traversal |
| Latency | N/A | <5ms API, <15ms hybrid |
| Visualization | None | Neural graph + dashboard |

## Quick Start

### Prerequisites

- Docker + Docker Compose
- Go 1.23+ (or use pre-built binaries)
- Ollama (installed automatically by setup script)
- Hermes Agent

### One-click install

```bash
git clone https://github.com/your-org/mindbank.git
cd mindbank
./scripts/setup.sh
```

The setup script:
1. Checks prerequisites
2. Installs Ollama + embedding model (if needed)
3. Builds binaries (or uses pre-built)
4. Starts PostgreSQL via Docker
5. Creates systemd service
6. Configures Hermes MCP
7. Installs the MindBank skill

### Manual install

```bash
# 1. Start Postgres
docker compose up -d

# 2. Install Ollama + embedding model
curl -fsSL https://ollama.ai/install.sh | sh
ollama pull nomic-embed-text

# 3. Build
go build -o mindbank ./cmd/mindbank
go build -o mindbank-mcp ./cmd/mindbank-mcp

# 4. Run
MB_DB_DSN="postgres://mindbank:YOUR_DB_PASSWORD@localhost:5434/mindbank?sslmode=disable" \
  ./mindbank

# 5. Add to Hermes config (~/.hermes/config.yaml)
mcp_servers:
  mindbank:
    command: "/path/to/mindbank-mcp"
    env:
      MB_DB_DSN: "postgres://mindbank:YOUR_DB_PASSWORD@localhost:5434/mindbank?sslmode=disable"
      MB_OLLAMA_URL: "http://localhost:11434"
    timeout: 30

# 6. Restart Hermes
hermes restart
```

## Usage

### Via Hermes (automatic)

Once installed, MindBank tools are available in every Hermes session:

```
You: "What port does the Klixsor API run on?"
Hermes: *searches MindBank* "Port 8081."

You: "Remember we switched to JWT refresh tokens"
Hermes: *creates node* "Done. Stored as a decision."

You: "Show me the memory graph"
Hermes: "Open http://localhost:8095/graph-view"
```

### Via API (direct)

```bash
# Create a node
curl -X POST http://localhost:8095/api/v1/nodes \
  -H "Content-Type: application/json" \
  -d '{"label":"Use Go for backend","node_type":"decision","content":"Chose Go over Python","namespace":"myproject"}'

# Search
curl "http://localhost:8095/api/v1/search?q=Go+backend"

# Hybrid search (FTS + semantic)
curl -X POST http://localhost:8095/api/v1/search/hybrid \
  -H "Content-Type: application/json" \
  -d '{"query":"what language do we use?"}'

# Get graph data
curl http://localhost:8095/api/v1/graph

# Get wake-up snapshot
curl http://localhost:8095/api/v1/snapshot
```

### Via MCP (programmatic)

```bash
# Test MCP connection
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | \
  MB_DB_DSN="postgres://mindbank:YOUR_DB_PASSWORD@localhost:5434/mindbank?sslmode=disable" \
  ./mindbank-mcp
```

## Web UI

- **Dashboard**: http://localhost:8095 — nodes, search, create, snapshot
- **Graph 2D**: tab on dashboard — vis-network force-directed graph
- **Brain Graph**: http://localhost:8095/graph-view — Canvas 2D neural visualization with force-directed physics

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/health` | Health check |
| POST | `/api/v1/nodes` | Create node |
| GET | `/api/v1/nodes` | List nodes |
| GET | `/api/v1/nodes/:id` | Get node |
| PUT | `/api/v1/nodes/:id` | Update (creates new version) |
| DELETE | `/api/v1/nodes/:id` | Soft-delete |
| GET | `/api/v1/nodes/:id/neighbors` | Graph neighbors |
| GET | `/api/v1/nodes/:id/path/:target` | Shortest path |
| GET | `/api/v1/nodes/:id/history` | Version history |
| POST | `/api/v1/edges` | Create edge |
| DELETE | `/api/v1/edges/:id` | Delete edge |
| GET | `/api/v1/search?q=` | Full-text search |
| POST | `/api/v1/search/semantic` | Semantic search |
| POST | `/api/v1/search/hybrid` | Hybrid search |
| POST | `/api/v1/ask` | Natural language query |
| GET | `/api/v1/snapshot` | Wake-up context |
| POST | `/api/v1/snapshot/rebuild` | Rebuild snapshot |
| POST | `/api/v1/sessions` | Create session |
| POST | `/api/v1/sessions/:id/messages` | Add messages |
| GET | `/api/v1/sessions/:id/context` | Session context |

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `MB_PORT` | 8095 | API server port |
| `MB_DB_DSN` | see below | PostgreSQL connection string |
| `MB_OLLAMA_URL` | http://localhost:11434 | Ollama API URL |
| `MB_EMBED_MODEL` | nomic-embed-text | Embedding model name |
| `MB_LOG_LEVEL` | info | Log level |

## Architecture

```
┌─────────────────────────────────────────────────┐
│                  Hermes Agent                    │
│         (calls mcp_mindbank_* tools)             │
└───────────────────┬─────────────────────────────┘
                    │ MCP stdio
┌───────────────────▼─────────────────────────────┐
│              MindBank MCP Server                 │
│         (cmd/mindbank-mcp, 15MB binary)          │
└───────────────────┬─────────────────────────────┘
                    │ HTTP
┌───────────────────▼─────────────────────────────┐
│              MindBank API Server                 │
│         (cmd/mindbank, 17MB binary)              │
│  ┌─────────┐ ┌──────────┐ ┌──────────────────┐  │
│  │  REST   │ │ Web UI   │ │ Embedding Worker │  │
│  │  24 API │ │ Dashboard│ │ Background queue │  │
│  │endpoints│ │ + Graph  │ │ + Reasoner       │  │
│  └────┬────┘ └──────────┘ └────────┬─────────┘  │
└───────┼────────────────────────────┼────────────┘
        │                            │
┌───────▼────────────┐    ┌──────────▼──────────┐
│   PostgreSQL 16    │    │       Ollama         │
│   + pgvector       │    │  nomic-embed-text    │
│   (Docker)         │    │  (localhost:11434)   │
│   Port 5434        │    │  768-dim embeddings  │
└────────────────────┘    └─────────────────────┘
```

## Troubleshooting

**MCP tools not showing in Hermes:**
- Restart Hermes after adding config
- Check: `journalctl -u mindbank | tail -20`
- Verify: `curl http://localhost:8095/api/v1/health`

**Ollama connection failed:**
- Check: `curl http://localhost:11434/api/tags`
- Start: `ollama serve`
- Pull model: `ollama pull nomic-embed-text`

**PostgreSQL connection failed:**
- Check: `docker compose ps`
- Logs: `docker compose logs mindbank-postgres`

**Port conflicts:**
- Change ports in `.env` file
- MB_PORT for API, MB_PG_PORT for Postgres

## License

MIT
