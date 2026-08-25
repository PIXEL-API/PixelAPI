//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
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

const geminiSkippedWriteMessage = "invalid Gemini function call history"

type geminiSkippedWriteUpstream struct {
	response *http.Response
}

func (s *geminiSkippedWriteUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	resp := *s.response
	return &resp, nil
}

func (s *geminiSkippedWriteUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func newGeminiSkippedWriteService(status int, body string) *GeminiMessagesCompatService {
	return &GeminiMessagesCompatService{
		httpUpstream: &geminiSkippedWriteUpstream{response: &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}},
		cfg:              &config.Config{},
		rateLimitService: NewRateLimitService(&errorPolicyRepoStub{}, nil, &config.Config{}, nil, nil),
	}
}

func newGeminiSkippedWriteAccount(custom bool) *Account {
	credentials := map[string]any{"api_key": "test-key", "pool_mode": true}
	if custom {
		credentials["pool_mode"] = false
		credentials["custom_error_codes_enabled"] = true
		credentials["custom_error_codes"] = []any{float64(429)}
	}
	return &Account{ID: 900, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: credentials}
}

func newGeminiSkippedWriteContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", strings.NewReader(`{"contents":[]}`))
	return c, rec
}

func TestGeminiForwardNativeSkippedPool400KeepsRealStatusAndMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"code":400,"message":"` + geminiSkippedWriteMessage + `","status":"INVALID_ARGUMENT"}}`
	svc := newGeminiSkippedWriteService(http.StatusBadRequest, body)
	c, rec := newGeminiSkippedWriteContext()

	result, err := svc.ForwardNative(context.Background(), c, newGeminiSkippedWriteAccount(false), "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))

	require.Nil(t, result)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, body, rec.Body.String())
}

func TestGeminiForwardNativeSkippedPool503ReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newGeminiSkippedWriteService(http.StatusServiceUnavailable, `{"error":{"message":"temporarily unavailable"}}`)
	c, rec := newGeminiSkippedWriteContext()

	result, err := svc.ForwardNative(context.Background(), c, newGeminiSkippedWriteAccount(false), "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.Zero(t, rec.Body.Len())
}

func TestGeminiForwardNativeSkippedCustom400HidesUpstreamMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"code":400,"message":"` + geminiSkippedWriteMessage + `","status":"INVALID_ARGUMENT"}}`
	svc := newGeminiSkippedWriteService(http.StatusBadRequest, body)
	c, rec := newGeminiSkippedWriteContext()

	result, err := svc.ForwardNative(context.Background(), c, newGeminiSkippedWriteAccount(true), "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	errObj, ok := payload["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, geminiCustomCodeSkippedClientMessage, errObj["message"])
	require.NotContains(t, rec.Body.String(), geminiSkippedWriteMessage)
}
