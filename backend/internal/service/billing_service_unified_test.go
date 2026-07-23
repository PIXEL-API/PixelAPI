//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// CalculateCostUnified
// ---------------------------------------------------------------------------

func TestCalculateCostUnified_NilResolver_FallsBackToOldPath(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	input := CostInput{
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 1.0,
		Resolver:       nil, // no resolver
	}
	cost, err := svc.CalculateCostUnified(input)
	require.NoError(t, err)

	// Should match the old-path result exactly
	expected, err := svc.calculateCostInternal("claude-sonnet-4", tokens, 1.0, "", nil)
	require.NoError(t, err)
	require.InDelta(t, expected.TotalCost, cost.TotalCost, 1e-10)
	require.InDelta(t, expected.ActualCost, cost.ActualCost, 1e-10)
	// BillingMode is NOT set by old path through CalculateCostUnified (resolver == nil)
	require.Empty(t, cost.BillingMode)
}

func TestCalculateCostUnified_NilResolverLongContextDefaultsOffAndSupportsExplicitEnable(t *testing.T) {
	svc := newTestBillingService()
	tokens := UsageTokens{InputTokens: 300000, OutputTokens: 1000}

	disabledCost, err := svc.CalculateCostUnified(CostInput{
		Model:          "gpt-5.4-2026-03-05",
		Tokens:         tokens,
		RateMultiplier: 1,
	})
	require.NoError(t, err)
	require.InDelta(t, float64(tokens.InputTokens)*62.5e-6, disabledCost.InputCost, 1e-10)
	require.InDelta(t, float64(tokens.OutputTokens)*375e-6, disabledCost.OutputCost, 1e-10)

	enabled := true
	enabledCost, err := svc.CalculateCostUnified(CostInput{
		Model:                     "gpt-5.4-2026-03-05",
		Tokens:                    tokens,
		RateMultiplier:            1,
		LongContextBillingEnabled: &enabled,
	})
	require.NoError(t, err)
	require.InDelta(t, disabledCost.InputCost*2, enabledCost.InputCost, 1e-10)
	require.InDelta(t, disabledCost.OutputCost*1.5, enabledCost.OutputCost, 1e-10)
}

func TestCalculateCostUnified_TokenMode(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	input := CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 1.5,
		Resolver:       resolver,
	}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.NotNil(t, cost)

	// Verify token billing: Input: 1000*3e-6=0.003, Output: 500*15e-6=0.0075
	expectedTotal := 1000*3e-6 + 500*15e-6
	require.InDelta(t, expectedTotal, cost.TotalCost, 1e-10)
	require.InDelta(t, expectedTotal*1.5, cost.ActualCost, 1e-10)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
}

func TestCalculateCostUnified_ChannelLongContextPolicy(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)
	basePricing, err := bs.GetModelPricing("gpt-5.6-sol")
	require.NoError(t, err)

	tests := []struct {
		name           string
		tokens         UsageTokens
		enabled        *bool
		threshold      *int
		wantInputCost  float64
		wantOutputCost float64
	}{
		{
			name:           "nil keeps long context multiplier disabled by default",
			tokens:         UsageTokens{InputTokens: 300000, OutputTokens: 1000},
			wantInputCost:  300000 * 5e-6,
			wantOutputCost: 1000 * 30e-6,
		},
		{
			name:           "explicit false keeps multiplier disabled",
			tokens:         UsageTokens{InputTokens: 300000, OutputTokens: 1000},
			enabled:        testPtrBool(false),
			wantInputCost:  300000 * 5e-6,
			wantOutputCost: 1000 * 30e-6,
		},
		{
			name:           "explicit true uses administrator threshold",
			tokens:         UsageTokens{InputTokens: 2000, OutputTokens: 100},
			enabled:        testPtrBool(true),
			threshold:      testPtrInt(1000),
			wantInputCost:  2000 * 5e-6 * 2,
			wantOutputCost: 100 * 30e-6 * 1.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, err := bs.CalculateCostUnified(CostInput{
				Ctx:            context.Background(),
				Model:          "gpt-5.6-sol",
				Tokens:         tt.tokens,
				RateMultiplier: 1,
				Resolver:       resolver,
				Resolved: &ResolvedPricing{
					Mode:                           BillingModeToken,
					BasePricing:                    basePricing,
					LongContextPricingEnabled:      tt.enabled,
					LongContextInputTokenThreshold: tt.threshold,
				},
			})
			require.NoError(t, err)
			require.InDelta(t, tt.wantInputCost, cost.InputCost, 1e-12)
			require.InDelta(t, tt.wantOutputCost, cost.OutputCost, 1e-12)
		})
	}
}

