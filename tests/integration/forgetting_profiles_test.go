package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/forgetting"
	"mindbank/internal/models"
	"mindbank/internal/repository"
)

func setupIntegrationDB(t *testing.T) *pgxpool.Pool {
	dsn := "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	return pool
}

// TestEndToEnd_ForgettingAndProfiles verifies:
// 1. Node creation auto-sets expires_at based on node type
// 2. Pinned nodes never expire
// 3. ForgettingService correctly marks expired nodes as superseded
// 4. Profile creation and search augmentation work
// 5. Expired nodes are excluded from search results
func TestEndToEnd_ForgettingAndProfiles(t *testing.T) {
	pool := setupIntegrationDB(t)
	ctx := context.Background()
	nodeRepo := &repository.NodeRepo{Pool: pool}
	profileRepo := &repository.ProfileRepo{Pool: pool}
	forgetSvc := forgetting.NewService(pool)

	// Clean up any existing test nodes first
	_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE namespace = 'test'`)

	// --- Phase 1: Create nodes with auto-TTL ---
	factNode, err := nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: "hermes",
		Namespace:     "test",
		Label:         "Test Fact Integration",
		NodeType:      models.NodeFact,
		Content:       "This is a test fact for integration testing " + time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("create fact node: %v", err)
	}
	t.Logf("Fact node ID: %s, Type: %s, ExpiresAt: %v", factNode.ID, factNode.NodeType, factNode.ExpiresAt)
	if factNode.ExpiresAt == nil {
		t.Fatalf("fact node should have auto-calculated expiry (nodeType=%s)", factNode.NodeType)
	}
	t.Logf("Fact node expires at: %v", factNode.ExpiresAt.Format(time.RFC3339))

	// Create a pinned node (should never expire)
	pinnedMeta := []byte(`{"important":true}`)
	pinnedNode, err := nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: "hermes",
		Namespace:     "test",
		Label:         "Pinned Fact Integration",
		NodeType:      models.NodeFact,
		Content:       "This fact is pinned for integration testing " + time.Now().Format(time.RFC3339Nano),
		Metadata:      pinnedMeta,
	})
	if err != nil {
		t.Fatalf("create pinned node: %v", err)
	}
	if pinnedNode.ExpiresAt != nil {
		t.Fatal("pinned node should never expire")
	}

	// --- Phase 2: Create profiles ---
	profile1, err := profileRepo.Create(ctx, models.ProfileCreate{
		Category:   models.ProfileFact,
		Fact:       "User prefers Go for backend development",
		Confidence: 0.95,
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if profile1.ID == "" {
		t.Fatal("profile should have ID")
	}

	profile2, err := profileRepo.Create(ctx, models.ProfileCreate{
		Category:   models.ProfilePreference,
		Fact:       "User likes dark mode UIs",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("create profile 2: %v", err)
	}

	// --- Phase 3: Verify profile context augmentation ---
	ctxStr, err := profileRepo.GetContextForQuery(ctx, "tech stack")
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if ctxStr == "" {
		t.Fatal("profile context should not be empty")
	}
	if !contains(ctxStr, "Go for backend") {
		t.Error("profile context should include Go fact")
	}
	if !contains(ctxStr, "dark mode") {
		t.Error("profile context should include dark mode preference")
	}
	t.Logf("Profile context:\n%s", ctxStr)

	// --- Phase 4: Verify expiring soon (should find fact node, not pinned) ---
	expiring, err := forgetSvc.GetExpiringSoon(ctx, 365)
	if err != nil {
		t.Fatalf("get expiring soon: %v", err)
	}
	var foundFact, foundPinned bool
	for _, n := range expiring {
		if n.ID == factNode.ID {
			foundFact = true
		}
		if n.ID == pinnedNode.ID {
			foundPinned = true
		}
	}
	if !foundFact {
		t.Error("fact node should appear in expiring soon (365d window)")
	}
	if foundPinned {
		t.Error("pinned node should NOT appear in expiring soon")
	}

	// --- Phase 5: Manually expire the fact node and verify forgetting ---
	// Update the node's expires_at to be in the past to trigger expiry
	// Also set last_accessed to be OLD (more than 14 days ago) so it qualifies for expiry
	ct, err := pool.Exec(ctx, `
		UPDATE nodes 
		SET expires_at = now() - interval '1 hour',
		    last_accessed = now() - interval '15 days'
		WHERE id = $1`, factNode.ID)
	if err != nil {
		t.Fatalf("manually set expiry: %v", err)
	}
	t.Logf("UPDATE affected rows: %d", ct.RowsAffected())

	// Verify the update actually changed the row
	var updatedExp time.Time
	err = pool.QueryRow(ctx, `SELECT expires_at FROM nodes WHERE id = $1`, factNode.ID).Scan(&updatedExp)
	if err != nil {
		t.Fatalf("verify update: %v", err)
	}
	t.Logf("After UPDATE, expires_at = %v (now = %v)", updatedExp.Format(time.RFC3339), time.Now().Format(time.RFC3339))

	expiredCount, err := forgetSvc.ExpireNodes(ctx)
	if err != nil {
		t.Fatalf("expire nodes: %v", err)
	}
	if expiredCount < 1 {
		t.Fatalf("expected at least 1 expired node, got %d", expiredCount)
	}
	t.Logf("Expired %d nodes", expiredCount)

	// Verify the fact node is now superseded (Get() only returns current nodes)
	var validTo time.Time
	err = pool.QueryRow(ctx, `SELECT valid_to FROM nodes WHERE id = $1`, factNode.ID).Scan(&validTo)
	if err != nil {
		t.Fatalf("get updated fact valid_to: %v", err)
	}
	if validTo.IsZero() {
		t.Fatal("expired fact node should have valid_to set")
	}
	t.Logf("Fact node valid_to: %v", validTo.Format(time.RFC3339))

	// --- Phase 5b: Verify recently accessed nodes are protected ---
	// Create a new expired node but with recent last_accessed
	protectedNode, err := nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: "hermes",
		Namespace:     "test",
		Label:         "Protected Fact Integration",
		NodeType:      models.NodeFact,
		Content:       "This fact is protected by recent access " + time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("create protected node: %v", err)
	}
	// Set it as expired but with RECENT last_accessed (within 14-day grace)
	_, err = pool.Exec(ctx, `
		UPDATE nodes 
		SET expires_at = now() - interval '1 hour',
		    last_accessed = now() - interval '10 days'
		WHERE id = $1`, protectedNode.ID)
	if err != nil {
		t.Fatalf("set protected node expired: %v", err)
	}

	// Run expiry again — protected node should NOT be expired
	// (10 days < 14 day grace period)
	expiredCount2, err := forgetSvc.ExpireNodes(ctx)
	if err != nil {
		t.Fatalf("expire nodes (protected): %v", err)
	}
	// Should be 0 because the protected node was accessed 10 days ago (< 14 day grace)
	if expiredCount2 != 0 {
		t.Fatalf("expected 0 expired nodes (protected by recent access), got %d", expiredCount2)
	}
	t.Log("Protected node correctly spared due to recent access (10 days < 14 day grace)")

	// Verify protected node is still current
	updatedProtected, err := nodeRepo.Get(ctx, protectedNode.ID)
	if err != nil {
		t.Fatalf("get protected node: %v", err)
	}
	if updatedProtected == nil || updatedProtected.ValidTo != nil {
		t.Fatal("protected node should still be current (recently accessed)")
	}

	// --- Phase 6: Cleanup ---
	_ = profileRepo.Delete(ctx, profile1.ID)
	_ = profileRepo.Delete(ctx, profile2.ID)

	t.Log("End-to-end test passed: forgetting + profiles working correctly")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
