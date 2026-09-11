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

func newCNAnthropicNativeTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
}

func newCNAnthropicNativeTestAccount() *Account {
	return &Account{
		ID: 901, Name: "cn-anthropic-native", Platform: PlatformKimi, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "kimi-test-key", "api_protocol": APIProtocolAnthropic,
			"base_url": "https://cn.example/anthropic",
		},
		Extra: map[string]any{},
	}
}

func TestCNAnthropicNativeMessagesUsesNativePathAndUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"cn-native-1"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"kimi-k2.5","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":7,"output_tokens":3}}`)),
	}}
	svc := newCNAnthropicNativeTestService(upstream)
	account := newCNAnthropicNativeTestAccount()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"kimi-k2.5","stream":false,"messages":[{"role":"user","content":"hi"}]}`)))

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, []byte(`{"model":"kimi-k2.5","stream":false,"messages":[{"role":"user","content":"hi"}]}`), "", "")
	require.NoError(t, err)
	require.Equal(t, "cn-native-1", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, "ok", gjson.Get(recorder.Body.String(), "content.0.text").String())
	req, body, ok := upstream.requestByMethodAndPath(http.MethodPost, "/anthropic/v1/messages")
	require.True(t, ok)
	require.Equal(t, "kimi-test-key", req.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", getHeaderRaw(req.Header, "anthropic-version"))
	require.Equal(t, "kimi-k2.5", gjson.GetBytes(body, "model").String())
}

func TestCNAnthropicNativeChatCompletionsStreamsTerminalUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"cn-native-cc-1"}},
		Body:       io.NopCloser(strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"model\":\"kimi-k2.5\",\"content\":[],\"usage\":{\"input_tokens\":5}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")),
	}}
	svc := newCNAnthropicNativeTestService(upstream)
	account := newCNAnthropicNativeTestAccount()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"kimi-k2.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.Equal(t, "cn-native-cc-1", result.RequestID)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "hello")
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
	req, bodySent, ok := upstream.requestByMethodAndPath(http.MethodPost, "/anthropic/v1/messages")
	require.True(t, ok)
	require.Equal(t, "kimi-test-key", req.Header.Get("x-api-key"))
	require.Equal(t, "kimi-k2.5", gjson.GetBytes(bodySent, "model").String())
	content := gjson.GetBytes(bodySent, "messages.0.content")
	require.True(t, content.String() == "hi" || gjson.GetBytes(bodySent, "messages.0.content.0.text").String() == "hi")
}

func TestCNAnthropicNativeResponsesStreamsTerminalUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"cn-native-resp-1"}},
		Body:       io.NopCloser(strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_3\",\"model\":\"kimi-k2.5\",\"content\":[],\"usage\":{\"input_tokens\":4}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")),
	}}
	svc := newCNAnthropicNativeTestService(upstream)
	account := newCNAnthropicNativeTestAccount()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"kimi-k2.5","stream":true,"input":"hi"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.Equal(t, "cn-native-resp-1", result.RequestID)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "response.completed")
	require.Contains(t, recorder.Body.String(), "done")
	req, bodySent, ok := upstream.requestByMethodAndPath(http.MethodPost, "/anthropic/v1/messages")
	require.True(t, ok)
	require.Equal(t, "kimi-test-key", req.Header.Get("x-api-key"))
	require.Equal(t, "kimi-k2.5", gjson.GetBytes(bodySent, "model").String())
}
