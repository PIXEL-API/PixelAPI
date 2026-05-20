package agent

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const credentialCacheCleanupInterval = 30 * time.Second

type CredentialCache struct {
	mu      sync.RWMutex
	entries map[string]credentialCacheEntry
	maxTTL  time.Duration
}

type credentialCacheEntry struct {
	authorization *service.AuthorizeSubsiteResponse
	expiresAt     time.Time
}

func NewCredentialCache(maxTTL time.Duration) *CredentialCache {
	if maxTTL <= 0 {
		maxTTL = 2 * time.Minute
	}
	return &CredentialCache{
		entries: make(map[string]credentialCacheEntry),
		maxTTL:  maxTTL,
	}
}

func (c *CredentialCache) Set(authorization *service.AuthorizeSubsiteResponse) {
	if c == nil || authorization == nil || authorization.RequestID == "" {
		return
	}
	now := time.Now()
	expiresAt := now.Add(c.maxTTL)
	if !authorization.ExpiresAt.IsZero() && authorization.ExpiresAt.Before(expiresAt) {
		expiresAt = authorization.ExpiresAt
	}
	if !authorization.Credential.ExpiresAt.IsZero() && authorization.Credential.ExpiresAt.Before(expiresAt) {
		expiresAt = authorization.Credential.ExpiresAt
	}
	if !expiresAt.After(now) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[authorization.RequestID] = credentialCacheEntry{
		authorization: cloneAuthorization(authorization),
		expiresAt:     expiresAt,
	}
}

func (c *CredentialCache) Get(requestID string) (*service.AuthorizeSubsiteResponse, bool) {
	if c == nil || requestID == "" {
		return nil, false
	}
	now := time.Now()
	c.mu.RLock()
	entry, ok := c.entries[requestID]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.After(now) {
		c.Delete(requestID)
		return nil, false
	}
	return cloneAuthorization(entry.authorization), true
}

func (c *CredentialCache) Delete(requestID string) {
	if c == nil || requestID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, requestID)
}

func (c *CredentialCache) ActiveCount() int {
	if c == nil {
		return 0
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	active := 0
	for requestID, entry := range c.entries {
		if entry.expiresAt.After(now) {
			active++
			continue
		}
		delete(c.entries, requestID)
	}
	return active
}

func (c *CredentialCache) StartCleanup(ctx context.Context) {
	if c == nil {
		return
	}
	ticker := time.NewTicker(credentialCacheCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanupExpired(time.Now())
		}
	}
}

func (c *CredentialCache) cleanupExpired(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for requestID, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			delete(c.entries, requestID)
		}
	}
}

func cloneAuthorization(input *service.AuthorizeSubsiteResponse) *service.AuthorizeSubsiteResponse {
	if input == nil {
		return nil
	}
	clone := *input
	clone.Credential.Credentials = cloneMap(input.Credential.Credentials)
	clone.Credential.Extra = cloneMap(input.Credential.Extra)
	return &clone
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
