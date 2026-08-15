//go:build unit

package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type grokFreeQuotaUsageRepoStub struct {
	UsageLogRepository

	mu      sync.Mutex
	stats   map[int64]*usagestats.AccountStats
	err     error
	calls   int
	lastIDs []int64
	start   time.Time
}

type grokFreeQuotaAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

type grokFreeQuotaBlockingUsageRepoStub struct {
	UsageLogRepository
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *grokFreeQuotaBlockingUsageRepoStub) GetAccountWindowStatsBatch(_ context.Context, accountIDs []int64, _ time.Time) (map[int64]*usagestats.AccountStats, error) {
	r.calls.Add(1)
	r.once.Do(func() { close(r.started) })
	<-r.release
	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = &usagestats.AccountStats{Tokens: 100_000}
	}
	return result, nil
}

func (r *grokFreeQuotaAccountRepoStub) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	result := make([]Account, 0, len(r.accounts))
	for i := range r.accounts {
		if r.accounts[i].Platform == platform {
			result = append(result, r.accounts[i])
		}
	}
	return result, nil
}

func (r *grokFreeQuotaAccountRepoStub) GetByID(_ context.Context, accountID int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == accountID {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *grokFreeQuotaUsageRepoStub) GetAccountWindowStatsBatch(_ context.Context, accountIDs []int64, start time.Time) (map[int64]*usagestats.AccountStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lastIDs = append([]int64(nil), accountIDs...)
	r.start = start
	if r.err != nil {
		return nil, r.err
	}
	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, accountID := range accountIDs {
		if stats := r.stats[accountID]; stats != nil {
			copyStats := *stats
			result[accountID] = &copyStats
		}
	}
	return result, nil
}

func grokFreeQuotaTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.Grok.FreeQuotaSoftGateEnabled = true
	cfg.Gateway.Grok.FreeQuotaTokenLimit = 500_000
	cfg.Gateway.Grok.FreeQuotaSoftGatePercent = 95
	cfg.Gateway.Grok.FreeQuotaWindowHours = 24
	cfg.Gateway.Grok.FreeQuotaStatsCacheSeconds = 60
	return cfg
}

func TestIsExplicitGrokFreeOAuthAccountOnlyAcceptsExactFree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "nil", account: nil},
		{name: "credential subscription tier", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": " FREE "}}, want: true},
		{name: "credential plan type", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"}}, want: true},
		{name: "extra subscription tier", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{"subscription_tier": "Free"}}, want: true},
		{name: "extra plan type", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{"plan_type": "FREE"}}, want: true},
		{name: "blank", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": " "}}},
		{name: "basic", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": "basic"}}},
		{name: "free prefix", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": "free_trial"}}},
		{name: "missing tier", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}},
		{name: "api key", account: &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"subscription_tier": "free"}}},
		{name: "non grok", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": "free"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isExplicitGrokFreeOAuthAccount(tt.account))
		})
	}
}

func TestFilterGrokFreeQuotaAccountsUsesAsyncBatchAndSoftBoundary(t *testing.T) {
	repo := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usagestats.AccountStats{
		1: {Tokens: 474_999},
		2: {Tokens: 475_000},
	}}
	var cache sync.Map
	accounts := []Account{
		{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": "free"}},
		{ID: 2, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"}},
		{ID: 3, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"subscription_tier": "basic"}},
	}

	filtered := filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
	require.Equal(t, []int64{1, 2, 3}, grokFreeQuotaAccountIDs(filtered), "cache miss must fail open")

	require.Eventually(t, func() bool {
		filtered = filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
		return slices.Equal([]int64{1, 3}, grokFreeQuotaAccountIDs(filtered))
	}, 2*time.Second, 10*time.Millisecond)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.calls)
	require.ElementsMatch(t, []int64{1, 2}, repo.lastIDs)
	require.WithinDuration(t, time.Now().UTC().Add(-24*time.Hour), repo.start, time.Second)
}

func TestFilterGrokFreeQuotaAccountsStatsFailureFailsOpenWithoutQueryStorm(t *testing.T) {
	repo := &grokFreeQuotaUsageRepoStub{err: errors.New("usage database unavailable")}
	var cache sync.Map
	accounts := []Account{{
		ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Credentials: map[string]any{"subscription_tier": "free"},
	}}
	failuresBefore := grokFreeQuotaGateQueryFailureTotal.Load()

	for range 20 {
		filtered := filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
		require.Equal(t, []int64{1}, grokFreeQuotaAccountIDs(filtered))
	}
	require.Eventually(t, func() bool {
		return grokFreeQuotaGateQueryFailureTotal.Load() > failuresBefore
	}, 2*time.Second, 10*time.Millisecond)

	for range 20 {
		filtered := filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
		require.Equal(t, []int64{1}, grokFreeQuotaAccountIDs(filtered))
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.calls, "negative cache must prevent repeated database queries")
}

func TestFilterGrokFreeQuotaAccountsDisabledDoesNotQuery(t *testing.T) {
	cfg := grokFreeQuotaTestConfig()
	cfg.Gateway.Grok.FreeQuotaSoftGateEnabled = false
	repo := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usagestats.AccountStats{1: {Tokens: 500_000}}}
	var cache sync.Map
	accounts := []Account{{
		ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Credentials: map[string]any{"subscription_tier": "free"},
	}}

	filtered := filterGrokFreeQuotaAccountsCore(context.Background(), cfg, repo, &cache, accounts)
	require.Equal(t, []int64{1}, grokFreeQuotaAccountIDs(filtered))
	time.Sleep(20 * time.Millisecond)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Zero(t, repo.calls)
}

