package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type forceChatErrorUpstream struct {
	statusCode int
	headers    http.Header
	body       string
	calls      int
}

func (u *forceChatErrorUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return &http.Response{
		StatusCode: u.statusCode,
		Header:     u.headers.Clone(),
		Body:       io.NopCloser(strings.NewReader(u.body)),
		Request:    req,
	}, nil
}

func (u *forceChatErrorUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestForceChatAnthropicDirectBridgeNonStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.5","max_tokens":256,"messages":[{"role":"user","content":"hello"}],"tools":[{"name":"Read","input_schema":{"type":"object"}}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"req-force-chat-anthropic"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl-direct","object":"chat.completion","model":"gpt-5.5",
			"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_read","type":"function","function":{"name":"Read","arguments":"{\"path\":\"README.md\",\"pages\":\"\"}"}}]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":20,"cache_creation_tokens":99,"cache_write_tokens":5}}
		}`)),
	}}
	service := newForceChatBridgeTestService(upstream)
	result, err := service.ForwardAsAnthropic(context.Background(), c, newForceChatBridgeTestAccount(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists(), "force-chat messages must skip the Responses intermediate request")
	require.Equal(t, "tool_use", gjson.Get(recorder.Body.String(), "stop_reason").String())
	require.Equal(t, int64(75), gjson.Get(recorder.Body.String(), "usage.input_tokens").Int())
	require.Equal(t, int64(20), gjson.Get(recorder.Body.String(), "usage.cache_read_input_tokens").Int())
	require.Equal(t, int64(5), gjson.Get(recorder.Body.String(), "usage.cache_creation_input_tokens").Int())
	require.Equal(t, int64(100), int64(result.Usage.InputTokens))
	require.Equal(t, int64(5), int64(result.Usage.CacheCreationInputTokens))
	require.JSONEq(t, `{"path":"README.md"}`, gjson.Get(recorder.Body.String(), "content.0.input").Raw)
}

func TestForceChatDirectAnthropicStreamPreservesToolIndicesReadCleanupAndUsage(t *testing.T) {
	state := NewChatCompletionsToAnthropicStreamState("gpt-5.5")
	index0, index1 := 0, 1
	finishReason := "tool_calls"
	events := ChatCompletionsChunkToAnthropicEvents(&apicompat.ChatCompletionsChunk{
		ID: "chatcmpl-stream",
		Choices: []apicompat.ChatChunkChoice{{Delta: apicompat.ChatDelta{ToolCalls: []apicompat.ChatToolCall{
			{Index: &index0, ID: "call_read", Type: "function", Function: apicompat.ChatFunctionCall{Name: "Read", Arguments: `{"path":"README.md","pages":""}`}},
			{Index: &index1, ID: "call_exec", Type: "function", Function: apicompat.ChatFunctionCall{Name: "exec", Arguments: `{"command":"go test"}`}},
		}}}},
	}, state)
	events = append(events, ChatCompletionsChunkToAnthropicEvents(&apicompat.ChatCompletionsChunk{
		Choices: []apicompat.ChatChunkChoice{{FinishReason: &finishReason}},
		Usage: &apicompat.ChatUsage{PromptTokens: 30, CompletionTokens: 5, PromptTokensDetails: &apicompat.ChatTokenDetails{
			CachedTokens: 10, CacheCreationTokens: 99, CacheWriteTokens: 4,
		}},
	}, state)...)
	events = append(events, FinalizeChatCompletionsAnthropicStream(state)...)

	encoded, err := json.Marshal(events)
	require.NoError(t, err)
	streamJSON := string(encoded)
	require.Contains(t, streamJSON, `"index":0`)
	require.Contains(t, streamJSON, `"index":1`)
	require.NotContains(t, streamJSON, `"pages"`)
	require.Contains(t, streamJSON, `"partial_json":"{\"path\":\"README.md\"}"`)
	require.Contains(t, streamJSON, `"partial_json":"{\"command\":\"go test\"}"`)
	require.NotContains(t, streamJSON, `"type":"text_delta"`, "tool-only chunks must not create ghost text deltas")
	require.Contains(t, streamJSON, `"stop_reason":"tool_use"`)
	require.Contains(t, streamJSON, `"cache_read_input_tokens":10`)
	require.Contains(t, streamJSON, `"cache_creation_input_tokens":4`)
	require.Equal(t, "max_tokens", ccFinishReasonToAnthropicStopReason("length", true))
	require.Equal(t, "end_turn", ccFinishReasonToAnthropicStopReason("content_filter", false))
}

func TestForceChatResponsesFallbackNonStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.5","input":"hello","stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-responses","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":3,"cache_write_tokens":2}}}`)),
	}}
	service := newForceChatBridgeTestService(upstream)
	result, err := service.forwardResponsesViaRawChatCompletions(context.Background(), c, newForceChatBridgeTestAccount(), body)
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.Equal(t, "response", gjson.Get(recorder.Body.String(), "object").String())
	require.Equal(t, "ok", gjson.Get(recorder.Body.String(), "output.0.content.0.text").String())
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
	require.Equal(t, 2, result.Usage.CacheCreationInputTokens)
}

