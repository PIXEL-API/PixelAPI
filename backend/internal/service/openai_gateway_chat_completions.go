package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// cursorResponsesUnsupportedFields are top-level Responses API parameters that
// Codex upstreams reject with "Unsupported parameter: ...". They must be
// stripped when forwarding a raw client body through the Responses-shape
// short-circuit in ForwardAsChatCompletions (see isResponsesShape branch).
// The normal Chat Completions → Responses conversion path is unaffected
// because ChatCompletionsRequest has no fields for these parameters — unknown
// fields are dropped naturally by json.Unmarshal. Kept semantically in sync
// with the list in openai_gateway_service.go:2034 used by the /v1/responses
// passthrough path.
var cursorResponsesUnsupportedFields = []string{
	"prompt_cache_retention",
	"safety_identifier",
	"metadata",
	"stream_options",
}

// ForwardAsChatCompletions accepts a Chat Completions request body, converts it
// to OpenAI Responses API format, forwards to the OpenAI upstream, and converts
// the response back to Chat Completions format. All account types (OAuth and API
// Key) go through the Responses API conversion path since the upstream only
// exposes the /v1/responses endpoint.
func (s *OpenAIGatewayService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
	}

	startTime := time.Now()

	// 1. Parse Chat Completions request
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	originalModel := chatReq.Model
	clientStream := chatReq.Stream
	includeUsage := chatReq.StreamOptions != nil && chatReq.StreamOptions.IncludeUsage

	// 2. Resolve model mapping early so compat prompt_cache_key injection can
	// derive a stable seed from the final upstream model family.
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)

	promptCacheKey = strings.TrimSpace(promptCacheKey)
	compatPromptCacheInjected := false
	if promptCacheKey == "" && account.Type == AccountTypeOAuth && shouldAutoInjectPromptCacheKeyForCompat(upstreamModel) {
		promptCacheKey = deriveCompatPromptCacheKey(&chatReq, upstreamModel)
		compatPromptCacheInjected = promptCacheKey != ""
	}

	// 3. Build the upstream (Responses API) body.
	//
	// Cursor compatibility: some clients (notably Cursor cloud) send Responses
	// API shaped bodies — `input: [...]` with no `messages` field — to the
	// /v1/chat/completions URL. Running those through ChatCompletionsToResponses
	// would silently drop Cursor's `input` array (the struct has no Input field)
	// and produce `input: null`, which Codex upstreams reject with
	// "Invalid type for 'input': expected a string, but got an object".
	//
	// Detect that shape and forward the raw body as-is, only rewriting `model`
	// to the resolved upstream model. The downstream codex OAuth transform will
	// still normalize store/stream/instructions/etc.
	isResponsesShape := !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists()

	var (
		responsesReq  *apicompat.ResponsesRequest
		responsesBody []byte
		err           error
	)
	if isResponsesShape {
		responsesBody, err = sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("rewrite model in responses-shape body: %w", err)
		}
		// Strip Responses API parameters that no Codex upstream accepts.
		// Because this branch forwards the raw body (the normal path rebuilds
		// it from ChatCompletionsRequest and drops unknown fields naturally),
		// we must filter these fields explicitly here — otherwise the upstream
		// rejects the request with "Unsupported parameter: ...".
		for _, field := range cursorResponsesUnsupportedFields {
			if stripped, derr := sjson.DeleteBytes(responsesBody, field); derr == nil {
				responsesBody = stripped
			}
		}
		responsesBody, normalizedServiceTier, err := normalizeResponsesBodyServiceTier(responsesBody)
		if err != nil {
			return nil, fmt.Errorf("normalize service_tier in responses-shape body: %w", err)
		}
		// Minimal stub populated from the raw body so downstream billing
		// propagation (ServiceTier, ReasoningEffort) keeps working.
		responsesReq = &apicompat.ResponsesRequest{
			Model:       upstreamModel,
			ServiceTier: normalizedServiceTier,
		}
		if effort := gjson.GetBytes(responsesBody, "reasoning.effort").String(); effort != "" {
			responsesReq.Reasoning = &apicompat.ResponsesReasoning{Effort: effort}
		}
	} else {
		// Normal path: convert Chat Completions → Responses.
		// ChatCompletionsToResponses always sets Stream=true (upstream always streams).
		responsesReq, err = apicompat.ChatCompletionsToResponses(&chatReq)
		if err != nil {
			return nil, fmt.Errorf("convert chat completions to responses: %w", err)
		}
		responsesReq.Model = upstreamModel
		normalizeResponsesRequestServiceTier(responsesReq)
		responsesBody, err = json.Marshal(responsesReq)
		if err != nil {
			return nil, fmt.Errorf("marshal responses request: %w", err)
		}
	}

	logFields := []zap.Field{
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
		zap.Bool("responses_shape", isResponsesShape),
	}
	if compatPromptCacheInjected {
		logFields = append(logFields,
			zap.Bool("compat_prompt_cache_key_injected", true),
			zap.String("compat_prompt_cache_key_sha256", hashSensitiveValueForLog(promptCacheKey)),
		)
	}
	logger.L().Debug("openai chat_completions: model mapping applied", logFields...)

	if account.Platform == PlatformGrok {
		return s.forwardGrokChatCompletions(ctx, c, account, responsesBody, originalModel, billingModel, upstreamModel, clientStream, includeUsage, startTime)
	}

	if account.Type == AccountTypeOAuth {
		var reqBody map[string]any
		if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
			return nil, fmt.Errorf("unmarshal for codex transform: %w", err)
		}
		codexResult := applyCodexOAuthTransform(reqBody, false, false)
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		} else if promptCacheKey != "" {
			reqBody["prompt_cache_key"] = promptCacheKey
		}
		responsesBody, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("remarshal after codex transform: %w", err)
		}
	}

	// 4b. Apply OpenAI fast policy (may filter service_tier or block the request).
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, responsesBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	responsesBody = updatedBody
	forwardedServiceTier := extractOpenAIServiceTierFromBody(responsesBody)
	var reasoningEffort *string
	if responsesReq.Reasoning != nil && strings.TrimSpace(responsesReq.Reasoning.Effort) != "" {
		effort := responsesReq.Reasoning.Effort
		reasoningEffort = &effort
	}
	forwardResult := &OpenAIForwardResult{
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ServiceTier:     forwardedServiceTier,
		ReasoningEffort: reasoningEffort,
		Stream:          clientStream,
	}
	ctx = withOpenAIForwardResultBillingState(ctx, c, forwardResult, startTime, openAIResponseImageBillingConfig{})

	// 5. Get access token
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// 6. Build upstream request
	upstreamCtx, releaseUpstreamCtx := s.detachOpenAIUpstreamContext(ctx)
	upstreamReq, err := s.buildUpstreamRequest(upstreamCtx, c, account, responsesBody, token, true, promptCacheKey, false)
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))

	if promptCacheKey != "" {
		upstreamReq.Header.Set("session_id", generateSessionUUID(promptCacheKey))
	}

	// 7. Send request
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

	// 8. Handle non-success response with failover
	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(resp, c, account, false, writeChatCompletionsError)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryWasTried(ctx) &&
			isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
			if err := s.recoverAgentIdentityTask(ctx, account, expectedAgentIdentityTaskID); err != nil {
				return nil, fmt.Errorf("recover Agent Identity task: %w", err)
			}
			retryCtx := withAgentIdentitySensitiveValues(markAgentIdentityTaskRecoveryTried(ctx), expectedAgentIdentityTaskID)
			return s.ForwardAsChatCompletions(retryCtx, c, account, body, promptCacheKey, defaultMappedModel)
		}
		respBody = s.redactAgentIdentitySensitiveBody(ctx, account, respBody)
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(respBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			s.handleOpenAIAccountUpstreamErrorForModel(ctx, account, billingModel, resp.StatusCode, resp.Header, respBody)
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

	// 9. Handle normal response
	var result *OpenAIForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleChatStreamingResponse(ctx, resp, c, account, originalModel, billingModel, upstreamModel, includeUsage, startTime)
	} else {
		result, handleErr = s.handleChatBufferedStreamingResponse(ctx, resp, c, account, originalModel, billingModel, upstreamModel, startTime)
	}

	// Propagate ServiceTier and ReasoningEffort to result for billing
	if handleErr == nil && result != nil {
		if upstreamServiceTier := extractOpenAIServiceTierFromResponses(result.ResponseServiceTier); upstreamServiceTier != nil {
			result.ServiceTier = upstreamServiceTier
		} else if forwardedServiceTier != nil {
			result.ServiceTier = forwardedServiceTier
		}
		result.ReasoningEffort = reasoningEffort
	}

	// Extract and save Codex usage snapshot from response headers (for OAuth accounts)
	if handleErr == nil && account.Type == AccountTypeOAuth {
		if snapshot := ParseCodexRateLimitHeaders(resp.Header); snapshot != nil {
			s.updateCodexUsageSnapshot(ctx, account.ID, snapshot)
		}
	}

	return result, handleErr
}

