package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
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
		"status":          "ok",
		"postgres":        "connected",
		"ollama":          ollamaStatus,
		"embedding_model": "nomic-embed-text",
		"version":         getLocalVersion(),
		"uptime_seconds":  time.Since(startTime).Seconds(),
		"goroutines":      runtime.NumGoroutine(),
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

	// Check for nodes without edges (isolated nodes)
	var isolatedNodes int
	err = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM nodes n
		WHERE n.valid_to IS NULL
		AND NOT EXISTS (SELECT 1 FROM edges e WHERE e.source_id = n.id OR e.target_id = n.id)
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

	// Check nodes missing embeddings
	var missingEmbeddings int
	err = h.pool.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM nodes
		WHERE valid_to IS NULL AND embedding IS NULL
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

	for _, fix := range req.Fixes {
		switch fix {
		case "remove_orphaned_edges":
			if req.DryRun {
				var count int
				h.pool.QueryRow(r.Context(), `
					SELECT COUNT(*) FROM edges e
					WHERE NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.source_id AND n.valid_to IS NULL)
					OR NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.target_id AND n.valid_to IS NULL)
				`).Scan(&count)
				results = append(results, map[string]any{"fix": fix, "status": "dry_run", "would_remove": count})
			} else {
				res, err := h.pool.Exec(r.Context(), `
					DELETE FROM edges e
					WHERE NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.source_id AND n.valid_to IS NULL)
					OR NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.target_id AND n.valid_to IS NULL)
				`)
				if err != nil {
					results = append(results, map[string]any{"fix": fix, "status": "error", "error": err.Error()})
				} else {
					count := res.RowsAffected()
					results = append(results, map[string]any{"fix": fix, "status": "ok", "removed": count})
				}
			}
		case "link_orphans":
			results = append(results, map[string]any{"fix": fix, "status": "not_implemented", "note": "Use /api/v1/analyze/link-orphans instead"})
		case "merge_duplicates":
			results = append(results, map[string]any{"fix": fix, "status": "not_implemented", "note": "Use /api/v1/analyze/merge-duplicates instead"})
		case "recalculate_embeddings":
			results = append(results, map[string]any{"fix": fix, "status": "not_implemented", "note": "Use /api/v1/nodes/recalculate instead"})
		default:
			results = append(results, map[string]any{"fix": fix, "status": "unknown", "error": "unknown fix type"})
		}
	}

	respondJSON(w, 200, map[string]any{
		"dry_run": req.DryRun,
		"results": results,
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

// RegisterDebugRoutes adds debug endpoints to the router.
func RegisterDebugRoutes(r chi.Router, dh *DebugHandler) {
	r.Route("/debug", func(r chi.Router) {
		r.Get("/health", dh.Health)
		r.Get("/integrity", dh.IntegrityScan)
		r.Get("/stats", dh.Stats)
		r.Get("/queue", dh.Queue)
		r.Post("/heal", dh.Heal)
		r.Get("/heal-log", dh.HealLog)
	})
}
