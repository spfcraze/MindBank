package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/repository"
	"mindbank/internal/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server implements a simple MCP-compatible stdio server for mindbank.
type Server struct {
	pool        *pgxpool.Pool
	pinned      *pinnedContext
	nodeRepo    *repository.NodeRepo
	edgeRepo    *repository.EdgeRepo
	searchRepo  *repository.SearchRepo
	snapRepo    *repository.SnapshotRepo
	sessionRepo *repository.SessionRepo
	depRepo     *repository.DependenceRepo
	embedder    *embedder.Client
	writeMu     sync.Mutex // protects stdout writes
	embedCache  *EmbeddingCache // LRU cache for query embeddings
}

// NewServer creates an MCP server.
func NewServer(pool *pgxpool.Pool, emb *embedder.Client) *Server {
	s := &Server{
		pool:        pool,
		pinned:      loadPinnedContext(),
		nodeRepo:    repository.NewNodeRepo(pool),
		edgeRepo:    repository.NewEdgeRepo(pool),
		searchRepo:  repository.NewSearchRepo(pool),
		snapRepo:    repository.NewSnapshotRepo(pool),
		sessionRepo: repository.NewSessionRepo(pool),
		depRepo:     repository.NewDependenceRepo(pool),
		embedder:    emb,
		embedCache:  NewEmbeddingCache(1000),
	}
	// Mirror the HTTP router's wiring: without these, MCP-side writes never
	// invalidate this process's own result/snapshot caches, so an agent that
	// saves a memory and re-searches within the cache TTL doesn't see it.
	s.nodeRepo.SetSearchRepo(s.searchRepo)
	s.nodeRepo.SetSnapshotRepo(s.snapRepo)
	s.edgeRepo.SetSearchRepo(s.searchRepo)
	return s
}

// MCP protocol types
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Run starts the MCP stdio server.
func (s *Server) Run(ctx context.Context) {
	slog.Info("mcp server started (stdio)")

	// One reader for the lifetime of the process: bufio buffers past the
	// first newline, so recreating the reader per message silently dropped
	// any pipelined requests already sitting in its buffer (e.g.
	// initialize + tools/list sent in a single client write).
	reader := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Stdio transport: EOF means the spawning client closed our
				// stdin and is gone. Exit instead of busy-polling forever —
				// a "reconnecting" client spawns a fresh process. (Service
				// deployments use the HTTP transport, not this path.)
				slog.Info("stdin closed, mcp stdio server exiting")
				return
			}
			slog.Error("stdin read error", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			slog.Warn("invalid json-rpc", "error", err)
			continue
		}

		// Bound each request so one hung DB/Ollama call can't wedge the
		// loop and stall every subsequent recall.
		reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		resp := s.handleRequest(reqCtx, &req)
		cancel()
		if resp != nil {
			s.writeResponse(resp)
		}
	}
}

// writeResponse writes a JSON-RPC response to stdout with proper locking and flushing.
func (s *Server) writeResponse(resp *MCPResponse) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("marshal response", "error", err)
		return
	}

	// Write raw bytes + newline, then flush
	if _, err := os.Stdout.Write(data); err != nil {
		slog.Error("write response", "error", err)
		return
	}
	if _, err := os.Stdout.Write([]byte("\n")); err != nil {
		slog.Error("write newline", "error", err)
		return
	}
	// Flush stdout to ensure data is sent immediately
	os.Stdout.Sync()
}

func (s *Server) handleRequest(ctx context.Context, req *MCPRequest) *MCPResponse {
	switch req.Method {
	case "initialize":
		return s.reply(req, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"prompts":   map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "mindbank",
				"version": "0.1.0",
			},
		})

	case "notifications/initialized":
		return nil // no response needed for notifications

	case "shutdown":
		return s.reply(req, nil)

	case "notifications/cancelled":
		return nil // client cancelled a request

	case "tools/list":
		return s.reply(req, map[string]any{
			"tools": s.tools(),
		})

	case "tools/call":
		return s.handleToolCall(ctx, req)

	case "prompts/list":
		return s.reply(req, map[string]any{"prompts": []any{}})

	case "resources/list":
		return s.reply(req, map[string]any{"resources": []any{}})

	case "ping":
		return s.reply(req, map[string]any{})

	default:
		// Unknown method — return error only if request has an ID (not a notification)
		if req.ID != nil && len(req.ID) > 0 && string(req.ID) != "null" {
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &MCPError{Code: -32601, Message: "method not found: " + req.Method},
			}
		}
		return nil
	}
}

