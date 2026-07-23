package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	clusterAdminMinimumReadyAfterDrain = 2
	clusterAdminDefaultOperationLimit  = 50
	clusterAdminMaximumOperationLimit  = 200
)

var ErrClusterDrainCapacityUnsafe = errors.New("cluster drain would leave insufficient ready nodes")

type ClusterService struct {
	repository   ClusterAdminRepository
	enabled      bool
	deploymentID string
	expected     int
	staleAfter   time.Duration
	offlineAfter time.Duration
}

// ClusterAdminRepository extends the runtime repository with the one operation
// whose safety invariant must be checked in the same PostgreSQL transaction as
// the audit insert. A list-then-insert implementation is not acceptable here.
type ClusterAdminRepository interface {
	ClusterRepository
	CreateDrainOperationSafely(
		ctx context.Context,
		input CreateClusterOperationInput,
		minimumReadyAfterDrain int,
		staleAfter time.Duration,
		offlineAfter time.Duration,
	) (*ClusterOperation, bool, error)
}

type ClusterSummary struct {
	Enabled           bool                     `json:"enabled"`
	DeploymentID      string                   `json:"deployment_id"`
	ExpectedNodes     int                      `json:"expected_nodes"`
	Counts            ClusterSummaryCounts     `json:"counts"`
	NMinusOneReady    bool                     `json:"n_minus_one_ready"`
	VersionConsistent bool                     `json:"version_consistent"`
	Versions          []string                 `json:"versions"`
	ActiveConnections ClusterActiveConnections `json:"active_connections"`
	Pools             ClusterPoolSummary       `json:"pools"`
	CacheLaggingNodes int                      `json:"cache_lagging_nodes"`
	RefreshedAt       time.Time                `json:"refreshed_at"`
}

type ClusterSummaryCounts struct {
	Ready     int `json:"ready"`
	Draining  int `json:"draining"`
	Unhealthy int `json:"unhealthy"`
	Stale     int `json:"stale"`
	Offline   int `json:"offline"`
}

type ClusterActiveConnections struct {
	HTTP      int64 `json:"http"`
	SSE       int64 `json:"sse"`
	WebSocket int64 `json:"websocket"`
}

type ClusterPoolSummary struct {
	DatabaseOpen int `json:"database_open"`
	DatabaseMax  int `json:"database_max"`
	RedisTotal   int `json:"redis_total"`
	RedisMax     int `json:"redis_max"`
}

// ClusterAdminInstance is the stable API projection for the cluster page.
// Metrics not yet recorded by cluster_instances remain zero rather than being
// inferred from unrelated process or pool values.
type ClusterAdminInstance struct {
	NodeID             string           `json:"node_id"`
	BootID             string           `json:"boot_id"`
	Hostname           string           `json:"hostname"`
	Version            string           `json:"version"`
	Commit             string           `json:"commit"`
	BuildDate          string           `json:"build_date"`
	DesiredState       string           `json:"desired_state"`
	ObservedState      string           `json:"observed_state"`
	Status             string           `json:"status"`
	StartedAt          time.Time        `json:"started_at"`
	LastSeenAt         time.Time        `json:"last_seen_at"`
	Ready              bool             `json:"ready"`
	DatabaseOK         bool             `json:"db_ok"`
	RedisOK            bool             `json:"redis_ok"`
	CPUUsagePercent    float64          `json:"cpu_usage_percent"`
	MemoryUsedBytes    int64            `json:"memory_used_bytes"`
	MemoryLimitBytes   int64            `json:"memory_limit_bytes"`
	GoroutineCount     int64            `json:"goroutine_count"`
	FDOpen             int64            `json:"fd_open"`
	FDLimit            int64            `json:"fd_limit"`
	ActiveHTTP         int64            `json:"active_http"`
	ActiveSSE          int64            `json:"active_sse"`
	ActiveWebSocket    int64            `json:"active_ws"`
	DBConnectionsInUse int              `json:"db_conn_active"`
	DBConnectionsIdle  int              `json:"db_conn_idle"`
	DBConnectionsWait  int64            `json:"db_conn_waiting"`
	DBConnectionsMax   int              `json:"db_conn_max_open"`
	RedisConnections   int              `json:"redis_conn_total"`
	RedisIdle          int              `json:"redis_conn_idle"`
	RedisPoolSize      int              `json:"redis_pool_size"`
	CacheVersions      map[string]int64 `json:"cache_versions"`
	ReadinessMessage   string           `json:"readiness_message"`
}

