//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func catalogInt64Ptr(v int64) *int64 { return &v }

// catalogOpenAIRepo 构造两个同平台 active 渠道（group 1/2）+ 一个禁用渠道，
// channel 2 含通配符定价 gpt-c*，用于验证列举/授权语义分离。
func catalogOpenAIRepo() *mockChannelRepository {
	return &mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) {
			return []Channel{
				{
					ID: 1, Status: StatusActive, GroupIDs: []int64{1},
					ModelPricing: []ChannelModelPricing{
						{Platform: PlatformOpenAI, Models: []string{"gpt-a", "gpt-b"}},
					},
				},
				{
					ID: 2, Status: StatusActive, GroupIDs: []int64{2},
					ModelPricing: []ChannelModelPricing{
						{Platform: PlatformOpenAI, Models: []string{"gpt-b", "gpt-c*"}},
					},
				},
				{
					ID: 3, Status: StatusDisabled, GroupIDs: []int64{3},
					ModelPricing: []ChannelModelPricing{
						{Platform: PlatformOpenAI, Models: []string{"gpt-disabled"}},
					},
				},
			}, nil
		},
		getGroupPlatformsFn: func(_ context.Context, _ []int64) (map[int64]string, error) {
			return map[int64]string{1: PlatformOpenAI, 2: PlatformOpenAI, 3: PlatformOpenAI}, nil
		},
	}
}

func TestListSelectablePricedModelIDs_PlatformScope(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	models, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{Platform: PlatformOpenAI})

	require.NoError(t, err)
	// gpt-b 去重、gpt-c* 通配符不列举、disabled 排除
	require.Equal(t, []string{"gpt-a", "gpt-b"}, models)
}

func TestListSelectablePricedModelIDs_GroupScope(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	models, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform: PlatformOpenAI,
		GroupID:  catalogInt64Ptr(1),
	})

	require.NoError(t, err)
	require.Equal(t, []string{"gpt-a", "gpt-b"}, models)
}

func TestListSelectablePricedModelIDs_ChannelScope(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	models, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform:  PlatformOpenAI,
		ChannelID: catalogInt64Ptr(2),
	})

	require.NoError(t, err)
	require.Equal(t, []string{"gpt-b"}, models)
}

func TestListSelectablePricedModelIDs_GroupUnboundReturnsEmpty(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	models, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform: PlatformOpenAI,
		GroupID:  catalogInt64Ptr(99),
	})

	require.NoError(t, err)
	require.Empty(t, models)
}

func TestListSelectablePricedModelIDs_MissingPlatform(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	_, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{})

	require.ErrorIs(t, err, ErrPricedModelScopeInvalid)
}

func TestListSelectablePricedModelIDs_GroupChannelMismatch(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	_, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform:  PlatformOpenAI,
		GroupID:   catalogInt64Ptr(1),
		ChannelID: catalogInt64Ptr(2),
	})

	require.ErrorIs(t, err, ErrPricedModelScopeMismatch)
}

func TestListSelectablePricedModelIDs_GroupPlatformMismatch(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	_, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform: PlatformAnthropic,
		GroupID:  catalogInt64Ptr(1),
	})

	require.ErrorIs(t, err, ErrPricedModelScopeMismatch)
}

func TestIsModelPriced_ExactAndWildcard(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())
	query := PricedModelQuery{Platform: PlatformOpenAI}

	// gpt-a：channel 1 精确定价
	priced, err := svc.IsModelPriced(context.Background(), query, "gpt-a")
	require.NoError(t, err)
	require.True(t, priced)

	// gpt-c-4o：channel 2 通配符 gpt-c* 前缀匹配
	priced, err = svc.IsModelPriced(context.Background(), query, "gpt-c-4o")
	require.NoError(t, err)
	require.True(t, priced)

	// gpt-unknown：无任何定价
	priced, err = svc.IsModelPriced(context.Background(), query, "gpt-unknown")
	require.NoError(t, err)
	require.False(t, priced)
}

func TestIsModelPriced_GroupScoped(t *testing.T) {
	svc := newTestChannelService(catalogOpenAIRepo())

	// group 1 只绑定 channel 1，通配符 gpt-c* 属于 channel 2，不应命中
	priced, err := svc.IsModelPriced(context.Background(), PricedModelQuery{
		Platform: PlatformOpenAI,
		GroupID:  catalogInt64Ptr(1),
	}, "gpt-c-4o")
	require.NoError(t, err)
	require.False(t, priced)

	// group 2 绑定 channel 2，通配符应命中
	priced, err = svc.IsModelPriced(context.Background(), PricedModelQuery{
		Platform: PlatformOpenAI,
		GroupID:  catalogInt64Ptr(2),
	}, "gpt-c-4o")
	require.NoError(t, err)
	require.True(t, priced)
}

func TestAccountStatsRuleInScope(t *testing.T) {
	rule := &AccountStatsPricingRule{
		GroupIDs:   []int64{1},
		AccountIDs: []int64{10},
	}

	// 无范围上下文 → 全部并集兼容语义
	require.True(t, accountStatsRuleInScope(rule, PricedModelQuery{}))

	// group 命中
	require.True(t, accountStatsRuleInScope(rule, PricedModelQuery{GroupID: catalogInt64Ptr(1)}))
	// group 未命中
	require.False(t, accountStatsRuleInScope(rule, PricedModelQuery{GroupID: catalogInt64Ptr(2)}))

	// 任一 account 命中
	require.True(t, accountStatsRuleInScope(rule, PricedModelQuery{AccountIDs: []int64{10}}))
	require.True(t, accountStatsRuleInScope(rule, PricedModelQuery{AccountIDs: []int64{99, 10}}))
	require.False(t, accountStatsRuleInScope(rule, PricedModelQuery{AccountIDs: []int64{99}}))
}

func TestListSelectablePricedModelIDs_AccountStatsRules(t *testing.T) {
	repo := &mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) {
			return []Channel{
				{
					ID: 1, Status: StatusActive, GroupIDs: []int64{1},
					ModelPricing: []ChannelModelPricing{
						{Platform: PlatformOpenAI, Models: []string{"gpt-main"}},
					},
					AccountStatsPricingRules: []AccountStatsPricingRule{
						{GroupIDs: []int64{1}, Pricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-stat-group"}}}},
						{AccountIDs: []int64{10}, Pricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-stat-account"}}}},
					},
				},
			}, nil
		},
		getGroupPlatformsFn: func(_ context.Context, _ []int64) (map[int64]string, error) {
			return map[int64]string{1: PlatformOpenAI}, nil
		},
	}
	svc := newTestChannelService(repo)

	// 无范围 → 主定价 + 全部统计规则并集
	models, err := svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{Platform: PlatformOpenAI})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-main", "gpt-stat-account", "gpt-stat-group"}, models)

	// group=1 → 主定价 + group 命中的统计规则（account 规则不命中）
	models, err = svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform: PlatformOpenAI,
		GroupID:  catalogInt64Ptr(1),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-main", "gpt-stat-group"}, models)

	// account=10 → 主定价 + account 命中的统计规则（group 规则不命中）
	models, err = svc.ListSelectablePricedModelIDs(context.Background(), PricedModelQuery{
		Platform:   PlatformOpenAI,
		AccountIDs: []int64{10},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-main", "gpt-stat-account"}, models)
}
