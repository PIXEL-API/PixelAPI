package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// geminiCLITmpDirRegex 用于从 Gemini CLI 请求体中提取 tmp 目录的哈希值
// 匹配格式: /Users/xxx/.gemini/tmp/[64位十六进制哈希]
var geminiCLITmpDirRegex = regexp.MustCompile(`/\.gemini/tmp/([A-Fa-f0-9]{64})`)

// GeminiV1BetaListModels proxies:
// GET /v1beta/models
func (h *GatewayHandler) GeminiV1BetaListModels(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	// 检查平台：优先使用强制平台（/antigravity 路由），否则要求 gemini 分组
	forcePlatform, hasForcePlatform := middleware.GetForcePlatformFromContext(c)
	if !hasForcePlatform && (apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGemini) {
		googleError(c, http.StatusBadRequest, "API key group platform is not gemini")
		return
	}

	// 强制 antigravity 模式：返回 antigravity 支持的模型列表
	if forcePlatform == service.PlatformAntigravity {
		c.JSON(http.StatusOK, antigravity.FallbackGeminiModelsList())
		return
	}

	account, err := h.geminiCompatService.SelectAccountForAIStudioEndpoints(c.Request.Context(), apiKey.GroupID)
	if err != nil {
		// 没有 gemini 账户，检查是否有 antigravity 账户可用
		hasAntigravity, _ := h.geminiCompatService.HasAntigravityAccounts(c.Request.Context(), apiKey.GroupID)
		if hasAntigravity {
			// antigravity 账户使用静态模型列表
			c.JSON(http.StatusOK, gemini.FallbackModelsList())
			return
		}
		googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts: "+err.Error())
		return
	}

	res, err := h.geminiCompatService.ForwardAIStudioGET(c.Request.Context(), account, "/v1beta/models")
	if err != nil {
		googleError(c, http.StatusBadGateway, err.Error())
		return
	}
	if shouldFallbackGeminiModels(res) {
		c.JSON(http.StatusOK, gemini.FallbackModelsList())
		return
	}
	writeUpstreamResponse(c, res)
}

// GeminiV1BetaGetModel proxies:
// GET /v1beta/models/{model}
func (h *GatewayHandler) GeminiV1BetaGetModel(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	// 检查平台：优先使用强制平台（/antigravity 路由），否则要求 gemini 分组
	forcePlatform, hasForcePlatform := middleware.GetForcePlatformFromContext(c)
	if !hasForcePlatform && (apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGemini) {
		googleError(c, http.StatusBadRequest, "API key group platform is not gemini")
		return
	}

	modelName := strings.TrimSpace(c.Param("model"))
	if modelName == "" {
		googleError(c, http.StatusBadRequest, "Missing model in URL")
		return
	}
	// 模型名会被拼进上游 URL 的 path，先在入口校验片段合规性，
	// 见 service/upstream_path_guard.go。
	if !service.IsSafeGeminiModelPathSegment(modelName) {
		googleError(c, http.StatusBadRequest, "Invalid model in URL")
		return
	}

	// 强制 antigravity 模式：返回 antigravity 模型信息
	if forcePlatform == service.PlatformAntigravity {
		c.JSON(http.StatusOK, antigravity.FallbackGeminiModel(modelName))
		return
	}

	account, err := h.geminiCompatService.SelectAccountForAIStudioEndpoints(c.Request.Context(), apiKey.GroupID)
	if err != nil {
		// 没有 gemini 账户，检查是否有 antigravity 账户可用
		hasAntigravity, _ := h.geminiCompatService.HasAntigravityAccounts(c.Request.Context(), apiKey.GroupID)
		if hasAntigravity {
			// antigravity 账户使用静态模型信息
			c.JSON(http.StatusOK, gemini.FallbackModel(modelName))
			return
		}
		googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts: "+err.Error())
		return
	}

	res, err := h.geminiCompatService.ForwardAIStudioGET(c.Request.Context(), account, "/v1beta/models/"+modelName)
	if err != nil {
		googleError(c, http.StatusBadGateway, err.Error())
		return
	}
	if shouldFallbackGeminiModel(modelName, res) {
		c.JSON(http.StatusOK, gemini.FallbackModel(modelName))
		return
	}
	writeUpstreamResponse(c, res)
}

