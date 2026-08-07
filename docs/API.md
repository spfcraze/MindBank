# MindBank API Reference

Base URL: `http://localhost:8095/api/v1`

All endpoints accept and return JSON unless noted.

## Authentication

Set `MB_API_KEY` in `.env` to require Bearer token auth on all endpoints.

```
Authorization: Bearer ***
```

If `MB_API_KEY` is empty, auth is disabled (development mode).

## Nodes

### Create Node

```
POST /api/v1/nodes
```

Request:
```json
{
  "label": "Use JWT for auth",
  "node_type": "decision",
  "content": "JWT with access + refresh tokens",
  "summary": "Short description (optional)",
  "namespace": "my-project",
  "importance": 0.8,
  "epistemic_label": "observed"
}
```

Response: `201 Created` — the full node object.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| label | string | Yes | Short name (max 512 chars) |
| node_type | string | Yes | One of the 12 node types |
| content | string | No | Full content (max 50KB) |
| summary | string | No | Short summary (max 1KB) |
| namespace | string | No | Project namespace (default: "global") |
| importance | float | No | 0.0-1.0 (default: 0.5) |
| epistemic_label | string | No | observed, inferred, assumed, recommended, unknown (default: unknown) |
| metadata | object | No | JSON metadata |

### Get Node

```
GET /api/v1/nodes/{id}
```

Returns the current version of a node. Temporal: returns 404 if node was updated (old version has `valid_to` set).

### Update Node

```
PUT /api/v1/nodes/{id}
```

Request (only these fields are updatable):
```json
{
  "content": "Updated content",
  "summary": "Updated summary",
  "importance": 0.9
}
```

**Temporal versioning:** This creates a NEW node with a new ID. The old node gets `valid_to` set. Always use the new ID from the response for subsequent operations.

### Delete Node

```
DELETE /api/v1/nodes/{id}
```

Soft-delete: sets `valid_to` to now. Node is preserved for temporal queries. Connected edges are also soft-deleted.

### List Nodes

```
GET /api/v1/nodes?namespace=my-project&type=decision&limit=50&offset=0
```

| Param | Type | Description |
|-------|------|-------------|
| namespace | string | Filter by namespace |
| type | string | Filter by node type |
| limit | int | Max results (default 50, max 100) |
| offset | int | Pagination offset |

### Batch Create

```
POST /api/v1/nodes/batch
```

```json
{
  "nodes": [
    {"label": "Node 1", "node_type": "fact", "content": "...", "namespace": "test"},
    {"label": "Node 2", "node_type": "decision", "content": "...", "namespace": "test"}
  ]
}
```

Max 100 nodes per batch.

### Auto-Connect

```
POST /api/v1/nodes/auto-connect
```

```json
{"namespace": "my-project"}
```

Creates semantic edges between related nodes based on type-matching rules. Returns count of edges created.

### Dedup

```
POST /api/v1/nodes/dedup?namespace=my-project&dry_run=true
```

Finds duplicate nodes (same label + type + namespace) and soft-deletes older versions.

| Param | Type | Description |
|-------|------|-------------|
| namespace | string | Scope to namespace (empty = all) |
| dry_run | bool | If true, report only (default: false) |

### Update Epistemic Label

```
PUT /api/v1/nodes/epistemic?label=observed&node_id=uuid
```

Valid labels: `observed`, `inferred`, `assumed`, `recommended`, `unknown`

Response:
```json
{
  "node_id": "uuid",
  "epistemic_label": "observed"
}
```

## Edges

### Create Edge

```
POST /api/v1/edges
```

```json
{
  "source_id": "node-uuid-1",
  "target_id": "node-uuid-2",
  "edge_type": "contains",
  "weight": 1.0
}
```

Valid edge types: `contains`, `relates_to`, `depends_on`, `decided_by`, `participated_in`, `produced`, `contradicts`, `supports`, `derived_from`, `tested_by`, `temporal_next`, `mentions`, `learned_from`

### List Edges

```
GET /api/v1/edges?type=contains&limit=500
```

### Get Node Neighbors

```
GET /api/v1/nodes/{id}/neighbors?depth=2&limit=100
```

Returns nodes connected to the given node. `depth` controls traversal depth (1-3).

### Batch Create Edges

```
POST /api/v1/edges/batch
```

```json
{
  "edges": [
    {"source_id": "...", "target_id": "...", "edge_type": "contains", "weight": 1.0},
    {"source_id": "...", "target_id": "...", "edge_type": "depends_on", "weight": 0.8}
  ]
}
```

Max 200 edges per batch.

## Search

### Full-Text Search

```
GET /api/v1/search?q=jwt+auth&namespace=my-project&limit=10
```

