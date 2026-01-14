//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type rateLimitAccountRepoStub struct {
	mockAccountRepoForGemini
	tempCalls     int
	tempUntil     time.Time
	tempReason    string
	setErrorCalls int
}

func (r *rateLimitAccountRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.tempUntil = until
	r.tempReason = reason
	return nil
}

func (r *rateLimitAccountRepoStub) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.setErrorCalls++
	return nil
}

type tokenCacheInvalidatorRecorder struct {
	accounts []*Account
	err      error
}

func (r *tokenCacheInvalidatorRecorder) InvalidateToken(ctx context.Context, account *Account) error {
	r.accounts = append(r.accounts, account)
	return r.err
}

func TestRateLimitService_HandleUpstreamError_OAuth401TempUnschedulable(t *testing.T) {
	tests := []struct {
		name     string
		platform string
	}{
		{name: "gemini", platform: PlatformGemini},
		{name: "antigravity", platform: PlatformAntigravity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			invalidator := &tokenCacheInvalidatorRecorder{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			service.SetTokenCacheInvalidator(invalidator)
			account := &Account{
				ID:       100,
				Platform: tt.platform,
				Type:     AccountTypeOAuth,
			}

			start := time.Now()
			shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

			require.True(t, shouldDisable)
			require.Equal(t, 1, repo.tempCalls)
			require.Equal(t, 0, repo.setErrorCalls)
			require.Len(t, invalidator.accounts, 1)
			require.WithinDuration(t, start.Add(5*time.Minute), repo.tempUntil, 10*time.Second)
			require.NotEmpty(t, repo.tempReason)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_OAuth401InvalidatorError(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{err: errors.New("boom")}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       101,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Len(t, invalidator.accounts, 1)
}

func TestRateLimitService_HandleUpstreamError_OAuth401CustomRule(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       103,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
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

	start := time.Now()
	shouldDisable := service.HandleUpstreamError(context.Background(), account, 401, http.Header{}, []byte("unauthorized"))

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Len(t, invalidator.accounts, 1)
	require.WithinDuration(t, start.Add(30*time.Minute), repo.tempUntil, 10*time.Second)
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
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Empty(t, invalidator.accounts)
}

// TestRateLimitService_HandleOAuth401_NilAccount 测试 account 为 nil 的情况
func TestRateLimitService_HandleOAuth401_NilAccount(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

	result := service.handleOAuth401TempUnschedulable(context.Background(), nil, "error")

	require.False(t, result)
	require.Equal(t, 0, repo.tempCalls)
}

// TestRateLimitService_HandleOAuth401_NilInvalidator 测试 tokenCacheInvalidator 为 nil 的情况
func TestRateLimitService_HandleOAuth401_NilInvalidator(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	// 不设置 tokenCacheInvalidator
	account := &Account{
		ID:       200,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	result := service.handleOAuth401TempUnschedulable(context.Background(), account, "error")

	require.True(t, result)
	require.Equal(t, 1, repo.tempCalls)
}

// TestRateLimitService_HandleOAuth401_SetTempUnschedulableFailed 测试 SetTempUnschedulable 失败的情况
func TestRateLimitService_HandleOAuth401_SetTempUnschedulableFailed(t *testing.T) {
	repo := &rateLimitAccountRepoStubWithError{
		setTempErr: errors.New("db error"),
	}
	invalidator := &tokenCacheInvalidatorRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       201,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	result := service.handleOAuth401TempUnschedulable(context.Background(), account, "error")

	require.False(t, result) // 失败应返回 false
	require.Len(t, invalidator.accounts, 1) // 但 invalidator 仍然被调用
}

// rateLimitAccountRepoStubWithError 支持返回错误的 stub
type rateLimitAccountRepoStubWithError struct {
	mockAccountRepoForGemini
	setTempErr    error
	setErrorCalls int
}

func (r *rateLimitAccountRepoStubWithError) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	return r.setTempErr
}

func (r *rateLimitAccountRepoStubWithError) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.setErrorCalls++
	return nil
}

// TestRateLimitService_HandleOAuth401_WithTempUnschedCache 测试 tempUnschedCache 存在的情况
func TestRateLimitService_HandleOAuth401_WithTempUnschedCache(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{}
	tempCache := &tempUnschedCacheStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, tempCache)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       202,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	result := service.handleOAuth401TempUnschedulable(context.Background(), account, "error")

	require.True(t, result)
	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, 1, tempCache.setCalls)
}

// TestRateLimitService_HandleOAuth401_TempUnschedCacheError 测试 tempUnschedCache 设置失败的情况
func TestRateLimitService_HandleOAuth401_TempUnschedCacheError(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	invalidator := &tokenCacheInvalidatorRecorder{}
	tempCache := &tempUnschedCacheStub{setErr: errors.New("cache error")}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, tempCache)
	service.SetTokenCacheInvalidator(invalidator)
	account := &Account{
		ID:       203,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	result := service.handleOAuth401TempUnschedulable(context.Background(), account, "error")

	require.True(t, result) // 缓存错误不影响主流程
	require.Equal(t, 1, repo.tempCalls)
}

// tempUnschedCacheStub 用于测试的 TempUnschedCache stub
type tempUnschedCacheStub struct {
	setCalls int
	setErr   error
}

func (c *tempUnschedCacheStub) GetTempUnsched(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	return nil, nil
}

func (c *tempUnschedCacheStub) SetTempUnsched(ctx context.Context, accountID int64, state *TempUnschedState) error {
	c.setCalls++
	return c.setErr
}

func (c *tempUnschedCacheStub) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	return nil
}

