package identity

import (
	"sync"
	"time"
)

const (
	DefaultNonceTTL   = 4 * time.Minute
	DefaultNonceLimit = 512
)

type NonceCache struct {
	ttl   time.Duration
	limit int

	mu   sync.Mutex
	seen map[string]map[string]time.Time
}

func NewNonceCache(ttl time.Duration, limit int) *NonceCache {
	if ttl <= 0 {
		ttl = DefaultNonceTTL
	}
	if limit <= 0 {
		limit = DefaultNonceLimit
	}
	return &NonceCache{
		ttl:   ttl,
		limit: limit,
		seen:  make(map[string]map[string]time.Time),
	}
}

func (c *NonceCache) Seen(deviceID, nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries := c.seen[deviceID]
	if entries == nil {
		entries = make(map[string]time.Time)
		c.seen[deviceID] = entries
	}

	for value, at := range entries {
		if now.Sub(at) > c.ttl {
			delete(entries, value)
		}
	}

	if _, used := entries[nonce]; used {
		return true
	}
	entries[nonce] = now

	if len(entries) > c.limit {
		oldestValue := ""
		var oldestAt time.Time
		for value, at := range entries {
			if oldestAt.IsZero() || at.Before(oldestAt) {
				oldestValue, oldestAt = value, at
			}
		}
		delete(entries, oldestValue)
	}
	return false
}

func (c *NonceCache) Size(deviceID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen[deviceID])
}

func (c *NonceCache) Devices() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}
