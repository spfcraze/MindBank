package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/go-chi/chi/v5"

	"mindbank/internal/embedder"
)

type DebugHandler struct {
	pool      *pgxpool.Pool
	embClient *embedder.Client
}

func NewDebugHandler(pool *pgxpool.Pool, embClient *embedder.Client) *DebugHandler {
	return &DebugHandler{pool: pool, embClient: embClient}
}

// Health returns detailed health info including uptime and goroutines.
func (h *DebugHandler) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		respondJSON(w, 503, map[string]any{
			"status":   "error",
			"postgres": "disconnected",
			"ollama":   "unknown",
		})
		return
	}

	embErr := h.embClient.Health(r.Context())
	ollamaStatus := "connected"
	if embErr != nil {
		ollamaStatus = "unavailable"
	}

	respondJSON(w, 200, map[string]any{
		"status":           "ok",
		"postgres":         "connected",
		"ollama":           ollamaStatus,
		"embedding_model":  "nomic-embed-text",
		"version":          getLocalVersion(),
		"uptime_seconds":   time.Since(startTime).Seconds(),
		"goroutines":       runtime.NumGoroutine(),
		"go_version":       runtime.Version(),
		"postgres_version": getPostgresVersion(r.Context(), h.pool),
	})
}

// IntegrityScan checks database integrity and returns issues.
func (h *DebugHandler) IntegrityScan(w http.ResponseWriter, r *http.Request) {
	issues := []map[string]any{}

	// Check for orphaned edges (edges pointing to non-existent nodes)
	var orphanedEdges int
	err := h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM edges e
		WHERE NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.source_id AND n.valid_to IS NULL)
		OR NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.target_id AND n.valid_to IS NULL)
	`).Scan(&orphanedEdges)
	if err != nil {
		slog.Error("integrity scan: orphaned edges", "error", err)
	}
	if orphanedEdges > 0 {
		issues = append(issues, map[string]any{
			"type":        "orphaned_edges",
			"severity":    "warning",
			"count":       orphanedEdges,
			"description": fmt.Sprintf("%d edges reference deleted or non-existent nodes", orphanedEdges),
			"fixable":     true,
			"fix_type":    "remove_orphaned_edges",
		})
	}

	// Check for nodes without ACTIVE edges (isolated nodes). Match the heal's
	// definition (valid_to IS NULL) so the scan count and the heal's dry-run
	// count agree.
	var isolatedNodes int
	err = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM nodes n
		WHERE n.valid_to IS NULL
		AND NOT EXISTS (SELECT 1 FROM edges e WHERE (e.source_id = n.id OR e.target_id = n.id) AND e.valid_to IS NULL)
	`).Scan(&isolatedNodes)
	if err != nil {
		slog.Error("integrity scan: isolated nodes", "error", err)
	}
	if isolatedNodes > 0 {
		issues = append(issues, map[string]any{
			"type":        "isolated_nodes",
			"severity":    "warning",
			"count":       isolatedNodes,
			"description": fmt.Sprintf("%d nodes have no connections", isolatedNodes),
			"fixable":     true,
			"fix_type":    "link_orphans",
		})
	}

	// Check for duplicate edges
	var duplicateEdges int
	err = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM (
			SELECT source_id, target_id, edge_type, COUNT(*) as cnt
			FROM edges
			GROUP BY source_id, target_id, edge_type
			HAVING COUNT(*) > 1
		) t
	`).Scan(&duplicateEdges)
	if err != nil {
		slog.Error("integrity scan: duplicate edges", "error", err)
	}
	if duplicateEdges > 0 {
		issues = append(issues, map[string]any{
			"type":        "duplicate_edges",
			"severity":    "warning",
			"count":       duplicateEdges,
			"description": fmt.Sprintf("%d duplicate edge groups found", duplicateEdges),
			"fixable":     true,
			"fix_type":    "merge_duplicates",
		})
	}

	// Check nodes missing embeddings (embeddings live in node_embeddings;
	// nodes has no embedding column, so the old query errored on every scan
	// and this issue class was never reported)
	var missingEmbeddings int
	err = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM nodes n
		WHERE n.valid_to IS NULL
		  AND NOT EXISTS (SELECT 1 FROM node_embeddings ne WHERE ne.node_id = n.id)
	`).Scan(&missingEmbeddings)
	if err != nil {
		slog.Error("integrity scan: missing embeddings", "error", err)
	}
	if missingEmbeddings > 0 {
		issues = append(issues, map[string]any{
			"type":        "missing_embeddings",
			"severity":    "warning",
			"count":       missingEmbeddings,
			"description": fmt.Sprintf("%d nodes missing embeddings", missingEmbeddings),
			"fixable":     true,
			"fix_type":    "recalculate_embeddings",
		})
	}

	status := "healthy"
	if len(issues) > 0 {
		status = "warning"
	}

	respondJSON(w, 200, map[string]any{
		"overall_status": status,
		"issues":         issues,
		"total_issues":   len(issues),
	})
}

