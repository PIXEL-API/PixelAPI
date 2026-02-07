package service

import (
	"context"
	"slices"
	"strings"
	"time"
)

const antigravityQuotaScopesKey = "antigravity_quota_scopes"

// AntigravityQuotaScope 表示 Antigravity 的配额域
type AntigravityQuotaScope string

const (
	AntigravityQuotaScopeClaude      AntigravityQuotaScope = "claude"
	AntigravityQuotaScopeGeminiText  AntigravityQuotaScope = "gemini_text"
	AntigravityQuotaScopeGeminiImage AntigravityQuotaScope = "gemini_image"
)

// IsScopeSupported 检查给定的 scope 是否在分组支持的 scope 列表中
func IsScopeSupported(supportedScopes []string, scope AntigravityQuotaScope) bool {
	if len(supportedScopes) == 0 {
		// 未配置时默认全部支持
		return true
	}
	supported := slices.Contains(supportedScopes, string(scope))
	return supported
}

// ResolveAntigravityQuotaScope 根据模型名称解析配额域（导出版本）
func ResolveAntigravityQuotaScope(requestedModel string) (AntigravityQuotaScope, bool) {
	return resolveAntigravityQuotaScope(requestedModel)
}

// resolveAntigravityQuotaScope 根据模型名称解析配额域
func resolveAntigravityQuotaScope(requestedModel string) (AntigravityQuotaScope, bool) {
	model := normalizeAntigravityModelName(requestedModel)
	if model == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(model, "claude-"):
		return AntigravityQuotaScopeClaude, true
	case strings.HasPrefix(model, "gemini-"):
		if isImageGenerationModel(model) {
			return AntigravityQuotaScopeGeminiImage, true
		}
		return AntigravityQuotaScopeGeminiText, true
	default:
		return "", false
	}
}

func normalizeAntigravityModelName(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.TrimPrefix(normalized, "models/")
	return normalized
}

// IsSchedulableForModel 结合 Antigravity 配额域限流判断是否可调度。
// 保持旧签名以兼容既有调用方；默认使用 context.Background()。
func (a *Account) IsSchedulableForModel(requestedModel string) bool {
	return a.IsSchedulableForModelWithContext(context.Background(), requestedModel)
}

func (a *Account) IsSchedulableForModelWithContext(ctx context.Context, requestedModel string) bool {
	if a == nil {
		return false
	}
	if !a.IsSchedulable() {
		return false
	}
	if a.isModelRateLimitedWithContext(ctx, requestedModel) {
		return false
	}
	if a.Platform != PlatformAntigravity {
		return true
	}
	scope, ok := resolveAntigravityQuotaScope(requestedModel)
	if !ok {
		return true
	}
	resetAt := a.antigravityQuotaScopeResetAt(scope)
	if resetAt == nil {
		return true
	}
	now := time.Now()
	return !now.Before(*resetAt)
}

func (a *Account) antigravityQuotaScopeResetAt(scope AntigravityQuotaScope) *time.Time {
	if a == nil || a.Extra == nil || scope == "" {
		return nil
	}
	rawScopes, ok := a.Extra[antigravityQuotaScopesKey].(map[string]any)
	if !ok {
		return nil
	}
	rawScope, ok := rawScopes[string(scope)].(map[string]any)
	if !ok {
		return nil
	}
	resetAtRaw, ok := rawScope["rate_limit_reset_at"].(string)
	if !ok || strings.TrimSpace(resetAtRaw) == "" {
		return nil
	}
	resetAt, err := time.Parse(time.RFC3339, resetAtRaw)
	if err != nil {
		return nil
	}
	return &resetAt
}

var antigravityAllScopes = []AntigravityQuotaScope{
	AntigravityQuotaScopeClaude,
	AntigravityQuotaScopeGeminiText,
	AntigravityQuotaScopeGeminiImage,
}

func (a *Account) GetAntigravityScopeRateLimits() map[string]int64 {
	if a == nil || a.Platform != PlatformAntigravity {
		return nil
	}
	now := time.Now()
	result := make(map[string]int64)
	for _, scope := range antigravityAllScopes {
		resetAt := a.antigravityQuotaScopeResetAt(scope)
		if resetAt != nil && now.Before(*resetAt) {
			remainingSec := int64(time.Until(*resetAt).Seconds())
			if remainingSec > 0 {
				result[string(scope)] = remainingSec
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// GetQuotaScopeRateLimitRemainingTime 获取模型域限流剩余时间
// 返回 0 表示未限流或已过期
func (a *Account) GetQuotaScopeRateLimitRemainingTime(requestedModel string) time.Duration {
	if a == nil || a.Platform != PlatformAntigravity {
		return 0
	}
	scope, ok := resolveAntigravityQuotaScope(requestedModel)
	if !ok {
		return 0
	}
	resetAt := a.antigravityQuotaScopeResetAt(scope)
	if resetAt == nil {
		return 0
	}
	if remaining := time.Until(*resetAt); remaining > 0 {
		return remaining
	}
	return 0
}

// GetRateLimitRemainingTime 获取限流剩余时间（模型限流和模型域限流取最大值）
// 返回 0 表示未限流或已过期
func (a *Account) GetRateLimitRemainingTime(requestedModel string) time.Duration {
	return a.GetRateLimitRemainingTimeWithContext(context.Background(), requestedModel)
}

// GetRateLimitRemainingTimeWithContext 获取限流剩余时间（模型限流和模型域限流取最大值）
// 返回 0 表示未限流或已过期
func (a *Account) GetRateLimitRemainingTimeWithContext(ctx context.Context, requestedModel string) time.Duration {
	if a == nil {
		return 0
	}
	modelRemaining := a.GetModelRateLimitRemainingTimeWithContext(ctx, requestedModel)
	scopeRemaining := a.GetQuotaScopeRateLimitRemainingTime(requestedModel)
	if modelRemaining > scopeRemaining {
		return modelRemaining
	}
	return scopeRemaining
}
