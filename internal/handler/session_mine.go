package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/autocapture"
	"mindbank/internal/dedup"
	"mindbank/internal/embedder"
	"mindbank/internal/reasoner"
)

// SessionMineHandler provides endpoints for mining Hermes sessions into MindBank.
type SessionMineHandler struct {
	pool     *pgxpool.Pool
	reasoner *reasoner.LLMReasoner // optional LLM extractor; nil/disabled => regex
}

// NewSessionMineHandler creates a new session mining handler. Pass a configured
// LLMReasoner to enable LLM-first extraction (falls back to regex otherwise).
func NewSessionMineHandler(pool *pgxpool.Pool, llm *reasoner.LLMReasoner) *SessionMineHandler {
	return &SessionMineHandler{pool: pool, reasoner: llm}
}

// MineRequest represents a request to mine sessions.
type MineRequest struct {
	SessionFiles []string `json:"session_files,omitempty"`
	MineAll      bool     `json:"mine_all,omitempty"`
	Workspace    string   `json:"workspace,omitempty"`
	Namespace    string   `json:"namespace,omitempty"`
}

// MineResponse represents the result of mining sessions.
type MineResponse struct {
	Mined      int      `json:"mined"`
	Failed     int      `json:"failed"`
	Total      int      `json:"total"`
	SessionIDs []string `json:"session_ids,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// KnowledgeItem represents extracted knowledge from a session.
type KnowledgeItem struct {
	Type    string
	Label   string
	Content string
	Summary string
}

// MineSessions handles POST /api/v1/sessions/mine
func (h *SessionMineHandler) MineSessions(w http.ResponseWriter, r *http.Request) {
	var req MineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Default to mining all if no body
		req.MineAll = true
		req.Workspace = "hermes"
	}

	if req.Workspace == "" {
		req.Workspace = "hermes"
	}

	// Find session files
	sessionDir := filepath.Join(os.Getenv("HOME"), ".hermes", "sessions")
	if sessionDir == "" || sessionDir == "/sessions" {
		sessionDir = "/home/rat/.hermes/sessions"
	}

	var sessionFiles []string
	if req.MineAll {
		// Find all session files
		entries, err := os.ReadDir(sessionDir)
		if err != nil {
			respondError(w, 500, fmt.Sprintf("cannot read session dir: %v", err))
			return
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				sessionFiles = append(sessionFiles, filepath.Join(sessionDir, entry.Name()))
			}
		}
	} else {
		sessionFiles = req.SessionFiles
	}

	// Mine each session
	resp := MineResponse{Total: len(sessionFiles)}

	for _, sessionFile := range sessionFiles {
		if err := h.mineSessionFile(sessionFile, req.Workspace, req.Namespace); err != nil {
			resp.Failed++
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", filepath.Base(sessionFile), err))
			slog.Error("session mining failed", "file", sessionFile, "error", err)
		} else {
			resp.Mined++
		}
	}

	respondJSON(w, 200, resp)
}

// mineSessionFile mines a single session file into MindBank.
func (h *SessionMineHandler) mineSessionFile(sessionPath, workspace, namespace string) error {
	ctx := context.Background()

	// Read session file
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Parse session. Hermes files are JSONL (one object per line); older
	// dumps are a single object. Handle both instead of erroring on JSONL.
	// `content` = assistant text (kept for the session node body/dedup hash).
	// `transcript` = role-labeled user+assistant text for LLM extraction, so
	// the extractor sees the user's decisions and preferences too, not just
	// the assistant's output.
	var content, transcript strings.Builder
	appendMsg := func(rec map[string]interface{}) {
		role, _ := rec["role"].(string)
		s, _ := rec["content"].(string)
		if s == "" {
			return
		}
		if role == "assistant" {
			content.WriteString(s)
			content.WriteString("\n\n")
		}
		if role == "user" || role == "assistant" {
			transcript.WriteString(strings.ToUpper(role))
			transcript.WriteString(": ")
			transcript.WriteString(s)
			transcript.WriteString("\n\n")
		}
	}

	var session map[string]interface{}
	if err := json.Unmarshal(data, &session); err == nil {
		// Single object: messages live under a "messages" array
		if messages, ok := session["messages"].([]interface{}); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					appendMsg(msgMap)
				}
			}
		} else {
			appendMsg(session)
		}
	} else {
		// JSONL: one record per line
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var rec map[string]interface{}
			if json.Unmarshal([]byte(line), &rec) == nil {
				appendMsg(rec)
			}
		}
	}

	// Derive namespace from the session's project directory. The robust
	// extractor handles JSONL and scans message bodies for /home/rat/<project>
	// — the single-field checks below only worked on the rare old format and
	// left everything in "global".
	ns := namespace
	if ns == "" {
		ns = autocapture.NamespaceFromSession(data)
	}

	// Insert session node — with deduplication check
	sessionName := filepath.Base(sessionPath)
	if title, ok := session["title"].(string); ok && title != "" {
		sessionName = title
	}
	contentStr := content.String()
	if len(contentStr) > 10000 {
		contentStr = contentStr[:10000] + "..."
	}
	sessionMeta := map[string]interface{}{"source_file": sessionPath}

	// Dedup: hash must be deterministic — use source file path, NOT timestamp
	// so re-mining the same session file is detected as a duplicate
	sessionHash := dedup.ComputeHash(sessionName, contentStr, sessionPath)
	existingSessionID, err := dedup.CheckDuplicate(ctx, h.pool, sessionHash)
	if err != nil {
		slog.Warn("dedup check failed for session", "error", err, "file", sessionPath)
	}
	if existingSessionID != "" {
		// Already mined — skip entirely, no new nodes or edges needed
		return nil
	}

	// Now build the real summary with timestamp for the node record
	sessionSummary := fmt.Sprintf("Session mined on %s", time.Now().Format(time.RFC3339))

	var sessionID string
	err = h.pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary,
		                   metadata, importance, epistemic_label, status, confirmation_count, content_hash)
		VALUES ($1, $2, $3, 'session', $4, $5, $6, 0.5, 'observed', 'open', 0, $7)
		RETURNING id
	`, workspace, ns, sessionName, contentStr, sessionSummary, sessionMeta, sessionHash).Scan(&sessionID)

	if err != nil {
		return fmt.Errorf("create session node: %w", err)
	}

	// Store hash for future dedup checks
	if err := dedup.StoreHash(ctx, h.pool, sessionHash, sessionID); err != nil {
		slog.Warn("store session hash failed", "error", err)
	}

	// Extract knowledge — LLM-first for clean, distilled memories; fall back
	// to the regex extractor when the LLM is disabled or yields nothing.
	var knowledge []KnowledgeItem
	if h.reasoner != nil && h.reasoner.Enabled() {
		transcriptStr := transcript.String()
		if transcriptStr == "" {
			transcriptStr = contentStr
		}
		if res, err := h.reasoner.ExtractFromText(ctx, transcriptStr); err == nil && res != nil {
			for _, n := range res.Nodes {
				knowledge = append(knowledge, KnowledgeItem{Type: n.NodeType, Label: n.Label, Content: n.Content, Summary: n.Summary})
			}
		}
	}
	if len(knowledge) == 0 {
		knowledge = extractKnowledgeFromContent(contentStr)
	}

	// Create knowledge nodes — with deduplication check per node
	for _, item := range knowledge {
		itemHash := dedup.ComputeHash(item.Label, item.Content, item.Summary)

		// Check if this knowledge node already exists
		existingID, err := dedup.CheckDuplicate(ctx, h.pool, itemHash)
		if err != nil {
			slog.Warn("dedup check failed for knowledge node", "error", err, "label", item.Label)
		}

		var nodeID string
		if existingID != "" {
			// Reuse existing node — just create the edge if missing
			nodeID = existingID
		} else {
			err := h.pool.QueryRow(ctx, `
				INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary,
				                   metadata, importance, epistemic_label, status, confirmation_count, content_hash)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 0.5, 'inferred', 'open', 0, $8)
				RETURNING id
			`, workspace, ns, item.Label, item.Type, item.Content, item.Summary,
				map[string]interface{}{"source_session": sessionID}, itemHash).Scan(&nodeID)

			if err != nil {
				slog.Error("failed to create knowledge node", "error", err, "label", item.Label)
				continue
			}

			// Store hash for this new node
			if err := dedup.StoreHash(ctx, h.pool, itemHash, nodeID); err != nil {
				slog.Warn("store knowledge hash failed", "error", err, "label", item.Label)
			}

			// Enqueue for embedding so the memory is semantically recallable
			// (previously these nodes relied on the periodic reconciler).
			if err := embedder.EnqueueNode(ctx, h.pool, nodeID); err != nil {
				slog.Warn("enqueue knowledge embedding failed", "error", err, "node_id", nodeID)
			}
		}

		// Create edge from session to knowledge node (dedup via ON CONFLICT)
		_, _ = h.pool.Exec(ctx, `
			INSERT INTO edges (source_id, target_id, edge_type, weight, workspace_name)
			VALUES ($1, $2, 'produced', 1.0, $3)
			ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
		`, sessionID, nodeID, workspace)
	}

	return nil
}

