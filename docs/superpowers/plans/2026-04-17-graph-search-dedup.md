# MindBank Graph Search + Memory Dedup — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: skill_view('subagent-driven-development') (recommended) or skill_view('executing-plans') to implement this plan task-by-task. Steps use checkbox (`- []`) syntax for tracking.

**Goal:** Fix the 67% graph search recall gap by making search graph-aware, and clean up memory deduplication.

**Architecture:** Add a `GraphExpand` step to HybridSearch that takes top text results, fetches their 1-hop neighbors, and merges them into the result set with graph-aware scoring. Add dedup check on node creation and a cleanup endpoint.

**Tech Stack:** Go 1.23, PostgreSQL 16, pgx/v5, chi/v5

---

## Root Cause Analysis (Praxis Diagnostic Reasoning)

**Problem:** "klixsor architecture" search returns slog/Systemd but misses "Go backend" — which IS a graph neighbor of the klixsor project node at depth 1.

**Root cause:** Search (`HybridSearch`, `FullTextSearch`) never consults the `edges` table. It's pure text matching. Graph and search are disconnected silos.

**Evidence:**
- Direct search for "Go backend" finds it → text search works
- Neighbors of klixsor project include "Go backend" at depth 1 → edges are correct
- "klixsor architecture" finds 5 results, none are "Go backend" → search has no graph awareness

**Memory dedup status:**
- 7 exact duplicate labels across real namespaces (temporal version artifacts)
- No content duplicates
- Mostly harmless but should prevent future accumulation

---

## File Structure

```
Modified:
  internal/repository/search.go    — Add GraphExpand() function, integrate into HybridSearch
  internal/repository/edge.go      — Add GetNeighborsByNodeIDs() batch lookup
  internal/handler/search.go       — Pass expanded results through
  internal/repository/node.go      — Add dedup check in Create()
  internal/repository/snapshot.go  — Deduplicate snapshot generation
```

```
Created:
  benchmarks/test_graph_search.py  — Graph-aware search tests
```

---

### Task 1: Batch Neighbor Lookup — Edge Repository

**Files:**
- Modify: `internal/repository/edge.go`

- [ ] **Step 1: Add GetNeighborsByNodeIDs to EdgeRepo**

Add this function after `GetByNode()`:

```go
// GetNeighborsByNodeIDs returns 1-hop neighbors for multiple nodes at once.
// Returns a map: nodeID -> []NeighborResult (deduped by neighbor ID).
func (r *EdgeRepo) GetNeighborsByNodeIDs(ctx context.Context, nodeIDs []string) (map[string][]models.NodeWithEdge, error) {
    if len(nodeIDs) == 0 {
        return map[string][]models.NodeWithEdge{}, nil
    }

    // Build placeholders for IN clause
    placeholders := make([]string, len(nodeIDs))
    args := make([]any, len(nodeIDs))
    for i, id := range nodeIDs {
        placeholders[i] = fmt.Sprintf("$%d", i+1)
        args[i] = id
    }
    ph := strings.Join(placeholders, ",")

    rows, err := r.pool.Query(ctx, fmt.Sprintf(`
        SELECT
            CASE WHEN e.source_id IN (%s) THEN e.source_id ELSE e.target_id END AS anchor_id,
            n.id, n.workspace_name, n.namespace, n.label, n.node_type, n.content, n.summary,
            n.metadata, n.importance, n.access_count, n.last_accessed, n.valid_from, n.valid_to,
            n.version, n.predecessor_id, n.created_at, n.updated_at,
            e.edge_type::text, e.weight
        FROM edges e
        JOIN nodes n ON n.id = CASE WHEN e.source_id IN (%s) THEN e.target_id ELSE e.source_id END
        WHERE (e.source_id IN (%s) OR e.target_id IN (%s))
          AND n.valid_to IS NULL
        ORDER BY e.weight DESC
    `, ph, ph, ph, ph), args...)
    if err != nil {
        return nil, fmt.Errorf("batch neighbors: %w", err)
    }
    defer rows.Close()

    result := make(map[string][]models.NodeWithEdge)
    for rows.Next() {
        var anchorID string
        var nw models.NodeWithEdge
        err := rows.Scan(
            &anchorID,
            &nw.ID, &nw.WorkspaceName, &nw.Namespace, &nw.Label, &nw.NodeType,
            &nw.Content, &nw.Summary, &nw.Metadata, &nw.Importance, &nw.AccessCount,
            &nw.LastAccessed, &nw.ValidFrom, &nw.ValidTo, &nw.Version,
            &nw.PredecessorID, &nw.CreatedAt, &nw.UpdatedAt,
            &nw.EdgeType, &nw.EdgeWeight,
        )
        if err != nil {
            continue
        }
        result[anchorID] = append(result[anchorID], nw)
    }
    return result, nil
}
```

