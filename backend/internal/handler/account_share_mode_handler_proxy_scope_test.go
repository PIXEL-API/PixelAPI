package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type listAvailableProxiesRepoStub struct {
	service.AccountShareModeProxyRepository

	gotScope service.ProxyScope
	calls    int
	proxies  []service.ProxyWithAccountCount
}

func (s *listAvailableProxiesRepoStub) ListActiveVisibleWithAccountCount(
	_ context.Context,
	scope service.ProxyScope,
) ([]service.ProxyWithAccountCount, error) {
	s.calls++
	s.gotScope = scope
	return s.proxies, nil
}

func newListAvailableProxiesHandler(repo service.AccountShareModeProxyRepository) *AccountShareModeHandler {
	return NewAccountShareModeHandler(
		service.NewAccountShareModeService(nil, nil, nil, nil, repo, nil),
	)
}

func invokeListAvailableProxies(handler *AccountShareModeHandler, userID int64, query string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account-share/proxies"+query, nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
	handler.ListAvailableProxies(c)
	return recorder
}

// 迁移 256 之前用户可以自行上传代理，并且刻意保留了这些代理的 owner_user_id。
// 可选代理列表必须带上调用者自己的归属豁免，否则老用户账号上已经绑定的自有代理
// 不会出现在选择器里，重新授权时又会被 scope 校验拒绝。
func TestListAvailableProxiesCarriesLegacyOwnerExemption(t *testing.T) {
	repo := &listAvailableProxiesRepoStub{}
	recorder := invokeListAvailableProxies(newListAvailableProxiesHandler(repo), 4242, "?platform=anthropic")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if repo.calls != 1 {
		t.Fatalf("expected exactly 1 repository call, got %d", repo.calls)
	}
	if repo.gotScope.OwnerUserID != 4242 {
		t.Fatalf("expected the caller's owner exemption in scope, got OwnerUserID=%d", repo.gotScope.OwnerUserID)
	}
	if repo.gotScope.Platform != service.PlatformAnthropic {
		t.Fatalf("expected platform to survive normalization, got %q", repo.gotScope.Platform)
	}
}

// 平台/等级筛选必须原样透传：CreateAccountModal 现在会按选中的平台与等级重新拉取，
// 如果这里把范围丢了，平台/等级专属代理就永远选不到（1.2.27 的 P0）。
func TestListAvailableProxiesForwardsPlatformAndLevelScope(t *testing.T) {
	repo := &listAvailableProxiesRepoStub{}
	recorder := invokeListAvailableProxies(
		newListAvailableProxiesHandler(repo),
		7,
		"?platform=openai&account_level=pro",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if repo.gotScope.Platform != service.PlatformOpenAI {
		t.Fatalf("expected platform openai, got %q", repo.gotScope.Platform)
	}
	if repo.gotScope.AccountLevel != "pro" {
		t.Fatalf("expected account level pro, got %q", repo.gotScope.AccountLevel)
	}
	if repo.gotScope.OwnerUserID != 7 {
		t.Fatalf("expected OwnerUserID=7, got %d", repo.gotScope.OwnerUserID)
	}
}

func TestListAvailableProxiesRequiresAuthenticatedSubject(t *testing.T) {
	repo := &listAvailableProxiesRepoStub{}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account-share/proxies", nil)

	newListAvailableProxiesHandler(repo).ListAvailableProxies(c)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if repo.calls != 0 {
		t.Fatalf("expected no repository call for an unauthenticated request, got %d", repo.calls)
	}
}

// 用户 OAuth 登录/重新授权同样要带豁免，否则列表放行、登录却拒绝，两边对不上。
func TestUserOAuthProxyScopeCarriesCallerOwnerExemption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 99})

	scope := userOAuthProxyScope(c, service.PlatformGemini, service.AccountLevelUnknown)

	if scope.OwnerUserID != 99 {
		t.Fatalf("expected OwnerUserID=99, got %d", scope.OwnerUserID)
	}
	if scope.Platform != service.PlatformGemini {
		t.Fatalf("expected gemini platform, got %q", scope.Platform)
	}
	if scope.AccountLevel != "" {
		t.Fatalf("expected unknown level to normalize to empty, got %q", scope.AccountLevel)
	}
}

// 没有登录态时不能凭空造出一个 owner 豁免（0 = 只看平台代理）。
func TestUserOAuthProxyScopeWithoutSubjectHasNoExemption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	scope := userOAuthProxyScope(c, service.PlatformAnthropic, service.AccountLevelUnknown)

	if scope.OwnerUserID != 0 {
		t.Fatalf("expected no owner exemption, got OwnerUserID=%d", scope.OwnerUserID)
	}
}

func TestListAvailableProxiesReturnsEmptyArrayNotNull(t *testing.T) {
	repo := &listAvailableProxiesRepoStub{}
	recorder := invokeListAvailableProxies(newListAvailableProxiesHandler(repo), 1, "")

	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, recorder.Body.String())
	}
	if payload.Data == nil {
		t.Fatalf("expected an empty array, got null: %s", recorder.Body.String())
	}
}
