package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/reasoner"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
)

type SessionHandler struct {
	repo      *repository.SessionRepo
	nodeRepo  *repository.NodeRepo
	ruleBased *reasoner.RuleBased
}

func NewSessionHandler(repo *repository.SessionRepo, nodeRepo *repository.NodeRepo, rb *reasoner.RuleBased) *SessionHandler {
	return &SessionHandler{repo: repo, nodeRepo: nodeRepo, ruleBased: rb}
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
	if sessions == nil {
		sessions = []models.Session{}
	}
	respondJSON(w, 200, sessions)
}

func (h *SessionHandler) AddMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")

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
				_ = h.ruleBased.ProcessAndStore(bgCtx, sessionID, ws, ns, msg.Content)
			}
		}
	}()

	// Enqueue messages for embedding (use background ctx — request may be cancelled)
	for _, m := range created {
		_ = embedder.EnqueueMessage(bgCtx, h.ruleBased.Pool(), m.ID)
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
				_ = h.ruleBased.ProcessAndStore(ctx, sessionID, workspace, ns, msg.Content)
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

// RegisterSessionRoutes adds session endpoints to the router.
func RegisterSessionRoutes(r chi.Router, sh *SessionHandler) {
	r.Route("/sessions", func(r chi.Router) {
		r.Post("/", sh.Create)
		r.Get("/", sh.List)
		r.Get("/{id}", sh.Get)
		r.Post("/{id}/messages", sh.AddMessages)
		r.Get("/{id}/context", sh.GetContext)
		r.Post("/{id}/close", sh.Close)
		r.Post("/sync", sh.Sync)
	})
}
