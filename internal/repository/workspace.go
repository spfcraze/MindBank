package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkspaceRepo struct {
	pool *pgxpool.Pool
}

func NewWorkspaceRepo(pool *pgxpool.Pool) *WorkspaceRepo {
	return &WorkspaceRepo{pool: pool}
}

// List returns all workspace names from:
//  1. Database nodes table (existing workspaces with data)
//  2. Hermes profiles directory (~/.hermes/profiles/*)
//  3. The "default" workspace (always available)
func (r *WorkspaceRepo) List(ctx context.Context) ([]string, error) {
	seen := make(map[string]bool)

	// 1. From database nodes
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT workspace_name 
		FROM nodes 
		WHERE valid_to IS NULL 
		  AND workspace_name IS NOT NULL
		ORDER BY workspace_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces from db: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ws string
		if err := rows.Scan(&ws); err != nil {
			continue
		}
		seen[ws] = true
	}

	// 2. From Hermes profiles directory
	hermesDir := os.Getenv("HOME")
	if hermesDir == "" {
		// Fallback: try to get home from /etc/passwd or current dir
		if h, err := os.UserHomeDir(); err == nil {
			 hermesDir = h
		}
	}
	if hermesDir != "" {
		profilesDir := filepath.Join(hermesDir, ".hermes", "profiles")
		entries, err := os.ReadDir(profilesDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() && !isHidden(entry.Name()) {
					seen[entry.Name()] = true
				}
			}
		}
	}

	// 3. Always include "default"
	seen["default"] = true

	// Convert to sorted slice
	var workspaces []string
	for ws := range seen {
		workspaces = append(workspaces, ws)
	}
	sort.Strings(workspaces)

	return workspaces, nil
}

func isHidden(name string) bool {
	return len(name) > 0 && name[0] == '.'
}
