package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// OpenAIGatewayHandler handles OpenAI API gateway requests
type OpenAIGatewayHandler struct {
	gatewayService             *service.OpenAIGatewayService
	billingCacheService        *service.BillingCacheService
	apiKeyService              *service.APIKeyService
	usageRecordWorkerPool      *service.UsageRecordWorkerPool
	errorPassthroughService    *service.ErrorPassthroughService
	contentModerationService   *service.ContentModerationService
	userModerationService      *service.UserContentModerationService
	grokMediaEligibilityProber grokMediaEligibilityProber
	noAccountBackoffLimiter    service.NoAccountBackoffLimiter
	concurrencyHelper          *ConcurrencyHelper
	maxAccountSwitches         int
	cfg                        *config.Config
}

type grokMediaEligibilityProber interface {
	ProbeMediaEligibility(ctx context.Context, accountID int64) (bool, string, error)
}

const maxOpenAIFirstOutputTimeoutSwitches = 1

func newOpenAIWSTurnClientRequestID(turn int, payloadHash string) string {
	return fmt.Sprintf(
		"openai-ws-turn:%d:%s:%s",
		turn,
		strings.TrimSpace(payloadHash),
		uuid.NewString(),
	)
}

func (h *OpenAIGatewayHandler) beginOpenAIUpstreamAttempt(c *gin.Context, apiKey *service.APIKey, account *service.Account) string {
	attemptID := uuid.NewString()
	effectiveGroupID := apiKeyGroupIDValue(apiKey)
	enforced := h != nil && h.gatewayService != nil && c != nil && c.Request != nil &&
		h.gatewayService.IsCyberPolicyGroupEnforced(c.Request.Context(), effectiveGroupID)
	service.BeginOpenAIUpstreamAttempt(c, attemptID, enforced)
	setOpsEffectiveRoute(c, apiKey, account)
	return attemptID
}

func openAIWSTurnBillingDisposition(
	result *service.OpenAIForwardResult,
	turnErr error,
) (recordUsage bool, completeWithoutUsage bool, hasBillableUsage bool) {
	hasBillableUsage = service.OpenAIForwardResultHasBillableUsage(result)
	recordUsage = result != nil && (turnErr == nil || hasBillableUsage)
	completeWithoutUsage = turnErr != nil && !hasBillableUsage
	return recordUsage, completeWithoutUsage, hasBillableUsage
}

func resolveOpenAIForwardDefaultMappedModel(apiKey *service.APIKey, fallbackModel string) string {
	if fallbackModel = strings.TrimSpace(fallbackModel); fallbackModel != "" {
		return fallbackModel
	}
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return strings.TrimSpace(apiKey.Group.DefaultMappedModel)
}

func resolveOpenAIMessagesDispatchMappedModel(apiKey *service.APIKey, requestedModel string) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return strings.TrimSpace(apiKey.Group.ResolveMessagesDispatchModel(requestedModel))
}

func resolveOpenAIAccountSelectionModel(requestedModel string, mapping service.ChannelMappingResult) string {
	if mapping.Mapped {
		if mappedModel := strings.TrimSpace(mapping.MappedModel); mappedModel != "" {
			return mappedModel
		}
	}
	return strings.TrimSpace(requestedModel)
}

// NewOpenAIGatewayHandler creates a new OpenAIGatewayHandler
func NewOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	userModerationService *service.UserContentModerationService,
	noAccountBackoffLimiter service.NoAccountBackoffLimiter,
	cfg *config.Config,
) *OpenAIGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
	}
	return &OpenAIGatewayHandler{
		gatewayService:           gatewayService,
		billingCacheService:      billingCacheService,
		apiKeyService:            apiKeyService,
		usageRecordWorkerPool:    usageRecordWorkerPool,
		errorPassthroughService:  errorPassthroughService,
		contentModerationService: contentModerationService,
		userModerationService:    userModerationService,
		noAccountBackoffLimiter:  noAccountBackoffLimiter,
		concurrencyHelper:        NewConcurrencyHelper(concurrencyService, SSEPingFormatComment, pingInterval),
		maxAccountSwitches:       maxAccountSwitches,
		cfg:                      cfg,
	}
}

// noAccountBackoffThrottledMessage 命中"无可用账号"退避时的 429 提示。
const noAccountBackoffThrottledMessage = "No available accounts for this group; requests are temporarily throttled, please retry later"

// gatewayCheckNoAccountBackoff 入口硬闸：(user, group) 处于"无可用账号"退避期时补
// Retry-After 并经 writeErr 写 429，返回 true 表示已拦截。必须在读 body/开流之前调用，
// 此时 writeErr 直接写 JSON 即可。cfg 未启用或 limiter 未装配时直接放行。
func gatewayCheckNoAccountBackoff(
	c *gin.Context,
	limiter service.NoAccountBackoffLimiter,
	cfg *config.Config,
	userID int64,
	groupID *int64,
	writeErr func(c *gin.Context, status int, errType, message string),
) bool {
	if limiter == nil || cfg == nil || !cfg.RateLimit.NoAccountBackoff.Enabled {
		return false
	}
	blocked, retryAfter := limiter.CheckBlocked(c.Request.Context(), userID, groupID)
	if !blocked {
		return false
	}
	if retryAfter <= 0 {
		retryAfter = cfg.RateLimit.NoAccountBackoff.RetryAfterHintSeconds
	}
	if retryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
	}
	writeErr(c, http.StatusTooManyRequests, "rate_limit_error", noAccountBackoffThrottledMessage)
	return true
}

// gatewayRecordNoAccountFailure 在"无可用账号"503 出口计一次失败；未开流时给 503 响应
// 附带 Retry-After 提示。必须在写响应体之前调用（header 需先于 body 写出）。
// 仅在本次记录跨过阈值（退避被激活）时打 warn，其余情况静默。
func gatewayRecordNoAccountFailure(
	c *gin.Context,
	log *zap.Logger,
	limiter service.NoAccountBackoffLimiter,
	cfg *config.Config,
	userID int64,
	groupID *int64,
	streamStarted bool,
) {
	if limiter == nil || cfg == nil || !cfg.RateLimit.NoAccountBackoff.Enabled {
		return
	}
	backoffCfg := cfg.RateLimit.NoAccountBackoff
	if !streamStarted && backoffCfg.RetryAfterHintSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(backoffCfg.RetryAfterHintSeconds))
	}
	blocked, retryAfter := limiter.RecordFailure(c.Request.Context(), userID, groupID)
	if !blocked {
		return
	}
	log.Warn("gateway.no_account_backoff_armed",
		zap.Int64("user_id", userID),
		zap.Int64p("group_id", groupID),
		zap.Int("count", backoffCfg.Threshold),
		zap.Int("backoff_seconds", retryAfter),
	)
}

// checkNoAccountBackoff 入口硬闸（OpenAI 侧），命中时已写响应，调用方直接 return。
func (h *OpenAIGatewayHandler) checkNoAccountBackoff(c *gin.Context, userID int64, groupID *int64, writeErr func(c *gin.Context, status int, errType, message string)) bool {
	return gatewayCheckNoAccountBackoff(c, h.noAccountBackoffLimiter, h.cfg, userID, groupID, writeErr)
}

// recordNoAccountFailure 记录一次"无可用账号"失败（OpenAI 侧），需在写 503 响应前调用。
func (h *OpenAIGatewayHandler) recordNoAccountFailure(c *gin.Context, log *zap.Logger, userID int64, groupID *int64, streamStarted bool) {
	gatewayRecordNoAccountFailure(c, log, h.noAccountBackoffLimiter, h.cfg, userID, groupID, streamStarted)
}

