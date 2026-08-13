//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheSnapshotUsesSlimMetadataButKeepsFullAccount(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewSchedulerCache(rdb)

	bucket := service.SchedulerBucket{GroupID: 2, Platform: service.PlatformGemini, Mode: service.SchedulerModeSingle}
	now := time.Now().UTC().Truncate(time.Second)
	limitReset := now.Add(10 * time.Minute)
	overloadUntil := now.Add(2 * time.Minute)
	tempUnschedUntil := now.Add(3 * time.Minute)
	windowEnd := now.Add(5 * time.Hour)

	account := service.Account{
		ID:          101,
		Name:        "gemini-heavy",
		Platform:    service.PlatformGemini,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 3,
		Priority:    7,
		LastUsedAt:  &now,
		Credentials: map[string]any{
			"api_key":       "gemini-api-key",
			"access_token":  "secret-access-token",
			"project_id":    "proj-1",
			"oauth_type":    "ai_studio",
			"model_mapping": map[string]any{"gemini-2.5-pro": "gemini-2.5-pro"},
			"huge_blob":     strings.Repeat("x", 4096),
		},
		Extra: map[string]any{
			"mixed_scheduling":             true,
			"window_cost_limit":            12.5,
			"window_cost_sticky_reserve":   8.0,
			"max_sessions":                 4,
			"session_idle_timeout_minutes": 11,
			"unused_large_field":           strings.Repeat("y", 4096),
		},
		RateLimitResetAt:       &limitReset,
		OverloadUntil:          &overloadUntil,
		TempUnschedulableUntil: &tempUnschedUntil,
		SessionWindowStart:     &now,
		SessionWindowEnd:       &windowEnd,
		SessionWindowStatus:    "active",
		GroupIDs:               []int64{bucket.GroupID},
		AccountGroups: []service.AccountGroup{
			{
				AccountID: 101,
				GroupID:   bucket.GroupID,
				Priority:  5,
				Group:     &service.Group{ID: bucket.GroupID, Name: "gemini-group"},
			},
		},
	}

	require.NoError(t, cache.SetSnapshot(ctx, bucket, []service.Account{account}))

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)

	got := snapshot[0]
	require.NotNil(t, got)
	require.Equal(t, "gemini-api-key", got.GetCredential("api_key"))
	require.Equal(t, "proj-1", got.GetCredential("project_id"))
	require.Equal(t, "ai_studio", got.GetCredential("oauth_type"))
	require.NotEmpty(t, got.GetModelMapping())
	require.Empty(t, got.GetCredential("access_token"))
	require.Empty(t, got.GetCredential("huge_blob"))
	require.Equal(t, true, got.Extra["mixed_scheduling"])
	require.Equal(t, 12.5, got.GetWindowCostLimit())
	require.Equal(t, 8.0, got.GetWindowCostStickyReserve())
	require.Equal(t, 4, got.GetMaxSessions())
	require.Equal(t, 11, got.GetSessionIdleTimeoutMinutes())
	require.Nil(t, got.Extra["unused_large_field"])
	require.Equal(t, []int64{bucket.GroupID}, got.GroupIDs)
	require.Len(t, got.AccountGroups, 1)
	require.Equal(t, account.ID, got.AccountGroups[0].AccountID)
	require.Equal(t, bucket.GroupID, got.AccountGroups[0].GroupID)
	require.Nil(t, got.AccountGroups[0].Group)

	full, err := cache.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, full)
	require.Equal(t, "secret-access-token", full.GetCredential("access_token"))
	require.Equal(t, strings.Repeat("x", 4096), full.GetCredential("huge_blob"))
	require.Len(t, full.AccountGroups, 1)
	require.NotNil(t, full.AccountGroups[0].Group)
}

