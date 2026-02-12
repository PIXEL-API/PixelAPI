package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestParseGatewayRequest(t *testing.T) {
	body := []byte(`{"model":"claude-3-7-sonnet","stream":true,"metadata":{"user_id":"session_123e4567-e89b-12d3-a456-426614174000"},"system":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}],"messages":[{"content":"hi"}]}`)
	parsed, err := ParseGatewayRequest(body, "")
	require.NoError(t, err)
	require.Equal(t, "claude-3-7-sonnet", parsed.Model)
	require.True(t, parsed.Stream)
	require.Equal(t, "session_123e4567-e89b-12d3-a456-426614174000", parsed.MetadataUserID)
	require.True(t, parsed.HasSystem)
	require.NotNil(t, parsed.System)
	require.Len(t, parsed.Messages, 1)
	require.False(t, parsed.ThinkingEnabled)
}

func TestParseGatewayRequest_ThinkingEnabled(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","thinking":{"type":"enabled"},"messages":[{"content":"hi"}]}`)
	parsed, err := ParseGatewayRequest(body, "")
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-5", parsed.Model)
	require.True(t, parsed.ThinkingEnabled)
}

func TestParseGatewayRequest_ThinkingAdaptiveEnabled(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","thinking":{"type":"adaptive"},"messages":[{"content":"hi"}]}`)
	parsed, err := ParseGatewayRequest(body, "")
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-5", parsed.Model)
	require.True(t, parsed.ThinkingEnabled)
}

func TestParseGatewayRequest_MaxTokens(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","max_tokens":1}`)
	parsed, err := ParseGatewayRequest(body, "")
	require.NoError(t, err)
	require.Equal(t, 1, parsed.MaxTokens)
}

func TestParseGatewayRequest_MaxTokensNonIntegralIgnored(t *testing.T) {
	body := []byte(`{"model":"claude-haiku-4-5","max_tokens":1.5}`)
	parsed, err := ParseGatewayRequest(body, "")
	require.NoError(t, err)
	require.Equal(t, 0, parsed.MaxTokens)
}

func TestParseGatewayRequest_SystemNull(t *testing.T) {
	body := []byte(`{"model":"claude-3","system":null}`)
	parsed, err := ParseGatewayRequest(body, "")
	require.NoError(t, err)
	// 显式传入 system:null 也应视为“字段已存在”，避免默认 system 被注入。
	require.True(t, parsed.HasSystem)
	require.Nil(t, parsed.System)
}

func TestParseGatewayRequest_InvalidModelType(t *testing.T) {
	body := []byte(`{"model":123}`)
	_, err := ParseGatewayRequest(body, "")
	require.Error(t, err)
}

func TestParseGatewayRequest_InvalidStreamType(t *testing.T) {
	body := []byte(`{"stream":"true"}`)
	_, err := ParseGatewayRequest(body, "")
	require.Error(t, err)
}

// ============ Gemini 原生格式解析测试 ============

func TestParseGatewayRequest_GeminiContents(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role": "user", "parts": [{"text": "Hello"}]},
			{"role": "model", "parts": [{"text": "Hi there"}]},
			{"role": "user", "parts": [{"text": "How are you?"}]}
		]
	}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformGemini)
	require.NoError(t, err)
	require.Len(t, parsed.Messages, 3, "should parse contents as Messages")
	require.False(t, parsed.HasSystem, "Gemini format should not set HasSystem")
	require.Nil(t, parsed.System, "no systemInstruction means nil System")
}

func TestParseGatewayRequest_GeminiSystemInstruction(t *testing.T) {
	body := []byte(`{
		"systemInstruction": {
			"parts": [{"text": "You are a helpful assistant."}]
		},
		"contents": [
			{"role": "user", "parts": [{"text": "Hello"}]}
		]
	}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformGemini)
	require.NoError(t, err)
	require.NotNil(t, parsed.System, "should parse systemInstruction.parts as System")
	parts, ok := parsed.System.([]any)
	require.True(t, ok)
	require.Len(t, parts, 1)
	partMap, ok := parts[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "You are a helpful assistant.", partMap["text"])
	require.Len(t, parsed.Messages, 1)
}

func TestParseGatewayRequest_GeminiWithModel(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-pro",
		"contents": [{"role": "user", "parts": [{"text": "test"}]}]
	}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformGemini)
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-pro", parsed.Model)
	require.Len(t, parsed.Messages, 1)
}

func TestParseGatewayRequest_GeminiIgnoresAnthropicFields(t *testing.T) {
	// Gemini 格式下 system/messages 字段应被忽略
	body := []byte(`{
		"system": "should be ignored",
		"messages": [{"role": "user", "content": "ignored"}],
		"contents": [{"role": "user", "parts": [{"text": "real content"}]}]
	}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformGemini)
	require.NoError(t, err)
	require.False(t, parsed.HasSystem, "Gemini protocol should not parse Anthropic system field")
	require.Nil(t, parsed.System, "no systemInstruction = nil System")
	require.Len(t, parsed.Messages, 1, "should use contents, not messages")
}

