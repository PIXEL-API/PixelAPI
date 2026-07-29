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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractCCReasoningEffortFromBody(t *testing.T) {
	t.Parallel()

	t.Run("nested reasoning.effort", func(t *testing.T) {
		got := extractCCReasoningEffortFromBody([]byte(`{"reasoning":{"effort":"HIGH"}}`))
		require.NotNil(t, got)
		require.Equal(t, "high", *got)
	})

	t.Run("flat reasoning_effort", func(t *testing.T) {
		got := extractCCReasoningEffortFromBody([]byte(`{"reasoning_effort":"x-high"}`))
		require.NotNil(t, got)
		require.Equal(t, "xhigh", *got)
	})

	t.Run("missing effort", func(t *testing.T) {
		require.Nil(t, extractCCReasoningEffortFromBody([]byte(`{"model":"gpt-5"}`)))
	})
}

func TestForwardAsChatCompletions_RejectsNon2xxBeforeSuccessBridge(t *testing.T) {
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
			gateCalls := 0
			ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
				gateCalls++
				return nil
			}))
			svc := newGatewayBridgeStatusTestService(tt.status)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			body := []byte(`{"model":"gpt-5","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))

			result, err := svc.ForwardAsChatCompletions(
				ctx,
				c,
				newGatewayBridgeStatusTestAccount(),
				body,
				nil,
			)

			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Contains(t, rec.Body.String(), `"error"`)
			require.NotContains(t, rec.Body.String(), `[DONE]`)
			require.Zero(t, gateCalls)
		})
	}
}

func TestHandleCCBufferedFromAnthropic_PreservesMessageStartCacheUsageAndReasoning(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	reasoningEffort := "high"
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_buffered"}},
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
	result, err := svc.handleCCBufferedFromAnthropic(context.Background(), resp, c, "gpt-5", "claude-sonnet-4.5", &reasoningEffort, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 9, result.Usage.CacheReadInputTokens)
	require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
	require.True(t, result.BillingUsageComplete)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "high", *result.ReasoningEffort)
}

func TestHandleCCStreamingFromAnthropic_PreservesMessageStartCacheUsageAndReasoning(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	reasoningEffort := "medium"
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_stream"}},
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
	result, err := svc.handleCCStreamingFromAnthropic(context.Background(), resp, c, "gpt-5", "claude-sonnet-4.5", &reasoningEffort, time.Now(), true)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.Equal(t, 11, result.Usage.CacheReadInputTokens)
	require.Equal(t, 4, result.Usage.CacheCreationInputTokens)
	require.True(t, result.BillingUsageComplete)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "medium", *result.ReasoningEffort)
	require.Contains(t, rec.Body.String(), `[DONE]`)
}

func TestHandleCCBufferedFromAnthropic_RequiresMessageStop(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_buffered_truncated"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_truncated","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCBufferedFromAnthropic(
		context.Background(),
		resp,
		c,
		"gpt-5",
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

func TestHandleCCStreamingFromAnthropic_RequiresMessageStopWithoutDone(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_stream_truncated"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_truncated","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":20}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"partial"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(
		context.Background(),
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
		true,
	)

	require.ErrorContains(t, err, "without message_stop")
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.NotContains(t, rec.Body.String(), `[DONE]`)
	require.NotContains(t, rec.Body.String(), `"finish_reason":"stop"`)
}

func TestHandleCCStreamingFromAnthropic_BillingGateBlocksTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gateErr := errors.New("billing unavailable")
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		require.Equal(t, 20, result.Usage.InputTokens)
		require.Equal(t, 8, result.Usage.OutputTokens)
		require.True(t, result.BillingUsageComplete)
		return gateErr
	}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_stream_gate"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_gate","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":20}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(
		ctx,
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
		true,
	)

	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, err, gateErr)
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.NotContains(t, rec.Body.String(), `[DONE]`)
	require.NotContains(t, rec.Body.String(), `"finish_reason":"stop"`)
}

func TestHandleCCBufferedFromAnthropic_BillingGateBlocksBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gateErr := errors.New("billing unavailable")
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		require.Equal(t, 12, result.Usage.InputTokens)
		require.Equal(t, 7, result.Usage.OutputTokens)
		require.True(t, result.BillingUsageComplete)
		return gateErr
	}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_buffered_gate"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_gate","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCBufferedFromAnthropic(
		ctx,
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, err, gateErr)
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Empty(t, rec.Body.String())
}

func TestHandleCCBufferedFromAnthropic_BillingGateRejectsInputOnlyUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gateCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
		gateCalls++
		return nil
	}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_buffered_input_only"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_input_only","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":12}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCBufferedFromAnthropic(
		ctx,
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorContains(t, err, "no complete billable usage")
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.Zero(t, gateCalls)
	require.Empty(t, rec.Body.String())
}

func TestHandleCCStreamingFromAnthropic_BillingGateRejectsOutputOnlyUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gateCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
		gateCalls++
		return nil
	}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_stream_output_only"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_output_only","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(
		ctx,
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
		true,
	)

	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorContains(t, err, "no complete billable usage")
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.Zero(t, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.False(t, result.BillingUsageComplete)
	require.Zero(t, gateCalls)
	require.NotContains(t, rec.Body.String(), `[DONE]`)
	require.NotContains(t, rec.Body.String(), `"finish_reason":"stop"`)
}

func TestHandleCCBufferedFromAnthropic_BillingGateAcceptsExplicitZeroUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gateCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		gateCalls++
		require.Zero(t, result.Usage.InputTokens)
		require.Zero(t, result.Usage.OutputTokens)
		require.True(t, result.BillingUsageComplete)
		return nil
	}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_buffered_zero_output"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_zero_usage","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":0}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCBufferedFromAnthropic(
		ctx,
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.BillingUsageComplete)
	require.Equal(t, 1, gateCalls)
	require.NotEmpty(t, rec.Body.String())
}

func TestHandleCCStreamingFromAnthropic_DoneWriteFailureAfterBillingPreservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gateCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		gateCalls++
		require.Equal(t, 20, result.Usage.InputTokens)
		require.Equal(t, 8, result.Usage.OutputTokens)
		require.True(t, result.BillingUsageComplete)
		return nil
	}))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writeErr := errors.New("injected done write failure")
	c.Writer = &chatBridgeSelectiveFailWriter{
		ResponseWriter: c.Writer,
		reject:         "[DONE]",
		err:            writeErr,
	}
	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_cc_done_write_error"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_done_write_error","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":20}}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	result, err := (&GatewayService{}).handleCCStreamingFromAnthropic(
		ctx,
		resp,
		c,
		"gpt-5",
		"claude-sonnet-4.5",
		nil,
		time.Now(),
		true,
	)

	require.ErrorIs(t, err, writeErr)
	require.True(t, IsBillableStreamUsageError(err))
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.Equal(t, 1, gateCalls)
	require.Contains(t, rec.Body.String(), `"finish_reason":"stop"`)
	require.NotContains(t, rec.Body.String(), `[DONE]`)
}

type chatBridgeSelectiveFailWriter struct {
	gin.ResponseWriter
	reject string
	err    error
}

func (w *chatBridgeSelectiveFailWriter) Write(payload []byte) (int, error) {
	if strings.Contains(string(payload), w.reject) {
		return 0, w.err
	}
	return w.ResponseWriter.Write(payload)
}

func (w *chatBridgeSelectiveFailWriter) WriteString(payload string) (int, error) {
	if strings.Contains(payload, w.reject) {
		return 0, w.err
	}
	return w.ResponseWriter.WriteString(payload)
}