func TestSchedulerCacheEmptySnapshotKeepsBucketRegistered(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewSchedulerCache(rdb)

	bucket := service.SchedulerBucket{GroupID: 77, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	account := service.Account{
		ID:          202,
		Name:        "openai-cache-entry",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Priority:    1,
		GroupIDs:    []int64{bucket.GroupID},
	}

	require.NoError(t, cache.SetSnapshot(ctx, bucket, []service.Account{account}))
	buckets, err := cache.ListBuckets(ctx)
	require.NoError(t, err)
	require.Contains(t, schedulerBucketStrings(buckets), bucket.String())

	require.NoError(t, cache.SetSnapshot(ctx, bucket, nil))

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Empty(t, snapshot)

	buckets, err = cache.ListBuckets(ctx)
	require.NoError(t, err)
	require.Contains(t, schedulerBucketStrings(buckets), bucket.String())

	exists, err := rdb.Exists(
		ctx,
		schedulerBucketKey(schedulerActivePrefix, bucket),
		schedulerBucketKey(schedulerReadyPrefix, bucket),
	).Result()
	require.NoError(t, err)
	require.Equal(t, int64(2), exists)

	isMember, err := rdb.SIsMember(ctx, schedulerBucketSetKey, bucket.String()).Result()
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestSchedulerCacheRetireAndReopenFencesOldEpochIntegration(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewSchedulerCache(rdb)
	lifecycleCache, ok := cache.(service.SchedulerLifecycleCache)
	require.True(t, ok)

	bucket := service.SchedulerBucket{GroupID: 78, Platform: service.PlatformAntigravity, Mode: service.SchedulerModeForced}
	account := service.Account{ID: 7801, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth}

	oldToken, err := lifecycleCache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, lifecycleCache.SetSnapshotFenced(ctx, bucket, oldToken, []service.Account{account}))
	require.NoError(t, lifecycleCache.RetireBucket(ctx, bucket))
	require.NoError(t, lifecycleCache.RetireBucket(ctx, bucket))

	_, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.False(t, hit)
	_, err = lifecycleCache.CaptureBucketWriteToken(ctx, bucket)
	require.ErrorIs(t, err, service.ErrSchedulerBucketRetired)
	require.ErrorIs(t, lifecycleCache.SetSnapshotFenced(ctx, bucket, oldToken, []service.Account{account}), service.ErrSchedulerBucketRetired)

	newToken, err := lifecycleCache.ReopenBucket(ctx, bucket)
	require.NoError(t, err)
	require.Greater(t, newToken.Epoch, oldToken.Epoch)
	require.ErrorIs(t, lifecycleCache.SetSnapshotFenced(ctx, bucket, oldToken, []service.Account{account}), service.ErrSchedulerBucketWriteFenced)
	require.NoError(t, lifecycleCache.SetSnapshotFenced(ctx, bucket, newToken, []service.Account{account}))

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)
	require.Equal(t, account.ID, snapshot[0].ID)
}

func TestSchedulerCacheGroupLifecycleLeaseOwnerAndTTLIntegration(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewSchedulerCache(rdb)
	lifecycleCache, ok := cache.(service.SchedulerLifecycleCache)
	require.True(t, ok)
	const groupID int64 = 79
	const ttl = 500 * time.Millisecond

	first, acquired, err := lifecycleCache.TryAcquireGroupLifecycleLease(ctx, groupID, ttl)
	require.NoError(t, err)
	require.True(t, acquired)
	pttl, err := rdb.PTTL(ctx, schedulerGroupLifecycleLockKey(groupID)).Result()
	require.NoError(t, err)
	require.Positive(t, pttl)
	require.LessOrEqual(t, pttl, ttl)

	var second service.SchedulerGroupLifecycleLease
	require.Eventually(t, func() bool {
		var acquireErr error
		second, acquired, acquireErr = lifecycleCache.TryAcquireGroupLifecycleLease(ctx, groupID, time.Minute)
		return acquireErr == nil && acquired
	}, 5*time.Second, 20*time.Millisecond)
	require.NotEqual(t, first.OwnerToken, second.OwnerToken)

	require.ErrorIs(t, lifecycleCache.ReleaseGroupLifecycleLease(ctx, first), service.ErrSchedulerGroupLifecycleLeaseLost)
	_, acquired, err = lifecycleCache.TryAcquireGroupLifecycleLease(ctx, groupID, time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "a stale release must not delete the successor lease")

	require.NoError(t, lifecycleCache.ReleaseGroupLifecycleLease(ctx, second))
	require.ErrorIs(t, lifecycleCache.ReleaseGroupLifecycleLease(ctx, second), service.ErrSchedulerGroupLifecycleLeaseLost)
	third, acquired, err := lifecycleCache.TryAcquireGroupLifecycleLease(ctx, groupID, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, third.ValidFor(groupID))
	require.NoError(t, lifecycleCache.ReleaseGroupLifecycleLease(ctx, third))
}

