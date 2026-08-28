package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 定价目录范围查询错误。
var (
	// ErrPricedModelScopeInvalid 目录范围查询缺少平台或平台名非法。
	ErrPricedModelScopeInvalid = infraerrors.BadRequest("ACCOUNT_MODEL_SCOPE_INVALID", "scoped priced model query requires a valid platform")
	// ErrPricedModelScopeMismatch 目录范围查询的 group/channel/platform 相互不一致。
	ErrPricedModelScopeMismatch = infraerrors.BadRequest("ACCOUNT_MODEL_SCOPE_MISMATCH", "channel does not match the requested group or platform scope")
)

// PricedModelQuery 定价目录的范围查询对象。
//
// Platform 为业务范围查询的必需字段；GroupID / ChannelID 用于把平台全局并集
// 进一步收窄到具体分组或渠道；AccountIDs 仅用于判定账号统计定价规则是否适用，
// 调用方必须先做去重与权限校验，不得用它绕过账号读取权限。
type PricedModelQuery struct {
	Platform   string
	GroupID    *int64
	ChannelID  *int64
	AccountIDs []int64
}

// PricedModelCatalog 定价目录窄接口，供账号测试 resolver 等业务方依赖，
// 避免业务层直接持有整个 ChannelService。
type PricedModelCatalog interface {
	ListPricedModelIDs(ctx context.Context, platforms []string) ([]string, error)
	ListSelectablePricedModelIDs(ctx context.Context, query PricedModelQuery) ([]string, error)
	IsModelPriced(ctx context.Context, query PricedModelQuery, modelID string) (bool, error)
}

// normalizeCatalogPlatform 规范化平台名（去首尾空白、大小写不敏感）。
func normalizeCatalogPlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

// isWildcardModel 判断定价条目里的模型名是否为通配符（claude-* 之类）。
// 通配符不能作为 UI 选择器的具体选项，只能参与对具体模型 ID 的授权匹配。
func isWildcardModel(model string) bool {
	return strings.HasSuffix(model, "*")
}

// accountStatsRuleInScope 判断账号统计定价规则是否落在查询范围内。
//
// 无 group/account 上下文时保持 ListPricedModelIDs 的「全部规则并集」兼容语义；
// 存在范围上下文时按 OR 合同（accountID ∈ AccountIDs 或 groupID ∈ GroupIDs）匹配。
func accountStatsRuleInScope(rule *AccountStatsPricingRule, query PricedModelQuery) bool {
	if rule == nil {
		return false
	}
	if query.GroupID == nil && len(query.AccountIDs) == 0 {
		return true
	}
	if query.GroupID != nil && containsInt64(rule.GroupIDs, *query.GroupID) {
		return true
	}
	for _, accountID := range query.AccountIDs {
		if containsInt64(rule.AccountIDs, accountID) {
			return true
		}
	}
	return false
}

// pricingEntryMatchesModel 判断单个定价条目是否匹配具体模型（精确或通配符前缀）。
func pricingEntryMatchesModel(entry ChannelModelPricing, platform, modelLower string) bool {
	if normalizeCatalogPlatform(entry.Platform) != platform {
		return false
	}
	for _, model := range entry.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if isWildcardModel(model) {
			prefix := strings.ToLower(strings.TrimSuffix(model, "*"))
			if strings.HasPrefix(modelLower, prefix) {
				return true
			}
		} else if strings.ToLower(model) == modelLower {
			return true
		}
	}
	return false
}

// collectScopedChannels 根据查询范围确定候选渠道集合。
//
// 返回 nil 表示范围内没有可用的 active 渠道（空目录），不静默回退到平台全局；
// 返回 error 表示 group/channel/platform 范围自相矛盾。
func collectScopedChannels(cache *channelCache, query PricedModelQuery) ([]*Channel, error) {
	platform := normalizeCatalogPlatform(query.Platform)

	if query.ChannelID != nil {
		ch, ok := cache.byID[*query.ChannelID]
		if !ok || !ch.IsActive() {
			return nil, nil
		}
		if query.GroupID != nil {
			if !containsInt64(ch.GroupIDs, *query.GroupID) {
				return nil, ErrPricedModelScopeMismatch
			}
			if gp := cache.groupPlatform[*query.GroupID]; gp != "" && normalizeCatalogPlatform(gp) != platform {
				return nil, ErrPricedModelScopeMismatch
			}
		}
		return []*Channel{ch}, nil
	}

	if query.GroupID != nil {
		ch, ok := cache.channelByGroupID[*query.GroupID]
		if !ok || !ch.IsActive() {
			return nil, nil
		}
		if gp := cache.groupPlatform[*query.GroupID]; gp != "" && normalizeCatalogPlatform(gp) != platform {
			return nil, ErrPricedModelScopeMismatch
		}
		return []*Channel{ch}, nil
	}

	channels := make([]*Channel, 0)
	for _, ch := range cache.byID {
		if ch.IsActive() {
			channels = append(channels, ch)
		}
	}
	return channels, nil
}

