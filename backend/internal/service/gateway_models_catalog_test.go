//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

// TestGatewayModelIsPriced 覆盖网关目录硬上限校验的放行分支：
// 目录未注入或模型为空时不阻断（由账号白名单兜底）。
func TestGatewayModelIsPriced(t *testing.T) {
	ctx := context.Background()

	require.True(t, (&GatewayService{}).gatewayModelIsPriced(ctx, PlatformOpenAI, "gpt-a"))
	require.True(t, (&GatewayService{}).gatewayModelIsPriced(ctx, PlatformOpenAI, "  "))
}

// TestGetAvailableModelsIntersectsPricedCatalog 覆盖 /v1/models 账号并集与定价目录求交：
// 账号支持的未定价模型不出现在结果里。
func TestGetAvailableModelsIntersectsPricedCatalog(t *testing.T) {
	resetGatewayHotpathStatsForTest()

	groupID := int64(9)
	repo := &modelsListAccountRepoStub{
		byGroup: map[int64][]Account{
			groupID: {
				{
					ID:       1,
					Platform: PlatformOpenAI,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"gpt-a": "gpt-a",
							"gpt-x": "gpt-x",
						},
					},
				},
			},
		},
	}
	channelService := newTestChannelService(&mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) {
		return []Channel{{
			Status:       StatusActive,
			ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-a"}}},
		}}, nil
	}})
	svc := &GatewayService{
		accountRepo:        repo,
		channelService:     channelService,
		modelsListCache:    gocache.New(time.Minute, time.Minute),
		modelsListCacheTTL: time.Minute,
	}

	models := svc.GetAvailableModels(context.Background(), &groupID, "")
	require.Equal(t, []string{"gpt-a"}, models)
}

// TestGetAvailableModelsWithoutCatalogKeepsAllModels 覆盖目录未注入时的兼容路径：
// 保持原有账号并集行为不变。
func TestGetAvailableModelsWithoutCatalogKeepsAllModels(t *testing.T) {
	groupID := int64(9)
	repo := &modelsListAccountRepoStub{
		byGroup: map[int64][]Account{
			groupID: {
				{
					ID:       1,
					Platform: PlatformOpenAI,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"gpt-a": "gpt-a",
							"gpt-x": "gpt-x",
						},
					},
				},
			},
		},
	}
	svc := &GatewayService{
		accountRepo:        repo,
		modelsListCache:    gocache.New(time.Minute, time.Minute),
		modelsListCacheTTL: time.Minute,
	}

	models := svc.GetAvailableModels(context.Background(), &groupID, "")
	require.Equal(t, []string{"gpt-a", "gpt-x"}, models)
}
