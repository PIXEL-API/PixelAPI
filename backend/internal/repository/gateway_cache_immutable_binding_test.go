package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheSessionAccountIDErrorContract(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()

	accountID, err := cache.GetSessionAccountID(ctx, 1, "missing-session")
	require.Zero(t, accountID)
	require.ErrorIs(t, err, service.ErrGatewaySessionStringNotFound)
	require.ErrorIs(t, err, redis.Nil)

	require.NoError(t, client.Set(ctx, buildSessionKey(1, "corrupted-session"), "not-an-account-id", time.Minute).Err())
	_, err = cache.GetSessionAccountID(ctx, 1, "corrupted-session")
	require.Error(t, err)
	require.NotErrorIs(t, err, service.ErrGatewaySessionStringNotFound)
	require.NotErrorIs(t, err, redis.Nil)

	server.SetError("ERR cache unavailable")
	_, err = cache.GetSessionAccountID(ctx, 1, "missing-session")
	require.ErrorContains(t, err, "cache unavailable")
	require.NotErrorIs(t, err, service.ErrGatewaySessionStringNotFound)
	require.NotErrorIs(t, err, redis.Nil)
}

func TestGatewayCacheBindSessionStringImmutable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()

	stored, err := cache.BindSessionStringImmutable(ctx, 0, "owner-key", "owner-a", time.Minute)
	require.NoError(t, err)
	require.Equal(t, "owner-a", stored)

	server.FastForward(30 * time.Second)
	stored, err = cache.BindSessionStringImmutable(ctx, 0, "owner-key", "owner-a", time.Minute)
	require.NoError(t, err)
	require.Equal(t, "owner-a", stored, "same owner must be idempotent")
	require.Greater(t, server.TTL(buildSessionKey(0, "owner-key")), 50*time.Second, "idempotent bind must refresh TTL")

	stored, err = cache.BindSessionStringImmutable(ctx, 0, "owner-key", "owner-b", time.Minute)
	require.NoError(t, err)
	require.Equal(t, "owner-a", stored, "a conflicting owner must be returned without overwrite")
	value, err := client.Get(ctx, buildSessionKey(0, "owner-key")).Result()
	require.NoError(t, err)
	require.Equal(t, "owner-a", value)
}
