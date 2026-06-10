package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRefineConnectivity_MissingContext(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Create two semantically similar nodes (both about Go backend)
	var nodeA, nodeB string
	err := pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_refine', 'Go backend API', 'fact', 'Building REST API in Go with chi router', 'Go backend patterns', 0.7)
		RETURNING id
	`).Scan(&nodeA)
	if err != nil {
		t.Fatalf("create nodeA: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_refine', 'Go middleware patterns', 'fact', 'Using middleware in Go web services for auth and logging', 'Go middleware', 0.6)
		RETURNING id
	`).Scan(&nodeB)
	if err != nil {
		t.Fatalf("create nodeB: %v", err)
	}

	t.Logf("Created nodes: A=%s B=%s", nodeA, nodeB)

	// Insert embeddings for both nodes
	_, err = pool.Exec(ctx, `
		INSERT INTO node_embeddings (node_id, embedding, model) 
		VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'test')
		ON CONFLICT (node_id) DO NOTHING
	`, nodeA)
	if err != nil {
		t.Fatalf("insert embedding A: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO node_embeddings (node_id, embedding, model) 
		VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'test')
		ON CONFLICT (node_id) DO NOTHING
	`, nodeB)
	if err != nil {
		t.Fatalf("insert embedding B: %v", err)
	}

	// Verify embeddings exist
	var embCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM node_embeddings WHERE node_id IN ($1, $2)`, nodeA, nodeB).Scan(&embCount)
	if err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if embCount != 2 {
		t.Fatalf("expected 2 embeddings, got %d", embCount)
	}

	// Verify node exists with embedding via the same query pattern as handler
	var testEmb string
	var testNS string
	err = pool.QueryRow(ctx, `
		SELECT ne.embedding::text, n.namespace
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.id = $1 AND n.valid_to IS NULL
	`, nodeA).Scan(&testEmb, &testNS)
	if err != nil {
		t.Fatalf("HANDLER QUERY FAILED: %v", err)
	}
	t.Logf("Handler query OK: namespace=%s embedding_len=%d", testNS, len(testEmb))

	// Verify no edge exists initially
	var edgeCount int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM edges WHERE (source_id = $1 AND target_id = $2) OR (source_id = $2 AND target_id = $1)`, nodeA, nodeB).Scan(&edgeCount)
	if edgeCount != 0 {
		t.Fatalf("expected 0 edges initially, got %d", edgeCount)
	}

	// Call refine-connectivity with missing_context
	body, _ := json.Marshal(map[string]string{
		"node_id":  nodeA,
		"feedback": "missing_context",
	})
	req := httptest.NewRequest("POST", "/api/v1/analyze/refine-connectivity", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.RefineConnectivity(rec, req)

	// Clean up AFTER test assertions
	defer pool.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id IN ($1, $2)`, nodeA, nodeB)
	defer pool.Exec(ctx, `DELETE FROM edges WHERE source_id IN ($1, $2) OR target_id IN ($1, $2)`, nodeA, nodeB)
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id IN ($1, $2)`, nodeA, nodeB)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Should have created at least one edge suggestion
	created, ok := resp["created_edges"].([]any)
	if !ok {
		t.Fatalf("expected created_edges array, got %T", resp["created_edges"])
	}
	if len(created) == 0 {
		t.Errorf("expected at least 1 created edge, got %d", len(created))
	}

	// Verify edge actually exists in DB
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM edges WHERE (source_id = $1 AND target_id = $2) OR (source_id = $2 AND target_id = $1)`, nodeA, nodeB).Scan(&edgeCount)
	if edgeCount == 0 {
		t.Errorf("expected edge to exist in DB, got %d", edgeCount)
	}
}

func TestRefineConnectivity_TooMuchNoise(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Create node with multiple low-weight edges (noise)
	var nodeA, nodeB, nodeC string
	pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_refine', 'Main topic', 'topic', 'Main content', 'summary', 0.8)
		RETURNING id
	`).Scan(&nodeA)
	pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_refine', 'Weakly related 1', 'fact', 'Some content', 'summary', 0.1)
		RETURNING id
	`).Scan(&nodeB)
	pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_refine', 'Weakly related 2', 'fact', 'Other content', 'summary', 0.1)
		RETURNING id
	`).Scan(&nodeC)

	// Create low-weight edges - edges table doesn't have workspace_name
	pool.Exec(ctx, `INSERT INTO edges (source_id, target_id, edge_type, weight) VALUES ($1, $2, 'relates_to', 0.1)`, nodeA, nodeB)
	pool.Exec(ctx, `INSERT INTO edges (source_id, target_id, edge_type, weight) VALUES ($1, $2, 'relates_to', 0.1)`, nodeA, nodeC)

	// Call refine-connectivity with too_much_noise
	body, _ := json.Marshal(map[string]string{
		"node_id":  nodeA,
		"feedback": "too_much_noise",
	})
	req := httptest.NewRequest("POST", "/api/v1/analyze/refine-connectivity", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.RefineConnectivity(rec, req)

	// Clean up AFTER assertions
	defer pool.Exec(ctx, `DELETE FROM edges WHERE source_id = $1 OR target_id = $1`, nodeA)
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id IN ($1, $2, $3)`, nodeA, nodeB, nodeC)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	// Should have pruned edges
	pruned, _ := resp["pruned_edges"].([]any)
	if len(pruned) == 0 {
		t.Errorf("expected at least 1 pruned edge, got %d", len(pruned))
	}
}

func TestRefineConnectivity_InvalidFeedback(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	h := NewAnalyzeHandler(pool)

	body, _ := json.Marshal(map[string]string{
		"node_id":  "some-id",
		"feedback": "invalid_feedback",
	})
	req := httptest.NewRequest("POST", "/api/v1/analyze/refine-connectivity", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.RefineConnectivity(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for invalid feedback, got %d", rec.Code)
	}
}
