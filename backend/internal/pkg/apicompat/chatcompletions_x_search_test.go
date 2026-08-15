package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatCompletionsToResponsesPreservesXSearchTool(t *testing.T) {
	t.Parallel()
	enableImages := true
	enableVideos := false
	req := &ChatCompletionsRequest{
		Model:    "grok-4.5",
		Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"latest xAI post"`)}},
		Tools: []ChatTool{{
			Type:                     "x_search",
			AllowedXHandles:          []string{"xai"},
			ExcludedXHandles:         []string{"spam"},
			FromDate:                 "2026-08-01",
			ToDate:                   "2026-08-10",
			EnableImageUnderstanding: &enableImages,
			EnableVideoUnderstanding: &enableVideos,
		}},
		ToolChoice: json.RawMessage(`{"type":"x_search"}`),
	}

	responses, err := ChatCompletionsToResponses(req)

	require.NoError(t, err)
	require.Len(t, responses.Tools, 1)
	tool := responses.Tools[0]
	require.Equal(t, "x_search", tool.Type)
	require.Equal(t, []string{"xai"}, tool.AllowedXHandles)
	require.Equal(t, []string{"spam"}, tool.ExcludedXHandles)
	require.Equal(t, "2026-08-01", tool.FromDate)
	require.Equal(t, "2026-08-10", tool.ToDate)
	require.NotNil(t, tool.EnableImageUnderstanding)
	require.True(t, *tool.EnableImageUnderstanding)
	require.NotNil(t, tool.EnableVideoUnderstanding)
	require.False(t, *tool.EnableVideoUnderstanding)
	require.JSONEq(t, `{"type":"x_search"}`, string(responses.ToolChoice))
}

func TestResponsesToChatCompletionsRequestPreservesXSearchTool(t *testing.T) {
	t.Parallel()
	enabled := true
	req := &ResponsesRequest{
		Model: "grok-4.5",
		Input: json.RawMessage(`"latest xAI post"`),
		Tools: []ResponsesTool{{
			Type:                     "x_search",
			AllowedXHandles:          []string{"xai"},
			ExcludedXHandles:         []string{"spam"},
			FromDate:                 "2026-08-01",
			ToDate:                   "2026-08-10",
			EnableImageUnderstanding: &enabled,
			EnableVideoUnderstanding: &enabled,
		}},
		ToolChoice: json.RawMessage(`{"type":"x_search"}`),
	}

	chat, err := ResponsesToChatCompletionsRequest(req)

	require.NoError(t, err)
	require.Len(t, chat.Tools, 1)
	tool := chat.Tools[0]
	require.Equal(t, "x_search", tool.Type)
	require.Equal(t, []string{"xai"}, tool.AllowedXHandles)
	require.Equal(t, []string{"spam"}, tool.ExcludedXHandles)
	require.Equal(t, "2026-08-01", tool.FromDate)
	require.Equal(t, "2026-08-10", tool.ToDate)
	require.JSONEq(t, `{"type":"x_search"}`, string(chat.ToolChoice))
}

func TestResponsesToChatCompletionsRequestPreservesXSearchStringChoice(t *testing.T) {
	t.Parallel()
	chat, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{
		Model:      "grok-4.5",
		Input:      json.RawMessage(`"latest xAI post"`),
		Tools:      []ResponsesTool{{Type: "x_search"}},
		ToolChoice: json.RawMessage(`"x_search"`),
	})
	require.NoError(t, err)
	require.JSONEq(t, `"x_search"`, string(chat.ToolChoice))
}
