package api

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// cacheEntry holds a cached response with its expiry time.
type cacheEntry struct {
	data      []byte
	expiresAt time.Time
}

// responseCache is a simple in-memory TTL cache for expensive API responses.
type responseCache struct {
	mu       sync.RWMutex
	entries  map[string]*cacheEntry
	stopCh   chan struct{}
	stopOnce sync.Once
}

// newResponseCache creates a new response cache and starts a background cleanup goroutine.
func newResponseCache() *responseCache {
	c := &responseCache{
		entries: make(map[string]*cacheEntry),
		stopCh:  make(chan struct{}),
	}
	go c.cleanup()
	return c
}

// Get retrieves a cached response. Returns the data and true on hit, nil and false on miss.
func (c *responseCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

// responseCacheMaxEntries caps the number of entries to prevent unbounded growth
// between cleanup intervals on high-traffic instances.
const responseCacheMaxEntries = 10_000

// Set stores a response in the cache with the given TTL.
func (c *responseCache) Set(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Prevent unbounded growth: skip caching if at capacity.
	if len(c.entries) >= responseCacheMaxEntries {
		return
	}

	c.entries[key] = &cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
}

// Stop terminates the background cleanup goroutine. Safe to call multiple times.
func (c *responseCache) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
}

// cleanup periodically evicts expired entries.
func (c *responseCache) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for k, v := range c.entries {
				if now.After(v.expiresAt) {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}

// cacheKey produces a deterministic cache key from the given parts.
func cacheKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte("|"))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:32]
}