// Responses handles OpenAI Responses API endpoint
// POST /openai/v1/responses
func (h *OpenAIGatewayHandler) Responses(c *gin.Context) {
	// 局部兜底：确保该 handler 内部任何 panic 都不会击穿到进程级。
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	compactStartedAt := time.Now()
	defer h.logOpenAIRemoteCompactOutcome(c, compactStartedAt)
	setOpenAIClientTransportHTTP(c)

	requestStart := time.Now()

	// Get apiKey and user from context (set by ApiKeyAuth middleware)
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
		"handler.openai_gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if h.checkNoAccountBackoff(c, subject.UserID, apiKey.GroupID, h.errorResponse) {
		return
	}
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	// Read request body
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

	setOpsRequestContext(c, "", false, body)
	sessionHashBody := body
	body, ok = h.normalizeOpenAIResponsesCompactRequest(c, reqLog, body)
	if !ok {
		return
	}
	stopCompactKeepalive := service.StartOpenAICompactSSEKeepalive(c, h.openAICompactKeepaliveInterval())
	defer stopCompactKeepalive()

	analysis, err := service.AnalyzeOpenAIResponsesRequest(body)
	if err != nil {
		if errors.Is(err, service.ErrOpenAIResponsesInvalidStreamFieldType) {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
			return
		}
		if errors.Is(err, service.ErrOpenAIResponsesInvalidModelFieldType) {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	if !analysis.ModelExists || analysis.Model == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := analysis.Model
	imageIntent := service.IsExplicitImageGenerationIntent("/v1/responses", reqModel, body)
	if imageIntent && !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}

	reqStream := analysis.Stream
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))
	previousResponseID := analysis.PreviousResponseID
	if previousResponseID != "" {
		previousResponseIDKind := service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
		reqLog = reqLog.With(
			zap.Bool("has_previous_response_id", true),
			zap.String("previous_response_id_kind", previousResponseIDKind),
			zap.Int("previous_response_id_len", len(previousResponseID)),
		)
		if previousResponseIDKind == service.OpenAIPreviousResponseIDKindMessageID {
			reqLog.Warn("openai.request_validation_failed",
				zap.String("reason", "previous_response_id_looks_like_message_id"),
			)
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id must be a response.id (resp_*), not a message id")
			return
		}
		reqLog.Warn("openai.request_validation_failed",
			zap.String("reason", "previous_response_id_requires_wsv2"),
		)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id is only supported on Responses WebSocket v2")
		return
	}

	setOpsRequestContext(c, reqModel, reqStream, nil)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 解析渠道级模型映射
	// 提前校验 function_call_output 是否具备可关联上下文，避免上游 400。
	if !h.validateFunctionCallOutputAnalysis(c, analysis, reqLog) {
		return
	}

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	// Get subscription info (may be nil)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()
	routingCtx := service.WithOpenAIFirstOutputStart(c.Request.Context(), routingStart)
	if reqStream {
		routingCtx = service.WithOpenAIFirstOutputBudget(routingCtx, h.gatewayService.OpenAIFirstOutputRoutingBudget(body, reqModel))
	}
	c.Request = c.Request.WithContext(routingCtx)

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	// 确保请求取消时也会释放槽位，避免长连接被动中断造成泄漏
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// Generate session hash (header first; fallback to prompt_cache_key)
	sessionAnalysis := analysis
	if len(sessionHashBody) > 0 && !bytes.Equal(sessionHashBody, body) {
		sessionAnalysis = service.AnalyzeOpenAIResponsesSessionRequest(sessionHashBody)
	}
	sessionHash := h.gatewayService.GenerateSessionHashFromAnalysis(c, sessionAnalysis)
	requireCompact := isOpenAIRemoteCompactPath(c)
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	if _, ok := routeCursor.current(); !ok {
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
		return
	}

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	firstOutputTimeoutSwitchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	type moderationRouteKey struct {
		apiKeyID int64
		groupID  int64
	}
	moderatedRoutes := make(map[moderationRouteKey]struct{})
	moderatedAccounts := make(map[int64]struct{})
	var lastFailoverErr *service.UpstreamFailoverError
	var routeBillingGate apiKeyGroupRouteBillingGate

	for {
		if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
			return
		}
		if !openAIRequestAllowsFailoverReplay(c) {
			return
		}
		routeCandidate, ok := routeCursor.current()
		if !ok {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
			return
		}
		currentAPIKey := routeCandidate.APIKey
		routingPlatform := openAICompatibleRoutingPlatform(currentAPIKey)
		switch h.checkCyberPolicyRouteBlock(c, currentAPIKey, reqModel, cyberBlockFormatResponses, routeCursor, reqLog) {
		case cyberPolicyRouteRejected:
			return
		case cyberPolicyRouteSkipped:
			failedAccountIDs = make(map[int64]struct{})
			sameAccountRetryCount = make(map[int64]int)
			switchCount = 0
			lastFailoverErr = nil
			continue
		}
		selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
		selectionCtx = openAICompatibleRequestContext(selectionCtx, currentAPIKey)
		selectionCtx, cancelSelectionRouting := service.WithOpenAIFirstOutputRoutingDeadline(selectionCtx)
		currentSubscription, subErr := h.gatewayService.ResolveRouteSubscription(selectionCtx, currentAPIKey, subscription)
		if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
			cancelSelectionRouting()
			return
		}
		if subErr != nil {
			cancelSelectionRouting()
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, subErr, "route_subscription_unavailable", reqLog)
			if retry {
				continue
			}
			status, code, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(selectionCtx, currentAPIKey.GroupID, reqModel)
		if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
			cancelSelectionRouting()
			return
		}
		if err := h.billingCacheService.CheckBillingEligibility(selectionCtx, currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
				cancelSelectionRouting()
				return
			}
			cancelSelectionRouting()
			reqLog.Info("openai.billing_eligibility_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, err, "route_billing_ineligible", reqLog)
			if retry {
				continue
			}
			status, code, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}

		// Select account supporting the requested model
		reqLog.Debug("openai.account_selecting",
			zap.Int("excluded_account_count", len(failedAccountIDs)),
			zap.Int64p("group_id", currentAPIKey.GroupID),
		)
		selectionModel := resolveOpenAIAccountSelectionModel(reqModel, channelMapping)
		routeKey := moderationRouteKey{apiKeyID: currentAPIKey.ID, groupID: apiKeyGroupIDValue(currentAPIKey)}
		if _, checked := moderatedRoutes[routeKey]; !checked {
			moderatedRoutes[routeKey] = struct{}{}
			if decision := h.checkCyberPreflightWithSource(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body, analysis); decision != nil && decision.Blocked {
				cancelSelectionRouting()
				h.handleStreamingAwareError(c, contentModerationStatus(decision), cyberPreflightErrorCode(decision), decision.Message, streamStarted)
				return
			}
			if decision := h.checkContentModerationWithSource(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body, analysis); decision != nil && decision.Blocked {
				cancelSelectionRouting()
				h.handleStreamingAwareError(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message, streamStarted)
				return
			}
		}
		if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
			cancelSelectionRouting()
			return
		}
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithCleanRelayScheduler(
			selectionCtx,
			c,
			currentAPIKey.GroupID,
			previousResponseID,
			sessionHash,
			reqModel,
			selectionModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			requireCompact,
			sessionHashBody,
		)
		if err != nil {
			cancelSelectionRouting()
			if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
				return
			}
			if failoverClientGone(c) {
				reqLog.Info("openai.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			if h.handleAccountShareModeSelectionError(c, err, streamStarted) {
				return
			}
			if len(failedAccountIDs) == 0 {
				if !errors.Is(err, service.ErrNoAvailableCompactAccounts) && routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(err)) {
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					continue
				}
				if errors.Is(err, service.ErrNoAvailableCompactAccounts) {
					h.recordNoAccountFailure(c, reqLog, subject.UserID, apiKey.GroupID, streamStarted)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "compact_not_supported", "No available accounts support /responses/compact", streamStarted)
					return
				}
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, selectionModel, reqModel, routingPlatform)
				if cls.Status == http.StatusServiceUnavailable {
					h.recordNoAccountFailure(c, reqLog, subject.UserID, apiKey.GroupID, streamStarted)
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return
			}
			if lastFailoverErr != nil {
				if !streamStarted && shouldSwitchAPIKeyGroupRoute(lastFailoverErr) &&
					routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", lastFailoverErr.StatusCode)) {
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					continue
				}
				h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
			} else {
				h.handleFailoverExhaustedSimple(c, 502, streamStarted)
			}
			return
		}
		if selection == nil || selection.Account == nil {
			cancelSelectionRouting()
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, selectionModel, reqModel, routingPlatform)
			if cls.Status == http.StatusServiceUnavailable {
				h.recordNoAccountFailure(c, reqLog, subject.UserID, apiKey.GroupID, streamStarted)
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		if previousResponseID != "" && selection != nil && selection.Account != nil {
			reqLog.Debug("openai.account_selected_with_previous_response_id", zap.Int64("account_id", selection.Account.ID))
		}
		reqLog.Debug("openai.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_previous_hit", scheduleDecision.StickyPreviousHit),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
			zap.Int("top_k", scheduleDecision.TopK),
			zap.Int64("latency_ms", scheduleDecision.LatencyMs),
			zap.Float64("load_skew", scheduleDecision.LoadSkew),
		)
		account := selection.Account
		if reqStream {
			accountBudget := h.gatewayService.OpenAIFirstOutputBudgetForAccount(account, body, reqModel, selectionModel)
			c.Request = c.Request.WithContext(service.WithOpenAIFirstOutputBudget(c.Request.Context(), accountBudget))
			selectionCtx = service.WithOpenAIFirstOutputBudget(selectionCtx, accountBudget)
		}
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai.account_selected",
			zap.Int64("account_id", account.ID),
			zap.String("account_name", account.Name),
			zap.Int64p("group_id", currentAPIKey.GroupID),
		)
		setOpsSelectedAccount(c, account.ID, account.Platform)
		if _, checked := moderatedAccounts[account.ID]; !checked {
			moderatedAccounts[account.ID] = struct{}{}
			if decision := h.checkUserContentModerationWithSource(selectionCtx, c, reqLog, currentAPIKey, subject, account, service.ContentModerationProtocolOpenAIResponses, reqModel, body, analysis); decision != nil && decision.Blocked {
				cancelSelectionRouting()
				if selection.Acquired && selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
				return
			}
		}
		dispatchCtx := openAIResponsesDispatchContext(c, selectionCtx, currentAPIKey)
		cancelSelectionRouting()
		if reqStream && h.abortIfOpenAIFirstOutputBudgetExpired(c, streamStarted) {
			if selection.Acquired && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			return
		}

		freshAccount, accountReleaseFunc, acquired, retryRoute := h.acquireResponsesAccountSlot(c, dispatchCtx, currentAPIKey.GroupID, sessionHash, service.OpenAIAccountDispatchRequirements{
			RequestedModel:    selectionModel,
			RequiredTransport: service.OpenAIUpstreamTransportAny,
			RequireCompact:    requireCompact,
		}, selection, reqStream, &streamStarted, routeCursor, reqLog)
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

		// Forward request
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		writerSizeBeforeForward := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
		// 应用渠道模型映射到请求体
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		forwardAnalysis := analysis
		if channelMapping.Mapped {
			forwardAnalysis = analysis.WithBodyAndModel(forwardBody, channelMapping.MappedModel)
		}
		forwardCtx, cancelForward := bindAccountSelectionForwardContext(dispatchCtx, selection)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetSecurityClientIP(c)
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
				ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
			})
		}
		upstreamAttemptID := h.beginOpenAIUpstreamAttempt(c, currentAPIKey, account)
		result, err := h.gatewayService.ForwardWithAnalysis(forwardCtx, c, account, forwardBody, forwardAnalysis)
		cancelForward()
		cyberPolicyHit, _ := h.recordCyberPolicyHitForAttempt(dispatchCtx, c, currentAPIKey, upstreamAttemptID)
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		recordUsageResult := func(result *service.OpenAIForwardResult) {
			if result == nil {
				return
			}
			h.submitUsageRecordTask(forwardCtx, func(ctx context.Context) {
				usageCtx := service.WithAccountShareModeRequestFromContext(ctx, forwardCtx)
				if err := recordUsage(usageCtx, result); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.responses"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", currentAPIKey.ID),
						zap.Any("group_id", currentAPIKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("openai.record_usage_failed", zap.Error(err))
				}
			})
		}
		hasBillableUsage := service.OpenAIForwardResultHasBillableUsage(result)
		finalizeAccountShareRequest(hasBillableUsage, func() { recordUsageResult(result) }, accountReleaseFunc)
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if cyberPolicyHit {
			if err != nil && !openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err) {
				h.ensureForwardErrorResponse(c, streamStarted)
			}
			reqLog.Warn("openai.cyber_policy_terminal",
				zap.Int64("user_id", currentAPIKey.UserID),
				zap.Int64("api_key_id", currentAPIKey.ID),
				zap.Int64("effective_group_id", apiKeyGroupIDValue(currentAPIKey)),
				zap.String("upstream_attempt_id", upstreamAttemptID),
			)
			return
		}
		if err != nil {
			err = h.gatewayService.NormalizeGrokCredentialFailure(c.Request.Context(), c, account, err)
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if failoverClientGone(c) {
					reqLog.Info("openai.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if !openAIForwardMayFailover(c, writerSizeBeforeForward, failoverErr) {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				if failoverErr.SafeToFailoverAfterWrite && c.Writer.Written() {
					streamStarted = true
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				if openAIFirstOutputFailoverExhausted(failoverErr, &firstOutputTimeoutSwitchCount) {
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				// 池模式：同账号重试
				if failoverErr.RetryableOnSameAccount {
					retryLimit := account.GetPoolModeRetryCount()
					if sameAccountRetryCount[account.ID] < retryLimit {
						sameAccountRetryCount[account.ID]++
						reqLog.Warn("openai.pool_mode_same_account_retry",
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
					if canSwitchOpenAIResponsesRouteAfterForward(c, routeCursor, failoverErr, streamStarted, writerSizeBeforeForward) &&
						routeCursor.switchToNext(apiKey.ID, "upstream_failover_exhausted", reqLog, zap.Int("upstream_status", failoverErr.StatusCode)) {
						failedAccountIDs = make(map[int64]struct{})
						sameAccountRetryCount = make(map[int64]int)
						switchCount = 0
						lastFailoverErr = nil
						continue
					}
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				switchCount++
				failoverSwitchFields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				}
				failoverSwitchFields = appendOpenAIProxyLogFields(failoverSwitchFields, account)
				reqLog.Warn("openai.upstream_failover_switching", failoverSwitchFields...)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
			upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
			}
			fields := []zap.Field{
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			}
			if shouldLogOpenAIForwardFailureAsWarn(c, wroteFallback) {
				reqLog.Warn("openai.forward_failed", fields...)
				return
			}
			reqLog.Error("openai.forward_failed", fields...)
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

		reqLog.Debug("openai.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func isOpenAIRemoteCompactPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	return strings.HasSuffix(normalizedPath, "/responses/compact")
}

func canSwitchOpenAIResponsesRouteAfterForward(c *gin.Context, cursor *apiKeyGroupRouteCursor, failoverErr *service.UpstreamFailoverError, streamStarted bool, writerSizeBeforeForward int) bool {
	if cursor == nil || !cursor.hasNext() || !shouldSwitchAPIKeyGroupRoute(failoverErr) || streamStarted {
		return false
	}
	return service.OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward
}

func isBareOpenAIResponsesPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	return strings.HasSuffix(normalizedPath, "/responses")
}

func isOpenAIRemoteCompactionV2Request(c *gin.Context, body []byte) bool {
	stream, valid := parseOpenAICompatibleStream(body)
	if !valid || !stream || c == nil || c.Request == nil {
		return false
	}
	for _, header := range c.Request.Header.Values("x-codex-beta-features") {
		for _, feature := range strings.Split(header, ",") {
			if strings.TrimSpace(feature) == "remote_compaction_v2" {
				return true
			}
		}
	}
	return false
}

func (h *OpenAIGatewayHandler) normalizeOpenAIResponsesCompactRequest(c *gin.Context, reqLog *zap.Logger, body []byte) ([]byte, bool) {
	isCompactRequest := service.IsOpenAIResponsesCompactPathForTest(c)
	if !isCompactRequest && isBareOpenAIResponsesPath(c) && service.HasCompactionTriggerInInput(body) {
		if isOpenAIRemoteCompactionV2Request(c, body) {
			return body, true
		}
		c.Request.URL.Path = strings.TrimRight(c.Request.URL.Path, "/") + "/compact"
		isCompactRequest = true
		clientStream := gjson.GetBytes(body, "stream").Bool()
		if clientStream {
			service.MarkOpenAICompactClientStream(c)
		}
		reqLog.Info("codex.remote_compact.detected_body_signal", zap.Bool("client_stream", clientStream))
	}
	if !isCompactRequest {
		return body, true
	}
	if compactSeed := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); compactSeed != "" {
		c.Set(service.OpenAICompactSessionSeedKeyForTest(), compactSeed)
	}
	normalizedBody, normalized, err := service.NormalizeOpenAICompactRequestBodyForTest(body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to normalize compact request body")
		return nil, false
	}
	if normalized {
		body = normalizedBody
	}
	return body, true
}

func (h *OpenAIGatewayHandler) logOpenAIRemoteCompactOutcome(c *gin.Context, startedAt time.Time) {
	if !isOpenAIRemoteCompactPath(c) {
		return
	}

	var (
		ctx    = context.Background()
		path   string
		status int
	)
	if c != nil {
		if c.Request != nil {
			ctx = c.Request.Context()
			if c.Request.URL != nil {
				path = strings.TrimSpace(c.Request.URL.Path)
			}
		}
		if c.Writer != nil {
			status = c.Writer.Status()
		}
	}

	outcome := "failed"
	if status >= 200 && status < 300 {
		outcome = "succeeded"
	}
	if outcome == "succeeded" && c != nil {
		if _, hasStreamErr := service.GetOpsStreamError(c); hasStreamErr {
			outcome = "failed"
		}
	}
	latencyMs := time.Since(startedAt).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}

	fields := []zap.Field{
		zap.String("component", "handler.openai_gateway.responses"),
		zap.Bool("remote_compact", true),
		zap.String("compact_outcome", outcome),
		zap.Int("status_code", status),
		zap.Int64("latency_ms", latencyMs),
		zap.String("path", path),
		zap.Bool("force_codex_cli", h != nil && h.cfg != nil && h.cfg.Gateway.ForceCodexCLI),
	}

	if c != nil {
		if userAgent := strings.TrimSpace(c.GetHeader("User-Agent")); userAgent != "" {
			fields = append(fields, zap.String("request_user_agent", userAgent))
		}
		if v, ok := c.Get(opsModelKey); ok {
			if model, ok := v.(string); ok && strings.TrimSpace(model) != "" {
				fields = append(fields, zap.String("request_model", strings.TrimSpace(model)))
			}
		}
		if v, ok := c.Get(opsAccountIDKey); ok {
			if accountID, ok := v.(int64); ok && accountID > 0 {
				fields = append(fields, zap.Int64("account_id", accountID))
			}
		}
		if c.Writer != nil {
			if upstreamRequestID := strings.TrimSpace(c.Writer.Header().Get("x-request-id")); upstreamRequestID != "" {
				fields = append(fields, zap.String("upstream_request_id", upstreamRequestID))
			} else if upstreamRequestID := strings.TrimSpace(c.Writer.Header().Get("X-Request-Id")); upstreamRequestID != "" {
				fields = append(fields, zap.String("upstream_request_id", upstreamRequestID))
			}
		}
	}

	log := logger.FromContext(ctx).With(fields...)
	if outcome == "succeeded" {
		log.Info("codex.remote_compact.succeeded")
		return
	}
	log.Warn("codex.remote_compact.failed")
}

// Messages handles Anthropic Messages API requests routed to OpenAI platform.
// POST /v1/messages (when group platform is OpenAI)
func (h *OpenAIGatewayHandler) Messages(c *gin.Context) {
	streamStarted := false
	defer h.recoverAnthropicMessagesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.messages",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if h.checkNoAccountBackoff(c, subject.UserID, apiKey.GroupID, h.anthropicErrorResponse) {
		return
	}

	// 检查分组是否允许 /v1/messages 调度
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if !gjson.ValidBytes(body) {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	routingModel := service.NormalizeOpenAICompatRequestedModel(reqModel)
	reqStream := gjson.GetBytes(body, "stream").Bool()

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream, body)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 解析渠道级模型映射
	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()
	c.Request = c.Request.WithContext(service.WithOpenAIFirstOutputStart(c.Request.Context(), routingStart))

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	sessionHash, promptCacheKey := h.gatewayService.GenerateOpenAIMessagesSessionIdentity(c, body, reqModel)
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	if _, ok := routeCursor.current(); !ok {
		h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
		return
	}

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var routeBillingGate apiKeyGroupRouteBillingGate

	for {
		if failoverClientGone(c) {
			return
		}
		routeCandidate, ok := routeCursor.current()
		if !ok {
			h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
			return
		}
		currentAPIKey := routeCandidate.APIKey
		routingPlatform := openAICompatibleRoutingPlatform(currentAPIKey)
		switch h.checkCyberPolicyRouteBlock(c, currentAPIKey, reqModel, cyberBlockFormatAnthropic, routeCursor, reqLog) {
		case cyberPolicyRouteRejected:
			return
		case cyberPolicyRouteSkipped:
			failedAccountIDs = make(map[int64]struct{})
			sameAccountRetryCount = make(map[int64]int)
			switchCount = 0
			lastFailoverErr = nil
			continue
		}
		if currentAPIKey.Group != nil && !currentAPIKey.Group.AllowMessagesDispatch {
			if routeCursor.skipToNext("messages_dispatch_not_allowed", reqLog, zap.Int64p("group_id", currentAPIKey.GroupID)) {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue
			}
			h.anthropicErrorResponse(c, http.StatusForbidden, "permission_error",
				"This group does not allow /v1/messages dispatch")
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
			h.anthropicStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		channelMappingMsg, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, reqModel)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("openai_messages.billing_eligibility_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
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
			h.anthropicStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		effectiveMappedModel := resolveOpenAIMessagesDispatchMappedModel(currentAPIKey, reqModel)
		currentRoutingModel := routingModel
		if effectiveMappedModel != "" {
			currentRoutingModel = effectiveMappedModel
		}
		currentRoutingModel = resolveOpenAIAccountSelectionModel(currentRoutingModel, channelMappingMsg)
		reqLog.Debug("openai_messages.account_selecting",
			zap.Int("excluded_account_count", len(failedAccountIDs)),
			zap.Int64p("group_id", currentAPIKey.GroupID),
		)
		selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
		selectionCtx = openAICompatibleRequestContext(selectionCtx, currentAPIKey)
		if decision := h.checkCyberPreflightWithContext(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolAnthropicMessages, reqModel, body); decision != nil && decision.Blocked {
			h.anthropicStreamingAwareError(c, contentModerationStatus(decision), "permission_error", decision.Message, streamStarted)
			return
		}
		if decision := h.checkContentModerationWithContext(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolAnthropicMessages, reqModel, body); decision != nil && decision.Blocked {
			h.anthropicStreamingAwareError(c, contentModerationStatus(decision), "permission_error", decision.Message, streamStarted)
			return
		}
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithCleanRelayScheduler(
			selectionCtx,
			c,
			currentAPIKey.GroupID,
			"", // no previous_response_id
			sessionHash,
			reqModel,
			currentRoutingModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			false,
			body,
		)
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai_messages.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai_messages.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			if h.handleAccountShareModeAnthropicError(c, err, streamStarted) {
				return
			}
			if len(failedAccountIDs) == 0 {
				if err != nil {
					if routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(err)) {
						failedAccountIDs = make(map[int64]struct{})
						sameAccountRetryCount = make(map[int64]int)
						switchCount = 0
						lastFailoverErr = nil
						continue
					}
					cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, currentRoutingModel, reqModel, routingPlatform)
					if cls.Status == http.StatusServiceUnavailable {
						h.recordNoAccountFailure(c, reqLog, subject.UserID, apiKey.GroupID, streamStarted)
					}
					h.anthropicStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
					return
				}
			} else {
				if lastFailoverErr != nil {
					if !streamStarted && shouldSwitchAPIKeyGroupRoute(lastFailoverErr) &&
						routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", lastFailoverErr.StatusCode)) {
						failedAccountIDs = make(map[int64]struct{})
						sameAccountRetryCount = make(map[int64]int)
						switchCount = 0
						lastFailoverErr = nil
						continue
					}
					h.handleAnthropicFailoverExhausted(c, lastFailoverErr, streamStarted)
				} else {
					h.anthropicStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
				}
				return
			}
		}
		if selection == nil || selection.Account == nil {
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, currentRoutingModel, reqModel, routingPlatform)
			if cls.Status == http.StatusServiceUnavailable {
				h.recordNoAccountFailure(c, reqLog, subject.UserID, apiKey.GroupID, streamStarted)
			}
			h.anthropicStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_messages.account_selected",
			zap.Int64("account_id", account.ID),
			zap.String("account_name", account.Name),
			zap.Int64p("group_id", currentAPIKey.GroupID),
		)
		_ = scheduleDecision
		setOpsSelectedAccount(c, account.ID, account.Platform)
		if decision := h.checkUserContentModerationWithContent(selectionCtx, c, reqLog, currentAPIKey, subject, account, service.ContentModerationProtocolAnthropicMessages, reqModel, body, nil); decision != nil && decision.Blocked {
			if selection.Acquired && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			h.anthropicStreamingAwareError(c, contentModerationStatus(decision), "permission_error", decision.Message, streamStarted)
			return
		}

		freshAccount, accountReleaseFunc, acquired, retryRoute := h.acquireResponsesAccountSlot(c, selectionCtx, currentAPIKey.GroupID, sessionHash, service.OpenAIAccountDispatchRequirements{
			RequestedModel:    currentRoutingModel,
			RequiredTransport: service.OpenAIUpstreamTransportAny,
		}, selection, reqStream, &streamStarted, routeCursor, reqLog)
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
		forwardStart := time.Now()
		writerSizeBeforeForward := c.Writer.Size()

		defaultMappedModel := strings.TrimSpace(effectiveMappedModel)
		// 应用渠道模型映射到请求体
		forwardBody := body
		if channelMappingMsg.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMappingMsg.MappedModel)
		}
		forwardCtx, cancelForward := bindAccountSelectionForwardContext(selectionCtx, selection)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetSecurityClientIP(c)
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
				ChannelUsageFields: channelMappingMsg.ToUsageFields(reqModel, result.UpstreamModel),
			})
		}
		upstreamAttemptID := h.beginOpenAIUpstreamAttempt(c, currentAPIKey, account)
		result, err := h.gatewayService.ForwardAsAnthropic(forwardCtx, c, account, forwardBody, promptCacheKey, defaultMappedModel)
		cancelForward()
		cyberPolicyHit, _ := h.recordCyberPolicyHitForAttempt(c.Request.Context(), c, currentAPIKey, upstreamAttemptID)

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		recordUsageResult := func(result *service.OpenAIForwardResult) {
			if result == nil {
				return
			}
			h.submitUsageRecordTask(forwardCtx, func(ctx context.Context) {
				usageCtx := service.WithAccountShareModeRequestFromContext(ctx, forwardCtx)
				if err := recordUsage(usageCtx, result); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.messages"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", currentAPIKey.ID),
						zap.Any("group_id", currentAPIKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("openai_messages.record_usage_failed", zap.Error(err))
				}
			})
		}
		hasBillableUsage := service.OpenAIForwardResultHasBillableUsage(result)
		finalizeAccountShareRequest(hasBillableUsage, func() { recordUsageResult(result) }, accountReleaseFunc)
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if cyberPolicyHit {
			if err != nil {
				h.ensureAnthropicErrorResponse(c, streamStarted)
			}
			reqLog.Warn("openai_messages.cyber_policy_terminal",
				zap.Int64("user_id", currentAPIKey.UserID),
				zap.Int64("api_key_id", currentAPIKey.ID),
				zap.Int64("effective_group_id", apiKeyGroupIDValue(currentAPIKey)),
				zap.String("upstream_attempt_id", upstreamAttemptID),
			)
			return
		}
		if err != nil {
			err = h.gatewayService.NormalizeGrokCredentialFailure(c.Request.Context(), c, account, err)
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if failoverClientGone(c) {
					reqLog.Info("openai_messages.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				// 池模式：同账号重试
				if failoverErr.RetryableOnSameAccount {
					retryLimit := account.GetPoolModeRetryCount()
					if sameAccountRetryCount[account.ID] < retryLimit {
						sameAccountRetryCount[account.ID]++
						reqLog.Warn("openai_messages.pool_mode_same_account_retry",
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
						failedAccountIDs = make(map[int64]struct{})
						sameAccountRetryCount = make(map[int64]int)
						switchCount = 0
						lastFailoverErr = nil
						continue
					}
					h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				switchCount++
				failoverSwitchFields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				}
				failoverSwitchFields = appendOpenAIProxyLogFields(failoverSwitchFields, account)
				reqLog.Warn("openai_messages.upstream_failover_switching", failoverSwitchFields...)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
			wroteFallback := h.ensureAnthropicErrorResponse(c, streamStarted)
			reqLog.Warn("openai_messages.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Error(err),
			)
			return
		}
		if result != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, result.FirstTokenMs, account.GetMappedModel(currentRoutingModel))
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil, account.GetMappedModel(currentRoutingModel))
		}
		routeCursor.recordSuccess(apiKey.ID)

		reqLog.Debug("openai_messages.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

// anthropicErrorResponse writes an error in Anthropic Messages API format.
func (h *OpenAIGatewayHandler) anthropicErrorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// anthropicStreamingAwareError handles errors that may occur during streaming,
// using Anthropic SSE error format.
func (h *OpenAIGatewayHandler) anthropicStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if streamStarted {
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			errPayload, _ := json.Marshal(gin.H{
				"type": "error",
				"error": gin.H{
					"type":    errType,
					"message": message,
				},
			})
			fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errPayload) //nolint:errcheck
			flusher.Flush()
		}
		return
	}
	h.anthropicErrorResponse(c, status, errType, message)
}