func TestParseGatewayRequest_GeminiEmptyContents(t *testing.T) {
	body := []byte(`{"contents": []}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformGemini)
	require.NoError(t, err)
	require.Empty(t, parsed.Messages)
}

func TestParseGatewayRequest_GeminiNoContents(t *testing.T) {
	body := []byte(`{"model": "gemini-2.5-flash"}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformGemini)
	require.NoError(t, err)
	require.Nil(t, parsed.Messages)
	require.Equal(t, "gemini-2.5-flash", parsed.Model)
}

func TestParseGatewayRequest_AnthropicIgnoresGeminiFields(t *testing.T) {
	// Anthropic 格式下 contents/systemInstruction 字段应被忽略
	body := []byte(`{
		"system": "real system",
		"messages": [{"role": "user", "content": "real content"}],
		"contents": [{"role": "user", "parts": [{"text": "ignored"}]}],
		"systemInstruction": {"parts": [{"text": "ignored"}]}
	}`)
	parsed, err := ParseGatewayRequest(body, domain.PlatformAnthropic)
	require.NoError(t, err)
	require.True(t, parsed.HasSystem)
	require.Equal(t, "real system", parsed.System)
	require.Len(t, parsed.Messages, 1)
	msg, ok := parsed.Messages[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "real content", msg["content"])
}

func TestFilterThinkingBlocks(t *testing.T) {
	containsThinkingBlock := func(body []byte) bool {
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			return false
		}
		messages, ok := req["messages"].([]any)
		if !ok {
			return false
		}
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			content, ok := msgMap["content"].([]any)
			if !ok {
				continue
			}
			for _, block := range content {
				blockMap, ok := block.(map[string]any)
				if !ok {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				if blockType == "thinking" {
					return true
				}
				if blockType == "" {
					if _, hasThinking := blockMap["thinking"]; hasThinking {
						return true
					}
				}
			}
		}
		return false
	}

	tests := []struct {
		name         string
		input        string
		shouldFilter bool
		expectError  bool
	}{
		{
			name:         "filters thinking blocks",
			input:        `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":[{"type":"text","text":"Hello"},{"type":"thinking","thinking":"internal","signature":"invalid"},{"type":"text","text":"World"}]}]}`,
			shouldFilter: true,
		},
		{
			name:         "does not filter signed thinking blocks when thinking adaptive",
			input:        `{"thinking":{"type":"adaptive"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"ok","signature":"sig_real_123"},{"type":"text","text":"B"}]}]}`,
			shouldFilter: false,
		},
		{
			name:         "filters unsigned thinking blocks when thinking adaptive",
			input:        `{"thinking":{"type":"adaptive"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"internal","signature":""},{"type":"text","text":"B"}]}]}`,
			shouldFilter: true,
		},
		{
			name:         "handles no thinking blocks",
			input:        `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":[{"type":"text","text":"Hello"}]}]}`,
			shouldFilter: false,
		},
		{
			name:         "handles invalid JSON gracefully",
			input:        `{invalid json`,
			shouldFilter: false,
			expectError:  true,
		},
		{
			name:         "handles multiple messages with thinking blocks",
			input:        `{"messages":[{"role":"user","content":[{"type":"text","text":"A"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"think"},{"type":"text","text":"B"}]}]}`,
			shouldFilter: true,
		},
		{
			name:         "filters thinking blocks without type discriminator",
			input:        `{"messages":[{"role":"assistant","content":[{"thinking":{"text":"internal"}},{"type":"text","text":"B"}]}]}`,
			shouldFilter: true,
		},
		{
			name:         "does not filter tool_use input fields named thinking",
			input:        `{"messages":[{"role":"user","content":[{"type":"tool_use","id":"t1","name":"foo","input":{"thinking":"keepme","x":1}},{"type":"text","text":"Hello"}]}]}`,
			shouldFilter: false,
		},
		{
			name:         "handles empty messages array",
			input:        `{"messages":[]}`,
			shouldFilter: false,
		},
		{
			name:         "handles missing messages field",
			input:        `{"model":"claude-3"}`,
			shouldFilter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterThinkingBlocks([]byte(tt.input))

			if tt.expectError {
				// For invalid JSON, should return original
				require.Equal(t, tt.input, string(result))
				return
			}

			if tt.shouldFilter {
				require.False(t, containsThinkingBlock(result))
			} else {
				// Ensure we don't rewrite JSON when no filtering is needed.
				require.Equal(t, tt.input, string(result))
			}

			// Verify valid JSON returned (unless input was invalid)
			var parsed map[string]any
			err := json.Unmarshal(result, &parsed)
			require.NoError(t, err)
		})
	}
}

