//go:build unit

package service

import (
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newTestBillingService() *BillingService {
	return NewBillingService(&config.Config{}, nil)
}

func TestCalculateCost_BasicComputation(t *testing.T) {
	svc := newTestBillingService()

	// 使用 claude-sonnet-4 的回退价格：Input $3/MTok, Output $15/MTok
	tokens := UsageTokens{
		InputTokens:  1000,
		OutputTokens: 500,
	}
	cost, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	// 1000 * 3e-6 = 0.003, 500 * 15e-6 = 0.0075
	expectedInput := 1000 * 3e-6
	expectedOutput := 500 * 15e-6
	require.InDelta(t, expectedInput, cost.InputCost, 1e-10)
	require.InDelta(t, expectedOutput, cost.OutputCost, 1e-10)
	require.InDelta(t, expectedInput+expectedOutput, cost.TotalCost, 1e-10)
	require.InDelta(t, expectedInput+expectedOutput, cost.ActualCost, 1e-10)
}

func TestCalculateCost_WithCacheTokens(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheCreationTokens: 2000,
		CacheReadTokens:     3000,
	}
	cost, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	expectedCacheCreation := 2000 * 3.75e-6
	expectedCacheRead := 3000 * 0.3e-6
	require.InDelta(t, expectedCacheCreation, cost.CacheCreationCost, 1e-10)
	require.InDelta(t, expectedCacheRead, cost.CacheReadCost, 1e-10)

	expectedTotal := cost.InputCost + cost.OutputCost + expectedCacheCreation + expectedCacheRead
	require.InDelta(t, expectedTotal, cost.TotalCost, 1e-10)
}

func TestCalculateCost_RateMultiplier(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	cost1x, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	cost2x, err := svc.CalculateCost("claude-sonnet-4", tokens, 2.0)
	require.NoError(t, err)

	// TotalCost 不受倍率影响，ActualCost 翻倍
	require.InDelta(t, cost1x.TotalCost, cost2x.TotalCost, 1e-10)
	require.InDelta(t, cost1x.ActualCost*2, cost2x.ActualCost, 1e-10)
}

func TestCalculateCost_ZeroMultiplierDefaultsToOne(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 1000}

	costZero, err := svc.CalculateCost("claude-sonnet-4", tokens, 0)
	require.NoError(t, err)

	costOne, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	require.InDelta(t, costOne.ActualCost, costZero.ActualCost, 1e-10)
}

func TestCalculateCost_NegativeMultiplierDefaultsToOne(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 1000}

	costNeg, err := svc.CalculateCost("claude-sonnet-4", tokens, -1.0)
	require.NoError(t, err)

	costOne, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	require.InDelta(t, costOne.ActualCost, costNeg.ActualCost, 1e-10)
}

func TestGetModelPricing_FallbackMatchesByFamily(t *testing.T) {
	svc := newTestBillingService()

	tests := []struct {
		model         string
		expectedInput float64
	}{
		{"claude-opus-4.5-20250101", 5e-6},
		{"claude-3-opus-20240229", 15e-6},
		{"claude-sonnet-4-20250514", 3e-6},
		{"claude-3-5-sonnet-20241022", 3e-6},
		{"claude-3-5-haiku-20241022", 1e-6},
		{"claude-3-haiku-20240307", 0.25e-6},
	}

	for _, tt := range tests {
		pricing, err := svc.GetModelPricing(tt.model)
		require.NoError(t, err, "模型 %s", tt.model)
		require.InDelta(t, tt.expectedInput, pricing.InputPricePerToken, 1e-12, "模型 %s 输入价格", tt.model)
	}
}

func TestGetModelPricing_CaseInsensitive(t *testing.T) {
	svc := newTestBillingService()

	p1, err := svc.GetModelPricing("Claude-Sonnet-4")
	require.NoError(t, err)

	p2, err := svc.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)

	require.Equal(t, p1.InputPricePerToken, p2.InputPricePerToken)
}

func TestGetModelPricing_UnknownModelFallsBackToSonnet(t *testing.T) {
	svc := newTestBillingService()

	// 不包含 opus/sonnet/haiku 关键词的 Claude 模型会走默认 Sonnet 价格
	pricing, err := svc.GetModelPricing("claude-unknown-model")
	require.NoError(t, err)
	require.InDelta(t, 3e-6, pricing.InputPricePerToken, 1e-12)
}

func TestCalculateCostWithLongContext_BelowThreshold(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{
		InputTokens:     50000,
		OutputTokens:    1000,
		CacheReadTokens: 100000,
	}
	// 总输入 150k < 200k 阈值，应走正常计费
	cost, err := svc.CalculateCostWithLongContext("claude-sonnet-4", tokens, 1.0, 200000, 2.0)
	require.NoError(t, err)

	normalCost, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	require.InDelta(t, normalCost.ActualCost, cost.ActualCost, 1e-10)
}

