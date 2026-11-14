package services

import (
	"github.com/coah80/yoink/internal/config"

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

// YtdlpEstimatedBytes reads the expected size from yt-dlp's --dump-single-json output; 0 means unknown.
func YtdlpEstimatedBytes(meta map[string]interface{}) int64 {
	if size := formatBytes(meta); size > 0 {
		return size
	}
	formats, _ := meta["requested_formats"].([]interface{})
	var total int64
	for _, f := range formats {
		format, _ := f.(map[string]interface{})
		size := formatBytes(format)
		if size <= 0 {
			return 0
		}
		total += size
	}
	return total
}

func formatBytes(format map[string]interface{}) int64 {
	for _, key := range []string{"filesize", "filesize_approx"} {
		if size, ok := format[key].(float64); ok && size > 0 {
			return int64(size)
		}
	}
	return 0
}

// EstimatedDownloadReserveGB sizes a download's disk reserve from its cached metadata; 0 means unknown.
func EstimatedDownloadReserveGB(url string) float64 {
	meta, ok := MetadataCache.Get(url)
	if !ok {
		return 0
	}
	size, _ := meta["filesize"].(int64)
	if size <= 0 {
		return 0
	}
	return max(float64(size)*config.DownloadReserveFactor/(1<<30), config.DownloadReserveMinGB)
}
