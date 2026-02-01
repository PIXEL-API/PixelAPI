package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SoraGatewayHandler handles Sora chat completions requests
type SoraGatewayHandler struct {
	gatewayService      *service.GatewayService
	soraGatewayService  *service.SoraGatewayService
	billingCacheService *service.BillingCacheService
	concurrencyHelper   *ConcurrencyHelper
	maxAccountSwitches  int
	streamMode          string
	soraMediaSigningKey string
	soraMediaRoot       string
}

// NewSoraGatewayHandler creates a new SoraGatewayHandler
func NewSoraGatewayHandler(
	gatewayService *service.GatewayService,
	soraGatewayService *service.SoraGatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	cfg *config.Config,
) *SoraGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	streamMode := "force"
	signKey := ""
	mediaRoot := "/app/data/sora"
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
		if mode := strings.TrimSpace(cfg.Gateway.SoraStreamMode); mode != "" {
			streamMode = mode
		}
		signKey = strings.TrimSpace(cfg.Gateway.SoraMediaSigningKey)
		if root := strings.TrimSpace(cfg.Sora.Storage.LocalPath); root != "" {
			mediaRoot = root
		}
	}
	return &SoraGatewayHandler{
		gatewayService:      gatewayService,
		soraGatewayService:  soraGatewayService,
		billingCacheService: billingCacheService,
		concurrencyHelper:   NewConcurrencyHelper(concurrencyService, SSEPingFormatComment, pingInterval),
		maxAccountSwitches:  maxAccountSwitches,
		streamMode:          strings.ToLower(streamMode),
		soraMediaSigningKey: signKey,
		soraMediaRoot:       mediaRoot,
	}
}

// ChatCompletions handles Sora /v1/chat/completions endpoint
func (h *SoraGatewayHandler) ChatCompletions(c *gin.Context) {
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

	body, err := io.ReadAll(c.Request.Body)
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

	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	reqModel, _ := reqBody["model"].(string)
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqMessages, _ := reqBody["messages"].([]any)
	if len(reqMessages) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}

	clientStream, _ := reqBody["stream"].(bool)
	if !clientStream {
		if h.streamMode == "error" {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Sora requires stream=true")
			return
		}
		reqBody["stream"] = true
		updated, err := json.Marshal(reqBody)
		if err != nil {
			h.errorResponse(c, http.StatusInternalServerError, "api_error", "Failed to process request")
			return
		}
		body = updated
	}

	setOpsRequestContext(c, reqModel, clientStream, body)

	platform := ""
	if forced, ok := middleware2.GetForcePlatformFromContext(c); ok {
		platform = forced
	} else if apiKey.Group != nil {
		platform = apiKey.Group.Platform
	}
	if platform != service.PlatformSora {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This endpoint only supports Sora platform")
		return
	}

	streamStarted := false
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := h.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err != nil {
		log.Printf("Increment wait count failed: %v", err)
	} else if !canWait {
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, clientStream, &streamStarted)
	if err != nil {
		log.Printf("User concurrency acquire failed: %v", err)
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	if waitCounted {
		h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription); err != nil {
		log.Printf("Billing eligibility check failed after wait: %v", err)
		status, code, message := billingErrorDetails(err)
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	sessionHash := generateOpenAISessionHash(c, reqBody)

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	lastFailoverStatus := 0

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, reqModel, failedAccountIDs, "")
		if err != nil {
			log.Printf("[Sora Handler] SelectAccount failed: %v", err)
			if len(failedAccountIDs) == 0 {
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts: "+err.Error(), streamStarted)
				return
			}
			h.handleFailoverExhausted(c, lastFailoverStatus, streamStarted)
			return
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID)

		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
				return
			}
			accountWaitCounted := false
			canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
			if err != nil {
				log.Printf("Increment account wait count failed: %v", err)
			} else if !canWait {
				log.Printf("Account wait queue full: account=%d", account.ID)
				h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
				return
			}
			if err == nil && canWait {
				accountWaitCounted = true
			}
			defer func() {
				if accountWaitCounted {
					h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
				}
			}()

			accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				clientStream,
				&streamStarted,
			)
			if err != nil {
				log.Printf("Account concurrency acquire failed: %v", err)
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
			if accountWaitCounted {
				h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
				accountWaitCounted = false
			}
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		result, err := h.soraGatewayService.Forward(c.Request.Context(), c, account, body, clientStream)
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				failedAccountIDs[account.ID] = struct{}{}
				if switchCount >= maxAccountSwitches {
					lastFailoverStatus = failoverErr.StatusCode
					h.handleFailoverExhausted(c, lastFailoverStatus, streamStarted)
					return
				}
				lastFailoverStatus = failoverErr.StatusCode
				switchCount++
				log.Printf("Account %d: upstream error %d, switching account %d/%d", account.ID, failoverErr.StatusCode, switchCount, maxAccountSwitches)
				continue
			}
			log.Printf("Account %d: Forward request failed: %v", account.ID, err)
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)

		go func(result *service.ForwardResult, usedAccount *service.Account, ua, ip string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:       result,
				APIKey:       apiKey,
				User:         apiKey.User,
				Account:      usedAccount,
				Subscription: subscription,
				UserAgent:    ua,
				IPAddress:    ip,
			}); err != nil {
				log.Printf("Record usage failed: %v", err)
			}
		}(result, account, userAgent, clientIP)
		return
	}
}

