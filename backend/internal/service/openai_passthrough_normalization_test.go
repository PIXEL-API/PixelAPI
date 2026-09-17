package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIPassthroughOAuthBody_RemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"prompt_cache_retention":"24h","safety_identifier":"sid","stream_options":{"include_usage":true}}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	for _, field := range openAIChatGPTInternalUnsupportedFields {
		require.False(t, gjson.GetBytes(normalized, field).Exists(), "%s should be stripped", field)
	}
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.False(t, gjson.GetBytes(normalized, "store").Bool())
}

func TestNormalizeOpenAIPassthroughOAuthBody_CompactRemovesUnsupportedUser(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","user":"user_123","metadata":{"user_id":"user_123"},"stream":true,"store":true}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "user").Exists())
	require.False(t, gjson.GetBytes(normalized, "metadata").Exists())
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
}

func TestNormalizeOpenAIPassthroughOAuthBody_PreservesCodexAutoReviewModel(t *testing.T) {
	body := []byte(`{"model":"codex-auto-review","input":"hello"}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "codex-auto-review", gjson.GetBytes(normalized, "model").String())
	require.False(t, gjson.GetBytes(normalized, "instructions").Exists())
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.False(t, gjson.GetBytes(normalized, "store").Bool())
	require.Empty(t, detectOpenAIPassthroughInstructionsRejectReason("codex-auto-review", normalized))
}

func TestNormalizeOpenAIPassthroughOAuthBody_PreservesCustomCompatibleModel(t *testing.T) {
	body := []byte(`{"model":"custom-compatible-model","input":"hello","stream":true,"store":false}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "custom-compatible-model", gjson.GetBytes(normalized, "model").String())
}

func TestNormalizeOpenAIPassthroughOAuthBody_StripsOnlyInputItemInternalMetadata(t *testing.T) {
	const field = "internal_chat_message_metadata_passthrough"
	body := []byte(`{
		"model":"gpt-5.4",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[{"type":"input_text","text":"hello","internal_chat_message_metadata_passthrough":{"keep":true}}],
			"internal_chat_message_metadata_passthrough":{"content_item_kinds":["text"]}
		}],
		"internal_chat_message_metadata_passthrough":{"top_level":true}
	}`)

	normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(normalized, "input.0."+field).Exists())
	require.True(t, gjson.GetBytes(normalized, "input.0.content.0."+field+".keep").Bool())
	require.True(t, gjson.GetBytes(normalized, field+".top_level").Bool())
}
