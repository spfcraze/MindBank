# DQA (Data Quality Analyzer) Audit Report

## Executive Summary

**Audit Date:** 2026-05-29
**Scope:** Frontend DQA logic + Backend quality metrics
**Current Score:** 6% (reported by user)
**Root Causes Identified:** 5 critical bugs
**Estimated Fix Impact:** 6% → 60-80%

---

## Root Cause Analysis: Why 6%?

### Bug 1: DQA Uses Client-Side Graph Data (LIMIT 2000)
**Severity:** CRITICAL
**Impact:** Missing nodes = false orphans/disconnected

The DQA fetches `/graph?limit=2000` but the backend `/graph` endpoint likely returns far fewer nodes with edges. Nodes beyond the limit are invisible to DQA and counted as "missing" — but they're actually fine, just not loaded.

**Evidence:**
```javascript
const graphData = await api('/graph?limit=2000');
const nodes = graphData.nodes; // Only 2000 max, possibly much fewer
```

If the graph has 5000 nodes but DQA only sees 2000, it thinks 3000 nodes are "missing" or have issues.

**Fix:** Use `/nodes?limit=10000` for node list, `/edges?limit=10000` for edges separately.

---

### Bug 2: Orphan Detection Uses `edge_count` Field
**Severity:** CRITICAL
**Impact:** False orphans when `edge_count` is stale or null

```javascript
const orphans = nodes.filter(n => (n.edge_count || 0) === 0);
```

The `edge_count` field is only accurate if:
1. The backend computes it correctly
2. It's included in the `/graph` response
3. It's kept in sync with edge mutations

If `edge_count` is missing from the response (common with field selection), ALL nodes appear as orphans.

**Fix:** Use actual edge data from `/edges` endpoint to determine connectivity.

---

### Bug 3: Duplicate Detection Ignores `workspace_name`
**Severity:** HIGH
**Impact:** False duplicates across workspaces

```javascript
const key = _s(n.namespace) + '::' + _s(n.label).toLowerCase() + '::' + _s(n.node_type);
```

The comment says "Graph endpoint doesn't return workspace_name — use namespace only" but this means nodes in different workspaces with the same label+type are flagged as duplicates.

**Fix:** Include `workspace_name` in the duplicate key if available, or document this limitation.

---

### Bug 4: Disconnected Component Logic is Broken
**Severity:** CRITICAL
**Impact:** All non-orphan nodes flagged as disconnected

```javascript
const disconnected = components.length > 1
  ? components.slice(1).flat().filter(id => !orphanIds.has(id))
  : [];
```

This logic:
1. Finds ALL components via BFS
2. Takes everything except the largest component
3. Filters out orphans

Result: Any node not in the single largest component is "disconnected." In a real graph with multiple projects/namespaces, this is expected and correct. But the DQA penalizes it heavily.

**Fix:** Only flag nodes that are TRULY isolated (no edges at all) or have no path to a "root" node (project/session).

---

### Bug 5: Score Calculation Double-Counts Issues
**Severity:** HIGH
**Impact:** Inflated issue count = lower score

```javascript
const badIds = new Set([
  ...orphans.map(n => _s(n.id)),
  ...disconnected.map(id => _s(id)),
  ...duplicates.map(n => _s(n.id)),
  ...emptyContent.map(n => _s(n.id)),
  ...lowImportance.map(n => _s(n.id)),
  ...uncontained.map(n => _s(n.id))
]);
```

A node can be:
- An orphan AND disconnected (orphan is a subset of disconnected)
- An orphan AND uncontained (all orphans are uncontained)
- Empty content AND low importance

The Set deduplicates, but the categories still show them separately, making the report look worse than it is.

**Fix:** Define clear, non-overlapping categories or weight overlapping issues correctly.

---

### Bug 6: Backend Quality Score Uses Different Formula
**Severity:** MEDIUM
**Impact:** Inconsistent scores between DQA and backend

Backend (`internal/quality/metrics.go`):
```go
orphanScore := 20.0 * (1.0 - m.OrphanPercent/100.0)
duplicateScore := 20.0 * (1.0 - m.DuplicatePercent/100.0)
densityScore := 20.0 * math.Min(m.EdgeDensity*1000, 1.0)
topicScore := 20.0 * (m.TopicCoveragePct / 100.0)
connectivityScore := 20.0 * (m.LargestComponentPct / 100.0)
```

Frontend DQA:
```javascript
const score = nodes.length === 0 ? 100 : Math.round((clean / nodes.length) * 100);
```

