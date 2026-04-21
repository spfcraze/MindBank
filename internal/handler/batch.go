package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"mindbank/internal/models"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
)

// BatchHandler handles batch operations for nodes and edges.
type BatchHandler struct {
	nodeRepo *repository.NodeRepo
	edgeRepo *repository.EdgeRepo
}

// NewBatchHandler creates a new batch handler.
func NewBatchHandler(nr *repository.NodeRepo, er *repository.EdgeRepo) *BatchHandler {
	return &BatchHandler{nodeRepo: nr, edgeRepo: er}
}

// BatchCreateNodes handles POST /api/v1/nodes/batch — create multiple nodes at once.
func (h *BatchHandler) BatchCreateNodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nodes []models.NodeCreate `json:"nodes"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if len(req.Nodes) == 0 {
		respondError(w, 400, "nodes array is required")
		return
	}
	if len(req.Nodes) > 100 {
		respondError(w, 400, "max 100 nodes per batch")
		return
	}

	results := make([]*models.Node, 0, len(req.Nodes))
	errors := make([]string, 0)

	for i, nc := range req.Nodes {
		if nc.Label == "" {
			errors = append(errors, "node "+strconv.Itoa(i)+": label is required")
			continue
		}
		if len(nc.Label) > 500 {
			errors = append(errors, "node "+strconv.Itoa(i)+": label too long (max 500 chars)")
			continue
		}
		if nc.NodeType == "" {
			errors = append(errors, "node "+strconv.Itoa(i)+": node_type is required")
			continue
		}
		if !nc.NodeType.IsValid() {
			errors = append(errors, "node "+strconv.Itoa(i)+": invalid node_type")
			continue
		}
		if len(nc.Content) > 50000 {
			errors = append(errors, "node "+strconv.Itoa(i)+": content too long (max 50KB)")
			continue
		}
		if len(nc.Summary) > 1000 {
			errors = append(errors, "node "+strconv.Itoa(i)+": summary too long (max 1000 chars)")
			continue
		}
		node, err := h.nodeRepo.Create(r.Context(), nc)
		if err != nil {
			slog.Error("batch create node", "index", i, "error", err)
			errors = append(errors, "node "+strconv.Itoa(i)+": "+err.Error())
			continue
		}
		results = append(results, node)
	}

	respondJSON(w, 200, map[string]any{
		"created": results,
		"errors":  errors,
		"total":   len(results),
	})
}

// BatchCreateEdges handles POST /api/v1/edges/batch — create multiple edges at once.
func (h *BatchHandler) BatchCreateEdges(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Edges []models.EdgeCreate `json:"edges"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if len(req.Edges) == 0 {
		respondError(w, 400, "edges array is required")
		return
	}
	if len(req.Edges) > 200 {
		respondError(w, 400, "max 200 edges per batch")
		return
	}

	results := make([]*models.Edge, 0, len(req.Edges))
	errors := make([]string, 0)

	for i, ec := range req.Edges {
		edge, err := h.edgeRepo.Create(r.Context(), ec)
		if err != nil {
			// Skip duplicates silently (edges may already exist)
			slog.Debug("batch create edge", "index", i, "error", err)
			continue
		}
		results = append(results, edge)
	}

	respondJSON(w, 200, map[string]any{
		"created": results,
		"errors":  errors,
		"total":   len(results),
	})
}

// CleanupOrphanEdges handles POST /api/v1/edges/cleanup — delete edges referencing non-existent nodes.
func (h *BatchHandler) CleanupOrphanEdges(w http.ResponseWriter, r *http.Request) {
	count, err := h.edgeRepo.DeleteOrphaned(r.Context())
	if err != nil {
		slog.Error("cleanup orphan edges", "error", err)
		respondError(w, 500, "cleanup failed")
		return
	}
	respondJSON(w, 200, map[string]any{"deleted": count})
}

