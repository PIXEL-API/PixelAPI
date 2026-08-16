package handler

import (
	"context"
	"errors"
	"net/http"

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

func (h *OpenAIGatewayHandler) handleAccountShareModeSelectionError(c *gin.Context, err error, streamStarted bool) bool {
	switch {
	case errors.Is(err, service.ErrAccountShareMembershipEnding):
		h.handleStreamingAwareError(c, http.StatusConflict, "account_share_membership_ending", "上一个房间的退出结算尚未完成，请稍候再发起请求", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareModeGroupUnbound):
		h.handleStreamingAwareError(c, http.StatusBadRequest, "account_share_mode_unbound", "该分组未绑定账号", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareBalanceBelowMinimum):
		h.handleStreamingAwareError(c, http.StatusForbidden, "account_share_balance_below_minimum", "账户余额低于共享账号最低准入余额", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountSharePerUserConcurrencyExceeded):
		h.handleStreamingAwareError(c, http.StatusTooManyRequests, "account_share_concurrency_exceeded", "共享账号单用户并发已达上限", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareModeUnsupportedModel):
		h.handleStreamingAwareError(c, http.StatusBadRequest, "account_share_model_unsupported", "模型不支持", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareModeSelection):
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "account_share_unavailable", "共享账号暂时不可用，请稍后重试", streamStarted)
		return true
	default:
		return false
	}
}

func (h *OpenAIGatewayHandler) handleAccountShareModeAnthropicError(c *gin.Context, err error, streamStarted bool) bool {
	switch {
	case errors.Is(err, service.ErrAccountShareMembershipEnding):
		h.anthropicStreamingAwareError(c, http.StatusConflict, "invalid_request_error", "上一个房间的退出结算尚未完成，请稍候再发起请求", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareModeGroupUnbound):
		h.anthropicStreamingAwareError(c, http.StatusBadRequest, "invalid_request_error", "该分组未绑定账号", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareBalanceBelowMinimum):
		h.anthropicStreamingAwareError(c, http.StatusForbidden, "permission_error", "账户余额低于共享账号最低准入余额", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountSharePerUserConcurrencyExceeded):
		h.anthropicStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "共享账号单用户并发已达上限", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareModeUnsupportedModel):
		h.anthropicStreamingAwareError(c, http.StatusBadRequest, "invalid_request_error", "模型不支持", streamStarted)
		return true
	case errors.Is(err, service.ErrAccountShareModeSelection):
		h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "共享账号暂时不可用，请稍后重试", streamStarted)
		return true
	default:
		return false
	}
}
