package handler

import (
	"log/slog"
	"net/http"

	"mindbank/internal/repository"
)

type WorkspaceHandler struct {
	repo *repository.WorkspaceRepo
}

func NewWorkspaceHandler(repo *repository.WorkspaceRepo) *WorkspaceHandler {
	return &WorkspaceHandler{repo: repo}
}

func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.repo.List(r.Context())
	if err != nil {
		slog.Error("list workspaces", "error", err)
		respondError(w, 500, "failed to list workspaces")
		return
	}
	if workspaces == nil {
		workspaces = []string{}
	}
	respondJSON(w, 200, workspaces)
}
