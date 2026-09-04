package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type cleanRelayErrorGatewayCache struct {
	stubGatewayCache
	getErr error
}

type cleanRelayAccountRepoErrorStub struct {
	stubOpenAIAccountRepo
	getErrors map[int64]error
}

func (r cleanRelayAccountRepoErrorStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	if err := r.getErrors[id]; err != nil {
		return nil, err
	}
	return r.stubOpenAIAccountRepo.GetByID(ctx, id)
}

type cleanRelaySettingRepoStub struct {
	values map[string]string
}

type cleanRelayConcurrencyCacheSpy struct {
	schedulerTestConcurrencyCache
	acquireCalls []int64
	releaseCalls []int64
}

func (c *cleanRelayConcurrencyCacheSpy) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	c.acquireCalls = append(c.acquireCalls, accountID)
	return c.schedulerTestConcurrencyCache.AcquireAccountSlot(ctx, accountID, maxConcurrency, requestID)
}

func (c *cleanRelayConcurrencyCacheSpy) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	c.releaseCalls = append(c.releaseCalls, accountID)
	return nil
}

func (s *cleanRelaySettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *cleanRelaySettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	_ = ctx
	if s != nil && s.values != nil {
		if value, ok := s.values[key]; ok {
			return value, nil
		}
	}
	return "", ErrSettingNotFound
}

func (s *cleanRelaySettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *cleanRelaySettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	_ = ctx
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if s != nil && s.values != nil {
			if value, ok := s.values[key]; ok {
				result[key] = value
			}
		}
	}
	return result, nil
}

func (s *cleanRelaySettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *cleanRelaySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *cleanRelaySettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func (c *cleanRelayErrorGatewayCache) GetSessionString(ctx context.Context, groupID int64, sessionHash string) (string, error) {
	if c.getErr != nil {
		return "", c.getErr
	}
	return c.stubGatewayCache.GetSessionString(ctx, groupID, sessionHash)
}

func newCleanRelaySettingService(enabled bool) *SettingService {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	gatewayForwardingSF.Forget("gateway_forwarding")
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
	value := "false"
	if enabled {
		value = "true"
	}
	return NewSettingService(&cleanRelaySettingRepoStub{
		values: map[string]string{
			SettingKeyOpenAICleanRelayEnabled: value,
		},
	}, &config.Config{})
}

func newCleanRelayGinContext(apiKeyID int64, groupID int64) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set(openAICleanRelayInstallationField, "client-installation")
	req.Header.Set("session_id", "client-session")
	req.Header.Set("conversation_id", "client-conversation")
	req.Header.Set(openAIWSTurnStateHeader, "client-turn-state")
	c.Request = req
	c.Set("api_key", &APIKey{ID: apiKeyID, GroupID: &groupID})
	return c
}

func newCleanRelayOAuthAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
		},
	}
}

func TestOpenAICleanRelay_FirstCleanStartRewritesBodyAndHeaders(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, 202)
	account := newCleanRelayOAuthAccount(303)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"client-cache","previous_response_id":"resp_old","client_metadata":{"x-codex-installation-id":"client-body-installation"},"input":[{"type":"reasoning","encrypted_content":"sealed"},{"type":"input_text","text":"hello"}]}`)

	rewritten, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, account, body, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, state)
	require.True(t, state.CleanStart)
	require.True(t, state.bodyCleaned)
	require.False(t, state.headersCleaned)
	require.False(t, gjson.GetBytes(rewritten, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(rewritten, "input.0.encrypted_content").Exists())
	require.Equal(t, "input_text", gjson.GetBytes(rewritten, "input.0.type").String())
	require.Equal(t, state.Mapping.PromptCacheKey, gjson.GetBytes(rewritten, "prompt_cache_key").String())
	require.Equal(t, state.Mapping.InstallationID, gjson.GetBytes(rewritten, "client_metadata.x-codex-installation-id").String())
	require.Len(t, cache.stringBindings, 1)

	headers := http.Header{}
	headers.Set(openAIWSTurnStateHeader, "client-turn-state")
	svc.applyOpenAICleanRelayWSHeaders(ctx, c, account, headers)
	require.Equal(t, state.Mapping.InstallationID, headers.Get(openAICleanRelayInstallationField))
	require.Equal(t, state.Mapping.SessionID, headers.Get("session_id"))
	require.Equal(t, state.Mapping.ConversationID, headers.Get("conversation_id"))
	require.Empty(t, headers.Get(openAIWSTurnStateHeader))
	require.True(t, state.headersCleaned)
}

func TestOpenAICleanRelay_CacheReadErrorFailsFast(t *testing.T) {
	ctx := context.Background()
	cacheErr := errors.New("redis unavailable")
	svc := &OpenAIGatewayService{
		cache:          &cleanRelayErrorGatewayCache{getErr: cacheErr},
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, 202)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"client-cache","previous_response_id":"resp_old"}`)
	rewritten, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, newCleanRelayOAuthAccount(303), body, body)

	require.Error(t, err)
	require.ErrorIs(t, err, cacheErr)
	require.Nil(t, state)
	require.False(t, changed)
	require.JSONEq(t, string(body), string(rewritten))
}

