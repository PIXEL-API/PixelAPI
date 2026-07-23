package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClusterRepositoryClaimInstanceRejectsLiveDifferentBoot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	heartbeat := service.ClusterInstanceHeartbeat{
		DeploymentID: "pixel-prod",
		NodeID:       "pixel-app-01",
		BootID:       uuid.NewString(),
		Hostname:     "app-01",
		Version:      "1.2.3",
	}
	mock.ExpectQuery(`(?s)INSERT INTO cluster_instances.*ON CONFLICT.*heartbeat_at.*RETURNING 1`).
		WithArgs(
			heartbeat.DeploymentID,
			heartbeat.NodeID,
			heartbeat.BootID,
			heartbeat.Hostname,
			heartbeat.Version,
			"",
			"",
			"",
			"",
			int64(30),
		).
		WillReturnError(sql.ErrNoRows)

	repo := NewClusterRepository(db)
	err = repo.ClaimInstance(context.Background(), heartbeat, 30*time.Second)
	require.ErrorIs(t, err, service.ErrClusterNodeConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryRenewTaskLeaseFencesExpiredOrStaleOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bootID := uuid.NewString()
	mock.ExpectExec(`(?s)UPDATE cluster_task_leases.*owner_boot_id = \$4::uuid.*fencing_token = \$5.*lease_expires_at > clock_timestamp\(\)`).
		WithArgs("pixel-prod", "ops-aggregate", "pixel-app-01", bootID, int64(17), int64(60)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewClusterRepository(db)
	renewed, err := repo.RenewTaskLease(
		context.Background(),
		"pixel-prod",
		"ops-aggregate",
		"pixel-app-01",
		bootID,
		17,
		time.Minute,
	)
	require.NoError(t, err)
	require.False(t, renewed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryAcquireTaskLeaseDoesNotReenterActiveOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bootID := uuid.NewString()
	mock.ExpectQuery(`(?s)ON CONFLICT \(deployment_id, task_name\) DO UPDATE.*fencing_token = cluster_task_leases\.fencing_token \+ 1.*WHERE cluster_task_leases\.lease_expires_at IS NULL\s+OR cluster_task_leases\.lease_expires_at <= clock_timestamp\(\)`).
		WithArgs("pixel-prod", "ops-aggregate", "pixel-app-01", bootID, int64(60)).
		WillReturnRows(sqlmock.NewRows([]string{"deployment_id"}))

	repo := NewClusterRepository(db)
	lease, acquired, err := repo.AcquireTaskLease(
		context.Background(),
		"pixel-prod",
		"ops-aggregate",
		"pixel-app-01",
		bootID,
		time.Minute,
	)
	require.NoError(t, err)
	require.Nil(t, lease)
	require.False(t, acquired)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateOperationDetectsIdempotencyFingerprintConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	idempotencyKey := uuid.NewString()
	operationID := uuid.NewString()
	bootID := uuid.NewString()
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	existingFingerprint := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	now := time.Now().UTC()

	mock.ExpectQuery(`(?s)INSERT INTO cluster_operations.*ON CONFLICT.*DO NOTHING.*RETURNING`).
		WithArgs(
			sqlmock.AnyArg(),
			"pixel-prod",
			idempotencyKey,
			fingerprint,
			service.ClusterOperationTypeDrain,
			"pixel-app-01",
			nil,
			"集群节点摘流操作",
			int64(1),
			"admin",
		).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs("pixel-prod", idempotencyKey).
		WillReturnRows(clusterOperationMockRows().AddRow(
			operationID,
			"pixel-prod",
			idempotencyKey,
			existingFingerprint,
			service.ClusterOperationTypeDrain,
			"pixel-app-01",
			"",
			"集群节点摘流操作",
			int64(1),
			"admin",
			service.ClusterOperationStatusRunning,
			int64(3),
			"pixel-app-01",
			bootID,
			now.Add(time.Minute),
			now,
			nil,
			"",
			"",
			now,
			now,
		))

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateOperation(context.Background(), service.CreateClusterOperationInput{
		DeploymentID:       "pixel-prod",
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fingerprint,
		Type:               service.ClusterOperationTypeDrain,
		TargetNodeID:       "pixel-app-01",
		Reason:             "集群节点摘流操作",
		ActorUserID:        1,
		ActorName:          "admin",
	})
	require.Nil(t, operation)
	require.False(t, created)
	require.ErrorIs(t, err, service.ErrClusterOperationConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateDrainOperationSafelyLocksCapacityAndCommitsAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	input := clusterDrainOperationInput()
	operationID := uuid.NewString()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)FROM cluster_instances.*ORDER BY node_id ASC.*FOR UPDATE`).
		WithArgs(input.DeploymentID, int64(30), int64(300)).
		WillReturnRows(clusterDrainCandidateMockRows().
			AddRow("pixel-app-01", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-02", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-03", "active", "ready", "ready", true, true, true, true))
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)SELECT target_node_id.*FROM cluster_operations.*status IN \('pending', 'running'\)`).
		WithArgs(input.DeploymentID).
		WillReturnRows(sqlmock.NewRows([]string{"target_node_id"}))
	mock.ExpectQuery(`(?s)INSERT INTO cluster_operations.*ON CONFLICT.*DO NOTHING.*RETURNING`).
		WithArgs(
			sqlmock.AnyArg(),
			input.DeploymentID,
			input.IdempotencyKey,
			input.RequestFingerprint,
			service.ClusterOperationTypeDrain,
			input.TargetNodeID,
			nil,
			input.Reason,
			input.ActorUserID,
			input.ActorName,
		).
		WillReturnRows(clusterOperationMockRows().AddRow(
			operationID,
			input.DeploymentID,
			input.IdempotencyKey,
			input.RequestFingerprint,
			service.ClusterOperationTypeDrain,
			input.TargetNodeID,
			"",
			input.Reason,
			input.ActorUserID,
			input.ActorName,
			service.ClusterOperationStatusPending,
			int64(0),
			"",
			"",
			nil,
			nil,
			nil,
			"",
			"",
			now,
			now,
		))
	mock.ExpectCommit()

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateDrainOperationSafely(
		context.Background(),
		input,
		2,
		30*time.Second,
		5*time.Minute,
	)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, input.TargetNodeID, operation.TargetNodeID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateDrainOperationSafelyRejectsUnsafeCapacity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	input := clusterDrainOperationInput()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)FROM cluster_instances.*ORDER BY node_id ASC.*FOR UPDATE`).
		WithArgs(input.DeploymentID, int64(30), int64(300)).
		WillReturnRows(clusterDrainCandidateMockRows().
			AddRow("pixel-app-01", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-02", "active", "ready", "ready", true, true, true, true))
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectRollback()

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateDrainOperationSafely(
		context.Background(),
		input,
		2,
		30*time.Second,
		5*time.Minute,
	)
	require.Nil(t, operation)
	require.False(t, created)
	require.ErrorIs(t, err, service.ErrClusterDrainCapacityUnsafe)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateDrainOperationSafelyReturnsIdempotentAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	input := clusterDrainOperationInput()
	operationID := uuid.NewString()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows().AddRow(
			operationID,
			input.DeploymentID,
			input.IdempotencyKey,
			input.RequestFingerprint,
			service.ClusterOperationTypeDrain,
			input.TargetNodeID,
			"",
			input.Reason,
			input.ActorUserID,
			input.ActorName,
			service.ClusterOperationStatusPending,
			int64(0),
			"",
			"",
			nil,
			nil,
			nil,
			"",
			"",
			now,
			now,
		))
	mock.ExpectCommit()

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateDrainOperationSafely(
		context.Background(),
		input,
		2,
		30*time.Second,
		5*time.Minute,
	)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, operationID, operation.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateDrainOperationSafelyRechecksIdempotencyAfterLockWait(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	input := clusterDrainOperationInput()
	operationID := uuid.NewString()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)FROM cluster_instances.*ORDER BY node_id ASC.*FOR UPDATE`).
		WithArgs(input.DeploymentID, int64(30), int64(300)).
		WillReturnRows(clusterDrainCandidateMockRows().
			AddRow("pixel-app-01", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-02", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-03", "active", "ready", "ready", true, true, true, true))
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows().AddRow(
			operationID,
			input.DeploymentID,
			input.IdempotencyKey,
			input.RequestFingerprint,
			service.ClusterOperationTypeDrain,
			input.TargetNodeID,
			"",
			input.Reason,
			input.ActorUserID,
			input.ActorName,
			service.ClusterOperationStatusPending,
			int64(0),
			"",
			"",
			nil,
			nil,
			nil,
			"",
			"",
			now,
			now,
		))
	mock.ExpectCommit()

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateDrainOperationSafely(
		context.Background(),
		input,
		2,
		30*time.Second,
		5*time.Minute,
	)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, operationID, operation.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateDrainOperationSafelyRejectsIdempotencyFingerprintConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	input := clusterDrainOperationInput()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows().AddRow(
			uuid.NewString(),
			input.DeploymentID,
			input.IdempotencyKey,
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			service.ClusterOperationTypeDrain,
			input.TargetNodeID,
			"",
			input.Reason,
			input.ActorUserID,
			input.ActorName,
			service.ClusterOperationStatusPending,
			int64(0),
			"",
			"",
			nil,
			nil,
			nil,
			"",
			"",
			now,
			now,
		))
	mock.ExpectRollback()

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateDrainOperationSafely(
		context.Background(),
		input,
		2,
		30*time.Second,
		5*time.Minute,
	)
	require.Nil(t, operation)
	require.False(t, created)
	require.ErrorIs(t, err, service.ErrClusterOperationConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCreateDrainOperationSafelyReservesPendingDrainCapacity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	input := clusterDrainOperationInput()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)FROM cluster_instances.*ORDER BY node_id ASC.*FOR UPDATE`).
		WithArgs(input.DeploymentID, int64(30), int64(300)).
		WillReturnRows(clusterDrainCandidateMockRows().
			AddRow("pixel-app-01", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-02", "active", "ready", "ready", true, true, true, true).
			AddRow("pixel-app-03", "active", "ready", "ready", true, true, true, true))
	mock.ExpectQuery(`(?s)SELECT.*FROM cluster_operations.*idempotency_key = \$2::uuid`).
		WithArgs(input.DeploymentID, input.IdempotencyKey).
		WillReturnRows(clusterOperationMockRows())
	mock.ExpectQuery(`(?s)SELECT target_node_id.*FROM cluster_operations.*status IN \('pending', 'running'\)`).
		WithArgs(input.DeploymentID).
		WillReturnRows(sqlmock.NewRows([]string{"target_node_id"}).AddRow("pixel-app-02"))
	mock.ExpectRollback()

	repo := NewClusterRepository(db)
	operation, created, err := repo.CreateDrainOperationSafely(
		context.Background(),
		input,
		2,
		30*time.Second,
		5*time.Minute,
	)
	require.Nil(t, operation)
	require.False(t, created)
	require.ErrorIs(t, err, service.ErrClusterDrainCapacityUnsafe)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryClaimPendingOperationsUsesSkipLockedAndAttemptFence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	operationID := uuid.NewString()
	idempotencyKey := uuid.NewString()
	bootID := uuid.NewString()
	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)FOR UPDATE SKIP LOCKED.*attempt_token = operation\.attempt_token \+ 1.*claim_expires_at = clock_timestamp`).
		WithArgs("pixel-prod", "pixel-app-01", bootID, 10, int64(60)).
		WillReturnRows(clusterOperationMockRows().AddRow(
			operationID,
			"pixel-prod",
			idempotencyKey,
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			service.ClusterOperationTypeCacheRefresh,
			"",
			service.ClusterCacheKeyChannelRouting,
			"刷新渠道路由安全缓存",
			int64(1),
			"admin",
			service.ClusterOperationStatusRunning,
			int64(1),
			"pixel-app-01",
			bootID,
			now.Add(time.Minute),
			now,
			nil,
			"",
			"",
			now,
			now,
		))

	repo := NewClusterRepository(db)
	operations, err := repo.ClaimPendingOperations(
		context.Background(),
		"pixel-prod",
		"pixel-app-01",
		bootID,
		10,
		time.Minute,
	)
	require.NoError(t, err)
	require.Len(t, operations, 1)
	require.Equal(t, int64(1), operations[0].AttemptToken)
	require.Equal(t, bootID, operations[0].ClaimedByBootID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryBumpCacheVersionIsAtomic(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)INSERT INTO cluster_cache_versions.*ON CONFLICT.*version = cluster_cache_versions\.version \+ 1.*RETURNING`).
		WithArgs("pixel-prod", service.ClusterCacheKeyRuntimeSettings, "pixel-app-01").
		WillReturnRows(sqlmock.NewRows([]string{
			"deployment_id",
			"cache_key",
			"version",
			"updated_by_node_id",
			"updated_at",
		}).AddRow(
			"pixel-prod",
			service.ClusterCacheKeyRuntimeSettings,
			int64(8),
			"pixel-app-01",
			now,
		))

	repo := NewClusterRepository(db)
	version, err := repo.BumpCacheVersion(
		context.Background(),
		"pixel-prod",
		service.ClusterCacheKeyRuntimeSettings,
		"pixel-app-01",
	)
	require.NoError(t, err)
	require.Equal(t, int64(8), version.Version)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryEnsureCacheVersionsDoesNotIncrementExistingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`(?s)INSERT INTO cluster_cache_versions.*channel_routing.*runtime_settings.*policy_metadata.*ON CONFLICT.*DO NOTHING`).
		WithArgs("pixel-prod", "pixel-app-01").
		WillReturnResult(sqlmock.NewResult(0, 2))

	repo := NewClusterRepository(db)
	err = repo.EnsureCacheVersions(context.Background(), "pixel-prod", "pixel-app-01")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryHeartbeatRejectsInvalidExtendedMetricsBeforeQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := NewClusterRepository(db)
	_, err = repo.Heartbeat(context.Background(), service.ClusterInstanceHeartbeat{
		DeploymentID:     "pixel-prod",
		NodeID:           "pixel-app-01",
		BootID:           uuid.NewString(),
		ObservedState:    service.ClusterObservedStateReady,
		GoroutineCount:   -1,
		CacheVersions:    map[string]int64{service.ClusterCacheKeyChannelRouting: 1},
		DatabaseHealthy:  true,
		RedisHealthy:     true,
		CacheHealthy:     true,
		MigrationHealthy: true,
	})
	require.ErrorContains(t, err, "metrics must be non-negative")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryHeartbeatRejectsUnsafeCacheVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := NewClusterRepository(db)
	_, err = repo.Heartbeat(context.Background(), service.ClusterInstanceHeartbeat{
		DeploymentID:  "pixel-prod",
		NodeID:        "pixel-app-01",
		BootID:        uuid.NewString(),
		ObservedState: service.ClusterObservedStateReady,
		CacheVersions: map[string]int64{"session": 1},
	})
	require.ErrorContains(t, err, "invalid cache_key")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryHeartbeatPersistsAndScansExtendedMetrics(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	bootID := uuid.NewString()
	now := time.Now().UTC()
	cacheVersionsJSON := []byte(`{"channel_routing":4,"runtime_settings":2}`)
	heartbeat := service.ClusterInstanceHeartbeat{
		DeploymentID:         "pixel-prod",
		NodeID:               "pixel-app-01",
		BootID:               bootID,
		ObservedState:        service.ClusterObservedStateReady,
		CPUPercent:           23.5,
		RSSBytes:             1024,
		MemoryLimitBytes:     2048,
		GoroutineCount:       42,
		FDOpen:               18,
		FDLimit:              65535,
		ActiveHTTP:           7,
		ActiveSSE:            2,
		ActiveWebSocket:      3,
		DBOpenConnections:    12,
		DBInUseConnections:   4,
		DBIdleConnections:    8,
		DBWaitCount:          9,
		DBMaxOpenConnections: 50,
		RedisPoolConnections: 24,
		RedisIdleConnections: 16,
		RedisPoolSize:        128,
		CacheVersions: map[string]int64{
			service.ClusterCacheKeyChannelRouting:  4,
			service.ClusterCacheKeyRuntimeSettings: 2,
		},
		DatabaseHealthy:  true,
		RedisHealthy:     true,
		CacheHealthy:     true,
		MigrationHealthy: true,
	}

	mock.ExpectQuery(`(?s)UPDATE cluster_instances.*memory_limit_bytes = \$7.*cache_versions = \$22::jsonb.*RETURNING`).
		WithArgs(
			heartbeat.DeploymentID,
			heartbeat.NodeID,
			heartbeat.BootID,
			heartbeat.ObservedState,
			heartbeat.CPUPercent,
			heartbeat.RSSBytes,
			heartbeat.MemoryLimitBytes,
			heartbeat.GoroutineCount,
			heartbeat.FDOpen,
			heartbeat.FDLimit,
			heartbeat.ActiveHTTP,
			heartbeat.ActiveSSE,
			heartbeat.ActiveWebSocket,
			heartbeat.DBOpenConnections,
			heartbeat.DBInUseConnections,
			heartbeat.DBIdleConnections,
			heartbeat.DBWaitCount,
			heartbeat.DBMaxOpenConnections,
			heartbeat.RedisPoolConnections,
			heartbeat.RedisIdleConnections,
			heartbeat.RedisPoolSize,
			cacheVersionsJSON,
			true,
			true,
			true,
			true,
			"",
		).
		WillReturnRows(clusterInstanceMockRows().AddRow(
			heartbeat.DeploymentID,
			heartbeat.NodeID,
			bootID,
			service.ClusterDesiredStateActive,
			service.ClusterObservedStateReady,
			service.ClusterObservedStateReady,
			"app-01",
			"1.2.3",
			"abc",
			"2026-07-23",
			"config-fingerprint",
			"secret-fingerprint",
			cacheVersionsJSON,
			now,
			now,
			now,
			heartbeat.CPUPercent,
			heartbeat.RSSBytes,
			heartbeat.MemoryLimitBytes,
			heartbeat.GoroutineCount,
			heartbeat.FDOpen,
			heartbeat.FDLimit,
			heartbeat.ActiveHTTP,
			heartbeat.ActiveSSE,
			heartbeat.ActiveWebSocket,
			heartbeat.DBOpenConnections,
			heartbeat.DBInUseConnections,
			heartbeat.DBIdleConnections,
			heartbeat.DBWaitCount,
			heartbeat.DBMaxOpenConnections,
			heartbeat.RedisPoolConnections,
			heartbeat.RedisIdleConnections,
			heartbeat.RedisPoolSize,
			true,
			true,
			true,
			true,
			"",
			now,
			now,
		))

	repo := NewClusterRepository(db)
	instance, err := repo.Heartbeat(context.Background(), heartbeat)
	require.NoError(t, err)
	require.Equal(t, int64(2048), instance.MemoryLimitBytes)
	require.Equal(t, int64(42), instance.GoroutineCount)
	require.Equal(t, 8, instance.DBIdleConnections)
	require.Equal(t, int64(9), instance.DBWaitCount)
	require.Equal(t, 128, instance.RedisPoolSize)
	require.Equal(t, int64(4), instance.CacheVersions[service.ClusterCacheKeyChannelRouting])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryValidatesOfflineThresholdBeforeQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := NewClusterRepository(db)
	_, err = repo.ListInstances(context.Background(), "pixel-prod", 30*time.Second, 30*time.Second)
	require.ErrorContains(t, err, "offline_after must be greater")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryDeleteOfflineInstancesUsesDatabaseTime(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`(?s)DELETE FROM cluster_instances.*heartbeat_at <= clock_timestamp\(\) - \(\$2 \* INTERVAL '1 second'\)`).
		WithArgs("pixel-prod", int64((30*24*time.Hour)/time.Second)).
		WillReturnResult(sqlmock.NewResult(0, 2))

	repo := NewClusterRepository(db)
	deleted, err := repo.DeleteOfflineInstances(
		context.Background(),
		"pixel-prod",
		30*24*time.Hour,
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryDeleteOfflineInstancesRejectsInvalidRetention(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := NewClusterRepository(db)
	_, err = repo.DeleteOfflineInstances(context.Background(), "pixel-prod", 0)
	require.ErrorContains(t, err, "offline_instance_retention")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClusterRepositoryCompleteOperationRejectsInvalidAttemptWithoutQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := NewClusterRepository(db)
	completed, err := repo.CompleteOperation(
		context.Background(),
		"pixel-prod",
		uuid.NewString(),
		"pixel-app-01",
		uuid.NewString(),
		0,
		true,
		"",
		"",
	)
	require.False(t, completed)
	require.True(t, errors.Is(err, service.ErrClusterOperationOwnerLost))
	require.NoError(t, mock.ExpectationsWereMet())
}

func clusterOperationMockRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"deployment_id",
		"idempotency_key",
		"request_fingerprint",
		"operation_type",
		"target_node_id",
		"cache_scope",
		"reason",
		"actor_user_id",
		"actor_name",
		"status",
		"attempt_token",
		"claimed_by_node_id",
		"claimed_by_boot_id",
		"claim_expires_at",
		"claimed_at",
		"completed_at",
		"result",
		"error_message",
		"created_at",
		"updated_at",
	})
}

func clusterDrainOperationInput() service.CreateClusterOperationInput {
	return service.CreateClusterOperationInput{
		DeploymentID:       "pixel-prod",
		IdempotencyKey:     uuid.NewString(),
		RequestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Type:               service.ClusterOperationTypeDrain,
		TargetNodeID:       "pixel-app-01",
		Reason:             "集群节点摘流操作",
		ActorUserID:        1,
		ActorName:          "admin",
	}
}

func clusterDrainCandidateMockRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"node_id",
		"desired_state",
		"observed_state",
		"derived_state",
		"database_healthy",
		"redis_healthy",
		"cache_healthy",
		"migration_healthy",
	})
}

func clusterInstanceMockRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"deployment_id",
		"node_id",
		"boot_id",
		"desired_state",
		"observed_state",
		"derived_state",
		"hostname",
		"version",
		"commit_sha",
		"build_date",
		"config_fingerprint",
		"secret_fingerprint",
		"cache_versions",
		"started_at",
		"heartbeat_at",
		"database_time",
		"cpu_percent",
		"rss_bytes",
		"memory_limit_bytes",
		"goroutine_count",
		"fd_open",
		"fd_limit",
		"active_http",
		"active_sse",
		"active_websocket",
		"db_open_connections",
		"db_in_use_connections",
		"db_idle_connections",
		"db_wait_count",
		"db_max_open_connections",
		"redis_pool_connections",
		"redis_idle_connections",
		"redis_pool_size",
		"database_healthy",
		"redis_healthy",
		"cache_healthy",
		"migration_healthy",
		"last_error",
		"created_at",
		"updated_at",
	})
}
