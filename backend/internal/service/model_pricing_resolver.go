package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// PricingSource 定价来源标识
const (
	PricingSourceChannel  = "channel"
	PricingSourceLiteLLM  = "litellm"
	PricingSourceFallback = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自 LiteLLM 或 fallback）
	BasePricing *ModelPricing

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 时间段定价列表（按一天内分钟区间覆盖基础价，与 Intervals 正交）
	TimeRanges []PricingTimeRange
	// Resolve 时按当前时刻解析出的命中时间段（nil 表示未命中或无时间段）
	ActiveTimeRange *PricingTimeRange

	// Token 模式：渠道级长上下文策略。nil 与 false 均表示关闭。
	LongContextPricingEnabled *bool
	// 仅在 LongContextPricingEnabled=true 时覆盖模型价卡阈值。
	LongContextInputTokenThreshold *int

	// 按次/图片模式：分层定价
	RequestTiers []PricingInterval

	// 按次/图片模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice float64

	// 来源标识
	Source string // "channel", "litellm", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Channel → LiteLLM → Fallback。
type ModelPricingResolver struct {
	channelService *ChannelService
	billingService *BillingService
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
	return &ModelPricingResolver{
		channelService: channelService,
		billingService: billingService,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Model   string
	GroupID *int64    // nil 表示不检查渠道
	Now     time.Time // 计费时刻（零值 = timezone.Now()），用于时间段定价命中
}

// pricingNowMinute 将计费时刻折算为一天内分钟数（本系统配置时区）。
func pricingNowMinute(now time.Time) int {
	if now.IsZero() {
		now = timezone.Now()
	}
	return now.Hour()*60 + now.Minute()
}

// Resolve 解析模型定价。
// 1. 获取基础定价（LiteLLM → Fallback）
// 2. 如果指定了 GroupID，查找渠道定价并覆盖
// 3. 解析当前时刻命中的时间段（ActiveTimeRange），供计费层叠加
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	nowMinute := pricingNowMinute(input.Now)

	var chPricing *ChannelModelPricing
	if input.GroupID != nil && r.channelService != nil {
		chPricing = r.channelService.GetChannelModelPricing(ctx, *input.GroupID, input.Model)
		if chPricing != nil {
			mode := chPricing.BillingMode
			if mode == "" {
				mode = BillingModeToken
			}
			if mode == BillingModePerRequest || mode == BillingModeImage {
				resolved := &ResolvedPricing{
					Mode:   mode,
					Source: PricingSourceChannel,
				}
				r.applyRequestTierOverrides(chPricing, resolved)
				resolved.ActiveTimeRange = FindActiveTimeRange(resolved.TimeRanges, nowMinute)
				return resolved
			}
		}
	}

	// 1. 获取基础定价
	basePricing, source := r.resolveBasePricing(input.Model)

	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}

	// 2. 如果有 GroupID，尝试渠道覆盖
	if chPricing != nil {
		resolved.Source = PricingSourceChannel
		r.applyTokenOverrides(chPricing, resolved)
	} else if input.GroupID != nil && r.channelService != nil {
		r.applyChannelOverrides(ctx, *input.GroupID, input.Model, resolved)
	}
	resolved.ActiveTimeRange = FindActiveTimeRange(resolved.TimeRanges, nowMinute)

	return resolved
}

// HasConfiguredPricing reports whether a model has an explicit global or
// channel price. Pointer-valued channel fields distinguish an intentional zero
// price from a missing price, which is required by fail-closed billing paths.
func (r *ModelPricingResolver) HasConfiguredPricing(ctx context.Context, input PricingInput) bool {
	if r == nil || r.billingService == nil || strings.TrimSpace(input.Model) == "" {
		return false
	}
	if _, err := r.billingService.GetModelPricing(input.Model); err == nil {
		return true
	}
	if input.GroupID == nil || *input.GroupID <= 0 || r.channelService == nil {
		return false
	}
	pricing := r.channelService.GetChannelModelPricing(ctx, *input.GroupID, input.Model)
	if pricing == nil {
		return false
	}
	if pricing.BillingMode == BillingModePerRequest || pricing.BillingMode == BillingModeImage {
		if pricing.PerRequestPrice != nil {
			return true
		}
		for _, interval := range pricing.Intervals {
			if interval.PerRequestPrice != nil {
				return true
			}
		}
		for _, tr := range pricing.TimeRanges {
			if tr.PerRequestPrice != nil {
				return true
			}
		}
		return false
	}
	if pricing.InputPrice != nil ||
		pricing.OutputPrice != nil ||
		pricing.CacheWritePrice != nil ||
		pricing.CacheReadPrice != nil ||
		pricing.ImageInputPrice != nil ||
		pricing.ImageCacheReadPrice != nil ||
		pricing.ImageOutputPrice != nil {
		return true
	}
	for _, interval := range pricing.Intervals {
		if interval.InputPrice != nil ||
			interval.OutputPrice != nil ||
			interval.CacheWritePrice != nil ||
			interval.CacheReadPrice != nil {
			return true
		}
	}
	for _, tr := range pricing.TimeRanges {
		if tr.InputPrice != nil ||
			tr.OutputPrice != nil ||
			tr.CacheWritePrice != nil ||
			tr.CacheReadPrice != nil ||
			tr.ImageInputPrice != nil ||
			tr.ImageCacheReadPrice != nil ||
			tr.ImageOutputPrice != nil {
			return true
		}
	}
	return false
}

// resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricing, string) {
	pricing, err := r.billingService.GetModelPricing(model)
	if err != nil {
		slog.Debug("failed to get model pricing from LiteLLM, using fallback",
			"model", model, "error", err)
		return nil, PricingSourceFallback
	}
	return pricing, PricingSourceLiteLLM
}

// applyChannelOverrides 应用渠道定价覆盖
func (r *ModelPricingResolver) applyChannelOverrides(ctx context.Context, groupID int64, model string, resolved *ResolvedPricing) {
	chPricing := r.channelService.GetChannelModelPricing(ctx, groupID, model)
	if chPricing == nil {
		return
	}

	resolved.Source = PricingSourceChannel
	resolved.Mode = chPricing.BillingMode
	if resolved.Mode == "" {
		resolved.Mode = BillingModeToken
	}

	switch resolved.Mode {
	case BillingModeToken:
		r.applyTokenOverrides(chPricing, resolved)
	case BillingModePerRequest, BillingModeImage:
		r.applyRequestTierOverrides(chPricing, resolved)
	}
}

// applyTokenOverrides 应用 token 模式的渠道覆盖
func (r *ModelPricingResolver) applyTokenOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.LongContextPricingEnabled = chPricing.LongContextPricingEnabled
	resolved.LongContextInputTokenThreshold = chPricing.LongContextInputTokenThreshold
	resolved.TimeRanges = filterValidTimeRanges(chPricing.TimeRanges)

	// 过滤掉所有价格字段都为空的无效 interval
	validIntervals := filterValidIntervals(chPricing.Intervals)

	// 如果有有效的区间定价，使用区间
	if len(validIntervals) > 0 {
		resolved.Intervals = validIntervals
		return
	}

	// 否则用 flat 字段覆盖 BasePricing
	if resolved.BasePricing == nil {
		resolved.BasePricing = &ModelPricing{}
	} else {
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}

	if chPricing.InputPrice != nil {
		resolved.BasePricing.InputPricePerToken = *chPricing.InputPrice
		resolved.BasePricing.InputPricePerTokenPriority = 0
	}
	if chPricing.OutputPrice != nil {
		resolved.BasePricing.OutputPricePerToken = *chPricing.OutputPrice
		resolved.BasePricing.OutputPricePerTokenPriority = 0
	}
	if chPricing.CacheWritePrice != nil {
		resolved.BasePricing.CacheCreationPricePerToken = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreationPricePerTokenPriority = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreationPriceExplicit = true
		resolved.BasePricing.CacheCreation5mPrice = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreation1hPrice = *chPricing.CacheWritePrice
	}
	if chPricing.CacheReadPrice != nil {
		resolved.BasePricing.CacheReadPricePerToken = *chPricing.CacheReadPrice
		resolved.BasePricing.CacheReadPricePerTokenPriority = 0
	}
	if chPricing.ImageInputPrice != nil {
		resolved.BasePricing.ImageInputPricePerToken = *chPricing.ImageInputPrice
	}
	if chPricing.ImageCacheReadPrice != nil {
		resolved.BasePricing.ImageCacheReadPricePerToken = *chPricing.ImageCacheReadPrice
	}
	if chPricing.ImageOutputPrice != nil {
		resolved.BasePricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
	}
}

// applyRequestTierOverrides 应用按次/图片模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidIntervals(chPricing.Intervals)
	resolved.TimeRanges = filterValidTimeRanges(chPricing.TimeRanges)
	if chPricing.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *chPricing.PerRequestPrice
	}
}

