package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"mindbank/internal/models"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
)

type EdgeHandler struct {
	repo     *repository.EdgeRepo
	nodeRepo *repository.NodeRepo
}

func (h *EdgeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.EdgeCreate
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request body: "+err.Error())
		return
	}
	if req.SourceID == "" || req.TargetID == "" {
		respondError(w, 400, "source_id and target_id are required")
		return
	}
	if req.EdgeType == "" {
		respondError(w, 400, "edge_type is required")
		return
	}
	if !req.EdgeType.IsValid() {
		respondError(w, 400, "invalid edge_type")
		return
	}

	edge, err := h.repo.Create(r.Context(), req)
	if err != nil {
		slog.Error("create edge", "error", err)
		respondError(w, 500, "failed to create edge: "+err.Error())
		return
	}
	respondJSON(w, 201, edge)
}

func (h *EdgeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := h.repo.Delete(r.Context(), id)
	if err != nil {
		slog.Error("delete edge", "error", err)
		respondError(w, 500, "failed to delete edge")
		return
	}
	if !ok {
		respondError(w, 404, "edge not found")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "deleted"})
}

func (h *EdgeHandler) List(w http.ResponseWriter, r *http.Request) {
	// List all edges (with optional type filter and limits)
	edgeType := models.EdgeType(r.URL.Query().Get("type"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 500
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	if edgeType != "" {
		edges, err := h.repo.GetByType(r.Context(), edgeType, limit, offset)
		if err != nil {
			slog.Error("list edges", "error", err)
			respondError(w, 500, "failed to list edges")
			return
		}
		if edges == nil {
			edges = []models.Edge{}
		}
		respondJSON(w, 200, edges)
		return
	}

	// No type filter — list all edges up to limit
	edges, err := h.repo.GetAll(r.Context(), limit, offset)
	if err != nil {
		slog.Error("list all edges", "error", err)
		respondError(w, 500, "failed to list edges")
		return
	}
	if edges == nil {
		edges = []models.Edge{}
	}
	respondJSON(w, 200, edges)
}

func (h *EdgeHandler) GetNeighbors(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	depthStr := r.URL.Query().Get("depth")

	if depthStr != "" {
		depth, _ := strconv.Atoi(depthStr)
		nodes, err := h.repo.GetNeighborsDeep(r.Context(), nodeID, depth)
		if err != nil {
			slog.Error("get neighbors deep", "error", err)
			respondError(w, 500, "failed to get neighbors")
			return
		}
		if nodes == nil {
			nodes = []models.NodeWithEdge{}
		}
		respondJSON(w, 200, nodes)
		return
	}

	// Default: 1 hop
	nodes, err := h.repo.GetNeighbors(r.Context(), nodeID)
	if err != nil {
		slog.Error("get neighbors", "error", err)
		respondError(w, 500, "failed to get neighbors")
		return
	}
	if nodes == nil {
		nodes = []models.NodeWithEdge{}
	}
	respondJSON(w, 200, nodes)
}

func (h *EdgeHandler) FindPath(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	targetID := chi.URLParam(r, "targetID")

	path, err := h.repo.FindPath(r.Context(), sourceID, targetID, 6)
	if err != nil {
		slog.Error("find path", "error", err)
		respondError(w, 500, "failed to find path")
		return
	}
	if path == nil {
		respondJSON(w, 200, map[string]any{"path": nil, "found": false})
		return
	}
	respondJSON(w, 200, map[string]any{"path": path, "found": true, "hops": len(path) - 1})
}
