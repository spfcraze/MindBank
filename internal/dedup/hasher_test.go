package dedup

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestComputeHash(t *testing.T) {
	h1 := ComputeHash("Hello", "World", "Summary")
	h2 := ComputeHash("hello", "world", "summary")
	h3 := ComputeHash("Hello", "World", "Other")

	if h1 != h2 {
		t.Errorf("ComputeHash should be case-insensitive: got %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("ComputeHash should differ for different content: got %q vs %q", h1, h3)
	}

	// Empty inputs should still produce a hash
	hEmpty := ComputeHash("", "", "")
	if hEmpty == "" {
		t.Error("ComputeHash with empty inputs should not return empty string")
	}

	// Whitespace trimming — ComputeHash trims each field individually
	hTrim1 := ComputeHash(" Hello ", " World ", " Summary ")
	hTrim2 := ComputeHash("Hello", "World", "Summary")
	if hTrim1 != hTrim2 {
		t.Errorf("ComputeHash should trim each field: got %q vs %q", hTrim1, hTrim2)
	}

	// Trailing whitespace on each field
	hTrim3 := ComputeHash("Hello", "World", "Summary")
	hTrim4 := ComputeHash("Hello ", "World ", "Summary ")
	if hTrim3 != hTrim4 {
		t.Errorf("ComputeHash should trim trailing whitespace per field: got %q vs %q", hTrim3, hTrim4)
	}
}

func TestCheckDuplicateAndStoreHash(t *testing.T) {
	// These tests require a live database; skip if no DSN is available.
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("MB_DB_DSN", "postgres://mindbank:${MB_POSTGRES_PASSWORD:-mindbank_test}@localhost:5432/mindbank?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("Skipping DB tests: %v", err)
	}
	defer pool.Close()

	// Verify connectivity quickly
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		t.Skipf("Skipping DB tests: cannot connect: %v", err)
	}

	ctx = context.Background()
	testHash := "testhash_" + ComputeHash("test", "content", "summary")

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM node_hashes WHERE hash = $1", testHash)

	// Should not find anything initially
	nodeID, err := CheckDuplicate(ctx, pool, testHash)
	if err != nil {
		t.Fatalf("CheckDuplicate initial: %v", err)
	}
	if nodeID != "" {
		t.Errorf("expected empty nodeID, got %q", nodeID)
	}

	// Store hash
	err = StoreHash(ctx, pool, testHash, "node-123")
	if err != nil {
		t.Fatalf("StoreHash: %v", err)
	}

	// Now should find it
	nodeID, err = CheckDuplicate(ctx, pool, testHash)
	if err != nil {
		t.Fatalf("CheckDuplicate after store: %v", err)
	}
	if nodeID != "node-123" {
		t.Errorf("expected node-123, got %q", nodeID)
	}

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM node_hashes WHERE hash = $1", testHash)
}