// GeminiV1BetaModels proxies Gemini native REST endpoints like:
// POST /v1beta/models/{model}:generateContent
// POST /v1beta/models/{model}:streamGenerateContent?alt=sse
func (h *GatewayHandler) GeminiV1BetaModels(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	authSubject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		googleError(c, http.StatusInternalServerError, "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gemini_v1beta.models",
		zap.Int64("user_id", authSubject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// 检查平台：优先使用强制平台（/antigravity 路由，中间件已设置 request.Context），否则要求 gemini 分组
	if !middleware.HasForcePlatform(c) {
		if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGemini {
			googleError(c, http.StatusBadRequest, "API key group platform is not gemini")
			return
		}
	}

	modelName, action, err := parseGeminiModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
	if err != nil {
		googleError(c, http.StatusNotFound, err.Error())
		return
	}
	// URL 里的模型名最终会被拼进上游 /v1beta/models/{model}:{action}，
	// 先在入口校验片段合规性，见 service/upstream_path_guard.go。
	if !service.IsSafeGeminiModelPathSegment(modelName) {
		googleError(c, http.StatusBadRequest, "Invalid model in URL")
		return
	}

	stream := action == "streamGenerateContent"
	reqLog = reqLog.With(zap.String("model", modelName), zap.String("action", action), zap.Bool("stream", stream))

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			googleError(c, http.StatusRequestEntityTooLarge, buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		googleError(c, http.StatusBadRequest, "Failed to read request body")
		return
	}
	if len(body) == 0 {
		googleError(c, http.StatusBadRequest, "Request body is empty")
		return
	}

	setOpsRequestContext(c, modelName, stream, body)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))

	reqModel := modelName
	baseBody := body

	// Get subscription (may be nil)
	subscription, _ := middleware.GetSubscriptionFromContext(c)

	// For Gemini native API, do not send Claude-style ping frames.
	geminiConcurrency := NewConcurrencyHelper(h.concurrencyHelper.concurrencyService, SSEPingFormatNone, 0)

	// 1) user concurrency slot
	streamStarted := false
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}
	userReleaseFunc, err := geminiConcurrency.AcquireUserSlotWithWait(c, authSubject.UserID, authSubject.Concurrency, stream, &streamStarted)
	if err != nil {
		reqLog.Warn("gemini.user_slot_acquire_failed", zap.Error(err))
		if failoverClientGone(c) {
			return
		}
		switch {
		case errors.Is(err, service.ErrOpenAIFirstOutputRoutingBudgetExceeded):
			googleError(c, http.StatusGatewayTimeout, "Gateway routing budget expired before an upstream attempt could start")
		case isSlotCapacityError(err, "user"):
			googleError(c, http.StatusTooManyRequests, "User concurrency limit exceeded, please retry later")
		default:
			googleError(c, http.StatusServiceUnavailable, "User concurrency service is temporarily unavailable")
		}
		return
	}
	// 确保请求取消时也会释放槽位，避免长连接被动中断造成泄漏
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2) 计算与分组无关的会话标识。具体绑定仍按每条路由的 group_id 隔离。
	sessionHash := extractGeminiCLISessionHash(c, body)
	if sessionHash == "" {
		// Fallback: 使用通用的会话哈希生成逻辑（适用于其他客户端）
		parsedReq, _ := service.ParseGatewayRequest(body, domain.PlatformGemini)
		if parsedReq != nil {
			parsedReq.SessionContext = &service.SessionContext{
				ClientIP:  ip.GetSecurityClientIP(c),
				UserAgent: c.GetHeader("User-Agent"),
				APIKeyID:  apiKey.ID,
			}
		}
		sessionHash = h.gatewayService.GenerateSessionHash(parsedReq)
	}
	sessionKey := sessionHash
	if sessionHash != "" {
		sessionKey = "gemini:" + sessionHash
	}
	baseSessionKey := sessionKey

	routeCursor, _, routeErr := newAPIKeyGroupRouteCursorWithModeIsolation(
		c.Request.Context(),
		apiKey,
		h.gatewayService.IsAccountShareModeGroup,
		true,
	)
	if routeErr != nil {
		if failoverClientGone(c) {
			return
		}
		reqLog.Error("api_key_group_route.mode_classification_failed", zap.Error(routeErr))
		googleError(c, http.StatusServiceUnavailable, "Account routing service is temporarily unavailable")
		return
	}
	var routeBillingGate apiKeyGroupRouteBillingGate

