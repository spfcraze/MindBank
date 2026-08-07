package reasoner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

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
		if err := embedder.EnqueueNode(ctx, r.pool, nodeID); err != nil {
			slog.Warn("failed to enqueue embedding", "node_id", nodeID, "error", err)
		}

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
// SettingsGetter is the minimal read interface the reasoner needs to pick up
// runtime LLM config saved from the dashboard (DB overrides env defaults).
type SettingsGetter interface {
	Get(ctx context.Context, key string) (string, error)
}

type LLMReasoner struct {
	pool     *pgxpool.Pool
	embedder *embedder.Client
	defURL   string // env default OpenAI-compatible endpoint
	defKey   string
	defModel string
	settings SettingsGetter // optional runtime overrides (dashboard-configurable)
}

func NewLLMReasoner(pool *pgxpool.Pool, emb *embedder.Client, apiURL, apiKey, model string, settings SettingsGetter) *LLMReasoner {
	return &LLMReasoner{
		pool:     pool,
		embedder: emb,
		defURL:   apiURL,
		defKey:   apiKey,
		defModel: model,
		settings: settings,
	}
}

// resolve returns the effective LLM config: dashboard-saved DB settings
// (llm_api_url / llm_api_key / llm_model) override the env defaults. Reading is
// cheap and happens per extraction (infrequent), so config changes take effect
// live without a restart.
func (l *LLMReasoner) resolve() (url, key, model string) {
	url, key, model = l.defURL, l.defKey, l.defModel
	if l.settings != nil {
		ctx := context.Background()
		if v, _ := l.settings.Get(ctx, "llm_api_url"); v != "" {
			url = v
		}
		if v, _ := l.settings.Get(ctx, "llm_api_key"); v != "" {
			key = v
		}
		if v, _ := l.settings.Get(ctx, "llm_model"); v != "" {
			model = v
		}
	}
	return url, key, model
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

// Enabled reports whether LLM extraction is configured (a URL and model exist,
// from either the dashboard settings or env).
func (l *LLMReasoner) Enabled() bool {
	if l == nil {
		return false
	}
	url, _, model := l.resolve()
	return url != "" && model != ""
}

const extractionSystemPrompt = `You extract durable, reusable memories from a coding-assistant work session for a long-term memory system. Return ONLY a JSON object {"nodes":[...]} and nothing else.`

const extractionUserPrompt = `Extract the memories worth remembering across FUTURE sessions from the session below.

Each node in "nodes" must have:
- "label": a concise 3-8 word noun phrase naming the memory. PLAIN TEXT ONLY — no markdown (no **, #, backticks), no leading bullet or number, no trailing colon. E.g. "Local Postgres DSN", "Decision to use pgvector HNSW index".
- "type": exactly one of: decision, fact, preference, problem, advice, person, project, concept, event.
- "content": 1-3 complete sentences stating the memory precisely and self-containedly, useful WITHOUT the transcript.
- "summary": one short sentence.

Only include: concrete decisions made, technical facts learned, user/project preferences, bugs/problems and their resolution status, and key entities (people, projects, tools). Merge duplicates. Ignore chit-chat, raw tool output, code dumps, and anything transient. Prefer 5-20 high-quality memories over many low-quality ones. If nothing durable, return {"nodes":[]}.

Session:
%s

Return ONLY the JSON object.`

// ExtractFromText runs LLM-based extraction over a block of session text and
// returns sanitized, deduplicated memory nodes. Falls back to an empty result
// (never an error the caller must handle) when the LLM is unavailable.
func (l *LLMReasoner) ExtractFromText(ctx context.Context, text string) (*ExtractionResult, error) {
	url, key, model := l.resolve()
	if url == "" || model == "" || strings.TrimSpace(text) == "" {
		return &ExtractionResult{}, nil
	}
	// Bound input so a huge transcript can't blow the context window.
	const maxInput = 24000
	if len(text) > maxInput {
		text = text[:maxInput]
	}

	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": extractionSystemPrompt},
			{"role": "user", "content": fmt.Sprintf(extractionUserPrompt, text)},
		},
		"temperature": 0,
		"stream":      false,
		"max_tokens":  2000, // bound generation time; extractions are small
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return &ExtractionResult{}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("llm extract: request failed, will fall back", "error", err)
		return &ExtractionResult{}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		slog.Warn("llm extract: non-200", "status", resp.StatusCode)
		return &ExtractionResult{}, nil
	}

	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil || len(llmResp.Choices) == 0 {
		return &ExtractionResult{}, nil
	}

	raw := extractJSONObject(llmResp.Choices[0].Message.Content)
	var result ExtractionResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("llm extract: unparseable JSON, falling back", "error", err)
		return &ExtractionResult{}, nil
	}
	result.Nodes = sanitizeExtracted(result.Nodes)
	// Free VRAM immediately after extraction (local Ollama only) so the model
	// doesn't linger in memory for Ollama's default keep-alive window. Cloud
	// endpoints (with an API key / non-11434 host) are left alone.
	l.unloadIfLocal(url, key, model)
	return &result, nil
}

