package autocapture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mindbank/internal/models"
	"mindbank/internal/repository"
)

// Watcher watches for Hermes session files and creates namespace-aware sessions.
type Watcher struct {
	sessionRepo *repository.SessionRepo
	nodeRepo    *repository.NodeRepo
	watchPath   string
}

// NewWatcher creates a new namespace-aware auto-capture watcher.
func NewWatcher(sessionRepo *repository.SessionRepo, nodeRepo *repository.NodeRepo, watchPath string) *Watcher {
	return &Watcher{
		sessionRepo: sessionRepo,
		nodeRepo:    nodeRepo,
		watchPath:   watchPath,
	}
}

// ProcessFile reads a session file, derives namespace, and creates a session.
func (w *Watcher) ProcessFile(ctx context.Context, path string) error {
	// Read the session file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read session file: %w", err)
	}

	// Derive namespace from session data
	namespace, err := ParseSessionForNamespace(data)
	if err != nil {
		slog.Warn("failed to parse session for namespace, using global", "path", path, "error", err)
		namespace = "global"
	}

	// Extract label from filename or session content
	label := w.deriveLabel(path, data)

	// Extract topic for metadata
	topic := w.extractTopic(data)
	meta := map[string]interface{}{}
	if topic != "" {
		meta["topic"] = topic
	}
	metaBytes, _ := json.Marshal(meta)

	// Create session in sessions table (not as a graph node)
	session, err := w.sessionRepo.Create(ctx, models.SessionCreate{
		WorkspaceName: "hermes",
		Name:          label,
		Metadata:      metaBytes,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	slog.Info("session captured",
		"path", path,
		"session_id", session.ID,
		"namespace", namespace,
	)

	return nil
}

// deriveLabel extracts a label from the filename or session content.
func (w *Watcher) deriveLabel(path string, data []byte) string {
	// Try to get from filename first
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Always try to extract title from content first (for any file)
	if title := w.extractTitle(data); title != "" {
		return title
	}

	// Fallback to filename
	return name
}

// isTimestampName checks if filename looks like a timestamp.
func (w *Watcher) isTimestampName(name string) bool {
	// Matches patterns like session_2026-04-08_192513 or 20260408_192513
	if len(name) >= 15 {
		// Check for date pattern YYYY-MM-DD (with dashes)
		if len(name) > 7 && name[4] == '-' && name[7] == '-' {
			return true
		}
		// Check for date pattern YYYYMMDD (no dashes, positions after prefix)
		// e.g., "session_20260408_192513" — find the date part
		if strings.Contains(name, "_") {
			parts := strings.Split(name, "_")
			for _, part := range parts {
				if len(part) == 8 {
					// Check if it's all digits (YYYYMMDD)
					isAllDigits := true
					for _, c := range part {
						if c < '0' || c > '9' {
							isAllDigits = false
							break
						}
					}
					if isAllDigits {
						return true
					}
				}
			}
		}
	}
	return false
}

// extractTitle tries to extract a meaningful title from session JSON.
// Priority: 1) JSON title field, 2) First user message, 3) Key topic words.
func (w *Watcher) extractTitle(data []byte) string {
	var session struct {
		Title    string `json:"title"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(data, &session); err != nil {
		return ""
	}

	// Priority 1: Explicit title field
	if session.Title != "" {
		return w.sanitizeTitle(session.Title)
	}

	// Priority 2: Extract from first meaningful user message
	var userMessages []string
	for _, msg := range session.Messages {
		if msg.Role == "user" && len(strings.TrimSpace(msg.Content)) > 10 {
			userMessages = append(userMessages, msg.Content)
		}
	}

	if len(userMessages) == 0 {
		return ""
	}

	// Priority 3: Generate title from first user message
	firstMsg := userMessages[0]
	title := w.generateTitleFromMessage(firstMsg)

	if title != "" {
		return title
	}

	// Fallback: first 60 chars of first message
	if len(firstMsg) > 60 {
		return w.sanitizeTitle(firstMsg[:57] + "...")
	}
	return w.sanitizeTitle(firstMsg)
}

// generateTitleFromMessage extracts key topics from a message to form a title
func (w *Watcher) generateTitleFromMessage(msg string) string {
	// Common technical keywords and their display forms
	topicKeywords := map[string]string{
		"deploy": "Deployment", "docker": "Docker", "kubernetes": "Kubernetes", "k8s": "K8s",
		"api": "API", "backend": "Backend", "frontend": "Frontend",
		"postgres": "Postgres", "database": "Database", "redis": "Redis",
		"auth": "Auth", "oauth": "OAuth", "jwt": "JWT",
		"bug": "Bugfix", "fix": "Fix", "error": "Error",
		"refactor": "Refactor", "optimize": "Optimize", "performance": "Performance",
		"test": "Testing", "testing": "Testing", "ci": "CI/CD",
		"git": "Git", "github": "GitHub", "merge": "Merge",
		"config": "Config", "setup": "Setup", "install": "Install",
		"python": "Python", "go": "Go", "rust": "Rust", "javascript": "JavaScript",
		"react": "React", "vue": "Vue",
	}

	lowerMsg := strings.ToLower(msg)
	var foundTopics []string

	for keyword, display := range topicKeywords {
		if strings.Contains(lowerMsg, keyword) {
			foundTopics = append(foundTopics, display)
		}
	}

	// Remove duplicates while preserving order
	seen := make(map[string]bool)
	var unique []string
	for _, t := range foundTopics {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}

	if len(unique) == 0 {
		return ""
	}

	// Build title from top 3 topics
	if len(unique) > 3 {
		unique = unique[:3]
	}
	return strings.Join(unique, " & ")
}

// sanitizeTitle cleans and formats a title string
func (w *Watcher) sanitizeTitle(title string) string {
	title = strings.TrimSpace(title)

	// Remove special characters except alphanumeric, spaces, hyphens
	var result []rune
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' {
			result = append(result, r)
		}
	}
	title = string(result)

	// Capitalize first letter of each word
	words := strings.Fields(title)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	title = strings.Join(words, " ")

	// Truncate if too long
	if len(title) > 60 {
		title = title[:57] + "..."
	}

	if title == "" {
		return "Untitled Session"
	}

	return title
}

// extractTopic analyzes session content and returns a topic tag for metadata.
// Returns empty string if no strong topic signal detected.
func (w *Watcher) extractTopic(data []byte) string {
	var session struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(data, &session); err != nil {
		return ""
	}

	// Combine all user messages
	var userText []string
	for _, msg := range session.Messages {
		if msg.Role == "user" {
			userText = append(userText, strings.ToLower(msg.Content))
		}
	}

	fullText := strings.Join(userText, " ")

	// Topic scoring
	topics := map[string][]string{
		"deployment":  {"deploy", "docker", "kubernetes", "k8s", "production", "staging", "release"},
		"database":    {"postgres", "postgresql", "sql", "database", "db", "migration", "schema", "query"},
		"api":         {"api", "endpoint", "rest", "graphql", "grpc", "swagger", "http", "route"},
		"auth":        {"auth", "oauth", "jwt", "login", "password", "token", "session"},
		"frontend":    {"react", "vue", "angular", "css", "html", "dom", "component", "ui"},
		"bugfix":      {"bug", "fix", "error", "crash", "exception", "debug", "breakpoint"},
		"refactoring": {"refactor", "clean", "rewrite", "restructure", "simplify", "optimize"},
		"testing":     {"test", "testing", "pytest", "jest", "unit test", "integration", "mock"},
		"config":      {"config", "setup", "install", "environment", "env", "variable", "setting"},
		"architecture": {"architecture", "design", "pattern", "microservice", "service", "module"},
	}

	scores := make(map[string]int)
	for topic, keywords := range topics {
		for _, kw := range keywords {
			if strings.Contains(fullText, kw) {
				scores[topic]++
			}
		}
	}

	// Find highest scoring topic (need at least 1 match for confidence)
	bestTopic := ""
	bestScore := 0
	for topic, score := range scores {
		if score > bestScore {
			bestScore = score
			bestTopic = topic
		}
	}

	return bestTopic
}

// deriveSummary creates a brief summary from session data.
func (w *Watcher) deriveSummary(data []byte) string {
	var session struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(data, &session); err != nil {
		// If not JSON, return first 200 chars
		if len(data) > 200 {
			return string(data[:200]) + "..."
		}
		return string(data)
	}

	// Count messages
	userCount := 0
	assistantCount := 0
	for _, msg := range session.Messages {
		if msg.Role == "user" {
			userCount++
		} else if msg.Role == "assistant" {
			assistantCount++
		}
	}

	return fmt.Sprintf("Session with %d user and %d assistant messages", userCount, assistantCount)
}

// parseEvent parses a JSON event file into an EventNode.
func (w *Watcher) parseEvent(data []byte) (*models.EventNode, error) {
	var raw struct {
		SessionID string          `json:"session_id"`
		EventType string          `json:"event_type"`
		Sequence  int             `json:"sequence"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal event: %w", err)
	}

	ts, _ := time.Parse(time.RFC3339Nano, raw.Timestamp)
	if ts.IsZero() {
		ts = time.Now()
	}

	return &models.EventNode{
		SessionID: raw.SessionID,
		EventType: raw.EventType,
		Sequence:  raw.Sequence,
		Timestamp: ts,
		Payload:   raw.Payload,
	}, nil
}

// ProcessEventFile reads an event file and creates an event node linked to its session.
func (w *Watcher) ProcessEventFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read event file: %w", err)
	}

	event, err := w.parseEvent(data)
	if err != nil {
		return fmt.Errorf("parse event: %w", err)
	}

	// Validate event type
	validTypes := models.ValidEventTypes()
	isValid := false
	for _, vt := range validTypes {
		if event.EventType == vt {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("invalid event type: %s", event.EventType)
	}

	// Create event node — use session_id as namespace for grouping
	req := models.NodeCreate{
		WorkspaceName: "hermes",
		Namespace:     event.SessionID,
		Label:         fmt.Sprintf("%s #%d", event.EventType, event.Sequence),
		NodeType:      models.NodeType("event"),
		Content:       string(data),
		Summary:       fmt.Sprintf("Event %s in session %s", event.EventType, event.SessionID),
		Metadata:      data,
	}

	node, err := w.nodeRepo.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create event node: %w", err)
	}

	slog.Info("event captured",
		"path", path,
		"node_id", node.ID,
		"session_id", event.SessionID,
		"event_type", event.EventType,
		"sequence", event.Sequence,
	)

	return nil
}