// AutoConnect handles POST /api/v1/nodes/auto-connect — create semantic edges for existing nodes.
func (h *BatchHandler) AutoConnect(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	workspace := "hermes"

	nodes, err := h.nodeRepo.List(r.Context(), workspace, ns, "", 500, 0)
	if err != nil {
		respondError(w, 500, "failed to list nodes")
		return
	}

	// Build namespace-grouped node index
	nsNodes := make(map[string][]models.Node)
	for _, n := range nodes {
		nsNodes[n.Namespace] = append(nsNodes[n.Namespace], n)
	}

	edgeRules := map[models.NodeType]map[models.NodeType]models.EdgeType{
		models.NodeDecision: {models.NodeProject: models.EdgeDecidedBy, models.NodeProblem: models.EdgeContradicts},
		models.NodeProblem:  {models.NodeDecision: models.EdgeContradicts, models.NodeAdvice: models.EdgeSupports},
		models.NodeAdvice:   {models.NodeDecision: models.EdgeSupports, models.NodeProblem: models.EdgeSupports},
	}

	created := 0
	for _, nsList := range nsNodes {
		for i, n1 := range nsList {
			for j, n2 := range nsList {
				if i >= j {
					continue
				}
				// Check if edge already exists
				existingEdges, _ := h.edgeRepo.GetByNode(r.Context(), n1.ID)
				already := false
				for _, e := range existingEdges {
					if (e.SourceID == n1.ID && e.TargetID == n2.ID) ||
						(e.SourceID == n2.ID && e.TargetID == n1.ID) {
						already = true
						break
					}
				}
				if already {
					continue
				}

				// Determine edge type from rules
				edgeType := models.EdgeRelatesTo
				if rules, ok := edgeRules[n1.NodeType]; ok {
					if et, ok := rules[n2.NodeType]; ok {
						edgeType = et
					}
				}
				if rules, ok := edgeRules[n2.NodeType]; ok {
					if et, ok := rules[n1.NodeType]; ok {
						edgeType = et
					}
				}

				w := float32(1.0)
				_, err := h.edgeRepo.Create(r.Context(), models.EdgeCreate{
					SourceID: n1.ID,
					TargetID: n2.ID,
					EdgeType: edgeType,
					Weight:   &w,
				})
				if err == nil {
					created++
				}
			}
		}
	}

	respondJSON(w, 200, map[string]any{
		"edges_created": created,
		"nodes_processed": len(nodes),
	})
}

