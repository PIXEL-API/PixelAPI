//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 覆盖渠道「限制模型」在 OpenAI 高级调度器 / 逐账号上游检查路径上的修复：
// 1. selectAccountWithScheduler 入口的 checkChannelPricingRestriction 预检查（requested/channel_mapped 基准）。
// 2. isOpenAIAccountChannelRestricted 逐账号上游检查（upstream 基准）。
// 3. defaultOpenAIAccountScheduler.filterOpenAIAccountsForLoadBalance 对上游受限账号的过滤。

func upstreamRestrictedChannel() Channel {
	return Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}},
		},
	}
}

func channelMappedRestrictedChannel() Channel {
	ch := upstreamRestrictedChannel()
	ch.BillingModelSource = BillingModelSourceChannelMapped
	return ch
}

func openAITestAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
	}
}

func TestSelectAccountWithScheduler_ChannelRestrictionPreCheck(t *testing.T) {
	t.Parallel()
	ch := channelMappedRestrictedChannel()
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}
	gid := int64(10)

	// gpt-5.6-sol 不在定价列表 → 预检查应在触碰其它依赖前就拦截。
	_, _, err := svc.selectAccountWithScheduler(context.Background(), &gid, "", "", "gpt-5.6-sol",
		nil, OpenAIUpstreamTransportHTTPSSE, "", "", false)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.True(t, strings.Contains(err.Error(), "channel pricing restriction"),
		"blocking error should carry the restriction marker, got: %v", err)
}

func TestIsOpenAIAccountChannelRestricted_Guards(t *testing.T) {
	t.Parallel()
	account := openAITestAccount(9001)

	// groupID == nil
	svc := &OpenAIGatewayService{channelService: newTestChannelService(makeStandardRepo(upstreamRestrictedChannel(), map[int64]string{10: PlatformOpenAI}))}
	require.False(t, svc.isOpenAIAccountChannelRestricted(context.Background(), nil, account, "gpt-5.6-sol", false))

	// channelService == nil
	emptySvc := &OpenAIGatewayService{}
	gid := int64(10)
	require.False(t, emptySvc.isOpenAIAccountChannelRestricted(context.Background(), &gid, account, "gpt-5.6-sol", false))
}

func TestIsOpenAIAccountChannelRestricted_UpstreamSource(t *testing.T) {
	t.Parallel()
	gid := int64(10)
	channelSvc := newTestChannelService(makeStandardRepo(upstreamRestrictedChannel(), map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}

	// upstream 基准 + restrict_models，上游模型 gpt-5.6-sol 不在定价列表 → 受限。
	require.True(t, svc.isOpenAIAccountChannelRestricted(context.Background(), &gid, openAITestAccount(9001), "gpt-5.6-sol", false))

	// 上游模型 gpt-5.4 在定价列表 → 不受限。
	require.False(t, svc.isOpenAIAccountChannelRestricted(context.Background(), &gid, openAITestAccount(9002), "gpt-5.4", false))
}

func TestIsOpenAIAccountChannelRestricted_ChannelMappedSourceNotChecked(t *testing.T) {
	t.Parallel()
	gid := int64(10)
	channelSvc := newTestChannelService(makeStandardRepo(channelMappedRestrictedChannel(), map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}

	// channel_mapped 基准由预检查统一拦截，逐账号检查必须返回 false。
	require.False(t, svc.isOpenAIAccountChannelRestricted(context.Background(), &gid, openAITestAccount(9001), "gpt-5.6-sol", false))
}

func TestFilterOpenAIAccountsForLoadBalance_UpstreamRestriction(t *testing.T) {
	t.Parallel()
	gid := int64(10)
	channelSvc := newTestChannelService(makeStandardRepo(upstreamRestrictedChannel(), map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}
	schedulerAny := newDefaultOpenAIAccountScheduler(svc, nil)
	scheduler, ok := schedulerAny.(*defaultOpenAIAccountScheduler)
	require.True(t, ok)

	accounts := []Account{
		*openAITestAccount(9001), // 无映射 → 上游 gpt-5.6-sol → 受限
		{
			ID:          9002,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 2,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.4"},
			},
		}, // 映射到 gpt-5.4（在定价列表）→ 允许
	}

	// gpt-5.6-sol 不在定价列表 → 9001（上游 gpt-5.6-sol）被过滤，9002（上游 gpt-5.4）保留。
	filtered, loadReq := scheduler.filterOpenAIAccountsForLoadBalance(context.Background(), accounts, OpenAIAccountScheduleRequest{
		GroupID:           &gid,
		RequestedModel:    "gpt-5.6-sol",
		RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
	}, nil)

	require.Len(t, filtered, 1)
	require.Equal(t, int64(9002), filtered[0].ID)
	require.Len(t, loadReq, 1)
	require.Equal(t, int64(9002), loadReq[0].ID)
}

func TestFilterOpenAIAccountsForLoadBalance_UpstreamRestrictionDisabled(t *testing.T) {
	t.Parallel()
	gid := int64(10)
	ch := upstreamRestrictedChannel()
	ch.RestrictModels = false
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}
	schedulerAny := newDefaultOpenAIAccountScheduler(svc, nil)
	scheduler, ok := schedulerAny.(*defaultOpenAIAccountScheduler)
	require.True(t, ok)

	accounts := []Account{*openAITestAccount(9001)}

	// RestrictModels=false → 不拦截。
	filtered, _ := scheduler.filterOpenAIAccountsForLoadBalance(context.Background(), accounts, OpenAIAccountScheduleRequest{
		GroupID:           &gid,
		RequestedModel:    "gpt-5.6-sol",
		RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
	}, nil)

	require.Len(t, filtered, 1)
	require.Equal(t, int64(9001), filtered[0].ID)
}
