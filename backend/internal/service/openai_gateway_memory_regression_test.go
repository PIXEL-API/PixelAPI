package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIGatewayServiceForwardWithAnalysis_RawPatchSkipsFullMapDecode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1e1000 is valid JSON but encoding/json cannot decode it into the float64
	// values used by map[string]any. A successful forward therefore proves that
	// this ordinary raw-patch path did not eagerly decode the complete request.
	// The deliberately spaced extension is also a wire-format sentinel: a full
	// map marshal would normalize it even when json.Number were used.
	const extension = `"future_extension" : { "huge" : 1e1000, "nonce" : 9007199254740993, "label" : "未知字段" }`
	body := []byte(`{"model":"gpt-5","stream":false,"reasoning":{"effort":"minimal"},` + extension + `,"input":"hello"}`)
	analysis, err := AnalyzeOpenAIResponsesRequest(body)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{resp: newOpenAIMemoryRegressionResponse(
		io.NopCloser(strings.NewReader(`{"id":"resp_raw_patch","object":"response","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)),
	)}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(body)

	result, err := svc.ForwardWithAnalysis(context.Background(), c, newOpenAIMemoryRegressionAccount(), body, analysis)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]
	require.True(t, bytes.Contains(forwarded, []byte(extension)), "raw patch must preserve untouched extension bytes")
	require.Equal(t, "1e1000", gjson.GetBytes(forwarded, "future_extension.huge").Raw)
	require.Equal(t, "9007199254740993", gjson.GetBytes(forwarded, "future_extension.nonce").Raw)
	require.Equal(t, "未知字段", gjson.GetBytes(forwarded, "future_extension.label").String())
	require.Equal(t, "none", gjson.GetBytes(forwarded, "reasoning.effort").String())
	require.Equal(t, "You are a helpful coding assistant.", gjson.GetBytes(forwarded, "instructions").String())
}

func TestProjectOpenAICleanRelaySessionBodyDropsUnrelatedLargePayload(t *testing.T) {
	body := []byte(`{"model":"gpt-5","prompt_cache_key":"session-123","client_metadata":{"x-codex-installation-id":"install-456","other":"ignored"},"input":"` + strings.Repeat("x", 1<<20) + `"}`)

	projected, err := ProjectOpenAICleanRelaySessionBody(body)

	require.NoError(t, err)
	require.Less(t, len(projected), 256)
	require.Equal(t, "session-123", gjson.GetBytes(projected, "prompt_cache_key").String())
	require.Equal(t, "install-456", gjson.GetBytes(projected, "client_metadata.x-codex-installation-id").String())
	require.False(t, gjson.GetBytes(projected, "input").Exists())
	require.False(t, gjson.GetBytes(projected, "client_metadata.other").Exists())
}

func TestOpenAIGatewayServiceForwardWithAnalysis_NonStreamingDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"gpt-5","stream":false,"instructions":"test","input":"hello"}`)
	responseBody := newOpenAIBlockingResponseBody(
		`{"id":"resp_request_release","object":"response","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`,
	)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.ForwardWithAnalysis(context.Background(), c, newOpenAIMemoryRegressionAccount(), requestBody, nil)
		done <- forwardOutcome{result: result, err: err}
	}()
	defer responseBody.unblock()

	select {
	case <-responseBody.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("non-streaming response body was not read")
	}

	// openAIResponseRequestLinkUpstream deliberately attaches the outbound
	// request to the response, matching net/http. The response body is still
	// blocked here, so nil proves the large request graph was detached before
	// the potentially long non-streaming body read, not merely on return.
	if response.Request != nil {
		t.Error("non-streaming response still retains its outbound request while reading the response body")
	}

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
	case <-time.After(2 * time.Second):
		t.Fatal("forward did not complete after unblocking the response body")
	}
}

func TestOpenAIGatewayServiceForwardWithAnalysis_PassthroughDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"gpt-5","stream":false,"instructions":"test","input":"hello"}`)
	responseBody := newOpenAIBlockingResponseBody(
		`{"id":"resp_passthrough_release","object":"response","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`,
	)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newOpenAIMemoryRegressionAccount()
	account.Extra["openai_passthrough"] = true

	done := make(chan error, 1)
	go func() {
		_, forwardErr := svc.ForwardWithAnalysis(context.Background(), c, account, requestBody, nil)
		done <- forwardErr
	}()
	defer responseBody.unblock()

	select {
	case <-responseBody.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("passthrough response body was not read")
	}
	require.Nil(t, response.Request, "passthrough response retained its outbound request while reading the body")

	responseBody.unblock()
	select {
	case forwardErr := <-done:
		require.NoError(t, forwardErr)
	case <-time.After(2 * time.Second):
		t.Fatal("passthrough forward did not complete after unblocking the response body")
	}
}

