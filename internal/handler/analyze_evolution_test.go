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

	// Ensure workspace exists
	pool.Exec(ctx, `INSERT INTO workspaces (name) VALUES ('test_ws') ON CONFLICT DO NOTHING`)

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
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)
	defer pool.Exec(ctx, `DELETE FROM workspaces WHERE name = 'test_ws'`)

	// Call evolution endpoint
	req := httptest.NewRequest("GET", "/api/v1/analyze/evolution?node_id="+nodeID, nil)
	rec := httptest.NewRecorder()

	h.Evolution(rec, req)

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

	// Verify single version has is_current flag
	if len(versions) > 0 {
		v := versions[0].(map[string]any)
		if _, hasIsCurrent := v["is_current"]; !hasIsCurrent {
			t.Errorf("expected version to have is_current field")
		}
	}
}

func TestEvolution_WithPredecessors(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Ensure workspace exists
	pool.Exec(ctx, `INSERT INTO workspaces (name) VALUES ('test_ws') ON CONFLICT DO NOTHING`)

	// Create predecessor chain: v1 -> v2 -> v3 (current)
	var v1ID, v2ID, v3ID string
	now := time.Now()

	// v1: oldest (superseded by v2)
	err := pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance, access_count, created_at, valid_to)
		VALUES ('test_ws', 'test_evo', 'Test Node v1', 'fact', 'Content v1', 'summary', 0.5, 5, $1, $2)
		RETURNING id
	`, now.Add(-30*24*time.Hour), now.Add(-20*24*time.Hour)).Scan(&v1ID)
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, v1ID)

	// v2: middle, points to v1
	err = pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance, access_count, created_at, predecessor_id)
		VALUES ('test_ws', 'test_evo', 'Test Node v2', 'fact', 'Content v2', 'summary', 0.6, 8, $1, $2)
		RETURNING id
	`, now.Add(-20*24*time.Hour), v1ID).Scan(&v2ID)
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, v2ID)

	// v3: current, points to v2
	err = pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance, access_count, created_at, predecessor_id)
		VALUES ('test_ws', 'test_evo', 'Test Node v3', 'fact', 'Content v3', 'summary', 0.8, 15, $1, $2)
		RETURNING id
	`, now.Add(-10*24*time.Hour), v2ID).Scan(&v3ID)
	if err != nil {
		t.Fatalf("create v3: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, v3ID)
	defer pool.Exec(ctx, `DELETE FROM workspaces WHERE name = 'test_ws'`)

	// Call evolution endpoint on current node
	req := httptest.NewRequest("GET", "/api/v1/analyze/evolution?node_id="+v3ID, nil)
	rec := httptest.NewRecorder()

	h.Evolution(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Should find 3 versions
	versions, ok := resp["versions"].([]any)
	if !ok || len(versions) != 3 {
		t.Errorf("expected 3 versions, got %v", resp["versions"])
	}

	// Should be stable (3 versions, 30+ days, access > 0)
	maturity, ok := resp["maturity_level"].(string)
	if !ok || maturity != "stable" {
		t.Errorf("expected maturity 'stable', got %v", maturity)
	}

	// Score should be positive
	score, ok := resp["evolution_score"].(float64)
	if !ok || score <= 0 {
		t.Errorf("expected positive score, got %v", score)
	}

	// Convergence delta should be < 1.0 (capped)
	delta, ok := resp["convergence_delta"].(float64)
	if !ok || delta > 1.0 {
		t.Errorf("expected convergence_delta <= 1.0, got %v", delta)
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
