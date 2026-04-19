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
	reader := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			slog.Error("stdin read error", "error", err)
			return
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
			if err := encoder.Encode(resp); err != nil {
				slog.Error("encode response", "error", err)
			}
		}
	}
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
	default:
		return s.error(req, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	if err != nil {
		return s.error(req, -32000, err.Error())
	}

	return s.reply(req, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": fmt.Sprintf("%v", result)},
		},
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
		Query     string `json:"query"`
		Workspace string `json:"workspace,omitempty"`
		Namespace string `json:"namespace,omitempty"`
		Limit     int    `json:"limit,omitempty"`
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
		Query     string `json:"query"`
		Workspace string `json:"workspace,omitempty"`
		Namespace string `json:"namespace,omitempty"`
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
	edge, err := s.edgeRepo.Create(ctx, models.EdgeCreate{
		WorkspaceName: req.Workspace,
		SourceID:      req.SourceID,
		TargetID:      req.TargetID,
		EdgeType:      models.EdgeType(req.EdgeType),
		Weight:        &w,
	})
	if err != nil {
		return nil, err
	}

	return fmt.Sprintf("Created edge: %s -> %s (%s, id: %s)", edge.SourceID, edge.TargetID, edge.EdgeType, edge.ID), nil
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
					"type":      map[string]string{"type": "string", "description": "Node type: person, agent, project, topic, decision, fact, event, preference, advice, problem, concept"},
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
			"description": "Search the mindmap using hybrid FTS + semantic search",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]string{"type": "string", "description": "Search query"},
					"workspace": map[string]string{"type": "string", "description": "Filter by workspace"},
					"namespace": map[string]string{"type": "string", "description": "Filter by namespace"},
					"limit":     map[string]string{"type": "integer", "description": "Max results (default: 10)"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "ask",
			"description": "Ask a natural language question and get relevant context from the mindmap",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]string{"type": "string", "description": "Your question"},
					"workspace": map[string]string{"type": "string", "description": "Filter by workspace"},
					"namespace": map[string]string{"type": "string", "description": "Filter by project namespace (isolates memories)"},
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
