package service

import (
	"context"
	"time"
)

type ClusterRepository interface {
	ClaimInstance(ctx context.Context, heartbeat ClusterInstanceHeartbeat, nodeTTL time.Duration) error
	Heartbeat(ctx context.Context, heartbeat ClusterInstanceHeartbeat) (*ClusterInstance, error)
	SetInstanceDesiredState(ctx context.Context, deploymentID, nodeID, desiredState string) (*ClusterInstance, error)
	ListInstances(ctx context.Context, deploymentID string, staleAfter, offlineAfter time.Duration) ([]ClusterInstance, error)
	GetInstance(ctx context.Context, deploymentID, nodeID string, staleAfter, offlineAfter time.Duration) (*ClusterInstance, error)
	DeleteOfflineInstances(ctx context.Context, deploymentID string, retention time.Duration) (int64, error)

	AcquireTaskLease(ctx context.Context, deploymentID, taskName, nodeID, bootID string, leaseDuration time.Duration) (*ClusterTaskLease, bool, error)
	RenewTaskLease(ctx context.Context, deploymentID, taskName, nodeID, bootID string, fencingToken int64, leaseDuration time.Duration) (bool, error)
	ReleaseTaskLease(ctx context.Context, deploymentID, taskName, nodeID, bootID string, fencingToken int64, succeeded bool, resultError string, duration time.Duration) (bool, error)
	ListTaskLeases(ctx context.Context, deploymentID string) ([]ClusterTaskLease, error)

	CreateOperation(ctx context.Context, input CreateClusterOperationInput) (*ClusterOperation, bool, error)
	GetOperation(ctx context.Context, deploymentID, operationID string) (*ClusterOperation, error)
	ClaimPendingOperations(ctx context.Context, deploymentID, nodeID, bootID string, limit int, claimDuration time.Duration) ([]ClusterOperation, error)
	CompleteOperation(ctx context.Context, deploymentID, operationID, nodeID, bootID string, attemptToken int64, succeeded bool, result, resultError string) (bool, error)
	ListOperations(ctx context.Context, filter ClusterOperationFilter) ([]ClusterOperation, error)

	GetCacheVersion(ctx context.Context, deploymentID, cacheKey string) (*ClusterCacheVersion, error)
	ListCacheVersions(ctx context.Context, deploymentID string) ([]ClusterCacheVersion, error)
	EnsureCacheVersions(ctx context.Context, deploymentID, nodeID string) error
	BumpCacheVersion(ctx context.Context, deploymentID, cacheKey, nodeID string) (*ClusterCacheVersion, error)
}
