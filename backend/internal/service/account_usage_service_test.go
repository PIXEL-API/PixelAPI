package service

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestAccountUsageService_GetOpenAILocalUsage_DoesNotProbeUpstream(t *testing.T) {
	t.Parallel()

	svc := &AccountUsageService{cache: NewUsageCache()}
	account := &Account{
		ID:       31,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}

	usage, err := svc.GetLocalUsageForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("GetLocalUsageForAccount() error = %v", err)
	}
	if usage.Source != "local" {
		t.Fatalf("usage source = %q, want local", usage.Source)
	}
	if usage.FiveHour == nil || usage.FiveHour.Utilization != 42 {
		t.Fatalf("local five-hour snapshot = %#v", usage.FiveHour)
	}
	if _, found := svc.cache.openAIProbeCache.Load(account.ID); found {
		t.Fatal("local OpenAI usage must not enter the upstream probe gate")
	}
}

func TestAccountUsageService_GetUsageForAccount_MarksActiveQueryMode(t *testing.T) {
	t.Parallel()

	svc := &AccountUsageService{cache: NewUsageCache()}
	account := &Account{
		ID:       32,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 10.0,
			"codex_5h_reset_at":     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"codex_7d_used_percent": 20.0,
			"codex_7d_reset_at":     time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		},
	}

	usage, err := svc.GetUsageForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("GetUsageForAccount() error = %v", err)
	}
	if usage.Source != "active" {
		t.Fatalf("usage source = %q, want active", usage.Source)
	}
}

func TestAccountUsageService_GetUsageForAccount_DoesNotMutateAntigravityCache(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(time.Hour)
	cachedUsage := &UsageInfo{
		Source: "passive",
		FiveHour: &UsageProgress{
			ResetsAt:         &resetAt,
			RemainingSeconds: 7,
		},
	}
	cache := NewUsageCache()
	cache.antigravityCache.Store(int64(33), &antigravityUsageCache{
		usageInfo: cachedUsage,
		timestamp: time.Now(),
	})
	svc := &AccountUsageService{
		cache:                   cache,
		antigravityQuotaFetcher: &AntigravityQuotaFetcher{},
	}

	usage, err := svc.GetUsageForAccount(context.Background(), &Account{
		ID:          33,
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "token"},
	})
	if err != nil {
		t.Fatalf("GetUsageForAccount() error = %v", err)
	}
	if usage.Source != "active" {
		t.Fatalf("usage source = %q, want active", usage.Source)
	}
	if cachedUsage.Source != "passive" {
		t.Fatalf("cached source mutated to %q", cachedUsage.Source)
	}
	if cachedUsage.FiveHour.RemainingSeconds != 7 {
		t.Fatalf("cached remaining seconds mutated to %d", cachedUsage.FiveHour.RemainingSeconds)
	}
	if usage == cachedUsage || usage.FiveHour == cachedUsage.FiveHour {
		t.Fatal("active response must not share mutable usage pointers with the cache")
	}
}

func TestAccountUsageService_GetAntigravityLocalUsage_ColdCacheDoesNotFetch(t *testing.T) {
	t.Parallel()

	svc := &AccountUsageService{cache: NewUsageCache()}
	usage, err := svc.GetLocalUsageForAccount(context.Background(), &Account{
		ID:       41,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
	})
	if err != nil {
		t.Fatalf("GetLocalUsageForAccount() error = %v", err)
	}
	if usage.Source != "local" || usage.ErrorCode != "snapshot_unavailable" {
		t.Fatalf("cold local Antigravity usage = %#v", usage)
	}
}

type accountUsageCodexProbeRepo struct {
	stubOpenAIAccountRepo
	updateExtraCh chan map[string]any
	rateLimitCh   chan time.Time
}

func (r *accountUsageCodexProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.updateExtraCh != nil {
		copied := make(map[string]any, len(updates))
		for k, v := range updates {
			copied[k] = v
		}
		r.updateExtraCh <- copied
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	if r.rateLimitCh != nil {
		r.rateLimitCh <- resetAt
	}
	return nil
}

func TestShouldRefreshOpenAICodexSnapshot(t *testing.T) {
	t.Parallel()

	rateLimitedUntil := time.Now().Add(5 * time.Minute)
	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{RateLimitResetAt: &rateLimitedUntil}, usage, now) {
		t.Fatal("expected rate-limited account to force codex snapshot refresh")
	}

	if shouldRefreshOpenAICodexSnapshot(&Account{}, usage, now) {
		t.Fatal("expected complete non-rate-limited usage to skip codex snapshot refresh")
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{}, &UsageInfo{FiveHour: nil, SevenDay: &UsageProgress{}}, now) {
		t.Fatal("expected missing 5h snapshot to require refresh")
	}

	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	if !shouldRefreshOpenAICodexSnapshot(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"codex_usage_updated_at":                       staleAt,
		},
	}, usage, now) {
		t.Fatal("expected stale ws snapshot to trigger refresh")
	}
}

