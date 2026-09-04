//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 目标：严格验证“antigravity 账号通过 /v1/messages 提供 Claude 服务时”，
// 当账号 credentials.intercept_warmup_requests=true 且请求为 Warmup 时，
// 后端会在转发上游前直接拦截并返回 mock 响应（不依赖上游）。

type fakeSchedulerCache struct {
	accounts      []*service.Account
	getSnapshotFn func(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error)
}

func (f *fakeSchedulerCache) GetSnapshot(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	if f.getSnapshotFn != nil {
		return f.getSnapshotFn(ctx, bucket)
	}
	return f.accounts, true, nil
}
func (f *fakeSchedulerCache) SetSnapshot(_ context.Context, _ service.SchedulerBucket, _ []service.Account) error {
	return nil
}
func (f *fakeSchedulerCache) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	for _, account := range f.accounts {
		if account != nil && account.ID == id {
			return account, nil
		}
	}
	return nil, nil
}
func (f *fakeSchedulerCache) SetAccount(_ context.Context, _ *service.Account) error { return nil }
func (f *fakeSchedulerCache) DeleteAccount(_ context.Context, _ int64) error         { return nil }
func (f *fakeSchedulerCache) UpdateLastUsed(_ context.Context, _ map[int64]time.Time) error {
	return nil
}
func (f *fakeSchedulerCache) TryLockBucket(_ context.Context, _ service.SchedulerBucket, _ time.Duration) (bool, error) {
	return true, nil
}
func (f *fakeSchedulerCache) UnlockBucket(_ context.Context, _ service.SchedulerBucket) error {
	return nil
}
func (f *fakeSchedulerCache) ListBuckets(_ context.Context) ([]service.SchedulerBucket, error) {
	return nil, nil
}
func (f *fakeSchedulerCache) GetOutboxWatermark(_ context.Context) (int64, error) { return 0, nil }
func (f *fakeSchedulerCache) SetOutboxWatermark(_ context.Context, _ int64) error { return nil }

type fakeGroupRepo struct {
	group *service.Group
}

func (f *fakeGroupRepo) Create(context.Context, *service.Group) error { return nil }
func (f *fakeGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return f.group, nil
}
func (f *fakeGroupRepo) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return f.group, nil
}
func (f *fakeGroupRepo) Update(context.Context, *service.Group) error          { return nil }
func (f *fakeGroupRepo) Delete(context.Context, int64) error                   { return nil }
func (f *fakeGroupRepo) DeleteCascade(context.Context, int64) ([]int64, error) { return nil, nil }
func (f *fakeGroupRepo) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (f *fakeGroupRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (f *fakeGroupRepo) ListActive(context.Context) ([]service.Group, error) { return nil, nil }
func (f *fakeGroupRepo) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (f *fakeGroupRepo) ListActiveByScope(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (f *fakeGroupRepo) ListActiveByPlatformAndScope(context.Context, string, string) ([]service.Group, error) {
	return nil, nil
}
func (f *fakeGroupRepo) ExistsByName(context.Context, string) (bool, error) { return false, nil }
func (f *fakeGroupRepo) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (f *fakeGroupRepo) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (f *fakeGroupRepo) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (f *fakeGroupRepo) BindAccountsToGroup(context.Context, int64, []int64) error { return nil }
func (f *fakeGroupRepo) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}

type fakeConcurrencyCache struct{}

func (f *fakeConcurrencyCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error { return nil }
func (f *fakeConcurrencyCache) GetAccountConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}
func (f *fakeConcurrencyCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) DecrementAccountWaitCount(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) GetAccountWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}
func (f *fakeConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error   { return nil }
func (f *fakeConcurrencyCache) GetUserConcurrency(context.Context, int64) (int, error) { return 0, nil }
func (f *fakeConcurrencyCache) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) DecrementWaitCount(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) GetAccountsLoadBatch(context.Context, []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	return map[int64]*service.AccountLoadInfo{}, nil
}
func (f *fakeConcurrencyCache) GetUsersLoadBatch(context.Context, []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	return map[int64]*service.UserLoadInfo{}, nil
}
func (f *fakeConcurrencyCache) GetAccountConcurrencyBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		result[id] = 0
	}
	return result, nil
}
func (f *fakeConcurrencyCache) CleanupExpiredAccountSlots(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) CleanupExpiredSlots(context.Context) error               { return nil }

