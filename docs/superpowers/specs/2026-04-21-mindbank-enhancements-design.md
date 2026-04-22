# MindBank Enhancements Design
Date: 2026-04-21

## 1. Bulk Edge Operations (#4)

**Goal:** Add UI tools for batch edge management in the Tools tab.

**Features:**
- **Delete edges by type:** Dropdown selects edge type, preview shows count, confirm deletes all matching edges
- **Connect namespace A → B by similarity:** Select two namespaces, find top N most similar node pairs (via embedding cosine similarity), create edges between them

**Backend:**
- `POST /api/v1/edges/batch` already exists — reuse for creation
- `GET /api/v1/edges?type=X` already exists for listing
- New: `POST /api/v1/edges/cleanup-type` — delete all edges of a given type
- New: `POST /api/v1/edges/connect-namespaces` — semantic cross-namespace linking

**Frontend:**
- Add "Bulk Edge Manager" section in Tools tab
- Two sub-panels: "Delete by Type" and "Connect Namespaces"

**Assumptions:**
- Embedding vectors exist in DB (verified: `embedding` column on nodes table)
- Ollama is available for similarity computation

---

## 2. Node Relationship Suggestions (#5)

**Goal:** When viewing any node, show "Suggested Edges" — unconnected nodes with high embedding similarity.

**Backend:**
- New: `GET /api/v1/nodes/{id}/suggestions?limit=10`
- Query: find nodes where cosine similarity of embeddings > threshold (e.g. 0.75), exclude already-connected nodes
- Use existing pgvector `<=>` operator

**Frontend:**
- Add "Suggestions" panel to node detail modal
- Show up to 10 suggested nodes with similarity score
- One-click "Create Edge" buttons

**Assumptions:**
- Embeddings are populated (most nodes should have them via the worker)
- pgvector extension is installed (verified in migrations)

---

## 3. Graph Analytics Panel (#2)

**Goal:** Dashboard widget showing graph topology metrics.

**Metrics to compute:**
- **Hub nodes:** Top 10 nodes by degree (incoming + outgoing edges)
- **Bridge nodes:** Nodes whose removal would increase connected component count (articulation points)
- **Graph density:** actual edges / possible edges
- **Avg connections:** total edges * 2 / total nodes
- **Isolated clusters:** Count of connected components

**Backend:**
- New: `GET /api/v1/analytics/graph`
- Pure SQL + Go computation, no DB schema changes
- Efficient queries using edge/node counts

**Frontend:**
- Add "Graph Analytics" section to Tools tab
- Cards for each metric with top nodes listed

**Assumptions:**
- Graph fits in memory for computation (currently ~548 nodes, ~274 edges)

---

## 4. Graph Health Trend (#3)

**Goal:** Persist DQA scores over time, show trend in dashboard.

**Backend:**
- New migration: `dqa_snapshots` table (id, score, total_nodes, issues_count, created_at)
- New: `POST /api/v1/dqa/snapshot` — called after each DQA run
- New: `GET /api/v1/dqa/trend?days=30` — returns historical scores

**Frontend:**
- Add sparkline to dashboard showing last 30 days of DQA scores
- Show "↑/↓" indicator comparing latest to previous
- Alert badge when score drops below threshold (configurable, default 60%)

**Assumptions:**
- DQA is run periodically by user (we auto-save on each run)

---

## Implementation Order

1. **#4 Bulk Edge Operations** — fastest, backend mostly done
2. **#5 Node Suggestions** — reuses embedding infra
3. **#2 Graph Analytics** — pure computation, no DB changes
4. **#3 Health Trend** — needs migration + persistence

## Testing Plan

- Each feature gets frontend integration test
- Backend endpoints tested via curl
- Verify no regressions in existing tabs