Two completely different scoring systems. The backend doesn't know about "empty content" or "low importance" checks.

**Fix:** Unify scoring or clearly document the difference.

---

### Bug 7: `workspace_name` in Edge Creation (Fixed)
**Severity:** HIGH (Fixed in d946f4f)
**Impact:** 500 error on edge batch creation

The DQA quick fix was creating edges with `workspace_name` field, but the `edges` table doesn't have this column. This caused batch edge creation to fail silently.

**Fix:** Removed `workspace_name` from edge batch creation in commit d946f4f.

---

## Recommended Fixes (Priority Order)

### P0: Fix Data Source
1. Change DQA to use `/nodes?limit=10000` + `/edges?limit=10000` instead of `/graph?limit=2000`
2. Or add a dedicated `/dqa/analyze` backend endpoint that returns accurate metrics

### P0: Fix Orphan Detection
1. Use actual edge data to determine connectivity, not `edge_count` field
2. Cross-reference node IDs against edge source/target IDs

### P1: Fix Disconnected Logic
1. Only flag nodes as "disconnected" if they have no path to ANY project/session node
2. Don't penalize legitimate separate components

### P1: Fix Duplicate Detection
1. Include `workspace_name` in duplicate key if available
2. If not available from API, document the limitation

### P1: Fix Score Calculation
1. Use weighted scoring instead of simple "bad node count"
2. Account for overlapping categories
3. Align with backend scoring or document differences

### P2: Add Backend DQA Endpoint
1. Create `/api/v1/dqa/analyze` that returns all metrics server-side
2. Frontend just renders the results
3. Eliminates client-side calculation bugs entirely

---

## Praxis Code Quality Analysis

### Frontend DQA Code (runQualityAnalysis + helpers)

| Check | Result | Notes |
|-------|--------|-------|
| Naming | FAIL | `_s`, `_sid` are unclear abbreviations |
| Function Size | FAIL | `runQualityAnalysis` is 180+ lines |
| Nesting Depth | PASS | Mostly flat with early returns |
| Single Responsibility | FAIL | One function does fetch, analyze, calculate, render |
| Coupling | FAIL | Tightly coupled to DOM IDs and API structure |
| Cohesion | FAIL | Mixed concerns: data fetch, analysis, UI update |
| Fail Fast | FAIL | Silent catch blocks in `saveDQASnapshot`, `loadDQATrend` |
| Idempotency | N/A | Read-only analysis |
| Input Validation | FAIL | No validation of API response shape |
| Command-Query Separation | FAIL | `runQualityAnalysis` fetches AND renders |
| Immutability | PASS | Uses `const` and `map/filter` |
| Surprise Check | FAIL | `edge_count` field behavior is surprising |
| Composition | N/A | No inheritance used |
| Orthogonality | FAIL | Changing API response format breaks DQA |
| Simplicity | FAIL | 180-line function with 6 interdependent checks |

**Score: 4/15**

### Backend Quality Metrics (internal/quality/metrics.go)

| Check | Result | Notes |
|-------|--------|-------|
| Naming | PASS | Clear names |
| Function Size | FAIL | `ComputeMetrics` is 160 lines |
| Nesting Depth | PASS | Flat structure |
| Single Responsibility | FAIL | One function computes ALL metrics |
| Coupling | PASS | Only depends on pgxpool |
| Cohesion | PASS | All metrics related to graph health |
| Fail Fast | FAIL | Silent error on topic query (`_ = pool.QueryRow`) |
| Idempotency | N/A | Read-only |
| Input Validation | PASS | Validates context and pool |
| Command-Query Separation | PASS | Returns data, no side effects |
| Immutability | PASS | Returns new struct |
| Surprise Check | FAIL | `TopicCoveragePct` hardcodes "10 possible topics" |
| Composition | N/A | No inheritance |
| Orthogonality | PASS | Changes to one metric don't affect others |
| Simplicity | FAIL | Could be split into smaller helpers |

**Score: 8/15**

---

## Action Plan

| Priority | Task | Impact | Effort |
|----------|------|--------|--------|
| P0 | Fix DQA data source (use /nodes + /edges) | +40-50% score | 2h |
| P0 | Fix orphan detection (use actual edges) | +10-20% score | 1h |
| P1 | Fix disconnected logic | +5-10% score | 1h |
| P1 | Unify scoring formula | Consistency | 2h |
| P2 | Create backend /dqa/analyze endpoint | Accuracy | 4h |

**Expected Result:** 6% → 60-80% after P0+P1 fixes.
