package service

import (
	"context"
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
	cachedAccounts []Account
}

func (c *schedulerSnapshotQuotaCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return nil, false, nil
}

func (c *schedulerSnapshotQuotaCache) SetSnapshot(_ context.Context, _ SchedulerBucket, accounts []Account) error {
	c.cachedAccounts = append([]Account(nil), accounts...)
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
	accounts []Account
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
