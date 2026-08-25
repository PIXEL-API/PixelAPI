//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractResponsesReasoningEffortFromBody(t *testing.T) {
	t.Parallel()

	got := ExtractResponsesReasoningEffortFromBody([]byte(`{"model":"claude-sonnet-4.5","reasoning":{"effort":"HIGH"}}`))
	require.NotNil(t, got)
	require.Equal(t, "high", *got)

	require.Nil(t, ExtractResponsesReasoningEffortFromBody([]byte(`{"model":"claude-sonnet-4.5"}`)))
}

func TestForwardAsResponses_RejectsNon2xxBeforeSuccessBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		status  int
		wantErr string
	}{
		{name: "informational", status: http.StatusContinue, wantErr: "upstream error: 100"},
		{name: "redirect", status: http.StatusFound, wantErr: "upstream error: 302"},
		{name: "not_modified", status: http.StatusNotModified, wantErr: "upstream error: 304"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newGatewayBridgeStatusTestService(tt.status)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			body := []byte(`{"model":"claude-sonnet-4.5","stream":false,"input":[{"role":"user","content":"hello"}]}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))

			result, err := svc.ForwardAsResponses(
				context.Background(),
				c,
				newGatewayBridgeStatusTestAccount(),
				body,
				nil,
			)

			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Contains(t, rec.Body.String(), `"error"`)
			require.NotContains(t, rec.Body.String(), `response.completed`)
		})
	}
}

func TestHandleResponsesBufferedStreamingResponse_PreservesMessageStartCacheUsage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_buffered"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","stop_reason":"","usage":{"input_tokens":12,"cache_read_input_tokens":9,"cache_creation_input_tokens":3}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	svc := &GatewayService{}
	result, err := svc.handleResponsesBufferedStreamingResponse(context.Background(), resp, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 9, result.Usage.CacheReadInputTokens)
	require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
	require.True(t, result.BillingUsageComplete)
	require.Contains(t, rec.Body.String(), `"cached_tokens":9`)
}

func TestHandleResponsesStreamingResponse_PreservesMessageStartCacheUsage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","stop_reason":"","usage":{"input_tokens":20,"cache_read_input_tokens":11,"cache_creation_input_tokens":4}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	svc := &GatewayService{}
	result, err := svc.handleResponsesStreamingResponse(context.Background(), resp, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.Equal(t, 11, result.Usage.CacheReadInputTokens)
	require.Equal(t, 4, result.Usage.CacheCreationInputTokens)
	require.True(t, result.BillingUsageComplete)
	require.Contains(t, rec.Body.String(), `response.completed`)
}

func TestHandleResponsesBufferedStreamingResponse_RequiresMessageStop(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_buffered_truncated"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_truncated","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesBufferedStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorContains(t, err, "without message_stop")
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.Empty(t, rec.Body.String())
}

func TestHandleResponsesStreamingResponse_RequiresMessageStopWithoutSyntheticCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_stream_truncated"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_truncated","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"partial"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorContains(t, err, "without message_stop")
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.NotContains(t, rec.Body.String(), `response.completed`)
	require.NotContains(t, rec.Body.String(), `response.incomplete`)
}

func TestHandleResponsesStreamingResponse_MissingTerminalWithoutUsageReturnsNoBillableResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_stream_no_usage"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: ping`,
			`data: {"type":"ping"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorContains(t, err, "without message_stop")
	require.False(t, IsBillableStreamUsageError(err))
	require.Nil(t, result)
	require.Empty(t, rec.Body.String())
}

func TestHandleResponsesStreamingResponse_ReadErrorFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	readErr := errors.New("injected upstream read failure")
	payload := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_read_error","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
		``,
	}, "\n")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_stream_read_error"}},
		Body: &responsesBridgeReadErrorBody{
			payload: []byte(payload),
			err:     readErr,
		},
	}

	result, err := (&GatewayService{}).handleResponsesStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorIs(t, err, readErr)
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.False(t, result.BillingUsageComplete)
	require.NotContains(t, rec.Body.String(), `response.completed`)
}

func TestHandleResponsesBufferedStreamingResponse_OutputOnlyUsageMarkedIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_responses_buffered_output_only"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_output_only","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"output_tokens":7}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesBufferedStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.NotEmpty(t, rec.Body.String())
}

func TestHandleResponsesStreamingResponse_InputOnlyUsageMarkedIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_responses_stream_input_only"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_input_only","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":20}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.Contains(t, rec.Body.String(), `response.completed`)
}

func TestHandleResponsesBufferedStreamingResponse_WriteFailurePreservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writeErr := errors.New("injected client write failure")
	c.Writer = &responsesBridgeFailingWriter{
		ResponseWriter: c.Writer,
		err:            writeErr,
	}
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_buffered_write_error"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_write_error","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesBufferedStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorIs(t, err, writeErr)
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Empty(t, rec.Body.String())
}

func TestHandleResponsesStreamingResponse_TerminalWriteFailurePreservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writeErr := errors.New("injected terminal write failure")
	c.Writer = &responsesBridgeSelectiveFailWriter{
		ResponseWriter: c.Writer,
		reject:         "response.completed",
		err:            writeErr,
	}
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_stream_terminal_write_error"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_terminal_write_error","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":20}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleResponsesStreamingResponse(
		context.Background(),
		resp,
		c,
		"claude-sonnet-4.5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorIs(t, err, writeErr)
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.NotContains(t, rec.Body.String(), `response.completed`)
}

func TestForwardResponsesViaRawChatCompletions_RejectsRedirectForBothStreamModes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{name: "non-stream", body: `{"model":"gpt-5.5","input":"hello","stream":false}`},
		{name: "stream", body: `{"model":"gpt-5.5","input":"hello","stream":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &rawChatResponsesUpstreamStub{resp: &http.Response{
				StatusCode: http.StatusFound,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"Location":     []string{"https://redirect.invalid/v1/chat/completions"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":{"message":"unexpected upstream redirect"}}`)),
			}}
			svc := &OpenAIGatewayService{
				cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
				httpUpstream: upstream,
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tt.body))

			result, err := svc.forwardResponsesViaRawChatCompletions(
				context.Background(),
				c,
				&Account{
					ID:          91,
					Name:        "raw-chat-responses",
					Platform:    PlatformOpenAI,
					Type:        AccountTypeAPIKey,
					Concurrency: 1,
					Credentials: map[string]any{
						"api_key":  "sk-raw-chat-responses",
						"base_url": "https://upstream.example/v1",
					},
				},
				[]byte(tt.body),
			)

			require.Error(t, err)
			require.Nil(t, result)
			require.Contains(t, rec.Body.String(), `"error"`)
			require.NotContains(t, rec.Body.String(), `response.completed`)
			require.NotContains(t, rec.Body.String(), `data: [DONE]`)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr))
		})
	}
}