Make sure `strings` is imported.

- [ ] **Step 2: Verify compilation**

```bash
cd /home/rat/mindbank && go vet ./...
```

---

### Task 2: Graph Expansion — Search Repository

**Files:**
- Modify: `internal/repository/search.go`

- [ ] **Step 1: Add GraphExpand function**

Add after the existing `trigramSearch` function:

```go
// GraphExpand takes top text search results and expands via 1-hop graph neighbors.
// This fixes the "klixsor architecture" → "Go backend" gap where the answer is
// connected via edges but not in the text.
func (r *SearchRepo) GraphExpand(ctx context.Context, textResults []models.SearchResult, edgeRepo *EdgeRepo, limit int) []models.SearchResult {
    if len(textResults) == 0 {
        return textResults
    }

    // Take top 5 text results for expansion (more would be too noisy)
    expandCount := 5
    if len(textResults) < expandCount {
        expandCount = len(textResults)
    }

    // Get node IDs from top results
    nodeIDs := make([]string, expandCount)
    for i := 0; i < expandCount; i++ {
        nodeIDs[i] = textResults[i].NodeID
    }

    // Batch lookup neighbors
    neighbors, err := edgeRepo.GetNeighborsByNodeIDs(ctx, nodeIDs)
    if err != nil {
        slog.Warn("graph expand failed", "error", err)
        return textResults // graceful degradation — return text results as-is
    }

    // Build set of existing result IDs to avoid duplicates
    existing := make(map[string]bool)
    for _, r := range textResults {
        existing[r.NodeID] = true
    }

    // Score and add neighbors
    // Graph score = edge_weight * 0.5 + importance * 0.3 + 1/access_count_boost * 0.2
    type scoredNeighbor struct {
        result models.SearchResult
        score  float32
    }

    var expanded []scoredNeighbor
    seen := make(map[string]bool)

    for anchorID, nbs := range neighbors {
        // Get the anchor's text score for boosting
        var anchorScore float32
        for _, tr := range textResults {
            if tr.NodeID == anchorID {
                anchorScore = tr.RRFScore
                break
            }
        }

        for _, nb := range nbs {
            if existing[nb.ID] || seen[nb.ID] {
                continue
            }
            seen[nb.ID] = true

            // Graph relevance score
            edgeWeight := nb.EdgeWeight
            importance := nb.Importance
            accessBoost := float32(1.0)
            if nb.AccessCount > 0 {
                accessBoost = float32(nb.AccessCount)
            }

            // Combined graph score, boosted by anchor's text relevance
            graphScore := edgeWeight*0.5 + importance*0.3 + (1.0/accessBoost)*0.2
            finalScore := graphScore * (0.3 + 0.7*anchorScore) // anchor relevance gives 70% boost

            expanded = append(expanded, scoredNeighbor{
                result: models.SearchResult{
                    NodeID:    nb.ID,
                    Label:     nb.Label,
                    NodeType:  string(nb.NodeType),
                    Content:   nb.Content,
                    Namespace: nb.Namespace,
                    RRFScore:  finalScore,
                },
                score: finalScore,
            })
        }
    }

    // Sort expanded by score descending
    sort.Slice(expanded, func(i, j int) bool {
        return expanded[i].score > expanded[j].score
    })

    // Take top N expanded results (limit to half of total results to avoid drowning text results)
    maxExpanded := limit / 2
    if maxExpanded < 3 {
        maxExpanded = 3
    }
    if len(expanded) > maxExpanded {
        expanded = expanded[:maxExpanded]
    }

    // Merge: text results first, then graph-expanded
    merged := make([]models.SearchResult, 0, len(textResults)+len(expanded))
    merged = append(merged, textResults...)
    for _, e := range expanded {
        merged = append(merged, e.result)
    }

    return merged
}
```

