package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyCodexOAuthTransform_ToolContinuationPreservesInput(t *testing.T) {
	// 续链场景：保留 item_reference 与 id，但不再强制 store=true。
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.2",
		"input": []any{
			map[string]any{"type": "item_reference", "id": "ref1", "text": "x"},
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "ok", "id": "o1"},
		},
		"tool_choice": "auto",
	}

	applyCodexOAuthTransform(reqBody, false)

	// 未显式设置 store=true，默认为 false。
	store, ok := reqBody["store"].(bool)
	require.True(t, ok)
	require.False(t, store)

	input, ok := reqBody["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 2)

	// 校验 input[0] 为 map，避免断言失败导致测试中断。
	first, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "item_reference", first["type"])
	require.Equal(t, "ref1", first["id"])

	// 校验 input[1] 为 map，确保后续字段断言安全。
	second, ok := input[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "o1", second["id"])
}

func TestApplyCodexOAuthTransform_ExplicitStoreFalsePreserved(t *testing.T) {
	// 续链场景：显式 store=false 不再强制为 true，保持 false。
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"store": false,
		"input": []any{
			map[string]any{"type": "function_call_output", "call_id": "call_1"},
		},
		"tool_choice": "auto",
	}

	applyCodexOAuthTransform(reqBody, false)

	store, ok := reqBody["store"].(bool)
	require.True(t, ok)
	require.False(t, store)
}

func TestApplyCodexOAuthTransform_ExplicitStoreTrueForcedFalse(t *testing.T) {
	// 显式 store=true 也会强制为 false。
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"store": true,
		"input": []any{
			map[string]any{"type": "function_call_output", "call_id": "call_1"},
		},
		"tool_choice": "auto",
	}

	applyCodexOAuthTransform(reqBody, false)

	store, ok := reqBody["store"].(bool)
	require.True(t, ok)
	require.False(t, store)
}

func TestApplyCodexOAuthTransform_NonContinuationDefaultsStoreFalseAndStripsIDs(t *testing.T) {
	// 非续链场景：未设置 store 时默认 false，并移除 input 中的 id。
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"input": []any{
			map[string]any{"type": "text", "id": "t1", "text": "hi"},
		},
	}

	applyCodexOAuthTransform(reqBody, false)

	store, ok := reqBody["store"].(bool)
	require.True(t, ok)
	require.False(t, store)

	input, ok := reqBody["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 1)
	// 校验 input[0] 为 map，避免类型不匹配触发 errcheck。
	item, ok := input[0].(map[string]any)
	require.True(t, ok)
	_, hasID := item["id"]
	require.False(t, hasID)
}

func TestFilterCodexInput_RemovesItemReferenceWhenNotPreserved(t *testing.T) {
	input := []any{
		map[string]any{"type": "item_reference", "id": "ref1"},
		map[string]any{"type": "text", "id": "t1", "text": "hi"},
	}

	filtered := filterCodexInput(input, false)
	require.Len(t, filtered, 1)
	// 校验 filtered[0] 为 map，确保字段检查可靠。
	item, ok := filtered[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "text", item["type"])
	_, hasID := item["id"]
	require.False(t, hasID)
}

func TestApplyCodexOAuthTransform_NormalizeCodexTools_PreservesResponsesFunctionTools(t *testing.T) {
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "bash",
				"description": "desc",
				"parameters":  map[string]any{"type": "object"},
			},
			map[string]any{
				"type":     "function",
				"function": nil,
			},
		},
	}

	applyCodexOAuthTransform(reqBody, false)

	tools, ok := reqBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	first, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", first["type"])
	require.Equal(t, "bash", first["name"])
}

func TestApplyCodexOAuthTransform_EmptyInput(t *testing.T) {
	// 空 input 应保持为空且不触发异常。
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"input": []any{},
	}

	applyCodexOAuthTransform(reqBody, false)

	input, ok := reqBody["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 0)
}

func TestNormalizeCodexModel_Gpt53(t *testing.T) {
	cases := map[string]string{
		"gpt-5.3":             "gpt-5.3",
		"gpt-5.3-codex":       "gpt-5.3-codex",
		"gpt-5.3-codex-xhigh": "gpt-5.3-codex",
		"gpt 5.3 codex":       "gpt-5.3-codex",
	}

	for input, expected := range cases {
		require.Equal(t, expected, normalizeCodexModel(input))
	}

}

