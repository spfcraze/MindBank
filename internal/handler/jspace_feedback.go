package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/repository"
	"mindbank/internal/workspace"
)

// JSpaceFeedback runs the workspace → memory feedback loop (toggleable).
//
// Every interval it recomputes the current workspace top-K (same composite
// score as the JSPACE tab) and marks those memories `workspace_active` in
// metadata, clearing the flag everywhere else. Downstream effects:
//   - forgetting service exempts workspace-active memories from TTL expiry
//   - search/snapshot rank workspace-active memories slightly higher
//
// The loop only mutates activation state + ranking signals — it never writes
// JSPACE items into the graph, and its importance effect is indirect (active
// memories get recalled more, so existing ReinforceRecall nudges apply).
// Disabled via POST /api/v1/jspace/feedback {"enabled": false} (persisted).
type JSpaceFeedback struct {
	pool     *pgxpool.Pool
	settings *repository.SettingsRepo

	mu          sync.Mutex
	enabled     bool
	lastRun     time.Time
	lastMarked  int
	lastCleanup time.Time

	interval time.Duration
	k        int
}

const feedbackSettingKey = "jspace_feedback_enabled"

// workspaceTopQuery is shared by the tab and the feedback loop.
const workspaceTopQuery = `
	SELECT n.id
	FROM nodes n
	WHERE n.valid_to IS NULL
	  AND n.node_type NOT IN ('session', 'event')
	  AND n.workspace_name <> '___test___'
	  AND n.namespace !~* '^(test|sim|repair|relink|mcp_test|header-test)([_-]|$)'
	  AND n.namespace !~* 'test$'
	  AND n.label !~* '^(memory(:|$)|untitled|no metadata)'
	ORDER BY (n.importance * (1 + n.access_count) * (1 + n.confirmation_count) *
	          (1.0 / (1.0 + EXTRACT(EPOCH FROM (now() - GREATEST(n.created_at, coalesce(n.updated_at, n.created_at)))) / 86400.0 / 7.0))) DESC
	LIMIT $1`

// NewJSpaceFeedback creates the loop, honoring the persisted toggle.
func NewJSpaceFeedback(pool *pgxpool.Pool, settings *repository.SettingsRepo, interval time.Duration) *JSpaceFeedback {
	f := &JSpaceFeedback{
		pool:     pool,
		settings: settings,
		interval: interval,
		k:        25, // workspace capacity (~25 concepts, matching the tab)
	}
	if settings != nil {
		f.enabled = settings.GetBool(context.Background(), feedbackSettingKey)
	}
	return f
}

// Enabled reports whether the loop is on.
func (f *JSpaceFeedback) Enabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled
}

// SetEnabled flips the loop and persists the setting.
func (f *JSpaceFeedback) SetEnabled(ctx context.Context, on bool) error {
	f.mu.Lock()
	f.enabled = on
	f.mu.Unlock()
	if f.settings != nil {
		v := "false"
		if on {
			v = "true"
		}
		return f.settings.Set(ctx, feedbackSettingKey, v)
	}
	return nil
}

// Status returns a snapshot for the UI.
func (f *JSpaceFeedback) Status() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[string]any{
		"enabled":     f.enabled,
		"interval_min": f.interval.Minutes(),
		"top_k":       f.k,
		"last_run":    f.lastRun,
		"last_marked": f.lastMarked,
	}
}

// Run ticks the loop until ctx is cancelled.
func (f *JSpaceFeedback) Run(ctx context.Context) {
	if f.interval <= 0 {
		f.interval = time.Hour
	}
	// First run shortly after startup so the flags exist even before the
	// first interval elapses.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
			f.tick(ctx)
		}
	}()
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.tick(ctx)
		}
	}
}

// tick recomputes workspace-active flags for the current top-K, renewing
// each memory's lease (JavaSpaces leasing model: membership is timed, not a
// boolean) and recording entered/left transitions in the event stream.
func (f *JSpaceFeedback) tick(ctx context.Context) {
	if !f.Enabled() {
		return
	}
	// 1. Snapshot the previously-leased set (id -> label).
	prev := map[string]string{}
	rows, err := f.pool.Query(ctx,
		`SELECT id, coalesce(label, '') FROM nodes WHERE valid_to IS NULL
		  AND lower(coalesce(metadata->>'workspace_active','false')) IN ('true','t','yes','1')`)
	if err != nil {
		slog.Warn("jspace feedback prev query", "error", err)
		return
	}
	for rows.Next() {
		var id, label string
		if rows.Scan(&id, &label) == nil {
			prev[id] = label
		}
	}
	rows.Close()

	// 2. Clear every existing flag + lease so non-top memories decay out.
	if _, err := f.pool.Exec(ctx,
		`UPDATE nodes SET metadata = metadata - 'workspace_active' - 'workspace_lease_until'
		 WHERE metadata ? 'workspace_active'`); err != nil {
		slog.Warn("jspace feedback clear flags", "error", err)
		return
	}

	// 3. Mark the current top-K with a fresh lease.
	rows, err = f.pool.Query(ctx, workspaceTopQuery, f.k)
	if err != nil {
		slog.Warn("jspace feedback top query", "error", err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) > 0 {
		if _, err := f.pool.Exec(ctx, `
			UPDATE nodes SET metadata = jsonb_set(
			  jsonb_set(coalesce(metadata, '{}'), '{workspace_active}', 'true'::jsonb),
			  '{workspace_lease_until}', to_jsonb(now() + make_interval(secs => $2)))
			WHERE id = ANY($1) AND valid_to IS NULL`, ids, int64(2*f.interval.Seconds())); err != nil {
			slog.Warn("jspace feedback mark flags", "error", err)
			return
		}
	}

	// 4. Event stream: entered (newly leased) and left (lease lapsed).
	current := map[string]bool{}
	for _, id := range ids {
		current[id] = true
		if _, was := prev[id]; !was {
			workspace.Record(ctx, f.pool, "entered", id, "", "", map[string]any{"reason": "top-k lease"})
		}
	}
	for id, label := range prev {
		if !current[id] {
			workspace.Record(ctx, f.pool, "left", id, label, "", map[string]any{"reason": "lease lapsed"})
		}
	}

	// Retention: the event stream is a capture log, not permanent storage —
	// prune entries older than 30 days at most once per day.
	f.mu.Lock()
	needCleanup := time.Since(f.lastCleanup) > 24*time.Hour
	if needCleanup {
		f.lastCleanup = time.Now()
	}
	f.mu.Unlock()
	if needCleanup {
		if _, err := f.pool.Exec(ctx,
			`DELETE FROM workspace_events WHERE created_at < now() - interval '30 days'`); err != nil {
			slog.Warn("jspace events retention", "error", err)
		}
	}

	f.mu.Lock()
	f.lastRun = time.Now()
	f.lastMarked = len(ids)
	f.mu.Unlock()
	slog.Info("jspace feedback cycle", "marked", len(ids), "left", len(prev)-len(current), "interval", f.interval.String())
}

// StatusHandler handles GET /api/v1/jspace/feedback
func (f *JSpaceFeedback) StatusHandler(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, f.Status())
}

// ToggleHandler handles POST /api/v1/jspace/feedback
func (f *JSpaceFeedback) ToggleHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON: expected {\"enabled\": true|false}")
		return
	}
	if err := f.SetEnabled(r.Context(), req.Enabled); err != nil {
		respondError(w, 500, "failed to persist setting: "+err.Error())
		return
	}
	respondJSON(w, 200, f.Status())
}
