# MindBank v0.5.0 — AgentMemory Integration Release

## Release Date: 2026-04-26

## Overview
This release integrates 5 key features inspired by [agentmemory](https://github.com/rohitg00/agentmemory) to make MindBank a zero-effort, production-ready AI memory system. The focus is on **automatic ingestion**, **intelligent deduplication**, **privacy protection**, **LLM-powered compression**, and **full session replay**.

---

## New Features

### 1. Auto-Capture Background Service 🤖
**What it does:** Automatically mines Hermes session files into MindBank as soon as they are created.

**How it works:**
- Watches `~/.hermes/sessions/` using `fsnotify` for file system events
- Parses session markdown (title, date, messages with roles)
- Extracts knowledge: facts, decisions, problems, preferences, advice
- Creates session nodes with full message timeline
- Links extracted knowledge as child nodes via `produced` edges

**Files:**
- `internal/capture/service.go` — Background service with fsnotify watcher
- `internal/capture/parser.go` — Session markdown parser
- `scripts/auto_miner.py` — Standalone Python mining script
- `scripts/hermes-session-hook.sh` — tmux session hook

**Dashboard:** Shows "🤖 Auto ON/OFF" indicator in header

---

### 2. Deduplication Middleware 🔗
**What it does:** Prevents creating duplicate nodes by checking content hash before insertion.

**How it works:**
- Computes SHA-256 hash of normalized content (lowercase, stripped whitespace)
- Checks `node_hashes` table before INSERT
- If duplicate found: returns existing node with incremented `access_count`
- If new: creates node + stores hash for future dedup checks

**Files:**
- `internal/dedup/hasher.go` — Hash computation and duplicate detection
- `internal/dedup/hasher_test.go` — Unit tests

**Dashboard:** Shows "DEDUP" badge next to deduplicated nodes in the table

**API Response:**
```json
{
  "id": "...",
  "label": "Test Node",
  "deduplicated": true,
  "access_count": 2
}
```

---

### 3. Privacy Filter Middleware 🔒
**What it does:** Automatically strips secrets, API keys, passwords, and tokens before storage.

**How it works:**
- Regex patterns for common secrets:
  - OpenAI keys: `sk-[a-zA-Z0-9]{48}`
  - AWS keys: `AKIA[0-9A-Z]{16}`
  - GitHub tokens: `ghp_[a-zA-Z0-9]{36}`
  - GitLab tokens: `glpat-[a-zA-Z0-9-]{20}`
  - Passwords: `password[:=]\s*\S+`, `passwd[:=]\s*\S+`
  - URLs with auth: `https?://[^:]+:[^@]+@`
- Replaces matches with `***` (configurable)
- Runs before deduplication so redacted content is what gets hashed

**Files:**
- `internal/privacy/filter.go` — Filter with configurable patterns
- `internal/privacy/filter_test.go` — Unit tests

**Example:**
```
Input:  "API key is sk-abc123def456..."
Stored: "API key is ***"
```

---

### 4. LLM Compression Worker 🧠
**What it does:** Compresses raw text into structured facts using local Ollama LLM.

**How it works:**
- Polls `embedding_queue` table for nodes marked `compress=true`
- Sends content to Ollama with structured extraction prompt
- Parses JSON response into: facts[], concepts[], decisions[], problems[]
- Creates child nodes for each extracted item
- Links with `produced` edges to parent node
- Removes parent from queue when done

**Files:**
- `internal/compression/worker.go` — Background worker with Ollama integration

**Prompt:**
```
Extract structured facts from this text. Return JSON with arrays:
facts (factual statements), concepts (key ideas), 
decisions (choices made), problems (issues identified)
```

**Note:** Requires Ollama running with a model like `llama3.2` or `mistral`

---

### 5. Session Replay API + Frontend 📼
**What it does:** Returns full session timeline with messages, knowledge nodes, and metadata.

**How it works:**
- New endpoint: `GET /api/v1/sessions/{id}/replay`
- Returns:
  - Session node (title, date, metadata)
  - All messages with role, content, timestamp
  - Knowledge nodes extracted from session
  - Statistics (message count, duration, knowledge count)
- Frontend modal with:
  - Session list with search
  - Timeline view with color-coded messages
  - User (cyan), Assistant (green), System (yellow)

**Files:**
- `internal/handler/session.go` — `GetReplay` handler
- `web/dashboard/index.html` — Session replay modal UI

---

## Database Changes

### New Migration: `013_agentmemory_features.sql`

```sql
-- Deduplication hashes
CREATE TABLE node_hashes (
    hash TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Compression queue flag
ALTER TABLE embedding_queue ADD COLUMN IF NOT EXISTS compress BOOLEAN DEFAULT false;

-- Session messages for replay
CREATE TABLE session_messages (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    session_id TEXT NOT NULL REFERENCES nodes(id),
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata JSONB DEFAULT '{}'
);

-- Indexes
CREATE INDEX idx_node_hashes_node_id ON node_hashes(node_id);
CREATE INDEX idx_session_messages_session_id ON session_messages(session_id);
```

---

## Dashboard Updates

### New UI Elements
1. **Auto-Capture Indicator** — "🤖 Auto ON/OFF" in header (polls status)
2. **Session Replay Button** — Opens modal with session list and timeline
3. **Token Budget Slider** — 500-5000 tokens for hybrid search
4. **DEDUP Badge** — Green badge on deduplicated nodes in table

### Updated Sections
- **Search**: Now shows token usage summary ("12 results · 450 tokens · ✓ within budget")
- **Nodes Table**: Shows deduplication status
- **Debug Tab**: Shows embedding queue with new `compress` column

---

## API Changes

### New Endpoints
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/sessions/{id}/replay` | Full session timeline with messages |

### Updated Endpoints
| Method | Endpoint | Change |
|--------|----------|--------|
| POST | `/nodes` | Now runs privacy filter + deduplication middleware |
| POST | `/search/hybrid` | Accepts `token_budget` parameter |

### Response Changes
**POST /nodes** now returns:
```json
{
  "id": "...",
  "label": "...",
  "deduplicated": true,        // NEW
  "access_count": 2,           // NEW (incremented on dedup)
  "privacy_filtered": true     // NEW (if secrets were redacted)
}
```

---

## Configuration

### New Config Options (config.yaml)
```yaml
auto_capture:
  enabled: true
  watch_paths:
    - ~/.hermes/sessions
  poll_interval: 5s

deduplication:
  enabled: true
  window_minutes: 5

privacy:
  enabled: true
  patterns:
    - name: openai_key
      regex: sk-[a-zA-Z0-9]{48}

compression:
  enabled: true
  model: llama3.2
  batch_size: 1
  max_tokens: 4096
```

---

## Testing

### Unit Tests
- `internal/dedup/hasher_test.go` — Hash computation, duplicate detection
- `internal/privacy/filter_test.go` — Regex patterns, redaction
- `internal/capture/service_test.go` — Session parsing

### Manual Testing
1. Create node with duplicate content → should return existing node
2. Create node with API key → should store redacted version
3. Drop session file in ~/.hermes/sessions/ → should auto-mine
4. Search with token budget → should limit results
5. Click Replay button → should show session timeline

---

## Migration Guide

### For Existing Users
1. **Apply migration:**
   ```bash
   psql $DATABASE_URL -f migrations/013_agentmemory_features.sql
   ```

2. **Restart server:**
   ```bash
   tmux kill-session -t mindbank
   ./mindbank-api
   ```

3. **Enable auto-capture (optional):**
   ```bash
   # Add to ~/.bashrc
   export HERMES_SESSION_HOOK="/path/to/hermes-session-hook.sh"
   ```

### For New Users
No changes needed — all features enabled by default.

---

## Known Issues

1. **Compression worker requires Ollama** — If Ollama is unavailable, compression queue will backlog
2. **Auto-capture needs fsnotify** — May not work on all filesystems (WSL polling fallback needed)
3. **Privacy regex coverage** — Custom secret formats may not be caught (add patterns to config)

---

## Performance Impact

| Feature | CPU | Memory | Disk | Notes |
|---------|-----|--------|------|-------|
| Auto-capture | Low | ~10MB | Minimal | File watcher only |
| Deduplication | Low | ~5MB | +1 row/node | Hash table |
| Privacy filter | Low | ~1MB | No change | Regex only |
| Compression | High | ~50MB | +child nodes | Ollama calls |
| Session replay | Low | ~5MB | +messages | Query only |

---

## Credits

- **agentmemory** by [Rohit Ghumare](https://github.com/rohitg00) — Inspiration for auto-capture, deduplication, privacy filtering, and compression concepts
- **MindBank team** — Implementation and integration

---

## Files Changed

```
New:
  internal/capture/service.go
  internal/capture/parser.go
  internal/privacy/filter.go
  internal/privacy/filter_test.go
  internal/dedup/hasher.go
  internal/dedup/hasher_test.go
  internal/compression/worker.go
  internal/db/migrations/013_agentmemory_features.sql
  scripts/auto_miner.py
  scripts/hermes-session-hook.sh
  docs/plans/2026-04-26-agentmemory-implementation.md
  docs/comparisons/mindbank-vs-agentmemory.md

Modified:
  cmd/mindbank/main.go
  internal/handler/session.go
  internal/handler/static/index.html
  internal/handler/static/index-legacy.html
  web/dashboard/index.html
```

---

## Version Info

```
Version:     0.5.0
Codename:    AgentMemory Integration
Go Version:  1.22+
Database:    PostgreSQL 15+ with pgvector
Embedding:   Ollama (nomic-embed-text)
Compression: Ollama (llama3.2/mistral)
```

---

*This release is part of the MindBank roadmap to become a fully autonomous AI memory system.*
