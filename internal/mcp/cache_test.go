package mcp

import (
	"testing"
)

func TestEmbeddingCache(t *testing.T) {
	cache := NewEmbeddingCache(3) // small for testing

	// Test 1: Miss returns nil
	if _, ok := cache.Get("unknown query"); ok {
		t.Error("expected cache miss for unknown query")
	}

	// Test 2: Set and Get
	vec := []float32{0.1, 0.2, 0.3}
	cache.Set("query1", vec)
	if got, ok := cache.Get("query1"); !ok {
		t.Error("expected cache hit after Set")
	} else if len(got) != 3 || got[0] != 0.1 {
		t.Errorf("expected [0.1 0.2 0.3], got %v", got)
	}

	// Test 3: LRU eviction — fill beyond capacity
	cache.Set("query2", []float32{0.4, 0.5})
	cache.Set("query3", []float32{0.6, 0.7})
	cache.Set("query4", []float32{0.8, 0.9}) // should evict query1

	if _, ok := cache.Get("query1"); ok {
		t.Error("expected query1 to be evicted (LRU)")
	}
	if _, ok := cache.Get("query2"); !ok {
		t.Error("expected query2 to still exist")
	}

	// Test 4: Get promotes to front (LRU order)
	cache.Get("query2") // promotes query2
	cache.Set("query5", []float32{1.0}) // should evict query3, not query2
	if _, ok := cache.Get("query3"); ok {
		t.Error("expected query3 to be evicted after query2 promotion")
	}
	if _, ok := cache.Get("query2"); !ok {
		t.Error("expected query2 to still exist after promotion")
	}
}

func TestEmbeddingCacheThreadSafe(t *testing.T) {
	cache := NewEmbeddingCache(100)
	// Just verify no panic under concurrent access
	go func() {
		for i := 0; i < 100; i++ {
			cache.Set("key", []float32{float32(i)})
		}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get("key")
		}
	}()
}
