# MindBank System Improvements Plan

> **For agentic workers:** REQUIRED SUB-SKILL: skill_view('subagent-driven-development') (recommended) or skill_view('executing-plans') to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix broken UI filters, deduplicate migrations, split monolithic code, add resilience, unify patterns, and add pagination — verified by Darwin rubric on each phase.

**Architecture:** 5 independent phases, each produces working code on its own. No cross-phase dependencies.

**Tech Stack:** Go, PostgreSQL, Chi router, Canvas 2D, vanilla JS

---

## Darwin Validation (per phase)

Each phase must pass this 5-point checklist before moving to the next:
- [ ] **Does it fix a real bug or measurably improve something?** (not cosmetic)
- [ ] **Is it surgical?** (touches only what's needed, no adjacent "improvements")
- [ ] **Is it testable?** (can verify with curl, UI check, or build)
- [ ] **Is it reversible?** (single commit, can revert cleanly)
- [ ] **Does it NOT break existing functionality?** (build passes, endpoints respond)

---

## Phase 1: Deduplicate Migrations

**Problem:** Two migration directories exist: `./migrations/` and `./internal/db/migrations/`. They've diverged:
- `migrations/003_nodes.sql` — missing `question` in node_type enum
- `migrations/055_question_node_type.sql` — adds `question` (redundant since 003 in internal already has it)
- `migrations/056_materialized_path.sql` — has backfill logic and comments (better version)
- `internal/db/migrations/` is authoritative (Go embed reads from here)

**Darwin score:** Edge Case Coverage 5/10 → 8/10. Confusing dual source-of-truth is a bug waiting to happen.

**Files:**
- Copy: `migrations/056_materialized_path.sql` → `internal/db/migrations/056_materialized_path.sql` (better version has backfill)
- Remove: `migrations/` directory entirely (use internal/db/migrations as single source of truth)

**Steps:**

- [ ] **Step 1: Verify the better 056 version**

```bash
diff migrations/056_materialized_path.sql internal/db/migrations/056_materialized_path.sql
# root version has: comments, index, backfill recursive CTE — better
```

- [ ] **Step 2: Copy better 056 to internal/db/migrations**

```bash
cp migrations/056_materialized_path.sql internal/db/migrations/056_materialized_path.sql
```

- [ ] **Step 3: Copy better 056 to ready too**

```bash
cp migrations/056_materialized_path.sql ready/internal/db/migrations/056_materialized_path.sql
```

- [ ] **Step 4: Remove redundant root migrations directory**

```bash
rm -rf migrations/
```

- [ ] **Step 5: Verify nothing references the old path**

```bash
grep -rn "migrations/" --include="*.go" --include="*.sh" --include="Makefile" . | grep -v "internal/db/migrations" | grep -v ready | grep -v ".git"
# Should return 0 matches (only ready/ copies)
```

- [ ] **Step 6: Verify build still works**

```bash
cd ready && go build -o mindbank-api ./cmd/mindbank/
```

---

## Phase 2: Split analyze.go into Per-Concern Files

**Problem:** `analyze.go` is 1092 lines with 7 unrelated intelligence endpoints. Each is a different concern (contradictions, gaps, diff, patterns, confidence, link-orphans, merge-duplicates) plus shared helpers. Hard to navigate, hard to review, hard to maintain.

**Darwin score:** Overall Architecture 6/10 → 8/10. Monolithic file is a maintenance risk.

**Current function breakdown (lines):**
```
L12-L91:    AnalyzeHandler + NewAnalyzeHandler + Contradictions (80 lines)
L93-L249:   Gaps (157 lines)
L251-L435:  Diff (185 lines)
L436-L462:  truncate + itoa helpers (27 lines)
L465-L605:  Patterns (141 lines)
L607-L804:  Confidence (198 lines)
L806-L918:  LinkOrphans (113 lines)
L920-L1091: MergeDuplicates (172 lines)
```

**Files:**
- Create: `internal/handler/analyze_common.go` — AnalyzeHandler struct, NewAnalyzeHandler, truncate, itoa
- Create: `internal/handler/analyze_contradictions.go` — Contradictions endpoint
- Create: `internal/handler/analyze_gaps.go` — Gaps endpoint
- Create: `internal/handler/analyze_diff.go` — Diff endpoint
- Create: `internal/handler/analyze_patterns.go` — Patterns endpoint
- Create: `internal/handler/analyze_confidence.go` — Confidence endpoint
- Create: `internal/handler/analyze_heal.go` — LinkOrphans + MergeDuplicates
- Delete: `internal/handler/analyze.go` (replaced by above files)

**Steps:**

- [ ] **Step 1: Create analyze_common.go**

Extract from analyze.go:
```go
package handler

import (
    "github.com/jackc/pgx/v5/pgxpool"
)

// AnalyzeHandler provides intelligence endpoints for graph analysis.
type AnalyzeHandler struct {
    pool *pgxpool.Pool
}

// NewAnalyzeHandler creates a new analyze handler.
func NewAnalyzeHandler(pool *pgxpool.Pool) *AnalyzeHandler {
    return &AnalyzeHandler{pool: pool}
}

// truncate returns the first n characters of s with "..." if truncated.
func truncate(s string, n int) string {
    if len(s) <= n {
        return s
    }
    return s[:n] + "..."
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
    return strconv.Itoa(n)
}
```

Wait — `itoa` uses strconv but `truncate` doesn't. Check imports carefully when splitting.

- [ ] **Step 2: Create analyze_contradictions.go**

Copy lines 24-91 from analyze.go (Contradictions function). Add `package handler` and needed imports.

- [ ] **Step 3: Create analyze_gaps.go**

Copy lines 95-249 from analyze.go (Gaps function).

- [ ] **Step 4: Create analyze_diff.go**

Copy lines 253-435 from analyze.go (Diff function).

- [ ] **Step 5: Create analyze_patterns.go**

Copy lines 468-605 from analyze.go (Patterns function).

- [ ] **Step 6: Create analyze_confidence.go**

Copy lines 609-804 from analyze.go (Confidence function).

- [ ] **Step 7: Create analyze_heal.go**

Copy lines 808-918 (LinkOrphans) and 922-1091 (MergeDuplicates).

- [ ] **Step 8: Delete analyze.go**

```bash
rm internal/handler/analyze.go
```

- [ ] **Step 9: Sync and build**

```bash
# Sync all new files to ready
cp internal/handler/analyze_*.go ready/internal/handler/
rm ready/internal/handler/analyze.go
# Build
cd ready && go build -o mindbank-api ./cmd/mindbank/
```

- [ ] **Step 10: Verify all endpoints still work**

```bash
./mindbank-api &
sleep 2
curl -s localhost:8095/api/v1/analyze/contradictions | jq .count
curl -s localhost:8095/api/v1/analyze/gaps | jq .total
curl -s "localhost:8095/api/v1/analyze/diff?since=last-session" | jq .new_nodes
curl -s localhost:8095/api/v1/analyze/patterns | jq .count
curl -s localhost:8095/api/v1/analyze/confidence | jq .count
```

---

## Phase 3: Ollama Circuit Breaker

**Problem:** When Ollama is down, the embedder worker keeps retrying every 2 seconds, filling the queue with failed attempts. No backoff, no circuit breaker. The queue table grows unbounded.

**Darwin score:** Edge Case Coverage 5/10 → 8/10. Missing resilience for a known failure mode.

**Files:**
- Modify: `internal/embedder/worker.go` — add circuit breaker state and logic

**Steps:**

- [ ] **Step 1: Add circuit breaker state to Worker struct**

```go
type Worker struct {
    pool      *pgxpool.Pool
    client    *Client
    batchSize int
    interval  time.Duration

    // Circuit breaker
    consecutiveFailures int
    circuitOpenUntil    time.Time
    lastFailureTime     time.Time
}

const (
    maxConsecutiveFailures = 5
    circuitBreakDuration   = 30 * time.Second
)
```

- [ ] **Step 2: Check circuit breaker before processing**

In `Run()`, check if circuit is open:

```go
func (w *Worker) Run(ctx context.Context) {
    slog.Info("embedding worker started")
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            slog.Info("embedding worker stopped")
            return
        case <-ticker.C:
            // Circuit breaker check
            if time.Now().Before(w.circuitOpenUntil) {
                slog.Warn("embedding circuit open, skipping batch",
                    "failures", w.consecutiveFailures,
                    "retry_at", w.circuitOpenUntil.Format(time.RFC3339))
                continue
            }
            w.processBatch(ctx)
        }
    }
}
```

- [ ] **Step 3: Track failures and open circuit**

In `processBatch()`, track consecutive failures:

```go
func (w *Worker) processBatch(ctx context.Context) {
    rows, err := w.pool.Query(ctx, `...`, w.batchSize)
    if err != nil {
        w.recordFailure("fetch queue items", err)
        return
    }
    // ... rest of processing ...

    // On success, reset circuit breaker
    if w.consecutiveFailures > 0 {
        slog.Info("embedding circuit closed after success",
            "was_failures", w.consecutiveFailures)
    }
    w.consecutiveFailures = 0
    w.circuitOpenUntil = time.Time{}
}

func (w *Worker) recordFailure(context string, err error) {
    w.consecutiveFailures++
    w.lastFailureTime = time.Now()
    slog.Error(context, "error", err, "consecutive_failures", w.consecutiveFailures)

    if w.consecutiveFailures >= maxConsecutiveFailures {
        w.circuitOpenUntil = time.Now().Add(circuitBreakDuration)
        slog.Warn("embedding circuit opened",
            "failures", w.consecutiveFailures,
            "retry_after", circuitBreakDuration)
    }
}
```

- [ ] **Step 4: Also track failures in processBatchItems**

When individual items fail to embed (Ollama returns error), call `recordFailure` too:

```go
// In processBatchItems, after embed call:
if err != nil {
    w.recordFailure("embed batch", err)
    // Mark items as failed
    for _, item := range batch {
        w.markFailed(ctx, item.ID, fmt.Sprintf("embed failed: %v", err))
    }
    return
}
```

- [ ] **Step 5: Sync and build**

```bash
cp internal/embedder/worker.go ready/internal/embedder/worker.go
cd ready && go build -o mindbank-api ./cmd/mindbank/
```

- [ ] **Step 6: Verify circuit breaker**

Stop Ollama, create a node, watch logs for "circuit open" after 5 failures. Restart Ollama, watch for "circuit closed after success".

---

## Phase 4: Unify Tab Loading Pattern

**Problem:** Each tab loads data differently:
- Dashboard: auto-loads via `loadStats()` + `loadNodes()` on page load
- Graph: auto-loads via `loadGraph()` on tab switch
- Brain: lazy-loads iframe on first tab switch
- Questions/Edges: load on tab switch

No consistent pattern. Some cache, some don't. Some auto-refresh, some don't.

**Darwin score:** Overall Architecture 5/10 → 7/10. Inconsistent behavior confuses users.

**Files:**
- Modify: `internal/handler/static/index.html` — `switchTab()` function and tab loading logic

**Steps:**

- [ ] **Step 1: Add loaded flags to all tabs**

```javascript
let tabLoaded = { dashboard: false, questions: false, edges: false, tools: false, graph: false, brain: false };
```

- [ ] **Step 2: Update switchTab to use unified pattern**

```javascript
function switchTab(name){
  document.querySelectorAll('.tab').forEach((t,i)=>{
    t.classList.toggle('active',['dashboard','questions','edges','tools','graph','brain'][i]===name);
  });
  document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
  document.getElementById('tab-'+name).classList.add('active');

  // Unified lazy-load pattern: load once on first switch, cache thereafter
  if(!tabLoaded[name]){
    tabLoaded[name] = true;
    switch(name){
      case 'dashboard': loadStats(); loadNodes(); break;
      case 'questions': loadQuestions(); break;
      case 'edges': loadEdges(); break;
      case 'graph': loadGraph(); break;
      case 'brain':
        const f=document.getElementById('brainFrame');
        if(!f.dataset.loaded){f.src='/graph-view?embed=1';f.dataset.loaded='1'}
        f.style.width='100%';f.style.height=(window.innerHeight-60)+'px';
        break;
    }
  }
}
```

- [ ] **Step 3: Remove redundant auto-calls**

Currently `loadStats()` runs on page load (outside switchTab). Keep this for dashboard (it's the default tab). But remove any other auto-loading that duplicates the lazy-load pattern.

- [ ] **Step 4: Sync and rebuild**

```bash
cp internal/handler/static/index.html ready/internal/handler/static/index.html
cd ready && go build -o mindbank-api ./cmd/mindbank/
```

- [ ] **Step 5: Verify**

Open dashboard — stats should load. Switch to Questions — should load once. Switch back — should NOT re-fetch. Same for all tabs.

---

## Phase 5: Graph Endpoint Pagination

**Problem:** `GET /api/v1/graph` returns ALL nodes (capped at 200) and ALL edges between them. As the graph grows, this becomes a performance bottleneck. Brain 3D consumes this endpoint.

**Darwin score:** Edge Case Coverage 5/10 → 7/10. Current 200 cap is a silent truncation — user doesn't know nodes are missing.

**Files:**
- Modify: `internal/handler/ask.go` — `Graph()` function (line ~177)
- Modify: `internal/handler/static/graph.html` — `load()` to accept params

**Steps:**

- [ ] **Step 1: Add limit/offset params to Graph handler**

```go
func (h *AskHandler) Graph(w http.ResponseWriter, r *http.Request) {
    namespace := r.URL.Query().Get("namespace")
    workspace := r.URL.Query().Get("workspace")
    if workspace == "" {
        workspace = "hermes"
    }

    // Pagination
    limit := 200 // default
    if l := r.URL.Query().Get("limit"); l != "" {
        if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 2000 {
            limit = parsed
        }
    }
    offset := 0
    if o := r.URL.Query().Get("offset"); o != "" {
        if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
            offset = parsed
        }
    }

    // ... existing query with LIMIT/OFFSET ...
    query += fmt.Sprintf(" ORDER BY importance DESC, access_count DESC LIMIT $%d OFFSET $%d", argN, argN+1)
    args = append(args, limit, offset)
    argN += 2

    // ... rest of handler ...

    // Include pagination metadata in response
    respondJSON(w, 200, map[string]any{
        "nodes":  nodes,
        "edges":  edges,
        "limit":  limit,
        "offset": offset,
    })
}
```

- [ ] **Step 2: Update frontend to use params**

For Brain 3D (graph.html), keep default:

```javascript
// No change needed — default limit=200 is fine for visualization
const d=await(await fetch('/api/v1/graph')).json();
```

- [ ] **Step 3: Sync and build**

```bash
cp internal/handler/ask.go ready/internal/handler/ask.go
cd ready && go build -o mindbank-api ./cmd/mindbank/
```

- [ ] **Step 4: Verify pagination works**

```bash
# Default (200)
curl -s localhost:8095/api/v1/graph | jq '.nodes | length'

# Custom limit
curl -s "localhost:8095/api/v1/graph?limit=50" | jq '.nodes | length'

# With offset
curl -s "localhost:8095/api/v1/graph?limit=50&offset=50" | jq '.nodes | length'

# Max cap (2000)
curl -s "localhost:8095/api/v1/graph?limit=5000" | jq '.limit'
# Should return 2000
```

---

## Summary

| Phase | What | Darwin Improvement | Lines Changed |
|-------|------|-------------------|---------------|
| 1 | Dedup migrations | Edge Case 5→8 | file moves |
| 2 | Split analyze.go | Architecture 6→8 | ~1092 → ~7 files |
| 3 | Ollama circuit breaker | Edge Case 5→8 | ~40 |
| 4 | Unify tab loading | Architecture 5→7 | ~20 |
| 5 | Graph pagination | Edge Case 5→7 | ~25 |

**Total estimated time:** ~2 hours
**Each phase is independent** — can be done in any order, each produces working code.

---

## Execution Order

1. Phase 1 (Migrations) — cleanup, prevents future confusion
2. Phase 2 (Split analyze.go) — code quality, no behavior change
3. Phase 3 (Circuit breaker) — resilience, prevents queue flood
4. Phase 4 (Tab loading) — consistency, reduces redundant fetches
5. Phase 5 (Pagination) — scalability, backward compatible
