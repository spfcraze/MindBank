package handler

import (
	"log/slog"
	"net/http"

	"mindbank/internal/models"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
)

// ProfileHandler handles user profile API endpoints.
type ProfileHandler struct {
	repo *repository.ProfileRepo
}

// NewProfileHandler creates a new profile handler.
func NewProfileHandler(repo *repository.ProfileRepo) *ProfileHandler {
	return &ProfileHandler{repo: repo}
}

// List returns all current profiles.
func (h *ProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.repo.ListAll(r.Context())
	if err != nil {
		slog.Error("list profiles", "error", err)
		respondError(w, 500, "failed to list profiles")
		return
	}
	if profiles == nil {
		profiles = []models.Profile{}
	}
	respondJSON(w, 200, profiles)
}

// Create adds a new profile fact.
func (h *ProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.ProfileCreate
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}
	if req.Fact == "" || req.Category == "" {
		respondError(w, 400, "fact and category are required")
		return
	}

	p, err := h.repo.Create(r.Context(), req)
	if err != nil {
		slog.Error("create profile", "error", err)
		respondError(w, 500, "failed to create profile")
		return
	}
	respondJSON(w, 201, p)
}

// Get returns a single profile by ID.
func (h *ProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, 400, "id is required")
		return
	}

	p, err := h.repo.Get(r.Context(), id)
	if err != nil {
		respondError(w, 404, "profile not found")
		return
	}
	respondJSON(w, 200, p)
}

// Update modifies a profile.
func (h *ProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, 400, "id is required")
		return
	}

	var req models.ProfileUpdate
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}

	p, err := h.repo.Update(r.Context(), id, req)
	if err != nil {
		slog.Error("update profile", "error", err)
		respondError(w, 500, "failed to update profile")
		return
	}
	respondJSON(w, 200, p)
}

// Delete soft-deletes a profile.
func (h *ProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, 400, "id is required")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		slog.Error("delete profile", "error", err)
		respondError(w, 500, "failed to delete profile")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "deleted"})
}

// RegisterProfileRoutes adds profile endpoints.
func RegisterProfileRoutes(r chi.Router, ph *ProfileHandler) {
	r.Get("/profiles", ph.List)
	r.Post("/profiles", ph.Create)
	r.Get("/profiles/{id}", ph.Get)
	r.Put("/profiles/{id}", ph.Update)
	r.Delete("/profiles/{id}", ph.Delete)
}
