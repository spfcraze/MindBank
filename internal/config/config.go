package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port       int    // MB_PORT (default 8095)
	DBDSN      string // MB_DB_DSN
	OllamaURL  string // MB_OLLAMA_URL (default http://localhost:11434)
	EmbedModel string // MB_EMBED_MODEL (default nomic-embed-text)
	LogLevel   string // MB_LOG_LEVEL (default info)
	CORSOrigins []string // MB_CORS_ORIGINS (default localhost, 127.0.0.1)

	// Compression settings
	CompressionEnabled   bool   // MB_COMPRESSION_ENABLED (default false)
	CompressionModel     string // MB_COMPRESSION_MODEL (default phi4-mini)
	CompressionFallback  string // MB_COMPRESSION_FALLBACK (default llama3.2)

	// LLM extraction for session mining. OpenAI-compatible chat endpoint —
	// works with OpenRouter/OpenAI or a local Ollama (/v1). Extraction stays
	// off (regex fallback) until MB_LLM_MODEL is set.
	LLMAPIURL string // MB_LLM_API_URL (default: OllamaURL + /v1)
	LLMAPIKey string // MB_LLM_API_KEY (default empty; Ollama needs none)
	LLMModel  string // MB_LLM_MODEL   (default empty = LLM extraction disabled)
}

func Load() Config {
	ollama := envStr("MB_OLLAMA_URL", "http://localhost:11434")
	return Config{
		Port:       envInt("MB_PORT", 8095),
		DBDSN:      envStr("MB_DB_DSN", "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"),
		OllamaURL:  ollama,
		EmbedModel: envStr("MB_EMBED_MODEL", "nomic-embed-text"),
		LogLevel:   envStr("MB_LOG_LEVEL", "info"),
		CORSOrigins: envSlice("MB_CORS_ORIGINS", []string{"http://localhost:8095", "http://127.0.0.1:8095"}),

		CompressionEnabled:  envBool("MB_COMPRESSION_ENABLED", false),
		CompressionModel:    envStr("MB_COMPRESSION_MODEL", "phi4-mini"),
		CompressionFallback: envStr("MB_COMPRESSION_FALLBACK", "llama3.2"),

		LLMAPIURL: envStr("MB_LLM_API_URL", ollama+"/v1"),
		LLMAPIKey: envStr("MB_LLM_API_KEY", ""),
		LLMModel:  envStr("MB_LLM_MODEL", ""),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envSlice(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		return strings.Split(v, ",")
	}
	return fallback
}