func TestForceChatResponsesRepeatedTransientErrorsBlockOnlyRequestedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &forceChatErrorUpstream{
		statusCode: http.StatusServiceUnavailable,
		headers:    http.Header{"Content-Type": []string{"application/json"}},
		body:       `{"error":{"message":"temporarily unavailable"}}`,
	}
	svc := newForceChatBridgeTestService(upstream)
	account := newForceChatBridgeTestAccount()
	body := []byte(`{"model":"gpt-5.5","input":"hello","stream":false}`)

	for range 2 {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		_, err := svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
	}

	require.Equal(t, 2, upstream.calls)
	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.6"))
}

func TestForwardAsChatCompletionsTransientFailureUsesEffectiveFallbackModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &forceChatErrorUpstream{
		statusCode: http.StatusServiceUnavailable,
		headers:    http.Header{"Content-Type": []string{"application/json"}},
		body:       `{"error":{"message":"temporarily unavailable"}}`,
	}
	svc := newForceChatBridgeTestService(upstream)
	account := newForceChatBridgeTestAccount()
	account.Extra[openai_compat.ExtraKeyResponsesSupported] = true
	body := []byte(`{"model":"gpt-requested","stream":false,"messages":[{"role":"user","content":"hello"}]}`)

	for range 2 {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "gpt-fallback")
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
	}

	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-fallback"))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-requested"))
}

func TestForceChatResponsesRepeated413RemainAccountScopedAndSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &forceChatErrorUpstream{
		statusCode: http.StatusRequestEntityTooLarge,
		headers:    http.Header{"X-Request-Id": []string{"req-body-limit"}},
		body:       `{"error":{"message":"proxy internal.example rejected tenant-secret payload size"}}`,
	}
	svc := newForceChatBridgeTestService(upstream)
	body := []byte(`{"model":"gpt-5.5","input":"hello","stream":false}`)

	for attempt := range 2 {
		account := newForceChatBridgeTestAccount()
		account.ID += int64(attempt)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		_, err := svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.True(t, failoverErr.IsOpenAIRequestBodyTooLarge())
		require.Equal(t, GatewayFailureScopeAccount, failoverErr.Scope)
		require.Equal(t, NextAccountRetry, failoverErr.NextAccountAction)
		require.False(t, failoverErr.RetryableOnSameAccount)
		require.Equal(t, http.StatusRequestEntityTooLarge, failoverErr.ClientStatusCode)
		require.Equal(t, OpenAIRequestBodyTooLargeClientMessage, failoverErr.ClientMessage)
		require.NotContains(t, failoverErr.ClientMessage, "internal.example")
		require.Equal(t, "req-body-limit", failoverErr.ResponseHeaders.Get("X-Request-Id"))
	}
	require.Equal(t, 2, upstream.calls)
}

func TestForceChatAnthropicMissingModelUsesAnthropicError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	service := newForceChatBridgeTestService(&httpUpstreamRecorder{})
	_, err := service.ForwardAsAnthropic(context.Background(), c, newForceChatBridgeTestAccount(), body, "", "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(recorder.Body.String(), "error.type").String())
}