func TestOpenAICleanRelay_NonOAuthAttemptClearsPreviousAccountState(t *testing.T) {
	ctx := context.Background()
	svc := &OpenAIGatewayService{
		cache:          &stubGatewayCache{},
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()
	c := newCleanRelayGinContext(101, 202)
	oauthAccount := newCleanRelayOAuthAccount(303)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"client-cache","input":"hello"}`)

	_, state, _, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, oauthAccount, body, body)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, getOpenAICleanRelayState(c))

	apiKeyAccount := &Account{ID: 404, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	rewritten, nextState, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, apiKeyAccount, body, body)
	require.NoError(t, err)
	require.Nil(t, nextState)
	require.False(t, changed)
	require.Equal(t, body, rewritten)
	require.Nil(t, getOpenAICleanRelayState(c))

	req := httptest.NewRequest(http.MethodPost, "https://example.test/v1/responses", nil)
	svc.applyOpenAICleanRelayHeaders(ctx, c, apiKeyAccount, req)
	require.Empty(t, req.Header.Get(openAICleanRelayInstallationField))
	require.Empty(t, req.Header.Get("session-id"))
	require.Empty(t, req.Header.Get("session_id"))
	require.Empty(t, req.Header.Get("conversation_id"))
}

func TestOpenAICleanRelayHeadersRejectStaleAccountState(t *testing.T) {
	ctx := context.Background()
	svc := &OpenAIGatewayService{settingService: newCleanRelaySettingService(true)}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()
	c := newCleanRelayGinContext(101, 202)
	state := &openAICleanRelayState{Mapping: newOpenAICleanRelayMapping(303, 1, "relay-installation")}
	setOpenAICleanRelayState(c, state)

	req := httptest.NewRequest(http.MethodPost, "https://example.test/v1/responses", nil)
	svc.applyOpenAICleanRelayHeaders(ctx, c, newCleanRelayOAuthAccount(404), req)

	require.Nil(t, getOpenAICleanRelayState(c))
	require.Empty(t, req.Header.Get(openAICleanRelayInstallationField))
	require.Empty(t, req.Header.Get("session-id"))
	require.Empty(t, req.Header.Get("session_id"))
	require.Empty(t, req.Header.Get("conversation_id"))
}

func TestOpenAICleanRelay_CompactDoesNotInjectBodyClientMetadata(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, 202)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	c.Request.Header.Set(openAICleanRelayInstallationField, "client-installation")
	c.Request.Header.Set("session_id", "client-session")
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"client-cache","client_metadata":{"x-codex-installation-id":"client-body-installation"},"input":[{"type":"input_text","text":"hello"}]}`)

	rewritten, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, newCleanRelayOAuthAccount(303), body, body)

	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, state)
	require.False(t, state.AllowBodyClientMetadata)
	require.False(t, gjson.GetBytes(rewritten, "client_metadata").Exists())
	require.Equal(t, state.Mapping.PromptCacheKey, gjson.GetBytes(rewritten, "prompt_cache_key").String())
}

