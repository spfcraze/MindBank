package dedup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ComputeHash generates a SHA-256 hash of normalized content.
func ComputeHash(label, content, summary string) string {
	normalized := strings.ToLower(strings.TrimSpace(label) + "|" + strings.TrimSpace(content) + "|" + strings.TrimSpace(summary))
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:])
}

// CheckDuplicate returns the existing node ID if the hash already exists.
func CheckDuplicate(ctx context.Context, pool *pgxpool.Pool, hash string) (string, error) {
	var nodeID string
	err := pool.QueryRow(ctx,
		"SELECT node_id FROM node_hashes WHERE hash = $1", hash).Scan(&nodeID)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("check duplicate: %w", err)
	}
	return nodeID, nil
}

// StoreHash saves a hash -> node_id mapping. On conflict the mapping is
// repointed to the new node: DO NOTHING left stale hash->dead-node rows in
// place forever, so once a node was superseded, every future save of that
// content skipped dedup and accumulated duplicates.
func StoreHash(ctx context.Context, pool *pgxpool.Pool, hash, nodeID string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO node_hashes (hash, node_id) VALUES ($1, $2)
		 ON CONFLICT (hash) DO UPDATE SET node_id = EXCLUDED.node_id, created_at = now()`,
		hash, nodeID)
	return err
}