func TestApplyCodexOAuthTransform_CodexCLI_PreservesExistingInstructions(t *testing.T) {
	// Codex CLI 场景：已有 instructions 时保持不变
	setupCodexCache(t)

	reqBody := map[string]any{
		"model":        "gpt-5.1",
		"instructions": "user custom instructions",
		"input":        []any{},
	}

	result := applyCodexOAuthTransform(reqBody, true)

	instructions, ok := reqBody["instructions"].(string)
	require.True(t, ok)
	require.Equal(t, "user custom instructions", instructions)
	// instructions 未变，但其他字段（如 store、stream）可能被修改
	require.True(t, result.Modified)
}

func TestApplyCodexOAuthTransform_CodexCLI_AddsInstructionsWhenEmpty(t *testing.T) {
	// Codex CLI 场景：无 instructions 时补充内置指令
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"input": []any{},
	}

	result := applyCodexOAuthTransform(reqBody, true)

	instructions, ok := reqBody["instructions"].(string)
	require.True(t, ok)
	require.NotEmpty(t, instructions)
	require.True(t, result.Modified)
}

func TestApplyCodexOAuthTransform_NonCodexCLI_UsesOpenCodeInstructions(t *testing.T) {
	// 非 Codex CLI 场景：使用 opencode 指令（缓存中有 header）
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		"input": []any{},
	}

	result := applyCodexOAuthTransform(reqBody, false)

	instructions, ok := reqBody["instructions"].(string)
	require.True(t, ok)
	require.Equal(t, "header", instructions) // setupCodexCache 设置的缓存内容
	require.True(t, result.Modified)
}

func setupCodexCache(t *testing.T) {
	t.Helper()

	// 使用临时 HOME 避免触发网络拉取 header。
	// Windows 使用 USERPROFILE，Unix 使用 HOME。
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("USERPROFILE", tempDir)

	cacheDir := filepath.Join(tempDir, ".opencode", "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "opencode-codex-header.txt"), []byte("header"), 0o644))

	meta := map[string]any{
		"etag":        "",
		"lastFetch":   time.Now().UTC().Format(time.RFC3339),
		"lastChecked": time.Now().UnixMilli(),
	}
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "opencode-codex-header-meta.json"), data, 0o644))
}

func TestApplyCodexOAuthTransform_CodexCLI_SuppliesDefaultWhenEmpty(t *testing.T) {
	// Codex CLI 场景：无 instructions 时补充默认值
	setupCodexCache(t)

	reqBody := map[string]any{
		"model": "gpt-5.1",
		// 没有 instructions 字段
	}

	result := applyCodexOAuthTransform(reqBody, true) // isCodexCLI=true

	instructions, ok := reqBody["instructions"].(string)
	require.True(t, ok)
	require.NotEmpty(t, instructions)
	require.True(t, result.Modified)
}

func TestApplyCodexOAuthTransform_NonCodexCLI_OverridesInstructions(t *testing.T) {
	// 非 Codex CLI 场景：使用 opencode 指令覆盖
	setupCodexCache(t)

	reqBody := map[string]any{
		"model":        "gpt-5.1",
		"instructions": "old instructions",
	}

	result := applyCodexOAuthTransform(reqBody, false) // isCodexCLI=false

	instructions, ok := reqBody["instructions"].(string)
	require.True(t, ok)
	require.NotEqual(t, "old instructions", instructions)
	require.True(t, result.Modified)
}

func TestIsInstructionsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		reqBody  map[string]any
		expected bool
	}{
		{"missing field", map[string]any{}, true},
		{"nil value", map[string]any{"instructions": nil}, true},
		{"empty string", map[string]any{"instructions": ""}, true},
		{"whitespace only", map[string]any{"instructions": "   "}, true},
		{"non-string", map[string]any{"instructions": 123}, true},
		{"valid string", map[string]any{"instructions": "hello"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInstructionsEmpty(tt.reqBody)
			require.Equal(t, tt.expected, result)
		})
	}
}