func TestOpenAIGatewayServiceForwardWithAnalysis_ErrorDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"gpt-5","stream":false,"instructions":"test","input":"hello"}`)
	responseBody := newOpenAIBlockingResponseBody(`{"error":{"type":"invalid_request_error","message":"bad request"}}`)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	response.StatusCode = http.StatusBadRequest
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)

	done := make(chan error, 1)
	go func() {
		_, forwardErr := svc.ForwardWithAnalysis(context.Background(), c, newOpenAIMemoryRegressionAccount(), requestBody, nil)
		done <- forwardErr
	}()
	defer responseBody.unblock()

	select {
	case <-responseBody.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("error response body was not read")
	}
	require.Nil(t, response.Request, "error response retained its outbound request while reading the body")

	responseBody.unblock()
	select {
	case forwardErr := <-done:
		require.Error(t, forwardErr)
	case <-time.After(2 * time.Second):
		t.Fatal("error forward did not complete after unblocking the response body")
	}
}

func TestOpenAIGatewayServiceForwardWithAnalysis_MappedSparkSkipsImageBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"spark-alias","stream":false,"instructions":"test","input":"hello","future_extension":1e1000}`)
	analysis, err := AnalyzeOpenAIResponsesRequest(requestBody)
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{resp: newOpenAIMemoryRegressionResponse(
		io.NopCloser(strings.NewReader(`{"id":"resp_spark","object":"response","model":"gpt-5.3-codex-spark","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)),
	)}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
	account := newOpenAIMemoryRegressionAccount()
	account.Credentials["model_mapping"] = map[string]any{"spark-alias": "gpt-5.3-codex-spark"}

	result, err := svc.ForwardWithAnalysis(context.Background(), c, account, requestBody, analysis)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]
	require.Equal(t, "gpt-5.3-codex-spark", gjson.GetBytes(forwarded, "model").String())
	require.False(t, openAIJSONToolsContainImageGeneration(gjson.GetBytes(forwarded, "tools")))
	require.False(t, gjson.GetBytes(forwarded, "tool_choice").Exists())
	require.NotContains(t, gjson.GetBytes(forwarded, "instructions").String(), codexImageGenerationBridgeMarker)
	require.Equal(t, "1e1000", gjson.GetBytes(forwarded, "future_extension").Raw)
}

func TestOpenAIGatewayServiceForwardWithAnalysis_NonEmptyBase64ImageStaysRaw(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"gpt-5","stream":false,"instructions":"test","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,QUJD"}]}],"future_extension":1e1000}`)
	analysis, err := AnalyzeOpenAIResponsesRequest(requestBody)
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{resp: newOpenAIMemoryRegressionResponse(
		io.NopCloser(strings.NewReader(`{"id":"resp_nonempty_image","object":"response","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)),
	)}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)

	result, err := svc.ForwardWithAnalysis(context.Background(), c, newOpenAIMemoryRegressionAccount(), requestBody, analysis)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]
	require.Equal(t, "data:image/png;base64,QUJD", gjson.GetBytes(forwarded, "input.0.content.0.image_url").String())
	require.Equal(t, "1e1000", gjson.GetBytes(forwarded, "future_extension").Raw)
}

func TestOpenAIGatewayServiceForwardWithAnalysis_PassthroughNonEmptyBase64ImageStaysRaw(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"gpt-5","stream":false,"instructions":"test","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,QUJD"}]}],"future_extension":1e1000}`)
	analysis, err := AnalyzeOpenAIResponsesRequest(requestBody)
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{resp: newOpenAIMemoryRegressionResponse(
		io.NopCloser(strings.NewReader(`{"id":"resp_passthrough_nonempty_image","object":"response","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)),
	)}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newOpenAIMemoryRegressionAccount()
	account.Extra["openai_passthrough"] = true

	result, err := svc.ForwardWithAnalysis(context.Background(), c, account, requestBody, analysis)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]
	require.Equal(t, "data:image/png;base64,QUJD", gjson.GetBytes(forwarded, "input.0.content.0.image_url").String())
	require.Equal(t, "1e1000", gjson.GetBytes(forwarded, "future_extension").Raw)
}

func TestForwardGrokResponsesDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"grok","stream":false,"reasoning":{"effort":"low"},"input":"hello","future_extension":1e1000}`)
	responseBody := newOpenAIBlockingResponseBody(
		`{"id":"resp_grok_release","object":"response","model":"grok-4.5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
	)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newGrokMemoryRegressionAccount()

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.forwardGrokResponses(context.Background(), c, account, requestBody, "grok", false, time.Now())
		done <- forwardOutcome{result: result, err: err}
	}()
	defer responseBody.unblock()

	select {
	case <-responseBody.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Grok response body was not read")
	}
	require.Nil(t, response.Request, "Grok response retained its outbound request while reading the body")

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.NotNil(t, outcome.result.ReasoningEffort)
		require.Equal(t, "low", *outcome.result.ReasoningEffort)
	case <-time.After(2 * time.Second):
		t.Fatal("Grok forward did not complete after unblocking the response body")
	}
}

func TestForwardGrokChatCompletionsDropsResponseRequestBeforeSSERead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"grok","stream":false,"reasoning":{"effort":"low"},"service_tier":"priority","input":"hello","future_extension":1e1000}`)
	responseBody := newOpenAIBlockingResponseBody(
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_grok_chat_release\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
	)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	response.Header.Set("Content-Type", "text/event-stream")
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newGrokMemoryRegressionAccount()

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.forwardGrokChatCompletions(
			context.Background(),
			c,
			account,
			requestBody,
			"grok",
			"grok",
			"grok-4.5",
			false,
			false,
			time.Now(),
		)
		done <- forwardOutcome{result: result, err: err}
	}()
	defer responseBody.unblock()

	select {
	case <-responseBody.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Grok Chat bridge response body was not read")
	}
	require.Nil(t, response.Request, "Grok Chat bridge retained its outbound request while reading the SSE body")

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.NotNil(t, outcome.result.ReasoningEffort)
		require.Equal(t, "low", *outcome.result.ReasoningEffort)
		require.NotNil(t, outcome.result.ServiceTier)
		require.Equal(t, "priority", *outcome.result.ServiceTier)
	case <-time.After(2 * time.Second):
		t.Fatal("Grok Chat bridge did not complete after unblocking the response body")
	}
}

func TestForwardGrokRawChatCompletionsDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"grok-4","stream":false,"reasoning_effort":"low","messages":[{"role":"user","content":"hello"}],"future_extension":"` + strings.Repeat("x", 32<<10) + `"}`)
	responseBody := newOpenAIBlockingResponseBody(
		`{"id":"chatcmpl_grok_raw_release","object":"chat.completion","model":"grok-4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
	)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newGrokMemoryRegressionAccount()

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.forwardGrokRawChatCompletions(context.Background(), c, account, requestBody, "grok-4")
		done <- forwardOutcome{result: result, err: err}
	}()
	defer responseBody.unblock()

	waitForOpenAIResponseBodyRead(t, responseBody)
	require.Nil(t, response.Request, "Grok raw Chat retained its outbound request while reading the response body")

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.Equal(t, "grok-4", outcome.result.Model)
	case <-time.After(2 * time.Second):
		t.Fatal("Grok raw Chat did not complete after unblocking the response body")
	}
}

func TestDescribeGrokComposerImageDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	responseBody := newOpenAIBlockingResponseBody(
		`{"id":"resp_grok_composer_release","object":"response","model":"grok-build-0.1","output":[{"type":"message","content":[{"type":"output_text","text":"a concise description"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
	)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(nil)
	account := newGrokMemoryRegressionAccount()
	imageURL := "data:image/png;base64," + strings.Repeat("A", 32<<10)

	type describeOutcome struct {
		description string
		usage       OpenAIUsage
		err         error
	}
	done := make(chan describeOutcome, 1)
	go func() {
		description, usage, err := svc.describeGrokComposerImage(context.Background(), c, account, "xai-test", imageURL, 1)
		done <- describeOutcome{description: description, usage: usage, err: err}
	}()
	defer responseBody.unblock()

	waitForOpenAIResponseBodyRead(t, responseBody)
	require.Nil(t, response.Request, "Grok Composer image bridge retained its base64 request while reading the response body")

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.Equal(t, "a concise description", outcome.description)
		require.Equal(t, 1, outcome.usage.InputTokens)
		require.Equal(t, 2, outcome.usage.OutputTokens)
	case <-time.After(2 * time.Second):
		t.Fatal("Grok Composer image bridge did not complete after unblocking the response body")
	}
}

func TestForwardGrokMediaDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":"grok-imagine-image-quality","prompt":"` + strings.Repeat("draw ", 8<<10) + `","n":1}`)
	responseBody := newOpenAIBlockingResponseBody(`{"data":[{"url":"https://images.example/result.png"}]}`)
	response := newOpenAIMemoryRegressionResponse(responseBody)
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newGrokMemoryRegressionAccount()

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.ForwardGrokMedia(
			context.Background(), c, account, GrokMediaEndpointImagesGenerations, "", requestBody, "application/json",
		)
		done <- forwardOutcome{result: result, err: err}
	}()
	defer responseBody.unblock()

	waitForOpenAIResponseBodyRead(t, responseBody)
	require.Nil(t, response.Request, "Grok media retained its outbound request while reading the response body")

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.Equal(t, 1, outcome.result.ImageCount)
	case <-time.After(2 * time.Second):
		t.Fatal("Grok media forward did not complete after unblocking the response body")
	}
}

