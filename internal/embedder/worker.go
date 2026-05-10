package embedder

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker processes the embedding queue in the background.
type Worker struct {
	pool      *pgxpool.Pool
	client    *Client
	batchSize int
	interval  time.Duration

	// Circuit breaker — protected by mu
	mu                  sync.Mutex
	consecutiveFailures int
	circuitOpenUntil    time.Time
}

const (
	maxConsecutiveFailures = 5
	circuitBreakDuration   = 30 * time.Second
)

// NewWorker creates an embedding queue worker.
func NewWorker(pool *pgxpool.Pool, client *Client) *Worker {
	return &Worker{
		pool:      pool,
		client:    client,
		batchSize: 10,
		interval:  2 * time.Second,
	}
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("embedding worker started")
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("embedding worker stopped")
			return
		case <-ticker.C:
			// Circuit breaker: skip if open
			w.mu.Lock()
			open := time.Now().Before(w.circuitOpenUntil)
			failures := w.consecutiveFailures
			retryAt := w.circuitOpenUntil
			w.mu.Unlock()

			if open {
				slog.Debug("embedding circuit open, skipping",
					"failures", failures,
					"retry_at", retryAt.Format(time.RFC3339))
				continue
			}
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	// Fetch pending items
	rows, err := w.pool.Query(ctx, `
		UPDATE embedding_queue
		SET status = 'processing', attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM embedding_queue
			WHERE status = 'pending' AND attempts < 3
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, source_type, source_id
	`, w.batchSize)
	if err != nil {
		slog.Error("fetch queue items", "error", err)
		return
	}
	defer rows.Close()

	var items []queueItem
	for rows.Next() {
		var item queueItem
		if err := rows.Scan(&item.ID, &item.SourceType, &item.SourceID); err != nil {
			slog.Error("scan queue item", "error", err)
			continue
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		return
	}

	// Batch: fetch content, embed in parallel, store in transaction
	w.processBatchItems(ctx, items)
}

func (w *Worker) processBatchItems(ctx context.Context, items []queueItem) {
	// Step 1: Fetch content for all items (truncate to 1500 chars for Ollama safety)
	type itemWithContent struct {
		queueItem
		content string
	}

	const maxEmbedLen = 1500 // nomic-embed-text context = 8192 tokens, ~4 chars/token → ~32K, but keep short for speed

	var batch []itemWithContent
	for _, item := range items {
		var content string
		var err error
		switch item.SourceType {
		case "node":
			err = w.pool.QueryRow(ctx,
				`SELECT coalesce(label || ' ' || content || ' ' || summary, '') FROM nodes WHERE id = $1`,
				item.SourceID,
			).Scan(&content)
		case "message":
			err = w.pool.QueryRow(ctx,
				`SELECT content FROM messages WHERE id = $1`,
				item.SourceID,
			).Scan(&content)
		default:
			w.markFailed(ctx, item.ID, fmt.Sprintf("unknown source_type: %s", item.SourceType))
			continue
		}
		if err != nil {
			w.markFailed(ctx, item.ID, fmt.Sprintf("fetch content: %v", err))
			continue
		}
		// Truncate to safe length for Ollama
		if len(content) > maxEmbedLen {
			content = content[:maxEmbedLen]
		}
		batch = append(batch, itemWithContent{queueItem: item, content: content})
	}

	if len(batch) == 0 {
		return
	}

	// Step 2: Batch embed in parallel
	texts := make([]string, len(batch))
	for i, b := range batch {
		texts[i] = b.content
	}

	embeddings, err := w.client.EmbedBatch(ctx, texts)
	if err != nil {
		// Track consecutive failures for circuit breaker
		w.mu.Lock()
		w.consecutiveFailures++
		failures := w.consecutiveFailures
		if w.consecutiveFailures >= maxConsecutiveFailures {
			w.circuitOpenUntil = time.Now().Add(circuitBreakDuration)
			slog.Warn("embedding circuit opened", "failures", failures, "retry_after", circuitBreakDuration)
		}
		w.mu.Unlock()
		slog.Warn("batch embed failed, falling back to sequential", "error", err, "consecutive_failures", failures)
		for _, item := range items {
			w.processItem(ctx, item)
		}
		return
	}

	// Success: reset circuit breaker
	w.mu.Lock()
	wasFailures := w.consecutiveFailures
	w.consecutiveFailures = 0
	w.circuitOpenUntil = time.Time{}
	w.mu.Unlock()
	if wasFailures > 0 {
		slog.Info("embedding circuit closed after success", "was_failures", wasFailures)
	}

	// Step 3: Store all embeddings in a single transaction
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		slog.Error("begin transaction", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	for i, b := range batch {
		emb := embeddings[i]
		switch b.SourceType {
		case "node":
			_, err = tx.Exec(ctx, `
				INSERT INTO node_embeddings (node_id, content, embedding, sync_state)
				VALUES ($1, $2, $3::vector, 'synced')
				ON CONFLICT (node_id) DO UPDATE
				SET content = $2, embedding = $3::vector, sync_state = 'synced', created_at = now()
			`, b.SourceID, b.content, vectorToString(emb))
		case "message":
			_, err = tx.Exec(ctx, `
				INSERT INTO message_embeddings (message_id, content, embedding, sync_state)
				VALUES ($1, $2, $3::vector, 'synced')
				ON CONFLICT (message_id) DO UPDATE
				SET content = $2, embedding = $3::vector, sync_state = 'synced', created_at = now()
			`, b.SourceID, b.content, vectorToString(emb))
		}
		if err != nil {
			slog.Error("store embedding", "id", b.ID, "error", err)
			w.markFailed(ctx, b.ID, fmt.Sprintf("store: %v", err))
			continue
		}
		// Mark done
		_, _ = tx.Exec(ctx, `
			UPDATE embedding_queue SET status = 'done', processed_at = now() WHERE id = $1
		`, b.ID)
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("commit batch", "error", err)
		return
	}

	slog.Debug("batch embedded", "count", len(batch))
}

type queueItem struct {
	ID         int64
	SourceType string
	SourceID   string
}

func (w *Worker) processItem(ctx context.Context, item queueItem) {
	var content string
	var err error

	// Get content based on source type
	switch item.SourceType {
	case "node":
		err = w.pool.QueryRow(ctx,
			`SELECT coalesce(label || ' ' || content || ' ' || summary, '') FROM nodes WHERE id = $1`,
			item.SourceID,
		).Scan(&content)
	case "message":
		err = w.pool.QueryRow(ctx,
			`SELECT content FROM messages WHERE id = $1`,
			item.SourceID,
		).Scan(&content)
	default:
		w.markFailed(ctx, item.ID, fmt.Sprintf("unknown source_type: %s", item.SourceType))
		return
	}
	if err != nil {
		w.markFailed(ctx, item.ID, fmt.Sprintf("fetch content: %v", err))
		return
	}

	// Truncate to safe length for Ollama
	if len(content) > 1500 {
		content = content[:1500]
	}

	// Generate embedding
	embedding, err := w.client.Embed(ctx, content)
	if err != nil {
		w.markFailed(ctx, item.ID, fmt.Sprintf("embed: %v", err))
		return
	}

	// Store embedding
	switch item.SourceType {
	case "node":
		_, err = w.pool.Exec(ctx, `
			INSERT INTO node_embeddings (node_id, content, embedding, sync_state)
			VALUES ($1, $2, $3::vector, 'synced')
			ON CONFLICT (node_id) DO UPDATE
			SET content = $2, embedding = $3::vector, sync_state = 'synced', created_at = now()
		`, item.SourceID, content, vectorToString(embedding))
	case "message":
		_, err = w.pool.Exec(ctx, `
			INSERT INTO message_embeddings (message_id, content, embedding, sync_state)
			VALUES ($1, $2, $3::vector, 'synced')
			ON CONFLICT (message_id) DO UPDATE
			SET content = $2, embedding = $3::vector, sync_state = 'synced', created_at = now()
		`, item.SourceID, content, vectorToString(embedding))
	}
	if err != nil {
		w.markFailed(ctx, item.ID, fmt.Sprintf("store embedding: %v", err))
		return
	}

	// Mark done
	_, _ = w.pool.Exec(ctx, `
		UPDATE embedding_queue SET status = 'done', processed_at = now() WHERE id = $1
	`, item.ID)
}

func (w *Worker) markFailed(ctx context.Context, id int64, errMsg string) {
	slog.Warn("embedding failed", "id", id, "error", errMsg)
	_, _ = w.pool.Exec(ctx, `
		UPDATE embedding_queue SET status = 'failed', last_error = $2 WHERE id = $1
	`, id, errMsg)
}

// EnqueueNode adds a node to the embedding queue.
func EnqueueNode(ctx context.Context, pool *pgxpool.Pool, nodeID string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO embedding_queue (source_type, source_id)
		VALUES ('node', $1)
		ON CONFLICT DO NOTHING
	`, nodeID)
	return err
}

// EnqueueMessage adds a message to the embedding queue.
func EnqueueMessage(ctx context.Context, pool *pgxpool.Pool, messageID int64) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO embedding_queue (source_type, source_id)
		VALUES ('message', $1::text)
	`, fmt.Sprintf("%d", messageID))
	return err
}

// vectorToString formats a float32 slice as a pgvector literal: "[0.1,0.2,...]"
func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, f := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%g", f)
	}
	s += "]"
	return s
}