Make sure `sort` is imported.

- [ ] **Step 2: Integrate into HybridSearch**

Find the `HybridSearch` function. After it computes the RRF results and before returning, add graph expansion:

```go
    // ... existing RRF merge code ...

    // Graph expansion: boost results with graph-connected nodes
    merged = r.GraphExpand(ctx, merged, /* need edgeRepo */, limit)

    if len(merged) > limit {
        merged = merged[:limit]
    }
    return merged, nil
```

**Problem:** `SearchRepo` doesn't have access to `EdgeRepo`. Options:
1. Pass `EdgeRepo` to `SearchRepo` constructor
2. Pass `EdgeRepo` as parameter to `HybridSearch`

**Decision:** Pass as parameter to avoid circular dependency. Update `HybridSearch` signature:

```go
func (r *SearchRepo) HybridSearch(ctx context.Context, query string, workspace, namespace string, limit int, edgeRepo *EdgeRepo) ([]models.SearchResult, error) {
```

Update callers in `search.go` handler to pass the edge repo.

- [ ] **Step 3: Verify compilation**

```bash
cd /home/rat/mindbank && go vet ./...
```

---

### Task 3: Wire EdgeRepo Through Handler

**Files:**
- Modify: `internal/handler/search.go`
- Modify: `internal/handler/router.go`

- [ ] **Step 1: Add EdgeRepo to SearchHandler**

In `search.go`, add `edgeRepo` field:

```go
type SearchHandler struct {
    searchRepo *repository.SearchRepo
    embedder   *embedder.Client
    edgeRepo   *repository.EdgeRepo
}

func NewSearchHandler(searchRepo *repository.SearchRepo, emb *embedder.Client, edgeRepo *repository.EdgeRepo) *SearchHandler {
    return &SearchHandler{searchRepo: searchRepo, embedder: emb, edgeRepo: edgeRepo}
}
```

- [ ] **Step 2: Pass edgeRepo to HybridSearch call**

In the `Hybrid()` handler, change:

```go
    results, err := h.searchRepo.HybridSearch(r.Context(), req.Query, embedding, req.Workspace, req.Namespace, req.Limit, h.edgeRepo)
```

- [ ] **Step 3: Update router.go to pass edgeRepo**

In `router.go`, find where `NewSearchHandler` is called and add the edgeRepo:

```go
    searchHandler := NewSearchHandler(searchRepo, emb, edgeRepo)
```

- [ ] **Step 4: Verify compilation**

```bash
cd /home/rat/mindbank && go vet ./...
```

---

### Task 4: Memory Dedup — Check on Create

**Files:**
- Modify: `internal/repository/node.go`

- [ ] **Step 1: Add dedup check before Create()**

In the `Create()` function, before the INSERT, add a check for existing nodes with the same label+namespace+type:

```go
func (r *NodeRepo) Create(ctx context.Context, req models.NodeCreateRequest) (*models.Node, error) {
    // ... existing workspace/namespace logic ...

    // Dedup check: if node with same label+type+namespace exists, update instead
    var existingID string
    err := r.pool.QueryRow(ctx, `
        SELECT id FROM nodes
        WHERE workspace_name = $1 AND namespace = $2 AND label = $3 AND node_type = $4 AND valid_to IS NULL
        LIMIT 1
    `, ws, ns, req.Label, req.NodeType).Scan(&existingID)

    if err == nil && existingID != "" {
        // Node exists — update content instead of creating duplicate
        updateReq := models.NodeUpdateRequest{
            Content: req.Content,
            Summary: req.Summary,
        }
        if req.Importance != nil {
            updateReq.Importance = req.Importance
        }
        return r.Update(ctx, existingID, updateReq)
    }

    // ... existing INSERT code ...
```

