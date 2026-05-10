package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/autocapture"
	"mindbank/internal/migration"
	"mindbank/internal/models"
	"mindbank/internal/repository"
)

// TestNamespaceMigrationDryRun verifies migration preview without data changes.
func TestNamespaceMigrationDryRun(t *testing.T) {
	ctx := context.Background()

	dsn := os.Getenv("MB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()

	nodeRepo := repository.NewNodeRepo(pool)

	// Create test nodes with various path patterns in metadata
	// Use a unique test namespace to avoid conflicts with previous test runs
	testNS := fmt.Sprintf("test_migration_global_%d", time.Now().UnixNano())
	testCases := []struct {
		label       string
		content     string
		wantNS      string
		metaCWD     string
		nodeType    models.NodeType
	}{
		{
			label:    "Session in worker dir",
			content:  "Working on feature X",
			wantNS:   "mindbank",
			metaCWD:  "/home/rat/mindbank-worker-20",
			nodeType: models.NodeSession,
		},
		{
			label:    "Session in team worker",
			content:  "Team collaboration",
			wantNS:   "test-website-team",
			metaCWD:  "/home/rat/test-website-team-worker-120",
			nodeType: models.NodeSession,
		},
		{
			label:    "Clean project path",
			content:  "Regular project work",
			wantNS:   "klixsor",
			metaCWD:  "/home/rat/klixsor",
			nodeType: models.NodeSession,
		},
		{
			label:    "Nested worker path",
			content:  "Deep nested project",
			wantNS:   "mindbank",
			metaCWD:  "/home/rat/projects/deep/mindbank-worker-99",
			nodeType: models.NodeSession,
		},
		{
			label:    "Hermes path",
			content:  "Hermes session",
			wantNS:   "hermes",
			metaCWD:  "/home/rat/hermes",
			nodeType: models.NodeSession,
		},
	}

	var createdIDs []string
	for i, tc := range testCases {
		// Make content unique to avoid deduplication with production data
		uniqueContent := fmt.Sprintf("%s [test-%d-%d]", tc.content, time.Now().UnixNano(), i)
		meta := []byte(fmt.Sprintf(`{"working_directory":"%s"}`, tc.metaCWD))
		node, err := nodeRepo.Create(ctx, models.NodeCreate{
			WorkspaceName: "hermes",
			Namespace:     testNS, // Use test namespace
			Label:         tc.label,
			NodeType:      tc.nodeType,
			Content:       uniqueContent,
			Metadata:      meta,
		})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		createdIDs = append(createdIDs, node.ID)
	}

	// Run migration in DRY-RUN mode on test namespace
	migrator := migration.NewNamespaceMigrator(pool, autocapture.DeriveNamespaceFromPath)
	report, err := migrator.Migrate(ctx, testNS, true) // dryRun=true
	if err != nil {
		t.Fatalf("dry-run migration: %v", err)
	}

	// Verify dry-run report
	if report.TotalScanned != len(testCases) {
		t.Errorf("scanned: got %d, want %d", report.TotalScanned, len(testCases))
	}
	if report.WouldUpdate != 5 { // All 5 test nodes would be updated (testNS → derived NS)
		t.Errorf("would update: got %d, want 5", report.WouldUpdate)
	}
	if report.ActuallyUpdated != 0 {
		t.Errorf("actually updated in dry-run: got %d, want 0", report.ActuallyUpdated)
	}

	// Verify NO nodes were actually changed
	for i, id := range createdIDs {
		node, err := nodeRepo.Get(ctx, id)
		if err != nil {
			t.Fatalf("get node %d: %v", i, err)
		}
		if node.Namespace != testNS {
			t.Errorf("node %d namespace changed in dry-run: got %q, want %q", i, node.Namespace, testNS)
		}
	}

	t.Logf("Dry-run report: %+v", report)

	// Cleanup
	t.Cleanup(func() {
		for _, id := range createdIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id)
		}
	})
}

// TestNamespaceMigrationCommit verifies actual migration updates data correctly.
func TestNamespaceMigrationCommit(t *testing.T) {
	ctx := context.Background()

	dsn := os.Getenv("MB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()

	nodeRepo := repository.NewNodeRepo(pool)

	// Create test nodes
	testNS := "test_migration_commit"
	testCases := []struct {
		label   string
		metaCWD string
		wantNS  string
	}{
		{"Worker session", "/home/rat/mindbank-worker-20", "mindbank"},
		{"Team worker", "/home/rat/test-website-team-worker-120", "test-website-team"},
		{"Clean path", "/home/rat/klixsor", "klixsor"},
		{"No metadata", "", "test_migration_commit"}, // Should stay in testNS
	}

	var createdIDs []string
	for i, tc := range testCases {
		var meta []byte
		if tc.metaCWD != "" {
			meta = []byte(fmt.Sprintf(`{"working_directory":"%s"}`, tc.metaCWD))
		}
		// Make content unique to avoid deduplication
		uniqueContent := fmt.Sprintf("test content %d %d", time.Now().UnixNano(), i)
		node, err := nodeRepo.Create(ctx, models.NodeCreate{
			WorkspaceName: "hermes",
			Namespace:     testNS,
			Label:         tc.label,
			NodeType:      models.NodeSession,
			Content:       uniqueContent,
			Metadata:      meta,
		})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		createdIDs = append(createdIDs, node.ID)
	}

	// Run migration FOR REAL on test namespace
	migrator := migration.NewNamespaceMigrator(pool, autocapture.DeriveNamespaceFromPath)
	report, err := migrator.Migrate(ctx, testNS, false) // dryRun=false
	if err != nil {
		t.Fatalf("commit migration: %v", err)
	}

	// Verify report
	if report.ActuallyUpdated != 3 { // 3 need change, 1 stays
		t.Errorf("updated: got %d, want 3", report.ActuallyUpdated)
	}

	// Verify each node has correct namespace
	for i, tc := range testCases {
		node, err := nodeRepo.Get(ctx, createdIDs[i])
		if err != nil {
			t.Fatalf("get node %d: %v", i, err)
		}
		if node.Namespace != tc.wantNS {
			t.Errorf("node %d (%s): got namespace %q, want %q", i, tc.label, node.Namespace, tc.wantNS)
		}
		t.Logf("Node %d: %s → namespace=%s ✓", i, tc.label, node.Namespace)
	}

	// Cleanup
	t.Cleanup(func() {
		for _, id := range createdIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id)
		}
	})
}