// handleAnthropicFailoverExhausted maps upstream failover errors to Anthropic format.
func (h *OpenAIGatewayHandler) handleAnthropicFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, streamStarted bool) {
	if failoverErr != nil && failoverErr.IsCredentialFailure() {
		status := failoverErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		message := strings.TrimSpace(failoverErr.ClientMessage)
		if message == "" {
			message = service.GrokCredentialUnavailableClientMessage
		}
		h.anthropicStreamingAwareError(c, status, "api_error", message, streamStarted)
		return
	}
	status, errType, errMsg := h.mapUpstreamError(failoverErr.StatusCode)
	h.anthropicStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

// ensureAnthropicErrorResponse writes a fallback Anthropic error if no response was written.
func (h *OpenAIGatewayHandler) ensureAnthropicErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return false
	}
	h.anthropicStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
	return true
}

func (h *OpenAIGatewayHandler) validateFunctionCallOutputRequest(c *gin.Context, body []byte, reqLog *zap.Logger) bool {
	if !gjson.GetBytes(body, `input.#(type=="function_call_output")`).Exists() {
		return true
	}

	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		// 保持原有容错语义：解析失败时跳过预校验，沿用后续上游校验结果。
		return true
	}

	validation := service.ValidateFunctionCallOutputContext(reqBody)
	if !validation.HasFunctionCallOutput {
		return true
	}

	previousResponseID, _ := reqBody["previous_response_id"].(string)
	if strings.TrimSpace(previousResponseID) != "" || validation.HasToolCallContext {
		return true
	}

	if validation.HasFunctionCallOutputMissingCallID {
		reqLog.Warn("openai.request_validation_failed",
			zap.String("reason", "function_call_output_missing_call_id"),
		)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2")
		return false
	}
	if validation.HasItemReferenceForAllCallIDs {
		return true
	}

	reqLog.Warn("openai.request_validation_failed",
		zap.String("reason", "function_call_output_missing_item_reference"),
	)
	h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires item_reference ids matching each call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2")
	return false
}

