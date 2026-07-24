package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
)

// localAPIBase returns the base URL of the sibling HTTP API. The MCP server
// and the API run as separate processes, so tools that need work only the API
// can do (LLM session mining, conflict detection) call it over HTTP.
func localAPIBase() string {
	if v := os.Getenv("MB_API_URL"); v != "" {
		return v
	}
	return "http://localhost:8095/api/v1"
}

// triggerLocalAPIAsync fires a POST to the sibling API in a detached goroutine
// and returns immediately. Used for heavy operations (conflict detection over
// the whole graph, session mining) that can run for minutes — far longer than
// the MCP HTTP transport's write timeout — so blocking the tool call on them
// would just time out the client. The work continues server-side regardless.
func triggerLocalAPIAsync(path string, body any) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := callLocalAPI(ctx, "POST", path, body); err != nil {
			slog.Warn("async API trigger failed", "path", path, "error", err)
		}
	}()
}

// callLocalAPI POSTs to the sibling API and returns the decoded JSON response.
func callLocalAPI(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, localAPIBase()+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(data))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode API response: %w", err)
	}
	return out, nil
}

// toolUpdateNode handles the update_node MCP tool.
// It goes through NodeRepo.Update so MCP edits get the same guarantees as
// HTTP ones: temporal versioning, optimistic locking, field carry-over, and
// cache invalidation. The previous raw in-place UPDATE left the old content's
// embedding in the vector index forever, so corrected memories kept being
// recalled by their pre-correction meaning.
func (s *Server) toolUpdateNode(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		NodeID         string   `json:"node_id"`
		Label          *string  `json:"label,omitempty"`
		Content        *string  `json:"content,omitempty"`
		Summary        *string  `json:"summary,omitempty"`
		Importance     *float64 `json:"importance,omitempty"`
		EpistemicLabel *string  `json:"epistemic_label,omitempty"`
		Status         *string  `json:"status,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if req.NodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}

	upd := models.NodeUpdate{
		Label:          req.Label,
		Content:        req.Content,
		Summary:        req.Summary,
		EpistemicLabel: req.EpistemicLabel,
	}
	if req.Importance != nil {
		v := float32(*req.Importance)
		upd.Importance = &v
	}
	if req.Status != nil {
		st := models.ValidationStatus(*req.Status)
		upd.Status = &st
	}
	if upd.Label == nil && upd.Content == nil && upd.Summary == nil &&
		upd.Importance == nil && upd.EpistemicLabel == nil && upd.Status == nil {
		return nil, fmt.Errorf("no fields to update")
	}

	node, err := s.nodeRepo.Update(ctx, req.NodeID, upd)
	if err != nil {
		return nil, fmt.Errorf("update failed: %w", err)
	}
	if node == nil {
		return nil, fmt.Errorf("node not found or already deleted")
	}

	if err := embedder.EnqueueNode(ctx, s.pool, node.ID); err != nil {
		slog.Warn("enqueue re-embedding after update", "node_id", node.ID, "error", err)
	}
	s.searchRepo.InvalidateResultCache()

	return fmt.Sprintf("Updated node: %s (new version %d, id: %s)", req.NodeID, node.Version, node.ID), nil
}

// toolMineSessions handles the mine_sessions MCP tool. It triggers session
// mining via the sibling API (which owns the LLM reasoner) and reports the
// result, instead of just telling the caller to run it themselves.
func (s *Server) toolMineSessions(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Workspace string `json:"workspace,omitempty"`
		Limit     int    `json:"limit,omitempty"`
	}
	_ = json.Unmarshal(args, &req)
	if req.Workspace == "" {
		req.Workspace = "hermes"
	}

	var before int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM nodes WHERE node_type = 'session' AND valid_to IS NULL`).Scan(&before)

	body := map[string]any{"workspace": req.Workspace}
	if req.Limit > 0 {
		body["limit"] = req.Limit
	} else {
		body["mine_all"] = true
	}
	// Mining reads session files and runs the LLM extractor — can take minutes.
	// Fire it in the background and report the current count.
	triggerLocalAPIAsync("/sessions/mine", body)

	return fmt.Sprintf("Session mining triggered (running in background). "+
		"Current session nodes: %d — re-run this tool or check the dashboard shortly for the new count.", before), nil
}

