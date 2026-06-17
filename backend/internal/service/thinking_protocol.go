package service

import "strings"

// ThinkingProtocol 描述上游对历史 thinking block 的处理契约。
type ThinkingProtocol int

const (
	ThinkingProtocolUnknown ThinkingProtocol = iota
	ThinkingProtocolAnthropicStrict
	ThinkingProtocolPassbackRequired
)

// ResolveThinkingProtocol 根据实际发给上游的模型 ID 推断 thinking 协议族。
func ResolveThinkingProtocol(modelID string) ThinkingProtocol {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return ThinkingProtocolUnknown
	}

	switch {
	case strings.HasPrefix(id, "deepseek-"),
		strings.HasPrefix(id, "kimi-"),
		strings.HasPrefix(id, "moonshot-"),
		strings.HasPrefix(id, "glm-"):
		return ThinkingProtocolPassbackRequired
	}
	if strings.HasPrefix(id, "minimax-m") {
		return ThinkingProtocolPassbackRequired
	}
	if (strings.HasPrefix(id, "qwen-") ||
		strings.HasPrefix(id, "qwen2-") ||
		strings.HasPrefix(id, "qwen3-") ||
		strings.HasPrefix(id, "qwen4-")) && strings.Contains(id, "-thinking") {
		return ThinkingProtocolPassbackRequired
	}

	switch {
	case strings.HasPrefix(id, "claude-"),
		strings.HasPrefix(id, "opus-"),
		strings.HasPrefix(id, "sonnet-"),
		strings.HasPrefix(id, "haiku-"):
		return ThinkingProtocolAnthropicStrict
	default:
		return ThinkingProtocolUnknown
	}
}

func ShouldRectifyThinkingSignatureError(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}

func ShouldApplyRetryFilters(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}
