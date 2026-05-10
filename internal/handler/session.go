package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/reasoner"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
)

type SessionHandler struct {
	repo      *repository.SessionRepo
	nodeRepo  *repository.NodeRepo
	edgeRepo  *repository.EdgeRepo
	ruleBased *reasoner.RuleBased
}

func NewSessionHandler(repo *repository.SessionRepo, nodeRepo *repository.NodeRepo, edgeRepo *repository.EdgeRepo, rb *reasoner.RuleBased) *SessionHandler {
	return &SessionHandler{repo: repo, nodeRepo: nodeRepo, edgeRepo: edgeRepo, ruleBased: rb}
}

func (h *SessionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.SessionCreate
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}

	session, err := h.repo.Create(r.Context(), req)
	if err != nil {
		slog.Error("create session", "error", err)
		respondError(w, 500, "failed to create session")
		return
	}
	respondJSON(w, 201, session)
}

func (h *SessionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, err := h.repo.Get(r.Context(), id)
	if err != nil {
		respondError(w, 404, "session not found")
		return
	}
	respondJSON(w, 200, session)
}

func (h *SessionHandler) List(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Query().Get("workspace")
	activeStr := r.URL.Query().Get("active")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	sessions, err := h.repo.List(r.Context(), workspace, activeStr == "true", limit, offset)
	if err != nil {
		slog.Error("list sessions", "error", err)
		respondError(w, 500, "failed to list sessions")
		return
	}
	// Fallback: if legacy sessions table is empty, return session-type graph nodes
	if len(sessions) == 0 {
		graphSessions, err := h.listGraphSessions(r.Context(), workspace, limit, offset)
		if err != nil {
			slog.Error("list graph sessions", "error", err)
			respondError(w, 500, "failed to list sessions")
			return
		}
		sessions = graphSessions
	}
	if sessions == nil {
		sessions = []models.Session{}
	}
	respondJSON(w, 200, sessions)
}