type fakeSessionLimitCache struct {
	service.SessionLimitCache

	canRegisterFn func(context.Context, int64, string, int, time.Duration) (bool, error)
	registerFn    func(context.Context, int64, string, int, time.Duration) (bool, error)
}

func (f *fakeSessionLimitCache) CanRegisterSession(
	ctx context.Context,
	accountID int64,
	sessionID string,
	maxSessions int,
	idleTimeout time.Duration,
) (bool, error) {
	if f.canRegisterFn != nil {
		return f.canRegisterFn(ctx, accountID, sessionID, maxSessions, idleTimeout)
	}
	return true, nil
}

func (f *fakeSessionLimitCache) RegisterSession(
	ctx context.Context,
	accountID int64,
	sessionID string,
	maxSessions int,
	idleTimeout time.Duration,
) (bool, error) {
	if f.registerFn != nil {
		return f.registerFn(ctx, accountID, sessionID, maxSessions, idleTimeout)
	}
	return true, nil
}

func newTestGatewayHandler(t *testing.T, group *service.Group, accounts []*service.Account) (*GatewayHandler, func()) {
	return newTestGatewayHandlerWithSelectionConcurrency(t, group, accounts, nil)
}

func newTestGatewayHandlerWithSelectionConcurrency(
	t *testing.T,
	group *service.Group,
	accounts []*service.Account,
	selectionConcurrencyCache service.ConcurrencyCache,
) (*GatewayHandler, func()) {
	return newTestGatewayHandlerWithCaches(
		t,
		group,
		&fakeSchedulerCache{accounts: accounts},
		selectionConcurrencyCache,
	)
}

func newTestGatewayHandlerWithCaches(
	t *testing.T,
	group *service.Group,
	schedulerCache *fakeSchedulerCache,
	selectionConcurrencyCache service.ConcurrencyCache,
) (*GatewayHandler, func()) {
	return newTestGatewayHandlerWithCachesAndSessionLimit(
		t,
		group,
		schedulerCache,
		selectionConcurrencyCache,
		nil,
	)
}

func newTestGatewayHandlerWithCachesAndSessionLimit(
	t *testing.T,
	group *service.Group,
	schedulerCache *fakeSchedulerCache,
	selectionConcurrencyCache service.ConcurrencyCache,
	sessionLimitCache service.SessionLimitCache,
) (*GatewayHandler, func()) {
	t.Helper()

	schedulerSnapshot := service.NewSchedulerSnapshotService(schedulerCache, nil, nil, nil, nil)
	var selectionConcurrencyService *service.ConcurrencyService
	if selectionConcurrencyCache != nil {
		selectionConcurrencyService = service.NewConcurrencyService(selectionConcurrencyCache)
	}

	gwSvc := service.NewGatewayService(
		nil, // accountRepo (not used: scheduler snapshot hit)
		nil, // accountSharePolicyRepo
		&fakeGroupRepo{group: group},
		nil, // usageLogRepo
		nil, // usageBillingRepo
		nil, // userRepo
		nil, // userSubRepo
		nil, // userGroupRateRepo
		nil, // cache (disable sticky)
		nil, // cfg
		schedulerSnapshot,
		selectionConcurrencyService,
		nil, // billingService
		nil, // rateLimitService
		nil, // billingCacheService
		nil, // identityService
		nil, // httpUpstream
		service.NewDeferredService(nil, nil, time.Minute),
		nil, // claudeTokenProvider
		sessionLimitCache,
		nil, // rpmCache
		nil, // digestStore
		nil, // settingService
		nil, // tlsFPProfileService
		nil, // channelService
		nil, // resolver
		nil, // balanceNotifyService
	)

	// RunModeSimple：跳过计费检查，避免引入 repo/cache 依赖。
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg)

	concurrencySvc := service.NewConcurrencyService(&fakeConcurrencyCache{})
	concurrencyHelper := NewConcurrencyHelper(concurrencySvc, SSEPingFormatClaude, 0)

	h := &GatewayHandler{
		gatewayService:      gwSvc,
		billingCacheService: billingCacheSvc,
		concurrencyHelper:   concurrencyHelper,
		// 这些字段对本测试不敏感，保持较小即可
		maxAccountSwitches:       1,
		maxAccountSwitchesGemini: 1,
	}

	cleanup := func() {
		billingCacheSvc.Stop()
	}
	return h, cleanup
}