func TestOpenAICleanRelay_RawRewriteReplacesInvalidClientMetadata(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, 202)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"client-cache","client_metadata":"bad","input":[{"type":"input_text","text":"hello"}]}`)

	rewritten, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, newCleanRelayOAuthAccount(303), body, body)

	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, state)
	require.Equal(t, state.Mapping.InstallationID, gjson.GetBytes(rewritten, "client_metadata.x-codex-installation-id").String())
	require.Equal(t, state.Mapping.PromptCacheKey, gjson.GetBytes(rewritten, "prompt_cache_key").String())
}

func TestOpenAICleanRelay_RawRewritePreservesLargeMetadataIntegers(t *testing.T) {
	state := &openAICleanRelayState{
		Mapping: openAICleanRelayMapping{
			InstallationID: "relay-installation",
			SessionID:      "relay-session",
			PromptCacheKey: "relay-cache",
		},
		AllowBodyClientMetadata: true,
	}
	body := []byte(`{"client_metadata":{"sequence":9007199254740993123,"x-codex-turn-metadata":"{\"sequence\":9007199254740993123}"}}`)

	rewritten, changed, err := applyOpenAICleanRelayMappingToRawBody(body, state)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "9007199254740993123", gjson.GetBytes(rewritten, "client_metadata.sequence").Raw)
	embedded := gjson.GetBytes(rewritten, "client_metadata.x-codex-turn-metadata").String()
	require.Equal(t, "9007199254740993123", gjson.Get(embedded, "sequence").Raw)
	require.Equal(t, state.Mapping.SessionID, gjson.Get(embedded, "session_id").String())
}

func TestOpenAICleanRelay_PreselectsCachedAccountBeforeScheduler(t *testing.T) {
	ctx := context.Background()
	groupID := int64(202)
	cachedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(303), groupID)
	otherAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(404), groupID)
	otherAccount.Priority = -10
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{cachedAccount, otherAccount}},
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache","previous_response_id":"resp_old"}`)
	_, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, &cachedAccount, body, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, state)

	c = newCleanRelayGinContext(101, groupID)
	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"resp_old",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, cachedAccount.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerCleanRelay, decision.Layer)
	require.True(t, decision.StickySessionHit)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAICleanRelay_BusyMappedAccountFallsBackToAvailableAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(202)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(303), groupID)
	busyAlternative := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(404), groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(505), groupID)
	mappedAccount.Priority = -10
	busyAlternative.Priority = 0
	availableAccount.Priority = 10

	cache := &stubGatewayCache{
		sessionBindings: map[string]int64{"openai:client-session": mappedAccount.ID},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	concurrencyCache := &cleanRelayConcurrencyCacheSpy{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{
				mappedAccount.ID:    false,
				busyAlternative.ID:  false,
				availableAccount.ID: true,
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{mappedAccount, busyAlternative, availableAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		settingService:     newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	excludedIDs := map[int64]struct{}{999: {}}
	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"",
		"client-session",
		"codex-auto-review",
		"gpt-5.1",
		excludedIDs,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, availableAccount.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Nil(t, selection.WaitPlan)
	require.NotNil(t, selection.ReleaseFunc)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, []int64{mappedAccount.ID, busyAlternative.ID, availableAccount.ID}, concurrencyCache.acquireCalls)
	require.Len(t, excludedIDs, 1)
	_, mappedWasExcluded := excludedIDs[mappedAccount.ID]
	require.False(t, mappedWasExcluded)
	require.Equal(t, availableAccount.ID, cache.sessionBindings["openai:client-session"])
	selection.ReleaseFunc()
	require.Equal(t, []int64{availableAccount.ID}, concurrencyCache.releaseCalls)

	rewritten, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, selection.Account, body, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, state)
	require.True(t, state.CleanStart)
	require.Equal(t, availableAccount.ID, state.Mapping.AccountID)
	require.Equal(t, mapping.Epoch+1, state.Mapping.Epoch)
	require.Equal(t, state.Mapping.PromptCacheKey, gjson.GetBytes(rewritten, "prompt_cache_key").String())
}

func TestOpenAICleanRelay_FallbackSkipsNonCleanRelayAccounts(t *testing.T) {
	ctx := context.Background()
	groupID := int64(203)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(603), groupID)
	apiKeyAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          604,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
	}, groupID)
	availableOAuth := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(605), groupID)
	mappedAccount.Priority = -10
	availableOAuth.Priority = 10

	cache := &stubGatewayCache{}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	concurrencyCache := &cleanRelayConcurrencyCacheSpy{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{
				mappedAccount.ID:  false,
				apiKeyAccount.ID:  true,
				availableOAuth.ID: true,
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{mappedAccount, apiKeyAccount, availableOAuth}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		settingService:     newCleanRelaySettingService(true),
	}
	t.Cleanup(func() {
		svc.settingService = newCleanRelaySettingService(false)
	})

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, _, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, availableOAuth.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Equal(t, []int64{mappedAccount.ID, availableOAuth.ID}, concurrencyCache.acquireCalls)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAICleanRelay_AdvancedSchedulerFallsBackToAvailableAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)

	ctx := context.Background()
	groupID := int64(204)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(703), groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(704), groupID)
	mappedAccount.Priority = -10
	availableAccount.Priority = 10

	cache := &stubGatewayCache{}
	concurrencyCache := &cleanRelayConcurrencyCacheSpy{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			loadMap: map[int64]*AccountLoadInfo{
				mappedAccount.ID:    {AccountID: mappedAccount.ID, CurrentConcurrency: 1, LoadRate: 100},
				availableAccount.ID: {AccountID: availableAccount.ID, CurrentConcurrency: 0, LoadRate: 0},
			},
			acquireResults: map[int64]bool{
				mappedAccount.ID:    false,
				availableAccount.ID: true,
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{mappedAccount, availableAccount}},
		cache:              cache,
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		concurrencyService: NewConcurrencyService(concurrencyCache),
		settingService:     newCleanRelaySettingService(true),
	}
	t.Cleanup(func() {
		svc.settingService = newCleanRelaySettingService(false)
	})

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, availableAccount.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, []int64{mappedAccount.ID, availableAccount.ID}, concurrencyCache.acquireCalls)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAICleanRelay_FallbackAcquireErrorIsNotConvertedToMappedWait(t *testing.T) {
	ctx := context.Background()
	groupID := int64(205)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(803), groupID)
	otherAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(804), groupID)
	mappedAccount.Priority = -10
	otherAccount.Priority = 10
	acquireErr := errors.New("account slot cache failed")

	cache := &stubGatewayCache{}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	concurrencyCache := &cleanRelayConcurrencyCacheSpy{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{mappedAccount.ID: false},
			acquireErrors:  map[int64]error{otherAccount.ID: acquireErr},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{mappedAccount, otherAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		settingService:     newCleanRelaySettingService(true),
	}
	t.Cleanup(func() {
		svc.settingService = newCleanRelaySettingService(false)
	})

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, _, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)
	require.ErrorIs(t, err, acquireErr)
	require.Nil(t, selection)
}