// filterValidIntervals 过滤掉所有价格字段都为空的无效 interval。
// 前端可能创建了只有 min/max 但无价格的空 interval。
func filterValidIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// filterValidTimeRanges 过滤掉所有价格字段都为空的无效时间段。
func filterValidTimeRanges(ranges []PricingTimeRange) []PricingTimeRange {
	var valid []PricingTimeRange
	for _, tr := range ranges {
		if tr.InputPrice != nil || tr.OutputPrice != nil ||
			tr.CacheWritePrice != nil || tr.CacheReadPrice != nil ||
			tr.ImageInputPrice != nil || tr.ImageCacheReadPrice != nil ||
			tr.ImageOutputPrice != nil || tr.PerRequestPrice != nil {
			valid = append(valid, tr)
		}
	}
	return valid
}

// GetIntervalPricing 合成 token 模式基础价。
// 合成顺序：默认价(BasePricing) → 命中时间段(ActiveTimeRange) → 命中上下文区间(Intervals)。
// 逐字段覆盖，未填字段逐级回退；上下文区间优先级最高。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	pricing := cloneModelPricingBase(resolved.BasePricing, resolved.SupportsCacheBreakdown)

	if tr := resolved.ActiveTimeRange; tr != nil {
		applyTimeRangeToModelPricing(pricing, tr)
	}

	if len(resolved.Intervals) > 0 {
		if iv := FindMatchingInterval(resolved.Intervals, totalContextTokens); iv != nil {
			applyIntervalToModelPricing(pricing, iv)
		}
	}

	return pricing
}

// cloneModelPricingBase 拷贝基础价，避免叠加时间段/区间时污染共享的 LiteLLM/fallback 指针。
func cloneModelPricingBase(base *ModelPricing, supportsCacheBreakdown bool) *ModelPricing {
	if base == nil {
		return &ModelPricing{SupportsCacheBreakdown: supportsCacheBreakdown}
	}
	cp := *base
	cp.SupportsCacheBreakdown = supportsCacheBreakdown
	return &cp
}

// applyTimeRangeToModelPricing 将时间段的非空价格字段逐字段叠加到 pricing 上。
func applyTimeRangeToModelPricing(pricing *ModelPricing, tr *PricingTimeRange) {
	if tr.InputPrice != nil {
		pricing.InputPricePerToken = *tr.InputPrice
		pricing.InputPricePerTokenPriority = 0
	}
	if tr.OutputPrice != nil {
		pricing.OutputPricePerToken = *tr.OutputPrice
		pricing.OutputPricePerTokenPriority = 0
	}
	if tr.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *tr.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *tr.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *tr.CacheWritePrice
		pricing.CacheCreation1hPrice = *tr.CacheWritePrice
	}
	if tr.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *tr.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = 0
	}
	if tr.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *tr.ImageInputPrice
	}
	if tr.ImageCacheReadPrice != nil {
		pricing.ImageCacheReadPricePerToken = *tr.ImageCacheReadPrice
	}
	if tr.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *tr.ImageOutputPrice
	}
}

// applyIntervalToModelPricing 将上下文区间的非空价格字段逐字段叠加到 pricing 上（覆盖时间段/默认价的同名字段）。
func applyIntervalToModelPricing(pricing *ModelPricing, iv *PricingInterval) {
	if iv.InputPrice != nil {
		pricing.InputPricePerToken = *iv.InputPrice
		pricing.InputPricePerTokenPriority = 0
	}
	if iv.OutputPrice != nil {
		pricing.OutputPricePerToken = *iv.OutputPrice
		pricing.OutputPricePerTokenPriority = 0
	}
	if iv.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *iv.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		pricing.CacheCreation1hPrice = *iv.CacheWritePrice
	}
	if iv.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = 0
	}
}

// LookupRequestTierPrice 根据层级标签获取按次价格，并区分显式免费与未命中。
func (r *ModelPricingResolver) LookupRequestTierPrice(resolved *ResolvedPricing, tierLabel string) (float64, bool) {
	for _, tier := range resolved.RequestTiers {
		if tier.TierLabel == tierLabel && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}

// GetRequestTierPrice 保留纯价格读取语义；需要执行价格回退时应使用 LookupRequestTierPrice。
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) float64 {
	price, _ := r.LookupRequestTierPrice(resolved, tierLabel)
	return price
}

// LookupRequestTierPriceByContext 根据 context token 数获取按次价格，并区分显式免费与未命中。
func (r *ModelPricingResolver) LookupRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) (float64, bool) {
	iv := FindMatchingInterval(resolved.RequestTiers, totalContextTokens)
	if iv != nil && iv.PerRequestPrice != nil {
		return *iv.PerRequestPrice, true
	}
	return 0, false
}

// GetRequestTierPriceByContext 保留纯价格读取语义；需要执行价格回退时应使用 LookupRequestTierPriceByContext。
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) float64 {
	price, _ := r.LookupRequestTierPriceByContext(resolved, totalContextTokens)
	return price
}
