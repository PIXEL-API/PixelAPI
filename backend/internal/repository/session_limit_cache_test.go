package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionLimitCacheCanRegisterDoesNotReserveOrRefreshSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	cache := NewSessionLimitCache(client, 5)
	ctx := context.Background()
	const accountID int64 = 71
	const maxSessions = 1
	idleTimeout := time.Minute
	key := sessionLimitKey(accountID)

	allowed, err := cache.CanRegisterSession(ctx, accountID, "candidate", maxSessions, idleTimeout)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, int64(0), client.ZCard(ctx, key).Val(), "precheck must not create a ghost session")

	allowed, err = cache.RegisterSession(ctx, accountID, "active", maxSessions, idleTimeout)
	require.NoError(t, err)
	require.True(t, allowed)
	scoreBefore, err := client.ZScore(ctx, key, "active").Result()
	require.NoError(t, err)

	server.FastForward(10 * time.Second)
	allowed, err = cache.CanRegisterSession(ctx, accountID, "active", maxSessions, idleTimeout)
	require.NoError(t, err)
	require.True(t, allowed, "an existing session remains eligible")
	scoreAfter, err := client.ZScore(ctx, key, "active").Result()
	require.NoError(t, err)
	require.Equal(t, scoreBefore, scoreAfter, "precheck must not refresh an existing session")

	allowed, err = cache.CanRegisterSession(ctx, accountID, "other", maxSessions, idleTimeout)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, int64(1), client.ZCard(ctx, key).Val())
}
