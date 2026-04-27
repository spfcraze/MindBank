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

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server implements a simple MCP-compatible stdio server for mindbank.
type Server struct {
	pool        *pgxpool.Pool
	nodeRepo    *repository.NodeRepo
	edgeRepo    *repository.EdgeRepo
	searchRepo  *repository.SearchRepo
	snapRepo    *repository.SnapshotRepo
	sessionRepo *repository.SessionRepo
	depRepo     *repository.DependenceRepo
	embedder    *embedder.Client
	writeMu     sync.Mutex // protects stdout writes
}

// NewServer creates an MCP server.
func NewServer(pool *pgxpool.Pool, emb *embedder.Client) *Server {
	return &Server{
		pool:        pool,
		nodeRepo:    repository.NewNodeRepo(pool),
		edgeRepo:    repository.NewEdgeRepo(pool),
		searchRepo:  repository.NewSearchRepo(pool),
		snapRepo:    repository.NewSnapshotRepo(pool),
		sessionRepo: repository.NewSessionRepo(pool),
		depRepo:     repository.NewDependenceRepo(pool),
		embedder:    emb,
	}
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

	// Create a fresh stdin reader for each connection attempt
	// This handles cases where the client reconnects
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check if stdin is still readable
		stat, err := os.Stdin.Stat()
		if err != nil {
			slog.Error("stdin stat error", "error", err)
			// Wait a bit and retry — client may reconnect
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}

		// If stdin is a pipe and has no data, wait for input
		if stat.Mode()&os.ModeNamedPipe != 0 && stat.Size() == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// EOF might mean client disconnected — wait and retry
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
					continue
				}
			}
			slog.Error("stdin read error", "error", err)
			// On error, wait briefly then retry — client may reconnect
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

		resp := s.handleRequest(ctx, &req)
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
		Label    string `json:"label"`
		Type     string `json:"type"`
		Content  string `json:"content,omitempty"`
		Summary  string `json:"summary,omitempty"`
		Workspace string `json:"workspace,omitempty"`
		Namespace string `json:"namespace,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if req.Label == "" {
		return nil, fmt.Errorf("label is required")
	}
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	nodeType := models.NodeType(req.Type)
	if !nodeType.IsValid() {
		return nil, fmt.Errorf("invalid node_type: %s", req.Type)
	}
	node, err := s.nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: req.Workspace,
		Namespace:     req.Namespace,
		Label:         req.Label,
		NodeType:      nodeType,
		Content:       req.Content,
		Summary:       req.Summary,
	})
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("Created node: %s (id: %s)", node.Label, node.ID), nil
}

func (s *Server) toolSearch(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Query               string `json:"query"`
		Workspace           string `json:"workspace,omitempty"`
		Namespace           string `json:"namespace,omitempty"`
		Limit               int    `json:"limit,omitempty"`
		DependenceExpansion bool   `json:"dependence_expansion,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}

	embedding, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w (try a different query or retry)", err)
	}

	results, err := s.searchRepo.HybridSearch(ctx, req.Query, embedding, req.Workspace, req.Namespace, req.Limit, s.edgeRepo)
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
				if !existing[p.NodeID] {
					results = append(results, p)
					existing[p.NodeID] = true
				}
			}
		}
	}

	if len(results) == 0 {
		return "No results found.", nil
	}

	out := ""
	for _, r := range results {
		content := r.Content
		if len(content) > 150 {
			content = content[:150] + "..."
		}
		out += fmt.Sprintf("- [%s] %s: %s (score: %.3f)\n", r.NodeType, r.Label, content, r.RRFScore)
	}
	return out, nil
}

func (s *Server) toolAsk(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Query               string `json:"query"`
		Workspace           string `json:"workspace,omitempty"`
		Namespace           string `json:"namespace,omitempty"`
		DependenceExpansion bool   `json:"dependence_expansion,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}

	embedding, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w (try a different query or retry)", err)
	}

	results, err := s.searchRepo.HybridSearch(ctx, req.Query, embedding, req.Workspace, req.Namespace, 5, s.edgeRepo)
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
				if !existing[p.NodeID] {
					results = append(results, p)
					existing[p.NodeID] = true
				}
			}
		}
	}

	if len(results) == 0 {
		return "No relevant information found.", nil
	}

	out := fmt.Sprintf("Context for: %s\n\n", req.Query)
	for _, r := range results {
		out += fmt.Sprintf("[%s] %s: %s\n", r.NodeType, r.Label, r.Content)
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
	if req.Workspace == "" {
		req.Workspace = "hermes"
	}

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

	return fmt.Sprintf("%s\n\n(Tokens: %d)", content, tokenCount), nil
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

	out := ""
	for _, n := range nodes {
		out += fmt.Sprintf("- [%s] %s (%s, depth %d)\n", n.NodeType, n.Label, n.EdgeType, n.Depth)
	}
	return out, nil
}

func (s *Server) toolCreateEdge(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		SourceID  string  `json:"source_id"`
		TargetID  string  `json:"target_id"`
		EdgeType  string  `json:"edge_type"`
		Weight    float32 `json:"weight,omitempty"`
		Workspace string  `json:"workspace,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}

	w := req.Weight
	edgeType := models.EdgeType(req.EdgeType)
	if !edgeType.IsValid() {
		return nil, fmt.Errorf("invalid edge_type: %s", req.EdgeType)
	}
	edge, err := s.edgeRepo.Create(ctx, models.EdgeCreate{
		WorkspaceName: req.Workspace,
		SourceID:      req.SourceID,
		TargetID:      req.TargetID,
		EdgeType:      edgeType,
		Weight:        &w,
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
