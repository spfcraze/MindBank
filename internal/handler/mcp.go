package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// MCPHealth handles the /mcp/health endpoint
func (h *NodeHandler) MCPHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{
		"status":   "ok",
		"service":  "mindbank-mcp",
		"version":  getLocalVersion(),
		"pid":      fmt.Sprintf("%d", os.Getpid()),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// MCPTools handles the /mcp/tools endpoint
func (h *NodeHandler) MCPTools(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]any{
		{
			"name":        "create_node",
			"description": "Create a new knowledge node in MindBank memory graph",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label": map[string]string{
						"type":        "string",
						"description": "Node label or title",
					},
					"content": map[string]string{
						"type":        "string",
						"description": "Node content or body text",
					},
					"node_type": map[string]string{
						"type":        "string",
						"description": "Type: fact, decision, advice, concept, problem",
					},
					"namespace": map[string]string{
						"type":        "string",
						"description": "Namespace for organization (default: hermes)",
					},
					"summary": map[string]string{
						"type":        "string",
						"description": "Brief summary of the node",
					},
				},
				"required": []string{"label", "content"},
			},
		},
		{
			"name":        "search_nodes",
			"description": "Search for nodes by text query using hybrid search (full-text + vector similarity)",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]string{
						"type":        "string",
						"description": "Search query text",
					},
					"limit": map[string]string{
						"type":        "integer",
						"description": "Maximum number of results (default: 10)",
					},
					"namespace": map[string]string{
						"type":        "string",
						"description": "Filter by namespace (optional)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_node",
			"description": "Retrieve a specific node by ID including its edges and neighbors",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]string{
						"type":        "string",
						"description": "Node UUID",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "create_edge",
			"description": "Create a relationship (edge) between two nodes",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source_id": map[string]string{
						"type":        "string",
						"description": "Source node ID",
					},
					"target_id": map[string]string{
						"type":        "string",
						"description": "Target node ID",
					},
					"relation": map[string]string{
						"type":        "string",
						"description": "Relationship type (e.g., relates_to, depends_on, similar_to)",
					},
					"strength": map[string]string{
						"type":        "number",
						"description": "Edge strength 0.0-1.0 (default: 0.5)",
					},
				},
				"required": []string{"source_id", "target_id", "relation"},
			},
		},
		{
			"name":        "analyze_graph",
			"description": "Get graph analytics including node connections, centrality, and cluster info",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_id": map[string]string{
						"type":        "string",
						"description": "Node to analyze (optional, analyzes whole graph if omitted)",
					},
					"depth": map[string]string{
						"type":        "integer",
						"description": "Graph traversal depth (default: 2)",
					},
				},
				"required": []string{},
			},
		},
	}

	resp := map[string]any{
		"tools": tools,
		"count": len(tools),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
