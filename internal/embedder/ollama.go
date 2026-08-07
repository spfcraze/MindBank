package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client talks to Ollama's embedding API.
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
	sem        chan struct{} // semaphore to limit concurrent Ollama requests

	// Metrics (atomic counters)
	activeReqs  atomic.Int64 // currently in-flight requests
	queuedReqs  atomic.Int64 // requests waiting on semaphore
	totalReqs   atomic.Int64 // total requests since start
	totalErrors atomic.Int64 // total errors since start
	totalMs     atomic.Int64 // cumulative latency in ms
}

// Stats holds embedding client metrics.
type Stats struct {
	Active     int64   `json:"active"`
	Queued     int64   `json:"queued"`
	Total      int64   `json:"total"`
	Errors     int64   `json:"errors"`
	AvgLatency float64 `json:"avg_latency_ms"`
	Model      string  `json:"model"`
	BaseURL    string  `json:"base_url"`
}

// NewClient creates an Ollama embedding client.
func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		sem: make(chan struct{}, 4), // max 4 concurrent embedding requests
	}
}

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed generates a vector embedding for the given text.
// Returns typed EmbedError on failure:
//   - BAD_QUERY: empty or unembeddable text
//   - BUSY: semaphore full, context deadline
//   - UNAVAILABLE: Ollama unreachable or error
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, NewBadQueryError("empty text")
	}

	// Track queue depth
	c.queuedReqs.Add(1)
	c.totalReqs.Add(1)

	// Acquire semaphore slot (blocks if 4 requests already in flight)
	select {
	case c.sem <- struct{}{}:
		c.queuedReqs.Add(-1)
		c.activeReqs.Add(1)
		defer func() {
			<-c.sem
			c.activeReqs.Add(-1)
		}()
	case <-ctx.Done():
		c.queuedReqs.Add(-1)
		c.totalErrors.Add(1)
		return nil, NewBusyError("context cancelled while waiting for slot")
	}

	start := time.Now()

	body, _ := json.Marshal(embedRequest{
		Model:  c.model,
		Prompt: text,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		c.totalErrors.Add(1)
		return nil, NewUnavailableError("create request failed", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.totalErrors.Add(1)
		return nil, NewUnavailableError("ollama request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		c.totalErrors.Add(1)
		return nil, NewBusyError(fmt.Sprintf("ollama returned %d", resp.StatusCode))
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		c.totalErrors.Add(1)
		return nil, NewUnavailableError(fmt.Sprintf("ollama status %d: %s", resp.StatusCode, string(respBody)), nil)
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.totalErrors.Add(1)
		return nil, NewUnavailableError("decode response failed", err)
	}

	if len(result.Embedding) == 0 {
		c.totalErrors.Add(1)
		return nil, NewUnavailableError("empty embedding returned", nil)
	}

	// Track latency
	elapsed := time.Since(start).Milliseconds()
	c.totalMs.Add(elapsed)

	return result.Embedding, nil
}

// GetStats returns current embedding client metrics.
func (c *Client) GetStats() Stats {
	total := c.totalReqs.Load()
	var avgLatency float64
	if total > 0 {
		avgLatency = float64(c.totalMs.Load()) / float64(total)
	}
	return Stats{
		Active:     c.activeReqs.Load(),
		Queued:     c.queuedReqs.Load(),
		Total:      total,
		Errors:     c.totalErrors.Load(),
		AvgLatency: avgLatency,
		Model:      c.model,
		BaseURL:    c.baseURL,
	}
}

// EmbedBatch generates embeddings for multiple texts in parallel.
// Concurrency is bounded by the client semaphore (4 max).
// Returns results in the same order as input texts.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	results := make([][]float32, len(texts))
	errs := make([]error, len(texts))
	var wg sync.WaitGroup

	for i, text := range texts {
		wg.Add(1)
		go func(i int, text string) {
			defer wg.Done()
			emb, err := c.Embed(ctx, text)
			results[i] = emb
			errs[i] = err
		}(i, text)
	}

	wg.Wait()

	// Return first error if any
	for i, err := range errs {
		if err != nil {
			slog.Warn("embedding failed in batch", "index", i, "error", err)
			return nil, fmt.Errorf("embed[%d]: %w", i, err)
		}
	}

	return results, nil
}

// Health checks if Ollama is reachable and the configured model is available.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	// Check model is available
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return fmt.Errorf("failed to decode tags: %w", err)
	}
	for _, m := range tags.Models {
		// Strip tag suffix (e.g., ":latest") for comparison
		name := m.Name
		if idx := strings.LastIndex(name, ":"); idx > 0 {
			name = name[:idx]
		}
		if name == c.model || m.Name == c.model {
			return nil
		}
	}
	return fmt.Errorf("model %q not found in Ollama", c.model)
}
