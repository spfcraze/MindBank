package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NamespaceFunc derives a namespace from a path.
type NamespaceFunc func(string) string

// Report captures migration statistics.
type Report struct {
	TotalScanned    int
	WouldUpdate     int
	ActuallyUpdated int
	Skipped         int
	Errors          int
	Details         []string
}

// NamespaceMigrator reclassifies nodes from 'global' to proper namespaces.
type NamespaceMigrator struct {
	pool      *pgxpool.Pool
	deriveNS  NamespaceFunc
}

// NewNamespaceMigrator creates a migrator with the given namespace derivation function.
func NewNamespaceMigrator(pool *pgxpool.Pool, deriveNS NamespaceFunc) *NamespaceMigrator {
	return &NamespaceMigrator{
		pool:     pool,
		deriveNS: deriveNS,
	}
}

// Migrate scans nodes in the given namespace (or 'global' if empty), derives proper namespace from metadata,
// and updates them. If dryRun is true, only reports what would change.
func (m *NamespaceMigrator) Migrate(ctx context.Context, namespace string, dryRun bool) (*Report, error) {
	report := &Report{Details: []string{}}

	// Default to 'global' if no namespace specified
	targetNS := namespace
	if targetNS == "" {
		targetNS = "global"
	}

	// Query nodes in target namespace with metadata
	rows, err := m.pool.Query(ctx, `
		SELECT id, metadata, namespace, label
		FROM nodes
		WHERE valid_to IS NULL
		  AND namespace = $1
		ORDER BY created_at DESC
	`, targetNS)
	if err != nil {
		return nil, fmt.Errorf("query nodes in namespace %s: %w", targetNS, err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var metadata []byte
		var currentNS, label string

		if err := rows.Scan(&id, &metadata, &currentNS, &label); err != nil {
			report.Errors++
			report.Details = append(report.Details, fmt.Sprintf("scan error: %v", err))
			continue
		}

		report.TotalScanned++

		// Extract working_directory from metadata
		var meta map[string]interface{}
		var cwd string
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &meta); err == nil {
				if v, ok := meta["working_directory"].(string); ok {
					cwd = v
				}
			}
		}

		// Derive proper namespace
		newNS := m.deriveNS(cwd)
		if newNS == "" || newNS == "global" {
			// No meaningful namespace derived — skip
			report.Skipped++
			report.Details = append(report.Details,
				fmt.Sprintf("SKIP %s: cwd=%q → ns=%q", label, cwd, newNS))
			continue
		}

		if newNS == currentNS {
			// Already correct
			report.Skipped++
			continue
		}

		report.WouldUpdate++

		if dryRun {
			report.Details = append(report.Details,
				fmt.Sprintf("DRY-RUN %s: %s → %s (cwd=%s)", id[:8], currentNS, newNS, cwd))
			continue
		}

		// Actually update
		_, err = m.pool.Exec(ctx, `
			UPDATE nodes 
			SET namespace = $1, 
			    updated_at = NOW(),
			    metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{migrated_from_namespace}', to_jsonb($2::text))
			WHERE id = $3
		`, newNS, currentNS, id)

		if err != nil {
			report.Errors++
			report.Details = append(report.Details,
				fmt.Sprintf("ERROR updating %s: %v", id[:8], err))
		} else {
			report.ActuallyUpdated++
			report.Details = append(report.Details,
				fmt.Sprintf("UPDATED %s: %s → %s", id[:8], currentNS, newNS))
		}
	}

	return report, nil
}

// HermesWorkspaceExtractor reads Hermes profile configuration to determine
// the correct workspace name for a given namespace.
type HermesWorkspaceExtractor struct {
	hermesDir string
}

// NewHermesWorkspaceExtractor creates an extractor for the given Hermes directory.
func NewHermesWorkspaceExtractor(hermesDir string) *HermesWorkspaceExtractor {
	return &HermesWorkspaceExtractor{hermesDir: hermesDir}
}

// ListProfiles returns all Hermes profile names.
func (e *HermesWorkspaceExtractor) ListProfiles(ctx context.Context) ([]string, error) {
	profilesDir := filepath.Join(e.hermesDir, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			profiles = append(profiles, entry.Name())
		}
	}
	return profiles, nil
}

// GetWorkspaceForNamespace returns the workspace name for a namespace.
// It checks the mindbank-namespaces.json mapping file first, then falls back
// to profile name matching or "hermes" default.
func (e *HermesWorkspaceExtractor) GetWorkspaceForNamespace(ctx context.Context, namespace string) (string, error) {
	// Read mindbank-namespaces.json mapping if it exists
	mappingPath := filepath.Join(e.hermesDir, "mindbank-namespaces.json")
	if data, err := os.ReadFile(mappingPath); err == nil {
		var mappings map[string]string
		if err := json.Unmarshal(data, &mappings); err == nil {
			if ws, ok := mappings[namespace]; ok {
				return ws, nil
			}
		}
	}

	// Check if namespace matches a profile name
	profiles, err := e.ListProfiles(ctx)
	if err == nil {
		for _, p := range profiles {
			if strings.EqualFold(p, namespace) {
				return p, nil
			}
		}
	}

	// Default fallback
	return "hermes", nil
}

// GetActiveProfile attempts to determine which Hermes profile is currently active.
// Currently checks for profile-specific state files or falls back to "default".
func (e *HermesWorkspaceExtractor) GetActiveProfile(ctx context.Context) (string, error) {
	// Check if there's an active profile indicator
	activeFile := filepath.Join(e.hermesDir, ".active_profile")
	if data, err := os.ReadFile(activeFile); err == nil {
		profile := strings.TrimSpace(string(data))
		if profile != "" {
			return profile, nil
		}
	}

	// Fallback: check which profile has the most recent session activity
	profiles, err := e.ListProfiles(ctx)
	if err != nil || len(profiles) == 0 {
		return "hermes", nil
	}

	// Return first profile as default (could be enhanced with mtime comparison)
	return profiles[0], nil
}
