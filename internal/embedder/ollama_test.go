package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbed_EmptyText(t *testing.T) {
	c := NewClient("http://localhost:11434", "test-model")
	_, err := c.Embed(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
	if !IsBadQuery(err) {
		t.Errorf("expected BAD_QUERY, got %T: %v", err, err)
	}
}

func TestEmbed_Success(t *testing.T) {
	wantVec := []float32{0.1, 0.2, 0.3}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode error: %v", err)
			http.Error(w, "bad request", 400)
			return
		}
		if req.Model != "test-model" {
			t.Errorf("expected model %q, got %q", "test-model", req.Model)
		}
		if req.Prompt != "hello world" {
			t.Errorf("expected prompt %q, got %q", "hello world", req.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embedResponse{Embedding: wantVec})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	vec, err := c.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != len(wantVec) {
		t.Fatalf("expected %d dims, got %d", len(wantVec), len(vec))
	}
	for i, v := range vec {
		if v != wantVec[i] {
			t.Errorf("vec[%d] = %v, want %v", i, v, wantVec[i])
		}
	}
}

func TestEmbed_OllamaUnavailable(t *testing.T) {
	// Server that immediately closes connection
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", 500)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	_, err := c.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsUnavailable(err) {
		t.Errorf("expected UNAVAILABLE, got %T: %v", err, err)
	}
}

func TestEmbed_429Busy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	_, err := c.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsBusy(err) {
		t.Errorf("expected BUSY, got %T: %v", err, err)
	}
}

func TestEmbed_EmptyEmbeddingResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{}})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	_, err := c.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
	if !IsUnavailable(err) {
		t.Errorf("expected UNAVAILABLE for empty embedding, got %T: %v", err, err)
	}
}

func TestEmbed_SemaphoreLimitsConcurrency(t *testing.T) {
	var active int64
	var maxActive int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := atomic.AddInt64(&active, 1)
		for {
			m := atomic.LoadInt64(&maxActive)
			if a > m {
				if atomic.CompareAndSwapInt64(&maxActive, m, a) {
					break
				}
			} else {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&active, -1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{0.1}})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	// Launch 10 concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Embed(context.Background(), "test")
		}()
	}
	wg.Wait()

	if maxActive > 4 {
		t.Errorf("max concurrent requests = %d, expected ≤ 4", maxActive)
	}
}

func TestEmbed_Stats(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{0.1}})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	ctx := context.Background()

	c.Embed(ctx, "a")
	c.Embed(ctx, "b")
	_, err := c.Embed(ctx, "") // BAD_QUERY — still counts as total req? No, returns before totalReqs.Add

	stats := c.GetStats()
	if stats.Total != 2 {
		t.Errorf("expected total=2, got %d", stats.Total)
	}
	if stats.Errors != 0 {
		t.Errorf("expected errors=0, got %d", stats.Errors)
	}
	if stats.Model != "test-model" {
		t.Errorf("expected model=test-model, got %s", stats.Model)
	}
	if stats.AvgLatency < 0 {
		t.Errorf("expected avg latency >= 0, got %f", stats.AvgLatency)
	}

	// Trigger an error
	_, err = c.Embed(ctx, "") // BAD_QUERY
	// This returns before totalReqs.Add, so total stays 2
	if err == nil {
		t.Error("expected error for empty text")
	}
	stats = c.GetStats()
	if stats.Total != 2 {
		t.Errorf("expected total still 2 (empty text returns early), got %d", stats.Total)
	}
}

func TestEmbedBatch_OrderPreserved(t *testing.T) {
	var received []string
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		received = append(received, req.Prompt)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Return embedding with first element matching index derived from prompt
		var vec []float32
		switch req.Prompt {
		case "first":
			vec = []float32{1.0}
		case "second":
			vec = []float32{2.0}
		case "third":
			vec = []float32{3.0}
		default:
			vec = []float32{0.0}
		}
		json.NewEncoder(w).Encode(embedResponse{Embedding: vec})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	results, err := c.EmbedBatch(context.Background(), []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Verify order preserved
	if results[0][0] != 1.0 {
		t.Errorf("results[0][0] = %v, want 1.0", results[0][0])
	}
	if results[1][0] != 2.0 {
		t.Errorf("results[1][0] = %v, want 2.0", results[1][0])
	}
	if results[2][0] != 3.0 {
		t.Errorf("results[2][0] = %v, want 3.0", results[2][0])
	}
}

func TestEmbedBatch_EmptyInput(t *testing.T) {
	c := NewClient("http://localhost:11434", "test-model")
	results, err := c.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestEmbedBatch_ErrorPropagation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Prompt, "bad") {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embedResponse{Embedding: []float32{0.1}})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	_, err := c.EmbedBatch(context.Background(), []string{"good", "bad", "good"})
	if err == nil {
		t.Fatal("expected error from batch")
	}
	if !strings.Contains(err.Error(), "embed[1]") {
		t.Errorf("expected error to mention embed[1], got: %v", err)
	}
}

func TestHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{
				{"name": "test-model:latest"},
				{"name": "other-model"},
			},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "test-model")
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("unexpected health error: %v", err)
	}

	// Model not found
	c2 := NewClient(ts.URL, "missing-model")
	if err := c2.Health(context.Background()); err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestHealth_Unreachable(t *testing.T) {
	c := NewClient("http://localhost:1", "test-model")
	// Use a short timeout so the test doesn't hang on connection refused
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Health(ctx); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
