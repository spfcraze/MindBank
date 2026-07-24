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
	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/repository"
)

// Worker compresses raw text into structured facts using Ollama.
type Worker struct {
	pool         *pgxpool.Pool
	settingsRepo *repository.SettingsRepo
	nodeRepo     *repository.NodeRepo
	ollamaURL    string
	model        string
	interval     time.Duration
	httpClient   *http.Client
}

// NewWorker creates a new compression worker.
func NewWorker(pool *pgxpool.Pool, settingsRepo *repository.SettingsRepo, ollamaURL, model string) *Worker {
	return &Worker{
		pool:         pool,
		settingsRepo: settingsRepo,
		nodeRepo:     repository.NewNodeRepo(pool),
		ollamaURL:    ollamaURL,
		model:        model,
		interval:     30 * time.Second,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
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
	// Check if compression is enabled
	if w.settingsRepo == nil {
		slog.Debug("compression: no settings repo, skipping")
		return
	}

	enabled := w.settingsRepo.GetBool(ctx, "compression_enabled")
	if !enabled {
		slog.Debug("compression: disabled, skipping")
		return
	}

	setupComplete := w.settingsRepo.GetBool(ctx, "compression_setup_complete")
	if !setupComplete {
		slog.Debug("compression: setup not complete, skipping")
		return
	}

	// Use configured model
	model, _ := w.settingsRepo.Get(ctx, "compression_model")
	if model == "" {
		model = "phi4-mini"
	}

	// Check model availability
	if !w.modelAvailable(model) {
		// Try fallback
		fallback := "llama3.2"
		if !w.modelAvailable(fallback) {
			slog.Error("compression: neither primary nor fallback model available",
				"primary", model,
				"fallback", fallback)
			return
		}
		model = fallback
	}

	// Self-feed: enqueue long raw-capture nodes that haven't been queued yet.
	// Nothing else produces compression work, so without this the pipeline
	// is permanently idle even when enabled.
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO compression_queue (node_id)
		SELECT n.id FROM nodes n
		WHERE n.valid_to IS NULL
		  AND n.node_type IN ('session', 'event')
		  AND length(n.content) >= 2000
		  AND NOT EXISTS (SELECT 1 FROM compression_queue cq WHERE cq.node_id = n.id)
		ORDER BY n.created_at
		LIMIT 50
	`); err != nil {
		slog.Error("compression enqueue candidates", "error", err)
		return
	}

	// Retire pending rows whose node was superseded/soft-deleted: the claim
	// join below would never pick them, so left pending they'd permanently
	// occupy the claim window and starve real work.
	if _, err := w.pool.Exec(ctx, `
		UPDATE compression_queue cq
		SET status = 'done', last_error = 'node superseded', processed_at = now()
		WHERE cq.status = 'pending'
		  AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = cq.node_id AND n.valid_to IS NULL)
	`); err != nil {
		slog.Error("compression retire stale", "error", err)
	}

	// Claim a batch (skip-locked so multiple instances can't double-claim),
	// joining nodes so superseded/deleted nodes are never compressed.
	rows, err := w.pool.Query(ctx, `
		UPDATE compression_queue cq
		SET status = 'processing', attempts = attempts + 1, processed_at = now()
		FROM nodes n
		WHERE cq.id IN (
			SELECT id FROM compression_queue
			WHERE status = 'pending' AND attempts < 3
			ORDER BY created_at
			LIMIT 5
			FOR UPDATE SKIP LOCKED
		)
		  AND n.id = cq.node_id AND n.valid_to IS NULL
		RETURNING n.id, n.label, n.content, coalesce(n.summary, ''), n.workspace_name, n.namespace
	`)
	if err != nil {
		slog.Error("compression query", "error", err)
		return
	}
	defer rows.Close()

	type job struct {
		id, label, content, summary, workspace, namespace string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.label, &j.content, &j.summary, &j.workspace, &j.namespace); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		slog.Error("compression rows", "error", err)
		return
	}
	rows.Close()

	for _, j := range jobs {
		if err := w.compressNode(ctx, j.id, j.label, j.content, j.summary, j.workspace, j.namespace, model); err != nil {
			slog.Error("compress node", "id", j.id, "error", err)
			w.markFailed(ctx, j.id, err.Error())
		}
	}
}

func (w *Worker) modelAvailable(model string) bool {
	resp, err := w.httpClient.Get(w.ollamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	for _, m := range result.Models {
		if strings.Contains(m.Name, model) {
			return true
		}
	}
	return false
}

func (w *Worker) compressNode(ctx context.Context, id, label, content, summary, workspace, namespace, model string) error {
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

	resp, err := w.generate(ctx, prompt, model)
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
	if err := w.createExtractedNodes(ctx, id, workspace, namespace, result); err != nil {
		return fmt.Errorf("create nodes: %w", err)
	}

	w.markCompleted(ctx, id)
	return nil
}

func (w *Worker) generate(ctx context.Context, prompt, model string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"model":  model,
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

func (w *Worker) createExtractedNodes(ctx context.Context, parentID, workspace, namespace string, result struct {
	Facts     []string `json:"facts"`
	Concepts  []string `json:"concepts"`
	Decisions []string `json:"decisions"`
	Problems  []string `json:"problems"`
}) error {
	// Create nodes for each extracted fact
	for _, fact := range result.Facts {
		if err := w.createNode(ctx, parentID, workspace, namespace, fact, "fact"); err != nil {
			slog.Error("compression: create fact node", "error", err)
		}
	}
	for _, concept := range result.Concepts {
		if err := w.createNode(ctx, parentID, workspace, namespace, concept, "concept"); err != nil {
			slog.Error("compression: create concept node", "error", err)
		}
	}
	for _, decision := range result.Decisions {
		if err := w.createNode(ctx, parentID, workspace, namespace, decision, "decision"); err != nil {
			slog.Error("compression: create decision node", "error", err)
		}
	}
	for _, problem := range result.Problems {
		if err := w.createNode(ctx, parentID, workspace, namespace, problem, "problem"); err != nil {
			slog.Error("compression: create problem node", "error", err)
		}
	}
	return nil
}

// createNode stores one extracted item via NodeRepo.Create so it gets hash
// dedup (retries don't spawn duplicates) and cache invalidation, inherits the
// parent's workspace/namespace (the old hardcoded 'default' workspace wasn't
// even seeded, so every insert failed its FK), links it to the source node,
// and enqueues embedding so the memory is reachable by semantic recall.
func (w *Worker) createNode(ctx context.Context, parentID, workspace, namespace, content, nodeType string) error {
	label := content
	if len(label) > 80 {
		label = label[:80] + "..."
	}
	node, err := w.nodeRepo.Create(ctx, models.NodeCreate{
		WorkspaceName: workspace,
		Namespace:     namespace,
		Label:         label,
		NodeType:      models.NodeType(nodeType),
		Content:       content,
		Summary:       content,
	})
	if err != nil {
		return fmt.Errorf("insert node: %w", err)
	}
	if !node.Deduplicated {
		_, _ = w.pool.Exec(ctx, `
			INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight)
			VALUES ($1, $2, $3, 'produced', 0.8)
			ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
		`, workspace, parentID, node.ID)
		if err := embedder.EnqueueNode(ctx, w.pool, node.ID); err != nil {
			slog.Warn("compression: enqueue embedding", "node_id", node.ID, "error", err)
		}
	}
	return nil
}

func (w *Worker) markFailed(ctx context.Context, nodeID, errMsg string) {
	_, _ = w.pool.Exec(ctx,
		"UPDATE compression_queue SET status = 'failed', last_error = $1, processed_at = now() WHERE node_id = $2",
		errMsg, nodeID)
}

func (w *Worker) markCompleted(ctx context.Context, nodeID string) {
	_, _ = w.pool.Exec(ctx,
		"UPDATE compression_queue SET status = 'done', processed_at = now() WHERE node_id = $1",
		nodeID)
}
