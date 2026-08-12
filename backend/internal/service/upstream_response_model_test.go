package service

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamResponseModelObserverTerminalWinsAndRecordsConflict(t *testing.T) {
	observer := &upstreamResponseModelObserver{}

	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5.5"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.4"}}`), "response.completed")

	require.Equal(t, "gpt-5.4", observer.Model())
	require.True(t, observer.Conflict())
	require.True(t, observer.BillingEligible())
}

func TestUpstreamResponseModelObserverBillingEligibilityRequiresSuccess(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{"response.failed", "response.incomplete", "response.cancelled", "response.canceled"} {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			observer := &upstreamResponseModelObserver{}
			observer.ObserveOpenAI(
				[]byte(`{"response":{"model":"gpt-5.4"}}`),
				eventType,
			)

			require.Equal(t, "gpt-5.4", observer.Model(), "failed terminal declarations remain auditable")
			require.False(t, observer.BillingEligible())
		})
	}

	for _, eventType := range []string{"response.completed", "response.done"} {
		t.Run(eventType, func(t *testing.T) {
			t.Parallel()
			observer := &upstreamResponseModelObserver{}
			observer.ObserveOpenAI(
				[]byte(`{"response":{"model":"gpt-5.4"}}`),
				eventType,
			)

			require.True(t, observer.BillingEligible())
		})
	}
}

func TestUpstreamResponseModelObserverProviderObservationDoesNotAuthorizeBilling(t *testing.T) {
	t.Parallel()

	anthropic := &upstreamResponseModelObserver{}
	anthropic.ObserveAnthropic([]byte(`{"message":{"model":"claude-sonnet-4"}}`))
	require.Equal(t, "claude-sonnet-4", anthropic.Model())
	require.False(t, anthropic.BillingEligible())
	anthropic.MarkBillingEligible()
	require.True(t, anthropic.BillingEligible())

	gemini := &upstreamResponseModelObserver{}
	gemini.ObserveGemini([]byte(`{"modelVersion":"gemini-3-pro"}`))
	require.Equal(t, "gemini-3-pro", gemini.Model())
	require.False(t, gemini.BillingEligible())
	gemini.MarkBillingEligible()
	require.True(t, gemini.BillingEligible())
}

func TestUpstreamResponseModelObserverProviderProtocolCompletion(t *testing.T) {
	t.Parallel()

	t.Run("anthropic requires message_stop", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(nil)
		observer := beginUpstreamResponseModelObservation(c)
		observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"model":"claude-sonnet-4"}}`))

		require.Equal(t, "claude-sonnet-4", observer.Model())
		require.False(t, observer.ProtocolComplete())
		require.False(t, observer.BillingEligible())

		observer.ObserveAnthropic([]byte(`{"type":"message_stop"}`))
		require.True(t, observer.ProtocolComplete())
		result := applyObservedUpstreamResponseModelToForwardResult(c, &ForwardResult{}, observer.ProtocolComplete())
		require.Equal(t, "claude-sonnet-4", result.UpstreamResponseModel)
		require.True(t, result.UpstreamResponseModelBillingEligible)
	})

	t.Run("gemini requires finish reason", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(nil)
		observer := beginUpstreamResponseModelObservation(c)
		observer.ObserveGemini([]byte(`{"modelVersion":"gemini-3-pro","candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`))

		require.Equal(t, "gemini-3-pro", observer.Model())
		require.False(t, observer.ProtocolComplete())
		require.False(t, observer.BillingEligible())

		observer.ObserveGemini([]byte(`{"candidates":[{"finishReason":"STOP"}]}`))
		require.True(t, observer.ProtocolComplete())
		result := applyObservedUpstreamResponseModelToForwardResult(c, &ForwardResult{}, observer.ProtocolComplete())
		require.Equal(t, "gemini-3-pro", result.UpstreamResponseModel)
		require.True(t, result.UpstreamResponseModelBillingEligible)
	})
}

func TestApplyObservedUpstreamResponseModelKeepsPartialAuditWithoutBillingEligibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveGemini([]byte(`{"modelVersion":"gemini-3-pro"}`))

	result := applyObservedUpstreamResponseModelToForwardResult(c, &ForwardResult{}, observer.ProtocolComplete())

	require.Equal(t, "gemini-3-pro", result.UpstreamResponseModel)
	require.False(t, result.UpstreamResponseModelBillingEligible)
}

func TestUpstreamResponseModelObserverFailedTerminalRevokesEarlierSuccess(t *testing.T) {
	t.Parallel()

	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.4"}}`), "response.completed")
	require.True(t, observer.BillingEligible())

	observer.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.4"}}`), "response.failed")
	require.False(t, observer.BillingEligible())

	observer.MarkBillingEligible()
	require.False(t, observer.BillingEligible(), "an explicit failed terminal must reject later generic success admission")
}

