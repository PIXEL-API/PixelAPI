package repository

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCyberPolicyIsolationTestCache(t *testing.T, now time.Time) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return &gatewayCache{rdb: client}, server
}

func TestCyberPolicyIsolationFirstHitBlocksUserGroupForNaturalDay(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	first, err := cache.RecordHit(ctx, 10, 1215, "attempt-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), first.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, first.Action)
	require.False(t, first.Duplicate)
	require.Equal(t, time.Date(2026, 8, 11, 0, 0, 0, 0, loc), first.BlockedUntil)

	state, err := cache.CheckBlock(ctx, 10, 1215)
	require.NoError(t, err)
	require.True(t, state.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, state.Scope)
	require.InDelta(t, (12 * time.Hour).Milliseconds(), state.RetryAfter.Milliseconds(), 2)

	otherGroup, err := cache.CheckBlock(ctx, 10, 18)
	require.NoError(t, err)
	require.False(t, otherGroup.Blocked)
	otherUser, err := cache.CheckBlock(ctx, 11, 1215)
	require.NoError(t, err)
	require.False(t, otherUser.Blocked)
}

func TestCyberPolicyIsolationUpstreamAttemptIsIdempotent(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	first, err := cache.RecordHit(ctx, 30, 71872, "same-attempt")
	require.NoError(t, err)
	require.False(t, first.Duplicate)

	duplicate, err := cache.RecordHit(ctx, 30, 71872, "same-attempt")
	require.NoError(t, err)
	require.True(t, duplicate.Duplicate)
	require.Equal(t, first.HitSequence, duplicate.HitSequence)
	require.Equal(t, first.Action, duplicate.Action)
	require.Equal(t, first.BlockedUntil, duplicate.BlockedUntil)

	second, err := cache.RecordHit(ctx, 30, 71872, "new-attempt")
	require.NoError(t, err)
	require.Equal(t, int64(2), second.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, second.Action)
}

func TestCyberPolicyIsolationExpiresAtLocalMidnight(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 23, 59, 0, 0, loc)
	cache, server := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	decision, err := cache.RecordHit(ctx, 40, 61711, "late-attempt")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 11, 0, 0, 0, 0, loc), decision.BlockedUntil)

	beforeMidnight, err := cache.CheckBlock(ctx, 40, 61711)
	require.NoError(t, err)
	require.True(t, beforeMidnight.Blocked)

	server.FastForward(2 * time.Minute)
	server.SetTime(now.Add(2 * time.Minute))
	afterMidnight, err := cache.CheckBlock(ctx, 40, 61711)
	require.NoError(t, err)
	require.False(t, afterMidnight.Blocked)

	newDay, err := cache.RecordHit(ctx, 40, 61711, "next-day-attempt")
	require.NoError(t, err)
	require.Equal(t, int64(1), newDay.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, newDay.Action)
	require.Equal(t, time.Date(2026, 8, 12, 0, 0, 0, 0, loc), newDay.BlockedUntil)
}

func TestCyberPolicyIsolationConcurrentHitsAreAtomic(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 14, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	const hits = 6
	type hitResult struct {
		decision service.CyberPolicyHitDecision
		err      error
	}
	results := make(chan hitResult, hits)
	sequences := make([]int64, 0, hits)
	for i := 0; i < hits; i++ {
		go func(attempt int) {
			decision, err := cache.RecordHit(ctx, 50, 1198, "concurrent-"+time.Duration(attempt).String())
			results <- hitResult{decision: decision, err: err}
		}(i)
	}
	for i := 0; i < hits; i++ {
		result := <-results
		require.NoError(t, result.err)
		require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, result.decision.Action)
		sequences = append(sequences, result.decision.HitSequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6}, sequences)

	state, err := cache.CheckBlock(ctx, 50, 1198)
	require.NoError(t, err)
	require.True(t, state.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, state.Scope)
}

func TestCyberPolicyIsolationAdminClearAndNewHit(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 16, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	_, err := cache.RecordHit(ctx, 60, 1215, "attempt-1")
	require.NoError(t, err)
	removed, err := cache.ClearBlock(ctx, 60, 1215)
	require.NoError(t, err)
	require.True(t, removed)

	state, err := cache.CheckBlock(ctx, 60, 1215)
	require.NoError(t, err)
	require.False(t, state.Blocked)

	duplicate, err := cache.RecordHit(ctx, 60, 1215, "attempt-1")
	require.NoError(t, err)
	require.True(t, duplicate.Duplicate)
	state, err = cache.CheckBlock(ctx, 60, 1215)
	require.NoError(t, err)
	require.False(t, state.Blocked, "replaying the cleared attempt must not recreate the restriction")

	newHit, err := cache.RecordHit(ctx, 60, 1215, "attempt-2")
	require.NoError(t, err)
	require.Equal(t, int64(1), newHit.HitSequence)
	state, err = cache.CheckBlock(ctx, 60, 1215)
	require.NoError(t, err)
	require.True(t, state.Blocked, "a genuinely new hit must restrict the user again")

	removed, err = cache.ClearBlock(ctx, 60, 1215)
	require.NoError(t, err)
	require.True(t, removed)
	removed, err = cache.ClearBlock(ctx, 60, 1215)
	require.NoError(t, err)
	require.False(t, removed)
}

func TestCyberPolicyIsolationRepairsDailyCounterTTL(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 16, 0, 0, 0, loc)
	cache, server := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	_, err := cache.RecordHit(ctx, 70, 1215, "repair-attempt-1")
	require.NoError(t, err)
	businessDate, resetAt := cyberPolicyBusinessWindow(now)
	keys := buildCyberPolicyIsolationKeys(70, 1215, businessDate, "")
	server.SetTTL(keys.count, 0)
	require.Equal(t, time.Duration(0), server.TTL(keys.count))

	_, err = cache.RecordHit(ctx, 70, 1215, "repair-attempt-2")
	require.NoError(t, err)
	require.InDelta(t, resetAt.Sub(now).Milliseconds(), server.TTL(keys.count).Milliseconds(), 2)
}

func TestCyberPolicyIsolationRejectsInvalidInput(t *testing.T) {
	loc := timezone.Location()
	cache, _ := newCyberPolicyIsolationTestCache(t, time.Date(2026, 8, 10, 12, 0, 0, 0, loc))
	ctx := context.Background()

	_, err := cache.RecordHit(ctx, 70, 1215, "  ")
	require.Error(t, err)
	_, err = cache.RecordHit(ctx, 0, 1215, "attempt")
	require.Error(t, err)
	_, err = cache.CheckBlock(ctx, 70, 0)
	require.Error(t, err)
	_, err = cache.ClearBlock(ctx, -1, 1215)
	require.Error(t, err)
}
