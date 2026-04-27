package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"mindbank/internal/repository"
)

// CompressionSettingsHandler manages compression configuration.
type CompressionSettingsHandler struct {
	settingsRepo *repository.SettingsRepo
	ollamaURL    string
}

// NewCompressionSettingsHandler creates a new handler.
func NewCompressionSettingsHandler(settingsRepo *repository.SettingsRepo, ollamaURL string) *CompressionSettingsHandler {
	return &CompressionSettingsHandler{
		settingsRepo: settingsRepo,
		ollamaURL:    ollamaURL,
	}
}

// GetSettings returns current compression settings.
func (h *CompressionSettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	enabled := h.settingsRepo.GetBool(ctx, "compression_enabled")
	model, _ := h.settingsRepo.Get(ctx, "compression_model")
	setupComplete := h.settingsRepo.GetBool(ctx, "compression_setup_complete")

	if model == "" {
		model = "phi4-mini"
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":        enabled,
		"model":          model,
		"setup_complete": setupComplete,
	})
}

// UpdateSettings updates compression settings.
func (h *CompressionSettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Enabled *bool   `json:"enabled,omitempty"`
		Model   *string `json:"model,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if req.Enabled != nil {
		h.settingsRepo.Set(ctx, "compression_enabled", fmt.Sprintf("%t", *req.Enabled))
	}
	if req.Model != nil && *req.Model != "" {
		h.settingsRepo.Set(ctx, "compression_model", *req.Model)
	}

	h.GetSettings(w, r)
}

// Setup pulls the model and enables compression.
func (h *CompressionSettingsHandler) Setup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	model, _ := h.settingsRepo.Get(ctx, "compression_model")
	if model == "" {
		model = "phi4-mini"
	}

	// Check if model is available
	available, err := h.checkModelAvailable(model)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"status": "error",
			"error":  fmt.Sprintf("failed to check model: %v", err),
		})
		return
	}

	if !available {
		// Try to pull model
		if err := h.pullModel(model); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"status": "error",
				"error":  fmt.Sprintf("failed to pull model %s: %v", model, err),
			})
			return
		}
	}

	// Mark setup complete and enable
	h.settingsRepo.Set(ctx, "compression_setup_complete", "true")
	h.settingsRepo.Set(ctx, "compression_enabled", "true")

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "setup_complete",
		"model":  model,
	})
}

func (h *CompressionSettingsHandler) checkModelAvailable(model string) (bool, error) {
	resp, err := http.Get(h.ollamaURL + "/api/tags")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	for _, m := range result.Models {
		if strings.HasPrefix(m.Name, model) {
			return true, nil
		}
	}
	return false, nil
}

func (h *CompressionSettingsHandler) pullModel(model string) error {
	payload := map[string]string{"name": model}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(h.ollamaURL+"/api/pull", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull failed: %s", resp.Status)
	}
	return nil
}