func generateOpenAISessionHash(c *gin.Context, reqBody map[string]any) string {
	if c == nil {
		return ""
	}
	sessionID := strings.TrimSpace(c.GetHeader("session_id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(c.GetHeader("conversation_id"))
	}
	if sessionID == "" && reqBody != nil {
		if v, ok := reqBody["prompt_cache_key"].(string); ok {
			sessionID = strings.TrimSpace(v)
		}
	}
	if sessionID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(hash[:])
}

func (h *SoraGatewayHandler) handleConcurrencyError(c *gin.Context, err error, slotType string, streamStarted bool) {
	h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error",
		fmt.Sprintf("Concurrency limit exceeded for %s, please retry later", slotType), streamStarted)
}

func (h *SoraGatewayHandler) handleFailoverExhausted(c *gin.Context, statusCode int, streamStarted bool) {
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

func (h *SoraGatewayHandler) mapUpstreamError(statusCode int) (int, string, string) {
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

func (h *SoraGatewayHandler) handleStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if streamStarted {
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			errorEvent := fmt.Sprintf(`event: error`+"\n"+`data: {"error": {"type": "%s", "message": "%s"}}`+"\n\n", errType, message)
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}
	h.errorResponse(c, status, errType, message)
}

func (h *SoraGatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// MediaProxy serves local Sora media files.
func (h *SoraGatewayHandler) MediaProxy(c *gin.Context) {
	h.proxySoraMedia(c, false)
}

// MediaProxySigned serves local Sora media files with signature verification.
func (h *SoraGatewayHandler) MediaProxySigned(c *gin.Context) {
	h.proxySoraMedia(c, true)
}

func (h *SoraGatewayHandler) proxySoraMedia(c *gin.Context, requireSignature bool) {
	rawPath := c.Param("filepath")
	if rawPath == "" {
		c.Status(http.StatusNotFound)
		return
	}
	cleaned := path.Clean(rawPath)
	if !strings.HasPrefix(cleaned, "/image/") && !strings.HasPrefix(cleaned, "/video/") {
		c.Status(http.StatusNotFound)
		return
	}

	query := c.Request.URL.Query()
	if requireSignature {
		if h.soraMediaSigningKey == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"type":    "api_error",
					"message": "Sora 媒体签名未配置",
				},
			})
			return
		}
		expiresStr := strings.TrimSpace(query.Get("expires"))
		signature := strings.TrimSpace(query.Get("sig"))
		expires, err := strconv.ParseInt(expiresStr, 10, 64)
		if err != nil || expires <= time.Now().Unix() {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "Sora 媒体签名已过期",
				},
			})
			return
		}
		query.Del("sig")
		query.Del("expires")
		signingQuery := query.Encode()
		if !service.VerifySoraMediaURL(cleaned, signingQuery, expires, signature, h.soraMediaSigningKey) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "Sora 媒体签名无效",
				},
			})
			return
		}
	}
	if strings.TrimSpace(h.soraMediaRoot) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "api_error",
				"message": "Sora 媒体目录未配置",
			},
		})
		return
	}

	relative := strings.TrimPrefix(cleaned, "/")
	localPath := filepath.Join(h.soraMediaRoot, filepath.FromSlash(relative))
	if _, err := os.Stat(localPath); err != nil {
		if os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	c.File(localPath)
}
