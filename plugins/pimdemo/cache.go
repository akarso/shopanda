package pimdemo

import (
	"sync"
	"time"
)

type cacheEntry struct {
	data      EnrichmentData
	expiresAt time.Time
}

// ttlCache stores PIM enrichment responses keyed by product slug.
type ttlCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]cacheEntry
}

func newTTLCache(ttl time.Duration) *ttlCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ttlCache{ttl: ttl, items: make(map[string]cacheEntry)}
}

func (c *ttlCache) Get(slug string) (EnrichmentData, bool) {
	if c == nil {
		return EnrichmentData{}, false
	}
	c.mu.RLock()
	entry, ok := c.items[slug]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return EnrichmentData{}, false
	}
	return entry.data, true
}

func (c *ttlCache) Set(slug string, data EnrichmentData) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items[slug] = cacheEntry{data: data, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
