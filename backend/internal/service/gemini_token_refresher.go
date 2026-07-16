package service

import (
	"context"
	"strings"
	"time"
)

type GeminiTokenRefresher struct {
	geminiOAuthService *GeminiOAuthService
}

func NewGeminiTokenRefresher(geminiOAuthService *GeminiOAuthService) *GeminiTokenRefresher {
	return &GeminiTokenRefresher{geminiOAuthService: geminiOAuthService}
}

// CacheKey 返回用于分布式锁的缓存键
func (r *GeminiTokenRefresher) CacheKey(account *Account) string {
	return GeminiTokenCacheKey(account)
}

func (r *GeminiTokenRefresher) CanRefresh(account *Account) bool {
	return account.Platform == PlatformGemini && account.Type == AccountTypeOAuth
}

func (r *GeminiTokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	if !r.CanRefresh(account) {
		return false
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		// expires_at 缺失（例如通过凭证导入创建、只带 access_token/refresh_token 的账号）时，
		// 无法判断过期时间。若存在 refresh_token 则主动刷新一次，让后台补齐 expires_at，
		// 使账号回到正常刷新轨道；否则（无 refresh_token）无法刷新，跳过。
		// 该行为与运行时 GeminiTokenProvider.GetAccessToken 对 expires_at 缺失即刷新的判断保持一致。
		return strings.TrimSpace(account.GetCredential("refresh_token")) != ""
	}
	return time.Until(*expiresAt) < refreshWindow
}

func (r *GeminiTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.geminiOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}

	newCredentials := r.geminiOAuthService.BuildAccountCredentials(tokenInfo)
	newCredentials = MergeCredentials(account.Credentials, newCredentials)

	return newCredentials, nil
}
