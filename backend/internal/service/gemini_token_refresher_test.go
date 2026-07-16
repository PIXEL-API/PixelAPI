//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGeminiTokenRefresher_NeedsRefresh 覆盖 Gemini OAuth 账号的刷新判定，
// 重点验证 expires_at 缺失场景（例如通过凭证导入创建的账号）：
// 有 refresh_token 时应主动刷新以补齐 expires_at，无 refresh_token 时应跳过。
func TestGeminiTokenRefresher_NeedsRefresh(t *testing.T) {
	refresher := &GeminiTokenRefresher{}
	refreshWindow := 30 * time.Minute

	tests := []struct {
		name        string
		platform    string
		accountType string
		credentials map[string]any
		wantRefresh bool
	}{
		{
			name:        "expires_at 已过期 -> 需要刷新",
			platform:    PlatformGemini,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{
				"expires_at":    "1000", // 1970，已过期
				"refresh_token": "rt",
			},
			wantRefresh: true,
		},
		{
			name:        "expires_at 远未来 -> 不需要刷新",
			platform:    PlatformGemini,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{
				"expires_at":    "9999999999",
				"refresh_token": "rt",
			},
			wantRefresh: false,
		},
		{
			name:        "expires_at 缺失但有 refresh_token -> 需要刷新",
			platform:    PlatformGemini,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{
				"access_token":  "at",
				"refresh_token": "rt",
				// 无 expires_at（模拟凭证导入）
			},
			wantRefresh: true,
		},
		{
			name:        "expires_at 缺失且 refresh_token 为空白 -> 不刷新",
			platform:    PlatformGemini,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{
				"access_token":  "at",
				"refresh_token": "   ",
			},
			wantRefresh: false,
		},
		{
			name:        "expires_at 缺失且无 refresh_token -> 不刷新",
			platform:    PlatformGemini,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{
				"access_token": "at",
			},
			wantRefresh: false,
		},
		{
			name:        "非 Gemini 平台 -> 不刷新",
			platform:    PlatformAnthropic,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{
				"refresh_token": "rt",
			},
			wantRefresh: false,
		},
		{
			name:        "Gemini 但非 OAuth 类型 -> 不刷新",
			platform:    PlatformGemini,
			accountType: AccountTypeServiceAccount,
			credentials: map[string]any{
				"refresh_token": "rt",
			},
			wantRefresh: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform:    tt.platform,
				Type:        tt.accountType,
				Credentials: tt.credentials,
			}
			got := refresher.NeedsRefresh(account, refreshWindow)
			require.Equal(t, tt.wantRefresh, got)
		})
	}
}
