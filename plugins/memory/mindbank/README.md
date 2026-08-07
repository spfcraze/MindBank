# MindBank Memory Provider

Graph-structured persistent memory for Hermes Agent.

## Features

- **Semantic search** — hybrid FTS + vector search (98% recall)
- **Temporal versioning** — never lose data, full version history
- **Graph traversal** — nodes connected by typed edges
- **Wake-up context** — pre-computed snapshot loaded at session start
- **Per-project namespaces** — isolate or connect projects
- **Local-first** — no API keys, no cloud

## Prerequisites

1. MindBank API running on port 8095
2. PostgreSQL with pgvector (via Docker)
3. Ollama with nomic-embed-text model

## Setup

```bash
# Install MindBank
cd mindbank
./scripts/setup.sh

# Or configure manually
hermes memory setup  # select "mindbank"
```

## Config

~/.hermes/mindbank.json:
```json
{
  "api_url": "http://localhost:8095/api/v1"
}
```

Or via environment variable:
```bash
export MINDBANK_API_URL=http://localhost:8095/api/v1
```

## Tools

| Tool | Description |
|------|-------------|
| `mindbank_store` | Store a decision, fact, or preference |
| `mindbank_search` | Hybrid FTS + semantic search |
| `mindbank_ask` | Natural language question → context |
| `mindbank_snapshot` | Wake-up context at session start |

## How It Works

1. **Session start**: System prompt gets snapshot context (top facts by importance)
2. **Each turn**: Prefetch runs hybrid search on user's query
3. **Each turn**: Sync stores conversation in MindBank
4. **Session end**: Extract key decisions/facts from conversation
5. **Memory writes**: Built-in MEMORY.md writes are mirrored to MindBank

## Architecture

```
Hermes Agent
    ↓ (MCP tools + MemoryProvider hooks)
MindBank API (:8095)
    ↓
PostgreSQL + pgvector (Docker :5434)
    ↓
Ollama nomic-embed-text (:11434)
```
