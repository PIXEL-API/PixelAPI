//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpencodeTestErrorRetryableWithOtherModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{"below 400 never retryable", http.StatusOK, `{"choices":[]}`, false},
		{"403 region error", http.StatusForbidden, `{"type":"error","error":{"type":"RegionError","message":"only available hosted in China"}}`, true},
		{"503 server_error", http.StatusServiceUnavailable, `{"error":{"type":"server_error","message":"Error from provider: Endpoint is unavailable."}}`, true},
		{"404 model not found", http.StatusNotFound, `{"error":{"message":"model not found"}}`, true},
		{"401 auth error", http.StatusUnauthorized, `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`, false},
		{"401 credits error", http.StatusUnauthorized, `{"type":"error","error":{"type":"CreditsError","message":"Insufficient balance."}}`, false},
		{"429 usage limit", http.StatusTooManyRequests, `{"type":"error","error":{"type":"GoUsageLimitError","message":"Weekly usage limit reached."}}`, false},
		{"403 non-model error not retryable", http.StatusForbidden, `{"error":{"message":"Forbidden"}}`, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, opencodeTestErrorRetryableWithOtherModel(tt.statusCode, tt.body))
		})
	}
}

func TestOpencodeChatCompletionsHasContent(t *testing.T) {
	t.Parallel()
	require.True(t, opencodeChatCompletionsHasContent([]byte(`{"choices":[{"message":{"content":"OK"}}]}`)))
	require.False(t, opencodeChatCompletionsHasContent([]byte(`{"choices":[]}`)))
	require.False(t, opencodeChatCompletionsHasContent([]byte(`not-json`)))
}

// opencodeTestUpstream 是 HTTPUpstream 的测试替身，按请求体里的 model 返回预设响应。
type opencodeTestUpstream struct {
	responses map[string]*http.Response
	calls     []string
}

func (u *opencodeTestUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *opencodeTestUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	bodyBytes, _ := io.ReadAll(req.Body)
	var parsed struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(bodyBytes, &parsed)
	u.calls = append(u.calls, parsed.Model)
	if resp, ok := u.responses[parsed.Model]; ok {
		return resp, nil
	}
	return newOpencodeOKResponse(), nil
}

func newOpencodeOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"OK"}}]}`)),
	}
}

func newOpencodeErrorResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newOpencodeTestService(upstream HTTPUpstream) *AccountTestService {
	return &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
}

func opencodeTestGinContext(t *testing.T) *gin.Context {
	t.Helper()
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginCtx.Request = (&http.Request{}).WithContext(context.Background())
	return ginCtx
}

func TestOpencodeAccountConnectionFallsBackOnRegionError(t *testing.T) {
	t.Parallel()
	upstream := &opencodeTestUpstream{responses: map[string]*http.Response{
		"deepseek-v4-flash": newOpencodeErrorResponse(http.StatusForbidden, `{"type":"error","error":{"type":"RegionError","message":"only available hosted in China"}}`),
		"gpt-5.6-luna":      newOpencodeOKResponse(),
	}}
	svc := newOpencodeTestService(upstream)
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
		},
	}

	err := svc.testOpencodeAccountConnection(opencodeTestGinContext(t), account, "")
	require.NoError(t, err)
	require.Equal(t, []string{"deepseek-v4-flash", "gpt-5.6-luna"}, upstream.calls,
		"should fall back from deepseek-v4-flash to gpt-5.6-luna on RegionError")
}

func TestOpencodeAccountConnectionNoFallbackOnAuthError(t *testing.T) {
	t.Parallel()
	upstream := &opencodeTestUpstream{responses: map[string]*http.Response{
		"deepseek-v4-flash": newOpencodeErrorResponse(http.StatusUnauthorized, `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`),
	}}
	svc := newOpencodeTestService(upstream)
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
		},
	}

	err := svc.testOpencodeAccountConnection(opencodeTestGinContext(t), account, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Equal(t, []string{"deepseek-v4-flash"}, upstream.calls,
		"auth error is account-level and must not trigger model fallback")
}
