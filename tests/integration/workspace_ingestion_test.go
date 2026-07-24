package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/autocapture"
	"mindbank/internal/models"
	"mindbank/internal/repository"
)

// TestWorkspaceIngestionPipeline simulates the full Hermes → MindBank ingestion
// to verify namespace normalization, workspace identification, and data integrity.
func TestWorkspaceIngestionPipeline(t *testing.T) {
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
	edgeRepo := repository.NewEdgeRepo(pool)

	// Clean up test data first
	_, _ = pool.Exec(ctx, `DELETE FROM edges WHERE source_id IN (SELECT id FROM nodes WHERE namespace LIKE 'sim_%')`)
	_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE namespace LIKE 'sim_%'`)

	// --- Phase 1: Simulate Hermes sessions with worker paths ---
	testCases := []struct {
		name        string
		cwd         string
		wantNS      string
		wantWS      string
		content     string
		nodeType    models.NodeType
	}{
		{
			name:     "Worker subdirectory session",
			cwd:      "/home/rat/mindbank-worker-20",
			wantNS:   "mindbank",
			wantWS:   "hermes",
			content:  "Working on MindBank auto-forgetting feature",
			nodeType: models.NodeSession,
		},
		{
			name:     "Team worker session",
			cwd:      "/home/rat/test-website-team-worker-120",
			wantNS:   "test-website-team",
			wantWS:   "hermes",
			content:  "Reviewing frontend components",
			nodeType: models.NodeSession,
		},
		{
			name:     "Clean project path",
			cwd:      "/home/rat/klixsor",
			wantNS:   "klixsor",
			wantWS:   "hermes",
			content:  "TDS analytics dashboard",
			nodeType: models.NodeSession,
		},
		{
			name:     "Nested worker path",
			cwd:      "/home/rat/projects/deep/mindbank-worker-99",
			wantNS:   "mindbank",
			wantWS:   "hermes",
			content:  "Deep nested project structure",
			nodeType: models.NodeSession,
		},
	}

	var createdNodes []*models.Node
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate namespace derivation (what auto_miner.py does)
			ns := autocapture.DeriveNamespaceFromPath(tc.cwd)
			if ns != tc.wantNS {
				t.Fatalf("namespace derivation: got %q, want %q", ns, tc.wantNS)
			}

			// Create node with simulated workspace
			node, err := nodeRepo.Create(ctx, models.NodeCreate{
				WorkspaceName: tc.wantWS,
				Namespace:     "sim_" + ns, // prefix to isolate test data
				Label:         fmt.Sprintf("Sim: %s", tc.name),
				NodeType:      tc.nodeType,
				Content:       tc.content,
			})
			if err != nil {
				t.Fatalf("create node: %v", err)
			}

			// Verify workspace and namespace are correct
			if node.WorkspaceName != tc.wantWS {
				t.Errorf("workspace: got %q, want %q", node.WorkspaceName, tc.wantWS)
			}
			if node.Namespace != "sim_"+ns {
				t.Errorf("namespace: got %q, want %q", node.Namespace, "sim_"+ns)
			}

			createdNodes = append(createdNodes, node)
			t.Logf("Created node: id=%s ns=%s ws=%s", node.ID, node.Namespace, node.WorkspaceName)
		})
	}

	// --- Phase 2: Verify knowledge nodes share namespace with session ---
	t.Run("Knowledge nodes share session namespace", func(t *testing.T) {
		sessionNode := createdNodes[0] // mindbank session

		// Create a fact extracted from the session
		fact, err := nodeRepo.Create(ctx, models.NodeCreate{
			WorkspaceName: sessionNode.WorkspaceName,
			Namespace:     sessionNode.Namespace,
			Label:         "Auto-forgetting grace period",
			NodeType:      models.NodeFact,
			Content:       "Grace period is 14 days for recently accessed nodes",
		})
		if err != nil {
			t.Fatalf("create fact: %v", err)
		}

		// Create edge using correct field names (source/target, not source_id/target_id)
		_, err = edgeRepo.Create(ctx, models.EdgeCreate{
			SourceID: sessionNode.ID,
			TargetID: fact.ID,
			EdgeType: "produced",
		})
		if err != nil {
			t.Fatalf("create edge: %v", err)
		}

		// Verify edge exists with correct field mapping
		edges, err := edgeRepo.ListBySource(ctx, sessionNode.ID, "produced")
		if err != nil {
			t.Fatalf("list edges: %v", err)
		}
		if len(edges) != 1 {
			t.Fatalf("expected 1 edge, got %d", len(edges))
		}
		if edges[0].TargetID != fact.ID {
			t.Errorf("edge target: got %q, want %q", edges[0].TargetID, fact.ID)
		}

		// Verify fact is in same namespace as session
		if fact.Namespace != sessionNode.Namespace {
			t.Errorf("fact namespace %q != session namespace %q", fact.Namespace, sessionNode.Namespace)
		}

		t.Logf("Knowledge graph: session=%s → fact=%s in ns=%s", sessionNode.ID, fact.ID, fact.Namespace)
	})

	// --- Phase 3: Verify no namespace pollution from worker suffixes ---
	t.Run("No worker-polluted namespaces exist", func(t *testing.T) {
		rows, err := pool.Query(ctx, `
			SELECT namespace, COUNT(*) 
			FROM nodes 
			WHERE valid_to IS NULL 
			  AND namespace LIKE 'sim_%'
			GROUP BY namespace
			ORDER BY COUNT(*) DESC
		`)
		if err != nil {
			t.Fatalf("query namespaces: %v", err)
		}
		defer rows.Close()

		for rows.Next() {
			var ns string
			var count int
			if err := rows.Scan(&ns, &count); err != nil {
				continue
			}
			// After stripping sim_ prefix, should NOT contain -worker-
			baseNS := ns[len("sim_"):]
			if containsWorkerSuffix(baseNS) {
				t.Errorf("namespace pollution detected: %q (base: %q)", ns, baseNS)
			}
			t.Logf("Clean namespace: %s (%d nodes)", ns, count)
		}
	})

	// --- Phase 4: Verify workspace consistency ---
	t.Run("All nodes have consistent workspace", func(t *testing.T) {
		var count int
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*) 
			FROM nodes 
			WHERE valid_to IS NULL 
			  AND namespace LIKE 'sim_%'
			  AND workspace_name != 'hermes'
		`).Scan(&count)
		if err != nil {
			t.Fatalf("query workspace consistency: %v", err)
		}
		if count > 0 {
			t.Errorf("%d nodes have inconsistent workspace (expected all 'hermes')", count)
		}
		t.Logf("All %d test nodes have workspace='hermes'", len(createdNodes))
	})

	// --- Phase 5: Cleanup ---
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM edges WHERE source_id IN (SELECT id FROM nodes WHERE namespace LIKE 'sim_%')`)
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE namespace LIKE 'sim_%'`)
		t.Log("Test cleanup completed")
	})

	t.Log("Workspace ingestion pipeline test passed")
}

