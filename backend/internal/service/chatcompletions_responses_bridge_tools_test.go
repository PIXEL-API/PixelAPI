package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceChatResponsesEffectiveToolsAndChoice(t *testing.T) {
	req := &apicompat.ResponsesRequest{
		Model: "gpt-test",
		Tools: []apicompat.ResponsesTool{{Type: "function", Name: "wait"}},
		Input: json.RawMessage(`[
			{"type":"message","role":"user","tools":"malformed","content":"hello"},
			{"type":"additional_tools","tools":[
				"exec",
				{"type":"tool_search"},
				{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"send_message"}]}
			]}
		]`),
		ToolChoice: json.RawMessage(`{"type":"function","name":"send_message","namespace":"collaboration"}`),
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Tools, 4)
	assert.Equal(t, []string{"wait", "exec", "tool_search", "collaboration__send_message"}, chatToolNames(out.Tools))
	assert.JSONEq(t, `{"type":"function","function":{"name":"collaboration__send_message"}}`, string(out.ToolChoice))
	require.Len(t, out.Messages, 1)
	assert.Equal(t, "user", out.Messages[0].Role)
}

func TestForceChatResponsesToolChoiceAndCollisionValidation(t *testing.T) {
	out, err := ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model:      "gpt-test",
		Input:      json.RawMessage(`"hello"`),
		Tools:      []apicompat.ResponsesTool{{Type: "function", Name: "wait"}, {Type: "web_search"}},
		ToolChoice: json.RawMessage(`{"type":"web_search"}`),
	})
	require.NoError(t, err)
	assert.Empty(t, out.ToolChoice)

	out, err = ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model:      "gpt-test",
		Input:      json.RawMessage(`"hello"`),
		Tools:      []apicompat.ResponsesTool{{Type: "tool_search"}},
		ToolChoice: json.RawMessage(`{"type":"tool_search"}`),
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"function","function":{"name":"tool_search"}}`, string(out.ToolChoice))

	_, err = ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model: "gpt-test",
		Input: json.RawMessage(`"hello"`),
		Tools: []apicompat.ResponsesTool{{Type: "tool_search"}, {Type: "function", Name: "tool_search"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool_search")

	_, err = ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model: "gpt-test",
		Input: json.RawMessage(`"hello"`),
		Tools: []apicompat.ResponsesTool{
			{Type: "function", Name: "gmail__send"},
			{Type: "namespace", Name: "gmail", Tools: []apicompat.ResponsesTool{{Type: "function", Name: "send"}}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gmail__send")

	out, err = ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model: "gpt-test",
		Input: json.RawMessage(`"hello"`),
		Tools: []apicompat.ResponsesTool{{
			Type: "namespace", Name: "gmail",
			Tools: []apicompat.ResponsesTool{{Type: "function", Name: "send"}, {Type: "function", Name: "send"}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
}

func TestForceChatResponsesToolHistoryIdentity(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"user","content":"run"},
		{"type":"custom_tool_call","call_id":"call_exec","name":"exec","input":"dir"},
		{"type":"tool_search_call","call_id":"call_search","arguments":{"query":"gmail"}},
		{"type":"function_call","call_id":"call_ns","namespace":"gmail","name":"send","arguments":"{\"to\":\"a@example.com\"}"},
		{"type":"custom_tool_call_output","call_id":"call_exec","output":"main.go"},
		{"type":"tool_search_output","call_id":"call_search","output":{"groups":["gmail"]}},
		{"type":"function_call_output","call_id":"call_ns","output":"ok"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 5)
	require.Len(t, messages[1].ToolCalls, 3)
	assert.Equal(t, "exec", messages[1].ToolCalls[0].Function.Name)
	assert.JSONEq(t, `{"input":"dir"}`, messages[1].ToolCalls[0].Function.Arguments)
	assert.Equal(t, "tool_search", messages[1].ToolCalls[1].Function.Name)
	assert.JSONEq(t, `{"query":"gmail"}`, messages[1].ToolCalls[1].Function.Arguments)
	assert.Equal(t, "gmail__send", messages[1].ToolCalls[2].Function.Name)
	assert.Equal(t, []string{"call_exec", "call_search", "call_ns"}, []string{messages[2].ToolCallID, messages[3].ToolCallID, messages[4].ToolCallID})
}

func TestForceChatResponsesNonStreamingRestoresToolIdentity(t *testing.T) {
	response := &apicompat.ChatCompletionsResponse{
		ID: "chatcmpl-tools",
		Choices: []apicompat.ChatChoice{{Message: apicompat.ChatMessage{
			Role: "assistant",
			ToolCalls: []apicompat.ChatToolCall{
				{ID: "call_exec", Function: apicompat.ChatFunctionCall{Name: "exec", Arguments: `{"input":"dir"}`}},
				{ID: "call_search", Function: apicompat.ChatFunctionCall{Name: "tool_search", Arguments: `{"query":"gmail"}`}},
				{ID: "call_ns", Function: apicompat.ChatFunctionCall{Name: "gmail__send", Arguments: `{"to":"a@example.com"}`}},
				{ID: "call_wait", Function: apicompat.ChatFunctionCall{Name: "wait", Arguments: `{"cell_id":1}`}},
			},
		}}},
	}

	out := ChatCompletionsResponseToResponses(
		response,
		"gpt-test",
		map[string]bool{"exec": true},
		true,
		map[string]apicompat.NamespacedToolName{"gmail__send": {Namespace: "gmail", Name: "send"}},
	)
	require.Len(t, out.Output, 4)
	assert.Equal(t, "custom_tool_call", out.Output[0].Type)
	assert.Equal(t, "dir", out.Output[0].Input)
	assert.Equal(t, "tool_search_call", out.Output[1].Type)
	assert.Equal(t, "function_call", out.Output[2].Type)
	assert.Equal(t, "send", out.Output[2].Name)
	assert.Equal(t, "gmail", out.Output[2].Namespace)
	assert.Equal(t, "wait", out.Output[3].Name)

	wire, err := json.Marshal(out.Output[1])
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"execution":"client"`)
	assert.Contains(t, string(wire), `"arguments":{"query":"gmail"}`)
}

