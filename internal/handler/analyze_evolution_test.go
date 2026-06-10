package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEvolution_Basic(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Create a node with some history
	var nodeID string
	err := pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance, access_count, created_at)
		VALUES ('test_ws', 'test_evo', 'Test Node', 'fact', 'Test content here', 'summary', 0.8, 10, $1)
		RETURNING id
	`, time.Now().Add(-5*24*time.Hour)).Scan(&nodeID)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Call evolution endpoint
	req := httptest.NewRequest("GET", "/api/v1/analyze/evolution?node_id="+nodeID, nil)
	rec := httptest.NewRecorder()

	h.Evolution(rec, req)

	// Clean up
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if resp["node_id"] != nodeID {
		t.Errorf("expected node_id %s, got %v", nodeID, resp["node_id"])
	}

	score, ok := resp["evolution_score"].(float64)
	if !ok || score <= 0 {
		t.Errorf("expected positive evolution_score, got %v", resp["evolution_score"])
	}

	maturity, ok := resp["maturity_level"].(string)
	if !ok || maturity == "" {
		t.Errorf("expected maturity_level, got %v", resp["maturity_level"])
	}

	versions, ok := resp["versions"].([]any)
	if !ok || len(versions) == 0 {
		t.Errorf("expected versions array, got %v", resp["versions"])
	}
}

func TestEvolution_MissingNodeID(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	h := NewAnalyzeHandler(pool)

	req := httptest.NewRequest("GET", "/api/v1/analyze/evolution", nil)
	rec := httptest.NewRecorder()

	h.Evolution(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for missing node_id, got %d", rec.Code)
	}
}

func TestEvolution_NotFound(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	h := NewAnalyzeHandler(pool)

	req := httptest.NewRequest("GET", "/api/v1/analyze/evolution?node_id=00000000-0000-0000-0000-000000000000", nil)
	rec := httptest.NewRecorder()

	h.Evolution(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404 for nonexistent node, got %d", rec.Code)
	}
}
