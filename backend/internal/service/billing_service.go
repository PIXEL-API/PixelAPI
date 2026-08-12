package service

import (
	"context"
	"errors"
	"fmt"

	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// APIKeyRateLimitCacheData holds rate limit usage data cached in Redis.
type APIKeyRateLimitCacheData struct {
	Usage5h  float64 `json:"usage_5h"`
	Usage1d  float64 `json:"usage_1d"`
	Usage7d  float64 `json:"usage_7d"`
	Window5h int64   `json:"window_5h"` // unix timestamp, 0 = not started
	Window1d int64   `json:"window_1d"`
	Window7d int64   `json:"window_7d"`
}

// BillingCache defines cache operations for billing service
type BillingCache interface {
	// Balance operations
	GetUserBalance(ctx context.Context, userID int64) (float64, error)
	SetUserBalance(ctx context.Context, userID int64, balance float64) error
	DeductUserBalance(ctx context.Context, userID int64, amount float64) error
	InvalidateUserBalance(ctx context.Context, userID int64) error

	// Subscription operations
	GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error)
	SetSubscriptionCache(ctx context.Context, userID, groupID int64, data *SubscriptionCacheData) error
	UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error
	InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error

	// API Key rate limit operations
	GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*APIKeyRateLimitCacheData, error)
	SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *APIKeyRateLimitCacheData) error
	UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error
	InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error
}

// ModelPricing 模型价格配置（per-token价格，与LiteLLM格式一致）
type ModelPricing struct {
	InputPricePerToken                 float64 // 每token输入价格 (USD)
	InputPricePerTokenPriority         float64 // priority service tier 下每token输入价格 (USD)
	OutputPricePerToken                float64 // 每token输出价格 (USD)
	OutputPricePerTokenPriority        float64 // priority service tier 下每token输出价格 (USD)
	CacheCreationPricePerToken         float64 // 缓存创建每token价格 (USD)
	CacheCreationPricePerTokenPriority float64 // priority service tier 下缓存创建每token价格 (USD)
	CacheCreationPriceExplicit         bool    // 是否由渠道/区间定价显式设定（为 true 时即使 == 0 也不回退）
	CacheReadPricePerToken             float64 // 缓存读取每token价格 (USD)
	CacheReadPricePerTokenPriority     float64 // priority service tier 下缓存读取每token价格 (USD)
	CacheCreation5mPrice               float64 // 5分钟缓存创建每token价格 (USD)
	CacheCreation1hPrice               float64 // 1小时缓存创建每token价格 (USD)
	SupportsCacheBreakdown             bool    // 是否支持详细的缓存分类
	LongContextInputThreshold          int     // 超过阈值后按整次会话提升输入价格
	LongContextInputMultiplier         float64 // 长上下文整次会话输入倍率
	LongContextOutputMultiplier        float64 // 长上下文整次会话输出倍率
	ImageInputPricePerToken            float64 // 图片输入 token 价格 (USD)
	ImageCacheReadPricePerToken        float64 // 图片缓存读取 token 价格 (USD)
	ImageOutputPricePerToken           float64 // 图片输出 token 价格 (USD)
}

const (
	openAIGPT54LongContextInputThreshold   = 272000
	openAIGPT54LongContextInputMultiplier  = 2.0
	openAIGPT54LongContextOutputMultiplier = 1.5
	openAIGPT54FastMultiplier              = 2.0
	openAIGPT55FastMultiplier              = 2.5
)

func normalizeBillingServiceTier(serviceTier string) string {
	return strings.ToLower(strings.TrimSpace(serviceTier))
}

func usePriorityServiceTierPricing(serviceTier string, pricing *ModelPricing) bool {
	if pricing == nil || normalizeBillingServiceTier(serviceTier) != "priority" {
		return false
	}
	return pricing.InputPricePerTokenPriority > 0 || pricing.OutputPricePerTokenPriority > 0 ||
		pricing.CacheCreationPricePerTokenPriority > 0 || pricing.CacheReadPricePerTokenPriority > 0
}

func serviceTierCostMultiplierForModel(model, serviceTier string) float64 {
	switch normalizeBillingServiceTier(serviceTier) {
	case "priority":
		if isOpenAIGPT55Model(model) {
			return openAIGPT55FastMultiplier
		}
		return openAIGPT54FastMultiplier
	case "flex":
		return 0.5
	default:
		return 1.0
	}
}

func serviceTierCostMultiplier(serviceTier string) float64 {
	return serviceTierCostMultiplierForModel("", serviceTier)
}

// UsageTokens 使用的token数量
type UsageTokens struct {
	InputTokens           int
	TextInputTokens       int
	ImageInputTokens      int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	ImageCacheReadTokens  int
	CacheCreation5mTokens int
	CacheCreation1hTokens int
	ImageOutputTokens     int
}

// CostBreakdown 费用明细
type CostBreakdown struct {
	InputCost         float64 // 文本输入费用，不含图片输入
	ImageInputCost    float64 // 图片输入 token 费用
	OutputCost        float64
	ImageOutputCost   float64
	CacheCreationCost float64
	CacheReadCost     float64
	TotalCost         float64
	ActualCost        float64 // 应用倍率后的实际费用
	BillingMode       string  // 计费模式（"token"/"per_request"/"image"），由 CalculateCostUnified 填充
}

// ErrModelPricingUnavailable indicates that none of the configured pricing
// sources can price the requested model.
var ErrModelPricingUnavailable = errors.New("pricing not found")

// fallbackWarnSeenMaxEntries 限制 fallbackWarnSeen 去重表的容量上界。
// getFallbackPricing 对 Claude 族有 `strings.Contains(model, "claude")` 这样的宽匹配，
// 模型名来自请求体，攻击者可用 claude-<随机串> 无限撑大去重表，
// 因此必须封顶而不是像上游那样无界增长。正常业务的兜底模型集合远小于该上界。
const fallbackWarnSeenMaxEntries = 1024

// BillingService 计费服务
type BillingService struct {
	cfg            *config.Config
	pricingService *PricingService
	fallbackPrices map[string]*ModelPricing // 硬编码回退价格

	// fallbackWarnSeen 记录已打过兜底定价告警的（已小写化）模型名，
	// 让 "[Billing] Using fallback pricing" 每个模型每进程最多打一条，
	// 避免热路径每请求刷屏、把 ops_system_logs 灌爆。
	// 告警本身是"新模型漏配定价"的唯一信号，只降噪不删除。
	// 零值即可用，无需在构造函数初始化。
	fallbackWarnSeen sync.Map
	// fallbackWarnSeenCount 近似记录 fallbackWarnSeen 的条目数，用于封顶。
	// 并发下可能短暂略微超过上界（最多超出并发 goroutine 数），可接受。
	fallbackWarnSeenCount atomic.Int64
	// fallbackWarnCapLogged 保证"去重表已满"这条提示每进程只打一次。
	fallbackWarnCapLogged atomic.Bool
}

// shouldWarnFallbackPricing 判断当前模型是否需要打一条兜底定价告警。
// 语义：每个模型名每进程最多返回一次 true；去重表达到上界后不再收录新模型，
// 并一次性打印一条提示说明后续首次告警被抑制（保证内存有界且不静默失明）。
// model 必须是调用方已小写化后的模型名。
func (s *BillingService) shouldWarnFallbackPricing(model string) bool {
	if _, seen := s.fallbackWarnSeen.Load(model); seen {
		return false
	}
	if s.fallbackWarnSeenCount.Load() >= fallbackWarnSeenMaxEntries {
		if s.fallbackWarnCapLogged.CompareAndSwap(false, true) {
			log.Printf("[Billing] fallback pricing warn dedup table reached %d models; suppressing further first-seen warnings",
				fallbackWarnSeenMaxEntries)
		}
		return false
	}
	if _, loaded := s.fallbackWarnSeen.LoadOrStore(model, struct{}{}); loaded {
		return false
	}
	s.fallbackWarnSeenCount.Add(1)
	return true
}

// NewBillingService 创建计费服务实例
func NewBillingService(cfg *config.Config, pricingService *PricingService) *BillingService {
	s := &BillingService{
		cfg:            cfg,
		pricingService: pricingService,
		fallbackPrices: make(map[string]*ModelPricing),
	}

	// 初始化硬编码回退价格（当动态价格不可用时使用）
	s.initFallbackPricing()

	return s
}