geminiNativeRouteLoop:
	for {
		routeCandidate, ok := routeCursor.current()
		if !ok {
			googleError(c, http.StatusServiceUnavailable, "No available API key group routes")
			return
		}
		currentAPIKey := routeCandidate.APIKey
		currentSubscription, resolveErr := h.gatewayService.ResolveRouteSubscription(c.Request.Context(), currentAPIKey, subscription)
		if resolveErr != nil {
			reqLog.Info("gemini.route_subscription_resolve_failed", zap.Error(resolveErr), zap.Int64p("group_id", currentAPIKey.GroupID))
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, resolveErr, "route_subscription_unavailable", reqLog)
			if retry {
				continue geminiNativeRouteLoop
			}
			status, _, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			googleError(c, status, message)
			return
		}
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, reqModel)
		routeModel := reqModel
		if channelMapping.Mapped {
			routeModel = channelMapping.MappedModel
		}

		// 3) billing eligibility check (after user wait, per route)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("gemini.billing_eligibility_check_failed", zap.Error(err), zap.Int64p("group_id", currentAPIKey.GroupID))
			retry, termErr := routeBillingGate.skipOrTerminate(routeCursor, err, "route_billing_ineligible", reqLog)
			if retry {
				continue geminiNativeRouteLoop
			}
			status, _, message, retryAfter := billingErrorDetails(termErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			googleError(c, status, message)
			return
		}

		routeBody := baseBody
		sessionKey := baseSessionKey

		// 查询当前路由分组的粘性会话绑定（用于检测账号切换）。
		var sessionBoundAccountID int64
		if sessionKey != "" {
			sessionBoundAccountID, _ = h.gatewayService.GetCachedSessionAccountID(c.Request.Context(), currentAPIKey.GroupID, sessionKey)
			if sessionBoundAccountID > 0 {
				prefetchedGroupID := derefGroupID(currentAPIKey.GroupID)
				ctx := service.WithPrefetchedStickySession(c.Request.Context(), sessionBoundAccountID, prefetchedGroupID, h.metadataBridgeEnabled())
				c.Request = c.Request.WithContext(ctx)
			}
		}

		// === Gemini 内容摘要会话 Fallback 逻辑 ===
		// 当原有会话标识无效时（sessionBoundAccountID == 0），尝试基于内容摘要链匹配
		var geminiDigestChain string
		var geminiPrefixHash string
		var geminiSessionUUID string
		var matchedDigestChain string
		useDigestFallback := sessionBoundAccountID == 0

		if useDigestFallback {
			// 解析 Gemini 请求体
			var geminiReq antigravity.GeminiRequest
			if err := json.Unmarshal(routeBody, &geminiReq); err == nil && len(geminiReq.Contents) > 0 {
				// 生成摘要链
				geminiDigestChain = service.BuildGeminiDigestChain(&geminiReq)
				if geminiDigestChain != "" {
					// 生成前缀 hash
					userAgent := c.GetHeader("User-Agent")
					clientIP := ip.GetSecurityClientIP(c)
					platform := ""
					if currentAPIKey.Group != nil {
						platform = currentAPIKey.Group.Platform
					}
					geminiPrefixHash = service.GenerateGeminiPrefixHash(
						authSubject.UserID,
						currentAPIKey.ID,
						clientIP,
						userAgent,
						platform,
						routeModel,
					)

					// 查找会话
					foundUUID, foundAccountID, foundMatchedChain, found := h.gatewayService.FindGeminiSession(
						c.Request.Context(),
						derefGroupID(currentAPIKey.GroupID),
						geminiPrefixHash,
						geminiDigestChain,
					)
					if found {
						matchedDigestChain = foundMatchedChain
						sessionBoundAccountID = foundAccountID
						geminiSessionUUID = foundUUID
						reqLog.Info("gemini.digest_fallback_matched",
							zap.String("session_uuid_prefix", safeShortPrefix(foundUUID, 8)),
							zap.Int64("account_id", foundAccountID),
							zap.String("digest_chain", truncateDigestChain(geminiDigestChain)),
						)

						// 关键：如果原 sessionKey 为空，使用 prefixHash + uuid 作为 sessionKey
						// 这样 SelectAccountWithLoadAwareness 的粘性会话逻辑会优先使用匹配到的账号
						if sessionKey == "" {
							sessionKey = service.GenerateGeminiDigestSessionKey(geminiPrefixHash, foundUUID)
						}
						_ = h.gatewayService.BindStickySession(c.Request.Context(), currentAPIKey.GroupID, sessionKey, foundAccountID)
					} else {
						// 生成新的会话 UUID
						geminiSessionUUID = uuid.New().String()
						// 为新会话也生成 sessionKey（用于后续请求的粘性会话）
						if sessionKey == "" {
							sessionKey = service.GenerateGeminiDigestSessionKey(geminiPrefixHash, geminiSessionUUID)
						}
					}
				}
			}
		}

		// 保留路由进入时的绑定用于成功后的粘性写回；另用 lastAttemptAccountID 检测同请求内切号。
		hasBoundSession := sessionKey != "" && sessionBoundAccountID > 0
		lastAttemptAccountID := sessionBoundAccountID
		cleanedForUnknownBinding := false

		fs := NewFailoverState(h.maxAccountSwitchesGemini, hasBoundSession)

		// SingleAccountRetry 是当前路由属性；切换分组时必须显式覆盖 true/false。
		singleAccountRetry := h.gatewayService.IsSingleAntigravityAccountGroup(c.Request.Context(), currentAPIKey.GroupID)
		ctx := service.WithSingleAccountRetry(c.Request.Context(), singleAccountRetry, h.metadataBridgeEnabled())
		c.Request = c.Request.WithContext(ctx)

		for {
			selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), currentAPIKey.GroupID, sessionKey, routeModel, fs.FailedAccountIDs, "", int64(0)) // Gemini 不使用会话限制
			if err != nil {
				if failoverClientGone(c) {
					return
				}
				if !errors.Is(err, service.ErrNoAvailableAccounts) {
					reqLog.Warn("gemini.account_selection_infrastructure_failed", zap.Error(err))
					googleError(c, http.StatusServiceUnavailable, "Account selection is temporarily unavailable")
					return
				}
				if len(fs.FailedAccountIDs) == 0 {
					if !streamStarted && routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(err)) {
						continue geminiNativeRouteLoop
					}
					googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts: "+err.Error())
					return
				}
				action := fs.HandleSelectionExhausted(c.Request.Context())
				switch action {
				case FailoverContinue:
					ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
					c.Request = c.Request.WithContext(ctx)
					continue
				case FailoverCanceled:
					failoverClientGone(c)
					return
				default: // FailoverExhausted
					if fs.LastFailoverErr != nil && !streamStarted && shouldSwitchAPIKeyGroupRoute(fs.LastFailoverErr) &&
						routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", fs.LastFailoverErr.StatusCode)) {
						continue geminiNativeRouteLoop
					}
					h.handleGeminiFailoverExhausted(c, fs.LastFailoverErr)
					return
				}
			}
			account := selection.Account
			setOpsSelectedAccount(c, account.ID, account.Platform)

			// 检测账号切换：如果粘性会话绑定的账号与当前选择的账号不同，清除 thoughtSignature
			// 注意：Gemini 原生 API 的 thoughtSignature 与具体上游账号强相关；跨账号透传会导致 400。
			if lastAttemptAccountID > 0 && lastAttemptAccountID != account.ID {
				reqLog.Info("gemini.sticky_session_account_switched",
					zap.Int64("from_account_id", lastAttemptAccountID),
					zap.Int64("to_account_id", account.ID),
					zap.Bool("clean_thought_signature", true),
				)
				routeBody = service.CleanGeminiNativeThoughtSignatures(routeBody)
				lastAttemptAccountID = account.ID
			} else if sessionKey != "" && lastAttemptAccountID == 0 && !cleanedForUnknownBinding && bytes.Contains(routeBody, []byte(`"thoughtSignature"`)) {
				// 无缓存绑定但请求里已有 thoughtSignature：常见于缓存丢失/TTL 过期后，客户端继续携带旧签名。
				// 为避免第一次转发就 400，这里做一次确定性清理，让新账号重新生成签名链路。
				reqLog.Info("gemini.sticky_session_binding_missing",
					zap.Bool("clean_thought_signature", true),
				)
				routeBody = service.CleanGeminiNativeThoughtSignatures(routeBody)
				cleanedForUnknownBinding = true
				lastAttemptAccountID = account.ID
			} else if lastAttemptAccountID == 0 {
				// 记录本次请求中首次选择到的账号，便于同一请求内 failover 时检测切换。
				lastAttemptAccountID = account.ID
			}

			// 4) account concurrency slot
			accountReleaseFunc := selection.ReleaseFunc
			if !selection.Acquired {
				if selection.WaitPlan == nil {
					googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts")
					return
				}
				if !streamStarted && routeCursor.skipToNext("account_slot_unavailable", reqLog, zap.Int64("account_id", account.ID)) {
					continue geminiNativeRouteLoop
				}
				accountWaitCounted := false
				canWait, err := geminiConcurrency.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
				if err != nil {
					reqLog.Warn("gemini.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					if failoverClientGone(c) {
						return
					}
					googleError(c, http.StatusServiceUnavailable, "Account concurrency service is temporarily unavailable")
					return
				} else if !canWait {
					reqLog.Info("gemini.account_wait_queue_full",
						zap.Int64("account_id", account.ID),
						zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
					)
					if !streamStarted && routeCursor.skipToNext("account_wait_queue_full", reqLog, zap.Int64("account_id", account.ID)) {
						continue geminiNativeRouteLoop
					}
					googleError(c, http.StatusTooManyRequests, "Too many pending requests, please retry later")
					return
				}
				if err == nil && canWait {
					accountWaitCounted = true
				}
				releaseWait := func() {
					if accountWaitCounted {
						geminiConcurrency.DecrementAccountWaitCount(c.Request.Context(), account.ID)
						accountWaitCounted = false
					}
				}

				accountReleaseFunc, err = geminiConcurrency.AcquireAccountSlotWithWaitTimeout(
					c,
					account.ID,
					selection.WaitPlan.MaxConcurrency,
					selection.WaitPlan.Timeout,
					stream,
					&streamStarted,
				)
				if err != nil {
					reqLog.Warn("gemini.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					releaseWait()
					if failoverClientGone(c) {
						return
					}
					switch {
					case errors.Is(err, service.ErrOpenAIFirstOutputRoutingBudgetExceeded):
						googleError(c, http.StatusGatewayTimeout, "Gateway routing budget expired before an upstream attempt could start")
					case isAccountSlotCapacityError(err):
						if !streamStarted && routeCursor.skipToNext("account_slot_acquire_timeout", reqLog, zap.Int64("account_id", account.ID)) {
							continue geminiNativeRouteLoop
						}
						googleError(c, http.StatusTooManyRequests, "Account concurrency limit exceeded, please retry later")
					default:
						googleError(c, http.StatusServiceUnavailable, "Account concurrency service is temporarily unavailable")
					}
					return
				}
				releaseWait()
				if err := h.gatewayService.BindStickySession(c.Request.Context(), currentAPIKey.GroupID, sessionKey, account.ID); err != nil {
					reqLog.Warn("gemini.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}
			// 账号槽位/等待计数需要在超时或断开时安全回收
			accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

			// 5) forward (根据平台分流)
			var result *service.ForwardResult
			requestCtx := c.Request.Context()
			if fs.SwitchCount > 0 {
				requestCtx = service.WithAccountSwitchCount(requestCtx, fs.SwitchCount, h.metadataBridgeEnabled())
			}
			writerSizeBeforeForward := c.Writer.Size()
			if account.Platform == service.PlatformAntigravity && account.Type != service.AccountTypeAPIKey {
				result, err = h.antigravityGatewayService.ForwardGemini(requestCtx, c, account, routeModel, action, stream, routeBody, hasBoundSession)
			} else {
				result, err = h.geminiCompatService.ForwardNative(requestCtx, c, account, routeModel, action, stream, routeBody)
			}
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			h.gatewayService.ReportAccountForwardResult(account.ID, result, err)
			if err != nil {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if c.Writer.Size() != writerSizeBeforeForward && !failoverErr.SafeToFailoverAfterWrite {
						h.handleGeminiFailoverExhausted(c, failoverErr)
						return
					}
					failoverAction := fs.HandleFailoverErrorWithRetryLimit(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
					switch failoverAction {
					case FailoverContinue:
						continue
					case FailoverExhausted:
						if canSwitchAPIKeyGroupRouteAfterForward(c, routeCursor, fs.LastFailoverErr, streamStarted, writerSizeBeforeForward) &&
							routeCursor.switchToNext(apiKey.ID, "upstream_failover_exhausted", reqLog, zap.Int("upstream_status", fs.LastFailoverErr.StatusCode)) {
							continue geminiNativeRouteLoop
						}
						h.handleGeminiFailoverExhausted(c, fs.LastFailoverErr)
						return
					case FailoverCanceled:
						failoverClientGone(c)
						return
					}
				}
				// ForwardNative already wrote the response
				reqLog.Error("gemini.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				return
			}

			// 捕获请求信息（用于异步记录，避免在 goroutine 中访问 gin.Context）
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetSecurityClientIP(c)

			// 保存 Gemini 内容摘要会话（用于 Fallback 匹配）
			if useDigestFallback && geminiDigestChain != "" && geminiPrefixHash != "" {
				if err := h.gatewayService.SaveGeminiSession(
					c.Request.Context(),
					derefGroupID(currentAPIKey.GroupID),
					geminiPrefixHash,
					geminiDigestChain,
					geminiSessionUUID,
					account.ID,
					matchedDigestChain,
				); err != nil {
					reqLog.Warn("gemini.digest_session_save_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			// 使用量记录通过有界 worker 池提交，避免请求热路径创建无界 goroutine。
			requestPayloadHash := service.HashUsageRequestPayload(routeBody)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
			forceCacheBilling := fs.ForceCacheBilling
			usageAPIKey := currentAPIKey
			usageSubscription := currentSubscription
			h.submitUsageRecordTask(requestCtx, func(ctx context.Context) {
				if err := h.gatewayService.RecordUsageWithLongContext(ctx, &service.RecordUsageLongContextInput{
					Result:                result,
					APIKey:                usageAPIKey,
					User:                  usageAPIKey.User,
					Account:               account,
					Subscription:          usageSubscription,
					InboundEndpoint:       inboundEndpoint,
					UpstreamEndpoint:      upstreamEndpoint,
					UserAgent:             userAgent,
					IPAddress:             clientIP,
					RequestPayloadHash:    requestPayloadHash,
					LongContextThreshold:  200000, // Gemini 200K 阈值
					LongContextMultiplier: 2.0,    // 超出部分双倍计费
					ForceCacheBilling:     forceCacheBilling,
					APIKeyService:         h.apiKeyService,
					ChannelUsageFields:    channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.gemini_v1beta.models"),
						zap.Int64("user_id", authSubject.UserID),
						zap.Int64("api_key_id", usageAPIKey.ID),
						zap.Any("group_id", usageAPIKey.GroupID),
						zap.String("model", routeModel),
						zap.Int64("account_id", account.ID),
					).Error("gemini.record_usage_failed", zap.Error(err))
				}
			})
			reqLog.Debug("gemini.request_completed",
				zap.Int64("account_id", account.ID),
				zap.Int("switch_count", fs.SwitchCount),
			)
			routeCursor.recordSuccess(apiKey.ID)
			if sessionKey != "" && (sessionBoundAccountID == 0 || sessionBoundAccountID == account.ID) {
				if err := h.gatewayService.BindStickySession(c.Request.Context(), currentAPIKey.GroupID, sessionKey, account.ID); err != nil {
					reqLog.Warn("gemini.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}
			return
		}
	}
}

func parseGeminiModelAction(rest string) (model string, action string, err error) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", &pathParseError{"missing path"}
	}

	// Standard: {model}:{action}
	if i := strings.Index(rest, ":"); i > 0 && i < len(rest)-1 {
		return rest[:i], rest[i+1:], nil
	}

	// Fallback: {model}/{action}
	if i := strings.Index(rest, "/"); i > 0 && i < len(rest)-1 {
		return rest[:i], rest[i+1:], nil
	}

	return "", "", &pathParseError{"invalid model action path"}
}

func (h *GatewayHandler) handleGeminiFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError) {
	if failoverErr == nil {
		googleError(c, http.StatusBadGateway, "Upstream request failed")
		return
	}

	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody

	// 先检查透传规则
	if h.errorPassthroughService != nil && len(responseBody) > 0 {
		if rule := h.errorPassthroughService.MatchRule(service.PlatformGemini, statusCode, responseBody); rule != nil {
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

			googleError(c, respCode, msg)
			return
		}
	}

	// 记录原始上游状态码，以便 ops 错误日志捕获真实的上游错误
	upstreamMsg := service.ExtractUpstreamErrorMessage(responseBody)
	service.SetOpsUpstreamError(c, statusCode, upstreamMsg, "")

	// 使用默认的错误映射
	status, message := mapGeminiUpstreamError(statusCode)
	googleError(c, status, message)
}

func mapGeminiUpstreamError(statusCode int) (int, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "Upstream rate limit exceeded, please retry later"
	case 529:
		return http.StatusServiceUnavailable, "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "Upstream request failed"
	}
}

type pathParseError struct{ msg string }

func (e *pathParseError) Error() string { return e.msg }

func googleError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    status,
			"message": message,
			"status":  googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
}

func writeUpstreamResponse(c *gin.Context, res *service.UpstreamHTTPResult) {
	if res == nil {
		googleError(c, http.StatusBadGateway, "Empty upstream response")
		return
	}
	for k, vv := range res.Headers {
		// Avoid overriding content-length and hop-by-hop headers.
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Transfer-Encoding") || strings.EqualFold(k, "Connection") {
			continue
		}
		for _, v := range vv {
			c.Writer.Header().Add(k, v)
		}
	}
	contentType := res.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(res.StatusCode, contentType, res.Body)
}

func shouldFallbackGeminiModels(res *service.UpstreamHTTPResult) bool {
	if res == nil {
		return true
	}
	if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusForbidden {
		return false
	}
	if strings.Contains(strings.ToLower(res.Headers.Get("Www-Authenticate")), "insufficient_scope") {
		return true
	}
	if strings.Contains(strings.ToLower(string(res.Body)), "insufficient authentication scopes") {
		return true
	}
	if strings.Contains(strings.ToLower(string(res.Body)), "access_token_scope_insufficient") {
		return true
	}
	return false
}

func shouldFallbackGeminiModel(modelName string, res *service.UpstreamHTTPResult) bool {
	if shouldFallbackGeminiModels(res) {
		return true
	}
	if res == nil || res.StatusCode != http.StatusNotFound {
		return false
	}
	return gemini.HasFallbackModel(modelName)
}

// extractGeminiCLISessionHash 从 Gemini CLI 请求中提取会话标识。
// 组合 x-gemini-api-privileged-user-id header 和请求体中的 tmp 目录哈希。
//
// 会话标识生成策略：
//  1. 从请求体中提取 tmp 目录哈希（64位十六进制）
//  2. 从 header 中提取 privileged-user-id（UUID）
//  3. 组合两者生成 SHA256 哈希作为最终的会话标识
//
// 如果找不到 tmp 目录哈希，返回空字符串（不使用粘性会话）。
//
// extractGeminiCLISessionHash extracts session identifier from Gemini CLI requests.
// Combines x-gemini-api-privileged-user-id header with tmp directory hash from request body.
func extractGeminiCLISessionHash(c *gin.Context, body []byte) string {
	// 1. 从请求体中提取 tmp 目录哈希
	match := geminiCLITmpDirRegex.FindSubmatch(body)
	if len(match) < 2 {
		return "" // 没有找到 tmp 目录，不使用粘性会话
	}
	tmpDirHash := string(match[1])

	// 2. 提取 privileged-user-id
	privilegedUserID := strings.TrimSpace(c.GetHeader("x-gemini-api-privileged-user-id"))

	// 3. 组合生成最终的 session hash
	if privilegedUserID != "" {
		// 组合两个标识符：privileged-user-id + tmp 目录哈希
		combined := privilegedUserID + ":" + tmpDirHash
		hash := sha256.Sum256([]byte(combined))
		return hex.EncodeToString(hash[:])
	}

	// 如果没有 privileged-user-id，直接使用 tmp 目录哈希
	return tmpDirHash
}

// truncateDigestChain 截断摘要链用于日志显示
func truncateDigestChain(chain string) string {
	if len(chain) <= 50 {
		return chain
	}
	return chain[:50] + "..."
}

// safeShortPrefix 返回字符串前 n 个字符；长度不足时返回原字符串。
// 用于日志展示，避免切片越界。
func safeShortPrefix(value string, n int) string {
	if n <= 0 || len(value) <= n {
		return value
	}
	return value[:n]
}

// derefGroupID 安全解引用 *int64，nil 返回 0
func derefGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}
