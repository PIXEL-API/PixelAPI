package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// opencodeMessagesRawEndpoint 是 OpenCode Go 订阅的 Anthropic 兼容端点路径。
const opencodeMessagesRawEndpoint = "/v1/messages"

// opencodeChatOnlyModels 列出 opencode 仅在 OpenAI chat/completions 格式提供、
// 不支持 Anthropic /messages 格式的模型。这些模型的 /v1/messages 入站请求必须走
// Anthropic→ChatCompletions 转换，直接透传上游会返回
// 401 "Model <x> is not supported for format anthropic"。
var opencodeChatOnlyModels = map[string]struct{}{
	"grok-4.5": {},
}

// opencodeSupportsAnthropicMessagesFormat 判断模型是否支持 opencode 的
// Anthropic /messages 原生格式（false = 需走 chat/completions 转换）。
func opencodeSupportsAnthropicMessagesFormat(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	model = normalizeClaudeCodeLongContextModel(model)
	_, chatOnly := opencodeChatOnlyModels[model]
	return !chatOnly
}

// forwardAsRawAnthropicMessages 将 Anthropic Messages 请求体原样 POST 到
// OpenCode Go 的 /messages 端点。opencode 账号只走这条 raw 转发路径，不做
// Anthropic → OpenAI 协议转换，计费通过解析 Anthropic usage 映射到 OpenAIUsage。
func (s *OpenAIGatewayService) forwardAsRawAnthropicMessages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	SetActualOpenAIUpstreamEndpoint(c, opencodeMessagesRawEndpoint)

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()

	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}

	forwardResult := &OpenAIForwardResult{
		Model:         originalModel,
		BillingModel:  billingModel,
		UpstreamModel: upstreamModel,
		Stream:        clientStream,
	}
	ctx = withOpenAIForwardResultBillingState(ctx, c, forwardResult, startTime, openAIResponseImageBillingConfig{})

	token := account.GetOpenAIApiKey()
	if token == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := account.GetOpencodeBaseURL()
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	targetURL := buildOpenAIMessagesURL(validatedURL)

	upstreamCtx, releaseUpstreamCtx := s.detachOpenAIUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	// opencode 的 /messages 端点是 Anthropic 兼容，用 x-api-key 鉴权（不是 Authorization: Bearer）。
	// 实测 Bearer 会被上游判为 "Missing API key"。
	upstreamReq.Header.Set("x-api-key", token)
	upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	if clientStream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			if !openaiChatRawAllowedHeaders[strings.ToLower(key)] {
				continue
			}
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}
	}
	if userAgent := strings.TrimSpace(account.GetOpenAIUserAgent()); userAgent != "" {
		upstreamReq.Header.Set("user-agent", userAgent)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.resolveOpenAIAccountTLSProfile(account))
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:    account.Platform,
			AccountID:   account.ID,
			AccountName: account.Name,
			Kind:        "request_error",
			Message:     safeErr,
		})
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(resp, c, account, false, writeAnthropicError)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			s.handleOpenAIAccountUpstreamErrorForModel(ctx, account, originalModel, resp.StatusCode, resp.Header, respBody)
			return nil, newOpenAIUpstreamFailoverError(
				resp.StatusCode,
				resp.Header,
				respBody,
				upstreamMsg,
				shouldRetryOpenAIOnSamePoolAccount(account, resp.StatusCode, upstreamMsg, respBody),
			)
		}
		return s.handleAnthropicErrorResponse(resp, c, account, originalModel)
	}

	if clientStream {
		return s.streamRawAnthropicMessages(ctx, c, resp, originalModel, billingModel, upstreamModel, startTime)
	}
	return s.bufferRawAnthropicMessages(ctx, c, resp, originalModel, billingModel, upstreamModel, startTime)
}

