package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewOpenAIUpstreamFailoverErrorClassifiesAccountBodyLimit(t *testing.T) {
	headers := http.Header{"X-Request-Id": []string{"req-body-limit"}}
	body := []byte(`{"error":{"type":"invalid_request_error","message":"request body exceeds the maximum allowed size"}}`)

	failoverErr := newOpenAIUpstreamFailoverError(
		http.StatusRequestEntityTooLarge,
		headers,
		body,
		"request body exceeds the maximum allowed size",
		true,
	)

	require.True(t, failoverErr.IsOpenAIRequestBodyTooLarge())
	require.Equal(t, GatewayFailureScopeAccount, failoverErr.Scope)
	require.Equal(t, NextAccountRetry, failoverErr.NextAccountAction)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.Equal(t, http.StatusRequestEntityTooLarge, failoverErr.ClientStatusCode)
	require.Equal(t, OpenAIRequestBodyTooLargeClientMessage, failoverErr.ClientMessage)
	require.Equal(t, "req-body-limit", failoverErr.ResponseHeaders.Get("X-Request-Id"))
}

func TestOpenAIRequestBodyLimitDoesNotReclassifyContextWindowError(t *testing.T) {
	body := []byte(`{"error":{"code":"context_length_exceeded","message":"maximum context length exceeded"}}`)
	failoverErr := newOpenAIUpstreamFailoverError(
		http.StatusRequestEntityTooLarge,
		nil,
		body,
		"maximum context length exceeded",
		false,
	)

	require.False(t, failoverErr.IsOpenAIRequestBodyTooLarge())
	require.False(t, shouldFailoverOpenAIPassthroughResponse(http.StatusRequestEntityTooLarge, "maximum context length exceeded", body))
}
