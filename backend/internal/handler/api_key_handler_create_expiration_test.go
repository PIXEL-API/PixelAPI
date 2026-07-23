package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseCreateAPIKeyExpirationPreservesRFC3339Instant(t *testing.T) {
	now := time.Date(2026, time.July, 23, 8, 0, 0, 0, time.UTC)
	raw := "2026-07-24T16:17:18+08:00"

	expiresAt, err := parseCreateAPIKeyExpiration(&raw, nil, now)

	require.NoError(t, err)
	require.NotNil(t, expiresAt)
	require.Equal(t, raw, expiresAt.Format(time.RFC3339))
}

func TestAPIKeyHandlerCreateRejectsInvalidExpiration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAPIKeyHandler(nil)
	router.POST("/keys", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	}, h.Create)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "expires_at and expires_in_days conflict",
			body: `{"name":"conflict","expires_at":"2099-03-03T21:06:00Z","expires_in_days":30}`,
		},
		{
			name: "expires_at is not RFC3339",
			body: `{"name":"invalid format","expires_at":"2099-03-03 21:06:00"}`,
		},
		{
			name: "expires_at is in the past",
			body: `{"name":"past","expires_at":"2000-01-01T00:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}
}