func TestOpenAICleanRelay_AllAccountsBusyKeepsMappedWaitPlan(t *testing.T) {
	ctx := context.Background()
	groupID := int64(202)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(303), groupID)
	otherAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(404), groupID)
	mappedAccount.Priority = -10
	otherAccount.Priority = 10

	cache := &stubGatewayCache{
		sessionBindings: map[string]int64{"openai:client-session": mappedAccount.ID},
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	concurrencyCache := &cleanRelayConcurrencyCacheSpy{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			loadMap: map[int64]*AccountLoadInfo{
				mappedAccount.ID: {AccountID: mappedAccount.ID, LoadRate: 100},
				otherAccount.ID:  {AccountID: otherAccount.ID, LoadRate: 0},
			},
			acquireResults: map[int64]bool{
				mappedAccount.ID: false,
				otherAccount.ID:  false,
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{mappedAccount, otherAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		settingService:     newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"",
		"client-session",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, mappedAccount.ID, selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, mappedAccount.ID, selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerCleanRelay, decision.Layer)
	require.True(t, decision.StickySessionHit)
	require.Equal(t, []int64{mappedAccount.ID, otherAccount.ID}, concurrencyCache.acquireCalls)
	require.Empty(t, concurrencyCache.releaseCalls)
	require.Equal(t, mappedAccount.ID, cache.sessionBindings["openai:client-session"])
}

func TestOpenAICleanRelay_BusyMappedContinuationKeepsMappedWaitPlan(t *testing.T) {
	ctx := context.Background()
	groupID := int64(202)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(303), groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(404), groupID)
	mappedAccount.Priority = -10
	availableAccount.Priority = 10

	cache := &stubGatewayCache{}
	cfg := newOpenAIWSV2TestConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	concurrencyCache := &cleanRelayConcurrencyCacheSpy{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			loadMap: map[int64]*AccountLoadInfo{
				mappedAccount.ID:    {AccountID: mappedAccount.ID, LoadRate: 100},
				availableAccount.ID: {AccountID: availableAccount.ID, LoadRate: 0},
			},
			acquireResults: map[int64]bool{
				mappedAccount.ID:    false,
				availableAccount.ID: true,
			},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{mappedAccount, availableAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		settingService:     newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache","previous_response_id":"resp_old"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"resp_old",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, mappedAccount.ID, selection.Account.ID)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, mappedAccount.ID, selection.WaitPlan.AccountID)
	require.Equal(t, openAIAccountScheduleLayerCleanRelay, decision.Layer)
	require.True(t, decision.StickySessionHit)
	require.Equal(t, []int64{mappedAccount.ID}, concurrencyCache.acquireCalls)
	require.Empty(t, concurrencyCache.releaseCalls)
}

func TestOpenAICleanRelay_AccountShareModeUsesMembershipAccount(t *testing.T) {
	modeGroupID := int64(61711)
	privateGroupID := int64(61761)
	ownerUserID := int64(1)
	boundAccount := Account{
		ID:          416100,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 20,
		OwnerUserID: &ownerUserID,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"},
		},
		GroupIDs: []int64{privateGroupID},
		AccountGroups: []AccountGroup{
			{AccountID: 416100, GroupID: privateGroupID},
		},
	}
	shareRepo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{ID: 1, AccountID: boundAccount.ID, ConsumerUserID: 5580, APIKeyID: 20103},
		listing:    &AccountShareListing{ID: 1, OwnerUserID: 1, Status: AccountShareListingStatusActive, AllowedModels: []string{"gpt-5.5"}, PerUserConcurrency: 1},
	}
	concurrencyService, accountShareService := newAccountShareRuntimeLeaseTestServices(shareRepo)
	svc := &OpenAIGatewayService{
		accountRepo:             stubOpenAIAccountRepo{accounts: []Account{boundAccount}},
		accountShareModeService: accountShareService,
		concurrencyService:      concurrencyService,
	}
	baseCtx := context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, int64(5580))
	ctx := WithAccountShareModeRequest(baseCtx, 5580, 20103)
	c := newCleanRelayGinContext(5580, modeGroupID)

	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&modeGroupID,
		"",
		"",
		"gpt-5.5",
		"gpt-5.5",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		[]byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"ping"}]}`),
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, boundAccount.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerAccountShareMode, decision.Layer)
	require.Equal(t, boundAccount.ID, decision.SelectedAccountID)
	require.Equal(t, 1, shareRepo.bindingCalls)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAICleanRelay_PreselectFallsBackWhenCachedAccountUnavailable(t *testing.T) {
	ctx := context.Background()
	groupID := int64(202)
	unavailableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(303), groupID)
	apiKeyAccount := openAITestAccountWithGroupIfUnset(Account{
		ID:          403,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    -10,
	}, groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(404), groupID)
	unavailableAccount.Status = StatusDisabled
	availableAccount.Priority = 10
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{unavailableAccount, apiKeyAccount, availableAccount}},
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(unavailableAccount.ID, 1, resolveConvergedInstallationID(&unavailableAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, availableAccount.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAICleanRelay_UnavailableMappedContinuationDoesNotSwitchAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(206)
	unavailableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(903), groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(904), groupID)
	unavailableAccount.Status = StatusDisabled
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{unavailableAccount, availableAccount}},
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	t.Cleanup(func() {
		svc.settingService = newCleanRelaySettingService(false)
	})

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache","previous_response_id":"resp_old"}`)
	mapping := newOpenAICleanRelayMapping(unavailableAccount.ID, 1, resolveConvergedInstallationID(&unavailableAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, _, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"resp_old",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.True(t, IsOpenAIWSContinuationPermanentError(err), "a disabled mapped continuation owner requires a new conversation")
	require.Nil(t, selection)
}

func TestOpenAICleanRelay_RateLimitedMappedContinuationRemainsRetryable(t *testing.T) {
	ctx := context.Background()
	groupID := int64(208)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(1103), groupID)
	mappedAccount.RateLimitResetAt = &rateLimitedUntil
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(1104), groupID)
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{mappedAccount, availableAccount}},
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	t.Cleanup(func() {
		svc.settingService = newCleanRelaySettingService(false)
	})

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache","previous_response_id":"resp_rate_limited"}`)
	mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

	selection, _, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&groupID,
		"resp_rate_limited",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.False(t, IsOpenAIWSContinuationPermanentError(err), "rate-limited mapped owners must remain retryable")
	require.Nil(t, selection, "a continuation must not switch to the available account")
}

