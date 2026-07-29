package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAlphaSearchOAuthPreservesWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"id":"search-session","model":"gpt-5.6-sol","commands":{"search_query":[{"q":"OpenAI news"}]},"future_field":{"keep":true}}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search?feature=standalone", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", codexCLIUserAgent)
	c.Request.Header.Set("Originator", "codex_cli_rs")
	c.Request.Header.Set("Version", "0.144.1")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"search-req"}},
		Body:       io.NopCloser(strings.NewReader(`{"encrypted_output":"ciphertext"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{
		"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account",
	}}

	result, err := svc.ForwardAlphaSearch(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.WebSearchCalls)
	require.Equal(t, "search-req", result.RequestID)
	require.Equal(t, chatgptCodexAlphaSearchURL+"?feature=standalone", upstream.lastReq.URL.String())
	require.Equal(t, "chatgpt.com", upstream.lastReq.Host)
	require.Equal(t, "Bearer oauth-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "chatgpt-account", upstream.lastReq.Header.Get("chatgpt-account-id"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "0.144.1", upstream.lastReq.Header.Get("Version"))
	require.Empty(t, upstream.lastReq.Header.Get("OpenAI-Beta"))
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
}

func TestForwardAlphaSearchAPIKeyMapsModelAndSanitizesUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"id":"search-session","model":"gpt-5.6-sol","commands":{"search_query":[{"q":"news"}]}}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/alpha/search", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	upstreamBody := `{"error":{"type":"invalid_request_error","message":"proxy https://internal.example rejected bearer sk-secret"}}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "sk-test", "base_url": "https://compat.example/v4",
		"model_mapping": map[string]any{"gpt-5.6-sol": "upstream-5.6"},
	}}

	result, err := svc.ForwardAlphaSearch(context.Background(), c, account, body)

	require.Error(t, err)
	require.Contains(t, err.Error(), "non-success status 400")
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "upstream_error", gjson.Get(recorder.Body.String(), "error.type").String())
	require.Equal(t, "Upstream request failed", gjson.Get(recorder.Body.String(), "error.message").String())
	require.NotContains(t, recorder.Body.String(), "internal.example")
	require.NotContains(t, recorder.Body.String(), "sk-secret")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "https://compat.example/v4/alpha/search", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "upstream-5.6", gjson.GetBytes(upstream.lastBody, "model").String())
}

func TestForwardAlphaSearchStrict2xxOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		status         int
		downstreamCode int
	}{
		{status: http.StatusContinue, downstreamCode: http.StatusBadGateway},
		{status: http.StatusMultipleChoices, downstreamCode: http.StatusBadGateway},
		{status: http.StatusMovedPermanently, downstreamCode: http.StatusBadGateway},
		{status: http.StatusBadRequest, downstreamCode: http.StatusBadRequest},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: tt.status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"status":"not-success"}`)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}}

			result, err := svc.ForwardAlphaSearch(context.Background(), c, account, []byte(`{"model":"gpt-5.6-sol"}`))

			require.Error(t, err)
			require.Contains(t, err.Error(), "non-success status")
			require.Nil(t, result)
			require.Equal(t, tt.downstreamCode, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "not-success")
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
		})
	}
}

func TestForwardAlphaSearchReturnsFailoverBeforeWriting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"id":"search-session","model":"gpt-5.6-sol"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}}

	result, err := svc.ForwardAlphaSearch(context.Background(), c, account, body)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.False(t, c.Writer.Written())
	require.Equal(t, openAIPlatformAlphaSearchURL, upstream.lastReq.URL.String())
}

func TestForwardAlphaSearchClientCancellationDoesNotBecomeFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	upstream := &httpUpstreamRecorder{err: context.Canceled}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}}

	result, err := svc.ForwardAlphaSearch(context.Background(), c, account, []byte(`{"model":"gpt-5.6-sol"}`))

	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
	require.False(t, c.Writer.Written())
}
