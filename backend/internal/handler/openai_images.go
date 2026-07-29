package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Images handles OpenAI Images API requests.
// POST /v1/images/generations
// POST /v1/images/edits
func (h *OpenAIGatewayHandler) Images(c *gin.Context) {
	streamStarted := false
	billingDispatchAttemptNo := 0
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.images",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if isMultipartImagesContentType(c.GetHeader("Content-Type")) {
		setOpsRequestContext(c, "", false, nil)
	} else {
		setOpsRequestContext(c, "", false, body)
	}

	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	reqLog = reqLog.With(
		zap.String("model", parsed.Model),
		zap.Bool("stream", parsed.Stream),
		zap.Bool("multipart", parsed.Multipart),
		zap.String("capability", string(parsed.RequiredCapability)),
	)

	if parsed.Multipart {
		setOpsRequestContext(c, parsed.Model, parsed.Stream, nil)
	} else {
		setOpsRequestContext(c, parsed.Model, parsed.Stream, nil)
	}
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, parsed.Stream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)

	maxAccountSwitches := h.maxAccountSwitches
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	stopJSONKeepalive := func() {}
	jsonKeepaliveStarted := false
	defer func() { stopJSONKeepalive() }()
	if _, ok := routeCursor.current(); !ok {
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
		return
	}