func TestOpenAICompatRoutesRejectUnexpectedNon2xxBeforeSuccessHandling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	responsesBody := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_secret_marker","object":"response","model":"gpt-5.5","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"secret-marker"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	chatBody := `{"id":"chatcmpl_secret_marker","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"secret-marker"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

	tests := []struct {
		name               string
		route              string
		statusCode         int
		responsesSupported bool
		requestBody        []byte
		upstreamBody       string
	}{
		{
			name:               "chat completions responses redirect",
			route:              "chat",
			statusCode:         http.StatusFound,
			responsesSupported: true,
			requestBody:        []byte(`{"model":"gpt-5.5","stream":false,"messages":[{"role":"user","content":"hello"}]}`),
			upstreamBody:       responsesBody,
		},
		{
			name:               "anthropic responses not modified",
			route:              "anthropic",
			statusCode:         http.StatusNotModified,
			responsesSupported: true,
			requestBody:        []byte(`{"model":"gpt-5.5","max_tokens":16,"stream":false,"messages":[{"role":"user","content":"hello"}]}`),
			upstreamBody:       responsesBody,
		},
		{
			name:               "anthropic raw chat redirect",
			route:              "anthropic",
			statusCode:         http.StatusFound,
			responsesSupported: false,
			requestBody:        []byte(`{"model":"gpt-5.5","max_tokens":16,"stream":false,"messages":[{"role":"user","content":"hello"}]}`),
			upstreamBody:       chatBody,
		},
		{
			name:               "chat completions raw continue",
			route:              "chat",
			statusCode:         http.StatusContinue,
			responsesSupported: false,
			requestBody:        []byte(`{"model":"gpt-5.5","stream":false,"messages":[{"role":"user","content":"hello"}]}`),
			upstreamBody:       chatBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			path := "/v1/messages"
			if tt.route == "chat" {
				path = "/v1/chat/completions"
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(tt.requestBody))

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: tt.statusCode,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"Location":     []string{"https://redirect.invalid/private"},
					"X-Request-Id": []string{"rid-compat-unexpected-status"},
				},
				Body: io.NopCloser(strings.NewReader(tt.upstreamBody)),
			}}
			service := newForceChatBridgeTestService(upstream)
			account := newForceChatBridgeTestAccount()
			account.Extra[openai_compat.ExtraKeyResponsesSupported] = tt.responsesSupported

			gateCalls := 0
			ctx := WithOpenAIForwardResultBillingGate(context.Background(), NewOpenAIForwardResultBillingGate(func(*OpenAIForwardResult) error {
				gateCalls++
				return nil
			}))
			var (
				result *OpenAIForwardResult
				err    error
			)
			if tt.route == "chat" {
				result, err = service.ForwardAsChatCompletions(ctx, c, account, tt.requestBody, "", "")
			} else {
				result, err = service.ForwardAsAnthropic(ctx, c, account, tt.requestBody, "", "")
			}

			require.Error(t, err)
			require.Nil(t, result)
			require.Zero(t, gateCalls)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			require.Equal(t, "Upstream request failed", gjson.Get(recorder.Body.String(), "error.message").String())
			require.NotContains(t, recorder.Body.String(), "secret-marker")
			require.NotContains(t, recorder.Header().Get("Location"), "redirect.invalid")
		})
	}
}

func newForceChatBridgeTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
}

func newForceChatBridgeTestAccount() *Account {
	return &Account{
		ID: 91, Name: "force-chat", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-force-chat", "base_url": "https://upstream.example/v1"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: false},
	}
}

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

func TestOpenAIUsageFromChatCompletionsUsage_CacheWriteTokens(t *testing.T) {
	t.Parallel()

	usage := openAIUsageFromChatCompletionsUsage(`{
		"usage": {
			"prompt_tokens": 2006,
			"completion_tokens": 300,
			"prompt_tokens_details": {
				"cached_tokens": 1920,
				"cache_write_tokens": 64
			}
		}
	}`)
	require.NotNil(t, usage)
	require.Equal(t, 2006, usage.InputTokens)
	require.Equal(t, 300, usage.OutputTokens)
	require.Equal(t, 1920, usage.CacheReadInputTokens)
	require.Equal(t, 64, usage.CacheCreationInputTokens)

	usage = openAIUsageFromChatCompletionsUsage(`{
		"usage": {
			"prompt_tokens": 12,
			"completion_tokens": 1,
			"cache_creation_input_tokens": 9,
			"prompt_tokens_details": {
				"cache_write_tokens": 0
			}
		}
	}`)
	require.NotNil(t, usage)
	require.Zero(t, usage.CacheCreationInputTokens, "官方显式零值必须优先于兼容别名")
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

func TestForwardAsChatCompletions_PreservesGPT56PromptCacheControls(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_cache_controls","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":1200,"output_tokens":1,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":1024}}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_cache_controls"}},
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
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"stream":false,
		"prompt_cache_key":"tenant:acme:support-v1",
		"prompt_cache_options":{"mode":"explicit","ttl":"30m"},
		"messages":[{"role":"system","content":[{"type":"text","text":"stable prefix","prompt_cache_breakpoint":{"mode":"explicit"}}]}]
	}`)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "tenant:acme:support-v1", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1024, result.Usage.CacheCreationInputTokens)
	require.Equal(t, "tenant:acme:support-v1", gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
	require.Equal(t, "explicit", gjson.GetBytes(upstream.lastBody, "prompt_cache_options.mode").String())
	require.Equal(t, "30m", gjson.GetBytes(upstream.lastBody, "prompt_cache_options.ttl").String())
	require.Equal(t, "input_text", gjson.GetBytes(upstream.lastBody, "input.0.content.0.type").String())
	require.Equal(t, "explicit", gjson.GetBytes(upstream.lastBody, "input.0.content.0.prompt_cache_breakpoint.mode").String())
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
		`data: {"id":"chatcmpl_1","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":4,"cache_write_tokens":5}}}`,
		"",
		"data:[DONE]",
		"",
		`data: {"id":"chatcmpl_post_done","choices":[{"delta":{"content":"post_done_secret"}}]}`,
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
	require.Equal(t, 5, result.Usage.CacheCreationInputTokens)
	require.NotContains(t, rec.Body.String(), "post_done_secret")
}

