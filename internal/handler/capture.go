package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/capture"
)

// CaptureHandler provides content capture endpoints.
type CaptureHandler struct {
	capturer *capture.Capturer
}

// NewCaptureHandler creates a new capture handler.
func NewCaptureHandler(pool *pgxpool.Pool) *CaptureHandler {
	return &CaptureHandler{
		capturer: capture.NewCapturer(pool),
	}
}

// CaptureRSS handles POST /api/v1/capture/rss
func (h *CaptureHandler) CaptureRSS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON: "+err.Error())
		return
	}

	if req.URL == "" {
		respondError(w, 400, "url is required")
		return
	}

	result, err := h.capturer.CaptureRSS(ctx, req.URL)
	if err != nil {
		respondError(w, 500, "capture failed: "+err.Error())
		return
	}

	respondJSON(w, 200, result)
}