// Stats returns database statistics.
func (h *DebugHandler) Stats(w http.ResponseWriter, r *http.Request) {
	var nodeCount, edgeCount int
	h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`).Scan(&nodeCount)
	h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM edges`).Scan(&edgeCount)

	// Node types breakdown
	rows, err := h.pool.Query(r.Context(), `SELECT node_type, COUNT(*) FROM nodes WHERE valid_to IS NULL GROUP BY node_type`)
	if err != nil {
		respondError(w, 500, "failed to query node types")
		return
	}
	defer rows.Close()

	nodeTypes := map[string]int{}
	for rows.Next() {
		var nt string
		var c int
		rows.Scan(&nt, &c)
		nodeTypes[nt] = c
	}

	// Edge types breakdown
	rows2, err := h.pool.Query(r.Context(), `SELECT edge_type, COUNT(*) FROM edges GROUP BY edge_type`)
	if err != nil {
		respondError(w, 500, "failed to query edge types")
		return
	}
	defer rows2.Close()

	edgeTypes := map[string]int{}
	for rows2.Next() {
		var et string
		var c int
		rows2.Scan(&et, &c)
		edgeTypes[et] = c
	}

	respondJSON(w, 200, map[string]any{
		"nodes":      nodeCount,
		"edges":      edgeCount,
		"node_types": nodeTypes,
		"edge_types": edgeTypes,
	})
}

// CaptureHealth reports the health of the capture→recall pipeline: ingestion
// rate, extraction yield, embedding coverage, graph connectivity, and memory
// quality — the operational picture Tier 4 was missing.
func (h *DebugHandler) CaptureHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scalar := func(q string) int64 {
		var v int64
		_ = h.pool.QueryRow(ctx, q).Scan(&v)
		return v
	}
	groupCount := func(q string) map[string]int64 {
		m := map[string]int64{}
		rows, err := h.pool.Query(ctx, q)
		if err != nil {
			return m
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var c int64
			if rows.Scan(&k, &c) == nil {
				m[k] = c
			}
		}
		return m
	}

	// Ingestion
	sessionsTotal := scalar(`SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL AND node_type='session'`)
	sessions24h := scalar(`SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL AND node_type='session' AND created_at > now()-interval '24 hours'`)
	sessions7d := scalar(`SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL AND node_type='session' AND created_at > now()-interval '7 days'`)
	// Autocapture ledger (may not exist on very old DBs)
	autocapture := groupCount(`SELECT outcome, COUNT(*) FROM autocapture_files GROUP BY outcome`)

	// Extraction (knowledge = non-container nodes)
	knowledge := scalar(`SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL AND node_type NOT IN ('session','event')`)
	byType := groupCount(`SELECT node_type::text, COUNT(*) FROM nodes WHERE valid_to IS NULL AND node_type NOT IN ('session','event') GROUP BY node_type`)
	byEpistemic := groupCount(`SELECT coalesce(epistemic_label,'unknown'), COUNT(*) FROM nodes WHERE valid_to IS NULL AND node_type NOT IN ('session','event') GROUP BY epistemic_label`)
	avgPerSession := 0.0
	if sessionsTotal > 0 {
		avgPerSession = float64(knowledge) / float64(sessionsTotal)
	}

	// Embeddings
	embQueue := groupCount(`SELECT status, COUNT(*) FROM embedding_queue GROUP BY status`)
	missingEmb := scalar(`SELECT COUNT(*) FROM nodes n WHERE n.valid_to IS NULL AND NOT EXISTS (SELECT 1 FROM node_embeddings ne WHERE ne.node_id=n.id)`)
	totalNodes := scalar(`SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`)
	coverage := 100.0
	if totalNodes > 0 {
		coverage = 100.0 * float64(totalNodes-missingEmb) / float64(totalNodes)
	}

	// Graph connectivity
	edges := scalar(`SELECT COUNT(*) FROM edges WHERE valid_to IS NULL`)
	orphans := scalar(`SELECT COUNT(*) FROM nodes n WHERE n.valid_to IS NULL AND node_type NOT IN ('session','event')
		AND NOT EXISTS (SELECT 1 FROM edges e WHERE (e.source_id=n.id OR e.target_id=n.id) AND e.valid_to IS NULL)`)
	orphanPct := 0.0
	if knowledge > 0 {
		orphanPct = 100.0 * float64(orphans) / float64(knowledge)
	}
	avgDegree := 0.0
	if knowledge > 0 {
		avgDegree = 2.0 * float64(edges) / float64(knowledge)
	}

	// Timeline: knowledge nodes + sessions created per day, last 14 days
	type dayRow struct {
		Day      string `json:"day"`
		Nodes    int64  `json:"nodes"`
		Sessions int64  `json:"sessions"`
	}
	timeline := []dayRow{}
	trows, err := h.pool.Query(ctx, `
		SELECT to_char(d::date,'MM-DD') AS day,
		       COALESCE(SUM(CASE WHEN n.node_type NOT IN ('session','event') THEN 1 ELSE 0 END),0) AS nodes,
		       COALESCE(SUM(CASE WHEN n.node_type='session' THEN 1 ELSE 0 END),0) AS sessions
		FROM generate_series(now()::date - interval '13 days', now()::date, interval '1 day') d
		LEFT JOIN nodes n ON n.created_at::date = d::date AND n.valid_to IS NULL
		GROUP BY d ORDER BY d`)
	if err == nil {
		defer trows.Close()
		for trows.Next() {
			var dr dayRow
			if trows.Scan(&dr.Day, &dr.Nodes, &dr.Sessions) == nil {
				timeline = append(timeline, dr)
			}
		}
	}

	respondJSON(w, 200, map[string]any{
		"ingestion": map[string]any{
			"sessions_total":    sessionsTotal,
			"sessions_last_24h": sessions24h,
			"sessions_last_7d":  sessions7d,
			"autocapture":       autocapture,
		},
		"extraction": map[string]any{
			"knowledge_nodes":       knowledge,
			"avg_nodes_per_session": avgPerSession,
			"by_type":               byType,
			"by_epistemic":          byEpistemic,
		},
		"embeddings": map[string]any{
			"queue":            embQueue,
			"missing":          missingEmb,
			"coverage_pct":     coverage,
		},
		"graph": map[string]any{
			"knowledge_nodes": knowledge,
			"edges":           edges,
			"avg_degree":      avgDegree,
			"orphans":         orphans,
			"orphan_pct":      orphanPct,
		},
		"timeline": timeline,
	})
}

