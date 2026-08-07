<div align="center">

<img src="docs/images/header.png" alt="MindBank" width="100%" style="max-width: 1200px; border-radius: 12px; margin-bottom: 24px;">

# MindBank

**Persistent graph memory for AI agents — a global workspace for your fleet of sessions.**

Hybrid search · Temporal versioning · Local embeddings · Per-project isolation · MCP server · Live global-workspace (JSPACE)

[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev)
[![Postgres](https://img.shields.io/badge/Postgres-16%2B-4169E1?logo=postgresql)](https://postgresql.org)
[![Ollama](https://img.shields.io/badge/Ollama-local-000000?logo=ollama)](https://ollama.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

</div>

---

## ⚡ One-Command Install

```bash
curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/scripts/install.sh | bash
```

That single command: checks prerequisites (Git, Go 1.25+, Docker), clones MindBank, starts the **pgvector** Postgres container, builds the API + MCP servers, runs migrations, and starts everything.

**Dashboard:** http://localhost:8095 · **MCP:** http://localhost:8096/mcp

Manual setup: see [QUICKSTART.md](QUICKSTART.md). Config reference: see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/API.md](docs/API.md).

---

## What is MindBank?

MindBank is a **persistent, graph-structured memory layer** that sits between your AI agents and raw conversation history. Agents stop losing context between sessions: everything important they learn is written once, deduplicated, embedded, and linked into a growing knowledge graph — then recalled with hybrid full-text + semantic search.

It is also a **global workspace** (JSPACE) for many concurrent agent sessions: memories are broadcast into a shared, leased workspace, integrated into the graph, and surfaced live so the whole fleet shares what any one session learned.

---

## Why MindBank?

| Problem | MindBank |
|---|---|
| Agents forget everything between sessions | Durable memories written at session end (`create_node`, session mining) |
| Keyword search misses meaning | **Hybrid search**: FTS + pgvector embeddings + graph expansion |
| Memories pile up as duplicates | **Content-hash dedup** + re-observation reinforcement |
| Old memories crowd out new ones | **Temporal versioning** + **TTL forgetting** with workspace-active protection |
| Every project mixes into one blob | **Per-project namespaces** + workspaces, auto-derived from the working directory |
| Many sessions run at once, unaware of each other | **JSPACE global workspace**: live reasoning feed, leased concept heatmap, event stream, integration health |

---

## Features

### 🧠 Graph memory
- **Nodes** — typed memories (fact, decision, preference, problem, advice, person, project, concept, event, session…) with importance, epistemic labels, validation status, and confirmation counts.
- **Edges** — typed connections (supports, contradicts, depends_on, produced, relates_to…) with weights and temporal validity.
- **Temporal versioning** — every update creates a new version with a predecessor link; full history is queryable and old versions can be purged on a schedule.
- **Deduplication & reinforcement** — identical saves are merged and *reinforced* (confirmation + importance + epistemic promotion) instead of duplicated.

### 🔎 Hybrid search
- Full-text (FTS with synonyms + trigram fallback), **semantic** (pgvector HNSW, local Ollama embeddings), and **hybrid** (RRF-fused) search.
- **Graph expansion** — top results pull in connected neighbors.
- **Dependence tracing** — follow `depends_on`/`decided_by` chains to surface causal precursors.
- Workspace-active memories get a small recall boost (JSPACE feedback loop).

### 🌍 JSPACE — the global workspace tab
Inspired by global-workspace theory and the J-space research program, the dashboard **JSPACE** tab shows what the whole fleet is thinking and remembering right now:
- **LIVE reasoning feed** — real-time chain-of-thought traces from active sessions, with per-session project attribution.
- **Workspace heatmap** — the top ~25 "lit up" memories, capacity-calibrated, clustered into concept families, rendered as tuple patterns with **leases**.
- **Event stream** — every workspace transition (`created · reinforced · entered · left · consumed · expired`) is captured.
- **Specialists panel** — parallel sessions, projects, activity.
- **Ingestion funnel** — raw session activity vs what actually persisted (where the pipeline loses information).
- **Feedback loop** (toggleable) — workspace-active memories are protected from TTL expiry and boosted in recall.
- **Provenance labels** — every panel is tagged RAW (session data) vs CURATED (MindBank memory), so the two worlds are never conflated.

### 🤖 MCP server
A full Model Context Protocol server (stdio + HTTP) so agents can read and write memory as tools. 22 tools including:

| Tool | Purpose |
|---|---|
| `create_node` / `create_nodes` | Save memories (dedup-aware, batch) |
| `search` / `ask` | Hybrid recall with FTS fallback, `node_types` filter |
| `get_node` / `update_node` / `delete_node` | Read / versioned-update / soft-delete |
| `recent` / `history` | Recency recall / version history |
| `snapshot` | Pre-computed wake-up context |
| `neighbors` / `dependence` / `create_edge` | Graph traversal + causal trace |
| `refine_connectivity` / `conflicts` / `evolution` | Knowledge hygiene + epistemic tooling |
| `cluster_sessions` / `list_namespaces` / `dream_status` / `mine_sessions` | Fleet + consolidation |
| `set_context` / `get_context` | Pin workspace/namespace per client |

See [docs/MCP_HTTP_TRANSPORT.md](docs/MCP_HTTP_TRANSPORT.md) for the full protocol.

### 🧩 Hermes integration
- Hermes auto-detects the MCP server; sessions write memories as they work and recall them at session start.
- **Automatic namespace attribution** — each session's project is derived from its transcript (state.db), so memories land in the right project even with hundreds of concurrent sessions.
- Session mining extracts durable memories from transcripts (LLM extraction with graceful fallback).
- Helper scripts: `scripts/hermes-mind.sh` (launcher that sets the project namespace), `scripts/hermes-session-ns.py` (session → project), `scripts/hermes-fleet-status.py` / `hermes-live-reasoning.py` / `hermes-raw-events.py` (JSPACE live data).

### 🧹 Memory lifecycle
- **Forgetting** — TTL-based expiry with a 14-day grace for recently-accessed memories; `workspace_active` memories are exempt.
- **Privacy redaction** — secrets (API keys, tokens, credentials) are redacted from stored content by default.
- **DQA** — data-quality analytics: orphans, duplicates, connectivity, topic coverage.
- **Dream engine** — neural consolidation: reranks, bridges, and merges related memories over time.
- **Repair tools** — heal orphan edges, merge exact duplicates, connect components (dry-run first).

---

## Quick Start

### Option 1: One-liner (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/scripts/install.sh | bash
```

### Option 2: Manual

```bash
git clone https://github.com/spfcraze/MindBank.git && cd MindBank
cp .env.example .env                # edit ports/model as needed
docker compose up -d --wait         # Postgres + pgvector
go build -o mindbank-api ./cmd/mindbank
go build -o mindbank-mcp ./cmd/mindbank-mcp
bash scripts/start.sh               # starts API (:8095) + MCP (:8096)
```

Verify:

```bash
curl http://localhost:8095/api/v1/health
# {"status":"ok","postgres":"connected","ollama":"connected",...}
```

You also need **Ollama** running with an embedding model:

```bash
ollama pull nomic-embed-text        # embeddings
ollama pull qwen3-coder:latest      # optional: LLM session mining
```

---

## How It Works

### Memory model

Sessions write **nodes** into workspaces and namespaces. Every node is embedded locally (Ollama) and stored in pgvector. Repeated saves of the same content **deduplicate and reinforce** instead of duplicating. Edges connect related nodes; the graph is traversable for neighbors and causal precursors.

### Search pipeline

`query → embed (cached) → FTS + vector candidates → RRF fusion → graph expansion → workspace-active boost → rank` — with graceful FTS-only fallback when the embedder is unavailable.

### Importance scoring

A composite of intrinsic importance, access count, and confirmation count, decayed by recency — this also drives the JSPACE workspace membership (top-K leased memories).

### Global workspace (JSPACE)

The workspace is a **derived, recomputable layer** — it never writes into the memory graph. The feedback loop (hourly, toggleable) marks the top-25 memories as workspace-active with a timed lease; forgetting exempts them and search boosts them. Every transition is recorded in the workspace event stream.

---

## Connect Your AI Agent

Any MCP client can use MindBank:

```yaml
# ~/.config/hermes/config.yaml (or your MCP client config)
mcp_servers:
  mindbank:
    url: http://127.0.0.1:8096/mcp
    enabled: true
```

For Hermes, add the namespace header so each session's memories land in its project:

```yaml
mcp_servers:
  mindbank:
    url: http://127.0.0.1:8096/mcp
    headers:
      X-Mindbank-Namespace: ${MINDBANK_NAMESPACE}   # set by scripts/hermes-mind.sh
    enabled: true
```

Claude Desktop / Cursor / any stdio-capable client can also launch `./mindbank-mcp` directly.

---

## API Overview

| Endpoint | Description |
|---|---|
| `POST /api/v1/nodes` | Create a node (dedup-aware) |
| `GET /api/v1/nodes` | List nodes (filters: workspace, namespace, type, sort) |
| `GET/PUT/DELETE /api/v1/nodes/{id}` | Read / versioned-update / soft-delete |
| `GET /api/v1/nodes/{id}/history` | Version history |
| `POST /api/v1/search/hybrid` | Hybrid FTS + semantic search |
| `GET /api/v1/snapshot` | Wake-up context of the most important memories |
| `GET /api/v1/analytics/graph` | Graph metrics |
| `GET /api/v1/jspace/overview` | Global workspace aggregate |
| `GET /api/v1/jspace/live` | Live fleet reasoning + memory activity |
| `GET /api/v1/jspace/raw` | Raw session activity + ingestion funnel |
| `GET/POST /api/v1/jspace/feedback` | Toggle the workspace feedback loop |
| `POST /api/v1/analyze/repair-orphan-edges` | Repair edges pointing at dead nodes |

Full reference: [docs/API.md](docs/API.md)

---

## Configuration

Environment variables (see `.env.example`):

| Var | Default | Purpose |
|---|---|---|
| `MB_DB_DSN` | `postgres://mindbank:mindbank@localhost:5436/mindbank` | Postgres/pgvector DSN |
| `MB_PORT` | `8095` | Dashboard + API port |
| `MCP_HTTP_PORT` | `8096` | MCP HTTP port |
| `MB_OLLAMA_URL` | `http://localhost:11434` | Embedding endpoint |
| `MB_EMBED_MODEL` | `nomic-embed-text` | Embedding model |
| `MB_LLM_MODEL` | `qwen3-coder:latest` | LLM for session mining |
| `MB_API_KEY` | *(empty)* | Set to require API auth |
| `MB_LOG_LEVEL` | `info` | Log verbosity |

---

## Development

```bash
make build          # build mindbank-api
make build-mcp      # build mindbank-mcp
make test           # run unit + integration tests (needs a running DB)
go vet ./...        # static analysis
```

The dashboard is a single self-contained HTML app embedded in the binary (`internal/handler/static/`).

---

## License

MIT — see [LICENSE](LICENSE).

---
