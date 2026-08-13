package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Responses handles OpenAI Responses API endpoint for Anthropic platform groups.
// POST /v1/responses
// This converts Responses API requests to Anthropic format, forwards to Anthropic
// upstream, and converts responses back to Responses format.
func (h *GatewayHandler) Responses(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.responsesErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.responsesErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if h.checkNoAccountBackoff(c, subject.UserID, apiKey.GroupID, h.responsesErrorResponse) {
		return
	}

	// Read request body
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.responsesErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false, body)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream using gjson (like OpenAI handler)
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	reqStream := gjson.GetBytes(body, "stream").Bool()
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream, nil)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 解析渠道级模型映射

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.responses.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// Parse request for session hash
	parsedReq, _ := service.ParseGatewayRequest(body, "responses")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: body}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetSecurityClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)

	// 3. Account selection + failover loop
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	if _, ok := routeCursor.current(); !ok {
		h.responsesErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes")
		return
	}

	var routeBillingGate apiKeyGroupRouteBillingGate

routeLoop:
	for {
		routeCandidate, ok := routeCursor.current()
		if !ok {
			h.responsesErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes")
			return
		}
		currentAPIKey := routeCandidate.APIKey
		currentSubscription, subErr := h.gatewayService.ResolveRouteSubscription(c.Request.Context(), currentAPIKey, subscription)
		if subErr != nil {
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, subErr, "route_subscription_unavailable", reqLog)
			if retry {
				continue routeLoop
			}
			status, code, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.responsesErrorResponse(c, status, code, message)
			return
		}
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, reqModel)
		if currentAPIKey.Group != nil && currentAPIKey.Group.ClaudeCodeOnly {
			if routeCursor.skipToNext("responses_claude_code_only", reqLog, zap.Int64p("group_id", currentAPIKey.GroupID)) {
				continue routeLoop
			}
			h.responsesErrorResponse(c, http.StatusForbidden, "permission_error",
				"This group is restricted to Claude Code clients (/v1/messages only)")
			return
		}
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("gateway.responses.billing_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, err, "route_billing_ineligible", reqLog)
			if retry {
				continue routeLoop
			}
			status, code, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.responsesErrorResponse(c, status, code, message)
			return
		}
		if decision := h.checkCyberPreflight(c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body); decision != nil && decision.Blocked {
			h.responsesErrorResponse(c, contentModerationStatus(decision), cyberPreflightErrorCode(decision), decision.Message)
			return
		}
		fs := NewFailoverState(h.maxAccountSwitches, false)

		for {
			selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
			selection, err := h.gatewayService.SelectAccountWithLoadAwareness(selectionCtx, currentAPIKey.GroupID, sessionHash, reqModel, fs.FailedAccountIDs, "", int64(0))
			if err != nil {
				if errors.Is(err, service.ErrAccountShareModeSelection) {
					h.responsesErrorResponse(c, http.StatusServiceUnavailable, "account_share_unavailable", "共享账号暂时不可用，请稍后重试")
					return
				}
				if len(fs.FailedAccountIDs) == 0 {
					if routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(err)) {
						continue routeLoop
					}
					cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, reqModel, reqModel, service.PlatformAnthropic)
					if cls.Status == http.StatusServiceUnavailable {
						h.recordNoAccountFailure(c, reqLog, subject.UserID, apiKey.GroupID, streamStarted)
					}
					message := cls.Message
					if !cls.ModelNotFound {
						message = "No available accounts: " + err.Error()
					}
					h.responsesErrorResponse(c, cls.Status, cls.ErrType, message)
					return
				}
				action := fs.HandleSelectionExhausted(c.Request.Context())
				switch action {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					failoverClientGone(c)
					return
				default:
					if fs.LastFailoverErr != nil {
						if !streamStarted && shouldSwitchAPIKeyGroupRoute(fs.LastFailoverErr) &&
							routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", fs.LastFailoverErr.StatusCode)) {
							continue routeLoop
						}
						h.handleResponsesFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
					} else {
						h.responsesErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
					}
					return
				}
			}
			account := selection.Account
			setOpsSelectedAccount(c, account.ID, account.Platform)
			if decision := h.checkUserContentModeration(c, reqLog, currentAPIKey, subject, account, service.ContentModerationProtocolOpenAIResponses, reqModel, body); decision != nil && decision.Blocked {
				if selection.Acquired && selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				h.responsesErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
				return
			}

			// 4. Acquire account concurrency slot
			accountReleaseFunc := selection.ReleaseFunc
			if !selection.Acquired {
				// 分组并发打满先尝试换下一条路由；已开始写字节后不能再换。
				capacityUnavailable := func(reason string, writeErr func()) bool {
					if !streamStarted && routeCursor.skipToNext(reason, reqLog, zap.Int64("account_id", account.ID)) {
						return true
					}
					writeErr()
					return false
				}
				if selection.WaitPlan == nil {
					if capacityUnavailable("account_slot_no_wait_plan", func() {
						h.responsesErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
					}) {
						continue routeLoop
					}
					return
				}
				accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
					c,
					account.ID,
					selection.WaitPlan.MaxConcurrency,
					selection.WaitPlan.Timeout,
					reqStream,
					&streamStarted,
				)
				if err != nil {
					reqLog.Warn("gateway.responses.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					if capacityUnavailable("account_slot_acquire_timeout", func() {
						h.handleConcurrencyError(c, err, "account", streamStarted)
					}) {
						continue routeLoop
					}
					return
				}
			}
			accountReleaseFunc = wrapAccountSelectionReleaseOnDone(c.Request.Context(), selection, accountReleaseFunc)

			// 5. Forward request
			writerSizeBeforeForward := c.Writer.Size()
			forwardBody := body
			if channelMapping.Mapped {
				forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
			}
			forwardCtx, cancelForward := bindAccountSelectionForwardContext(selectionCtx, selection)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetSecurityClientIP(c)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
			recordUsage := func(ctx context.Context, result *service.ForwardResult) error {
				if result == nil {
					return nil
				}
				return h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
					Result:             result,
					APIKey:             currentAPIKey,
					User:               currentAPIKey.User,
					Account:            account,
					Subscription:       currentSubscription,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					RequestPayloadHash: requestPayloadHash,
					APIKeyService:      h.apiKeyService,
					ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
				})
			}
			result, err := h.gatewayService.ForwardAsResponses(forwardCtx, c, account, forwardBody, parsedReq)
			cancelForward()

			recordUsageResult := func(result *service.ForwardResult) {
				if result == nil {
					return
				}
				h.submitUsageRecordTask(forwardCtx, func(ctx context.Context) {
					usageCtx := service.WithAccountShareModeRequestFromContext(ctx, forwardCtx)
					if err := recordUsage(usageCtx, result); err != nil {
						reqLog.Error("gateway.responses.record_usage_failed",
							zap.Int64("account_id", account.ID),
							zap.Error(err),
						)
					}
				})
			}
			hasBillableUsage := result != nil &&
				(service.IsBillableStreamUsageError(err) || service.ForwardResultHasBillableUsage(result))
			finalizeAccountShareRequest(hasBillableUsage, func() { recordUsageResult(result) }, accountReleaseFunc)
			h.gatewayService.ReportAccountForwardResult(account.ID, result, err)

			if err != nil {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					// Can't failover if streaming content already sent
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleResponsesFailoverExhausted(c, failoverErr, true)
						return
					}
					action := fs.HandleFailoverErrorWithRetryLimit(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
					switch action {
					case FailoverContinue:
						continue
					case FailoverExhausted:
						if canSwitchAPIKeyGroupRouteAfterForward(c, routeCursor, fs.LastFailoverErr, streamStarted, writerSizeBeforeForward) &&
							routeCursor.switchToNext(apiKey.ID, "upstream_failover_exhausted", reqLog, zap.Int("upstream_status", fs.LastFailoverErr.StatusCode)) {
							continue routeLoop
						}
						h.handleResponsesFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
						return
					case FailoverCanceled:
						failoverClientGone(c)
						return
					}
				}
				h.ensureForwardErrorResponse(c, streamStarted)
				reqLog.Error("gateway.responses.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
				return
			}
			routeCursor.recordSuccess(apiKey.ID)

			return
		}
	}
}

// responsesErrorResponse writes an error in OpenAI Responses API format.
func (h *GatewayHandler) responsesErrorResponse(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

// handleResponsesFailoverExhausted writes a failover-exhausted error in Responses format.
func (h *GatewayHandler) handleResponsesFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return // Can't write error after stream started
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	h.responsesErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
