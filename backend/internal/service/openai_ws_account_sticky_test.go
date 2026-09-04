package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_Hit(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          2,
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
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()

	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_1", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_1", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_RateLimitedFailsClosed(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	account := Account{
		ID:               12,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		RateLimitResetAt: &rateLimitedUntil,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_rl", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_rl", "gpt-5.1", nil, false)
	require.ErrorIs(t, err, errOpenAIContinuationAccountUnavailable)
	require.False(t, IsOpenAIWSContinuationPermanentError(err), "rate-limit windows must remain retryable")
	require.Nil(t, selection, "限额中的续聊账号不可切换到其他账号")
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_rl")
	require.NoError(t, getErr)
	require.Equal(t, account.ID, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_CleanRelayOAuthSkipsSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          22,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
		settingService:     newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_clean_relay", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_clean_relay", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection, "洁净中继开启时 OAuth 账号不应继续按客户端 previous_response_id 粘连")
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_clean_relay")
	require.NoError(t, getErr)
	require.Equal(t, account.ID, boundAccountID, "跳过调度粘连不应删除已有 response-account 绑定")
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_DBRuntimeRecheckRateLimitedFailsClosed(t *testing.T) {
	ctx := context.Background()
	groupID := int64(24)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleAccount := &Account{
		ID:          13,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	dbAccount := Account{
		ID:               13,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		RateLimitResetAt: &rateLimitedUntil,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	staleAccount = openAITestAccountPtrWithGroupIfUnset(staleAccount, groupID)
	dbAccount = openAITestAccountWithGroupIfUnset(dbAccount, groupID)
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	snapshotCache := &openAISnapshotCacheStub{
		accountsByID: map[int64]*Account{dbAccount.ID: staleAccount},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{dbAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache},
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_db_rl", dbAccount.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_db_rl", "gpt-5.1", nil, false)
	require.ErrorIs(t, err, errOpenAIContinuationAccountUnavailable)
	require.False(t, IsOpenAIWSContinuationPermanentError(err), "DB-observed rate limiting must remain retryable")
	require.Nil(t, selection, "DB 中已限流的续聊账号不可切换到其他账号")
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_db_rl")
	require.NoError(t, getErr)
	require.Equal(t, dbAccount.ID, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_RestartRequiredClassification(t *testing.T) {
	groupID := int64(27)
	tests := []struct {
		name           string
		mutate         func(*Account)
		requestedModel string
		requireCompact bool
	}{
		{
			name: "disabled account",
			mutate: func(account *Account) {
				account.Status = StatusDisabled
			},
			requestedModel: "gpt-5.1",
		},
		{
			name: "scheduling disabled",
			mutate: func(account *Account) {
				account.Schedulable = false
			},
			requestedModel: "gpt-5.1",
		},
		{
			name: "model no longer supported",
			mutate: func(account *Account) {
				account.Credentials = map[string]any{
					"model_mapping": map[string]any{"gpt-allowed": "gpt-allowed"},
				}
			},
			requestedModel: "gpt-blocked",
		},
		{
			name: "compact explicitly unsupported",
			mutate: func(account *Account) {
				account.Extra["openai_compact_supported"] = false
			},
			requestedModel: "gpt-5.1",
			requireCompact: true,
		},
		{
			name: "total quota exhausted",
			mutate: func(account *Account) {
				account.Extra["quota_limit"] = 10.0
				account.Extra["quota_used"] = 10.0
			},
			requestedModel: "gpt-5.1",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			account := openAITestAccountWithGroupIfUnset(Account{
				ID:          int64(100 + i),
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": true,
				},
			}, groupID)
			tc.mutate(&account)
			cache := &stubGatewayCache{}
			store := NewOpenAIWSStateStore(cache)
			svc := &OpenAIGatewayService{
				accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
				cache:              cache,
				cfg:                newOpenAIWSV2TestConfig(),
				openaiWSStateStore: store,
			}
			responseID := "resp_restart_required_" + tc.name
			require.NoError(t, store.BindResponseAccount(context.Background(), groupID, responseID, account.ID, time.Hour))

			selection, err := svc.SelectAccountByPreviousResponseID(
				context.Background(),
				&groupID,
				responseID,
				tc.requestedModel,
				nil,
				tc.requireCompact,
			)
			require.Nil(t, selection)
			require.ErrorIs(t, err, errOpenAIContinuationAccountUnavailable)
			require.True(t, IsOpenAIWSContinuationPermanentError(err))
		})
	}
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_Excluded(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          8,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_2", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_2", "gpt-5.1", map[int64]struct{}{account.ID: {}}, false)
	require.ErrorIs(t, err, errOpenAIContinuationAccountUnavailable)
	require.False(t, IsOpenAIWSContinuationPermanentError(err), "request-local exclusions must remain retryable")
	require.Nil(t, selection)
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_2")
	require.NoError(t, getErr)
	require.Equal(t, account.ID, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_ForceHTTPKeepsSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          11,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_ws_force_http":            true,
			"responses_websockets_v2_enabled": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_force_http", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_force_http", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID, "previous_response_id 账号亲和性不应因 HTTP 传输而失效")
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_OAuthForceHTTPIgnoresSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          111,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_ws_force_http":                         true,
			"openai_oauth_responses_websockets_v2_enabled": true,
		},
	}
	account = openAITestAccountWithGroupIfUnset(account, groupID)
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                newOpenAIWSV2TestConfig(),
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_oauth_force_http", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_oauth_force_http", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection, "OAuth HTTP transport cannot carry public Responses continuation state")

	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_oauth_force_http")
	require.NoError(t, getErr)
	require.Equal(t, account.ID, boundAccountID, "ignoring an incompatible legacy binding must not delete it")
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_BusyKeepsSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	accounts := []Account{
		{
			ID:          21,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          22,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}
	accounts = openAITestAccountsWithGroupIfUnset(accounts, groupID)

	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 30 * time.Second

	concurrencyCache := stubConcurrencyCache{
		acquireResults: map[int64]bool{
			21: false, // previous_response 命中的账号繁忙
			22: true,  // 次优账号可用（若回退会命中）
		},
		waitCounts: map[int64]int{
			21: 999,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_busy", 21, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_busy", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21), selection.Account.ID, "busy previous_response sticky account should remain selected")
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(21), selection.WaitPlan.AccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_AcquireErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	groupID := int64(24)
	account := openAITestAccountWithGroupIfUnset(Account{
		ID:          31,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)
	acquireErr := errors.New("account slot cache failed")
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:       cache,
		cfg:         newOpenAIWSV2TestConfig(),
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{
			acquireErrors: map[int64]error{account.ID: acquireErr},
		}),
		openaiWSStateStore: store,
	}
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_error", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_error", "gpt-5.1", nil, false)
	require.ErrorIs(t, err, acquireErr)
	require.Nil(t, selection)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_LookupErrorsAreReturnedAndBindingPreserved(t *testing.T) {
	ctx := context.Background()
	groupID := int64(25)
	account := openAITestAccountWithGroupIfUnset(Account{
		ID:          41,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}, groupID)
	lookupErr := errors.New("previous response account lookup failed")

	for _, tc := range []struct {
		name              string
		schedulerSnapshot *SchedulerSnapshotService
	}{
		{name: "direct repository lookup"},
		{
			name: "database recheck after snapshot lookup",
			schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
				accountsByID: map[int64]*Account{account.ID: &account},
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &stubGatewayCache{}
			store := NewOpenAIWSStateStore(cache)
			repo := cleanRelayAccountRepoErrorStub{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				getErrors:             map[int64]error{account.ID: lookupErr},
			}
			svc := &OpenAIGatewayService{
				accountRepo:        repo,
				cache:              cache,
				cfg:                newOpenAIWSV2TestConfig(),
				openaiWSStateStore: store,
				schedulerSnapshot:  tc.schedulerSnapshot,
			}
			responseID := "resp_prev_lookup_error"
			require.NoError(t, store.BindResponseAccount(ctx, groupID, responseID, account.ID, time.Hour))

			selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, responseID, "gpt-5.1", nil, false)
			require.ErrorIs(t, err, lookupErr)
			require.Nil(t, selection)

			boundAccountID, getErr := store.GetResponseAccountStrict(ctx, groupID, responseID)
			require.NoError(t, getErr)
			require.Equal(t, account.ID, boundAccountID, "transient lookup errors must not delete continuation ownership")
		})
	}
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_BindingLookupErrorIsReturned(t *testing.T) {
	lookupErr := errors.New("previous response binding cache unavailable")
	store := NewOpenAIWSStateStore(&openAIWSRouteResolutionCache{accountErr: lookupErr})
	svc := &OpenAIGatewayService{
		cfg:                newOpenAIWSV2TestConfig(),
		openaiWSStateStore: store,
	}
	groupID := int64(26)

	selection, err := svc.SelectAccountByPreviousResponseID(
		context.Background(),
		&groupID,
		"resp_prev_binding_error",
		"gpt-5.1",
		nil,
		false,
	)
	require.ErrorIs(t, err, lookupErr)
	require.Nil(t, selection)
}

func newOpenAIWSV2TestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	return cfg
}
