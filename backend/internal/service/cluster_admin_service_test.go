package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type clusterAdminRepositoryStub struct {
	instances            []ClusterInstance
	instance             *ClusterInstance
	operation            *ClusterOperation
	drainErr             error
	createErr            error
	listInstancesCalls   int
	createOperationInput CreateClusterOperationInput
	drainOperationInput  CreateClusterOperationInput
	drainMinimumReady    int
	drainStaleAfter      time.Duration
	drainOfflineAfter    time.Duration
	bumpCacheVersionFunc func(context.Context, string, string, string) (*ClusterCacheVersion, error)
	acquiredLease        *ClusterTaskLease
	leaseAcquired        bool
	leaseRenewed         bool
	leaseReleased        bool
	acquiredTaskName     string
	deletedDeploymentID  string
	deletedRetention     time.Duration
	deleteOfflineCalls   int
}

func (r *clusterAdminRepositoryStub) ClaimInstance(context.Context, ClusterInstanceHeartbeat, time.Duration) error {
	return nil
}

func (r *clusterAdminRepositoryStub) Heartbeat(context.Context, ClusterInstanceHeartbeat) (*ClusterInstance, error) {
	return r.instance, nil
}

func (r *clusterAdminRepositoryStub) SetInstanceDesiredState(context.Context, string, string, string) (*ClusterInstance, error) {
	return r.instance, nil
}

func (r *clusterAdminRepositoryStub) ListInstances(context.Context, string, time.Duration, time.Duration) ([]ClusterInstance, error) {
	r.listInstancesCalls++
	return r.instances, nil
}

func (r *clusterAdminRepositoryStub) GetInstance(context.Context, string, string, time.Duration, time.Duration) (*ClusterInstance, error) {
	if r.instance == nil {
		return nil, ErrClusterInstanceNotFound
	}
	return r.instance, nil
}

func (r *clusterAdminRepositoryStub) DeleteOfflineInstances(
	_ context.Context,
	deploymentID string,
	retention time.Duration,
) (int64, error) {
	r.deleteOfflineCalls++
	r.deletedDeploymentID = deploymentID
	r.deletedRetention = retention
	return 1, nil
}

func (r *clusterAdminRepositoryStub) AcquireTaskLease(
	_ context.Context,
	_, taskName, _, _ string,
	_ time.Duration,
) (*ClusterTaskLease, bool, error) {
	r.acquiredTaskName = taskName
	return r.acquiredLease, r.leaseAcquired, nil
}

func (r *clusterAdminRepositoryStub) RenewTaskLease(context.Context, string, string, string, string, int64, time.Duration) (bool, error) {
	return r.leaseRenewed, nil
}

func (r *clusterAdminRepositoryStub) ReleaseTaskLease(context.Context, string, string, string, string, int64, bool, string, time.Duration) (bool, error) {
	return r.leaseReleased, nil
}

func (r *clusterAdminRepositoryStub) ListTaskLeases(context.Context, string) ([]ClusterTaskLease, error) {
	return []ClusterTaskLease{}, nil
}

func (r *clusterAdminRepositoryStub) CreateOperation(_ context.Context, input CreateClusterOperationInput) (*ClusterOperation, bool, error) {
	r.createOperationInput = input
	if r.createErr != nil {
		return nil, false, r.createErr
	}
	if r.operation == nil {
		r.operation = &ClusterOperation{ID: uuid.NewString()}
	}
	return r.operation, true, nil
}

func (r *clusterAdminRepositoryStub) CreateDrainOperationSafely(
	_ context.Context,
	input CreateClusterOperationInput,
	minimumReadyAfterDrain int,
	staleAfter time.Duration,
	offlineAfter time.Duration,
) (*ClusterOperation, bool, error) {
	r.drainOperationInput = input
	r.drainMinimumReady = minimumReadyAfterDrain
	r.drainStaleAfter = staleAfter
	r.drainOfflineAfter = offlineAfter
	if r.drainErr != nil {
		return nil, false, r.drainErr
	}
	if r.operation == nil {
		r.operation = &ClusterOperation{ID: uuid.NewString()}
	}
	return r.operation, true, nil
}

