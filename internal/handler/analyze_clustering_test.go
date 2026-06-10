package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestClusterSessions_Basic(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Create 3 session nodes with similar embeddings
	var nodeA, nodeB, nodeC string
	pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_cluster', 'Session A', 'session', 'Working on API design', 'API session', 0.7)
		RETURNING id
	`).Scan(&nodeA)
	pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_cluster', 'Session B', 'session', 'API design patterns discussion', 'API session', 0.6)
		RETURNING id
	`).Scan(&nodeB)
	pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test_ws', 'test_cluster', 'Session C', 'session', 'Database schema design', 'DB session', 0.5)
		RETURNING id
	`).Scan(&nodeC)

	// Insert embeddings (all identical for clustering)
	pool.Exec(ctx, `
		INSERT INTO node_embeddings (node_id, embedding, model)
		VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'test')
		ON CONFLICT (node_id) DO NOTHING
	`, nodeA)
	pool.Exec(ctx, `
		INSERT INTO node_embeddings (node_id, embedding, model)
		VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'test')
		ON CONFLICT (node_id) DO NOTHING
	`, nodeB)
	pool.Exec(ctx, `
		INSERT INTO node_embeddings (node_id, embedding, model)
		VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'test')
		ON CONFLICT (node_id) DO NOTHING
	`, nodeC)

	// Call cluster endpoint
	body, _ := json.Marshal(map[string]string{
		"namespace": "test_cluster",
	})
	req := httptest.NewRequest("POST", "/api/v1/analyze/cluster-sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ClusterSessions(rec, req)

	// Clean up
	defer pool.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id IN ($1, $2, $3)`, nodeA, nodeB, nodeC)
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id IN ($1, $2, $3)`, nodeA, nodeB, nodeC)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	clusters, ok := resp["clusters"].([]any)
	if !ok {
		t.Fatalf("expected clusters array, got %T", resp["clusters"])
	}

	// With identical embeddings, all 3 should cluster together
	if len(clusters) == 0 {
		t.Errorf("expected at least 1 cluster, got %d", len(clusters))
	}

	count, ok := resp["count"].(float64)
	if !ok || count == 0 {
		t.Errorf("expected positive cluster count, got %v", resp["count"])
	}
}

func TestClusterSessions_EmptyNamespace(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	h := NewAnalyzeHandler(pool)

	body, _ := json.Marshal(map[string]string{
		"namespace": "nonexistent_namespace_12345",
	})
	req := httptest.NewRequest("POST", "/api/v1/analyze/cluster-sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ClusterSessions(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	clusters, ok := resp["clusters"].([]any)
	if !ok || len(clusters) != 0 {
		t.Errorf("expected empty clusters for nonexistent namespace, got %v", clusters)
	}
}