func normalizeResponsesRequestServiceTier(req *apicompat.ResponsesRequest) {
	if req == nil {
		return
	}
	req.ServiceTier = normalizedOpenAIServiceTierValue(req.ServiceTier)
}

func normalizeResponsesBodyServiceTier(body []byte) ([]byte, string, error) {
	if len(body) == 0 {
		return body, "", nil
	}
	rawServiceTier := gjson.GetBytes(body, "service_tier").String()
	if rawServiceTier == "" {
		return body, "", nil
	}
	normalizedServiceTier := normalizedOpenAIServiceTierValue(rawServiceTier)
	if normalizedServiceTier == "" {
		trimmed, err := sjson.DeleteBytes(body, "service_tier")
		return trimmed, "", err
	}
	if normalizedServiceTier == rawServiceTier {
		return body, normalizedServiceTier, nil
	}
	trimmed, err := sjson.SetBytes(body, "service_tier", normalizedServiceTier)
	return trimmed, normalizedServiceTier, err
}

func normalizedOpenAIServiceTierValue(raw string) string {
	normalized := normalizeOpenAIServiceTier(raw)
	if normalized == nil {
		return ""
	}
	return *normalized
}

func extractOpenAIServiceTierFromResponses(raw string) *string {
	return normalizeOpenAIServiceTier(raw)
}

