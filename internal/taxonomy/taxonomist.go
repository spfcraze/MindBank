// Package taxonomy provides auto-classification of MindBank nodes by topic.
// Inspired by gbrain's brain-taxonomist skill.
package taxonomy

import (
	"context"
	"fmt"
	"strings"

	"mindbank/internal/models"
	"mindbank/internal/repository"
)

// KeywordMap maps topic names to sets of keywords for classification.
// Topics are ordered by priority (earlier = higher priority tiebreaker).
var KeywordMap = map[string][]string{
	"security":     {"auth", "password", "encrypt", "vuln", "cve", "exploit", "xss", "sql injection", "penetration", "redteam", "blueteam", "soc", "siem", "firewall", "malware", "phishing"},
	"devops":       {"docker", "kubernetes", "k8s", "deploy", "ci/cd", "pipeline", "terraform", "ansible", "helm", "argo", "prometheus", "grafana", "observability", "logging", "monitoring"},
	"frontend":     {"react", "vue", "angular", "css", "html", "ui", "component", "dom", "browser", "tailwind", "sass", "webpack", "vite", "nextjs", "svelte"},
	"backend":      {"api", "database", "sql", "rest", "grpc", "graphql", "postgres", "redis", "mongodb", "sqlite", "orm", "middleware", "handler", "router", "controller"},
	"ml/ai":        {"model", "train", "llm", "embedding", "neural", "gpt", "claude", "fine-tune", "inference", "transformer", "token", "prompt", "rag", "vector", "classification"},
	"data":         {"csv", "json", "etl", "pipeline", "analytics", "metrics", "dashboard", "visualization", "chart", "plot", "pandas", "dataframe", "aggregation"},
	"project_mgmt": {"roadmap", "milestone", "sprint", "backlog", "jira", "ticket", "kanban", "scrum", "agile", "planning", "timeline", "deliverable"},
	"system_design":{"architecture", "microservice", "monolith", "distributed", "scalability", "latency", "throughput", "cache", "queue", "event-driven", "cqrs"},
	"testing":      {"test", "pytest", "unittest", "integration", "e2e", "mock", "stub", "coverage", "benchmark", "regression", "tdd", "bdd"},
	"gaming":       {"game", "minecraft", "server", "mod", "modpack", "forge", "fabric", "player", "world", "chunk", "entity", "tick"},
}

// ClassifyNode determines the best topic for a node based on its label and content.
// Returns empty string if no topic matches (uncategorized).
func ClassifyNode(node *models.Node) string {
	text := strings.ToLower(node.Label + " " + node.Content)

	bestTopic := ""
	bestScore := 0

	for topic, keywords := range KeywordMap {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestTopic = topic
		}
	}

	return bestTopic
}

// BatchClassify classifies all unclassified nodes in the database.
// Returns (classified_count, error).
func BatchClassify(ctx context.Context, nodeRepo *repository.NodeRepo) (int, error) {
	// Fetch all current nodes — we'll filter in-memory for now
	nodes, err := nodeRepo.List(ctx, "", "", "", "", "", "", 10000, 0)
	if err != nil {
		return 0, fmt.Errorf("fetch nodes: %w", err)
	}

	classified := 0
	for _, node := range nodes {
		// Skip already classified nodes
		if node.Topic != "" {
			continue
		}
		topic := ClassifyNode(&node)
		if topic == "" {
			continue
		}
		// Persist to database
		if err := nodeRepo.UpdateTopic(ctx, node.ID, topic); err != nil {
			continue
		}
		classified++
	}

	return classified, nil
}

// GetTopicDistribution returns the count of nodes per topic.
// Uses stored topics when available, falls back to on-the-fly classification.
func GetTopicDistribution(nodes []models.Node) map[string]int {
	dist := make(map[string]int)
	for _, n := range nodes {
		topic := n.Topic
		if topic == "" {
			topic = ClassifyNode(&n)
		}
		if topic == "" {
			topic = "uncategorized"
		}
		dist[topic]++
	}
	return dist
}

// SuggestConnections analyzes nodes by topic and suggests potential edges
// between nodes in the same topic that aren't already connected.
func SuggestConnections(nodes []models.Node, existingEdges []models.Edge) []SuggestedEdge {
	// Build edge lookup
	edgeSet := make(map[string]bool)
	for _, e := range existingEdges {
		key := e.SourceID + "->" + e.TargetID
		edgeSet[key] = true
	}

	// Use stored topics if available, otherwise classify on-the-fly
	byTopic := make(map[string][]models.Node)
	for _, n := range nodes {
		topic := n.Topic
		if topic == "" {
			topic = ClassifyNode(&n)
		}
		if topic != "" {
			byTopic[topic] = append(byTopic[topic], n)
		}
	}

	var suggestions []SuggestedEdge
	for _, topicNodes := range byTopic {
		if len(topicNodes) < 2 {
			continue
		}
		// Suggest edges between nodes in same topic (limit to avoid explosion)
		for i := 0; i < len(topicNodes) && i < 10; i++ {
			for j := i + 1; j < len(topicNodes) && j < 10; j++ {
				src := topicNodes[i].ID
				tgt := topicNodes[j].ID
				key := src + "->" + tgt
				if !edgeSet[key] {
					suggestions = append(suggestions, SuggestedEdge{
						SourceID: src,
						TargetID: tgt,
						Topic:    topicNodes[i].Label, // Use label as topic indicator since Topic field not in model
						Reason:   "same topic classification",
					})
				}
			}
		}
	}

	return suggestions
}

// SuggestedEdge represents a proposed connection between two nodes.
type SuggestedEdge struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Topic    string `json:"topic"`
	Reason   string `json:"reason"`
}
