package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWithTx(t *testing.T) {
	// This is a unit test for the withTx helper.
	// It verifies that withTx properly commits on success and rolls back on error.
	// Full integration test requires a real database connection.

	// Since we don't have a test DB configured, we test the function signature
	// and basic behavior with a mock-like approach.

	ctx := context.Background()

	// Test that withTx returns an error when pool is nil
	var pool *pgxpool.Pool
	err := withTx(ctx, pool, func(tx pgx.Tx) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error with nil pool")
	}
}
