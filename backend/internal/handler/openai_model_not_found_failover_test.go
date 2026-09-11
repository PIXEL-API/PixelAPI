//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHandleFailoverExhaustedPreservesStructuredOpenAIModelNotFound400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"error":{"type":"invalid_request_error","code":"model_not_found","param":"model","message":"The requested model is unavailable on this account"}}`)

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: body,
	}, false)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(recorder.Body.String(), "error.type").String())
	require.Equal(t, "model_not_found", gjson.Get(recorder.Body.String(), "error.code").String())
	require.Equal(t, "model", gjson.Get(recorder.Body.String(), "error.param").String())
	require.Equal(t, "The requested model is unavailable on this account", gjson.Get(recorder.Body.String(), "error.message").String())
}

func TestHandleFailoverExhaustedSanitizesOpenAIModelNotFoundMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"error":{"code":"model_not_found","message":"Model not found; inspect https://upstream.example/debug?access_token=super-secret-value&model=x"}}`)

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: body,
	}, false)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "super-secret-value")
	require.Contains(t, gjson.Get(recorder.Body.String(), "error.message").String(), "access_token=***")
	recorded, ok := c.Get(service.OpsUpstreamErrorMessageKey)
	require.True(t, ok)
	require.NotContains(t, recorded, "super-secret-value")
}
