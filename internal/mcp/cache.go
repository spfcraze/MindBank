package mcp

import (
	"container/list"
	"sync"
)

// EmbeddingCache is an LRU cache for query → embedding vectors.
type EmbeddingCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[string]*list.Element
	order    *list.List
}

type cacheEntry struct {
	key   string
	value []float32
}

// NewEmbeddingCache creates an LRU cache with the given capacity.
func NewEmbeddingCache(capacity int) *EmbeddingCache {
	if capacity <= 0 {
		capacity = 1000
	}
	return &EmbeddingCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get retrieves an embedding from cache.
func (c *EmbeddingCache) Get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	// The value must be read under the lock: a concurrent Set on the same
	// key rewrites this field, and an unlocked read is a data race that can
	// hand a torn slice header to the search path.
	return el.Value.(*cacheEntry).value, true
}

// Set stores an embedding in cache.
func (c *EmbeddingCache) Set(key string, value []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if el, ok := c.items[key]; ok {
		// Update existing
		el.Value.(*cacheEntry).value = value
		c.order.MoveToFront(el)
		return
	}
	
	// Add new
	el := c.order.PushFront(&cacheEntry{key: key, value: value})
	c.items[key] = el
	
	// Evict if over capacity
	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*cacheEntry).key)
		}
	}
}