type ClusterAdminTaskLease struct {
	TaskName       string     `json:"task_name"`
	OwnerNodeID    string     `json:"owner_node_id"`
	OwnerBootID    string     `json:"owner_boot_id"`
	FencingToken   int64      `json:"fencing_token"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at"`
	LastRunAt      *time.Time `json:"last_run_at"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	LastError      string     `json:"last_error"`
	LastDurationMs *int64     `json:"last_duration_ms"`
}

type ClusterAdminOperation struct {
	ID           string     `json:"id"`
	BatchID      string     `json:"batch_id"`
	Kind         string     `json:"kind"`
	TargetNodeID *string    `json:"target_node_id"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason"`
	RequestedBy  string     `json:"requested_by"`
	RequestedAt  time.Time  `json:"requested_at"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	Error        string     `json:"error"`
}

type ClusterOperationActor struct {
	UserID int64
	Name   string
}

type ClusterNodeOperationRequest struct {
	NodeID         string
	Reason         string
	IdempotencyKey string
	Actor          ClusterOperationActor
}

type ClusterCacheRefreshRequest struct {
	Scope          string
	Reason         string
	IdempotencyKey string
	Actor          ClusterOperationActor
}

type ClusterOperationResponse struct {
	OperationIDs []string `json:"operation_ids"`
	Status       string   `json:"status"`
}

func NewClusterService(repository ClusterAdminRepository, cfg *config.Config) *ClusterService {
	service := &ClusterService{repository: repository}
	if cfg == nil {
		return service
	}
	service.enabled = cfg.Cluster.Enabled
	service.deploymentID = strings.TrimSpace(cfg.Cluster.DeploymentID)
	service.expected = cfg.Cluster.ExpectedNodes
	service.staleAfter = time.Duration(cfg.Cluster.NodeTTLSeconds) * time.Second
	service.offlineAfter = time.Duration(cfg.Cluster.OfflineAfterSeconds) * time.Second
	return service
}

func (s *ClusterService) GetSummary(ctx context.Context) (*ClusterSummary, error) {
	summary := &ClusterSummary{
		Enabled:           s != nil && s.enabled,
		VersionConsistent: true,
		Versions:          make([]string, 0),
		RefreshedAt:       time.Now().UTC(),
	}
	if s == nil {
		return summary, nil
	}
	summary.DeploymentID = s.deploymentID
	summary.ExpectedNodes = s.expected
	if !s.enabled {
		return summary, nil
	}

	instances, err := s.listInstances(ctx)
	if err != nil {
		return nil, err
	}
	cacheVersions, err := s.repository.ListCacheVersions(ctx, s.deploymentID)
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	authoritativeCacheVersions := make(map[string]int64, len(cacheVersions))
	for i := range cacheVersions {
		authoritativeCacheVersions[cacheVersions[i].CacheKey] = cacheVersions[i].Version
	}
	versionSet := make(map[string]struct{})
	for i := range instances {
		instance := &instances[i]
		status, _ := clusterAdminInstanceState(instance)
		switch status {
		case ClusterObservedStateReady:
			summary.Counts.Ready++
		case ClusterObservedStateDraining:
			summary.Counts.Draining++
		case ClusterDerivedStateStale:
			summary.Counts.Stale++
		case ClusterDerivedStateOffline:
			summary.Counts.Offline++
		default:
			summary.Counts.Unhealthy++
		}
		if status != ClusterDerivedStateStale && status != ClusterDerivedStateOffline {
			version := strings.TrimSpace(instance.Version)
			if commit := strings.TrimSpace(instance.CommitSHA); commit != "" {
				version += "@" + commit
			}
			if version != "" {
				versionSet[version] = struct{}{}
			}
		}
		if status != ClusterDerivedStateOffline &&
			clusterAdminCacheIsLagging(instance, authoritativeCacheVersions) {
			summary.CacheLaggingNodes++
		}
		summary.ActiveConnections.HTTP += instance.ActiveHTTP
		summary.ActiveConnections.SSE += instance.ActiveSSE
		summary.ActiveConnections.WebSocket += instance.ActiveWebSocket
		summary.Pools.DatabaseOpen += instance.DBOpenConnections
		summary.Pools.DatabaseMax += instance.DBMaxOpenConnections
		summary.Pools.RedisTotal += instance.RedisPoolConnections
		summary.Pools.RedisMax += instance.RedisPoolSize
		if instance.DatabaseTime.After(summary.RefreshedAt) || i == 0 {
			summary.RefreshedAt = instance.DatabaseTime
		}
	}
	for version := range versionSet {
		summary.Versions = append(summary.Versions, version)
	}
	sort.Strings(summary.Versions)
	summary.VersionConsistent = len(summary.Versions) <= 1
	requiredReady := s.expected - 1
	if requiredReady < 1 {
		requiredReady = 1
	}
	summary.NMinusOneReady = summary.Counts.Ready >= requiredReady
	return summary, nil
}

func (s *ClusterService) ListInstances(ctx context.Context) ([]ClusterAdminInstance, error) {
	if s == nil || !s.enabled {
		return []ClusterAdminInstance{}, nil
	}
	instances, err := s.listInstances(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ClusterAdminInstance, 0, len(instances))
	for i := range instances {
		result = append(result, s.projectInstance(&instances[i]))
	}
	return result, nil
}

func (s *ClusterService) GetInstance(ctx context.Context, nodeID string) (*ClusterAdminInstance, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, clusterAdminBadRequest("CLUSTER_NODE_ID_REQUIRED", "node_id is required")
	}
	instance, err := s.repository.GetInstance(ctx, s.deploymentID, nodeID, s.staleAfter, s.offlineAfter)
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	projected := s.projectInstance(instance)
	return &projected, nil
}

func (s *ClusterService) ListTasks(ctx context.Context) ([]ClusterAdminTaskLease, error) {
	if s == nil || !s.enabled {
		return []ClusterAdminTaskLease{}, nil
	}
	leases, err := s.repository.ListTaskLeases(ctx, s.deploymentID)
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	result := make([]ClusterAdminTaskLease, 0, len(leases))
	for i := range leases {
		lease := &leases[i]
		result = append(result, ClusterAdminTaskLease{
			TaskName:       lease.TaskName,
			OwnerNodeID:    lease.OwnerNodeID,
			OwnerBootID:    lease.OwnerBootID,
			FencingToken:   lease.FencingToken,
			LeaseExpiresAt: lease.LeaseExpiresAt,
			LastRunAt:      lease.LastAcquiredAt,
			LastSuccessAt:  lease.LastSuccessAt,
			LastError:      lease.LastError,
			LastDurationMs: lease.LastDurationMs,
		})
	}
	return result, nil
}

func (s *ClusterService) ListOperations(ctx context.Context, limit int) ([]ClusterAdminOperation, error) {
	if s == nil || !s.enabled {
		return []ClusterAdminOperation{}, nil
	}
	if limit <= 0 {
		limit = clusterAdminDefaultOperationLimit
	}
	if limit > clusterAdminMaximumOperationLimit {
		return nil, clusterAdminBadRequest("CLUSTER_OPERATION_LIMIT_INVALID", "limit must be between 1 and 200")
	}
	operations, err := s.repository.ListOperations(ctx, ClusterOperationFilter{
		DeploymentID: s.deploymentID,
		Limit:        limit,
	})
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	result := make([]ClusterAdminOperation, 0, len(operations))
	for i := range operations {
		result = append(result, projectClusterAdminOperation(&operations[i]))
	}
	return result, nil
}

func (s *ClusterService) Drain(ctx context.Context, request ClusterNodeOperationRequest) (*ClusterOperationResponse, error) {
	if err := s.validateNodeOperationRequest(request); err != nil {
		return nil, err
	}
	nodeID := strings.TrimSpace(request.NodeID)
	fingerprint, err := clusterAdminFingerprint(struct {
		Type    string `json:"type"`
		Target  string `json:"target"`
		Reason  string `json:"reason"`
		ActorID int64  `json:"actor_id"`
	}{
		Type:    ClusterOperationTypeDrain,
		Target:  nodeID,
		Reason:  strings.TrimSpace(request.Reason),
		ActorID: request.Actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	operation, _, err := s.repository.CreateDrainOperationSafely(
		ctx,
		CreateClusterOperationInput{
			DeploymentID:       s.deploymentID,
			IdempotencyKey:     strings.TrimSpace(request.IdempotencyKey),
			RequestFingerprint: fingerprint,
			Type:               ClusterOperationTypeDrain,
			TargetNodeID:       nodeID,
			Reason:             strings.TrimSpace(request.Reason),
			ActorUserID:        request.Actor.UserID,
			ActorName:          clusterAdminActorName(request.Actor),
		},
		clusterAdminMinimumReadyAfterDrain,
		s.staleAfter,
		s.offlineAfter,
	)
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	return &ClusterOperationResponse{
		OperationIDs: []string{operation.ID},
		Status:       ClusterOperationStatusPending,
	}, nil
}

func (s *ClusterService) Resume(ctx context.Context, request ClusterNodeOperationRequest) (*ClusterOperationResponse, error) {
	if err := s.validateNodeOperationRequest(request); err != nil {
		return nil, err
	}
	nodeID := strings.TrimSpace(request.NodeID)
	instance, err := s.repository.GetInstance(ctx, s.deploymentID, nodeID, s.staleAfter, s.offlineAfter)
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	if instance.DerivedState == ClusterDerivedStateStale || instance.DerivedState == ClusterDerivedStateOffline {
		return nil, clusterAdminConflict("CLUSTER_RESUME_NODE_OFFLINE", "only an online node can resume traffic")
	}
	if !instance.DatabaseHealthy || !instance.RedisHealthy || !instance.CacheHealthy || !instance.MigrationHealthy {
		return nil, clusterAdminConflict(
			"CLUSTER_RESUME_DEPENDENCY_UNHEALTHY",
			"node database, Redis, cache, and migration checks must all be healthy",
		)
	}
	return s.createSingleOperation(ctx, ClusterOperationTypeResume, nodeID, "", request.Reason, request.IdempotencyKey, request.Actor)
}

func (s *ClusterService) RefreshCache(ctx context.Context, request ClusterCacheRefreshRequest) (*ClusterOperationResponse, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	if err := validateClusterAdminIdempotencyKey(request.IdempotencyKey); err != nil {
		return nil, err
	}
	if err := validateClusterAdminReason(request.Reason); err != nil {
		return nil, err
	}
	if err := validateClusterAdminActor(request.Actor); err != nil {
		return nil, err
	}
	scope, err := normalizeClusterCacheScope(request.Scope)
	if err != nil {
		return nil, err
	}
	fingerprint, err := clusterAdminFingerprint(struct {
		Type    string `json:"type"`
		Scope   string `json:"scope"`
		Reason  string `json:"reason"`
		ActorID int64  `json:"actor_id"`
	}{
		Type:    ClusterOperationTypeCacheRefresh,
		Scope:   scope,
		Reason:  strings.TrimSpace(request.Reason),
		ActorID: request.Actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	operation, _, err := s.repository.CreateOperation(ctx, CreateClusterOperationInput{
		DeploymentID:       s.deploymentID,
		IdempotencyKey:     strings.TrimSpace(request.IdempotencyKey),
		RequestFingerprint: fingerprint,
		Type:               ClusterOperationTypeCacheRefresh,
		CacheScope:         scope,
		Reason:             strings.TrimSpace(request.Reason),
		ActorUserID:        request.Actor.UserID,
		ActorName:          clusterAdminActorName(request.Actor),
	})
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	return &ClusterOperationResponse{
		OperationIDs: []string{operation.ID},
		Status:       ClusterOperationStatusPending,
	}, nil
}

func (s *ClusterService) createSingleOperation(
	ctx context.Context,
	operationType, targetNodeID, cacheScope, reason, idempotencyKey string,
	actor ClusterOperationActor,
) (*ClusterOperationResponse, error) {
	fingerprint, err := clusterAdminFingerprint(struct {
		Type    string `json:"type"`
		Target  string `json:"target"`
		Scope   string `json:"scope"`
		Reason  string `json:"reason"`
		ActorID int64  `json:"actor_id"`
	}{
		Type:    operationType,
		Target:  targetNodeID,
		Scope:   cacheScope,
		Reason:  strings.TrimSpace(reason),
		ActorID: actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	operation, _, err := s.repository.CreateOperation(ctx, CreateClusterOperationInput{
		DeploymentID:       s.deploymentID,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fingerprint,
		Type:               operationType,
		TargetNodeID:       targetNodeID,
		CacheScope:         cacheScope,
		Reason:             strings.TrimSpace(reason),
		ActorUserID:        actor.UserID,
		ActorName:          clusterAdminActorName(actor),
	})
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	return &ClusterOperationResponse{
		OperationIDs: []string{operation.ID},
		Status:       ClusterOperationStatusPending,
	}, nil
}

func (s *ClusterService) validateNodeOperationRequest(request ClusterNodeOperationRequest) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	if strings.TrimSpace(request.NodeID) == "" {
		return clusterAdminBadRequest("CLUSTER_NODE_ID_REQUIRED", "node_id is required")
	}
	if err := validateClusterAdminIdempotencyKey(request.IdempotencyKey); err != nil {
		return err
	}
	if err := validateClusterAdminReason(request.Reason); err != nil {
		return err
	}
	return validateClusterAdminActor(request.Actor)
}

func (s *ClusterService) listInstances(ctx context.Context) ([]ClusterInstance, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	instances, err := s.repository.ListInstances(ctx, s.deploymentID, s.staleAfter, s.offlineAfter)
	if err != nil {
		return nil, mapClusterAdminRepositoryError(err)
	}
	return instances, nil
}

func (s *ClusterService) projectInstance(instance *ClusterInstance) ClusterAdminInstance {
	status, ready := clusterAdminInstanceState(instance)
	cacheVersions := make(map[string]int64, len(instance.CacheVersions))
	for cacheKey, version := range instance.CacheVersions {
		cacheVersions[cacheKey] = version
	}
	return ClusterAdminInstance{
		NodeID:             instance.NodeID,
		BootID:             instance.BootID,
		Hostname:           instance.Hostname,
		Version:            instance.Version,
		Commit:             instance.CommitSHA,
		BuildDate:          instance.BuildDate,
		DesiredState:       instance.DesiredState,
		ObservedState:      instance.ObservedState,
		Status:             status,
		StartedAt:          instance.StartedAt,
		LastSeenAt:         instance.HeartbeatAt,
		Ready:              ready,
		DatabaseOK:         instance.DatabaseHealthy,
		RedisOK:            instance.RedisHealthy,
		CPUUsagePercent:    instance.CPUPercent,
		MemoryUsedBytes:    instance.RSSBytes,
		MemoryLimitBytes:   instance.MemoryLimitBytes,
		GoroutineCount:     instance.GoroutineCount,
		FDOpen:             instance.FDOpen,
		FDLimit:            instance.FDLimit,
		ActiveHTTP:         instance.ActiveHTTP,
		ActiveSSE:          instance.ActiveSSE,
		ActiveWebSocket:    instance.ActiveWebSocket,
		DBConnectionsInUse: instance.DBInUseConnections,
		DBConnectionsIdle:  instance.DBIdleConnections,
		DBConnectionsWait:  instance.DBWaitCount,
		DBConnectionsMax:   instance.DBMaxOpenConnections,
		RedisConnections:   instance.RedisPoolConnections,
		RedisIdle:          instance.RedisIdleConnections,
		RedisPoolSize:      instance.RedisPoolSize,
		CacheVersions:      cacheVersions,
		ReadinessMessage:   clusterAdminReadinessMessage(instance, status, ready),
	}
}

func clusterAdminCacheIsLagging(instance *ClusterInstance, authoritative map[string]int64) bool {
	if instance == nil || !instance.CacheHealthy {
		return true
	}
	for cacheKey, version := range authoritative {
		if instance.CacheVersions[cacheKey] < version {
			return true
		}
	}
	return false
}

func (s *ClusterService) requireEnabled() error {
	if s == nil || !s.enabled {
		return infraerrors.New(http.StatusServiceUnavailable, "CLUSTER_DISABLED", "cluster mode is not enabled")
	}
	if s.repository == nil {
		return infraerrors.New(http.StatusServiceUnavailable, "CLUSTER_REPOSITORY_UNAVAILABLE", "cluster repository is not available")
	}
	if s.deploymentID == "" {
		return infraerrors.New(http.StatusServiceUnavailable, "CLUSTER_CONFIG_INVALID", "cluster deployment_id is not configured")
	}
	if s.staleAfter <= 0 || s.offlineAfter <= s.staleAfter {
		return infraerrors.New(http.StatusServiceUnavailable, "CLUSTER_CONFIG_INVALID", "cluster status intervals are invalid")
	}
	return nil
}

func clusterAdminInstanceState(instance *ClusterInstance) (string, bool) {
	if instance == nil {
		return ClusterObservedStateUnhealthy, false
	}
	if instance.DerivedState == ClusterDerivedStateOffline {
		return ClusterDerivedStateOffline, false
	}
	if instance.DerivedState == ClusterDerivedStateStale {
		return ClusterDerivedStateStale, false
	}
	if instance.DesiredState == ClusterDesiredStateDraining || instance.ObservedState == ClusterObservedStateDraining {
		return ClusterObservedStateDraining, false
	}
	ready := instance.ObservedState == ClusterObservedStateReady &&
		instance.DatabaseHealthy &&
		instance.RedisHealthy &&
		instance.CacheHealthy &&
		instance.MigrationHealthy
	if ready {
		return ClusterObservedStateReady, true
	}
	if instance.ObservedState == ClusterObservedStateStarting {
		return ClusterObservedStateStarting, false
	}
	return ClusterObservedStateUnhealthy, false
}

func clusterAdminReadinessMessage(instance *ClusterInstance, status string, ready bool) string {
	if ready {
		return ""
	}
	if instance == nil {
		return "instance data is unavailable"
	}
	if message := strings.TrimSpace(instance.LastError); message != "" {
		return message
	}
	switch status {
	case ClusterDerivedStateOffline:
		return "node heartbeat is offline"
	case ClusterDerivedStateStale:
		return "node heartbeat is stale"
	case ClusterObservedStateDraining:
		return "node is draining"
	case ClusterObservedStateStarting:
		return "node is starting"
	}
	unhealthy := make([]string, 0, 4)
	if !instance.DatabaseHealthy {
		unhealthy = append(unhealthy, "database")
	}
	if !instance.RedisHealthy {
		unhealthy = append(unhealthy, "redis")
	}
	if !instance.CacheHealthy {
		unhealthy = append(unhealthy, "cache")
	}
	if !instance.MigrationHealthy {
		unhealthy = append(unhealthy, "migration")
	}
	if len(unhealthy) > 0 {
		return strings.Join(unhealthy, ", ") + " check failed"
	}
	return "node is not ready"
}

func projectClusterAdminOperation(operation *ClusterOperation) ClusterAdminOperation {
	var targetNodeID *string
	if operation.TargetNodeID != "" {
		value := operation.TargetNodeID
		targetNodeID = &value
	}
	requestedBy := strings.TrimSpace(operation.ActorName)
	if requestedBy == "" {
		requestedBy = "admin:" + strconv.FormatInt(operation.ActorUserID, 10)
	}
	return ClusterAdminOperation{
		ID:           operation.ID,
		BatchID:      operation.IdempotencyKey,
		Kind:         operation.Type,
		TargetNodeID: targetNodeID,
		Status:       operation.Status,
		Reason:       operation.Reason,
		RequestedBy:  requestedBy,
		RequestedAt:  operation.CreatedAt,
		StartedAt:    operation.ClaimedAt,
		CompletedAt:  operation.CompletedAt,
		Error:        operation.ErrorMessage,
	}
}

func normalizeClusterCacheScope(value string) (string, error) {
	scope := strings.TrimSpace(value)
	switch scope {
	case ClusterCacheKeyChannelRouting,
		ClusterCacheKeyRuntimeSettings,
		ClusterCacheKeyPolicyMetadata,
		ClusterCacheScopeAllSafe:
		return scope, nil
	default:
		return "", clusterAdminBadRequest(
			"CLUSTER_CACHE_SCOPE_INVALID",
			fmt.Sprintf("invalid cache scope %q", scope),
		)
	}
}

func validateClusterAdminIdempotencyKey(value string) error {
	trimmed := strings.TrimSpace(value)
	parsed, err := uuid.Parse(trimmed)
	if err != nil || parsed.String() != strings.ToLower(trimmed) {
		return clusterAdminBadRequest(
			"CLUSTER_IDEMPOTENCY_KEY_INVALID",
			"Idempotency-Key must be a UUID",
		)
	}
	return nil
}

func validateClusterAdminReason(reason string) error {
	length := utf8.RuneCountInString(strings.TrimSpace(reason))
	if length < 8 || length > 500 {
		return clusterAdminBadRequest(
			"CLUSTER_OPERATION_REASON_INVALID",
			"reason must contain between 8 and 500 characters",
		)
	}
	return nil
}

func validateClusterAdminActor(actor ClusterOperationActor) error {
	if actor.UserID <= 0 {
		return infraerrors.New(http.StatusUnauthorized, "CLUSTER_ACTOR_INVALID", "authenticated administrator is required")
	}
	return nil
}

func clusterAdminActorName(actor ClusterOperationActor) string {
	if value := strings.TrimSpace(actor.Name); value != "" {
		return value
	}
	return "admin:" + strconv.FormatInt(actor.UserID, 10)
}

func clusterAdminFingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal cluster operation fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func mapClusterAdminRepositoryError(err error) error {
	switch {
	case errors.Is(err, ErrClusterInstanceNotFound):
		return clusterAdminNotFound("CLUSTER_INSTANCE_NOT_FOUND", "cluster instance not found")
	case errors.Is(err, ErrClusterOperationNotFound):
		return clusterAdminNotFound("CLUSTER_OPERATION_NOT_FOUND", "cluster operation not found")
	case errors.Is(err, ErrClusterOperationConflict):
		return clusterAdminConflict(
			"CLUSTER_IDEMPOTENCY_CONFLICT",
			"Idempotency-Key was already used for a different request",
		)
	case errors.Is(err, ErrClusterDrainCapacityUnsafe):
		return clusterAdminConflict(
			"CLUSTER_DRAIN_CAPACITY_UNSAFE",
			"draining this node would leave fewer than two ready nodes",
		)
	default:
		return err
	}
}

func clusterAdminBadRequest(reason, message string) error {
	return infraerrors.New(http.StatusBadRequest, reason, message)
}

func clusterAdminNotFound(reason, message string) error {
	return infraerrors.New(http.StatusNotFound, reason, message)
}

func clusterAdminConflict(reason, message string) error {
	return infraerrors.New(http.StatusConflict, reason, message)
}
