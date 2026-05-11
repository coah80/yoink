package services

import (
	"sync"
	"time"
)

type cachedMeta struct {
	data      map[string]interface{}
	expiresAt time.Time
}

type MetaCache struct {
	mu      sync.RWMutex
	entries map[string]*cachedMeta
}

var MetadataCache = &MetaCache{
	entries: make(map[string]*cachedMeta),
}

func (c *MetaCache) Get(url string) (map[string]interface{}, bool) {
	c.mu.RLock()
	entry, ok := c.entries[url]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	// Return a copy to prevent mutation
	result := make(map[string]interface{}, len(entry.data))
	for k, v := range entry.data {
		result[k] = v
	}
	return result, true
}

func (c *MetaCache) Set(url string, data map[string]interface{}, ttl time.Duration) {
	c.mu.Lock()
	c.entries[url] = &cachedMeta{
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
	c.mu.Unlock()
}

func (c *MetaCache) StartCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			c.mu.Lock()
			now := time.Now()
			for url, entry := range c.entries {
				if now.After(entry.expiresAt) {
					delete(c.entries, url)
				}
			}
			c.mu.Unlock()
		}
	}()
}
