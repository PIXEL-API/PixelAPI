package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectiveResponsesToolsAdditionalItemScope(t *testing.T) {
	req := &ResponsesRequest{
		Tools: []ResponsesTool{{Type: "function", Name: "wait"}},
		Input: json.RawMessage(`[
			{"type":"message","role":"user","tools":"malformed","content":"hello"},
			{"type":"additional_tools","tools":["exec",{"type":"tool_search"}]}
		]`),
	}

	tools, err := EffectiveResponsesTools(req)
	require.NoError(t, err)
	require.Len(t, tools, 3)
	assert.Equal(t, "wait", tools[0].Name)
	assert.Equal(t, ResponsesTool{Type: "custom", Name: "exec"}, tools[1])
	assert.Equal(t, "tool_search", tools[2].Type)
	assert.True(t, CustomToolNames(tools)["exec"])
	assert.True(t, HasToolSearchTool(tools))
}

func TestEffectiveResponsesToolsRejectsMalformedAdditionalItem(t *testing.T) {
	req := &ResponsesRequest{Input: json.RawMessage(`[{"type":"additional_tools","tools":"malformed"}]`)}
	tools, err := EffectiveResponsesTools(req)
	require.Error(t, err)
	assert.Nil(t, tools)
	assert.Contains(t, err.Error(), "parse responses additional tools item")
}

func TestNamespaceIdentityUsesStableUTF8ByteLimit(t *testing.T) {
	assert.Equal(t, "collaboration__send_message", FlattenResponsesNamespaceToolName("collaboration", "send_message"))
	flattened := FlattenResponsesNamespaceToolName(strings.Repeat("工具", 20), strings.Repeat("子", 20))
	assert.LessOrEqual(t, len(flattened), 64)
	assert.Regexp(t, `__[0-9a-f]{8}$`, flattened)
	assert.Equal(t, flattened, FlattenResponsesNamespaceToolName(strings.Repeat("工具", 20), strings.Repeat("子", 20)))
}

func TestResponsesOutputToolSearchArgumentsWireAndUnmarshal(t *testing.T) {
	item := ResponsesOutput{
		Type:      "tool_search_call",
		ID:        "item_1",
		CallID:    "call_1",
		Arguments: `{"query":"gmail","limit":2}`,
		Status:    "completed",
	}
	wire, err := json.Marshal(item)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"tool_search_call","id":"item_1","call_id":"call_1","execution":"client","arguments":{"query":"gmail","limit":2},"status":"completed"}`, string(wire))

	var decoded ResponsesOutput
	require.NoError(t, json.Unmarshal(wire, &decoded))
	assert.JSONEq(t, `{"query":"gmail","limit":2}`, decoded.Arguments)

	require.NoError(t, json.Unmarshal([]byte(`{"type":"tool_search_call","arguments":"{\"query\":\"calendar\"}"}`), &decoded))
	assert.JSONEq(t, `{"query":"calendar"}`, decoded.Arguments)

	var response ResponsesResponse
	require.NoError(t, json.Unmarshal([]byte(`{
		"id":"resp_1","object":"response","model":"gpt-test","status":"completed",
		"output":[{"type":"tool_search_call","id":"item_2","call_id":"call_2","execution":"client","arguments":{"query":"drive"}}]
	}`), &response))
	require.Len(t, response.Output, 1)
	assert.JSONEq(t, `{"query":"drive"}`, response.Output[0].Arguments)

	var event ResponsesStreamEvent
	require.NoError(t, json.Unmarshal([]byte(`{
		"type":"response.output_item.done","output_index":0,
		"item":{"type":"tool_search_call","id":"item_3","call_id":"call_3","execution":"client","arguments":{"query":"docs"}}
	}`), &event))
	require.NotNil(t, event.Item)
	assert.JSONEq(t, `{"query":"docs"}`, event.Item.Arguments)
}

func TestResponsesStreamEventStrictWireKeepsZeroAndToolFields(t *testing.T) {
	event := ResponsesStreamEvent{
		Type:           "response.output_item.added",
		OutputIndex:    0,
		SequenceNumber: 0,
		Item: &ResponsesOutput{
			Type:      "function_call",
			ID:        "item_1",
			CallID:    "",
			Name:      "",
			Arguments: "",
		},
	}
	wire, err := json.Marshal(event)
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"output_index":0`)
	assert.Contains(t, string(wire), `"sequence_number":0`)
	assert.Contains(t, string(wire), `"call_id":""`)
	assert.Contains(t, string(wire), `"name":""`)
	assert.Contains(t, string(wire), `"arguments":""`)

	customDone := ResponsesStreamEvent{
		Type:           "response.custom_tool_call_input.done",
		OutputIndex:    0,
		SequenceNumber: 1,
		ItemID:         "item_2",
		CallID:         "call_2",
		Name:           "exec",
		Input:          "",
	}
	wire, err = json.Marshal(customDone)
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"input":""`)
	assert.Contains(t, string(wire), `"output_index":0`)
}
