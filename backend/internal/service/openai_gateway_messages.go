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
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ForwardAsAnthropic accepts an Anthropic Messages request body, converts it
// to OpenAI Responses API format, forwards to the OpenAI upstream, and converts
// the response back to Anthropic Messages format. This enables Claude Code
// clients to access OpenAI models through the standard /v1/messages endpoint.
func (s *OpenAIGatewayService) ForwardAsAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	resetOpenAIRequestIdentityState(c)
	beginUpstreamResponseModelObservation(c)
	var opencodeResolved *OpencodeGoResolvedModel
	if account.IsOpencode() {
		resolved, err := resolveOpencodeGoForwardModel(account, gjson.GetBytes(body, "model").String(), defaultMappedModel)
		if err != nil {
			return rejectOpencodeGoRoutingError(c, OpencodeGoProtocolMessages, err)
		}
		switch resolved.Spec.Protocol {
		case OpencodeGoProtocolMessages:
			return s.forwardAsRawAnthropicMessages(ctx, c, account, body, defaultMappedModel)
		case OpencodeGoProtocolChat:
			if err := validateOpencodeMessagesToChatBridge(body, resolved); err != nil {
				return rejectOpencodeGoRoutingError(c, OpencodeGoProtocolMessages, err)
			}
			SetActualOpenAIUpstreamEndpoint(c, openAIChatRawEndpoint)
			return s.forwardAnthropicViaRawChatCompletions(ctx, c, account, body, defaultMappedModel, resolved)
		case OpencodeGoProtocolResponses:
			if err := validateOpencodeMessagesToResponsesBridge(body, resolved); err != nil {
				return rejectOpencodeGoRoutingError(c, OpencodeGoProtocolMessages, err)
			}
			// Continue through the established Anthropic → Responses → Anthropic
			// bridge. buildUpstreamRequest selects the OpenCode Go Responses URL.
			SetActualOpenAIUpstreamEndpoint(c, opencodeResponsesRawEndpoint)
			resolvedCopy := resolved
			opencodeResolved = &resolvedCopy
		default:
			return nil, fmt.Errorf("unsupported OpenCode Go protocol %q for model %q", resolved.Spec.Protocol, resolved.UpstreamModel)
		}
	}
	// OpenCode Go 的协议由映射后的最终模型目录决定。普通 OpenAI API Key 的
	// 账号级 Responses 探测/强制模式不得覆盖已经完成的 OpenCode 路由决策。
	if account.Type == AccountTypeAPIKey && !account.IsOpencode() && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return s.forwardAnthropicViaRawChatCompletions(ctx, c, account, body, defaultMappedModel)
	}

	startTime := time.Now()

	// 1. Parse Anthropic request
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := anthropicReq.Model
	applyOpenAICompatModelNormalization(&anthropicReq)
	normalizedModel := anthropicReq.Model
	clientStream := anthropicReq.Stream // client's original stream preference

	billingModel := resolveOpenAIForwardModel(account, normalizedModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	if opencodeResolved != nil {
		billingModel = opencodeResolved.BillingModel
		upstreamModel = opencodeResolved.UpstreamModel
	}
	if account.Platform == PlatformGrok {
		ctx = withGrokRequestedModel(ctx, upstreamModel)
	}
	compatGuardEnabled := shouldAutoInjectPromptCacheKeyForCompat(upstreamModel)
	compatReplayTrimmed := false
	if compatGuardEnabled && account.Type != AccountTypeOAuth {
		compatReplayTrimmed = applyAnthropicCompatFullReplayGuard(&anthropicReq)
	}

	// 2. Convert Anthropic → Responses
	responsesReq, err := apicompat.AnthropicToResponses(&anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("convert anthropic to responses: %w", err)
	}

	// Upstream always uses streaming (upstream may not support sync mode).
	// The client's original preference determines the response format.
	responsesReq.Stream = true
	isStream := true

	// 2b. Handle BetaFastMode → service_tier: "priority"
	if containsBetaToken(c.GetHeader("anthropic-beta"), claude.BetaFastMode) {
		responsesReq.ServiceTier = "priority"
	}

	// 3. Model mapping
	responsesReq.Model = upstreamModel
	if compatGuardEnabled && account.Type != AccountTypeOAuth {
		appendOpenAICompatClaudeCodeTodoGuard(responsesReq)
	}

	logFields := []zap.Field{
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("normalized_model", normalizedModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", isStream),
	}
	if compatReplayTrimmed {
		logFields = append(logFields,
			zap.Bool("compat_full_replay_trimmed", true),
			zap.Int("compat_messages_after_trim", len(anthropicReq.Messages)),
		)
	}
	logger.L().Debug("openai messages: model mapping applied", logFields...)

	// 4. Marshal Responses request body, then apply OAuth codex transform
	responsesBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	if account.Type == AccountTypeOAuth && account.Platform != PlatformGrok {
		var reqBody map[string]any
		if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
			return nil, fmt.Errorf("unmarshal for codex transform: %w", err)
		}
		codexResult := applyCodexOAuthTransform(reqBody, false, false)
		forcedTemplateText := ""
		if s.cfg != nil {
			forcedTemplateText = s.cfg.Gateway.ForcedCodexInstructionsTemplate
		}
		templateUpstreamModel := upstreamModel
		if codexResult.NormalizedModel != "" {
			templateUpstreamModel = codexResult.NormalizedModel
		}
		existingInstructions, _ := reqBody["instructions"].(string)
		if _, err := applyForcedCodexInstructionsTemplate(reqBody, forcedTemplateText, forcedCodexInstructionsTemplateData{
			ExistingInstructions: strings.TrimSpace(existingInstructions),
			OriginalModel:        originalModel,
			NormalizedModel:      normalizedModel,
			BillingModel:         billingModel,
			UpstreamModel:        templateUpstreamModel,
		}); err != nil {
			return nil, err
		}
		if compatGuardEnabled {
			appendOpenAICompatClaudeCodeTodoGuardToRequestBody(reqBody)
		}
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		} else if promptCacheKey != "" {
			reqBody["prompt_cache_key"] = promptCacheKey
		}
		// OAuth codex transform forces stream=true upstream, so always use
		// the streaming response handler regardless of what the client asked.
		isStream = true
		responsesBody, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("remarshal after codex transform: %w", err)
		}
	}

	// For API key accounts (including OpenAI-compatible upstream gateways),
	// ensure promptCacheKey is also propagated via the request body so that
	// upstreams using the Responses API can derive a stable session identifier
	// from prompt_cache_key. This makes our Anthropic /v1/messages compatibility
	// path behave more like a native Responses client.
	if account.Type == AccountTypeAPIKey {
		if trimmedKey := strings.TrimSpace(promptCacheKey); trimmedKey != "" {
			var reqBody map[string]any
			if err := json.Unmarshal(responsesBody, &reqBody); err != nil {
				return nil, fmt.Errorf("unmarshal for prompt cache key injection: %w", err)
			}
			if existing, ok := reqBody["prompt_cache_key"].(string); !ok || strings.TrimSpace(existing) == "" {
				reqBody["prompt_cache_key"] = trimmedKey
				updated, err := json.Marshal(reqBody)
				if err != nil {
					return nil, fmt.Errorf("remarshal after prompt cache key injection: %w", err)
				}
				responsesBody = updated
			}
		}
	}

	// 4c. Apply OpenAI fast policy (may filter service_tier or block the request).
	// Mirrors the Claude anthropic-beta "fast-mode-2026-02-01" filter, but keyed
	// on the body-level service_tier field (priority/flex).
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, responsesBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			writeAnthropicError(c, http.StatusForbidden, "forbidden_error", blocked.Message)
		}
		return nil, policyErr
	}
	responsesBody = updatedBody
	cleanRelaySessionBody := responsesBody
	if account.IsOpenAIOAuth() {
		fingerprintedBody, _, _, fingerprintErr := s.applyCodexFingerprintToRawBody(ctx, c, account, responsesBody)
		if fingerprintErr != nil {
			return nil, fingerprintErr
		}
		responsesBody = fingerprintedBody
	}
	responsesBody, cleanRelayState, _, cleanRelayErr := s.applyOpenAICleanRelayToRawBody(ctx, c, account, responsesBody, cleanRelaySessionBody)
	if cleanRelayErr != nil {
		return nil, cleanRelayErr
	}
	if cleanRelayState != nil {
		promptCacheKey = cleanRelayState.Mapping.PromptCacheKey
	}
	grokCacheIdentity := ""
	if account.Platform == PlatformGrok {
		grokIntentBody := responsesBody
		grokCacheIdentity = resolveGrokCacheIdentity(c, grokIntentBody, promptCacheKey, upstreamModel)
		patchedBody, patchErr := patchGrokResponsesBody(grokIntentBody, upstreamModel)
		if patchErr != nil {
			return nil, patchErr
		}
		responsesBody, patchErr = applyGrokResponsesCacheIdentity(
			patchedBody,
			grokIntentBody,
			grokCacheIdentity,
			account.IsGrokOAuth(),
		)
		if patchErr != nil {
			return nil, fmt.Errorf("apply grok prompt cache identity: %w", patchErr)
		}
		responsesBody, patchErr = applyGrokFreeMessagesFunctionToolCacheRoute(
			responsesBody,
			grokIntentBody,
			account,
			grokCacheIdentity,
		)
		if patchErr != nil {
			return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", patchErr)
		}
	}
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
	var upstreamReq *http.Request
	if account.Platform == PlatformGrok {
		upstreamReq, err = s.buildGrokResponsesRequest(upstreamCtx, c, account, responsesBody, token, grokCacheIdentity)
	} else {
		upstreamReq, err = s.buildUpstreamRequest(upstreamCtx, c, account, responsesBody, token, isStream, promptCacheKey, false)
	}
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	expectedAgentIdentityTaskID := strings.TrimSpace(account.GetCredential("task_id"))
	if account.IsOpenAIOAuth() {
		// The Messages compatibility bridge uses the ChatGPT Codex endpoint.
		// Restore a complete identity after request construction, then pair it
		// against the final User-Agent to avoid upstream 404 gating.
		ensureCodexIdentityHeaders(upstreamReq.Header)
		enforceCodexIdentityHeaders(upstreamReq.Header)
	}

	// Override session_id with a deterministic UUID derived from the isolated
	// session key, ensuring different API keys produce different upstream sessions.
	if account.Platform != PlatformGrok && promptCacheKey != "" && cleanRelayState == nil && !currentCodexFingerprintOwnsSession(c, account) {
		apiKeyID := getAPIKeyIDFromContext(c)
		upstreamReq.Header.Set("session_id", generateSessionUUID(isolateOpenAISessionID(apiKeyID, promptCacheKey)))
	}

	// 7. Send request
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	resp, err := s.doOpenAIAccountUpstream(upstreamReq, proxyURL, account)
	if err != nil {
		if account.Platform == PlatformGrok {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
		}
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
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	// 8. Handle non-success response with failover
	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(resp, c, account, false, writeAnthropicError)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryWasTried(ctx) &&
			isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
			if err := s.recoverAgentIdentityTask(ctx, account, expectedAgentIdentityTaskID); err != nil {
				return nil, fmt.Errorf("recover Agent Identity task: %w", err)
			}
			retryCtx := withAgentIdentitySensitiveValues(markAgentIdentityTaskRecoveryTried(ctx), expectedAgentIdentityTaskID)
			return s.ForwardAsAnthropic(retryCtx, c, account, body, promptCacheKey, defaultMappedModel)
		}
		// Grok account-switched history often fails decrypt; strip encrypted
		// reasoning once at the client-body level so failover accounts can accept
		// the multi-turn tool continuation instead of cascading 400s.
		if account.Platform == PlatformGrok &&
			isGrokInvalidEncryptedContentResponse(resp.StatusCode, respBody) &&
			!grokEncryptedContentStripRetried(ctx) {
			if strippedBody, ok := stripAnthropicThinkingSignatures(body); ok {
				logger.L().Info("openai messages: stripping thinking signatures for Grok failover retry",
					zap.Int64("account_id", account.ID),
				)
				return s.ForwardAsAnthropic(markGrokEncryptedContentStripRetried(ctx), c, account, strippedBody, promptCacheKey, defaultMappedModel)
			}
		}
		respBody = s.redactAgentIdentitySensitiveBody(ctx, account, respBody)
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody)
		if account.Platform == PlatformGrok {
			shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody)
		}
		// 命中「临时不可调度」规则的错误此前会被直接回给客户端：账号既没被标记，也没换号重试，
		// 下一发请求还会落到同一个账号。这里在未提交响应、尚未判定 failover 时补一次策略检查，
		// 命中即转为 failover。CheckErrorPolicy 自身已完成标记，故下面不再重复走错误处理。
		tempUnscheduled := false
		if c != nil && account != nil && account.Platform != PlatformGrok && !shouldFailover &&
			!IsResponseCommitted(c) && s.rateLimitService != nil {
			tempUnscheduled = s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, respBody) == ErrorPolicyTempUnscheduled
			shouldFailover = tempUnscheduled
		}
		if shouldFailover {
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
			if account.Platform == PlatformGrok {
				s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			} else if !tempUnscheduled {
				s.handleOpenAIAccountUpstreamErrorForModel(ctx, account, originalModel, resp.StatusCode, resp.Header, respBody)
			}
			return nil, newOpenAIUpstreamFailoverError(
				resp.StatusCode,
				resp.Header,
				respBody,
				upstreamMsg,
				shouldRetryOpenAIOnSamePoolAccount(account, resp.StatusCode, upstreamMsg, respBody),
			)
		}
		// Non-failover error: return Anthropic-formatted error to client
		return s.handleAnthropicErrorResponse(resp, c, account, originalModel)
	}
	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
		configuredIdleSeconds := 0
		if s.cfg != nil {
			configuredIdleSeconds = s.cfg.Gateway.StreamDataIntervalTimeout
		}
		streamIdle := resolveGrokStreamIdleTimeout(configuredIdleSeconds)
		resp.Body = newGrokStreamIdleReadCloser(resp.Body, streamIdle, func() {
			s.tempUnscheduleGrok(ctx, account, grokStreamIdleCooldown, "grok stream idle timeout")
		})
	}

	// 9. Handle normal response
	// Upstream is always streaming; choose response format based on client preference.
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	resp.Body = newGrokResponsesBillingPingFilterBody(resp.Body, account, maxLineSize)
	var result *OpenAIForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleAnthropicStreamingResponse(ctx, resp, c, account, originalModel, billingModel, upstreamModel, startTime)
	} else {
		// Client wants JSON: buffer the streaming response and assemble a JSON reply.
		result, handleErr = s.handleAnthropicBufferedStreamingResponse(ctx, resp, c, account, originalModel, billingModel, upstreamModel, startTime)
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
	if opencodeResolved != nil {
		applyOpencodeGoResolvedModelToResult(result, *opencodeResolved)
	}

	// Extract and save Codex usage snapshot from response headers (for OAuth accounts)
	if handleErr == nil && account.Type == AccountTypeOAuth {
		if snapshot := ParseCodexRateLimitHeaders(resp.Header); snapshot != nil {
			s.updateCodexUsageSnapshot(ctx, account.ID, snapshot)
		}
	}

	return result, handleErr
}

