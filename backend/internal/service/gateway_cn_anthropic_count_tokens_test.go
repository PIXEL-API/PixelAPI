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

func newCNCountTokensGatewayTestService(upstream HTTPUpstream) *GatewayService {
	return &GatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false, AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
}

func newCNCountTokensContext(body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	return c, recorder
}

func TestForwardCountTokensCNAdaptiveUsesNativeAnthropicWithMappingAndFilteredAuth(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"input_tokens":17}`)),
	}}
	svc := newCNCountTokensGatewayTestService(upstream)
	account := &Account{
		ID: 901, Name: "kimi-adaptive", Platform: PlatformKimi, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "kimi-count-key", "api_protocol": APIProtocolAdaptive,
			"api_base_urls": map[string]any{APIProtocolAnthropic: "http://127.0.0.1:43123/anthropic"},
			"model_mapping": map[string]any{"kimi-k2.5": "kimi-k2.5-20250101"},
		},
		Extra: map[string]any{},
	}
	body := []byte(`{"model":"kimi-k2.5","messages":[{"role":"user","content":"hello"}]}`)
	c, recorder := newCNCountTokensContext(body)
	c.Request.Header.Set("Authorization", "Bearer client-secret")
	c.Request.Header.Set("X-Api-Key", "client-secret")
	c.Request.Header.Set("anthropic-version", "2023-06-01")

	err := svc.ForwardCountTokens(context.Background(), c, account, &ParsedRequest{Body: body, Model: "kimi-k2.5"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(17), gjson.GetBytes(recorder.Body.Bytes(), "input_tokens").Int())
	req, sentBody, ok := upstream.requestByMethodAndPath(http.MethodPost, "/anthropic/v1/messages/count_tokens")
	require.True(t, ok)
	require.Equal(t, "kimi-count-key", getHeaderRaw(req.Header, "x-api-key"))
	require.Empty(t, getHeaderRaw(req.Header, "authorization"))
	require.Equal(t, "2023-06-01", getHeaderRaw(req.Header, "anthropic-version"))
	require.Equal(t, "kimi-k2.5-20250101", gjson.GetBytes(sentBody, "model").String())
}

func TestForwardCountTokensQwenReturnsUnsupportedWithoutUpstreamRequest(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"input_tokens":1}`)),
	}}
	svc := newCNCountTokensGatewayTestService(upstream)
	account := &Account{
		ID: 902, Platform: PlatformQwen, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "qwen-key", "api_protocol": APIProtocolAnthropic},
	}
	body := []byte(`{"model":"qwen-max","messages":[]}`)
	c, recorder := newCNCountTokensContext(body)

	err := svc.ForwardCountTokens(context.Background(), c, account, &ParsedRequest{Body: body, Model: "qwen-max"})
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, "not_found_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Empty(t, upstream.requests)
}

func TestForwardCountTokensCNNativeRejectsMalformedTokenResponseAndUpstreamError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus int
		wantErr    bool
	}{
		{name: "zero input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":0}`, wantStatus: http.StatusOK},
		{name: "missing input_tokens", statusCode: http.StatusOK, body: `{"usage":{}}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "truncated JSON", statusCode: http.StatusOK, body: `{"input_tokens":17`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "negative input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":-1}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "fractional input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":1.5}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "exponent input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":1e3}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "non numeric input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":"17"}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "overflow input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":18446744073709551616}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "non finite input_tokens", statusCode: http.StatusOK, body: `{"input_tokens":"Infinity"}`, wantStatus: http.StatusBadGateway, wantErr: true},
		{name: "redirect status", statusCode: http.StatusFound, body: `{"input_tokens":17}`, wantStatus: http.StatusFound, wantErr: true},
		{name: "upstream error", statusCode: http.StatusBadRequest, body: `{"error":{"message":"invalid messages"}}`, wantStatus: http.StatusBadRequest, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}}
			svc := newCNCountTokensGatewayTestService(upstream)
			account := &Account{
				ID: 903, Platform: PlatformZhipu, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "glm-key", "api_protocol": APIProtocolAnthropic, "base_url": "http://127.0.0.1:43124/anthropic"},
			}
			body := []byte(`{"model":"glm-4.5","messages":[]}`)
			c, recorder := newCNCountTokensContext(body)

			err := svc.ForwardCountTokens(context.Background(), c, account, &ParsedRequest{Body: body, Model: "glm-4.5"})
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantStatus, recorder.Code)
			require.NotEmpty(t, upstream.requests)
		})
	}
}
