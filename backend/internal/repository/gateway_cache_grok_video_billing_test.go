package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheGrokVideoBillingState(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()

	pending, err := cache.GetGrokVideoPendingBilling(ctx, "u1:k1:task-1")
	require.NoError(t, err)
	require.Nil(t, pending)

	require.NoError(t, cache.SetGrokVideoPendingBilling(ctx, "u1:k1:task-1", []byte(`{"model":"grok-imagine-video"}`), 24*time.Hour))
	pending, err = cache.GetGrokVideoPendingBilling(ctx, "u1:k1:task-1")
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"grok-imagine-video"}`, string(pending))

	claimed, err := cache.ClaimGrokVideoBilled(ctx, "u1:k1:task-1", 48*time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.ClaimGrokVideoBilled(ctx, "u1:k1:task-1", 48*time.Hour)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, cache.ReleaseGrokVideoBilled(ctx, "u1:k1:task-1"))
	claimed, err = cache.ClaimGrokVideoBilled(ctx, "u1:k1:task-1", 48*time.Hour)
	require.NoError(t, err)
	require.True(t, claimed)
}

func TestGatewayCacheGrokVideoBillingRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	var nilCache *gatewayCache
	require.Error(t, nilCache.SetGrokVideoPendingBilling(ctx, "key", []byte("{}"), time.Hour))
	require.Error(t, nilCache.ReleaseGrokVideoBilled(ctx, "key"))

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	cache := &gatewayCache{rdb: client}

	require.Error(t, cache.SetGrokVideoPendingBilling(ctx, "", []byte("{}"), time.Hour))
	require.Error(t, cache.SetGrokVideoPendingBilling(ctx, "key", nil, time.Hour))
	require.Error(t, cache.SetGrokVideoPendingBilling(ctx, "key", []byte("{}"), 0))
	_, err := cache.GetGrokVideoPendingBilling(ctx, "")
	require.Error(t, err)
	_, err = cache.ClaimGrokVideoBilled(ctx, "", time.Hour)
	require.Error(t, err)
	_, err = cache.ClaimGrokVideoBilled(ctx, "key", 0)
	require.Error(t, err)
	require.Error(t, cache.ReleaseGrokVideoBilled(ctx, ""))
}
