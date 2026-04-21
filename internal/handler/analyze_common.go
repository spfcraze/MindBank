package handler

import (
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AnalyzeHandler provides intelligence endpoints for graph analysis.
type AnalyzeHandler struct {
	pool *pgxpool.Pool
}

// NewAnalyzeHandler creates a new analyze handler.
func NewAnalyzeHandler(pool *pgxpool.Pool) *AnalyzeHandler {
	return &AnalyzeHandler{pool: pool}
}

// truncate returns the first n characters of s with "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// itoa converts int to string.
func itoa(n int) string {
	return strconv.Itoa(n)
}
