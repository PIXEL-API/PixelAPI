package service

import (
	"errors"
	"time"
)

const (
	ClusterDesiredStateActive   = "active"
	ClusterDesiredStateDraining = "draining"

	ClusterObservedStateStarting  = "starting"
	ClusterObservedStateReady     = "ready"
	ClusterObservedStateDraining  = "draining"
	ClusterObservedStateUnhealthy = "unhealthy"

	ClusterDerivedStateStale   = "stale"
	ClusterDerivedStateOffline = "offline"

	ClusterOperationTypeDrain        = "drain"
	ClusterOperationTypeResume       = "resume"
	ClusterOperationTypeCacheRefresh = "cache_refresh"

	ClusterOperationStatusPending   = "pending"
	ClusterOperationStatusRunning   = "running"
	ClusterOperationStatusSucceeded = "succeeded"
	ClusterOperationStatusFailed    = "failed"

	ClusterCacheKeyChannelRouting  = "channel_routing"
	ClusterCacheKeyRuntimeSettings = "runtime_settings"
	ClusterCacheKeyPolicyMetadata  = "policy_metadata"
	ClusterCacheScopeAllSafe       = "all_safe"
)

var (
	ErrClusterNodeConflict         = errors.New("cluster node_id is owned by another live boot")
	ErrClusterInstanceNotFound     = errors.New("cluster instance not found")
	ErrClusterInstanceOwnerLost    = errors.New("cluster instance ownership lost")
	ErrClusterTaskLeaseNotAcquired = errors.New("cluster task lease not acquired")
	ErrClusterOperationNotFound    = errors.New("cluster operation not found")
	ErrClusterOperationConflict    = errors.New("cluster operation idempotency key conflicts with another request")
	ErrClusterOperationOwnerLost   = errors.New("cluster operation ownership lost")
	ErrClusterCacheVersionNotFound = errors.New("cluster cache version not found")
)

type ClusterInstance struct {
	DeploymentID         string
	NodeID               string
	BootID               string
	DesiredState         string
	ObservedState        string
	DerivedState         string
	Hostname             string
	Version              string
	CommitSHA            string
	BuildDate            string
	ConfigFingerprint    string
	SecretFingerprint    string
	CacheVersions        map[string]int64
	StartedAt            time.Time
	HeartbeatAt          time.Time
	DatabaseTime         time.Time
	CPUPercent           float64
	RSSBytes             int64
	MemoryLimitBytes     int64
	GoroutineCount       int64
	FDOpen               int64
	FDLimit              int64
	ActiveHTTP           int64
	ActiveSSE            int64
	ActiveWebSocket      int64
	DBOpenConnections    int
	DBInUseConnections   int
	DBIdleConnections    int
	DBWaitCount          int64
	DBMaxOpenConnections int
	RedisPoolConnections int
	RedisIdleConnections int
	RedisPoolSize        int
	DatabaseHealthy      bool
	RedisHealthy         bool
	CacheHealthy         bool
	MigrationHealthy     bool
	LastError            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ClusterInstanceHeartbeat struct {
	DeploymentID         string
	NodeID               string
	BootID               string
	Hostname             string
	Version              string
	CommitSHA            string
	BuildDate            string
	ConfigFingerprint    string
	SecretFingerprint    string
	CacheVersions        map[string]int64
	ObservedState        string
	CPUPercent           float64
	RSSBytes             int64
	MemoryLimitBytes     int64
	GoroutineCount       int64
	FDOpen               int64
	FDLimit              int64
	ActiveHTTP           int64
	ActiveSSE            int64
	ActiveWebSocket      int64
	DBOpenConnections    int
	DBInUseConnections   int
	DBIdleConnections    int
	DBWaitCount          int64
	DBMaxOpenConnections int
	RedisPoolConnections int
	RedisIdleConnections int
	RedisPoolSize        int
	DatabaseHealthy      bool
	RedisHealthy         bool
	CacheHealthy         bool
	MigrationHealthy     bool
	LastError            string
}

type ClusterTaskLease struct {
	DeploymentID   string
	TaskName       string
	OwnerNodeID    string
	OwnerBootID    string
	FencingToken   int64
	LeaseExpiresAt *time.Time
	LastAcquiredAt *time.Time
	LastRenewedAt  *time.Time
	LastReleasedAt *time.Time
	LastSuccessAt  *time.Time
	LastError      string
	LastDurationMs *int64
	DatabaseTime   time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ClusterOperation struct {
	ID                 string
	DeploymentID       string
	IdempotencyKey     string
	RequestFingerprint string
	Type               string
	TargetNodeID       string
	CacheScope         string
	Reason             string
	ActorUserID        int64
	ActorName          string
	Status             string
	AttemptToken       int64
	ClaimedByNodeID    string
	ClaimedByBootID    string
	ClaimExpiresAt     *time.Time
	ClaimedAt          *time.Time
	CompletedAt        *time.Time
	Result             string
	ErrorMessage       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateClusterOperationInput struct {
	ID                 string
	DeploymentID       string
	IdempotencyKey     string
	RequestFingerprint string
	Type               string
	TargetNodeID       string
	CacheScope         string
	Reason             string
	ActorUserID        int64
	ActorName          string
}

type ClusterOperationFilter struct {
	DeploymentID string
	Status       string
	Type         string
	TargetNodeID string
	Limit        int
	Offset       int
}

type ClusterCacheVersion struct {
	DeploymentID    string
	CacheKey        string
	Version         int64
	UpdatedByNodeID string
	UpdatedAt       time.Time
}
