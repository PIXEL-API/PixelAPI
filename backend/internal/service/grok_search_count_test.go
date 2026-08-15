//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokSearchCountJSONRecognizesNativeAndCompatibilityCalls(t *testing.T) {
	t.Parallel()
	body := []byte(`{"output":[
		{"type":"web_search_call","id":"ws1"},
		{"type":"x_search_call","id":"xs1"},
		{"type":"tool_search_call","id":"ts1"},
		{"type":"function_call","name":"tool_search","call_id":"f1"},
		{"type":"custom_tool_call","name":"x_search","call_id":"c1"},
		{"type":"function_call","name":"lookup","call_id":"ignored"},
		{"type":"message","content":[]}
	]}`)
	require.Equal(t, 5, countGrokNativeSearchCallsFromJSONBytes(body))
}

func TestGrokSearchCountJSONPrefersNestedResponseWithoutDoubleBilling(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"output":[{"type":"web_search_call","id":"duplicate"}],
		"response":{"output":[
			{"type":"web_search_call","id":"duplicate"},
			{"type":"x_search_call","id":"xs1"}
		]}
	}`)
	require.Equal(t, 2, countGrokNativeSearchCallsFromJSONBytes(body))
	require.Equal(t, 1, countGrokNativeSearchCallsFromJSONBytes([]byte(`{"output":[{"type":"web_search_call"}],"response":{"output":null}}`)))
	require.Zero(t, countGrokNativeSearchCallsFromJSONBytes([]byte(`not-json`)))
}

func TestGrokSearchCountSSEDeduplicatesTerminalAggregate(t *testing.T) {
	t.Parallel()
	sse := "event: response.output_item.done\r\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"web_search_call\",\"id\":\"ws1\",\"call_id\":\"c1\"}}\r\n\r\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"web_search_call\",\"id\":\"ws1\",\"call_id\":\"c1\"},{\"type\":\"x_search_call\",\"id\":\"xs1\",\"call_id\":\"c2\"}]}}\r\n\r\n"
	require.Equal(t, 2, countGrokNativeSearchCallsFromSSEBody(sse))
}

func TestGrokSearchCountSSEIgnoresDeltasAndFailedEvents(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	require.Zero(t, countGrokNativeSearchCallsInSSEDataDedup([]byte(`{"type":"response.output_item.added","item":{"type":"web_search_call","id":"ws1"}}`), seen))
	require.Zero(t, countGrokNativeSearchCallsInSSEDataDedup([]byte(`{"type":"response.failed","response":{"output":[{"type":"web_search_call","id":"ws1"}]}}`), seen))
	require.Empty(t, seen)
}

func TestGrokSearchCountSSEIdlessCallsRemainDistinctAndDeduplicated(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	firstDone := []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call"}}`)
	secondDone := []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call"}}`)
	completed := []byte(`{"type":"response.completed","response":{"output":[{"type":"web_search_call"},{"type":"web_search_call"}]}}`)
	require.Equal(t, 1, countGrokNativeSearchCallsInSSEDataDedup(firstDone, seen))
	require.Equal(t, 1, countGrokNativeSearchCallsInSSEDataDedup(secondDone, seen))
	require.Zero(t, countGrokNativeSearchCallsInSSEDataDedup(completed, seen))
}

func TestGrokSearchCountSSEMixedKeyedAndIdlessCalls(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	done := []byte(`{"type":"response.output_item.done","item":{"type":"x_search_call","call_id":"x1"}}`)
	completed := []byte(`{"type":"response.completed","response":{"output":[{"type":"x_search_call","call_id":"x1"},{"type":"web_search_call"}]}}`)
	require.Equal(t, 1, countGrokNativeSearchCallsInSSEDataDedup(done, seen))
	require.Equal(t, 1, countGrokNativeSearchCallsInSSEDataDedup(completed, seen))
}
