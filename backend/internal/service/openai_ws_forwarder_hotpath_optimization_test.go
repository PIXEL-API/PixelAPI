package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOpenAIWSEventEnvelope(t *testing.T) {
	eventType, responseID, response := parseOpenAIWSEventEnvelope([]byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.1"}}`))
	require.Equal(t, "response.completed", eventType)
	require.Equal(t, "resp_1", responseID)
	require.True(t, response.Exists())
	require.Equal(t, `{"id":"resp_1","model":"gpt-5.1"}`, response.Raw)

	eventType, responseID, response = parseOpenAIWSEventEnvelope([]byte(`{"type":"response.delta","id":"evt_1"}`))
	require.Equal(t, "response.delta", eventType)
	require.Equal(t, "evt_1", responseID)
	require.False(t, response.Exists())
}

func TestParseOpenAIWSResponseUsageFromTerminalEvent(t *testing.T) {
	usage := &OpenAIUsage{}
	parseOpenAIWSResponseUsageFromTerminalEvent(
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":11,"output_tokens":7,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":4}}}}`),
		usage,
	)
	require.Equal(t, 11, usage.InputTokens)
	require.Equal(t, 7, usage.OutputTokens)
	require.Equal(t, 3, usage.CacheReadInputTokens)
	require.Equal(t, 4, usage.CacheCreationInputTokens)
}

func TestOpenAIWSEventShouldParseUsageSupportsSuccessfulTerminalEvents(t *testing.T) {
	tests := []struct {
		eventType string
		want      bool
	}{
		{eventType: "response.completed", want: true},
		{eventType: " response.completed ", want: true},
		{eventType: "response.done", want: true},
		{eventType: " response.done ", want: true},
		{eventType: "response.failed", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			require.Equal(t, tt.want, openAIWSEventShouldParseUsage(tt.eventType))
		})
	}
}

func TestParseOpenAIWSResponseUsageFromTerminalEventUsesSharedEnvelopePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		initial    OpenAIUsage
		wantInput  int
		wantOutput int
	}{
		{
			name:       "top level usage",
			message:    `{"type":"response.completed","usage":{"input_tokens":11,"output_tokens":7}}`,
			wantInput:  11,
			wantOutput: 7,
		},
		{
			name:       "response usage",
			message:    `{"type":"response.completed","response":{"usage":{"input_tokens":12,"output_tokens":8}}}`,
			wantInput:  12,
			wantOutput: 8,
		},
		{
			name:       "data usage on response done",
			message:    `{"type":"response.done","data":{"usage":{"input_tokens":13,"output_tokens":9}}}`,
			wantInput:  13,
			wantOutput: 9,
		},
		{
			name:       "data response usage",
			message:    `{"type":"response.done","data":{"response":{"usage":{"input_tokens":14,"output_tokens":10}}}}`,
			wantInput:  14,
			wantOutput: 10,
		},
		{
			name:       "top level wins",
			message:    `{"type":"response.completed","usage":{"input_tokens":1,"output_tokens":2},"response":{"usage":{"input_tokens":100,"output_tokens":200}},"data":{"usage":{"input_tokens":300,"output_tokens":400}}}`,
			wantInput:  1,
			wantOutput: 2,
		},
		{
			name:       "empty top level object blocks lower priority",
			message:    `{"type":"response.completed","usage":{},"response":{"usage":{"input_tokens":100,"output_tokens":200}}}`,
			initial:    OpenAIUsage{InputTokens: 9, OutputTokens: 8},
			wantInput:  0,
			wantOutput: 0,
		},
		{
			name:       "invalid high priority shape is skipped",
			message:    `{"type":"response.completed","usage":"invalid","response":{"usage":{"input_tokens":3,"output_tokens":4}}}`,
			wantInput:  3,
			wantOutput: 4,
		},
		{
			name:       "missing usage preserves current snapshot",
			message:    `{"type":"response.completed","response":{"id":"resp_without_usage"}}`,
			initial:    OpenAIUsage{InputTokens: 9, OutputTokens: 8},
			wantInput:  9,
			wantOutput: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := tt.initial
			parseOpenAIWSResponseUsageFromTerminalEvent([]byte(tt.message), &usage)
			require.Equal(t, tt.wantInput, usage.InputTokens)
			require.Equal(t, tt.wantOutput, usage.OutputTokens)
		})
	}
}

func TestOpenAIWSErrorEventHelpers_ConsistentWithWrapper(t *testing.T) {
	message := []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_request","message":"invalid input"}}`)
	codeRaw, errTypeRaw, errMsgRaw := parseOpenAIWSErrorEventFields(message)

	wrappedReason, wrappedRecoverable := classifyOpenAIWSErrorEvent(message)
	rawReason, rawRecoverable := classifyOpenAIWSErrorEventFromRaw(codeRaw, errTypeRaw, errMsgRaw)
	require.Equal(t, wrappedReason, rawReason)
	require.Equal(t, wrappedRecoverable, rawRecoverable)

	wrappedStatus := openAIWSErrorHTTPStatus(message)
	rawStatus := openAIWSErrorHTTPStatusFromRaw(codeRaw, errTypeRaw)
	require.Equal(t, wrappedStatus, rawStatus)
	require.Equal(t, http.StatusBadRequest, rawStatus)

	wrappedCode, wrappedType, wrappedMsg := summarizeOpenAIWSErrorEventFields(message)
	rawCode, rawType, rawMsg := summarizeOpenAIWSErrorEventFieldsFromRaw(codeRaw, errTypeRaw, errMsgRaw)
	require.Equal(t, wrappedCode, rawCode)
	require.Equal(t, wrappedType, rawType)
	require.Equal(t, wrappedMsg, rawMsg)
}

func TestOpenAIWSMessageLikelyContainsToolCalls(t *testing.T) {
	require.False(t, openAIWSMessageLikelyContainsToolCalls([]byte(`{"type":"response.output_text.delta","delta":"hello"}`)))
	require.True(t, openAIWSMessageLikelyContainsToolCalls([]byte(`{"type":"response.output_item.added","item":{"tool_calls":[{"id":"tc1"}]}}`)))
	require.True(t, openAIWSMessageLikelyContainsToolCalls([]byte(`{"type":"response.output_item.added","item":{"type":"function_call"}}`)))
}

func TestReplaceOpenAIWSMessageModel_OptimizedStillCorrect(t *testing.T) {
	noModel := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
	require.Equal(t, string(noModel), string(replaceOpenAIWSMessageModel(noModel, "gpt-5.1", "custom-model")))

	rootOnly := []byte(`{"type":"response.created","model":"gpt-5.1"}`)
	require.Equal(t, `{"type":"response.created","model":"custom-model"}`, string(replaceOpenAIWSMessageModel(rootOnly, "gpt-5.1", "custom-model")))

	responseOnly := []byte(`{"type":"response.completed","response":{"model":"gpt-5.1"}}`)
	require.Equal(t, `{"type":"response.completed","response":{"model":"custom-model"}}`, string(replaceOpenAIWSMessageModel(responseOnly, "gpt-5.1", "custom-model")))

	both := []byte(`{"model":"gpt-5.1","response":{"model":"gpt-5.1"}}`)
	require.Equal(t, `{"model":"custom-model","response":{"model":"custom-model"}}`, string(replaceOpenAIWSMessageModel(both, "gpt-5.1", "custom-model")))
}