// TestRateLimitService_OAuth401Cooldown 测试 oauth401Cooldown 函数
func TestRateLimitService_OAuth401Cooldown(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected time.Duration
	}{
		{
			name:     "default_when_config_zero",
			cfg:      &config.Config{RateLimit: config.RateLimitConfig{OAuth401CooldownMinutes: 0}},
			expected: 5 * time.Minute,
		},
		{
			name:     "custom_cooldown_10_minutes",
			cfg:      &config.Config{RateLimit: config.RateLimitConfig{OAuth401CooldownMinutes: 10}},
			expected: 10 * time.Minute,
		},
		{
			name:     "custom_cooldown_1_minute",
			cfg:      &config.Config{RateLimit: config.RateLimitConfig{OAuth401CooldownMinutes: 1}},
			expected: 1 * time.Minute,
		},
		{
			name:     "negative_value_uses_default",
			cfg:      &config.Config{RateLimit: config.RateLimitConfig{OAuth401CooldownMinutes: -5}},
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewRateLimitService(nil, nil, tt.cfg, nil, nil)
			result := service.oauth401Cooldown()
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestRateLimitService_OAuth401Cooldown_NilConfig 测试 cfg 为 nil 的情况
func TestRateLimitService_OAuth401Cooldown_NilConfig(t *testing.T) {
	service := &RateLimitService{cfg: nil}
	result := service.oauth401Cooldown()
	require.Equal(t, 5*time.Minute, result)
}

// TestRateLimitService_HandleOAuth401_WithCustomCooldown 测试自定义 cooldown 配置
func TestRateLimitService_HandleOAuth401_WithCustomCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{
			OAuth401CooldownMinutes: 15,
		},
	}
	service := NewRateLimitService(repo, nil, cfg, nil, nil)
	account := &Account{
		ID:       204,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
	}

	start := time.Now()
	result := service.handleOAuth401TempUnschedulable(context.Background(), account, "error")

	require.True(t, result)
	require.WithinDuration(t, start.Add(15*time.Minute), repo.tempUntil, 10*time.Second)
}

// TestRateLimitService_HandleOAuth401_EmptyUpstreamMsg 测试 upstreamMsg 为空的情况
func TestRateLimitService_HandleOAuth401_EmptyUpstreamMsg(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       205,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	result := service.handleOAuth401TempUnschedulable(context.Background(), account, "")

	require.True(t, result)
	require.Contains(t, repo.tempReason, "Authentication failed (401)")
}
