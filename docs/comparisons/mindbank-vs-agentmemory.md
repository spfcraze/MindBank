# MindBank vs agentmemory: Save & Recall Comparison

## Executive Summary

| Aspect | **agentmemory** | **MindBank** |
|--------|----------------|--------------|
| **Storage** | SQLite + iii-engine (local, embedded) | PostgreSQL + pgvector (external, server) |
| **Search** | BM25 + Vector + Graph (RRF fusion) | BM25 (tsvector) + Vector + Graph (RRF fusion) |
| **Auto-capture** | 12 hooks (zero manual) | Scripts (manual/automated via cron) |
| **Embeddings** | all-MiniLM-L6-v2 (local, free) | nomic-embed-text via Ollama (local, free) |
| **Memory lifecycle** | 4-tier: raw → compressed → facts → graph | Temporal versioning with valid_from/valid_to |
| **Token budget** | Yes (~1,900 tokens/session) | Yes (configurable slider) |
| **Multi-agent** | MCP server + REST API | MCP server + REST API |
| **External deps** | **0** (self-contained) | PostgreSQL + Ollama |
| **Real-time viewer** | Port 3113 | Port 8095 dashboard |

---

## 1. HOW DATA IS SAVED

### agentmemory Save Pipeline

```
Hook fires (PostToolUse, SessionStart, etc.)
  -> SHA-256 deduplication (5-minute window)
  -> Privacy filter (strips secrets, API keys)
  -> Store RAW observation (SQLite)
  -> LLM compression (structured facts + concepts + narrative)
  -> Vector embedding (all-MiniLM-L6-v2)
  -> Index in BM25 + vector + knowledge graph
```

**Storage Layers:**
1. **Raw observations** - Full text of what happened
2. **Compressed memory** - LLM-summarized facts
3. **Structured facts** - Extracted concepts, decisions, problems
4. **Knowledge graph** - Connected nodes with relationships

**Key Features:**
- **Deduplication**: SHA-256 hash prevents storing identical observations within 5 minutes
- **Privacy filtering**: Automatically strips API keys, passwords, tokens
- **Compression**: LLM reduces raw text to structured facts (saves ~92% tokens)
- **4-tier consolidation**: Raw → compressed → facts → graph (automatic lifecycle)

### MindBank Save Pipeline

```
API call (POST /api/v1/nodes)
  -> Validate node_type (ENUM: person, agent, project, topic, decision, fact, event, preference, advice, problem, concept, question)
  -> Insert into PostgreSQL nodes table
  -> Generate tsvector for BM25 search (automatic via GENERATED ALWAYS)
  -> Queue for embedding (embedding_queue table)
  -> Background worker generates embedding via Ollama
  -> Store embedding in node_embeddings table
```

**Storage Schema (PostgreSQL):**
```sql
CREATE TABLE nodes (
    id              TEXT PRIMARY KEY,
    workspace_name  TEXT NOT NULL,
    namespace       TEXT NOT NULL DEFAULT 'global',
    label           TEXT NOT NULL,
    node_type       node_type NOT NULL,  -- ENUM
    content         TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    importance      REAL NOT NULL DEFAULT 0.5,
    access_count    INTEGER NOT NULL DEFAULT 0,
    last_accessed   TIMESTAMPTZ,
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to        TIMESTAMPTZ,          -- NULL = current version
    version         INTEGER NOT NULL DEFAULT 1,
    predecessor_id  TEXT REFERENCES nodes(id),  -- Version chain
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    search_vector   tsvector GENERATED ALWAYS AS (...) STORED,  -- BM25
    materialized_path TEXT                   -- For tree traversal
);

CREATE TABLE edges (
    id              TEXT PRIMARY KEY,
    workspace_name  TEXT NOT NULL,
    source_id       TEXT NOT NULL REFERENCES nodes(id),
    target_id       TEXT NOT NULL REFERENCES nodes(id),
    edge_type       edge_type NOT NULL,     -- ENUM: contains, relates_to, depends_on, etc.
    weight          REAL NOT NULL DEFAULT 1.0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE node_embeddings (
    node_id         TEXT PRIMARY KEY REFERENCES nodes(id),
    embedding       vector(768),            -- pgvector
    model           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Key Features:**
- **Temporal versioning**: Every update creates a new version (valid_from/valid_to)
- **Materialized paths**: Tree traversal support (e.g., "/parent/child/grandchild")
- **Type system**: Strict ENUM for node types and edge types
- **Workspace isolation**: Multi-tenant via workspace_name
- **Full-text search**: PostgreSQL tsvector with GIN index (automatic)

---

## 2. HOW DATA IS RECALLED

### agentmemory Recall Pipeline

```
Query received
  -> Parse intent (what type of memory needed)
  -> BM25 search (lexical matching)
  -> Vector search (semantic similarity)
  -> Graph traversal (connected nodes)
  -> RRF fusion (Reciprocal Rank Fusion)
  -> Importance scoring (recency + frequency + connectivity)
  -> Token budget check
  -> Return top N results
