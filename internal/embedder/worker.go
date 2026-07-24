package embedder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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

	// maxAttempts caps retries for a queue item; the reaper/reconciler re-pend
	// rows, so without a cap a permanently bad item would retry forever.
	maxAttempts = 5

	// maintenanceInterval controls how often the reaper + reconciler run.
	maintenanceInterval = 1 * time.Minute
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
	maintenance := time.NewTicker(maintenanceInterval)
	defer maintenance.Stop()

	// Run maintenance once at startup so rows stranded by a previous
	// crash (stuck 'processing') and nodes that never got enqueued are
	// recovered immediately, not after the first interval.
	w.runMaintenance(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("embedding worker stopped")
			return
		case <-maintenance.C:
			w.runMaintenance(ctx)
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

// runMaintenance makes the queue self-healing:
//  1. Reaper: rows stuck in 'processing' (worker crash/shutdown mid-batch)
//     go back to 'pending'. Stores are idempotent upserts, so the worst
//     case of a false positive is duplicate work, never corruption.
//  2. Retry: 'failed' rows get re-pended after a backoff, until maxAttempts.
//  3. Reconciler: any current node without an embedding row is enqueued,
//     regardless of which code path created it. processed_at records the
//     last state transition (set on claim, done, and failure).
func (w *Worker) runMaintenance(ctx context.Context) {
	if tag, err := w.pool.Exec(ctx, `
		UPDATE embedding_queue
		SET status = 'pending'
		WHERE status = 'processing'
		  AND coalesce(processed_at, created_at) < now() - interval '10 minutes'
	`); err != nil {
		slog.Error("embedding reaper", "error", err)
	} else if n := tag.RowsAffected(); n > 0 {
		slog.Warn("embedding reaper re-pended stuck items", "count", n)
	}

	if tag, err := w.pool.Exec(ctx, `
		UPDATE embedding_queue
		SET status = 'pending'
		WHERE status = 'failed'
		  AND attempts < $1
		  AND coalesce(processed_at, created_at) < now() - interval '5 minutes'
	`, maxAttempts); err != nil {
		slog.Error("embedding failed-retry", "error", err)
	} else if n := tag.RowsAffected(); n > 0 {
		slog.Info("embedding retry re-pended failed items", "count", n)
	}

	if tag, err := w.pool.Exec(ctx, `
		INSERT INTO embedding_queue (source_type, source_id)
		SELECT 'node', n.id::text
		FROM nodes n
		WHERE n.valid_to IS NULL
		  AND NOT EXISTS (SELECT 1 FROM node_embeddings ne WHERE ne.node_id = n.id)
		ON CONFLICT (source_type, source_id) DO UPDATE
		SET status = 'pending', attempts = 0, last_error = NULL
		WHERE embedding_queue.status = 'done'
	`); err != nil {
		slog.Error("embedding reconciler", "error", err)
	} else if n := tag.RowsAffected(); n > 0 {
		slog.Info("embedding reconciler enqueued unembedded nodes", "count", n)
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	// Fetch pending items
	rows, err := w.pool.Query(ctx, `
		UPDATE embedding_queue
		SET status = 'processing', attempts = attempts + 1, processed_at = now()
		WHERE id IN (
			SELECT id FROM embedding_queue
			WHERE status = 'pending' AND attempts < $2
			ORDER BY created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, source_type, source_id
	`, w.batchSize, maxAttempts)
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

	var batch []itemWithContent
	for _, item := range items {
		content, err := w.fetchContent(ctx, item)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Node superseded/deleted (or message gone) before the worker
				// got to it — embedding it would resurrect a dead memory.
				w.markDone(ctx, item.ID, "source missing or superseded")
				continue
			}
			w.markFailed(ctx, item.ID, fmt.Sprintf("fetch content: %v", err))
			continue
		}
		batch = append(batch, itemWithContent{queueItem: item, content: truncateEmbedText(content)})
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
		for _, b := range batch {
			w.processItem(ctx, b.queueItem)
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

	// Step 3: Store embeddings item-by-item. Items are independent and the
	// upserts idempotent; a shared transaction would let one bad item abort
	// the tx and silently discard every other embedding in the batch.
	for i, b := range batch {
		if err := w.storeEmbedding(ctx, b.queueItem, b.content, embeddings[i]); err != nil {
			slog.Error("store embedding", "id", b.ID, "error", err)
			w.markFailed(ctx, b.ID, fmt.Sprintf("store: %v", err))
			continue
		}
		w.markDone(ctx, b.ID, "")
		if b.SourceType == "node" {
			w.autoLinkNode(ctx, b.SourceID)
		}
	}

	slog.Debug("batch embedded", "count", len(batch))
}

func (w *Worker) fetchContent(ctx context.Context, item queueItem) (string, error) {
	switch item.SourceType {
	case "node":
		var content string
		err := w.pool.QueryRow(ctx,
			`SELECT coalesce(label || ' ' || content || ' ' || summary, '')
			 FROM nodes WHERE id = $1 AND valid_to IS NULL`,
			item.SourceID,
		).Scan(&content)
		return content, err
	case "message":
		var content string
		err := w.pool.QueryRow(ctx,
			`SELECT content FROM messages WHERE id = $1`,
			item.SourceID,
		).Scan(&content)
		return content, err
	default:
		return "", fmt.Errorf("unknown source_type: %s", item.SourceType)
	}
}

func (w *Worker) storeEmbedding(ctx context.Context, item queueItem, content string, emb []float32) error {
	switch item.SourceType {
	case "node":
		_, err := w.pool.Exec(ctx, `
			INSERT INTO node_embeddings (node_id, content, embedding, sync_state)
			VALUES ($1, $2, $3::vector, 'synced')
			ON CONFLICT (node_id) DO UPDATE
			SET content = $2, embedding = $3::vector, sync_state = 'synced', created_at = now()
		`, item.SourceID, content, vectorToString(emb))
		return err
	case "message":
		_, err := w.pool.Exec(ctx, `
			INSERT INTO message_embeddings (message_id, content, embedding, sync_state)
			VALUES ($1, $2, $3::vector, 'synced')
			ON CONFLICT (message_id) DO UPDATE
			SET content = $2, embedding = $3::vector, sync_state = 'synced', created_at = now()
		`, item.SourceID, content, vectorToString(emb))
		return err
	default:
		return fmt.Errorf("unknown source_type: %s", item.SourceType)
	}
}

// autoLinkNode connects a freshly-embedded, under-connected node to its most
// similar same-workspace neighbours with relates_to edges. This grows a real
// semantic graph as memories are formed (instead of a pile of orphans), so
// graph-expansion recall and consolidation actually have edges to work with.
// Gated to nodes with < 3 active edges and a high similarity threshold so it
// densifies conservatively and never crosses workspaces. Best-effort.
func (w *Worker) autoLinkNode(ctx context.Context, nodeID string) {
	_, err := w.pool.Exec(ctx, `
		INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight)
		SELECT n.workspace_name, $1, cand.node_id, 'relates_to', cand.sim
		FROM node_embeddings src
		JOIN nodes n ON n.id = $1 AND n.valid_to IS NULL
		CROSS JOIN LATERAL (
			SELECT ne.node_id, (1 - (ne.embedding <=> src.embedding))::real AS sim
			FROM node_embeddings ne
			JOIN nodes n2 ON n2.id = ne.node_id AND n2.valid_to IS NULL AND n2.workspace_name = n.workspace_name
			WHERE ne.node_id <> $1
			  AND (1 - (ne.embedding <=> src.embedding)) > 0.75
			  AND NOT EXISTS (
				SELECT 1 FROM edges e WHERE e.valid_to IS NULL
				AND ((e.source_id = $1 AND e.target_id = ne.node_id)
				  OR (e.source_id = ne.node_id AND e.target_id = $1)))
			ORDER BY ne.embedding <=> src.embedding
			LIMIT 2
		) cand
		WHERE src.node_id = $1
		  AND (SELECT COUNT(*) FROM edges e WHERE (e.source_id = $1 OR e.target_id = $1) AND e.valid_to IS NULL) < 3
		ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
	`, nodeID)
	if err != nil {
		slog.Debug("auto-link node", "id", nodeID, "error", err)
	}
}

// truncateEmbedText bounds embed input without splitting a UTF-8 rune.
// nomic-embed-text handles 8192 tokens (~32KB); 8KB keeps latency low while
// covering far more of long memories than the previous 1500-byte cut.
func truncateEmbedText(content string) string {
	const maxEmbedLen = 8000
	if len(content) <= maxEmbedLen {
		return content
	}
	cut := maxEmbedLen
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut]
}

type queueItem struct {
	ID         int64
	SourceType string
	SourceID   string
}

func (w *Worker) processItem(ctx context.Context, item queueItem) {
	content, err := w.fetchContent(ctx, item)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.markDone(ctx, item.ID, "source missing or superseded")
			return
		}
		w.markFailed(ctx, item.ID, fmt.Sprintf("fetch content: %v", err))
		return
	}
	content = truncateEmbedText(content)

	embedding, err := w.client.Embed(ctx, content)
	if err != nil {
		w.markFailed(ctx, item.ID, fmt.Sprintf("embed: %v", err))
		return
	}

	if err := w.storeEmbedding(ctx, item, content, embedding); err != nil {
		w.markFailed(ctx, item.ID, fmt.Sprintf("store embedding: %v", err))
		return
	}
	w.markDone(ctx, item.ID, "")
	if item.SourceType == "node" {
		w.autoLinkNode(ctx, item.SourceID)
	}
}