func TestCalculateCostWithLongContext_AboveThreshold_CacheExceedsThreshold(t *testing.T) {
	svc := newTestBillingService()

	// 缓存 210k + 输入 10k = 220k > 200k 阈值
	// 缓存已超阈值：范围内 200k 缓存，范围外 10k 缓存 + 10k 输入
	tokens := UsageTokens{
		InputTokens:     10000,
		OutputTokens:    1000,
		CacheReadTokens: 210000,
	}
	cost, err := svc.CalculateCostWithLongContext("claude-sonnet-4", tokens, 1.0, 200000, 2.0)
	require.NoError(t, err)

	// 范围内：200k cache + 0 input + 1k output
	inRange, _ := svc.CalculateCost("claude-sonnet-4", UsageTokens{
		InputTokens:     0,
		OutputTokens:    1000,
		CacheReadTokens: 200000,
	}, 1.0)

	// 范围外：10k cache + 10k input，倍率 2.0
	outRange, _ := svc.CalculateCost("claude-sonnet-4", UsageTokens{
		InputTokens:     10000,
		CacheReadTokens: 10000,
	}, 2.0)

	require.InDelta(t, inRange.ActualCost+outRange.ActualCost, cost.ActualCost, 1e-10)
}

func TestCalculateCostWithLongContext_AboveThreshold_CacheBelowThreshold(t *testing.T) {
	svc := newTestBillingService()

	// 缓存 100k + 输入 150k = 250k > 200k 阈值
	// 缓存未超阈值：范围内 100k 缓存 + 100k 输入，范围外 50k 输入
	tokens := UsageTokens{
		InputTokens:     150000,
		OutputTokens:    1000,
		CacheReadTokens: 100000,
	}
	cost, err := svc.CalculateCostWithLongContext("claude-sonnet-4", tokens, 1.0, 200000, 2.0)
	require.NoError(t, err)

	require.True(t, cost.ActualCost > 0, "费用应大于 0")

	// 正常费用不含长上下文
	normalCost, _ := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.True(t, cost.ActualCost > normalCost.ActualCost, "长上下文费用应高于正常费用")
}

func TestCalculateCostWithLongContext_DisabledThreshold(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 300000, CacheReadTokens: 0}

	// threshold <= 0 应禁用长上下文计费
	cost1, err := svc.CalculateCostWithLongContext("claude-sonnet-4", tokens, 1.0, 0, 2.0)
	require.NoError(t, err)

	cost2, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	require.InDelta(t, cost2.ActualCost, cost1.ActualCost, 1e-10)
}

func TestCalculateCostWithLongContext_ExtraMultiplierLessEqualOne(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{InputTokens: 300000}

	// extraMultiplier <= 1 应禁用长上下文计费
	cost, err := svc.CalculateCostWithLongContext("claude-sonnet-4", tokens, 1.0, 200000, 1.0)
	require.NoError(t, err)

	normalCost, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	require.InDelta(t, normalCost.ActualCost, cost.ActualCost, 1e-10)
}

func TestCalculateImageCost(t *testing.T) {
	svc := newTestBillingService()

	price := 0.134
	cfg := &ImagePriceConfig{Price1K: &price}
	cost := svc.CalculateImageCost("gpt-image-1", "1K", 3, cfg, 1.0)

	require.InDelta(t, 0.134*3, cost.TotalCost, 1e-10)
	require.InDelta(t, 0.134*3, cost.ActualCost, 1e-10)
}

func TestCalculateSoraVideoCost(t *testing.T) {
	svc := newTestBillingService()

	price := 0.5
	cfg := &SoraPriceConfig{VideoPricePerRequest: &price}
	cost := svc.CalculateSoraVideoCost("sora-video", cfg, 1.0)

	require.InDelta(t, 0.5, cost.TotalCost, 1e-10)
}

func TestCalculateSoraVideoCost_HDModel(t *testing.T) {
	svc := newTestBillingService()

	hdPrice := 1.0
	normalPrice := 0.5
	cfg := &SoraPriceConfig{
		VideoPricePerRequest:   &normalPrice,
		VideoPricePerRequestHD: &hdPrice,
	}
	cost := svc.CalculateSoraVideoCost("sora2pro-hd", cfg, 1.0)
	require.InDelta(t, 1.0, cost.TotalCost, 1e-10)
}

func TestIsModelSupported(t *testing.T) {
	svc := newTestBillingService()

	require.True(t, svc.IsModelSupported("claude-sonnet-4"))
	require.True(t, svc.IsModelSupported("Claude-Opus-4.5"))
	require.True(t, svc.IsModelSupported("claude-3-haiku"))
	require.False(t, svc.IsModelSupported("gpt-4o"))
	require.False(t, svc.IsModelSupported("gemini-pro"))
}

func TestCalculateCost_ZeroTokens(t *testing.T) {
	svc := newTestBillingService()

	cost, err := svc.CalculateCost("claude-sonnet-4", UsageTokens{}, 1.0)
	require.NoError(t, err)
	require.Equal(t, 0.0, cost.TotalCost)
	require.Equal(t, 0.0, cost.ActualCost)
}

func TestCalculateCostWithConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1.5
	svc := NewBillingService(cfg, nil)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	cost, err := svc.CalculateCostWithConfig("claude-sonnet-4", tokens)
	require.NoError(t, err)

	expected, _ := svc.CalculateCost("claude-sonnet-4", tokens, 1.5)
	require.InDelta(t, expected.ActualCost, cost.ActualCost, 1e-10)
}

func TestCalculateCostWithConfig_ZeroMultiplier(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 0
	svc := NewBillingService(cfg, nil)

	tokens := UsageTokens{InputTokens: 1000}
	cost, err := svc.CalculateCostWithConfig("claude-sonnet-4", tokens)
	require.NoError(t, err)

	// 倍率 <=0 时默认 1.0
	expected, _ := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.InDelta(t, expected.ActualCost, cost.ActualCost, 1e-10)
}

func TestGetEstimatedCost(t *testing.T) {
	svc := newTestBillingService()

	est, err := svc.GetEstimatedCost("claude-sonnet-4", 1000, 500)
	require.NoError(t, err)
	require.True(t, est > 0)
}

func TestListSupportedModels(t *testing.T) {
	svc := newTestBillingService()

	models := svc.ListSupportedModels()
	require.NotEmpty(t, models)
	require.GreaterOrEqual(t, len(models), 6)
}

func TestGetPricingServiceStatus_NilService(t *testing.T) {
	svc := newTestBillingService()

	status := svc.GetPricingServiceStatus()
	require.NotNil(t, status)
	require.Equal(t, "using fallback", status["last_updated"])
}

func TestForceUpdatePricing_NilService(t *testing.T) {
	svc := newTestBillingService()

	err := svc.ForceUpdatePricing()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not initialized")
}

func TestCalculateSoraImageCost(t *testing.T) {
	svc := newTestBillingService()

	price360 := 0.05
	price540 := 0.08
	cfg := &SoraPriceConfig{ImagePrice360: &price360, ImagePrice540: &price540}

	cost := svc.CalculateSoraImageCost("360", 2, cfg, 1.0)
	require.InDelta(t, 0.10, cost.TotalCost, 1e-10)

	cost540 := svc.CalculateSoraImageCost("540", 1, cfg, 2.0)
	require.InDelta(t, 0.08, cost540.TotalCost, 1e-10)
	require.InDelta(t, 0.16, cost540.ActualCost, 1e-10)
}

func TestCalculateSoraImageCost_ZeroCount(t *testing.T) {
	svc := newTestBillingService()
	cost := svc.CalculateSoraImageCost("360", 0, nil, 1.0)
	require.Equal(t, 0.0, cost.TotalCost)
}

func TestCalculateSoraVideoCost_NilConfig(t *testing.T) {
	svc := newTestBillingService()
	cost := svc.CalculateSoraVideoCost("sora-video", nil, 1.0)
	require.Equal(t, 0.0, cost.TotalCost)
}

func TestCalculateCostWithLongContext_PropagatesError(t *testing.T) {
	// 使用空的 fallback prices 让 GetModelPricing 失败
	svc := &BillingService{
		cfg:            &config.Config{},
		fallbackPrices: make(map[string]*ModelPricing),
	}

	tokens := UsageTokens{InputTokens: 300000, CacheReadTokens: 0}
	_, err := svc.CalculateCostWithLongContext("unknown-model", tokens, 1.0, 200000, 2.0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pricing not found")
}

func TestCalculateCost_SupportsCacheBreakdown(t *testing.T) {
	svc := &BillingService{
		cfg: &config.Config{},
		fallbackPrices: map[string]*ModelPricing{
			"claude-sonnet-4": {
				InputPricePerToken:     3e-6,
				OutputPricePerToken:    15e-6,
				SupportsCacheBreakdown: true,
				CacheCreation5mPrice:   4.0, // per million tokens
				CacheCreation1hPrice:   5.0, // per million tokens
			},
		},
	}

	tokens := UsageTokens{
		InputTokens:           1000,
		OutputTokens:          500,
		CacheCreation5mTokens: 100000,
		CacheCreation1hTokens: 50000,
	}
	cost, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	expected5m := float64(100000) / 1_000_000 * 4.0
	expected1h := float64(50000) / 1_000_000 * 5.0
	require.InDelta(t, expected5m+expected1h, cost.CacheCreationCost, 1e-10)
}

func TestCalculateCost_LargeTokenCount(t *testing.T) {
	svc := newTestBillingService()

	tokens := UsageTokens{
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
	}
	cost, err := svc.CalculateCost("claude-sonnet-4", tokens, 1.0)
	require.NoError(t, err)

	// Input: 1M * 3e-6 = $3, Output: 1M * 15e-6 = $15
	require.InDelta(t, 3.0, cost.InputCost, 1e-6)
	require.InDelta(t, 15.0, cost.OutputCost, 1e-6)
	require.False(t, math.IsNaN(cost.TotalCost))
	require.False(t, math.IsInf(cost.TotalCost, 0))
}
