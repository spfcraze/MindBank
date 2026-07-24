package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestPool(t testing.TB) *pgxpool.Pool {
	dsn := os.Getenv("MB_DB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("DB ping failed: %v", err)
	}
	return pool
}

func TestUpdateEpistemicLabel(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	h := &NodeHandler{pool: pool}

	// Create a test node directly in the DB
	var nodeID string
	err := pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, metadata, importance)
		VALUES ('test_ws', 'test_ns', 'Epistemic Test Node', 'fact', 'test content', 'test summary', '{}', 0.5)
		RETURNING id
	`).Scan(&nodeID)
	if err != nil {
		t.Fatalf("create test node: %v", err)
	}

	// Clean up after test
	defer pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/nodes/epistemic?label=observed&node_id="+nodeID, nil)
		rec := httptest.NewRecorder()

		h.UpdateEpistemicLabel(rec, req)

		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON response: %v", err)
		}
		if resp["node_id"] != nodeID {
			t.Errorf("node_id = %q, want %q", resp["node_id"], nodeID)
		}
		if resp["epistemic_label"] != "observed" {
			t.Errorf("epistemic_label = %q, want %q", resp["epistemic_label"], "observed")
		}

		// Verify DB state
		var dbLabel string
		err := pool.QueryRow(ctx, `SELECT epistemic_label FROM nodes WHERE id = $1 AND valid_to IS NULL`, nodeID).Scan(&dbLabel)
		if err != nil {
			t.Fatalf("query db label: %v", err)
		}
		if dbLabel != "observed" {
			t.Errorf("db label = %q, want %q", dbLabel, "observed")
		}
	})

	t.Run("missing_node_id", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/nodes/epistemic?label=observed", nil)
		rec := httptest.NewRecorder()

		h.UpdateEpistemicLabel(rec, req)

		if rec.Code != 400 {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing_label", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/nodes/epistemic?node_id="+nodeID, nil)
		rec := httptest.NewRecorder()

		h.UpdateEpistemicLabel(rec, req)

		if rec.Code != 400 {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("invalid_label", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/nodes/epistemic?label=invalid&node_id="+nodeID, nil)
		rec := httptest.NewRecorder()

		h.UpdateEpistemicLabel(rec, req)

		if rec.Code != 400 {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("node_not_found", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/nodes/epistemic?label=observed&node_id=nonexistent-id", nil)
		rec := httptest.NewRecorder()

		h.UpdateEpistemicLabel(rec, req)

		if rec.Code != 404 {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("all_valid_labels", func(t *testing.T) {
		labels := []string{"observed", "inferred", "assumed", "recommended", "unknown"}
		for _, label := range labels {
			req := httptest.NewRequest("PUT", "/api/v1/nodes/epistemic?label="+label+"&node_id="+nodeID, nil)
			rec := httptest.NewRecorder()

			h.UpdateEpistemicLabel(rec, req)

			if rec.Code != 200 {
				t.Errorf("label %q: expected 200, got %d: %s", label, rec.Code, rec.Body.String())
			}
		}
	})
}