func TestCalculateCostUnified_EnabledLongContextPolicyFailsWithoutThreshold(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)
	basePricing, err := bs.GetModelPricing("gpt-5.6-sol")
	require.NoError(t, err)

	_, err = bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "gpt-5.6-sol",
		Tokens:         UsageTokens{InputTokens: 300000},
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode:                      BillingModeToken,
			BasePricing:               basePricing,
			LongContextPricingEnabled: testPtrBool(true),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an input token threshold")
}

func TestCalculateCostUnified_EnabledLongContextPolicyFailsWithIntervals(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)
	basePricing, err := bs.GetModelPricing("gpt-5.6-sol")
	require.NoError(t, err)

	_, err = bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "gpt-5.6-sol",
		Tokens:         UsageTokens{InputTokens: 2000},
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode:                           BillingModeToken,
			BasePricing:                    basePricing,
			Intervals:                      []PricingInterval{{MinTokens: 0, InputPrice: testPtrFloat64(5e-6)}},
			LongContextPricingEnabled:      testPtrBool(true),
			LongContextInputTokenThreshold: testPtrInt(1000),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicts with context pricing intervals")
}

func TestCalculateCostUnified_ImageTokenPrices(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:   context.Background(),
		Model: "gpt-image-2",
		Tokens: UsageTokens{
			TextInputTokens:      22,
			ImageInputTokens:     10,
			OutputTokens:         196,
			CacheReadTokens:      4,
			ImageCacheReadTokens: 3,
			ImageOutputTokens:    196,
		},
		RateMultiplier: 8.0,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode: BillingModeToken,
			BasePricing: &ModelPricing{
				InputPricePerToken:          5e-6,
				ImageInputPricePerToken:     8e-6,
				CacheReadPricePerToken:      1.25e-6,
				ImageCacheReadPricePerToken: 2e-6,
				ImageOutputPricePerToken:    30e-6,
			},
		},
	})
	require.NoError(t, err)

	expectedTextInput := 22 * 5e-6
	expectedImageInput := 10 * 8e-6
	expectedCacheRead := 1*1.25e-6 + 3*2e-6
	expectedImageOutput := 196 * 30e-6
	expectedTotal := expectedTextInput + expectedImageInput + expectedCacheRead + expectedImageOutput
	require.InDelta(t, expectedTextInput, cost.InputCost, 1e-12)
	require.InDelta(t, expectedImageInput, cost.ImageInputCost, 1e-12)
	require.InDelta(t, 0.0, cost.OutputCost, 1e-12)
	require.InDelta(t, expectedImageOutput, cost.ImageOutputCost, 1e-12)
	require.InDelta(t, expectedCacheRead, cost.CacheReadCost, 1e-12)
	require.InDelta(t, expectedTotal, cost.TotalCost, 1e-12)
	require.InDelta(t, expectedTotal*8, cost.ActualCost, 1e-12)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
}

func TestCalculateCostUnified_PerRequestMode(t *testing.T) {
	// Set up a ChannelService with a per-request pricing channel
	cs := newTestChannelServiceWithCache(t, &channelCache{
		pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
			{groupID: 1, model: "claude-sonnet-4"}: {
				BillingMode:     BillingModePerRequest,
				PerRequestPrice: testPtrFloat64(0.05),
			},
		},
		channelByGroupID: map[int64]*Channel{
			1: {ID: 1, Status: StatusActive},
		},
		groupPlatform:           map[int64]string{1: ""},
		wildcardByGroupPlatform: map[channelGroupPlatformKey][]*wildcardPricingEntry{},
		mappingByGroupModel:     map[channelModelKey]string{},
		wildcardMappingByGP:     map[channelGroupPlatformKey][]*wildcardMappingEntry{},
		byID:                    map[int64]*Channel{},
	})

	bs := newTestBillingService()
	resolver := NewModelPricingResolver(cs, bs)
	groupID := int64(1)

	input := CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		GroupID:        &groupID,
		Tokens:         UsageTokens{InputTokens: 100, OutputTokens: 50},
		RequestCount:   3,
		RateMultiplier: 2.0,
		Resolver:       resolver,
	}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.NotNil(t, cost)

	// 3 requests * $0.05 = $0.15
	require.InDelta(t, 0.15, cost.TotalCost, 1e-10)
	// ActualCost = 0.15 * 2.0 = 0.30
	require.InDelta(t, 0.30, cost.ActualCost, 1e-10)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
}

