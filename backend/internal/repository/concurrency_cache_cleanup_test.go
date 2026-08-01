package repository

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// 单趟 SCAN 清理必须只触碰白名单前缀的槽位有序集合：
// concurrency:wait:* 是 string 计数器，被 ZSET 脚本触碰会 WRONGTYPE 炸整批 pipeline；
// concurrency:openai_ws_ingress:* 是 60s 租约键，被触碰会按槽位 TTL 误删成员并拉长 EXPIRE。
func TestCleanupExpiredSlotsSingleScanCleansOnlyWhitelistedSlotKeys(t *testing.T) {
	slotTTL := 15 * time.Minute
	cache, _ := newRuntimeLeaseCacheTest(t, slotTTL)
	ctx := context.Background()
	rdb := cache.rdb

	now, err := cache.redisUnixTime(ctx)
	require.NoError(t, err)
	expired := now - int64(slotTTL/time.Second) - 1

	accountKey := accountSlotKey(1)
	userKey := userSlotKey(2)
	membershipKey := accountShareMembershipSlotKey(3)
	apiKeyKey := apiKeySlotKey(5)
	ingressKey := openAIWSIngressLeaseKey(7)
	waitKey := waitQueueKey(9)

	require.NoError(t, rdb.ZAdd(ctx, accountKey,
		redis.Z{Score: float64(expired), Member: "expired"},
		redis.Z{Score: float64(now), Member: "active"},
	).Err())
	// 仅含过期成员的槽位键应被整体删除
	require.NoError(t, rdb.ZAdd(ctx, userKey, redis.Z{Score: float64(expired), Member: "expired"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, membershipKey,
		redis.Z{Score: float64(expired), Member: "expired"},
		redis.Z{Score: float64(now), Member: "active"},
	).Err())
	// 白名单之外的 concurrency:* 键，即使成员分数已"过期"也不得被触碰
	require.NoError(t, rdb.ZAdd(ctx, apiKeyKey, redis.Z{Score: float64(expired), Member: "expired"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, ingressKey, redis.Z{Score: float64(expired), Member: "stale-lease"}).Err())
	require.NoError(t, rdb.Expire(ctx, ingressKey, openAIWSIngressLeaseTTLSeconds*time.Second).Err())
	require.NoError(t, rdb.Set(ctx, waitKey, 3, time.Minute).Err())

	require.NoError(t, cache.CleanupExpiredSlots(ctx))

	accountMembers, err := rdb.ZRange(ctx, accountKey, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"active"}, accountMembers)

	userExists, err := rdb.Exists(ctx, userKey).Result()
	require.NoError(t, err)
	require.EqualValues(t, 0, userExists, "只剩过期成员的槽位键应被删除")

	membershipMembers, err := rdb.ZRange(ctx, membershipKey, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"active"}, membershipMembers)

	apiKeyMembers, err := rdb.ZRange(ctx, apiKeyKey, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"expired"}, apiKeyMembers, "api_key 槽位不在白名单内，不应被清理")

	ingressMembers, err := rdb.ZRange(ctx, ingressKey, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"stale-lease"}, ingressMembers, "ingress 租约成员不得被清理脚本删除")
	ingressTTL, err := rdb.TTL(ctx, ingressKey).Result()
	require.NoError(t, err)
	require.Greater(t, ingressTTL, time.Duration(0))
	require.LessOrEqual(t, ingressTTL, openAIWSIngressLeaseTTLSeconds*time.Second, "ingress 租约 TTL 不应被拉长为槽位 TTL")

	waitVal, err := rdb.Get(ctx, waitKey).Int()
	require.NoError(t, err)
	require.Equal(t, 3, waitVal)
	waitTTL, err := rdb.TTL(ctx, waitKey).Result()
	require.NoError(t, err)
	require.Greater(t, waitTTL, time.Duration(0))
	require.LessOrEqual(t, waitTTL, time.Minute, "wait 计数器 TTL 不应被改写")
}

func TestIsCleanableSlotKey(t *testing.T) {
	cleanable := []string{
		accountSlotKey(1),
		userSlotKey(2),
		accountShareMembershipSlotKey(3),
	}
	for _, key := range cleanable {
		require.True(t, isCleanableSlotKey(key), "key %s 应可清理", key)
	}
	untouchable := []string{
		waitQueueKey(9),
		openAIWSIngressLeaseKey(7),
		apiKeySlotKey(5),
		accountWaitKey(4),
		"concurrency:other:1",
	}
	for _, key := range untouchable {
		require.False(t, isCleanableSlotKey(key), "key %s 不得清理", key)
	}
}
