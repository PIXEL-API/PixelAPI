package admin

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

type idempotencyStoreUnavailableMode int

const (
	idempotencyStoreUnavailableFailClose idempotencyStoreUnavailableMode = iota
	idempotencyStoreUnavailableFailOpen
)

func executeAdminIdempotent(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) (*service.IdempotencyExecuteResult, error) {
	return executeAdminIdempotentWithPolicy(c, scope, payload, ttl, false, execute)
}

func executeAdminStrictIdempotent(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) (*service.IdempotencyExecuteResult, error) {
	return executeAdminIdempotentWithPolicy(c, scope, payload, ttl, true, execute)
}

func executeAdminIdempotentWithPolicy(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	strict bool,
	execute func(context.Context) (any, error),
) (*service.IdempotencyExecuteResult, error) {
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		if strict {
			return nil, service.ErrIdempotencyStoreUnavail
		}
		data, err := execute(c.Request.Context())
		if err != nil {
			return nil, err
		}
		return &service.IdempotencyExecuteResult{Data: data}, nil
	}

	idempotencyKey := c.GetHeader("Idempotency-Key")
	if strict {
		normalizedKey, err := service.NormalizeIdempotencyKey(idempotencyKey)
		if err != nil {
			return nil, err
		}
		if normalizedKey == "" {
			return nil, service.ErrIdempotencyKeyRequired
		}
		idempotencyKey = normalizedKey
	}

	return coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope:          scope,
		ActorScope:     adminActorScope(c),
		Method:         c.Request.Method,
		Route:          c.FullPath(),
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		RequireKey:     true,
		TTL:            ttl,
	}, execute)
}

func adminActorScope(c *gin.Context) string {
	actorScope := "admin:0"
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		actorScope = "admin:" + strconv.FormatInt(subject.UserID, 10)
	}
	return actorScope
}

func executeAdminIdempotentJSON(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) {
	executeAdminIdempotentJSONWithMode(c, scope, payload, ttl, idempotencyStoreUnavailableFailClose, execute)
}

func executeAdminIdempotentJSONFailOpenOnStoreUnavailable(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) {
	executeAdminIdempotentJSONWithMode(c, scope, payload, ttl, idempotencyStoreUnavailableFailOpen, execute)
}

func executeAdminIdempotentJSONWithMode(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	mode idempotencyStoreUnavailableMode,
	execute func(context.Context) (any, error),
) {
	executeAdminIdempotentJSONWithPolicy(c, scope, payload, ttl, mode, false, execute)
}

func executeAdminStrictIdempotentJSON(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) {
	executeAdminIdempotentJSONWithPolicy(
		c,
		scope,
		payload,
		ttl,
		idempotencyStoreUnavailableFailClose,
		true,
		execute,
	)
}

func executeAdminIdempotentJSONWithPolicy(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	mode idempotencyStoreUnavailableMode,
	strict bool,
	execute func(context.Context) (any, error),
) {
	var (
		result *service.IdempotencyExecuteResult
		err    error
	)
	if strict {
		result, err = executeAdminStrictIdempotent(c, scope, payload, ttl, execute)
	} else {
		result, err = executeAdminIdempotent(c, scope, payload, ttl, execute)
	}
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(service.ErrIdempotencyStoreUnavail) {
			strategy := "fail_close"
			if mode == idempotencyStoreUnavailableFailOpen {
				strategy = "fail_open"
			}
			service.RecordIdempotencyStoreUnavailable(c.FullPath(), scope, "handler_"+strategy)
			logger.LegacyPrintf("handler.idempotency", "[Idempotency] store unavailable: method=%s route=%s scope=%s strategy=%s", c.Request.Method, c.FullPath(), scope, strategy)
			if mode == idempotencyStoreUnavailableFailOpen {
				data, fallbackErr := execute(c.Request.Context())
				if fallbackErr != nil {
					response.ErrorFrom(c, fallbackErr)
					return
				}
				c.Header("X-Idempotency-Degraded", "store-unavailable")
				response.Success(c, data)
				return
			}
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
