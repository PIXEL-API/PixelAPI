//go:build unit

package service

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
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGrokChatResponsesBridgeEligibility(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		want       bool
		wantReason string
	}{
		{name: "plain text", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stream":false}`, want: true},
		{name: "image url", body: `{"model":"grok-4.5","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QQ=="}}]}]}`, want: true},
		{name: "safe function tool", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":false}}],"tool_choice":"auto"}`, want: true},
		{name: "nullable compatibility fields", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stop":null,"reasoning_effort":null,"functions":null,"function_call":"none"}`, want: true},
		{name: "unknown top level", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"seed":7}`, wantReason: "unknown_field_seed"},
		{name: "stop", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stop":"END"}`, wantReason: "unsupported_stop"},
		{name: "reasoning effort", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`, wantReason: "unsupported_reasoning_effort"},
		{name: "legacy functions", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"functions":[{"name":"lookup","parameters":{"type":"object"}}]}`, wantReason: "unsupported_functions"},
		{name: "legacy function call", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"function_call":{"name":"lookup"}}`, wantReason: "unsupported_function_call"},
		{name: "named tool choice", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`, wantReason: "unsupported_tool_choice"},
		{name: "unknown stream option", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true,"vendor_trace":true}}`, wantReason: "unknown_stream_option_vendor_trace"},
		{name: "unsupported tool type", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, wantReason: "unsupported_tool_type"},
		{name: "developer role", body: `{"model":"grok-4.5","messages":[{"role":"developer","content":"rules"},{"role":"user","content":"hi"}]}`, wantReason: "unsupported_message_role_developer"},
		{name: "unsafe message field", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi","name":"alice"}]}`, wantReason: "unsafe_message_field_name"},
		{name: "audio content", body: `{"model":"grok-4.5","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA=="}}]}]}`, wantReason: "unsupported_content_part_input_audio"},
		{name: "small max tokens", body: `{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`, wantReason: "unsafe_max_tokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := grokChatResponsesBridgeEligibility([]byte(tt.body))
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantReason, reason)
		})
	}
}

func TestGrokChatRawNonStreamingPreservesSemanticsAndUsesOAuthTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-alias","messages":[{"role":"user","content":"hi"}],"stream":false,"stop":["END"],"reasoning_effort":"high","seed":7,"tool_choice":{"type":"function","function":{"name":"lookup"}},"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}}},"search_parameters":{"mode":"auto","max_search_results":5,"return_citations":true},"prompt_cache_key":"raw-session"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
	c.Set("api_key", &APIKey{ID: 4101})
	proxyID := int64(91)
	account := grokChatBridgeOAuthAccount(410)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example.com", Port: 8080, Username: "proxy-user", Password: "proxy-pass"}
	account.Credentials["model_mapping"] = map[string]any{"grok-alias": "grok-4.5"}
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{"x-grok-test": "override-ok"}
	upstream := &httpUpstreamRecorder{resp: grokRawChatResponse(http.StatusOK, http.Header{"Xai-Request-Id": []string{"xai-raw-request-410"}})}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, xai.DefaultCLIBaseURL+grokChatRawEndpoint[3:], upstream.lastReq.URL.String())
	require.Equal(t, "Bearer oauth-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.NotEmpty(t, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, "interactive", upstream.lastReq.Header.Get("X-Grok-Client-Mode"))
	require.Equal(t, "override-ok", upstream.lastReq.Header.Get("X-Grok-Test"))
	require.Equal(t, "http://proxy-user:proxy-pass@proxy.example.com:8080", upstream.lastProxyURL)
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
	require.Equal(t, "grok-4.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "END", gjson.GetBytes(upstream.lastBody, "stop.0").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
	require.Equal(t, int64(7), gjson.GetBytes(upstream.lastBody, "seed").Int())
	require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "tool_choice.function.name").String())
	require.Equal(t, "json_schema", gjson.GetBytes(upstream.lastBody, "response_format.type").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "response_format.json_schema.strict").Bool())
	require.Equal(t, "auto", gjson.GetBytes(upstream.lastBody, "search_parameters.mode").String())
	require.Equal(t, int64(5), gjson.GetBytes(upstream.lastBody, "search_parameters.max_search_results").Int())
	require.True(t, gjson.GetBytes(upstream.lastBody, "search_parameters.return_citations").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").Exists())
	require.NotEmpty(t, upstream.lastReq.Header.Get(grokConversationIDHeader))
	require.Equal(t, grokChatRawEndpoint, result.UpstreamEndpoint)
	require.Equal(t, grokChatRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, "xai-raw-request-410", result.RequestID)
	require.Equal(t, "xai-raw-request-410", result.ResponseHeaders.Get("xai-request-id"))
	require.Equal(t, "chat_raw", result.ResponseID)
	require.Equal(t, "grok-alias", result.Model)
	require.Equal(t, "grok-4.5", result.BillingModel)
	require.Equal(t, "grok-4.5", result.UpstreamModel)
	require.Equal(t, "grok-4.5", result.UpstreamResponseModel)
	require.False(t, result.Stream)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 2, result.SearchCount)
	require.True(t, result.BillingUsageComplete)
	require.Equal(t, "raw ok", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
}