func TestForwardGrokVoiceDropsResponseRequestBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"text":"` + strings.Repeat("speak ", 8<<10) + `","voice":"eve"}`)
	responseBody := newOpenAIBlockingResponseBody("audio-data")
	response := newOpenAIMemoryRegressionResponse(responseBody)
	response.Header.Set("Content-Type", "audio/mpeg")
	upstream := &openAIResponseRequestLinkUpstream{response: response}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)
	account := newGrokMemoryRegressionAccount()

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.ForwardGrokVoice(context.Background(), c, account, "tts", requestBody, "application/json")
		done <- forwardOutcome{result: result, err: err}
	}()
	defer responseBody.unblock()

	waitForOpenAIResponseBodyRead(t, responseBody)
	require.Nil(t, response.Request, "Grok voice retained its outbound request while reading the response body")

	responseBody.unblock()
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.Equal(t, "tts", outcome.result.Model)
	case <-time.After(2 * time.Second):
		t.Fatal("Grok voice forward did not complete after unblocking the response body")
	}
}

func TestOpenAIGatewayServiceForwardWithAnalysis_NormalizesRawModelAndServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestBody := []byte(`{"model":" gpt-5 ","service_tier":" priority ","stream":false,"instructions":"test","input":"hello"}`)
	analysis, err := AnalyzeOpenAIResponsesRequest(requestBody)
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{resp: newOpenAIMemoryRegressionResponse(
		io.NopCloser(strings.NewReader(`{"id":"resp_normalized","object":"response","model":"gpt-5","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)),
	)}
	svc := newOpenAIMemoryRegressionService(upstream)
	c := newOpenAIMemoryRegressionContext(requestBody)

	result, err := svc.ForwardWithAnalysis(context.Background(), c, newOpenAIMemoryRegressionAccount(), requestBody, analysis)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	forwarded := upstream.bodies[0]
	require.Equal(t, "gpt-5", gjson.GetBytes(forwarded, "model").String())
	require.Equal(t, `"gpt-5"`, gjson.GetBytes(forwarded, "model").Raw)
	require.Equal(t, "priority", gjson.GetBytes(forwarded, "service_tier").String())
	require.Equal(t, `"priority"`, gjson.GetBytes(forwarded, "service_tier").Raw)
}

