package repository

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SnapshotRepo struct {
	pool *pgxpool.Pool
	// In-memory cache for namespace-filtered snapshots (TTL: 5 min)
	cacheMu sync.RWMutex
	cache   map[string]snapshotCacheEntry
}

type snapshotCacheEntry struct {
	content string
	tokens  int
	time    time.Time
}

func NewSnapshotRepo(pool *pgxpool.Pool) *SnapshotRepo {
	return &SnapshotRepo{pool: pool, cache: make(map[string]snapshotCacheEntry)}
}

const snapshotCacheTTL = 5 * time.Minute

// snapLeadMarkers matches leading list/heading markers on a snippet.
var snapLeadMarkers = regexp.MustCompile(`^([-*>#]+\s*|\d+[.)]\s*)+`)

// Pool returns the underlying connection pool.
func (r *SnapshotRepo) Pool() *pgxpool.Pool {
	return r.pool
}

// Generate builds a snapshot — a pre-computed context blob of the most important nodes.
func (r *SnapshotRepo) Generate(ctx context.Context, workspace, name string, maxTokens int) (string, int, int, error) {
	return r.GenerateFiltered(ctx, workspace, "", name, maxTokens)
}

// GenerateFiltered generates a snapshot optionally filtered by namespace.
func (r *SnapshotRepo) GenerateFiltered(ctx context.Context, workspace, nsFilter, name string, maxTokens int) (string, int, int, error) {
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	// Cap at 4000 tokens to prevent browser/API issues
	if maxTokens > 4000 {
		maxTokens = 4000
	}
	if name == "" {
		name = "default"
	}

	// Get top nodes by importance score with status weighting and confirmation boost
	rows, err := r.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, n.content, n.summary, n.namespace,
			(
				0.25 * COALESCE(1.0 - EXTRACT(EPOCH FROM (now() - n.last_accessed)) / 2592000.0, 0.5)::real
				+ 0.20 * LEAST(n.access_count::real / 100.0, 1.0)::real
				+ 0.15 * LEAST((SELECT COUNT(*)::real / 20.0 FROM edges WHERE source_id = n.id OR target_id = n.id), 1.0)::real
				+ 0.15 * n.importance
				+ 0.10 * CASE n.node_type
					WHEN 'decision' THEN 1.0
					WHEN 'preference' THEN 0.9
					WHEN 'problem' THEN 0.9
					WHEN 'advice' THEN 0.8
					WHEN 'fact' THEN 0.7
					WHEN 'person' THEN 0.7
					WHEN 'project' THEN 0.7
					WHEN 'event' THEN 0.5
					WHEN 'topic' THEN 0.4
					WHEN 'concept' THEN 0.3
					ELSE 0.5
				END::real
				+ 0.10 * CASE n.status
					WHEN 'supported' THEN 1.5
					WHEN 'open' THEN 1.0
					WHEN 'inconclusive' THEN 0.7
					WHEN 'refuted' THEN 0.1
					WHEN 'blocked' THEN 0.0
					ELSE 1.0
				END::real
				+ 0.05 * LEAST(n.confirmation_count::real / 3.0, 1.0)::real
			) AS score
		FROM nodes n
		WHERE n.valid_to IS NULL
		  AND n.status <> 'blocked'
		  -- Exclude container/shell nodes: sessions and events are transcript
		  -- wrappers ("Session mined on DATE"), not memories. The wake-up
		  -- context should be distilled knowledge, not session filenames.
		  AND n.node_type NOT IN ('session', 'event')
		  -- Require some substance so empty/placeholder nodes don't fill it.
		  AND (COALESCE(NULLIF(n.summary, ''), NULLIF(n.content, '')) IS NOT NULL)
		  AND ($1 = '' OR n.workspace_name = $1)
		  AND ($2 = '' OR n.namespace = $2)
		ORDER BY score DESC
		LIMIT 100
	`, workspace, nsFilter)
	if err != nil {
		return "", 0, 0, fmt.Errorf("get top nodes: %w", err)
	}
	defer rows.Close()

	var lines []string
	tokens := 0
	nodeCount := 0
	seen := make(map[string]bool) // deduplicate by label+type

	for rows.Next() {
		var id, label, nodeType, content, summary, namespace string
		var score float32
		if err := rows.Scan(&id, &label, &nodeType, &content, &summary, &namespace, &score); err != nil {
			continue
		}

		// Deduplicate by label (collapses regex-era duplicate-label nodes).
		label = strings.TrimSpace(label)
		seenKey := strings.ToLower(label) + "|" + nodeType
		if seen[seenKey] {
			continue
		}
		seen[seenKey] = true

		// Compact snippet, and omit it when it just repeats the label (common
		// in regex-era nodes where content ≈ label) so the wake-up context
		// stays clean and cheap.
		snip := snapshotSnippet(summary)
		if snip == "" {
			snip = snapshotSnippet(content)
		}
		entry := "- [" + nodeType + "] " + label
		if snip != "" {
			ll, ss := strings.ToLower(label), strings.ToLower(snip)
			if !strings.HasPrefix(ss, ll) && !strings.HasPrefix(ll, ss) {
				entry += " — " + snip
			}
		}
		entryTokens := len(entry) / 4

		if tokens+entryTokens > maxTokens {
			break
		}

		lines = append(lines, entry)
		tokens += entryTokens
		nodeCount++
	}

	if len(lines) == 0 {
		return "No memories stored yet.", 0, 0, nil
	}

	content := "## Key Facts & Decisions\n\n" + strings.Join(lines, "\n")

	// Upsert snapshot
	_, err = r.pool.Exec(ctx, `
		INSERT INTO snapshots (workspace_name, name, content, token_count, node_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_name, name) DO UPDATE
		SET content = $3, token_count = $4, node_count = $5, updated_at = now()
	`, workspace, name, content, tokens, nodeCount)
	if err != nil {
		slog.Warn("failed to save snapshot", "error", err)
	}

	return content, tokens, nodeCount, nil
}

// Get retrieves a pre-computed snapshot.
func (r *SnapshotRepo) Get(ctx context.Context, workspace, name string) (string, int, error) {
	return r.GetFiltered(ctx, workspace, "", name)
}

// SetCache stores a namespace-filtered snapshot in the cache.
func (r *SnapshotRepo) SetCache(workspace, nsFilter, name, content string, tokens int) {
	cacheKey := workspace + ":" + name + ":" + nsFilter
	r.cacheMu.Lock()
	r.cache[cacheKey] = snapshotCacheEntry{content: content, tokens: tokens, time: time.Now()}
	r.cacheMu.Unlock()
}

// InvalidateCache removes a snapshot from the cache.
func (r *SnapshotRepo) InvalidateCache(workspace, nsFilter, name string) {
	cacheKey := workspace + ":" + name + ":" + nsFilter
	r.cacheMu.Lock()
	delete(r.cache, cacheKey)
	r.cacheMu.Unlock()
}

// InvalidateAllNamespaces removes all cached snapshots for a workspace.
func (r *SnapshotRepo) InvalidateAllNamespaces(workspace, name string) {
	r.cacheMu.Lock()
	prefix := workspace + ":" + name + ":"
	for key := range r.cache {
		if strings.HasPrefix(key, prefix) {
			delete(r.cache, key)
		}
	}
	r.cacheMu.Unlock()
}

// InvalidateAllWorkspaces removes all cached snapshots.
func (r *SnapshotRepo) InvalidateAllWorkspaces() {
	r.cacheMu.Lock()
	for key := range r.cache {
		delete(r.cache, key)
	}
	r.cacheMu.Unlock()
}

// GetFiltered retrieves a snapshot, filtering by namespace if specified.
// When namespace is provided, always generates fresh (cached snapshots don't account for namespace).
func (r *SnapshotRepo) GetFiltered(ctx context.Context, workspace, nsFilter, name string) (string, int, error) {
	if nsFilter == "" {
		// No namespace filter — use pre-computed snapshot
		if name == "" {
			name = "default"
		}
		var content string
		var tokenCount int
		err := r.pool.QueryRow(ctx, `
			SELECT content, token_count FROM snapshots
			WHERE workspace_name = $1 AND name = $2
		`, workspace, name).Scan(&content, &tokenCount)
		if err != nil {
			return "", 0, err
		}
		return content, tokenCount, nil
	}

	// Namespace-filtered: check cache first
	cacheKey := workspace + ":" + name + ":" + nsFilter
	r.cacheMu.RLock()
	if entry, ok := r.cache[cacheKey]; ok {
		if time.Since(entry.time) < snapshotCacheTTL {
			r.cacheMu.RUnlock()
			return entry.content, entry.tokens, nil
		}
	}
	r.cacheMu.RUnlock()

	// Cache miss or expired — signal caller to regenerate
	return "", 0, fmt.Errorf("namespace filter requires regeneration")
}

// snapshotSnippet returns a clean, single-line preview: markdown stripped,
// first sentence, capped. Keeps the wake-up context lean and readable.
func snapshotSnippet(s string) string {
	s = strings.NewReplacer("**", "", "__", "", "`", "", "│", " ", "\n", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	// Strip leading list/heading markers ("- ", "### ", "7. ", "2) ").
	s = snapLeadMarkers.ReplaceAllString(s, "")
	if i := strings.IndexAny(s, ".!?"); i > 20 && i < 130 {
		return s[:i+1]
	}
	if len(s) > 130 {
		return strings.TrimSpace(s[:130]) + "…"
	}
	return s
}
