//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFindActiveTimeRange(t *testing.T) {
	ranges := []PricingTimeRange{
		{StartMinute: 540, EndMinute: 720},  // 09:00–12:00
		{StartMinute: 840, EndMinute: 1080}, // 14:00–18:00
	}

	require.NotNil(t, FindActiveTimeRange(ranges, 630))
	require.NotNil(t, FindActiveTimeRange(ranges, 540))
	require.Nil(t, FindActiveTimeRange(ranges, 720)) // end 开区间
	require.Nil(t, FindActiveTimeRange(ranges, 539))
	require.Nil(t, FindActiveTimeRange(ranges, 1200))
	require.Nil(t, FindActiveTimeRange(nil, 630))
}

func TestValidateTimeRanges(t *testing.T) {
	valid := []PricingTimeRange{
		{StartMinute: 540, EndMinute: 720, InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(2e-6)},
		{StartMinute: 840, EndMinute: 1080, InputPrice: testPtrFloat64(2e-6)},
	}
	require.NoError(t, ValidateTimeRanges(valid))

	require.Error(t, ValidateTimeRanges([]PricingTimeRange{
		{StartMinute: 540, EndMinute: 720}, // no price
	}))
	require.Error(t, ValidateTimeRanges([]PricingTimeRange{
		{StartMinute: 540, EndMinute: 540, InputPrice: testPtrFloat64(1e-6)}, // end <= start
	}))
	require.Error(t, ValidateTimeRanges([]PricingTimeRange{
		{StartMinute: 1440, EndMinute: 1440, InputPrice: testPtrFloat64(1e-6)}, // start out of range
	}))
	require.Error(t, ValidateTimeRanges([]PricingTimeRange{
		{StartMinute: 540, EndMinute: 720, InputPrice: testPtrFloat64(1e-6)},
		{StartMinute: 700, EndMinute: 900, InputPrice: testPtrFloat64(2e-6)}, // overlap
	}))
	require.Error(t, ValidateTimeRanges([]PricingTimeRange{
		{StartMinute: 540, EndMinute: 720, InputPrice: testPtrFloat64(-1e-6)}, // negative
	}))
}

func TestResolve_WithTimeRanges_TokenLayering(t *testing.T) {
	r := newResolverWithChannel(t, []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"claude-sonnet-4"},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(3e-6), // 谷价（默认）
		OutputPrice: testPtrFloat64(15e-6),
		TimeRanges: []PricingTimeRange{
			{StartMinute: 540, EndMinute: 720, InputPrice: testPtrFloat64(5e-6), OutputPrice: testPtrFloat64(25e-6)}, // 峰价 09:00–12:00
		},
	}})

	// 10:30 → 命中峰值时间段
	resolved := r.Resolve(context.Background(), PricingInput{
		Model:   "claude-sonnet-4",
		GroupID: groupIDPtr(),
		Now:     time.Date(2026, 8, 17, 10, 30, 0, 0, time.UTC),
	})
	require.NotNil(t, resolved.ActiveTimeRange)
	require.Equal(t, 540, resolved.ActiveTimeRange.StartMinute)

	pricing := r.GetIntervalPricing(resolved, 50000)
	require.InDelta(t, 5e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 25e-6, pricing.OutputPricePerToken, 1e-12)

	// 13:00 → 未命中时间段，回退默认价
	resolvedOff := r.Resolve(context.Background(), PricingInput{
		Model:   "claude-sonnet-4",
		GroupID: groupIDPtr(),
		Now:     time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC),
	})
	require.Nil(t, resolvedOff.ActiveTimeRange)
	pricingOff := r.GetIntervalPricing(resolvedOff, 50000)
	require.InDelta(t, 3e-6, pricingOff.InputPricePerToken, 1e-12)
	require.InDelta(t, 15e-6, pricingOff.OutputPricePerToken, 1e-12)
}

func TestGetIntervalPricing_TimeRangeThenInterval_Layering(t *testing.T) {
	// 默认价 + 峰值时间段 + 上下文区间三者同时存在：区间覆盖时间段的同名字段，未填字段回退。
	r := newResolverWithChannel(t, []ChannelModelPricing{{
		Platform:       "anthropic",
		Models:         []string{"claude-sonnet-4"},
		BillingMode:    BillingModeToken,
		InputPrice:     testPtrFloat64(3e-6),
		OutputPrice:    testPtrFloat64(15e-6),
		CacheReadPrice: testPtrFloat64(0.3e-6), // 默认缓存读
		TimeRanges: []PricingTimeRange{
			{StartMinute: 540, EndMinute: 720, InputPrice: testPtrFloat64(5e-6), OutputPrice: testPtrFloat64(25e-6)},
		},
		Intervals: []PricingInterval{
			{MinTokens: 0, MaxTokens: testPtrInt(128000), OutputPrice: testPtrFloat64(40e-6)}, // 区间只覆盖输出价
		},
	}})

	resolved := r.Resolve(context.Background(), PricingInput{
		Model:   "claude-sonnet-4",
		GroupID: groupIDPtr(),
		Now:     time.Date(2026, 8, 17, 10, 30, 0, 0, time.UTC),
	})

	pricing := r.GetIntervalPricing(resolved, 50000)
	// 输出价：区间(40e-6)覆盖时间段(25e-6)
	require.InDelta(t, 40e-6, pricing.OutputPricePerToken, 1e-12)
	// 输入价：区间未填 → 回退到时间段(5e-6)
	require.InDelta(t, 5e-6, pricing.InputPricePerToken, 1e-12)
	// 缓存读：区间/时间段都未填 → 回退到默认(0.3e-6)
	require.InDelta(t, 0.3e-6, pricing.CacheReadPricePerToken, 1e-12)
}

func TestResolve_WithTimeRanges_PerRequest(t *testing.T) {
	r := newResolverWithChannel(t, []ChannelModelPricing{{
		Platform:        "anthropic",
		Models:          []string{"claude-sonnet-4"},
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: testPtrFloat64(0.05),
		TimeRanges: []PricingTimeRange{
			{StartMinute: 540, EndMinute: 720, PerRequestPrice: testPtrFloat64(0.09)},
		},
	}})

	resolved := r.Resolve(context.Background(), PricingInput{
		Model:   "claude-sonnet-4",
		GroupID: groupIDPtr(),
		Now:     time.Date(2026, 8, 17, 10, 30, 0, 0, time.UTC),
	})
	require.NotNil(t, resolved.ActiveTimeRange)
	require.InDelta(t, 0.09, *resolved.ActiveTimeRange.PerRequestPrice, 1e-12)
}