func (r *clusterAdminRepositoryStub) GetOperation(context.Context, string, string) (*ClusterOperation, error) {
	return r.operation, nil
}

func (r *clusterAdminRepositoryStub) ClaimPendingOperations(context.Context, string, string, string, int, time.Duration) ([]ClusterOperation, error) {
	return []ClusterOperation{}, nil
}

func (r *clusterAdminRepositoryStub) CompleteOperation(context.Context, string, string, string, string, int64, bool, string, string) (bool, error) {
	return false, nil
}

func (r *clusterAdminRepositoryStub) ListOperations(context.Context, ClusterOperationFilter) ([]ClusterOperation, error) {
	return []ClusterOperation{}, nil
}

func (r *clusterAdminRepositoryStub) GetCacheVersion(context.Context, string, string) (*ClusterCacheVersion, error) {
	return nil, ErrClusterCacheVersionNotFound
}

func (r *clusterAdminRepositoryStub) ListCacheVersions(context.Context, string) ([]ClusterCacheVersion, error) {
	return []ClusterCacheVersion{}, nil
}

func (r *clusterAdminRepositoryStub) EnsureCacheVersions(context.Context, string, string) error {
	return nil
}

func (r *clusterAdminRepositoryStub) BumpCacheVersion(
	ctx context.Context,
	deploymentID, cacheKey, nodeID string,
) (*ClusterCacheVersion, error) {
	if r.bumpCacheVersionFunc != nil {
		return r.bumpCacheVersionFunc(ctx, deploymentID, cacheKey, nodeID)
	}
	return nil, nil
}

func testClusterAdminConfig() *config.Config {
	return &config.Config{
		Cluster: config.ClusterConfig{
			Enabled:             true,
			DeploymentID:        "pixel-prod",
			ExpectedNodes:       3,
			NodeTTLSeconds:      30,
			OfflineAfterSeconds: 300,
		},
		Database: config.DatabaseConfig{MaxOpenConns: 50},
		Redis:    config.RedisConfig{PoolSize: 128},
	}
}

func testClusterOperationRequest() ClusterNodeOperationRequest {
	return ClusterNodeOperationRequest{
		NodeID:         "pixel-app-01",
		Reason:         "planned maintenance",
		IdempotencyKey: uuid.NewString(),
		Actor:          ClusterOperationActor{UserID: 42},
	}
}

func TestClusterServiceDrainUsesAtomicRepositoryGuard(t *testing.T) {
	repository := &clusterAdminRepositoryStub{}
	service := NewClusterService(repository, testClusterAdminConfig())

	result, err := service.Drain(context.Background(), testClusterOperationRequest())

	require.NoError(t, err)
	require.Len(t, result.OperationIDs, 1)
	require.Equal(t, 0, repository.listInstancesCalls, "drain must not use a list-then-insert check")
	require.Equal(t, clusterAdminMinimumReadyAfterDrain, repository.drainMinimumReady)
	require.Equal(t, 30*time.Second, repository.drainStaleAfter)
	require.Equal(t, 300*time.Second, repository.drainOfflineAfter)
	require.Equal(t, ClusterOperationTypeDrain, repository.drainOperationInput.Type)
	require.Equal(t, "pixel-app-01", repository.drainOperationInput.TargetNodeID)
}

func TestClusterServiceDrainMapsUnsafeCapacityToConflict(t *testing.T) {
	repository := &clusterAdminRepositoryStub{drainErr: ErrClusterDrainCapacityUnsafe}
	service := NewClusterService(repository, testClusterAdminConfig())

	_, err := service.Drain(context.Background(), testClusterOperationRequest())

	require.Error(t, err)
	require.Equal(t, http.StatusConflict, infraerrors.Code(err))
	require.Equal(t, "CLUSTER_DRAIN_CAPACITY_UNSAFE", infraerrors.Reason(err))
}

