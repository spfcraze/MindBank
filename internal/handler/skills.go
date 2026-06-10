package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/skills"
)

// SkillsHandler provides skill registry endpoints.
type SkillsHandler struct {
	registry *skills.Registry
}

// NewSkillsHandler creates a new skills handler.
func NewSkillsHandler(pool *pgxpool.Pool) *SkillsHandler {
	return &SkillsHandler{
		registry: skills.NewRegistry(pool),
	}
}

// ListSkills handles GET /api/v1/skills
func (h *SkillsHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	list := h.registry.List()
	respondJSON(w, 200, map[string]interface{}{
		"skills": list,
		"count":  len(list),
	})
}

// ExecuteSkill handles POST /api/v1/skills/{name}/run
func (h *SkillsHandler) ExecuteSkill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")

	var params map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		// Empty body is ok — use nil params
		params = nil
	}

	result, err := h.registry.Execute(ctx, name, params)
	if err != nil {
		respondError(w, 404, err.Error())
		return
	}

	respondJSON(w, 200, map[string]interface{}{
		"skill":  name,
		"result": result,
	})
}