func TestForwardAsChatCompletions_RawNonStreamingClientCancelStillReturnsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	inboundCtx, cancelInbound := context.WithCancel(context.Background())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil)).WithContext(inboundCtx)

	upstream := &openAIContextCancelUpstream{
		cancel:     cancelInbound,
		statusCode: http.StatusOK,
		header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_raw_nonstream_cancel"}},
		body:       `{"id":"chatcmpl_1","choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":13,"completion_tokens":6,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":3}}}`,
	}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat.example.com/v1",
		},
		Extra: map[string]any{"openai_responses_supported": false},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream)
	require.Equal(t, 13, result.Usage.InputTokens)
	require.Equal(t, 6, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.ErrorIs(t, inboundCtx.Err(), context.Canceled)
}

func TestForwardAsChatCompletions_RawStreamClientDisconnectStillCollectsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 1}

	upstreamSSE := strings.Join([]string{
		`data: {"id":"chatcmpl_1","choices":[{"delta":{"content":"hi"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","choices":[],"usage":{"prompt_tokens":15,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_raw_stream_disconnect"}},
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
			"api_key":  "sk-test",
			"base_url": "https://compat.example.com/v1",
		},
		Extra: map[string]any{"openai_responses_supported": false},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 15, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
}

func TestForwardAsChatCompletions_RawStreamClientDisconnectDisabledDrainReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 1}

	upstreamSSE := strings.Join([]string{
		`data: {"id":"chatcmpl_1","choices":[{"delta":{"content":"hi"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","choices":[],"usage":{"prompt_tokens":15,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_raw_stream_disconnect_disabled"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{
		cfg:            &config.Config{},
		httpUpstream:   upstream,
		settingService: newOpenAIDetachedDrainSettingServiceForTest(t, false),
	}
	account := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat.example.com/v1",
		},
		Extra: map[string]any{"openai_responses_supported": false},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stream usage incomplete after disconnect")
	require.NotNil(t, result)
}

func TestForwardAsChatCompletions_ResponsesStreamClientDisconnectStillCollectsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 1}

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_chat_stream","model":"gpt-5.5","status":"in_progress"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_chat_stream","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":16,"output_tokens":5,"input_tokens_details":{"cached_tokens":4}}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_responses_stream_disconnect"}},
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
		Credentials: map[string]any{"api_key": "sk-test"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 16, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Equal(t, 4, result.Usage.CacheReadInputTokens)
}

func TestForwardAsChatCompletions_ResponsesStreamClientDisconnectDisabledDrainReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 1}

	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_chat_stream","model":"gpt-5.5","status":"in_progress"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_chat_stream","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":16,"output_tokens":5,"input_tokens_details":{"cached_tokens":4}}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_responses_stream_disconnect_disabled"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{
		cfg:            &config.Config{},
		httpUpstream:   upstream,
		settingService: newOpenAIDetachedDrainSettingServiceForTest(t, false),
	}
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
	}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stream usage incomplete after disconnect")
	require.NotNil(t, result)
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

func newOpenAIDetachedDrainSettingServiceForTest(t *testing.T, enabled bool) *SettingService {
	t.Helper()
	repo := &openAIFastPolicyRepoStub{values: map[string]string{
		SettingKeyDetachedUsageDrainEnabled: fmt.Sprintf("%t", enabled),
	}}
	return NewSettingService(repo, &config.Config{})
}