func (s *Server) handleToolCall(ctx context.Context, req *MCPRequest) *MCPResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.error(req, -32602, "invalid params")
	}

	var result any
	var err error

	switch params.Name {
	case "create_node":
		result, err = s.toolCreateNode(ctx, params.Arguments)
	case "search":
		result, err = s.toolSearch(ctx, params.Arguments)
	case "ask":
		result, err = s.toolAsk(ctx, params.Arguments)
	case "snapshot":
		result, err = s.toolSnapshot(ctx, params.Arguments)
	case "neighbors":
		result, err = s.toolNeighbors(ctx, params.Arguments)
	case "create_edge":
		result, err = s.toolCreateEdge(ctx, params.Arguments)
	case "dependence":
		result, err = s.toolDependence(ctx, params.Arguments)
	case "refine_connectivity":
		result, err = s.toolRefineConnectivity(ctx, params.Arguments)
	case "evolution":
		result, err = s.toolEvolution(ctx, params.Arguments)
	case "cluster_sessions":
		result, err = s.toolClusterSessions(ctx, params.Arguments)
	case "update_node":
		result, err = s.toolUpdateNode(ctx, params.Arguments)
	case "mine_sessions":
		result, err = s.toolMineSessions(ctx, params.Arguments)
	case "dream_status":
		result, err = s.toolDreamStatus(ctx, params.Arguments)
	case "list_namespaces":
		result, err = s.toolListNamespaces(ctx, params.Arguments)
	case "conflicts":
		result, err = s.toolConflicts(ctx, params.Arguments)
	case "get_node":
		result, err = s.toolGetNode(ctx, params.Arguments)
	case "delete_node":
		result, err = s.toolDeleteNode(ctx, params.Arguments)
	case "create_nodes":
		result, err = s.toolCreateNodes(ctx, params.Arguments)
	case "recent":
		result, err = s.toolRecent(ctx, params.Arguments)
	case "history":
		result, err = s.toolHistory(ctx, params.Arguments)
	case "set_context":
		result, err = s.toolSetContext(ctx, params.Arguments)
	case "get_context":
		result, err = s.toolGetContext(ctx, params.Arguments)
	default:
		return s.error(req, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	if err != nil {
		return s.error(req, -32000, err.Error())
	}

	// Return result in the proper MCP format
	return s.reply(req, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": fmt.Sprintf("%v", result)},
		},
		"isError": false,
	})
}