// extractKnowledgeFromContent extracts knowledge nuggets from session content.
func extractKnowledgeFromContent(content string) []KnowledgeItem {
	var knowledge []KnowledgeItem
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 20 || len(line) > 500 {
			continue
		}

		// Detect decisions
		if strings.Contains(strings.ToLower(line), "decided") ||
			strings.Contains(strings.ToLower(line), "decision") ||
			strings.Contains(strings.ToLower(line), "we will") ||
			strings.Contains(strings.ToLower(line), "let's") {
			knowledge = append(knowledge, KnowledgeItem{
				Type:    "decision",
				Label:   line[:min(100, len(line))],
				Content: line,
				Summary: line[:min(200, len(line))],
			})
			continue
		}

		// Detect facts
		if strings.Contains(strings.ToLower(line), "fact") ||
			strings.Contains(strings.ToLower(line), "note") ||
			strings.Contains(strings.ToLower(line), "remember") {
			knowledge = append(knowledge, KnowledgeItem{
				Type:    "fact",
				Label:   line[:min(100, len(line))],
				Content: line,
				Summary: line[:min(200, len(line))],
			})
			continue
		}

		// Detect problems
		if strings.Contains(strings.ToLower(line), "problem") ||
			strings.Contains(strings.ToLower(line), "issue") ||
			strings.Contains(strings.ToLower(line), "bug") ||
			strings.Contains(strings.ToLower(line), "error") {
			knowledge = append(knowledge, KnowledgeItem{
				Type:    "problem",
				Label:   line[:min(100, len(line))],
				Content: line,
				Summary: line[:min(200, len(line))],
			})
			continue
		}

		// Detect preferences
		if strings.Contains(strings.ToLower(line), "prefer") ||
			strings.Contains(strings.ToLower(line), "should use") ||
			strings.Contains(strings.ToLower(line), "recommend") {
			knowledge = append(knowledge, KnowledgeItem{
				Type:    "preference",
				Label:   line[:min(100, len(line))],
				Content: line,
				Summary: line[:min(200, len(line))],
			})
			continue
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []KnowledgeItem
	for _, item := range knowledge {
		key := strings.ToLower(item.Label[:min(50, len(item.Label))])
		if !seen[key] {
			seen[key] = true
			unique = append(unique, item)
		}
	}

	// Limit to 20 items
	if len(unique) > 20 {
		unique = unique[:20]
	}

	return unique
}

func deriveNamespaceFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return "global"
	}
	path = strings.TrimRight(path, "/")
	base := filepath.Base(path)
	if base == "/" || base == "." || base == "" {
		return "global"
	}
	return base
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RegisterSessionMineRoutes adds session mining endpoints.
func RegisterSessionMineRoutes(r chi.Router, h *SessionMineHandler) {
	r.Route("/sessions", func(r chi.Router) {
		r.Post("/mine", h.MineSessions)
	})
}
