//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type rateLimitAccountRepoStub struct {
	mockAccountRepoForGemini
	setErrorCalls          int
	rateLimitCalls         []time.Time
	tempCalls              int
	updateCredentialsCalls int
	modelRateLimitCalls    []modelRateLimitCall
	lastCredentials        map[string]any
	lastErrorMsg           string
	lastTempReason         string
	lastTempUntil          time.Time
}

func (r *rateLimitAccountRepoStub) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

func (r *rateLimitAccountRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastTempUntil = until
	r.lastTempReason = reason
	return nil
}

func (r *rateLimitAccountRepoStub) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls = append(r.rateLimitCalls, resetAt)
	return nil
}

func (r *rateLimitAccountRepoStub) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	r.updateCredentialsCalls++
	r.lastCredentials = cloneCredentials(credentials)
	return nil
}

func (r *rateLimitAccountRepoStub) SetModelRateLimit(ctx context.Context, id int64, modelKey string, resetAt time.Time, reason ...string) error {
	reasonStr := ""
	if len(reason) > 0 {
		reasonStr = reason[0]
	}
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, modelRateLimitCall{accountID: id, modelKey: modelKey, resetAt: resetAt, reason: reasonStr})
	return nil
}

func (r *rateLimitAccountRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.mockAccountRepoForGemini.UpdateExtra(ctx, id, updates)
	return nil
}

type tokenCacheInvalidatorRecorder struct {
	accounts []*Account
	err      error
}

type openAI403CounterCacheStub struct {
	counts     []int64
	resetCalls []int64
	err        error
}

func (s *openAI403CounterCacheStub) IncrementOpenAI403Count(_ context.Context, _ int64, _ int) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if len(s.counts) == 0 {
		return 1, nil
	}
	count := s.counts[0]
	s.counts = s.counts[1:]
	return count, nil
}

func (s *openAI403CounterCacheStub) ResetOpenAI403Count(_ context.Context, accountID int64) error {
	s.resetCalls = append(s.resetCalls, accountID)
	return nil
}

func (r *tokenCacheInvalidatorRecorder) InvalidateToken(ctx context.Context, account *Account) error {
	r.accounts = append(r.accounts, account)
	return r.err
}

func TestRateLimitService_HandleUpstreamError_OAuth401SetsTempUnschedulable(t *testing.T) {
	t.Run("gemini", func(t *testing.T) {
		repo := &rateLimitAccountRepoStub{}
		invalidator := &tokenCacheInvalidatorRecorder{}
		service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		service.SetTokenCacheInvalidator(invalidator)
		account := &Account{
			ID:       100,
			Platform: PlatformGemini,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				// 有 refresh_token 才走冷却路径：没有它的账号刷新不了，
				// 冷却结束只会再 401 一次，改走 SetError（见 OAuth401NoRefreshToken）。
				"refresh_token":              "rt",
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       401,
						"keywords":         []any{"unauthorized"},
						"duration_minutes": 30,
						"description":      "custom rule",
					},
				},
			},
		}

		shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

		require.True(t, shouldDisable)
		require.Equal(t, 0, repo.setErrorCalls)
		require.Equal(t, 1, repo.tempCalls)
		require.Len(t, invalidator.accounts, 1)
	})

	t.Run("antigravity_401_uses_SetError", func(t *testing.T) {
		// Antigravity 401 由 applyErrorPolicy 的 temp_unschedulable_rules 控制，
		// HandleUpstreamError 中走 SetError 路径。
		repo := &rateLimitAccountRepoStub{}
		invalidator := &tokenCacheInvalidatorRecorder{}
		service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		service.SetTokenCacheInvalidator(invalidator)
		account := &Account{
			ID:       100,
			Platform: PlatformAntigravity,
			Type:     AccountTypeOAuth,
		}

		shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

		require.True(t, shouldDisable)
		require.Equal(t, 1, repo.setErrorCalls)
		require.Equal(t, 0, repo.tempCalls)
		require.Empty(t, invalidator.accounts)
	})
}

