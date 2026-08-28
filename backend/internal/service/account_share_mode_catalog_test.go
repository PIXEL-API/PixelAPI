//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveAccountShareRoomDefaultModels 覆盖房间默认模型从静态列表切换到定价目录的语义：
// 显式模型直通、目录缺失报服务不可用、目录空报 4xx、有目录时用目录全集。
func TestResolveAccountShareRoomDefaultModels(t *testing.T) {
	ctx := context.Background()

	// 显式模型：归一化去空白去重，不触碰目录。
	svc := &AccountShareModeService{}
	got, err := svc.resolveAccountShareRoomDefaultModels(ctx, PlatformOpenAI, []string{" gpt-5.5 ", "gpt-5.5", ""})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, got)

	// 目录未注入：视为服务不可用，绝不回退静态默认列表。
	_, err = svc.resolveAccountShareRoomDefaultModels(ctx, PlatformOpenAI, nil)
	require.ErrorIs(t, err, ErrServiceUnavailable)

	// 目录有模型：返回目录全集（已排序）。
	svc.pricedModelCatalog = &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"gpt-5.4", "gpt-5.5"}, nil
	}}
	got, err = svc.resolveAccountShareRoomDefaultModels(ctx, PlatformOpenAI, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.4", "gpt-5.5"}, got)

	// 目录空：报业务错误，阻止创建。
	svc.pricedModelCatalog = &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return nil, nil
	}}
	_, err = svc.resolveAccountShareRoomDefaultModels(ctx, PlatformOpencode, nil)
	require.ErrorIs(t, err, ErrAccountShareModeCatalogEmpty)
}

// TestApplyAccountSharePricedCatalog 覆盖房间账号能力交集与定价目录求交：
// 未注入目录或交集为 nil 保持原语义，具体交集逐个走 pattern-aware IsModelPriced 过滤。
func TestApplyAccountSharePricedCatalog(t *testing.T) {
	ctx := context.Background()

	// 目录未注入：保留原交集。
	svc := &AccountShareModeService{}
	require.Equal(t, []string{"a", "b"}, svc.applyAccountSharePricedCatalog(ctx, PlatformOpenAI, []string{"a", "b"}))

	// nil 交集（不限）：保持 nil，由前端动态目录补齐。
	require.Nil(t, svc.applyAccountSharePricedCatalog(ctx, PlatformOpenAI, nil))

	// 具体交集：过滤掉未定价模型。
	svc.pricedModelCatalog = &catalogStub{priced: func(_ context.Context, _ PricedModelQuery, modelID string) (bool, error) {
		return modelID == "gpt-5.5", nil
	}}
	got := svc.applyAccountSharePricedCatalog(ctx, PlatformOpenAI, []string{"gpt-5.5", "gpt-4"})
	require.Equal(t, []string{"gpt-5.5"}, got)

	// 目录读取失败：保留原交集，不因定价服务抖动破坏展示。
	svc.pricedModelCatalog = &catalogStub{priced: func(_ context.Context, _ PricedModelQuery, _ string) (bool, error) {
		return false, errors.New("cache unavailable")
	}}
	got = svc.applyAccountSharePricedCatalog(ctx, PlatformOpenAI, []string{"gpt-5.5"})
	require.Equal(t, []string{"gpt-5.5"}, got)
}

// TestAccountShareRoomModelIsPriced 覆盖 dispatch 前目录校验：
// 目录未注入/模型为空/读取失败放行，未定价拒绝。
func TestAccountShareRoomModelIsPriced(t *testing.T) {
	ctx := context.Background()

	require.True(t, accountShareRoomModelIsPriced(ctx, nil, PlatformOpenAI, "gpt-5.5"))
	require.True(t, accountShareRoomModelIsPriced(ctx, &catalogStub{}, PlatformOpenAI, "  "))

	stub := &catalogStub{priced: func(_ context.Context, _ PricedModelQuery, modelID string) (bool, error) {
		return modelID == "gpt-5.5", nil
	}}
	require.True(t, accountShareRoomModelIsPriced(ctx, stub, PlatformOpenAI, "gpt-5.5"))
	require.False(t, accountShareRoomModelIsPriced(ctx, stub, PlatformOpenAI, "gpt-4"))

	errStub := &catalogStub{priced: func(_ context.Context, _ PricedModelQuery, _ string) (bool, error) {
		return false, errors.New("cache unavailable")
	}}
	require.True(t, accountShareRoomModelIsPriced(ctx, errStub, PlatformOpenAI, "gpt-5.5"))
}