// handleAnthropicErrorResponse reads an upstream error and returns it in
// Anthropic error format.
func (s *OpenAIGatewayService) handleAnthropicErrorResponse(
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	return s.handleCompatErrorResponse(resp, c, account, requestedModel, writeAnthropicError)
}

// handleAnthropicBufferedStreamingResponse reads all Responses SSE events from
// the upstream streaming response, finds the terminal event (response.completed
// / response.incomplete / response.failed), converts the complete response to
// Anthropic Messages JSON format, and writes it to the client.
// This is used when the client requested stream=false but the upstream is always
// streaming.
func (s *OpenAIGatewayService) handleAnthropicBufferedStreamingResponse(
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
			logger.L().Warn("openai messages buffered: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			continue
		}

		// Accumulate delta content for fallback when terminal output is empty.
		acc.ProcessEvent(&event)

		// Terminal events carry the complete ResponsesResponse with output + usage.
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
			logger.L().Warn("openai messages buffered: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
		}
		if errors.Is(err, errGrokStreamIdleTimeout) && !c.Writer.Written() {
			configuredSeconds := 0
			if s.cfg != nil {
				configuredSeconds = s.cfg.Gateway.StreamDataIntervalTimeout
			}
			return nil, grokStreamIdleFailoverError(account, resolveGrokStreamIdleTimeout(configuredSeconds))
		}
	}

	if finalResponse == nil {
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Upstream stream ended without a terminal response event")
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
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", clientMsg)
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
			writeAnthropicError(c, status, errType, errMsg)
			return nil, fmt.Errorf("upstream response failed (passthrough): %s", errMsg)
		}
		writeAnthropicError(c, http.StatusBadGateway, "api_error", message)
		return nil, fmt.Errorf("upstream response failed: %s", message)
	}

	// When the terminal event has an empty output array, reconstruct from
	// accumulated delta events so the client receives the full content.
	acc.SupplementResponseOutput(finalResponse)

	anthropicResp := apicompat.ResponsesToAnthropic(finalResponse, originalModel)
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
	// Grok /v1/messages uses the Responses upstream. Preserve the completed
	// native-search count so the shared usage layer can add the search surcharge
	// instead of silently recording token cost only.
	if account != nil && account.IsGrok() {
		if responseBody, marshalErr := json.Marshal(finalResponse); marshalErr == nil {
			result.SearchCount = countGrokNativeSearchCallsFromJSONBytes(responseBody)
		}
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(http.StatusOK, anthropicResp)
	return result, nil
}

