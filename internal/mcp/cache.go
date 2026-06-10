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
	c.mu.RLock()
	el, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	
	// Promote to front (LRU)
	c.mu.Lock()
	c.order.MoveToFront(el)
	c.mu.Unlock()
	
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