func TestBufferChatCompletionsAsResponses_MissingUsageMarkedIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"X-Request-Id": []string{"rid_raw_buffer_missing_usage"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl-buffer-missing-usage","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		)),
	}

	result, err := (&OpenAIGatewayService{}).bufferChatCompletionsAsResponses(
		context.Background(),
		c,
		resp,
		"gpt-5.5",
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.BillingUsageComplete)
	require.Contains(t, rec.Body.String(), `"id":"chatcmpl-buffer-missing-usage"`)
}

func TestBufferChatCompletionsAsResponses_PartialUsageMarkedIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		usage string
	}{
		{name: "missing completion tokens", usage: `"prompt_tokens":12`},
		{name: "missing prompt tokens", usage: `"completion_tokens":2`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			resp := &http.Response{
				Header: http.Header{"X-Request-Id": []string{"rid_raw_buffer_partial_usage"}},
				Body: io.NopCloser(strings.NewReader(
					`{"id":"chatcmpl-buffer-partial-usage","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{` + tt.usage + `}}`,
				)),
			}

			result, err := (&OpenAIGatewayService{}).bufferChatCompletionsAsResponses(
				context.Background(),
				c,
				resp,
				"gpt-5.5",
				nil,
				false,
				nil,
				"gpt-5.5",
				"gpt-5.5",
				nil,
				nil,
				time.Now(),
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.BillingUsageComplete)
			require.Contains(t, rec.Body.String(), `"id":"chatcmpl-buffer-partial-usage"`)
		})
	}
}

func TestBufferChatCompletionsAsResponses_ExplicitZeroUsageMarkedComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"X-Request-Id": []string{"rid_raw_buffer_zero_usage"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl-buffer-zero-usage","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
		)),
	}

	result, err := (&OpenAIGatewayService{}).bufferChatCompletionsAsResponses(
		context.Background(),
		c,
		resp,
		"gpt-5.5",
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.BillingUsageComplete)
	require.Zero(t, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), `"id":"chatcmpl-buffer-zero-usage"`)
}

func TestStreamChatCompletionsAsResponses_PartialUsageMarkedIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		usage string
	}{
		{name: "missing completion tokens", usage: `"prompt_tokens":12`},
		{name: "missing prompt tokens", usage: `"completion_tokens":2`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			resp := rawChatCompletionsResponsesStream("rid_raw_stream_partial_usage", []string{
				`data: {"id":"chatcmpl-stream-partial-usage","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
				``,
				`data: {"id":"chatcmpl-stream-partial-usage","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{` + tt.usage + `}}`,
				``,
				`data: [DONE]`,
				``,
			})

			result, err := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(
				context.Background(),
				c,
				resp,
				"gpt-5.5",
				nil,
				false,
				nil,
				"gpt-5.5",
				"gpt-5.5",
				nil,
				nil,
				time.Now(),
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.BillingUsageComplete)
			require.Contains(t, rec.Body.String(), `response.completed`)
			require.Contains(t, rec.Body.String(), `data: [DONE]`)
		})
	}
}