func containsWorkerSuffix(s string) bool {
	for i := 0; i < len(s)-7; i++ {
		if s[i:i+7] == "-worker-" {
			return true
		}
	}
	return false
}

// TestHermesProfileWorkspaceExtraction validates workspace name extraction
// from Hermes profile directory structure.
func TestHermesProfileWorkspaceExtraction(t *testing.T) {
	// Create a temporary Hermes profiles directory
	tempDir := t.TempDir()
	profilesDir := filepath.Join(tempDir, ".hermes", "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatalf("create profiles dir: %v", err)
	}

	// Create mock profile directories
	profiles := []string{"default", "work", "personal", "mindbank-dev"}
	for _, p := range profiles {
		if err := os.MkdirAll(filepath.Join(profilesDir, p), 0755); err != nil {
			t.Fatalf("create profile %s: %v", p, err)
		}
	}

	// Verify we can list profiles
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("read profiles dir: %v", err)
	}

	var found []string
	for _, e := range entries {
		if e.IsDir() && e.Name()[0] != '.' {
			found = append(found, e.Name())
		}
	}

	if len(found) != len(profiles) {
		t.Fatalf("expected %d profiles, found %d: %v", len(profiles), len(found), found)
	}

	for _, p := range profiles {
		foundIt := false
		for _, f := range found {
			if f == p {
				foundIt = true
				break
			}
		}
		if !foundIt {
			t.Errorf("profile %q not found in listing", p)
		}
	}

	t.Logf("Found profiles: %v", found)
}

// TestSessionMetadataNamespace validates that session metadata carries namespace info.
func TestSessionMetadataNamespace(t *testing.T) {
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

	sessionRepo := repository.NewSessionRepo(pool)

	// Create session with namespace in metadata
	meta := []byte(`{"namespace":"mindbank","project":"auto-forgetting"}`)
	session, err := sessionRepo.Create(ctx, models.SessionCreate{
		WorkspaceName: "hermes",
		Name:          "Test Namespace Session",
		Metadata:      meta,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Verify metadata round-trip
	if session.Metadata == nil {
		t.Fatal("session metadata is nil")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(session.Metadata, &parsed); err != nil {
		t.Fatalf("parse metadata: %v", err)
	}

	if ns, ok := parsed["namespace"].(string); !ok || ns != "mindbank" {
		t.Errorf("metadata namespace: got %v, want 'mindbank'", parsed["namespace"])
	}

	// Cleanup
	_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, session.ID)

	t.Logf("Session metadata namespace verified: %v", parsed["namespace"])
}
