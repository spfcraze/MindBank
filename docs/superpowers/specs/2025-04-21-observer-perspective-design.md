# MindBank Observer Perspective: Query Domain of Dependence

**Date:** 2025-04-21
**Status:** Draft — pending gap-analysis
**Author:** Hermes Agent
**Source Paper:** Zaki, T.A. (2025). "Turbulence from an Observer Perspective." *Annu. Rev. Fluid Mech.* 57:311–334.

---

## 1. Problem Statement

MindBank is a forward-evolving knowledge graph: memories are added, linked, and queried by traversing outward from nodes. The Zaki (2025) review on turbulence data assimilation introduces a dual perspective — starting from an observation (a query, a user question, a detected knowledge gap) and rigorously tracing its dependence on prior states of the system.

**The goal:** enable MindBank to answer not just "what do I know?" but "why do I know this?" and "what is the minimal set of prior memories needed to reconstruct this understanding?"

---

## 2. Concept Mapping

| Turbulence Concept | MindBank Analog |
|---|---|
| Measurement (wall shear stress sensor) | **Query** — a search string, an `ask` request, or a specific node |
| Domain of Dependence | **Causal Support Set** — minimal subgraph of precursor nodes |
| Adjoint evolution (backward-in-time) | **Backward graph traversal** — upstream via `depends_on`, `learned_from`, `decided_by`, `produced` |
| Forward evolution | **Forward graph traversal** — downstream via `contains`, `relates_to`, `supports`, `temporal_next` |
| Chaos / butterfly effect | **Graph sparsity** — beyond a critical hop depth, nodes are "invisible" |
| Critical resolution threshold | **Critical hop depth** — minimum depth to capture 90% of causal influence |
| POD modes (most influential structures) | **Influence modes** — ranked precursor nodes by causal weight |
| Hessian / cost-function landscape | **Confidence landscape** — sensitivity to missing/contradictory precursors |

---

## 3. Architecture

### 3.1 New API Endpoints

#### `POST /api/v1/analyze/dependence`
Given a `node_id` or `query`, compute its domain of dependence.

**Request:**
```json
{
  "node_id": "uuid-of-some-node",
  "query": "optional text query instead of node_id",
  "namespace": "my-project",
  "max_depth": 5,
  "edge_types": ["depends_on", "learned_from", "decided_by", "produced", "supports"],
  "min_weight": 0.1
}
```

**Response:**
```json
{
  "seed": { "id": "...", "label": "...", "node_type": "..." },
  "dependence_graph": { "nodes": [...], "edges": [...] },
  "critical_depth": 3,
  "coverage": 0.87,
  "influence_modes": [
    {"node_id": "...", "label": "...", "influence_score": 0.92, "depth": 1}
  ],
  "blind_spots": [
    {"description": "No precursor found for contradiction edge from X", "severity": "medium"}
  ]
}
```

**Algorithm:**
1. If `node_id` given → start there. If `query` given → run hybrid search, take top result as seed.
2. BFS backward through specified `edge_types` up to `max_depth`.
3. Track cumulative edge weights; prune paths where cumulative weight < `min_weight`.
4. Compute `critical_depth`: depth at which 90% of total influence is captured.
5. Compute `coverage`: fraction of seed's immediate edge types that have upstream precursors.
6. Rank precursors by `influence_score = sum(edge_weights_along_path * importance_decay^depth)`.
7. Identify `blind_spots`: missing edge types, zero-weight paths, unresolved contradictions.

#### `POST /api/v1/analyze/synchronize`
Inject a new observation and propagate its influence through the graph.

**Request:**
```json
{
  "node_id": "newly-created-node-uuid",
  "namespace": "my-project",
  "propagate_depth": 3,
  "resolve_contradictions": true
}
```

**Response:**
```json
{
  "affected_nodes": 12,
  "confidence_updates": [
    {"node_id": "...", "old_confidence": 0.45, "new_confidence": 0.72}
  ],
  "resolved_contradictions": [
    {"node_id": "...", "resolution": "superseded_by_new_evidence"}
  ],
  "propagation_graph": { "nodes": [...], "edges": [...] }
}
```

**Algorithm:**
1. Run forward BFS from `node_id` through all edge types up to `propagate_depth`.
2. For each reached node: recompute confidence using the existing confidence formula, but with the new node counted as an additional access + connectivity boost.
3. If `resolve_contradictions`: find nodes connected via `contradicts` edges to the new node; mark older versions as resolved if the new node has higher composite score.

#### `GET /api/v1/analyze/observability`
Compute what fraction of a namespace is "observable" from a set of seed nodes.

**Query params:** `namespace`, `seed_node_ids` (comma-separated)

**Response:**
```json
{
  "namespace": "my-project",
  "seed_count": 3,
  "total_nodes": 150,
  "observable_nodes": 89,
  "observability_ratio": 0.593,
  "unobservable_nodes": [...],
  "coverage_by_type": {"fact": 0.7, "decision": 0.5, "advice": 0.3}
}
```

---

### 3.2 Data Model Changes

**Minimal change:** one new cache table.

```sql
CREATE TABLE IF NOT EXISTS dependence_cache (
    seed_node_id UUID NOT NULL REFERENCES nodes(id),
    params_hash TEXT NOT NULL,
    critical_depth INT,
    coverage FLOAT,
    influence_modes JSONB,
    computed_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (seed_node_id, params_hash)
);
```

No changes to `nodes`, `edges`, or existing indexes.

---

### 3.3 Frontend Integration

New dashboard section: **"Observer"** or **"Causal Trace"**
- Visualize dependence graph (subgraph of full graph, same force-directed renderer)
- Critical depth marker on a depth-vs-influence chart
- Heatmap of influence scores on nodes
- Blind spots list with severity badges
- Synchronize button on node detail view

---

## 4. Testing Strategy

1. **Unit tests** for BFS traversal, influence scoring, critical depth calculation
2. **Integration tests** on synthetic graph with known causal structure
3. **Before/after benchmarks** on real namespace: measure query result relevance with and without dependence-aware search
4. **Darwin evaluation:** Run 10 representative queries; compare result relevance scores; only ship if mean relevance improves by ≥10%

---

## 5. Success Criteria

- `/analyze/dependence` returns in <50ms for graphs up to 1000 nodes
- `/analyze/synchronize` updates confidence for ≤100 nodes in <100ms
- Darwin benchmark shows ≥10% improvement in query result relevance when using dependence-aware expansion
- Zero regressions in existing analyze endpoints
