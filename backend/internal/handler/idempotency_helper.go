package handler

import (
	"context"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func executeUserIdempotentJSON(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) {
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		data, err := execute(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, data)
		return
	}

	actorScope := "user:0"
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		actorScope = "user:" + strconv.FormatInt(subject.UserID, 10)
	}

	result, err := coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope:          scope,
		ActorScope:     actorScope,
		Method:         c.Request.Method,
		Route:          c.FullPath(),
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
		Payload:        payload,
		RequireKey:     true,
		TTL:            ttl,
	}, execute)
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(service.ErrIdempotencyStoreUnavail) {
			service.RecordIdempotencyStoreUnavailable(c.FullPath(), scope, "handler_fail_close")
			logger.LegacyPrintf("handler.idempotency", "[Idempotency] store unavailable: method=%s route=%s scope=%s strategy=fail_close", c.Request.Method, c.FullPath(), scope)
		}
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result.Data)
}

// executeUserRequiredIdempotentJSON is reserved for mutations that cannot be
// safely repeated (for example, consuming a one-time OAuth authorization
// code). Unlike the compatibility helper above, it fails closed when the key,
// coordinator, or durable idempotency store is unavailable.
func executeUserRequiredIdempotentJSON(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context, string) (any, error),
	respond func(*gin.Context, any),
) {
	executeUserRequiredIdempotentJSONWithKey(
		c,
		c.GetHeader("Idempotency-Key"),
		scope,
		payload,
		ttl,
		execute,
		respond,
	)
}

func executeUserRequiredIdempotentJSONWithKey(
	c *gin.Context,
	rawIdempotencyKey string,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context, string) (any, error),
	respond func(*gin.Context, any),
) {
	idempotencyKey, err := service.NormalizeIdempotencyKey(rawIdempotencyKey)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if idempotencyKey == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		service.RecordIdempotencyStoreUnavailable(c.FullPath(), scope, "coordinator_nil")
		response.ErrorFrom(c, service.ErrIdempotencyStoreUnavail)
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, err := coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope:          scope,
		ActorScope:     "user:" + strconv.FormatInt(subject.UserID, 10),
		Method:         c.Request.Method,
		Route:          c.FullPath(),
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		RequireKey:     true,
		TTL:            ttl,
	}, func(ctx context.Context) (any, error) {
		return execute(ctx, idempotencyKey)
	})
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(service.ErrIdempotencyStoreUnavail) {
			service.RecordIdempotencyStoreUnavailable(c.FullPath(), scope, "handler_fail_close")
			logger.LegacyPrintf(
				"handler.idempotency",
				"[Idempotency] store unavailable: method=%s route=%s scope=%s strategy=fail_close",
				c.Request.Method,
				c.FullPath(),
				scope,
			)
		}
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	var data any
	if result != nil {
		data = result.Data
	}
	if respond == nil {
		response.Success(c, data)
		return
	}
	respond(c, data)
}