// Queue returns embedding queue status.
func (h *DebugHandler) Queue(w http.ResponseWriter, r *http.Request) {
	// This is a simplified version - in production you'd query the actual queue
	respondJSON(w, 200, map[string]any{
		"queued":  0,
		"active":  0,
		"errors":  0,
		"status":  "idle",
	})
}

// Heal runs self-healing operations.
func (h *DebugHandler) Heal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fixes  []string `json:"fixes"`
		DryRun bool     `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid request")
		return
	}

	results := []map[string]any{}
	// add records a normalized result. `affected` is the row count (rows that
	// would be / were changed) — the frontend reads exactly this field, so
	// every branch must report it, or the UI shows "undefined".
	add := func(fix, status string, affected int64, extra map[string]any) {
		res := map[string]any{"fix": fix, "status": status, "affected": affected}
		for k, v := range extra {
			res[k] = v
		}
		results = append(results, res)
	}

	for _, fix := range req.Fixes {
		switch fix {
		case "remove_orphaned_edges":
			// Edges pointing at a node that no longer exists as a current version.
			const cond = `
				WHERE NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.source_id AND n.valid_to IS NULL)
				   OR NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.target_id AND n.valid_to IS NULL)`
			if req.DryRun {
				var count int64
				h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM edges e`+cond).Scan(&count)
				add(fix, "dry_run", count, nil)
			} else {
				res, err := h.pool.Exec(r.Context(), `DELETE FROM edges e`+cond)
				if err != nil {
					add(fix, "error", 0, map[string]any{"error": err.Error()})
				} else {
					add(fix, "ok", res.RowsAffected(), nil)
				}
			}

		case "link_orphans":
			// An orphan = current node with no ACTIVE edges. Attach each to the
			// highest-importance connected node in its OWN workspace+namespace,
			// so links are meaningful and never cross tenant boundaries. Orphans
			// in a namespace with no connected node are left alone (correct).
			const orphanNoActive = `NOT EXISTS (SELECT 1 FROM edges e WHERE (e.source_id = o.id OR e.target_id = o.id) AND e.valid_to IS NULL)`
			const hubExists = `EXISTS (
				SELECT 1 FROM nodes n
				WHERE n.valid_to IS NULL AND n.workspace_name = o.workspace_name AND n.namespace = o.namespace AND n.id <> o.id
				  AND EXISTS (SELECT 1 FROM edges e WHERE (e.source_id = n.id OR e.target_id = n.id) AND e.valid_to IS NULL))`
			if req.DryRun {
				var count int64
				h.pool.QueryRow(r.Context(), `
					SELECT COUNT(*) FROM nodes o
					WHERE o.valid_to IS NULL AND `+orphanNoActive+` AND `+hubExists).Scan(&count)
				add(fix, "dry_run", count, nil)
			} else {
				res, err := h.pool.Exec(r.Context(), `
					INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight)
					SELECT o.workspace_name, o.id, hub.id, 'relates_to', 0.2
					FROM nodes o
					CROSS JOIN LATERAL (
						SELECT n.id FROM nodes n
						WHERE n.valid_to IS NULL AND n.workspace_name = o.workspace_name AND n.namespace = o.namespace AND n.id <> o.id
						  AND EXISTS (SELECT 1 FROM edges e WHERE (e.source_id = n.id OR e.target_id = n.id) AND e.valid_to IS NULL)
						ORDER BY n.importance DESC, n.access_count DESC
						LIMIT 1
					) hub
					WHERE o.valid_to IS NULL AND `+orphanNoActive+`
					ON CONFLICT (source_id, target_id, edge_type) DO NOTHING`)
				if err != nil {
					add(fix, "error", 0, map[string]any{"error": err.Error()})
				} else {
					add(fix, "ok", res.RowsAffected(), nil)
				}
			}

		case "merge_duplicates":
			const dupCond = `WHERE ctid NOT IN (SELECT MIN(ctid) FROM edges GROUP BY source_id, target_id, edge_type)`
			if req.DryRun {
				var count int64
				h.pool.QueryRow(r.Context(), `
					SELECT COALESCE(SUM(cnt-1),0) FROM (
						SELECT COUNT(*) cnt FROM edges GROUP BY source_id, target_id, edge_type HAVING COUNT(*) > 1
					) t`).Scan(&count)
				add(fix, "dry_run", count, nil)
			} else {
				res, err := h.pool.Exec(r.Context(), `DELETE FROM edges `+dupCond)
				if err != nil {
					add(fix, "error", 0, map[string]any{"error": err.Error()})
				} else {
					add(fix, "ok", res.RowsAffected(), nil)
				}
			}

		case "recalculate_embeddings":
			// Enqueue every current node missing an embedding; the embed worker
			// picks them up. (Previously a no-op that pointed at the unrelated
			// importance-recalc endpoint.)
			const missing = `
				FROM nodes n
				WHERE n.valid_to IS NULL
				  AND NOT EXISTS (SELECT 1 FROM node_embeddings ne WHERE ne.node_id = n.id)`
			if req.DryRun {
				var count int64
				h.pool.QueryRow(r.Context(), `SELECT COUNT(*) `+missing).Scan(&count)
				add(fix, "dry_run", count, nil)
			} else {
				res, err := h.pool.Exec(r.Context(), `
					INSERT INTO embedding_queue (source_type, source_id)
					SELECT 'node', n.id::text `+missing+`
					ON CONFLICT (source_type, source_id) DO UPDATE
					SET status = 'pending', attempts = 0, last_error = NULL
					WHERE embedding_queue.status = 'done'`)
				if err != nil {
					add(fix, "error", 0, map[string]any{"error": err.Error()})
				} else {
					add(fix, "ok", res.RowsAffected(), map[string]any{"note": "queued for re-embedding"})
				}
			}

		default:
			add(fix, "unknown", 0, map[string]any{"error": "unknown fix type"})
		}
	}

	respondJSON(w, 200, map[string]any{
		"dry_run": req.DryRun,
		"applied": results, // frontend reads .applied[].{fix,affected}
		"results": results, // kept for any other consumers
	})
}

