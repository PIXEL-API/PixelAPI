package service

import (
	"context"
	"time"
)

// ClusterCachePublisher is the narrow cache-invalidation notification port
// required by business writes. PostgreSQL remains authoritative; publishing is
// only the cross-node wake-up path.
type ClusterCachePublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// ClusterCacheSubscription represents one cluster notification subscription.
type ClusterCacheSubscription interface {
	Receive(ctx context.Context) error
	Close() error
}

// ClusterRedisPoolStats contains only the connection metrics persisted in a
// cluster heartbeat, without exposing a Redis client type to the service layer.
type ClusterRedisPoolStats struct {
	TotalConnections uint32
	IdleConnections  uint32
}

// ClusterRedisPort contains the Redis operations owned by the cluster runtime.
// It intentionally excludes generic key/value commands.
type ClusterRedisPort interface {
	ClusterCachePublisher
	Subscribe(ctx context.Context, topic string) ClusterCacheSubscription
	Ping(ctx context.Context) error
	PoolStats() ClusterRedisPoolStats
}

// EphemeralStateStore owns short-lived state that must be consumed at most
// once. Take must atomically read and delete the value.
type EphemeralStateStore interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Take(ctx context.Context, key string) (value []byte, found bool, err error)
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Delete(ctx context.Context, key string) error
}

// OIDCProviderStateStore is kept as a semantic alias for the built-in OIDC
// provider while Redis infrastructure is shared with other single-use flows.
type OIDCProviderStateStore = EphemeralStateStore