// roomModelInfoRepoStub 实现 accountShareRoomModelInfoRepository，供房间 supported_models 测试使用。
type roomModelInfoRepoStub struct {
	AccountShareModeRepository
	infos map[int64][]AccountShareRoomModelInfo
}

func (r *roomModelInfoRepoStub) ListRoomAccountModelInfos(_ context.Context, _ []int64) (map[int64][]AccountShareRoomModelInfo, error) {
	return r.infos, nil
}

// TestEnrichListingsSupportedModels_UnmappedAccountsUseCatalog 覆盖房间内账号均未配置显式映射时，
// supported_models 用平台定价目录全集，而不是保持「不限」让前端回退静态默认。
func TestEnrichListingsSupportedModels_UnmappedAccountsUseCatalog(t *testing.T) {
	ctx := context.Background()
	svc := &AccountShareModeService{
		repo: &roomModelInfoRepoStub{infos: map[int64][]AccountShareRoomModelInfo{
			1: {
				{AccountID: 10, Models: nil},
				{AccountID: 11, Models: nil},
			},
		}},
		pricedModelCatalog: &catalogStub{
			selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
				return []string{"gpt-5.4", "gpt-5.5"}, nil
			},
			priced: func(_ context.Context, _ PricedModelQuery, _ string) (bool, error) {
				return true, nil
			},
		},
	}

	listings := []AccountShareListing{{ID: 1, Platform: PlatformOpenAI}}
	svc.enrichListingsSupportedModels(ctx, listings, []int64{1})

	require.Equal(t, []string{"gpt-5.4", "gpt-5.5"}, listings[0].SupportedModels)
}

// TestEnrichListingsSupportedModels_NoAccountsKeepsNil 覆盖无账号房间保持 nil（不限），
// 不误用目录全集。
func TestEnrichListingsSupportedModels_NoAccountsKeepsNil(t *testing.T) {
	ctx := context.Background()
	svc := &AccountShareModeService{
		repo:             &roomModelInfoRepoStub{infos: map[int64][]AccountShareRoomModelInfo{}},
		pricedModelCatalog: &catalogStub{
			selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
				return []string{"gpt-5.4", "gpt-5.5"}, nil
			},
		},
	}

	listings := []AccountShareListing{{ID: 1, Platform: PlatformOpenAI}}
	svc.enrichListingsSupportedModels(ctx, listings, []int64{1})

	require.Nil(t, listings[0].SupportedModels)
}

// TestEnrichListingsSupportedModels_MappedIntersectionFilteredByCatalog 覆盖账号有显式映射时，
// 交集先收缩到共同支持集合，再与目录求交。
func TestEnrichListingsSupportedModels_MappedIntersectionFilteredByCatalog(t *testing.T) {
	ctx := context.Background()
	svc := &AccountShareModeService{
		repo: &roomModelInfoRepoStub{infos: map[int64][]AccountShareRoomModelInfo{
			1: {
				{AccountID: 10, Models: []string{"gpt-5.4", "gpt-5.5"}},
				{AccountID: 11, Models: []string{"gpt-5.5", "gpt-5.6"}},
			},
		}},
		pricedModelCatalog: &catalogStub{
			priced: func(_ context.Context, _ PricedModelQuery, modelID string) (bool, error) {
				return modelID == "gpt-5.5", nil
			},
		},
	}

	listings := []AccountShareListing{{ID: 1, Platform: PlatformOpenAI}}
	svc.enrichListingsSupportedModels(ctx, listings, []int64{1})

	require.Equal(t, []string{"gpt-5.5"}, listings[0].SupportedModels)
}