func TestCalculateCostUnified_ImageMode(t *testing.T) {
	cs := newTestChannelServiceWithCache(t, &channelCache{
		pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
			{groupID: 2, model: "gemini-image"}: {
				BillingMode:     BillingModeImage,
				PerRequestPrice: testPtrFloat64(0.10),
			},
		},
		channelByGroupID: map[int64]*Channel{
			2: {ID: 2, Status: StatusActive},
		},
		groupPlatform:           map[int64]string{2: ""},
		wildcardByGroupPlatform: map[channelGroupPlatformKey][]*wildcardPricingEntry{},
		mappingByGroupModel:     map[channelModelKey]string{},
		wildcardMappingByGP:     map[channelGroupPlatformKey][]*wildcardMappingEntry{},
		byID:                    map[int64]*Channel{},
	})

	bs := &BillingService{
		cfg:            &config.Config{},
		fallbackPrices: map[string]*ModelPricing{},
	}
	resolver := NewModelPricingResolver(cs, bs)
	groupID := int64(2)

	input := CostInput{
		Ctx:            context.Background(),
		Model:          "gemini-image",
		GroupID:        &groupID,
		Tokens:         UsageTokens{},
		RequestCount:   2,
		RateMultiplier: 1.0,
		Resolver:       resolver,
	}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.NotNil(t, cost)

	// 2 * $0.10 = $0.20
	require.InDelta(t, 0.20, cost.TotalCost, 1e-10)
	require.InDelta(t, 0.20, cost.ActualCost, 1e-10)
	require.Equal(t, string(BillingModeImage), cost.BillingMode)
}

func TestCalculateCostUnified_ExplicitFreeSizeTierDoesNotFallBackToDefaultPrice(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)
	freePrice := 0.0

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "image-model",
		RequestCount:   2,
		SizeTier:       "1K",
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved: &ResolvedPricing{
			Mode:                   BillingModeImage,
			DefaultPerRequestPrice: 0.25,
			RequestTiers: []PricingInterval{{
				TierLabel:       "1K",
				PerRequestPrice: &freePrice,
			}},
		},
	})
	require.NoError(t, err)
	require.Zero(t, cost.TotalCost)
	require.Zero(t, cost.ActualCost)
}

// TestCalculateCostUnified_RateMultiplierZeroProducesZero 锁定新行为：
// 保存时强制 > 0；若 0 仍泄漏到计费层，按 0 计费（而非历史上的 1.0）。
func TestCalculateCostUnified_RateMultiplierZeroProducesZero(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: 0,
		Resolver:       resolver,
	})
	require.NoError(t, err)
	require.Greater(t, cost.TotalCost, 0.0)
	require.InDelta(t, 0.0, cost.ActualCost, 1e-10)
}

// TestCalculateCostUnified_NegativeRateMultiplierClampedToZero 锁定新行为：
// 负数倍率按 0 计费，避免历史的 <=0 → 1.0 把配置异常静默按标准价扣费。
func TestCalculateCostUnified_NegativeRateMultiplierClampedToZero(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         tokens,
		RateMultiplier: -5.0,
		Resolver:       resolver,
	})
	require.NoError(t, err)
	require.Greater(t, cost.TotalCost, 0.0)
	require.InDelta(t, 0.0, cost.ActualCost, 1e-10)
}

func TestCalculateCostUnified_BillingModeFieldFilled(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         UsageTokens{InputTokens: 100},
		RateMultiplier: 1.0,
		Resolver:       resolver,
	})
	require.NoError(t, err)
	require.Equal(t, "token", cost.BillingMode)
}

func TestCalculateCostUnified_UsesPreResolvedPricing(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	// Pre-resolve with per_request mode to verify it's used instead of re-resolving
	preResolved := &ResolvedPricing{
		Mode:                   BillingModePerRequest,
		DefaultPerRequestPrice: 0.07,
	}

	cost, err := bs.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		Tokens:         UsageTokens{InputTokens: 100},
		RequestCount:   2,
		RateMultiplier: 1.0,
		Resolver:       resolver,
		Resolved:       preResolved,
	})
	require.NoError(t, err)
	require.NotNil(t, cost)

	// 2 * $0.07 = $0.14
	require.InDelta(t, 0.14, cost.TotalCost, 1e-10)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestChannelServiceWithCache creates a ChannelService with a pre-populated
// cache snapshot, bypassing the repository layer entirely.
func newTestChannelServiceWithCache(t *testing.T, cache *channelCache) *ChannelService {
	t.Helper()
	cs := &ChannelService{}
	cache.loadedAt = time.Now()
	cs.cache.Store(cache)
	return cs
}