func (s *OpenAIGatewayService) streamRawAnthropicMessages(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	requestID := resp.Header.Get("x-request-id")
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var usage OpenAIUsage
	var responseID string
	var firstTokenMs *int
	sawMessageStop := false
	clientDisconnected := false
	var cancelDisconnectedDrain context.CancelFunc
	defer func() {
		if cancelDisconnectedDrain != nil {
			cancelDisconnectedDrain()
		}
	}()
	startDisconnectedDrain := func() {
		if cancelDisconnectedDrain == nil {
			cancelDisconnectedDrain = s.startDisconnectedStreamDrainDeadline(ctx, resp.Body, requestID)
		}
	}
	resultWithUsage := func() *OpenAIForwardResult {
		result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
			requestID:            requestID,
			responseID:           responseID,
			usage:                &usage,
			firstTokenMs:         firstTokenMs,
			responseHeaders:      resp.Header,
			billingUsageComplete: sawMessageStop,
		})
		result.Model = originalModel
		result.BillingModel = billingModel
		result.UpstreamModel = upstreamModel
		result.UpstreamResponseModel = observedUpstreamResponseModel(c)
		result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
		result.Stream = true
		return result
	}

	for scanner.Scan() {
		line := scanner.Text()
		if payload, ok := extractOpenAISSEDataLine(line); ok {
			trimmedPayload := strings.TrimSpace(payload)
			if trimmedPayload != "" && trimmedPayload != "[DONE]" {
				observer.ObserveAnthropic([]byte(payload))
				mergeAnthropicStreamUsageInto(payload, &usage)
				eventType := strings.TrimSpace(gjson.Get(payload, "type").String())
				if eventType == "message_stop" {
					sawMessageStop = true
				}
				if responseID == "" {
					responseID = strings.TrimSpace(gjson.Get(payload, "message.id").String())
					if responseID == "" {
						responseID = strings.TrimSpace(gjson.Get(payload, "id").String())
					}
				}
				if firstTokenMs == nil && eventType == "content_block_delta" {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
			}
		}
		if !clientDisconnected {
			if _, err := c.Writer.WriteString(line + "\n"); err != nil {
				clientDisconnected = true
				startDisconnectedDrain()
				s.logClientDisconnectDrainDecision(ctx, "opencode messages raw", requestID, "write_line")
			} else if line == "" {
				c.Writer.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		logger.L().Warn("opencode messages raw: stream read error",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
	}
	if clientDisconnected {
		streamErr := s.clientDisconnectIncompleteUsageError(ctx)
		if streamErr == nil && !sawMessageStop {
			streamErr = errors.New("stream usage incomplete after disconnect: missing message_stop")
		}
		return resultWithUsage(), streamErr
	}
	if !sawMessageStop {
		return resultWithUsage(), errors.New("upstream anthropic messages stream ended without message_stop")
	}
	markObservedUpstreamResponseModelBillingEligible(c)
	return resultWithUsage(), nil
}

func (s *OpenAIGatewayService) bufferRawAnthropicMessages(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	readCtx, cancelRead := s.detachedNonStreamingReadContext(ctx)
	defer cancelRead()
	respBody, err := ReadUpstreamResponseBodyWithContext(readCtx, resp.Body, s.cfg, c, anthropicTooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeAnthropicError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveAnthropic(respBody)
	markObservedUpstreamResponseModelBillingEligible(c)

	var usage OpenAIUsage
	if u := openAIUsageFromAnthropicMessagesPayload(string(respBody)); u != nil {
		usage = *u
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:            requestID,
		responseID:           strings.TrimSpace(gjson.GetBytes(respBody, "id").String()),
		usage:                &usage,
		responseHeaders:      resp.Header,
		billingUsageComplete: usage.InputTokens > 0 || usage.OutputTokens > 0,
	})
	result.Model = originalModel
	result.BillingModel = billingModel
	result.UpstreamModel = upstreamModel
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.Stream = false
	c.Data(http.StatusOK, contentType, respBody)
	return result, nil
}

// buildOpenAIMessagesURL 复用 chat/completions 的版本段识别启发式，把
// `.../zen/go/v1` 正确拼成 `.../zen/go/v1/messages`，避免 /v1/v1 重复。
func buildOpenAIMessagesURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/messages") {
		return normalized
	}
	lastSlash := strings.LastIndex(normalized, "/")
	lastSegment := normalized
	if lastSlash >= 0 {
		lastSegment = normalized[lastSlash+1:]
	}
	lowerSegment := strings.ToLower(lastSegment)
	if len(lowerSegment) >= 2 && lowerSegment[0] == 'v' && lowerSegment[1] >= '0' && lowerSegment[1] <= '9' {
		return normalized + "/messages"
	}
	return normalized + "/v1/messages"
}

// openAIUsageFromAnthropicMessagesPayload 解析非流式 Anthropic Messages 响应的
// 顶层 usage，映射到 OpenAIUsage 计费结构。
func openAIUsageFromAnthropicMessagesPayload(payload string) *OpenAIUsage {
	if strings.TrimSpace(payload) == "" {
		return nil
	}
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	return &OpenAIUsage{
		InputTokens:              int(usageResult.Get("input_tokens").Int()),
		OutputTokens:             int(usageResult.Get("output_tokens").Int()),
		CacheCreationInputTokens: int(usageResult.Get("cache_creation_input_tokens").Int()),
		CacheReadInputTokens:     int(usageResult.Get("cache_read_input_tokens").Int()),
	}
}

// mergeAnthropicStreamUsageInto 从 Anthropic SSE 的 message_start / message_delta
// 事件里累计输入与输出 token。
func mergeAnthropicStreamUsageInto(payload string, usage *OpenAIUsage) {
	if usage == nil || strings.TrimSpace(payload) == "" {
		return
	}
	if msgUsage := gjson.Get(payload, "message.usage"); msgUsage.Exists() && msgUsage.IsObject() {
		usage.InputTokens = int(msgUsage.Get("input_tokens").Int())
		usage.CacheCreationInputTokens = int(msgUsage.Get("cache_creation_input_tokens").Int())
		usage.CacheReadInputTokens = int(msgUsage.Get("cache_read_input_tokens").Int())
	}
	if deltaUsage := gjson.Get(payload, "usage"); deltaUsage.Exists() && deltaUsage.IsObject() {
		// opencode 在 message_delta 的顶层 usage 里给完整用量（input/output/cache）。
		if in := deltaUsage.Get("input_tokens"); in.Exists() {
			usage.InputTokens = int(in.Int())
		}
		if out := deltaUsage.Get("output_tokens"); out.Exists() {
			usage.OutputTokens = int(out.Int())
		}
		if cacheRead := deltaUsage.Get("cache_read_input_tokens"); cacheRead.Exists() {
			usage.CacheReadInputTokens = int(cacheRead.Int())
		}
		if cacheCreation := deltaUsage.Get("cache_creation_input_tokens"); cacheCreation.Exists() {
			usage.CacheCreationInputTokens = int(cacheCreation.Int())
		}
	}
}
