package service

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const (
	VideoPriceFamilyGrokImagineVideo   = "grok-imagine-video"
	VideoPriceFamilyGrokImagineVideo15 = "grok-imagine-video-1.5"
)

// CanonicalGrokImagineVideoPriceFamily 将预览版与兼容别名归一到持久化价格族。
func CanonicalGrokImagineVideoPriceFamily(model string) string {
	if canonical := xai.CanonicalImagineVideoModel(model); canonical != "" {
		switch canonical {
		case xai.DefaultImagineVideo15Model:
			return VideoPriceFamilyGrokImagineVideo15
		case xai.DefaultImagineVideoModel:
			return VideoPriceFamilyGrokImagineVideo
		}
	}
	model = strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"xai/", "x-ai/", "grok/"} {
		if strings.HasPrefix(model, prefix) {
			model = strings.TrimPrefix(model, prefix)
			break
		}
	}
	switch model {
	case "grok-imagine-video-1.5", "grok-imagine-video-1.5-preview", "grok-video-1.5":
		return VideoPriceFamilyGrokImagineVideo15
	case "grok-imagine-video", "grok-imagine-video-preview", "grok-video", "grok-video-latest":
		return VideoPriceFamilyGrokImagineVideo
	default:
		return ""
	}
}

func NormalizeVideoModelPrices(input map[string]map[string]float64) map[string]map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]map[string]float64)
	modelKeys := make([]string, 0, len(input))
	for model := range input {
		modelKeys = append(modelKeys, model)
	}
	sort.Strings(modelKeys)
	for _, model := range modelKeys {
		tiers := input[model]
		family := CanonicalGrokImagineVideoPriceFamily(model)
		if family == "" {
			continue
		}
		tierKeys := make([]string, 0, len(tiers))
		for resolution := range tiers {
			tierKeys = append(tierKeys, resolution)
		}
		sort.Strings(tierKeys)
		for _, resolution := range tierKeys {
			price := tiers[resolution]
			if price < 0 {
				continue
			}
			normalized, ok := NormalizeVideoBillingResolution(resolution)
			if !ok {
				slog.Warn(
					"video_model_prices_unknown_resolution_dropped",
					"model", model,
					"family", family,
					"resolution", resolution,
				)
				continue
			}
			if result[family] == nil {
				result[family] = make(map[string]float64)
			}
			if existing, exists := result[family][normalized]; exists && existing != price {
				slog.Warn(
					"video_model_prices_conflicting_tier_price",
					"family", family,
					"resolution", normalized,
					"existing", existing,
					"new", price,
				)
			}
			result[family][normalized] = price
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func LookupVideoModelPrice(prices map[string]map[string]float64, model, resolution string) *float64 {
	family := CanonicalGrokImagineVideoPriceFamily(model)
	if family == "" {
		return nil
	}
	tiers := prices[family]
	if len(tiers) == 0 {
		return nil
	}
	price, ok := tiers[NormalizeVideoBillingResolutionOrDefault(resolution)]
	if !ok {
		return nil
	}
	return &price
}
