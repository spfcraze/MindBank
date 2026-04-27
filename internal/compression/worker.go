package compression

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker compresses raw text into structured facts using Ollama.
type Worker struct {
	pool     *pgxpool.Pool
	ollamaURL string
	model    string
	interval time.Duration
	httpClient *http.Client
}

// NewWorker creates a new compression worker.
func NewWorker(pool *pgxpool.Pool, ollamaURL, model string) *Worker {
	return &Worker{
		pool:       pool,
		ollamaURL:  ollamaURL,
		model:      model,
		interval:   30 * time.Second,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Start begins the compression worker loop.
func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, label, content, summary 
		FROM nodes 
		WHERE id IN (
			SELECT node_id FROM embedding_queue 
			WHERE compress = true AND status = 'pending'
			ORDER BY created_at 
			LIMIT 5
		)
	`)
	if err != nil {
		slog.Error("compression query", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, label, content, summary string
		if err := rows.Scan(&id, &label, &content, &summary); err != nil {
			continue
		}

		if err := w.compressNode(ctx, id, label, content, summary); err != nil {
			slog.Error("compress node", "id", id, "error", err)
			w.markFailed(ctx, id, err.Error())
		}
	}
}

func (w *Worker) compressNode(ctx context.Context, id, label, content, summary string) error {
	text := label + "\n" + summary + "\n" + content
	if len(text) > 8000 {
		text = text[:8000]
	}

	prompt := fmt.Sprintf(`Extract structured facts from this text. Return JSON:
{
  "facts": ["fact 1", "fact 2"],
  "concepts": ["concept 1"],
  "decisions": ["decision 1"],
  "problems": ["problem 1"]
}

Text:
%s`, text)

	resp, err := w.generate(ctx, prompt)
	if err != nil {
		return fmt.Errorf("ollama generate: %w", err)
	}

	// Parse JSON from response
	var result struct {
		Facts     []string `json:"facts"`
		Concepts  []string `json:"concepts"`
		Decisions []string `json:"decisions"`
		Problems  []string `json:"problems"`
	}

	// Extract JSON from response text
	jsonStr := extractJSON(resp)
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	// Create child nodes for each extracted item
	if err := w.createExtractedNodes(ctx, id, result); err != nil {
		return fmt.Errorf("create nodes: %w", err)
	}

	w.markCompleted(ctx, id)
	return nil
}

func (w *Worker) generate(ctx context.Context, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"model":  w.model,
		"prompt": prompt,
		"stream": "false",
	})

	req, err := http.NewRequestWithContext(ctx, "POST", w.ollamaURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	return result.Response, nil
}

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func (w *Worker) createExtractedNodes(ctx context.Context, parentID string, result struct {
	Facts     []string `json:"facts"`
	Concepts  []string `json:"concepts"`
	Decisions []string `json:"decisions"`
	Problems  []string `json:"problems"`
}) error {
	// TODO: Implement node creation via repository
	return nil
}

func (w *Worker) markFailed(ctx context.Context, nodeID, errMsg string) {
	_, _ = w.pool.Exec(ctx,
		"UPDATE embedding_queue SET status = 'failed', error = $1 WHERE node_id = $2",
		errMsg, nodeID)
}

func (w *Worker) markCompleted(ctx context.Context, nodeID string) {
	_, _ = w.pool.Exec(ctx,
		"UPDATE embedding_queue SET status = 'completed', completed_at = now() WHERE node_id = $1",
		nodeID)
}
