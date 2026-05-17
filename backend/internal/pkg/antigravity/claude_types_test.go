package antigravity

import "testing"

func TestDefaultModels_ExcludesImageModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	byID := make(map[string]ClaudeModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	if _, ok := byID["claude-opus-4-6-thinking"]; !ok {
		t.Fatalf("expected non-image model %q to be exposed in DefaultModels", "claude-opus-4-6-thinking")
	}

	blockedIDs := []string{
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image",
	}

	for _, id := range blockedIDs {
		if _, ok := byID[id]; ok {
			t.Fatalf("did not expect image generation model %q to be exposed in DefaultModels", id)
		}
	}
}
