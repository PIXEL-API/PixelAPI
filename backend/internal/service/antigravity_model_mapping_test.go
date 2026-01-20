//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsAntigravityModelSupported(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		// 直接支持的模型
		{"直接支持 - claude-sonnet-4-5", "claude-sonnet-4-5", true},
		{"直接支持 - claude-opus-4-5-thinking", "claude-opus-4-5-thinking", true},
		{"直接支持 - claude-sonnet-4-5-thinking", "claude-sonnet-4-5-thinking", true},
		{"直接支持 - gemini-2.5-flash", "gemini-2.5-flash", true},
		{"直接支持 - gemini-2.5-flash-lite", "gemini-2.5-flash-lite", true},
		{"直接支持 - gemini-3-pro-high", "gemini-3-pro-high", true},

		// 可映射的模型
		{"可映射 - claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20241022", true},
		{"可映射 - claude-3-5-sonnet-20240620", "claude-3-5-sonnet-20240620", true},
		{"可映射 - claude-opus-4", "claude-opus-4", true},
		{"可映射 - claude-haiku-4", "claude-haiku-4", true},
		{"可映射 - claude-3-haiku-20240307", "claude-3-haiku-20240307", true},

		// Gemini 前缀透传
		{"Gemini前缀 - gemini-2.5-pro", "gemini-2.5-pro", true},
		{"Gemini前缀 - gemini-unknown-model", "gemini-unknown-model", true},
		{"Gemini前缀 - gemini-future-version", "gemini-future-version", true},

		// Claude 前缀兜底
		{"Claude前缀 - claude-unknown-model", "claude-unknown-model", true},
		{"Claude前缀 - claude-3-opus-20240229", "claude-3-opus-20240229", true},
		{"Claude前缀 - claude-future-version", "claude-future-version", true},

		// 不支持的模型
		{"不支持 - gpt-4", "gpt-4", false},
		{"不支持 - gpt-4o", "gpt-4o", false},
		{"不支持 - llama-3", "llama-3", false},
		{"不支持 - mistral-7b", "mistral-7b", false},
		{"不支持 - 空字符串", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAntigravityModelSupported(tt.model)
			require.Equal(t, tt.expected, got, "model: %s", tt.model)
		})
	}
}