// listGraphSessions queries nodes with node_type='session' and transforms them to models.Session.
func (h *SessionHandler) listGraphSessions(ctx context.Context, workspace string, limit, offset int) ([]models.Session, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := h.nodeRepo.Pool.Query(ctx, `
		SELECT id, label, namespace, content, summary, metadata, created_at
		FROM nodes
		WHERE valid_to IS NULL AND node_type = 'session' AND workspace_name = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, workspace, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var id, label, ns, content, summary string
		var metaJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &label, &ns, &content, &summary, &metaJSON, &createdAt); err != nil {
			slog.Warn("scan graph session", "error", err)
			continue
		}
		sessions = append(sessions, models.Session{
			ID:            id,
			WorkspaceName: ns,
			Name:          label,
			IsActive:      true,
			Metadata:      metaJSON,
			Summary:       summary,
			CreatedAt:     createdAt,
			UpdatedAt:     createdAt,
		})
	}
	return sessions, nil
}

func (h *SessionHandler) AddMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

	// Validate session exists before inserting messages
	_, err := h.repo.Get(r.Context(), sessionID)
	if err != nil {
		respondError(w, 404, "session not found")
		return
	}

	var msgs []models.MessageCreate
	if err := bindJSON(r, &msgs); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}

	created, err := h.repo.AddMessages(r.Context(), sessionID, msgs)
	if err != nil {
		slog.Error("add messages", "error", err)
		respondError(w, 500, "failed to add messages")
		return
	}

	// Run rule-based extraction on each message (async, best-effort)
	// Use background context — request context will be cancelled after response
	bgCtx := context.Background()
	go func() {
		// Get session info once (not per message)
		sess, _ := h.repo.Get(bgCtx, sessionID)
		if sess == nil {
			return
		}
		ws := sess.WorkspaceName
		ns := "global"
		if nsVal := sess.Metadata; len(nsVal) > 0 {
			var meta map[string]any
			if json.Unmarshal(nsVal, &meta) == nil {
				if v, ok := meta["namespace"].(string); ok {
					ns = v
				}
			}
		}
		for _, msg := range created {
			if msg.Role == "user" || msg.Role == "assistant" {
				if err := h.ruleBased.ProcessAndStore(bgCtx, sessionID, ws, ns, msg.Content); err != nil {
					slog.Error("process and store message", "session_id", sessionID, "error", err)
				}
			}
		}
	}()

	// Enqueue messages for embedding (use background ctx — request may be cancelled)
	for _, m := range created {
		if err := embedder.EnqueueMessage(bgCtx, h.ruleBased.Pool(), m.ID); err != nil {
			slog.Error("enqueue message for embedding", "message_id", m.ID, "error", err)
		}
	}

	if created == nil {
		created = []models.Message{}
	}
	respondJSON(w, 201, created)
}

func (h *SessionHandler) GetContext(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	maxTokens, _ := strconv.Atoi(r.URL.Query().Get("max_tokens"))
	if maxTokens <= 0 {
		maxTokens = 4000
	}

	context, tokens, err := h.repo.GetContext(r.Context(), sessionID, maxTokens)
	if err != nil {
		slog.Error("get context", "error", err)
		respondError(w, 500, "failed to get context")
		return
	}

	respondJSON(w, 200, map[string]any{
		"context":    context,
		"token_count": tokens,
	})
}

func (h *SessionHandler) Close(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.Close(r.Context(), id); err != nil {
		slog.Error("close session", "error", err)
		respondError(w, 500, "failed to close session")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "closed"})
}

func (h *SessionHandler) Sync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Count nodes before sync
	beforeCount, _ := h.nodeRepo.CountNodes(ctx)

	// Get all active sessions with messages that have few or no linked nodes
	rows, err := h.repo.Pool().Query(ctx, `
		SELECT DISTINCT s.id, s.workspace_name, s.metadata
		FROM sessions s
		JOIN messages m ON m.session_id = s.id
		LEFT JOIN session_nodes sn ON sn.session_id = s.id
		WHERE s.is_active = true
		GROUP BY s.id
		HAVING COUNT(sn.node_id) < COUNT(m.id) / 5 + 1
		LIMIT 100
	`)
	if err != nil {
		slog.Error("sync sessions query", "error", err)
		respondError(w, 500, "failed to query sessions")
		return
	}
	defer rows.Close()

	var processed int
	for rows.Next() {
		var sessionID, workspace string
		var metadata []byte
		if err := rows.Scan(&sessionID, &workspace, &metadata); err != nil {
			continue
		}

		// Determine namespace from metadata
		ns := "global"
		if len(metadata) > 0 {
			var meta map[string]any
			if json.Unmarshal(metadata, &meta) == nil {
				if v, ok := meta["namespace"].(string); ok && v != "" {
					ns = v
				}
			}
		}

		// Get messages for this session
		msgs, err := h.repo.GetMessages(ctx, sessionID, 10000)
		if err != nil {
			continue
		}

		for _, msg := range msgs {
			if msg.Role == "user" || msg.Role == "assistant" {
				if err := h.ruleBased.ProcessAndStore(ctx, sessionID, workspace, ns, msg.Content); err != nil {
					slog.Error("process and store message", "session_id", sessionID, "error", err)
				}
			}
		}
		processed++
	}

	// Count nodes after sync
	afterCount, _ := h.nodeRepo.CountNodes(ctx)

	respondJSON(w, 200, map[string]any{
		"status":        "ok",
		"sessions_processed": processed,
		"nodes_created": afterCount - beforeCount,
	})
}

func (h *SessionHandler) GetContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Get session node
	session, err := h.nodeRepo.Get(r.Context(), id)
	if err != nil {
		respondError(w, 404, "session not found")
		return
	}

	// Get produced edges
	edges, err := h.edgeRepo.ListBySource(r.Context(), id, "produced")
	if err != nil {
		slog.Error("list produced edges", "error", err)
		respondError(w, 500, "failed to list edges")
		return
	}

	// Get all target nodes
	var knowledgeNodes []models.Node
	for _, edge := range edges {
		node, err := h.nodeRepo.Get(r.Context(), edge.TargetID)
		if err == nil {
			knowledgeNodes = append(knowledgeNodes, *node)
		}
	}

	respondJSON(w, 200, map[string]any{
		"session":         session,
		"knowledge_nodes": knowledgeNodes,
		"node_count":      len(knowledgeNodes),
	})
}

// GetReplay returns full session timeline with messages for replay.
func (h *SessionHandler) GetReplay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Get session node
	session, err := h.nodeRepo.Get(r.Context(), id)
	if err != nil {
		respondError(w, 404, "session not found")
		return
	}

	// Get messages from session_messages table
	messages, err := h.getSessionMessages(r.Context(), id)
	if err != nil {
		slog.Error("get session messages", "error", err)
		respondError(w, 500, "failed to load messages")
		return
	}

	// Get produced nodes (children via produced edges)
	edges, err := h.edgeRepo.ListBySource(r.Context(), id, "produced")
	if err != nil {
		slog.Error("list produced edges", "error", err)
		respondError(w, 500, "failed to list edges")
		return
	}

	var children []models.Node
	for _, edge := range edges {
		node, err := h.nodeRepo.Get(r.Context(), edge.TargetID)
		if err == nil {
			children = append(children, *node)
		}
	}

	respondJSON(w, 200, map[string]any{
		"session":  session,
		"messages": messages,
		"children": children,
	})
}

func (h *SessionHandler) getSessionMessages(ctx context.Context, sessionID string) ([]map[string]any, error) {
	rows, err := h.nodeRepo.Pool.Query(ctx, `
		SELECT role, content, timestamp, metadata
		FROM session_messages
		WHERE session_id = $1
		ORDER BY timestamp
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []map[string]any
	for rows.Next() {
		var role, content string
		var timestamp time.Time
		var metadata []byte
		if err := rows.Scan(&role, &content, &timestamp, &metadata); err != nil {
			continue
		}
		msg := map[string]any{
			"role":      role,
			"content":   content,
			"timestamp": timestamp.Format(time.RFC3339),
		}
		if len(metadata) > 0 {
			var meta map[string]any
			json.Unmarshal(metadata, &meta)
			msg["metadata"] = meta
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// RegisterSessionRoutes adds session endpoints to the router.
func (h *SessionHandler) AutoCaptureStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check if auto-capture is enabled via env var
	enabled := os.Getenv("MB_AUTOCAPTURE_ENABLED") == "true" || os.Getenv("MB_MINER_ENABLED") == "true"

	// Check if there are any sessions in the watch path
	hasSessions := false
	watchPath := os.Getenv("MB_WATCH_PATH")
	if watchPath == "" {
		watchPath = filepath.Join(os.Getenv("HOME"), ".hermes", "sessions")
	}
	if entries, err := os.ReadDir(watchPath); err == nil && len(entries) > 0 {
		hasSessions = true
	}

	// Also check if we have graph sessions
	if !hasSessions {
		if sessions, err := h.repo.List(ctx, "", false, 1, 0); err == nil && len(sessions) > 0 {
			hasSessions = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":      enabled,
		"has_sessions": hasSessions,
		"watch_path":   watchPath,
	})
}

func RegisterSessionRoutes(r chi.Router, sh *SessionHandler) {
	r.Route("/sessions", func(r chi.Router) {
		r.Post("/", sh.Create)
		r.Get("/", sh.List)
		r.Get("/auto-capture-status", sh.AutoCaptureStatus)
		r.Get("/{id}", sh.Get)
		r.Post("/{id}/messages", sh.AddMessages)
		r.Get("/{id}/context", sh.GetContext)
		r.Get("/{id}/content", sh.GetContent)
		r.Get("/{id}/replay", sh.GetReplay)
		r.Post("/{id}/close", sh.Close)
		r.Post("/sync", sh.Sync)
	})
}