// TestRateLimitService_HandleUpstreamError_OAuth401InvalidatorError
// OpenAI OAuth 401 缓存失效出错时仍走 temp_unschedulable
func TestRateLimitService_HandleUpstreamError_OAuth401InvalidatorError(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{err: errors.New("boom")}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       101,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		// 有 refresh_token，账号可自愈 → 走冷却而非 SetError。
		Credentials: map[string]any{"refresh_token": "rt"},
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	// 401 分支不再回写 credentials，见下面的 OAuth401DoesNotRewriteCredentials。
	require.Equal(t, 0, repo.updateCredentialsCalls)
	require.Len(t, invalidator.accounts, 1)
}

func TestRateLimitService_HandleUpstreamError_NonOAuth401(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       102,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Empty(t, invalidator.accounts)
}

// TestRateLimitService_HandleUpstreamError_OAuth401DoesNotRewriteCredentials
// OAuth 401 分支绝不能回写 credentials JSONB。
//
// 该分支拿到的 account 是网关 SelectAccount 时刻的请求起始快照。原实现会往里塞
// expires_at 再经 persistAccountCredentials → UpdateCredentials 整列覆盖，
// 若期间另一 worker 已经轮换过 refresh_token，就会把 DB 里的新值回滚成旧值；
// 下一轮刷新拿旧 token 换到 invalid_grant，tryRecoverFromRefreshRace 重读 DB
// 发现 currentRT == usedRT 直接放弃，账号被误判永久失效并禁用。
//
// 强制刷新的语义由冷却结束后 token_provider 的 NeedsRefresh 承担，这里只保留
// 缓存失效 + 临时不可调度。
func TestRateLimitService_HandleUpstreamError_OAuth401DoesNotRewriteCredentials(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       103,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "token",
			"refresh_token": "fresh-rotated-token",
		},
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.updateCredentialsCalls)
	require.Nil(t, repo.lastCredentials)
	// 账号仍必须被挡在调度之外，等冷却结束后走带锁的正路刷新。
	require.Equal(t, 1, repo.tempCalls)
}

func TestRateLimitService_HandleUpstreamError_Anthropic7dOiOnlyMarksFableModel(t *testing.T) {
	reset5h := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	resetOI := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.3")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.5")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(resetOI.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-7d_oi-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d_oi-reset", strconv.FormatInt(resetOI.Unix(), 10))

	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       200,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, nil)

	require.False(t, shouldDisable)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, anthropicFableRateLimitKey, repo.modelRateLimitCalls[0].modelKey)
	require.True(t, repo.modelRateLimitCalls[0].resetAt.Equal(resetOI))
	require.Empty(t, repo.rateLimitCalls)
	require.Equal(t, 0, repo.tempCalls)
}

// D-1：没有 refresh_token 的 OAuth 账号 401 后必须直接置为 error，而不是进冷却队列。
//
// 冷却是为「等 token 刷新」留窗口，而刷新的前提就是有 refresh_token。缺了它，
// 冷却结束后账号会被重新选中 → 再次 401 → 再冷却，如此往复，对用户表现为
// 该账号持续吐 502。所有 OAuth 平台的 refresh_token 都存在同一个凭证键下，
// 故此判定对 openai/gemini/grok/claude 一致生效。
func TestRateLimitService_HandleUpstreamError_OAuth401NoRefreshTokenSetsError(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGemini} {
		t.Run(platform, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			invalidator := &tokenCacheInvalidatorRecorder{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			service.SetTokenCacheInvalidator(invalidator)
			account := &Account{
				ID:       104,
				Platform: platform,
				Type:     AccountTypeOAuth,
				// 只有 access_token，没有 refresh_token —— 永远无法自愈
				Credentials: map[string]any{"access_token": "token"},
			}

			shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

			require.True(t, shouldDisable)
			require.Equal(t, 1, repo.setErrorCalls, "无 refresh_token 必须落 error")
			require.Equal(t, 0, repo.tempCalls, "不得进冷却队列反复空转")
			// 既然不再调度它，也没必要失效 token 缓存
			require.Empty(t, invalidator.accounts)
		})
	}

	t.Run("blank refresh token counts as missing", func(t *testing.T) {
		repo := &rateLimitAccountRepoStub{}
		service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		account := &Account{
			ID:          105,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Credentials: map[string]any{"refresh_token": "   "},
		}

		require.True(t, service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized")))
		require.Equal(t, 1, repo.setErrorCalls)
		require.Equal(t, 0, repo.tempCalls)
	})
}

