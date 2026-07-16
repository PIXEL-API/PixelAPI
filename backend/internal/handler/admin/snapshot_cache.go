package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const defaultSnapshotCacheMaxEntries = 256

type snapshotCacheEntry struct {
	ETag      string
	Payload   any
	ExpiresAt time.Time
}

type snapshotCache struct {
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	items      map[string]snapshotCacheEntry
	sf         singleflight.Group
}

type snapshotCacheLoadResult struct {
	Entry snapshotCacheEntry
	Hit   bool
}

func newSnapshotCache(ttl time.Duration) *snapshotCache {
	return newSnapshotCacheWithLimit(ttl, defaultSnapshotCacheMaxEntries)
}

func newSnapshotCacheWithLimit(ttl time.Duration, maxEntries int) *snapshotCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if maxEntries <= 0 {
		maxEntries = defaultSnapshotCacheMaxEntries
	}
	return &snapshotCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		items:      make(map[string]snapshotCacheEntry),
	}
}

func (c *snapshotCache) Get(key string) (snapshotCacheEntry, bool) {
	if c == nil || key == "" {
		return snapshotCacheEntry{}, false
	}
	now := time.Now()

	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return snapshotCacheEntry{}, false
	}
	if !now.Before(entry.ExpiresAt) {
		c.mu.Lock()
		current, currentExists := c.items[key]
		if currentExists && !now.Before(current.ExpiresAt) {
			delete(c.items, key)
			currentExists = false
		}
		c.mu.Unlock()
		if currentExists {
			return current, true
		}
		return snapshotCacheEntry{}, false
	}
	return entry, true
}

func (c *snapshotCache) Set(key string, payload any) snapshotCacheEntry {
	if c == nil {
		return snapshotCacheEntry{}
	}
	now := time.Now()
	entry := snapshotCacheEntry{
		ETag:      buildETagFromAny(payload),
		Payload:   payload,
		ExpiresAt: now.Add(c.ttl),
	}
	if key == "" {
		return entry
	}
	c.mu.Lock()
	c.removeExpiredLocked(now)
	if _, exists := c.items[key]; !exists && len(c.items) >= c.maxEntries {
		c.evictOldestLocked()
	}
	c.items[key] = entry
	c.mu.Unlock()
	return entry
}

func (c *snapshotCache) removeExpiredLocked(now time.Time) {
	for key, entry := range c.items {
		if !now.Before(entry.ExpiresAt) {
			delete(c.items, key)
		}
	}
}

func (c *snapshotCache) evictOldestLocked() {
	var oldestKey string
	var oldestExpiry time.Time
	for key, entry := range c.items {
		if oldestKey == "" || entry.ExpiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = entry.ExpiresAt
		}
	}
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

func (c *snapshotCache) GetOrLoad(key string, load func() (any, error)) (snapshotCacheEntry, bool, error) {
	if load == nil {
		return snapshotCacheEntry{}, false, nil
	}
	if entry, ok := c.Get(key); ok {
		return entry, true, nil
	}
	if c == nil || key == "" {
		payload, err := load()
		if err != nil {
			return snapshotCacheEntry{}, false, err
		}
		return c.Set(key, payload), false, nil
	}

	value, err, _ := c.sf.Do(key, func() (any, error) {
		if entry, ok := c.Get(key); ok {
			return snapshotCacheLoadResult{Entry: entry, Hit: true}, nil
		}
		payload, err := load()
		if err != nil {
			return nil, err
		}
		return snapshotCacheLoadResult{Entry: c.Set(key, payload), Hit: false}, nil
	})
	if err != nil {
		return snapshotCacheEntry{}, false, err
	}
	result, ok := value.(snapshotCacheLoadResult)
	if !ok {
		return snapshotCacheEntry{}, false, nil
	}
	return result.Entry, result.Hit, nil
}

// GetOrLoadContext lets each waiter stop waiting on its own request context
// without cancelling a shared singleflight load needed by other callers.
func (c *snapshotCache) GetOrLoadContext(ctx context.Context, key string, load func() (any, error)) (snapshotCacheEntry, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if load == nil {
		return snapshotCacheEntry{}, false, nil
	}
	if entry, ok := c.Get(key); ok {
		return entry, true, nil
	}
	if c == nil || key == "" {
		payload, err := load()
		if err != nil {
			return snapshotCacheEntry{}, false, err
		}
		return c.Set(key, payload), false, nil
	}

	resultCh := c.sf.DoChan(key, func() (any, error) {
		if entry, ok := c.Get(key); ok {
			return snapshotCacheLoadResult{Entry: entry, Hit: true}, nil
		}
		payload, err := load()
		if err != nil {
			return nil, err
		}
		return snapshotCacheLoadResult{Entry: c.Set(key, payload), Hit: false}, nil
	})

	select {
	case <-ctx.Done():
		return snapshotCacheEntry{}, false, ctx.Err()
	case flightResult := <-resultCh:
		if flightResult.Err != nil {
			return snapshotCacheEntry{}, false, flightResult.Err
		}
		result, ok := flightResult.Val.(snapshotCacheLoadResult)
		if !ok {
			return snapshotCacheEntry{}, false, nil
		}
		return result.Entry, result.Hit, nil
	}
}

func buildETagFromAny(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "\"" + hex.EncodeToString(sum[:]) + "\""
}

func parseBoolQueryWithDefault(raw string, def bool) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return def
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
