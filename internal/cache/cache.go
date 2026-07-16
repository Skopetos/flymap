// Package cache implements a minimal in-memory TTL cache.
// Entries are evicted lazily on read; there is no background sweeper.
package cache

import (
	"sync"
	"time"
)

type entry[T any] struct {
	value     T
	expiresAt time.Time
}

// Cache is a mutex-guarded map of keyed values that expire after their TTL.
type Cache[T any] struct {
	mu    sync.Mutex
	items map[string]entry[T]
}

// New creates an empty Cache.
func New[T any]() *Cache[T] {
	return &Cache[T]{items: make(map[string]entry[T])}
}

// Get returns the cached value for key if present and not expired.
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok || time.Now().After(e.expiresAt) {
		var zero T
		delete(c.items, key)
		return zero, false
	}
	return e.value, true
}

// Set stores value under key with the given TTL, replacing any existing entry.
func (c *Cache[T]) Set(key string, value T, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = entry[T]{value: value, expiresAt: time.Now().Add(ttl)}
}
