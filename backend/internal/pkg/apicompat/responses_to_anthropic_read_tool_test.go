package apicompat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesToAnthropicReadWaitsForCompleteJSONThenStreamsSanitizedInput(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_read", Model: "gpt-5.6"},
	}, state)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_read", Name: "Read"},
	}, state)

	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 0,
		Delta:       `{"file_path":"/tmp/te`,
	}, state)
	assert.Empty(t, events)
	assert.False(t, state.CurrentToolHadDelta)

	events = ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 0,
		Delta:       `st.go","pages":""}`,
	}, state)
	require.Len(t, events, 1)
	assert.Equal(t, "content_block_delta", events[0].Type)
	assert.Equal(t, "input_json_delta", events[0].Delta.Type)
	assert.JSONEq(t, `{"file_path":"/tmp/test.go"}`, events[0].Delta.PartialJSON)
	assert.Equal(t, `{"file_path":"/tmp/test.go","pages":""}`, state.CurrentToolArgs)
	assert.True(t, state.CurrentToolHadDelta)

	events = ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 0,
		Arguments:   `{"file_path":"/tmp/test.go","pages":""}`,
	}, state)
	require.Len(t, events, 1)
	assert.Equal(t, "content_block_stop", events[0].Type)

	duplicate := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 0,
		Arguments:   `{"file_path":"/tmp/test.go","pages":""}`,
	}, state)
	assert.Empty(t, duplicate)
}

func TestResponsesToAnthropicReadWithoutDoneClosesOnCompleted(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_read", Model: "gpt-5.6"},
	}, state)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_read", Name: "Read"},
	}, state)
	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 0,
		Delta:       `{"file_path":"/tmp/test.go","pages":""}`,
	}, state)
	require.Len(t, events, 1)

	events = ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.completed",
		Response: &ResponsesResponse{Status: "completed"},
	}, state)
	require.Len(t, events, 3)
	assert.Equal(t, "content_block_stop", events[0].Type)
	assert.Equal(t, "message_delta", events[1].Type)
	assert.Equal(t, "tool_use", events[1].Delta.StopReason)
	assert.Equal(t, "message_stop", events[2].Type)
	assert.Empty(t, FinalizeResponsesAnthropicStream(state))
}

func TestResponsesToAnthropicNonReadStreamsPartialJSONImmediately(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	state.CurrentBlockType = "tool_use"
	state.CurrentToolName = "Write"
	state.OutputIndexToBlockIdx[0] = 0

	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 0,
		Delta:       `{"file_path":"/tmp/out`,
	}, state)
	require.Len(t, events, 1)
	assert.Equal(t, `{"file_path":"/tmp/out`, events[0].Delta.PartialJSON)
	assert.True(t, state.CurrentToolHadDelta)
}
