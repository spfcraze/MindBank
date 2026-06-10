package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/quality"
)

// QualityHandler provides graph health metrics endpoints.
type QualityHandler struct {
	pool *pgxpool.Pool
}

// NewQualityHandler creates a new quality handler.
func NewQualityHandler(pool *pgxpool.Pool) *QualityHandler {
	return &QualityHandler{pool: pool}
}

// GetMetrics handles GET /api/v1/quality/metrics
func (h *QualityHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metrics, err := quality.ComputeMetrics(ctx, h.pool)
	if err != nil {
		respondError(w, 500, "compute metrics: "+err.Error())
		return
	}

	respondJSON(w, 200, metrics)
}
