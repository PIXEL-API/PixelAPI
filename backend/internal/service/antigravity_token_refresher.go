package service

import (
	"context"
	"fmt"
	"time"
)

const (
	// antigravityRefreshWindow Antigravity token 提前刷新窗口：15分钟
	// Google OAuth token 有效期55分钟，提前15分钟刷新
	antigravityRefreshWindow = 15 * time.Minute
)

// AntigravityTokenRefresher 实现 TokenRefresher 接口
type AntigravityTokenRefresher struct {
	antigravityOAuthService *AntigravityOAuthService
}

func NewAntigravityTokenRefresher(antigravityOAuthService *AntigravityOAuthService) *AntigravityTokenRefresher {
	return &AntigravityTokenRefresher{
		antigravityOAuthService: antigravityOAuthService,
	}
}

// CanRefresh 检查是否可以刷新此账户
func (r *AntigravityTokenRefresher) CanRefresh(account *Account) bool {
	return account.Platform == PlatformAntigravity && account.Type == AccountTypeOAuth
}

// NeedsRefresh 检查账户是否需要刷新
// Antigravity 使用固定的15分钟刷新窗口，忽略全局配置
func (r *AntigravityTokenRefresher) NeedsRefresh(account *Account, _ time.Duration) bool {
	if !r.CanRefresh(account) {
		return false
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return false
	}
	timeUntilExpiry := time.Until(*expiresAt)
	needsRefresh := timeUntilExpiry < antigravityRefreshWindow
	if needsRefresh {
		fmt.Printf("[AntigravityTokenRefresher] Account %d needs refresh: expires_at=%s, time_until_expiry=%v, window=%v\n",
			account.ID, expiresAt.Format("2006-01-02 15:04:05"), timeUntilExpiry, antigravityRefreshWindow)
	}
	return needsRefresh
}

// Refresh 执行 token 刷新
func (r *AntigravityTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.antigravityOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}

	newCredentials := r.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
	for k, v := range account.Credentials {
		if _, exists := newCredentials[k]; !exists {
			newCredentials[k] = v
		}
	}

	// 如果 project_id 获取失败，返回 credentials 但同时返回错误让账户被标记
	if tokenInfo.ProjectIDMissing {
		return newCredentials, fmt.Errorf("missing_project_id: 账户缺少project id，可能无法使用Antigravity")
	}

	return newCredentials, nil
}