func (h *OpenAIGatewayHandler) validateFunctionCallOutputAnalysis(c *gin.Context, analysis *service.OpenAIResponsesRequestAnalysis, reqLog *zap.Logger) bool {
	if analysis == nil || !analysis.FunctionCallOutputValidation.HasFunctionCallOutput {
		return true
	}

	validation := analysis.FunctionCallOutputValidation
	if analysis.PreviousResponseID != "" || validation.HasToolCallContext {
		return true
	}

	if validation.HasFunctionCallOutputMissingCallID {
		reqLog.Warn("openai.request_validation_failed",
			zap.String("reason", "function_call_output_missing_call_id"),
		)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2")
		return false
	}
	if validation.HasItemReferenceForAllCallIDs {
		return true
	}

	reqLog.Warn("openai.request_validation_failed",
		zap.String("reason", "function_call_output_missing_item_reference"),
	)
	h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires item_reference ids matching each call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2")
	return false
}

func (h *OpenAIGatewayHandler) acquireResponsesUserSlot(
	c *gin.Context,
	userID int64,
	userConcurrency int,
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (func(), bool) {
	ctx := c.Request.Context()
	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, userID, userConcurrency, reqStream, streamStarted)
	if err != nil {
		reqLog.Warn("openai.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", *streamStarted)
		return nil, false
	}
	return wrapReleaseOnDone(ctx, userReleaseFunc), true
}

// acquireResponsesAccountSlot 取账号并发槽位。
//
// 返回 retryRoute=true 表示「当前分组吃不下这次请求，但多分组路由里还有下一条」——
// 此时不会向客户端写任何响应，调用方应 continue 到下一条路由重试。分组被并发打满
// 时直接回 429/503、连备用分组都不试，正是多分组路由「配了不生效」的主要原因之一。
//
// routeCursor 允许为 nil（尚未接入路由的调用方），此时退化为原有的就地写错误。
func (h *OpenAIGatewayHandler) acquireResponsesAccountSlot(
	c *gin.Context,
	selectionCtx context.Context,
	groupID *int64,
	sessionHash string,
	fallbackRequirements service.OpenAIAccountDispatchRequirements,
	selection *service.AccountSelectionResult,
	reqStream bool,
	streamStarted *bool,
	routeCursor *apiKeyGroupRouteCursor,
	reqLog *zap.Logger,
) (acc *service.Account, release func(), acquired bool, retryRoute bool) {
	if selection == nil || selection.Account == nil {
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, false
	}

	ctx := c.Request.Context()
	account := selection.Account

	// capacityUnavailable 统一处理「这个分组当下吃不下这次请求」的终止点。
	//
	// 已经开始向客户端写字节（等槽位期间的 keepalive）之后不能再换路由：换了也只能
	// 把新响应拼在旧字节后面，只好维持原样把错误写完。
	capacityUnavailable := func(reason string, writeErr func()) (*service.Account, func(), bool, bool) {
		if !*streamStarted && routeCursor.skipToNext(reason, reqLog, zap.Int64("account_id", account.ID)) {
			return nil, nil, false, true
		}
		writeErr()
		return nil, nil, false, false
	}
	dispatchRequirements := fallbackRequirements
	if selection.OpenAIDispatchRequirements != nil {
		dispatchRequirements = *selection.OpenAIDispatchRequirements
	}
	dispatchCtx := service.WithAccountShareModeRequestFromContext(ctx, selectionCtx)
	finishAcquired := func(release func()) (*service.Account, func(), bool, bool) {
		latest, err := h.gatewayService.RevalidateSelectedOpenAIAccountForDispatch(
			dispatchCtx,
			groupID,
			account,
			dispatchRequirements,
		)
		if err != nil {
			if release != nil {
				release()
			}
			reqLog.Info("openai.account_selection_invalidated_before_dispatch",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Selected account is no longer available, please retry", *streamStarted)
			return nil, nil, false, false
		}
		selection.Account = latest
		if err := h.gatewayService.BindStickySession(ctx, groupID, sessionHash, latest.ID); err != nil {
			reqLog.Warn("openai.bind_sticky_session_failed", zap.Int64("account_id", latest.ID), zap.Error(err))
		}
		return latest, wrapAccountSelectionReleaseOnDone(ctx, selection, release), true, false
	}
	if selection.Acquired {
		return finishAcquired(selection.ReleaseFunc)
	}
	if selection.WaitPlan == nil {
		return capacityUnavailable("account_slot_no_wait_plan", func() {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		})
	}

	fastReleaseFunc, fastAcquired, err := h.concurrencyHelper.TryAcquireAccountSlot(
		ctx,
		account.ID,
		selection.WaitPlan.MaxConcurrency,
	)
	if err != nil {
		reqLog.Warn("openai.account_slot_quick_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		return capacityUnavailable("account_slot_quick_acquire_failed", func() {
			h.handleConcurrencyError(c, err, "account", *streamStarted)
		})
	}
	if fastAcquired {
		return finishAcquired(fastReleaseFunc)
	}

	canWait, waitErr := h.concurrencyHelper.IncrementAccountWaitCount(ctx, account.ID, selection.WaitPlan.MaxWaiting)
	if waitErr != nil {
		reqLog.Warn("openai.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(waitErr))
	} else if !canWait {
		reqLog.Info("openai.account_wait_queue_full",
			zap.Int64("account_id", account.ID),
			zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
		)
		return capacityUnavailable("account_wait_queue_full", func() {
			h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", *streamStarted)
		})
	}

	accountWaitCounted := waitErr == nil && canWait
	releaseWait := func() {
		if accountWaitCounted {
			h.concurrencyHelper.DecrementAccountWaitCount(ctx, account.ID)
			accountWaitCounted = false
		}
	}
	defer releaseWait()

	accountReleaseFunc, err := h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
		c,
		account.ID,
		selection.WaitPlan.MaxConcurrency,
		selection.WaitPlan.Timeout,
		reqStream,
		streamStarted,
	)
	if err != nil {
		reqLog.Warn("openai.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		return capacityUnavailable("account_slot_acquire_timeout", func() {
			h.handleConcurrencyError(c, err, "account", *streamStarted)
		})
	}

	// Slot acquired: no longer waiting in queue.
	releaseWait()
	return finishAcquired(accountReleaseFunc)
}

