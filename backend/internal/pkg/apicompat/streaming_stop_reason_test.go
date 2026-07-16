package apicompat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicStreamingMaxTokensMapsToIncomplete(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:    "message_start",
		Message: &AnthropicResponse{ID: "msg_max", Model: "claude-opus-4-6"},
	}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "message_delta",
		Delta: &AnthropicDelta{StopReason: "max_tokens"},
		Usage: &AnthropicUsage{OutputTokens: 4096},
	}, state)

	events := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_stop"}, state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.incomplete", events[0].Type)
	require.NotNil(t, events[0].Response)
	assert.Equal(t, "incomplete", events[0].Response.Status)
	require.NotNil(t, events[0].Response.IncompleteDetails)
	assert.Equal(t, "max_output_tokens", events[0].Response.IncompleteDetails.Reason)
}

func TestAnthropicStreamingMaxTokensFinalizePreservesIncomplete(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:    "message_start",
		Message: &AnthropicResponse{ID: "msg_max", Model: "claude-opus-4-6"},
	}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "message_delta",
		Delta: &AnthropicDelta{StopReason: "max_tokens"},
	}, state)

	events := FinalizeAnthropicResponsesStream(state)
	require.Len(t, events, 1)
	assert.Equal(t, "response.incomplete", events[0].Type)
	assert.Equal(t, "incomplete", events[0].Response.Status)
	assert.Equal(t, "max_output_tokens", events[0].Response.IncompleteDetails.Reason)
	assert.Empty(t, FinalizeAnthropicResponsesStream(state))
}

func TestResponsesToChatCompletionsMapsContentFilter(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_filter",
		Status: "incomplete",
		IncompleteDetails: &ResponsesIncompleteDetails{
			Reason: "content_filter",
		},
		Output: []ResponsesOutput{{
			Type:    "message",
			Content: []ResponsesContentPart{{Type: "output_text", Text: "partial"}},
		}},
	}
	out := ResponsesToChatCompletions(resp, "gpt-5.6")
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "content_filter", out.Choices[0].FinishReason)
}

func TestResponsesStreamingToChatCompletionsMapsContentFilter(t *testing.T) {
	state := NewResponsesEventToChatState()
	events := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.incomplete",
		Response: &ResponsesResponse{
			Status:            "incomplete",
			IncompleteDetails: &ResponsesIncompleteDetails{Reason: "content_filter"},
		},
	}, state)
	require.Len(t, events, 1)
	require.Len(t, events[0].Choices, 1)
	require.NotNil(t, events[0].Choices[0].FinishReason)
	assert.Equal(t, "content_filter", *events[0].Choices[0].FinishReason)
}
