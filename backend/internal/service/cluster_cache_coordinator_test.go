package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestClusterCacheCoordinatorBumpFailureBlocksHealthUntilRetry(t *testing.T) {
	bumpCalls := 0
	repository := &clusterAdminRepositoryStub{
		bumpCacheVersionFunc: func(
			_ context.Context,
			deploymentID, cacheKey, nodeID string,
		) (*ClusterCacheVersion, error) {
			bumpCalls++
			require.Equal(t, "pixel-prod", deploymentID)
			require.Equal(t, ClusterCacheKeyPolicyMetadata, cacheKey)
			require.Equal(t, "pixel-app-01", nodeID)
			if bumpCalls == 1 {
				return nil, errors.New("database unavailable")
			}
			return &ClusterCacheVersion{
				DeploymentID: deploymentID,
				CacheKey:     cacheKey,
				Version:      2,
			}, nil
		},
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	require.NoError(t, redisClient.Close())
	coordinator := NewClusterCacheCoordinator(testClusterCacheConfig(), repository, redisClient)

	err := coordinator.Advance(context.Background(), ClusterCacheKeyPolicyMetadata)
	require.ErrorContains(t, err, "database unavailable")
	require.False(t, coordinator.Healthy())
	require.ErrorContains(t, errors.New(coordinator.LastError()), ClusterCacheKeyPolicyMetadata)

	require.NoError(t, coordinator.RetryPending(context.Background()))
	require.True(t, coordinator.Healthy())
	require.Empty(t, coordinator.LastError())
	require.Equal(t, 2, bumpCalls)
}

func TestClusterCacheCoordinatorPublishFailureKeepsPostgresVersionHealthy(t *testing.T) {
	repository := &clusterAdminRepositoryStub{
		bumpCacheVersionFunc: func(
			_ context.Context,
			deploymentID, cacheKey, _ string,
		) (*ClusterCacheVersion, error) {
			return &ClusterCacheVersion{
				DeploymentID: deploymentID,
				CacheKey:     cacheKey,
				Version:      7,
			}, nil
		},
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	require.NoError(t, redisClient.Close())
	coordinator := NewClusterCacheCoordinator(testClusterCacheConfig(), repository, redisClient)

	require.NoError(t, coordinator.Advance(context.Background(), ClusterCacheKeyChannelRouting))
	require.True(t, coordinator.Healthy())
	require.Empty(t, coordinator.LastError())
}

func TestContentModerationUpdateAdvancesOnlyPolicyMetadataVersion(t *testing.T) {
	var bumpedKeys []string
	repository := &clusterAdminRepositoryStub{
		bumpCacheVersionFunc: func(
			_ context.Context,
			deploymentID, cacheKey, _ string,
		) (*ClusterCacheVersion, error) {
			bumpedKeys = append(bumpedKeys, cacheKey)
			return &ClusterCacheVersion{
				DeploymentID: deploymentID,
				CacheKey:     cacheKey,
				Version:      1,
			}, nil
		},
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	require.NoError(t, redisClient.Close())
	coordinator := NewClusterCacheCoordinator(testClusterCacheConfig(), repository, redisClient)
	service := NewContentModerationService(
		&contentModerationSettingRepoStub{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	service.SetClusterCacheCoordinator(coordinator)
	blockMessage := "updated policy message"

	_, err := service.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		BlockMessage: &blockMessage,
	})
	require.NoError(t, err)
	require.Equal(t, []string{ClusterCacheKeyPolicyMetadata}, bumpedKeys)
}

func TestClusterRuntimeReadinessFailsWhileCacheVersionAdvanceIsPending(t *testing.T) {
	coordinator := NewClusterCacheCoordinator(
		testClusterCacheConfig(),
		&clusterAdminRepositoryStub{},
		redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}),
	)
	coordinator.markPending(ClusterCacheKeyRuntimeSettings, errors.New("version bump failed"))
	runtimeService := &ClusterRuntime{
		enabled:      true,
		clusterCache: coordinator,
	}
	runtimeService.desiredState.Store(ClusterDesiredStateActive)
	runtimeService.observedState.Store(ClusterObservedStateReady)
	runtimeService.databaseHealthy.Store(true)
	runtimeService.redisHealthy.Store(true)
	runtimeService.cacheHealthy.Store(true)
	runtimeService.migrationHealthy.Store(true)
	runtimeService.configCompatible.Store(true)
	runtimeService.identityOwned.Store(true)

	readiness := runtimeService.Readiness()
	require.False(t, readiness.Ready)
	require.False(t, readiness.CacheHealthy)
	require.Contains(t, readiness.Message, "version bump failed")
}

func testClusterCacheConfig() *config.Config {
	return &config.Config{
		Cluster: config.ClusterConfig{
			Enabled:      true,
			DeploymentID: "pixel-prod",
			NodeID:       "pixel-app-01",
		},
	}
}