func (s *Server) toolCreateNode(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Label          string `json:"label"`
		Type           string `json:"type"`
		Content        string `json:"content,omitempty"`
		Summary        string `json:"summary,omitempty"`
		Workspace      string `json:"workspace,omitempty"`
		Namespace      string `json:"namespace,omitempty"`
		EpistemicLabel string `json:"epistemic_label,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	// Do NOT derive the namespace from the server's PWD: this MCP runs as a
	// long-lived HTTP service (WorkingDirectory=$HOME/mindbank), so PWD is
	// fixed and would mis-tag every memory as "mindbank" regardless of the
	// agent's actual project. Callers that want project isolation must pass an
	// explicit namespace; otherwise it defaults to "global" in NodeRepo.Create.
	return s.createNode(ctx, req.Label, req.Type, req.Content, req.Summary, req.Workspace, req.Namespace, req.EpistemicLabel)
}

// hybridSearch runs hybrid FTS + vector search. When the embedder (Ollama)
// is unavailable it degrades to FTS-only, so recall still works during
// embedder outages instead of failing every search.
func (s *Server) hybridSearch(ctx context.Context, query, workspace, namespace string, limit int, nodeTypes string) ([]models.SearchResult, error) {
	var embedding []float32
	var err error
	if cached, ok := s.embedCache.Get(query); ok {
		embedding = cached
	} else {
		embedding, err = s.embedder.Embed(ctx, query)
		if err != nil {
			// FTS-only fallback — better than failing the whole recall.
			results, ftsErr := s.searchRepo.FullTextSearch(ctx, query, workspace, namespace, limit)
			if ftsErr != nil {
				return nil, fmt.Errorf("embedding failed: %w (fts fallback: %v)", err, ftsErr)
			}
			if len(results) == 0 {
				return results, nil // caller renders "no results"
			}
			results = filterByNodeTypes(results, nodeTypes)
			return results, nil
		}
		s.embedCache.Set(query, embedding)
	}

	results, err := s.searchRepo.HybridSearch(ctx, query, embedding, workspace, namespace, limit, s.edgeRepo)
	if err != nil {
		return nil, err
	}
	return filterByNodeTypes(results, nodeTypes), nil
}

// filterByNodeTypes keeps only results whose node_type is in the comma-list.
func filterByNodeTypes(results []models.SearchResult, nodeTypes string) []models.SearchResult {
	if nodeTypes == "" {
		return results
	}
	allowed := map[string]bool{}
	for _, t := range strings.Split(nodeTypes, ",") {
		allowed[strings.ToLower(strings.TrimSpace(t))] = true
	}
	filtered := results[:0]
	for _, r := range results {
		if allowed[strings.ToLower(r.NodeType)] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (s *Server) toolSearch(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Query               string `json:"query"`
		Workspace           string `json:"workspace,omitempty"`
		Namespace           string `json:"namespace,omitempty"`
		NodeTypes           string `json:"node_types,omitempty"`
		Limit               int    `json:"limit,omitempty"`
		DependenceExpansion bool   `json:"dependence_expansion,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if err := validateLimit(req.Limit); err != nil {
		return nil, err
	}
	// Effective context: explicit arg > header > pinned > derived-from-transcript.
	req.Workspace, req.Namespace = s.resolveContext(ctx, req.Workspace, req.Namespace)

	results, err := s.hybridSearch(ctx, req.Query, req.Workspace, req.Namespace, req.Limit, req.NodeTypes)
	if err != nil {
		return nil, err
	}

	// Dependence-aware expansion: trace backward from top result to find supporting evidence
	if req.DependenceExpansion && len(results) > 0 && s.depRepo != nil {
		topResult := results[0]
		precursors, err := s.depRepo.DependenceExpand(ctx, topResult.NodeID, req.Limit/4)
		if err == nil && len(precursors) > 0 {
			existing := make(map[string]bool)
			for _, r := range results {
				existing[r.NodeID] = true
			}
			for _, p := range precursors {
				if !existing[p.NodeID] && !isShellNodeType(p.NodeType) {
					results = append(results, p)
					existing[p.NodeID] = true
				}
			}
		}
	}

	if len(results) == 0 {
		return "No results found.", nil
	}

	s.reinforceRecall(ctx, results)

	// Token-lean output: dedupe by node, drop internal scores, one tight
	// snippet per memory. This text goes straight into the agent's context.
	out := ""
	seen := make(map[string]bool)
	for _, r := range results {
		key := strings.ToLower(strings.TrimSpace(r.Label))
		if seen[key] {
			continue // collapse duplicate-label memories (regex-era dupes)
		}
		seen[key] = true
		out += "- " + memoryLine(r.NodeType, r.Label, r.Content, 130) + "\n"
	}
	return out, nil
}

// snippet returns a compact, single-line preview of a memory's content: the
// first sentence, capped at maxLen, rune-safe. Keeps recall output cheap.
func snippet(s string, maxLen int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace
	if i := strings.IndexAny(s, ".!?"); i > 20 && i < maxLen {
		return s[:i+1]
	}
	if len(s) > maxLen {
		return utils.TruncateUTF8(s, maxLen) + "…"
	}
	return s
}

// memoryLine formats one memory as a compact recall line, omitting the content
// snippet when it just repeats the label (common in old regex-mined nodes where
// label == content) so we never pay tokens for the same text twice.
func memoryLine(nodeType, label, content string, maxLen int) string {
	label = strings.TrimSpace(label)
	snip := snippet(content, maxLen)
	base := fmt.Sprintf("[%s] %s", nodeType, label)
	if snip == "" {
		return base
	}
	ll, ss := strings.ToLower(label), strings.ToLower(snip)
	if strings.HasPrefix(ss, ll) || strings.HasPrefix(ll, ss) {
		return base // snippet is redundant with the label
	}
	return base + " — " + snip
}

