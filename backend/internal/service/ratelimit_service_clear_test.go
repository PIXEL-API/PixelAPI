//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type rateLimitClearRepoStub struct {
	mockAccountRepoForGemini
	clearRateLimitCalls       int
	clearAntigravityCalls     int
	clearModelRateLimitCalls  int
	clearTempUnschedCalls     int
	clearRateLimitErr         error
	clearAntigravityErr       error
	clearModelRateLimitErr    error
	clearTempUnschedulableErr error
}

func (r *rateLimitClearRepoStub) ClearRateLimit(ctx context.Context, id int64) error {
	r.clearRateLimitCalls++
	return r.clearRateLimitErr
}

func (r *rateLimitClearRepoStub) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	r.clearAntigravityCalls++
	return r.clearAntigravityErr
}

func (r *rateLimitClearRepoStub) ClearModelRateLimits(ctx context.Context, id int64) error {
	r.clearModelRateLimitCalls++
	return r.clearModelRateLimitErr
}

func (r *rateLimitClearRepoStub) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearTempUnschedCalls++
	return r.clearTempUnschedulableErr
}

type tempUnschedCacheRecorder struct {
	deletedIDs []int64
	deleteErr  error
}

func (c *tempUnschedCacheRecorder) SetTempUnsched(ctx context.Context, accountID int64, state *TempUnschedState) error {
	return nil
}

func (c *tempUnschedCacheRecorder) GetTempUnsched(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	return nil, nil
}

func (c *tempUnschedCacheRecorder) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	c.deletedIDs = append(c.deletedIDs, accountID)
	return c.deleteErr
}

func TestRateLimitService_ClearRateLimit_AlsoClearsTempUnschedulable(t *testing.T) {
	repo := &rateLimitClearRepoStub{}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 42)
	require.NoError(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Equal(t, []int64{42}, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearTempUnschedulableFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearTempUnschedulableErr: errors.New("clear temp unsched failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 7)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearRateLimitFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearRateLimitErr: errors.New("clear rate limit failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 11)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 0, repo.clearAntigravityCalls)
	require.Equal(t, 0, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearAntigravityFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearAntigravityErr: errors.New("clear antigravity failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 12)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 0, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearModelRateLimitsFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearModelRateLimitErr: errors.New("clear model rate limits failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 13)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_CacheDeleteFailedShouldNotFail(t *testing.T) {
	repo := &rateLimitClearRepoStub{}
	cache := &tempUnschedCacheRecorder{
		deleteErr: errors.New("cache delete failed"),
	}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 14)
	require.NoError(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Equal(t, []int64{14}, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_WithoutTempUnschedCache(t *testing.T) {
	repo := &rateLimitClearRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

	err := svc.ClearRateLimit(context.Background(), 15)
	require.NoError(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
}
