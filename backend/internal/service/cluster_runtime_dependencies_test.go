package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewClusterRuntimeRejectsMissingRequiredClusterDependencies(t *testing.T) {
	cfg := testClusterRuntimeConfig()

	_, err := NewClusterRuntime(
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		BuildInfo{},
		nil,
		nil,
		nil,
	)
	require.Error(t, err)
	for _, dependency := range []string{
		"cluster repository",
		"PostgreSQL",
		"Redis",
		"connection tracker",
		"node state",
		"cache coordinator",
		"task executor",
		"channel cache service",
		"runtime settings service",
		"policy metadata service",
	} {
		require.ErrorContains(t, err, dependency)
	}
}

func TestClusterRuntimeOfflineRetentionUsesLeaseAndThirtyDayDatabaseCleanup(t *testing.T) {
	cfg := testClusterRuntimeConfig()
	nodeState := NewClusterNodeState(cfg)
	repository := &clusterAdminRepositoryStub{
		acquiredLease: &ClusterTaskLease{
			TaskName:     "cluster.instances.retention",
			FencingToken: 9,
		},
		leaseAcquired: true,
		leaseRenewed:  true,
		leaseReleased: true,
	}
	executor := NewClusterTaskExecutor(cfg, repository, nodeState)
	runtimeService := &ClusterRuntime{
		ctx:          context.Background(),
		clusterCfg:   cfg.Cluster,
		repository:   repository,
		taskExecutor: executor,
	}

	runtimeService.runInstanceRetention()

	require.Equal(t, "cluster.instances.retention", repository.acquiredTaskName)
	require.Equal(t, 1, repository.deleteOfflineCalls)
	require.Equal(t, "pixel-prod", repository.deletedDeploymentID)
	require.Equal(t, 30*24*time.Hour, repository.deletedRetention)
}

func testClusterRuntimeConfig() *config.Config {
	return &config.Config{
		Cluster: config.ClusterConfig{
			Enabled:                  true,
			DeploymentID:             "pixel-prod",
			NodeID:                   "pixel-app-01",
			TaskLeaseSeconds:         60,
			TaskRenewIntervalSeconds: 20,
		},
	}
}
