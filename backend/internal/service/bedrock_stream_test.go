package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func buildBedrockEventStreamFrameForTest(eventType string, payload []byte) []byte {
	var headersBuf bytes.Buffer
	_ = headersBuf.WriteByte(byte(len(":event-type")))
	_, _ = headersBuf.WriteString(":event-type")
	_ = headersBuf.WriteByte(7)
	_ = binary.Write(&headersBuf, binary.BigEndian, uint16(len(eventType)))
	_, _ = headersBuf.WriteString(eventType)
	_ = headersBuf.WriteByte(byte(len(":message-type")))
	_, _ = headersBuf.WriteString(":message-type")
	_ = headersBuf.WriteByte(7)
	_ = binary.Write(&headersBuf, binary.BigEndian, uint16(len("event")))
	_, _ = headersBuf.WriteString("event")

	headers := headersBuf.Bytes()
	headersLen := uint32(len(headers))
	totalLen := uint32(12 + len(headers) + len(payload) + 4)

	var preludeBuf bytes.Buffer
	_ = binary.Write(&preludeBuf, binary.BigEndian, totalLen)
	_ = binary.Write(&preludeBuf, binary.BigEndian, headersLen)
	preludeBytes := preludeBuf.Bytes()

	var frame bytes.Buffer
	_, _ = frame.Write(preludeBytes)
	_ = binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(preludeBytes))
	_, _ = frame.Write(headers)
	_, _ = frame.Write(payload)
	_ = binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes()))
	return frame.Bytes()
}

func buildBedrockChunkFrameForTest(data string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	return buildBedrockEventStreamFrameForTest("chunk", []byte(`{"bytes":"`+encoded+`"}`))
}

func buildBedrockEventStreamPreludeForTest(totalLength, headersLength uint32) []byte {
	var prelude bytes.Buffer
	_ = binary.Write(&prelude, binary.BigEndian, totalLength)
	_ = binary.Write(&prelude, binary.BigEndian, headersLength)
	_ = binary.Write(&prelude, binary.BigEndian, crc32.ChecksumIEEE(prelude.Bytes()))
	return prelude.Bytes()
}