func (w *Worker) markDone(ctx context.Context, id int64, note string) {
	var noteVal any
	if note != "" {
		noteVal = note
	}
	_, _ = w.pool.Exec(ctx, `
		UPDATE embedding_queue SET status = 'done', processed_at = now(), last_error = $2 WHERE id = $1
	`, id, noteVal)
}

func (w *Worker) markFailed(ctx context.Context, id int64, errMsg string) {
	slog.Warn("embedding failed", "id", id, "error", errMsg)
	_, _ = w.pool.Exec(ctx, `
		UPDATE embedding_queue SET status = 'failed', processed_at = now(), last_error = $2 WHERE id = $1
	`, id, errMsg)
}

// EnqueueNode adds a node to the embedding queue. Re-enqueueing a node whose
// row already exists (done or failed) resets it to pending so content changes
// always trigger a fresh embed instead of erroring or no-opping.
func EnqueueNode(ctx context.Context, pool *pgxpool.Pool, nodeID string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO embedding_queue (source_type, source_id)
		VALUES ('node', $1)
		ON CONFLICT (source_type, source_id) DO UPDATE
		SET status = 'pending', attempts = 0, last_error = NULL
	`, nodeID)
	return err
}

// EnqueueMessage adds a message to the embedding queue.
func EnqueueMessage(ctx context.Context, pool *pgxpool.Pool, messageID int64) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO embedding_queue (source_type, source_id)
		VALUES ('message', $1::text)
		ON CONFLICT (source_type, source_id) DO UPDATE
		SET status = 'pending', attempts = 0, last_error = NULL
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