func TestExtractOpenAICodexProbeUpdatesAccepts429WithCodexHeaders(t *testing.T) {
	t.Parallel()

	headers := make(http.Header)
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	updates, err := extractOpenAICodexProbeUpdates(&http.Response{StatusCode: http.StatusTooManyRequests, Header: headers})
	if err != nil {
		t.Fatalf("extractOpenAICodexProbeUpdates() error = %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected codex probe updates from 429 headers")
	}
	if got := updates["codex_5h_used_percent"]; got != 100.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 100", got)
	}
	if got := updates["codex_7d_used_percent"]; got != 100.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 100", got)
	}
}

func TestAccountUsageService_PersistOpenAICodexProbeSnapshotOnlyUpdatesExtra(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		updateExtraCh: make(chan map[string]any, 1),
		rateLimitCh:   make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	svc.persistOpenAICodexProbeSnapshot(321, map[string]any{
		"codex_7d_used_percent": 100.0,
		"codex_7d_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
	})

	select {
	case updates := <-repo.updateExtraCh:
		if got := updates["codex_7d_used_percent"]; got != 100.0 {
			t.Fatalf("codex_7d_used_percent = %v, want 100", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待 codex 探测快照写入 extra 超时")
	}

	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将探测快照写入运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAccountUsageService_GetOpenAIUsage_DoesNotPromoteCodexExtraToRateLimit(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		rateLimitCh: make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 1.0,
			"codex_5h_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     resetAt.Format(time.RFC3339),
		},
	}

	usage, err := svc.getOpenAIUsage(context.Background(), account)
	if err != nil {
		t.Fatalf("getOpenAIUsage() error = %v", err)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 100.0 {
		t.Fatalf("预期 7 天用量仍然可见，实际为 %#v", usage.SevenDay)
	}
	if account.RateLimitResetAt != nil {
		t.Fatalf("不应让已耗尽的 codex extra 改写运行时限流状态: %v", account.RateLimitResetAt)
	}
	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将已耗尽的 codex extra 持久化为运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBuildCodexUsageProgressFromExtra_ZerosExpiredWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)

	t.Run("expired 5h window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     "2026-03-16T10:00:00Z", // 2h ago
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired window, got %v", progress.Utilization)
		}
		if progress.RemainingSeconds != 0 {
			t.Fatalf("expected RemainingSeconds=0, got %v", progress.RemainingSeconds)
		}
		if progress.WindowStart != nil {
			t.Fatalf("expected WindowStart=nil for expired window, got %v", progress.WindowStart)
		}
	})

	t.Run("active 5h window keeps utilization and derives exact start", func(t *testing.T) {
		resetTime := now.Add(2 * time.Hour)
		extra := map[string]any{
			"codex_5h_used_percent":   42.0,
			"codex_5h_reset_at":       resetTime.Format(time.RFC3339),
			"codex_5h_window_minutes": 300,
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 42.0 {
			t.Fatalf("expected Utilization=42, got %v", progress.Utilization)
		}
		expectedStart := resetTime.Add(-5 * time.Hour)
		if progress.WindowStart == nil || !progress.WindowStart.Equal(expectedStart) {
			t.Fatalf("expected WindowStart=%v, got %v", expectedStart, progress.WindowStart)
		}
	})

	t.Run("expired 7d window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_7d_used_percent": 88.0,
			"codex_7d_reset_at":     "2026-03-15T00:00:00Z", // yesterday
		}
		progress := buildCodexUsageProgressFromExtra(extra, "7d", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired 7d window, got %v", progress.Utilization)
		}
	})
}

func TestUsageWindowElapsedHours(t *testing.T) {
	t.Parallel()

	if got := usageWindowElapsedHours("5h", 2*60*60); got != 3 {
		t.Fatalf("usageWindowElapsedHours(5h, 2h remaining) = %v, want 3", got)
	}
	if got := usageWindowElapsedHours("5h", 5*60*60); got != 0 {
		t.Fatalf("usageWindowElapsedHours(5h, full remaining) = %v, want 0", got)
	}
	if got := usageWindowElapsedHours("unknown", 0); got != 0 {
		t.Fatalf("usageWindowElapsedHours(unknown) = %v, want 0", got)
	}
}