func TestStreamChatCompletionsAsResponses_ExplicitZeroUsageMarkedComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := rawChatCompletionsResponsesStream("rid_raw_stream_zero_usage", []string{
		`data: {"id":"chatcmpl-stream-zero-usage","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
		``,
		`data: {"id":"chatcmpl-stream-zero-usage","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
		``,
		`data: [DONE]`,
		``,
	})

	result, err := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(
		context.Background(),
		c,
		resp,
		"gpt-5.5",
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.BillingUsageComplete)
	require.Zero(t, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), `response.completed`)
	require.Contains(t, rec.Body.String(), `data: [DONE]`)
}

func TestStreamChatCompletionsAsResponses_AcceptsFinishReasonWithoutDoneSentinel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := rawChatCompletionsResponsesStream("rid_raw_stream_finish", []string{
		`data: {"id":"chatcmpl-stream-finish","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
		``,
		`data: {"id":"chatcmpl-stream-finish","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`,
		``,
	})

	result, err := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(
		context.Background(),
		c,
		resp,
		"gpt-5.5",
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), `response.completed`)
	require.Contains(t, rec.Body.String(), `data: [DONE]`)
}

func TestStreamChatCompletionsAsResponses_RejectsEOFFromIncompleteStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := rawChatCompletionsResponsesStream("rid_raw_stream_truncated", []string{
		`data: {"id":"chatcmpl-stream-truncated","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"}}]}`,
		``,
		`data: {"id":"chatcmpl-stream-truncated","object":"chat.completion.chunk","model":"gpt-5.5","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`,
		``,
	})

	result, err := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(
		context.Background(),
		c,
		resp,
		"gpt-5.5",
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.ErrorContains(t, err, "without a completion signal")
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.NotContains(t, rec.Body.String(), `response.completed`)
	require.NotContains(t, rec.Body.String(), `data: [DONE]`)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
}

func TestStreamChatCompletionsAsResponses_MissingTerminalUsageMarkedIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := rawChatCompletionsResponsesStream("rid_raw_stream_missing_usage", []string{
		`data: {"id":"chatcmpl-stream-missing-usage","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
		``,
		`data: {"id":"chatcmpl-stream-missing-usage","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	})

	result, err := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(
		context.Background(),
		c,
		resp,
		"gpt-5.5",
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.BillingUsageComplete)
	require.Contains(t, rec.Body.String(), `response.completed`)
	require.Contains(t, rec.Body.String(), `data: [DONE]`)
}

func rawChatCompletionsResponsesStream(requestID string, lines []string) *http.Response {
	return &http.Response{
		Header: http.Header{"X-Request-Id": []string{requestID}},
		Body:   io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))),
	}
}

type rawChatResponsesUpstreamStub struct {
	HTTPUpstream
	resp *http.Response
}

func (u *rawChatResponsesUpstreamStub) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return u.resp, nil
}

func (u *rawChatResponsesUpstreamStub) DoWithTLS(
	*http.Request,
	string,
	int64,
	int,
	*tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.resp, nil
}

func newGatewayBridgeStatusTestService(statusCode int) *GatewayService {
	upstream := &gatewayBridgeStatusUpstream{resp: &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"x-request-id": []string{"rid_gateway_bridge_non_2xx"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"unexpected non-2xx response"}}`)),
	}}
	return &GatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		httpUpstream: upstream,
	}
}

func newGatewayBridgeStatusTestAccount() *Account {
	return &Account{
		ID:          201,
		Name:        "gateway-bridge-status-test",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "upstream-anthropic-key",
			"base_url": "https://api.anthropic.com",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

type gatewayBridgeStatusUpstream struct {
	resp *http.Response
}

func (u *gatewayBridgeStatusUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return u.resp, nil
}

func (u *gatewayBridgeStatusUpstream) DoWithTLS(
	*http.Request,
	string,
	int64,
	int,
	*tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.resp, nil
}

type responsesBridgeReadErrorBody struct {
	payload []byte
	err     error
	sent    bool
}

func (b *responsesBridgeReadErrorBody) Read(dst []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(dst, b.payload), nil
	}
	return 0, b.err
}

func (b *responsesBridgeReadErrorBody) Close() error {
	return nil
}

type responsesBridgeFailingWriter struct {
	gin.ResponseWriter
	err error
}

func (w *responsesBridgeFailingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *responsesBridgeFailingWriter) WriteString(string) (int, error) {
	return 0, w.err
}

type responsesBridgeSelectiveFailWriter struct {
	gin.ResponseWriter
	reject string
	err    error
}

func (w *responsesBridgeSelectiveFailWriter) Write(payload []byte) (int, error) {
	if strings.Contains(string(payload), w.reject) {
		return 0, w.err
	}
	return w.ResponseWriter.Write(payload)
}

func (w *responsesBridgeSelectiveFailWriter) WriteString(payload string) (int, error) {
	if strings.Contains(payload, w.reject) {
		return 0, w.err
	}
	return w.ResponseWriter.WriteString(payload)
}
