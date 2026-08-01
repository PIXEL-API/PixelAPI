package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 回归：系统写入的值不得把账号所有者锁在门外。
//
// 历史缺陷：GrokOAuthService.BuildAccountCredentials 无条件写入 base_url，令牌刷新
// 把它落库，此后 UpdateOwned 对库内完整凭证重跑安全扫描，所有者的每一次写操作
// （切调度/启停/改名/改并发/批量）都返回 400 OWNED_ACCOUNT_CREDENTIALS_NOT_ALLOWED。

func grokCredentialsFromRefresh() map[string]any {
	stored := map[string]any{
		"access_token":  "old-access-token",
		"refresh_token": "refresh-token",
		"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"email":         "user@example.com",
	}
	fresh := (&GrokOAuthService{}).BuildAccountCredentials(&GrokTokenInfo{
		AccessToken:  "new-access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		TokenType:    "Bearer",
	})
	return MergeCredentials(stored, fresh)
}

func TestGrokRefreshCredentialsCarryNoBaseURL(t *testing.T) {
	credentials := grokCredentialsFromRefresh()

	require.NotContains(t, credentials, "base_url",
		"刷新写回的凭证不能带默认出站地址；地址由 Account.GetGrokBaseURL() 在请求时解析")
	require.NoError(t, validateOwnedAccountSourceForPlatform(PlatformGrok, AccountTypeOAuth, credentials, nil))

	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: credentials}
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1", account.GetGrokBaseURL(),
		"删掉持久化字段后出站地址必须仍然回退到 CLI 默认值")
}

func newOwnedAccountForUpdate(t *testing.T, ownerUserID int64, platform string, credentials, extra map[string]any) (*ownedAgentIdentityRepoStub, *AccountService) {
	t.Helper()
	repo := newOwnedAgentIdentityRepoStub()
	repo.accounts[1] = &Account{
		ID:           1,
		Name:         "My account",
		OwnerUserID:  &ownerUserID,
		Platform:     platform,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelUnknown,
		Credentials:  credentials,
		Extra:        extra,
		ShareMode:    AccountShareModePrivate,
		ShareStatus:  AccountShareStatusApproved,
		Concurrency:  3,
		Priority:     1,
		Status:       StatusActive,
		Schedulable:  true,
		UpdatedAt:    time.Now(),
	}
	svc, _ := newOwnedAgentIdentityService(repo)
	return repo, svc
}

func TestUpdateOwnedIgnoresStoredStateTheOwnerDidNotTouch(t *testing.T) {
	ownerUserID := int64(101)
	schedulable := false

	poisoned := []struct {
		name        string
		platform    string
		credentials map[string]any
		extra       map[string]any
	}{
		{
			name:     "legacy grok base_url written by the refresher",
			platform: PlatformGrok,
			credentials: map[string]any{
				"access_token":  "token",
				"refresh_token": "refresh-token",
				"base_url":      "https://cli-chat-proxy.grok.com/v1",
			},
			extra: map[string]any{},
		},
		{
			name:        "compact probe error text containing an upstream URL",
			platform:    PlatformOpenAI,
			credentials: map[string]any{"access_token": "token"},
			extra: map[string]any{
				"openai_compact_last_error": `Post "https://chatgpt.com/backend-api/codex/responses/compact": i/o timeout`,
			},
		},
		{
			name:        "model rate limit scope named like a forbidden credential key",
			platform:    PlatformOpenAI,
			credentials: map[string]any{"access_token": "token"},
			extra: map[string]any{
				"model_rate_limits": map[string]any{
					"base-url": map[string]any{"rate_limited_at": "2026-08-01T00:00:00Z"},
				},
			},
		},
		{
			name:        "admin-written custom relay配置",
			platform:    PlatformAnthropic,
			credentials: map[string]any{"access_token": "token"},
			extra: map[string]any{
				"custom_base_url_enabled": true,
				"custom_base_url":         "https://relay.example.com",
			},
		},
		{
			name:     "admin-written header overrides",
			platform: PlatformAnthropic,
			credentials: map[string]any{
				"access_token":            "token",
				"header_override_enabled": false,
			},
			extra: map[string]any{},
		},
	}

	for _, test := range poisoned {
		t.Run(test.name, func(t *testing.T) {
			repo, svc := newOwnedAccountForUpdate(t, ownerUserID, test.platform, test.credentials, test.extra)

			account, err := svc.UpdateOwned(context.Background(), ownerUserID, 1, UpdateAccountRequest{Schedulable: &schedulable})
			require.NoError(t, err, "切调度不该因为库里存着系统/管理员写的值而失败")
			require.False(t, account.Schedulable)

			name := "renamed"
			_, err = svc.UpdateOwned(context.Background(), ownerUserID, 1, UpdateAccountRequest{Name: &name})
			require.NoError(t, err)

			// 系统写入的值必须原样留在库里，不能被"顺手清理"掉。
			for key, want := range test.credentials {
				require.Equal(t, want, repo.accounts[1].Credentials[key])
			}
		})
	}
}

