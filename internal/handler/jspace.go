package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JSpaceHandler serves the JSPACE (global workspace) tab: a derived view over
// the Hermes fleet (state.db) and MindBank memory metrics. The workspace is a
// RECOMPUTABLE layer — nothing here writes into the nodes graph, so the
// transient "what's lit up now" data never contaminates long-term memory.
type JSpaceHandler struct {
	pool *pgxpool.Pool

	mu            sync.Mutex
	fleetCache    []byte
	fleetAt       time.Time
	fleetCacheKey string
	liveCache     []byte
	liveAt        time.Time
	rawCache      []byte
	rawAt         time.Time
	prevJthought  []string
	prevAt        time.Time
}

// workspaceMem is one memory in the workspace top-K.
type mem struct {
	ID                string    `json:"id"`
	Label             string    `json:"label"`
	NodeType          string    `json:"node_type"`
	Namespace         string    `json:"namespace"`
	Importance        float64   `json:"importance"`
	AccessCount       int       `json:"access_count"`
	ConfirmationCount int       `json:"confirmation_count"`
	EpistemicLabel    string    `json:"epistemic_label"`
	Degree            int       `json:"degree"`
	CreatedAt         time.Time `json:"created_at"`
	LeaseUntil        string    `json:"lease_until,omitempty"`
	Score             float64   `json:"score"`
}

// NewJSpaceHandler creates the JSPACE overview handler.
func NewJSpaceHandler(pool *pgxpool.Pool) *JSpaceHandler {
	return &JSpaceHandler{pool: pool}
}

// testNoise filters test/sim/repair namespaces that pollute the workspace view.
func testNoiseFilter(includeTests bool) string {
	if includeTests {
		return ` AND n.label !~* '^(memory(:|$)|untitled|no metadata)' `
	}
	return ` AND n.workspace_name <> '___test___'
	  AND n.namespace !~* '^(test|sim|repair|relink|mcp_test|header-test)([_-]|$)'
	  AND n.namespace !~* 'test$'
	  AND n.label !~* '^(memory(:|$)|untitled|no metadata)' `
}

// fleetStatus returns cached fleet JSON from the python stdlib helper.
func (h *JSpaceHandler) fleetStatus(ctx context.Context) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fleetCache != nil && time.Since(h.fleetAt) < 30*time.Second {
		return h.fleetCache, nil
	}
	helper := fleetHelperPath()
	if helper == "" {
		return []byte(`{"active_total":0,"active_24h":0,"busy_last_hour":0,"sessions":[]}`), nil
	}
	out, err := exec.CommandContext(ctx, "python3", helper).Output()
	if err != nil {
		slog.Warn("jspace fleet helper failed", "error", err)
		return []byte(`{"active_total":0,"active_24h":0,"busy_last_hour":0,"sessions":[]}`), nil
	}
	h.fleetCache, h.fleetAt = out, time.Now()
	return out, nil
}

