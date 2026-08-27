package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func openAIAccountShareModeRequestContext(c *gin.Context, apiKey *service.APIKey) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	ctx := c.Request.Context()
	if apiKey == nil || apiKey.ID <= 0 {
		return ctx
	}
	userID := apiKey.UserID
	if apiKey.User != nil && apiKey.User.ID > 0 {
		userID = apiKey.User.ID
	}
	if userID <= 0 {
		return ctx
	}
	return service.WithAccountShareModeRequest(ctx, userID, apiKey.ID)
}

func openAICompatibleRoutingPlatform(apiKey *service.APIKey) string {
	if apiKey != nil && apiKey.Group != nil {
		switch apiKey.Group.Platform {
		case service.PlatformGrok:
			return service.PlatformGrok
		case service.PlatformOpencode:
			return service.PlatformOpencode
		}
	}
	return service.PlatformOpenAI
}

func openAICompatibleRequestContext(ctx context.Context, apiKey *service.APIKey) context.Context {
	routingPlatform := openAICompatibleRoutingPlatform(apiKey)
	if routingPlatform != service.PlatformGrok && routingPlatform != service.PlatformOpencode {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.ForcePlatform, routingPlatform)
}

// openAIResponsesDispatchContext removes the routing-only deadline before the
// upstream attempt while retaining request cancellation and route-scoped values.
func openAIResponsesDispatchContext(c *gin.Context, routingCtx context.Context, apiKey *service.APIKey) context.Context {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	ctx = service.WithAccountShareModeRequestFromContext(ctx, routingCtx)
	return openAICompatibleRequestContext(ctx, apiKey)
}

type accountShareModeHTTPErrorDetails struct {
	status        int
	openAIType    string
	anthropicType string
	message       string
	retryAfter    int
}

func classifyAccountShareModeHTTPError(err error) (accountShareModeHTTPErrorDetails, bool) {
	switch {
	case errors.Is(err, service.ErrAccountShareMembershipIdleTimeout):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusConflict,
			openAIType:    "account_share_idle_timeout",
			anthropicType: "invalid_request_error",
			message:       "账号房间绑定已因空闲超时结束，请重新加入房间",
		}, true
	case errors.Is(err, service.ErrAccountShareModeGroupUnbound):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusBadRequest,
			openAIType:    "account_share_mode_unbound",
			anthropicType: "invalid_request_error",
			message:       "该分组未绑定账号",
		}, true
	case errors.Is(err, service.ErrAccountShareModeRecovering):
		retryAfter := service.RetryAfterSecondsFromError(err)
		if retryAfter <= 0 {
			retryAfter = service.AccountShareModeDefaultRecoveryRetryAfter
		}
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusServiceUnavailable,
			openAIType:    "account_share_recovering",
			anthropicType: "api_error",
			message:       "共享账号正在恢复，请稍后重试",
			retryAfter:    retryAfter,
		}, true
	case errors.Is(err, service.ErrAccountShareMembershipEnding):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusConflict,
			openAIType:    "account_share_membership_ending",
			anthropicType: "invalid_request_error",
			message:       "上一个房间的退出结算尚未完成，请稍候再发起请求",
		}, true
	case errors.Is(err, service.ErrAccountShareBalanceBelowMinimum):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusForbidden,
			openAIType:    "account_share_balance_below_minimum",
			anthropicType: "permission_error",
			message:       "账户余额低于共享账号最低准入余额",
		}, true
	case errors.Is(err, service.ErrAccountSharePerUserConcurrencyExceeded):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusTooManyRequests,
			openAIType:    "account_share_concurrency_exceeded",
			anthropicType: "rate_limit_error",
			message:       "共享账号单用户并发已达上限",
		}, true
	case errors.Is(err, service.ErrAccountShareModeUnsupportedModel):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusBadRequest,
			openAIType:    "account_share_model_unsupported",
			anthropicType: "invalid_request_error",
			message:       "模型不支持",
		}, true
	case errors.Is(err, service.ErrAccountShareModeSelection):
		return accountShareModeHTTPErrorDetails{
			status:        http.StatusServiceUnavailable,
			openAIType:    "account_share_unavailable",
			anthropicType: "api_error",
			message:       "共享账号暂时不可用，请稍后重试",
		}, true
	default:
		return accountShareModeHTTPErrorDetails{}, false
	}
}

func applyAccountShareModeRetryAfter(c *gin.Context, details accountShareModeHTTPErrorDetails) {
	if c == nil || details.retryAfter <= 0 || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.Header("Retry-After", strconv.Itoa(details.retryAfter))
}

func (h *OpenAIGatewayHandler) handleAccountShareModeSelectionError(c *gin.Context, err error, streamStarted bool) bool {
	details, ok := classifyAccountShareModeHTTPError(err)
	if !ok {
		return false
	}
	applyAccountShareModeRetryAfter(c, details)
	h.handleStreamingAwareError(c, details.status, details.openAIType, details.message, streamStarted)
	return true
}

func (h *OpenAIGatewayHandler) handleAccountShareModeAnthropicError(c *gin.Context, err error, streamStarted bool) bool {
	details, ok := classifyAccountShareModeHTTPError(err)
	if !ok {
		return false
	}
	applyAccountShareModeRetryAfter(c, details)
	h.anthropicStreamingAwareError(c, details.status, details.anthropicType, details.message, streamStarted)
	return true
}