// handleChatCompletionsErrorResponse reads an upstream error and returns it in
// OpenAI Chat Completions error format.
func (s *OpenAIGatewayService) handleChatCompletionsErrorResponse(
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	return s.handleCompatErrorResponse(resp, c, account, requestedModel, writeChatCompletionsError)
}

// handleChatBufferedStreamingResponse reads all Responses SSE events from the
// upstream, finds the terminal event, converts to a Chat Completions JSON
// response, and writes it to the client.
func (s *OpenAIGatewayService) handleChatBufferedStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var finalResponse *apicompat.ResponsesResponse
	var usage OpenAIUsage
	var billingUsageObservation openAIResponsesBillingUsageObservation
	acc := apicompat.NewBufferedResponseAccumulator()

	for scanner.Scan() {
		line := scanner.Text()
		payload, ok := extractOpenAISSEDataLine(line)
		if !ok || strings.TrimSpace(payload) == "[DONE]" {
			continue
		}
		observer.ObserveOpenAI([]byte(payload), strings.TrimSpace(gjson.Get(payload, "type").String()))
		billingUsageObservation.observePayload([]byte(payload))

		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			logger.L().Warn("openai chat_completions buffered: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			continue
		}

		// Accumulate delta content for fallback when terminal output is empty.
		acc.ProcessEvent(&event)

		if (event.Type == "response.completed" || event.Type == "response.done" ||
			event.Type == "response.incomplete" || event.Type == "response.failed") &&
			event.Response != nil {
			finalResponse = event.Response
			if event.Response.Usage != nil {
				usage = copyOpenAIUsageFromResponsesUsage(event.Response.Usage)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("openai chat_completions buffered: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
		}
	}

	if finalResponse == nil {
		writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Upstream stream ended without a terminal response event")
		return nil, fmt.Errorf("upstream stream ended without terminal event")
	}
	observer.Observe(finalResponse.Model, true)
	if IsOpenAICyberPolicyEnforcedForCurrentAttempt(c) &&
		strings.EqualFold(strings.TrimSpace(finalResponse.Status), "failed") && finalResponse.Error != nil &&
		strings.EqualFold(strings.TrimSpace(finalResponse.Error.Code), "cyber_policy") {
		clientMsg := openAICyberPolicyClientMessage(finalResponse.Error.Message)
		MarkOpsCyberPolicy(c, CyberPolicyMark{
			Message:        clientMsg,
			UpstreamStatus: http.StatusOK,
			UpstreamInTok:  usage.InputTokens,
			UpstreamOutTok: usage.OutputTokens,
		})
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", clientMsg)
		return resultForOpenAICompatFailure(c, requestID, usage, originalModel, billingModel, upstreamModel, finalResponse.ServiceTier, false, startTime),
			fmt.Errorf("openai cyber_policy: %s", clientMsg)
	}
	if strings.EqualFold(strings.TrimSpace(finalResponse.Status), "failed") {
		payload, _ := json.Marshal(gin.H{"type": "response.failed", "response": finalResponse})
		message := ""
		if finalResponse.Error != nil {
			message = strings.TrimSpace(finalResponse.Error.Message)
		}
		if openAIStreamFailedEventShouldFailover(payload, message) {
			return nil, s.newOpenAIStreamFailoverError(c, account, false, requestID, payload, message)
		}
		message = s.recordOpenAIStreamUpstreamError(c, account, false, requestID, "http_error", payload, message)
		if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(c, account.Platform, payload, message); matched {
			if errMsg == "" {
				errMsg = message
			}
			MarkResponseCommitted(c)
			writeChatCompletionsError(c, status, errType, errMsg)
			return nil, fmt.Errorf("upstream response failed (passthrough): %s", errMsg)
		}
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", message)
		return nil, fmt.Errorf("upstream response failed: %s", message)
	}

	// When the terminal event has an empty output array, reconstruct from
	// accumulated delta events so the client receives the full content.
	acc.SupplementResponseOutput(finalResponse)

	chatResp := apicompat.ResponsesToChatCompletions(finalResponse, originalModel)
	usage.ResponseServiceTier = finalResponse.ServiceTier
	markObservedUpstreamResponseModelBillingEligible(c)
	result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:            requestID,
		responseID:           finalResponse.ID,
		usage:                &usage,
		responseHeaders:      resp.Header,
		billingUsageComplete: billingUsageObservation.complete(),
	})
	result.Model = originalModel
	result.BillingModel = billingModel
	result.UpstreamModel = upstreamModel
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.ResponseServiceTier = finalResponse.ServiceTier
	result.Stream = false

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.JSON(http.StatusOK, chatResp)
	return result, nil
}

