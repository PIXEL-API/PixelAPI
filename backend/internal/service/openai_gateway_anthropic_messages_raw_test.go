//go:build unit

package service

import "testing"

// TestAnthropicInputTokensToOpenAI_FoldsCache 验证 Anthropic 语义（cache_read > input_tokens）
// 折叠缓存回 InputTokens，归一化为 OpenAI 语义。
func TestAnthropicInputTokensToOpenAI_FoldsCache(t *testing.T) {
	got := anthropicInputTokensToOpenAI(9, 1792, 0)
	if got != 1801 {
		t.Fatalf("折叠后 InputTokens = %d, want 1801 (9+1792+0)", got)
	}
}

// TestAnthropicInputTokensToOpenAI_NoFoldWhenOpenAI 验证 OpenAI 语义（input 含 cache）不折叠，
// 避免缓存重复计数导致多计费。
func TestAnthropicInputTokensToOpenAI_NoFoldWhenOpenAI(t *testing.T) {
	got := anthropicInputTokensToOpenAI(219954, 219951, 0)
	if got != 219954 {
		t.Fatalf("OpenAI 语义不应折叠, got %d, want 219954", got)
	}
}

// TestAnthropicInputTokensToOpenAI_NoCache 验证无缓存时不折叠（cache=0）。
func TestAnthropicInputTokensToOpenAI_NoCache(t *testing.T) {
	got := anthropicInputTokensToOpenAI(1442, 0, 0)
	if got != 1442 {
		t.Fatalf("无缓存不应折叠, got %d, want 1442", got)
	}
}

// TestOpenAIUsageFromAnthropicMessagesPayload_FoldsCache 验证非流式解析折叠缓存。
func TestOpenAIUsageFromAnthropicMessagesPayload_FoldsCache(t *testing.T) {
	u := openAIUsageFromAnthropicMessagesPayload(
		`{"usage":{"input_tokens":9,"output_tokens":32,"cache_creation_input_tokens":0,"cache_read_input_tokens":1792}}`)
	if u == nil {
		t.Fatal("解析结果为空")
	}
	if u.InputTokens != 1801 {
		t.Fatalf("InputTokens = %d, want 1801", u.InputTokens)
	}
	if u.CacheReadInputTokens != 1792 {
		t.Fatalf("CacheReadInputTokens = %d, want 1792", u.CacheReadInputTokens)
	}
}

// TestMergeAnthropicStreamUsageInto_MessageStartFoldsCache 验证流式 message_start 折叠。
func TestMergeAnthropicStreamUsageInto_MessageStartFoldsCache(t *testing.T) {
	usage := &OpenAIUsage{}
	mergeAnthropicStreamUsageInto(
		`{"type":"message_start","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":1792,"cache_creation_input_tokens":0}}}`,
		usage)
	if usage.InputTokens != 1801 {
		t.Fatalf("InputTokens = %d, want 1801", usage.InputTokens)
	}
}

// TestMergeAnthropicStreamUsageInto_MessageDeltaCacheFallback 验证 message_delta 的 cache 桶缺失时
// 沿用已累计值，InputTokens 折叠口径一致。
func TestMergeAnthropicStreamUsageInto_MessageDeltaCacheFallback(t *testing.T) {
	usage := &OpenAIUsage{}
	mergeAnthropicStreamUsageInto(
		`{"type":"message_start","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":1792,"cache_creation_input_tokens":0}}}`,
		usage)
	mergeAnthropicStreamUsageInto(
		`{"type":"message_delta","usage":{"input_tokens":9,"output_tokens":32}}`,
		usage)
	if usage.InputTokens != 1801 {
		t.Fatalf("message_delta 后 InputTokens = %d, want 1801（cache 缺失沿用已累计值）", usage.InputTokens)
	}
	if usage.CacheReadInputTokens != 1792 {
		t.Fatalf("CacheReadInputTokens = %d, want 1792", usage.CacheReadInputTokens)
	}
}

// TestAnthropicMessagesBilling_EndToEnd 端到端验证：修复后 Anthropic 语义的缓存命中请求
// actualInputTokens 不再被减到 0，而是恢复为新增输入。
func TestAnthropicMessagesBilling_EndToEnd(t *testing.T) {
	usage := openAIUsageFromAnthropicMessagesPayload(
		`{"usage":{"input_tokens":9,"output_tokens":32,"cache_creation_input_tokens":0,"cache_read_input_tokens":1792}}`)
	if usage == nil {
		t.Fatal("解析结果为空")
	}
	tokens, actualInputTokens := openAIUsageTokens(*usage)
	if actualInputTokens != 9 {
		t.Fatalf("actualInputTokens = %d, want 9（新增输入不应被清零）", actualInputTokens)
	}
	if tokens.CacheReadTokens != 1792 {
		t.Fatalf("CacheReadTokens = %d, want 1792", tokens.CacheReadTokens)
	}
}

// TestAnthropicMessagesBilling_OpenAISemanticUnchanged 端到端验证 OpenAI 语义（gpt-5.6-luna）
// 保持现状不变，避免误伤导致多计费。
func TestAnthropicMessagesBilling_OpenAISemanticUnchanged(t *testing.T) {
	usage := openAIUsageFromAnthropicMessagesPayload(
		`{"usage":{"input_tokens":219954,"output_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":219951}}`)
	if usage == nil {
		t.Fatal("解析结果为空")
	}
	_, actualInputTokens := openAIUsageTokens(*usage)
	if actualInputTokens != 3 {
		t.Fatalf("OpenAI 语义 actualInputTokens = %d, want 3（保持现状）", actualInputTokens)
	}
}