func TestGrokChatRawNonStreamingMissingUsageFailsBeforeResponseCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stop":"END"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
	account := grokChatBridgeOAuthAccount(412)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   []string{"application/json"},
			"Xai-Request-Id": []string{"xai-missing-usage-412"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"chat_without_usage","object":"chat.completion","model":"grok-4.5","choices":[{"index":0,"message":{"role":"assistant","content":"unbillable"},"finish_reason":"stop"}]}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, grokMissingUsageErrorCode, gjson.GetBytes(failoverErr.ResponseBody, "error.code").String())
	require.Equal(t, grokMissingUsageMessage, gjson.GetBytes(failoverErr.ResponseBody, "error.message").String())
	require.Equal(t, "xai-missing-usage-412", failoverErr.ResponseHeaders.Get("x-request-id"))
	require.Equal(t, grokChatRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func TestGrokChatRawStreamingPreservesUnknownOptionsAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":false,"vendor_trace":true},"seed":11}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
	account := grokChatBridgeOAuthAccount(411)
	upstream := &httpUpstreamRecorder{resp: grokRawChatStreamingResponse()}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, grokChatRawEndpoint, result.UpstreamEndpoint)
	require.Equal(t, "xai-raw-stream-411", result.RequestID)
	require.Equal(t, "xai-raw-stream-411", result.ResponseHeaders.Get("xai-request-id"))
	require.Equal(t, "chat_stream", result.ResponseID)
	require.Equal(t, "grok-4.5", result.Model)
	require.Equal(t, "grok-4.5", result.BillingModel)
	require.Equal(t, "grok-4.5", result.UpstreamModel)
	require.Equal(t, "grok-4.5", result.UpstreamResponseModel)
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.vendor_trace").Bool())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Equal(t, int64(11), gjson.GetBytes(upstream.lastBody, "seed").Int())
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 3, result.SearchCount)
	require.True(t, result.BillingUsageComplete)
	require.Contains(t, recorder.Body.String(), `"content":"stream ok"`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestGrokRawChatNumSourcesUsedStrictParsing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		want    int
		ok      bool
	}{
		{name: "zero", payload: `{"usage":{"num_sources_used":0}}`, ok: true},
		{name: "positive", payload: `{"usage":{"num_sources_used":7}}`, want: 7, ok: true},
		{name: "missing", payload: `{"usage":{}}`},
		{name: "string is not coerced", payload: `{"usage":{"num_sources_used":"7"}}`},
		{name: "negative", payload: `{"usage":{"num_sources_used":-1}}`},
		{name: "fraction", payload: `{"usage":{"num_sources_used":1.5}}`},
		{name: "malformed json", payload: `{"usage":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := grokRawChatNumSourcesUsed([]byte(tt.payload))
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGrokRawChatSearchCountingReadCloserUsesMaximumAcrossFragmentedSSE(t *testing.T) {
	t.Parallel()
	want := []byte(strings.Join([]string{
		`data: {"id":"s1","usage":{"num_sources_used":1}}`,
		"",
		`data: {"id":"s1","usage":{"num_sources_used":3}}`,
		"",
		`data: {"id":"s1","usage":{"num_sources_used":3}}`,
		"",
		`data: {"id":"s1","usage":{"num_sources_used":"99"}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n"))
	source := &grokRawChatChunkReader{chunks: [][]byte{
		want[:13], want[13:61], want[61:127], want[127:193], want[193:],
	}}
	counter := newGrokRawChatSearchCountingReadCloser(source)
	got, err := io.ReadAll(counter)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 3, counter.Count())
	require.NoError(t, counter.Close())
	require.NoError(t, counter.Close())
	require.True(t, source.closed)
}

func TestGrokChatRawStreamingMissingUsageDoesNotFailoverAfterCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stream":true,"seed":19}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
	account := grokChatBridgeOAuthAccount(413)
	streamBody := strings.Join([]string{
		`data: {"id":"chat_no_usage","object":"chat.completion.chunk","model":"grok-4.5","choices":[{"index":0,"delta":{"role":"assistant","content":"already committed"},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   []string{"text/event-stream"},
			"Xai-Request-Id": []string{"xai-stream-no-usage-413"},
		},
		Body: io.NopCloser(strings.NewReader(streamBody)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	require.NotNil(t, result)
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), "already committed")
	require.Equal(t, "xai-stream-no-usage-413", result.RequestID)
	require.False(t, result.BillingUsageComplete)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
}

func TestGrokChatRawFailuresPreserveFailoverContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stop":"END"}`)
	tests := []struct {
		name        string
		response    *http.Response
		upstreamErr error
		wantStatus  int
		wantRetry   string
	}{
		{name: "rate limit", response: grokRawChatResponse(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"45"}}), wantStatus: http.StatusTooManyRequests, wantRetry: "45"},
		{name: "server unavailable", response: grokRawChatResponse(http.StatusServiceUnavailable, nil), wantStatus: http.StatusServiceUnavailable},
		{name: "transport", upstreamErr: errors.New("dial tcp: connection refused"), wantStatus: http.StatusBadGateway},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
			account := grokChatBridgeOAuthAccount(int64(420 + index))
			upstream := &httpUpstreamRecorder{resp: tt.response, err: tt.upstreamErr}
			svc := &OpenAIGatewayService{httpUpstream: upstream}

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, tt.wantStatus, failoverErr.StatusCode)
			require.Equal(t, tt.wantRetry, failoverErr.ResponseHeaders.Get("Retry-After"))
			require.Equal(t, grokChatRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
			require.False(t, c.Writer.Written())
		})
	}
}