func TestGatewayHandlerMessages_InterceptWarmup_AntigravityAccount_MixedSchedulingV1(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(2001)
	accountID := int64(1001)

	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic, // /v1/messages（Claude兼容）入口
		Status:   service.StatusActive,
	}

	account := &service.Account{
		ID:       accountID,
		Name:     "ag-1",
		Platform: service.PlatformAntigravity,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":              "tok_xxx",
			"intercept_warmup_requests": true,
		},
		Extra: map[string]any{
			"mixed_scheduling": true, // 关键：允许被 anthropic 分组混合调度选中
		},
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}

	h, cleanup := newTestGatewayHandler(t, group, []*service.Account{account})
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"messages": [{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
	}`)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req

	apiKey := &service.APIKey{
		ID:      3001,
		UserID:  4001,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:          4001,
			Concurrency: 10,
			Balance:     100,
		},
		Group: group,
	}

	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, 200, rec.Code)

	// 断言：确实选中了 antigravity 账号（不是纯函数测试，而是从 Handler 里验证调度结果）
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, accountID, selected)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "msg_mock_warmup", resp["id"])
	require.Equal(t, "claude-sonnet-4-5", resp["model"])

	content, ok := resp["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	first, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "New Conversation", first["text"])
}

func TestGatewayHandlerMessages_InterceptWarmup_AntigravityAccount_ForcePlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(2002)
	accountID := int64(1002)

	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAntigravity,
		Status:   service.StatusActive,
	}

	account := &service.Account{
		ID:       accountID,
		Name:     "ag-2",
		Platform: service.PlatformAntigravity,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":              "tok_xxx",
			"intercept_warmup_requests": true,
		},
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}

	h, cleanup := newTestGatewayHandler(t, group, []*service.Account{account})
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"messages": [{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
	}`)
	req := httptest.NewRequest("POST", "/antigravity/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// 模拟 routes/gateway.go 里的 ForcePlatform 中间件效果：
	// - 写入 request.Context（Service读取）
	// - 写入 gin.Context（Handler快速读取）
	ctx := context.WithValue(req.Context(), ctxkey.Group, group)
	ctx = context.WithValue(ctx, ctxkey.ForcePlatform, service.PlatformAntigravity)
	req = req.WithContext(ctx)
	c.Request = req
	c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)

	apiKey := &service.APIKey{
		ID:      3002,
		UserID:  4002,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:          4002,
			Concurrency: 10,
			Balance:     100,
		},
		Group: group,
	}

	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, 200, rec.Code)

	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, accountID, selected)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "msg_mock_warmup", resp["id"])
	require.Equal(t, "claude-sonnet-4-5", resp["model"])
}

func TestGatewayHandlerMessages_RouteSwitchClearsSingleAccountRetryForMultiAccountBackup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const requestedModel = "claude-sonnet-4-5"
	primaryGroupID := int64(2051)
	backupGroupID := int64(2052)
	primaryGroup := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	backupGroup := &service.Group{ID: backupGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	primaryAccount := &service.Account{
		ID: 1051, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "primary-token",
			"model_mapping": map[string]any{"another-model": "another-model"},
		},
		Extra:         map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{AccountID: 1051, GroupID: primaryGroupID}},
	}
	backupAccounts := []*service.Account{
		{
			ID: 1052, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1,
			Credentials: map[string]any{"access_token": "backup-token-1", "intercept_warmup_requests": true},
			Extra:       map[string]any{"mixed_scheduling": true}, AccountGroups: []service.AccountGroup{{AccountID: 1052, GroupID: backupGroupID}},
		},
		{
			ID: 1053, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1,
			Credentials: map[string]any{"access_token": "backup-token-2", "intercept_warmup_requests": true},
			Extra:       map[string]any{"mixed_scheduling": true}, AccountGroups: []service.AccountGroup{{AccountID: 1053, GroupID: backupGroupID}},
		},
	}
	allAccounts := append([]*service.Account{primaryAccount}, backupAccounts...)
	schedulerCache := &fakeSchedulerCache{accounts: allAccounts}
	schedulerCache.getSnapshotFn = func(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
		switch bucket.GroupID {
		case primaryGroupID:
			return []*service.Account{primaryAccount}, true, nil
		case backupGroupID:
			return backupAccounts, true, nil
		default:
			return nil, true, nil
		}
	}
	h, cleanup := newTestGatewayHandlerWithCaches(t, primaryGroup, schedulerCache, nil)
	defer cleanup()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"` + requestedModel + `","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primaryGroup))
	c.Request = req
	apiKey := &service.APIKey{
		ID: 3051, UserID: 4051, GroupID: &primaryGroupID, Status: service.StatusActive,
		User: &service.User{ID: 4051, Concurrency: 10, Balance: 100}, Group: primaryGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryGroupID, Priority: 1, Weight: 1, Enabled: true, Group: primaryGroup},
			{GroupID: backupGroupID, Priority: 2, Weight: 1, Enabled: true, Group: backupGroup},
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Contains(t, []int64{1052, 1053}, selected)
	singleAccountRetry, present := service.SingleAccountRetryFromContext(c.Request.Context())
	require.True(t, present)
	require.False(t, singleAccountRetry, "backup group has two accounts and must not inherit the primary route's retry mode")
}

