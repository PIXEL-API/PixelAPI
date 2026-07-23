package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/redis/go-redis/v9"
)

// ClusterCacheCoordinator advances the PostgreSQL-authoritative generation
// after a business write and uses Redis only to wake other nodes quickly.
type ClusterCacheCoordinator struct {
	enabled      bool
	deploymentID string
	nodeID       string
	repository   ClusterRepository
	redis        *redis.Client
	topic        string
	healthy      atomic.Bool
	pendingMu    sync.Mutex
	pending      map[string]struct{}
	lastError    string
}

func NewClusterCacheCoordinator(
	cfg *config.Config,
	repository ClusterRepository,
	redisClient *redis.Client,
) *ClusterCacheCoordinator {
	coordinator := &ClusterCacheCoordinator{
		repository: repository,
		redis:      redisClient,
		pending:    make(map[string]struct{}, 3),
	}
	coordinator.healthy.Store(true)
	if cfg == nil || !cfg.Cluster.Enabled {
		return coordinator
	}
	coordinator.enabled = true
	coordinator.deploymentID = cfg.Cluster.DeploymentID
	coordinator.nodeID = cfg.Cluster.NodeID
	coordinator.topic = "sub2api:cluster:" + cfg.Cluster.DeploymentID + ":cache-versions"
	return coordinator
}

func (c *ClusterCacheCoordinator) Advance(ctx context.Context, cacheKey string) error {
	if c == nil || !c.enabled {
		return nil
	}
	if c.repository == nil || c.redis == nil {
		err := fmt.Errorf("cluster cache coordinator is unavailable")
		c.markPending(cacheKey, err)
		return err
	}
	version, err := c.repository.BumpCacheVersion(ctx, c.deploymentID, cacheKey, c.nodeID)
	if err != nil {
		wrapped := fmt.Errorf("advance %s cache version: %w", cacheKey, err)
		c.markPending(cacheKey, wrapped)
		return wrapped
	}
	c.clearPending(cacheKey)
	payload, err := json.Marshal(clusterCacheNotification{
		CacheKey: version.CacheKey,
		Version:  version.Version,
		NodeID:   c.nodeID,
	})
	if err != nil {
		return fmt.Errorf("encode %s cache notification: %w", cacheKey, err)
	}
	if err := c.redis.Publish(ctx, c.topic, payload).Err(); err != nil {
		// PostgreSQL already contains the authoritative version. Periodic
		// reconciliation is reliable, so Pub/Sub failure is observable but does
		// not make the write inconsistent.
		slog.Warn("cluster cache notification publish failed",
			"cache_key", cacheKey,
			"version", version.Version,
			"error", err,
		)
	}
	return nil
}

func (c *ClusterCacheCoordinator) Healthy() bool {
	return c == nil || !c.enabled || c.healthy.Load()
}

func (c *ClusterCacheCoordinator) LastError() string {
	if c == nil {
		return ""
	}
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	return c.lastError
}

// RetryPending durably advances any generation whose original business write
// committed but whose version bump failed. It is safe for periodic heartbeat
// execution; an extra generation increment only causes a harmless reload.
func (c *ClusterCacheCoordinator) RetryPending(ctx context.Context) error {
	if c == nil || !c.enabled {
		return nil
	}
	c.pendingMu.Lock()
	keys := make([]string, 0, len(c.pending))
	for key := range c.pending {
		keys = append(keys, key)
	}
	c.pendingMu.Unlock()
	for _, key := range keys {
		if err := c.Advance(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (c *ClusterCacheCoordinator) markPending(cacheKey string, err error) {
	c.pendingMu.Lock()
	c.pending[cacheKey] = struct{}{}
	c.lastError = err.Error()
	c.pendingMu.Unlock()
	c.healthy.Store(false)
}

func (c *ClusterCacheCoordinator) clearPending(cacheKey string) {
	c.pendingMu.Lock()
	delete(c.pending, cacheKey)
	if len(c.pending) == 0 {
		c.lastError = ""
		c.healthy.Store(true)
	}
	c.pendingMu.Unlock()
}
