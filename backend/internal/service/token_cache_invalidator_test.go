//go:build unit

package service

import (
	"context"
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
	require.Equal(t, []string{"project-x"}, cache.deletedKeys)
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

func TestCompositeTokenCacheInvalidator_SkipNonOAuth(t *testing.T) {
	cache := &geminiTokenCacheStub{}
	invalidator := NewCompositeTokenCacheInvalidator(cache)
	account := &Account{
		ID:       1,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
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