func TestAntigravityGatewayService_GetMappedModel(t *testing.T) {
	svc := &AntigravityGatewayService{}

	tests := []struct {
		name           string
		requestedModel string
		accountMapping map[string]string
		expected       string
	}{
		// 1. 账户级映射优先（注意：model_mapping 在 credentials 中存储为 map[string]any）
		{
			name:           "账户映射优先",
			requestedModel: "claude-3-5-sonnet-20241022",
			accountMapping: map[string]string{"claude-3-5-sonnet-20241022": "custom-model"},
			expected:       "custom-model",
		},
		{
			name:           "账户映射覆盖系统映射",
			requestedModel: "claude-opus-4",
			accountMapping: map[string]string{"claude-opus-4": "my-opus"},
			expected:       "my-opus",
		},

		// 2. 系统默认映射
		{
			name:           "系统映射 - claude-3-5-sonnet-20241022",
			requestedModel: "claude-3-5-sonnet-20241022",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "系统映射 - claude-3-5-sonnet-20240620",
			requestedModel: "claude-3-5-sonnet-20240620",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "系统映射 - claude-opus-4",
			requestedModel: "claude-opus-4",
			accountMapping: nil,
			expected:       "claude-opus-4-5-thinking",
		},
		{
			name:           "系统映射 - claude-opus-4-5-20251101",
			requestedModel: "claude-opus-4-5-20251101",
			accountMapping: nil,
			expected:       "claude-opus-4-5-thinking",
		},
		{
			name:           "系统映射 - claude-haiku-4 → claude-sonnet-4-5",
			requestedModel: "claude-haiku-4",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "系统映射 - claude-haiku-4-5 → claude-sonnet-4-5",
			requestedModel: "claude-haiku-4-5",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "系统映射 - claude-3-haiku-20240307 → claude-sonnet-4-5",
			requestedModel: "claude-3-haiku-20240307",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "系统映射 - claude-haiku-4-5-20251001 → claude-sonnet-4-5",
			requestedModel: "claude-haiku-4-5-20251001",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "系统映射 - claude-sonnet-4-5-20250929",
			requestedModel: "claude-sonnet-4-5-20250929",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},

		// 3. Gemini 透传
		{
			name:           "Gemini透传 - gemini-2.5-flash",
			requestedModel: "gemini-2.5-flash",
			accountMapping: nil,
			expected:       "gemini-2.5-flash",
		},
		{
			name:           "Gemini透传 - gemini-2.5-pro",
			requestedModel: "gemini-2.5-pro",
			accountMapping: nil,
			expected:       "gemini-2.5-pro",
		},
		{
			name:           "Gemini透传 - gemini-future-model",
			requestedModel: "gemini-future-model",
			accountMapping: nil,
			expected:       "gemini-future-model",
		},

		// 4. 直接支持的模型
		{
			name:           "直接支持 - claude-sonnet-4-5",
			requestedModel: "claude-sonnet-4-5",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "直接支持 - claude-opus-4-5-thinking",
			requestedModel: "claude-opus-4-5-thinking",
			accountMapping: nil,
			expected:       "claude-opus-4-5-thinking",
		},
		{
			name:           "直接支持 - claude-sonnet-4-5-thinking",
			requestedModel: "claude-sonnet-4-5-thinking",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5-thinking",
		},

		// 5. 默认值 fallback（未知 claude 模型）
		{
			name:           "默认值 - claude-unknown",
			requestedModel: "claude-unknown",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "默认值 - claude-3-opus-20240229",
			requestedModel: "claude-3-opus-20240229",
			accountMapping: nil,
			expected:       "claude-sonnet-4-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformAntigravity,
			}
			if tt.accountMapping != nil {
				// GetModelMapping 期望 model_mapping 是 map[string]any 格式
				mappingAny := make(map[string]any)
				for k, v := range tt.accountMapping {
					mappingAny[k] = v
				}
				account.Credentials = map[string]any{
					"model_mapping": mappingAny,
				}
			}

			got := svc.getMappedModel(account, tt.requestedModel)
			require.Equal(t, tt.expected, got, "model: %s", tt.requestedModel)
		})
	}
}

func TestAntigravityGatewayService_GetMappedModel_EdgeCases(t *testing.T) {
	svc := &AntigravityGatewayService{}

	tests := []struct {
		name           string
		requestedModel string
		expected       string
	}{
		// 空字符串回退到默认值
		{"空字符串", "", "claude-sonnet-4-5"},

		// 非 claude/gemini 前缀回退到默认值
		{"非claude/gemini前缀 - gpt", "gpt-4", "claude-sonnet-4-5"},
		{"非claude/gemini前缀 - llama", "llama-3", "claude-sonnet-4-5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformAntigravity}
			got := svc.getMappedModel(account, tt.requestedModel)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestAntigravityGatewayService_IsModelSupported(t *testing.T) {
	svc := &AntigravityGatewayService{}

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		// 直接支持
		{"直接支持 - claude-sonnet-4-5", "claude-sonnet-4-5", true},
		{"直接支持 - gemini-3-flash", "gemini-3-flash", true},

		// 可映射
		{"可映射 - claude-opus-4", "claude-opus-4", true},

		// 前缀透传
		{"Gemini前缀", "gemini-unknown", true},
		{"Claude前缀", "claude-unknown", true},

		// 不支持
		{"不支持 - gpt-4", "gpt-4", false},
		{"不支持 - 空字符串", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.IsModelSupported(tt.model)
			require.Equal(t, tt.expected, got)
		})
	}
}
