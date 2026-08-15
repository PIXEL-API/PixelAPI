//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGrokTransportFailureTestAccount() *Account {
	return &Account{
		ID:          9901,
		Name:        "grok-transport-failure",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "test-access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
}

func TestForwardGrokChatTransportErrorReturnsFailoverWithoutCommittingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	svc := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{
		err: errors.New("dial tcp: connection refused"),
	}}

	result, err := svc.forwardGrokChatCompletions(
		context.Background(), c, newGrokTransportFailureTestAccount(), body,
		"grok", "grok", "grok-4.5", false, false, time.Now(),
	)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, c.Writer.Written(), "transport failure must leave the writer uncommitted for account failover")
	require.Empty(t, recorder.Body.String())
}

func TestForwardGrokMessagesTransportErrorReturnsFailoverWithoutCommittingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	svc := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{
		err: errors.New("read tcp: connection reset by peer"),
	}}

	result, err := svc.ForwardAsAnthropic(
		context.Background(), c, newGrokTransportFailureTestAccount(), body, "", "",
	)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, c.Writer.Written(), "transport failure must leave the writer uncommitted for account failover")
	require.Empty(t, recorder.Body.String())
}

func TestForwardGrokHTTP405ReturnsFailoverWithoutCommittingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		body []byte
		call func(context.Context, *gin.Context, *OpenAIGatewayService, *Account, []byte) (*OpenAIForwardResult, error)
	}{
		{
			name: "responses",
			path: "/v1/responses",
			body: []byte(`{"model":"grok","input":"hello","stream":false}`),
			call: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.forwardGrokResponses(ctx, c, account, body, "grok", false, time.Now())
			},
		},
		{
			name: "chat",
			path: "/v1/chat/completions",
			body: []byte(`{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`),
			call: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.forwardGrokChatCompletions(ctx, c, account, body, "grok", "grok", "grok-4.5", false, false, time.Now())
			},
		},
		{
			name: "messages",
			path: "/v1/messages",
			body: []byte(`{"model":"grok","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`),
			call: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			},
		},
		{
			name: "media",
			path: "/v1/images/generations",
			body: []byte(`{"model":"grok-imagine-image","prompt":"hello"}`),
			call: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointImagesGenerations, "", body, "application/json")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			svc := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusMethodNotAllowed,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"method not allowed"}}`)),
			}}}

			result, err := tt.call(context.Background(), c, svc, newGrokTransportFailureTestAccount(), tt.body)

			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, http.StatusMethodNotAllowed, failoverErr.StatusCode)
			require.False(t, c.Writer.Written(), "405 must leave the writer uncommitted for account failover")
			require.Empty(t, recorder.Body.String())
		})
	}
}
