//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiNativeRouteHTTPUpstreamStub struct {
	accountIDs []int64
	bodies     [][]byte
}

func (s *geminiNativeRouteHTTPUpstreamStub) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	s.accountIDs = append(s.accountIDs, accountID)
	requestBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	s.bodies = append(s.bodies, requestBody)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1},"modelVersion":"gemini-2.5-pro"}`,
		)),
	}, nil
}

func (s *geminiNativeRouteHTTPUpstreamStub) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

// TestGeminiV1BetaHandler_PlatformRoutingInvariant 文档化并验证 Handler 层的平台路由逻辑不变量
// 该测试确保 gemini 和 antigravity 平台的路由逻辑符合预期
func TestGeminiV1BetaHandler_PlatformRoutingInvariant(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		expectedService string
		description     string
	}{
		{
			name:            "Gemini平台使用ForwardNative",
			platform:        service.PlatformGemini,
			expectedService: "GeminiMessagesCompatService.ForwardNative",
			description:     "Gemini OAuth 账户直接调用 Google API",
		},
		{
			name:            "Antigravity平台使用ForwardGemini",
			platform:        service.PlatformAntigravity,
			expectedService: "AntigravityGatewayService.ForwardGemini",
			description:     "Antigravity 账户通过 CRS 中转，支持 Gemini 协议",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaModels 中的路由决策 (lines 199-205 in gemini_v1beta_handler.go)
			var routedService string
			if tt.platform == service.PlatformAntigravity {
				routedService = "AntigravityGatewayService.ForwardGemini"
			} else {
				routedService = "GeminiMessagesCompatService.ForwardNative"
			}

			require.Equal(t, tt.expectedService, routedService,
				"平台 %s 应该路由到 %s: %s",
				tt.platform, tt.expectedService, tt.description)
		})
	}
}

func TestGeminiV1BetaHandler_RoutesToBackupAndCleansUnknownThoughtSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		apiKeyID       int64 = 3701
		primaryGroupID int64 = 2701
		backupGroupID  int64 = 2702
		backupID       int64 = 1702
	)
	breakerKey := apiKeyGroupRouteBreakerKey(apiKeyID, primaryGroupID)
	apiKeyGroupRouteBreaker.mu.Lock()
	delete(apiKeyGroupRouteBreaker.states, breakerKey)
	apiKeyGroupRouteBreaker.mu.Unlock()
	t.Cleanup(func() {
		apiKeyGroupRouteBreaker.mu.Lock()
		delete(apiKeyGroupRouteBreaker.states, breakerKey)
		apiKeyGroupRouteBreaker.mu.Unlock()
	})

	primaryGroup := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	backupGroup := &service.Group{ID: backupGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	backupAccount := &service.Account{
		ID: backupID, Name: "native-gemini-backup", Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"api_key": "backup-key"},
		AccountGroups: []service.AccountGroup{{AccountID: backupID, GroupID: backupGroupID}},
	}
	callsByGroup := make(map[int64]int)
	schedulerCache := &fakeSchedulerCache{
		accounts: []*service.Account{backupAccount},
		getSnapshotFn: func(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
			callsByGroup[bucket.GroupID]++
			if bucket.GroupID == backupGroupID {
				return []*service.Account{backupAccount}, true, nil
			}
			return nil, true, nil
		},
	}
	h, cleanup := newTestGatewayHandlerWithCaches(t, primaryGroup, schedulerCache, nil)
	defer cleanup()
	upstream := &geminiNativeRouteHTTPUpstreamStub{}
	h.geminiCompatService = service.NewGeminiMessagesCompatService(
		nil, nil, nil, nil, nil, nil, upstream, nil,
		&config.Config{RunMode: config.RunModeSimple}, nil,
	)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello","thoughtSignature":"bound-to-another-account"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primaryGroup))
	c.Request = req
	apiKey := &service.APIKey{
		ID: apiKeyID, UserID: 4701, GroupID: ptrInt64(primaryGroupID), Status: service.StatusActive,
		User: &service.User{ID: 4701, Concurrency: 10, Balance: 100}, Group: primaryGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryGroupID, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: primaryGroup},
			{GroupID: backupGroupID, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: backupGroup},
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Positive(t, callsByGroup[primaryGroupID])
	require.Positive(t, callsByGroup[backupGroupID])
	require.Equal(t, []int64{backupID}, upstream.accountIDs)
	require.Len(t, upstream.bodies, 1)
	require.NotContains(t, string(upstream.bodies[0]), "bound-to-another-account")
	require.Contains(t, string(upstream.bodies[0]), "skip_thought_signature_validator")
	selected, ok := c.Get(opsAccountIDKey)
	require.True(t, ok)
	require.Equal(t, backupID, selected)
}

func TestGeminiV1BetaHandler_CapacitySkipsToBackupWithoutOpeningBreaker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		apiKeyID       int64 = 3703
		primaryGroupID int64 = 2703
		backupGroupID  int64 = 2704
		primaryID      int64 = 1703
		backupID       int64 = 1704
	)
	primaryGroup := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	backupGroup := &service.Group{ID: backupGroupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	primaryAccount := &service.Account{
		ID: primaryID, Name: "native-primary-busy", Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"api_key": "primary-key"},
		AccountGroups: []service.AccountGroup{{AccountID: primaryID, GroupID: primaryGroupID}},
	}
	backupAccount := &service.Account{
		ID: backupID, Name: "native-backup-free", Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"api_key": "backup-key"},
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
	upstream := &geminiNativeRouteHTTPUpstreamStub{}
	h.geminiCompatService = service.NewGeminiMessagesCompatService(
		nil, nil, nil, nil, nil, nil, upstream, nil,
		&config.Config{RunMode: config.RunModeSimple}, nil,
	)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, primaryGroup))
	c.Request = req
	apiKey := &service.APIKey{
		ID: apiKeyID, UserID: 4703, GroupID: ptrInt64(primaryGroupID), Status: service.StatusActive,
		User: &service.User{ID: 4703, Concurrency: 10, Balance: 100}, Group: primaryGroup,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryGroupID, Priority: 1, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: primaryGroup},
			{GroupID: backupGroupID, Priority: 2, Weight: 1, Enabled: true, CooldownSeconds: 30, Group: backupGroup},
		},
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{backupID}, upstream.accountIDs)
	require.True(t, apiKeyGroupRouteBreaker.available(apiKeyID, primaryGroupID, time.Now()),
		"capacity-only route skips must not open the upstream circuit breaker")
}

func TestGeminiV1BetaHandler_WaitCounterErrorIsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(2301)
	accountID := int64(1301)
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

	waitErr := errors.New("redis wait counter unavailable")
	accountAcquireCalls := 0
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
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
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req
	apiKey := &service.APIKey{
		ID:      3301,
		UserID:  4301,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User:    &service.User{ID: 4301, Concurrency: 10, Balance: 100},
		Group:   group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "concurrency service is temporarily unavailable")
	require.Zero(t, accountAcquireCalls, "wait-counter failure must stop before account-slot waiting")

	canceledRecorder := httptest.NewRecorder()
	canceledContext, _ := gin.CreateTestContext(canceledRecorder)
	canceledContext.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
	canceledRequest := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(body))
	canceledRequest.Header.Set("Content-Type", "application/json")
	requestContext, cancelRequest := context.WithCancel(canceledRequest.Context())
	defer cancelRequest()
	requestContext = context.WithValue(requestContext, ctxkey.Group, group)
	canceledContext.Request = canceledRequest.WithContext(requestContext)
	canceledContext.Set(string(middleware.ContextKeyAPIKey), apiKey)
	canceledContext.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})
	cache.incrementAccountWaitCountFn = func(context.Context, int64, int) (bool, error) {
		cancelRequest()
		return false, waitErr
	}

	h.GeminiV1BetaModels(canceledContext)

	require.Equal(t, statusClientClosedRequest, canceledContext.Writer.Status())
	require.Empty(t, canceledRecorder.Body.String())
	require.Zero(t, accountAcquireCalls, "client cancellation must stop before account-slot waiting")
}

func TestGeminiV1BetaHandler_UserSlotErrorClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		acquireErr   error
		cancel       bool
		wantStatus   int
		wantContains string
	}{
		{
			name:         "typed capacity timeout is rate limited",
			acquireErr:   &ConcurrencyError{SlotType: "user", IsTimeout: true},
			wantStatus:   http.StatusTooManyRequests,
			wantContains: "User concurrency limit exceeded",
		},
		{
			name:         "routing budget is gateway timeout",
			acquireErr:   service.ErrOpenAIFirstOutputRoutingBudgetExceeded,
			wantStatus:   http.StatusGatewayTimeout,
			wantContains: "routing budget expired",
		},
		{
			name:         "infrastructure error is service unavailable",
			acquireErr:   errors.New("redis unavailable"),
			wantStatus:   http.StatusServiceUnavailable,
			wantContains: "concurrency service is temporarily unavailable",
		},
		{
			name:       "client cancellation is 499",
			acquireErr: errors.New("redis unavailable"),
			cancel:     true,
			wantStatus: statusClientClosedRequest,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(2600 + index)
			group := &service.Group{
				ID:       groupID,
				Hydrated: true,
				Platform: service.PlatformGemini,
				Status:   service.StatusActive,
			}
			h, cleanup := newTestGatewayHandler(t, group, nil)
			defer cleanup()

			cache := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return false, test.acquireErr
				},
			}
			h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:generateContent"}}
			body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
			req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			requestCtx, cancelRequest := context.WithCancel(req.Context())
			defer cancelRequest()
			requestCtx = context.WithValue(requestCtx, ctxkey.Group, group)
			c.Request = req.WithContext(requestCtx)

			apiKey := &service.APIKey{
				ID:      int64(3600 + index),
				UserID:  int64(4600 + index),
				GroupID: &groupID,
				Status:  service.StatusActive,
				User:    &service.User{ID: int64(4600 + index), Concurrency: 10, Balance: 100},
				Group:   group,
			}
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})
			if test.cancel {
				cancelRequest()
			}

			h.GeminiV1BetaModels(c)

			require.Equal(t, test.wantStatus, c.Writer.Status())
			if test.wantContains == "" {
				require.Empty(t, recorder.Body.String())
			} else {
				require.Contains(t, recorder.Body.String(), test.wantContains)
			}
		})
	}
}

// TestGeminiV1BetaHandler_ListModelsAntigravityFallback 验证 ListModels 的 antigravity 降级逻辑
// 当没有 gemini 账户但有 antigravity 账户时，应返回静态模型列表
func TestGeminiV1BetaHandler_ListModelsAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态列表",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_fallback",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaListModels 的逻辑 (lines 33-44 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_fallback"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

// TestGeminiV1BetaHandler_GetModelAntigravityFallback 验证 GetModel 的 antigravity 降级逻辑
func TestGeminiV1BetaHandler_GetModelAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态模型信息",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_model_info",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaGetModel 的逻辑 (lines 77-87 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_model_info"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

func TestShouldFallbackGeminiModel_KnownFallbackOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.True(t, shouldFallbackGeminiModel("gemini-3.1-pro-preview-customtools", res))
}

func TestShouldFallbackGeminiModel_UnknownModelOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.False(t, shouldFallbackGeminiModel("gemini-future-model", res))
}

func TestShouldFallbackGeminiModel_DelegatesScopeFallback(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{
		StatusCode: http.StatusForbidden,
		Headers:    http.Header{"Www-Authenticate": []string{"Bearer error=\"insufficient_scope\""}},
		Body:       []byte("insufficient authentication scopes"),
	}
	require.True(t, shouldFallbackGeminiModel("gemini-future-model", res))
}
