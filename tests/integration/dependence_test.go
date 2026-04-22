package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/repository"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	var err error
	dsn := os.Getenv("MB_DB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}
	testPool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	testPool.Close()
	os.Exit(code)
}

func createTestNode(t *testing.T, pool *pgxpool.Pool, label, nodeType, ns string) string {
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('hermes', $1, $2, $3, 'test content', 'test summary', 0.5)
		RETURNING id
	`, ns, label, nodeType).Scan(&id)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	return id
}

func createTestEdge(t *testing.T, pool *pgxpool.Pool, src, tgt, edgeType string, weight float32) {
	_, err := pool.Exec(context.Background(), `
		INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight)
		VALUES ('hermes', $1, $2, $3, $4)
	`, src, tgt, edgeType, weight)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
}

func cleanupTestData(t *testing.T, pool *pgxpool.Pool, ids []string) {
	if len(ids) == 0 {
		return
	}
	_, _ = pool.Exec(context.Background(), `DELETE FROM edges WHERE source_id = ANY($1) OR target_id = ANY($1)`, ids)
	_, _ = pool.Exec(context.Background(), `DELETE FROM node_embeddings WHERE node_id = ANY($1)`, ids)
	_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE id = ANY($1)`, ids)
}

func TestGetDependenceGraph(t *testing.T) {
	repo := repository.NewDependenceRepo(testPool)
	ctx := context.Background()

	ns := "test-dependence"
	// A depends_on B, B depends_on C
	nodeA := createTestNode(t, testPool, "Node A", "decision", ns)
	nodeB := createTestNode(t, testPool, "Node B", "fact", ns)
	nodeC := createTestNode(t, testPool, "Node C", "fact", ns)
	createTestEdge(t, testPool, nodeB, nodeA, "depends_on", 1.0)
	createTestEdge(t, testPool, nodeC, nodeB, "depends_on", 0.8)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC})

	nodes, edges, modes, criticalDepth, coverage, blindSpots, err := repo.GetDependenceGraph(ctx, nodeA, []string{"depends_on"}, 3, 0.1)
	if err != nil {
		t.Fatalf("GetDependenceGraph: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
	if criticalDepth != 2 {
		t.Errorf("expected critical_depth=2, got %d", criticalDepth)
	}
	if coverage != 1.0 {
		t.Errorf("expected coverage=1.0, got %f", coverage)
	}
	if len(blindSpots) != 0 {
		t.Errorf("expected 0 blind spots, got %d", len(blindSpots))
	}
	if len(modes) != 2 {
		t.Errorf("expected 2 influence modes, got %d", len(modes))
	}
	// B should have higher influence than C (closer to seed)
	if modes[0].NodeID != nodeB {
		t.Errorf("expected top influence to be node B (%s), got %s", nodeB, modes[0].NodeID)
	}
}

func TestSynchronizeNode(t *testing.T) {
	repo := repository.NewDependenceRepo(testPool)
	ctx := context.Background()

	ns := "test-sync"
	nodeA := createTestNode(t, testPool, "Node A", "fact", ns)
	nodeB := createTestNode(t, testPool, "Node B", "decision", ns)
	nodeC := createTestNode(t, testPool, "Node C", "advice", ns)
	createTestEdge(t, testPool, nodeA, nodeB, "supports", 1.0)
	createTestEdge(t, testPool, nodeB, nodeC, "depends_on", 0.9)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC})

	affected, confUpdates, resolved, nodes, edges, err := repo.SynchronizeNode(ctx, nodeA, 3, false)
	if err != nil {
		t.Fatalf("SynchronizeNode: %v", err)
	}

	if affected != 2 {
		t.Errorf("expected 2 affected nodes, got %d", affected)
	}
	if len(confUpdates) != 2 {
		t.Errorf("expected 2 confidence updates, got %d", len(confUpdates))
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved contradictions, got %d", len(resolved))
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes in propagation graph, got %d", len(nodes))
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges in propagation graph, got %d", len(edges))
	}
}

func TestGetObservability(t *testing.T) {
	repo := repository.NewDependenceRepo(testPool)
	ctx := context.Background()

	ns := "test-obs-" + fmt.Sprintf("%d", time.Now().UnixNano())
	nodeA := createTestNode(t, testPool, "Node A", "fact", ns)
	nodeB := createTestNode(t, testPool, "Node B", "decision", ns)
	nodeC := createTestNode(t, testPool, "Node C", "advice", ns)
	nodeD := createTestNode(t, testPool, "Node D", "fact", ns) // isolated
	createTestEdge(t, testPool, nodeA, nodeB, "supports", 1.0)
	createTestEdge(t, testPool, nodeB, nodeC, "depends_on", 0.9)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC, nodeD})

	observable, total, ratio, coverage, err := repo.GetObservability(ctx, ns, []string{nodeA})
	if err != nil {
		t.Fatalf("GetObservability: %v", err)
	}

	if total != 4 {
		t.Errorf("expected total=4, got %d", total)
	}
	if observable != 3 {
		t.Errorf("expected observable=3 (A,B,C), got %d", observable)
	}
	expectedRatio := 3.0 / 4.0
	if ratio != expectedRatio {
		t.Errorf("expected ratio=%f, got %f", expectedRatio, ratio)
	}
	if coverage["decision"] != 1.0 {
		t.Errorf("expected decision coverage=1.0, got %f", coverage["decision"])
	}
}
