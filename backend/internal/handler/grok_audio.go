package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GrokVoice handles /tts, /stt, and the custom-voices HTTP resource family.
func (h *OpenAIGatewayHandler) GrokVoice(c *gin.Context, endpoint string) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGrok {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Voice API is not supported for this platform")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.grok_voice", zap.String("endpoint", endpoint))
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readGrokVoiceGatewayBody(c)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if err := service.ValidateGrokAudioBillingPrice(apiKey.Group, endpoint); err != nil {
		reqLog.Warn("grok_voice.billing_configuration_unavailable", zap.Error(err))
		h.errorResponse(
			c,
			http.StatusServiceUnavailable,
			"billing_configuration_error",
			"Grok Voice billing price must be explicitly configured",
		)
		return
	}
	if endpoint == "tts" {
		if input := extractGrokTTSInputText(body); input != "" {
			auditBody, marshalErr := json.Marshal(map[string]any{
				"messages": []map[string]any{{"role": "user", "content": input}},
			})
			if marshalErr != nil {
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid TTS input")
				return
			}
			if decision := h.checkContentModeration(
				c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, "grok-voice-latest", auditBody,
			); decision != nil && decision.Blocked {
				h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
				return
			}
		}
	}

	requestCtx := context.WithValue(c.Request.Context(), ctxkey.ForcePlatform, service.PlatformGrok)
	c.Request = c.Request.WithContext(requestCtx)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(
		requestCtx, apiKey.User, apiKey, apiKey.Group, subscription,
	); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	streamStarted := false
	userRelease, acquired := h.acquireResponsesUserSlot(
		c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog,
	)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}

	sessionHash := service.GrokVoiceSessionHash(h.gatewayService.GenerateExplicitSessionHash(c, body))
	contentType := c.GetHeader("Content-Type")
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	maxSwitches := h.maxAccountSwitches
	if maxSwitches <= 0 {
		maxSwitches = 3
	}

	for switchCount := 0; ; {
		selection, _, selectErr := h.gatewayService.SelectAccountWithSchedulerForGrok(
			requestCtx,
			apiKey.GroupID,
			sessionHash,
			"grok-4.5",
			failedAccountIDs,
			"",
		)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available Grok accounts")
			}
			return
		}
		account := selection.Account
		freshAccount, accountRelease, accountAcquired, _ := h.acquireResponsesAccountSlot(
			c,
			requestCtx,
			apiKey.GroupID,
			sessionHash,
			service.OpenAIAccountDispatchRequirements{
				RequestedModel:             "grok-4.5",
				RequiredTransport:          service.OpenAIUpstreamTransportHTTPSSE,
				RequiredEndpointCapability: "",
				RequiredPlatform:           service.PlatformGrok,
			},
			selection,
			false,
			&streamStarted,
			nil,
			reqLog,
		)
		if !accountAcquired {
			return
		}
		account = freshAccount
		writerSizeBeforeForward := c.Writer.Size()
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			if accountRelease != nil {
				defer accountRelease()
			}
			return h.gatewayService.ForwardGrokVoice(
				requestCtx, c, account, endpoint, body, contentType,
			)
		}()
		if forwardErr == nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
			h.recordGrokVoiceUsage(c, apiKey, subject, account, subscription, endpoint, body, result)
			return
		}

		forwardErr = h.gatewayService.NormalizeGrokCredentialFailure(requestCtx, c, account, forwardErr)
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
			if c.Writer.Size() == writerSizeBeforeForward && !service.IsResponseCommitted(c) {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			return
		}
		if failoverErr.ShouldReportAccountScheduleFailure() {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
		}
		if c.Writer.Size() != writerSizeBeforeForward || !failoverErr.ShouldRetryNextAccount() {
			h.handleFailoverExhausted(c, failoverErr, c.Writer.Size() != writerSizeBeforeForward)
			return
		}
		if failoverErr.RetryableOnSameAccount {
			retryLimit := account.GetPoolModeRetryCount()
			if sameAccountRetryCount[account.ID] < retryLimit {
				sameAccountRetryCount[account.ID]++
				continue
			}
		}
		failedAccountIDs[account.ID] = struct{}{}
		lastFailoverErr = failoverErr
		if switchCount >= maxSwitches {
			h.handleFailoverExhausted(c, failoverErr, false)
			return
		}
		switchCount++
	}
}