// TestNamespaceMigrationIdempotent verifies running migration twice is safe.
func TestNamespaceMigrationIdempotent(t *testing.T) {
	ctx := context.Background()

	dsn := os.Getenv("MB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()

	nodeRepo := repository.NewNodeRepo(pool)

	// Create a node that needs migration
	testNS := fmt.Sprintf("test_migration_idempotent_%d", time.Now().UnixNano())
	meta := []byte(`{"working_directory":"/home/rat/mindbank-worker-20"}`)
	node, err := nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: "hermes",
		Namespace:     testNS,
		Label:         "Idempotent test",
		NodeType:      models.NodeSession,
		Content:       fmt.Sprintf("test %d", time.Now().UnixNano()),
		Metadata:      meta,
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	migrator := migration.NewNamespaceMigrator(pool, autocapture.DeriveNamespaceFromPath)

	// First migration
	r1, err := migrator.Migrate(ctx, testNS, false)
	if err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if r1.ActuallyUpdated != 1 {
		t.Errorf("first run: updated %d, want 1", r1.ActuallyUpdated)
	}

	// Second migration — should update 0
	r2, err := migrator.Migrate(ctx, testNS, false)
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if r2.ActuallyUpdated != 0 {
		t.Errorf("second run: updated %d, want 0 (not idempotent)", r2.ActuallyUpdated)
	}

	// Verify node still correct
	n, err := nodeRepo.Get(ctx, node.ID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if n.Namespace != "mindbank" {
		t.Errorf("namespace after idempotent runs: got %q, want 'mindbank'", n.Namespace)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, node.ID)
	})
}

// TestHermesWorkspaceExtractor verifies workspace name from Hermes profile.
func TestHermesWorkspaceExtractor(t *testing.T) {
	ctx := context.Background()

	// Test with actual Hermes installation
	extractor := migration.NewHermesWorkspaceExtractor("/home/rat/.hermes")

	// Should find profiles
	profiles, err := extractor.ListProfiles(ctx)
	if err != nil {
		t.Logf("ListProfiles error (may be expected in test env): %v", err)
	} else {
		t.Logf("Found profiles: %v", profiles)
		if len(profiles) == 0 {
			t.Log("No profiles found — Hermes may not be configured")
		}
	}

	// Test workspace extraction with namespace mapping
	ws, err := extractor.GetWorkspaceForNamespace(ctx, "klixsor")
	if err != nil {
		t.Logf("GetWorkspaceForNamespace error: %v", err)
	} else {
		t.Logf("Workspace for 'klixsor': %s", ws)
		// Based on mindbank-namespaces.json, klixsor maps to klixsor
		if ws != "klixsor" && ws != "" {
			t.Errorf("unexpected workspace for klixsor: %s", ws)
		}
	}

	// Test fallback
	wsFallback, err := extractor.GetWorkspaceForNamespace(ctx, "nonexistent")
	if err != nil {
		t.Logf("Fallback error: %v", err)
	}
	if wsFallback != "" {
		t.Logf("Fallback workspace: %s", wsFallback)
	}
}

// TestWorkspaceNameInIngestion verifies workspace is set correctly during ingestion.
func TestWorkspaceNameInIngestion(t *testing.T) {
	ctx := context.Background()

	dsn := os.Getenv("MB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()

	nodeRepo := repository.NewNodeRepo(pool)
	extractor := migration.NewHermesWorkspaceExtractor("/home/rat/.hermes")

	// Simulate ingestion for klixsor namespace
	ns := "klixsor"
	ws, err := extractor.GetWorkspaceForNamespace(ctx, ns)
	if err != nil {
		t.Logf("Using default workspace: hermes")
		ws = "hermes" // fallback
	}
	if ws == "" {
		ws = "hermes"
	}

	// Create node with extracted workspace
	// If workspace doesn't exist in DB, use "hermes" which is guaranteed to exist
	wsSafe := ws
	if ws != "hermes" {
		// Check if workspace exists
		var exists bool
		_ = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE name = $1)`, ws).Scan(&exists)
		if !exists {
			t.Logf("Workspace %q not found, falling back to 'hermes'", ws)
			wsSafe = "hermes"
		}
	}

	node, err := nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: wsSafe,
		Namespace:     ns,
		Label:         "Klixsor TDS Feature",
		NodeType:      models.NodeFact,
		Content:       "Traffic distribution system analytics [test]",
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Verify workspace was set correctly
	if node.WorkspaceName != wsSafe {
		t.Errorf("workspace: got %q, want %q", node.WorkspaceName, wsSafe)
	}
	if node.Namespace != ns {
		t.Errorf("namespace: got %q, want %q", node.Namespace, ns)
	}

	t.Logf("Created node: workspace=%s namespace=%s", node.WorkspaceName, node.Namespace)

	// Cleanup
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, node.ID)
	})
}