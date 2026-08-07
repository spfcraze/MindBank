// Package workspace records the global-workspace (JSPACE) event stream:
// every transition a memory makes as it enters, is leased, is consumed, or
// expires from the shared workspace. Best-effort writes — capture must never
// break the write path it is observing.
package workspace

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Record appends a workspace transition event (fire-and-forget).
func Record(ctx context.Context, pool *pgxpool.Pool, eventType, nodeID, label, namespace string, meta map[string]any) {
	if pool == nil {
		return
	}
	m, _ := json.Marshal(meta)
	if m == nil {
		m = []byte("{}")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_events (event_type, node_id, label, namespace, meta)
		VALUES ($1, $2, $3, $4, $5::jsonb)`, eventType, nodeID, label, namespace, string(m)); err != nil {
		slog.Debug("workspace event record failed", "type", eventType, "error", err)
	}
}
