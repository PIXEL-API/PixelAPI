package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type tokenRefreshLeaseAccountRepository struct {
	AccountRepository
	accounts  []Account
	listCalls atomic.Int64
}

func (r *tokenRefreshLeaseAccountRepository) ListActive(context.Context) ([]Account, error) {
	r.listCalls.Add(1)
	return r.accounts, nil
}

type tokenRefreshLeaseRefresher struct {
	refreshCalls atomic.Int64
}

func (r *tokenRefreshLeaseRefresher) CanRefresh(*Account) bool {
	return true
}

func (r *tokenRefreshLeaseRefresher) NeedsRefresh(*Account, time.Duration) bool {
	return true
}

func (r *tokenRefreshLeaseRefresher) Refresh(context.Context, *Account) (map[string]any, error) {
	r.refreshCalls.Add(1)
	return map[string]any{"access_token": "refreshed"}, nil
}

type contentModerationCleanupRepositoryStub struct {
	ContentModerationRepository
	cleanupCalls atomic.Int64
}

func (r *contentModerationCleanupRepositoryStub) CleanupExpiredLogs(
	context.Context,
	time.Time,
	time.Time,
) (*ContentModerationCleanupResult, error) {
	r.cleanupCalls.Add(1)
	return &ContentModerationCleanupResult{FinishedAt: time.Now()}, nil
}

type contentModerationCleanupSettingRepositoryStub struct {
	SettingRepository
}

func (*contentModerationCleanupSettingRepositoryStub) GetValue(context.Context, string) (string, error) {
	return "", ErrSettingNotFound
}

func TestTokenRefreshCycleRequiresClusterLeaseBeforeListingCandidates(t *testing.T) {
	accountRepo := &tokenRefreshLeaseAccountRepository{}
	clusterRepo := &clusterAdminRepositoryStub{}
	cfg := testClusterRuntimeConfig()
	tokenCfg := config.TokenRefreshConfig{
		RefreshBeforeExpiryHours: 1,
		MaxRetries:               1,
	}
	svc := &TokenRefreshService{
		accountRepo:  accountRepo,
		cfg:          &tokenCfg,
		taskExecutor: NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg)),
	}

	svc.processRefresh(context.Background())

	require.Equal(t, tokenRefreshCycleTaskName, clusterRepo.acquiredTaskName)
	require.Zero(t, accountRepo.listCalls.Load())
}

func TestTokenRefreshCycleChecksLeaseBeforeExternalRefresh(t *testing.T) {
	accountRepo := &tokenRefreshLeaseAccountRepository{
		accounts: []Account{{ID: 1, Name: "oauth-account"}},
	}
	refresher := &tokenRefreshLeaseRefresher{}
	clusterRepo := &clusterAdminRepositoryStub{
		acquiredLease: &ClusterTaskLease{FencingToken: 7},
		leaseAcquired: true,
		leaseRenewed:  false,
	}
	cfg := testClusterRuntimeConfig()
	tokenCfg := config.TokenRefreshConfig{
		RefreshBeforeExpiryHours: 1,
		MaxRetries:               1,
	}
	svc := &TokenRefreshService{
		accountRepo:  accountRepo,
		refreshers:   []TokenRefresher{refresher},
		cfg:          &tokenCfg,
		taskExecutor: NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg)),
	}

	svc.processRefresh(context.Background())

	require.Equal(t, int64(1), accountRepo.listCalls.Load())
	require.Zero(t, refresher.refreshCalls.Load())
	require.False(t, clusterRepo.leaseReleased)
}

func TestContentModerationCleanupRequiresClusterLeaseBeforeDelete(t *testing.T) {
	repo := &contentModerationCleanupRepositoryStub{}
	clusterRepo := &clusterAdminRepositoryStub{}
	cfg := testClusterRuntimeConfig()
	svc := &ContentModerationService{
		repo:         repo,
		settingRepo:  &contentModerationCleanupSettingRepositoryStub{},
		taskExecutor: NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg)),
	}

	svc.runCleanupOnce()

	require.Equal(t, contentModerationCleanupTaskName, clusterRepo.acquiredTaskName)
	require.Zero(t, repo.cleanupCalls.Load())
}

func TestContentModerationCleanupWorkerStopsIdempotently(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc := &ContentModerationService{cancelCleanup: cancel}
	svc.cleanupWG.Add(1)
	go svc.cleanupWorker(ctx)

	stopped := make(chan struct{})
	go func() {
		svc.StopCleanupWorker()
		svc.StopCleanupWorker()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("content moderation cleanup worker did not stop")
	}
}

func TestDashboardStartupRecomputeRequiresClusterLease(t *testing.T) {
	repo := &dashboardAggregationRepoTestStub{}
	clusterRepo := &clusterAdminRepositoryStub{}
	cfg := testClusterRuntimeConfig()
	svc := &DashboardAggregationService{
		repo: repo,
		cfg: config.DashboardAggregationConfig{
			RecomputeDays: 1,
		},
		taskExecutor: NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg)),
	}

	svc.recomputeRecentDays()

	require.Equal(t, dashboardStartupRecomputeTaskName, clusterRepo.acquiredTaskName)
	require.Zero(t, repo.recomputeCalls)
	require.Zero(t, repo.aggregateCalls)
}