func TestGatewayHandlerMessages_SessionLimitRaceReselectsWithinGroupWithoutOpeningRouteBreaker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		apiKeyID       int64 = 3061
		groupID        int64 = 2061
		limitedID      int64 = 1061
		fallbackID     int64 = 1062
		requestedModel       = "claude-sonnet-4-5"
	)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	limitedAccount := &service.Account{
		ID:          limitedID,
		Name:        "anthropic-session-limited",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeOAuth,
		Concurrency: 1,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "limited-token"},
		Extra:       map[string]any{"max_sessions": 1},
		AccountGroups: []service.AccountGroup{{
			AccountID: limitedID,
			GroupID:   groupID,
		}},
	}
	fallbackAccount := &service.Account{
		ID:          fallbackID,
		Name:        "same-group-free-account",
		Platform:    service.PlatformAntigravity,
		Type:        service.AccountTypeOAuth,
		Concurrency: 1,
		Priority:    2,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":              "fallback-token",
			"intercept_warmup_requests": true,
		},
		Extra: map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{
			AccountID: fallbackID,
			GroupID:   groupID,
		}},
	}

	// The selector observes a dispatch race for both accounts and therefore
	// returns a WaitPlan. The handler-side cache then succeeds immediately.
	selectionCache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return false, nil
		},
	}
	var registeredAccountIDs []int64
	sessionCache := &fakeSessionLimitCache{
		canRegisterFn: func(context.Context, int64, string, int, time.Duration) (bool, error) {
			return true, nil
		},
		registerFn: func(_ context.Context, accountID int64, _ string, _ int, _ time.Duration) (bool, error) {
			registeredAccountIDs = append(registeredAccountIDs, accountID)
			return false, nil
		},
	}
	h, cleanup := newTestGatewayHandlerWithCachesAndSessionLimit(
		t,
		group,
		&fakeSchedulerCache{accounts: []*service.Account{limitedAccount, fallbackAccount}},
		selectionCache,
		sessionCache,
	)
	defer cleanup()

	handlerConcurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
	}
	h.concurrencyHelper = NewConcurrencyHelper(
		service.NewConcurrencyService(handlerConcurrencyCache),
		SSEPingFormatClaude,
		0,
	)

	breakerKey := apiKeyGroupRouteBreakerKey(apiKeyID, groupID)
	apiKeyGroupRouteBreaker.mu.Lock()
	delete(apiKeyGroupRouteBreaker.states, breakerKey)
	apiKeyGroupRouteBreaker.mu.Unlock()
	t.Cleanup(func() {
		apiKeyGroupRouteBreaker.mu.Lock()
		delete(apiKeyGroupRouteBreaker.states, breakerKey)
		apiKeyGroupRouteBreaker.mu.Unlock()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{
		"model":"` + requestedModel + `",
		"max_tokens":256,
		"metadata":{"user_id":"user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
		"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req
	groupIDCopy := groupID
	apiKey := &service.APIKey{
		ID:      apiKeyID,
		UserID:  4061,
		GroupID: &groupIDCopy,
		Status:  service.StatusActive,
		User:    &service.User{ID: 4061, Concurrency: 10, Balance: 100},
		Group:   group,
		GroupRoutes: []service.APIKeyGroupRoute{{
			GroupID:         groupID,
			Priority:        1,
			Weight:          1,
			Enabled:         true,
			CooldownSeconds: 30,
			Group:           group,
		}},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{limitedID}, registeredAccountIDs)
	require.Equal(t, int32(1), atomic.LoadInt32(&handlerConcurrencyCache.releaseAccountCalled))
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, fallbackID, selected)

	apiKeyGroupRouteBreaker.mu.Lock()
	_, breakerOpened := apiKeyGroupRouteBreaker.states[breakerKey]
	apiKeyGroupRouteBreaker.mu.Unlock()
	require.False(t, breakerOpened, "a local max_sessions race must not mark the route as an upstream failure")
}

func TestGatewayHandlerMessages_GeminiAccountSlotErrorClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		acquireErr  error
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "infrastructure error is service unavailable",
			acquireErr:  errors.New("redis unavailable"),
			wantStatus:  http.StatusServiceUnavailable,
			wantMessage: "concurrency service is temporarily unavailable",
		},
		{
			name:        "typed account timeout is rate limited",
			acquireErr:  &ConcurrencyError{SlotType: "account", IsTimeout: true},
			wantStatus:  http.StatusTooManyRequests,
			wantMessage: "Concurrency limit exceeded for account",
		},
		{
			name:        "routing budget is gateway timeout",
			acquireErr:  service.ErrOpenAIFirstOutputRoutingBudgetExceeded,
			wantStatus:  http.StatusGatewayTimeout,
			wantMessage: "routing budget expired",
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(2100 + index)
			accountID := int64(1100 + index)
			group := &service.Group{
				ID:       groupID,
				Hydrated: true,
				Platform: service.PlatformGemini,
				Status:   service.StatusActive,
			}
			account := &service.Account{
				ID:          accountID,
				Platform:    service.PlatformGemini,
				Type:        service.AccountTypeOAuth,
				Status:      service.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				Credentials: map[string]any{"access_token": "test-token"},
				AccountGroups: []service.AccountGroup{{
					AccountID: accountID,
					GroupID:   groupID,
				}},
			}
			selectionCache := &concurrencyCacheMock{
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return false, nil
				},
			}
			h, cleanup := newTestGatewayHandlerWithSelectionConcurrency(t, group, []*service.Account{account}, selectionCache)
			defer cleanup()

			cache := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return true, nil
				},
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return false, test.acquireErr
				},
			}
			h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := []byte(`{"model":"gemini-2.5-pro","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
			c.Request = req

			apiKey := &service.APIKey{
				ID:      int64(3100 + index),
				UserID:  int64(4100 + index),
				GroupID: &groupID,
				Status:  service.StatusActive,
				User:    &service.User{ID: int64(4100 + index), Concurrency: 10, Balance: 100},
				Group:   group,
			}
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

			h.Messages(c)

			require.Equal(t, test.wantStatus, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.wantMessage)
		})
	}
}

func TestGatewayHandlerMessages_GeminiRoutesToBackupAfterPrimaryGroupExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		apiKeyID      int64 = 3191
		primaryGroup  int64 = 2191
		backupGroup   int64 = 2192
		backupAccount int64 = 1192
	)
	breakerKey := apiKeyGroupRouteBreakerKey(apiKeyID, primaryGroup)
	apiKeyGroupRouteBreaker.mu.Lock()
	delete(apiKeyGroupRouteBreaker.states, breakerKey)
	apiKeyGroupRouteBreaker.mu.Unlock()
	t.Cleanup(func() {
		apiKeyGroupRouteBreaker.mu.Lock()
		delete(apiKeyGroupRouteBreaker.states, breakerKey)
		apiKeyGroupRouteBreaker.mu.Unlock()
	})
	primary := &service.Group{ID: primaryGroup, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	backup := &service.Group{ID: backupGroup, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	account := &service.Account{
		ID: backupAccount, Name: "gemini-backup", Platform: service.PlatformGemini, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"access_token": "backup-token", "intercept_warmup_requests": true},
		AccountGroups: []service.AccountGroup{{AccountID: backupAccount, GroupID: backupGroup}},
	}
	callsByGroup := make(map[int64]int)
	schedulerCache := &fakeSchedulerCache{
		accounts: []*service.Account{account},
		getSnapshotFn: func(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
			callsByGroup[bucket.GroupID]++
			if bucket.GroupID == backupGroup {
				return []*service.Account{account}, true, nil
			}
			return nil, true, nil
		},
	}
	h, cleanup := newTestGatewayHandlerWithCaches(t, primary, schedulerCache, nil)
	defer cleanup()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gemini-2.5-pro","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primary))
	c.Request = req
	primaryGroupID := primaryGroup
	apiKey := &service.APIKey{
		ID: apiKeyID, UserID: 4191, GroupID: &primaryGroupID, Status: service.StatusActive,
		User: &service.User{ID: 4191, Concurrency: 10, Balance: 100}, Group: primary,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryGroup, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: primary},
			{GroupID: backupGroup, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: backup},
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Positive(t, callsByGroup[primaryGroup])
	require.Positive(t, callsByGroup[backupGroup])
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, backupAccount, selected)
}

func TestGatewayHandlerMessages_GeminiCapacitySkipsToBackupWithoutOpeningBreaker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		apiKeyID       int64 = 3193
		primaryGroupID int64 = 2193
		backupGroupID  int64 = 2194
		primaryID      int64 = 1193
		backupID       int64 = 1194
	)
	primaryGroup := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	backupGroup := &service.Group{ID: backupGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	primaryAccount := &service.Account{
		ID: primaryID, Name: "gemini-primary-busy", Platform: service.PlatformGemini, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"access_token": "primary-token"},
		AccountGroups: []service.AccountGroup{{AccountID: primaryID, GroupID: primaryGroupID}},
	}
	backupAccount := &service.Account{
		ID: backupID, Name: "gemini-backup-free", Platform: service.PlatformGemini, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"access_token": "backup-token", "intercept_warmup_requests": true},
		AccountGroups: []service.AccountGroup{{AccountID: backupID, GroupID: backupGroupID}},
	}
	schedulerCache := &fakeSchedulerCache{
		accounts: []*service.Account{primaryAccount, backupAccount},
		getSnapshotFn: func(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
			switch bucket.GroupID {
			case primaryGroupID:
				return []*service.Account{primaryAccount}, true, nil
			case backupGroupID:
				return []*service.Account{backupAccount}, true, nil
			default:
				return nil, true, nil
			}
		},
	}
	selectionCache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(_ context.Context, accountID int64, _ int, _ string) (bool, error) {
			return accountID == backupID, nil
		},
	}
	h, cleanup := newTestGatewayHandlerWithCaches(t, primaryGroup, schedulerCache, selectionCache)
	defer cleanup()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gemini-2.5-pro","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primaryGroup))
	c.Request = req
	apiKey := &service.APIKey{
		ID: apiKeyID, UserID: 4193, GroupID: ptrInt64(primaryGroupID), Status: service.StatusActive,
		User: &service.User{ID: 4193, Concurrency: 10, Balance: 100}, Group: primaryGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryGroupID, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: primaryGroup},
			{GroupID: backupGroupID, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: backupGroup},
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, backupID, selected)
	require.True(t, apiKeyGroupRouteBreaker.available(apiKeyID, primaryGroupID, time.Now()),
		"capacity-only route skips must not open the upstream circuit breaker")
}

func TestGatewayHandler_GeminiSelectionInfrastructureErrorDoesNotAdvanceGroupRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		body        string
		modelAction string
		handle      func(*GatewayHandler, *gin.Context)
	}{
		{
			name:   "messages",
			path:   "/v1/messages",
			body:   `{"model":"gemini-2.5-pro","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`,
			handle: (*GatewayHandler).Messages,
		},
		{
			name:        "native v1beta",
			path:        "/v1beta/models/gemini-2.5-pro:generateContent",
			body:        `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`,
			modelAction: "/gemini-2.5-pro:generateContent",
			handle:      (*GatewayHandler).GeminiV1BetaModels,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			primaryGroupID := int64(2290 + index*10)
			backupGroupID := primaryGroupID + 1
			apiKeyID := int64(3290 + index)
			primaryGroup := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
			backupGroup := &service.Group{ID: backupGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
			callsByGroup := make(map[int64]int)
			schedulerCache := &fakeSchedulerCache{
				getSnapshotFn: func(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
					callsByGroup[bucket.GroupID]++
					return nil, false, errors.New("scheduler cache unavailable")
				},
			}
			h, cleanup := newTestGatewayHandlerWithCaches(t, primaryGroup, schedulerCache, nil)
			defer cleanup()

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			if test.modelAction != "" {
				c.Params = gin.Params{{Key: "modelAction", Value: test.modelAction}}
			}
			req := httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader([]byte(test.body)))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primaryGroup))
			c.Request = req
			apiKey := &service.APIKey{
				ID: apiKeyID, UserID: int64(4290 + index), GroupID: ptrInt64(primaryGroupID), Status: service.StatusActive,
				User: &service.User{ID: int64(4290 + index), Concurrency: 10, Balance: 100}, Group: primaryGroup,
				GroupRoutes: []service.APIKeyGroupRoute{
					{GroupID: primaryGroupID, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: primaryGroup},
					{GroupID: backupGroupID, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: backupGroup},
				},
			}
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

			test.handle(h, c)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.Contains(t, recorder.Body.String(), "Account selection is temporarily unavailable")
			require.Positive(t, callsByGroup[primaryGroupID])
			require.Zero(t, callsByGroup[backupGroupID])
			require.True(t, apiKeyGroupRouteBreaker.available(apiKeyID, primaryGroupID, time.Now()),
				"infrastructure errors must not advance or open the route breaker")
		})
	}
}