// ResponsesWebSocket handles OpenAI Responses API WebSocket ingress endpoint
// GET /openai/v1/responses (Upgrade: websocket)
func (h *OpenAIGatewayHandler) ResponsesWebSocket(c *gin.Context) {
	if !isOpenAIWSUpgradeRequest(c.Request) {
		h.errorResponse(c, http.StatusUpgradeRequired, "invalid_request_error", "WebSocket upgrade required (Upgrade: websocket)")
		return
	}
	setOpenAIClientTransportWS(c)

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
		"handler.openai_gateway.responses_ws",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.Bool("openai_ws_mode", true),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	reqLog.Info("openai.websocket_ingress_started")
	clientIP := ip.GetSecurityClientIP(c)
	userAgent := strings.TrimSpace(c.GetHeader("User-Agent"))
	// 必须在 ingress 租约覆盖请求上下文之前捕获：下面 c.Request 被替换后
	// 就再也拿不到不含租约取消信号的原始生命周期 ctx。
	clientLifecycleCtx := c.Request.Context()
	ctx := clientLifecycleCtx
	maxIngressConnections := 0
	if h.cfg != nil {
		maxIngressConnections = h.cfg.Gateway.OpenAIWS.MaxIngressConnectionsPerAPIKey
	}
	ingressLease, ingressLeaseAcquired, ingressLeaseErr := h.concurrencyHelper.AcquireOpenAIWSIngressLease(ctx, apiKey.ID, maxIngressConnections)
	if ingressLeaseErr != nil {
		reqLog.Error("openai.websocket_ingress_lease_acquire_failed", zap.Error(ingressLeaseErr))
		h.errorResponse(c, http.StatusServiceUnavailable, "service_unavailable", "WebSocket ingress capacity is temporarily unavailable")
		return
	}
	if !ingressLeaseAcquired {
		reqLog.Info("openai.websocket_ingress_capacity_rejected", zap.Int("max_ingress_connections_per_api_key", maxIngressConnections))
		c.Header("Retry-After", "5")
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many open WebSocket connections, please retry later")
		return
	}
	if ingressLease != nil {
		defer ingressLease.Release()
		ctx = ingressLease.Context()
		c.Request = c.Request.WithContext(ctx)
	}

	wsConn, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
		CompressionMode: coderws.CompressionContextTakeover,
	})
	if err != nil {
		reqLog.Warn("openai.websocket_accept_failed",
			zap.Error(err),
			zap.String("client_ip", clientIP),
			zap.String("request_user_agent", userAgent),
			zap.String("upgrade_header", strings.TrimSpace(c.GetHeader("Upgrade"))),
			zap.String("connection_header", strings.TrimSpace(c.GetHeader("Connection"))),
			zap.String("sec_websocket_version", strings.TrimSpace(c.GetHeader("Sec-WebSocket-Version"))),
			zap.Bool("has_sec_websocket_key", strings.TrimSpace(c.GetHeader("Sec-WebSocket-Key")) != ""),
		)
		return
	}
	defer func() {
		_ = wsConn.CloseNow()
	}()
	wsConn.SetReadLimit(16 * 1024 * 1024)
	firstMessageTimeout := 30 * time.Second
	if h.cfg != nil && h.cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds > 0 {
		firstMessageTimeout = time.Duration(h.cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds) * time.Second
	}
	msgType, firstMessage, err := service.ReadOpenAIWSClientMessage(
		ctx,
		wsConn,
		firstMessageTimeout,
		coderws.StatusPolicyViolation,
		"missing first response.create message",
	)
	if err != nil {
		if errors.Is(context.Cause(ctx), service.ErrOpenAIWSIngressLeaseLost) {
			reqLog.Warn("openai.websocket_ingress_lease_lost_before_first_message", zap.Error(err))
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "websocket ingress capacity lease lost; please reconnect")
			return
		}
		closeStatus, closeReason := summarizeWSCloseErrorForLog(err)
		reqLog.Warn("openai.websocket_read_first_message_failed",
			zap.Error(err),
			zap.String("client_ip", clientIP),
			zap.String("close_status", closeStatus),
			zap.String("close_reason", closeReason),
			zap.Duration("read_timeout", firstMessageTimeout),
		)
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "missing first response.create message")
		return
	}
	if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "unsupported websocket message type")
		return
	}
	if !gjson.ValidBytes(firstMessage) {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "invalid JSON payload")
		return
	}

	reqModel := strings.TrimSpace(gjson.GetBytes(firstMessage, "model").String())
	if reqModel == "" {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "model is required in first response.create payload")
		return
	}
	previousResponseID := strings.TrimSpace(gjson.GetBytes(firstMessage, "previous_response_id").String())
	previousResponseIDKind := service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
	if previousResponseID != "" && previousResponseIDKind == service.OpenAIPreviousResponseIDKindMessageID {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "previous_response_id must be a response.id (resp_*), not a message id")
		return
	}
	reqLog = reqLog.With(
		zap.Bool("ws_ingress", true),
		zap.String("model", reqModel),
		zap.Bool("has_previous_response_id", previousResponseID != ""),
		zap.String("previous_response_id_kind", previousResponseIDKind),
	)
	setOpenAIWSOpsTurnRequestContext(c, reqModel, firstMessage)
	setOpsEndpointContext(c, "", int16(service.RequestTypeWSV2))

	// 解析渠道级模型映射

	var currentUserRelease func()
	var currentAccountRelease func()
	releaseTurnSlots := func() {
		if currentAccountRelease != nil {
			currentAccountRelease()
			currentAccountRelease = nil
		}
		if currentUserRelease != nil {
			currentUserRelease()
			currentUserRelease = nil
		}
	}
	// 必须尽早注册，确保任何 early return 都能释放已获取的并发槽位。
	defer releaseTurnSlots()

	userReleaseFunc, userAcquired, err := h.concurrencyHelper.TryAcquireUserSlotForAPIKey(ctx, subject.UserID, subject.Concurrency, apiKey.ID)
	if err != nil {
		reqLog.Warn("openai.websocket_user_slot_acquire_failed", zap.Error(err))
		closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "failed to acquire user concurrency slot")
		return
	}
	if !userAcquired {
		closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "too many concurrent requests, please retry later")
		return
	}
	currentUserRelease = wrapReleaseOnDone(ctx, userReleaseFunc)

	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(
		c,
		firstMessage,
		openAIWSIngressFallbackSessionSeed(subject.UserID, apiKey.ID, apiKey.GroupID),
	)
	if previousResponseID != "" && sessionHash != "" {
		repairedFirstMessage, repairedPreviousResponseID, repaired := h.gatewayService.RepairOpenAIWSPreviousResponseIDForSession(
			ctx,
			apiKeyGroupIDValue(apiKey),
			sessionHash,
			firstMessage,
			true,
		)
		if repaired {
			firstMessage = repairedFirstMessage
			previousResponseID = repairedPreviousResponseID
			previousResponseIDKind = service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
			reqLog = reqLog.With(
				zap.String("previous_response_id_kind", previousResponseIDKind),
				zap.Bool("previous_response_id_repaired", true),
			)
			setOpenAIWSOpsTurnRequestContext(c, reqModel, firstMessage)
		}
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	var currentAPIKey *service.APIKey
	var currentSubscription *service.UserSubscription
	var channelMappingWS service.ChannelMappingResult
	var selection *service.AccountSelectionResult
	var scheduleDecision service.OpenAIAccountScheduleDecision
	var selectedAccountShareCtx context.Context
	var selectedRoutingModel string
	var routeBillingGate apiKeyGroupRouteBillingGate
	for {
		if failoverClientGone(c) {
			return
		}
		routeCandidate, ok := routeCursor.current()
		if !ok {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "no available API key group routes")
			return
		}
		currentAPIKey = routeCandidate.APIKey
		effectiveGroupID := apiKeyGroupIDValue(currentAPIKey)
		setOpsEffectiveRoute(c, currentAPIKey, nil)
		blockState := h.gatewayService.CheckCyberPolicyBlock(ctx, currentAPIKey.UserID, effectiveGroupID)
		if blockState.Blocked {
			if routeCursor.skipToNext(
				"cyber_policy_route_blocked",
				reqLog,
				zap.Int64("user_id", currentAPIKey.UserID),
				zap.Int64("api_key_id", currentAPIKey.ID),
				zap.Int64("effective_group_id", effectiveGroupID),
				zap.String("block_scope", string(blockState.Scope)),
			) {
				continue
			}
			writeCyberPolicyBlockedWSError(ctx, wsConn)
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "request scope isolated by cyber-security policy")
			return
		}
		var subErr error
		currentSubscription, subErr = h.gatewayService.ResolveRouteSubscription(ctx, currentAPIKey, subscription)
		if subErr != nil {
			reqLog.Info("openai.websocket_subscription_resolve_failed",
				zap.Error(subErr),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			if retry, _ := routeBillingGate.skipOrTerminate(routeCursor, subErr, "route_subscription_unavailable", reqLog); retry {
				continue
			}
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "subscription required")
			return
		}
		channelMappingWS, _ = h.gatewayService.ResolveChannelMappingAndRestrict(ctx, currentAPIKey.GroupID, reqModel)
		if err := h.billingCacheService.CheckBillingEligibility(ctx, currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("openai.websocket_billing_eligibility_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			if retry, _ := routeBillingGate.skipOrTerminate(routeCursor, err, "route_billing_ineligible", reqLog); retry {
				continue
			}
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "billing check failed")
			return
		}
		var selectErr error
		selectionModel := resolveOpenAIAccountSelectionModel(reqModel, channelMappingWS)
		selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
		if decision := h.checkCyberPreflightWithContext(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, firstMessage); decision != nil && decision.Blocked {
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, decision.Message)
			return
		}
		if decision := h.checkContentModerationWithContext(selectionCtx, c, reqLog, currentAPIKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, firstMessage); decision != nil && decision.Blocked {
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, decision.Message)
			return
		}
		selection, scheduleDecision, selectErr = h.gatewayService.SelectAccountWithCleanRelayScheduler(
			selectionCtx,
			c,
			currentAPIKey.GroupID,
			previousResponseID,
			sessionHash,
			reqModel,
			selectionModel,
			nil,
			service.OpenAIUpstreamTransportResponsesWebsocketV2,
			false,
			firstMessage,
		)
		if selectErr == nil && selection != nil && selection.Account != nil {
			selectedAccountShareCtx = selectionCtx
			selectedRoutingModel = selectionModel
			break
		}
		if failoverClientGone(c) {
			reqLog.Info("openai.websocket_account_select_aborted_client_disconnected", zap.Error(selectErr))
			return
		}
		reqLog.Warn("openai.websocket_account_select_failed",
			zap.Error(selectErr),
			zap.Int64p("group_id", currentAPIKey.GroupID),
		)
		if errors.Is(selectErr, service.ErrAccountShareModeGroupUnbound) {
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "该分组未绑定账号")
			return
		}
		if errors.Is(selectErr, service.ErrAccountShareBalanceBelowMinimum) {
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "账户余额低于共享账号最低准入余额")
			return
		}
		if errors.Is(selectErr, service.ErrAccountSharePerUserConcurrencyExceeded) {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "共享账号单用户并发已达上限")
			return
		}
		if errors.Is(selectErr, service.ErrAccountShareModeSelection) {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "共享账号暂时不可用，请稍后重试")
			return
		}
		if !routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(selectErr)) {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "no available account")
			return
		}
	}

	account := selection.Account
	accountReleaseFunc := selection.ReleaseFunc
	if selection.Acquired {
		// The scheduler may acquire the account slot before user-level moderation.
		// Transfer ownership to the common defer before any subsequent early return.
		currentAccountRelease = wrapAccountSelectionReleaseOnDone(ctx, selection, accountReleaseFunc)
	}
	if decision := h.checkUserContentModerationWithContent(selectedAccountShareCtx, c, reqLog, currentAPIKey, subject, account, service.ContentModerationProtocolOpenAIResponses, reqModel, firstMessage, nil); decision != nil && decision.Blocked {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, decision.Message)
		return
	}
	accountMaxConcurrency := account.Concurrency
	if selection.WaitPlan != nil && selection.WaitPlan.MaxConcurrency > 0 {
		accountMaxConcurrency = selection.WaitPlan.MaxConcurrency
	}
	if !selection.Acquired {
		if selection.WaitPlan == nil {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "account is busy, please retry later")
			return
		}
		fastReleaseFunc, fastAcquired, err := h.concurrencyHelper.TryAcquireAccountSlot(
			ctx,
			account.ID,
			selection.WaitPlan.MaxConcurrency,
		)
		if err != nil {
			reqLog.Warn("openai.websocket_account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "failed to acquire account concurrency slot")
			return
		}
		if !fastAcquired {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "account is busy, please retry later")
			return
		}
		accountReleaseFunc = fastReleaseFunc
		currentAccountRelease = wrapReleaseOnDone(ctx, accountReleaseFunc)
	}
	dispatchCtx := service.WithAccountShareModeRequestFromContext(ctx, selectedAccountShareCtx)
	dispatchRequirements := service.OpenAIAccountDispatchRequirements{
		RequestedModel:    selectedRoutingModel,
		RequiredTransport: service.OpenAIUpstreamTransportResponsesWebsocketV2,
	}
	if selection.OpenAIDispatchRequirements != nil {
		dispatchRequirements = *selection.OpenAIDispatchRequirements
	}
	latestAccount, err := h.gatewayService.RevalidateSelectedOpenAIAccountForDispatch(
		dispatchCtx,
		currentAPIKey.GroupID,
		account,
		dispatchRequirements,
	)
	if err != nil {
		reqLog.Info("openai.websocket_selection_invalidated_before_dispatch", zap.Int64("account_id", account.ID), zap.Error(err))
		closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "selected account is no longer available; please reconnect")
		return
	}
	account = latestAccount
	selection.Account = latestAccount
	accountMaxConcurrency = latestAccount.Concurrency
	if err := h.gatewayService.BindStickySession(ctx, currentAPIKey.GroupID, sessionHash, account.ID); err != nil {
		reqLog.Warn("openai.websocket_bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}

	token, _, err := h.gatewayService.GetRequestCredential(ctx, c, account)
	if err != nil {
		reqLog.Warn("openai.websocket_get_access_token_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		var failoverErr *service.UpstreamFailoverError
		if errors.As(err, &failoverErr) && failoverErr.IsCredentialFailure() {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, service.GrokCredentialUnavailableClientMessage)
			return
		}
		closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "failed to get access token")
		return
	}

	reqLog.Debug("openai.websocket_account_selected",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.Int64p("group_id", currentAPIKey.GroupID),
		zap.String("schedule_layer", scheduleDecision.Layer),
		zap.Int("candidate_count", scheduleDecision.CandidateCount),
	)

	cyberBlockedThisConn := false
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	accountShareWS := selection.AccountShareMode
	var activeTurnNo int
	var activeTurnCtx context.Context
	var activeTurnCancel context.CancelFunc
	var activeTurnPayloadHash string
	var activeUpstreamAttemptID string
	var activeTurnAccount *service.Account
	var activeTurnSelection *service.AccountSelectionResult
	clearActiveTurn := func() {
		if activeTurnCancel != nil {
			activeTurnCancel()
		}
		activeTurnNo = 0
		activeTurnCtx = nil
		activeTurnCancel = nil
		activeTurnPayloadHash = ""
		activeUpstreamAttemptID = ""
		activeTurnAccount = nil
		activeTurnSelection = nil
		releaseTurnSlots()
	}
	defer clearActiveTurn()

	fixedRequestedModel := ""
	fixedRoutingModel := ""
	if accountShareWS {
		fixedRequestedModel = reqModel
		fixedRoutingModel = service.ResolveOpenAIWebSocketForwardModel(account, selectedRoutingModel)
	}
	hooks := &service.OpenAIWSIngressHooks{
		ClientLifecycleContext: clientLifecycleCtx,
		FixedRequestedModel:    fixedRequestedModel,
		FixedRoutingModel:      fixedRoutingModel,
		BeforeTurnPayload: func(turn int, payload []byte) (context.Context, error) {
			if cyberBlockedThisConn {
				return nil, service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, cyberPolicyBlockedClientMsg, nil)
			}
			effectiveGroupID := apiKeyGroupIDValue(currentAPIKey)
			blockState := h.gatewayService.CheckCyberPolicyBlock(dispatchCtx, currentAPIKey.UserID, effectiveGroupID)
			if blockState.Blocked {
				cyberBlockedThisConn = true
				return nil, service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, cyberPolicyBlockedClientMsg, nil)
			}
			if activeTurnNo != 0 {
				return nil, service.NewOpenAIWSClientCloseError(
					coderws.StatusInternalError,
					"websocket turn lifecycle is inconsistent; please reconnect",
					fmt.Errorf("turn %d started while turn %d is still active", turn, activeTurnNo),
				)
			}
			if turn != 1 {
				// 防御式清理：避免异常路径下旧槽位覆盖导致泄漏。
				releaseTurnSlots()
			}

			turnSelection := selection
			if turn != 1 {
				// 每轮重新抢占用户槽位；长连接空闲期间不占用户并发。
				userReleaseFunc, userAcquired, acquireErr := h.concurrencyHelper.TryAcquireUserSlotForAPIKey(
					ctx,
					subject.UserID,
					subject.Concurrency,
					currentAPIKey.ID,
				)
				if acquireErr != nil {
					return nil, service.NewOpenAIWSClientCloseError(coderws.StatusInternalError, "failed to acquire user concurrency slot", acquireErr)
				}
				if !userAcquired {
					return nil, service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "too many concurrent requests, please retry later", nil)
				}
				currentUserRelease = wrapReleaseOnDone(ctx, userReleaseFunc)

				if accountShareWS {
					// 账号广场必须逐轮重新获取 membership + account 的 paired
					// runtime lease。重新选择只用于校验并获取本轮租约，既有
					// WebSocket 不允许静默切换到另一个上游账号。
					previousResponseID := strings.TrimSpace(gjson.GetBytes(payload, "previous_response_id").String())
					nextSelection, _, selectErr := h.gatewayService.SelectAccountWithCleanRelayScheduler(
						dispatchCtx,
						c,
						currentAPIKey.GroupID,
						previousResponseID,
						sessionHash,
						reqModel,
						selectedRoutingModel,
						nil,
						service.OpenAIUpstreamTransportResponsesWebsocketV2,
						false,
						payload,
					)
					if selectErr != nil {
						releaseTurnSlots()
						return nil, service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "shared account is temporarily unavailable; please reconnect", selectErr)
					}
					if nextSelection == nil || nextSelection.Account == nil || !nextSelection.AccountShareMode ||
						nextSelection.RuntimeLease == nil || !nextSelection.Acquired {
						if nextSelection != nil && nextSelection.ReleaseFunc != nil {
							nextSelection.ReleaseFunc()
						}
						releaseTurnSlots()
						return nil, service.NewOpenAIWSClientCloseError(
							coderws.StatusTryAgainLater,
							"shared account runtime lease is unavailable; please reconnect",
							service.ErrAccountShareRuntimeLeaseUnavailable,
						)
					}
					if nextSelection.Account.ID != account.ID {
						nextSelection.ReleaseFunc()
						releaseTurnSlots()
						return nil, service.NewOpenAIWSClientCloseError(
							coderws.StatusTryAgainLater,
							"shared account binding changed; please reconnect",
							fmt.Errorf("websocket account changed from %d to %d", account.ID, nextSelection.Account.ID),
						)
					}
					turnSelection = nextSelection
					currentAccountRelease = wrapAccountSelectionReleaseOnDone(ctx, nextSelection, nextSelection.ReleaseFunc)
				} else {
					accountReleaseFunc, accountAcquired, acquireErr := h.concurrencyHelper.TryAcquireAccountSlot(ctx, account.ID, accountMaxConcurrency)
					if acquireErr != nil {
						releaseTurnSlots()
						return nil, service.NewOpenAIWSClientCloseError(coderws.StatusInternalError, "failed to acquire account concurrency slot", acquireErr)
					}
					if !accountAcquired {
						releaseTurnSlots()
						return nil, service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "account is busy, please retry later", nil)
					}
					currentAccountRelease = wrapReleaseOnDone(ctx, accountReleaseFunc)
				}
			}

			turnCtx, cancelTurn := bindAccountSelectionForwardContext(dispatchCtx, turnSelection)
			if cause := context.Cause(turnCtx); cause != nil {
				cancelTurn()
				releaseTurnSlots()
				return nil, service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "account share runtime lease lost; please reconnect", cause)
			}
			latest, revalidateErr := h.gatewayService.RevalidateSelectedOpenAIAccountForDispatch(
				turnCtx,
				currentAPIKey.GroupID,
				account,
				dispatchRequirements,
			)
			if revalidateErr != nil {
				cancelTurn()
				releaseTurnSlots()
				return nil, service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "selected account is no longer available; please reconnect", revalidateErr)
			}
			if latest == nil || latest.ID != account.ID {
				cancelTurn()
				releaseTurnSlots()
				return nil, service.NewOpenAIWSClientCloseError(
					coderws.StatusTryAgainLater,
					"selected account changed; please reconnect",
					fmt.Errorf("revalidated websocket account does not match account %d", account.ID),
				)
			}
			turnSelection.Account = latest
			accountMaxConcurrency = latest.Concurrency

			payloadHash := service.HashUsageRequestPayload(payload)
			routedModel := service.ResolveOpenAIWebSocketForwardModel(latest, selectedRoutingModel)
			if accountShareWS && routedModel != fixedRoutingModel {
				cancelTurn()
				releaseTurnSlots()
				return nil, service.NewOpenAIWSClientCloseError(
					coderws.StatusTryAgainLater,
					"shared account model routing changed; please reconnect",
					fmt.Errorf("websocket routed model changed from %q to %q", fixedRoutingModel, routedModel),
				)
			}
			activeTurnNo = turn
			activeTurnCtx = turnCtx
			activeTurnCancel = cancelTurn
			activeTurnPayloadHash = payloadHash
			activeTurnAccount = latest
			activeTurnSelection = turnSelection
			// Keep the Ops request snapshot aligned with the response.create turn
			// that is actually about to reach upstream. Otherwise a cyber_policy on
			// a later turn would persist the connection's first payload instead.
			setOpenAIWSOpsTurnRequestContext(c, reqModel, payload)
			activeUpstreamAttemptID = h.beginOpenAIUpstreamAttempt(c, currentAPIKey, latest)
			return turnCtx, nil
		},
		AfterTurnPayload: func(turn int, payload []byte, result *service.OpenAIForwardResult, turnErr error) error {
			if activeTurnNo != turn || activeTurnCtx == nil || activeTurnAccount == nil || activeTurnSelection == nil {
				activeNo := activeTurnNo
				clearActiveTurn()
				return fmt.Errorf("websocket turn lifecycle mismatch: completed=%d active=%d", turn, activeNo)
			}
			turnCtx := activeTurnCtx
			turnPayloadHash := activeTurnPayloadHash
			turnUpstreamAttemptID := activeUpstreamAttemptID
			turnAccount := activeTurnAccount
			turnSelection := activeTurnSelection
			turnAPIKey := currentAPIKey
			turnSubscription := currentSubscription
			defer clearActiveTurn()

			cyberPolicyHit, hitDecision := h.recordCyberPolicyHitForAttempt(
				turnCtx,
				c,
				turnAPIKey,
				turnUpstreamAttemptID,
			)
			if cyberPolicyHit && (hitDecision.HitSequence > 0 || hitDecision.Action != service.CyberPolicyBlockScopeNone || hitDecision.Duplicate) {
				cyberBlockedThisConn = true
			}
			if turnErr == nil && result == nil {
				turnErr = errors.New("websocket turn result is nil")
			}
			recordUsage, _, _ := openAIWSTurnBillingDisposition(result, turnErr)
			if recordUsage {
				recordTurnUsage := func(taskCtx context.Context) error {
					usageCtx := service.WithAccountShareModeRequestFromContext(taskCtx, turnCtx)
					return h.gatewayService.RecordUsage(usageCtx, &service.OpenAIRecordUsageInput{
						Result:             result,
						APIKey:             turnAPIKey,
						User:               turnAPIKey.User,
						Account:            turnAccount,
						Subscription:       turnSubscription,
						InboundEndpoint:    inboundEndpoint,
						UpstreamEndpoint:   upstreamEndpoint,
						UserAgent:          userAgent,
						IPAddress:          clientIP,
						RequestPayloadHash: turnPayloadHash,
						APIKeyService:      h.apiKeyService,
						ChannelUsageFields: channelMappingWS.ToUsageFields(reqModel, result.UpstreamModel),
					})
				}
				logRecordUsageError := func(err error) {
					if err != nil {
						reqLog.Error("openai.websocket_record_usage_failed",
							zap.Int64("account_id", turnAccount.ID),
							zap.String("request_id", result.RequestID),
							zap.Int("turn", turn),
							zap.Error(err),
						)
					}
				}
				if turnSelection.AccountShareMode {
					// A paired account-share lease cannot be reacquired for the
					// next turn until this turn's durable intent reaches ready.
					// Waiting here keeps the release barrier and the WebSocket
					// turn boundary atomic from the client's perspective.
					taskCtx, cancelTask := context.WithTimeout(context.Background(), 10*time.Second)
					recordErr := recordTurnUsage(taskCtx)
					cancelTask()
					if recordErr != nil {
						logRecordUsageError(recordErr)
						return recordErr
					}
				} else {
					h.submitUsageRecordTask(turnCtx, func(taskCtx context.Context) {
						logRecordUsageError(recordTurnUsage(taskCtx))
					})
				}
			}

			if turnErr != nil || result == nil {
				return nil
			}
			if turnAccount.Type == service.AccountTypeOAuth {
				h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(ctx, turnAccount.ID, result.ResponseHeaders)
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(turnAccount.ID, true, result.FirstTokenMs, result.UpstreamModel)
			return nil
		},
	}

	// 应用渠道模型映射到 WebSocket 首条消息
	wsFirstMessage := firstMessage
	if channelMappingWS.Mapped && !accountShareWS {
		wsFirstMessage = h.gatewayService.ReplaceModelInBody(firstMessage, channelMappingWS.MappedModel)
	}

	if err := h.gatewayService.ProxyResponsesWebSocketFromClient(ctx, c, wsConn, account, token, wsFirstMessage, hooks); err != nil {
		if errors.Is(context.Cause(ctx), service.ErrOpenAIWSIngressLeaseLost) {
			reqLog.Warn("openai.websocket_ingress_lease_lost", zap.Int64("account_id", account.ID), zap.Error(err))
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "websocket ingress capacity lease lost; please reconnect")
			return
		}
		if errors.Is(err, service.ErrAccountShareRuntimeLeaseLost) {
			reqLog.Warn("openai.websocket_account_share_runtime_lease_lost", zap.Int64("account_id", account.ID), zap.Error(err))
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "account share runtime lease lost; please reconnect")
			return
		}
		var closeErr *service.OpenAIWSClientCloseError
		if errors.As(err, &closeErr) && closeErr.StatusCode() == coderws.StatusNormalClosure {
			reqLog.Info("openai.websocket_ingress_closed_normally", zap.Int64("account_id", account.ID), zap.String("reason", closeErr.Reason()))
			closeOpenAIClientWS(wsConn, closeErr.StatusCode(), closeErr.Reason())
			return
		}
		h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
		closeStatus, closeReason := summarizeWSCloseErrorForLog(err)
		proxyFailedFields := []zap.Field{
			zap.Int64("account_id", account.ID),
			zap.Error(err),
			zap.String("close_status", closeStatus),
			zap.String("close_reason", closeReason),
		}
		proxyFailedFields = appendOpenAIProxyLogFields(proxyFailedFields, account)
		reqLog.Warn("openai.websocket_proxy_failed", proxyFailedFields...)
		if errors.As(err, &closeErr) {
			closeOpenAIClientWS(wsConn, closeErr.StatusCode(), closeErr.Reason())
			return
		}
		closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "upstream websocket proxy failed")
		return
	}
	routeCursor.recordSuccess(apiKey.ID)
	reqLog.Info("openai.websocket_ingress_closed", zap.Int64("account_id", account.ID))
}

