package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/repository"
	"mindbank/internal/utils"

	"github.com/go-chi/chi/v5"
)

type SearchHandler struct {
	searchRepo *repository.SearchRepo
	embedder   *embedder.Client
	edgeRepo   *repository.EdgeRepo
	depRepo    *repository.DependenceRepo
}

func NewSearchHandler(searchRepo *repository.SearchRepo, emb *embedder.Client, edgeRepo *repository.EdgeRepo, depRepo *repository.DependenceRepo) *SearchHandler {
	return &SearchHandler{searchRepo: searchRepo, embedder: emb, edgeRepo: edgeRepo, depRepo: depRepo}
}

// FTS handles GET /api/v1/search?q=...
func (h *SearchHandler) FTS(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		respondError(w, 400, "q parameter is required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	workspace := r.URL.Query().Get("workspace")
	namespace := r.URL.Query().Get("namespace")

	results, err := h.searchRepo.FullTextSearch(r.Context(), query, workspace, namespace, limit)
	if err != nil {
		slog.Error("fts search", "error", err)
		respondError(w, 500, "search failed")
		return
	}
	if results == nil {
		results = []models.SearchResult{}
	}
	respondJSON(w, 200, results)
}

// Semantic handles POST /api/v1/search/semantic
func (h *SearchHandler) Semantic(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string `json:"query"`
		Workspace string `json:"workspace,omitempty"`
		Namespace string `json:"namespace,omitempty"`
		Limit     int    `json:"limit,omitempty"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if req.Query == "" {
		respondError(w, 400, "query is required")
		return
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 10
	}

	// Use cached embedding
	embedding, err := h.searchRepo.GetCachedEmbedding(r.Context(), req.Query, h.embedder.Embed)
	if err != nil {
		respondEmbedError(w, err, "embed query for semantic search")
		return
	}

	results, err := h.searchRepo.VectorSearch(r.Context(), embedding, req.Workspace, req.Namespace, req.Limit)
	if err != nil {
		slog.Error("vector search", "error", err)
		respondError(w, 500, "search failed")
		return
	}
	if results == nil {
		results = []models.SearchResult{}
	}
	respondJSON(w, 200, results)
}

// Hybrid handles POST /api/v1/search/hybrid
func (h *SearchHandler) Hybrid(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query               string `json:"query"`
		Workspace           string `json:"workspace,omitempty"`
		Namespace           string `json:"namespace,omitempty"`
		Limit               int    `json:"limit,omitempty"`
		TokenBudget         int    `json:"token_budget,omitempty"`
		DependenceExpansion bool   `json:"dependence_expansion,omitempty"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if req.Query == "" {
		respondError(w, 400, "query is required")
		return
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 10
	}

	// Use cached embedding
	embedding, err := h.searchRepo.GetCachedEmbedding(r.Context(), req.Query, h.embedder.Embed)
	if err != nil {
		respondEmbedError(w, err, "embed query for hybrid search")
		return
	}

	results, err := h.searchRepo.HybridSearch(r.Context(), req.Query, embedding, req.Workspace, req.Namespace, req.Limit, h.edgeRepo)
	if err != nil {
		slog.Error("hybrid search", "error", err)
		respondError(w, 500, "search failed")
		return
	}

	// Token budget limiting
	tokenBudget := req.TokenBudget
	if tokenBudget <= 0 {
		tokenBudget = 2000 // default
	}

	var filtered []models.SearchResult
	var totalTokens int
	var budgetExceeded bool

	for _, res := range results {
		nodeTokens := utils.EstimateNodeTokens(res.Label, res.Content, res.Content)
		if totalTokens+nodeTokens > tokenBudget && len(filtered) > 0 {
			budgetExceeded = true
			break
		}
		filtered = append(filtered, res)
		totalTokens += nodeTokens
	}

	respondJSON(w, 200, map[string]any{
		"nodes":           filtered,
		"total_tokens":    totalTokens,
		"budget_exceeded": budgetExceeded,
		"total_available": len(results),
	})
}

// Embed generates an embedding for the given text.
func (sh *SearchHandler) Embed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request")
		return
	}
	embedding, err := sh.embedder.Embed(r.Context(), req.Text)
	if err != nil {
		slog.Error("embed text", "error", err)
		respondError(w, 500, "embedding generation failed")
		return
	}
	respondJSON(w, 200, map[string]any{
		"embedding": embedding,
		"dimensions": len(embedding),
		"model":     "nomic-embed-text:v1.5",
	})
}

// EmbedStats returns embedding client metrics.
func (sh *SearchHandler) EmbedStats(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, sh.embedder.GetStats())
}

// EmbedBatch handles POST /api/v1/embeddings/batch — parallel batch embedding.
func (sh *SearchHandler) EmbedBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Texts []string `json:"texts"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if len(req.Texts) == 0 {
		respondError(w, 400, "texts array is required")
		return
	}
	if len(req.Texts) > 100 {
		respondError(w, 400, "max 100 texts per batch")
		return
	}

	embeddings, err := sh.embedder.EmbedBatch(r.Context(), req.Texts)
	if err != nil {
		respondEmbedError(w, err, "batch embed")
		return
	}
	if len(embeddings) == 0 {
		respondError(w, 500, "embedding generation returned empty result")
		return
	}

	respondJSON(w, 200, map[string]any{
		"embeddings": embeddings,
		"count":      len(embeddings),
		"dimensions": len(embeddings[0]),
		"model":      "nomic-embed-text:v1.5",
	})
}

// RegisterSearchRoutes adds search endpoints to the router.
func RegisterSearchRoutes(r chi.Router, sh *SearchHandler) {
	r.Get("/search", sh.FTS)
	r.Post("/search/semantic", sh.Semantic)
	r.Post("/search/hybrid", sh.Hybrid)
	r.Post("/embeddings/generate", sh.Embed)
	r.Post("/embeddings/batch", sh.EmbedBatch)
	r.Get("/embeddings/stats", sh.EmbedStats)
}