func TestGatewayHandler_GroupRouteAccountSlotRoutingBudgetIsGatewayTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		path   string
		body   string
		handle func(*GatewayHandler, *gin.Context)
	}{
		{
			name:   "messages",
			path:   "/v1/messages",
			body:   `{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`,
			handle: (*GatewayHandler).Messages,
		},
		{
			name:   "chat completions",
			path:   "/v1/chat/completions",
			body:   `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`,
			handle: (*GatewayHandler).ChatCompletions,
		},
		{
			name:   "responses",
			path:   "/v1/responses",
			body:   `{"model":"claude-sonnet-4-5","input":"hello"}`,
			handle: (*GatewayHandler).Responses,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(2500 + index)
			accountID := int64(1500 + index)
			group := &service.Group{
				ID:       groupID,
				Hydrated: true,
				Platform: service.PlatformAnthropic,
				Status:   service.StatusActive,
			}
			account := &service.Account{
				ID:          accountID,
				Platform:    service.PlatformAnthropic,
				Type:        service.AccountTypeOAuth,
				Status:      service.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				Credentials: map[string]any{"access_token": "test-token"},
				AccountGroups: []service.AccountGroup{{
					AccountID: accountID,
					GroupID:   groupID,
				}},
			}
			selectionCache := &concurrencyCacheMock{
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return false, nil
				},
			}
			h, cleanup := newTestGatewayHandlerWithSelectionConcurrency(t, group, []*service.Account{account}, selectionCache)
			defer cleanup()

			runtimeCache := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return true, nil
				},
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return false, service.ErrOpenAIFirstOutputRoutingBudgetExceeded
				},
			}
			h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(runtimeCache), SSEPingFormatNone, 0)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader([]byte(test.body)))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
			c.Request = req

			apiKey := &service.APIKey{
				ID:      int64(3500 + index),
				UserID:  int64(4500 + index),
				GroupID: &groupID,
				Status:  service.StatusActive,
				User:    &service.User{ID: int64(4500 + index), Concurrency: 10, Balance: 100},
				Group:   group,
				GroupRoutes: []service.APIKeyGroupRoute{{
					GroupID:         groupID,
					Priority:        1,
					Weight:          1,
					Enabled:         true,
					CooldownSeconds: 30,
					Group:           group,
				}},
			}
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

			test.handle(h, c)

			require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
			require.Contains(t, recorder.Body.String(), "routing budget expired")
		})
	}
}

