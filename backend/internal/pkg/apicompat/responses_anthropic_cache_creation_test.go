package apicompat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicUsageFromResponsesUsagePreservesCacheCreation(t *testing.T) {
	usage := anthropicUsageFromResponsesUsage(&ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             5,
		CacheCreationInputTokens: 6,
		InputTokensDetails:       &ResponsesInputTokensDetails{CachedTokens: 4},
	})
	assert.Equal(t, 10, usage.InputTokens)
	assert.Equal(t, 5, usage.OutputTokens)
	assert.Equal(t, 4, usage.CacheReadInputTokens)
	assert.Equal(t, 6, usage.CacheCreationInputTokens)
}

func TestAnthropicToResponsesResponsePreservesCacheCreation(t *testing.T) {
	out := AnthropicToResponsesResponse(&AnthropicResponse{
		ID:         "msg_cache",
		Model:      "claude-opus-4-6",
		StopReason: "end_turn",
		Usage: AnthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     4,
			CacheCreationInputTokens: 6,
		},
	})
	require.NotNil(t, out.Usage)
	assert.Equal(t, 20, out.Usage.InputTokens)
	assert.Equal(t, 25, out.Usage.TotalTokens)
	assert.Equal(t, 6, out.Usage.CacheCreationInputTokens)
	require.NotNil(t, out.Usage.InputTokensDetails)
	assert.Equal(t, 4, out.Usage.InputTokensDetails.CachedTokens)
}

func TestResponsesToAnthropicStreamingPreservesCacheCreation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event *ResponsesStreamEvent
	}{
		{
			name: "response usage",
			event: &ResponsesStreamEvent{
				Type: "response.completed",
				Response: &ResponsesResponse{
					Status: "completed",
					Usage: &ResponsesUsage{
						InputTokens:              20,
						OutputTokens:             5,
						CacheCreationInputTokens: 6,
						InputTokensDetails:       &ResponsesInputTokensDetails{CachedTokens: 4},
					},
				},
			},
		},
		{
			name: "top level usage",
			event: &ResponsesStreamEvent{
				Type: "response.completed",
				Usage: &ResponsesUsage{
					InputTokens:              20,
					OutputTokens:             5,
					CacheCreationInputTokens: 6,
					InputTokensDetails:       &ResponsesInputTokensDetails{CachedTokens: 4},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewResponsesEventToAnthropicState()
			state.MessageStartSent = true
			events := ResponsesEventToAnthropicEvents(tc.event, state)
			require.Len(t, events, 2)
			require.NotNil(t, events[0].Usage)
			assert.Equal(t, 10, events[0].Usage.InputTokens)
			assert.Equal(t, 4, events[0].Usage.CacheReadInputTokens)
			assert.Equal(t, 6, events[0].Usage.CacheCreationInputTokens)
		})
	}
}

func TestAnthropicToResponsesStreamingPreservesCacheCreation(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_cache",
			Model: "claude-opus-4-6",
			Usage: AnthropicUsage{
				InputTokens:              10,
				CacheReadInputTokens:     4,
				CacheCreationInputTokens: 6,
			},
		},
	}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "message_delta",
		Delta: &AnthropicDelta{StopReason: "end_turn"},
		Usage: &AnthropicUsage{OutputTokens: 5},
	}, state)
	events := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_stop"}, state)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Response)
	require.NotNil(t, events[0].Response.Usage)
	assert.Equal(t, 20, events[0].Response.Usage.InputTokens)
	assert.Equal(t, 25, events[0].Response.Usage.TotalTokens)
	assert.Equal(t, 6, events[0].Response.Usage.CacheCreationInputTokens)
}