// mergeChannelPricedModels 合并渠道主定价与账号统计规则定价，
// 返回可安全展示的精确模型 ID 集合（不含通配符）。
func mergeChannelPricedModels(ch *Channel, platform string, query PricedModelQuery) map[string]struct{} {
	models := make(map[string]struct{})
	if ch == nil {
		return models
	}

	addPricing := func(pricing []ChannelModelPricing) {
		for _, entry := range pricing {
			if normalizeCatalogPlatform(entry.Platform) != platform {
				continue
			}
			for _, model := range entry.Models {
				if isWildcardModel(model) {
					continue
				}
				if m := strings.TrimSpace(model); m != "" {
					models[m] = struct{}{}
				}
			}
		}
	}

	addPricing(ch.ModelPricing)
	for i := range ch.AccountStatsPricingRules {
		rule := &ch.AccountStatsPricingRules[i]
		if accountStatsRuleInScope(rule, query) {
			addPricing(rule.Pricing)
		}
	}
	return models
}

// ListSelectablePricedModelIDs 返回可按范围展示的有限、具体模型 ID（不含通配符）。
//
// 与 ListPricedModelIDs 的差异：支持 group/channel 收窄，并复用 channelCache
// 而非每次全表扫描；列举语义只返回精确模型 ID，通配符定价通过 IsModelPriced 授权。
func (s *ChannelService) ListSelectablePricedModelIDs(ctx context.Context, query PricedModelQuery) ([]string, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("channel service is not configured")
	}
	if normalizeCatalogPlatform(query.Platform) == "" {
		return nil, ErrPricedModelScopeInvalid
	}

	cache, err := s.loadCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("load channel cache for scoped priced models: %w", err)
	}

	channels, err := collectScopedChannels(cache, query)
	if err != nil {
		return nil, err
	}

	models := make(map[string]struct{})
	platform := normalizeCatalogPlatform(query.Platform)
	for _, ch := range channels {
		for model := range mergeChannelPricedModels(ch, platform, query) {
			models[model] = struct{}{}
		}
	}

	result := make([]string, 0, len(models))
	for model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result, nil
}

// IsModelPriced 判断具体模型 ID 在查询范围内是否有定价（精确或通配符前缀）。
func (s *ChannelService) IsModelPriced(ctx context.Context, query PricedModelQuery, modelID string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("channel service is not configured")
	}
	if normalizeCatalogPlatform(query.Platform) == "" {
		return false, ErrPricedModelScopeInvalid
	}

	model := strings.TrimSpace(modelID)
	if model == "" {
		return false, nil
	}

	cache, err := s.loadCache(ctx)
	if err != nil {
		return false, fmt.Errorf("load channel cache for priced model check: %w", err)
	}

	channels, err := collectScopedChannels(cache, query)
	if err != nil {
		return false, err
	}

	platform := normalizeCatalogPlatform(query.Platform)
	modelLower := strings.ToLower(model)
	for _, ch := range channels {
		if channelHasModelPriced(ch, platform, modelLower, query) {
			return true, nil
		}
	}
	return false, nil
}

// channelHasModelPriced 判断渠道（主定价 + 账号统计规则）是否对具体模型有定价。
func channelHasModelPriced(ch *Channel, platform, modelLower string, query PricedModelQuery) bool {
	if ch == nil {
		return false
	}
	for i := range ch.ModelPricing {
		if pricingEntryMatchesModel(ch.ModelPricing[i], platform, modelLower) {
			return true
		}
	}
	for i := range ch.AccountStatsPricingRules {
		rule := &ch.AccountStatsPricingRules[i]
		if !accountStatsRuleInScope(rule, query) {
			continue
		}
		for j := range rule.Pricing {
			if pricingEntryMatchesModel(rule.Pricing[j], platform, modelLower) {
				return true
			}
		}
	}
	return false
}