func TestUpstreamResponseModelObserverSupportsProviderShapes(t *testing.T) {
	t.Run("openai top-level", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveOpenAI([]byte(`{"type":"response.completed","model":"gpt-5.5"}`), "response.completed")
		require.Equal(t, "gpt-5.5", observer.Model())
	})

	t.Run("anthropic", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"model":"claude-sonnet-4-20250514"}}`))
		require.Equal(t, "claude-sonnet-4-20250514", observer.Model())
	})

	t.Run("gemini wrapper shapes", func(t *testing.T) {
		tests := []struct {
			payload string
			want    string
		}{
			{payload: `{"modelVersion":"gemini-3-pro"}`, want: "gemini-3-pro"},
			{payload: `{"response":{"modelVersion":"gemini-3-pro"}}`, want: "gemini-3-pro"},
			{payload: `{"response":{"response":{"modelVersion":"gemini-3-pro"}}}`, want: "gemini-3-pro"},
			{payload: `{"modelVersion":"gemini-outer","response":{"modelVersion":"gemini-inner"}}`, want: "gemini-outer"},
		}

		for _, tt := range tests {
			observer := &upstreamResponseModelObserver{}
			observer.ObserveGemini([]byte(tt.payload))
			require.Equal(t, tt.want, observer.Model())
			require.False(t, observer.Conflict())
		}
	})
}

func TestUpstreamResponseModelObserverInfersNonStreamingResponsesStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   string
		eligible bool
	}{
		{name: "completed", status: "completed", eligible: true},
		{name: "done", status: "done", eligible: true},
		{name: "failed", status: "failed"},
		{name: "incomplete", status: "incomplete"},
		{name: "cancelled", status: "cancelled"},
		{name: "canceled", status: "canceled"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			observer := &upstreamResponseModelObserver{}
			observer.ObserveOpenAI([]byte(`{"object":"response","status":"`+test.status+`","model":"gpt-5.4"}`), "")
			require.Equal(t, "gpt-5.4", observer.Model())
			require.Equal(t, test.eligible, observer.BillingEligible())
		})
	}

	chat := &upstreamResponseModelObserver{}
	chat.ObserveOpenAI([]byte(`{"object":"chat.completion","status":"completed","model":"gpt-5.4"}`), "")
	require.Equal(t, "gpt-5.4", chat.Model())
	require.False(t, chat.BillingEligible(), "chat completions require their own success boundary")
}

func TestUpstreamResponseModelObserverCaseInsensitiveModelDoesNotConflict(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.Observe("gpt-5.5", false)
	observer.Observe("GPT-5.5", true)

	require.Equal(t, "GPT-5.5", observer.Model())
	require.False(t, observer.Conflict())
}

func TestUpstreamResponseModelObservationAttemptReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	first := beginUpstreamResponseModelObservation(c)
	first.Observe("failed-attempt-model", false)
	second := beginUpstreamResponseModelObservation(c)
	second.Observe("successful-attempt-model", false)

	require.Equal(t, "successful-attempt-model", observedUpstreamResponseModel(c))
	require.False(t, observedUpstreamResponseModelConflict(c))
}

func TestUpstreamModelMismatchThreeStateAndCaseInsensitiveComparison(t *testing.T) {
	require.Nil(t, upstreamModelMismatch("gpt-5.5", ""))

	matched := upstreamModelMismatch("gpt-5.5", "GPT-5.5")
	require.NotNil(t, matched)
	require.False(t, *matched)

	mismatched := upstreamModelMismatch("gpt-5.5", "gpt-5.4")
	require.NotNil(t, mismatched)
	require.True(t, *mismatched)
}

func TestUpstreamSentModelUsesRequestedModelWhenMappedModelIsEmpty(t *testing.T) {
	require.Equal(t, "mapped-model", upstreamSentModel("requested-model", " mapped-model "))
	require.Equal(t, "requested-model", upstreamSentModel(" requested-model ", ""))
}

func TestObserveOpenAISSEBodyIgnoresMalformedPayload(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observeOpenAISSEBody(observer, "data: not-json\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.4\"}}\n\n")

	require.Equal(t, "gpt-5.4", observer.Model())
	require.False(t, observer.Conflict())
}

func TestUpstreamResponseModelObserverRejectsMalformedJSONWithModelField(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.4"}`), "response.completed")

	require.Empty(t, observer.Model())
}

func TestUpstreamResponseModelObserverIgnoresModelFreeDelta(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"type":"response.output_text.delta","delta":"hello"}`), "response.output_text.delta")

	require.Empty(t, observer.Model())
	require.False(t, observer.Conflict())
}

func TestUpstreamResponseModelObserverBoundsUntrustedModelName(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.Observe("  "+strings.Repeat("模", upstreamResponseModelMaxLength+1)+"  ", false)

	require.Len(t, []rune(observer.Model()), upstreamResponseModelMaxLength)
}