func setOpenAIWSOpsTurnRequestContext(c *gin.Context, requestedModel string, payload []byte) {
	setOpsRequestContext(c, requestedModel, true, payload)
}

func appendOpenAIProxyLogFields(fields []zap.Field, account *service.Account) []zap.Field {
	if account == nil {
		return fields
	}
	if account.Proxy != nil {
		return append(fields,
			zap.Int64("proxy_id", account.Proxy.ID),
			zap.String("proxy_name", account.Proxy.Name),
			zap.String("proxy_host", account.Proxy.Host),
			zap.Int("proxy_port", account.Proxy.Port),
		)
	}
	if account.ProxyID != nil {
		return append(fields, zap.Int64p("proxy_id", account.ProxyID))
	}
	return fields
}

func (h *OpenAIGatewayHandler) recoverResponsesPanic(c *gin.Context, streamStarted *bool) {
	recovered := recover()
	if recovered == nil {
		return
	}

	started := false
	if streamStarted != nil {
		started = *streamStarted
	}
	wroteFallback := h.ensureForwardErrorResponse(c, started)
	requestLogger(c, "handler.openai_gateway.responses").Error(
		"openai.responses_panic_recovered",
		zap.Bool("fallback_error_response_written", wroteFallback),
		zap.Any("panic", recovered),
		zap.ByteString("stack", debug.Stack()),
	)
}