// toolDreamStatus handles the dream_status MCP tool.
func (s *Server) toolDreamStatus(ctx context.Context, args json.RawMessage) (any, error) {
	var cycleID int
	var status string
	var edgesStrengthened, edgesPruned, edgesBridged, clustersFound int
	var startedAt time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT id, status, edges_strengthened, edges_pruned, edges_bridged,
		       clusters_found, started_at
		FROM dream_cycles
		ORDER BY started_at DESC
		LIMIT 1
	`).Scan(&cycleID, &status, &edgesStrengthened, &edgesPruned, &edgesBridged,
		&clustersFound, &startedAt)

	if err != nil {
		return "No dream cycles found. Run consolidation via POST /api/v1/dream/trigger.", nil
	}

	return fmt.Sprintf("Dream Engine Status:\n"+
		"  Cycle: #%d\n"+
		"  Status: %s\n"+
		"  Started: %s\n"+
		"  Edges strengthened: %d\n"+
		"  Edges pruned: %d\n"+
		"  Edges bridged: %d\n"+
		"  Clusters found: %d",
		cycleID, status, startedAt.Format(time.RFC3339),
		edgesStrengthened, edgesPruned, edgesBridged, clustersFound), nil
}

// toolListNamespaces handles the list_namespaces MCP tool.
func (s *Server) toolListNamespaces(ctx context.Context, args json.RawMessage) (any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT namespace, COUNT(*) as node_count
		FROM nodes
		WHERE valid_to IS NULL
		  AND namespace != 'global'
		GROUP BY namespace
		ORDER BY node_count DESC
		LIMIT 30
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}
	defer rows.Close()

	out := "Project Namespaces in MindBank:\n"
	total := 0
	for rows.Next() {
		var ns string
		var count int
		if err := rows.Scan(&ns, &count); err != nil {
			continue
		}
		out += fmt.Sprintf("  %s: %d nodes\n", ns, count)
		total += count
	}

	// Also count global
	var globalCount int
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL AND namespace = 'global'`).Scan(&globalCount)
	out += fmt.Sprintf("  global: %d nodes\n", globalCount)
	total += globalCount

	out += fmt.Sprintf("\nTotal: %d nodes", total)
	return out, nil
}

// toolConflicts handles the conflicts MCP tool.
func (s *Server) toolConflicts(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Action string `json:"action,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		req.Action = "list"
	}
	if req.Action == "" {
		req.Action = "list"
	}

	switch req.Action {
	case "list":
		rows, err := s.pool.Query(ctx, `
			SELECT c.id, c.similarity, c.attribute_path, c.resolution,
			       na.label, nb.label
			FROM conflicts c
			LEFT JOIN nodes na ON na.id = c.node_a_id
			LEFT JOIN nodes nb ON nb.id = c.node_b_id
			WHERE c.resolution = 'open'
			ORDER BY c.similarity DESC
			LIMIT 20
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query conflicts: %w", err)
		}
		defer rows.Close()

		out := "Open Conflicts:\n"
		count := 0
		for rows.Next() {
			var id, attrPath, resolution, labelA, labelB string
			var similarity float64
			if err := rows.Scan(&id, &similarity, &attrPath, &resolution, &labelA, &labelB); err != nil {
				continue
			}
			out += fmt.Sprintf("  - %s vs %s (sim: %.2f, attr: %s)\n", labelA, labelB, similarity, attrPath)
			count++
		}
		if count == 0 {
			return "No open conflicts found.", nil
		}
		out += fmt.Sprintf("\nTotal: %d open conflicts", count)
		return out, nil

	case "detect":
		// Detection scans the whole graph for contradictory near-duplicates —
		// a multi-minute job. Trigger it in the background and tell the caller
		// to check back with action:list.
		triggerLocalAPIAsync("/conflicts/detect", map[string]any{})
		return "Conflict detection triggered (running in background). Use action:'list' shortly to see any conflicts found.", nil

	default:
		return nil, fmt.Errorf("unknown action: %s (use 'list' or 'detect')", req.Action)
	}
}