// GrokRealtime exposes the native xAI Voice Realtime WebSocket.
func (h *OpenAIGatewayHandler) GrokRealtime(c *gin.Context) {
	if c == nil || c.Request == nil || !isOpenAIWSUpgradeRequest(c.Request) {
		h.errorResponse(c, http.StatusUpgradeRequired, "invalid_request_error", "WebSocket upgrade required (Upgrade: websocket)")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGrok {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Realtime API is not supported for this platform")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(c, "handler.openai_gateway.grok_realtime")
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	if err := service.ValidateGrokAudioBillingPrice(apiKey.Group, "realtime"); err != nil {
		reqLog.Warn("grok_realtime.billing_configuration_unavailable", zap.Error(err))
		h.errorResponse(
			c,
			http.StatusServiceUnavailable,
			"billing_configuration_error",
			"Grok Realtime billing price must be explicitly configured",
		)
		return
	}
	requestCtx := context.WithValue(c.Request.Context(), ctxkey.ForcePlatform, service.PlatformGrok)
	c.Request = c.Request.WithContext(requestCtx)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(
		requestCtx, apiKey.User, apiKey, apiKey.Group, subscription,
	); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	streamStarted := false
	userRelease, acquired := h.acquireResponsesUserSlot(
		c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog,
	)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		model = "grok-voice-latest"
	}
	maxSwitches := h.maxAccountSwitches
	if maxSwitches <= 0 {
		maxSwitches = 3
	}
	prepared, err := prepareGrokRealtimeClient(maxSwitches, grokRealtimePreAcceptOps{
		selectAccount: func(failedAccountIDs map[int64]struct{}) (*service.AccountSelectionResult, error) {
			selection, _, selectErr := h.gatewayService.SelectAccountWithSchedulerForGrok(
				requestCtx,
				apiKey.GroupID,
				"",
				"grok-4.5",
				failedAccountIDs,
				"",
			)
			return selection, selectErr
		},
		acquireAccount: func(selection *service.AccountSelectionResult) (*service.Account, func(), bool) {
			account, release, accountAcquired, _ := h.acquireResponsesAccountSlot(
				c,
				requestCtx,
				apiKey.GroupID,
				"",
				service.OpenAIAccountDispatchRequirements{
					RequestedModel:             "grok-4.5",
					RequiredTransport:          service.OpenAIUpstreamTransportHTTPSSE,
					RequiredEndpointCapability: "",
					RequiredPlatform:           service.PlatformGrok,
				},
				selection,
				false,
				&streamStarted,
				nil,
				reqLog,
			)
			return account, release, accountAcquired
		},
		getCredential: func(account *service.Account) (string, error) {
			token, _, credentialErr := h.gatewayService.GetRequestCredential(requestCtx, c, account)
			return token, credentialErr
		},
		reportFailure: func(accountID int64, failoverErr *service.UpstreamFailoverError) {
			if failoverErr.ShouldReportAccountScheduleFailure() {
				h.gatewayService.ReportOpenAIAccountScheduleResult(accountID, false, nil)
			}
		},
		accept: func() (*coderws.Conn, error) {
			return coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
				CompressionMode: coderws.CompressionContextTakeover,
			})
		},
	})
	if err != nil {
		if errors.Is(err, errGrokRealtimeNoAvailableAccounts) {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available Grok accounts")
		} else if !service.IsResponseCommitted(c) {
			h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Grok Realtime setup failed")
		}
		return
	}
	if prepared == nil {
		return
	}
	if prepared.exhausted != nil {
		h.handleFailoverExhausted(c, prepared.exhausted, false)
		return
	}
	if prepared.release != nil {
		defer prepared.release()
	}
	account := prepared.account
	token := prepared.token
	client := prepared.client
	defer func() { _ = client.CloseNow() }()

	started := time.Now()
	audioObserved, proxyErr := h.gatewayService.ProxyGrokRealtime(requestCtx, client, account, token, model)
	elapsed := time.Since(started)
	if proxyErr != nil {
		reqLog.Info("grok_realtime.proxy_failed", zap.Error(proxyErr))
		if !isExpectedGrokRealtimeClose(proxyErr) {
			_ = client.Close(coderws.StatusInternalError, "upstream realtime websocket failed")
			return
		}
	}
	if result := grokRealtimeBillingResult(model, elapsed, audioObserved); result != nil {
		h.recordGrokVoiceUsage(c, apiKey, subject, account, subscription, "realtime", nil, result)
	}
}

var errGrokRealtimeNoAvailableAccounts = errors.New("no available Grok realtime accounts")

type grokRealtimePreAcceptOps struct {
	selectAccount  func(failedAccountIDs map[int64]struct{}) (*service.AccountSelectionResult, error)
	acquireAccount func(selection *service.AccountSelectionResult) (*service.Account, func(), bool)
	getCredential  func(account *service.Account) (string, error)
	reportFailure  func(accountID int64, failoverErr *service.UpstreamFailoverError)
	accept         func() (*coderws.Conn, error)
}

type grokRealtimePreparedClient struct {
	account   *service.Account
	token     string
	client    *coderws.Conn
	release   func()
	exhausted *service.UpstreamFailoverError
}