func TestFilterThinkingBlocksForRetry_DisablesThinkingAndPreservesAsText(t *testing.T) {
	input := []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"Hi"}]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"Let me think...","signature":"bad_sig"},
				{"type":"text","text":"Answer"}
			]}
		]
	}`)

	out := FilterThinkingBlocksForRetry(input)

	var req map[string]any
	require.NoError(t, json.Unmarshal(out, &req))
	_, hasThinking := req["thinking"]
	require.False(t, hasThinking)

	msgs, ok := req["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 2)

	assistant, ok := msgs[1].(map[string]any)
	require.True(t, ok)
	content, ok := assistant["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 2)

	first, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "text", first["type"])
	require.Equal(t, "Let me think...", first["text"])
}

func TestFilterThinkingBlocksForRetry_DisablesThinkingEvenWithoutThinkingBlocks(t *testing.T) {
	input := []byte(`{
		"model":"claude-3-5-sonnet-20241022",
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"Hi"}]},
			{"role":"assistant","content":[{"type":"text","text":"Prefill"}]}
		]
	}`)

	out := FilterThinkingBlocksForRetry(input)

	var req map[string]any
	require.NoError(t, json.Unmarshal(out, &req))
	_, hasThinking := req["thinking"]
	require.False(t, hasThinking)
}

func TestFilterThinkingBlocksForRetry_RemovesRedactedThinkingAndKeepsValidContent(t *testing.T) {
	input := []byte(`{
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[
			{"role":"assistant","content":[
				{"type":"redacted_thinking","data":"..."},
				{"type":"text","text":"Visible"}
			]}
		]
	}`)

	out := FilterThinkingBlocksForRetry(input)

	var req map[string]any
	require.NoError(t, json.Unmarshal(out, &req))
	_, hasThinking := req["thinking"]
	require.False(t, hasThinking)

	msgs, ok := req["messages"].([]any)
	require.True(t, ok)
	msg0, ok := msgs[0].(map[string]any)
	require.True(t, ok)
	content, ok := msg0["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	content0, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "text", content0["type"])
	require.Equal(t, "Visible", content0["text"])
}

func TestFilterThinkingBlocksForRetry_EmptyContentGetsPlaceholder(t *testing.T) {
	input := []byte(`{
		"thinking":{"type":"enabled"},
		"messages":[
			{"role":"assistant","content":[{"type":"redacted_thinking","data":"..."}]}
		]
	}`)

	out := FilterThinkingBlocksForRetry(input)

	var req map[string]any
	require.NoError(t, json.Unmarshal(out, &req))
	msgs, ok := req["messages"].([]any)
	require.True(t, ok)
	msg0, ok := msgs[0].(map[string]any)
	require.True(t, ok)
	content, ok := msg0["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	content0, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "text", content0["type"])
	require.NotEmpty(t, content0["text"])
}

func TestFilterSignatureSensitiveBlocksForRetry_DowngradesTools(t *testing.T) {
	input := []byte(`{
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}},
				{"type":"tool_result","tool_use_id":"t1","content":"ok","is_error":false}
			]}
		]
	}`)

	out := FilterSignatureSensitiveBlocksForRetry(input)

	var req map[string]any
	require.NoError(t, json.Unmarshal(out, &req))
	_, hasThinking := req["thinking"]
	require.False(t, hasThinking)

	msgs, ok := req["messages"].([]any)
	require.True(t, ok)
	msg0, ok := msgs[0].(map[string]any)
	require.True(t, ok)
	content, ok := msg0["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 2)
	content0, ok := content[0].(map[string]any)
	require.True(t, ok)
	content1, ok := content[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "text", content0["type"])
	require.Equal(t, "text", content1["type"])
	require.Contains(t, content0["text"], "tool_use")
	require.Contains(t, content1["text"], "tool_result")
}
