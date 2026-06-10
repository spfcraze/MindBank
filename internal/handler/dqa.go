package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/quality"
)

// DQAHandler provides Data Quality Analyzer endpoints.
type DQAHandler struct {
	pool *pgxpool.Pool
}

// NewDQAHandler creates a new DQA handler.
func NewDQAHandler(pool *pgxpool.Pool) *DQAHandler {
	return &DQAHandler{pool: pool}
}

// Analyze handles GET /api/v1/dqa/analyze
// Returns comprehensive data quality metrics computed server-side.
func (h *DQAHandler) Analyze(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metrics, err := quality.ComputeMetrics(ctx, h.pool)
	if err != nil {
		respondError(w, 500, "compute metrics: "+err.Error())
		return
	}

	respondJSON(w, 200, metrics)
}
