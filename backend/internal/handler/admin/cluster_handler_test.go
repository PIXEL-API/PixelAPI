package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClusterHandlerGetSummaryReturnsDisabledState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewClusterHandler(service.NewClusterService(nil, &config.Config{}))
	router := gin.New()
	router.GET("/summary", handler.GetSummary)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/summary", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Data service.ClusterSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.False(t, body.Data.Enabled)
	require.NotNil(t, body.Data.Versions)
}

func TestClusterHandlerWriteRejectsAdminAPIKeyAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewClusterHandler(service.NewClusterService(nil, &config.Config{}))
	router := gin.New()
	router.POST("/instances/:node_id/drain", func(c *gin.Context) {
		c.Set("auth_method", "admin_api_key")
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	}, handler.DrainInstance)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/instances/pixel-app-01/drain",
		strings.NewReader(`{"reason":"planned maintenance"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestClusterHandlerWriteRequiresAuthenticatedSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewClusterHandler(service.NewClusterService(nil, &config.Config{}))
	router := gin.New()
	router.POST("/instances/:node_id/resume", func(c *gin.Context) {
		c.Set("auth_method", "jwt")
		c.Next()
	}, handler.ResumeInstance)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/instances/pixel-app-01/resume",
		strings.NewReader(`{"reason":"dependencies are healthy"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestClusterHandlerRejectsMalformedJSONBeforeServiceCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewClusterHandler(service.NewClusterService(nil, &config.Config{}))
	router := gin.New()
	router.POST("/cache-refresh", func(c *gin.Context) {
		c.Set("auth_method", "jwt")
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	}, handler.RefreshCache)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/cache-refresh", strings.NewReader(`{"scope":`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestClusterHandlerOperationsRejectsOutOfRangeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewClusterHandler(service.NewClusterService(nil, &config.Config{}))
	router := gin.New()
	router.GET("/operations", handler.ListOperations)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/operations?limit=201", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
