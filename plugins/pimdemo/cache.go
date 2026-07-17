package pimdemo

import (
	"sync"
	"time"
)

const defaultCacheMaxEntries = 256

type cacheEntry struct {
	data      EnrichmentData
	expiresAt time.Time
}

// ttlCache stores PIM enrichment responses keyed by product slug.
type ttlCache struct {
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	items      map[string]cacheEntry
}

func newTTLCache(ttl time.Duration) *ttlCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ttlCache{
		ttl:        ttl,
		maxEntries: defaultCacheMaxEntries,
		items:      make(map[string]cacheEntry),
	}
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
	defer c.mu.Unlock()

	now := time.Now()
	_, slugPresent := c.items[slug]

	var oldestKey string
	var oldestExpiry time.Time
	foundOldest := false

	for key, entry := range c.items {
		if now.After(entry.expiresAt) {
			delete(c.items, key)
			if key == slug {
				slugPresent = false
			}
			continue
		}
		if !foundOldest || entry.expiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = entry.expiresAt
			foundOldest = true
		}
	}

	if c.maxEntries > 0 && len(c.items) >= c.maxEntries && !slugPresent && foundOldest {
		delete(c.items, oldestKey)
	}
	c.items[slug] = cacheEntry{data: data, expiresAt: now.Add(c.ttl)}
}
