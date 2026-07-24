package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/dream"
)

// RerankHandler provides HTTP endpoints for token reranking.
type RerankHandler struct {
	reranker *dream.TokenReranker
}

// NewRerankHandler creates a rerank handler.
func NewRerankHandler(pool *pgxpool.Pool) *RerankHandler {
	return &RerankHandler{
		reranker: dream.NewTokenReranker(pool),
	}
}

// Search handles POST /search/rerank - search with token reranking.
func (h *RerankHandler) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TopK <= 0 || req.TopK > 50 {
		req.TopK = 10
	}

	results, err := h.reranker.SearchAndRerank(ctx, req.Query, req.TopK)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"query":   req.Query,
		"results": results,
		"count":   len(results),
	})
}

// Rerank handles POST /rerank - rerank existing candidates.
func (h *RerankHandler) Rerank(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Query      string   `json:"query"`
		Candidates []string `json:"candidates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Candidates) == 0 {
		respondError(w, http.StatusBadRequest, "no candidates provided")
		return
	}

	results, err := h.reranker.Rerank(ctx, req.Query, req.Candidates)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "rerank failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"query":   req.Query,
		"results": results,
		"count":   len(results),
	})
}

// GraphEmbedHandler provides HTTP endpoints for graph-aware embeddings.
type GraphEmbedHandler struct {
	embedder *dream.GraphEmbedder
}

// NewGraphEmbedHandler creates a graph embed handler.
func NewGraphEmbedHandler(pool *pgxpool.Pool) *GraphEmbedHandler {
	return &GraphEmbedHandler{
		embedder: dream.NewGraphEmbedder(pool),
	}
}

// Compute handles POST /graph-embed/compute - compute graph embedding for a node.
func (h *GraphEmbedHandler) Compute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		NodeID   string `json:"node_id"`
		HopCount int    `json:"hop_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.HopCount <= 0 || req.HopCount > 3 {
		req.HopCount = 1
	}

	embedding, err := h.embedder.ComputeEmbedding(ctx, req.NodeID, req.HopCount)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "compute failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"node_id":     embedding.NodeID,
		"hop_count":   embedding.HopCount,
		"neighbors":   len(embedding.NeighborIDs),
		"status":      "computed",
	})
}

// BatchCompute handles POST /graph-embed/batch - compute for all nodes.
func (h *GraphEmbedHandler) BatchCompute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	hopCount := 1
	if hStr := r.URL.Query().Get("hops"); hStr != "" {
		if h, err := strconv.Atoi(hStr); err == nil && h > 0 && h <= 3 {
			hopCount = h
		}
	}

	count, err := h.embedder.BatchCompute(ctx, hopCount)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "batch compute failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"nodes_processed": count,
		"hop_count":       hopCount,
		"status":          "completed",
	})
}

// Search handles POST /graph-embed/search - search using graph embeddings.
func (h *GraphEmbedHandler) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10
	}

	results, err := h.embedder.SearchWithGraphEmbedding(ctx, req.Query, req.Limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"query":   req.Query,
		"results": results,
		"count":   len(results),
	})
}