**Gotcha:** The `reasoner.go` already has an ON CONFLICT clause for upsert. This new dedup check is for the manual API create path (node.go). Don't double-apply.

Actually, looking at the code again, the reasoner's `StoreFacts` already uses ON CONFLICT. The manual API `Create()` in node.go does NOT have dedup. So this is the right place.

But there's a subtlety: temporal versioning means PUT creates new IDs. If we auto-update on create, we skip the temporal versioning. Better approach: **reject the create and return the existing node** with a 409 Conflict.

Revised approach:

```go
    // Dedup check: reject if node with same label+type+namespace already exists
    var existingID string
    err := r.pool.QueryRow(ctx, `
        SELECT id FROM nodes
        WHERE workspace_name = $1 AND namespace = $2 AND label = $3 AND node_type = $4 AND valid_to IS NULL
        LIMIT 1
    `, ws, ns, req.Label, req.NodeType).Scan(&existingID)

    if err == nil && existingID != "" {
        // Return existing node with 200 (not error, just informing)
        return r.Get(ctx, existingID)
    }
```

Wait, that changes the create semantics. Let me think about this differently.

**Better approach:** Don't block creation. Instead, add a `POST /api/v1/nodes/dedup` endpoint that finds and merges duplicates. This is a maintenance operation, not a create-time check.

Actually, the simplest effective approach: **dedup at snapshot/search time, not create time.** The snapshot already deduplicates by label+type. The search already works fine with duplicates. The real issue is the graph has 7 duplicate labels in real namespaces. Let's add a cleanup endpoint instead.

- [ ] **Step 1 revised: Add dedup cleanup endpoint**

Add to `internal/handler/node.go`:

```go
// Dedup handles POST /api/v1/nodes/dedup
// Finds duplicate nodes (same label+type+namespace) and soft-deletes older versions.
func (h *NodeHandler) Dedup(w http.ResponseWriter, r *http.Request) {
    namespace := r.URL.Query().Get("namespace")
    dryRun := r.URL.Query().Get("dry_run") == "true"

    // Find duplicates
    rows, err := h.repo.Pool().Query(r.Context(), `
        SELECT label, node_type, namespace, array_agg(id ORDER BY created_at DESC) AS ids, COUNT(*) as cnt
        FROM nodes
        WHERE valid_to IS NULL AND ($1 = '' OR namespace = $1)
        GROUP BY workspace_name, namespace, label, node_type
        HAVING COUNT(*) > 1
        ORDER BY COUNT(*) DESC
    `, namespace)
    if err != nil {
        respondError(w, 500, "dedup query failed")
        return
    }
    defer rows.Close()

    type DupGroup struct {
        Label     string   `json:"label"`
        NodeType  string   `json:"node_type"`
        Namespace string   `json:"namespace"`
        IDs       []string `json:"ids"`
        Count     int      `json:"count"`
    }

    var groups []DupGroup
    var totalDupes int

    for rows.Next() {
        var g DupGroup
        var idArray []string
        if err := rows.Scan(&g.Label, &g.NodeType, &g.Namespace, &idArray, &g.Count); err != nil {
            continue
        }
        g.IDs = idArray
        totalDupes += g.Count - 1 // Keep 1, delete rest
        groups = append(groups, g)
    }

    if dryRun || len(groups) == 0 {
        respondJSON(w, 200, map[string]any{
            "duplicate_groups": len(groups),
            "nodes_to_remove":  totalDupes,
            "groups":           groups,
            "dry_run":          true,
        })
        return
    }

    // Soft-delete all but the newest in each group
    deleted := 0
    for _, g := range groups {
        // Keep the first (newest), delete the rest
        for _, id := range g.IDs[1:] {
            _, err := h.repo.SoftDelete(r.Context(), id)
            if err == nil {
                deleted++
            }
        }
    }

    respondJSON(w, 200, map[string]any{
        "duplicate_groups": len(groups),
        "nodes_deleted":    deleted,
        "dry_run":          false,
    })
}
```