func TestForceChatResponsesStreamRestoresLateAndParallelToolIdentity(t *testing.T) {
	state := NewChatCompletionsToResponsesStreamState("gpt-test")
	state.CustomTools = map[string]bool{"exec": true}
	state.ToolSearchDeclared = true
	state.NamespaceTools = map[string]apicompat.NamespacedToolName{
		"gmail__send": {Namespace: "gmail", Name: "send"},
	}
	index0, index1, index2, index3 := 0, 1, 2, 3
	first := &apicompat.ChatCompletionsChunk{Choices: []apicompat.ChatChunkChoice{{Delta: apicompat.ChatDelta{ToolCalls: []apicompat.ChatToolCall{
		{Index: &index0, ID: "call_exec", Function: apicompat.ChatFunctionCall{Arguments: `{"inp`}},
		{Index: &index1, ID: "call_search", Function: apicompat.ChatFunctionCall{Name: "tool_search", Arguments: `{"query":"gmail"}`}},
		{Index: &index2, ID: "call_ns", Function: apicompat.ChatFunctionCall{Arguments: `{"te`}},
		{Index: &index3, ID: "call_wait", Function: apicompat.ChatFunctionCall{Name: "wait", Arguments: `{"cell_id":1}`}},
	}}}}}
	second := &apicompat.ChatCompletionsChunk{Choices: []apicompat.ChatChunkChoice{{Delta: apicompat.ChatDelta{ToolCalls: []apicompat.ChatToolCall{
		{Index: &index0, Function: apicompat.ChatFunctionCall{Name: "exec", Arguments: `ut":"dir"}`}},
		{Index: &index2, Function: apicompat.ChatFunctionCall{Name: "gmail__send", Arguments: `xt":"hi"}`}},
	}}}}}

	events := ChatCompletionsChunkToResponsesEvents(first, state)
	events = append(events, ChatCompletionsChunkToResponsesEvents(second, state)...)
	events = append(events, FinalizeChatCompletionsResponsesStream(state)...)

	added := make(map[string]*apicompat.ResponsesOutput)
	done := make(map[string]*apicompat.ResponsesOutput)
	functionDeltas := make(map[string]string)
	for i := range events {
		event := &events[i]
		switch event.Type {
		case "response.output_item.added":
			if event.Item != nil && event.Item.CallID != "" {
				added[event.Item.CallID] = event.Item
			}
		case "response.output_item.done":
			if event.Item != nil && event.Item.CallID != "" {
				done[event.Item.CallID] = event.Item
			}
		case "response.function_call_arguments.delta":
			functionDeltas[event.CallID] += event.Delta
		}
	}

	assert.Equal(t, "custom_tool_call", added["call_exec"].Type)
	assert.Equal(t, "tool_search_call", added["call_search"].Type)
	assert.Equal(t, "send", added["call_ns"].Name)
	assert.Equal(t, "gmail", added["call_ns"].Namespace)
	assert.Equal(t, "function_call", added["call_wait"].Type)
	assert.NotContains(t, functionDeltas, "call_exec")
	assert.NotContains(t, functionDeltas, "call_search")
	assert.JSONEq(t, `{"text":"hi"}`, functionDeltas["call_ns"])
	assert.JSONEq(t, `{"cell_id":1}`, functionDeltas["call_wait"])
	assert.Equal(t, "dir", done["call_exec"].Input)
	assert.Equal(t, "tool_search_call", done["call_search"].Type)
	assert.Equal(t, "send", done["call_ns"].Name)
	assert.Equal(t, "gmail", done["call_ns"].Namespace)

	final := events[len(events)-1]
	require.Equal(t, "response.completed", final.Type)
	require.NotNil(t, final.Response)
	require.Len(t, final.Response.Output, 4)

	toolSearchWire, err := apicompat.ResponsesEventToSSE(findToolEvent(events, "response.output_item.done", "call_search"))
	require.NoError(t, err)
	assert.Contains(t, toolSearchWire, `"execution":"client"`)
	namespaceWire, err := apicompat.ResponsesEventToSSE(findToolEvent(events, "response.output_item.done", "call_ns"))
	require.NoError(t, err)
	assert.Contains(t, namespaceWire, `"namespace":"gmail"`)
}

func chatToolNames(tools []apicompat.ChatTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Function != nil {
			names = append(names, tool.Function.Name)
		}
	}
	return names
}

func findToolEvent(events []apicompat.ResponsesStreamEvent, eventType, callID string) apicompat.ResponsesStreamEvent {
	for _, event := range events {
		if event.Type == eventType && event.Item != nil && event.Item.CallID == callID {
			return event
		}
	}
	return apicompat.ResponsesStreamEvent{}
}
