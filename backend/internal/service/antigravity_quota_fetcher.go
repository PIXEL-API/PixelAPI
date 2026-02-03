package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// AntigravityQuotaFetcher 从 Antigravity API 获取额度
type AntigravityQuotaFetcher struct {
	proxyRepo ProxyRepository
}

// NewAntigravityQuotaFetcher 创建 AntigravityQuotaFetcher
func NewAntigravityQuotaFetcher(proxyRepo ProxyRepository) *AntigravityQuotaFetcher {
	return &AntigravityQuotaFetcher{proxyRepo: proxyRepo}
}

// CanFetch 检查是否可以获取此账户的额度
func (f *AntigravityQuotaFetcher) CanFetch(account *Account) bool {
	if account.Platform != PlatformAntigravity {
		return false
	}
	accessToken := account.GetCredential("access_token")
	return accessToken != ""
}

// FetchQuota 获取 Antigravity 账户额度信息
func (f *AntigravityQuotaFetcher) FetchQuota(ctx context.Context, account *Account, proxyURL string) (*QuotaResult, error) {
	accessToken := account.GetCredential("access_token")
	projectID := account.GetCredential("project_id")

	client := antigravity.NewClient(proxyURL)

	// 调用 API 获取配额
	modelsResp, modelsRaw, err := client.FetchAvailableModels(ctx, accessToken, projectID)
	if err != nil {
		return nil, err
	}

	// 转换为 UsageInfo
	usageInfo := f.buildUsageInfo(modelsResp)

	return &QuotaResult{
		UsageInfo: usageInfo,
		Raw:       modelsRaw,
	}, nil
}

// buildUsageInfo 将 API 响应转换为 UsageInfo
func (f *AntigravityQuotaFetcher) buildUsageInfo(modelsResp *antigravity.FetchAvailableModelsResponse) *UsageInfo {
	now := time.Now()
	info := &UsageInfo{
		UpdatedAt:        &now,
		AntigravityQuota: make(map[string]*AntigravityModelQuota),
	}

	// 遍历所有模型，填充 AntigravityQuota
	for modelName, modelInfo := range modelsResp.Models {
		if modelInfo.QuotaInfo == nil {
			continue
		}

		// remainingFraction 是剩余比例 (0.0-1.0)，转换为使用率百分比
		utilization := int((1.0 - modelInfo.QuotaInfo.RemainingFraction) * 100)

		info.AntigravityQuota[modelName] = &AntigravityModelQuota{
			Utilization: utilization,
			ResetTime:   modelInfo.QuotaInfo.ResetTime,
		}
	}

	// 同时设置 FiveHour 用于兼容展示（取主要模型）
	priorityModels := []string{"claude-sonnet-4-20250514", "claude-sonnet-4", "gemini-2.5-pro"}
	for _, modelName := range priorityModels {
		if modelInfo, ok := modelsResp.Models[modelName]; ok && modelInfo.QuotaInfo != nil {
			utilization := (1.0 - modelInfo.QuotaInfo.RemainingFraction) * 100
			progress := &UsageProgress{
				Utilization: utilization,
			}
			if modelInfo.QuotaInfo.ResetTime != "" {
				if resetTime, err := time.Parse(time.RFC3339, modelInfo.QuotaInfo.ResetTime); err == nil {
					progress.ResetsAt = &resetTime
					progress.RemainingSeconds = int(time.Until(resetTime).Seconds())
				}
			}
			info.FiveHour = progress
			break
		}
	}

	return info
}

// GetProxyURL 获取账户的代理 URL
func (f *AntigravityQuotaFetcher) GetProxyURL(ctx context.Context, account *Account) string {
	if account.ProxyID == nil || f.proxyRepo == nil {
		return ""
	}
	proxy, err := f.proxyRepo.GetByID(ctx, *account.ProxyID)
	if err != nil || proxy == nil {
		return ""
	}
	return proxy.URL()
}
