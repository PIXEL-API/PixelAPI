package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type clusterRedisAdapter struct {
	client *redis.Client
}

func NewClusterRedisPort(client *redis.Client) service.ClusterRedisPort {
	return &clusterRedisAdapter{client: client}
}

func NewClusterCachePublisher(client *redis.Client) service.ClusterCachePublisher {
	return &clusterRedisAdapter{client: client}
}

func (a *clusterRedisAdapter) Publish(ctx context.Context, topic string, payload []byte) error {
	return a.client.Publish(ctx, topic, payload).Err()
}

func (a *clusterRedisAdapter) Subscribe(ctx context.Context, topic string) service.ClusterCacheSubscription {
	return &clusterRedisSubscription{pubsub: a.client.Subscribe(ctx, topic)}
}

func (a *clusterRedisAdapter) Ping(ctx context.Context) error {
	return a.client.Ping(ctx).Err()
}

func (a *clusterRedisAdapter) PoolStats() service.ClusterRedisPoolStats {
	stats := a.client.PoolStats()
	return service.ClusterRedisPoolStats{
		TotalConnections: stats.TotalConns,
		IdleConnections:  stats.IdleConns,
	}
}

type clusterRedisSubscription struct {
	pubsub *redis.PubSub
}

func (s *clusterRedisSubscription) Receive(ctx context.Context) error {
	_, err := s.pubsub.ReceiveMessage(ctx)
	return err
}

func (s *clusterRedisSubscription) Close() error {
	return s.pubsub.Close()
}

type ephemeralRedisStateStore struct {
	client *redis.Client
}

func NewEphemeralStateStore(client *redis.Client) service.EphemeralStateStore {
	return &ephemeralRedisStateStore{client: client}
}

func (s *ephemeralRedisStateStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *ephemeralRedisStateStore) Take(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.GetDel(ctx, key).Bytes()
	return redisStateResult(value, err)
}

func (s *ephemeralRedisStateStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.Get(ctx, key).Bytes()
	return redisStateResult(value, err)
}

func (s *ephemeralRedisStateStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func redisStateResult(value []byte, err error) ([]byte, bool, error) {
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}
