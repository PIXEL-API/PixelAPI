package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIWSTurnClientRequestIDUsesPerTurnPayloadIdentity(t *testing.T) {
	t.Parallel()

	firstHash := service.HashUsageRequestPayload([]byte(`{"type":"response.create","input":"first"}`))
	secondHash := service.HashUsageRequestPayload([]byte(`{"type":"response.create","input":"second"}`))
	firstID := newOpenAIWSTurnClientRequestID(1, firstHash)
	secondID := newOpenAIWSTurnClientRequestID(2, secondHash)

	require.NotEqual(t, firstHash, secondHash)
	require.NotEqual(t, firstID, secondID)
	require.Contains(t, firstID, firstHash)
	require.Contains(t, secondID, secondHash)
	require.True(t, strings.HasPrefix(firstID, "openai-ws-turn:1:"))
	require.True(t, strings.HasPrefix(secondID, "openai-ws-turn:2:"))
	require.LessOrEqual(t, len(firstID), 255)
	require.LessOrEqual(t, len(secondID), 255)
}

func TestOpenAIWSTurnBillingDisposition(t *testing.T) {
	t.Parallel()

	t.Run("forward error without usage requires durable zero-usage completion", func(t *testing.T) {
		recordUsage, completeWithoutUsage, billable := openAIWSTurnBillingDisposition(
			&service.OpenAIForwardResult{},
			errors.New("upstream failed"),
		)
		require.False(t, recordUsage)
		require.True(t, completeWithoutUsage)
		require.False(t, billable)
	})

	t.Run("billable error records usage and must not use zero-usage completion", func(t *testing.T) {
		recordUsage, completeWithoutUsage, billable := openAIWSTurnBillingDisposition(
			&service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 7}},
			errors.New("upstream failed after usage"),
		)
		require.True(t, recordUsage)
		require.False(t, completeWithoutUsage)
		require.True(t, billable)
	})

	t.Run("successful zero-token result still records the completed request", func(t *testing.T) {
		recordUsage, completeWithoutUsage, billable := openAIWSTurnBillingDisposition(
			&service.OpenAIForwardResult{RequestID: "resp_zero"},
			nil,
		)
		require.True(t, recordUsage)
		require.False(t, completeWithoutUsage)
		require.False(t, billable)
	})
}
