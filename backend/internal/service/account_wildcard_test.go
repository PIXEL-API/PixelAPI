//go:build unit

package service

import (
	"testing"
)

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		str      string
		expected bool
	}{
		// 精确匹配
		{"exact match", "claude-sonnet-4-5", "claude-sonnet-4-5", true},
		{"exact mismatch", "claude-sonnet-4-5", "claude-opus-4-5", false},

		// 通配符匹配
		{"wildcard prefix match", "claude-*", "claude-sonnet-4-5", true},
		{"wildcard prefix match 2", "claude-*", "claude-opus-4-5-thinking", true},
		{"wildcard prefix mismatch", "claude-*", "gemini-3-flash", false},
		{"wildcard partial match", "gemini-3*", "gemini-3-flash", true},
		{"wildcard partial match 2", "gemini-3*", "gemini-3-pro-image", true},
		{"wildcard partial mismatch", "gemini-3*", "gemini-2.5-flash", false},

		// 边界情况
		{"empty pattern exact", "", "", true},
		{"empty pattern mismatch", "", "claude", false},
		{"single star", "*", "anything", true},
		{"star at end only", "abc*", "abcdef", true},
		{"star at end empty suffix", "abc*", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.str)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.str, result, tt.expected)
			}
		})
	}
}

func TestMatchWildcardMapping(t *testing.T) {
	tests := []struct {
		name           string
		mapping        map[string]string
		requestedModel string
		expected       string
	}{
		// 精确匹配优先于通配符
		{
			name: "exact match takes precedence",
			mapping: map[string]string{
				"claude-sonnet-4-5": "claude-sonnet-4-5-exact",
				"claude-*":          "claude-default",
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5-exact",
		},

		// 最长通配符优先
		{
			name: "longer wildcard takes precedence",
			mapping: map[string]string{
				"claude-*":         "claude-default",
				"claude-sonnet-*":  "claude-sonnet-default",
				"claude-sonnet-4*": "claude-sonnet-4-series",
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-series",
		},

		// 单个通配符
		{
			name: "single wildcard",
			mapping: map[string]string{
				"claude-*": "claude-mapped",
			},
			requestedModel: "claude-opus-4-5",
			expected:       "claude-mapped",
		},

		// 无匹配返回原始模型
		{
			name: "no match returns original",
			mapping: map[string]string{
				"claude-*": "claude-mapped",
			},
			requestedModel: "gemini-3-flash",
			expected:       "gemini-3-flash",
		},

		// 空映射返回原始模型
		{
			name:           "empty mapping returns original",
			mapping:        map[string]string{},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},

		// Gemini 模型映射
		{
			name: "gemini wildcard mapping",
			mapping: map[string]string{
				"gemini-3*":   "gemini-3-pro-high",
				"gemini-2.5*": "gemini-2.5-flash",
			},
			requestedModel: "gemini-3-flash-preview",
			expected:       "gemini-3-pro-high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcardMapping(tt.mapping, tt.requestedModel)
			if result != tt.expected {
				t.Errorf("matchWildcardMapping(%v, %q) = %q, want %q", tt.mapping, tt.requestedModel, result, tt.expected)
			}
		})
	}
}

func TestAccountIsModelSupported(t *testing.T) {
	tests := []struct {
		name           string
		credentials    map[string]any
		requestedModel string
		expected       bool
	}{
		// 无映射 = 允许所有
		{
			name:           "no mapping allows all",
			credentials:    nil,
			requestedModel: "any-model",
			expected:       true,
		},
		{
			name:           "empty mapping allows all",
			credentials:    map[string]any{},
			requestedModel: "any-model",
			expected:       true,
		},

		// 精确匹配
		{
			name: "exact match supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5": "target-model",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       true,
		},
		{
			name: "exact match not supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5": "target-model",
				},
			},
			requestedModel: "claude-opus-4-5",
			expected:       false,
		},

		// 通配符匹配
		{
			name: "wildcard match supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-*": "claude-sonnet-4-5",
				},
			},
			requestedModel: "claude-opus-4-5-thinking",
			expected:       true,
		},
		{
			name: "wildcard match not supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-*": "claude-sonnet-4-5",
				},
			},
			requestedModel: "gemini-3-flash",
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Credentials: tt.credentials,
			}
			result := account.IsModelSupported(tt.requestedModel)
			if result != tt.expected {
				t.Errorf("IsModelSupported(%q) = %v, want %v", tt.requestedModel, result, tt.expected)
			}
		})
	}
}

func TestAccountGetMappedModel(t *testing.T) {
	tests := []struct {
		name           string
		credentials    map[string]any
		requestedModel string
		expected       string
	}{
		// 无映射 = 返回原始模型
		{
			name:           "no mapping returns original",
			credentials:    nil,
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},

		// 精确匹配
		{
			name: "exact match",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5": "target-model",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "target-model",
		},

		// 通配符匹配（最长优先）
		{
			name: "wildcard longest match",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-*":        "claude-default",
					"claude-sonnet-*": "claude-sonnet-mapped",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-mapped",
		},

		// 无匹配返回原始模型
		{
			name: "no match returns original",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-*": "gemini-mapped",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Credentials: tt.credentials,
			}
			result := account.GetMappedModel(tt.requestedModel)
			if result != tt.expected {
				t.Errorf("GetMappedModel(%q) = %q, want %q", tt.requestedModel, result, tt.expected)
			}
		})
	}
}