func TestUpdateOwnedStillRejectsCredentialsTheOwnerIntroduces(t *testing.T) {
	ownerUserID := int64(101)
	// 用 OpenAI 账号：Anthropic/Gemini/Antigravity/Grok 携带凭据更新时会先撞上
	// 强制代理校验，测不到这里想覆盖的凭证扫描分支。
	_, svc := newOwnedAccountForUpdate(t, ownerUserID, PlatformOpenAI, map[string]any{
		"access_token": "token",
	}, map[string]any{})

	t.Run("new custom upstream", func(t *testing.T) {
		credentials := map[string]any{
			"access_token": "token",
			"base_url":     "https://evil.example.com/v1",
		}
		_, err := svc.UpdateOwned(context.Background(), ownerUserID, 1, UpdateAccountRequest{Credentials: &credentials})
		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
	})

	t.Run("changing a stored value to a different upstream", func(t *testing.T) {
		_, svc := newOwnedAccountForUpdate(t, ownerUserID, PlatformOpenAI, map[string]any{
			"access_token": "token",
			"base_url":     "https://cli-chat-proxy.grok.com/v1",
		}, map[string]any{})
		credentials := map[string]any{
			"access_token": "token",
			"base_url":     "https://evil.example.com/v1",
		}
		_, err := svc.UpdateOwned(context.Background(), ownerUserID, 1, UpdateAccountRequest{Credentials: &credentials})
		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed,
			"改动一个已存在的违规字段仍然属于用户提交，必须继续拒绝")
	})

	t.Run("new api key in extra", func(t *testing.T) {
		extra := map[string]any{"api_key": "sk-proj-should-be-rejected"}
		_, err := svc.UpdateOwned(context.Background(), ownerUserID, 1, UpdateAccountRequest{Extra: &extra})
		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
	})

	t.Run("nested forbidden field in extra", func(t *testing.T) {
		extra := map[string]any{"metadata": []any{map[string]any{"proxy_url": "https://evil.example.com"}}}
		_, err := svc.UpdateOwned(context.Background(), ownerUserID, 1, UpdateAccountRequest{Extra: &extra})
		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
	})
}

func TestCreateAndGateStillScanTheWholeObject(t *testing.T) {
	// 新建/导入以及对外提供服务的准入闸口仍然全量扫描。
	err := validateOwnedAccountSourceForPlatform(PlatformGrok, AccountTypeOAuth, map[string]any{
		"access_token": "token",
		"base_url":     "https://evil.example.com/v1",
	}, nil)
	require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
}

func TestBulkUpdateOwnedKeepsHealthyAccountsWhenOneFails(t *testing.T) {
	ownerUserID := int64(101)
	repo, svc := newOwnedAccountForUpdate(t, ownerUserID, PlatformGrok, map[string]any{
		"access_token":  "token",
		"refresh_token": "refresh-token",
	}, map[string]any{})
	// 第二个账号缺少 access_token，会在自身校验上失败。
	repo.accounts[2] = &Account{
		ID:          2,
		Name:        "Broken account",
		OwnerUserID: &ownerUserID,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{},
		Extra:       map[string]any{},
		ShareMode:   AccountShareModePrivate,
		ShareStatus: AccountShareStatusApproved,
		Concurrency: 3,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		UpdatedAt:   time.Now(),
	}

	schedulable := false
	result, err := svc.BulkUpdateOwned(context.Background(), ownerUserID, &BulkUpdateOwnedAccountsInput{
		AccountIDs:  []int64{1, 2},
		Schedulable: &schedulable,
	})

	require.NoError(t, err, "单个账号的校验失败不该让整批中止")
	require.Equal(t, 1, result.Success)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, []int64{1}, result.SuccessIDs)
	require.Equal(t, []int64{2}, result.FailedIDs)
	require.Equal(t, []int64{1}, repo.bulkUpdateIDs, "健康账号必须真的进入批量写入")
}