func TestGatewayHandler_AccountSelectionInfrastructureErrorDoesNotAdvanceGroupRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		path   string
		body   string
		handle func(*GatewayHandler, *gin.Context)
	}{
		{
			name:   "messages",
			path:   "/v1/messages",
			body:   `{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`,
			handle: (*GatewayHandler).Messages,
		},
		{
			name:   "chat completions",
			path:   "/v1/chat/completions",
			body:   `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`,
			handle: (*GatewayHandler).ChatCompletions,
		},
		{
			name:   "responses",
			path:   "/v1/responses",
			body:   `{"model":"claude-sonnet-4-5","input":"hello"}`,
			handle: (*GatewayHandler).Responses,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			primaryGroupID := int64(2400 + index*10)
			backupGroupID := primaryGroupID + 1
			apiKeyID := int64(3400 + index)
			primaryGroup := &service.Group{
				ID:       primaryGroupID,
				Hydrated: true,
				Platform: service.PlatformAnthropic,
				Status:   service.StatusActive,
			}
			backupGroup := &service.Group{
				ID:       backupGroupID,
				Hydrated: true,
				Platform: service.PlatformAnthropic,
				Status:   service.StatusActive,
			}

			callsByGroup := make(map[int64]int)
			schedulerCache := &fakeSchedulerCache{
				getSnapshotFn: func(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
					callsByGroup[bucket.GroupID]++
					return nil, false, errors.New("scheduler cache unavailable")
				},
			}
			h, cleanup := newTestGatewayHandlerWithCaches(t, primaryGroup, schedulerCache, nil)
			defer cleanup()

			apiKey := &service.APIKey{
				ID:      apiKeyID,
				UserID:  int64(4400 + index),
				GroupID: &primaryGroupID,
				Status:  service.StatusActive,
				User:    &service.User{ID: int64(4400 + index), Concurrency: 10, Balance: 100},
				Group:   primaryGroup,
				GroupRoutes: []service.APIKeyGroupRoute{
					{
						GroupID:         primaryGroupID,
						Priority:        1,
						Weight:          1,
						Enabled:         true,
						CooldownSeconds: 30,
						Group:           primaryGroup,
					},
					{
						GroupID:         backupGroupID,
						Priority:        2,
						Weight:          1,
						Enabled:         true,
						CooldownSeconds: 30,
						Group:           backupGroup,
					},
				},
			}

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader([]byte(test.body)))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primaryGroup))
			c.Request = req
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

			test.handle(h, c)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.Contains(t, recorder.Body.String(), "Account selection is temporarily unavailable")
			require.Positive(t, callsByGroup[primaryGroupID], "primary route must attempt account selection")
			require.Zero(t, callsByGroup[backupGroupID], "infrastructure errors must not advance to a backup route")
			require.True(t, apiKeyGroupRouteBreaker.available(apiKeyID, primaryGroupID, time.Now()),
				"infrastructure errors must not trip the primary route circuit breaker")
		})
	}
}