// recoverAnthropicMessagesPanic recovers from panics in the Anthropic Messages
// handler and returns an Anthropic-formatted error response.
func (h *OpenAIGatewayHandler) recoverAnthropicMessagesPanic(c *gin.Context, streamStarted *bool) {
	recovered := recover()
	if recovered == nil {
		return
	}

	started := streamStarted != nil && *streamStarted
	requestLogger(c, "handler.openai_gateway.messages").Error(
		"openai.messages_panic_recovered",
		zap.Bool("stream_started", started),
		zap.Any("panic", recovered),
		zap.ByteString("stack", debug.Stack()),
	)
	if !started {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "Internal server error")
	}
}

func (h *OpenAIGatewayHandler) ensureResponsesDependencies(c *gin.Context, reqLog *zap.Logger) bool {
	missing := h.missingResponsesDependencies()
	if len(missing) == 0 {
		return true
	}

	if reqLog == nil {
		reqLog = requestLogger(c, "handler.openai_gateway.responses")
	}
	reqLog.Error("openai.handler_dependencies_missing", zap.Strings("missing_dependencies", missing))

	if c != nil && c.Writer != nil && !c.Writer.Written() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "api_error",
				"message": "Service temporarily unavailable",
			},
		})
	}
	return false
}

func (h *OpenAIGatewayHandler) missingResponsesDependencies() []string {
	missing := make([]string, 0, 5)
	if h == nil {
		return append(missing, "handler")
	}
	if h.gatewayService == nil {
		missing = append(missing, "gatewayService")
	}
	if h.billingCacheService == nil {
		missing = append(missing, "billingCacheService")
	}
	if h.apiKeyService == nil {
		missing = append(missing, "apiKeyService")
	}
	if h.concurrencyHelper == nil || h.concurrencyHelper.concurrencyService == nil {
		missing = append(missing, "concurrencyHelper")
	}
	return missing
}

func getContextInt64(c *gin.Context, key string) (int64, bool) {
	if c == nil || key == "" {
		return 0, false
	}
	v, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	default:
		return 0, false
	}
}

func (h *OpenAIGatewayHandler) submitUsageRecordTask(requestCtx context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	if h.usageRecordWorkerPool != nil {
		mode := h.usageRecordWorkerPool.Submit(task)
		if mode != service.UsageRecordSubmitModeDropped {
			return
		}
		logger.L().With(
			zap.String("component", "handler.openai_gateway.responses"),
		).Warn("openai.usage_record_task_dropped_sync_fallback")
	}
	runUsageRecordTaskSync(requestCtx, task, "handler.openai_gateway.responses", "openai.usage_record_task_panic_recovered")
}

// handleConcurrencyError distinguishes a gateway first-output budget from a
// real concurrency rejection so users and metrics do not misclassify a 504 as
// an account/user rate limit.
func (h *OpenAIGatewayHandler) handleConcurrencyError(c *gin.Context, err error, slotType string, streamStarted bool) {
	if errors.Is(err, service.ErrOpenAIFirstOutputRoutingBudgetExceeded) {
		h.handleStreamingAwareError(c, http.StatusGatewayTimeout, "routing_budget_exhausted",
			"Gateway routing budget expired before an upstream attempt could start", streamStarted)
		return
	}
	var waitErr *WaitQueueFullError
	if errors.As(err, &waitErr) {
		h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", waitErr.Error(), streamStarted)
		return
	}
	h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error",
		fmt.Sprintf("Concurrency limit exceeded for %s, please retry later", slotType), streamStarted)
}

func (h *OpenAIGatewayHandler) abortIfOpenAIFirstOutputBudgetExpired(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Request == nil {
		return false
	}
	remaining, enabled := service.OpenAIFirstOutputBudgetRemaining(c.Request.Context())
	if !enabled || remaining > 0 {
		return false
	}
	h.handleStreamingAwareError(c, http.StatusGatewayTimeout, "routing_budget_exhausted",
		"Gateway routing budget expired before an upstream attempt could start", streamStarted)
	return true
}