routeLoop:
	for {
		if failoverClientGone(c) {
			return
		}
		routeCandidate, ok := routeCursor.current()
		if !ok {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
			return
		}
		currentAPIKey := routeCandidate.APIKey
		currentSubscription, subErr := h.gatewayService.ResolveRouteSubscription(c.Request.Context(), currentAPIKey, subscription)
		if subErr != nil {
			status, code, message, retryAfter := billingErrorDetails(subErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, parsed.Model)
		selectionModel := resolveOpenAIAccountSelectionModel(parsed.Model, channelMapping)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("openai.images.billing_eligibility_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		switchCount := 0
		failedAccountIDs := make(map[int64]struct{})
		sameAccountRetryCount := make(map[int64]int)
		var lastFailoverErr *service.UpstreamFailoverError

		for {
			if failoverClientGone(c) {
				return
			}
			reqLog.Debug("openai.images.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
			selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
			if decision := h.checkCyberPreflightWithContext(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIImages, parsed.Model, body); decision != nil && decision.Blocked {
				h.handleStreamingAwareError(c, contentModerationStatus(decision), cyberPreflightErrorCode(decision), decision.Message, streamStarted)
				return
			}
			if decision := h.checkContentModerationWithContext(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIImages, parsed.Model, body); decision != nil && decision.Blocked {
				h.handleStreamingAwareError(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message, streamStarted)
				return
			}
			selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForImages(
				selectionCtx,
				currentAPIKey.GroupID,
				sessionHash,
				selectionModel,
				failedAccountIDs,
				parsed.RequiredCapability,
			)
			if err != nil {
				if failoverClientGone(c) {
					return
				}
				reqLog.Warn("openai.images.account_select_failed",
					zap.Error(err),
					zap.Int("excluded_account_count", len(failedAccountIDs)),
				)
				if h.handleAccountShareModeSelectionError(c, err, streamStarted) {
					return
				}
				if len(failedAccountIDs) == 0 {
					if routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(err)) {
						continue routeLoop
					}
					cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, selectionModel, parsed.Model, service.PlatformOpenAI)
					h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
					return
				}
				if lastFailoverErr != nil {
					if shouldSwitchAPIKeyGroupRoute(lastFailoverErr) &&
						routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", lastFailoverErr.StatusCode)) {
						continue routeLoop
					}
					h.handleImagesFailoverExhausted(c, lastFailoverErr, streamStarted)
				} else {
					h.handleFailoverExhaustedSimple(c, 502, streamStarted)
				}
				return
			}
			if selection == nil || selection.Account == nil {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, selectionModel, parsed.Model, service.PlatformOpenAI)
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return
			}

			reqLog.Debug("openai.images.account_schedule_decision",
				zap.String("layer", scheduleDecision.Layer),
				zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
				zap.Int("candidate_count", scheduleDecision.CandidateCount),
				zap.Int("top_k", scheduleDecision.TopK),
				zap.Int64("latency_ms", scheduleDecision.LatencyMs),
				zap.Float64("load_skew", scheduleDecision.LoadSkew),
			)

			account := selection.Account
			sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
			reqLog.Debug("openai.images.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
			setOpsSelectedAccount(c, account.ID, account.Platform)
			if decision := h.checkUserContentModerationWithContent(selectionCtx, c, reqLog, currentAPIKey, subject, account, service.ContentModerationProtocolOpenAIImages, parsed.Model, body, nil); decision != nil && decision.Blocked {
				if selection.Acquired && selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				h.handleStreamingAwareError(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message, streamStarted)
				return
			}

			freshAccount, accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, selectionCtx, currentAPIKey.GroupID, sessionHash, service.OpenAIAccountDispatchRequirements{
				RequestedModel:          selectionModel,
				RequiredTransport:       service.OpenAIUpstreamTransportHTTPSSE,
				RequiredImageCapability: parsed.RequiredCapability,
			}, selection, parsed.Stream, &streamStarted, reqLog)
			if !acquired {
				return
			}
			account = freshAccount

			service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
			if !parsed.Stream && !jsonKeepaliveStarted {
				stopJSONKeepalive = service.StartOpenAIImagesJSONKeepalive(c, h.openAIImagesJSONKeepaliveInterval())
				jsonKeepaliveStarted = true
			}
			forwardStart := time.Now()
			writerSizeBeforeForward := service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
			forwardCtx, cancelForward := bindAccountSelectionForwardContext(selectionCtx, selection)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			if parsed.Multipart {
				requestPayloadHash = service.HashUsageRequestPayload([]byte(parsed.StickySessionSeed()))
			}
			routedModel := parsed.Model
			if channelMapping.Mapped {
				routedModel = channelMapping.MappedModel
			}
			routedModel = account.GetMappedModel(routedModel)
			forwardCtx, err = beginAccountShareBillingDispatch(
				forwardCtx,
				h.gatewayService,
				selection,
				&billingDispatchAttemptNo,
				service.AccountShareBillingDispatchInput{
					APIKey:             currentAPIKey,
					User:               currentAPIKey.User,
					Subscription:       currentSubscription,
					RequestedModel:     parsed.Model,
					RoutedModel:        routedModel,
					InboundEndpoint:    GetInboundEndpoint(c),
					UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
					RequestType:        accountShareBillingRequestType(parsed.Stream),
					RequestPayloadHash: requestPayloadHash,
					ChannelUsageFields: channelMapping.ToUsageFields(parsed.Model, routedModel),
				},
			)
			if err != nil {
				cancelForward()
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				reqLog.Error("openai.images.billing_dispatch_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Billing state is temporarily unavailable; no upstream request was sent", streamStarted)
				return
			}
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
			recordUsage := func(ctx context.Context, result *service.OpenAIForwardResult) error {
				if result == nil {
					return nil
				}
				return h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
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
					ChannelUsageFields: channelMapping.ToUsageFields(parsed.Model, result.UpstreamModel),
				})
			}
			forwardCtx = withOpenAIForwardResultBillingGate(forwardCtx, recordUsage)
			result, err := h.gatewayService.ForwardImages(forwardCtx, c, account, body, parsed, channelMapping.MappedModel)
			cancelForward()
			forwardDurationMs := time.Since(forwardStart).Milliseconds()
			recordUsageResult := func(result *service.OpenAIForwardResult) {
				if result == nil {
					return
				}
				if handled, billingErr := service.CommitOpenAIForwardResultBillingGate(forwardCtx, result); handled {
					if billingErr != nil {
						logger.L().With(
							zap.String("component", "handler.openai_gateway.images"),
							zap.Int64("user_id", subject.UserID),
							zap.Int64("api_key_id", currentAPIKey.ID),
							zap.Any("group_id", currentAPIKey.GroupID),
							zap.String("model", parsed.Model),
							zap.Int64("account_id", account.ID),
						).Error("openai.images.record_usage_failed", zap.Error(billingErr))
					}
					return
				}
				h.submitUsageRecordTask(forwardCtx, func(ctx context.Context) {
					usageCtx := service.WithAccountShareModeRequestFromContext(ctx, forwardCtx)
					if err := recordUsage(usageCtx, result); err != nil {
						logger.L().With(
							zap.String("component", "handler.openai_gateway.images"),
							zap.Int64("user_id", subject.UserID),
							zap.Int64("api_key_id", currentAPIKey.ID),
							zap.Any("group_id", currentAPIKey.GroupID),
							zap.String("model", parsed.Model),
							zap.Int64("account_id", account.ID),
						).Error("openai.images.record_usage_failed", zap.Error(err))
					}
				})
			}
			hasBillableUsage := service.OpenAIForwardResultHasBillableUsage(result)
			if err != nil && hasBillableUsage {
				recordUsageResult(result)
			}
			if finalizeErr := completeAccountShareBillingDispatchWithoutUsage(forwardCtx, err, hasBillableUsage, parsed.Stream); finalizeErr != nil {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				reqLog.Error("openai.images.billing_dispatch_finalize_failed", zap.Int64("account_id", account.ID), zap.Error(finalizeErr))
				if c.Writer.Size() == writerSizeBeforeForward {
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Billing state is temporarily unavailable", streamStarted)
				}
				return
			}
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
			responseLatencyMs := forwardDurationMs
			if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
				responseLatencyMs = forwardDurationMs - upstreamLatencyMs
			}
			service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
			if err == nil && result != nil && result.FirstTokenMs != nil {
				service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
			}
			if err != nil {
				err = h.gatewayService.NormalizeGrokCredentialFailure(c.Request.Context(), c, account, err)
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if failoverClientGone(c) {
						return
					}
					if shouldReportOpenAIImagesScheduleFailure(failoverErr) {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
					}
					if !openAIImagesForwardMayFailover(c, writerSizeBeforeForward, failoverErr) {
						reqLog.Warn("openai.images.upstream_failover_skipped_after_flush",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						h.handleImagesFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.SafeToFailoverAfterWrite && c.Writer.Written() && !service.OpenAIImagesJSONKeepalivePresent(c) {
						streamStarted = true
					}
					if !failoverErr.ShouldRetryNextAccount() {
						h.handleImagesFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai.images.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(sameAccountRetryDelay):
							}
							continue
						}
					}
					h.gatewayService.RecordOpenAIAccountSwitch()
					failedAccountIDs[account.ID] = struct{}{}
					lastFailoverErr = failoverErr
					if switchCount >= maxAccountSwitches {
						if canSwitchAPIKeyGroupRouteAfterForward(c, routeCursor, failoverErr, streamStarted, writerSizeBeforeForward) &&
							routeCursor.switchToNext(apiKey.ID, "upstream_failover_exhausted", reqLog, zap.Int("upstream_status", failoverErr.StatusCode)) {
							continue routeLoop
						}
						h.handleImagesFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					reqLog.Warn("openai.images.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				wroteFallback := h.ensureForwardErrorResponse(c, streamStarted)
				fields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Error(err),
				}
				if shouldLogOpenAIForwardFailureAsWarn(c, wroteFallback) {
					reqLog.Warn("openai.images.forward_failed", fields...)
					return
				}
				reqLog.Error("openai.images.forward_failed", fields...)
				return
			}

			if result != nil {
				if account.Type == service.AccountTypeOAuth {
					h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, result.FirstTokenMs, account.GetMappedModel(selectionModel))
			} else {
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil, account.GetMappedModel(selectionModel))
			}
			routeCursor.recordSuccess(apiKey.ID)

			recordUsageResult(result)

			reqLog.Debug("openai.images.request_completed",
				zap.Int64("account_id", account.ID),
				zap.Int("switch_count", switchCount),
			)
			return
		}
	}
}