// reinforceRecall strengthens memories the agent just recalled (fire-and-forget
// so it never slows the response). Recalled memories rise; unused ones decay.
func (s *Server) reinforceRecall(ctx context.Context, results []models.SearchResult) {
	if s.nodeRepo == nil || len(results) == 0 {
		return
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.NodeID)
	}
	go func() {
		_ = s.nodeRepo.ReinforceRecall(context.WithoutCancel(ctx), ids)
	}()
}

func (s *Server) toolAsk(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Query               string `json:"query"`
		Workspace           string `json:"workspace,omitempty"`
		Namespace           string `json:"namespace,omitempty"`
		NodeTypes           string `json:"node_types,omitempty"`
		DependenceExpansion bool   `json:"dependence_expansion,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	// Effective context: explicit arg > header > pinned > derived-from-transcript.
	req.Workspace, req.Namespace = s.resolveContext(ctx, req.Workspace, req.Namespace)

	results, err := s.hybridSearch(ctx, req.Query, req.Workspace, req.Namespace, 5, req.NodeTypes)
	if err != nil {
		return nil, err
	}

	// Dependence-aware expansion
	if req.DependenceExpansion && len(results) > 0 && s.depRepo != nil {
		topResult := results[0]
		precursors, err := s.depRepo.DependenceExpand(ctx, topResult.NodeID, 2)
		if err == nil && len(precursors) > 0 {
			existing := make(map[string]bool)
			for _, r := range results {
				existing[r.NodeID] = true
			}
			for _, p := range precursors {
				if !existing[p.NodeID] && !isShellNodeType(p.NodeType) {
					results = append(results, p)
					existing[p.NodeID] = true
				}
			}
		}
	}

	if len(results) == 0 {
		return "No relevant information found.", nil
	}

	s.reinforceRecall(ctx, results)

	// Token-lean: dedupe by node and cap each memory's snippet. Previously
	// this dumped FULL untruncated content (a single node could be 10KB),
	// which burned the agent's context on every ask.
	out := fmt.Sprintf("Context for: %s\n\n", req.Query)
	seen := make(map[string]bool)
	for _, r := range results {
		key := strings.ToLower(strings.TrimSpace(r.Label))
		if seen[key] {
			continue // collapse duplicate-label memories (regex-era dupes)
		}
		seen[key] = true
		out += memoryLine(r.NodeType, r.Label, r.Content, 240) + "\n"
	}
	return out, nil
}

func (s *Server) toolSnapshot(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Workspace string `json:"workspace,omitempty"`
		Namespace string `json:"namespace,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	req.Workspace, req.Namespace = s.resolveContext(ctx, req.Workspace, req.Namespace)

	content, tokenCount, err := s.snapRepo.GetFiltered(ctx, req.Workspace, req.Namespace, "default")
	if err != nil {
		// Generate fresh
		content, tokenCount, _, err = s.snapRepo.GenerateFiltered(ctx, req.Workspace, req.Namespace, "default", 2000)
		if err != nil {
			return nil, err
		}
		// Cache namespace-filtered result
		if req.Namespace != "" {
			s.snapRepo.SetCache(req.Workspace, req.Namespace, "default", content, tokenCount)
		}
	}

	_ = tokenCount
	return content, nil
}

