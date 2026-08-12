package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const openAIInvalidFunctionParametersBody = `{"error":{"message":"Invalid schema for function 'automation_update': schema must be a JSON Schema of 'type: \"object\"', got 'type: \"None\"'.","type":"invalid_request_error","param":"input[8].tools[1].tools[2].parameters","code":"invalid_function_parameters"}}`

func newOpenAIUpstreamClientErrorTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func newOpenAIUpstreamClientErrorResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newOpenAIUpstreamClientErrorAccount() *Account {
	return &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Name: "acct"}
}

func TestIsOpenAIDeterministicClientError(t *testing.T) {
	require.True(t, isOpenAIDeterministicClientError(http.StatusBadRequest))
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	} {
		require.False(t, isOpenAIDeterministicClientError(status), "status %d", status)
	}
}

func TestWriteOpenAIUpstreamClientError_JSONPayloadShape(t *testing.T) {
	c, rec := newOpenAIUpstreamClientErrorTestContext(t)

	writeOpenAIUpstreamClientError(
		c,
		http.StatusBadRequest,
		[]byte(openAIInvalidFunctionParametersBody),
		"Invalid schema for function 'automation_update': schema must be a JSON Schema of 'type: \"object\"', got 'type: \"None\"'.",
	)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
	require.Equal(t, "invalid_function_parameters", gjson.Get(rec.Body.String(), "error.code").String())
	require.Equal(t, "input[8].tools[1].tools[2].parameters", gjson.Get(rec.Body.String(), "error.param").String())
	require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "Invalid schema for function 'automation_update'")
}

func TestWriteOpenAIUpstreamClientError_JSONFallbackMessage(t *testing.T) {
	c, rec := newOpenAIUpstreamClientErrorTestContext(t)

	writeOpenAIUpstreamClientError(c, http.StatusBadRequest, []byte(`<html><body>400 Bad Request</body></html>`), "")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, openAIUpstreamClientErrorFallbackType, gjson.Get(rec.Body.String(), "error.type").String())
	require.Equal(t, openAIUpstreamClientErrorFallbackMessage, gjson.Get(rec.Body.String(), "error.message").String())
	require.False(t, gjson.Get(rec.Body.String(), "error.code").Exists())
	require.False(t, gjson.Get(rec.Body.String(), "error.param").Exists())
}

func TestWriteOpenAIUpstreamClientError_CompactCommittedWritesResponseFailed(t *testing.T) {
	c, rec := newOpenAIUpstreamClientErrorTestContext(t)
	c.Set(openAICompactSSEKeepaliveKey, &openAICompactSSEKeepalive{
		started: true,
		stop:    make(chan struct{}),
	})

	writeOpenAIUpstreamClientError(
		c,
		http.StatusBadRequest,
		[]byte(openAIInvalidFunctionParametersBody),
		"Invalid schema for function 'automation_update': schema must be a JSON Schema of 'type: \"object\"', got 'type: \"None\"'.",
	)

	body := rec.Body.String()
	require.Contains(t, body, "event: response.failed")
	require.Equal(t, "invalid_request_error", gjson.Get(body, "response.error.type").String())
	require.Equal(t, "invalid_function_parameters", gjson.Get(body, "response.error.code").String())
	require.Equal(t, "input[8].tools[1].tools[2].parameters", gjson.Get(body, "response.error.param").String())
	require.Contains(t, gjson.Get(body, "response.error.message").String(), "Invalid schema for function 'automation_update'")
}

func TestHandleErrorResponse_Deterministic400IsNotRewrappedAs502(t *testing.T) {
	c, rec := newOpenAIUpstreamClientErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	_, err := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamClientErrorResponse(http.StatusBadRequest, openAIInvalidFunctionParametersBody),
		c,
		newOpenAIUpstreamClientErrorAccount(),
		nil,
		"",
	)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
	require.Equal(t, "invalid_function_parameters", gjson.Get(rec.Body.String(), "error.code").String())
	require.Equal(t, "input[8].tools[1].tools[2].parameters", gjson.Get(rec.Body.String(), "error.param").String())
	require.NotContains(t, rec.Body.String(), "Upstream request failed")
	require.Contains(t, err.Error(), "upstream error: 400")
}

func TestHandleErrorResponse_PassthroughRuleStillWinsOver400Branch(t *testing.T) {
	c, rec := newOpenAIUpstreamClientErrorTestContext(t)
	ruleSvc := &ErrorPassthroughService{}
	ruleSvc.setLocalCache([]*model.ErrorPassthroughRule{
		newNonFailoverPassthroughRule(http.StatusBadRequest, "automation_update", http.StatusTeapot, "自定义文案"),
	})
	BindErrorPassthroughService(c, ruleSvc)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	_, err := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamClientErrorResponse(http.StatusBadRequest, openAIInvalidFunctionParametersBody),
		c,
		newOpenAIUpstreamClientErrorAccount(),
		nil,
		"",
	)

	require.Error(t, err)
	require.Equal(t, http.StatusTeapot, rec.Code)
	require.Equal(t, "自定义文案", gjson.Get(rec.Body.String(), "error.message").String())
}
