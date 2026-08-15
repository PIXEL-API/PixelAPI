//go:build unit

package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type grokQuotaHandlerAccountRepo struct {
	service.AccountRepository
	account *service.Account
	updates map[int64]map[string]any
	mu      sync.Mutex
}

type grokOAuthHandlerAdminService struct {
	service.AdminService
	account *service.Account
}

func (s *grokOAuthHandlerAdminService) GetAccount(_ context.Context, _ int64) (*service.Account, error) {
	return s.account, nil
}

func (r *grokQuotaHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if r.account != nil && r.account.ID == id {
		return r.account, nil
	}
	return nil, service.ErrAccountNotFound
}

func (r *grokQuotaHandlerAccountRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	r.updates[id] = updates
	return nil
}

func (r *grokQuotaHandlerAccountRepo) hasUpdate(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.updates[id] != nil
}

type grokQuotaHandlerUpstream struct {
	resp     *http.Response
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
}

func (u *grokQuotaHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	u.mu.Lock()
	u.requests = append(u.requests, req.Clone(req.Context()))
	u.bodies = append(u.bodies, body)
	u.mu.Unlock()
	if req.Method == http.MethodGet && req.URL.Path == "/v1/billing" {
		body := `{"config":{"billingPeriodStart":"2026-07-01T00:00:00Z","billingPeriodEnd":"2026-08-01T00:00:00Z"}}`
		if req.URL.Query().Get("format") == "credits" {
			body = `{"config":{"currentPeriod":{"type":"WEEKLY","start":"2026-07-09T00:00:00Z","end":"2026-07-16T00:00:00Z"}}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
	if req.Method == http.MethodGet && req.URL.Path == "/v1/models" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		}, nil
	}
	return u.resp, nil
}

func (u *grokQuotaHandlerUpstream) responseProbe() (*http.Request, []byte, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i, request := range u.requests {
		if request.Method == http.MethodPost && request.URL.Path == "/v1/responses" {
			return request, append([]byte(nil), u.bodies[i]...), true
		}
	}
	return nil, nil, false
}

func (u *grokQuotaHandlerUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGrokOAuthHandlerQueryQuotaProbesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &grokQuotaHandlerAccountRepo{account: &service.Account{
		ID:          42,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		},
	}}
	upstream := &grokQuotaHandlerUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"8"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	quotaService := service.NewGrokQuotaService(repo, nil, service.NewGrokTokenProvider(repo, nil), upstream)
	handler := NewGrokOAuthHandler(nil, nil, nil, quotaService)

	router := gin.New()
	router.GET("/api/v1/admin/grok/accounts/:id/quota", handler.QueryQuota)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/grok/accounts/42/quota", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"source":"hybrid_probe"`)
	require.Contains(t, rec.Body.String(), `"headers_observed":true`)
	require.NotContains(t, rec.Body.String(), "access-token")
	var probeRequest *http.Request
	var probeBody []byte
	require.Eventually(t, func() bool {
		var found bool
		probeRequest, probeBody, found = upstream.responseProbe()
		return found
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, xai.DefaultCLIBaseURL+"/responses", probeRequest.URL.String())
	require.Equal(t, "Bearer access-token", probeRequest.Header.Get("Authorization"))
	require.NotContains(t, string(probeBody), `"store"`)
	require.True(t, repo.hasUpdate(42))
}

func TestGrokOAuthHandlerCapabilitiesDefaultPasswordAuthOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauthService := service.NewGrokOAuthService(nil, nil)
	defer oauthService.Stop()
	handler := NewGrokOAuthHandler(oauthService, nil, nil, nil)

	router := gin.New()
	router.GET("/api/v1/admin/grok/oauth/capabilities", handler.GetCapabilities)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/grok/oauth/capabilities", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"password_auth_enabled":false`)
}

func TestGrokOAuthHandlerPasswordDisabledRejectsBeforeParsingSensitiveBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauthService := service.NewGrokOAuthService(nil, nil)
	defer oauthService.Stop()
	handler := NewGrokOAuthHandler(oauthService, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/password", handler.AuthorizePassword)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/grok/oauth/password",
		strings.NewReader(`{"email":"admin@example.com","password":"password-secret"`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"reason":"GROK_OAUTH_PASSWORD_AUTH_DISABLED"`)
	require.NotContains(t, recorder.Body.String(), "password-secret")
}

func TestGrokOAuthHandlerResetQuotaReturnsUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &grokQuotaHandlerAccountRepo{account: &service.Account{
		ID:       43,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
	}}
	quotaService := service.NewGrokQuotaService(repo, nil, nil, nil)
	handler := NewGrokOAuthHandler(nil, nil, nil, quotaService)

	router := gin.New()
	router.POST("/api/v1/admin/grok/accounts/:id/reset-quota", handler.ResetQuota)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/accounts/43/reset-quota", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotImplemented, rec.Code)
	require.Contains(t, rec.Body.String(), `"reason":"GROK_QUOTA_RESET_UNSUPPORTED"`)
	require.NotContains(t, rec.Body.String(), "access-token")
}

func TestGrokOAuthHandlerRuntimeSanityDoesNotExposeSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(xai.EnvBaseURL, "http://127.0.0.1:8080/v1?access_token=secret")
	t.Setenv(xai.EnvClientID, "client-secret-like-value")

	handler := NewGrokOAuthHandler(nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/grok/runtime-sanity", handler.RuntimeSanity)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/grok/runtime-sanity", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"public_gateway_scope":"responses_only"`)
	require.Contains(t, rec.Body.String(), `"valid":false`)
	require.NotContains(t, rec.Body.String(), "access_token")
	require.NotContains(t, rec.Body.String(), "secret")
	require.NotContains(t, rec.Body.String(), "client-secret-like-value")
}

func TestGrokOAuthHandlerRefreshAccountTokenFailsWhenProviderUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminService := &grokOAuthHandlerAdminService{account: &service.Account{
		ID:       44,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
	}}
	handler := NewGrokOAuthHandler(nil, nil, adminService, nil)
	router := gin.New()
	router.POST("/api/v1/admin/grok/accounts/:id/refresh", handler.RefreshAccountToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/accounts/44/refresh", nil)

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), `"reason":"GROK_TOKEN_PROVIDER_UNAVAILABLE"`)
}

