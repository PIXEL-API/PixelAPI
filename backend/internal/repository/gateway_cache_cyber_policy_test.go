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

func TestCyberPolicyIsolationRecordHitSequenceAndPairIsolation(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	first, err := cache.RecordHit(ctx, 10, 1215, "session-a", "attempt-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), first.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeSession, first.Action)
	require.WithinDuration(t, now.Add(5*time.Minute), first.BlockedUntil, time.Millisecond)

	state, err := cache.CheckBlock(ctx, 10, 1215, "session-a")
	require.NoError(t, err)
	require.True(t, state.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeSession, state.Scope)
	require.InDelta(t, (5 * time.Minute).Milliseconds(), state.RetryAfter.Milliseconds(), 2)

	otherSession, err := cache.CheckBlock(ctx, 10, 1215, "session-b")
	require.NoError(t, err)
	require.False(t, otherSession.Blocked)
	otherGroup, err := cache.CheckBlock(ctx, 10, 18, "session-a")
	require.NoError(t, err)
	require.False(t, otherGroup.Blocked)
	otherAPIKey, err := cache.CheckBlock(ctx, 11, 1215, "session-a")
	require.NoError(t, err)
	require.False(t, otherAPIKey.Blocked)

	second, err := cache.RecordHit(ctx, 10, 1215, "session-a", "attempt-2")
	require.NoError(t, err)
	require.Equal(t, int64(2), second.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeSession, second.Action)
	require.WithinDuration(t, now.Add(15*time.Minute), second.BlockedUntil, time.Millisecond)

	third, err := cache.RecordHit(ctx, 10, 1215, "session-b", "attempt-3")
	require.NoError(t, err)
	require.Equal(t, int64(3), third.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupDay, third.Action)
	require.Equal(t, time.Date(2026, 8, 11, 0, 0, 0, 0, loc), third.BlockedUntil)

	groupState, err := cache.CheckBlock(ctx, 10, 1215, "unrelated-session")
	require.NoError(t, err)
	require.True(t, groupState.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupDay, groupState.Scope)
}

func TestCyberPolicyIsolationMissingSessionFallsBackToPairShortBlock(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	first, err := cache.RecordHit(ctx, 20, 18, "", "fallback-attempt-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), first.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupShort, first.Action)
	require.WithinDuration(t, now.Add(5*time.Minute), first.BlockedUntil, time.Millisecond)

	state, err := cache.CheckBlock(ctx, 20, 18, "any-later-session")
	require.NoError(t, err)
	require.True(t, state.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupShort, state.Scope)

	second, err := cache.RecordHit(ctx, 20, 18, "", "fallback-attempt-2")
	require.NoError(t, err)
	require.Equal(t, int64(2), second.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupShort, second.Action)
	require.WithinDuration(t, now.Add(15*time.Minute), second.BlockedUntil, time.Millisecond)

	unrelated, err := cache.CheckBlock(ctx, 20, 1198, "any-later-session")
	require.NoError(t, err)
	require.False(t, unrelated.Blocked)
}

func TestCyberPolicyIsolationUpstreamAttemptIsIdempotent(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	first, err := cache.RecordHit(ctx, 30, 71872, "session-a", "same-attempt")
	require.NoError(t, err)
	require.False(t, first.Duplicate)

	duplicate, err := cache.RecordHit(ctx, 30, 71872, "session-a", "same-attempt")
	require.NoError(t, err)
	require.True(t, duplicate.Duplicate)
	require.Equal(t, first.HitSequence, duplicate.HitSequence)
	require.Equal(t, first.Action, duplicate.Action)
	require.Equal(t, first.BlockedUntil, duplicate.BlockedUntil)

	second, err := cache.RecordHit(ctx, 30, 71872, "session-a", "new-attempt")
	require.NoError(t, err)
	require.Equal(t, int64(2), second.HitSequence)
}

func TestCyberPolicyIsolationNaturalDayAndShortBlockAcrossMidnight(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 23, 59, 0, 0, loc)
	cache, server := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	first, err := cache.RecordHit(ctx, 40, 61711, "", "late-attempt-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), first.HitSequence)
	require.WithinDuration(t, time.Date(2026, 8, 11, 0, 4, 0, 0, loc), first.BlockedUntil, time.Millisecond)

	second, err := cache.RecordHit(ctx, 40, 61711, "", "late-attempt-2")
	require.NoError(t, err)
	require.Equal(t, int64(2), second.HitSequence)
	require.WithinDuration(t, time.Date(2026, 8, 11, 0, 14, 0, 0, loc), second.BlockedUntil, time.Millisecond)

	third, err := cache.RecordHit(ctx, 40, 61711, "", "late-attempt-3")
	require.NoError(t, err)
	require.Equal(t, int64(3), third.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupDay, third.Action)
	require.Equal(t, time.Date(2026, 8, 11, 0, 0, 0, 0, loc), third.BlockedUntil)

	beforeMidnight, err := cache.CheckBlock(ctx, 40, 61711, "")
	require.NoError(t, err)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupDay, beforeMidnight.Scope)

	server.FastForward(2 * time.Minute)
	afterMidnight, err := cache.CheckBlock(ctx, 40, 61711, "")
	require.NoError(t, err)
	require.True(t, afterMidnight.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupShort, afterMidnight.Scope)

	newDay, err := cache.RecordHit(ctx, 40, 61711, "", "next-day-attempt-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), newDay.HitSequence)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupShort, newDay.Action)
	// The new day's five-minute action must not shorten the previous day's
	// still-active fifteen-minute fallback.
	require.Equal(t, second.BlockedUntil, newDay.BlockedUntil)
}

func TestCyberPolicyIsolationConcurrentHitsAreAtomic(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 14, 0, 0, 0, loc)
	cache, _ := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	const hits = 6
	type hitResult struct {
		sequence int64
		err      error
	}
	results := make(chan hitResult, hits)
	sequences := make([]int64, 0, hits)
	for i := 0; i < hits; i++ {
		go func(attempt int) {
			decision, err := cache.RecordHit(ctx, 50, 1198, "session", "concurrent-"+time.Duration(attempt).String())
			results <- hitResult{sequence: decision.HitSequence, err: err}
		}(i)
	}
	for i := 0; i < hits; i++ {
		result := <-results
		require.NoError(t, result.err)
		sequences = append(sequences, result.sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6}, sequences)

	state, err := cache.CheckBlock(ctx, 50, 1198, "another-session")
	require.NoError(t, err)
	require.True(t, state.Blocked)
	require.Equal(t, service.CyberPolicyBlockScopeAPIKeyGroupDay, state.Scope)
}

func TestCyberPolicyIsolationRepairsDailyCounterTTL(t *testing.T) {
	loc := timezone.Location()
	now := time.Date(2026, 8, 10, 16, 0, 0, 0, loc)
	cache, server := newCyberPolicyIsolationTestCache(t, now)
	ctx := context.Background()

	_, err := cache.RecordHit(ctx, 60, 1215, "session", "repair-attempt-1")
	require.NoError(t, err)
	businessDate, resetAt := cyberPolicyBusinessWindow(now)
	keys := buildCyberPolicyIsolationKeys(60, 1215, businessDate, "session", "")
	server.SetTTL(keys.count, 0)
	require.Equal(t, time.Duration(0), server.TTL(keys.count))

	_, err = cache.RecordHit(ctx, 60, 1215, "session", "repair-attempt-2")
	require.NoError(t, err)
	require.InDelta(t, resetAt.Sub(now).Milliseconds(), server.TTL(keys.count).Milliseconds(), 2)
}

func TestCyberPolicyIsolationRejectsMissingAttemptID(t *testing.T) {
	loc := timezone.Location()
	cache, _ := newCyberPolicyIsolationTestCache(t, time.Date(2026, 8, 10, 12, 0, 0, 0, loc))

	_, err := cache.RecordHit(context.Background(), 70, 1215, "session", "  ")
	require.Error(t, err)
}
