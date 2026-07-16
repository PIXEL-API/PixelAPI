package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestOpenAIHandleErrorResponse_LogsRedactedSingleLineBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, observed := observer.New(zapcore.DebugLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core, zap.AddStacktrace(zapcore.ErrorLevel)))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		LogUpstreamErrorBody:         true,
		LogUpstreamErrorBodyMaxBytes: 192,
	}}}
	respBody := []byte("{\n\"error\":{" +
		"\"message\":\"bad\"," +
		"\"access_token\":\"secret-access-token\"," +
		"\"api_key\":\"secret-api-key\"," +
		"\"authorization\":\"secret-authorization\"," +
		"\"x-api-key\":\"secret-x-api-key\"," +
		"\"secret\":\"secret-value\"," +
		"\"zz_detail\":\"" + strings.Repeat("中", 64) + "\"}}")
	resp := &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     http.Header{},
	}
	account := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	_, err := svc.handleErrorResponse(ctx, resp, c, account, nil, "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, rec.Code)

	entries := observed.FilterMessage("openai.upstream_error_body").All()
	require.Len(t, entries, 1)
	entry := entries[0]
	require.Equal(t, zapcore.WarnLevel, entry.Level)
	require.Empty(t, entry.Stack)

	fields := entry.ContextMap()
	require.Equal(t, "service.openai_gateway", fields["component"])
	require.EqualValues(t, http.StatusUnprocessableEntity, fields["upstream_status_code"])
	require.EqualValues(t, account.ID, fields["account_id"])
	require.Equal(t, account.Platform, fields["account_platform"])
	require.Equal(t, account.Type, fields["account_type"])

	loggedBody, ok := fields["upstream_error_body"].(string)
	require.True(t, ok)
	for _, secret := range []string{
		"secret-access-token",
		"secret-api-key",
		"secret-authorization",
		"secret-x-api-key",
		"secret-value",
	} {
		require.NotContains(t, loggedBody, secret)
	}
	require.Contains(t, loggedBody, "***")
	require.NotContains(t, loggedBody, "\n")
	require.NotContains(t, loggedBody, "\r")
	require.LessOrEqual(t, len(loggedBody), 192)
	require.True(t, utf8.ValidString(loggedBody))
}
