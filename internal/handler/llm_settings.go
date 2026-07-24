package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mindbank/internal/config"
	"mindbank/internal/repository"
)

// LLMSettingsHandler manages the LLM API configuration used for session
// mining / fact extraction. Users without a local GPU can point MindBank at a
// hosted OpenAI-compatible endpoint (OpenRouter, OpenAI, Groq, …) from the
// dashboard Settings tab; the values are stored in the settings table and
// override the env defaults at runtime (see reasoner.resolve()).
type LLMSettingsHandler struct {
	settingsRepo *repository.SettingsRepo
	cfg          config.Config
}

// NewLLMSettingsHandler creates a new handler.
func NewLLMSettingsHandler(settingsRepo *repository.SettingsRepo, cfg config.Config) *LLMSettingsHandler {
	return &LLMSettingsHandler{settingsRepo: settingsRepo, cfg: cfg}
}

// maskKey returns a display-safe version of an API key ("sk-…abcd").
func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "••••"
	}
	return key[:3] + "…" + key[len(key)-4:]
}

// detectProvider guesses a friendly provider name from the API URL.
func detectProvider(url string) string {
	u := strings.ToLower(url)
	switch {
	case strings.Contains(u, "openrouter.ai"):
		return "OpenRouter"
	case strings.Contains(u, "api.openai.com"):
		return "OpenAI"
	case strings.Contains(u, "api.anthropic.com"):
		return "Anthropic"
	case strings.Contains(u, "api.groq.com"):
		return "Groq"
	case strings.Contains(u, "api.mistral.ai"):
		return "Mistral"
	case strings.Contains(u, "api.together"):
		return "Together"
	case strings.Contains(u, "api.deepseek.com"):
		return "DeepSeek"
	case strings.Contains(u, "generativelanguage.googleapis.com"):
		return "Google Gemini"
	case strings.Contains(u, "11434") || strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1"):
		return "Ollama (local)"
	case u == "":
		return ""
	default:
		return "Custom"
	}
}

// resolveEffective returns the URL/key/model actually in effect (DB override
// or env default), matching reasoner.resolve() precedence.
func (h *LLMSettingsHandler) resolveEffective(ctx context.Context) (url, key, model string) {
	url, _ = h.settingsRepo.Get(ctx, "llm_api_url")
	key, _ = h.settingsRepo.Get(ctx, "llm_api_key")
	model, _ = h.settingsRepo.Get(ctx, "llm_model")
	if url == "" {
		url = h.cfg.LLMAPIURL
	}
	if key == "" {
		key = h.cfg.LLMAPIKey
	}
	if model == "" {
		model = h.cfg.LLMModel
	}
	return url, key, model
}

// GetSettings returns current LLM settings (key masked).
func (h *LLMSettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	url, key, model := h.resolveEffective(ctx)

	// Whether the values come from DB (user-set) vs. env default.
	dbURL, _ := h.settingsRepo.Get(ctx, "llm_api_url")
	dbModel, _ := h.settingsRepo.Get(ctx, "llm_model")

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"api_url":     url,
		"api_key":     maskKey(key),
		"has_api_key": strings.TrimSpace(key) != "",
		"model":       model,
		"provider":    detectProvider(url),
		"enabled":     url != "" && model != "",
		"is_custom":   dbURL != "" || dbModel != "",
	})
}

// UpdateSettings persists the LLM configuration. An empty api_key is left
// unchanged (so re-saving doesn't wipe the key); send api_key:"" with
// clear_key:true to remove it.
func (h *LLMSettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		APIURL   *string `json:"api_url,omitempty"`
		APIKey   *string `json:"api_key,omitempty"`
		Model    *string `json:"model,omitempty"`
		ClearKey bool    `json:"clear_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if req.APIURL != nil {
		h.settingsRepo.Set(ctx, "llm_api_url", strings.TrimSpace(*req.APIURL))
	}
	if req.Model != nil {
		h.settingsRepo.Set(ctx, "llm_model", strings.TrimSpace(*req.Model))
	}
	if req.ClearKey {
		h.settingsRepo.Set(ctx, "llm_api_key", "")
	} else if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		// Ignore masked value coming back from the UI unchanged.
		if !strings.Contains(*req.APIKey, "…") {
			h.settingsRepo.Set(ctx, "llm_api_key", strings.TrimSpace(*req.APIKey))
		}
	}

	h.GetSettings(w, r)
}

// TestConnection calls the configured (or supplied) endpoint's /models list to
// verify the URL + key work, and returns detected provider + model count.
func (h *LLMSettingsHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Allow testing values before they are saved.
	var req struct {
		APIURL string `json:"api_url,omitempty"`
		APIKey string `json:"api_key,omitempty"`
		Model  string `json:"model,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	url, key, model := h.resolveEffective(ctx)
	if strings.TrimSpace(req.APIURL) != "" {
		url = strings.TrimSpace(req.APIURL)
	}
	if strings.TrimSpace(req.APIKey) != "" && !strings.Contains(req.APIKey, "…") {
		key = strings.TrimSpace(req.APIKey)
	}
	if strings.TrimSpace(req.Model) != "" {
		model = strings.TrimSpace(req.Model)
	}

	if url == "" {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": "No API URL configured",
		})
		return
	}

	base := strings.TrimRight(url, "/")
	httpReq, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": "Could not reach endpoint: " + err.Error(),
			"provider": detectProvider(url),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": "Authentication failed — check your API key",
			"provider": detectProvider(url), "status": resp.StatusCode,
		})
		return
	}
	if resp.StatusCode >= 400 {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf("Endpoint returned %d", resp.StatusCode),
			"provider": detectProvider(url), "status": resp.StatusCode,
		})
		return
	}

	// Parse the OpenAI-compatible model list.
	var ml struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &ml)
	modelFound := false
	modelIDs := make([]string, 0, len(ml.Data))
	for _, m := range ml.Data {
		modelIDs = append(modelIDs, m.ID)
		if model != "" && m.ID == model {
			modelFound = true
		}
	}

	// If a chat model is set, do a tiny chat completion to confirm it works.
	chatOK := true
	chatErr := ""
	if model != "" {
		chatOK, chatErr = h.probeChat(ctx, base, key, model)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":          chatOK,
		"provider":    detectProvider(url),
		"model_count": len(modelIDs),
		"model_found": modelFound,
		"chat_ok":     chatOK,
		"error":       chatErr,
	})
}

// probeChat issues a minimal chat completion to confirm the model responds.
func (h *LLMSettingsHandler) probeChat(ctx context.Context, base, key, model string) (bool, string) {
	payload, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "Chat request failed: " + err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("Model test returned %d", resp.StatusCode)
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return false, msg
	}
	return true, ""
}