```

**Search Methods:**
1. **BM25** - Fast lexical search via SQLite FTS5
2. **Vector** - Cosine similarity on embeddings
3. **Graph** - 1-hop neighbor expansion
4. **Hybrid** - RRF fusion of all three

**Ranking Formula:**
```
RRF score = Σ(1 / (k + rank_i)) where k=60
Final score = RRF score × importance_boost
```

**Importance Factors:**
- Recency (30-day decay): 30%
- Access frequency: 25%
- Graph connectivity: 20%
- Explicit importance: 15%
- Type weight: 10%

### MindBank Recall Pipeline

```
Query received
  -> If FTS mode: PostgreSQL ts_rank_cd + websearch_to_tsquery
  -> If Vector mode: pgvector cosine similarity
  -> If Hybrid mode:
     - FTS search (tsvector + trigram fallback)
     - Vector search (embedding cosine similarity)
     - RRF fusion (k=60)
     - Namespace boost (+50% for matching namespace)
     - Graph expansion (1-hop neighbors)
  -> Token budget filtering (optional)
  -> Return results
```

**Search Methods:**
1. **FTS** - PostgreSQL ts_rank_cd with websearch_to_tsquery, plainto_tsquery fallback, trigram fallback
2. **Vector** - pgvector `<=>` operator (cosine distance)
3. **Hybrid** - RRF fusion of FTS + Vector + Graph expansion

**Ranking Formula (Hybrid):**
```go
// RRF fusion
scores[id] += 1.0 / (k + float64(rank+1))  // k=60

// Namespace boost
if namespace != "" {
    scores[id] *= 1.5  // 50% boost
}

// Graph expansion
graphScore = edge_weight*0.5 + importance*0.3 + (1.0/access_count)*0.2
finalScore = graphScore * (0.3 + 0.7*anchor_relevance)
```

**Importance Score (SQL):**
```sql
score = 0.30 * recency_decay(30 days)
      + 0.25 * normalized_access_count
      + 0.20 * normalized_edge_count
      + 0.15 * explicit_importance
      + 0.10 * type_weight
```

---

## 3. KEY DIFFERENCES

### Storage Philosophy

| | **agentmemory** | **MindBank** |
|---|---|---|
| **Raw vs Structured** | Stores raw observations + LLM-compressed | Stores structured nodes only |
| **Deduplication** | SHA-256 hash (5-min window) | No automatic dedup (manual) |
| **Privacy** | Automatic secret stripping | No automatic filtering |
| **Compression** | LLM compresses raw → facts | User provides content/summary |
| **Lifecycle** | 4-tier auto-consolidation | Temporal versioning (manual) |

### Search Philosophy

| | **agentmemory** | **MindBank** |
|---|---|---|
| **FTS engine** | SQLite FTS5 | PostgreSQL tsvector + GIN |
| **Vector engine** | iii-engine (custom) | pgvector (standard) |
| **Fallback** | BM25-only mode | Trigram similarity (pg_trgm) |
| **Graph expansion** | 1-hop neighbors | 1-hop with weighted scoring |
| **Token budget** | Hard limit (~1900 tokens) | Configurable slider |
| **Query expansion** | Synonym expansion | websearch_to_tsquery |

### Architecture

| | **agentmemory** | **MindBank** |
|---|---|---|
| **Database** | SQLite (embedded) | PostgreSQL (external) |
| **Embeddings** | all-MiniLM-L6-v2 (built-in) | nomic-embed-text (Ollama) |
| **Deployment** | Single binary (Node.js) | Go binary + Postgres + Ollama |
| **Hooks** | 12 auto-hooks | Scripts + cron |
| **MCP tools** | 51 tools | 10+ tools |
| **Multi-agent** | Leases + signals | Workspace isolation |

---

## 4. WHAT WE CAN ADOPT FROM agentmemory

### Already Implemented in MindBank
- [x] BM25 + Vector hybrid search (RRF fusion)
- [x] Graph expansion (1-hop neighbors)
- [x] Token budget (configurable)
- [x] Importance scoring (multi-factor)
- [x] MCP tools (memory_search, memory_graph_query)
- [x] Namespace/workspace isolation
- [x] Temporal versioning

### Missing (High Value)
- [ ] **Auto-capture hooks** - 12 hooks for zero-manual-effort capture
- [ ] **Deduplication** - SHA-256 hash to prevent duplicate storage
- [ ] **Privacy filtering** - Automatic secret/API key stripping
- [ ] **LLM compression** - Auto-compress raw text to structured facts
- [ ] **4-tier consolidation** - Raw → compressed → facts → graph lifecycle
- [ ] **Session replay** - Full session content viewing with timeline
- [ ] **Real-time viewer** - Live memory building visualization

### Missing (Medium Value)
- [ ] **Synonym expansion** - Expand queries with synonyms
- [ ] **Query intent parsing** - Detect what type of memory is needed
- [ ] **Memory decay** - Auto-forget old/less-important memories
- [ ] **Multi-agent coordination** - Leases and signals for concurrent agents

---

## 5. RECOMMENDATION

MindBank already has a solid foundation with:
- PostgreSQL + pgvector (scalable, standard)
- Hybrid search (BM25 + Vector + Graph)
- Temporal versioning
- Workspace isolation

**Priority adoptions from agentmemory:**

1. **Auto-capture hooks** (Phase 3) - Biggest UX improvement
2. **Deduplication** - Prevent duplicate nodes
3. **Privacy filtering** - Security essential
4. **Session replay** - Complete the session workflow
5. **LLM compression** - Reduce token usage

The architectures are compatible - both use hybrid search (BM25 + Vector + Graph), both support MCP, both have token budgets. The main gap is the **automation layer** (hooks, dedup, compression) that agentmemory provides.