// fleetHelperPath locates scripts/hermes-fleet-status.py next to the binary
// or under $HOME/mindbank.
func fleetHelperPath() string {
	candidates := []string{
		filepath.Join("scripts", "hermes-fleet-status.py"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "mindbank", "scripts", "hermes-fleet-status.py"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Overview handles GET /api/v1/jspace/overview
func (h *JSpaceHandler) Overview(w http.ResponseWriter, r *http.Request) {
	k := 25 // workspace capacity is ~25 active concepts (Anthropic J-space)
	if v := r.URL.Query().Get("k"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			k = n
		}
	}
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 24*30 {
			hours = n
		}
	}
	includeTests := r.URL.Query().Get("include_tests") == "1"
	ctx := r.Context()

	noise := testNoiseFilter(includeTests)

	// --- workspace top-K by composite score with recency decay ---
	var top []mem
	rows, err := h.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, coalesce(n.namespace, 'global'),
		       n.importance, n.access_count, n.confirmation_count,
		       coalesce(n.epistemic_label, ''),
		       (SELECT count(*) FROM edges e WHERE (e.source_id = n.id OR e.target_id = n.id) AND e.valid_to IS NULL),
		       n.created_at, coalesce(n.metadata->>'workspace_lease_until', '') AS lease_until,
		       (n.importance * (1 + n.access_count) * (1 + n.confirmation_count) *
		         (1.0 / (1.0 + EXTRACT(EPOCH FROM (now() - GREATEST(n.created_at, coalesce(n.updated_at, n.created_at)))) / 86400.0 / 7.0)))
		       AS score
		FROM nodes n
		WHERE n.valid_to IS NULL
		  AND n.node_type NOT IN ('session', 'event')`+noise+`
		ORDER BY score DESC
		LIMIT $1`, k)
	if err != nil {
		respondError(w, 500, "workspace query failed")
		return
	}
	for rows.Next() {
		var m mem
		if err := rows.Scan(&m.ID, &m.Label, &m.NodeType, &m.Namespace, &m.Importance,
			&m.AccessCount, &m.ConfirmationCount, &m.EpistemicLabel, &m.Degree,
			&m.CreatedAt, &m.LeaseUntil, &m.Score); err != nil {
			continue
		}
		top = append(top, m)
	}
	rows.Close()
	if top == nil {
		top = []mem{}
	}
	// Capacity calibration: the number of memories that carry ~95% of the
	// workspace score mass (occupancy knee — mirrors the ~25-concept plateau).
	occ := 0
	{
		total := 0.0
		for _, m := range top {
			total += m.Score
		}
		cum := 0.0
		for i, m := range top {
			cum += m.Score
			if total > 0 && cum >= total*0.95 {
				occ = i + 1
				break
			}
		}
		if occ == 0 && len(top) > 0 {
			occ = len(top)
		}
	}
	// Workspace families: cluster top-K memories by embedding similarity —
	// related concepts co-reside in the workspace, unrelated ones displace
	// each other (J-space capacity is relationship-dependent).
	families := clusterWorkspaceFamilies(ctx, h.pool, top)

	// --- recent broadcasts (what the fleet just learned) ---
	type bc struct {
		ID             string    `json:"id"`
		Label          string    `json:"label"`
		NodeType       string    `json:"node_type"`
		Namespace      string    `json:"namespace"`
		Importance     float64   `json:"importance"`
		EpistemicLabel string    `json:"epistemic_label"`
		CreatedAt      time.Time `json:"created_at"`
	}
	var broadcasts []bc
	rows, err = h.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, coalesce(n.namespace, 'global'),
		       n.importance, coalesce(n.epistemic_label, ''), n.created_at
		FROM nodes n
		WHERE n.valid_to IS NULL
		  AND n.node_type NOT IN ('session', 'event')
		  AND n.created_at > now() - make_interval(hours => $1)`+noise+`
		ORDER BY n.created_at DESC
		LIMIT 100`, hours)
	if err != nil {
		respondError(w, 500, "broadcast query failed")
		return
	}
	for rows.Next() {
		var b bc
		if err := rows.Scan(&b.ID, &b.Label, &b.NodeType, &b.Namespace, &b.Importance,
			&b.EpistemicLabel, &b.CreatedAt); err != nil {
			continue
		}
		broadcasts = append(broadcasts, b)
	}
	rows.Close()
	if broadcasts == nil {
		broadcasts = []bc{}
	}

	// --- integration health ---
	var edges, isolated, orphans, reinforced int64
	err = h.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM edges WHERE valid_to IS NULL),
		  (SELECT count(*) FROM nodes n WHERE n.valid_to IS NULL
		     AND n.node_type NOT IN ('session','event')
		     AND NOT EXISTS (SELECT 1 FROM edges e WHERE (e.source_id = n.id OR e.target_id = n.id) AND e.valid_to IS NULL)),
		  (SELECT count(*) FROM edges e
		     LEFT JOIN nodes s ON s.id = e.source_id AND s.valid_to IS NULL
		     LEFT JOIN nodes t ON t.id = e.target_id AND t.valid_to IS NULL
		     WHERE e.valid_to IS NULL AND (s.id IS NULL OR t.id IS NULL)),
		  (SELECT count(*) FROM nodes WHERE valid_to IS NULL
		     AND node_type NOT IN ('session','event') AND confirmation_count > 0)`).Scan(&edges, &isolated, &orphans, &reinforced)
	if err != nil {
		respondError(w, 500, "integration query failed")
		return
	}

	fleetRaw, _ := h.fleetStatus(ctx)
	var fleet any
	if err := json.Unmarshal(fleetRaw, &fleet); err != nil {
		fleet = map[string]any{}
	}

	events := recentWorkspaceEvents(ctx, h.pool, 50)
	respondJSON(w, 200, map[string]any{
		"fleet":       fleet,
		"workspace":   map[string]any{"total": len(top), "top": top, "occupancy": occ, "families": families},
		"broadcasts":  map[string]any{"hours": hours, "count": len(broadcasts), "recent": broadcasts},
		"events":      events,
		"integration": map[string]any{"edges": edges, "isolated_nodes": isolated, "orphan_edges": orphans, "reinforced_nodes": reinforced, "consumed": consumedCount(ctx, h.pool)},
	})
}

// Live handles GET /api/v1/jspace/live — what the fleet is reasoning about
// RIGHT NOW (recent messages incl. model `reasoning` traces from state.db).
// Cached ~10s; window default 5 minutes.
func (h *JSpaceHandler) Live(w http.ResponseWriter, r *http.Request) {
	window := 600 // wide enough to cover tool loops that pause >5min between messages
	if v := r.URL.Query().Get("window"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 30 && n <= 7200 {
			window = n
		}
	}
	h.mu.Lock()
	if h.liveCache != nil && time.Since(h.liveAt) < 10*time.Second {
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(h.liveCache)
		return
	}
	h.mu.Unlock()

	helper := liveHelperPath()
	if helper == "" {
		respondError(w, 500, "live helper not found")
		return
	}
	out, err := exec.CommandContext(r.Context(), "python3", helper, strconv.Itoa(window)).Output()
	if err != nil {
		respondError(w, 502, "live reasoning scan failed")
		return
	}
	// JADR-style diagnostics: drift (recognition shift vs previous window) and
	// recognition↔behavior alignment (reasoning concepts that became memory
	// broadcasts in the window).
	payload := map[string]any{}
	if json.Unmarshal(out, &payload) == nil {
		// current j-thought term set
		curTerms := []string{}
		if jt, ok := payload["jthought"].([]any); ok {
			for _, item := range jt {
				if m, ok := item.(map[string]any); ok {
					if t, ok := m["term"].(string); ok {
						curTerms = append(curTerms, t)
					}
				}
			}
		}
		h.mu.Lock()
		drift := 0.0
		prevCount := 0
		if len(h.prevJthought) > 0 && len(curTerms) > 0 && !h.prevAt.IsZero() {
			prevCount = len(h.prevJthought)
			// 1 - Jaccard over the top-8 term sets
			curSet := map[string]bool{}
			for _, t := range curTerms[:min(8, len(curTerms))] {
				curSet[t] = true
			}
			inter, union := 0, len(curSet)
			for _, t := range h.prevJthought[:min(8, len(h.prevJthought))] {
				union++
				if curSet[t] {
					inter++
				}
			}
			if union > 0 {
				drift = 1.0 - float64(inter)/float64(union)
			}
		}
		h.prevJthought, h.prevAt = append([]string(nil), curTerms...), time.Now()
		h.mu.Unlock()
		payload["drift"] = map[string]any{
			"value":      round2(drift),
			"label":      driftLabel(drift),
			"prev_count": prevCount,
			"current_count": len(curTerms),
		}
		payload["alignment"] = liveAlignment(r.Context(), h.pool, curTerms, window)
		payload["memory_writes"] = liveMemoryWrites(r.Context(), h.pool, window)
		out, _ = json.Marshal(payload)
	}
	h.mu.Lock()
	h.liveCache, h.liveAt = out, time.Now()
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// driftLabel buckets the workspace drift into stable/shifting/major.
func driftLabel(d float64) string {
	switch {
	case d < 0.33:
		return "stable"
	case d < 0.66:
		return "shifting"
	default:
		return "major"
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}


// liveAlignment measures how much of the fleet's current reasoning (recognition)
// materialized as durable memory broadcasts (behavior) in the window — the
// JADR recognition-vs-behavior gap, applied to the fleet.
func liveAlignment(ctx context.Context, pool *pgxpool.Pool, terms []string, window int) map[string]any {
	if len(terms) == 0 {
		return map[string]any{"score": 0, "recognized": 0, "materialized": 0, "lost": []string{}, "unrecognized_broadcasts": 0}
	}
	rows, err := pool.Query(ctx, `
		SELECT lower(n.label)
		FROM nodes n
		WHERE n.valid_to IS NULL
		  AND n.node_type NOT IN ('session', 'event')
		  AND n.workspace_name <> '___test___'
		  AND n.namespace !~* '^(test|sim|repair|relink|mcp_test|header-test)([_-]|$)'
		  AND n.created_at > now() - make_interval(secs => $1)`, window)
	if err != nil {
		return map[string]any{"score": 0, "recognized": len(terms), "materialized": 0, "lost": terms, "unrecognized_broadcasts": 0}
	}
	var labels []string
	for rows.Next() {
		var l string
		if rows.Scan(&l) == nil {
			labels = append(labels, l)
		}
	}
	rows.Close()

	matched := map[string]bool{}
	for _, t := range terms {
		for _, l := range labels {
			if strings.Contains(l, t) {
				matched[t] = true
				break
			}
		}
	}
	lost := []string{}
	for _, t := range terms {
		if !matched[t] {
			lost = append(lost, t)
		}
	}
	unrecognized := 0
	for _, l := range labels {
		hit := false
		for _, t := range terms {
			if strings.Contains(l, t) {
				hit = true
				break
			}
		}
		if !hit {
			unrecognized++
		}
	}
	return map[string]any{
		"recognized":             len(terms),
		"materialized":           len(matched),
		"score":                  round2(float64(len(matched)) / float64(len(terms))),
		"lost":                   lost,
		"broadcast_count":        len(labels),
		"unrecognized_broadcasts": unrecognized,
	}
}

// liveHelperPath locates scripts/hermes-live-reasoning.py.
func liveHelperPath() string {
	candidates := []string{filepath.Join("scripts", "hermes-live-reasoning.py")}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "mindbank", "scripts", "hermes-live-reasoning.py"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}


// clusterWorkspaceFamilies groups workspace memories by embedding similarity.
// Nodes without embeddings become singleton families.
func clusterWorkspaceFamilies(ctx context.Context, pool *pgxpool.Pool, top []mem) []map[string]any {
	if len(top) == 0 {
		return []map[string]any{}
	}
	ids := make([]string, 0, len(top))
	for _, m := range top {
		ids = append(ids, m.ID)
	}
	emb := map[string][]float64{}
	rows, err := pool.Query(ctx,
		`SELECT node_id, embedding::text FROM node_embeddings WHERE node_id = ANY($1)`, ids)
	if err == nil {
		for rows.Next() {
			var id, vec string
			if rows.Scan(&id, &vec) == nil {
				if v := parseVectorString(vec); len(v) > 0 {
					emb[id] = v
				}
			}
		}
		rows.Close()
	}

	// Order members by score desc, then greedily cluster (cosine >= 0.7).
	const simThreshold = 0.7
	var families []map[string]any
	assigned := map[string]bool{}
	for _, anchor := range top {
		if assigned[anchor.ID] {
			continue
		}
		family := []map[string]any{{"id": anchor.ID, "label": anchor.Label, "score": anchor.Score}}
		assigned[anchor.ID] = true
		anchorVec := emb[anchor.ID]
		if anchorVec != nil {
			for _, m := range top {
				if assigned[m.ID] {
					continue
				}
				mv, ok := emb[m.ID]
				if !ok {
					continue
				}
				if cosineSimilarity(anchorVec, mv) >= simThreshold {
					family = append(family, map[string]any{"id": m.ID, "label": m.Label, "score": m.Score})
					assigned[m.ID] = true
				}
			}
		}
		families = append(families, map[string]any{
			"label":  anchor.Label,
			"count":  len(family),
			"members": family,
		})
	}
	return families
}


// Raw handles GET /api/v1/jspace/raw — MindBank-independent session activity
// (state.db tool events) plus the ingestion funnel: how much true session
// activity survived MindBank's curation pipeline. Cached ~10s.
func (h *JSpaceHandler) Raw(w http.ResponseWriter, r *http.Request) {
	window := 3600
	if v := r.URL.Query().Get("window"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 60 && n <= 86400 {
			window = n
		}
	}
	cacheKey := "raw:" + strconv.Itoa(window)
	h.mu.Lock()
	if h.rawCache != nil && h.fleetCacheKey == cacheKey && time.Since(h.rawAt) < 10*time.Second {
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(h.rawCache)
		return
	}
	h.mu.Unlock()

	helper := rawEventsHelperPath()
	if helper == "" {
		respondError(w, 500, "raw events helper not found")
		return
	}
	out, err := exec.CommandContext(r.Context(), "python3", helper, strconv.Itoa(window)).Output()
	if err != nil {
		respondError(w, 502, "raw events scan failed")
		return
	}
	raw := map[string]any{}
	if json.Unmarshal(out, &raw) != nil {
		respondError(w, 502, "raw events parse failed")
		return
	}

	// Ingestion funnel: curated side from MindBank.
	sessionsActive := 0.0
	sessionsWithEvents := 0.0
	memoryCalls := 0.0
	if v, ok := raw["active_sessions"].(float64); ok {
		sessionsActive = v
	}
	if v, ok := raw["sessions_with_events"].(float64); ok {
		sessionsWithEvents = v
	}
	if v, ok := raw["memory_write_calls"].(float64); ok {
		memoryCalls = v
	}
	var memories, sessionsPersisted int64
	_ = h.pool.QueryRow(r.Context(), `
		SELECT
		  (SELECT count(*) FROM nodes WHERE valid_to IS NULL
		     AND node_type NOT IN ('session','event')
		     AND workspace_name <> '___test___'
		     AND namespace !~* '^(test|sim|repair|relink|mcp_test|header-test)([_-]|$)'
		     AND created_at > now() - make_interval(secs => $1)),
		  (SELECT count(DISTINCT metadata->>'source_session') FROM nodes
		     WHERE created_at > now() - make_interval(secs => $1)
		       AND metadata ? 'source_session')`, window).Scan(&memories, &sessionsPersisted)
	if err != nil {
		slog.Warn("jspace funnel query", "error", err)
	}
	coverage := 0.0
	if sessionsActive > 0 {
		coverage = float64(sessionsPersisted) / sessionsActive
	}
	throughput := 0.0
	if sessionsWithEvents > 0 {
		throughput = float64(memories) / sessionsWithEvents
	}

	funnel := map[string]any{
		"window":            window,
		"sessions_active":   sessionsActive,
		"sessions_with_events": sessionsWithEvents,
		"memory_write_calls": memoryCalls,
		"memories_persisted": memories,
		"sessions_persisted": sessionsPersisted,
		"persistence_coverage": round2(coverage), // fraction of active sessions that persisted
		"throughput_per_session": round2(throughput),
		"note": "raw = state.db session activity (true data); curated = MindBank nodes (after MCP/mining curation)",
	}
	payload := map[string]any{"raw": raw, "funnel": funnel}
	out2, _ := json.Marshal(payload)
	h.mu.Lock()
	h.rawCache, h.rawAt, h.fleetCacheKey = out2, time.Now(), cacheKey
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Write(out2)
}

// rawEventsHelperPath locates scripts/hermes-raw-events.py.
func rawEventsHelperPath() string {
	candidates := []string{filepath.Join("scripts", "hermes-raw-events.py")}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "mindbank", "scripts", "hermes-raw-events.py"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}


// liveMemoryWrites lists MindBank memory broadcasts in the window (the curated
// side of live fleet activity) with session attribution where available.
func liveMemoryWrites(ctx context.Context, pool *pgxpool.Pool, window int) []map[string]any {
	rows, err := pool.Query(ctx, `
		SELECT label, coalesce(namespace, 'global'), node_type::text, created_at,
		       coalesce(metadata->>'source_session', '')
		FROM nodes
		WHERE valid_to IS NULL
		  AND node_type NOT IN ('session', 'event')
		  AND workspace_name <> '___test___'
		  AND namespace !~* '^(test|sim|repair|relink|mcp_test|header-test)([_-]|$)'
		  AND created_at > now() - make_interval(secs => $1)
		ORDER BY created_at DESC
		LIMIT 15`, window)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var label, ns, ntype string
		var created time.Time
		var sid string
		if rows.Scan(&label, &ns, &ntype, &created, &sid) != nil {
			continue
		}
		out = append(out, map[string]any{
			"label":     label,
			"namespace": ns,
			"node_type": ntype,
			"created_at": created,
			"age":       int(time.Since(created).Seconds()),
			"session_id": sid,
		})
	}
	return out
}


// recentWorkspaceEvents returns the latest workspace transitions.
func recentWorkspaceEvents(ctx context.Context, pool *pgxpool.Pool, limit int) []map[string]any {
	rows, err := pool.Query(ctx, `
		SELECT event_type, coalesce(node_id,''), coalesce(label,''), coalesce(namespace,''),
		       coalesce(meta::text,'{}'), created_at
		FROM workspace_events ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var typ, nid, label, ns, meta string
		var created time.Time
		if rows.Scan(&typ, &nid, &label, &ns, &meta, &created) != nil {
			continue
		}
		out = append(out, map[string]any{
			"event_type": typ, "node_id": nid, "label": label, "namespace": ns,
			"meta": meta, "created_at": created, "age": int(time.Since(created).Seconds()),
		})
	}
	return out
}

// consumedCount counts workspace memories consumed (acted upon to completion).
func consumedCount(ctx context.Context, pool *pgxpool.Pool) int64 {
	var n int64
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM nodes
		WHERE metadata ? 'workspace_consumed_at' AND valid_to IS NULL`).Scan(&n)
	return n
}