func TestExtractBedrockChunkData(t *testing.T) {
	t.Run("valid base64 payload", func(t *testing.T) {
		original := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`
		b64 := base64.StdEncoding.EncodeToString([]byte(original))
		payload := []byte(`{"bytes":"` + b64 + `"}`)

		result, err := extractBedrockChunkData(payload)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.JSONEq(t, original, string(result))
	})

	t.Run("empty bytes field", func(t *testing.T) {
		result, err := extractBedrockChunkData([]byte(`{"bytes":""}`))
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("no bytes field", func(t *testing.T) {
		result, err := extractBedrockChunkData([]byte(`{"other":"value"}`))
		require.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("invalid base64", func(t *testing.T) {
		result, err := extractBedrockChunkData([]byte(`{"bytes":"not-valid-base64!!!"}`))
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestTransformBedrockInvocationMetrics(t *testing.T) {
	t.Run("converts metrics to usage", func(t *testing.T) {
		input := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":150,"outputTokenCount":42}}`
		result := transformBedrockInvocationMetrics([]byte(input))

		// amazon-bedrock-invocationMetrics should be removed
		assert.False(t, gjson.GetBytes(result, "amazon-bedrock-invocationMetrics").Exists())
		// usage should be set
		assert.Equal(t, int64(150), gjson.GetBytes(result, "usage.input_tokens").Int())
		assert.Equal(t, int64(42), gjson.GetBytes(result, "usage.output_tokens").Int())
		// original fields preserved
		assert.Equal(t, "message_delta", gjson.GetBytes(result, "type").String())
		assert.Equal(t, "end_turn", gjson.GetBytes(result, "delta.stop_reason").String())
	})

	t.Run("no metrics present", func(t *testing.T) {
		input := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`
		result := transformBedrockInvocationMetrics([]byte(input))
		assert.JSONEq(t, input, string(result))
	})

	t.Run("does not overwrite existing usage", func(t *testing.T) {
		input := `{"type":"message_delta","usage":{"output_tokens":100},"amazon-bedrock-invocationMetrics":{"inputTokenCount":150,"outputTokenCount":42}}`
		result := transformBedrockInvocationMetrics([]byte(input))

		// metrics removed but existing usage preserved
		assert.False(t, gjson.GetBytes(result, "amazon-bedrock-invocationMetrics").Exists())
		assert.Equal(t, int64(100), gjson.GetBytes(result, "usage.output_tokens").Int())
	})
}

func TestExtractEventStreamHeaderValue(t *testing.T) {
	// Build a header with :event-type = "chunk" (string type = 7)
	buildStringHeader := func(name, value string) []byte {
		var buf bytes.Buffer
		// name length (1 byte)
		_ = buf.WriteByte(byte(len(name)))
		// name
		_, _ = buf.WriteString(name)
		// value type (7 = string)
		_ = buf.WriteByte(7)
		// value length (2 bytes, big-endian)
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(value)))
		// value
		_, _ = buf.WriteString(value)
		return buf.Bytes()
	}

	t.Run("find string header", func(t *testing.T) {
		headers := buildStringHeader(":event-type", "chunk")
		assert.Equal(t, "chunk", extractEventStreamHeaderValue(headers, ":event-type"))
	})

	t.Run("header not found", func(t *testing.T) {
		headers := buildStringHeader(":event-type", "chunk")
		assert.Equal(t, "", extractEventStreamHeaderValue(headers, ":message-type"))
	})

	t.Run("multiple headers", func(t *testing.T) {
		var buf bytes.Buffer
		_, _ = buf.Write(buildStringHeader(":content-type", "application/json"))
		_, _ = buf.Write(buildStringHeader(":event-type", "chunk"))
		_, _ = buf.Write(buildStringHeader(":message-type", "event"))

		headers := buf.Bytes()
		assert.Equal(t, "chunk", extractEventStreamHeaderValue(headers, ":event-type"))
		assert.Equal(t, "application/json", extractEventStreamHeaderValue(headers, ":content-type"))
		assert.Equal(t, "event", extractEventStreamHeaderValue(headers, ":message-type"))
	})

	t.Run("empty headers", func(t *testing.T) {
		assert.Equal(t, "", extractEventStreamHeaderValue([]byte{}, ":event-type"))
	})
}

func TestBedrockEventStreamDecoder(t *testing.T) {
	buildFrame := buildBedrockEventStreamFrameForTest

	t.Run("decode chunk event", func(t *testing.T) {
		payload := []byte(`{"bytes":"dGVzdA=="}`) // base64("test")
		frame := buildFrame("chunk", payload)

		decoder := newBedrockEventStreamDecoder(bytes.NewReader(frame))
		result, err := decoder.Decode()
		require.NoError(t, err)
		assert.Equal(t, payload, result)
	})

	t.Run("skip non-chunk events", func(t *testing.T) {
		// Write initial-response followed by chunk
		var buf bytes.Buffer
		_, _ = buf.Write(buildFrame("initial-response", []byte(`{}`)))
		chunkPayload := []byte(`{"bytes":"aGVsbG8="}`)
		_, _ = buf.Write(buildFrame("chunk", chunkPayload))

		decoder := newBedrockEventStreamDecoder(&buf)
		result, err := decoder.Decode()
		require.NoError(t, err)
		assert.Equal(t, chunkPayload, result)
	})

	t.Run("EOF on empty input", func(t *testing.T) {
		decoder := newBedrockEventStreamDecoder(bytes.NewReader(nil))
		_, err := decoder.Decode()
		assert.Equal(t, io.EOF, err)
	})

	t.Run("reject oversized total length before allocation", func(t *testing.T) {
		prelude := buildBedrockEventStreamPreludeForTest(bedrockEventStreamMaxMessageBytes+1, 0)
		decoder := newBedrockEventStreamDecoder(bytes.NewReader(prelude))
		_, err := decoder.Decode()
		require.ErrorContains(t, err, "total_length")
		require.ErrorContains(t, err, "exceeds limit")
	})

	t.Run("reject oversized headers before allocation", func(t *testing.T) {
		prelude := buildBedrockEventStreamPreludeForTest(
			bedrockEventStreamMaxMessageBytes,
			bedrockEventStreamMaxHeadersBytes+1,
		)
		decoder := newBedrockEventStreamDecoder(bytes.NewReader(prelude))
		_, err := decoder.Decode()
		require.ErrorContains(t, err, "headers_length")
		require.ErrorContains(t, err, "exceeds limit")
	})

	t.Run("reject headers longer than frame data", func(t *testing.T) {
		prelude := buildBedrockEventStreamPreludeForTest(16, 1)
		decoder := newBedrockEventStreamDecoder(bytes.NewReader(prelude))
		_, err := decoder.Decode()
		require.ErrorContains(t, err, "headers_length")
		require.ErrorContains(t, err, "exceeds available")
	})

	t.Run("corrupted prelude CRC", func(t *testing.T) {
		frame := buildFrame("chunk", []byte(`{"bytes":"dGVzdA=="}`))
		// Corrupt the prelude CRC (bytes 8-11)
		frame[8] ^= 0xFF
		decoder := newBedrockEventStreamDecoder(bytes.NewReader(frame))
		_, err := decoder.Decode()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prelude CRC mismatch")
	})

	t.Run("corrupted message CRC", func(t *testing.T) {
		frame := buildFrame("chunk", []byte(`{"bytes":"dGVzdA=="}`))
		// Corrupt the message CRC (last 4 bytes)
		frame[len(frame)-1] ^= 0xFF
		decoder := newBedrockEventStreamDecoder(bytes.NewReader(frame))
		_, err := decoder.Decode()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "message CRC mismatch")
	})

	t.Run("castagnoli encoded frame is rejected", func(t *testing.T) {
		castagnoliTab := crc32.MakeTable(crc32.Castagnoli)
		payload := []byte(`{"bytes":"dGVzdA=="}`)

		var headersBuf bytes.Buffer
		_ = headersBuf.WriteByte(byte(len(":event-type")))
		_, _ = headersBuf.WriteString(":event-type")
		_ = headersBuf.WriteByte(7)
		_ = binary.Write(&headersBuf, binary.BigEndian, uint16(len("chunk")))
		_, _ = headersBuf.WriteString("chunk")

		headers := headersBuf.Bytes()
		headersLen := uint32(len(headers))
		totalLen := uint32(12 + len(headers) + len(payload) + 4)

		var preludeBuf bytes.Buffer
		_ = binary.Write(&preludeBuf, binary.BigEndian, totalLen)
		_ = binary.Write(&preludeBuf, binary.BigEndian, headersLen)
		preludeBytes := preludeBuf.Bytes()

		var frame bytes.Buffer
		_, _ = frame.Write(preludeBytes)
		_ = binary.Write(&frame, binary.BigEndian, crc32.Checksum(preludeBytes, castagnoliTab))
		_, _ = frame.Write(headers)
		_, _ = frame.Write(payload)
		_ = binary.Write(&frame, binary.BigEndian, crc32.Checksum(frame.Bytes(), castagnoliTab))

		decoder := newBedrockEventStreamDecoder(bytes.NewReader(frame.Bytes()))
		_, err := decoder.Decode()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prelude CRC mismatch")
	})
}

func TestHandleBedrockStreamingResponseBillingGateBlocksMessageStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	var stream bytes.Buffer
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":9,"outputTokenCount":4}}`))
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_stop"}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"x-amzn-requestid": []string{"bedrock-request"}},
		Body:       io.NopCloser(bytes.NewReader(stream.Bytes())),
	}
	submitErr := errors.New("billing unavailable")
	submitCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		submitCalls++
		require.Equal(t, 9, result.Usage.InputTokens)
		require.Equal(t, 4, result.Usage.OutputTokens)
		return submitErr
	}))
	svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	result, err := svc.handleBedrockStreamingResponseWithModels(
		ctx,
		resp,
		c,
		&Account{ID: 17},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NotNil(t, result)
	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, err, submitErr)
	require.Equal(t, 1, submitCalls)
	require.Contains(t, recorder.Body.String(), `"type":"message_delta"`)
	require.NotContains(t, recorder.Body.String(), `"type":"message_stop"`)
}

