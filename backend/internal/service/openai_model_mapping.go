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
