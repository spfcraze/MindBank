<div align="center">

<img src="docs/images/header.png" alt="MindBank" width="100%" style="max-width: 1200px; border-radius: 12px; margin-bottom: 24px;">

# MindBank

**Persistent graph memory for AI agents.**

Hybrid search · Temporal versioning · Local embeddings · Per-project isolation

[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](https://go.dev)
[![Postgres](https://img.shields.io/badge/Postgres-16%2B-4169E1?logo=postgresql)](https://postgresql.org)
[![Ollama](https://img.shields.io/badge/Ollama-local-000000?logo=ollama)](https://ollama.com)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

</div>

---

## Table of Contents

- [What is MindBank?](#what-is-mindbank)
- [Why MindBank?](#why-mindbank)
- [Quick Start](#quick-start)
- [Dashboard](#dashboard)
- [How It Works](#how-it-works)
- [Observer Perspective](#observer-perspective-causal-intelligence)
- [Connect Your AI Agent](#connect-your-ai-agent)
- [API Overview](#api-overview)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Development](#development)
- [Node Types](#node-types)
- [License](#license)

---

## ⚡ One-Command Install

```bash
curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/install.sh | bash
```

**Dashboard:** http://localhost:8095

---

## What is MindBank?

MindBank is a **persistent memory layer** that lives between your AI agents and raw conversation history. Instead of losing context every time a session ends, MindBank remembers what matters — decisions, facts, problems, preferences — and surfaces them when relevant.

Think of it as a **second brain for AI**: structured, searchable, and relationship-aware.

### The Problem

Every AI conversation starts from zero. Ask Claude to refactor a service, and next session it has forgotten the design decisions. Ask it about a bug, and the context from three weeks ago is gone. You're re-explaining, re-contextualizing, re-deciding.

### The Solution

MindBank **remembers**:

- **Decisions** → "We chose PostgreSQL over MongoDB for ACID compliance"
- **Facts** → "The API base URL is https://api.internal/v2"
- **Problems** → "Rate limiter fails under >1000 req/s"
- **Preferences** → "User prefers functional React patterns"
- **Questions** → "Should we migrate to gRPC?" (unanswered, tracked)

...and connects them into a **graph** so your AI can follow relationships: "This decision depends on that problem, which relates to this project."

---

## Why MindBank?

| Feature | What you get |
|---------|-------------|
| 🔍 **Hybrid Search** | Full-text + semantic vector search combined with Reciprocal Rank Fusion. Find "auth bug" even if you wrote "authentication failure" |
| 📁 **Temporal Versioning** | Never lose history. Every update creates a new version. See what changed, when, and why |
| 🌱 **100% Local** | Embeddings via Ollama (nomic-embed-text). No API keys, no cloud, no data leaves your machine |
| 🔗 **Graph Relationships** | Nodes connected by typed edges (`contains`, `relates_to`, `depends_on`, `decided_by`, etc.) |
| 🖼️ **Per-Project Isolation** | Auto-namespace by working directory. `~/project-a` and `~/project-b` have separate memory graphs |
| ⚡ **Wake-Up Context** | Pre-computed snapshot of important memories served on session start |
| 🔮 **Observer Perspective** | Trace causal precursors, detect blind spots, measure knowledge coverage — understand *why* a decision was made |
| 🤖 **MCP Native** | Works with Claude Desktop, Claude Code CLI, and Hermes Agent out of the box |

---

## Quick Start

### Option 1: One-liner (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/spfcraze/MindBank/main/install.sh | bash
```

### Option 2: Manual

```bash
# 1. Clone
git clone https://github.com/spfcraze/MindBank.git ~/mindbank
cd ~/mindbank

# 2. Setup (Docker Postgres + build + plugin install)
make setup

# 3. Start Ollama + pull model
curl -fsSL https://ollama.ai/install.sh | sh
ollama pull nomic-embed-text

# 4. Run
make run

# 5. Verify
curl http://localhost:8095/api/v1/health
# → {"status":"ok","postgres":"connected","ollama":"connected"}
```

Open **http://localhost:8095** for the dashboard.

---

## Dashboard

The web UI gives you:

- **Dashboard** — Stats, namespace breakdown, activity feed, node creation, search
- **Questions** — Unanswered queries tracked for future reference
- **Edges** — Browse and filter graph relationships
- **Tools** — Auto-connect, clean orphans, import/export, batch operations, data quality analyzer
- **Graph 2D** — Interactive force-directed visualization
- **Brain 3D** — Immersive 3D graph exploration
- **Observer** — Causal precursor tracing, blind spot detection, knowledge coverage analysis

---

## How It Works

### Memory Model

```
Node (type: decision)
├── label: "Use PostgreSQL for primary store"
├── content: "Chose Postgres over MongoDB because..."
├── namespace: "my-project"
├── importance: 0.92
└── version: 3 (2 previous versions preserved)

    Edge (decided_by)
    └─── connects to Node "Load testing results"

    Edge (depends_on)
    └─── connects to Node "ACID requirements"
```

### Search Pipeline

```
User query
    ↓
[Hybrid Search]
  ├── FTS: tsvector ts_rank_cd (keyword matching)
  └── Vector: pgvector HNSW (semantic similarity)
    ↓
[Reciprocal Rank Fusion]
    ↓
[Graph Boost]
  └── Edges to ranked nodes get boost
    ↓
Results ranked by combined score
```

### Importance Scoring

Nodes are automatically scored by 5 factors:

| Factor | Weight | What it measures |
|--------|--------|-----------------|
| Recency | 30% | How recently accessed/updated |
| Frequency | 25% | How often referenced |
| Connectivity | 20% | Number of edges (hub nodes) |
| Explicit | 15% | User-set importance |
| Type | 10% | Decisions and problems weigh more |

---

## 🔮 Observer Perspective: Causal Intelligence

MindBank doesn't just store memories — it understands their **causal structure**. The Observer Perspective system treats every memory as an observable event and traces backward to find its **domain of dependence**: the set of precursors that had to exist for this memory to be possible.

### Why This Matters

Traditional search gives you *what* you know. The Observer Perspective tells you *why* you know it:

- **"We chose PostgreSQL"** → *Because* load testing showed MongoDB failed at 10k req/s
- **"Use connection pooling"** → *Because* we learned from an outage where connections exhausted
- **"JWT for auth"** → *Depends on* the decision to go stateless, which *depends on* scaling requirements

Without this, AI agents repeat decisions without understanding their rationale. With it, they inherit the full reasoning chain.

### Core Concepts

| Concept | Definition |
|---------|-----------|
| **Domain of Dependence** | All causal precursors of a node — everything that had to happen for this memory to exist |
| **Critical Depth** | The shallowest depth at which 90% of total influence is captured. If critical depth is 2, you only need to look 2 hops back to understand almost everything that matters |
| **Coverage** | Fraction of a node's immediate edges that have upstream precursors. 100% = fully explained; low = orphan decisions |
| **Influence Modes** | Ranked list of precursors by influence score (weighted by edge strength and decayed by depth) |
| **Blind Spots** | Automatically detected gaps: unresolved contradictions, missing supporting evidence, orphan decisions |

### How It Works

```
User asks: "Why did we choose PostgreSQL?"
    ↓
[Hybrid Search] finds node: "Use PostgreSQL for primary store"
    ↓
[Dependence Trace] backward BFS through causal edges:
  depth 1: "Load testing results" (decided_by)
  depth 1: "ACID requirements" (depends_on)
  depth 2: "Financial data integrity" (depends_on)
  depth 2: "MongoDB failure at 10k req/s" (learned_from)
    ↓
[Analysis]
  Critical Depth: 2 (90% of influence within 2 hops)
  Coverage: 100% (all immediate edges have precursors)
  Blind Spots: none
  Influence Modes:
    1. "Load testing results" (score: 0.85, depth: 1)
    2. "ACID requirements" (score: 0.72, depth: 1)
    3. "MongoDB failure at 10k req/s" (score: 0.51, depth: 2)
```

### Dependence-Aware Search & Q&A

Both search and Q&A support **opt-in dependence expansion**:

```json
// Search with causal context
{"name": "mindbank_search", "arguments": {
  "query": "database choice",
  "dependence_expansion": true
}}

// Ask with supporting evidence
{"name": "mindbank_ask", "arguments": {
  "query": "why postgres over mongodb",
  "dependence_expansion": true
}}
```

When enabled, MindBank:
1. Runs hybrid search to find the most relevant node
2. Traces backward up to 2 hops through causal edges
3. Appends the top precursors (up to `limit/4`) to the result set
4. Returns both the answer *and* its supporting evidence chain

This is **disabled by default** for backward compatibility. Agents must explicitly opt-in.

### Direct Dependence Trace

For deep causal analysis, use the dedicated `dependence` tool:

```json
{"name": "mindbank_dependence", "arguments": {
  "query": "database configuration",
  "max_depth": 3
}}
```

Returns:
- **Critical Depth**: How far back you need to go to capture 90% of influence
- **Coverage %**: How well-explained the seed node is
- **Influence Modes**: Ranked precursors with scores and depths
- **Blind Spots**: Detected gaps (unresolved contradictions, missing evidence)
- **Graph Stats**: Node and edge counts in the dependence subgraph

### Edge Types Used for Causal Tracing

The dependence system follows these edge types backward:

| Edge Type | Direction | Meaning |
|-----------|-----------|---------|
| `depends_on` | A → B | A depends on B (B is prerequisite) |
| `learned_from` | A → B | A was learned from experience B |
| `decided_by` | A → B | Decision A was informed by B |
| `produced` | A → B | A produced outcome B |
| `supports` | A → B | A supports/evidences B |
| `contradicts` | A → B | A contradicts B (triggers blind spot detection) |

---

## Connect Your AI Agent

```bash
bash scripts/install-plugin.sh
```

Detects and configures:

- **Claude Desktop** → `~/.config/claude/claude_desktop_config.json`
- **Claude Code CLI** → `~/.claude/mcp.json`
- **Hermes Agent** → `~/.hermes/hermes-agent/plugins/memory/`

Or use flags:

```bash
bash scripts/install-plugin.sh --all
bash scripts/install-plugin.sh --claude-desktop --hermes
```

### MCP Tools Available to Agents

| Tool | Description |
|------|-------------|
| `mindbank_store` | Save facts, decisions, questions, preferences |
| `mindbank_search` | Hybrid FTS + semantic search (opt-in causal precursors) |
| `mindbank_ask` | Natural language query → structured context (opt-in supporting evidence) |
| `mindbank_snapshot` | Get wake-up context on session start |
| `mindbank_neighbors` | Graph traversal (connected nodes) |
| `mindbank_dependence` | **Trace causal precursors** — understand *why* a decision exists |

**Dependence Expansion:** Both `search` and `ask` support an optional `dependence_expansion` flag. When enabled, MindBank traces backward from the top result through `depends_on`, `learned_from`, `decided_by`, `produced`, and `supports` edges to surface the supporting evidence that led to a decision or fact. This gives your AI agent the full causal chain, not just the conclusion.

---

## API Overview

### Nodes

```bash
# Create
POST /api/v1/nodes
{"label":"API Rate Limit","type":"problem","content":"...","namespace":"my-project"}

# List (with filters)
GET /api/v1/nodes?namespace=my-project&type=decision&limit=50

# Get (bumps access count)
GET /api/v1/nodes/{id}

# Update (creates temporal version)
PUT /api/v1/nodes/{id}

# History
GET /api/v1/nodes/{id}/history

# Neighbors (graph traversal)
GET /api/v1/nodes/{id}/neighbors?depth=2
```

### Search

```bash
# Full-text
GET /api/v1/search?q=authentication&limit=10

# Semantic
POST /api/v1/search/semantic
{"query":"how do we handle auth","limit":10}

# Hybrid (best of both)
POST /api/v1/search/hybrid
{"query":"rate limiter bug","limit":10}

# Dependence trace
POST /api/v1/analyze/dependence
{"query":"database choice","max_depth":3}
```

### Snapshots

```bash
# Get wake-up context
GET /api/v1/snapshot

# Rebuild
POST /api/v1/snapshot/rebuild
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                    MindBank Architecture                     │
└─────────────────────────────────────────────────────────────────────┘

  💻 Dashboard (static/)          🤖 MCP Server (stdio)
         │                              │
         └──────────────────────────────────┘
                        │
              🔗 Go HTTP API (chi router)
                        │
       ┌──────────────────────────────────────────┐
       │                                              │
   💾 PostgreSQL 16+                          🔬 Ollama
   • pgvector (HNSW)                          • nomic-embed-text:v1.5
   • tsvector (FTS)                           • 768 dims
   • Temporal tables                          • Local, offline
   • Recursive CTEs                           • 4 concurrent semaphores
   • Dependence analysis (BFS)                • Embedding client
```

### Key Design Decisions

- **Temporal versioning** — `valid_from`/`valid_to` columns, never `DELETE`. Full audit trail.
- **Dual history path** — Fast `latest` view + complete version chain.
- **Semaphore-bounded embeddings** — Max 4 concurrent Ollama requests to prevent overload.
- **Typed errors** — `BUSY` (retry), `UNAVAILABLE` (wait), `BAD_QUERY` (don't retry).
- **Rate limiting** — 100 req/min per IP with chi middleware.
- **Observer Perspective** — Recursive CTE BFS for causal precursor tracing, influence scoring with depth decay, and blind spot detection.

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MB_PORT` | `8095` | HTTP server port |
| `MB_DB_DSN` | `postgres://...` | PostgreSQL connection string |
| `MB_OLLAMA_URL` | `http://localhost:11434` | Ollama API endpoint |
| `MB_EMBED_MODEL` | `nomic-embed-text` | Embedding model |
| `MB_LOG_LEVEL` | `info` | debug / info / warn / error |
| `MB_API_KEY` | *(none)* | Require API key for all endpoints |

---

## Development

```bash
# Build
make build        # API server
make build-mcp    # MCP server

# Run
make run          # Build + start Postgres + start API
make stop         # Stop everything

# Quality
make test         # Run Go tests
make vet          # Run go vet
make health       # Quick health check

# Update
make update       # Check GitHub + auto-update
```

---

## Node Types

MindBank natively understands these memory types:

| Type | Use for | Example |
|------|---------|---------|
| `decision` | Architecture choices, tech stack | "Use Go for the API" |
| `fact` | Technical facts, config values | "Redis runs on port 6379" |
| `problem` | Known issues, bugs, limitations | "Webhook delivery is unreliable" |
| `preference` | Style guides, user preferences | "Prefer table-driven tests" |
| `advice` | Recommendations, best practices | "Use connection pooling" |
| `project` | Top-level project containers | "MindBank v2 refactor" |
| `question` | Unanswered queries (tracked) | "Should we add caching?" |
| `concept` | Domain concepts, definitions | "Idempotency" |
| `topic` | Thematic groupings | "Authentication" |
| `person` | Team members, stakeholders | "Sarah owns the frontend" |
| `event` | Milestones, incidents | "v1.0 launch" |

---

## License

MIT — free for personal and commercial use.

---

<div align="center">

Built with ❤️ for AI agents that deserve to remember.

[Report Bug](https://github.com/spfcraze/MindBank/issues) · [Request Feature](https://github.com/spfcraze/MindBank/issues) · [Releases](https://github.com/spfcraze/MindBank/releases)

</div>