func TestHandleBedrockStreamingResponseBillingGateRejectsPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	var stream bytes.Buffer
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_delta","amazon-bedrock-invocationMetrics":{"inputTokenCount":9}}`))
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_stop"}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(stream.Bytes())),
	}
	submitCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
		submitCalls++
		return nil
	}))
	svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	result, err := svc.handleBedrockStreamingResponseWithModels(
		ctx,
		resp,
		c,
		&Account{ID: 19},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NotNil(t, result)
	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.Zero(t, submitCalls)
	require.NotContains(t, recorder.Body.String(), `"type":"message_stop"`)
}

func TestHandleBedrockStreamingResponseRejectsInvalidChunkBeforeLaterTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	var stream bytes.Buffer
	_, _ = stream.Write(buildBedrockEventStreamFrameForTest("chunk", []byte(`{"bytes":"not-valid-base64!!!"}`)))
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_stop"}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(stream.Bytes())),
	}
	svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	result, err := svc.handleBedrockStreamingResponseWithModels(
		context.Background(),
		resp,
		c,
		&Account{ID: 20},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NotNil(t, result)
	require.ErrorContains(t, err, "decode bedrock chunk")
	require.Empty(t, recorder.Body.String())
}

func TestHandleBedrockStreamingResponseStopsAfterMessageStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	var stream bytes.Buffer
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_delta","amazon-bedrock-invocationMetrics":{"inputTokenCount":3,"outputTokenCount":2}}`))
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_stop"}`))
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"must-not-leak"}}`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(stream.Bytes())),
	}
	svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	result, err := svc.handleBedrockStreamingResponseWithModels(
		context.Background(),
		resp,
		c,
		&Account{ID: 21},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, recorder.Body.String(), `"type":"message_stop"`)
	require.NotContains(t, recorder.Body.String(), "must-not-leak")
}

func TestHandleBedrockNonStreamingResponseBillingGateBlocksBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"x-amzn-requestid": []string{"bedrock-nonstream"}},
		Body:       io.NopCloser(strings.NewReader(`{"type":"message","usage":{"input_tokens":9,"output_tokens":4}}`)),
	}
	submitErr := errors.New("billing unavailable")
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
		return submitErr
	}))
	svc := &GatewayService{cfg: &config.Config{}}

	usage, err := svc.handleBedrockNonStreamingResponseWithModels(
		ctx,
		resp,
		c,
		&Account{ID: 22},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NotNil(t, usage)
	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, err, submitErr)
	require.Empty(t, recorder.Body.String())
}

func TestHandleBedrockNonStreamingResponseBillingGateRejectsPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"type":"message","usage":{"input_tokens":9}}`)),
	}
	submitCalls := 0
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
		submitCalls++
		return nil
	}))
	svc := &GatewayService{cfg: &config.Config{}}

	usage, err := svc.handleBedrockNonStreamingResponseWithModels(
		ctx,
		resp,
		c,
		&Account{ID: 23},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NotNil(t, usage)
	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.Zero(t, submitCalls)
	require.Empty(t, recorder.Body.String())
}

