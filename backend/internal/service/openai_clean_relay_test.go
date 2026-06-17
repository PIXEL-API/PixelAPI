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

type cleanRelaySettingRepoStub struct {
	values map[string]string
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
	applyOpenAICleanRelayWSHeaders(c, headers)
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
		GroupIDs:    []int64{privateGroupID},
		AccountGroups: []AccountGroup{
			{AccountID: 416100, GroupID: privateGroupID},
		},
	}
	shareRepo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{ID: 1, AccountID: boundAccount.ID, ConsumerUserID: 5580, APIKeyID: 20103},
		listing:    &AccountShareListing{ID: 1, OwnerUserID: 1, Status: AccountShareListingStatusActive},
	}
	svc := &OpenAIGatewayService{
		accountRepo:             stubOpenAIAccountRepo{accounts: []Account{boundAccount}},
		accountShareModeService: &AccountShareModeService{repo: shareRepo},
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
	availableAccount := openAITestAccountWithGroupIfUnset(*newCleanRelayOAuthAccount(404), groupID)
	unavailableAccount.Status = StatusDisabled
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{unavailableAccount, availableAccount}},
		cache:          cache,
		settingService: newCleanRelaySettingService(true),
	}
	defer func() {
		svc.settingService = newCleanRelaySettingService(false)
	}()

	c := newCleanRelayGinContext(101, groupID)
	body := []byte(`{"model":"codex-auto-review","prompt_cache_key":"client-cache"}`)
	mapping := newOpenAICleanRelayMapping(unavailableAccount.ID, 1, openAICleanRelayInstallationID(unavailableAccount.ID))
	encoded, err := marshalOpenAICleanRelayMapping(mapping)
	require.NoError(t, err)
	cacheKey := openAICleanRelayCacheKey(101, groupID, "client-installation", "client-cache")
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
	mapping := newOpenAICleanRelayMapping(account.ID, 1, openAICleanRelayInstallationID(account.ID))
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
