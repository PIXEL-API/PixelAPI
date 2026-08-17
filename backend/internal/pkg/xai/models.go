package xai

import (
	"strings"
	"sync/atomic"
)

// runtimeMappingOpts holds operator-configured defaults applied when Grok
// accounts leave credentials.model_mapping empty. Updated from settings.
var runtimeMappingOpts atomic.Value // ModelMappingOptions
var runtimeMappingVersion atomic.Uint64

func init() {
	runtimeMappingOpts.Store(ModelMappingOptions{})
	runtimeMappingVersion.Store(1)
}

// SetRuntimeModelMappingOptions updates process-wide defaults used by
// DefaultModelMapping (e.g. after settings load). Safe for concurrent use.
func SetRuntimeModelMappingOptions(opts ModelMappingOptions) {
	runtimeMappingOpts.Store(opts)
	runtimeMappingVersion.Add(1)
}

// RuntimeModelMappingVersion changes whenever runtime mapping options change.
// Account-level caches include it so settings updates take effect without a restart.
func RuntimeModelMappingVersion() uint64 {
	return runtimeMappingVersion.Load()
}

// RuntimeModelMappingOptions returns the last options set via SetRuntimeModelMappingOptions.
func RuntimeModelMappingOptions() ModelMappingOptions {
	if v := runtimeMappingOpts.Load(); v != nil {
		if opts, ok := v.(ModelMappingOptions); ok {
			return opts
		}
	}
	return ModelMappingOptions{}
}

const (
	DefaultTextModel = "grok-4.5"

	DefaultImagineImageQualityModel  = "grok-imagine-image-quality"
	DefaultImagineImageFastModel     = "grok-imagine-image"
	DefaultImagineVideoModel         = "grok-imagine-video"
	DefaultImagineVideo15LegacyModel = "grok-imagine-video-1.5"
	DefaultImagineVideo15Model       = "grok-imagine-video-1.5-preview"
)

// ModelMappingOptions controls optional expansions of the default mapping.
// Cross-client wildcards (gpt-*/claude-*) default ON via settings
// grok_cross_client_model_map_enabled so Codex/Claude clients keep working
// against Grok groups (map to DefaultText / grok-4.5). Operators may disable.
type ModelMappingOptions struct {
	// DefaultText is the target for empty models and optional cross-client maps.
	// Empty → DefaultTextModel (grok-4.5).
	DefaultText string
	// EnableCrossClientMap merges gpt-*/codex-*/o*/claude-* → DefaultText.
	EnableCrossClientMap bool
}

func (o ModelMappingOptions) defaultText() string {
	if t := strings.TrimSpace(o.DefaultText); t != "" {
		return t
	}
	return DefaultTextModel
}

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

// DefaultModelMapping returns native Grok/Imagine identity + aliases, using
// runtime options (default text model / optional cross-client wildcards).
// Does NOT enable gpt-*/claude-* unless SetRuntimeModelMappingOptions enables them.
func DefaultModelMapping() map[string]string {
	return ModelMappingWithOptions(RuntimeModelMappingOptions())
}

// ModelMappingWithOptions builds the default Grok mapping with optional
// cross-client wildcards and a configurable default text model.
func ModelMappingWithOptions(opts ModelMappingOptions) map[string]string {
	defaultText := opts.defaultText()
	mapping := make(map[string]string, len(defaultModels)+len(grokTextResponsesModelAliases)+48)
	for _, model := range defaultModels {
		mapping[model.ID] = model.ID
	}
	for alias, canonical := range grokTextResponsesModelAliases {
		// Remap aliases that pointed at DefaultTextModel constant to runtime default.
		if canonical == DefaultTextModel {
			mapping[alias] = defaultText
		} else {
			mapping[alias] = canonical
		}
	}
	// Imagine aliases / legacy IDs → official catalog.
	mapping["grok-imagine"] = DefaultImagineImageQualityModel
	mapping["grok-imagine-1"] = DefaultImagineImageQualityModel
	// Backward-compatible client alias; xAI exposes image editing through the
	// image-quality model rather than a separate grok-imagine-edit model.
	mapping["grok-imagine-edit"] = DefaultImagineImageQualityModel
	mapping["grok-imagine-image"] = DefaultImagineImageFastModel
	mapping["grok-imagine-image-quality"] = DefaultImagineImageQualityModel
	// Keep official IDs as identity so client-requested model strings are not
	// rewritten on the wire (pricing still canonicalizes 1.5* via CanonicalImagineVideoModel).
	mapping["grok-imagine-video"] = DefaultImagineVideoModel
	mapping["grok-imagine-video-1.5"] = DefaultImagineVideo15LegacyModel
	mapping["grok-imagine-video-1.5-preview"] = DefaultImagineVideo15Model
	// Informal alias only:
	mapping["grok-video-1.5"] = DefaultImagineVideo15Model

	if opts.EnableCrossClientMap {
		// Codex / OpenAI Responses client defaults (wildcard patterns).
		mapping["gpt-*"] = defaultText
		mapping["codex-*"] = defaultText
		mapping["o1*"] = defaultText
		mapping["o3*"] = defaultText
		mapping["o4*"] = defaultText
		// Claude Code defaults when operators intentionally enable bridging.
		mapping["claude-*"] = defaultText
	}
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

// ResolveDefaultTextModel returns defaultText (or DefaultTextModel) when model is empty.
func ResolveDefaultTextModel(model string, defaultText ...string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	if len(defaultText) > 0 && strings.TrimSpace(defaultText[0]) != "" {
		return strings.TrimSpace(defaultText[0])
	}
	return DefaultTextModel
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
