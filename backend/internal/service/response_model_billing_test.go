package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseModelBillingDeclaration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		source          string
		model           string
		conflict        bool
		nonTokenBilled  bool
		billingEligible bool
		want            string
	}{
		{name: "opted in", source: BillingModelSourceResponse, model: " gpt-5.4-nano ", billingEligible: true, want: "gpt-5.4-nano"},
		{name: "other source", source: BillingModelSourceChannelMapped, model: "gpt-5.4-nano"},
		{name: "empty source", model: "gpt-5.4-nano"},
		{name: "conflict", source: BillingModelSourceResponse, model: "gpt-5.4-nano", conflict: true, billingEligible: true},
		{name: "non token billing", source: BillingModelSourceResponse, model: "gpt-5.4-nano", nonTokenBilled: true, billingEligible: true},
		{name: "failed response", source: BillingModelSourceResponse, model: "gpt-5.4-nano"},
		{name: "blank declaration", source: BillingModelSourceResponse, model: "   ", billingEligible: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, responseModelBillingDeclaration(tt.source, tt.model, tt.conflict, tt.nonTokenBilled, tt.billingEligible))
		})
	}
}

func TestResponseModelBillingAdoptable(t *testing.T) {
	t.Parallel()
	cost := func(total float64) *CostBreakdown {
		return &CostBreakdown{TotalCost: total, ActualCost: total}
	}
	tests := []struct {
		name                  string
		baseline              *CostBreakdown
		response              *CostBreakdown
		baselineChannelPriced bool
		responseChannelPriced bool
		want                  bool
	}{
		{name: "cheaper", baseline: cost(2), response: cost(1), want: true},
		{name: "same cost", baseline: cost(1), response: cost(1), want: true},
		{name: "floating point epsilon", baseline: cost(1), response: cost(1 + responseModelBillingCostEpsilon/2), want: true},
		{name: "more expensive", baseline: cost(1), response: cost(2)},
		{name: "zero cannot replace paid", baseline: cost(1), response: cost(0)},
		{name: "zero may replace zero", baseline: cost(0), response: cost(0), want: true},
		{name: "channel price cannot fall through global", baseline: cost(2), response: cost(1), baselineChannelPriced: true},
		{name: "channel price to channel price", baseline: cost(2), response: cost(1), baselineChannelPriced: true, responseChannelPriced: true, want: true},
		{name: "nil baseline", response: cost(1)},
		{name: "nil response", baseline: cost(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, responseModelBillingAdoptable(tt.baseline, tt.response, tt.baselineChannelPriced, tt.responseChannelPriced))
		})
	}
}

func TestBillingServiceHasIdentifiedTokenPricingRejectsFamilyGuesses(t *testing.T) {
	t.Parallel()
	billing := &BillingService{fallbackPrices: map[string]*ModelPricing{
		"known-model": {InputPricePerToken: 1e-6, OutputPricePerToken: 2e-6},
	}}

	require.True(t, billing.HasIdentifiedTokenPricing("  KNOWN-MODEL  "))
	require.False(t, billing.HasIdentifiedTokenPricing(""))
	require.False(t, billing.HasIdentifiedTokenPricing("totally-made-up-haiku-v9"))
}

func TestBillingServiceHasIdentifiedTokenPricingRejectsDynamicFamilyGuess(t *testing.T) {
	t.Parallel()
	haikuPricing := &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 5e-6}
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"claude-3-haiku": haikuPricing,
	}}
	billing := NewBillingService(nil, pricing)

	const forged = "totally-made-up-haiku-v9"
	require.Same(t, haikuPricing, pricing.GetModelPricing(forged), "test precondition: ordinary lookup should exercise its broad family fallback")
	require.False(t, billing.HasIdentifiedTokenPricing(forged), "family-guessed pricing must not qualify an untrusted response model")
}

func TestPricingServiceGetIdentifiedModelPricingAllowsDeterministicVariants(t *testing.T) {
	t.Parallel()
	sonnetPricing := &LiteLLMModelPricing{InputCostPerToken: 3e-6}
	opusPricing := &LiteLLMModelPricing{InputCostPerToken: 5e-6}
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"claude-sonnet-4":          sonnetPricing,
		"claude-opus-4.5-20251101": opusPricing,
	}}

	require.Same(t, sonnetPricing, pricing.GetIdentifiedModelPricing(" CLAUDE-SONNET-4-20250514 "))
	require.Same(t, opusPricing, pricing.GetIdentifiedModelPricing("claude-opus-4-5-20251101"))
	require.Nil(t, pricing.GetIdentifiedModelPricing("totally-made-up-haiku-v9"))
}
