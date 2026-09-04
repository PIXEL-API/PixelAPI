package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type openAISnapshotCacheStub struct {
	SchedulerCache
	snapshotAccounts []*Account
	accountsByID     map[int64]*Account
	getErrors        map[int64]error
}

type openAICandidateSnapshotCacheStub struct {
	openAISnapshotCacheStub
	candidateAccounts []*Account
	candidateHits     int
	fullHits          int
	bypassHits        int
}

type schedulerTestOpenAIAccountRepo struct {
	AccountRepository
	accounts  []Account
	getErrors map[int64]error
}

func (r schedulerTestOpenAIAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if err := r.getErrors[id]; err != nil {
		return nil, err
	}
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, errors.New("account not found")
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform != platform {
			continue
		}
		if openAITestAccountHasGroupMetadata(acc) {
			if openAITestAccountBelongsToGroup(acc, groupID) {
				result = append(result, acc)
			}
			continue
		}
		result = append(result, openAITestAccountWithGroupIfUnset(acc, groupID))
	}
	return result, nil
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r schedulerTestOpenAIAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

type schedulerTestConcurrencyCache struct {
	ConcurrencyCache
	loadBatchErr    error
	loadMap         map[int64]*AccountLoadInfo
	acquireResults  map[int64]bool
	acquireErrors   map[int64]error
	acquireCalls    *[]int64
	acquireOnCall   int
	waitCounts      map[int64]int
	skipDefaultLoad bool
}

func (c schedulerTestConcurrencyCache) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	if c.acquireCalls != nil {
		*c.acquireCalls = append(*c.acquireCalls, accountID)
		if c.acquireOnCall > 0 && len(*c.acquireCalls) == c.acquireOnCall {
			return true, nil
		}
	}
	if c.acquireErrors != nil {
		if err, ok := c.acquireErrors[accountID]; ok {
			return false, err
		}
	}
	if c.acquireResults != nil {
		if result, ok := c.acquireResults[accountID]; ok {
			return result, nil
		}
	}
	return true, nil
}

func (c schedulerTestConcurrencyCache) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	return nil
}

func (c schedulerTestConcurrencyCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	if c.loadBatchErr != nil {
		return nil, c.loadBatchErr
	}
	out := make(map[int64]*AccountLoadInfo, len(accounts))
	if c.skipDefaultLoad && c.loadMap != nil {
		for _, acc := range accounts {
			if load, ok := c.loadMap[acc.ID]; ok {
				out[acc.ID] = load
			}
		}
		return out, nil
	}
	for _, acc := range accounts {
		if c.loadMap != nil {
			if load, ok := c.loadMap[acc.ID]; ok {
				out[acc.ID] = load
				continue
			}
		}
		out[acc.ID] = &AccountLoadInfo{AccountID: acc.ID, LoadRate: 0}
	}
	return out, nil
}

func (c schedulerTestConcurrencyCache) GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error) {
	if c.waitCounts != nil {
		if count, ok := c.waitCounts[accountID]; ok {
			return count, nil
		}
	}
	return 0, nil
}

type schedulerTestGatewayCache struct {
	sessionBindings map[string]int64
	deletedSessions map[string]int
	stringBindings  map[string]string
	getErrors       map[string]error
}

func (c *schedulerTestGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if err := c.getErrors[sessionHash]; err != nil {
		return 0, err
	}
	if id, ok := c.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, redis.Nil
}

func (c *schedulerTestGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if c.sessionBindings == nil {
		c.sessionBindings = make(map[string]int64)
	}
	c.sessionBindings[sessionHash] = accountID
	return nil
}

func (c *schedulerTestGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	return nil
}

func (c *schedulerTestGatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	if c.sessionBindings == nil {
		return nil
	}
	if c.deletedSessions == nil {
		c.deletedSessions = make(map[string]int)
	}
	c.deletedSessions[sessionHash]++
	delete(c.sessionBindings, sessionHash)
	return nil
}

func (c *schedulerTestGatewayCache) GetSessionString(ctx context.Context, groupID int64, sessionHash string) (string, error) {
	if c.stringBindings != nil {
		if value, ok := c.stringBindings[sessionHash]; ok {
			return value, nil
		}
	}
	return "", ErrGatewaySessionStringNotFound
}

func (c *schedulerTestGatewayCache) SetSessionString(ctx context.Context, groupID int64, sessionHash string, value string, ttl time.Duration) error {
	if c.stringBindings == nil {
		c.stringBindings = make(map[string]string)
	}
	c.stringBindings[sessionHash] = value
	return nil
}

func (c *schedulerTestGatewayCache) DeleteSessionString(ctx context.Context, groupID int64, sessionHash string) error {
	if c.stringBindings != nil {
		delete(c.stringBindings, sessionHash)
	}
	return nil
}

func newSchedulerTestOpenAIWSV2Config() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	return cfg
}

type openAIAdvancedSchedulerSettingRepoStub struct {
	values map[string]string
}

func (s *openAIAdvancedSchedulerSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *openAIAdvancedSchedulerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s == nil || s.values == nil {
		return "", ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *openAIAdvancedSchedulerSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected call to Set")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected call to GetMultiple")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected call to SetMultiple")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected call to GetAll")
}

func (s *openAIAdvancedSchedulerSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected call to Delete")
}

func newOpenAIAdvancedSchedulerRateLimitService(enabled string) *RateLimitService {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	repo := &openAIAdvancedSchedulerSettingRepoStub{
		values: map[string]string{},
	}
	if enabled != "" {
		repo.values[openAIAdvancedSchedulerSettingKey] = enabled
	}
	return &RateLimitService{
		settingService: NewSettingService(repo, &config.Config{}),
	}
}

func (s *openAISnapshotCacheStub) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	if len(s.snapshotAccounts) == 0 {
		return nil, false, nil
	}
	out := make([]*Account, 0, len(s.snapshotAccounts))
	for _, account := range s.snapshotAccounts {
		if account == nil {
			continue
		}
		cloned := *account
		out = append(out, &cloned)
	}
	return out, true, nil
}

func (s *openAISnapshotCacheStub) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	if err := s.getErrors[accountID]; err != nil {
		return nil, err
	}
	if s.accountsByID == nil {
		return nil, nil
	}
	account := s.accountsByID[accountID]
	if account == nil {
		return nil, nil
	}
	cloned := *account
	return &cloned, nil
}

func (s *openAICandidateSnapshotCacheStub) GetCandidateSnapshot(ctx context.Context, bucket SchedulerBucket, limit, threshold int, globalEnabled bool) ([]*Account, bool, error) {
	s.candidateHits++
	if len(s.candidateAccounts) == 0 {
		return nil, false, nil
	}
	out := make([]*Account, 0, len(s.candidateAccounts))
	for _, account := range s.candidateAccounts {
		if account == nil {
			continue
		}
		cloned := *account
		out = append(out, &cloned)
		if len(out) >= limit {
			break
		}
	}
	return out, true, nil
}