// handleAnthropicStreamingResponse reads Responses SSE events from upstream,
// converts each to Anthropic SSE events, and writes them to the client.
// When StreamKeepaliveInterval is configured, it uses a goroutine + channel
// pattern to send Anthropic ping events during periods of upstream silence,
// preventing proxy/client timeout disconnections.
func (s *OpenAIGatewayService) handleAnthropicStreamingResponse(
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

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	state := apicompat.NewResponsesEventToAnthropicState()
	state.Model = originalModel
	var usage OpenAIUsage
	var billingUsageObservation openAIResponsesBillingUsageObservation
	var firstTokenMs *int
	var responseServiceTier string
	var responseID string
	searchCount := 0
	streamSearchSeen := make(map[string]struct{})
	countSearch := account != nil && account.IsGrok()
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

	// resultWithUsage builds the final result snapshot.
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
		result.SearchCount = searchCount
		return result
	}

	// processDataLine handles a single "data: ..." SSE line from upstream.
	// Returns (clientDisconnected bool).
	processDataLine := func(payload string) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		payloadBytes := []byte(payload)
		if countSearch {
			searchCount += countGrokNativeSearchCallsInSSEDataDedup(payloadBytes, streamSearchSeen)
		}
		observer.ObserveOpenAI(payloadBytes, strings.TrimSpace(gjson.GetBytes(payloadBytes, "type").String()))
		billingUsageObservation.observePayload(payloadBytes)
		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal(payloadBytes, &event); err != nil {
			logger.L().Warn("openai messages stream: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			return false
		}

		eventType := strings.TrimSpace(event.Type)
		isBareErrorEvent := eventType == "error"

		// Extract usage from completion events
		if (eventType == "response.completed" || eventType == "response.done" || eventType == "response.incomplete" || eventType == "response.failed") &&
			event.Response != nil && event.Response.Usage != nil {
			if event.Response.ServiceTier != "" {
				responseServiceTier = event.Response.ServiceTier
			}
			usage = copyOpenAIUsageFromResponsesUsage(event.Response.Usage)
		}
		if event.Response != nil && strings.TrimSpace(event.Response.ID) != "" {
			responseID = event.Response.ID
		}
		if eventType == "response.failed" || isBareErrorEvent {
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
						writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", clientMsg)
						clientDisconnected = true
					} else if writeAnthropicStreamError(c, "invalid_request_error", clientMsg) {
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
			errStatus, errType, errMsg := http.StatusBadGateway, "api_error", message
			if status, mappedType, mappedMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(c, account.Platform, payloadBytes, message); matched {
				if mappedMsg == "" {
					mappedMsg = errMsg
				}
				errStatus, errType, errMsg = status, mappedType, mappedMsg
				MarkResponseCommitted(c)
			}
			if !clientDisconnected {
				if c.Writer.Written() {
					_ = writeAnthropicStreamError(c, errType, errMsg)
				} else {
					writeAnthropicError(c, errStatus, errType, errMsg)
				}
			}
			terminalErr = fmt.Errorf("upstream response failed: %s", errMsg)
			return true
		}
		// response.incomplete remains an accepted compatibility terminal for
		// downstream conversion, while the observer's sticky rejection keeps its
		// model audit-only and ineligible for billing.
		successTerminal := eventType == "response.completed" || eventType == "response.done" || eventType == "response.incomplete"
		if eventType == "response.done" {
			event.Type = "response.completed"
		}

		// Convert and serialize the whole event before the durable billing gate.
		// This ensures a conversion failure cannot leave a ready billing intent
		// while withholding the corresponding terminal response from the client.
		events := apicompat.ResponsesEventToAnthropicEvents(&event, state)
		serializedEvents := make([]string, 0, len(events))
		for _, evt := range events {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(evt)
			if err != nil {
				observer.RejectBilling()
				terminalErr = fmt.Errorf("marshal Anthropic stream event: %w", err)
				return true
			}
			serializedEvents = append(serializedEvents, sse)
		}
		if successTerminal {
			sawSuccessTerminal = true
		}

		if !clientDisconnected {
			for _, sse := range serializedEvents {
				if _, err := fmt.Fprint(c.Writer, sse); err != nil {
					clientDisconnected = true
					startDisconnectedDrain()
					s.logClientDisconnectDrainDecision(ctx, "openai messages stream", requestID, "write_event")
					break
				}
			}
		}
		if !clientDisconnected && len(serializedEvents) > 0 {
			c.Writer.Flush()
		}
		return false
	}

	// finalizeStream sends any remaining Anthropic events and returns the result.
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
		if !sawSuccessTerminal {
			return resultWithUsage(), errors.New("upstream responses stream ended without a successful terminal event")
		}
		markObservedUpstreamResponseModelBillingEligible(c)
		if finalEvents := apicompat.FinalizeResponsesAnthropicStream(state); len(finalEvents) > 0 {
			for _, evt := range finalEvents {
				sse, err := apicompat.ResponsesAnthropicEventToSSE(evt)
				if err != nil {
					continue
				}
				fmt.Fprint(c.Writer, sse) //nolint:errcheck
			}
			c.Writer.Flush()
		}
		return resultWithUsage(), nil
	}
	streamReadError := func(err error) error {
		if errors.Is(err, errGrokStreamIdleTimeout) {
			configuredSeconds := 0
			if s.cfg != nil {
				configuredSeconds = s.cfg.Gateway.StreamDataIntervalTimeout
			}
			idle := resolveGrokStreamIdleTimeout(configuredSeconds)
			if !c.Writer.Written() {
				return grokStreamIdleFailoverError(account, idle)
			}
			if !clientDisconnected {
				_ = writeAnthropicStreamError(c, "api_error", "Grok upstream stream timed out")
			}
			return fmt.Errorf("grok stream idle timeout after partial Anthropic output: %w", err)
		}
		return err
	}

	// handleScanErr logs scanner errors if meaningful.
	handleScanErr := func(err error) {
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("openai messages stream: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
		}
	}

	// ── Determine keepalive interval ──
	keepaliveInterval := time.Duration(0)
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
	}

	// ── No keepalive: fast synchronous path (no goroutine overhead) ──
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
		if scanErr := scanner.Err(); scanErr != nil {
			return resultWithUsage(), streamReadError(scanErr)
		}
		return finalizeStream()
	}

	// ── With keepalive: goroutine + channel + select ──
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
				// Upstream closed
				return finalizeStream()
			}
			if ev.err != nil {
				handleScanErr(ev.err)
				return resultWithUsage(), streamReadError(ev.err)
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
			// Send Anthropic-format ping event
			if _, err := fmt.Fprint(c.Writer, "event: ping\ndata: {\"type\":\"ping\"}\n\n"); err != nil {
				// Client disconnected
				clientDisconnected = true
				startDisconnectedDrain()
				s.logClientDisconnectDrainDecision(ctx, "openai messages stream", requestID, "keepalive")
				continue
			}
			c.Writer.Flush()
		}
	}
}

// writeAnthropicError writes an error response in Anthropic Messages API format.
func writeAnthropicError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func writeAnthropicStreamError(c *gin.Context, errType, message string) bool {
	payload, err := json.Marshal(gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
	if err != nil {
		logger.L().Warn("openai messages stream: failed to marshal error event", zap.Error(err))
		return false
	}
	if _, err := fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", payload); err != nil {
		return true
	}
	c.Writer.Flush()
	return false
}

func copyOpenAIUsageFromResponsesUsage(usage *apicompat.ResponsesUsage) OpenAIUsage {
	if usage == nil {
		return OpenAIUsage{}
	}
	result := OpenAIUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
	}
	if usage.InputTokensDetails != nil {
		result.CacheReadInputTokens = usage.InputTokensDetails.CachedTokens
	}
	return result
}
