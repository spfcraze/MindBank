package reasoner

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"mindbank/internal/embedder"
	"mindbank/internal/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RuleBased extracts nodes and edges from messages using regex and keyword patterns.
type RuleBased struct {
	pool *pgxpool.Pool
}

// Patterns for extraction
var (
	decisionPattern = regexp.MustCompile(`(?i)(we chose|we decided|let'?s use|going with|decided to|decision:|chose to|picked|selected|settled on)\s+(.{10,200})`)
	preferencePattern = regexp.MustCompile(`(?i)(I prefer|user prefers|always use|don'?t use|never use|prefer to|favorite|default is)\s+(.{5,200})`)
	problemPattern = regexp.MustCompile(`(?i)(bug|broken|fails?|error|issue|problem|crash|doesn'?t work|not working|regression)\s*[:\-]?\s*(.{5,200})`)
	advicePattern = regexp.MustCompile(`(?i)(tip|advice|recommend|should always|make sure|remember to|best practice|pro tip)\s*[:\-]?\s*(.{5,200})`)
	urlPattern = regexp.MustCompile(`https?://[^\s\)]+`)
	ipPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	portPattern = regexp.MustCompile(`(?::|port\s+)(\d{2,5})`)
	filePathPattern = regexp.MustCompile(`(?:^|\s)(/[a-zA-Z0-9_\-./]+\.[a-zA-Z0-9]+)\b`)
)

func NewRuleBased(pool *pgxpool.Pool) *RuleBased {
	return &RuleBased{pool: pool}
}

// Pool returns the underlying connection pool.
func (r *RuleBased) Pool() *pgxpool.Pool {
	return r.pool
}

// ExtractedNode represents a node extracted from text.
type ExtractedNode struct {
	Label    string
	NodeType models.NodeType
	Content  string
	Summary  string
}

// Extract analyzes a message and returns extracted nodes.
func (r *RuleBased) Extract(message string) []ExtractedNode {
	var nodes []ExtractedNode

	// Decision extraction
	for _, match := range decisionPattern.FindAllStringSubmatch(message, -1) {
		if len(match) >= 3 {
			content := strings.TrimSpace(match[2])
			label := truncateLabel(content, 80)
			nodes = append(nodes, ExtractedNode{
				Label:    label,
				NodeType: models.NodeDecision,
				Content:  content,
				Summary:  match[1] + " " + truncate(content, 100),
			})
		}
	}

	// Preference extraction
	for _, match := range preferencePattern.FindAllStringSubmatch(message, -1) {
		if len(match) >= 3 {
			content := strings.TrimSpace(match[2])
			label := truncateLabel(content, 80)
			nodes = append(nodes, ExtractedNode{
				Label:    label,
				NodeType: models.NodePreference,
				Content:  content,
				Summary:  match[1] + " " + truncate(content, 100),
			})
		}
	}

	// Problem extraction
	for _, match := range problemPattern.FindAllStringSubmatch(message, -1) {
		if len(match) >= 3 {
			content := strings.TrimSpace(match[2])
			label := truncateLabel(content, 80)
			nodes = append(nodes, ExtractedNode{
				Label:    label,
				NodeType: models.NodeProblem,
				Content:  content,
				Summary:  "Problem: " + truncate(content, 100),
			})
		}
	}

	// Advice extraction
	for _, match := range advicePattern.FindAllStringSubmatch(message, -1) {
		if len(match) >= 3 {
			content := strings.TrimSpace(match[2])
			label := truncateLabel(content, 80)
			nodes = append(nodes, ExtractedNode{
				Label:    label,
				NodeType: models.NodeAdvice,
				Content:  content,
				Summary:  "Advice: " + truncate(content, 100),
			})
		}
	}

	// URL extraction as facts
	for _, url := range urlPattern.FindAllString(message, -1) {
		nodes = append(nodes, ExtractedNode{
			Label:    url,
			NodeType: models.NodeFact,
			Content:  url,
			Summary:  "URL: " + url,
		})
	}

	// IP extraction as facts
	for _, ip := range ipPattern.FindAllString(message, -1) {
		nodes = append(nodes, ExtractedNode{
			Label:    "IP: " + ip,
			NodeType: models.NodeFact,
			Content:  ip,
			Summary:  "IP address: " + ip,
		})
	}

	return nodes
}

