package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NodeHandler struct {
	repo *repository.NodeRepo
	pool *pgxpool.Pool
}

func (h *NodeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.NodeCreate
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request body: "+err.Error())
		return
	}
	if req.Label == "" {
		respondError(w, 400, "label is required")
		return
	}
	if len(req.Label) > 500 {
		respondError(w, 400, "label too long (max 500 chars)")
		return
	}
	if req.NodeType == "" {
		respondError(w, 400, "node_type is required")
		return
	}
	if !req.NodeType.IsValid() {
		respondError(w, 400, "invalid node_type: must be one of "+strings.Join(nodeTypeList(), ", "))
		return
	}
	if len(req.Content) > 50000 {
		respondError(w, 400, "content too long (max 50KB)")
		return
	}
	if len(req.Summary) > 1000 {
		respondError(w, 400, "summary too long (max 1000 chars)")
		return
	}

	node, err := h.repo.Create(r.Context(), req)
	if err != nil {
		slog.Error("create node", "error", err)
		respondError(w, 500, "failed to create node")
		return
	}

	// Enqueue for embedding (best-effort, non-blocking)
	_ = embedder.EnqueueNode(r.Context(), h.pool, node.ID)

	respondJSON(w, 201, node)
}

func (h *NodeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	node, err := h.repo.Get(r.Context(), id)
	if err != nil {
		slog.Error("get node", "error", err)
		respondError(w, 500, "failed to get node")
		return
	}
	if node == nil {
		respondError(w, 404, "node not found")
		return
	}
	respondJSON(w, 200, node)
}

func (h *NodeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req models.NodeUpdate
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request body: "+err.Error())
		return
	}

	node, err := h.repo.Update(r.Context(), id, req)
	if err != nil {
		slog.Error("update node", "error", err)
		respondError(w, 500, "failed to update node")
		return
	}
	if node == nil {
		respondError(w, 404, "node not found")
		return
	}
	respondJSON(w, 200, node)
}

func (h *NodeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := h.repo.Delete(r.Context(), id)
	if err != nil {
		slog.Error("delete node", "error", err)
		respondError(w, 500, "failed to delete node")
		return
	}
	if !ok {
		respondError(w, 404, "node not found")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "deleted"})
}
// List handles GET /api/v1/nodes — list current nodes with optional filters.
func (h *NodeHandler) List(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspace")
	namespace := r.URL.Query().Get("namespace")
	nodeType := models.NodeType(r.URL.Query().Get("type"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	// If requesting count only (limit=0), return just the count
	if r.URL.Query().Get("count") == "true" {
		var count int
		err := h.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`).Scan(&count)
		if err != nil {
			respondError(w, 500, "count failed")
			return
		}
		respondJSON(w, 200, map[string]int{"count": count})
		return
	}

	nodes, err := h.repo.List(r.Context(), workspace, namespace, nodeType, limit, offset)
	if err != nil {
		slog.Error("list nodes", "error", err)
		respondError(w, 500, "failed to list nodes")
		return
	}
	if nodes == nil {
		nodes = []models.Node{}
	}
	respondJSON(w, 200, nodes)
}

func (h *NodeHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	history, err := h.repo.GetHistory(r.Context(), id)
	if err != nil {
		slog.Error("get node history", "error", err)
		respondError(w, 500, "failed to get history")
		return
	}
	if history == nil {
		history = []models.NodeHistoryEntry{}
	}
	respondJSON(w, 200, history)
}

func (h *NodeHandler) Dedup(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	dryRun := r.URL.Query().Get("dry_run") == "true"

	rows, err := h.pool.Query(r.Context(), `
		SELECT label, node_type, namespace, array_agg(id ORDER BY created_at DESC) AS ids, COUNT(*) as cnt
		FROM nodes
		WHERE valid_to IS NULL AND ($1 = '' OR namespace = $1)
		GROUP BY workspace_name, namespace, label, node_type
		HAVING COUNT(*) > 1
		ORDER BY COUNT(*) DESC
	`, namespace)
	if err != nil {
		slog.Error("dedup query", "error", err)
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
			slog.Error("dedup scan", "error", err)
			continue
		}
		g.IDs = idArray
		totalDupes += g.Count - 1
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

	deleted := 0
	for _, g := range groups {
		for _, id := range g.IDs[1:] {
			ok, err := h.repo.Delete(r.Context(), id)
			if err != nil {
				slog.Error("dedup delete", "id", id, "error", err)
				continue
			}
			if ok {
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

// Recalculate triggers importance score recalculation for all nodes.
func (h *NodeHandler) Recalculate(w http.ResponseWriter, r *http.Request) {
	rows, err := h.repo.RecalculateImportance(r.Context())
	if err != nil {
		slog.Error("recalculate importance", "error", err)
		respondError(w, 500, "recalculation failed: "+err.Error())
		return
	}
	slog.Info("importance recalculated", "rows_updated", rows)
	respondJSON(w, 200, map[string]any{
		"status":       "ok",
		"rows_updated": rows,
	})
}

// nodeTypeList returns a formatted list of valid node types for error messages.
func nodeTypeList() []string {
	types := models.AllNodeTypes()
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}