func shouldReportOpenAIImagesScheduleFailure(failoverErr *service.UpstreamFailoverError) bool {
	if failoverErr == nil {
		return false
	}
	if failoverErr.Scope == service.GatewayFailureScopeRequest && failoverErr.NextAccountAction == service.NextAccountStop {
		return false
	}
	return failoverErr.ShouldReportAccountScheduleFailure()
}

func openAIImagesForwardMayFailover(c *gin.Context, writerSizeBeforeForward int, failoverErr *service.UpstreamFailoverError) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	if service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return true
	}
	return failoverErr != nil && failoverErr.SafeToFailoverAfterWrite
}

func (h *OpenAIGatewayHandler) handleImagesFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, streamStarted bool) {
	if failoverErr != nil &&
		failoverErr.Scope == service.GatewayFailureScopeRequest &&
		failoverErr.NextAccountAction == service.NextAccountStop &&
		failoverErr.ClientStatusCode >= http.StatusBadRequest &&
		failoverErr.ClientStatusCode < http.StatusInternalServerError {
		message := strings.TrimSpace(failoverErr.ClientMessage)
		if message == "" {
			message = http.StatusText(failoverErr.ClientStatusCode)
		}
		h.handleStreamingAwareError(c, failoverErr.ClientStatusCode, "invalid_request_error", message, streamStarted)
		return
	}
	h.handleFailoverExhausted(c, failoverErr, streamStarted)
}

func (h *OpenAIGatewayHandler) openAIImagesJSONKeepaliveInterval() time.Duration {
	if h == nil || h.cfg == nil || h.cfg.Gateway.ImageNonstreamKeepaliveInterval <= 0 {
		return 0
	}
	return time.Duration(h.cfg.Gateway.ImageNonstreamKeepaliveInterval) * time.Second
}

func isMultipartImagesContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data")
}
