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
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

var openaiChatRawAllowedHeaders = map[string]bool{
	"accept-language": true,
	"user-agent":      true,
}

func (s *OpenAIGatewayService) forwardAsRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	startTime := time.Now()

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()

	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)

	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}

	var err error
	upstreamBody, err = s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, err
	}
	serviceTier := extractOpenAIServiceTierFromBody(upstreamBody)
	forwardResult := &OpenAIForwardResult{
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          clientStream,
	}
	ctx = withOpenAIForwardResultBillingState(ctx, c, forwardResult, startTime, openAIResponseImageBillingConfig{})
	if clientStream {
		upstreamBody, err = ensureOpenAIChatStreamUsage(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("enable stream usage: %w", err)
		}
	}

	token := account.GetOpenAIApiKey()
	if token == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	targetURL := buildOpenAIChatCompletionsURL(validatedURL)
	SetActualOpenAIUpstreamEndpoint(c, grokChatRawEndpoint)

	upstreamCtx, releaseUpstreamCtx := s.detachOpenAIUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
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
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			Kind:               "request_error",
			Message:            safeErr,
		})
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				resp,
				c,
				account,
				false,
				writeChatCompletionsError,
			)
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
		return s.handleChatCompletionsErrorResponse(resp, c, account, originalModel)
	}

	if clientStream {
		return s.streamRawChatCompletions(ctx, c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferRawChatCompletions(ctx, c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) streamRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
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
	var billingUsageObservation openAIChatCompletionsBillingUsageObservation
	var firstTokenMs *int
	var responseID string
	var responseServiceTier string
	sawDone := false
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
		usage.ResponseServiceTier = responseServiceTier
		result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
			requestID:            requestID,
			responseID:           responseID,
			usage:                &usage,
			firstTokenMs:         firstTokenMs,
			responseHeaders:      resp.Header,
			billingUsageComplete: billingUsageObservation.complete(),
		})
		result.Model = originalModel
		result.BillingModel = billingModel
		result.UpstreamModel = upstreamModel
		result.UpstreamResponseModel = observedUpstreamResponseModel(c)
		result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
		result.ReasoningEffort = reasoningEffort
		if result.ServiceTier == nil {
			result.ServiceTier = serviceTier
		}
		result.ResponseServiceTier = responseServiceTier
		result.Stream = true
		return result
	}
	for scanner.Scan() {
		line := scanner.Text()
		if payload, ok := extractOpenAISSEDataLine(line); ok {
			trimmedPayload := strings.TrimSpace(payload)
			if trimmedPayload == "[DONE]" {
				sawDone = true
			} else {
				observer.ObserveOpenAI([]byte(payload), strings.TrimSpace(gjson.Get(payload, "type").String()))
				billingUsageObservation.observePayload([]byte(payload))
				usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(payload)
				if u := extractOpenAIChatStreamUsage(payload); u != nil {
					usage = *u
				}
				if responseID == "" {
					responseID = strings.TrimSpace(gjson.Get(payload, "id").String())
				}
				if tier := strings.TrimSpace(gjson.Get(payload, "service_tier").String()); tier != "" {
					responseServiceTier = tier
				}
				if firstTokenMs == nil && !usageOnlyChunk {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
			}
		}
		if !clientDisconnected {
			if _, err := c.Writer.WriteString(line + "\n"); err != nil {
				clientDisconnected = true
				startDisconnectedDrain()
				s.logClientDisconnectDrainDecision(ctx, "openai chat_completions raw", requestID, "write_line")
			} else if line == "" {
				c.Writer.Flush()
			}
		}
		if sawDone {
			if !clientDisconnected {
				if _, err := c.Writer.WriteString("\n"); err != nil {
					clientDisconnected = true
					startDisconnectedDrain()
					s.logClientDisconnectDrainDecision(ctx, "openai chat_completions raw", requestID, "write_terminal_delimiter")
				} else {
					c.Writer.Flush()
				}
			}
			break
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		logger.L().Warn("openai chat_completions raw: stream read error",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
	}
	if clientDisconnected {
		streamErr := s.clientDisconnectIncompleteUsageError(ctx)
		if streamErr == nil && !billingUsageObservation.complete() {
			streamErr = errors.New("stream usage incomplete after disconnect: missing terminal usage")
		}
		return resultWithUsage(), streamErr
	}

	result := resultWithUsage()
	if !sawDone {
		return result, errors.New("upstream chat completions stream ended without [DONE]")
	}
	markObservedUpstreamResponseModelBillingEligible(c)
	result = resultWithUsage()
	return result, nil
}

func ensureOpenAIChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func isOpenAIChatUsageOnlyStreamChunk(payload string) bool {
	if strings.TrimSpace(payload) == "" || !gjson.Get(payload, "usage").Exists() {
		return false
	}
	choices := gjson.Get(payload, "choices")
	return choices.Exists() && choices.IsArray() && len(choices.Array()) == 0
}

func extractOpenAIChatStreamUsage(payload string) *OpenAIUsage {
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	return openAIUsageFromChatCompletionsUsage(payload)
}

func (s *OpenAIGatewayService) bufferRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	readCtx, cancelRead := s.detachedNonStreamingReadContext(ctx)
	defer cancelRead()
	respBody, err := ReadUpstreamResponseBodyWithContext(readCtx, resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(respBody, strings.TrimSpace(gjson.GetBytes(respBody, "type").String()))
	markObservedUpstreamResponseModelBillingEligible(c)

	var usage OpenAIUsage
	if u := openAIUsageFromChatCompletionsUsage(string(respBody)); u != nil {
		usage = *u
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	usage.ResponseServiceTier = strings.TrimSpace(gjson.GetBytes(respBody, "service_tier").String())
	result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:            requestID,
		responseID:           strings.TrimSpace(gjson.GetBytes(respBody, "id").String()),
		usage:                &usage,
		responseHeaders:      resp.Header,
		billingUsageComplete: openAIChatCompletionsBillingUsageComplete(respBody),
	})
	result.Model = originalModel
	result.BillingModel = billingModel
	result.UpstreamModel = upstreamModel
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.ReasoningEffort = reasoningEffort
	if result.ServiceTier == nil {
		result.ServiceTier = serviceTier
	}
	result.ResponseServiceTier = usage.ResponseServiceTier
	result.Stream = false
	c.Data(http.StatusOK, contentType, respBody)
	return result, nil
}

func openAIUsageFromChatCompletionsUsage(payload string) *OpenAIUsage {
	if strings.TrimSpace(payload) == "" {
		return nil
	}
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	usage, ok := openAIUsageFromGJSON(usageResult)
	if !ok {
		return nil
	}
	return &usage
}

func buildOpenAIChatCompletionsURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/chat/completions") {
		return normalized
	}
	lastSlash := strings.LastIndex(normalized, "/")
	lastSegment := normalized
	if lastSlash >= 0 {
		lastSegment = normalized[lastSlash+1:]
	}
	lowerSegment := strings.ToLower(lastSegment)
	if len(lowerSegment) >= 2 && lowerSegment[0] == 'v' && lowerSegment[1] >= '0' && lowerSegment[1] <= '9' {
		return normalized + "/chat/completions"
	}
	return normalized + "/v1/chat/completions"
}
