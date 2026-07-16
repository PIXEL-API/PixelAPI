package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIImagesJSONKeepalivePreservesSingleJSONDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	stop := StartOpenAIImagesJSONKeepalive(c, time.Millisecond)
	waitForOpenAIImagesJSONKeepalive(t, c)
	require.Equal(t, -1, OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c))

	c.JSON(http.StatusOK, gin.H{"created": 1, "data": []any{}})
	stop()

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, float64(1), payload["created"])
	require.Greater(t, OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c), 0)
}

func TestOpenAIImagesJSONKeepaliveFastErrorPreservesStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	stop := StartOpenAIImagesJSONKeepalive(c, time.Hour)
	c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "failed"}})
	stop()

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.JSONEq(t, `{"error":{"message":"failed"}}`, recorder.Body.String())
}

func TestOpenAIImagesJSONKeepaliveDisabledIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	originalWriter := c.Writer

	stop := StartOpenAIImagesJSONKeepalive(c, 0)
	stop()

	require.Same(t, originalWriter, c.Writer)
	require.False(t, OpenAIImagesJSONKeepalivePresent(c))
}

func waitForOpenAIImagesJSONKeepalive(t *testing.T, c *gin.Context) {
	t.Helper()
	require.Eventually(t, func() bool {
		return c.Writer.Written()
	}, time.Second, time.Millisecond)
}
