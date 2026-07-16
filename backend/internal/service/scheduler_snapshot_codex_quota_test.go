package service

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFilterSchedulableAccountsExcludesCodexQuotaProtectedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_5h_limit_percent": 80.0,
				"codex_5h_used_percent":  81.0,
				"codex_5h_reset_at":      resetAt,
			},
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_5h_limit_percent": 80.0,
				"codex_5h_used_percent":  79.9,
				"codex_5h_reset_at":      resetAt,
			},
		},
	}

	filtered := filterSchedulableAccounts(accounts)
	if len(filtered) != 1 || filtered[0].ID != 2 {
		t.Fatalf("filtered accounts = %+v, want only account 2", filtered)
	}
}

func TestFilterSchedulableAccountsForSnapshotKeepsCodexQuotaProtectedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				"codex_5h_limit_percent": 80.0,
				"codex_5h_used_percent":  81.0,
				"codex_5h_reset_at":      resetAt,
			},
		},
	}

	filtered := filterSchedulableAccountsForSnapshot(accounts)
	if len(filtered) != 1 || filtered[0].ID != 1 {
		t.Fatalf("filtered accounts = %+v, want protected account retained in snapshot", filtered)
	}
}

func TestSchedulerSnapshotFallbackCachesButDoesNotReturnCodexQuotaProtectedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	cache := &schedulerSnapshotQuotaCache{}
	repo := &schedulerSnapshotQuotaAccountRepo{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"codex_5h_limit_percent": 80.0,
					"codex_5h_used_percent":  81.0,
					"codex_5h_reset_at":      resetAt,
				},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Extra: map[string]any{
					"codex_5h_limit_percent": 80.0,
					"codex_5h_used_percent":  79.0,
					"codex_5h_reset_at":      resetAt,
				},
			},
		},
	}
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)

	accounts, _, err := svc.ListSchedulableAccounts(context.Background(), nil, PlatformOpenAI, false)
	if err != nil {
		t.Fatalf("ListSchedulableAccounts error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != 2 {
		t.Fatalf("returned accounts = %+v, want only account 2", accounts)
	}
	if len(cache.cachedAccounts) != 2 {
		t.Fatalf("cached accounts = %+v, want both accounts retained for automatic recovery", cache.cachedAccounts)
	}
}

type schedulerSnapshotQuotaCache struct {
	cachedAccounts  []Account
	retiredBuckets  []SchedulerBucket
	reopenedBuckets []SchedulerBucket
}

func (c *schedulerSnapshotQuotaCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return nil, false, nil
}

func (c *schedulerSnapshotQuotaCache) SetSnapshot(_ context.Context, _ SchedulerBucket, accounts []Account) error {
	c.cachedAccounts = append([]Account(nil), accounts...)
	return nil
}

func (c *schedulerSnapshotQuotaCache) CaptureBucketWriteToken(_ context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error) {
	return SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (c *schedulerSnapshotQuotaCache) SetSnapshotFenced(ctx context.Context, bucket SchedulerBucket, _ SchedulerBucketWriteToken, accounts []Account) error {
	return c.SetSnapshot(ctx, bucket, accounts)
}

func (c *schedulerSnapshotQuotaCache) RetireBucket(_ context.Context, bucket SchedulerBucket) error {
	c.retiredBuckets = append(c.retiredBuckets, bucket)
	return nil
}

func (c *schedulerSnapshotQuotaCache) ReopenBucket(_ context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error) {
	c.reopenedBuckets = append(c.reopenedBuckets, bucket)
	return SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (c *schedulerSnapshotQuotaCache) TryAcquireGroupLifecycleLease(_ context.Context, groupID int64, _ time.Duration) (SchedulerGroupLifecycleLease, bool, error) {
	return SchedulerGroupLifecycleLease{GroupID: groupID, OwnerToken: "test"}, true, nil
}

func (c *schedulerSnapshotQuotaCache) ReleaseGroupLifecycleLease(context.Context, SchedulerGroupLifecycleLease) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) GetAccount(context.Context, int64) (*Account, error) {
	return nil, nil
}

func (c *schedulerSnapshotQuotaCache) SetAccount(context.Context, *Account) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) DeleteAccount(context.Context, int64) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) UpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) TryLockBucket(context.Context, SchedulerBucket, time.Duration) (bool, error) {
	return true, nil
}

func (c *schedulerSnapshotQuotaCache) UnlockBucket(context.Context, SchedulerBucket) error {
	return nil
}

func (c *schedulerSnapshotQuotaCache) ListBuckets(context.Context) ([]SchedulerBucket, error) {
	return nil, nil
}

func (c *schedulerSnapshotQuotaCache) GetOutboxWatermark(context.Context) (int64, error) {
	return 0, nil
}

