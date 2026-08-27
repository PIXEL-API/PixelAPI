package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClassifyAccountShareModeHTTPErrorPreservesSpecificContract(t *testing.T) {
	wrapSelection := func(err error) error {
		return fmt.Errorf("%w: %w", service.ErrAccountShareModeSelection, err)
	}
	recovering := service.NewAccountShareModeRecoveringError(17)

	tests := []struct {
		name          string
		err           error
		status        int
		openAIType    string
		anthropicType string
		message       string
		retryAfter    int
	}{
		{
			name:          "plain true unbound",
			err:           service.ErrAccountShareModeGroupUnbound,
			status:        http.StatusBadRequest,
			openAIType:    "account_share_mode_unbound",
			anthropicType: "invalid_request_error",
			message:       "该分组未绑定账号",
		},
		{
			name:          "wrapped true unbound wins over broad selection",
			err:           wrapSelection(service.ErrAccountShareModeGroupUnbound),
			status:        http.StatusBadRequest,
			openAIType:    "account_share_mode_unbound",
			anthropicType: "invalid_request_error",
			message:       "该分组未绑定账号",
		},
		{
			name:          "plain recovering exposes safe retry after",
			err:           recovering,
			status:        http.StatusServiceUnavailable,
			openAIType:    "account_share_recovering",
			anthropicType: "api_error",
			message:       "共享账号正在恢复，请稍后重试",
			retryAfter:    17,
		},
		{
			name:          "wrapped recovering wins over broad selection",
			err:           wrapSelection(recovering),
			status:        http.StatusServiceUnavailable,
			openAIType:    "account_share_recovering",
			anthropicType: "api_error",
			message:       "共享账号正在恢复，请稍后重试",
			retryAfter:    17,
		},
		{
			name:          "idle timeout requires rejoin",
			err:           service.ErrAccountShareMembershipIdleTimeout,
			status:        http.StatusConflict,
			openAIType:    "account_share_idle_timeout",
			anthropicType: "invalid_request_error",
			message:       "账号房间绑定已因空闲超时结束，请重新加入房间",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			details, handled := classifyAccountShareModeHTTPError(test.err)

			require.True(t, handled)
			require.Equal(t, test.status, details.status)
			require.Equal(t, test.openAIType, details.openAIType)
			require.Equal(t, test.anthropicType, details.anthropicType)
			require.Equal(t, test.message, details.message)
			require.Equal(t, test.retryAfter, details.retryAfter)
		})
	}
}

func TestAccountShareModeWSCloseDetailsSeparatesUnboundFromRecovering(t *testing.T) {
	wrapSelection := func(err error) error {
		return fmt.Errorf("%w: %w", service.ErrAccountShareModeSelection, err)
	}
	tests := []struct {
		name   string
		err    error
		status coderws.StatusCode
		reason string
	}{
		{
			name:   "true unbound is a policy violation",
			err:    wrapSelection(service.ErrAccountShareModeGroupUnbound),
			status: coderws.StatusPolicyViolation,
			reason: "该分组未绑定账号",
		},
		{
			name:   "recovering asks the client to retry later",
			err:    wrapSelection(service.NewAccountShareModeRecoveringError(11)),
			status: coderws.StatusTryAgainLater,
			reason: "共享账号正在恢复，请稍后重试",
		},
		{
			name:   "unsupported model is a policy violation",
			err:    wrapSelection(service.ErrAccountShareModeUnsupportedModel),
			status: coderws.StatusPolicyViolation,
			reason: "模型不支持",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, reason, handled := accountShareModeWSCloseDetails(test.err)

			require.True(t, handled)
			require.Equal(t, test.status, status)
			require.Equal(t, test.reason, reason)
		})
	}
}

func TestHandleAccountShareModeSelectionErrorWritesSafeHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIGatewayHandler{}

	tests := []struct {
		name       string
		err        error
		status     int
		retryAfter string
		errType    string
		message    string
	}{
		{
			name:       "recovering",
			err:        fmt.Errorf("account_id=797016 membership_id=73814 upstream=secret: %w", service.NewAccountShareModeRecoveringError(17)),
			status:     http.StatusServiceUnavailable,
			retryAfter: "17",
			errType:    "account_share_recovering",
			message:    "共享账号正在恢复，请稍后重试",
		},
		{
			name:    "true unbound",
			err:     fmt.Errorf("internal binding detail: %w", service.ErrAccountShareModeGroupUnbound),
			status:  http.StatusBadRequest,
			errType: "account_share_mode_unbound",
			message: "该分组未绑定账号",
		},
		{
			name:    "idle timeout",
			err:     fmt.Errorf("membership_id=73814: %w", service.ErrAccountShareMembershipIdleTimeout),
			status:  http.StatusConflict,
			errType: "account_share_idle_timeout",
			message: "账号房间绑定已因空闲超时结束，请重新加入房间",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)

			require.True(t, handler.handleAccountShareModeSelectionError(ctx, test.err, false))
			require.Equal(t, test.status, recorder.Code)
			require.Equal(t, test.retryAfter, recorder.Header().Get("Retry-After"))
			require.JSONEq(t, fmt.Sprintf(
				`{"error":{"type":%q,"message":%q}}`,
				test.errType,
				test.message,
			), recorder.Body.String())
			for _, secret := range []string{"797016", "73814", "upstream=secret", "internal binding detail"} {
				require.NotContains(t, recorder.Body.String(), secret)
			}
		})
	}
}

func TestHandleAccountShareModeAnthropicErrorWritesSafeHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	handler := &OpenAIGatewayHandler{}
	err := fmt.Errorf("account_id=797016 upstream=secret: %w", service.NewAccountShareModeRecoveringError(23))

	require.True(t, handler.handleAccountShareModeAnthropicError(ctx, err, false))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "23", recorder.Header().Get("Retry-After"))
	require.JSONEq(t,
		`{"type":"error","error":{"type":"api_error","message":"共享账号正在恢复，请稍后重试"}}`,
		recorder.Body.String(),
	)
	require.NotContains(t, recorder.Body.String(), "797016")
	require.NotContains(t, recorder.Body.String(), "upstream=secret")
}

func TestAccountShareModeWSCloseReasonDoesNotExposeCause(t *testing.T) {
	cause := fmt.Errorf("account_id=797016 membership_id=73814 upstream=secret: %w", service.NewAccountShareModeRecoveringError(11))
	status, reason, handled := accountShareModeWSCloseDetails(cause)

	require.True(t, handled)
	require.Equal(t, coderws.StatusTryAgainLater, status)
	require.Equal(t, "共享账号正在恢复，请稍后重试", reason)
	for _, secret := range []string{"797016", "73814", "upstream=secret"} {
		require.False(t, strings.Contains(reason, secret))
	}
}
