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
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// AlphaSearch proxies the standalone Codex web-search endpoint.
func (h *OpenAIGatewayHandler) AlphaSearch(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	setOpenAIClientTransportHTTP(c)
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group.Platform != service.PlatformOpenAI {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search is only available for OpenAI groups")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.alpha_search",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readAlphaSearchBody(c)
	if err != nil {
		status, code, message := http.StatusBadRequest, "invalid_request_error", "Failed to read request body"
		if maxErr, ok := extractMaxBytesError(err); ok {
			status = http.StatusRequestEntityTooLarge
			message = buildBodyTooLargeMessage(maxErr.Limit)
		}
		h.errorResponse(c, status, code, message)
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	modelResult := gjson.GetBytes(body, "model")
	if modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	requestedModel := strings.TrimSpace(modelResult.String())
	setOpsRequestContext(c, requestedModel, false, body)
	reqLog = reqLog.With(zap.String("model", requestedModel))
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	routingStart := time.Now()
	userRelease, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}

	searchID := strings.TrimSpace(gjson.GetBytes(body, "id").String())
	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(c, body, searchID)
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	if _, ok := routeCursor.current(); !ok {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes")
		return
	}
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	switchCount := 0
	var routeBillingGate apiKeyGroupRouteBillingGate

	for {
		if failoverClientGone(c) {
			return
		}
		routeCandidate, ok := routeCursor.current()
		if !ok || routeCandidate.APIKey == nil {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes")
			return
		}
		currentAPIKey := routeCandidate.APIKey
		if currentAPIKey.Group == nil || currentAPIKey.Group.Platform != service.PlatformOpenAI {
			if routeCursor.skipToNext("alpha_search_non_openai_group", reqLog) {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue
			}
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search is only available for OpenAI groups")
			return
		}

		currentSubscription, subErr := h.gatewayService.ResolveRouteSubscription(c.Request.Context(), currentAPIKey, subscription)
		if subErr != nil {
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, subErr, "route_subscription_unavailable", reqLog)
			if retry {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue
			}
			status, code, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, requestedModel)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, err, "route_billing_ineligible", reqLog)
			if retry {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue
			}
			status, code, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}

		selectionModel := requestedModel
		if channelMapping.Mapped && strings.TrimSpace(channelMapping.MappedModel) != "" {
			selectionModel = channelMapping.MappedModel
		}
		selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
		selectionCtx = openAICompatibleRequestContext(selectionCtx, currentAPIKey)
		selection, _, selectErr := h.gatewayService.SelectAccountWithScheduler(
			selectionCtx,
			currentAPIKey.GroupID,
			"",
			sessionHash,
			selectionModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportHTTPSSE,
			false,
		)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if failoverClientGone(c) {
				return
			}
			if selectErr != nil && h.handleAccountShareModeSelectionError(c, selectErr, streamStarted) {
				return
			}
			if lastFailoverErr != nil && routeCursor.hasNext() && shouldSwitchAPIKeyGroupRoute(lastFailoverErr) && routeCursor.switchToNext(currentAPIKey.ID, "alpha_search_account_selection_exhausted", reqLog) {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue
			}
			if len(failedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, selectionModel, requestedModel, service.PlatformOpenAI)
				h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			return
		}

		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)
		freshAccount, accountRelease, acquired, retryRoute := h.acquireResponsesAccountSlot(c, selectionCtx, currentAPIKey.GroupID, sessionHash, service.OpenAIAccountDispatchRequirements{
			RequestedModel:    selectionModel,
			RequiredTransport: service.OpenAIUpstreamTransportHTTPSSE,
		}, selection, false, &streamStarted, routeCursor, reqLog)
		if retryRoute {
			// 当前分组并发打满，换下一条路由重试（未向客户端写任何响应）。
			failedAccountIDs = make(map[int64]struct{})
			sameAccountRetryCount = make(map[int64]int)
			switchCount = 0
			lastFailoverErr = nil
			continue
		}
		if !acquired {
			return
		}
		account = freshAccount
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped && strings.TrimSpace(channelMapping.MappedModel) != "" {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		forwardStart := time.Now()
		forwardCtx, cancelForward := bindAccountSelectionForwardContext(selectionCtx, selection)
		requestPayloadHash := service.HashUsageRequestPayload(body)
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
				ChannelUsageFields: channelMapping.ToUsageFields(requestedModel, result.UpstreamModel),
			})
		}
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			defer cancelForward()
			if accountRelease != nil {
				defer accountRelease()
			}
			return h.gatewayService.ForwardAlphaSearch(forwardCtx, c, account, forwardBody)
		}()
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, time.Since(forwardStart).Milliseconds())
		recordUsageResult := func(result *service.OpenAIForwardResult) {
			if result == nil {
				return
			}
			h.submitUsageRecordTask(forwardCtx, func(ctx context.Context) {
				usageCtx := service.WithAccountShareModeRequestFromContext(ctx, forwardCtx)
				if err := recordUsage(usageCtx, result); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.alpha_search"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", currentAPIKey.ID),
						zap.Any("group_id", currentAPIKey.GroupID),
						zap.String("model", requestedModel),
						zap.Int64("account_id", account.ID),
					).Error("openai_alpha_search.record_usage_failed", zap.Error(err))
				}
			})
		}
		hasBillableUsage := service.OpenAIForwardResultHasBillableUsage(result)
		if forwardErr != nil && hasBillableUsage {
			recordUsageResult(result)
		}

		if forwardErr == nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil, account.GetMappedModel(selectionModel))
			routeCursor.recordSuccess(currentAPIKey.ID)
			recordUsageResult(result)
			return
		}

		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
			if failoverClientGone(c) {
				reqLog.Info("openai_alpha_search.forward_aborted_client_disconnected", zap.Int64("account_id", account.ID))
				return
			}
			if c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("openai_alpha_search.forward_failed", zap.Int64("account_id", account.ID), zap.Error(forwardErr))
			return
		}
		if failoverErr.ShouldReportAccountScheduleFailure() {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
		}
		if !openAIForwardMayFailover(c, writerSizeBeforeForward, failoverErr) {
			h.handleFailoverExhausted(c, failoverErr, false)
			return
		}
		if failoverClientGone(c) {
			return
		}
		if failoverErr.RetryableOnSameAccount {
			retryLimit := account.GetPoolModeRetryCount()
			if sameAccountRetryCount[account.ID] < retryLimit {
				sameAccountRetryCount[account.ID]++
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
		sameAccountRetryCount[account.ID] = 0
		lastFailoverErr = failoverErr
		if switchCount >= h.maxAccountSwitches {
			if canSwitchAPIKeyGroupRouteAfterForward(c, routeCursor, failoverErr, false, writerSizeBeforeForward) &&
				routeCursor.switchToNext(currentAPIKey.ID, "alpha_search_upstream_failover_exhausted", reqLog, zap.Int("upstream_status", failoverErr.StatusCode)) {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue
			}
			h.handleFailoverExhausted(c, failoverErr, false)
			return
		}
		switchCount++
		reqLog.Warn("openai_alpha_search.upstream_failover_switching",
			zap.Int64("account_id", account.ID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("switch_count", switchCount),
		)
	}
}

func readAlphaSearchBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil {
		return nil, errors.New("request is required")
	}
	return pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
}
