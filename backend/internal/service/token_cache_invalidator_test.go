//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type geminiTokenCacheStub struct {
	deletedKeys []string
	deleteErr   error
}

func (s *geminiTokenCacheStub) GetAccessToken(ctx context.Context, cacheKey string) (string, error) {
	return "", nil
}

func (s *geminiTokenCacheStub) SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error {
	return nil
}

func (s *geminiTokenCacheStub) DeleteAccessToken(ctx context.Context, cacheKey string) error {
	s.deletedKeys = append(s.deletedKeys, cacheKey)
	return s.deleteErr
}

func (s *geminiTokenCacheStub) AcquireRefreshLock(ctx context.Context, cacheKey string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (s *geminiTokenCacheStub) ReleaseRefreshLock(ctx context.Context, cacheKey string) error {
	return nil
}

func TestCompositeTokenCacheInvalidator_Gemini(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	account := &Account{
		ID:       10,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"project_id": "project-x",
		},
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"gemini:project-x"}, cache.deletedKeys)
}

func TestCompositeTokenCacheInvalidator_Antigravity(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	account := &Account{
		ID:       99,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"project_id": "ag-project",
		},
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"ag:ag-project"}, cache.deletedKeys)
}

func TestCompositeTokenCacheInvalidator_OpenAI(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	account := &Account{
		ID:       500,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "openai-token",
		},
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"openai:account:500"}, cache.deletedKeys)
}

func TestCompositeTokenCacheInvalidator_Claude(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	account := &Account{
		ID:       600,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "claude-token",
		},
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"claude:account:600"}, cache.deletedKeys)
}

func TestCompositeTokenCacheInvalidator_SkipNonOAuth(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)

	tests := []struct {
		name    string
		account *Account
	}{
		{
			name: "gemini_api_key",
			account: &Account{
				ID:       1,
				Platform: PlatformGemini,
				Type:     AccountTypeAPIKey,
			},
		},
		{
			name: "openai_api_key",
			account: &Account{
				ID:       2,
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
			},
		},
		{
			name: "claude_api_key",
			account: &Account{
				ID:       3,
				Platform: PlatformAnthropic,
				Type:     AccountTypeAPIKey,
			},
		},
		{
			name: "claude_setup_token",
			account: &Account{
				ID:       4,
				Platform: PlatformAnthropic,
				Type:     AccountTypeSetupToken,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache.deletedKeys = nil
			err := invalidator.InvalidateToken(context.Background(), tt.account)
			require.NoError(t, err)
			require.Empty(t, cache.deletedKeys)
		})
	}
}

func TestCompositeTokenCacheInvalidator_SkipUnsupportedPlatform(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	account := &Account{
		ID:       100,
		Platform: "unknown-platform",
		Type:     AccountTypeOAuth,
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, cache.deletedKeys)
}

func TestCompositeTokenCacheInvalidator_NilCache(t *testing.T) {
	invalidator := NewCompositeTokenCacheInvalidator(nil)
	account := &Account{
		ID:       2,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
}

func TestCompositeTokenCacheInvalidator_NilAccount(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)

	err := invalidator.InvalidateToken(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, cache.deletedKeys)
}

func TestCompositeTokenCacheInvalidator_NilInvalidator(t *testing.T) {
	var invalidator *CompositeTokenCacheInvalidator
	account := &Account{
		ID:       5,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}

	err := invalidator.InvalidateToken(context.Background(), account)
	require.NoError(t, err)
}

func TestCompositeTokenCacheInvalidator_DeleteError(t *testing.T) {
	expectedErr := errors.New("redis connection failed")
	cache := &geminiTokenCacheStub{deleteErr: expectedErr}
	invalidator := NewCompositeTokenCacheInvalidator(cache)

	tests := []struct {
		name    string
		account *Account
	}{
		{
			name: "openai_delete_error",
			account: &Account{
				ID:       700,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
			},
		},
		{
			name: "claude_delete_error",
			account: &Account{
				ID:       800,
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := invalidator.InvalidateToken(context.Background(), tt.account)
			require.Error(t, err)
			require.Equal(t, expectedErr, err)
		})
	}
}

func TestCompositeTokenCacheInvalidator_AllPlatformsIntegration(t *testing.T) {
	// 测试所有平台的缓存键生成和删除
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)

	accounts := []*Account{
		{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": "gemini-proj"}},
		{ID: 2, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": "ag-proj"}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
	}

	expectedKeys := []string{
		"gemini:gemini-proj",
		"ag:ag-proj",
		"openai:account:3",
		"claude:account:4",
	}

	for _, acc := range accounts {
		err := invalidator.InvalidateToken(context.Background(), acc)
		require.NoError(t, err)
	}

	require.Equal(t, expectedKeys, cache.deletedKeys)
}
