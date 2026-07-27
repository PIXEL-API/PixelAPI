package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRuntimeLeaseCacheTest(t *testing.T, ttl time.Duration) (*concurrencyCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return &concurrencyCache{
		rdb:                 client,
		slotTTLSeconds:      int(ttl.Seconds()),
		waitQueueTTLSeconds: int(ttl.Seconds()),
	}, server
}

func TestConcurrencyCacheRuntimeLeaseRefresh(t *testing.T) {
	ctx := context.Background()
	slotTTL := 9 * time.Second

	tests := []struct {
		name    string
		acquire func(*concurrencyCache, string) (bool, error)
		refresh func(*concurrencyCache, string) (bool, error)
		key     string
	}{
		{
			name: "account",
			acquire: func(cache *concurrencyCache, requestID string) (bool, error) {
				return cache.AcquireAccountSlot(ctx, 71, 1, requestID)
			},
			refresh: func(cache *concurrencyCache, requestID string) (bool, error) {
				return cache.RefreshAccountSlot(ctx, 71, requestID)
			},
			key: accountSlotKey(71),
		},
		{
			name: "membership",
			acquire: func(cache *concurrencyCache, requestID string) (bool, error) {
				return cache.AcquireAccountShareMembershipSlot(ctx, 81, 1, requestID)
			},
			refresh: func(cache *concurrencyCache, requestID string) (bool, error) {
				return cache.RefreshAccountShareMembershipSlot(ctx, 81, requestID)
			},
			key: accountShareMembershipSlotKey(81),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, server := newRuntimeLeaseCacheTest(t, slotTTL)
			const requestID = "runtime-lease-owner"

			acquired, err := tt.acquire(cache, requestID)
			require.NoError(t, err)
			require.True(t, acquired)

			server.FastForward(6 * time.Second)
			owned, err := tt.refresh(cache, requestID)
			require.NoError(t, err)
			require.True(t, owned)

			// The original lease would be stale after twelve seconds. A refresh
			// at six seconds must keep both the ZSET member and key alive.
			server.FastForward(6 * time.Second)
			score, err := cache.rdb.ZScore(ctx, tt.key, requestID).Result()
			require.NoError(t, err)
			require.NotZero(t, score)
			require.Equal(t, int64(1), cache.rdb.ZCard(ctx, tt.key).Val())
		})
	}
}

func TestConcurrencyCacheRuntimeLeaseRefreshDoesNotRecreateMissingSlot(t *testing.T) {
	ctx := context.Background()
	cache, _ := newRuntimeLeaseCacheTest(t, 9*time.Second)

	owned, err := cache.RefreshAccountSlot(ctx, 72, "missing-account-slot")
	require.NoError(t, err)
	require.False(t, owned)
	require.Equal(t, int64(0), cache.rdb.ZCard(ctx, accountSlotKey(72)).Val())

	owned, err = cache.RefreshAccountShareMembershipSlot(ctx, 82, "missing-membership-slot")
	require.NoError(t, err)
	require.False(t, owned)
	require.Equal(t, int64(0), cache.rdb.ZCard(ctx, accountShareMembershipSlotKey(82)).Val())
}
