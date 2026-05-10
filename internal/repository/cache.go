package repository

import (
	"sync"
	"time"

	"mindbank/internal/models"
)

// ResultCache caches search results (hybrid search output) to avoid repeated
// database queries for identical queries within a short TTL.
type ResultCache struct {
	mu      sync.RWMutex
	entries map[string]resultCacheEntry
	maxAge  time.Duration
	maxSize int
}

type resultCacheEntry struct {
	results   []models.SearchResult
	createdAt time.Time
}

// NewResultCache creates a new search result cache.
func NewResultCache() *ResultCache {
	return &ResultCache{
		entries: make(map[string]resultCacheEntry),
		maxAge:  60 * time.Second,
		maxSize: 1000,
	}
}

// Get retrieves cached search results.
func (c *ResultCache) Get(key string) ([]models.SearchResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.createdAt) > c.maxAge {
		return nil, false
	}
	// Return a deep copy so callers can safely mutate (e.g., graph expansion)
	return cloneResults(entry.results), true
}

// Set stores search results in the cache.
func (c *ResultCache) Set(key string, results []models.SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		var oldest string
		var oldestTime time.Time
		for k, v := range c.entries {
			if oldestTime.IsZero() || v.createdAt.Before(oldestTime) {
				oldest = k
				oldestTime = v.createdAt
			}
		}
		delete(c.entries, oldest)
	}

	c.entries[key] = resultCacheEntry{
		results:   cloneResults(results),
		createdAt: time.Now(),
	}
}

// InvalidateAll clears all cached results. Call after any write operation
// that could affect search relevance (node create/update/delete).
func (c *ResultCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]resultCacheEntry)
}

// Stats returns cache statistics.
func (c *ResultCache) Stats() (size int, maxAge time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries), c.maxAge
}

// cloneResults creates a deep copy of search results.
func cloneResults(src []models.SearchResult) []models.SearchResult {
	if src == nil {
		return nil
	}
	out := make([]models.SearchResult, len(src))
	for i, r := range src {
		out[i] = models.SearchResult{
			NodeID:    r.NodeID,
			Label:     r.Label,
			NodeType:  r.NodeType,
			Content:   r.Content,
			Namespace: r.Namespace,
			RRFScore:  r.RRFScore,
		}
	}
	return out
}
