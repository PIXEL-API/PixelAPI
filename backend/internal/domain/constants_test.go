package domain

import "testing"

func TestDefaultAntigravityModelMapping_IncludesImageCompatibilityAliases(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"gemini-2.5-flash-image":         "gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview": "gemini-2.5-flash-image",
		"gemini-3.1-flash-image":         "gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
		"gemini-3-pro-image":             "gemini-3.1-flash-image",
		"gemini-3-pro-image-preview":     "gemini-3.1-flash-image",
	}

	for model, want := range expected {
		got, ok := DefaultAntigravityModelMapping[model]
		if !ok {
			t.Fatalf("expected image generation model %q in default mapping", model)
		}
		if got != want {
			t.Fatalf("DefaultAntigravityModelMapping[%q] = %q, want %q", model, got, want)
		}
	}
}

func TestDefaultClaudeModelMappings_IncludeCurrentModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapping  map[string]string
		expected map[string]string
	}{
		{
			name:    "antigravity",
			mapping: DefaultAntigravityModelMapping,
			expected: map[string]string{
				"claude-fable-5":  "claude-fable-5",
				"claude-opus-5":   "claude-opus-5",
				"claude-opus-4-8": "claude-opus-4-8",
				"claude-sonnet-5": "claude-sonnet-5",
			},
		},
		{
			name:    "bedrock",
			mapping: DefaultBedrockModelMapping,
			expected: map[string]string{
				"claude-fable-5":  "anthropic.claude-fable-5",
				"claude-opus-5":   "us.anthropic.claude-opus-5-v1",
				"claude-opus-4-8": "us.anthropic.claude-opus-4-8-v1",
				"claude-sonnet-5": "us.anthropic.claude-sonnet-5-v1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for model, want := range tt.expected {
				got, ok := tt.mapping[model]
				if !ok {
					t.Fatalf("expected model %q in %s default mapping", model, tt.name)
				}
				if got != want {
					t.Fatalf("%s mapping[%q] = %q, want %q", tt.name, model, got, want)
				}
			}
		})
	}
}
