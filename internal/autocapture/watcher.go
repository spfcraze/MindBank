package autocapture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mindbank/internal/models"
	"mindbank/internal/repository"
)

// quiescencePeriod is how long a file must be unmodified before ingestion.
// Session files are processed only after the writer has plausibly finished;
// ingesting a file mid-write captured truncated JSON and then permanently
// skipped the completed session.
const quiescencePeriod = 30 * time.Second

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

// contentHash returns the ledger key for a file's current content.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// alreadyProcessed reports whether this exact (path, content) pair was
// already ingested. A ledger miss for a changed file means the new content
// gets processed even if an earlier (e.g. truncated) version was recorded.
func (w *Watcher) alreadyProcessed(ctx context.Context, path, hash string) bool {
	var exists bool
	err := w.sessionRepo.Pool().QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM autocapture_files WHERE path = $1 AND content_hash = $2)
	`, path, hash).Scan(&exists)
	if err != nil {
		slog.Warn("autocapture ledger check failed", "path", path, "error", err)
		return false
	}
	return exists
}

// recordProcessed writes the ledger entry for a processed file.
func (w *Watcher) recordProcessed(ctx context.Context, path, hash, outcome, sessionID string) {
	var sid any
	if sessionID != "" {
		sid = sessionID
	}
	if _, err := w.sessionRepo.Pool().Exec(ctx, `
		INSERT INTO autocapture_files (path, content_hash, outcome, session_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (path, content_hash) DO NOTHING
	`, path, hash, outcome, sid); err != nil {
		slog.Warn("autocapture ledger write failed", "path", path, "error", err)
	}
}

// ProcessFile reads a session file, derives namespace, and creates a session.
// Quality gates applied to prevent noise injection:
//   - Skips files already ingested with identical content (durable ledger)
//   - Skips files with generic/test labels
//   - Skips sessions with no meaningful content
//   - Skips duplicate sessions (same label + namespace within 1 hour)
func (w *Watcher) ProcessFile(ctx context.Context, path string) error {
	// Read the session file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read session file: %w", err)
	}

	hash := contentHash(data)
	if w.alreadyProcessed(ctx, path, hash) {
		return nil
	}

	// Derive namespace from session data
	namespace, err := ParseSessionForNamespace(data)
	if err != nil {
		slog.Warn("failed to parse session for namespace, using global", "path", path, "error", err)
		namespace = "global"
	}

	// Extract label from filename or session content
	label := w.deriveLabel(path, data)

	// QUALITY GATE 1: Skip generic/test labels
	if w.isLowQualityLabel(label) {
		slog.Info("skipping low-quality session", "path", path, "label", label)
		w.recordProcessed(ctx, path, hash, "skipped_low_quality", "")
		return nil
	}

	// QUALITY GATE 2: Skip sessions with no meaningful content
	if !w.hasMeaningfulContent(data) {
		slog.Info("skipping empty session", "path", path)
		w.recordProcessed(ctx, path, hash, "skipped_empty", "")
		return nil
	}

	// QUALITY GATE 3: Skip duplicate sessions (same label + namespace within 1 hour)
	if w.isDuplicateSession(ctx, label, namespace) {
		slog.Info("skipping duplicate session", "path", path, "label", label, "namespace", namespace)
		w.recordProcessed(ctx, path, hash, "skipped_duplicate", "")
		return nil
	}

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
	w.recordProcessed(ctx, path, hash, "ingested", session.ID)

	slog.Info("session captured",
		"path", path,
		"session_id", session.ID,
		"namespace", namespace,
	)

	return nil
}

// isLowQualityLabel checks if a label indicates test/simulation noise.
func (w *Watcher) isLowQualityLabel(label string) bool {
	lower := strings.ToLower(label)
	prefixes := []string{"sim:", "test:", "worker", "session_", "untitled", "no metadata", "clean path"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) || strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// hasMeaningfulContent checks if the session has actual user content.
func (w *Watcher) hasMeaningfulContent(data []byte) bool {
	var session struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return false
	}
	// Require at least one user message with >20 chars
	for _, msg := range session.Messages {
		if msg.Role == "user" && len(strings.TrimSpace(msg.Content)) > 20 {
			return true
		}
	}
	return false
}

// isDuplicateSession checks if a similar session was created recently.
func (w *Watcher) isDuplicateSession(ctx context.Context, label, namespace string) bool {
	// Check for existing session with same name in last hour
	recent, err := w.sessionRepo.ListRecent(ctx, "hermes", namespace, time.Hour)
	if err != nil {
		return false
	}
	for _, s := range recent {
		if strings.EqualFold(s.Name, label) {
			return true
		}
	}
	return false
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

	hash := contentHash(data)
	if w.alreadyProcessed(ctx, path, hash) {
		return nil
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
	w.recordProcessed(ctx, path, hash, "ingested_event", "")

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

	// Process existing files first. The durable ledger inside ProcessFile
	// makes this restart-safe: previously every boot re-ingested the whole
	// directory with only a 1-hour dedup window.
	w.scanForNewFiles(ctx, make(map[string]fileStamp))

	slog.Info("namespace-aware auto-capture watcher started", "path", w.watchPath)

	// Note: Continuous file watching would require fsnotify integration
	// For now, this processes existing files on startup
	// A full implementation would start a goroutine with fsnotify.Watcher

	return nil
}

// fileStamp identifies a file state already handled this process lifetime,
// so unchanged files don't get re-hashed and re-checked every scan tick.
type fileStamp struct {
	modTime time.Time
	size    int64
}

// Watch continuously monitors for new session files.
// This is a placeholder for future fsnotify-based implementation.
func (w *Watcher) Watch(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	seen := make(map[string]fileStamp)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scanForNewFiles(ctx, seen)
		}
	}
}

// shouldProcess applies the in-memory stamp cache and the write-quiescence
// gate. Returning false with no stamp update means the file is retried on a
// later scan (e.g. still being written).
func shouldProcess(seen map[string]fileStamp, path string, info fs.FileInfo, now time.Time) bool {
	stamp := fileStamp{modTime: info.ModTime(), size: info.Size()}
	if prev, ok := seen[path]; ok && prev == stamp {
		return false
	}
	// Quiescence: skip files still (plausibly) being written. The old logic
	// was inverted — it processed ONLY files modified in the last minute,
	// i.e. exactly the ones most likely mid-write, and skipped everything
	// older forever.
	if now.Sub(info.ModTime()) < quiescencePeriod {
		return false
	}
	return true
}

// scanForNewFiles scans the watch directory for new files.
func (w *Watcher) scanForNewFiles(ctx context.Context, seen map[string]fileStamp) {
	// Scan session files
	entries, err := os.ReadDir(w.watchPath)
	if err != nil {
		slog.Warn("failed to scan watch directory", "path", w.watchPath, "error", err)
		return
	}

	now := time.Now()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip request dump files (failed API error artifacts) and
		// in-flight temp files from atomic writers
		if strings.HasPrefix(name, "request_dump_") || strings.HasSuffix(name, ".tmp") {
			continue
		}

		path := filepath.Join(w.watchPath, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !shouldProcess(seen, path, info, now) {
			continue
		}

		if err := w.ProcessFile(ctx, path); err != nil {
			// Leave no stamp: the file is retried next scan.
			slog.Warn("failed to process file", "path", path, "error", err)
		} else {
			seen[path] = fileStamp{modTime: info.ModTime(), size: info.Size()}
		}
	}

	// Scan event files
	w.scanEventFiles(ctx, seen, now)
}

// scanEventFiles scans the events directory for new event files.
func (w *Watcher) scanEventFiles(ctx context.Context, seen map[string]fileStamp, now time.Time) {
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
			if eventFile.IsDir() || strings.HasSuffix(eventFile.Name(), ".tmp") {
				continue
			}

			path := filepath.Join(sessionDir, eventFile.Name())
			info, err := eventFile.Info()
			if err != nil {
				continue
			}
			if !shouldProcess(seen, path, info, now) {
				continue
			}

			if err := w.ProcessEventFile(ctx, path); err != nil {
				slog.Warn("failed to process event file", "path", path, "error", err)
			} else {
				seen[path] = fileStamp{modTime: info.ModTime(), size: info.Size()}
			}
		}
	}
}