// prepareGrokRealtimeClient completes every retryable account-auth step before
// accepting the client WebSocket. After accept returns, this function never
// selects another account: reconnecting a different upstream behind an already
// upgraded client connection would merge two independent realtime sessions.
func prepareGrokRealtimeClient(maxSwitches int, ops grokRealtimePreAcceptOps) (*grokRealtimePreparedClient, error) {
	if ops.selectAccount == nil || ops.acquireAccount == nil || ops.getCredential == nil || ops.accept == nil {
		return nil, errors.New("grok realtime pre-accept dependencies are incomplete")
	}
	if maxSwitches < 0 {
		maxSwitches = 0
	}

	failedAccountIDs := make(map[int64]struct{})
	var lastFailoverErr *service.UpstreamFailoverError
	for switchCount := 0; ; {
		selection, selectErr := ops.selectAccount(failedAccountIDs)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if lastFailoverErr != nil {
				return &grokRealtimePreparedClient{exhausted: lastFailoverErr}, nil
			}
			return nil, errGrokRealtimeNoAvailableAccounts
		}

		account, release, acquired := ops.acquireAccount(selection)
		if !acquired || account == nil {
			if release != nil {
				release()
			}
			return nil, nil
		}
		token, credentialErr := ops.getCredential(account)
		if credentialErr == nil {
			client, acceptErr := ops.accept()
			if acceptErr != nil {
				if release != nil {
					release()
				}
				return nil, acceptErr
			}
			if client == nil {
				if release != nil {
					release()
				}
				return nil, errors.New("grok realtime websocket accept returned nil client")
			}
			return &grokRealtimePreparedClient{
				account: account,
				token:   token,
				client:  client,
				release: release,
			}, nil
		}

		var failoverErr *service.UpstreamFailoverError
		if !errors.As(credentialErr, &failoverErr) {
			if release != nil {
				release()
			}
			return nil, credentialErr
		}
		if ops.reportFailure != nil {
			ops.reportFailure(account.ID, failoverErr)
		}
		if release != nil {
			release()
		}
		if !failoverErr.ShouldRetryNextAccount() {
			return &grokRealtimePreparedClient{exhausted: failoverErr}, nil
		}
		failedAccountIDs[account.ID] = struct{}{}
		lastFailoverErr = failoverErr
		if switchCount >= maxSwitches {
			return &grokRealtimePreparedClient{exhausted: failoverErr}, nil
		}
		switchCount++
	}
}

func grokRealtimeBillingResult(model string, elapsed time.Duration, audioObserved bool) *service.OpenAIForwardResult {
	if !audioObserved || elapsed <= 0 {
		return nil
	}
	return &service.OpenAIForwardResult{
		RequestID: service.StableGrokRealtimeBillingRequestID(""),
		Model:     model,
		Duration:  elapsed,
		AudioUsage: &service.AudioUsage{
			Mode: "realtime", DurationOrUnits: elapsed.Minutes(),
		},
	}
}

func isExpectedGrokRealtimeClose(err error) bool {
	if err == nil {
		return true
	}
	switch coderws.CloseStatus(err) {
	case coderws.StatusNormalClosure, coderws.StatusGoingAway,
		coderws.StatusNoStatusRcvd, coderws.StatusAbnormalClosure:
		return true
	default:
		return false
	}
}

func (h *OpenAIGatewayHandler) recordGrokVoiceUsage(
	c *gin.Context,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	account *service.Account,
	subscription *service.UserSubscription,
	endpoint string,
	body []byte,
	result *service.OpenAIForwardResult,
) {
	if h == nil || c == nil || apiKey == nil || account == nil || result == nil || result.AudioUsage == nil {
		return
	}
	if strings.TrimSpace(result.AudioUsage.Mode) == "realtime" {
		result.RequestID = service.StableGrokRealtimeBillingRequestID(result.RequestID)
	} else {
		result.RequestID = service.StableGrokAudioBillingRequestID(result.RequestID)
	}
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetSecurityClientIP(c)
	payloadHash := service.HashUsageRequestPayload(body)
	if payloadHash == "" {
		payloadHash = service.HashUsageRequestPayload([]byte(endpoint))
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	model := firstNonEmptyString(result.Model, endpoint)
	h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		ctx = context.WithValue(ctx, ctxkey.ForcePlatform, service.PlatformGrok)
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: payloadHash,
			APIKeyService:      h.apiKeyService,
			ChannelUsageFields: service.ChannelUsageFields{
				OriginalModel: model, ChannelMappedModel: model,
			},
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.grok_voice"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Int64("account_id", account.ID),
				zap.String("endpoint", endpoint),
			).Error("grok_voice.record_usage_failed", zap.Error(err))
		}
	})
}

func readGrokVoiceGatewayBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil {
		return nil, errors.New("request body is required")
	}
	if c.Request.Body == nil {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete {
			return nil, nil
		}
		return nil, errors.New("request body is required")
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		return nil, errors.New("failed to read request body")
	}
	if len(body) == 0 && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodDelete {
		return nil, errors.New("request body is required")
	}
	return body, nil
}

func extractGrokTTSInputText(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"input", "text", "prompt"} {
		if value, ok := payload[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}