// PurgeSoftDeleted handles DELETE /api/v1/nodes/purge — hard-delete old temporal versions.
func (h *BatchHandler) PurgeSoftDeleted(w http.ResponseWriter, r *http.Request) {
	days := 30 // default: purge versions older than 30 days
	if d := r.URL.Query().Get("older_than_days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}
	if days < 1 {
		days = 1
	}

	count, err := h.nodeRepo.PurgeOldVersions(r.Context(), days)
	if err != nil {
		slog.Error("purge old versions", "error", err)
		respondError(w, 500, "purge failed: "+err.Error())
		return
	}

	respondJSON(w, 200, map[string]any{
		"purged": count,
		"days":   days,
	})
}

// Export handles GET /api/v1/export — export graph as JSON.
func (h *BatchHandler) Export(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")

	nodes, err := h.nodeRepo.List(r.Context(), "", ns, "", 10000, 0)
	if err != nil {
		slog.Error("export nodes", "error", err)
		respondError(w, 500, "export failed")
		return
	}

	edges, err := h.edgeRepo.GetAll(r.Context(), 10000, 0)
	if err != nil {
		slog.Error("export edges", "error", err)
		respondError(w, 500, "export failed")
		return
	}

	// Filter edges to only include those where both endpoints are in the export
	nodeSet := make(map[string]bool)
	for _, n := range nodes {
		nodeSet[n.ID] = true
	}
	var filteredEdges []models.Edge
	for _, e := range edges {
		if nodeSet[e.SourceID] && nodeSet[e.TargetID] {
			filteredEdges = append(filteredEdges, e)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=mindbank-export.json")
	respondJSON(w, 200, map[string]any{
		"version": "1.0",
		"nodes":   nodes,
		"edges":   filteredEdges,
	})
}

// Import handles POST /api/v1/import — import graph from JSON.
func (h *BatchHandler) Import(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nodes []models.NodeCreate `json:"nodes"`
		Edges []struct {
			SourceLabel string          `json:"source_label"`
			TargetLabel string          `json:"target_label"`
			EdgeType    models.EdgeType `json:"edge_type"`
			Weight      float32         `json:"weight"`
		} `json:"edges"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid import: "+err.Error())
		return
	}

	// Create nodes, build label→ID map
	labelToID := make(map[string]string)
	created := 0
	for _, nc := range req.Nodes {
		if nc.Label == "" {
			continue
		}
		node, err := h.nodeRepo.Create(r.Context(), nc)
		if err != nil {
			continue
		}
		labelToID[nc.Label] = node.ID
		created++
	}

	// Create edges using label mapping
	edgeCount := 0
	for _, e := range req.Edges {
		srcID, ok1 := labelToID[e.SourceLabel]
		tgtID, ok2 := labelToID[e.TargetLabel]
		if !ok1 || !ok2 {
			continue
		}
		ec := models.EdgeCreate{
			SourceID: srcID,
			TargetID: tgtID,
			EdgeType: e.EdgeType,
			Weight:   &e.Weight,
		}
		if _, err := h.edgeRepo.Create(r.Context(), ec); err == nil {
			edgeCount++
		}
	}

	respondJSON(w, 200, map[string]any{
		"nodes_imported": created,
		"edges_imported": edgeCount,
	})
}

// DeleteEdgesByType handles DELETE /api/v1/edges — delete all edges of a given type.
func (h *BatchHandler) DeleteEdgesByType(w http.ResponseWriter, r *http.Request) {
	edgeTypeStr := r.URL.Query().Get("type")
	if edgeTypeStr == "" {
		respondError(w, 400, "type parameter is required")
		return
	}
	edgeType := models.EdgeType(edgeTypeStr)
	if !edgeType.IsValid() {
		respondError(w, 400, "invalid edge_type: "+edgeTypeStr)
		return
	}

	count, err := h.edgeRepo.DeleteByType(r.Context(), edgeType)
	if err != nil {
		slog.Error("delete edges by type", "error", err)
		respondError(w, 500, "delete failed")
		return
	}
	respondJSON(w, 200, map[string]any{"deleted": count, "type": edgeTypeStr})
}

// ConnectNamespaces handles POST /api/v1/edges/connect-namespaces — create semantic edges between two namespaces.
func (h *BatchHandler) ConnectNamespaces(w http.ResponseWriter, r *http.Request) {
	sourceNS := r.URL.Query().Get("source")
	targetNS := r.URL.Query().Get("target")
	if sourceNS == "" || targetNS == "" {
		respondError(w, 400, "source and target namespace parameters are required")
		return
	}
	if sourceNS == targetNS {
		respondError(w, 400, "source and target namespaces must be different")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limitPerNode := 3
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 10 {
			limitPerNode = n
		}
	}

	thresholdStr := r.URL.Query().Get("threshold")
	threshold := float32(0.70)
	if thresholdStr != "" {
		if f, err := strconv.ParseFloat(thresholdStr, 32); err == nil && f > 0 && f <= 1.0 {
			threshold = float32(f)
		}
	}

	pairs, err := h.nodeRepo.FindSimilarAcrossNamespaces(r.Context(), sourceNS, targetNS, limitPerNode, threshold)
	if err != nil {
		slog.Error("connect namespaces", "error", err)
		respondError(w, 500, "similarity search failed")
		return
	}

	created := 0
	skipped := 0
	wgt := float32(1.0)
	for _, pair := range pairs {
		_, err := h.edgeRepo.Create(r.Context(), models.EdgeCreate{
			SourceID: pair.SourceID,
			TargetID: pair.TargetID,
			EdgeType: models.EdgeRelatesTo,
			Weight:   &wgt,
		})
		if err != nil {
			skipped++
		} else {
			created++
		}
	}

	respondJSON(w, 200, map[string]any{
		"edges_created": created,
		"edges_skipped": skipped,
		"pairs_found":   len(pairs),
		"source_ns":     sourceNS,
		"target_ns":     targetNS,
	})
}

// RegisterBatchRoutes adds batch endpoints to the router.
func RegisterBatchRoutes(r chi.Router, bh *BatchHandler) {
	r.Post("/nodes/batch", bh.BatchCreateNodes)
	r.Post("/edges/batch", bh.BatchCreateEdges)
	r.Post("/nodes/auto-connect", bh.AutoConnect)
	r.Post("/edges/cleanup", bh.CleanupOrphanEdges)
	r.Delete("/edges", bh.DeleteEdgesByType)
	r.Post("/edges/connect-namespaces", bh.ConnectNamespaces)
	r.Delete("/nodes/purge", bh.PurgeSoftDeleted)
	r.Get("/export", bh.Export)
	r.Post("/import", bh.Import)
}
