package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestParseInt verifies the parseInt helper.
func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		defaultV int
		want     int
	}{
		{"", 50, 50},
		{"10", 50, 10},
		{"abc", 50, 50},
		{"-5", 50, -5},
	}
	for _, tc := range tests {
		got := parseInt(tc.input, tc.defaultV)
		if got != tc.want {
			t.Errorf("parseInt(%q, %d) = %d, want %d", tc.input, tc.defaultV, got, tc.want)
		}
	}
}

// TestEmbeddingRecall_EmptyGraph tests the endpoint with no nodes.
func TestEmbeddingRecall_EmptyGraph(t *testing.T) {
	// This test requires a real database connection.
	// Skip if DB is not available.
	pool, err := pgxpool.New(context.Background(), "postgres://mindbank:mindbank@localhost:5434/mindbank")
	if err != nil {
		t.Skip("DB not available:", err)
	}
	defer pool.Close()

	// Check if DB actually has nodes — if it does, this test is not testing an empty graph
	var nodeCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL").Scan(&nodeCount); err != nil {
		t.Skip("Could not query node count:", err)
	}
	if nodeCount > 0 {
		t.Skip("DB has", nodeCount, "nodes — skipping empty graph test")
	}

	h := NewAnalyzeHandler(pool)
	req := httptest.NewRequest(http.MethodGet, "/analyze/embedding-recall?n=10", nil)
	rr := httptest.NewRecorder()

	h.EmbeddingRecall(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp["sample_size"].(float64) != 0 {
		t.Errorf("expected sample_size 0 for empty graph, got %v", resp["sample_size"])
	}
	if resp["recall_rate"].(float64) != 0 {
		t.Errorf("expected recall_rate 0 for empty graph, got %v", resp["recall_rate"])
	}
}
