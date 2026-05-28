package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePricingData_ParsesPriorityAndServiceTierFields(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000625,
			"input_cost_per_token_priority": 0.000125,
			"output_cost_per_token": 0.000375,
			"output_cost_per_token_priority": 0.00075,
			"cache_creation_input_token_cost": 0.0000625,
			"cache_read_input_token_cost": 0.00000625,
			"cache_read_input_token_cost_priority": 0.0000125,
			"supports_service_tier": true,
			"supports_prompt_caching": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 125e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 750e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 12.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestGetModelPricing_Gpt53CodexSparkUsesGpt51CodexPricing(t *testing.T) {
	sparkPricing := &LiteLLMModelPricing{InputCostPerToken: 1}
	gpt53Pricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.3":       gpt53Pricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex-spark")
	require.Same(t, sparkPricing, got)
}

func TestGetModelPricing_NormalizesOpenAIModelAliasSpelling(t *testing.T) {
	sparkPricing := &LiteLLMModelPricing{InputCostPerToken: 1}
	miniPricing := &LiteLLMModelPricing{InputCostPerToken: 2}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.4-mini":  miniPricing,
		},
	}

	require.Same(t, sparkPricing, svc.GetModelPricing("openai/GPT_5.3CodexSpark"))
	require.Same(t, miniPricing, svc.GetModelPricing("models/gpt5.4mini"))
}

func TestGetModelPricing_Gpt53CodexFallbackStillUsesGpt52Codex(t *testing.T) {
	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)
}

func TestGetModelPricing_OpenAIFallbackMatchedLoggedAsInfo(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)

	require.True(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "info"))
	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "warn"))
}

func TestGetModelPricing_Gpt54UsesStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": &LiteLLMModelPricing{InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4")
	require.NotNil(t, got)
	require.InDelta(t, 62.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 125e-6, got.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 375e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 750e-6, got.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 6.25e-6, got.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 12.5e-6, got.CacheReadInputTokenCostPriority, 1e-12)
	require.InDelta(t, 62.5e-6, got.CacheCreationInputTokenCost, 1e-12)
	require.Equal(t, 272000, got.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, got.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, got.LongContextOutputCostMultiplier, 1e-12)
}

func TestGetModelPricing_Gpt55UsesStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": &LiteLLMModelPricing{InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.5")
	require.NotNil(t, got)
	require.InDelta(t, 125e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 312.5e-6, got.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 750e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 1875e-6, got.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 12.5e-6, got.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 31.25e-6, got.CacheReadInputTokenCostPriority, 1e-12)
	require.InDelta(t, 125e-6, got.CacheCreationInputTokenCost, 1e-12)
	require.Equal(t, 272000, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_Gpt54MiniUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-mini")
	require.NotNil(t, got)
	require.InDelta(t, 7.5e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.5e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 7.5e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_Gpt54NanoUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-nano")
	require.NotNil(t, got)
	require.InDelta(t, 2e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.25e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_ImageModelDoesNotFallbackToTextModel(t *testing.T) {
	imagePricing := &LiteLLMModelPricing{InputCostPerToken: 3}
	textPricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2": imagePricing,
			"gpt-5.4":     textPricing,
		},
	}

	got := svc.GetModelPricing("gpt-image-3")
	require.Same(t, imagePricing, got)
	require.NotSame(t, textPricing, got)
}

func TestParsePricingData_PreservesPriorityAndServiceTierFields(t *testing.T) {
	raw := map[string]any{
		"gpt-5.4": map[string]any{
			"input_cost_per_token":                 62.5e-6,
			"input_cost_per_token_priority":        125e-6,
			"output_cost_per_token":                375e-6,
			"output_cost_per_token_priority":       750e-6,
			"cache_read_input_token_cost":          6.25e-6,
			"cache_read_input_token_cost_priority": 12.5e-6,
			"supports_service_tier":                true,
			"supports_prompt_caching":              true,
			"litellm_provider":                     "openai",
			"mode":                                 "chat",
		},
	}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	svc := &PricingService{}
	pricingMap, err := svc.parsePricingData(body)
	require.NoError(t, err)

	pricing := pricingMap["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 62.5e-6, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 125e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 375e-6, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 750e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 6.25e-6, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 12.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_PreservesServiceTierPriorityFields(t *testing.T) {
	svc := &PricingService{}
	pricingData, err := svc.parsePricingData([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000625,
			"input_cost_per_token_priority": 0.000125,
			"output_cost_per_token": 0.000375,
			"output_cost_per_token_priority": 0.00075,
			"cache_read_input_token_cost": 0.00000625,
			"cache_read_input_token_cost_priority": 0.0000125,
			"supports_service_tier": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	pricing := pricingData["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.0000625, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 0.000125, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.000375, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.00075, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.00000625, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.0000125, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}