// HealLog returns heal operation history.
func (h *DebugHandler) HealLog(w http.ResponseWriter, r *http.Request) {
	// Simplified - no persistent heal log table yet
	respondJSON(w, 200, map[string]any{
		"entries": []map[string]any{},
		"total":   0,
	})
}

var startTime = time.Now()

// getPostgresVersion queries the PostgreSQL server version.
func getPostgresVersion(ctx context.Context, pool *pgxpool.Pool) string {
	var version string
	err := pool.QueryRow(ctx, `SELECT version()`).Scan(&version)
	if err != nil {
		return "unknown"
	}
	// Extract version number from string like "PostgreSQL 16.2 on x86_64..."
	parts := strings.Split(version, " ")
	if len(parts) >= 2 {
		return parts[1]
	}
	return version
}

// RegisterDebugRoutes adds debug endpoints to the router.
func RegisterDebugRoutes(r chi.Router, dh *DebugHandler) {
	r.Route("/debug", func(r chi.Router) {
		r.Get("/health", dh.Health)
		r.Get("/integrity", dh.IntegrityScan)
		r.Get("/stats", dh.Stats)
		r.Get("/capture-health", dh.CaptureHealth)
		r.Get("/queue", dh.Queue)
		r.Post("/heal", dh.Heal)
		r.Get("/heal-log", dh.HealLog)
	})
}
