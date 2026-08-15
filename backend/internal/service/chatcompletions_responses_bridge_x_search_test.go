//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestForceChatResponsesPreservesXSearchToolAndChoice(t *testing.T) {
	t.Parallel()
	enableImages := true
	enableVideos := false
	out, err := ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model: "grok-4.5",
		Input: json.RawMessage(`"latest xAI post"`),
		Tools: []apicompat.ResponsesTool{{
			Type:                     "x_search",
			AllowedXHandles:          []string{"xai"},
			ExcludedXHandles:         []string{"spam"},
			FromDate:                 "2026-08-01",
			ToDate:                   "2026-08-10",
			EnableImageUnderstanding: &enableImages,
			EnableVideoUnderstanding: &enableVideos,
		}},
		ToolChoice: json.RawMessage(`{"type":"x_search"}`),
	})

	require.NoError(t, err)
	require.Len(t, out.Tools, 1)
	tool := out.Tools[0]
	require.Equal(t, "x_search", tool.Type)
	require.Nil(t, tool.Function)
	require.Equal(t, []string{"xai"}, tool.AllowedXHandles)
	require.Equal(t, []string{"spam"}, tool.ExcludedXHandles)
	require.Equal(t, "2026-08-01", tool.FromDate)
	require.Equal(t, "2026-08-10", tool.ToDate)
	require.NotNil(t, tool.EnableImageUnderstanding)
	require.True(t, *tool.EnableImageUnderstanding)
	require.NotNil(t, tool.EnableVideoUnderstanding)
	require.False(t, *tool.EnableVideoUnderstanding)
	require.JSONEq(t, `{"type":"x_search"}`, string(out.ToolChoice))
}

func TestForceChatResponsesDropsXSearchChoiceWhenToolWasNotDeclared(t *testing.T) {
	t.Parallel()
	out, err := ResponsesToChatCompletionsRequest(&apicompat.ResponsesRequest{
		Model:      "grok-4.5",
		Input:      json.RawMessage(`"latest xAI post"`),
		Tools:      []apicompat.ResponsesTool{{Type: "web_search"}},
		ToolChoice: json.RawMessage(`{"type":"x_search"}`),
	})
	require.NoError(t, err)
	require.Empty(t, out.Tools)
	require.Empty(t, out.ToolChoice)
}
