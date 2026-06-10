package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// BenchmarkRefineConnectivity_MissingContext measures the performance of link expansion
func BenchmarkRefineConnectivity_MissingContext(b *testing.B) {
	pool := setupTestPool(nil)
	if pool == nil {
		b.Skip("DB not available")
	}
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Setup: create a node with embedding
	var nodeID string
	err := pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('bench_ws', 'bench', 'Benchmark Node', 'fact', 'Benchmark content for testing', 'bench summary', 0.7)
		RETURNING id
	`).Scan(&nodeID)
	if err != nil {
		b.Fatalf("create node: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_embeddings (node_id, embedding, model)
		VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'bench')
		ON CONFLICT (node_id) DO NOTHING
	`, nodeID)
	if err != nil {
		b.Fatalf("insert embedding: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, nodeID)

	body, _ := json.Marshal(map[string]string{
		"node_id":  nodeID,
		"feedback": "missing_context",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/v1/analyze/refine-connectivity", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.RefineConnectivity(rec, req)
		if rec.Code != 200 {
			b.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkEvolution_Basic measures evolution score computation
func BenchmarkEvolution_Basic(b *testing.B) {
	pool := setupTestPool(nil)
	if pool == nil {
		b.Skip("DB not available")
	}
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Setup: create a node
	var nodeID string
	err := pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance, access_count, created_at)
		VALUES ('bench_ws', 'bench', 'Benchmark Node', 'fact', 'Benchmark content', 'bench summary', 0.8, 100, $1)
		RETURNING id
	`, time.Now().Add(-30*24*time.Hour)).Scan(&nodeID)
	if err != nil {
		b.Fatalf("create node: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/v1/analyze/evolution?node_id="+nodeID, nil)
		rec := httptest.NewRecorder()
		h.Evolution(rec, req)
		if rec.Code != 200 {
			b.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkClusterSessions_Basic measures clustering performance
func BenchmarkClusterSessions_Basic(b *testing.B) {
	pool := setupTestPool(nil)
	if pool == nil {
		b.Skip("DB not available")
	}
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Setup: create 10 session nodes with embeddings
	var nodeIDs []string
	for i := 0; i < 10; i++ {
		var nodeID string
		err := pool.QueryRow(ctx, `
			INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
			VALUES ('bench_ws', 'bench_cluster', $1, 'session', 'Session content', 'summary', 0.5)
			RETURNING id
		`, fmt.Sprintf("Session %d", i)).Scan(&nodeID)
		if err != nil {
			b.Fatalf("create node %d: %v", i, err)
		}
		nodeIDs = append(nodeIDs, nodeID)

		_, err = pool.Exec(ctx, `
			INSERT INTO node_embeddings (node_id, embedding, model)
			VALUES ($1, (SELECT ARRAY(SELECT 0.1::real FROM generate_series(1,768)))::vector, 'bench')
			ON CONFLICT (node_id) DO NOTHING
		`, nodeID)
		if err != nil {
			b.Fatalf("insert embedding %d: %v", i, err)
		}
	}

	// Cleanup
	defer func() {
		for _, id := range nodeIDs {
			pool.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, id)
			pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id)
		}
	}()

	body, _ := json.Marshal(map[string]string{
		"namespace": "bench_cluster",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/v1/analyze/cluster-sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ClusterSessions(rec, req)
		if rec.Code != 200 {
			b.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkClusterSessions_Scale measures clustering with 50 sessions
func BenchmarkClusterSessions_Scale(b *testing.B) {
	pool := setupTestPool(nil)
	if pool == nil {
		b.Skip("DB not available")
	}
	defer pool.Close()

	ctx := context.Background()
	h := NewAnalyzeHandler(pool)

	// Setup: create 50 session nodes
	var nodeIDs []string
	for i := 0; i < 50; i++ {
		var nodeID string
		err := pool.QueryRow(ctx, `
			INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
			VALUES ('bench_ws', 'bench_cluster_scale', $1, 'session', 'Session content', 'summary', 0.5)
			RETURNING id
		`, fmt.Sprintf("Session %d", i)).Scan(&nodeID)
		if err != nil {
			b.Fatalf("create node %d: %v", i, err)
		}
		nodeIDs = append(nodeIDs, nodeID)

		_, err = pool.Exec(ctx, `
			INSERT INTO node_embeddings (node_id, embedding, model)
			VALUES ($1, (SELECT ARRAY(SELECT random()::real FROM generate_series(1,768)))::vector, 'bench')
			ON CONFLICT (node_id) DO NOTHING
		`, nodeID)
		if err != nil {
			b.Fatalf("insert embedding %d: %v", i, err)
		}
	}

	defer func() {
		for _, id := range nodeIDs {
			pool.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, id)
			pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id)
		}
	}()

	body, _ := json.Marshal(map[string]string{
		"namespace": "bench_cluster_scale",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/v1/analyze/cluster-sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ClusterSessions(rec, req)
		if rec.Code != 200 {
			b.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}