func TestRedactCredentialUnsafeTextSurvivesTheOwnedScan(t *testing.T) {
	payloads := []string{
		`Post "https://chatgpt.com/backend-api/codex/responses/compact": dial tcp 1.2.3.4:443: i/o timeout`,
		`<html><body>Attention Required! <a href="https://www.cloudflare.com/5xx-error">More info</a></body></html>`,
		`Missing bearer or basic authentication in header`,
		`{"error":{"message":"invalid api_key supplied","type":"invalid_request_error"}}`,
		`upstream_url=https://api.openai.com/v1 cookie: sid=abc`,
	}

	for _, payload := range payloads {
		redacted := redactCredentialUnsafeText(payload)
		_, blocked := disallowedCredentialStringReason("openai_compact_last_error", redacted, credentialSafetyOptions{})
		require.False(t, blocked, "清洗后的诊断文本必须能通过安全扫描: %q -> %q", payload, redacted)
	}

	require.Equal(t, "", redactCredentialUnsafeText(""))
	require.Equal(t, "plain failure", redactCredentialUnsafeText("plain failure"))
}

func TestChangedAccountMapSubset(t *testing.T) {
	base := map[string]any{
		"access_token": "old",
		"base_url":     "https://cli-chat-proxy.grok.com/v1",
		"nested":       map[string]any{"kept": "same", "changed": "before"},
	}
	next := map[string]any{
		"access_token": "new",
		"base_url":     "https://cli-chat-proxy.grok.com/v1",
		"nested":       map[string]any{"kept": "same", "changed": "after"},
		"added":        "value",
	}

	delta := changedAccountMapSubset(base, next)

	require.Equal(t, map[string]any{
		"access_token": "new",
		"nested":       map[string]any{"changed": "after"},
		"added":        "value",
	}, delta)
	require.Nil(t, changedAccountMapSubset(base, nil))
	require.Nil(t, changedAccountMapSubset(base, map[string]any{"access_token": "old"}))
}

func TestSanitizeOwnedAccountCredentialWriteDropsSystemIntroducedFields(t *testing.T) {
	ownerUserID := int64(101)
	owned := &Account{
		ID:          1,
		OwnerUserID: &ownerUserID,
		Platform:    PlatformGrok,
		Credentials: map[string]any{"access_token": "old"},
	}

	next := map[string]any{"access_token": "new", "base_url": "https://cli-chat-proxy.grok.com/v1"}
	require.Equal(t, map[string]any{"access_token": "new"}, sanitizeOwnedAccountCredentialWrite(owned, next))

	// 平台账号（无 owner）不受影响。
	platformAccount := &Account{ID: 2, Platform: PlatformGrok, Credentials: map[string]any{"access_token": "old"}}
	platformNext := map[string]any{"access_token": "new", "base_url": "https://relay.example.com"}
	require.Equal(t, platformNext, sanitizeOwnedAccountCredentialWrite(platformAccount, platformNext))
}

func TestModelRateLimitScopeRejectsForbiddenKeyNames(t *testing.T) {
	require.True(t, isSafeModelRateLimitScope("gpt-5.2-pro"))
	require.True(t, isSafeModelRateLimitScope("claude-opus-4-1-20250805"))
	require.False(t, isSafeModelRateLimitScope("base-url"))
	require.False(t, isSafeModelRateLimitScope("Base.Url"))
	require.False(t, isSafeModelRateLimitScope("api_key"))
	require.False(t, isSafeModelRateLimitScope("cookie"))
	require.False(t, isSafeModelRateLimitScope("  "))
}
