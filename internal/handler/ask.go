package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
)

type AskHandler struct {
	searchRepo   *repository.SearchRepo
	snapshotRepo *repository.SnapshotRepo
	edgeRepo     *repository.EdgeRepo
	embedder     *embedder.Client
}

func NewAskHandler(sr *repository.SearchRepo, snap *repository.SnapshotRepo, er *repository.EdgeRepo, emb *embedder.Client) *AskHandler {
	return &AskHandler{searchRepo: sr, snapshotRepo: snap, edgeRepo: er, embedder: emb}
}

// Ask handles POST /api/v1/ask — natural language query, returns structured context.
func (h *AskHandler) Ask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query           string `json:"query"`
		Workspace       string `json:"workspace,omitempty"`
		Namespace       string `json:"namespace,omitempty"`
		MaxTokens       int    `json:"max_tokens,omitempty"`
		IncludeMessages bool   `json:"include_messages,omitempty"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if req.Query == "" {
		respondError(w, 400, "query is required")
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 2000
	}

	// Generate embedding for the query
	embedding, err := h.embedder.Embed(r.Context(), req.Query)
	if err != nil {
		respondEmbedError(w, err, "embed query for ask")
		return
	}

	// Hybrid search for relevant nodes
	results, err := h.searchRepo.HybridSearch(r.Context(), req.Query, embedding, req.Workspace, req.Namespace, 10, h.edgeRepo)
	if err != nil {
		slog.Error("hybrid search for ask", "error", err)
		respondError(w, 500, "search failed")
		return
	}
	if results == nil {
		results = []models.SearchResult{}
	}

	// Build context from results
	context := ""
	tokens := 0
	for _, sr := range results {
		line := sr.Content
		if len(line) > 200 {
			line = line[:200] + "..."
		}
		entry := "[" + sr.NodeType + "] " + sr.Label + ": " + line + "\n"
		entryTokens := len(entry) / 4
		if tokens+entryTokens > req.MaxTokens {
			break
		}
		context += entry
		tokens += entryTokens
	}

	// Build graph paths for top results
	var graphPaths []string
	for i, sr := range results {
		if i >= 3 {
			break
		}
		// Get neighbors to show context
		neighbors, _ := h.edgeRepo.GetNeighbors(r.Context(), sr.NodeID)
		if len(neighbors) > 0 {
			path := sr.Label
			for j, n := range neighbors {
				if j >= 3 {
					break
				}
				path += " → " + string(n.EdgeType) + " → " + n.Label
			}
			graphPaths = append(graphPaths, path)
		}
	}

	respondJSON(w, 200, map[string]any{
		"context":     context,
		"nodes":       results,
		"graph_paths":  graphPaths,
		"token_count": tokens,
	})
}

// Snapshot handles GET /api/v1/snapshot — pre-computed wake-up context.
func (h *AskHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "hermes"
	}
	nsFilter := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("name")

	content, tokenCount, err := h.snapshotRepo.GetFiltered(r.Context(), workspace, nsFilter, name)
	if err != nil {
		// No snapshot exists — generate one
		maxTokens, _ := strconv.Atoi(r.URL.Query().Get("max_tokens"))
		if maxTokens <= 0 {
			maxTokens = 2000
		}
		content, tokenCount, _, err = h.snapshotRepo.GenerateFiltered(r.Context(), workspace, nsFilter, name, maxTokens)
		if err != nil {
			slog.Error("generate snapshot", "error", err)
			respondError(w, 500, "failed to generate snapshot")
			return
		}
		// Cache namespace-filtered result
		if nsFilter != "" {
			h.snapshotRepo.SetCache(workspace, nsFilter, name, content, tokenCount)
		}
	}

	respondJSON(w, 200, map[string]any{
		"content":     content,
		"token_count": tokenCount,
	})
}

// RebuildSnapshot handles POST /api/v1/snapshot/rebuild.
func (h *AskHandler) RebuildSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace,omitempty"`
		Name      string `json:"name,omitempty"`
		MaxTokens int    `json:"max_tokens,omitempty"`
	}
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if req.Workspace == "" {
		req.Workspace = "hermes"
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 2000
	}

	content, tokenCount, nodeCount, err := h.snapshotRepo.Generate(r.Context(), req.Workspace, req.Name, req.MaxTokens)
	if err != nil {
		slog.Error("rebuild snapshot", "error", err)
		respondError(w, 500, "failed to rebuild snapshot")
		return
	}

	respondJSON(w, 200, map[string]any{
		"content":     content,
		"token_count": tokenCount,
		"node_count":  nodeCount,
	})
}

