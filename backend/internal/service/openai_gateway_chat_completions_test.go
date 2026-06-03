package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeResponsesRequestServiceTier(t *testing.T) {
	t.Parallel()

	req := &apicompat.ResponsesRequest{ServiceTier: " fast "}
	normalizeResponsesRequestServiceTier(req)
	require.Equal(t, "priority", req.ServiceTier)

	req.ServiceTier = "flex"
	normalizeResponsesRequestServiceTier(req)
	require.Equal(t, "flex", req.ServiceTier)

	// OpenAI 官方合法 tier 应被透传保留。
	req.ServiceTier = "auto"
	normalizeResponsesRequestServiceTier(req)
	require.Equal(t, "auto", req.ServiceTier)

	req.ServiceTier = "default"
	normalizeResponsesRequestServiceTier(req)
	require.Equal(t, "default", req.ServiceTier)

	req.ServiceTier = "scale"
	normalizeResponsesRequestServiceTier(req)
	require.Equal(t, "scale", req.ServiceTier)

	// 真未知值仍被剥离。
	req.ServiceTier = "turbo"
	normalizeResponsesRequestServiceTier(req)
	require.Empty(t, req.ServiceTier)
}

func TestNormalizeResponsesBodyServiceTier(t *testing.T) {
	t.Parallel()

	body, tier, err := normalizeResponsesBodyServiceTier([]byte(`{"model":"gpt-5.1","service_tier":"fast"}`))
	require.NoError(t, err)
	require.Equal(t, "priority", tier)
	require.Equal(t, "priority", gjson.GetBytes(body, "service_tier").String())

	body, tier, err = normalizeResponsesBodyServiceTier([]byte(`{"model":"gpt-5.1","service_tier":"flex"}`))
	require.NoError(t, err)
	require.Equal(t, "flex", tier)
	require.Equal(t, "flex", gjson.GetBytes(body, "service_tier").String())

	// OpenAI 官方 tier 直接保留在 body 中（透传上游）。
	body, tier, err = normalizeResponsesBodyServiceTier([]byte(`{"model":"gpt-5.1","service_tier":"auto"}`))
	require.NoError(t, err)
	require.Equal(t, "auto", tier)
	require.Equal(t, "auto", gjson.GetBytes(body, "service_tier").String())

	body, tier, err = normalizeResponsesBodyServiceTier([]byte(`{"model":"gpt-5.1","service_tier":"default"}`))
	require.NoError(t, err)
	require.Equal(t, "default", tier)
	require.Equal(t, "default", gjson.GetBytes(body, "service_tier").String())

	body, tier, err = normalizeResponsesBodyServiceTier([]byte(`{"model":"gpt-5.1","service_tier":"scale"}`))
	require.NoError(t, err)
	require.Equal(t, "scale", tier)
	require.Equal(t, "scale", gjson.GetBytes(body, "service_tier").String())

	// 真未知值才会被删除。
	body, tier, err = normalizeResponsesBodyServiceTier([]byte(`{"model":"gpt-5.1","service_tier":"turbo"}`))
	require.NoError(t, err)
	require.Empty(t, tier)
	require.False(t, gjson.GetBytes(body, "service_tier").Exists())
}

func TestForwardAsChatCompletions_FilteredFastTierBillsAsStandardWhenUpstreamOmitsTier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_chat_filter","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_chat_filter"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}

	svc := &OpenAIGatewayService{
		cfg:            &config.Config{},
		httpUpstream:   upstream,
		settingService: newOpenAIFastPolicySettingServiceForTest(t, DefaultOpenAIFastPolicySettings()),
	}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://api.openai.com"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"service_tier":"fast","messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.ServiceTier, "上游未回显 service_tier 时，应按策略过滤后的请求体 fallback，而不是原始 fast 请求计费")
	require.False(t, gjson.GetBytes(upstream.lastBody, "service_tier").Exists())
}

func TestForwardAsChatCompletions_UpstreamTierOverridesRequestFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_chat_tier","model":"gpt-5.5","service_tier":"priority","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_chat_tier"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}

	svc := &OpenAIGatewayService{
		cfg:            &config.Config{},
		httpUpstream:   upstream,
		settingService: newOpenAIFastPolicySettingServiceForTest(t, &OpenAIFastPolicySettings{Rules: nil}),
	}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://api.openai.com"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"service_tier":"flex","messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ServiceTier)
	require.Equal(t, "priority", *result.ServiceTier)
}

func TestForwardAsChatCompletions_APIKeyWithoutResponsesSupportUsesRawChat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	c.Request.Header.Set("Originator", "codex")
	c.Request.Header.Set("Accept-Language", "zh-CN")

	upstreamSSE := strings.Join([]string{
		`data: {"id":"chatcmpl_1","choices":[{"delta":{"content":"hi"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":4}}}`,
		"",
		"data:[DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_raw_chat"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}

	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":    "sk-test",
			"base_url":   "https://compat.example.com/v1",
			"user_agent": "custom-agent",
		},
		Extra: map[string]any{"openai_responses_supported": false},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://compat.example.com/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "custom-agent", upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "zh-CN", upstream.lastReq.Header.Get("Accept-Language"))
	require.Empty(t, upstream.lastReq.Header.Get("Originator"))
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 4, result.Usage.CacheReadInputTokens)
}

func TestForwardAsChatCompletions_AcceptsCompactSSEDataPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))

	upstreamSSE := strings.Join([]string{
		`data:{"type":"response.completed","response":{"id":"resp_compact_sse","model":"gpt-5.5","output":[],"usage":{"input_tokens":7,"output_tokens":2}}}`,
		"",
		"data:[DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_compact_sse"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://api.openai.com"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, rec.Code)
}

func newOpenAIFastPolicySettingServiceForTest(t *testing.T, settings *OpenAIFastPolicySettings) *SettingService {
	t.Helper()
	repo := &openAIFastPolicyRepoStub{values: map[string]string{}}
	if settings != nil {
		raw, err := json.Marshal(settings)
		require.NoError(t, err)
		repo.values[SettingKeyOpenAIFastPolicySettings] = string(raw)
	}
	return NewSettingService(repo, &config.Config{})
}