func (h *OpenAIGatewayHandler) handleFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, streamStarted bool) {
	if failoverErr == nil {
		h.handleFailoverExhaustedSimple(c, http.StatusBadGateway, streamStarted)
		return
	}
	if failoverErr.Reason == service.GatewayFailureReasonRoutingBudgetExhausted {
		// This is a request-scoped local deadline, not an upstream 504. Preserve
		// the same contract used when the budget expires in a concurrency wait.
		h.handleStreamingAwareError(c, http.StatusGatewayTimeout, "routing_budget_exhausted",
			"Gateway routing budget expired before an upstream attempt could start", streamStarted)
		return
	}
	if failoverErr.IsOpenAIRequestBodyTooLarge() {
		service.SetOpsUpstreamError(c, http.StatusRequestEntityTooLarge, service.OpenAIRequestBodyTooLargeClientMessage, "")
		h.handleStreamingAwareError(
			c,
			http.StatusRequestEntityTooLarge,
			"invalid_request_error",
			service.OpenAIRequestBodyTooLargeClientMessage,
			streamStarted,
		)
		return
	}
	if failoverErr.IsCredentialFailure() {
		status := failoverErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		message := strings.TrimSpace(failoverErr.ClientMessage)
		if message == "" {
			message = service.GrokCredentialUnavailableClientMessage
		}
		h.handleStreamingAwareError(c, status, "upstream_error", message, streamStarted)
		return
	}
	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody

	// 先检查透传规则
	if h.errorPassthroughService != nil && len(responseBody) > 0 {
		if rule := h.errorPassthroughService.MatchRule("openai", statusCode, responseBody); rule != nil {
			// 确定响应状态码
			respCode := statusCode
			if !rule.PassthroughCode && rule.ResponseCode != nil {
				respCode = *rule.ResponseCode
			}

			// 确定响应消息
			msg := service.ExtractUpstreamErrorMessage(responseBody)
			if !rule.PassthroughBody && rule.CustomMessage != nil {
				msg = *rule.CustomMessage
			}

			if rule.SkipMonitoring {
				c.Set(service.OpsSkipPassthroughKey, true)
			}

			h.handleStreamingAwareError(c, respCode, "upstream_error", msg, streamStarted)
			return
		}
	}

	// 记录原始上游状态码，以便 ops 错误日志捕获真实的上游错误
	upstreamMsg := service.ExtractUpstreamErrorMessage(responseBody)
	service.SetOpsUpstreamError(c, statusCode, upstreamMsg, "")

	// 使用默认的错误映射
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

// handleFailoverExhaustedSimple 简化版本，用于没有响应体的情况
func (h *OpenAIGatewayHandler) handleFailoverExhaustedSimple(c *gin.Context, statusCode int, streamStarted bool) {
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	service.SetOpsUpstreamError(c, statusCode, errMsg, "")
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

func (h *OpenAIGatewayHandler) mapUpstreamError(statusCode int) (int, string, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"
	case 529:
		return http.StatusServiceUnavailable, "upstream_error", "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}

// handleStreamingAwareError handles errors that may occur after streaming has started
func (h *OpenAIGatewayHandler) handleStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		streamStarted = true
	}
	if streamStarted {
		service.MarkOpsStreamError(c, errType, message, status)
		if isOpenAIRemoteCompactPath(c) {
			service.WriteOpenAICompactSSEFailureForHandler(c, status, errType, message)
			return
		}
		// Stream already started, send error as SSE event then close
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			// SSE 错误事件固定 schema，使用 Quote 直拼可避免额外 Marshal 分配。
			errorEvent := "event: error\ndata: " + `{"error":{"type":` + strconv.Quote(errType) + `,"message":` + strconv.Quote(message) + `}}` + "\n\n"
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}

	// Normal case: return JSON response with proper status code
	h.errorResponse(c, status, errType, message)
}

const cyberPolicyBlockedClientMsg = "当前请求范围已被网络安全策略暂时隔离，请稍后重试或切换可用分组 / This request scope is temporarily isolated by cyber-security policy; retry later or use another available group"

type cyberPolicyBlockFormat int

const (
	cyberBlockFormatResponses cyberPolicyBlockFormat = iota
	cyberBlockFormatChat
	cyberBlockFormatAnthropic
)

type cyberPolicyRouteDisposition int

const (
	cyberPolicyRouteAllowed cyberPolicyRouteDisposition = iota
	cyberPolicyRouteSkipped
	cyberPolicyRouteRejected
)

func (h *OpenAIGatewayHandler) checkCyberPolicyRouteBlock(
	c *gin.Context,
	apiKey *service.APIKey,
	model string,
	format cyberPolicyBlockFormat,
	routeCursor *apiKeyGroupRouteCursor,
	reqLog *zap.Logger,
) cyberPolicyRouteDisposition {
	if h == nil || h.gatewayService == nil || c == nil || apiKey == nil {
		return cyberPolicyRouteAllowed
	}
	effectiveGroupID := apiKeyGroupIDValue(apiKey)
	if effectiveGroupID <= 0 {
		return cyberPolicyRouteAllowed
	}
	setOpsEffectiveRoute(c, apiKey, nil)
	state := h.gatewayService.CheckCyberPolicyBlock(c.Request.Context(), apiKey.UserID, effectiveGroupID)
	if !state.Blocked {
		return cyberPolicyRouteAllowed
	}
	if routeCursor != nil && routeCursor.skipToNext(
		"cyber_policy_route_blocked",
		reqLog,
		zap.Int64("user_id", apiKey.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Int64("effective_group_id", effectiveGroupID),
		zap.String("block_scope", string(state.Scope)),
	) {
		return cyberPolicyRouteSkipped
	}
	if state.RetryAfter > 0 {
		retryAfterSeconds := int((state.RetryAfter + time.Second - 1) / time.Second)
		if retryAfterSeconds > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfterSeconds))
		}
	}
	if format == cyberBlockFormatResponses && service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		service.WriteOpenAICompactSSEFailureForHandler(c, http.StatusForbidden, "permission_error", cyberPolicyBlockedClientMsg)
		return cyberPolicyRouteRejected
	}
	switch format {
	case cyberBlockFormatAnthropic:
		c.JSON(http.StatusForbidden, gin.H{"type": "error", "error": gin.H{
			"type":    "permission_error",
			"message": cyberPolicyBlockedClientMsg,
		}})
	default:
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"type":    "permission_error",
			"code":    "request_scope_blocked_by_cyber_policy",
			"message": cyberPolicyBlockedClientMsg,
		}})
	}
	requestLogger(c, "handler.openai_gateway.cyber_session_block").Warn(
		"openai.cyber_policy_route_blocked",
		zap.Int64("user_id", apiKey.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Int64("effective_group_id", effectiveGroupID),
		zap.String("block_scope", string(state.Scope)),
		zap.String("model", model),
	)
	return cyberPolicyRouteRejected
}

func (h *OpenAIGatewayHandler) recordCyberPolicyHitForAttempt(
	ctx context.Context,
	c *gin.Context,
	apiKey *service.APIKey,
	upstreamAttemptID string,
) (bool, service.CyberPolicyHitDecision) {
	if h == nil || h.gatewayService == nil || c == nil || apiKey == nil {
		return false, service.CyberPolicyHitDecision{}
	}
	if service.GetOpsCyberPolicyForAttempt(c, upstreamAttemptID) == nil {
		return false, service.CyberPolicyHitDecision{}
	}
	if !service.IsOpenAICyberPolicyEnforcedForCurrentAttempt(c) {
		return false, service.CyberPolicyHitDecision{}
	}
	effectiveGroupID := apiKeyGroupIDValue(apiKey)
	if effectiveGroupID <= 0 {
		return false, service.CyberPolicyHitDecision{}
	}
	decision := h.gatewayService.RecordCyberPolicyHitForEnforcedAttempt(
		ctx,
		apiKey.UserID,
		effectiveGroupID,
		upstreamAttemptID,
	)
	if decision.Enforced {
		requestLogger(c, "handler.openai_gateway.cyber_policy_hit").Warn(
			"openai.cyber_policy_hit",
			zap.Int64("user_id", apiKey.UserID),
			zap.Int64("api_key_id", apiKey.ID),
			zap.Int64("effective_group_id", effectiveGroupID),
			zap.Int64("hit_sequence", decision.HitSequence),
			zap.String("action", string(decision.Action)),
			zap.Time("blocked_until", decision.BlockedUntil),
			zap.Bool("duplicate", decision.Duplicate),
		)
	}
	return decision.Enforced, decision
}

func writeCyberPolicyBlockedWSError(ctx context.Context, conn *coderws.Conn) {
	if conn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(gin.H{
		"event_id": "evt_cyber_policy_blocked",
		"type":     "error",
		"error": gin.H{
			"type":    "permission_error",
			"code":    "request_scope_blocked_by_cyber_policy",
			"message": cyberPolicyBlockedClientMsg,
		},
	})
	if err != nil {
		payload = []byte(`{"event_id":"evt_cyber_policy_blocked","type":"error","error":{"type":"permission_error","code":"request_scope_blocked_by_cyber_policy","message":"This request scope is temporarily isolated by cyber-security policy; retry later or use another available group"}}`)
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = conn.Write(writeCtx, coderws.MessageText, payload)
}

// ensureForwardErrorResponse 在 Forward 返回错误但尚未写响应时补写统一错误响应。
func (h *OpenAIGatewayHandler) ensureForwardErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	compactKeepaliveCommitted := service.StopOpenAICompactSSEKeepaliveCommitted(c)
	if compactKeepaliveCommitted {
		streamStarted = true
	}
	imageKeepalivePresent := service.OpenAIImagesJSONKeepalivePresent(c)
	service.StopOpenAIImagesJSONKeepaliveCommitted(c)
	imageKeepalivePaddingOnly := false
	imageKeepaliveResponseWritten := false
	if imageKeepalivePresent {
		adjustedSize := service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
		imageKeepalivePaddingOnly = adjustedSize < 0
		imageKeepaliveResponseWritten = adjustedSize >= 0
	}
	if service.IsResponseCommitted(c) || (!compactKeepaliveCommitted && imageKeepaliveResponseWritten) {
		return false
	}
	if c.Writer.Written() && !imageKeepalivePresent && !streamStarted {
		return false
	}
	if c.Writer.Written() && !imageKeepalivePaddingOnly {
		streamStarted = true
	}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed", streamStarted)
	return true
}

func shouldLogOpenAIForwardFailureAsWarn(c *gin.Context, wroteFallback bool) bool {
	if wroteFallback {
		return false
	}
	if c == nil || c.Writer == nil {
		return false
	}
	return c.Writer.Written()
}

func openAIForwardErrorAlreadyCommunicated(c *gin.Context, writerSizeBeforeForward int, err error) bool {
	if err == nil || c == nil || c.Writer == nil {
		return false
	}
	if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward ||
		service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return false
	}
	if service.GetOpsCyberPolicy(c) != nil {
		return true
	}

	msg := strings.TrimSpace(err.Error())
	for _, prefix := range []string{
		"upstream response failed:",
		"non-streaming openai protocol error:",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

func openAIForwardMayFailover(c *gin.Context, writerSizeBeforeForward int, failoverErr *service.UpstreamFailoverError) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return true
	}
	return failoverErr != nil && failoverErr.SafeToFailoverAfterWrite
}

func openAIRequestAllowsFailoverReplay(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return !failoverClientGone(c)
}

func openAIFirstOutputFailoverExhausted(failoverErr *service.UpstreamFailoverError, switchCount *int) bool {
	if failoverErr == nil || !failoverErr.SafeToFailoverAfterWrite || switchCount == nil {
		return false
	}
	if *switchCount >= maxOpenAIFirstOutputTimeoutSwitches {
		return true
	}
	*switchCount = *switchCount + 1
	return false
}

// errorResponse returns OpenAI API format error response
func (h *OpenAIGatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		service.WriteOpenAICompactSSEFailureForHandler(c, status, errType, message)
		return
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func (h *OpenAIGatewayHandler) openAICompactKeepaliveInterval() time.Duration {
	if h == nil || h.cfg == nil || h.cfg.Gateway.StreamKeepaliveInterval <= 0 {
		return 0
	}
	return time.Duration(h.cfg.Gateway.StreamKeepaliveInterval) * time.Second
}

func setOpenAIClientTransportHTTP(c *gin.Context) {
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportHTTP)
}

func setOpenAIClientTransportWS(c *gin.Context) {
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
}

func ensureOpenAIPoolModeSessionHash(sessionHash string, account *service.Account) string {
	if sessionHash != "" || account == nil || !account.IsPoolMode() {
		return sessionHash
	}
	// 为当前请求生成一次性粘性会话键，确保同账号重试不会重新负载均衡到其他账号。
	return "openai-pool-retry-" + uuid.NewString()
}

func openAIWSIngressFallbackSessionSeed(userID, apiKeyID int64, groupID *int64) string {
	gid := int64(0)
	if groupID != nil {
		gid = *groupID
	}
	return fmt.Sprintf("openai_ws_ingress:%d:%d:%d", gid, userID, apiKeyID)
}

func isOpenAIWSUpgradeRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Connection"))), "upgrade")
}

func closeOpenAIClientWS(conn *coderws.Conn, status coderws.StatusCode, reason string) {
	if conn == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = conn.Close(status, reason)
	_ = conn.CloseNow()
}

func summarizeWSCloseErrorForLog(err error) (string, string) {
	if err == nil {
		return "-", "-"
	}
	statusCode := coderws.CloseStatus(err)
	if statusCode == -1 {
		return "-", "-"
	}
	closeStatus := fmt.Sprintf("%d(%s)", int(statusCode), statusCode.String())
	closeReason := "-"
	var closeErr coderws.CloseError
	if errors.As(err, &closeErr) {
		reason := strings.TrimSpace(closeErr.Reason)
		if reason != "" {
			closeReason = reason
		}
	}
	return closeStatus, closeReason
}