func TestSchedulerCacheCandidateSamplingManualBucket(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	bucket := service.SchedulerBucket{GroupID: 18, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	cache := newSchedulerCacheWithOptions(rdb, 128, 256, []string{bucket.String()})

	accounts := make([]service.Account, 0, minSchedulerCandidateShardSize+1)
	for i := 0; i < minSchedulerCandidateShardSize+1; i++ {
		accounts = append(accounts, service.Account{
			ID:          int64(100000 + i),
			Name:        "candidate-index",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    i % 10,
			GroupIDs:    []int64{bucket.GroupID},
		})
	}
	require.NoError(t, cache.SetSnapshot(ctx, bucket, accounts))

	candidateCache, ok := cache.(service.SchedulerCandidateCache)
	require.True(t, ok)
	candidates, hit, err := candidateCache.GetCandidateSnapshot(ctx, bucket, 64, 0, false)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, candidates, 64)

	exists, err := rdb.Exists(
		ctx,
		schedulerBucketKey(schedulerCandidateActivePrefix, bucket),
		schedulerBucketKey(schedulerCandidateReadyPrefix, bucket),
	).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
	legacyIndexKeys, err := rdb.Keys(ctx, schedulerCandidateIndexPrefix+"*").Result()
	require.NoError(t, err)
	require.Empty(t, legacyIndexKeys)
	legacyMetaKeys, err := rdb.Keys(ctx, schedulerCandidateMetaPrefix+"*").Result()
	require.NoError(t, err)
	require.Empty(t, legacyMetaKeys)

	require.NoError(t, cache.SetSnapshot(ctx, bucket, accounts[:1]))
	candidates, hit, err = candidateCache.GetCandidateSnapshot(ctx, bucket, 64, 0, false)
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, candidates)
}

func TestSchedulerCacheCandidateSamplingExpiresLegacyIndex(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	bucket := service.SchedulerBucket{GroupID: 18, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	cache := newSchedulerCacheWithOptions(rdb, 128, 256, []string{bucket.String()})

	legacyVersion := "7"
	require.NoError(t, rdb.Set(ctx, schedulerBucketKey(schedulerCandidateActivePrefix, bucket), legacyVersion, 0).Err())
	require.NoError(t, rdb.Set(ctx, schedulerBucketKey(schedulerCandidateReadyPrefix, bucket), "1", 0).Err())
	require.NoError(t, rdb.HSet(ctx, schedulerCandidateMetaKey(bucket, legacyVersion), "shards", 2).Err())
	require.NoError(t, rdb.Set(ctx, schedulerCandidateShardKey(bucket, legacyVersion, 0), "legacy-0", 0).Err())
	require.NoError(t, rdb.Set(ctx, schedulerCandidateShardKey(bucket, legacyVersion, 1), "legacy-1", 0).Err())

	require.NoError(t, cache.SetSnapshot(ctx, bucket, []service.Account{{
		ID:          42,
		Name:        "legacy-cleanup",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 3,
		GroupIDs:    []int64{bucket.GroupID},
	}}))

	exists, err := rdb.Exists(
		ctx,
		schedulerBucketKey(schedulerCandidateActivePrefix, bucket),
		schedulerBucketKey(schedulerCandidateReadyPrefix, bucket),
	).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
	for _, key := range []string{
		schedulerCandidateMetaKey(bucket, legacyVersion),
		schedulerCandidateShardKey(bucket, legacyVersion, 0),
		schedulerCandidateShardKey(bucket, legacyVersion, 1),
	} {
		ttl, err := rdb.TTL(ctx, key).Result()
		require.NoError(t, err)
		require.Greater(t, ttl, time.Duration(0))
	}
}

func TestSchedulerCacheCandidateSamplingManualSmallBucketMisses(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	bucket := service.SchedulerBucket{GroupID: 18, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	cache := newSchedulerCacheWithOptions(rdb, 128, 256, []string{bucket.String()})

	require.NoError(t, cache.SetSnapshot(ctx, bucket, []service.Account{{
		ID:          42,
		Name:        "small-index",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 3,
		GroupIDs:    []int64{bucket.GroupID},
	}}))

	candidateCache, ok := cache.(service.SchedulerCandidateCache)
	require.True(t, ok)
	candidates, hit, err := candidateCache.GetCandidateSnapshot(ctx, bucket, 64, 0, false)
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, candidates)
}

func TestSchedulerCacheCandidateSamplingDisabledBucketMisses(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	enabledBucket := service.SchedulerBucket{GroupID: 18, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	disabledBucket := service.SchedulerBucket{GroupID: 19, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	cache := newSchedulerCacheWithOptions(rdb, 128, 256, []string{enabledBucket.String()})

	require.NoError(t, cache.SetSnapshot(ctx, disabledBucket, []service.Account{{
		ID:          42,
		Name:        "disabled-index",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 3,
		GroupIDs:    []int64{disabledBucket.GroupID},
	}}))

	candidateCache, ok := cache.(service.SchedulerCandidateCache)
	require.True(t, ok)
	candidates, hit, err := candidateCache.GetCandidateSnapshot(ctx, disabledBucket, 64, 0, false)
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, candidates)
}

func schedulerBucketStrings(buckets []service.SchedulerBucket) []string {
	out := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, bucket.String())
	}
	return out
}
