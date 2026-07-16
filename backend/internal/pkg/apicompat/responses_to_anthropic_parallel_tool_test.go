package apicompat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesToAnthropicParallelPackedDoneDoesNotEmitGhostDelta(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_parallel", Model: "gpt-5.6"},
	}, state)

	for outputIndex, name := range []string{"tool_a", "tool_b", "tool_c"} {
		events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
			Type:        "response.output_item.added",
			OutputIndex: outputIndex,
			Item: &ResponsesOutput{
				Type:   "function_call",
				CallID: "call_" + name,
				Name:   name,
			},
		}, state)
		require.NotEmpty(t, events)
		start := events[len(events)-1]
		require.Equal(t, "content_block_start", start.Type)
		require.NotNil(t, start.Index)
		assert.Equal(t, outputIndex, *start.Index)
	}

	for outputIndex, arguments := range []string{`{"a":1}`, `{"b":2}`} {
		events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
			Type:        "response.function_call_arguments.done",
			OutputIndex: outputIndex,
			Arguments:   arguments,
		}, state)
		assert.Empty(t, events, "已关闭的并行工具块不得向当前块补发 delta")
	}

	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 2,
		Arguments:   `{"c":3}`,
	}, state)
	require.Len(t, events, 2)
	assert.Equal(t, "content_block_delta", events[0].Type)
	require.NotNil(t, events[0].Index)
	assert.Equal(t, 2, *events[0].Index)
	assert.Equal(t, `{"c":3}`, events[0].Delta.PartialJSON)
	assert.Equal(t, "content_block_stop", events[1].Type)
	require.NotNil(t, events[1].Index)
	assert.Equal(t, 2, *events[1].Index)

	duplicate := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 2,
		Arguments:   `{"c":3}`,
	}, state)
	assert.Empty(t, duplicate, "重复 done 必须幂等")
}

func TestResponsesToAnthropicToolStopReasonSurvivesLaterText(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_tool_text", Model: "gpt-5.6"},
	}, state)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_a", Name: "tool_a"},
	}, state)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 0,
		Arguments:   `{}`,
	}, state)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.output_text.delta",
		OutputIndex: 1,
		Delta:       "later text",
	}, state)

	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.completed",
		Response: &ResponsesResponse{Status: "completed"},
	}, state)
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "message_delta", events[len(events)-2].Type)
	assert.Equal(t, "tool_use", events[len(events)-2].Delta.StopReason)
}