func newOpenAIMemoryRegressionService(upstream HTTPUpstream) *OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	return &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
}

func newOpenAIMemoryRegressionAccount() *Account {
	return &Account{
		ID:          6101,
		Name:        "openai-memory-regression",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://example.com",
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesSupported: true,
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newGrokMemoryRegressionAccount() *Account {
	return &Account{
		ID:          6102,
		Name:        "grok-memory-regression",
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "xai-test",
			"base_url": "https://api.x.ai/v1",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newOpenAIMemoryRegressionContext(body []byte) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "curl/8.0")
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	return c
}

func newOpenAIMemoryRegressionResponse(body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"req_memory_regression"},
		},
		Body: body,
	}
}

type openAIResponseRequestLinkUpstream struct {
	response *http.Response
}

func (u *openAIResponseRequestLinkUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if req != nil && req.Body != nil {
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return nil, err
		}
		if err := req.Body.Close(); err != nil {
			return nil, err
		}
	}
	u.response.Request = req
	return u.response, nil
}

func (u *openAIResponseRequestLinkUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type openAIBlockingResponseBody struct {
	reader      *strings.Reader
	readStarted chan struct{}
	allowRead   chan struct{}
	startOnce   sync.Once
	unblockOnce sync.Once
}

func newOpenAIBlockingResponseBody(body string) *openAIBlockingResponseBody {
	return &openAIBlockingResponseBody{
		reader:      strings.NewReader(body),
		readStarted: make(chan struct{}),
		allowRead:   make(chan struct{}),
	}
}

func (b *openAIBlockingResponseBody) Read(p []byte) (int, error) {
	b.startOnce.Do(func() { close(b.readStarted) })
	<-b.allowRead
	return b.reader.Read(p)
}

func (b *openAIBlockingResponseBody) Close() error {
	b.unblock()
	return nil
}

func (b *openAIBlockingResponseBody) unblock() {
	b.unblockOnce.Do(func() { close(b.allowRead) })
}

func waitForOpenAIResponseBodyRead(t *testing.T, body *openAIBlockingResponseBody) {
	t.Helper()
	select {
	case <-body.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream response body was not read")
	}
}