func (s *openAICandidateSnapshotCacheStub) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	if IsSchedulerCandidateIndexBypassed(ctx) {
		s.bypassHits++
	} else {
		s.fullHits++
	}
	return s.openAISnapshotCacheStub.GetSnapshot(ctx, bucket)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabledUsesLegacyLoadAwareness(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10106)
	accounts := []Account{
		{
			ID:          36001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
		},
		{
			ID:          36002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cache := &schedulerTestGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	require.False(t, svc.isOpenAIAdvancedSchedulerEnabled(ctx))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(36002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickyPreviousHit)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabledContinuationKeepsBusyStickyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	groupID := int64(10111)
	stickyAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          36101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          36102,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)

	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 30 * time.Second
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:continuation-session": stickyAccount.ID,
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{
			stickyAccount.ID:    false,
			availableAccount.ID: true,
		},
		waitCounts: map[int64]int{
			stickyAccount.ID: 999,
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{stickyAccount, availableAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_continuation",
		"continuation-session",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, stickyAccount.ID, selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, stickyAccount.ID, selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabledFiltersTransportBeforeAcquire(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	groupID := int64(10114)
	forceHTTPAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          36301,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"openai_ws_force_http": true,
		},
	}, groupID)
	websocketAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          36302,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)

	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{
		"openai:transport-filter-session": forceHTTPAccount.ID,
	}}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{forceHTTPAccount, websocketAccount}},
		cache:       cache,
		cfg:         cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquireErrors:  map[int64]error{forceHTTPAccount.ID: context.Canceled},
			acquireResults: map[int64]bool{websocketAccount.ID: true},
		}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"transport-filter-session",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, websocketAccount.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, websocketAccount.ID, cache.sessionBindings["openai:transport-filter-session"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_AdvancedContinuationKeepsBusyStickyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	groupID := int64(10112)
	stickyAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          36201,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          36202,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)

	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 30 * time.Second
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:advanced-continuation-session": stickyAccount.ID,
		},
	}
	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{
			stickyAccount.ID:    false,
			availableAccount.ID: true,
		},
		waitCounts: map[int64]int{
			stickyAccount.ID: 999,
		},
		loadMap: map[int64]*AccountLoadInfo{
			stickyAccount.ID:    {AccountID: stickyAccount.ID, CurrentConcurrency: 1, LoadRate: 100},
			availableAccount.ID: {AccountID: availableAccount.ID, CurrentConcurrency: 0, LoadRate: 0},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{stickyAccount, availableAccount}},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_advanced_continuation",
		"advanced-continuation-session",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, stickyAccount.ID, selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, stickyAccount.ID, selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ContinuationStickyErrorsFailClosed(t *testing.T) {
	schedulerModes := []struct {
		name          string
		enabled       string
		noConcurrency bool
	}{
		{name: "legacy", enabled: "false"},
		{name: "legacy_no_concurrency", enabled: "false", noConcurrency: true},
		{name: "advanced", enabled: "true"},
	}
	failureModes := []string{"redis_get", "account_lookup", "db_recheck"}

	for _, schedulerMode := range schedulerModes {
		schedulerMode := schedulerMode
		t.Run(schedulerMode.name, func(t *testing.T) {
			for _, failureMode := range failureModes {
				failureMode := failureMode
				t.Run(failureMode, func(t *testing.T) {
					resetOpenAIAdvancedSchedulerSettingCacheForTest()
					t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

					ctx := context.Background()
					groupID := int64(10115)
					sessionHash := schedulerMode.name + "-" + failureMode + "-continuation"
					stickyKey := "openai:" + sessionHash
					stickyAccount := openAITestAccountWithGroupIfUnset(Account{
						ID:          36401,
						Platform:    PlatformOpenAI,
						Type:        AccountTypeAPIKey,
						Status:      StatusActive,
						Schedulable: true,
						Concurrency: 1,
						Priority:    0,
						Extra: map[string]any{
							"openai_apikey_responses_websockets_v2_enabled": true,
						},
					}, groupID)
					availableAccount := openAITestAccountWithGroupIfUnset(Account{
						ID:          36402,
						Platform:    PlatformOpenAI,
						Type:        AccountTypeAPIKey,
						Status:      StatusActive,
						Schedulable: true,
						Concurrency: 1,
						Priority:    1,
						Extra: map[string]any{
							"openai_apikey_responses_websockets_v2_enabled": true,
						},
					}, groupID)
					lookupErr := fmt.Errorf("%s continuation sticky lookup failed", failureMode)
					cache := &schedulerTestGatewayCache{
						sessionBindings: map[string]int64{stickyKey: stickyAccount.ID},
						getErrors:       map[string]error{},
					}
					repo := schedulerTestOpenAIAccountRepo{
						accounts:  []Account{stickyAccount, availableAccount},
						getErrors: map[int64]error{},
					}
					concurrencySpy := &cleanRelayConcurrencyCacheSpy{
						schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
							acquireResults: map[int64]bool{
								stickyAccount.ID:    true,
								availableAccount.ID: true,
							},
						},
					}
					cfg := newSchedulerTestOpenAIWSV2Config()
					cfg.Gateway.Scheduling.LoadBatchEnabled = false
					svc := &OpenAIGatewayService{
						accountRepo:      repo,
						cache:            cache,
						cfg:              cfg,
						rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(schedulerMode.enabled),
					}
					if !schedulerMode.noConcurrency {
						svc.concurrencyService = NewConcurrencyService(concurrencySpy)
					}

					switch failureMode {
					case "redis_get":
						cache.getErrors[stickyKey] = lookupErr
					case "account_lookup":
						repo.getErrors[stickyAccount.ID] = lookupErr
						svc.accountRepo = repo
					case "db_recheck":
						repo.getErrors[stickyAccount.ID] = lookupErr
						svc.accountRepo = repo
						stickySnapshot := stickyAccount
						availableSnapshot := availableAccount
						snapshotCache := &openAISnapshotCacheStub{
							snapshotAccounts: []*Account{&stickySnapshot, &availableSnapshot},
							accountsByID: map[int64]*Account{
								stickySnapshot.ID:    &stickySnapshot,
								availableSnapshot.ID: &availableSnapshot,
							},
						}
						svc.schedulerSnapshot = &SchedulerSnapshotService{cache: snapshotCache}
					}

					selection, decision, err := svc.SelectAccountWithScheduler(
						ctx,
						&groupID,
						"resp_unbound_"+failureMode,
						sessionHash,
						"gpt-5.1",
						nil,
						OpenAIUpstreamTransportResponsesWebsocketV2,
						false,
					)
					require.ErrorIs(t, err, lookupErr)
					require.Nil(t, selection)
					require.Zero(t, decision.SelectedAccountID)
					require.Empty(t, concurrencySpy.acquireCalls, "a continuation lookup failure must not probe another account")
					require.Equal(t, stickyAccount.ID, cache.sessionBindings[stickyKey])
					require.Zero(t, cache.deletedSessions[stickyKey])
				})
			}
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_KnownContinuationOwnerUnavailableDoesNotSwitch(t *testing.T) {
	for _, schedulerMode := range []struct {
		name    string
		enabled string
	}{
		{name: "legacy", enabled: "false"},
		{name: "advanced", enabled: "true"},
	} {
		schedulerMode := schedulerMode
		t.Run(schedulerMode.name, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

			ctx := context.Background()
			groupID := int64(10116)
			rateLimitedUntil := time.Now().Add(time.Hour)
			owner := openAITestAccountWithGroupIfUnset(Account{
				ID:               36501,
				Platform:         PlatformOpenAI,
				Type:             AccountTypeAPIKey,
				Status:           StatusActive,
				Schedulable:      true,
				Concurrency:      1,
				Priority:         0,
				RateLimitResetAt: &rateLimitedUntil,
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			}, groupID)
			available := openAITestAccountWithGroupIfUnset(Account{
				ID:          36502,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			}, groupID)
			cache := &schedulerTestGatewayCache{}
			concurrencySpy := &cleanRelayConcurrencyCacheSpy{
				schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
					acquireResults: map[int64]bool{available.ID: true},
				},
			}
			svc := &OpenAIGatewayService{
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{owner, available}},
				cache:              cache,
				cfg:                newSchedulerTestOpenAIWSV2Config(),
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(schedulerMode.enabled),
				concurrencyService: NewConcurrencyService(concurrencySpy),
			}
			responseID := "resp_known_owner_unavailable_" + schedulerMode.name
			store := svc.getOpenAIWSStateStore()
			require.NoError(t, store.BindResponseAccount(ctx, groupID, responseID, owner.ID, time.Hour))

			selection, decision, err := svc.SelectAccountWithScheduler(
				ctx,
				&groupID,
				responseID,
				"",
				"gpt-5.1",
				nil,
				OpenAIUpstreamTransportResponsesWebsocketV2,
				false,
			)
			require.ErrorIs(t, err, errOpenAIContinuationAccountUnavailable)
			require.False(t, IsOpenAIWSContinuationPermanentError(err), "rate-limited owners must remain retryable")
			require.Nil(t, selection)
			require.Zero(t, decision.SelectedAccountID)
			require.Empty(t, concurrencySpy.acquireCalls, "known continuation ownership must stop fallback before probing another account")
			boundAccountID, getErr := store.GetResponseAccountStrict(ctx, groupID, responseID)
			require.NoError(t, getErr)
			require.Equal(t, owner.ID, boundAccountID)
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithSchedulerForImages_FallbackPersistsBasicDispatchRequirement(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10107)
	account := Account{
		ID:          36003,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&groupID,
		"",
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityNative,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	require.NotNil(t, selection.OpenAIDispatchRequirements)
	require.Equal(t, "gpt-image-2", selection.OpenAIDispatchRequirements.RequestedModel)
	require.Equal(t, OpenAIUpstreamTransportHTTPSSE, selection.OpenAIDispatchRequirements.RequiredTransport)
	require.Equal(t, OpenAIImagesCapabilityBasic, selection.OpenAIDispatchRequirements.RequiredImageCapability)

	latest, err := svc.RevalidateSelectedOpenAIAccountForDispatch(ctx, &groupID, selection.Account, *selection.OpenAIDispatchRequirements)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, account.ID, latest.ID)
}

func TestOpenAIGatewayService_SelectAccountWithSchedulerForImages_AccountShareModeOAuthPersistsBasicDispatchRequirement(t *testing.T) {
	modeGroupID := int64(10110)
	consumerUserID := int64(5581)
	apiKeyID := int64(20104)
	account := Account{
		ID:          36004,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	shareRepo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{ID: 2, AccountID: account.ID, ConsumerUserID: consumerUserID, APIKeyID: apiKeyID},
		listing:    &AccountShareListing{ID: 2, AccountID: account.ID, OwnerUserID: 1, Status: AccountShareListingStatusActive, AllowedModels: []string{"gpt-image-2"}, PerUserConcurrency: 1},
	}
	concurrencyService, accountShareService := newAccountShareRuntimeLeaseTestServices(shareRepo)
	svc := &OpenAIGatewayService{
		accountRepo:             stubOpenAIAccountRepo{accounts: []Account{account}},
		accountShareModeService: accountShareService,
		concurrencyService:      concurrencyService,
	}
	ctx := WithAccountShareModeRequest(context.Background(), consumerUserID, apiKeyID)

	selection, decision, err := svc.SelectAccountWithSchedulerForImages(
		ctx,
		&modeGroupID,
		"",
		"gpt-image-2",
		nil,
		OpenAIImagesCapabilityNative,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, openAIAccountScheduleLayerAccountShareMode, decision.Layer)
	if selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	}

	require.NotNil(t, selection.OpenAIDispatchRequirements)
	require.Equal(t, OpenAIImagesCapabilityBasic, selection.OpenAIDispatchRequirements.RequiredImageCapability)

	latest, err := svc.RevalidateSelectedOpenAIAccountForDispatch(ctx, &modeGroupID, selection.Account, *selection.OpenAIDispatchRequirements)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, account.ID, latest.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_RequiredWSV2_SkipsHTTPOnlyAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10108)
	accounts := []Account{
		{
			ID:          36011,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          36012,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(36012), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DefaultDisabled_RequiredWSV2_NoAvailableAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10109)
	accounts := []Account{
		{
			ID:          36021,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.ErrorContains(t, err, "no available OpenAI accounts")
	require.Nil(t, selection)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_EnabledUsesAdvancedPreviousResponseRouting(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	ctx := context.Background()
	groupID := int64(10107)
	accounts := []Account{
		{
			ID:          37001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          37002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}
	accounts = openAITestAccountsWithGroupIfUnset(accounts, groupID)
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_enabled_001", 37001, time.Hour))
	require.True(t, svc.isOpenAIAdvancedSchedulerEnabled(ctx))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_enabled_001",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(37001), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
}

func TestOpenAIGatewayService_OpenAIAccountSchedulerMetrics_DisabledNoOp(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	svc := &OpenAIGatewayService{}
	ttft := 120
	svc.ReportOpenAIAccountScheduleResult(10, true, &ttft)
	svc.RecordOpenAIAccountSwitch()

	snapshot := svc.SnapshotOpenAIAccountSchedulerMetrics()
	require.Equal(t, OpenAIAccountSchedulerMetricsSnapshot{}, snapshot)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyRateLimitedAccountFallsBackToFreshCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10101)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleSticky := &Account{ID: 31001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0}
	staleBackup := &Account{ID: 31002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	freshSticky := &Account{ID: 31001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, RateLimitResetAt: &rateLimitedUntil}
	freshBackup := &Account{ID: 31002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	staleSticky = openAITestAccountPtrWithGroupIfUnset(staleSticky, groupID)
	staleBackup = openAITestAccountPtrWithGroupIfUnset(staleBackup, groupID)
	freshSticky = openAITestAccountPtrWithGroupIfUnset(freshSticky, groupID)
	freshBackup = openAITestAccountPtrWithGroupIfUnset(freshBackup, groupID)
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_rate_limited": 31001}}
	snapshotCache := &openAISnapshotCacheStub{snapshotAccounts: []*Account{staleSticky, staleBackup}, accountsByID: map[int64]*Account{31001: freshSticky, 31002: freshBackup}}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{*freshSticky, *freshBackup}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot:  snapshotService,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_rate_limited", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(31002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_SkipsFreshlyRateLimitedSnapshotCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10102)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	stalePrimary := &Account{ID: 32001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0}
	staleSecondary := &Account{ID: 32002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	freshPrimary := &Account{ID: 32001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, RateLimitResetAt: &rateLimitedUntil}
	freshSecondary := &Account{ID: 32002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	stalePrimary = openAITestAccountPtrWithGroupIfUnset(stalePrimary, groupID)
	staleSecondary = openAITestAccountPtrWithGroupIfUnset(staleSecondary, groupID)
	freshPrimary = openAITestAccountPtrWithGroupIfUnset(freshPrimary, groupID)
	freshSecondary = openAITestAccountPtrWithGroupIfUnset(freshSecondary, groupID)
	snapshotCache := &openAISnapshotCacheStub{snapshotAccounts: []*Account{stalePrimary, staleSecondary}, accountsByID: map[int64]*Account{32001: freshPrimary, 32002: freshSecondary}}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{*freshPrimary, *freshSecondary}},
		cfg:               &config.Config{},
		rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot: snapshotService,
	}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(32002), account.ID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyDBRuntimeRecheckSkipsStaleCachedAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10103)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleSticky := &Account{ID: 33001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0}
	staleBackup := &Account{ID: 33002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	dbSticky := Account{ID: 33001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, RateLimitResetAt: &rateLimitedUntil}
	dbBackup := Account{ID: 33002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	staleSticky = openAITestAccountPtrWithGroupIfUnset(staleSticky, groupID)
	staleBackup = openAITestAccountPtrWithGroupIfUnset(staleBackup, groupID)
	dbSticky = openAITestAccountWithGroupIfUnset(dbSticky, groupID)
	dbBackup = openAITestAccountWithGroupIfUnset(dbBackup, groupID)
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session_hash_db_runtime_recheck": 33001}}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{staleSticky, staleBackup},
		accountsByID:     map[int64]*Account{33001: staleSticky, 33002: staleBackup},
	}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{dbSticky, dbBackup}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot:  snapshotService,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_db_runtime_recheck", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(33002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyClearsAccountMovedOutOfGroup(t *testing.T) {
	ctx := context.Background()
	freeGroupID := int64(1197)
	plusGroupID := int64(18)
	sessionHash := "session_hash_moved_out_of_group"

	movedOut := Account{
		ID:            234206,
		Platform:      PlatformOpenAI,
		Type:          AccountTypeOAuth,
		Status:        StatusActive,
		Schedulable:   true,
		Concurrency:   1,
		Priority:      0,
		GroupIDs:      []int64{plusGroupID},
		AccountGroups: []AccountGroup{{AccountID: 234206, GroupID: plusGroupID}},
	}
	freeBackup := Account{
		ID:            234207,
		Platform:      PlatformOpenAI,
		Type:          AccountTypeOAuth,
		Status:        StatusActive,
		Schedulable:   true,
		Concurrency:   1,
		Priority:      1,
		GroupIDs:      []int64{freeGroupID},
		AccountGroups: []AccountGroup{{AccountID: 234207, GroupID: freeGroupID}},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{"openai:" + sessionHash: movedOut.ID},
	}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{&movedOut, &freeBackup},
		accountsByID:     map[int64]*Account{movedOut.ID: &movedOut, freeBackup.ID: &freeBackup},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{movedOut, freeBackup}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&freeGroupID,
		"",
		sessionHash,
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, freeBackup.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, 1, cache.deletedSessions["openai:"+sessionHash])
	require.Equal(t, freeBackup.ID, cache.sessionBindings["openai:"+sessionHash])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_DBRuntimeRecheckSkipsStaleCachedCandidate(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10104)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	stalePrimary := &Account{ID: 34001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0}
	staleSecondary := &Account{ID: 34002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	dbPrimary := Account{ID: 34001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, RateLimitResetAt: &rateLimitedUntil}
	dbSecondary := Account{ID: 34002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5}
	stalePrimary = openAITestAccountPtrWithGroupIfUnset(stalePrimary, groupID)
	staleSecondary = openAITestAccountPtrWithGroupIfUnset(staleSecondary, groupID)
	dbPrimary = openAITestAccountWithGroupIfUnset(dbPrimary, groupID)
	dbSecondary = openAITestAccountWithGroupIfUnset(dbSecondary, groupID)
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{stalePrimary, staleSecondary},
		accountsByID:     map[int64]*Account{34001: stalePrimary, 34002: staleSecondary},
	}
	snapshotService := &SchedulerSnapshotService{cache: snapshotCache}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, dbSecondary}},
		cfg:               &config.Config{},
		rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot: snapshotService,
	}

	account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(34002), account.ID)
}

func TestOpenAIGatewayService_SelectAccountForModelWithExclusions_CandidateRefreshErrorsUseFirstErrorOnlyWithoutSuccess(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10114)
	accounts := openAITestAccountsWithGroupIfUnset([]Account{
		{ID: 34011, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 34012, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1},
	}, groupID)
	snapshotErr := errors.New("scheduler snapshot refresh unavailable")
	dbErr := errors.New("account database recheck unavailable")
	newService := func(secondDBErr error) *OpenAIGatewayService {
		snapshotCache := &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{&accounts[0], &accounts[1]},
			accountsByID:     map[int64]*Account{accounts[1].ID: &accounts[1]},
			getErrors:        map[int64]error{accounts[0].ID: snapshotErr},
		}
		snapshotFallbackRepo := schedulerTestOpenAIAccountRepo{
			accounts:  accounts,
			getErrors: map[int64]error{accounts[0].ID: snapshotErr},
		}
		return &OpenAIGatewayService{
			accountRepo: schedulerTestOpenAIAccountRepo{
				accounts:  accounts,
				getErrors: map[int64]error{accounts[1].ID: secondDBErr},
			},
			cfg: &config.Config{},
			schedulerSnapshot: &SchedulerSnapshotService{
				cache:       snapshotCache,
				accountRepo: snapshotFallbackRepo,
			},
		}
	}

	account, err := newService(nil).SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, accounts[1].ID, account.ID)

	account, err = newService(dbErr).SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
	require.Nil(t, account)
	require.ErrorIs(t, err, snapshotErr)
	require.NotErrorIs(t, err, dbErr)
	require.False(t, IsOpenAIAccountSelectionExhausted(err))
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_DBRecheckErrorFallsThroughButIsNotHidden(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	groupID := int64(10115)
	accounts := openAITestAccountsWithGroupIfUnset([]Account{
		{ID: 34021, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 34022, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1},
	}, groupID)
	dbErr := errors.New("account database recheck unavailable")
	newService := func(secondDBErr error) *OpenAIGatewayService {
		snapshotCache := &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{&accounts[0], &accounts[1]},
			accountsByID: map[int64]*Account{
				accounts[0].ID: &accounts[0],
				accounts[1].ID: &accounts[1],
			},
		}
		return &OpenAIGatewayService{
			accountRepo: schedulerTestOpenAIAccountRepo{
				accounts: accounts,
				getErrors: map[int64]error{
					accounts[0].ID: dbErr,
					accounts[1].ID: secondDBErr,
				},
			},
			cfg:                &config.Config{},
			rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
			schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache},
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		}
	}

	selection, _, err := newService(nil).SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, accounts[1].ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	selection, _, err = newService(dbErr).SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.Nil(t, selection)
	require.ErrorIs(t, err, dbErr)
	require.False(t, IsOpenAIAccountSelectionExhausted(err))
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LoadBatchErrorIsInfrastructure(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	loadBatchErr := errors.New("account load batch unavailable")
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          34023,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
		}}},
		cfg:              &config.Config{},
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			loadBatchErr: loadBatchErr,
		}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.Nil(t, selection)
	require.ErrorIs(t, err, loadBatchErr)
	require.False(t, IsOpenAIAccountSelectionExhausted(err))
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_RefreshErrorDoesNotSuppressValidBusyWaitPlan(t *testing.T) {
	tests := []struct {
		name                   string
		advancedSchedulerValue string
	}{
		{name: "legacy", advancedSchedulerValue: "false"},
		{name: "advanced", advancedSchedulerValue: "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

			ctx := context.Background()
			groupID := int64(10116)
			accounts := openAITestAccountsWithGroupIfUnset([]Account{
				{ID: 34031, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
				{ID: 34032, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1},
			}, groupID)
			refreshErr := errors.New("scheduler snapshot refresh unavailable")
			snapshotCache := &openAISnapshotCacheStub{
				snapshotAccounts: []*Account{&accounts[0], &accounts[1]},
				accountsByID:     map[int64]*Account{accounts[1].ID: &accounts[1]},
				getErrors:        map[int64]error{accounts[0].ID: refreshErr},
			}
			snapshotFallbackRepo := schedulerTestOpenAIAccountRepo{
				accounts:  accounts,
				getErrors: map[int64]error{accounts[0].ID: refreshErr},
			}
			svc := &OpenAIGatewayService{
				accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
				cfg:              &config.Config{},
				rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(tt.advancedSchedulerValue),
				schedulerSnapshot: &SchedulerSnapshotService{
					cache:       snapshotCache,
					accountRepo: snapshotFallbackRepo,
				},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
					acquireResults: map[int64]bool{accounts[1].ID: false},
				}),
			}

			selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.NotNil(t, selection.Account)
			require.Equal(t, accounts[1].ID, selection.Account.ID)
			require.False(t, selection.Acquired)
			require.NotNil(t, selection.WaitPlan)
			require.Equal(t, accounts[1].ID, selection.WaitPlan.AccountID)
		})
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_PreviousResponseSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(9)
	account := Account{
		ID:          1001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &schedulerTestGatewayCache{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 1800
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_001", account.ID, time.Hour))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_prev_001",
		"session_hash_001",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	require.Equal(t, account.ID, cache.sessionBindings["openai:session_hash_001"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LegacyUsesPreviousResponseBinding(t *testing.T) {
	ctx := context.Background()
	groupID := int64(9)
	boundAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          1002,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Priority:    10,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)
	preferredOtherAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          1003,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Priority:    0,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)
	cache := &schedulerTestGatewayCache{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true

	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{
			preferredOtherAccount,
			boundAccount,
		}},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("false"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_prev_legacy", boundAccount.ID, time.Hour))

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"resp_prev_legacy",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, boundAccount.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.True(t, decision.StickyPreviousHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10)
	account := Account{
		ID:          2001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_abc": account.ID,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_abc",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyBusyEscapesForReplaySafeRequest(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10100)
	accounts := []Account{
		{
			ID:          21001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          21002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_sticky_busy": 21001,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true

	concurrencyCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{
			21001: false, // sticky 账号已满
			21002: true,
		},
		waitCounts: map[int64]int{
			21001: 0, // 普通请求不应因粘性账号繁忙而进入长等待
		},
		loadMap: map[int64]*AccountLoadInfo{
			21001: {AccountID: 21001, LoadRate: 90, WaitingCount: 9},
			21002: {AccountID: 21002, LoadRate: 1, WaitingCount: 0},
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_sticky_busy",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21002), selection.Account.ID, "busy sticky account should escape to a healthy load-balance candidate")
	require.True(t, selection.Acquired)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ProbesOverflowWhenHybridTopKBusy(t *testing.T) {
	ctx := context.Background()
	accounts := make([]Account, 0, 40)
	acquireResults := make(map[int64]bool, 16)
	for i := int64(1); i <= 40; i++ {
		priority := 99
		if i <= 16 {
			priority = 0
			acquireResults[30000+i] = false
		}
		accounts = append(accounts, Account{
			ID:          30000 + i,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    priority,
		})
	}

	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: acquireResults}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		nil,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.True(t, selection.Acquired)
	require.Greater(t, selection.Account.ID, int64(30016), "all primary hybrid top-k accounts are busy, so scheduler should probe overflow candidates before waiting")
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_PrioritizesRawFreeSlotBeyondBoundedProbeSet(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	accounts := make([]Account, 0, 33)
	loadMap := make(map[int64]*AccountLoadInfo, 33)
	acquireResults := make(map[int64]bool, 33)
	for i := int64(1); i <= 33; i++ {
		accountID := int64(40000) + i
		priority := 0
		concurrency := 1
		currentConcurrency := 1
		if i == 33 {
			priority = 99
			concurrency = 2
			currentConcurrency = 1
			acquireResults[accountID] = true
		} else {
			acquireResults[accountID] = false
		}
		accounts = append(accounts, Account{
			ID:          accountID,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: concurrency,
			Priority:    priority,
		})
		loadMap[accountID] = &AccountLoadInfo{
			AccountID:          accountID,
			CurrentConcurrency: currentConcurrency,
			LoadRate:           100,
		}
	}

	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 7
	var acquireCalls []int64
	svc := &OpenAIGatewayService{
		accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
		cfg:              cfg,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			loadMap:        loadMap,
			acquireResults: acquireResults,
			acquireCalls:   &acquireCalls,
		}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		nil,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(40033), selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, 33, decision.CandidateCount)
	require.Equal(t, 16, decision.TopK)
	require.Equal(t, []int64{40033}, acquireCalls, "真实空槽账号必须在满槽候选之前被原子探测")
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_WaitsOnlyAfterExhaustingEveryCandidate(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	accounts := make([]Account, 0, 40)
	loadMap := make(map[int64]*AccountLoadInfo, 40)
	acquireResults := make(map[int64]bool, 40)
	for i := int64(1); i <= 40; i++ {
		accountID := int64(40700) + i
		accounts = append(accounts, Account{
			ID:          accountID,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    int(i / 17),
		})
		loadMap[accountID] = &AccountLoadInfo{AccountID: accountID, CurrentConcurrency: 0, LoadRate: 0}
		acquireResults[accountID] = false
	}

	var acquireCalls []int64
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 7
	svc := &OpenAIGatewayService{
		accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
		cfg:              cfg,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			loadMap:        loadMap,
			acquireResults: acquireResults,
			acquireCalls:   &acquireCalls,
		}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		nil,
		"",
		"exact-exhaustion",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, 40, decision.CandidateCount)
	require.Len(t, acquireCalls, 40, "生成 WaitPlan 前必须原子探测同组全部候选")
	seen := make(map[int64]struct{}, len(acquireCalls))
	for _, accountID := range acquireCalls {
		_, duplicate := seen[accountID]
		require.False(t, duplicate, "同一账号不应在一次精确耗尽中重复抢占")
		seen[accountID] = struct{}{}
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_ExhaustsGroupBeyondBoundedProbeSet(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	accounts := make([]Account, 0, 40)
	loadMap := make(map[int64]*AccountLoadInfo, 40)
	acquireResults := make(map[int64]bool, 40)
	for i := int64(1); i <= 40; i++ {
		accountID := int64(40500) + i
		priority := 99
		if i <= 32 {
			priority = 0
			acquireResults[accountID] = false
		}
		acquireResults[accountID] = false
		accounts = append(accounts, Account{
			ID:          accountID,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    priority,
		})
		loadMap[accountID] = &AccountLoadInfo{
			AccountID:          accountID,
			CurrentConcurrency: 0,
			LoadRate:           0,
		}
	}

	var acquireCalls []int64
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 7
	svc := &OpenAIGatewayService{
		accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
		cfg:              cfg,
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			loadMap:        loadMap,
			acquireResults: acquireResults,
			acquireCalls:   &acquireCalls,
			acquireOnCall:  33,
		}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		nil,
		"",
		"bounded-exhaustion",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.True(t, selection.Acquired)
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, 40, decision.CandidateCount)
	require.Equal(t, 16, decision.TopK)
	require.Len(t, acquireCalls, 33, "前 32 个原子抢占均失败时，必须继续探测同组剩余账号")
	require.Equal(t, acquireCalls[len(acquireCalls)-1], selection.Account.ID)
	seen := make(map[int64]struct{}, len(acquireCalls))
	for _, accountID := range acquireCalls {
		_, duplicate := seen[accountID]
		require.False(t, duplicate, "同一账号不应在一次调度中重复抢占")
		seen[accountID] = struct{}{}
	}
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_AcquireErrorFallsThroughToSuccessOrWaitPlan(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	accounts := []Account{
		{ID: 41001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 41002, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1},
	}
	acquireErr := errors.New("account slot cache failed")
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 1
	newService := func(secondAcquired bool) *OpenAIGatewayService {
		return &OpenAIGatewayService{
			accountRepo:      schedulerTestOpenAIAccountRepo{accounts: accounts},
			cfg:              cfg,
			rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
				loadMap: map[int64]*AccountLoadInfo{
					41001: {AccountID: 41001, CurrentConcurrency: 0, LoadRate: 0},
					41002: {AccountID: 41002, CurrentConcurrency: 0, LoadRate: 0},
				},
				acquireErrors:  map[int64]error{41001: acquireErr},
				acquireResults: map[int64]bool{41002: secondAcquired},
			}),
		}
	}

	selection, _, err := newService(true).SelectAccountWithScheduler(ctx, nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(41002), selection.Account.ID)
	require.True(t, selection.Acquired)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	selection, _, err = newService(false).SelectAccountWithScheduler(ctx, nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(41002), selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(41002), selection.WaitPlan.AccountID)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_AcquireErrorIsNotConvertedToWaitPlan(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	acquireErr := errors.New("account slot cache failed")
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{
			{ID: 41011, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1},
		}},
		cfg:              &config.Config{},
		rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			loadMap: map[int64]*AccountLoadInfo{
				41011: {AccountID: 41011, CurrentConcurrency: 0, LoadRate: 0},
			},
			acquireErrors: map[int64]error{41011: acquireErr},
		}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.Nil(t, selection)
	require.ErrorIs(t, err, acquireErr)
	require.False(t, IsOpenAIAccountSelectionExhausted(err))
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionStickyAcquireErrorUsesOtherBusyCandidateWaitPlan(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	groupID := int64(10113)
	accounts := openAITestAccountsWithGroupIfUnset([]Account{
		{ID: 42001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 42002, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1},
	}, groupID)
	acquireErr := errors.New("sticky account slot cache failed")
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 2
	newService := func(alternativeAcquired bool) *OpenAIGatewayService {
		return &OpenAIGatewayService{
			accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
			cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{
				"openai:sticky-acquire-error": 42001,
			}},
			cfg:              cfg,
			rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true"),
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
				loadMap: map[int64]*AccountLoadInfo{
					42001: {AccountID: 42001, CurrentConcurrency: 0, LoadRate: 0},
					42002: {AccountID: 42002, CurrentConcurrency: 0, LoadRate: 0},
				},
				acquireErrors:  map[int64]error{42001: acquireErr},
				acquireResults: map[int64]bool{42002: alternativeAcquired},
			}),
		}
	}

	selection, decision, err := newService(true).SelectAccountWithScheduler(
		ctx, &groupID, "", "sticky-acquire-error", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(42002), selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	selection, decision, err = newService(false).SelectAccountWithScheduler(
		ctx, &groupID, "", "sticky-acquire-error", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(42002), selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(42002), selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_SessionSticky_ForceHTTP(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1010)
	account := Account{
		ID:          2101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_ws_force_http": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_force_http": account.ID,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_force_http",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_RequiredWSV2_SkipsStickyHTTPAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1011)
	accounts := []Account{
		{
			ID:          2201,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          2202,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_ws_only": 2201,
		},
	}
	cfg := newSchedulerTestOpenAIWSV2Config()

	// 构造“HTTP-only 账号负载更低”的场景，验证 required transport 会强制过滤。
	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			2201: {AccountID: 2201, LoadRate: 0, WaitingCount: 0},
			2202: {AccountID: 2202, LoadRate: 90, WaitingCount: 5},
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"session_hash_ws_only",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(2202), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, 1, decision.CandidateCount)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_RequiredWSV2_NoAvailableAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1012)
	accounts := []Account{
		{
			ID:          2301,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                newSchedulerTestOpenAIWSV2Config(),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
	)
	require.Error(t, err)
	require.Nil(t, selection)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 0, decision.CandidateCount)
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LoadBalanceTopKFallback(t *testing.T) {
	ctx := context.Background()
	groupID := int64(11)
	accounts := []Account{
		{
			ID:          3001,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          3002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
		{
			ID:          3003,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
		},
	}

	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 0.4
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1.0
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 1.0
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 0.2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 0.1

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			3001: {AccountID: 3001, LoadRate: 95, WaitingCount: 8},
			3002: {AccountID: 3002, LoadRate: 20, WaitingCount: 1},
			3003: {AccountID: 3003, LoadRate: 10, WaitingCount: 0},
		},
		acquireResults: map[int64]bool{
			3003: false, // top1 失败，必须回退到 top-K 的下一候选
			3002: true,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(3002), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 3, decision.CandidateCount)
	require.Equal(t, 2, decision.TopK)
	require.Greater(t, decision.LoadSkew, 0.0)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_CandidateIndexFallbacksToFullSnapshot(t *testing.T) {
	ctx := context.Background()
	groupID := int64(18)
	indexedBucket := SchedulerBucket{GroupID: groupID, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}

	candidateOnly := Account{
		ID:          3101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-4o": "gpt-4o"},
		},
	}
	fullOnly := Account{
		ID:          3102,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-5.1": "gpt-5.1"},
		},
	}
	candidateOnly = openAITestAccountWithGroupIfUnset(candidateOnly, groupID)
	fullOnly = openAITestAccountWithGroupIfUnset(fullOnly, groupID)

	cache := &openAICandidateSnapshotCacheStub{
		openAISnapshotCacheStub: openAISnapshotCacheStub{
			snapshotAccounts: []*Account{&candidateOnly, &fullOnly},
			accountsByID: map[int64]*Account{
				candidateOnly.ID: &candidateOnly,
				fullOnly.ID:      &fullOnly,
			},
		},
		candidateAccounts: []*Account{&candidateOnly},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.IndexedBuckets = []string{indexedBucket.String()}
	cfg.Gateway.Scheduling.IndexedCandidateLimit = 256
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 1

	snapshot := NewSchedulerSnapshotService(cache, nil, schedulerTestOpenAIAccountRepo{accounts: []Account{candidateOnly, fullOnly}}, nil, cfg)
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{candidateOnly, fullOnly}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		schedulerSnapshot:  snapshot,
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		ctx,
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, fullOnly.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, 1, decision.CandidateCount)
	require.Equal(t, 1, cache.candidateHits)
	require.Equal(t, 1, cache.bypassHits)
	require.Zero(t, cache.fullHits)
}

func TestOpenAIGatewayService_OpenAIAccountSchedulerMetrics(t *testing.T) {
	ctx := context.Background()
	groupID := int64(12)
	account := Account{
		ID:          4001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &schedulerTestGatewayCache{
		sessionBindings: map[string]int64{
			"openai:session_hash_metrics": account.ID,
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session_hash_metrics", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	svc.ReportOpenAIAccountScheduleResult(account.ID, true, intPtrForTest(120))
	svc.RecordOpenAIAccountSwitch()

	snapshot := svc.SnapshotOpenAIAccountSchedulerMetrics()
	require.GreaterOrEqual(t, snapshot.SelectTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.StickySessionHitTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.AccountSwitchTotal, int64(1))
	require.GreaterOrEqual(t, snapshot.SchedulerLatencyMsAvg, float64(0))
	require.GreaterOrEqual(t, snapshot.StickyHitRatio, 0.0)
	require.GreaterOrEqual(t, snapshot.RuntimeStatsAccountCount, 1)
}

func intPtrForTest(v int) *int {
	return &v
}

func TestOpenAIAccountRuntimeStats_ReportAndSnapshot(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	stats.report(1001, true, nil)
	firstTTFT := 100
	stats.report(1001, false, &firstTTFT)
	secondTTFT := 200
	stats.report(1001, false, &secondTTFT)

	errorRate, ttft, hasTTFT := stats.snapshot(1001)
	require.True(t, hasTTFT)
	require.InDelta(t, 0.36, errorRate, 1e-9)
	require.InDelta(t, 120.0, ttft, 1e-9)
	require.Equal(t, 1, stats.size())
}

func TestOpenAIAccountRuntimeStats_ReportConcurrent(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()

	const (
		accountCount = 4
		workers      = 16
		iterations   = 800
	)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				accountID := int64(i%accountCount + 1)
				success := (i+worker)%3 != 0
				ttft := 80 + (i+worker)%40
				stats.report(accountID, success, &ttft)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, accountCount, stats.size())
	for accountID := int64(1); accountID <= accountCount; accountID++ {
		errorRate, ttft, hasTTFT := stats.snapshot(accountID)
		require.GreaterOrEqual(t, errorRate, 0.0)
		require.LessOrEqual(t, errorRate, 1.0)
		require.True(t, hasTTFT)
		require.Greater(t, ttft, 0.0)
	}
}

func TestSelectTopKOpenAICandidates(t *testing.T) {
	candidates := []openAIAccountCandidateScore{
		{
			account:  &Account{ID: 11, Priority: 2},
			loadInfo: &AccountLoadInfo{LoadRate: 10, WaitingCount: 1},
			score:    10.0,
		},
		{
			account:  &Account{ID: 12, Priority: 1},
			loadInfo: &AccountLoadInfo{LoadRate: 20, WaitingCount: 1},
			score:    9.5,
		},
		{
			account:  &Account{ID: 13, Priority: 1},
			loadInfo: &AccountLoadInfo{LoadRate: 30, WaitingCount: 0},
			score:    10.0,
		},
		{
			account:  &Account{ID: 14, Priority: 0},
			loadInfo: &AccountLoadInfo{LoadRate: 40, WaitingCount: 0},
			score:    8.0,
		},
	}

	top2 := selectTopKOpenAICandidates(candidates, 2)
	require.Len(t, top2, 2)
	require.Equal(t, int64(13), top2[0].account.ID)
	require.Equal(t, int64(11), top2[1].account.ID)

	topAll := selectTopKOpenAICandidates(candidates, 8)
	require.Len(t, topAll, len(candidates))
	require.Equal(t, int64(13), topAll[0].account.ID)
	require.Equal(t, int64(11), topAll[1].account.ID)
	require.Equal(t, int64(12), topAll[2].account.ID)
	require.Equal(t, int64(14), topAll[3].account.ID)
}

func TestSelectHybridTopKOpenAICandidates_IncludesFairLRUCandidates(t *testing.T) {
	now := time.Now()
	recent := now.Add(-1 * time.Minute)
	old := now.Add(-1 * time.Hour)
	candidates := make([]openAIAccountCandidateScore, 0, 10)
	for id := int64(1); id <= 10; id++ {
		lastUsed := recent
		score := 10.0
		if id >= 9 {
			lastUsed = old
			score = 9
		}
		candidates = append(candidates, openAIAccountCandidateScore{
			account:  &Account{ID: id, Priority: 0, LastUsedAt: &lastUsed},
			loadInfo: &AccountLoadInfo{LoadRate: 10, WaitingCount: 0},
			score:    score,
		})
	}

	legacyTop := selectTopKOpenAICandidates(candidates, 7)
	legacyIDs := make(map[int64]struct{}, len(legacyTop))
	for _, candidate := range legacyTop {
		legacyIDs[candidate.account.ID] = struct{}{}
	}
	require.NotContains(t, legacyIDs, int64(9))
	require.NotContains(t, legacyIDs, int64(10))

	hybridTop := selectHybridTopKOpenAICandidates(candidates, 7, OpenAIAccountScheduleRequest{
		SessionHash:    "fair-session",
		RequestedModel: "gpt-5.1",
	})
	hybridIDs := make(map[int64]struct{}, len(hybridTop))
	for _, candidate := range hybridTop {
		hybridIDs[candidate.account.ID] = struct{}{}
	}
	require.Len(t, hybridTop, 7)
	require.Contains(t, hybridIDs, int64(9))
	require.Contains(t, hybridIDs, int64(10))
}

func TestEffectiveOpenAIHybridTopK_ExpandsLargePools(t *testing.T) {
	require.Equal(t, 7, effectiveOpenAIHybridTopK(7, 10))
	require.Equal(t, 16, effectiveOpenAIHybridTopK(7, 50))
	require.Equal(t, 24, effectiveOpenAIHybridTopK(7, 300))
	require.Equal(t, 32, effectiveOpenAIHybridTopK(7, 1000))
	require.Equal(t, 40, effectiveOpenAIHybridTopK(40, 1000))
}

func TestOpenAIHybridFairCandidateCount_ConservativeShare(t *testing.T) {
	require.Equal(t, 2, openAIHybridFairCandidateCount(7, 1000))
	require.Equal(t, 4, openAIHybridFairCandidateCount(16, 1000))
	require.Equal(t, 7, openAIHybridFairCandidateCount(24, 1000))
	require.Equal(t, 9, openAIHybridFairCandidateCount(32, 1000))
}

func TestOpenAIHybridOverflowProbeCount_Bounded(t *testing.T) {
	require.Equal(t, 0, openAIHybridOverflowProbeCount(7, 7))
	require.Equal(t, 7, openAIHybridOverflowProbeCount(7, 1000))
	require.Equal(t, 32, openAIHybridOverflowProbeCount(32, 1000))
	require.Equal(t, 32, openAIHybridOverflowProbeCount(100, 1000))
}

func TestBuildOpenAIWeightedSelectionOrder_DeterministicBySessionSeed(t *testing.T) {
	candidates := []openAIAccountCandidateScore{
		{
			account:  &Account{ID: 101},
			loadInfo: &AccountLoadInfo{LoadRate: 10, WaitingCount: 0},
			score:    4.2,
		},
		{
			account:  &Account{ID: 102},
			loadInfo: &AccountLoadInfo{LoadRate: 30, WaitingCount: 1},
			score:    3.5,
		},
		{
			account:  &Account{ID: 103},
			loadInfo: &AccountLoadInfo{LoadRate: 50, WaitingCount: 2},
			score:    2.1,
		},
	}
	req := OpenAIAccountScheduleRequest{
		GroupID:        int64PtrForTest(99),
		SessionHash:    "session_seed_fixed",
		RequestedModel: "gpt-5.1",
	}

	first := buildOpenAIWeightedSelectionOrder(candidates, req)
	second := buildOpenAIWeightedSelectionOrder(candidates, req)
	require.Len(t, first, len(candidates))
	require.Len(t, second, len(candidates))
	for i := range first {
		require.Equal(t, first[i].account.ID, second[i].account.ID)
	}
}

func TestOpenAIGatewayService_SelectAccountWithScheduler_LoadBalanceDistributesAcrossSessions(t *testing.T) {
	ctx := context.Background()
	groupID := int64(15)
	accounts := []Account{
		{
			ID:          5101,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    0,
		},
		{
			ID:          5102,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    0,
		},
		{
			ID:          5103,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 3,
			Priority:    0,
		},
	}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 3
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 1

	concurrencyCache := schedulerTestConcurrencyCache{
		loadMap: map[int64]*AccountLoadInfo{
			5101: {AccountID: 5101, LoadRate: 20, WaitingCount: 1},
			5102: {AccountID: 5102, LoadRate: 20, WaitingCount: 1},
			5103: {AccountID: 5103, LoadRate: 20, WaitingCount: 1},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{sessionBindings: map[string]int64{}},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selected := make(map[int64]int, len(accounts))
	for i := 0; i < 60; i++ {
		sessionHash := fmt.Sprintf("session_hash_lb_%d", i)
		selection, decision, err := svc.SelectAccountWithScheduler(
			ctx,
			&groupID,
			"",
			sessionHash,
			"gpt-5.1",
			nil,
			OpenAIUpstreamTransportAny,
			false,
		)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.NotNil(t, selection.Account)
		require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
		selected[selection.Account.ID]++
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}

	// 多 session 应该能打散到多个账号，避免“恒定单账号命中”。
	require.GreaterOrEqual(t, len(selected), 2)
}

func TestDeriveOpenAISelectionSeed_NoAffinityAddsEntropy(t *testing.T) {
	req := OpenAIAccountScheduleRequest{
		RequestedModel: "gpt-5.1",
	}
	seed1 := deriveOpenAISelectionSeed(req)
	time.Sleep(1 * time.Millisecond)
	seed2 := deriveOpenAISelectionSeed(req)
	require.NotZero(t, seed1)
	require.NotZero(t, seed2)
	require.NotEqual(t, seed1, seed2)
}

func TestBuildOpenAIWeightedSelectionOrder_HandlesInvalidScores(t *testing.T) {
	candidates := []openAIAccountCandidateScore{
		{
			account:  &Account{ID: 901},
			loadInfo: &AccountLoadInfo{LoadRate: 5, WaitingCount: 0},
			score:    math.NaN(),
		},
		{
			account:  &Account{ID: 902},
			loadInfo: &AccountLoadInfo{LoadRate: 5, WaitingCount: 0},
			score:    math.Inf(1),
		},
		{
			account:  &Account{ID: 903},
			loadInfo: &AccountLoadInfo{LoadRate: 5, WaitingCount: 0},
			score:    -1,
		},
	}
	req := OpenAIAccountScheduleRequest{
		SessionHash: "seed_invalid_scores",
	}

	order := buildOpenAIWeightedSelectionOrder(candidates, req)
	require.Len(t, order, len(candidates))
	seen := map[int64]struct{}{}
	for _, item := range order {
		seen[item.account.ID] = struct{}{}
	}
	require.Len(t, seen, len(candidates))
}

func TestOpenAISelectionRNG_SeedZeroStillWorks(t *testing.T) {
	rng := newOpenAISelectionRNG(0)
	v1 := rng.nextUint64()
	v2 := rng.nextUint64()
	require.NotEqual(t, v1, v2)
	require.GreaterOrEqual(t, rng.nextFloat64(), 0.0)
	require.Less(t, rng.nextFloat64(), 1.0)
}

func TestOpenAIAccountCandidateHeap_PushPopAndInvalidType(t *testing.T) {
	h := openAIAccountCandidateHeap{}
	h.Push(openAIAccountCandidateScore{
		account:  &Account{ID: 7001},
		loadInfo: &AccountLoadInfo{LoadRate: 0, WaitingCount: 0},
		score:    1.0,
	})
	require.Equal(t, 1, h.Len())
	popped, ok := h.Pop().(openAIAccountCandidateScore)
	require.True(t, ok)
	require.Equal(t, int64(7001), popped.account.ID)
	require.Equal(t, 0, h.Len())

	require.Panics(t, func() {
		h.Push("bad_element_type")
	})
}

func TestClamp01_AllBranches(t *testing.T) {
	require.Equal(t, 0.0, clamp01(-0.2))
	require.Equal(t, 1.0, clamp01(1.3))
	require.Equal(t, 0.5, clamp01(0.5))
}

func TestCalcLoadSkewByMoments_Branches(t *testing.T) {
	require.Equal(t, 0.0, calcLoadSkewByMoments(1, 1, 1))
	// variance < 0 分支：sumSquares/count - mean^2 为负值时应钳制为 0。
	require.Equal(t, 0.0, calcLoadSkewByMoments(1, 0, 2))
	require.GreaterOrEqual(t, calcLoadSkewByMoments(6, 20, 3), 0.0)
}

func TestDefaultOpenAIAccountScheduler_ReportSwitchAndSnapshot(t *testing.T) {
	schedulerAny := newDefaultOpenAIAccountScheduler(&OpenAIGatewayService{}, nil)
	scheduler, ok := schedulerAny.(*defaultOpenAIAccountScheduler)
	require.True(t, ok)

	ttft := 100
	scheduler.ReportResult(1001, true, &ttft)
	scheduler.ReportSwitch()
	scheduler.metrics.recordSelect(OpenAIAccountScheduleDecision{
		Layer:             openAIAccountScheduleLayerLoadBalance,
		LatencyMs:         8,
		LoadSkew:          0.5,
		StickyPreviousHit: true,
	})
	scheduler.metrics.recordSelect(OpenAIAccountScheduleDecision{
		Layer:            openAIAccountScheduleLayerSessionSticky,
		LatencyMs:        6,
		LoadSkew:         0.2,
		StickySessionHit: true,
	})

	snapshot := scheduler.SnapshotMetrics()
	require.Equal(t, int64(2), snapshot.SelectTotal)
	require.Equal(t, int64(1), snapshot.StickyPreviousHitTotal)
	require.Equal(t, int64(1), snapshot.StickySessionHitTotal)
	require.Equal(t, int64(1), snapshot.LoadBalanceSelectTotal)
	require.Equal(t, int64(1), snapshot.AccountSwitchTotal)
	require.Greater(t, snapshot.SchedulerLatencyMsAvg, 0.0)
	require.Greater(t, snapshot.StickyHitRatio, 0.0)
	require.Greater(t, snapshot.LoadSkewAvg, 0.0)
}

func TestDefaultOpenAIAccountScheduler_FilterAllowsGrokCompatibleAccount(t *testing.T) {
	schedulerAny := newDefaultOpenAIAccountScheduler(&OpenAIGatewayService{}, nil)
	scheduler, ok := schedulerAny.(*defaultOpenAIAccountScheduler)
	require.True(t, ok)

	accounts := []Account{
		{
			ID:          9101,
			Platform:    PlatformGrok,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 2,
		},
	}

	filtered, loadReq := scheduler.filterOpenAIAccountsForLoadBalance(context.Background(), accounts, OpenAIAccountScheduleRequest{
		RequestedModel:    "grok-4.3",
		RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
	}, nil)

	require.Len(t, filtered, 1)
	require.Equal(t, PlatformGrok, filtered[0].Platform)
	require.Len(t, loadReq, 1)
	require.Equal(t, int64(9101), loadReq[0].ID)
}

func TestOpenAIGatewayService_SchedulerWrappersAndDefaults(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	svc := &OpenAIGatewayService{}
	ttft := 120
	svc.ReportOpenAIAccountScheduleResult(10, true, &ttft)
	svc.RecordOpenAIAccountSwitch()
	snapshot := svc.SnapshotOpenAIAccountSchedulerMetrics()
	require.Equal(t, OpenAIAccountSchedulerMetricsSnapshot{}, snapshot)
	require.Equal(t, 7, svc.openAIWSLBTopK())
	require.Equal(t, openaiStickySessionTTL, svc.openAIWSSessionStickyTTL())

	defaultWeights := svc.openAIWSSchedulerWeights()
	require.Equal(t, 1.0, defaultWeights.Priority)
	require.Equal(t, 1.0, defaultWeights.Load)
	require.Equal(t, 0.7, defaultWeights.Queue)
	require.Equal(t, 0.8, defaultWeights.ErrorRate)
	require.Equal(t, 0.5, defaultWeights.TTFT)
	require.Equal(t, 0.0, defaultWeights.QuotaHeadroom)

	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 9
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 180
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 0.2
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 0.3
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue = 0.4
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate = 0.5
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 0.6
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.QuotaHeadroom = 0.7
	svcWithCfg := &OpenAIGatewayService{cfg: cfg}

	require.Equal(t, 9, svcWithCfg.openAIWSLBTopK())
	require.Equal(t, 180*time.Second, svcWithCfg.openAIWSSessionStickyTTL())
	customWeights := svcWithCfg.openAIWSSchedulerWeights()
	require.Equal(t, 0.2, customWeights.Priority)
	require.Equal(t, 0.3, customWeights.Load)
	require.Equal(t, 0.4, customWeights.Queue)
	require.Equal(t, 0.5, customWeights.ErrorRate)
	require.Equal(t, 0.6, customWeights.TTFT)
	require.Equal(t, 0.7, customWeights.QuotaHeadroom)
}

func TestOpenAIQuotaHeadroomFactor_PrimaryUsedPercent(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	require.InDelta(t, 0.8, openAIQuotaHeadroomFactor(account, now), 0.0001)
}

func TestOpenAIQuotaHeadroomFactor_PrimaryMissingIsNeutral(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
}

func TestOpenAIQuotaHeadroomFactor_StaleSnapshotIsNeutral(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-9 * time.Hour).Format(time.RFC3339),
		},
	}

	require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
}

func TestOpenAIQuotaHeadroomFactor_PrimaryResetExpiredIsNeutral(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_primary_used_percent": 20.0,
			"codex_primary_reset_at":     now.Add(-time.Minute).Format(time.RFC3339),
			"codex_usage_updated_at":     now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
}

func TestOpenAIQuotaHeadroomFactor_SecondaryLowHeadroomDiscountsPrimary(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_primary_used_percent":   20.0,
			"codex_primary_reset_at":       now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_secondary_used_percent": 95.0,
			"codex_secondary_reset_at":     now.Add(time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at":       now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	require.InDelta(t, 0.4, openAIQuotaHeadroomFactor(account, now), 0.0001)
}

func TestOpenAIQuotaHeadroomFactor_FallbackNormalizedWindows(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_7d_used_percent":  30.0,
			"codex_7d_reset_at":      now.Add(24 * time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	require.InDelta(t, 0.7, openAIQuotaHeadroomFactor(account, now), 0.0001)
}

func TestOpenAIQuotaHeadroomFactor_ResetAfterSeconds(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Extra: map[string]any{
			"codex_primary_used_percent":        20.0,
			"codex_primary_reset_after_seconds": 120,
			"codex_usage_updated_at":            now.Add(-3 * time.Minute).Format(time.RFC3339),
		},
	}

	require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
}

func TestOpenAIQuotaHeadroomExtraNumber_InvalidValueMissing(t *testing.T) {
	value, ok := openAIQuotaHeadroomExtraNumber(map[string]any{
		"codex_primary_used_percent": "not-a-number",
	}, "codex_primary_used_percent")

	require.False(t, ok)
	require.Zero(t, value)
}

func TestDefaultOpenAIAccountScheduler_IsAccountTransportCompatible_Branches(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{}
	require.True(t, scheduler.isAccountTransportCompatible(nil, OpenAIUpstreamTransportAny))
	require.True(t, scheduler.isAccountTransportCompatible(nil, OpenAIUpstreamTransportHTTPSSE))
	require.False(t, scheduler.isAccountTransportCompatible(nil, OpenAIUpstreamTransportResponsesWebsocketV2))

	cfg := newSchedulerTestOpenAIWSV2Config()
	scheduler.service = &OpenAIGatewayService{cfg: cfg}
	account := &Account{
		ID:          8801,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	require.True(t, scheduler.isAccountTransportCompatible(account, OpenAIUpstreamTransportResponsesWebsocketV2))
}

func int64PtrForTest(v int64) *int64 {
	return &v
}
