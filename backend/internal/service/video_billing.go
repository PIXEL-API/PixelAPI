package service

import (
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
	switch {
	case model == "grok-imagine-video-1.5", model == "grok-imagine-video-1.5-preview", model == "grok-video-1.5":
		return VideoPriceFamilyGrokImagineVideo15
	case model == "grok-imagine-video", model == "grok-imagine-video-preview", model == "grok-video", model == "grok-video-latest":
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
	for model, tiers := range input {
		family := CanonicalGrokImagineVideoPriceFamily(model)
		if family == "" {
			continue
		}
		for resolution, price := range tiers {
			if price < 0 {
				continue
			}
			if result[family] == nil {
				result[family] = make(map[string]float64)
			}
			result[family][NormalizeVideoBillingResolutionOrDefault(resolution)] = price
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