func (c *schedulerSnapshotQuotaCache) SetOutboxWatermark(context.Context, int64) error {
	return nil
}

type schedulerSnapshotQuotaAccountRepo struct {
	AccountRepository
	accounts   []Account
	groupCalls atomic.Int32
}

func (r *schedulerSnapshotQuotaAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	out := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *schedulerSnapshotQuotaAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	r.groupCalls.Add(1)
	out := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

type schedulerLifecycleGroupRepo struct {
	GroupRepository
	group *Group
	err   error
}

func (r *schedulerLifecycleGroupRepo) GetByIDLite(context.Context, int64) (*Group, error) {
	return r.group, r.err
}

func TestSchedulerSnapshotDisabledGroupRetiresCanonicalBuckets(t *testing.T) {
	cache := &schedulerSnapshotQuotaCache{}
	groups := &schedulerLifecycleGroupRepo{group: &Group{ID: 41, Status: StatusDisabled, Hydrated: true}}
	service := NewSchedulerSnapshotService(cache, nil, nil, groups, nil)
	groupID := int64(41)

	err := service.handleGroupEvent(context.Background(), &groupID, make(map[batchSeenKey]struct{}))
	if err != nil {
		t.Fatalf("handleGroupEvent error: %v", err)
	}
	want := len(schedulerBucketsForGroup(41))
	if len(cache.retiredBuckets) != want {
		t.Fatalf("retired buckets = %d, want %d", len(cache.retiredBuckets), want)
	}
	if len(cache.reopenedBuckets) != 0 {
		t.Fatalf("unexpected reopened buckets: %+v", cache.reopenedBuckets)
	}
}

func TestSchedulerSnapshotRebuildBatchReusesSingleForcedQuery(t *testing.T) {
	repo := &schedulerSnapshotQuotaAccountRepo{accounts: []Account{{
		ID:          7,
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: true,
	}}}
	service := NewSchedulerSnapshotService(nil, nil, repo, nil, nil)
	tasks := []schedulerBucketWriteTask{
		{bucket: SchedulerBucket{GroupID: 3, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}},
		{bucket: SchedulerBucket{GroupID: 3, Platform: PlatformOpenAI, Mode: SchedulerModeForced}},
	}
	queries := newSchedulerAccountQueryCache(tasks)

	first, err := service.loadAccountsForRebuild(context.Background(), tasks[0].bucket, queries)
	if err != nil {
		t.Fatalf("first load error: %v", err)
	}
	second, err := service.loadAccountsForRebuild(context.Background(), tasks[1].bucket, queries)
	if err != nil {
		t.Fatalf("second load error: %v", err)
	}
	if repo.groupCalls.Load() != 1 {
		t.Fatalf("group query calls = %d, want 1", repo.groupCalls.Load())
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("unexpected cached query results: first=%+v second=%+v", first, second)
	}
}

func TestSchedulerSnapshotFullRebuildCoalescesQueuedRequests(t *testing.T) {
	service := &SchedulerSnapshotService{}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	queued := make(chan struct{}, 8)
	var calls atomic.Int32
	run := func() error {
		call := calls.Add(1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = service.coalesceFullRebuild(run)
	}()
	<-firstStarted

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queued <- struct{}{}
			_ = service.coalesceFullRebuild(run)
		}()
	}
	for range 8 {
		<-queued
	}
	deadline := time.Now().Add(time.Second)
	for {
		service.fullRebuildStateMu.Lock()
		requested := service.fullRebuildRequested
		service.fullRebuildStateMu.Unlock()
		if requested == 9 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued rebuild generations = %d, want 9", requested)
		}
		runtime.Gosched()
	}
	close(releaseFirst)
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatalf("full rebuild calls = %d, want 2", calls.Load())
	}
}

func TestSchedulerSnapshotClearDegradedEpisodePreservesRunningRebuild(t *testing.T) {
	service := &SchedulerSnapshotService{
		lagFailures:              3,
		outboxRebuildLatched:     true,
		outboxRebuildRunning:     true,
		outboxRebuildFailures:    2,
		outboxRebuildRetryAt:     time.Now().Add(time.Minute),
		outboxRebuildRetryReason: "outbox_lag",
		outboxLagWarningActive:   true,
	}

	service.clearOutboxDegradedEpisode()

	if service.lagFailures != 0 || service.outboxRebuildLatched ||
		service.outboxRebuildFailures != 0 || !service.outboxRebuildRetryAt.IsZero() ||
		service.outboxRebuildRetryReason != "" || service.outboxLagWarningActive {
		t.Fatalf("degraded episode state was not cleared: %+v", service)
	}
	if !service.outboxRebuildRunning {
		t.Fatal("running rebuild state must be owned and cleared by the rebuild caller")
	}
}