// OpenCode 将模型能力拒绝编码为 401，并在 Anthropic 兼容错误体中标注
// provider error.type=ModelError。该响应说明当前模型不可用，不代表 API key
// 已失效；因此必须只写 (account, model) 冷却，不能把整个账号置为 error。
func TestRateLimitService_HandleUpstreamErrorForModel_OpencodeModelErrorUsesModelRateLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model string
	}{
		{name: "Hy3", model: "Hy3"},
		{name: "DeepSeek-V4-Flash-Vision-Exp", model: "DeepSeek-V4-Flash-Vision-Exp"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := &rateLimitAccountRepoStub{}
			svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:          300,
				Platform:    PlatformOpencode,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
			}
			body := []byte(`{"type":"error","error":{"type":"ModelError","message":"Model ` + tt.model + ` is not supported"}}`)

			handled := svc.HandleUpstreamErrorForModel(
				context.Background(),
				account,
				tt.model,
				http.StatusUnauthorized,
				http.Header{},
				body,
			)

			require.True(t, handled, "模型级拒绝仍需触发当前请求转移到下一个账号")
			require.Zero(t, repo.setErrorCalls, "模型不支持不能把 OpenCode API key 账号置为永久 error")
			require.Empty(t, repo.tempCalls)
			require.Empty(t, repo.rateLimitCalls)
			require.Len(t, repo.modelRateLimitCalls, 1)
			call := repo.modelRateLimitCalls[0]
			require.Equal(t, account.ID, call.accountID)
			require.Equal(t, tt.model, call.modelKey)
			require.Equal(t, upstreamOpencodeModelNotSupportedReason, call.reason)
			require.True(t, call.resetAt.After(time.Now()), "模型级冷却必须有未来的 reset_at")
		})
	}
}

// 只有明确的 ModelError 才能走模型级冷却；真正的 OpenCode 凭证 401
// 仍必须保留账号级错误处理，避免把无效 API key 当成可重试的模型问题。
func TestRateLimitService_HandleUpstreamErrorForModel_OpencodeAuthentication401SetsError(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       302,
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
	}

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"Hy3",
		http.StatusUnauthorized,
		http.Header{},
		[]byte(`{"type":"error","error":{"type":"AuthenticationError","message":"Invalid API key"}}`),
	)

	require.True(t, handled)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

// 网关层必须把 OpenCode 的模型级 401 原样转交给
// RateLimitService.HandleUpstreamErrorForModel，不能在转发入口提前按通用
// 认证失败处理。该契约覆盖 /v1/responses、/v1/chat/completions、/v1/messages
// 共用的错误处理桥接点。
func TestOpenAIGatewayService_HandleOpenAIAccountUpstreamErrorForModel_ForwardsOpencodeModelError(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	rateLimitService := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimitService}
	account := &Account{
		ID:          301,
		Platform:    PlatformOpencode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	body := []byte(`{"type":"error","error":{"type":"ModelError","message":"Model Hy3 is not supported"}}`)

	handled := gateway.handleOpenAIAccountUpstreamErrorForModel(
		context.Background(),
		account,
		"Hy3",
		http.StatusUnauthorized,
		http.Header{},
		body,
	)

	require.True(t, handled)
	require.Zero(t, repo.setErrorCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, account.ID, repo.modelRateLimitCalls[0].accountID)
	require.Equal(t, "Hy3", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, upstreamOpencodeModelNotSupportedReason, repo.modelRateLimitCalls[0].reason)
}
