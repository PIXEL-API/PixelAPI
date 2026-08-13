package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIResponsesLiteToolsMovesNamespacesIntoInput(t *testing.T) {
	functionTool := map[string]any{"type": "function", "name": "read_file"}
	namespaceTool := map[string]any{
		"type":  "namespace",
		"name":  "mcp",
		"tools": []any{map[string]any{"type": "function", "name": "lookup"}},
	}
	req := map[string]any{
		"input": "hello",
		"tools": []any{functionTool, namespaceTool},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, []any{functionTool}, req["tools"])
	input, ok := req["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 2)
	message, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "message", message["type"])
	additional, ok := input[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "additional_tools", additional["type"])
	require.Equal(t, []any{namespaceTool}, additional["tools"])
}

func TestNormalizeOpenAIResponsesLiteToolsMergesWithoutDuplicating(t *testing.T) {
	namespaceTool := map[string]any{"type": "namespace", "name": "mcp", "tools": []any{}}
	req := map[string]any{
		"input": []any{map[string]any{
			"type":  "additional_tools",
			"role":  "developer",
			"tools": []any{namespaceTool},
		}},
		"tools": []any{namespaceTool},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, req, "tools")
	input, ok := req["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 1)
	additional, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{namespaceTool}, additional["tools"])
}

func TestNormalizeOpenAIResponsesLiteToolsRejectsConflictingDefinitions(t *testing.T) {
	req := map[string]any{
		"input": []any{map[string]any{
			"type": "additional_tools",
			"tools": []any{map[string]any{
				"type": "namespace", "name": "mcp", "description": "old",
			}},
		}},
		"tools": []any{map[string]any{
			"type": "namespace", "name": "mcp", "description": "new",
		}},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(req)
	require.ErrorContains(t, err, "conflicts with migrated")
	require.False(t, changed)
}

func TestNormalizeOpenAIResponsesLiteToolsRejectsUnsupportedHostedTool(t *testing.T) {
	req := map[string]any{
		"input": "hello",
		"tools": []any{map[string]any{"type": "web_search_preview"}},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(req)
	require.ErrorContains(t, err, "does not support top-level tool type")
	require.False(t, changed)
}

func TestNormalizeOpenAIResponsesLiteToolsEnsuresAllTurnsReasoningContext(t *testing.T) {
	tests := []struct {
		name      string
		reasoning any
	}{
		{name: "missing"},
		{name: "missing context", reasoning: map[string]any{"effort": "high"}},
		{name: "wrong context", reasoning: map[string]any{"effort": "medium", "context": "current_turn"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]any{"input": "hello"}
			if tt.reasoning != nil {
				reqBody["reasoning"] = tt.reasoning
			}

			changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

			require.NoError(t, err)
			require.True(t, changed)
			reasoning, ok := reqBody["reasoning"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "all_turns", reasoning["context"])
		})
	}
}

func TestNormalizeOpenAIResponsesLiteToolsRejectsNonObjectReasoning(t *testing.T) {
	reqBody := map[string]any{"reasoning": "high"}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.ErrorContains(t, err, "reasoning to be an object")
	require.False(t, changed)
	require.Equal(t, "high", reqBody["reasoning"])
}

func TestCodexImageFunctionToolPreventsNativeImageToolInjection(t *testing.T) {
	req := map[string]any{
		"model": "gpt-5.4",
		"tools": []any{map[string]any{
			"type": "function",
			"name": codexImageGenerationFunctionToolName,
		}},
	}

	require.True(t, hasCodexImageGenerationFunctionTool(req))
	require.False(t, ensureOpenAIResponsesImageGenerationTool(req))
	require.False(t, ensureOpenAIResponsesImageGenerationToolChoiceAuto(req))
	require.False(t, applyCodexImageGenerationBridgeInstructions(req))
	require.Len(t, req["tools"], 1)
}

func TestNormalizeCodexToolChoiceFindsResponsesLiteAdditionalTool(t *testing.T) {
	req := map[string]any{
		"input": []any{map[string]any{
			"type":  "additional_tools",
			"tools": []any{map[string]any{"type": "namespace", "name": "mcp"}},
		}},
		"tool_choice": map[string]any{"type": "namespace"},
	}

	require.False(t, normalizeCodexToolChoice(req))
	require.Equal(t, map[string]any{"type": "namespace"}, req["tool_choice"])
}

func TestResponsesLiteImageNamespaceIsDetectedAndStrippedSymmetrically(t *testing.T) {
	payload := []byte(`{
		"model":"gpt-5.4",
		"input":[{
			"type":"additional_tools",
			"tools":[
				{"type":"namespace","name":"image_gen","tools":[]},
				{"type":"namespace","name":"mcp","tools":[]}
			]
		}]
	}`)

	require.True(t, IsImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.4", payload))
	stripped, changed, err := stripOpenAIImageGenerationToolsFromRawPayload(payload)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, IsImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.4", stripped))
	require.Contains(t, string(stripped), `"name":"mcp"`)
	require.NotContains(t, string(stripped), `"name":"image_gen"`)
}