func (s *Server) toolNeighbors(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		NodeID string `json:"node_id"`
		Depth  int    `json:"depth,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}

	var nodes []models.NodeWithEdge
	var err error
	if req.Depth > 1 {
		nodes, err = s.edgeRepo.GetNeighborsDeep(ctx, req.NodeID, req.Depth)
	} else {
		nodes, err = s.edgeRepo.GetNeighbors(ctx, req.NodeID)
	}
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return "No neighbors found.", nil
	}

	// Drop session/event shells: they're transcript wrappers ("session_…json"),
	// not memories, and only leak into recall through their `produced` edges.
	// The search/snapshot paths already exclude them; do the same here so the
	// neighbors tool returns real connected knowledge, not filenames.
	out := ""
	for _, n := range nodes {
		if isShellNodeType(string(n.NodeType)) {
			continue
		}
		out += fmt.Sprintf("- [%s] %s (%s, depth %d)\n", n.NodeType, n.Label, n.EdgeType, n.Depth)
	}
	if out == "" {
		return "No neighbors found.", nil
	}
	return out, nil
}

// isShellNodeType reports whether a node is a transcript container (session or
// event) rather than a distilled memory. Recall paths exclude these.
func isShellNodeType(nodeType string) bool {
	return nodeType == "session" || nodeType == "event"
}

func (s *Server) toolCreateEdge(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		SourceID  string   `json:"source_id"`
		TargetID  string   `json:"target_id"`
		EdgeType  string   `json:"edge_type"`
		Weight    *float32 `json:"weight,omitempty"`
		Workspace string   `json:"workspace,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if err := validateUUID(req.SourceID); err != nil {
		return nil, fmt.Errorf("source_id: %w", err)
	}
	if err := validateUUID(req.TargetID); err != nil {
		return nil, fmt.Errorf("target_id: %w", err)
	}

	edgeType := models.EdgeType(req.EdgeType)
	if !edgeType.IsValid() {
		return nil, fmt.Errorf("invalid edge_type: %s", req.EdgeType)
	}
	// Pass the weight pointer straight through: nil (weight omitted) lets
	// EdgeRepo.Create apply its 1.0 default. Previously a plain float32 was
	// always sent as a non-nil pointer, so an omitted weight became 0.0 — and
	// a 0-weight edge scores 0 in graph expansion (edge_weight*0.5), making
	// MCP-created edges invisible to recall.
	edge, err := s.edgeRepo.Create(ctx, models.EdgeCreate{
		WorkspaceName: req.Workspace,
		SourceID:      req.SourceID,
		TargetID:      req.TargetID,
		EdgeType:      edgeType,
		Weight:        req.Weight,
	})
	if err != nil {
		return nil, err
	}

	return fmt.Sprintf("Created edge: %s -> %s (%s, id: %s)", edge.SourceID, edge.TargetID, edge.EdgeType, edge.ID), nil
}