- [ ] **Step 2: Add SoftDelete to NodeRepo**

Check if `SoftDelete` already exists in node.go. If not, add:

```go
// SoftDelete sets valid_to on a node (temporal delete).
func (r *NodeRepo) SoftDelete(ctx context.Context, id string) (bool, error) {
    result, err := r.pool.Exec(ctx, `
        UPDATE nodes SET valid_to = now(), updated_at = now()
        WHERE id = $1 AND valid_to IS NULL
    `, id)
    if err != nil {
        return false, err
    }
    return result.RowsAffected() > 0, nil
}
```

- [ ] **Step 3: Wire dedup endpoint in router**

In `router.go`, add:

```go
    r.Post("/api/v1/nodes/dedup", nodeHandler.Dedup)
```

- [ ] **Step 4: Verify compilation**

```bash
cd /home/rat/mindbank && go vet ./...
```

---

### Task 5: Tests

**Files:**
- Create: `benchmarks/test_graph_search.py`

- [ ] **Step 1: Write graph search test**

```python
#!/usr/bin/env python3
"""Test graph-aware search expansion."""
import json, urllib.request, sys

API = "http://localhost:8095/api/v1"

def api(path, body=None, method=None):
    url = API + path
    data = json.dumps(body).encode() if body else None
    if method is None:
        method = 'POST' if body else 'GET'
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header('Content-Type', 'application/json')
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return json.loads(r.read())
    except Exception as e:
        return {'error': str(e)}

passed = 0
failed = 0

def test(name, condition):
    global passed, failed
    if condition:
        passed += 1
        print(f"  PASS: {name}")
    else:
        failed += 1
        print(f"  FAIL: {name}")

# Test 1: "klixsor architecture" should find "Go backend" via graph expansion
print("Test 1: Graph-expansion finds Go backend for 'klixsor architecture'")
r = api("/search/hybrid", {"query": "klixsor architecture", "namespace": "klixsor", "limit": 10})
labels = [x.get("label", "").lower() for x in r]
go_found = any("go backend" in l or "go over python" in l or "klixsor uses go" in l for l in labels)
test("'klixsor architecture' finds Go-related node", go_found)
if not go_found:
    print(f"    Got: {[l[:30] for l in labels[:5]]}")

# Test 2: "mindbank design decisions" should find "pgvector"
print("Test 2: Graph-expansion finds pgvector for 'mindbank design decisions'")
r = api("/search/hybrid", {"query": "mindbank design decisions", "namespace": "mindbank", "limit": 10})
labels = [x.get("label", "").lower() for x in r]
pg_found = any("pgvector" in l for l in labels)
test("'mindbank design decisions' finds pgvector", pg_found)
if not pg_found:
    print(f"    Got: {[l[:30] for l in labels[:5]]}")

# Test 3: Graph expansion should NOT degrade text relevance
print("Test 3: Text results still ranked first")
r = api("/search/hybrid", {"query": "ClickHouse analytics", "namespace": "klixsor", "limit": 10})
labels = [x.get("label", "").lower() for x in r]
clickhouse_first = any("clickhouse" in l for l in labels[:3])
test("'ClickHouse analytics' still has ClickHouse in top 3", clickhouse_first)

# Test 4: Dedup endpoint works
print("Test 4: Dedup endpoint")
dedup = api("/nodes/dedup?dry_run=true")
test("dedup dry_run returns result", "duplicate_groups" in dedup)
print(f"  Found {dedup.get('duplicate_groups', 0)} duplicate groups, {dedup.get('nodes_to_remove', 0)} to remove")

# Test 5: Dedup with namespace filter
print("Test 5: Dedup namespace filter")
dedup_hermes = api("/nodes/dedup?namespace=hermes&dry_run=true")
test("dedup with namespace filter works", "duplicate_groups" in dedup_hermes)

print(f"\n{'='*50}")
print(f"Results: {passed} passed, {failed} failed")
print(f"{'='*50}")
sys.exit(0 if failed == 0 else 1)
```

- [ ] **Step 2: Run existing tests to verify no regression**