// handleChatStreamingResponse reads Responses SSE events from upstream,
// converts each to Chat Completions SSE chunks, and writes them to the client.
func (s *OpenAIGatewayService) handleChatStreamingResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	originalModel string,
	billingModel string,
	upstreamModel string,
	includeUsage bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	state := apicompat.NewResponsesEventToChatState()
	state.Model = originalModel
	state.IncludeUsage = includeUsage

	var usage OpenAIUsage
	var billingUsageObservation openAIResponsesBillingUsageObservation
	var firstTokenMs *int
	var responseServiceTier string
	var responseID string
	firstChunk := true
	sawSuccessTerminal := false
	var terminalErr error
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

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

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
		result.ResponseServiceTier = responseServiceTier
		result.Stream = true
		return result
	}

	processDataLine := func(payload string) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		payloadBytes := []byte(payload)
		observer.ObserveOpenAI(payloadBytes, strings.TrimSpace(gjson.GetBytes(payloadBytes, "type").String()))
		billingUsageObservation.observePayload(payloadBytes)
		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal(payloadBytes, &event); err != nil {
			logger.L().Warn("openai chat_completions stream: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			return false
		}

		// Extract usage from completion events
		if (event.Type == "response.completed" || event.Type == "response.done" || event.Type == "response.incomplete" || event.Type == "response.failed") &&
			event.Response != nil && event.Response.Usage != nil {
			if event.Response.ServiceTier != "" {
				responseServiceTier = event.Response.ServiceTier
			}
			usage = copyOpenAIUsageFromResponsesUsage(event.Response.Usage)
		}
		if event.Response != nil && strings.TrimSpace(event.Response.ID) != "" {
			responseID = event.Response.ID
		}
		if event.Type == "response.failed" {
			if hit, _, msg := detectOpenAICyberPolicyForCurrentAttempt(c, []byte(payload)); hit {
				clientMsg := openAICyberPolicyClientMessage(msg)
				MarkOpsCyberPolicy(c, CyberPolicyMark{
					Message:        clientMsg,
					Body:           truncateString(payload, 4096),
					UpstreamStatus: http.StatusOK,
					UpstreamInTok:  usage.InputTokens,
					UpstreamOutTok: usage.OutputTokens,
				})
				terminalErr = fmt.Errorf("openai cyber_policy: %s", clientMsg)
				if !clientDisconnected {
					if !c.Writer.Written() {
						writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", clientMsg)
						clientDisconnected = true
					} else if writeChatCompletionsStreamError(c, "invalid_request_error", clientMsg) {
						clientDisconnected = true
						startDisconnectedDrain()
					}
				}
				return true
			}
			message := extractOpenAISSEErrorMessage(payloadBytes)
			if !c.Writer.Written() && openAIStreamFailedEventShouldFailover(payloadBytes, message) {
				terminalErr = s.newOpenAIStreamFailoverError(c, account, false, requestID, payloadBytes, message)
				return true
			}
			message = s.recordOpenAIStreamUpstreamError(c, account, false, requestID, "http_error", payloadBytes, message)
			errStatus, errType, errMsg := http.StatusBadGateway, "upstream_error", message
			if status, mappedType, mappedMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(c, account.Platform, payloadBytes, message); matched {
				if mappedMsg == "" {
					mappedMsg = errMsg
				}
				errStatus, errType, errMsg = status, mappedType, mappedMsg
				MarkResponseCommitted(c)
			}
			if !clientDisconnected {
				if c.Writer.Written() {
					_ = writeChatCompletionsStreamError(c, errType, errMsg)
				} else {
					writeChatCompletionsError(c, errStatus, errType, errMsg)
				}
			}
			terminalErr = fmt.Errorf("upstream response failed: %s", errMsg)
			return true
		}
		// response.incomplete remains an accepted compatibility terminal for
		// downstream conversion, while the observer's sticky rejection keeps its
		// model audit-only and ineligible for billing.
		successTerminal := event.Type == "response.completed" || event.Type == "response.done" || event.Type == "response.incomplete"
		if event.Type == "response.done" {
			event.Type = "response.completed"
		}
		chunks := apicompat.ResponsesEventToChatChunks(&event, state)
		serializedChunks := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			sse, err := apicompat.ChatChunkToSSE(chunk)
			if err != nil {
				observer.RejectBilling()
				terminalErr = fmt.Errorf("marshal chat completions stream chunk: %w", err)
				return true
			}
			serializedChunks = append(serializedChunks, sse)
		}
		if successTerminal {
			sawSuccessTerminal = true
		}

		if !clientDisconnected {
			for _, sse := range serializedChunks {
				if _, err := fmt.Fprint(c.Writer, sse); err != nil {
					clientDisconnected = true
					startDisconnectedDrain()
					s.logClientDisconnectDrainDecision(ctx, "openai chat_completions stream", requestID, "write_chunk")
					break
				}
			}
		}
		if !clientDisconnected && len(serializedChunks) > 0 {
			c.Writer.Flush()
		}
		return false
	}

	finalizeStream := func() (*OpenAIForwardResult, error) {
		if clientDisconnected {
			if err := s.clientDisconnectIncompleteUsageError(ctx); err != nil {
				return resultWithUsage(), err
			}
			if !billingUsageObservation.complete() {
				return resultWithUsage(), errors.New("stream usage incomplete after disconnect: missing terminal usage")
			}
			return resultWithUsage(), nil
		}
		result := resultWithUsage()
		if !sawSuccessTerminal {
			return result, errors.New("upstream responses stream ended without a successful terminal event")
		}
		markObservedUpstreamResponseModelBillingEligible(c)
		result = resultWithUsage()
		// Send [DONE] sentinel
		if _, err := fmt.Fprint(c.Writer, "data: [DONE]\n\n"); err != nil {
			return result, fmt.Errorf("write chat completions terminal sentinel: %w", err)
		}
		c.Writer.Flush()
		return result, nil
	}

	handleScanErr := func(err error) {
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("openai chat_completions stream: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
		}
	}

	// Determine keepalive interval
	keepaliveInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
	}

	// No keepalive: fast synchronous path
	if keepaliveInterval <= 0 {
		for scanner.Scan() {
			line := scanner.Text()
			payload, ok := extractOpenAISSEDataLine(line)
			if !ok || strings.TrimSpace(payload) == "[DONE]" {
				continue
			}
			if processDataLine(payload) {
				return resultWithUsage(), terminalErr
			}
		}
		handleScanErr(scanner.Err())
		return finalizeStream()
	}

	// With keepalive: goroutine + channel + select
	type scanEvent struct {
		line string
		err  error
	}
	events := make(chan scanEvent, 16)
	done := make(chan struct{})
	sendEvent := func(ev scanEvent) bool {
		select {
		case events <- ev:
			return true
		case <-done:
			return false
		}
	}
	go func() {
		defer close(events)
		for scanner.Scan() {
			if !sendEvent(scanEvent{line: scanner.Text()}) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			_ = sendEvent(scanEvent{err: err})
		}
	}()
	defer close(done)

	keepaliveTicker := time.NewTicker(keepaliveInterval)
	defer keepaliveTicker.Stop()
	lastDataAt := time.Now()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return finalizeStream()
			}
			if ev.err != nil {
				handleScanErr(ev.err)
				return finalizeStream()
			}
			lastDataAt = time.Now()
			line := ev.line
			payload, ok := extractOpenAISSEDataLine(line)
			if !ok || strings.TrimSpace(payload) == "[DONE]" {
				continue
			}
			if processDataLine(payload) {
				return resultWithUsage(), terminalErr
			}

		case <-keepaliveTicker.C:
			if clientDisconnected {
				continue
			}
			if time.Since(lastDataAt) < keepaliveInterval {
				continue
			}
			// Send SSE comment as keepalive
			if _, err := fmt.Fprint(c.Writer, ":\n\n"); err != nil {
				clientDisconnected = true
				startDisconnectedDrain()
				s.logClientDisconnectDrainDecision(ctx, "openai chat_completions stream", requestID, "keepalive")
				continue
			}
			c.Writer.Flush()
		}
	}
}

// writeChatCompletionsError writes an error response in OpenAI Chat Completions format.
func writeChatCompletionsError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func writeChatCompletionsStreamError(c *gin.Context, errType, message string) bool {
	payload, err := json.Marshal(gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
	if err != nil {
		logger.L().Warn("openai chat_completions stream: failed to marshal error event", zap.Error(err))
		return false
	}
	if _, err := fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", payload); err != nil {
		return true
	}
	fmt.Fprint(c.Writer, "data: [DONE]\n\n") //nolint:errcheck
	c.Writer.Flush()
	return false
}

func resultForOpenAICompatFailure(
	c *gin.Context,
	requestID string,
	usage OpenAIUsage,
	model string,
	billingModel string,
	upstreamModel string,
	responseServiceTier string,
	stream bool,
	startTime time.Time,
) *OpenAIForwardResult {
	return &OpenAIForwardResult{
		RequestID:                     requestID,
		Usage:                         usage,
		Model:                         model,
		BillingModel:                  billingModel,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		ResponseServiceTier:           responseServiceTier,
		Stream:                        stream,
		Duration:                      time.Since(startTime),
	}
}