func TestHandleBedrockStreamingResponseRejectsDoneWithoutMessageStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	var stream bytes.Buffer
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`{"type":"message_delta","amazon-bedrock-invocationMetrics":{"inputTokenCount":3,"outputTokenCount":2}}`))
	_, _ = stream.Write(buildBedrockChunkFrameForTest(`[DONE]`))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(stream.Bytes())),
	}
	svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}

	result, err := svc.handleBedrockStreamingResponseWithModels(
		context.Background(),
		resp,
		c,
		&Account{ID: 18},
		time.Now(),
		"claude-original",
		"claude-bedrock",
	)

	require.NotNil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected [DONE] before message_stop")
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestBuildBedrockURL(t *testing.T) {
	t.Run("stream URL with colon in model ID", func(t *testing.T) {
		url := BuildBedrockURL("us-east-1", "us.anthropic.claude-opus-4-5-20251101-v1:0", true)
		assert.Equal(t, "https://bedrock-runtime.us-east-1.amazonaws.com/model/us.anthropic.claude-opus-4-5-20251101-v1%3A0/invoke-with-response-stream", url)
	})

	t.Run("non-stream URL with colon in model ID", func(t *testing.T) {
		url := BuildBedrockURL("eu-west-1", "eu.anthropic.claude-sonnet-4-5-20250929-v1:0", false)
		assert.Equal(t, "https://bedrock-runtime.eu-west-1.amazonaws.com/model/eu.anthropic.claude-sonnet-4-5-20250929-v1%3A0/invoke", url)
	})

	t.Run("model ID without colon", func(t *testing.T) {
		url := BuildBedrockURL("us-east-1", "us.anthropic.claude-sonnet-4-6", true)
		assert.Equal(t, "https://bedrock-runtime.us-east-1.amazonaws.com/model/us.anthropic.claude-sonnet-4-6/invoke-with-response-stream", url)
	})
}