func TestGatewayHandlerWebSearch_WaitCounterErrorIsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(2201)
	accountID := int64(1201)
	searchPrice := 1.0
	searchModel := resolveGrokStandaloneSearchModel()
	group := &service.Group{
		ID:               groupID,
		Hydrated:         true,
		Platform:         service.PlatformGrok,
		Status:           service.StatusActive,
		SearchPricePer1K: &searchPrice,
	}
	account := &service.Account{
		ID:          accountID,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
		Credentials: map[string]any{
			"api_key":       "test-key",
			"model_mapping": map[string]any{searchModel: searchModel},
		},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	selectionCache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return false, nil
		},
	}
	h, cleanup := newTestGatewayHandlerWithSelectionConcurrency(t, group, []*service.Account{account}, selectionCache)
	defer cleanup()

	waitErr := errors.New("redis wait counter unavailable")
	accountAcquireCalls := 0
	cache := &concurrencyCacheMock{
		incrementAccountWaitCountFn: func(context.Context, int64, int) (bool, error) {
			return false, waitErr
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			accountAcquireCalls++
			return false, nil
		},
	}
	h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/web_search", bytes.NewReader([]byte(`{"query":"openai"}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	apiKey := &service.APIKey{
		ID:      3201,
		UserID:  4201,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User:    &service.User{ID: 4201, Concurrency: 10, Balance: 100},
		Group:   group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.WebSearch(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "concurrency service is temporarily unavailable")
	require.Zero(t, accountAcquireCalls, "wait-counter failure must stop before account-slot waiting")

	canceledRecorder := httptest.NewRecorder()
	canceledContext, _ := gin.CreateTestContext(canceledRecorder)
	canceledRequest := httptest.NewRequest(http.MethodPost, "/v1/web_search", bytes.NewReader([]byte(`{"query":"openai"}`)))
	canceledRequest.Header.Set("Content-Type", "application/json")
	requestContext, cancelRequest := context.WithCancel(canceledRequest.Context())
	defer cancelRequest()
	canceledContext.Request = canceledRequest.WithContext(requestContext)
	canceledContext.Set(string(middleware.ContextKeyAPIKey), apiKey)
	canceledContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})
	cache.incrementAccountWaitCountFn = func(context.Context, int64, int) (bool, error) {
		cancelRequest()
		return false, waitErr
	}

	h.WebSearch(canceledContext)

	require.Equal(t, statusClientClosedRequest, canceledContext.Writer.Status())
	require.Empty(t, canceledRecorder.Body.String())
	require.Zero(t, accountAcquireCalls, "client cancellation must stop before account-slot waiting")
}