func TestGrokChatRawCredentialFailureUsesNormalized503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stop":"END"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
	account := grokChatBridgeOAuthAccount(430)
	delete(account.Credentials, "access_token")
	SetActualOpenAIUpstreamEndpoint(c, grokChatResponsesEndpoint)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.IsCredentialFailure())
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.ClientStatusCode)
	require.Nil(t, upstream.lastReq)
	require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
}

func TestGrokChatCompatibleImageUsesExistingResponsesBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QQ=="}}]}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
	account := grokChatBridgeOAuthAccount(440)
	upstream := &httpUpstreamRecorder{resp: grokResponsesChatResponse()}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, xai.DefaultCLIBaseURL+"/responses", upstream.lastReq.URL.String())
	require.Equal(t, grokChatResponsesEndpoint, result.UpstreamEndpoint)
	require.Equal(t, grokChatResponsesEndpoint, GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, "input_image", gjson.GetBytes(upstream.lastBody, "input.0.content.1.type").String())
	require.Equal(t, 1, result.SearchCount)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
}

func TestGrokChatAPIKeyKeepsRawAndResponsesConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	tests := []struct {
		name         string
		mode         openai_compat.ResponsesSupportMode
		response     *http.Response
		wantEndpoint string
	}{
		{name: "force raw", mode: openai_compat.ResponsesSupportModeForceChatCompletions, response: grokRawChatResponse(http.StatusOK, nil), wantEndpoint: grokChatRawEndpoint},
		{name: "force responses", mode: openai_compat.ResponsesSupportModeForceResponses, response: grokResponsesChatResponse(), wantEndpoint: grokChatResponsesEndpoint},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, grokChatRawEndpoint, bytes.NewReader(body))
			account := &Account{
				ID:          int64(450 + index),
				Name:        "grok-api-key",
				Platform:    PlatformGrok,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "xai-api-key", "base_url": "https://api.x.ai/v1"},
				Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(tt.mode)},
			}
			upstream := &httpUpstreamRecorder{resp: tt.response}
			svc := &OpenAIGatewayService{
				httpUpstream: upstream,
				cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
					Enabled: false,
				}}},
			}

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "https://api.x.ai"+tt.wantEndpoint, upstream.lastReq.URL.String())
			require.Equal(t, "Bearer xai-api-key", upstream.lastReq.Header.Get("Authorization"))
			require.Empty(t, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
			require.Equal(t, tt.wantEndpoint, result.UpstreamEndpoint)
		})
	}
}

func grokChatBridgeOAuthAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "grok-chat-bridge",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "oauth-access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"base_url":     xai.DefaultCLIBaseURL,
		},
	}
}

type grokRawChatChunkReader struct {
	chunks [][]byte
	index  int
	closed bool
}

func (r *grokRawChatChunkReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.index])
	if n == len(r.chunks[r.index]) {
		r.index++
	} else {
		r.chunks[r.index] = r.chunks[r.index][n:]
	}
	return n, nil
}

func (r *grokRawChatChunkReader) Close() error {
	r.closed = true
	return nil
}

func grokRawChatResponse(status int, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	body := `{"error":{"message":"upstream failed"}}`
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		body = `{"id":"chat_raw","object":"chat.completion","model":"grok-4.5","choices":[{"index":0,"message":{"role":"assistant","content":"raw ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6,"num_sources_used":2}}`
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

func grokRawChatStreamingResponse() *http.Response {
	body := strings.Join([]string{
		`data: {"id":"chat_stream","object":"chat.completion.chunk","model":"grok-4.5","choices":[{"index":0,"delta":{"role":"assistant","content":"stream ok"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chat_stream","object":"chat.completion.chunk","model":"grok-4.5","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8,"num_sources_used":3}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"Content-Type":   []string{"text/event-stream"},
		"Xai-Request-Id": []string{"xai-raw-stream-411"},
	}, Body: io.NopCloser(strings.NewReader(body))}
}

func grokResponsesChatResponse() *http.Response {
	search := `{"type":"x_search_call","id":"xs1","call_id":"search_1","status":"completed"}`
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":` + search + `}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"responses ok"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_bridge","object":"response","model":"grok-4.5","status":"completed","output":[` + search + `,{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"responses ok"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
}
