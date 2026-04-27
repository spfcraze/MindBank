package capture

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service watches for Hermes session files and auto-mines them into MindBank.
type Service struct {
	pool      *pgxpool.Pool
	watcher   *fsnotify.Watcher
	watchPath string
	apiURL    string
}

// Session represents a mined Hermes session.
type Session struct {
	Title    string
	Date     time.Time
	Messages []Message
}

// Message represents a single message in a session.
type Message struct {
	Role    string
	Content string
	Time    time.Time
}

// NewService creates a new auto-capture service.
func NewService(pool *pgxpool.Pool, watchPath, apiURL string) *Service {
	return &Service{
		pool:      pool,
		watchPath: watchPath,
		apiURL:    apiURL,
	}
}

// Start begins watching for new session files.
func (s *Service) Start(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	s.watcher = watcher

	// Watch directory
	if err := watcher.Add(s.watchPath); err != nil {
		return fmt.Errorf("watch path: %w", err)
	}

	slog.Info("auto-capture started", "path", s.watchPath)

	go s.loop(ctx)
	return nil
}

func (s *Service) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create == fsnotify.Create {
				if strings.HasSuffix(event.Name, ".md") {
					go s.processFile(ctx, event.Name)
				}
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("watcher error", "error", err)
		}
	}
}

func (s *Service) processFile(ctx context.Context, path string) {
	// Compute file hash
	hash, err := s.fileHash(path)
	if err != nil {
		slog.Error("hash file", "path", path, "error", err)
		return
	}

	// Check if already captured
	var existingID string
	err = s.pool.QueryRow(ctx,
		"SELECT node_id FROM captured_sessions WHERE file_hash = $1", hash).Scan(&existingID)
	if err == nil && existingID != "" {
		slog.Debug("already captured", "path", path)
		return
	}

	// Parse session
	session, err := s.parseSession(path)
	if err != nil {
		slog.Error("parse session", "path", path, "error", err)
		s.recordCapture(ctx, path, hash, "", "failed", err.Error())
		return
	}

	// Create session node via API
	nodeID, err := s.createSessionNode(ctx, session)
	if err != nil {
		slog.Error("create node", "path", path, "error", err)
		s.recordCapture(ctx, path, hash, "", "failed", err.Error())
		return
	}

	s.recordCapture(ctx, path, hash, nodeID, "completed", "")
	slog.Info("session captured", "path", path, "node_id", nodeID)
}

func (s *Service) fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) recordCapture(ctx context.Context, path, hash, nodeID, status, errMsg string) {
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO captured_sessions (file_path, file_hash, node_id, status, error)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (file_path) DO UPDATE SET
			file_hash = EXCLUDED.file_hash,
			node_id = EXCLUDED.node_id,
			status = EXCLUDED.status,
			error = EXCLUDED.error,
			completed_at = CASE WHEN EXCLUDED.status = 'completed' THEN now() ELSE NULL END
	`, path, hash, nodeID, status, errMsg)
}

// parseSession parses a markdown session file into a Session struct.
func (s *Service) parseSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	session := &Session{
		Date: time.Now(),
	}

	scanner := bufio.NewScanner(f)
	var currentMsg *Message

	for scanner.Scan() {
		line := scanner.Text()

		// Parse headers
		if strings.HasPrefix(line, "# ") {
			session.Title = strings.TrimPrefix(line, "# ")
			continue
		}

		// Parse message blocks
		if strings.HasPrefix(line, "**User**") || strings.HasPrefix(line, "**Assistant**") {
			if currentMsg != nil {
				session.Messages = append(session.Messages, *currentMsg)
			}
			role := "user"
			if strings.HasPrefix(line, "**Assistant**") {
				role = "assistant"
			}
			currentMsg = &Message{Role: role}
			continue
		}

		if currentMsg != nil {
			currentMsg.Content += line + "\n"
		}
	}

	if currentMsg != nil {
		session.Messages = append(session.Messages, *currentMsg)
	}

	return session, scanner.Err()
}

// createSessionNode creates a session node via the API.
func (s *Service) createSessionNode(ctx context.Context, session *Session) (string, error) {
	// For now, return a placeholder - this will be implemented to call the actual API
	// The actual implementation would POST to /api/v1/nodes with session data
	return "", nil
}
