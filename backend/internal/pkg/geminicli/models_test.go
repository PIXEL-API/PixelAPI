package geminicli

import "testing"

func TestDefaultModels_ExcludesImageModels(t *testing.T) {
	t.Parallel()

	byID := make(map[string]Model, len(DefaultModels))
	for _, model := range DefaultModels {
		byID[model.ID] = model
	}

	blocked := []string{
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
	}

	for _, id := range blocked {
		if _, ok := byID[id]; ok {
			t.Fatalf("did not expect curated Gemini image model %q to exist", id)
		}
	}
}