// Graph handles GET /api/v1/graph — returns nodes+edges for visualization.
// Supports: ?limit=500&offset=0 (default: 200, max: 2000)
func (h *AskHandler) Graph(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	workspace := r.URL.Query().Get("workspace")
	// Note: no hardcoded workspace default — consistent with other endpoints

	// Pagination
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	// Build query for nodes — include edge_count for orphan detection
	query := `SELECT n.id, n.namespace, n.label, n.node_type::text, n.content, n.summary, n.importance, n.access_count, n.metadata, n.epistemic_label,
		COALESCE(e.cnt, 0) AS edge_count
		FROM nodes n
		LEFT JOIN (
			SELECT node_id, COUNT(*) as cnt FROM (
				SELECT source_id AS node_id FROM edges
				UNION ALL
				SELECT target_id AS node_id FROM edges
			) sub GROUP BY node_id
		) e ON e.node_id = n.id
		WHERE n.valid_to IS NULL`
	args := []any{}
	argN := 1

	if workspace != "" {
		query += fmt.Sprintf(" AND workspace_name = $%d", argN)
		args = append(args, workspace)
		argN++
	}
	if namespace != "" {
		query += fmt.Sprintf(" AND namespace = $%d", argN)
		args = append(args, namespace)
		argN++
	}
	query += fmt.Sprintf(" ORDER BY importance DESC, access_count DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, limit, offset)

	rows, err := h.snapshotRepo.Pool().Query(r.Context(), query, args...)
	if err != nil {
		respondError(w, 500, "graph query failed")
		return
	}
	defer rows.Close()

	type graphNode struct {
		ID              string          `json:"id"`
		Label           string          `json:"label"`
		NodeType        string          `json:"node_type"`
		Summary         string          `json:"summary"`
		NS              string          `json:"namespace"`
		Importance      float32         `json:"importance"`
		AccessCount     int             `json:"access_count"`
		Metadata        json.RawMessage `json:"metadata,omitempty"`
		EpistemicLabel  string          `json:"epistemic_label,omitempty"`
		EdgeCount       int             `json:"edge_count"`
		// Event-specific fields (omitted for non-event nodes)
		SessionID string `json:"session_id,omitempty"`
		EventType string `json:"event_type,omitempty"`
		Sequence  int    `json:"sequence,omitempty"`
	}
	var nodes []graphNode
	nodeIDs := []any{}
	for rows.Next() {
		var n graphNode
		var content string
		var metadata json.RawMessage
		if err := rows.Scan(&n.ID, &n.NS, &n.Label, &n.NodeType, &content, &n.Summary, &n.Importance, &n.AccessCount, &metadata, &n.EpistemicLabel, &n.EdgeCount); err != nil {
			continue
		}
		n.Metadata = metadata

		// Extract event-specific fields from metadata if this is an event node
		if n.NodeType == "event" && len(metadata) > 0 {
			var eventMeta struct {
				SessionID string `json:"session_id"`
				EventType string `json:"event_type"`
				Sequence  int    `json:"sequence"`
			}
			if err := json.Unmarshal(metadata, &eventMeta); err == nil {
				n.SessionID = eventMeta.SessionID
				n.EventType = eventMeta.EventType
				n.Sequence = eventMeta.Sequence
			}
		}
		nodes = append(nodes, n)
		nodeIDs = append(nodeIDs, n.ID)
	}

	if nodes == nil {
		nodes = []graphNode{}
	}

	// Get edges connected to these nodes (at least one endpoint in the node set)
	type graphEdge struct {
		ID       string  `json:"id"`
		Source   string  `json:"source"`
		Target   string  `json:"target"`
		EdgeType string  `json:"edge_type"`
		Weight   float32 `json:"weight"`
	}
	var edges []graphEdge

	if len(nodeIDs) > 0 {
		n := len(nodeIDs)
		placeholders := ""
		for i := 0; i < n; i++ {
			if i > 0 {
				placeholders += ","
			}
			placeholders += fmt.Sprintf("$%d", i+1)
		}
		edgeQuery := fmt.Sprintf(`
			SELECT id, source_id, target_id, edge_type::text, weight
			FROM edges
			WHERE source_id IN (%s) OR target_id IN (%s)
			ORDER BY weight DESC
			LIMIT 10000
		`, placeholders, placeholders)

		edgeRows, err := h.snapshotRepo.Pool().Query(r.Context(), edgeQuery, nodeIDs...)
		if err == nil {
			defer edgeRows.Close()
			for edgeRows.Next() {
				var e graphEdge
				if err := edgeRows.Scan(&e.ID, &e.Source, &e.Target, &e.EdgeType, &e.Weight); err != nil {
					continue
				}
				edges = append(edges, e)
			}
		}
	}

	if edges == nil {
		edges = []graphEdge{}
	}

	respondJSON(w, 200, map[string]any{
		"nodes":  nodes,
		"edges":  edges,
		"limit":  limit,
		"offset": offset,
	})
}

// RegisterAskRoutes adds ask and snapshot endpoints to the router.
func RegisterAskRoutes(r chi.Router, ah *AskHandler) {
	r.Post("/ask", ah.Ask)
	r.Get("/snapshot", ah.Snapshot)
	r.Post("/snapshot/rebuild", ah.RebuildSnapshot)
	r.Get("/graph", ah.Graph)
}
