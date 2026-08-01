package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newNoAccountBackoffCacheTest(t *testing.T, cfg config.NoAccountBackoffConfig) (*noAccountBackoffCacheImpl, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	full := &config.Config{}
	full.RateLimit.NoAccountBackoff = cfg
	limiter := NewNoAccountBackoffCache(client, full)
	impl, ok := limiter.(*noAccountBackoffCacheImpl)
	require.True(t, ok)
	return impl, server
}

func TestNoAccountBackoffThresholdArmsBlock(t *testing.T) {
	limiter, _ := newNoAccountBackoffCacheTest(t, config.NoAccountBackoffConfig{
		Enabled:        true,
		WindowSeconds:  60,
		Threshold:      3,
		BackoffSeconds: 60,
	})
	ctx := context.Background()
	groupID := int64(7)

	// 阈值前不触发,恰好达到阈值的那次调用返回 blocked=true
	blocked, _ := limiter.RecordFailure(ctx, 100, &groupID)
	require.False(t, blocked)
	blocked, _ = limiter.RecordFailure(ctx, 100, &groupID)
	require.False(t, blocked)
	blocked, retryAfter := limiter.RecordFailure(ctx, 100, &groupID)
	require.True(t, blocked)
	require.Equal(t, 60, retryAfter)

	blocked, retryAfter = limiter.CheckBlocked(ctx, 100, &groupID)
	require.True(t, blocked)
	require.Greater(t, retryAfter, 0)
	require.LessOrEqual(t, retryAfter, 60)

	// 其他 (user, group) 组合互不影响
	blocked, _ = limiter.CheckBlocked(ctx, 101, &groupID)
	require.False(t, blocked)
	blocked, _ = limiter.CheckBlocked(ctx, 100, nil)
	require.False(t, blocked)
}

func TestNoAccountBackoffBlockExpiresAndCounterResets(t *testing.T) {
	limiter, server := newNoAccountBackoffCacheTest(t, config.NoAccountBackoffConfig{
		Enabled:        true,
		WindowSeconds:  60,
		Threshold:      2,
		BackoffSeconds: 30,
	})
	ctx := context.Background()

	limiter.RecordFailure(ctx, 200, nil)
	blocked, _ := limiter.RecordFailure(ctx, 200, nil)
	require.True(t, blocked)

	// 退避到期后放行;计数键在跨阈值时已删除,不会立刻再次触发
	server.FastForward(31 * time.Second)
	blocked, _ = limiter.CheckBlocked(ctx, 200, nil)
	require.False(t, blocked)
	blocked, _ = limiter.RecordFailure(ctx, 200, nil)
	require.False(t, blocked)
}

func TestNoAccountBackoffFailsOpenOnRedisError(t *testing.T) {
	limiter, server := newNoAccountBackoffCacheTest(t, config.NoAccountBackoffConfig{
		Enabled:        true,
		WindowSeconds:  60,
		Threshold:      1,
		BackoffSeconds: 60,
	})
	ctx := context.Background()
	server.Close()

	blocked, retryAfter := limiter.CheckBlocked(ctx, 300, nil)
	require.False(t, blocked)
	require.Zero(t, retryAfter)
	blocked, retryAfter = limiter.RecordFailure(ctx, 300, nil)
	require.False(t, blocked)
	require.Zero(t, retryAfter)
}