PostgreSQL full-text search with synonym expansion and trigram fallback.

### Hybrid Search

```
POST /api/v1/search/hybrid
```

```json
{
  "query": "how do we handle authentication",
  "namespace": "my-project",
  "limit": 10
}
```

Combines full-text search + vector semantic search via Reciprocal Rank Fusion. Includes graph expansion — finds nodes connected via edges even if text doesn't match.

### Semantic Search

```
POST /api/v1/search/semantic
```

```json
{
  "query": "database configuration",
  "namespace": "my-project",
  "limit": 10
}
```

Pure vector similarity search.

## Ask

```
POST /api/v1/ask
```

```json
{
  "query": "what database are we using?",
  "namespace": "my-project",
  "max_tokens": 500
}
```

Natural language Q&A. Returns relevant nodes and graph paths formatted as context.

## Snapshot

```
GET /api/v1/snapshot?namespace=my-project
```

Pre-computed context of the most important memories. Use this at session start to load relevant context. Results are importance-scored and deduplicated.

## Graph

```
GET /api/v1/graph?namespace=my-project
```

Returns all current nodes and edges. Used by the web dashboard.

## Export / Import

```
GET /api/v1/export?namespace=my-project
POST /api/v1/import
```

Export graph as JSON, import from JSON. Useful for backups and migrations.

## Embeddings

```
POST /api/v1/embeddings/generate
```

```json
{"text": "text to embed"}
```

Returns 768-dim vector from nomic-embed-text via Ollama.

## Health & Metrics

```
GET /api/v1/health
```
Returns: `{"status":"ok","postgres":"connected","ollama":"connected","version":"0.1.0"}`

```
GET /api/v1/metrics
```
Returns Prometheus-format metrics: node counts by namespace, edge counts, up status.

## Temporal Versioning

When you `PUT /nodes/{id}`, MindBank:
1. Creates a new node with a new UUID
2. Sets `valid_to` on the old node
3. Links new → old via `predecessor_id`
4. Increments `version`
5. Relinks all edges from old ID to new ID

This means:
- `GET /nodes/{id}` returns only current versions (`valid_to IS NULL`)
- `GET /nodes/{id}/history` returns all versions
- Old data is never lost
- Edges always point to the current version

## Confidence Scoring (Enhanced V3)

The system provides confidence scoring for nodes based on topology + epistemic signals.

### Formula
```
confidence = 0.25×frequency + 0.20×connectivity + 0.15×ageStability
           + 0.10×importance + 0.15×groundingScore + 0.15×epistemicBonus
           - 0.05×contradictionPenalty
```

### Components
| Factor | Weight | Description |
|--------|--------|-------------|
| frequency | 0.25 | min(access_count / 50, 1.0) |
| connectivity | 0.20 | min(edge_count / 10, 1.0) |
| ageStability | 0.15 | 1.0 - min(age_days / 365, 1.0) — newer is better |
| importance | 0.10 | User-assigned score (0.0-1.0) |
| groundingScore | 0.15 | min(evidence_count / 5, 1.0) — counts supports, derived_from, tested_by edges |
| epistemicBonus | 0.15 | observed(+0.15), inferred(+0.05), assumed(-0.15), recommended(0), unknown(0) |
| contradictionPenalty | 0.05 | min(contradiction_count × 0.10, 0.30) |

### Trust Levels
- **High**: ≥ 0.60
- **Medium**: ≥ 0.35
- **Low**: < 0.35

### Endpoints
```
GET /api/v1/analyze/confidence?node_id=uuid
GET /api/v1/analyze/confidence?namespace=my-project
```

Response includes: `score`, `trust_level`, `breakdown` (per-factor scores), `edge_count`, `contradiction_count`, `evidence_count`, `epistemic_label`.

## Analysis Endpoints

### Contradictions
```
GET /api/v1/analyze/contradictions?namespace=my-project
```
Lists all `contradicts` edges with source/target summaries.

### Gaps
```
GET /api/v1/analyze/gaps?namespace=my-project
```
Detects: orphan nodes (0 edges), unanswered questions, unsolved problems, stale nodes.

### Dependence Graph
```
GET /api/v1/analyze/dependence?node_id=uuid&max_depth=3
```
Returns upstream/downstream dependency chains.

### Diff
```
GET /api/v1/analyze/diff?since=2024-01-01T00:00:00Z&namespace=my-project
```
Reports new/updated/deleted nodes and edges since timestamp.

### Heal
```
POST /api/v1/analyze/heal?namespace=my-project&dry_run=true
```
Auto-links orphan nodes to related content via FTS similarity. Review suggested links before applying.