func TestClusterServiceResumeRejectsUnhealthyDependencyBeforeAudit(t *testing.T) {
	repository := &clusterAdminRepositoryStub{
		instance: &ClusterInstance{
			NodeID:           "pixel-app-01",
			DerivedState:     ClusterObservedStateDraining,
			DatabaseHealthy:  true,
			RedisHealthy:     true,
			CacheHealthy:     false,
			MigrationHealthy: true,
		},
	}
	service := NewClusterService(repository, testClusterAdminConfig())

	_, err := service.Resume(context.Background(), testClusterOperationRequest())

	require.Error(t, err)
	require.Equal(t, http.StatusConflict, infraerrors.Code(err))
	require.Empty(t, repository.createOperationInput.Type)
}

func TestClusterServiceRefreshCacheCreatesOneGlobalOperation(t *testing.T) {
	repository := &clusterAdminRepositoryStub{}
	service := NewClusterService(repository, testClusterAdminConfig())

	result, err := service.RefreshCache(context.Background(), ClusterCacheRefreshRequest{
		Scope:          ClusterCacheScopeAllSafe,
		Reason:         "refresh safe caches",
		IdempotencyKey: uuid.NewString(),
		Actor:          ClusterOperationActor{UserID: 42},
	})

	require.NoError(t, err)
	require.Len(t, result.OperationIDs, 1)
	require.Equal(t, ClusterOperationTypeCacheRefresh, repository.createOperationInput.Type)
	require.Equal(t, ClusterCacheScopeAllSafe, repository.createOperationInput.CacheScope)
	require.Empty(t, repository.createOperationInput.TargetNodeID)
}

func TestClusterServiceRejectsInvalidReasonAndIdempotencyKey(t *testing.T) {
	service := NewClusterService(&clusterAdminRepositoryStub{}, testClusterAdminConfig())
	request := testClusterOperationRequest()
	request.Reason = "短"
	request.IdempotencyKey = "not-a-uuid"

	_, err := service.Drain(context.Background(), request)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "CLUSTER_IDEMPOTENCY_KEY_INVALID", infraerrors.Reason(err))
}

func TestClusterServiceSummaryClassifiesReadinessWithoutInventingMetrics(t *testing.T) {
	now := time.Now().UTC()
	repository := &clusterAdminRepositoryStub{
		instances: []ClusterInstance{
			{
				NodeID:               "pixel-app-01",
				Version:              "1.2.3",
				CommitSHA:            "abc",
				DesiredState:         ClusterDesiredStateActive,
				ObservedState:        ClusterObservedStateReady,
				DerivedState:         ClusterObservedStateReady,
				DatabaseHealthy:      true,
				RedisHealthy:         true,
				CacheHealthy:         true,
				MigrationHealthy:     true,
				ActiveHTTP:           3,
				DBOpenConnections:    5,
				DBMaxOpenConnections: 50,
				RedisPoolConnections: 7,
				RedisPoolSize:        128,
				DatabaseTime:         now,
			},
			{
				NodeID:       "pixel-app-02",
				DerivedState: ClusterDerivedStateStale,
				CacheHealthy: false,
				DatabaseTime: now,
			},
		},
	}
	service := NewClusterService(repository, testClusterAdminConfig())

	summary, err := service.GetSummary(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, summary.Counts.Ready)
	require.Equal(t, 1, summary.Counts.Stale)
	require.Equal(t, 1, summary.CacheLaggingNodes)
	require.Equal(t, int64(3), summary.ActiveConnections.HTTP)
	require.Equal(t, 50, summary.Pools.DatabaseMax)
	require.Equal(t, 128, summary.Pools.RedisMax)
	require.Equal(t, []string{"1.2.3@abc"}, summary.Versions)
	require.False(t, summary.NMinusOneReady)
}

func TestClusterServiceMapsIdempotencyConflict(t *testing.T) {
	repository := &clusterAdminRepositoryStub{createErr: ErrClusterOperationConflict}
	service := NewClusterService(repository, testClusterAdminConfig())
	request := testClusterOperationRequest()
	repository.instance = &ClusterInstance{
		DerivedState:     ClusterObservedStateDraining,
		DatabaseHealthy:  true,
		RedisHealthy:     true,
		CacheHealthy:     true,
		MigrationHealthy: true,
	}

	_, err := service.Resume(context.Background(), request)

	require.True(t, errors.Is(err, infraerrors.New(http.StatusConflict, "CLUSTER_IDEMPOTENCY_CONFLICT", "")))
}
