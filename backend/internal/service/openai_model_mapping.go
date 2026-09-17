package service

import "strings"

// resolveOpenAIForwardModel determines the upstream model for OpenAI-compatible
// forwarding. messagesDispatchMappedModel is an exact /v1/messages dispatch
// result resolved by the caller; ordinary OpenAI requests must pass it empty.
func resolveOpenAIForwardModel(account *Account, requestedModel, messagesDispatchMappedModel string) string {
	messagesDispatchMappedModel = strings.TrimSpace(messagesDispatchMappedModel)
	if account == nil {
		if messagesDispatchMappedModel != "" {
			return messagesDispatchMappedModel
		}
		return requestedModel
	}

	mappedModel, matched := account.ResolveMappedModel(requestedModel)
	if !matched && messagesDispatchMappedModel != "" {
		return messagesDispatchMappedModel
	}
	return mappedModel
}

// ResolveOpenAIForwardModel exposes the exact forwarding model decision to
// pre-forward durable billing without duplicating mapping rules in handlers.
func ResolveOpenAIForwardModel(account *Account, requestedModel, messagesDispatchMappedModel string) string {
	return resolveOpenAIForwardModel(account, requestedModel, messagesDispatchMappedModel)
}

// ResolveOpenAIWebSocketForwardModel includes the final OAuth/Codex
// normalization performed by both WebSocket transports. Handlers use it before
// creating a durable billing intent so the billed routed model exactly matches
// the model written to the upstream frame.
func ResolveOpenAIWebSocketForwardModel(account *Account, requestedModel string) string {
	return normalizeOpenAIModelForUpstream(account, resolveOpenAIForwardModel(account, requestedModel, ""))
}

// deepseekServableModels 是 DeepSeek 平台账号未配置 model_mapping 时可服务的
// 官方模型名单。deepseek-flash 与 deepseek-v4-pro 是现行名，其余为上游仍接受
// 的兼容/版本别名。
var deepseekServableModels = []string{
	"deepseek-flash",
	"deepseek-v4-pro",
	"deepseek-v4-flash",
	"deepseek-v4-flash-vision-exp",
	"deepseek-v4-pro-0813",
}

func isDeepseekServableModel(requestedModel string) bool {
	model := strings.ToLower(normalizeClaudeCodeLongContextModel(strings.TrimSpace(requestedModel)))
	if model == "" {
		return true
	}
	for _, servable := range deepseekServableModels {
		if model == servable {
			return true
		}
	}
	return false
}

// resolveOpenAICompactForwardModel determines the compact-only upstream model
// for /responses/compact requests. It never affects normal /responses traffic.
// When no compact-specific mapping matches, the input model is returned as-is.
func resolveOpenAICompactForwardModel(account *Account, model string) string {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" || account == nil {
		return trimmedModel
	}

	mappedModel, matched := account.ResolveCompactMappedModel(trimmedModel)
	if !matched {
		return trimmedModel
	}
	if trimmedMapped := strings.TrimSpace(mappedModel); trimmedMapped != "" {
		return trimmedMapped
	}
	return trimmedModel
}

// ResolveOpenAICompactForwardModel keeps compact pre-forward billing aligned
// with the compact transport's account-specific model mapping.
func ResolveOpenAICompactForwardModel(account *Account, model string) string {
	return resolveOpenAICompactForwardModel(account, model)
}
