package handler

import (
	"bytes"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateAPIKeyCreateRequestRejectsInvalidNumericLimits(t *testing.T) {
	tests := []struct {
		name string
		req  CreateAPIKeyRequest
		want string
	}{
		{
			name: "negative quota",
			req:  CreateAPIKeyRequest{Quota: apiKeyHandlerFloat64Ptr(-1)},
			want: "invalid quota",
		},
		{
			name: "nan quota",
			req:  CreateAPIKeyRequest{Quota: apiKeyHandlerFloat64Ptr(math.NaN())},
			want: "invalid quota",
		},
		{
			name: "infinite 5h rate limit",
			req:  CreateAPIKeyRequest{RateLimit5h: apiKeyHandlerFloat64Ptr(math.Inf(1))},
			want: "invalid rate_limit_5h",
		},
		{
			name: "negative 1d rate limit",
			req:  CreateAPIKeyRequest{RateLimit1d: apiKeyHandlerFloat64Ptr(-1)},
			want: "invalid rate_limit_1d",
		},
		{
			name: "negative 7d rate limit",
			req:  CreateAPIKeyRequest{RateLimit7d: apiKeyHandlerFloat64Ptr(-1)},
			want: "invalid rate_limit_7d",
		},
		{
			name: "zero expires_in_days",
			req:  CreateAPIKeyRequest{ExpiresInDays: apiKeyHandlerIntPtr(0)},
			want: "invalid expires_in_days",
		},
		{
			name: "negative expires_in_days",
			req:  CreateAPIKeyRequest{ExpiresInDays: apiKeyHandlerIntPtr(-1)},
			want: "invalid expires_in_days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPIKeyCreateRequest(tt.req)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateAPIKeyCreateRequestAcceptsValidNumericLimits(t *testing.T) {
	req := CreateAPIKeyRequest{
		Quota:         apiKeyHandlerFloat64Ptr(0),
		RateLimit5h:   apiKeyHandlerFloat64Ptr(0),
		RateLimit1d:   apiKeyHandlerFloat64Ptr(123456.789),
		RateLimit7d:   apiKeyHandlerFloat64Ptr(999999.5),
		ExpiresInDays: apiKeyHandlerIntPtr(7),
	}

	require.NoError(t, validateAPIKeyCreateRequest(req))
}

func TestValidateAPIKeyUpdateRequestRejectsInvalidNumericLimits(t *testing.T) {
	tests := []struct {
		name string
		req  UpdateAPIKeyRequest
		want string
	}{
		{
			name: "negative quota",
			req:  UpdateAPIKeyRequest{Quota: apiKeyHandlerFloat64Ptr(-1)},
			want: "invalid quota",
		},
		{
			name: "nan 5h rate limit",
			req:  UpdateAPIKeyRequest{RateLimit5h: apiKeyHandlerFloat64Ptr(math.NaN())},
			want: "invalid rate_limit_5h",
		},
		{
			name: "infinite 1d rate limit",
			req:  UpdateAPIKeyRequest{RateLimit1d: apiKeyHandlerFloat64Ptr(math.Inf(1))},
			want: "invalid rate_limit_1d",
		},
		{
			name: "negative 7d rate limit",
			req:  UpdateAPIKeyRequest{RateLimit7d: apiKeyHandlerFloat64Ptr(-1)},
			want: "invalid rate_limit_7d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPIKeyUpdateRequest(tt.req)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateAPIKeyUpdateRequestAcceptsValidNumericLimits(t *testing.T) {
	req := UpdateAPIKeyRequest{
		Quota:       apiKeyHandlerFloat64Ptr(0),
		RateLimit5h: apiKeyHandlerFloat64Ptr(0),
		RateLimit1d: apiKeyHandlerFloat64Ptr(123456.789),
		RateLimit7d: apiKeyHandlerFloat64Ptr(999999.5),
	}

	require.NoError(t, validateAPIKeyUpdateRequest(req))
}

func TestAPIKeyHandlerCreateRejectsInvalidNumericLimitsBeforeServiceCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAPIKeyHandler(nil)
	router.POST("/keys", apiKeyHandlerAuthSubjectMiddleware(), h.Create)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "negative quota",
			body: `{"name":"bad-create","quota":-1}`,
		},
		{
			name: "zero expires_in_days",
			body: `{"name":"bad-create","expires_in_days":0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "numeric limits must be finite and non-negative")
		})
	}
}

func TestAPIKeyHandlerUpdateRejectsInvalidNumericLimitsBeforeServiceCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAPIKeyHandler(nil)
	router.PUT("/keys/:id", apiKeyHandlerAuthSubjectMiddleware(), h.Update)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/keys/42", bytes.NewBufferString(`{"quota":-1}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "numeric limits must be finite and non-negative")
}

func apiKeyHandlerAuthSubjectMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	}
}

func apiKeyHandlerFloat64Ptr(v float64) *float64 {
	return &v
}

func apiKeyHandlerIntPtr(v int) *int {
	return &v
}