// ProcessAndStore extracts nodes from a message and creates them in the database.
func (r *RuleBased) ProcessAndStore(ctx context.Context, sessionID, workspace, namespace string, message string) error {
	// Verify session exists before linking — prevents FK violations on orphaned async jobs
	var sessExists int
	_ = r.pool.QueryRow(ctx, `SELECT 1 FROM sessions WHERE id = $1`, sessionID).Scan(&sessExists)
	if sessExists != 1 {
		return nil
	}
	extracted := r.Extract(message)
	if len(extracted) == 0 {
		return nil
	}

	for _, ext := range extracted {
		// Check if node already exists
		var existingID string
		err := r.pool.QueryRow(ctx, `
			SELECT id FROM nodes
			WHERE workspace_name = $1 AND label = $2 AND node_type = $3 AND valid_to IS NULL
			LIMIT 1
		`, workspace, ext.Label, ext.NodeType).Scan(&existingID)

		if err == nil {
			// Node exists — link to session
			_, _ = r.pool.Exec(ctx, `
				INSERT INTO session_nodes (session_id, node_id)
				VALUES ($1, $2)
				ON CONFLICT (session_id, node_id) DO UPDATE
				SET mention_count = session_nodes.mention_count + 1, last_mentioned = now()
			`, sessionID, existingID)
			continue
		}

		// Create new node
		var nodeID string
		err = r.pool.QueryRow(ctx, `
			INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, workspace, namespace, ext.Label, ext.NodeType, ext.Content, ext.Summary,
		).Scan(&nodeID)
		if err != nil {
			slog.Warn("failed to create extracted node", "label", ext.Label, "error", err)
			continue
		}

		// Enqueue for embedding
		_, _ = r.pool.Exec(ctx, `INSERT INTO embedding_queue (source_type, source_id) VALUES ('node', $1)`, nodeID)

		// Link to session
		_, _ = r.pool.Exec(ctx, `
			INSERT INTO session_nodes (session_id, node_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, sessionID, nodeID)

		slog.Debug("extracted node", "type", ext.NodeType, "label", ext.Label)
	}

	return nil
}

func truncateLabel(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// LLMReasoner uses an LLM for more sophisticated extraction.
// For now, this is a placeholder — you'd call your LLM API here.
type LLMReasoner struct {
	pool     *pgxpool.Pool
	embedder *embedder.Client
	apiURL   string // OpenAI-compatible endpoint
	apiKey   string
	model    string
}

func NewLLMReasoner(pool *pgxpool.Pool, emb *embedder.Client, apiURL, apiKey, model string) *LLMReasoner {
	return &LLMReasoner{
		pool:     pool,
		embedder: emb,
		apiURL:   apiURL,
		apiKey:   apiKey,
		model:    model,
	}
}

// ExtractionResult is what the LLM returns.
type ExtractionResult struct {
	Nodes []ExtractedFact `json:"nodes"`
}

type ExtractedFact struct {
	Label    string `json:"label"`
	NodeType string `json:"type"`
	Content  string `json:"content"`
	Summary  string `json:"summary"`
}

// ExtractBatch sends a batch of messages to the LLM for extraction.
// This is a stub — implement when you have an LLM API configured.
func (l *LLMReasoner) ExtractBatch(ctx context.Context, messages []string) (*ExtractionResult, error) {
	// TODO: Implement LLM-based extraction
	// For now, concatenate messages and use a simple heuristic prompt
	combined := strings.Join(messages, "\n---\n")

	// If no LLM configured, return empty
	if l.apiURL == "" {
		return &ExtractionResult{}, nil
	}

	// Build extraction prompt
	prompt := `Analyze the following conversation and extract key facts, decisions, preferences, problems, and advice.
Return a JSON object with a "nodes" array. Each node has: "label" (short name), "type" (decision/fact/preference/problem/advice/event/person/project), "content" (full text), "summary" (one-line summary).

Conversation:
` + combined + `

Return ONLY valid JSON, no other text.`

	// Call LLM API (OpenAI-compatible)
	body, _ := json.Marshal(map[string]any{
		"model": l.model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a fact extractor. Return only valid JSON."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
		"max_tokens":  2000,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", l.apiURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Parse response
	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, err
	}

	if len(llmResp.Choices) == 0 {
		return &ExtractionResult{}, nil
	}

	// Parse the JSON from the LLM response
	content := llmResp.Choices[0].Message.Content
	// Strip markdown code blocks if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSpace(content)

	var result ExtractionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		slog.Warn("failed to parse LLM extraction", "error", err, "content", content[:min(200, len(content))])
		return &ExtractionResult{}, nil
	}

	return &result, nil
}

// ProcessAndStoreLLM extracts nodes using LLM and stores them.
func (l *LLMReasoner) ProcessAndStoreLLM(ctx context.Context, sessionID, workspace, namespace string, messages []string) error {
	// Verify session exists before linking — prevents FK violations on orphaned async jobs
	var sessExists int
	_ = l.pool.QueryRow(ctx, `SELECT 1 FROM sessions WHERE id = $1`, sessionID).Scan(&sessExists)
	if sessExists != 1 {
		return nil
	}
	result, err := l.ExtractBatch(ctx, messages)
	if err != nil {
		return err
	}

	for _, fact := range result.Nodes {
		var nodeID string
		err := l.pool.QueryRow(ctx, `
			INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (workspace_name, label, node_type) WHERE valid_to IS NULL
			DO UPDATE SET content = EXCLUDED.content, summary = EXCLUDED.summary, updated_at = now()
			RETURNING id
		`, workspace, namespace, fact.Label, fact.NodeType, fact.Content, fact.Summary,
		).Scan(&nodeID)
		if err != nil {
			slog.Warn("llm: failed to store node", "label", fact.Label, "error", err)
			continue
		}

		_, _ = l.pool.Exec(ctx, `INSERT INTO embedding_queue (source_type, source_id) VALUES ('node', $1)`, nodeID)
		_, _ = l.pool.Exec(ctx, `INSERT INTO session_nodes (session_id, node_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, sessionID, nodeID)
	}

	return nil
}