// initFallbackPricing 初始化硬编码回退价格（当动态价格不可用时使用）
// 价格单位：USD per token（与LiteLLM格式一致）
func (s *BillingService) initFallbackPricing() {
	// Claude 4.5 Opus
	s.fallbackPrices["claude-opus-4.5"] = &ModelPricing{
		InputPricePerToken:         5e-6,    // $5 per MTok
		OutputPricePerToken:        25e-6,   // $25 per MTok
		CacheCreationPricePerToken: 6.25e-6, // $6.25 per MTok
		CacheReadPricePerToken:     0.5e-6,  // $0.50 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 4 Sonnet
	s.fallbackPrices["claude-sonnet-4"] = &ModelPricing{
		InputPricePerToken:         3e-6,    // $3 per MTok
		OutputPricePerToken:        15e-6,   // $15 per MTok
		CacheCreationPricePerToken: 3.75e-6, // $3.75 per MTok
		CacheReadPricePerToken:     0.3e-6,  // $0.30 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3.5 Sonnet
	s.fallbackPrices["claude-3-5-sonnet"] = &ModelPricing{
		InputPricePerToken:         3e-6,    // $3 per MTok
		OutputPricePerToken:        15e-6,   // $15 per MTok
		CacheCreationPricePerToken: 3.75e-6, // $3.75 per MTok
		CacheReadPricePerToken:     0.3e-6,  // $0.30 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3.5 Haiku
	s.fallbackPrices["claude-3-5-haiku"] = &ModelPricing{
		InputPricePerToken:         1e-6,    // $1 per MTok
		OutputPricePerToken:        5e-6,    // $5 per MTok
		CacheCreationPricePerToken: 1.25e-6, // $1.25 per MTok
		CacheReadPricePerToken:     0.1e-6,  // $0.10 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3 Opus
	s.fallbackPrices["claude-3-opus"] = &ModelPricing{
		InputPricePerToken:         15e-6,    // $15 per MTok
		OutputPricePerToken:        75e-6,    // $75 per MTok
		CacheCreationPricePerToken: 18.75e-6, // $18.75 per MTok
		CacheReadPricePerToken:     1.5e-6,   // $1.50 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 3 Haiku
	s.fallbackPrices["claude-3-haiku"] = &ModelPricing{
		InputPricePerToken:         0.25e-6, // $0.25 per MTok
		OutputPricePerToken:        1.25e-6, // $1.25 per MTok
		CacheCreationPricePerToken: 0.3e-6,  // $0.30 per MTok
		CacheReadPricePerToken:     0.03e-6, // $0.03 per MTok
		SupportsCacheBreakdown:     false,
	}

	// Claude 4.6 Opus (与4.5同价)
	s.fallbackPrices["claude-opus-4.6"] = s.fallbackPrices["claude-opus-4.5"]

	// Claude 4.7 Opus (暂与4.6同价，待官方定价更新)
	s.fallbackPrices["claude-opus-4.7"] = s.fallbackPrices["claude-opus-4.6"]

	// Claude Opus 5（官方基础费率与 Claude 4.5 Opus 相同）
	s.fallbackPrices["claude-opus-5"] = s.fallbackPrices["claude-opus-4.5"]

	// Gemini 3.1 Pro
	s.fallbackPrices["gemini-3.1-pro"] = &ModelPricing{
		InputPricePerToken:         2e-6,   // $2 per MTok
		OutputPricePerToken:        12e-6,  // $12 per MTok
		CacheCreationPricePerToken: 2e-6,   // $2 per MTok
		CacheReadPricePerToken:     0.2e-6, // $0.20 per MTok
		SupportsCacheBreakdown:     false,
	}

	// OpenAI GPT-5.4（业务指定价格）
	s.fallbackPrices["gpt-5.4"] = &ModelPricing{
		InputPricePerToken:             62.5e-6,
		InputPricePerTokenPriority:     125e-6,
		OutputPricePerToken:            375e-6,
		OutputPricePerTokenPriority:    750e-6,
		CacheCreationPricePerToken:     62.5e-6,
		CacheReadPricePerToken:         6.25e-6,
		CacheReadPricePerTokenPriority: 12.5e-6,
		SupportsCacheBreakdown:         false,
		LongContextInputThreshold:      openAIGPT54LongContextInputThreshold,
		LongContextInputMultiplier:     openAIGPT54LongContextInputMultiplier,
		LongContextOutputMultiplier:    openAIGPT54LongContextOutputMultiplier,
	}
	// OpenAI GPT-5.5（Codex rate card）
	s.fallbackPrices["gpt-5.5"] = &ModelPricing{
		InputPricePerToken:             125e-6,
		InputPricePerTokenPriority:     312.5e-6,
		OutputPricePerToken:            750e-6,
		OutputPricePerTokenPriority:    1875e-6,
		CacheCreationPricePerToken:     125e-6,
		CacheReadPricePerToken:         12.5e-6,
		CacheReadPricePerTokenPriority: 31.25e-6,
		SupportsCacheBreakdown:         false,
		LongContextInputThreshold:      openAIGPT54LongContextInputThreshold,
		LongContextInputMultiplier:     openAIGPT54LongContextInputMultiplier,
		LongContextOutputMultiplier:    openAIGPT54LongContextOutputMultiplier,
	}

	// OpenAI GPT-5.6 官方价格（USD/token）。缓存写入为输入价的 1.25 倍。
	s.fallbackPrices["gpt-5.6-sol"] = &ModelPricing{
		InputPricePerToken:                 5e-6,
		InputPricePerTokenPriority:         10e-6,
		OutputPricePerToken:                30e-6,
		OutputPricePerTokenPriority:        60e-6,
		CacheCreationPricePerToken:         6.25e-6,
		CacheCreationPricePerTokenPriority: 12.5e-6,
		CacheReadPricePerToken:             0.5e-6,
		CacheReadPricePerTokenPriority:     1e-6,
		LongContextInputThreshold:          openAIGPT54LongContextInputThreshold,
		LongContextInputMultiplier:         openAIGPT54LongContextInputMultiplier,
		LongContextOutputMultiplier:        openAIGPT54LongContextOutputMultiplier,
	}
	s.fallbackPrices["gpt-5.6-terra"] = &ModelPricing{
		InputPricePerToken:                 2.5e-6,
		InputPricePerTokenPriority:         5e-6,
		OutputPricePerToken:                15e-6,
		OutputPricePerTokenPriority:        30e-6,
		CacheCreationPricePerToken:         3.125e-6,
		CacheCreationPricePerTokenPriority: 6.25e-6,
		CacheReadPricePerToken:             0.25e-6,
		CacheReadPricePerTokenPriority:     0.5e-6,
		LongContextInputThreshold:          openAIGPT54LongContextInputThreshold,
		LongContextInputMultiplier:         openAIGPT54LongContextInputMultiplier,
		LongContextOutputMultiplier:        openAIGPT54LongContextOutputMultiplier,
	}
	s.fallbackPrices["gpt-5.6-luna"] = &ModelPricing{
		InputPricePerToken:                 1e-6,
		InputPricePerTokenPriority:         2e-6,
		OutputPricePerToken:                6e-6,
		OutputPricePerTokenPriority:        12e-6,
		CacheCreationPricePerToken:         1.25e-6,
		CacheCreationPricePerTokenPriority: 2.5e-6,
		CacheReadPricePerToken:             0.1e-6,
		CacheReadPricePerTokenPriority:     0.2e-6,
		LongContextInputThreshold:          openAIGPT54LongContextInputThreshold,
		LongContextInputMultiplier:         openAIGPT54LongContextInputMultiplier,
		LongContextOutputMultiplier:        openAIGPT54LongContextOutputMultiplier,
	}

	s.fallbackPrices["gpt-5.4-mini"] = &ModelPricing{
		InputPricePerToken:     7.5e-7,
		OutputPricePerToken:    4.5e-6,
		CacheReadPricePerToken: 7.5e-8,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["gpt-5.4-nano"] = &ModelPricing{
		InputPricePerToken:     2e-7,
		OutputPricePerToken:    1.25e-6,
		CacheReadPricePerToken: 2e-8,
		SupportsCacheBreakdown: false,
	}
	// OpenAI GPT-5.2（本地兜底）
	s.fallbackPrices["gpt-5.2"] = &ModelPricing{
		InputPricePerToken:             1.75e-6,
		InputPricePerTokenPriority:     3.5e-6,
		OutputPricePerToken:            14e-6,
		OutputPricePerTokenPriority:    28e-6,
		CacheCreationPricePerToken:     1.75e-6,
		CacheReadPricePerToken:         0.175e-6,
		CacheReadPricePerTokenPriority: 0.35e-6,
		SupportsCacheBreakdown:         false,
	}
	// Codex 族兜底统一按 GPT-5.3 Codex 价格计费
	s.fallbackPrices["gpt-5.3-codex"] = &ModelPricing{
		InputPricePerToken:             1.5e-6, // $1.5 per MTok
		InputPricePerTokenPriority:     3e-6,   // $3 per MTok
		OutputPricePerToken:            12e-6,  // $12 per MTok
		OutputPricePerTokenPriority:    24e-6,  // $24 per MTok
		CacheCreationPricePerToken:     1.5e-6, // $1.5 per MTok
		CacheReadPricePerToken:         0.15e-6,
		CacheReadPricePerTokenPriority: 0.3e-6,
		SupportsCacheBreakdown:         false,
	}

	// ============================================================
	// 国产 LLM 兜底定价（数据源：各家官方定价页，USD 口径）
	// 顺序：DeepSeek → 智谱 GLM → 月之暗面 Kimi → MiniMax → 火山方舟豆包
	// 匹配逻辑见同文件 getFallbackPricing()，采用白名单语义：
	// 未在本表命中的国产模型 alias 一律不返回兜底价，避免误计价。
	// 注意：这些价格只是"兜底"，LiteLLM 动态定价与渠道自定义定价都优先于它。
	// ============================================================

	// ---- DeepSeek V4 系列 ----
	// Source: https://api-docs.deepseek.com/quick_start/pricing
	s.fallbackPrices["deepseek-v4-pro"] = &ModelPricing{
		InputPricePerToken:     4.35e-7,  // $0.435 per MTok (cache miss)
		OutputPricePerToken:    8.7e-7,   // $0.87 per MTok
		CacheReadPricePerToken: 3.625e-9, // $0.003625 per MTok (cache hit)
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["deepseek-v4-flash"] = &ModelPricing{
		InputPricePerToken:     1.4e-7, // $0.14 per MTok (cache miss)
		OutputPricePerToken:    2.8e-7, // $0.28 per MTok
		CacheReadPricePerToken: 2.8e-9, // $0.0028 per MTok (cache hit)
		SupportsCacheBreakdown: false,
	}

	// ---- 智谱 GLM（Z.AI）----
	// Source: https://docs.z.ai/guides/overview/pricing (USD per 1M tokens)
	// 注意：CacheReadPricePerToken 即"缓存命中"价格；智谱未公开缓存写入价，CacheCreationPricePerToken 留空按 0 处理。
	// 采用 z.ai 国际版 USD 口径（与本表 Claude/GPT 一致），不用国内 ¥ 价换算。
	// GLM-5.2 与 GLM-5.1 在 z.ai 上同价。
	s.fallbackPrices["glm-5.2"] = &ModelPricing{
		InputPricePerToken:     1.4e-6, // $1.40 per MTok
		OutputPricePerToken:    4.4e-6, // $4.40 per MTok
		CacheReadPricePerToken: 0.26e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-5.1"] = &ModelPricing{
		InputPricePerToken:     1.4e-6, // $1.40 per MTok
		OutputPricePerToken:    4.4e-6, // $4.40 per MTok
		CacheReadPricePerToken: 0.26e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-5"] = &ModelPricing{
		InputPricePerToken:     1e-6, // $1.00 per MTok
		OutputPricePerToken:    3.2e-6,
		CacheReadPricePerToken: 0.2e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-5-turbo"] = &ModelPricing{
		InputPricePerToken:     1.2e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.24e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.7"] = &ModelPricing{
		InputPricePerToken:     0.6e-6, // $0.60 per MTok
		OutputPricePerToken:    2.2e-6,
		CacheReadPricePerToken: 0.11e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.7-flashx"] = &ModelPricing{
		InputPricePerToken:     0.07e-6, // $0.07 per MTok
		OutputPricePerToken:    0.4e-6,
		CacheReadPricePerToken: 0.01e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.6"] = &ModelPricing{
		InputPricePerToken:     0.6e-6, // $0.60 per MTok
		OutputPricePerToken:    2.2e-6,
		CacheReadPricePerToken: 0.11e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5"] = &ModelPricing{
		InputPricePerToken:     0.6e-6, // $0.60 per MTok
		OutputPricePerToken:    2.2e-6,
		CacheReadPricePerToken: 0.11e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5-x"] = &ModelPricing{
		InputPricePerToken:     2.2e-6, // $2.20 per MTok
		OutputPricePerToken:    8.9e-6,
		CacheReadPricePerToken: 0.45e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5-air"] = &ModelPricing{
		InputPricePerToken:     0.2e-6, // $0.20 per MTok
		OutputPricePerToken:    1.1e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.5-airx"] = &ModelPricing{
		InputPricePerToken:     1.1e-6,
		OutputPricePerToken:    4.5e-6,
		CacheReadPricePerToken: 0.22e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4-32b-0414-128k"] = &ModelPricing{
		InputPricePerToken:     0.1e-6, // $0.10 per MTok
		OutputPricePerToken:    0.1e-6,
		SupportsCacheBreakdown: false,
	}
	// GLM-4.5-Flash / GLM-4.7-Flash 在 z.ai 上免费，保留 0 价条目
	// 是为了让它们命中白名单而不是掉进"未知模型"分支。
	s.fallbackPrices["glm-4.5-flash"] = &ModelPricing{
		InputPricePerToken:     0,
		OutputPricePerToken:    0,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["glm-4.7-flash"] = &ModelPricing{
		InputPricePerToken:     0,
		OutputPricePerToken:    0,
		SupportsCacheBreakdown: false,
	}

	// ---- 月之暗面 Kimi（K 系列）----
	// Source: https://platform.moonshot.cn/docs/pricing/overview（¥/百万 token，按 ¥1≈$0.14 换算）
	//         Kimi K3 国际站 USD 价目：https://platform.kimi.ai/docs/pricing/chat-k3.md
	// Moonshot V1（¥2/¥5/¥10 多 tier）公开页未直接标注 USD 价，不覆盖，避免误计价。
	// K2-0905 / K2-0711 官方页面未保留定价，隐性回退到 kimi-k2。
	s.fallbackPrices["kimi-k3"] = &ModelPricing{
		InputPricePerToken:     3e-6,    // $3.00 per MTok (cache miss)
		OutputPricePerToken:    15e-6,   // $15.00 per MTok
		CacheReadPricePerToken: 0.30e-6, // $0.30 per MTok (cache hit)
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2.6"] = &ModelPricing{
		InputPricePerToken:     0.95e-6, // $0.95 per MTok (cache miss)
		OutputPricePerToken:    4e-6,    // $4.00 per MTok
		CacheReadPricePerToken: 0.15e-6, // $0.15 per MTok (cache hit, ¥1.10)
		SupportsCacheBreakdown: false,
	}
	// kimi-for-coding 走 Kimi Coding endpoint，官方无独立按 token 价目，
	// 按当前 K2.6 coding 档位兜底计费。
	s.fallbackPrices["kimi-for-coding"] = &ModelPricing{
		InputPricePerToken:     0.95e-6,
		OutputPricePerToken:    4e-6,
		CacheReadPricePerToken: 0.15e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2.5"] = &ModelPricing{
		InputPricePerToken:     0.60e-6, // $0.60 per MTok
		OutputPricePerToken:    3e-6,    // $3.00 per MTok
		CacheReadPricePerToken: 0.098e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2-thinking"] = &ModelPricing{
		InputPricePerToken:     0.56e-6, // ¥4/百万 ≈ $0.56
		OutputPricePerToken:    2.24e-6, // ¥16/百万
		CacheReadPricePerToken: 0.14e-6, // ¥1/百万
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["kimi-k2"] = &ModelPricing{
		InputPricePerToken:     0.56e-6, // ¥4/百万
		OutputPricePerToken:    2.24e-6, // ¥16/百万
		CacheReadPricePerToken: 0.14e-6, // ¥1/百万
		SupportsCacheBreakdown: false,
	}

	// ---- MiniMax M 系列 ----
	// Source: https://platform.minimax.io/docs/guides/pricing-paygo
	// 注意：MiniMax M3 在 >512K context 时价格翻倍，本兜底采用 ≤512K 标准 tier
	//（保守口径，对用户有利）。如需长上下文倍率可参考 GPT-5.4 的 LongContextXxx 字段扩展。
	s.fallbackPrices["minimax-m3"] = &ModelPricing{
		InputPricePerToken:     0.60e-6, // $0.60 per MTok (≤512K standard tier)
		OutputPricePerToken:    2.40e-6,
		CacheReadPricePerToken: 0.12e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2.7"] = &ModelPricing{
		InputPricePerToken:     0.30e-6, // $0.30 per MTok
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.06e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2.7-highspeed"] = &ModelPricing{
		InputPricePerToken:     0.60e-6,
		OutputPricePerToken:    2.40e-6,
		CacheReadPricePerToken: 0.06e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2.5"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2.1"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}
	s.fallbackPrices["minimax-m2"] = &ModelPricing{
		InputPricePerToken:     0.30e-6,
		OutputPricePerToken:    1.20e-6,
		CacheReadPricePerToken: 0.03e-6,
		SupportsCacheBreakdown: false,
	}

	// ---- 火山方舟 豆包 Embedding（多模态向量化）----
	// doubao-embedding-vision 图文向量化：上游 usage 回传 prompt_tokens_details.{text_tokens,image_tokens}，
	// 按量付费官方价 文本 ¥0.7/MTok、图片 ¥1.8/MTok；汇率口径 ÷7.14（与本表其他国产模型一致，¥1≈$0.14）。
	// embedding 无 output，OutputPricePerToken 置 0。
	s.fallbackPrices["doubao-embedding-vision"] = &ModelPricing{
		InputPricePerToken:      0.098e-6, // ¥0.7/MTok ≈ $0.098（文本输入）
		ImageInputPricePerToken: 0.252e-6, // ¥1.8/MTok ≈ $0.252（图片输入）
		OutputPricePerToken:     0,
		SupportsCacheBreakdown:  false,
	}
}

// getFallbackPricing 根据模型系列获取回退价格
func (s *BillingService) getFallbackPricing(model string) *ModelPricing {
	modelLower := strings.ToLower(model)

	// 按模型系列匹配
	if strings.Contains(modelLower, "opus") {
		if strings.Contains(modelLower, "opus-5") {
			return s.fallbackPrices["claude-opus-5"]
		}
		if strings.Contains(modelLower, "4.7") || strings.Contains(modelLower, "4-7") {
			return s.fallbackPrices["claude-opus-4.7"]
		}
		if strings.Contains(modelLower, "4.6") || strings.Contains(modelLower, "4-6") {
			return s.fallbackPrices["claude-opus-4.6"]
		}
		if strings.Contains(modelLower, "4.5") || strings.Contains(modelLower, "4-5") {
			return s.fallbackPrices["claude-opus-4.5"]
		}
		return s.fallbackPrices["claude-3-opus"]
	}
	if strings.Contains(modelLower, "sonnet") {
		if strings.Contains(modelLower, "4") && !strings.Contains(modelLower, "3") {
			return s.fallbackPrices["claude-sonnet-4"]
		}
		return s.fallbackPrices["claude-3-5-sonnet"]
	}
	if strings.Contains(modelLower, "haiku") {
		if strings.Contains(modelLower, "3-5") || strings.Contains(modelLower, "3.5") {
			return s.fallbackPrices["claude-3-5-haiku"]
		}
		return s.fallbackPrices["claude-3-haiku"]
	}
	// Claude 未知型号统一回退到 Sonnet，避免计费中断。
	if strings.Contains(modelLower, "claude") {
		return s.fallbackPrices["claude-sonnet-4"]
	}
	if strings.Contains(modelLower, "gemini-3.1-pro") || strings.Contains(modelLower, "gemini-3-1-pro") {
		return s.fallbackPrices["gemini-3.1-pro"]
	}

	// ---- 国产 LLM 兜底匹配 ----
	// 匹配策略：长 key 优先（具体型号 → 系列），未知型号不回退以避免误计价。
	// 与 OpenAI 分支一样采用白名单语义：未在本表命中的国产模型 alias 一律返回 nil。

	// DeepSeek V4 系列。deepseek-chat / deepseek-reasoner 是官方兼容别名，
	// 现网通常由 LiteLLM 动态定价先命中，这里只作为动态定价缺失时的兜底。
	if strings.Contains(modelLower, "deepseek-v4-flash") {
		return s.fallbackPrices["deepseek-v4-flash"]
	}
	if strings.Contains(modelLower, "deepseek-v4-pro") {
		return s.fallbackPrices["deepseek-v4-pro"]
	}
	if strings.Contains(modelLower, "deepseek-chat") || strings.Contains(modelLower, "deepseek-reasoner") {
		return s.fallbackPrices["deepseek-v4-flash"]
	}

	// 智谱 GLM（z.ai 公开 SKU：glm-5.2 / glm-5.1 / glm-5 / glm-5-turbo / glm-4.7 / glm-4.6 / glm-4.5 等）
	// 顺序要求（strings.Contains 是子串匹配，先写的先赢）：
	//   1. 带小数点的高版本必须排在裸 "glm-5" 之前，否则 glm-5.2 会被 glm-5 抢走；
	//   2. "-flashx" 必须排在 "-flash" 之前（前者包含后者）；
	//   3. "-airx" 必须排在 "-air" 之前（同上）。
	if strings.Contains(modelLower, "glm-5.2") {
		return s.fallbackPrices["glm-5.2"]
	}
	if strings.Contains(modelLower, "glm-5.1") {
		return s.fallbackPrices["glm-5.1"]
	}
	if strings.Contains(modelLower, "glm-5-turbo") || strings.Contains(modelLower, "glm-5turbo") {
		return s.fallbackPrices["glm-5-turbo"]
	}
	if strings.Contains(modelLower, "glm-5") {
		return s.fallbackPrices["glm-5"]
	}
	if strings.Contains(modelLower, "glm-4.7-flashx") {
		return s.fallbackPrices["glm-4.7-flashx"]
	}
	if strings.Contains(modelLower, "glm-4.7-flash") {
		return s.fallbackPrices["glm-4.7-flash"]
	}
	if strings.Contains(modelLower, "glm-4.7") {
		return s.fallbackPrices["glm-4.7"]
	}
	if strings.Contains(modelLower, "glm-4.6") {
		return s.fallbackPrices["glm-4.6"]
	}
	if strings.Contains(modelLower, "glm-4.5-flash") {
		return s.fallbackPrices["glm-4.5-flash"]
	}
	if strings.Contains(modelLower, "glm-4.5-x") || strings.Contains(modelLower, "glm-4.5x") {
		return s.fallbackPrices["glm-4.5-x"]
	}
	if strings.Contains(modelLower, "glm-4.5-airx") || strings.Contains(modelLower, "glm-4.5airx") {
		return s.fallbackPrices["glm-4.5-airx"]
	}
	if strings.Contains(modelLower, "glm-4.5-air") || strings.Contains(modelLower, "glm-4.5air") {
		return s.fallbackPrices["glm-4.5-air"]
	}
	if strings.Contains(modelLower, "glm-4.5") {
		return s.fallbackPrices["glm-4.5"]
	}
	if strings.Contains(modelLower, "glm-4-32b") {
		return s.fallbackPrices["glm-4-32b-0414-128k"]
	}

	// 月之暗面 Kimi（kimi-k3 / k3 / k3-256k / kimi-k2.6 / kimi-for-coding / kimi-k2.5 / kimi-k2-thinking / kimi-k2）
	// K3 规则必须置于 K2 之前。K3 只接受精确名或 "/" 路径后缀，
	// 避免 kimi-k30 之类未知型号被子串误命中。
	// 注意：kimi-k3[1m] 是 Claude Code 的上下文选择语法，不是 Kimi API 模型 ID，不进入兜底。
	if strings.Contains(modelLower, "kimi-for-coding") {
		return s.fallbackPrices["kimi-for-coding"]
	}
	if modelLower == "kimi-k3" || strings.HasSuffix(modelLower, "/kimi-k3") ||
		modelLower == "k3" || modelLower == "k3-256k" ||
		strings.HasSuffix(modelLower, "/k3") || strings.HasSuffix(modelLower, "/k3-256k") {
		return s.fallbackPrices["kimi-k3"]
	}
	if strings.Contains(modelLower, "kimi-k2.6") || strings.Contains(modelLower, "kimi-k2-6") {
		return s.fallbackPrices["kimi-k2.6"]
	}
	if strings.Contains(modelLower, "kimi-k2.5") || strings.Contains(modelLower, "kimi-k2-5") {
		return s.fallbackPrices["kimi-k2.5"]
	}
	if strings.Contains(modelLower, "kimi-k2-thinking") {
		return s.fallbackPrices["kimi-k2-thinking"]
	}
	if strings.Contains(modelLower, "kimi-k2") || strings.Contains(modelLower, "kimi/k2") {
		return s.fallbackPrices["kimi-k2"]
	}

	// MiniMax M 系列（M3 / M2.7 / M2.5 / M2.1 / M2；含 highspeed 变体）
	// highspeed 变体必须排在裸 m2.7 之前。
	if strings.Contains(modelLower, "minimax-m3") {
		return s.fallbackPrices["minimax-m3"]
	}
	if strings.Contains(modelLower, "minimax-m2.7-highspeed") || strings.Contains(modelLower, "minimax-m2-7-highspeed") {
		return s.fallbackPrices["minimax-m2.7-highspeed"]
	}
	if strings.Contains(modelLower, "minimax-m2.7") || strings.Contains(modelLower, "minimax-m2-7") {
		return s.fallbackPrices["minimax-m2.7"]
	}
	if strings.Contains(modelLower, "minimax-m2.5") || strings.Contains(modelLower, "minimax-m2-5") {
		return s.fallbackPrices["minimax-m2.5"]
	}
	if strings.Contains(modelLower, "minimax-m2.1") || strings.Contains(modelLower, "minimax-m2-1") {
		return s.fallbackPrices["minimax-m2.1"]
	}
	if strings.Contains(modelLower, "minimax-m2") || strings.Contains(modelLower, "minimax-m-2") {
		return s.fallbackPrices["minimax-m2"]
	}

	// 火山方舟 豆包 Embedding（多模态向量化）。
	// most-specific-first：需排在未来任何 doubao-embedding / doubao 宽匹配之前。
	// 覆盖带版本后缀的别名（如 doubao-embedding-vision-251215）。
	if strings.Contains(modelLower, "doubao-embedding-vision") {
		return s.fallbackPrices["doubao-embedding-vision"]
	}

	// OpenAI 仅匹配已知 GPT-5/Codex 族，避免未知 OpenAI 型号误计价。
	if normalized := normalizeKnownOpenAICodexModel(modelLower); normalized != "" {
		switch normalized {
		case "gpt-5.6-sol":
			return s.fallbackPrices["gpt-5.6-sol"]
		case "gpt-5.6-terra":
			return s.fallbackPrices["gpt-5.6-terra"]
		case "gpt-5.6-luna":
			return s.fallbackPrices["gpt-5.6-luna"]
		case "gpt-5.5":
			return s.fallbackPrices["gpt-5.5"]
		case "gpt-5.4-nano":
			return s.fallbackPrices["gpt-5.4-nano"]
		case "gpt-5.4-mini":
			return s.fallbackPrices["gpt-5.4-mini"]
		case "gpt-5.4":
			return s.fallbackPrices["gpt-5.4"]
		case "gpt-5.2":
			return s.fallbackPrices["gpt-5.2"]
		case "gpt-5.3-codex", "gpt-5.3-codex-spark":
			return s.fallbackPrices["gpt-5.3-codex"]
		}
	}

	return nil
}

// GetModelPricing 获取模型价格配置
// HasIdentifiedTokenPricing reports whether model has a deterministic token
// price. It intentionally excludes family-name guesses used by the ordinary
// billing fallback path because response-declared model names are untrusted.
func (s *BillingService) HasIdentifiedTokenPricing(model string) bool {
	if s == nil {
		return false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if s.pricingService != nil {
		// Pixel's pricing parser retains only entries that declare at least one
		// input/output token price, so a deterministic hit here is token-capable.
		if pricing := s.pricingService.GetIdentifiedModelPricing(model); pricing != nil {
			return true
		}
	}
	pricing, ok := s.fallbackPrices[model]
	return ok && pricing != nil
}

func (s *BillingService) GetModelPricing(model string) (*ModelPricing, error) {
	// 标准化模型名称（转小写）
	model = strings.ToLower(model)

	// 1. 优先从动态价格服务获取
	if s.pricingService != nil {
		litellmPricing := s.pricingService.GetModelPricing(model)
		if litellmPricing != nil {
			// 启用 5m/1h 分类计费的条件：
			// 1. 存在 1h 价格
			// 2. 1h 价格 > 5m 价格（防止 LiteLLM 数据错误导致少收费）
			price5m := litellmPricing.CacheCreationInputTokenCost
			price1h := litellmPricing.CacheCreationInputTokenCostAbove1hr
			enableBreakdown := price1h > 0 && price1h > price5m
			return s.applyModelSpecificPricingPolicy(model, &ModelPricing{
				InputPricePerToken:                 litellmPricing.InputCostPerToken,
				InputPricePerTokenPriority:         litellmPricing.InputCostPerTokenPriority,
				OutputPricePerToken:                litellmPricing.OutputCostPerToken,
				OutputPricePerTokenPriority:        litellmPricing.OutputCostPerTokenPriority,
				CacheCreationPricePerToken:         litellmPricing.CacheCreationInputTokenCost,
				CacheCreationPricePerTokenPriority: litellmPricing.CacheCreationInputTokenCostPriority,
				CacheReadPricePerToken:             litellmPricing.CacheReadInputTokenCost,
				CacheReadPricePerTokenPriority:     litellmPricing.CacheReadInputTokenCostPriority,
				CacheCreation5mPrice:               price5m,
				CacheCreation1hPrice:               price1h,
				SupportsCacheBreakdown:             enableBreakdown,
				LongContextInputThreshold:          litellmPricing.LongContextInputTokenThreshold,
				LongContextInputMultiplier:         litellmPricing.LongContextInputCostMultiplier,
				LongContextOutputMultiplier:        litellmPricing.LongContextOutputCostMultiplier,
				ImageOutputPricePerToken:           litellmPricing.OutputCostPerImageToken,
			}), nil
		}
	}

	// 2. 使用硬编码回退价格
	fallback := s.getFallbackPricing(model)
	if fallback != nil {
		// 按模型名去重：每个模型每进程最多打一条告警，避免热路径每请求刷屏。
		// model 在函数入口已 ToLower，故 GLM-5.2 / glm-5.2 视为同一条目。
		if s.shouldWarnFallbackPricing(model) {
			log.Printf("[Billing] Using fallback pricing for model: %s", model)
		}
		return s.applyModelSpecificPricingPolicy(model, fallback), nil
	}

	return nil, fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, model)
}

// GetModelPricingWithChannel 获取模型定价，渠道配置的价格覆盖默认值
// 仅覆盖渠道中非 nil 的价格字段，nil 字段使用默认定价
func (s *BillingService) GetModelPricingWithChannel(model string, channelPricing *ChannelModelPricing) (*ModelPricing, error) {
	pricing, err := s.GetModelPricing(model)
	if err != nil {
		return nil, err
	}
	if channelPricing == nil {
		return pricing, nil
	}
	// 防止渠道覆盖修改 fallbackPrices / PricingService 中的共享定价指针。
	cloned := *pricing
	pricing = &cloned
	if channelPricing.InputPrice != nil {
		pricing.InputPricePerToken = *channelPricing.InputPrice
		pricing.InputPricePerTokenPriority = 0
	}
	if channelPricing.OutputPrice != nil {
		pricing.OutputPricePerToken = *channelPricing.OutputPrice
		pricing.OutputPricePerTokenPriority = 0
	}
	if channelPricing.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *channelPricing.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *channelPricing.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *channelPricing.CacheWritePrice
		pricing.CacheCreation1hPrice = *channelPricing.CacheWritePrice
	}
	if channelPricing.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *channelPricing.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = 0
	}
	if channelPricing.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *channelPricing.ImageInputPrice
	}
	if channelPricing.ImageCacheReadPrice != nil {
		pricing.ImageCacheReadPricePerToken = *channelPricing.ImageCacheReadPrice
	}
	if channelPricing.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *channelPricing.ImageOutputPrice
	}
	return pricing, nil
}

// --- 统一计费入口 ---

// CostInput 统一计费输入
type CostInput struct {
	Ctx            context.Context
	Model          string
	GroupID        *int64 // 用于渠道定价查找
	Tokens         UsageTokens
	RequestCount   int    // 按次计费时使用
	SizeTier       string // 按次/图片模式的层级标签（"1K","2K","4K","HD" 等）
	RateMultiplier float64
	ServiceTier    string                // "priority","flex","" 等
	Resolver       *ModelPricingResolver // 定价解析器
	Resolved       *ResolvedPricing      // 可选：预解析的定价结果（避免重复 Resolve 调用）
	// LongContextBillingEnabled 只用于无 Resolver 的兼容路径。
	// nil 与 false 均表示关闭，避免缺少渠道策略时隐式启用长上下文倍率。
	LongContextBillingEnabled *bool
}

// CalculateCostUnified 统一计费入口，支持三种计费模式。
// 使用 ModelPricingResolver 解析定价，然后根据 BillingMode 分发计算。
func (s *BillingService) CalculateCostUnified(input CostInput) (*CostBreakdown, error) {
	if input.Resolver == nil {
		applyLongContextBilling := false
		if input.LongContextBillingEnabled != nil {
			applyLongContextBilling = *input.LongContextBillingEnabled
		}
		return s.calculateCostInternalWithPolicy(
			input.Model,
			input.Tokens,
			input.RateMultiplier,
			input.ServiceTier,
			nil,
			applyLongContextBilling,
		)
	}

	// 优先使用预解析结果，避免重复 Resolve 调用
	resolved := input.Resolved
	if resolved == nil {
		resolved = input.Resolver.Resolve(input.Ctx, PricingInput{
			Model:   input.Model,
			GroupID: input.GroupID,
		})
	}

	// 保存时强制 > 0；若仍有负数泄漏（缓存/迁移残留），按 0 处理避免按 1x 误扣。
	if input.RateMultiplier < 0 {
		input.RateMultiplier = 0
	}

	var breakdown *CostBreakdown
	var err error
	switch resolved.Mode {
	case BillingModePerRequest, BillingModeImage:
		breakdown, err = s.calculatePerRequestCost(resolved, input)
	default: // BillingModeToken
		breakdown, err = s.calculateTokenCost(resolved, input)
	}
	if err == nil && breakdown != nil {
		breakdown.BillingMode = string(resolved.Mode)
		if breakdown.BillingMode == "" {
			breakdown.BillingMode = string(BillingModeToken)
		}
	}
	return breakdown, err
}

// calculateTokenCost 按 token 区间计费
func (s *BillingService) calculateTokenCost(resolved *ResolvedPricing, input CostInput) (*CostBreakdown, error) {
	totalContext := input.Tokens.InputTokens + input.Tokens.TextInputTokens + input.Tokens.ImageInputTokens +
		input.Tokens.CacheCreationTokens + input.Tokens.CacheReadTokens

	pricing := input.Resolver.GetIntervalPricing(resolved, totalContext)
	if pricing == nil {
		return nil, fmt.Errorf("no pricing available for model: %s: %w", input.Model, ErrModelPricingUnavailable)
	}

	pricing = s.applyModelSpecificPricingPolicy(input.Model, pricing)

	// 长上下文倍率默认关闭，仅允许渠道价格配置显式开启。
	// 区间定价已包含上下文分层，不能与该倍率同时使用。
	applyLongCtx := false
	if resolved.LongContextPricingEnabled != nil {
		applyLongCtx = *resolved.LongContextPricingEnabled
		if applyLongCtx {
			if len(resolved.Intervals) > 0 {
				return nil, fmt.Errorf("long-context pricing conflicts with context pricing intervals for model: %s", input.Model)
			}
			if resolved.LongContextInputTokenThreshold == nil || *resolved.LongContextInputTokenThreshold <= 0 ||
				*resolved.LongContextInputTokenThreshold > maxLongContextInputTokenThreshold {
				return nil, fmt.Errorf("long-context pricing requires an input token threshold between 1 and %d for model: %s", maxLongContextInputTokenThreshold, input.Model)
			}
			cloned := *pricing
			cloned.LongContextInputThreshold = *resolved.LongContextInputTokenThreshold
			pricing = &cloned
		}
	}

	return s.computeTokenBreakdown(input.Model, pricing, input.Tokens, input.RateMultiplier, input.ServiceTier, applyLongCtx), nil
}

// computeTokenBreakdown 是 token 计费的核心逻辑，由 calculateTokenCost 和 calculateCostInternal 共用。
// applyLongCtx 控制是否检查长上下文定价（区间定价已自含上下文分层，不需要额外应用）。
func (s *BillingService) computeTokenBreakdown(
	model string, pricing *ModelPricing, tokens UsageTokens,
	rateMultiplier float64, serviceTier string,
	applyLongCtx bool,
) *CostBreakdown {
	// 保存时强制 > 0；若仍有负数泄漏，按 0 处理避免按 1x 误扣。
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}

	inputPrice := pricing.InputPricePerToken
	outputPrice := pricing.OutputPricePerToken
	cacheReadPrice := pricing.CacheReadPricePerToken
	cacheCreationPrice := pricing.CacheCreationPricePerToken
	imageInputPrice := pricing.ImageInputPricePerToken
	if imageInputPrice == 0 {
		imageInputPrice = inputPrice
	}
	imageCacheReadPrice := pricing.ImageCacheReadPricePerToken
	if imageCacheReadPrice == 0 {
		imageCacheReadPrice = cacheReadPrice
	}
	cacheCreationLongContextMultiplier := 1.0
	inputTierMultiplier := 1.0
	outputTierMultiplier := 1.0
	cacheCreationTierMultiplier := 1.0
	cacheReadTierMultiplier := 1.0
	imageInputTierMultiplier := 1.0
	imageCacheReadTierMultiplier := 1.0
	imageOutputTierMultiplier := 1.0

	switch normalizeBillingServiceTier(serviceTier) {
	case "priority":
		tierMultiplier := serviceTierCostMultiplierForModel(model, serviceTier)
		if pricing.InputPricePerTokenPriority > 0 {
			inputPrice = pricing.InputPricePerTokenPriority
		} else {
			inputTierMultiplier = tierMultiplier
		}
		if pricing.OutputPricePerTokenPriority > 0 {
			outputPrice = pricing.OutputPricePerTokenPriority
		} else {
			outputTierMultiplier = tierMultiplier
		}
		if pricing.CacheReadPricePerTokenPriority > 0 {
			cacheReadPrice = pricing.CacheReadPricePerTokenPriority
		} else {
			cacheReadTierMultiplier = tierMultiplier
		}
		if pricing.CacheCreationPricePerTokenPriority > 0 {
			cacheCreationPrice = pricing.CacheCreationPricePerTokenPriority
		} else {
			cacheCreationTierMultiplier = tierMultiplier
		}
		imageInputTierMultiplier = tierMultiplier
		imageCacheReadTierMultiplier = tierMultiplier
		imageOutputTierMultiplier = tierMultiplier
	case "flex":
		tierMultiplier := serviceTierCostMultiplier(serviceTier)
		inputTierMultiplier = tierMultiplier
		outputTierMultiplier = tierMultiplier
		cacheCreationTierMultiplier = tierMultiplier
		cacheReadTierMultiplier = tierMultiplier
		imageInputTierMultiplier = tierMultiplier
		imageCacheReadTierMultiplier = tierMultiplier
		imageOutputTierMultiplier = tierMultiplier
	}

	if applyLongCtx && s.shouldApplySessionLongContextPricing(tokens, pricing) {
		inputPrice *= pricing.LongContextInputMultiplier
		imageInputPrice *= pricing.LongContextInputMultiplier
		outputPrice *= pricing.LongContextOutputMultiplier
		cacheReadPrice *= pricing.LongContextInputMultiplier
		imageCacheReadPrice *= pricing.LongContextInputMultiplier
		cacheCreationLongContextMultiplier = pricing.LongContextInputMultiplier
	}

	bd := &CostBreakdown{}
	textInputTokens := tokens.InputTokens + tokens.TextInputTokens
	bd.InputCost = float64(textInputTokens) * inputPrice * inputTierMultiplier
	bd.ImageInputCost = float64(tokens.ImageInputTokens) * imageInputPrice * imageInputTierMultiplier

	// 分离图片输出 token 与文本输出 token
	textOutputTokens := tokens.OutputTokens - tokens.ImageOutputTokens
	if textOutputTokens < 0 {
		textOutputTokens = 0
	}
	bd.OutputCost = float64(textOutputTokens) * outputPrice * outputTierMultiplier

	// 图片输出 token 费用（独立费率）
	if tokens.ImageOutputTokens > 0 {
		imgPrice := pricing.ImageOutputPricePerToken
		if imgPrice == 0 {
			imgPrice = outputPrice // 回退到常规输出价格
		}
		bd.ImageOutputCost = float64(tokens.ImageOutputTokens) * imgPrice * imageOutputTierMultiplier
	}

	// 缓存创建费用
	bd.CacheCreationCost = s.computeCacheCreationCost(pricing, tokens, cacheCreationPrice) *
		cacheCreationLongContextMultiplier * cacheCreationTierMultiplier

	imageCacheReadTokens := clampImageCacheReadTokens(tokens)
	textCacheReadTokens := tokens.CacheReadTokens - imageCacheReadTokens
	bd.CacheReadCost = float64(textCacheReadTokens)*cacheReadPrice*cacheReadTierMultiplier +
		float64(imageCacheReadTokens)*imageCacheReadPrice*imageCacheReadTierMultiplier

	bd.TotalCost = bd.InputCost + bd.ImageInputCost + bd.OutputCost + bd.ImageOutputCost +
		bd.CacheCreationCost + bd.CacheReadCost
	bd.ActualCost = bd.TotalCost * rateMultiplier

	return bd
}

// computeCacheCreationCost 计算缓存创建费用（支持 5m/1h 分类或标准计费）。
func (s *BillingService) computeCacheCreationCost(pricing *ModelPricing, tokens UsageTokens, price float64) float64 {
	if pricing.SupportsCacheBreakdown && (pricing.CacheCreation5mPrice > 0 || pricing.CacheCreation1hPrice > 0) {
		if tokens.CacheCreation5mTokens == 0 && tokens.CacheCreation1hTokens == 0 && tokens.CacheCreationTokens > 0 {
			// API 未返回 ephemeral 明细，回退到全部按 5m 单价计费
			return float64(tokens.CacheCreationTokens) * pricing.CacheCreation5mPrice
		}
		return float64(tokens.CacheCreation5mTokens)*pricing.CacheCreation5mPrice +
			float64(tokens.CacheCreation1hTokens)*pricing.CacheCreation1hPrice
	}
	return float64(tokens.CacheCreationTokens) * price
}

// calculatePerRequestCost 按次/图片计费
func (s *BillingService) calculatePerRequestCost(resolved *ResolvedPricing, input CostInput) (*CostBreakdown, error) {
	count := input.RequestCount
	if count <= 0 {
		count = 1
	}

	var (
		unitPrice  float64
		priceFound bool
	)

	if input.SizeTier != "" {
		unitPrice, priceFound = input.Resolver.LookupRequestTierPrice(resolved, input.SizeTier)
	}

	if !priceFound {
		totalContext := input.Tokens.InputTokens + input.Tokens.TextInputTokens + input.Tokens.ImageInputTokens +
			input.Tokens.CacheCreationTokens + input.Tokens.CacheReadTokens
		unitPrice, priceFound = input.Resolver.LookupRequestTierPriceByContext(resolved, totalContext)
	}

	// 回退到默认按次价格
	if !priceFound {
		unitPrice = resolved.DefaultPerRequestPrice
	}

	totalCost := unitPrice * float64(count)
	actualCost := totalCost * input.RateMultiplier

	return &CostBreakdown{
		TotalCost:  totalCost,
		ActualCost: actualCost,
	}, nil
}

// CalculateCost 计算使用费用
func (s *BillingService) CalculateCost(model string, tokens UsageTokens, rateMultiplier float64) (*CostBreakdown, error) {
	return s.calculateCostInternal(model, tokens, rateMultiplier, "", nil)
}

func (s *BillingService) CalculateCostWithServiceTier(model string, tokens UsageTokens, rateMultiplier float64, serviceTier string) (*CostBreakdown, error) {
	return s.calculateCostInternal(model, tokens, rateMultiplier, serviceTier, nil)
}

func (s *BillingService) calculateCostInternal(model string, tokens UsageTokens, rateMultiplier float64, serviceTier string, channelPricing *ChannelModelPricing) (*CostBreakdown, error) {
	return s.calculateCostInternalWithPolicy(model, tokens, rateMultiplier, serviceTier, channelPricing, false)
}

func (s *BillingService) calculateCostInternalWithPolicy(
	model string,
	tokens UsageTokens,
	rateMultiplier float64,
	serviceTier string,
	channelPricing *ChannelModelPricing,
	longContextBillingEnabled bool,
) (*CostBreakdown, error) {
	var pricing *ModelPricing
	var err error
	if channelPricing != nil {
		pricing, err = s.GetModelPricingWithChannel(model, channelPricing)
	} else {
		pricing, err = s.GetModelPricing(model)
	}
	if err != nil {
		return nil, err
	}

	return s.computeTokenBreakdown(model, pricing, tokens, rateMultiplier, serviceTier, longContextBillingEnabled), nil
}

func (s *BillingService) applyModelSpecificPricingPolicy(model string, pricing *ModelPricing) *ModelPricing {
	if pricing == nil {
		return nil
	}
	normalized := normalizeKnownOpenAICodexModel(model)
	isGPT56 := isOpenAIGPT56Model(normalized)
	usesLegacyLongContextPricing := normalized == "gpt-5.4" || normalized == "gpt-5.5"
	if !isGPT56 && !usesLegacyLongContextPricing {
		return pricing
	}
	needsLongContextPolicy := pricing.LongContextInputThreshold <= 0 || pricing.LongContextInputMultiplier <= 0 || pricing.LongContextOutputMultiplier <= 0
	needsCacheCreationPolicy := isGPT56 && !pricing.CacheCreationPriceExplicit && (pricing.CacheCreationPricePerToken <= 0 ||
		(pricing.InputPricePerTokenPriority > 0 && pricing.CacheCreationPricePerTokenPriority <= 0))
	if !needsLongContextPolicy && !needsCacheCreationPolicy {
		return pricing
	}
	cloned := *pricing
	if isGPT56 && !cloned.CacheCreationPriceExplicit {
		if cloned.CacheCreationPricePerToken <= 0 {
			cloned.CacheCreationPricePerToken = cloned.InputPricePerToken * 1.25
		}
		if cloned.CacheCreationPricePerTokenPriority <= 0 {
			cloned.CacheCreationPricePerTokenPriority = cloned.InputPricePerTokenPriority * 1.25
		}
	}
	if cloned.LongContextInputThreshold <= 0 {
		cloned.LongContextInputThreshold = openAIGPT54LongContextInputThreshold
	}
	if cloned.LongContextInputMultiplier <= 0 {
		cloned.LongContextInputMultiplier = openAIGPT54LongContextInputMultiplier
	}
	if cloned.LongContextOutputMultiplier <= 0 {
		cloned.LongContextOutputMultiplier = openAIGPT54LongContextOutputMultiplier
	}
	return &cloned
}

func (s *BillingService) shouldApplySessionLongContextPricing(tokens UsageTokens, pricing *ModelPricing) bool {
	if pricing == nil || pricing.LongContextInputThreshold <= 0 {
		return false
	}
	if pricing.LongContextInputMultiplier <= 1 && pricing.LongContextOutputMultiplier <= 1 {
		return false
	}
	totalInputTokens := tokens.InputTokens + tokens.TextInputTokens + tokens.ImageInputTokens +
		tokens.CacheCreationTokens + tokens.CacheReadTokens
	return totalInputTokens > pricing.LongContextInputThreshold
}

func isOpenAIGPT54Model(model string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(model))
	// 仅当模型字符串实际属于 GPT-5/Codex 族时才做归一判定，避免 normalizeCodexModel
	// 的默认兜底把非 OpenAI 模型（claude-*、gemini-*、gpt-4o）误识别为 gpt-5.4。
	if !strings.Contains(trimmed, "gpt-5") && !strings.Contains(trimmed, "codex") {
		return false
	}
	normalized := normalizeCodexModel(trimmed)
	return normalized == "gpt-5.4" || normalized == "gpt-5.5"
}

func isOpenAIGPT55Model(model string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(model))
	if !strings.Contains(trimmed, "gpt-5") {
		return false
	}
	return normalizeCodexModel(trimmed) == "gpt-5.5"
}

// CalculateCostWithConfig 使用配置中的默认倍率计算费用
func (s *BillingService) CalculateCostWithConfig(model string, tokens UsageTokens) (*CostBreakdown, error) {
	multiplier := s.cfg.Default.RateMultiplier
	if multiplier <= 0 {
		multiplier = 1.0
	}
	return s.CalculateCost(model, tokens, multiplier)
}

// CalculateCostWithLongContext 计算费用，支持长上下文双倍计费
// threshold: 阈值（如 200000），超过此值的部分按 extraMultiplier 倍计费
// extraMultiplier: 超出部分的倍率（如 2.0 表示双倍）
//
// 示例：缓存 210k + 输入 10k = 220k，阈值 200k，倍率 2.0
// 拆分为：范围内 (200k, 0) + 范围外 (10k, 10k)
// 范围内正常计费，范围外 × 2 计费
func (s *BillingService) CalculateCostWithLongContext(model string, tokens UsageTokens, rateMultiplier float64, threshold int, extraMultiplier float64) (*CostBreakdown, error) {
	// 未启用长上下文计费，直接走正常计费
	if threshold <= 0 || extraMultiplier <= 1 {
		return s.CalculateCost(model, tokens, rateMultiplier)
	}

	// 计算总输入 token（缓存读取 + 新输入）
	total := tokens.CacheReadTokens + tokens.InputTokens
	if total <= threshold {
		return s.CalculateCost(model, tokens, rateMultiplier)
	}

	// 拆分成范围内和范围外
	var inRangeCacheTokens, inRangeInputTokens int
	var outRangeCacheTokens, outRangeInputTokens int

	if tokens.CacheReadTokens >= threshold {
		// 缓存已超过阈值：范围内只有缓存，范围外是超出的缓存+全部输入
		inRangeCacheTokens = threshold
		inRangeInputTokens = 0
		outRangeCacheTokens = tokens.CacheReadTokens - threshold
		outRangeInputTokens = tokens.InputTokens
	} else {
		// 缓存未超过阈值：范围内是全部缓存+部分输入，范围外是剩余输入
		inRangeCacheTokens = tokens.CacheReadTokens
		inRangeInputTokens = threshold - tokens.CacheReadTokens
		outRangeCacheTokens = 0
		outRangeInputTokens = tokens.InputTokens - inRangeInputTokens
	}

	// 范围内部分：正常计费
	inRangeTokens := UsageTokens{
		InputTokens:           inRangeInputTokens,
		OutputTokens:          tokens.OutputTokens, // 输出只算一次
		CacheCreationTokens:   tokens.CacheCreationTokens,
		CacheReadTokens:       inRangeCacheTokens,
		CacheCreation5mTokens: tokens.CacheCreation5mTokens,
		CacheCreation1hTokens: tokens.CacheCreation1hTokens,
		ImageOutputTokens:     tokens.ImageOutputTokens,
	}
	inRangeCost, err := s.CalculateCost(model, inRangeTokens, rateMultiplier)
	if err != nil {
		return nil, err
	}

	// 范围外部分：× extraMultiplier 计费
	outRangeTokens := UsageTokens{
		InputTokens:     outRangeInputTokens,
		CacheReadTokens: outRangeCacheTokens,
	}
	outRangeCost, err := s.CalculateCost(model, outRangeTokens, rateMultiplier*extraMultiplier)
	if err != nil {
		return inRangeCost, fmt.Errorf("out-range cost: %w", err)
	}

	// 合并成本
	return &CostBreakdown{
		InputCost:         inRangeCost.InputCost + outRangeCost.InputCost,
		ImageInputCost:    inRangeCost.ImageInputCost + outRangeCost.ImageInputCost,
		OutputCost:        inRangeCost.OutputCost,
		ImageOutputCost:   inRangeCost.ImageOutputCost,
		CacheCreationCost: inRangeCost.CacheCreationCost,
		CacheReadCost:     inRangeCost.CacheReadCost + outRangeCost.CacheReadCost,
		TotalCost:         inRangeCost.TotalCost + outRangeCost.TotalCost,
		ActualCost:        inRangeCost.ActualCost + outRangeCost.ActualCost,
	}, nil
}

// ListSupportedModels 列出所有支持的模型（现在总是返回true，因为有模糊匹配）
func (s *BillingService) ListSupportedModels() []string {
	models := make([]string, 0)
	// 返回回退价格支持的模型系列
	for model := range s.fallbackPrices {
		models = append(models, model)
	}
	return models
}

// IsModelSupported 检查模型是否支持（现在总是返回true，因为有模糊匹配回退）
func (s *BillingService) IsModelSupported(model string) bool {
	// 所有Claude模型都有回退价格支持
	modelLower := strings.ToLower(model)
	return strings.Contains(modelLower, "claude") ||
		strings.Contains(modelLower, "opus") ||
		strings.Contains(modelLower, "sonnet") ||
		strings.Contains(modelLower, "haiku")
}

// GetEstimatedCost 估算费用（用于前端展示）
func (s *BillingService) GetEstimatedCost(model string, estimatedInputTokens, estimatedOutputTokens int) (float64, error) {
	tokens := UsageTokens{
		InputTokens:  estimatedInputTokens,
		OutputTokens: estimatedOutputTokens,
	}

	breakdown, err := s.CalculateCostWithConfig(model, tokens)
	if err != nil {
		return 0, err
	}

	return breakdown.ActualCost, nil
}

// GetPricingServiceStatus 获取价格服务状态
func (s *BillingService) GetPricingServiceStatus() map[string]any {
	if s.pricingService != nil {
		return s.pricingService.GetStatus()
	}
	return map[string]any{
		"model_count":  len(s.fallbackPrices),
		"last_updated": "using fallback",
		"local_hash":   "N/A",
	}
}

// ForceUpdatePricing 强制更新价格数据
func (s *BillingService) ForceUpdatePricing() error {
	if s.pricingService != nil {
		return s.pricingService.ForceUpdate()
	}
	return fmt.Errorf("pricing service not initialized")
}

// ImagePriceConfig 图片计费配置
type ImagePriceConfig struct {
	Price1K *float64 // 1K 尺寸价格（nil 表示使用默认值）
	Price2K *float64 // 2K 尺寸价格（nil 表示使用默认值）
	Price4K *float64 // 4K 尺寸价格（nil 表示使用默认值）
}

// VideoPriceConfig 保存 Grok 视频每秒价格（USD/s）。
type VideoPriceConfig struct {
	Price480P  *float64
	Price720P  *float64
	Price1080P *float64
}

const (
	defaultGrokImagineVideoPrice480P    = 0.05
	defaultGrokImagineVideoPrice720P    = 0.07
	defaultGrokImagineVideo15Price480P  = 0.08
	defaultGrokImagineVideo15Price720P  = 0.14
	defaultGrokImagineVideo15Price1080P = 0.25
	defaultWebSearchPricePerCall        = 0.01
)

// CalculateWebSearchCost 计算 Codex alpha/search 的按次费用。
// nil 使用默认价 0.01 USD/次，0 表示免费。
func (s *BillingService) CalculateWebSearchCost(callCount int, groupPrice *float64, rateMultiplier float64) *CostBreakdown {
	if callCount <= 0 {
		return &CostBreakdown{}
	}
	unitPrice := defaultWebSearchPricePerCall
	if groupPrice != nil && *groupPrice >= 0 {
		unitPrice = *groupPrice
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	totalCost := unitPrice * float64(callCount)
	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  totalCost * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

// CalculateImageCost 计算图片生成费用
// model: 请求的模型名称（用于获取 LiteLLM 默认价格）
// imageSize: 图片尺寸 "1K", "2K", "4K"
// imageCount: 生成的图片数量
// groupConfig: 分组配置的价格（可能为 nil，表示使用默认值）
// rateMultiplier: 费率倍数
func (s *BillingService) CalculateImageCost(model string, imageSize string, imageCount int, groupConfig *ImagePriceConfig, rateMultiplier float64) *CostBreakdown {
	if imageCount <= 0 {
		return &CostBreakdown{}
	}

	// 获取单价
	unitPrice := s.getImageUnitPrice(model, imageSize, groupConfig)

	// 计算总费用
	totalCost := unitPrice * float64(imageCount)

	// 应用倍率（保存时强制 > 0；负数按 0 处理避免按 1x 误扣）
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	actualCost := totalCost * rateMultiplier

	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  actualCost,
		BillingMode: string(BillingModeImage),
	}
}

// CalculateVideoCost 计算 Grok 视频费用：每秒价 × 时长 × 数量 × 倍率。
func (s *BillingService) CalculateVideoCost(model, resolution string, videoCount, durationSeconds int, groupConfig *VideoPriceConfig, rateMultiplier float64) *CostBreakdown {
	if videoCount <= 0 {
		return &CostBreakdown{}
	}
	resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
	durationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds)
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	perSecondPrice := s.getVideoUnitPrice(model, resolution, groupConfig)
	totalCost := perSecondPrice * float64(durationSeconds) * float64(videoCount)
	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  totalCost * rateMultiplier,
		BillingMode: string(BillingModeVideo),
	}
}

// getImageUnitPrice 获取图片单价
func (s *BillingService) getImageUnitPrice(model string, imageSize string, groupConfig *ImagePriceConfig) float64 {
	// 优先使用分组配置的价格
	if groupConfig != nil {
		switch imageSize {
		case "1K":
			if groupConfig.Price1K != nil {
				return *groupConfig.Price1K
			}
		case "2K":
			if groupConfig.Price2K != nil {
				return *groupConfig.Price2K
			}
		case "4K":
			if groupConfig.Price4K != nil {
				return *groupConfig.Price4K
			}
		}
	}

	// 回退到 LiteLLM 默认价格
	return s.getDefaultImagePrice(model, imageSize)
}

func (s *BillingService) getVideoUnitPrice(model, resolution string, groupConfig *VideoPriceConfig) float64 {
	if groupConfig != nil {
		switch resolution {
		case VideoBillingResolution480P:
			if groupConfig.Price480P != nil {
				return *groupConfig.Price480P
			}
		case VideoBillingResolution720P:
			if groupConfig.Price720P != nil {
				return *groupConfig.Price720P
			}
		case VideoBillingResolution1080P:
			if groupConfig.Price1080P != nil {
				return *groupConfig.Price1080P
			}
		}
	}
	return s.getDefaultVideoPrice(model, resolution)
}

// getDefaultImagePrice 获取 LiteLLM 默认图片价格
func (s *BillingService) getDefaultImagePrice(model string, imageSize string) float64 {
	basePrice := 0.0

	// 从 PricingService 获取 output_cost_per_image
	if s.pricingService != nil {
		pricing := s.pricingService.GetModelPricing(model)
		if pricing != nil && pricing.OutputCostPerImage > 0 {
			basePrice = pricing.OutputCostPerImage
		}
	}

	// 如果没有找到价格，使用硬编码默认值（$0.134，来自 gemini-3-pro-image-preview）
	if basePrice <= 0 {
		basePrice = 0.134
	}

	// 2K 尺寸 1.5 倍，4K 尺寸翻倍
	if imageSize == "2K" {
		return basePrice * 1.5
	}
	if imageSize == "4K" {
		return basePrice * 2
	}

	return basePrice
}

func (s *BillingService) getDefaultVideoPrice(model, resolution string) float64 {
	model = strings.ToLower(strings.TrimSpace(model))
	resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
	switch {
	case strings.HasPrefix(model, "grok-imagine-video-1.5"):
		switch resolution {
		case VideoBillingResolution720P:
			return defaultGrokImagineVideo15Price720P
		case VideoBillingResolution1080P:
			return defaultGrokImagineVideo15Price1080P
		default:
			return defaultGrokImagineVideo15Price480P
		}
	case strings.HasPrefix(model, "grok-imagine-video"):
		if resolution == VideoBillingResolution480P {
			return defaultGrokImagineVideoPrice480P
		}
		return defaultGrokImagineVideoPrice720P
	default:
		// LiteLLM does not expose output-video pricing; retain the historical media fallback.
		return s.getDefaultImagePrice(model, "2K")
	}
}