// Start begins watching the session directory for new files.
func (w *Watcher) Start(ctx context.Context) error {
	// Ensure watch path exists
	if err := os.MkdirAll(w.watchPath, 0755); err != nil {
		return fmt.Errorf("create watch path: %w", err)
	}

	// Process existing files first
	entries, err := os.ReadDir(w.watchPath)
	if err != nil {
		slog.Warn("failed to read watch directory", "path", w.watchPath, "error", err)
	} else {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Skip request dump files (failed API error artifacts)
			if strings.HasPrefix(name, "request_dump_") {
				continue
			}
			path := filepath.Join(w.watchPath, name)
			if err := w.ProcessFile(ctx, path); err != nil {
				slog.Warn("failed to process existing file", "path", path, "error", err)
			}
		}
	}

	slog.Info("namespace-aware auto-capture watcher started", "path", w.watchPath)

	// Note: Continuous file watching would require fsnotify integration
	// For now, this processes existing files on startup
	// A full implementation would start a goroutine with fsnotify.Watcher

	return nil
}

// Watch continuously monitors for new session files.
// This is a placeholder for future fsnotify-based implementation.
func (w *Watcher) Watch(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	processed := make(map[string]time.Time)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scanForNewFiles(ctx, processed)
		}
	}
}

// scanForNewFiles scans the watch directory for new files.
func (w *Watcher) scanForNewFiles(ctx context.Context, processed map[string]time.Time) {
	// Scan session files
	entries, err := os.ReadDir(w.watchPath)
	if err != nil {
		slog.Warn("failed to scan watch directory", "path", w.watchPath, "error", err)
		return
	}

	now := time.Now()
	// Clean up old entries (older than 1 hour)
	for path, t := range processed {
		if now.Sub(t) > time.Hour {
			delete(processed, path)
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip request dump files (failed API error artifacts)
		if strings.HasPrefix(name, "request_dump_") {
			continue
		}

		path := filepath.Join(w.watchPath, name)
		// Skip recently processed files
		if _, ok := processed[path]; ok {
			continue
		}

		// Check file modification time
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Only process files modified in the last minute
		if now.Sub(info.ModTime()) > time.Minute {
			continue
		}

		if err := w.ProcessFile(ctx, path); err != nil {
			slog.Warn("failed to process file", "path", path, "error", err)
		} else {
			processed[path] = now
		}
	}

	// Scan event files
	w.scanEventFiles(ctx, processed, now)
}

// scanEventFiles scans the events directory for new event files.
func (w *Watcher) scanEventFiles(ctx context.Context, processed map[string]time.Time, now time.Time) {
	eventsDir := filepath.Join(w.watchPath, "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		// Events dir might not exist yet — that's ok
		if !os.IsNotExist(err) {
			slog.Warn("failed to scan events directory", "path", eventsDir, "error", err)
		}
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Events are in session subdirectories
		}

		sessionDir := filepath.Join(eventsDir, entry.Name())
		eventFiles, err := os.ReadDir(sessionDir)
		if err != nil {
			continue
		}

		for _, eventFile := range eventFiles {
			if eventFile.IsDir() {
				continue
			}

			path := filepath.Join(sessionDir, eventFile.Name())
			// Skip recently processed
			if _, ok := processed[path]; ok {
				continue
			}

			// Check modification time
			info, err := eventFile.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > time.Minute {
				continue
			}

			if err := w.ProcessEventFile(ctx, path); err != nil {
				slog.Warn("failed to process event file", "path", path, "error", err)
			} else {
				processed[path] = now
			}
		}
	}
}
