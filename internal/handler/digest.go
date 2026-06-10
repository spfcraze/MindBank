package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/digest"
)

// DigestHandler provides memory digest endpoints.
type DigestHandler struct {
	gen *digest.Generator
}

// NewDigestHandler creates a new digest handler.
func NewDigestHandler(pool *pgxpool.Pool) *DigestHandler {
	return &DigestHandler{gen: digest.NewGenerator(pool)}
}

// GetDigest handles GET /api/v1/digest?period=daily|weekly|monthly
func (h *DigestHandler) GetDigest(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "daily"
	}

	d, err := h.gen.Generate(r.Context(), period)
	if err != nil {
		respondError(w, 500, "generate digest: "+err.Error())
		return
	}

	respondJSON(w, 200, d)
}

// GetTopicTrends handles GET /api/v1/digest/trends?days=7
func (h *DigestHandler) GetTopicTrends(w http.ResponseWriter, r *http.Request) {
	days := 7
	if d := r.URL.Query().Get("days"); d != "" {
		// Simple parse, default to 7 if invalid
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 30 {
			days = n
		}
	}

	trends, err := h.gen.GetTopicTrends(r.Context(), days)
	if err != nil {
		respondError(w, 500, "get trends: "+err.Error())
		return
	}

	respondJSON(w, 200, map[string]interface{}{
		"days":   days,
		"trends": trends,
	})
}