func TestOpenAICleanRelay_MappedAccountLookupErrorsAreReturned(t *testing.T) {
	ctx := context.Background()
	groupID := int64(207)
	mappedAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(1003), groupID)
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(1004), groupID)
	lookupErr := errors.New("account lookup failed")

	for _, tc := range []struct {
		name              string
		schedulerSnapshot *SchedulerSnapshotService
	}{
		{name: "direct repository lookup"},
		{
			name: "database recheck after snapshot lookup",
			schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
				snapshotAccounts: []*Account{&mappedAccount, &availableAccount},
				accountsByID: map[int64]*Account{
					mappedAccount.ID:    &mappedAccount,
					availableAccount.ID: &availableAccount,
				},
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &stubGatewayCache{}
			repo := cleanRelayAccountRepoErrorStub{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{mappedAccount, availableAccount}},
				getErrors:             map[int64]error{mappedAccount.ID: lookupErr},
			}
			svc := &OpenAIGatewayService{
				accountRepo:       repo,
				cache:             cache,
				schedulerSnapshot: tc.schedulerSnapshot,
				settingService:    newCleanRelaySettingService(true),
			}
			t.Cleanup(func() {
				svc.settingService = newCleanRelaySettingService(false)
			})

			c := newCleanRelayGinContext(101, groupID)
			body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
			mapping := newOpenAICleanRelayMapping(mappedAccount.ID, 1, resolveConvergedInstallationID(&mappedAccount))
			encoded, err := marshalOpenAICleanRelayMapping(mapping)
			require.NoError(t, err)
			cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-session")
			require.NoError(t, cache.SetSessionString(ctx, groupID, cacheKey, encoded, time.Hour))

			selection, _, err := svc.SelectAccountWithCleanRelayScheduler(
				ctx,
				c,
				&groupID,
				"",
				"",
				"codex-auto-review",
				"gpt-5.1",
				nil,
				OpenAIUpstreamTransportAny,
				false,
				body,
			)
			require.ErrorIs(t, err, lookupErr)
			require.Nil(t, selection)
		})
	}
}

func TestOpenAICleanRelay_PreselectUsesCurrentRouteGroupForCacheKey(t *testing.T) {
	ctx := context.Background()
	originalGroupID := int64(59)
	routeGroupID := int64(202)
	account := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(303), routeGroupID)
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, originalGroupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(account.ID, 1, resolveConvergedInstallationID(&account))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	routeCacheKey := openAICleanRelayCacheKey(101, routeGroupID, "client-installation", "client-session")
	originalCacheKey := openAICleanRelayCacheKey(101, originalGroupID, "client-installation", "client-session")
	require.NoError(t, cache.SetSessionString(ctx, routeGroupID, routeCacheKey, encoded, time.Hour))
	require.NoError(t, cache.SetSessionString(ctx, originalGroupID, originalCacheKey, `{"account_id":999}`, time.Hour))

	selection, decision, err := svc.SelectAccountWithCleanRelayScheduler(
		ctx,
		c,
		&routeGroupID,
		"",
		"",
		"codex-auto-review",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerCleanRelay, decision.Layer)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}

	_, state, changed, err := svc.applyOpenAICleanRelayToRawBody(ctx, c, selection.Account, body, body)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.True(t, changed)
	require.Equal(t, account.ID, state.Mapping.AccountID)
}
