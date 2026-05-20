package domain

import "testing"

func TestDefaultAntigravityModelMapping_ExcludesImageCompatibilityAliases(t *testing.T) {
	t.Parallel()

	blocked := []string{
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image",
		"gemini-3-pro-image-preview",
	}

	for _, model := range blocked {
		if got, ok := DefaultAntigravityModelMapping[model]; ok {
			t.Fatalf("did not expect image generation model %q in default mapping, got %q", model, got)
		}
	}
}