// unloadIfLocal asks a local Ollama to drop the extraction model from memory
// right after use (keep_alive: 0), so it only occupies VRAM during the few
// seconds of extraction. No-op for cloud/OpenAI-compatible endpoints.
func (l *LLMReasoner) unloadIfLocal(url, key, model string) {
	if key != "" || !strings.Contains(url, "11434") {
		return // cloud endpoint — don't try to unload
	}
	base := strings.TrimSuffix(strings.TrimSuffix(url, "/"), "/v1")
	body, _ := json.Marshal(map[string]any{"model": model, "keep_alive": 0})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req); err == nil {
		resp.Body.Close()
	}
}

// extractJSONObject pulls the first balanced {...} out of an LLM reply,
// tolerating ```json fences and surrounding prose.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

var validExtractTypes = map[string]bool{
	"decision": true, "fact": true, "preference": true, "problem": true,
	"advice": true, "person": true, "project": true, "concept": true, "event": true,
}

// sanitizeExtracted cleans labels, validates types, drops empties, and
// deduplicates — a guard against messy LLM output regardless of the prompt.
func sanitizeExtracted(nodes []ExtractedFact) []ExtractedFact {
	out := make([]ExtractedFact, 0, len(nodes))
	seen := make(map[string]bool)
	for _, n := range nodes {
		label := cleanLabel(n.Label)
		content := strings.TrimSpace(n.Content)
		if label == "" || content == "" {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(n.NodeType))
		if !validExtractTypes[t] {
			t = "fact"
		}
		key := strings.ToLower(label)
		if seen[key] {
			continue
		}
		seen[key] = true
		summary := strings.TrimSpace(n.Summary)
		if summary == "" {
			summary = firstSentence(content)
		}
		out = append(out, ExtractedFact{Label: label, NodeType: t, Content: content, Summary: summary})
	}
	return out
}

// cleanLabel strips markdown, list markers, and trailing punctuation so labels
// are clean noun phrases instead of transcript fragments.
func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	// Drop leading list/heading markers: -, *, #, 1., 2)
	s = regexp.MustCompile(`^[\s>#*\-]+`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`^\d+[.)]\s*`).ReplaceAllString(s, "")
	// Strip markdown emphasis and backticks
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	// Collapse whitespace, trim trailing punctuation
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, " :.-—")
	if len(s) > 100 {
		s = strings.TrimSpace(s[:100])
	}
	return s
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ".!?"); i > 0 && i < 200 {
		return s[:i+1]
	}
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}

// ExtractBatch sends a batch of messages to the LLM for extraction.
// This is a stub — implement when you have an LLM API configured.
func (l *LLMReasoner) ExtractBatch(ctx context.Context, messages []string) (*ExtractionResult, error) {
	// TODO: Implement LLM-based extraction
	// For now, concatenate messages and use a simple heuristic prompt
	combined := strings.Join(messages, "\n---\n")

	url, key, model := l.resolve()
	// If no LLM configured, return empty
	if url == "" || model == "" {
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
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a fact extractor. Return only valid JSON."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
		"max_tokens":  2000,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
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

		// EnqueueNode resets an existing queue row to pending; the previous
		// bare INSERT hit the unique constraint whenever the ON CONFLICT
		// branch above refreshed content, permanently leaving the embedding
		// keyed to the OLD text (recall then matched the outdated meaning).
		if err := embedder.EnqueueNode(ctx, l.pool, nodeID); err != nil {
			slog.Warn("llm: failed to enqueue embedding", "node_id", nodeID, "error", err)
		}
		_, _ = l.pool.Exec(ctx, `INSERT INTO session_nodes (session_id, node_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, sessionID, nodeID)
	}

	return nil
}