func TestGrokSSOImportCredentialsPreservesRequestedBaseURL(t *testing.T) {
	built := map[string]any{
		"access_token": "at-1",
		"base_url":     xai.DefaultCLIBaseURL,
	}
	reqCredentials := map[string]any{
		"base_url":                "https://relay.example.com/v1",
		"header_override_enabled": true,
		"header_overrides":        map[string]any{"x-relay-key": "k"},
	}

	credentials := grokSSOImportCredentials(built, reqCredentials)

	require.Equal(t, "at-1", credentials["access_token"])
	require.Equal(t, "https://relay.example.com/v1", credentials["base_url"])
	require.Equal(t, true, credentials["header_override_enabled"])
	require.Equal(t, map[string]any{"x-relay-key": "k"}, credentials["header_overrides"])
	require.Equal(t, "https://relay.example.com/v1", reqCredentials["base_url"])
}

func TestGrokSSOImportCredentialsUsesBuiltDefaultWhenRequestHasNoBaseURL(t *testing.T) {
	built := map[string]any{
		"access_token": "at-1",
		"base_url":     xai.DefaultCLIBaseURL,
	}

	credentials := grokSSOImportCredentials(built, nil)
	require.Equal(t, xai.DefaultCLIBaseURL, credentials["base_url"])

	credentials = grokSSOImportCredentials(built, map[string]any{"base_url": "   "})
	require.Equal(t, xai.DefaultCLIBaseURL, credentials["base_url"])
	require.Equal(t, "at-1", credentials["access_token"])
}

func TestGrokSSOImportCredentialsRejectsRequestSecretsAndUnknownFields(t *testing.T) {
	built := map[string]any{
		"access_token":  "built-access-token",
		"refresh_token": "built-refresh-token",
		"base_url":      xai.DefaultCLIBaseURL,
	}
	requestCredentials := map[string]any{
		"access_token":           "request-access-token",
		"refresh_token":          "request-refresh-token",
		"password":               "secret",
		"sso_token":              "sso-secret",
		"cookie":                 "cookie-secret",
		"unknown_operator_field": "must-not-persist",
		"base_url":               "https://relay.example.com/v1",
		"model_mapping":          map[string]any{"grok-4": "grok-4-fast"},
		"custom_headers":         map[string]any{"X-Relay": "enabled"},
	}

	credentials := grokSSOImportCredentials(built, requestCredentials)

	require.Equal(t, "built-access-token", credentials["access_token"])
	require.Equal(t, "built-refresh-token", credentials["refresh_token"])
	require.Equal(t, "https://relay.example.com/v1", credentials["base_url"])
	require.Equal(t, requestCredentials["model_mapping"], credentials["model_mapping"])
	require.Equal(t, requestCredentials["custom_headers"], credentials["custom_headers"])
	for _, key := range []string{
		"password",
		"sso_token",
		"cookie",
		"unknown_operator_field",
	} {
		require.NotContains(t, credentials, key)
	}
}

func TestGrokSSOImportCredentialsSanitizesConvertedCredentialResidue(t *testing.T) {
	built := map[string]any{
		"access_token":      "built-access-token",
		"refresh_token":     "built-refresh-token",
		"password":          "must-not-persist",
		"sso":               "must-not-persist",
		"sso-rw":            "must-not-persist",
		"clearTextPassword": "must-not-persist",
		"cookie":            "must-not-persist",
	}

	credentials := grokSSOImportCredentials(built, nil)

	require.Equal(t, "built-access-token", credentials["access_token"])
	require.Equal(t, "built-refresh-token", credentials["refresh_token"])
	for _, key := range []string{"password", "sso", "sso-rw", "clearTextPassword", "cookie"} {
		require.NotContains(t, credentials, key)
	}
}