func TestFilterGrokFreeQuotaAccountsConcurrentMissesAreCoalesced(t *testing.T) {
	repo := &grokFreeQuotaBlockingUsageRepoStub{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	var cache sync.Map
	accounts := []Account{{
		ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Credentials: map[string]any{"subscription_tier": "free"},
	}}

	const callerCount = 64
	var callers sync.WaitGroup
	results := make(chan bool, callerCount)
	callers.Add(callerCount)
	for range callerCount {
		go func() {
			defer callers.Done()
			filtered := filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
			results <- slices.Equal([]int64{1}, grokFreeQuotaAccountIDs(filtered))
		}()
	}
	select {
	case <-repo.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background quota refresh did not start")
	}
	callers.Wait()
	close(results)
	for result := range results {
		require.True(t, result)
	}
	require.Equal(t, int64(1), repo.calls.Load())
	close(repo.release)
	require.Eventually(t, func() bool {
		entry, ok := cache.Load(int64(1))
		cached, valid := entry.(grokFreeQuotaGateCacheEntry)
		return ok && valid && cached.known
	}, 2*time.Second, 10*time.Millisecond)
}

func TestFilterGrokFreeQuotaAccountsRecoversAfterCachedWindowUsageFalls(t *testing.T) {
	repo := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usagestats.AccountStats{
		1: {Tokens: 100_000},
	}}
	var cache sync.Map
	cache.Store(int64(1), grokFreeQuotaGateCacheEntry{
		tokens:    490_000,
		checkedAt: time.Now().UTC().Add(-2 * time.Minute),
		known:     true,
	})
	accounts := []Account{{
		ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Credentials: map[string]any{"plan_type": "free"},
	}}

	filtered := filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
	require.Equal(t, []int64{1}, grokFreeQuotaAccountIDs(filtered), "stale cache must fail open while refreshing")
	require.Eventually(t, func() bool {
		value, ok := cache.Load(int64(1))
		entry, valid := value.(grokFreeQuotaGateCacheEntry)
		return ok && valid && entry.known && entry.tokens == 100_000
	}, 2*time.Second, 10*time.Millisecond)
	filtered = filterGrokFreeQuotaAccountsCore(context.Background(), grokFreeQuotaTestConfig(), repo, &cache, accounts)
	require.Equal(t, []int64{1}, grokFreeQuotaAccountIDs(filtered))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.calls)
}

func TestGrokFreeQuotaGateSettingsPreserveUpstreamDefaults(t *testing.T) {
	settings, ok := resolveGrokFreeQuotaGateSettings(grokFreeQuotaTestConfig())
	require.True(t, ok)
	require.Equal(t, int64(500_000), settings.limitTokens)
	require.Equal(t, int64(475_000), settings.gateTokens)
	require.Equal(t, 24*time.Hour, settings.window)
	require.Equal(t, 60*time.Second, settings.cacheTTL)
	require.Equal(t, int64(0), calculateGrokFreeQuotaSoftGateTokens(0, 95))
	require.Equal(t, int64(1), calculateGrokFreeQuotaSoftGateTokens(1, 100))
}

func TestGrokFreeQuotaGateIsSharedByResponsesAndStandaloneSearchSchedulers(t *testing.T) {
	cfg := grokFreeQuotaTestConfig()
	repo := &grokFreeQuotaUsageRepoStub{}
	accounts := []Account{
		{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"subscription_tier": "free"}},
		{ID: 2, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"subscription_tier": "pro"}},
	}
	accountRepo := &grokFreeQuotaAccountRepoStub{accounts: accounts}
	now := time.Now().UTC()
	openaiGrokFreeQuotaGateCache = sync.Map{}
	gatewayGrokFreeQuotaGateCache = sync.Map{}
	openaiGrokFreeQuotaGateCache.Store(int64(1), grokFreeQuotaGateCacheEntry{tokens: 475_000, checkedAt: now, known: true})
	gatewayGrokFreeQuotaGateCache.Store(int64(1), grokFreeQuotaGateCacheEntry{tokens: 475_000, checkedAt: now, known: true})

	responsesService := &OpenAIGatewayService{cfg: cfg, accountRepo: accountRepo, usageLogRepo: repo}
	responsesAccounts, err := responsesService.listSchedulableAccounts(withGrokPlatform(context.Background()), nil)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, grokFreeQuotaAccountIDs(responsesAccounts))

	searchService := &GatewayService{cfg: cfg, accountRepo: accountRepo, usageLogRepo: repo}
	searchAccounts, _, err := searchService.listSchedulableAccounts(context.Background(), nil, PlatformGrok, true)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, grokFreeQuotaAccountIDs(searchAccounts))

	responsesSticky, err := responsesService.getSchedulableAccount(context.Background(), 1)
	require.NoError(t, err)
	require.Nil(t, responsesSticky)
	searchSticky, err := searchService.getSchedulableAccount(context.Background(), 1)
	require.NoError(t, err)
	require.Nil(t, searchSticky)
}

func grokFreeQuotaAccountIDs(accounts []Account) []int64 {
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		ids = append(ids, accounts[i].ID)
	}
	return ids
}
