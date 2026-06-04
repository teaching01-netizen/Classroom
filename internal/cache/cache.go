package cache

import (
	"sync"
	"time"
)

type entry struct {
	data      interface{}
	expiresAt time.Time
}

// Cache is a simple in-memory TTL cache.
type Cache struct {
	mu    sync.RWMutex
	items map[string]*entry
}

func New() *Cache {
	return &Cache{items: make(map[string]*entry)}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	if !ok {
		c.mu.RUnlock()
		return nil, false
	}
	expired := time.Now().After(e.expiresAt)
	c.mu.RUnlock()
	if expired {
		return nil, false
	}
	return e.data, true
}

func (c *Cache) Set(key string, data interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = &entry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
}

func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// GetStale returns cached data even if the TTL has expired.
// Returns false only if the key does not exist at all.
// This enables "serve stale" patterns where callers can return slightly
// stale data while triggering an async refresh.
func (c *Cache) GetStale(key string) (interface{}, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return e.data, true
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*entry)
}

// Size returns the number of cached items.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// MarkStale extends a cached entry's TTL by delta without removing it.
// This enables "stale-while-revalidate": the caller returns the stale data
// immediately and triggers an async refresh. The entry stays valid long
// enough for the refresh goroutine to populate a fresh copy.
func (c *Cache) MarkStale(key string, delta time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return false
	}
	e.expiresAt = e.expiresAt.Add(delta)
	return true
}