func (s *Server) toolDependence(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		NodeID    string `json:"node_id,omitempty"`
		Query     string `json:"query,omitempty"`
		Namespace string `json:"namespace,omitempty"`
		MaxDepth  int    `json:"max_depth,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}

	seedID := req.NodeID
	if seedID == "" && req.Query != "" {
		// Search for seed via hybrid search
		embedding, err := s.embedder.Embed(ctx, req.Query)
		if err != nil {
			return nil, fmt.Errorf("embedding failed: %w", err)
		}
		results, err := s.searchRepo.HybridSearch(ctx, req.Query, embedding, "", req.Namespace, 1, s.edgeRepo)
		if err != nil || len(results) == 0 {
			return nil, fmt.Errorf("no seed found for query")
		}
		seedID = results[0].NodeID
	}
	if seedID == "" {
		return nil, fmt.Errorf("node_id or query required")
	}
	if req.MaxDepth <= 0 || req.MaxDepth > 5 {
		req.MaxDepth = 3
	}

	nodes, edges, modes, criticalDepth, coverage, blindSpots, err := s.depRepo.GetDependenceGraph(ctx, seedID,
		[]string{"depends_on", "learned_from", "decided_by", "produced", "supports"},
		req.MaxDepth, 0.1)
	if err != nil {
		return nil, err
	}

	out := fmt.Sprintf("Domain of Dependence for %s\n", seedID)
	out += fmt.Sprintf("Critical Depth: %d | Coverage: %.1f%%\n\n", criticalDepth, coverage*100)

	if len(modes) > 0 {
		out += "Top Influence Modes:\n"
		for _, m := range modes {
			out += fmt.Sprintf("- [%s] %s (score: %.3f, depth: %d)\n", m.NodeType, m.Label, m.InfluenceScore, m.Depth)
		}
	}

	if len(blindSpots) > 0 {
		out += "\nBlind Spots:\n"
		for _, b := range blindSpots {
			out += fmt.Sprintf("- [%s] %s\n", b.Severity, b.Description)
		}
	}

	out += fmt.Sprintf("\nGraph: %d nodes, %d edges\n", len(nodes), len(edges))
	return out, nil
}

func (s *Server) tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "create_node",
			"description": "Create a new node in the mindmap memory bank",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
			"label":     map[string]string{"type": "string", "description": "Node label/name"},
				"type":      map[string]string{"type": "string", "description": "Node type: person, agent, project, topic, decision, fact, event, preference, advice, problem, concept, question, session"},
				"content":   map[string]string{"type": "string", "description": "Full content"},
				"summary":   map[string]string{"type": "string", "description": "Short summary"},
				"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
				"namespace": map[string]string{"type": "string", "description": "Project namespace (default: global)"},
				"epistemic_label": map[string]string{"type": "string", "description": "Epistemic classification: observed, inferred, assumed, recommended, unknown (default: unknown)"},
				},
				"required": []string{"label", "type"},
			},
		},
		{
			"name":        "search",
			"description": "Search the mindmap using hybrid FTS + semantic search. Optionally traces causal precursors (dependence expansion) for richer context.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":                map[string]string{"type": "string", "description": "Search query"},
					"workspace":            map[string]string{"type": "string", "description": "Filter by workspace"},
					"namespace":            map[string]string{"type": "string", "description": "Filter by namespace"},
					"node_types":           map[string]string{"type": "string", "description": "Comma-separated node types to keep, e.g. fact,decision (default: all)"},
					"limit":                map[string]string{"type": "integer", "description": "Max results (default: 10)"},
					"dependence_expansion": map[string]string{"type": "boolean", "description": "Trace causal precursors of top result (default: false)"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "ask",
			"description": "Ask a natural language question and get relevant context from the mindmap. Optionally includes causal precursors.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":                map[string]string{"type": "string", "description": "Your question"},
					"workspace":            map[string]string{"type": "string", "description": "Filter by workspace"},
					"namespace":            map[string]string{"type": "string", "description": "Filter by project namespace (isolates memories)"},
					"node_types":           map[string]string{"type": "string", "description": "Comma-separated node types to keep, e.g. fact,decision (default: all)"},
					"dependence_expansion": map[string]string{"type": "boolean", "description": "Trace causal precursors of top result (default: false)"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "snapshot",
			"description": "Get a pre-computed wake-up context of the most important memories",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
					"namespace": map[string]string{"type": "string", "description": "Filter by project namespace (isolates memories)"},
				},
			},
		},
		{
			"name":        "neighbors",
			"description": "Get nodes connected to a specific node in the mindmap",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]string{"type": "string", "description": "Node ID"},
					"depth":   map[string]string{"type": "integer", "description": "Traversal depth (default: 1)"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name":        "dependence",
			"description": "Trace the domain of dependence (causal precursors) for a node or query. Returns influence modes, critical depth, coverage, and blind spots.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id":   map[string]string{"type": "string", "description": "Node ID to trace (optional if query provided)"},
					"query":     map[string]string{"type": "string", "description": "Search query to find seed node (optional if node_id provided)"},
					"namespace": map[string]string{"type": "string", "description": "Filter by namespace"},
					"max_depth": map[string]string{"type": "integer", "description": "Max traversal depth 1-5 (default: 3)"},
				},
			},
		},
		{
			"name":        "create_edge",
			"description": "Create a connection between two nodes",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source_id": map[string]string{"type": "string", "description": "Source node ID"},
					"target_id": map[string]string{"type": "string", "description": "Target node ID"},
					"edge_type": map[string]string{"type": "string", "description": "Edge type: contains, relates_to, depends_on, decided_by, participated_in, produced, contradicts, supports, temporal_next, mentions, learned_from"},
					"weight":    map[string]string{"type": "number", "description": "Connection weight (default: 1.0)"},
					"workspace": map[string]string{"type": "string", "description": "Workspace name"},
				},
				"required": []string{"source_id", "target_id", "edge_type"},
			},
		},
		{
			"name":        "refine_connectivity",
			"description": "Feedback-driven topological editing: expand missing links, prune noise, or review content granularity. Creates or deletes edges based on semantic similarity.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id":  map[string]string{"type": "string", "description": "Node ID to refine"},
					"feedback": map[string]string{"type": "string", "description": "Feedback type: missing_context (find similar unconnected nodes), too_much_noise (prune weak edges), granularity_mismatch (review content length)"},
				},
				"required": []string{"node_id", "feedback"},
			},
		},
		{
			"name":        "evolution",
			"description": "Get the evolution maturity score for a node. Returns PEMS-inspired metrics: evolution_score, maturity_level (stable/evolving/volatile), version_count, and convergence_delta.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]string{"type": "string", "description": "Node ID to analyze"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name":        "cluster_sessions",
			"description": "Group session nodes by embedding similarity into episodic clusters. Returns clusters with member IDs, labels, and centroid previews.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"namespace": map[string]string{"type": "string", "description": "Namespace to cluster (default: global)"},
					"threshold": map[string]string{"type": "number", "description": "Cosine similarity threshold 0-1 (default: 0.85)"},
				},
			},
		},
		{
			"name":        "update_node",
			"description": "Update an existing node's fields (label, content, summary, importance, epistemic_label, status). Use this to classify nodes or correct information.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id":          map[string]string{"type": "string", "description": "Node ID to update"},
					"label":            map[string]string{"type": "string", "description": "New label (optional)"},
					"content":          map[string]string{"type": "string", "description": "New content (optional)"},
					"summary":          map[string]string{"type": "string", "description": "New summary (optional)"},
					"importance":       map[string]string{"type": "number", "description": "Importance 0.0-1.0 (optional)"},
					"epistemic_label":  map[string]string{"type": "string", "description": "Epistemic classification: observed, inferred, assumed, recommended, unknown (optional)"},
					"status":           map[string]string{"type": "string", "description": "Status: open, supported, refuted, inconclusive, blocked (optional)"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name":        "list_namespaces",
			"description": "List all namespaces in MindBank with their node counts. Use this to discover what projects/knowledge areas are available.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "dream_status",
			"description": "Check the status of the Dream Engine (Neural Consolidation). Shows the latest cycle metrics: edges strengthened, clusters found, etc.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "mine_sessions",
			"description": "Check session mining status. Shows how many session nodes exist in MindBank.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
				},
			},
		},
		{
			"name":        "conflicts",
			"description": "List or detect knowledge conflicts. Conflicts are nodes with high semantic similarity but contradictory attributes.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]string{"type": "string", "description": "Action: 'list' (show open conflicts) or 'detect' (trigger detection). Default: list"},
				},
			},
		},
		{
			"name":        "get_node",
			"description": "Fetch a single node by ID with full content, metadata, and connection count. Use after search to read a memory in full.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]string{"type": "string", "description": "Node ID"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name":        "delete_node",
			"description": "Soft-delete a node (workspace-scoped; version history retained). Use to remove wrong or obsolete memories.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id":   map[string]string{"type": "string", "description": "Node ID"},
					"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name":        "create_nodes",
			"description": "Batch-create multiple memories in one call (max 50). Each item: label, type, content, summary. Reports created vs already-known (reinforced) per item.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodes":     map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]string{"type": "string"}, "type": map[string]string{"type": "string"}, "content": map[string]string{"type": "string"}, "summary": map[string]string{"type": "string"}}, "required": []string{"label", "type"}}},
					"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
					"namespace": map[string]string{"type": "string", "description": "Project namespace (default: global)"},
				},
				"required": []string{"nodes"},
			},
		},
		{
			"name":        "recent",
			"description": "List the most recently created or updated memories. Use to recall what was worked on recently.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
					"namespace": map[string]string{"type": "string", "description": "Filter by namespace"},
					"limit":     map[string]string{"type": "integer", "description": "Max results (default: 10)"},
				},
			},
		},
		{
			"name":        "history",
			"description": "Show the temporal version history of a node (label and timestamp per version).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]string{"type": "string", "description": "Node ID"},
				},
				"required": []string{"node_id"},
			},
		},
		{
			"name":        "set_context",
			"description": "Pin the default workspace/namespace for subsequent calls (persisted). Use when auto-detection from the session transcript is wrong or you switch projects mid-session. Pass empty strings to clear.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string", "description": "Workspace name (default: hermes)"},
					"namespace": map[string]string{"type": "string", "description": "Project namespace"},
				},
			},
		},
		{
			"name":        "get_context",
			"description": "Show the effective workspace/namespace (header, pinned, and auto-derived from the active Hermes session).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
	}
}

func (s *Server) reply(req *MCPRequest, result any) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) error(req *MCPRequest, code int, msg string) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Error:   &MCPError{Code: code, Message: msg},
	}
}
