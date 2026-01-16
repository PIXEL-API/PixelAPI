package service

import (
	"strings"
	"time"
)

const modelRateLimitsKey = "model_rate_limits"
const modelRateLimitScopeClaudeSonnet = "claude_sonnet"

func resolveModelRateLimitScope(requestedModel string) (string, bool) {
	model := strings.ToLower(strings.TrimSpace(requestedModel))
	if model == "" {
		return "", false
	}
	model = strings.TrimPrefix(model, "models/")
	if strings.Contains(model, "sonnet") {
		return modelRateLimitScopeClaudeSonnet, true
	}
	return "", false
}

func (a *Account) isModelRateLimited(requestedModel string) bool {
	scope, ok := resolveModelRateLimitScope(requestedModel)
	if !ok {
		return false
	}
	resetAt := a.modelRateLimitResetAt(scope)
	if resetAt == nil {
		return false
	}
	return time.Now().Before(*resetAt)
}

func (a *Account) modelRateLimitResetAt(scope string) *time.Time {
	if a == nil || a.Extra == nil || scope == "" {
		return nil
	}
	rawLimits, ok := a.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return nil
	}
	rawLimit, ok := rawLimits[scope].(map[string]any)
	if !ok {
		return nil
	}
	resetAtRaw, ok := rawLimit["rate_limit_reset_at"].(string)
	if !ok || strings.TrimSpace(resetAtRaw) == "" {
		return nil
	}
	resetAt, err := time.Parse(time.RFC3339, resetAtRaw)
	if err != nil {
		return nil
	}
	return &resetAt
}