```bash
cd /home/rat/mindbank && python3 benchmarks/test_500_final.py
```

Expected: All 74 tests still pass.

- [ ] **Step 3: Run graph search tests**

```bash
cd /home/rat/mindbank && python3 benchmarks/test_graph_search.py
```

Expected: All 5 tests pass (especially #1 and #2).

---

### Task 6: Build + Deploy + Verify

- [ ] **Step 1: Build**

```bash
cd /home/rat/mindbank && CGO_ENABLED=0 go build -o mindbank-api cmd/mindbank/main.go
```

- [ ] **Step 2: Deploy**

```bash
pkill mindbank-api 2>/dev/null; sleep 1
cd /home/rat/mindbank
MB_DB_DSN="postgres://mindbank:YOUR_DB_PASSWORD@localhost:5434/mindbank?sslmode=disable" nohup ./mindbank-api > /tmp/mindbank.log 2>&1 &
sleep 2
curl -s http://localhost:8095/api/v1/health | python3 -m json.tool
```

- [ ] **Step 3: Verify graph search fix**

```bash
curl -s -X POST http://localhost:8095/api/v1/search/hybrid \
  -H 'Content-Type: application/json' \
  -d '{"query":"klixsor architecture","namespace":"klixsor","limit":10}' | \
  python3 -c "import json,sys; r=json.load(sys.stdin); print([x['label'][:40] for x in r])"
```

Expected: Results include "Go backend" or "Go over Python" or similar Go-related nodes.

- [ ] **Step 4: Run full test suite**

```bash
cd /home/rat/mindbank && python3 benchmarks/test_graph_search.py && python3 benchmarks/test_500_final.py
```

- [ ] **Step 5: Run dedup cleanup (real, not dry_run)**

```bash
curl -s -X POST 'http://localhost:8095/api/v1/nodes/dedup' | python3 -m json.tool
```

Expected: Reports nodes deleted (should be ~7).

- [ ] **Step 6: Verify post-dedup**

```bash
curl -s 'http://localhost:8095/api/v1/nodes/dedup?dry_run=true' | python3 -m json.tool
```

Expected: `nodes_to_remove: 0`.

- [ ] **Step 7: Commit**

```bash
cd /home/rat/mindbank
git add -A
git commit -m "feat: graph-aware search + memory dedup

- GraphExpand: hybrid search now expands top results via 1-hop neighbors
- Fixes 'klixsor architecture' → 'Go backend' (67% → 95%+ graph recall)
- Batch neighbor lookup (GetNeighborsByNodeIDs) for efficient expansion
- Dedup endpoint: POST /nodes/dedup?namespace=X&dry_run=true
- Cleans duplicate nodes (same label+type+namespace, keeps newest)
- 7 duplicates removed from real namespaces
- 5 new tests in test_graph_search.py
- No regression in existing 74-test suite"
```

---

## Summary

| Task | What | Files | Effort |
|------|------|-------|--------|
| 1 | Batch neighbor lookup | internal/repository/edge.go | 10 min |
| 2 | Graph expansion logic | internal/repository/search.go | 15 min |
| 3 | Wire edgeRepo through handler | internal/handler/search.go, router.go | 5 min |
| 4 | Dedup endpoint | internal/handler/node.go, internal/repository/node.go | 10 min |
| 5 | Tests | benchmarks/test_graph_search.py | 10 min |
| 6 | Build + deploy + verify | all | 10 min |
| **Total** | | | **~60 min** |

## Expected Impact

| Metric | Before | After |
|---|---|---|
| Graph category recall | 67% | 95%+ |
| Overall PRAXIS | 98.7% | 99.5%+ |
| Duplicate labels | 7 | 0 |
| "klixsor architecture" finds Go backend | No | Yes |
| "mindbank design decisions" finds pgvector | No | Yes |

## Backward Compatibility

- HybridSearch signature adds optional `edgeRepo` parameter
- New `/nodes/dedup` endpoint — additive, no existing endpoints changed
- Graph expansion is additive — text results still ranked first
- Dedup only soft-deletes older duplicates — data preserved in temporal versions
