package repository

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mindbank/internal/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRepo struct {
	pool *pgxpool.Pool
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

func (r *SessionRepo) Pool() *pgxpool.Pool {
	return r.pool
}

// Create creates a new session.
func (r *SessionRepo) Create(ctx context.Context, req models.SessionCreate) (*models.Session, error) {
	ws := req.WorkspaceName
	if ws == "" {
		ws = "hermes"
	}

	s := &models.Session{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sessions (workspace_name, name, metadata)
		VALUES ($1, $2, coalesce($3, '{}'::jsonb))
		RETURNING id, workspace_name, name, is_active, metadata, summary, created_at, updated_at
	`, ws, req.Name, req.Metadata,
	).Scan(&s.ID, &s.WorkspaceName, &s.Name, &s.IsActive, &s.Metadata, &s.Summary, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return s, nil
}

// Get retrieves a session by ID.
func (r *SessionRepo) Get(ctx context.Context, id string) (*models.Session, error) {
	s := &models.Session{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_name, name, is_active, metadata, summary, created_at, updated_at
		FROM sessions WHERE id = $1
	`, id).Scan(&s.ID, &s.WorkspaceName, &s.Name, &s.IsActive, &s.Metadata, &s.Summary, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

// AddMessages inserts messages into a session and returns the created messages.
func (r *SessionRepo) AddMessages(ctx context.Context, sessionID string, msgs []models.MessageCreate) ([]models.Message, error) {
	if len(msgs) == 0 {
		return nil, nil
	}

	// Get current max seq
	var maxSeq int
	_ = r.pool.QueryRow(ctx, `SELECT coalesce(max(seq_in_session), 0) FROM messages WHERE session_id = $1`, sessionID).Scan(&maxSeq)

	var created []models.Message
	for i, msg := range msgs {
		seq := maxSeq + i + 1
		m := models.Message{}
		err := r.pool.QueryRow(ctx, `
			INSERT INTO messages (session_id, role, content, seq_in_session, metadata)
			VALUES ($1, $2, $3, $4, coalesce($5, '{}'::jsonb))
			RETURNING id, session_id, role, content, seq_in_session, token_count, metadata, created_at
		`, sessionID, msg.Role, msg.Content, seq, msg.Metadata,
		).Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.SeqInSession, &m.TokenCount, &m.Metadata, &m.CreatedAt)
		if err != nil {
			return created, fmt.Errorf("insert message %d: %w", i, err)
		}
		created = append(created, m)
	}

	return created, nil
}

// GetMessages retrieves messages from a session with token limit.
func (r *SessionRepo) GetMessages(ctx context.Context, sessionID string, maxTokens int) ([]models.Message, error) {
	if maxTokens <= 0 {
		maxTokens = 10000
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, role, content, seq_in_session, token_count, metadata, created_at
		FROM messages
		WHERE session_id = $1
		ORDER BY seq_in_session DESC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var messages []models.Message
	tokenBudget := maxTokens
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.SeqInSession, &m.TokenCount, &m.Metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		// Rough token estimate: ~4 chars per token
		tokens := len(m.Content) / 4
		if tokens > m.TokenCount {
			m.TokenCount = tokens
		}
		tokenBudget -= m.TokenCount
		if tokenBudget < 0 {
			break
		}
		messages = append(messages, m)
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// LinkNode associates a node with a session.
func (r *SessionRepo) LinkNode(ctx context.Context, sessionID, nodeID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO session_nodes (session_id, node_id)
		VALUES ($1, $2)
		ON CONFLICT (session_id, node_id) DO UPDATE
		SET mention_count = session_nodes.mention_count + 1,
		    last_mentioned = now()
	`, sessionID, nodeID)
	return err
}

// GetSessionNodes returns nodes referenced in a session.
func (r *SessionRepo) GetSessionNodes(ctx context.Context, sessionID string) ([]models.Node, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT n.id, n.workspace_name, n.namespace, n.label, n.node_type, n.content, n.summary,
		       n.metadata, n.importance, n.access_count, n.last_accessed, n.valid_from, n.valid_to,
		       n.version, n.predecessor_id, n.created_at, n.updated_at
		FROM session_nodes sn
		JOIN nodes n ON n.id = sn.node_id
		WHERE sn.session_id = $1 AND n.valid_to IS NULL
		ORDER BY sn.mention_count DESC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session nodes: %w", err)
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		if err := rows.Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
			&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
			&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
			&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session node: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// List returns sessions with optional workspace filter.
func (r *SessionRepo) List(ctx context.Context, workspace string, activeOnly bool, limit, offset int) ([]models.Session, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT id, workspace_name, name, is_active, metadata, summary, created_at, updated_at
		FROM sessions WHERE 1=1
	`
	args := []any{}
	argN := 1

	if workspace != "" {
		query += fmt.Sprintf(" AND workspace_name = $%d", argN)
		args = append(args, workspace)
		argN++
	}
	if activeOnly {
		query += " AND is_active = true"
	}

	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.WorkspaceName, &s.Name, &s.IsActive, &s.Metadata, &s.Summary, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// Close marks a session as inactive.
func (r *SessionRepo) Close(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE sessions SET is_active = false, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	return nil
}

// ListRecent returns sessions created within the given duration.
func (r *SessionRepo) ListRecent(ctx context.Context, workspace, namespace string, within time.Duration) ([]models.Session, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_name, name, is_active, metadata, summary, created_at, updated_at
		FROM sessions
		WHERE workspace_name = $1
		  AND created_at > now() - $2::interval
		ORDER BY created_at DESC
	`, workspace, within)
	if err != nil {
		return nil, fmt.Errorf("list recent sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.WorkspaceName, &s.Name, &s.IsActive, &s.Metadata, &s.Summary, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// GetContext returns a token-limited context for a session (messages + associated nodes).
func (r *SessionRepo) GetContext(ctx context.Context, sessionID string, maxTokens int) (string, int, error) {
	messages, err := r.GetMessages(ctx, sessionID, maxTokens)
	if err != nil {
		return "", 0, err
	}

	nodes, err := r.GetSessionNodes(ctx, sessionID)
	if err != nil {
		slog.Warn("failed to get session nodes", "error", err)
		nodes = nil
	}

	context := ""
	tokens := 0

	if len(nodes) > 0 {
		context += "## Known Entities\n\n"
		for _, n := range nodes {
			line := fmt.Sprintf("- [%s] %s: %s\n", n.NodeType, n.Label, n.Summary)
			if n.Summary == "" && n.Content != "" {
				line = fmt.Sprintf("- [%s] %s: %s\n", n.NodeType, n.Label, truncate(n.Content, 100))
			}
			context += line
			tokens += len(line) / 4
		}
		context += "\n"
	}

	if len(messages) > 0 {
		context += "## Conversation\n\n"
		for _, m := range messages {
			line := fmt.Sprintf("%s: %s\n", m.Role, m.Content)
			context += line
			tokens += len(line) / 4
		}
	}

	return context, tokens, nil
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
