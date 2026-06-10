package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/enrichment"
)

// EnrichmentHandler provides node enrichment endpoints.
type EnrichmentHandler struct {
	enricher *enrichment.Enricher
}

// NewEnrichmentHandler creates a new enrichment handler.
func NewEnrichmentHandler(pool *pgxpool.Pool) *EnrichmentHandler {
	return &EnrichmentHandler{
		enricher: enrichment.NewEnricher(pool),
	}
}

// EnrichAll handles POST /api/v1/enrichment/enrich-all
// Batch enriches nodes with empty summaries.
func (h *EnrichmentHandler) EnrichAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	enriched, err := h.enricher.BatchEnrich(ctx, limit)
	if err != nil {
		respondError(w, 500, "enrichment failed: "+err.Error())
		return
	}

	// Get updated stats
	stats, err := h.enricher.GetEnrichmentStats(ctx)
	if err != nil {
		stats = map[string]interface{}{"error": err.Error()}
	}

	respondJSON(w, 200, map[string]interface{}{
		"enriched_count": enriched,
		"limit":          limit,
		"stats":          stats,
	})
}

// GetEnrichmentStats handles GET /api/v1/enrichment/stats
func (h *EnrichmentHandler) GetEnrichmentStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := h.enricher.GetEnrichmentStats(ctx)
	if err != nil {
		respondError(w, 500, "get stats: "+err.Error())
		return
	}

	respondJSON(w, 200, stats)
}
