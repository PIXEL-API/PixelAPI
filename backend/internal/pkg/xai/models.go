package xai

import "strings"

const (
	DefaultTextModel = "grok-4.5"

	DefaultImagineImageQualityModel  = "grok-imagine-image-quality"
	DefaultImagineImageFastModel     = "grok-imagine-image"
	DefaultImagineVideoModel         = "grok-imagine-video"
	DefaultImagineVideo15LegacyModel = "grok-imagine-video-1.5"
	DefaultImagineVideo15Model       = "grok-imagine-video-1.5-preview"
)

// Model describes an xAI model in OpenAI-compatible /models shape.
type Model struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created,omitempty"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"`
}

var defaultModels = []Model{
	{ID: "grok-4.6", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.6"},
	{ID: "grok-4.5", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.5"},
	{ID: "grok-4.3", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.3"},
	{ID: "grok-3-mini", Object: "model", OwnedBy: "xai", DisplayName: "Grok 3 Mini"},
	{ID: "grok-3-mini-fast", Object: "model", OwnedBy: "xai", DisplayName: "Grok 3 Mini Fast"},
	{ID: "grok-build-0.1", Object: "model", OwnedBy: "xai", DisplayName: "Grok Build 0.1"},
	{ID: "grok-composer-2.5-fast", Object: "model", OwnedBy: "xai", DisplayName: "Grok Composer 2.5 Fast"},
	{ID: "grok-4.20-0309-reasoning", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.20 Reasoning"},
	{ID: "grok-4.20-0309-non-reasoning", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.20 Non Reasoning"},
	{ID: "grok-4.20-multi-agent-0309", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.20 Multi Agent"},
	{ID: DefaultImagineImageQualityModel, Object: "model", OwnedBy: "xai", DisplayName: "Grok Imagine Image Quality"},
	{ID: DefaultImagineImageFastModel, Object: "model", OwnedBy: "xai", DisplayName: "Grok Imagine Image"},
	{ID: DefaultImagineVideoModel, Object: "model", OwnedBy: "xai", DisplayName: "Grok Imagine Video"},
	{ID: DefaultImagineVideo15Model, Object: "model", OwnedBy: "xai", DisplayName: "Grok Imagine Video 1.5 Preview"},
	{ID: DefaultImagineVideo15LegacyModel, Object: "model", OwnedBy: "xai", DisplayName: "Grok Imagine Video 1.5 Legacy"},
}

var grokTextResponsesModelAliases = map[string]string{
	"grok":                         DefaultTextModel,
	"grok-latest":                  DefaultTextModel,
	"grok-4.6":                     "grok-4.6",
	"grok-4.6-latest":              "grok-4.6",
	"grok-4.5":                     DefaultTextModel,
	"grok-4.5-latest":              DefaultTextModel,
	"grok-4.3":                     "grok-4.3",
	"grok-4.3-latest":              "grok-4.3",
	"grok-3-mini":                  "grok-3-mini",
	"grok-3-mini-fast":             "grok-3-mini-fast",
	"grok-build":                   "grok-build-0.1",
	"grok-build-latest":            DefaultTextModel,
	"grok-build-0.1":               "grok-build-0.1",
	"grok-composer-2.5-fast":       "grok-composer-2.5-fast",
	"grok-composer":                "grok-composer-2.5-fast",
	"composer-2.5":                 "grok-composer-2.5-fast",
	"grok-4.20-reasoning":          "grok-4.20-0309-reasoning",
	"grok-4.20-0309-reasoning":     "grok-4.20-0309-reasoning",
	"grok-4.20-non-reasoning":      "grok-4.20-0309-non-reasoning",
	"grok-4.20-0309-non-reasoning": "grok-4.20-0309-non-reasoning",
	"grok-4.20-multi-agent":        "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-latest": "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-0309":   "grok-4.20-multi-agent-0309",
}

func DefaultModels() []Model {
	out := make([]Model, len(defaultModels))
	copy(out, defaultModels)
	return out
}

func DefaultModelIDs() []string {
	models := DefaultModels()
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func DefaultModelMapping() map[string]string {
	mapping := make(map[string]string, len(defaultModels)+len(grokTextResponsesModelAliases)+32)
	for _, model := range defaultModels {
		mapping[model.ID] = model.ID
	}
	for alias, canonical := range grokTextResponsesModelAliases {
		mapping[alias] = canonical
	}
	mapping["grok-imagine"] = DefaultImagineImageQualityModel
	mapping["grok-imagine-1"] = DefaultImagineImageQualityModel
	mapping["grok-imagine-edit"] = DefaultImagineImageQualityModel
	mapping[DefaultImagineImageFastModel] = DefaultImagineImageFastModel
	mapping[DefaultImagineImageQualityModel] = DefaultImagineImageQualityModel
	mapping[DefaultImagineVideoModel] = DefaultImagineVideoModel
	mapping[DefaultImagineVideo15LegacyModel] = DefaultImagineVideo15LegacyModel
	mapping[DefaultImagineVideo15Model] = DefaultImagineVideo15Model
	mapping["grok-video-1.5"] = DefaultImagineVideo15Model
	addGrokProviderPrefixedMappings(mapping)
	return mapping
}

func addGrokProviderPrefixedMappings(mapping map[string]string) {
	snapshot := make(map[string]string, len(mapping))
	for key, value := range mapping {
		snapshot[key] = value
	}
	for key, value := range snapshot {
		if !isGrokNativeOrAlias(key) {
			continue
		}
		for _, prefix := range []string{"xai/", "x-ai/", "grok/"} {
			mapping[prefix+key] = value
		}
	}
}

func isGrokNativeOrAlias(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model != "" && (strings.HasPrefix(model, "grok") ||
		strings.HasPrefix(model, "imagine") || strings.HasPrefix(model, "composer"))
}

func StripGrokProviderPrefix(model string) string {
	trimmed := strings.TrimSpace(model)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"xai/", "x-ai/", "grok/"} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return trimmed
}

func IsGrokModelID(model string) bool {
	normalized := strings.ToLower(StripGrokProviderPrefix(model))
	return strings.HasPrefix(normalized, "grok") || strings.HasPrefix(normalized, "imagine")
}

func IsGrokTextResponsesModelID(model string) bool {
	_, ok := grokTextResponsesModelAliases[strings.ToLower(StripGrokProviderPrefix(model))]
	return ok
}

func ResolveGrokTextResponsesModelID(model string, defaultText ...string) string {
	fallback := DefaultTextModel
	if len(defaultText) > 0 && strings.TrimSpace(defaultText[0]) != "" {
		fallback = strings.TrimSpace(defaultText[0])
	}
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return fallback
	}
	normalized := strings.ToLower(StripGrokProviderPrefix(trimmed))
	if canonical, ok := grokTextResponsesModelAliases[normalized]; ok {
		if canonical == DefaultTextModel {
			return fallback
		}
		return canonical
	}
	return StripGrokProviderPrefix(trimmed)
}

func CanonicalImagineVideoModel(model string) string {
	normalized := strings.ToLower(StripGrokProviderPrefix(model))
	switch {
	case normalized == "" || normalized == DefaultImagineVideoModel || normalized == "grok-imagine-video-preview":
		return DefaultImagineVideoModel
	case strings.HasPrefix(normalized, "grok-imagine-video-1.5") || normalized == "grok-video-1.5":
		return DefaultImagineVideo15Model
	default:
		return normalized
	}
}
