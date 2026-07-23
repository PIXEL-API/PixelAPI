package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const clusterInstanceReturningColumns = `
	deployment_id,
	node_id,
	boot_id::text,
	desired_state,
	observed_state,
	observed_state AS derived_state,
	hostname,
	version,
	commit_sha,
	build_date,
	config_fingerprint,
	secret_fingerprint,
	cache_versions,
	started_at,
	heartbeat_at,
	clock_timestamp() AS database_time,
	cpu_percent,
	rss_bytes,
	memory_limit_bytes,
	goroutine_count,
	fd_open,
	fd_limit,
	active_http,
	active_sse,
	active_websocket,
	db_open_connections,
	db_in_use_connections,
	db_idle_connections,
	db_wait_count,
	db_max_open_connections,
	redis_pool_connections,
	redis_idle_connections,
	redis_pool_size,
	database_healthy,
	redis_healthy,
	cache_healthy,
	migration_healthy,
	last_error,
	created_at,
	updated_at`

func (r *clusterRepository) ClaimInstance(ctx context.Context, heartbeat service.ClusterInstanceHeartbeat, nodeTTL time.Duration) error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := validateClusterHeartbeatIdentity(heartbeat); err != nil {
		return err
	}
	ttlSeconds, err := clusterDurationSeconds("node_ttl", nodeTTL)
	if err != nil {
		return err
	}

	var claimed int
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO cluster_instances (
			deployment_id,
			node_id,
			boot_id,
			desired_state,
			observed_state,
			hostname,
			version,
			commit_sha,
			build_date,
			config_fingerprint,
			secret_fingerprint
		) VALUES (
			$1, $2, $3::uuid, 'active', 'starting', $4, $5, $6, $7, $8, $9
		)
		ON CONFLICT (deployment_id, node_id) DO UPDATE
		SET
			boot_id = EXCLUDED.boot_id,
			observed_state = 'starting',
			hostname = EXCLUDED.hostname,
			version = EXCLUDED.version,
			commit_sha = EXCLUDED.commit_sha,
			build_date = EXCLUDED.build_date,
			config_fingerprint = EXCLUDED.config_fingerprint,
			secret_fingerprint = EXCLUDED.secret_fingerprint,
			cache_versions = '{}'::jsonb,
			started_at = CASE
				WHEN cluster_instances.boot_id = EXCLUDED.boot_id
					THEN cluster_instances.started_at
				ELSE clock_timestamp()
			END,
			heartbeat_at = clock_timestamp(),
			cpu_percent = 0,
			rss_bytes = 0,
			memory_limit_bytes = 0,
			goroutine_count = 0,
			fd_open = 0,
			fd_limit = 0,
			active_http = 0,
			active_sse = 0,
			active_websocket = 0,
			db_open_connections = 0,
			db_in_use_connections = 0,
			db_idle_connections = 0,
			db_wait_count = 0,
			db_max_open_connections = 0,
			redis_pool_connections = 0,
			redis_idle_connections = 0,
			redis_pool_size = 0,
			database_healthy = FALSE,
			redis_healthy = FALSE,
			cache_healthy = FALSE,
			migration_healthy = FALSE,
			last_error = '',
			updated_at = clock_timestamp()
		WHERE cluster_instances.boot_id = EXCLUDED.boot_id
			OR cluster_instances.heartbeat_at
				<= clock_timestamp() - ($10 * INTERVAL '1 second')
		RETURNING 1
	`,
		heartbeat.DeploymentID,
		heartbeat.NodeID,
		heartbeat.BootID,
		heartbeat.Hostname,
		heartbeat.Version,
		heartbeat.CommitSHA,
		heartbeat.BuildDate,
		heartbeat.ConfigFingerprint,
		heartbeat.SecretFingerprint,
		ttlSeconds,
	).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrClusterNodeConflict
	}
	if err != nil {
		return err
	}
	if claimed != 1 {
		return service.ErrClusterNodeConflict
	}
	return nil
}

func (r *clusterRepository) Heartbeat(ctx context.Context, heartbeat service.ClusterInstanceHeartbeat) (*service.ClusterInstance, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterHeartbeatIdentity(heartbeat); err != nil {
		return nil, err
	}
	if err := validateClusterState(
		heartbeat.ObservedState,
		service.ClusterObservedStateStarting,
		service.ClusterObservedStateReady,
		service.ClusterObservedStateDraining,
		service.ClusterObservedStateUnhealthy,
	); err != nil {
		return nil, err
	}
	cacheVersionsJSON, err := encodeClusterCacheVersions(heartbeat.CacheVersions)
	if err != nil {
		return nil, err
	}
	if heartbeat.CPUPercent < 0 ||
		heartbeat.RSSBytes < 0 ||
		heartbeat.MemoryLimitBytes < 0 ||
		heartbeat.GoroutineCount < 0 ||
		heartbeat.FDOpen < 0 ||
		heartbeat.FDLimit < 0 ||
		heartbeat.ActiveHTTP < 0 ||
		heartbeat.ActiveSSE < 0 ||
		heartbeat.ActiveWebSocket < 0 ||
		heartbeat.DBOpenConnections < 0 ||
		heartbeat.DBInUseConnections < 0 ||
		heartbeat.DBIdleConnections < 0 ||
		heartbeat.DBWaitCount < 0 ||
		heartbeat.DBMaxOpenConnections < 0 ||
		heartbeat.RedisPoolConnections < 0 ||
		heartbeat.RedisIdleConnections < 0 ||
		heartbeat.RedisPoolSize < 0 {
		return nil, errors.New("cluster heartbeat metrics must be non-negative")
	}

	instance, err := clusterQueryOne(
		ctx,
		r.db,
		`
		UPDATE cluster_instances
		SET
			observed_state = $4,
			heartbeat_at = clock_timestamp(),
			cpu_percent = $5,
			rss_bytes = $6,
			memory_limit_bytes = $7,
			goroutine_count = $8,
			fd_open = $9,
			fd_limit = $10,
			active_http = $11,
			active_sse = $12,
			active_websocket = $13,
			db_open_connections = $14,
			db_in_use_connections = $15,
			db_idle_connections = $16,
			db_wait_count = $17,
			db_max_open_connections = $18,
			redis_pool_connections = $19,
			redis_idle_connections = $20,
			redis_pool_size = $21,
			cache_versions = $22::jsonb,
			database_healthy = $23,
			redis_healthy = $24,
			cache_healthy = $25,
			migration_healthy = $26,
			last_error = $27,
			updated_at = clock_timestamp()
		WHERE deployment_id = $1
			AND node_id = $2
			AND boot_id = $3::uuid
		RETURNING `+clusterInstanceReturningColumns,
		[]any{
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
			heartbeat.DatabaseHealthy,
			heartbeat.RedisHealthy,
			heartbeat.CacheHealthy,
			heartbeat.MigrationHealthy,
			heartbeat.LastError,
		},
		scanClusterInstance,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrClusterInstanceOwnerLost
	}
	return instance, err
}

func (r *clusterRepository) SetInstanceDesiredState(ctx context.Context, deploymentID, nodeID, desiredState string) (*service.ClusterInstance, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("node_id", nodeID); err != nil {
		return nil, err
	}
	if err := validateClusterState(
		desiredState,
		service.ClusterDesiredStateActive,
		service.ClusterDesiredStateDraining,
	); err != nil {
		return nil, err
	}

	instance, err := clusterQueryOne(
		ctx,
		r.db,
		`
		UPDATE cluster_instances
		SET desired_state = $3, updated_at = clock_timestamp()
		WHERE deployment_id = $1 AND node_id = $2
		RETURNING `+clusterInstanceReturningColumns,
		[]any{deploymentID, nodeID, desiredState},
		scanClusterInstance,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrClusterInstanceNotFound
	}
	return instance, err
}

func (r *clusterRepository) ListInstances(ctx context.Context, deploymentID string, staleAfter, offlineAfter time.Duration) ([]service.ClusterInstance, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}
	staleSeconds, offlineSeconds, err := validateClusterStatusDurations(staleAfter, offlineAfter)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, clusterInstanceListQuery(""), deploymentID, staleSeconds, offlineSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	instances := make([]service.ClusterInstance, 0)
	for rows.Next() {
		instance, scanErr := scanClusterInstance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		instances = append(instances, *instance)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return instances, nil
}

func (r *clusterRepository) GetInstance(ctx context.Context, deploymentID, nodeID string, staleAfter, offlineAfter time.Duration) (*service.ClusterInstance, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("node_id", nodeID); err != nil {
		return nil, err
	}
	staleSeconds, offlineSeconds, err := validateClusterStatusDurations(staleAfter, offlineAfter)
	if err != nil {
		return nil, err
	}

	instance, err := clusterQueryOne(
		ctx,
		r.db,
		clusterInstanceListQuery(" AND node_id = $4"),
		[]any{deploymentID, staleSeconds, offlineSeconds, nodeID},
		scanClusterInstance,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrClusterInstanceNotFound
	}
	return instance, err
}

func (r *clusterRepository) DeleteOfflineInstances(
	ctx context.Context,
	deploymentID string,
	retention time.Duration,
) (int64, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return 0, err
	}
	retentionSeconds, err := clusterDurationSeconds("offline_instance_retention", retention)
	if err != nil {
		return 0, err
	}
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM cluster_instances
		WHERE deployment_id = $1
			AND heartbeat_at <= clock_timestamp() - ($2 * INTERVAL '1 second')
	`, deploymentID, retentionSeconds)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func validateClusterHeartbeatIdentity(heartbeat service.ClusterInstanceHeartbeat) error {
	if err := validateClusterRequired("deployment_id", heartbeat.DeploymentID); err != nil {
		return err
	}
	if err := validateClusterRequired("node_id", heartbeat.NodeID); err != nil {
		return err
	}
	if err := validateClusterRequired("boot_id", heartbeat.BootID); err != nil {
		return err
	}
	if _, err := uuid.Parse(heartbeat.BootID); err != nil {
		return fmt.Errorf("invalid boot_id: %w", err)
	}
	return nil
}

func validateClusterStatusDurations(staleAfter, offlineAfter time.Duration) (int64, int64, error) {
	staleSeconds, err := clusterDurationSeconds("stale_after", staleAfter)
	if err != nil {
		return 0, 0, err
	}
	offlineSeconds, err := clusterDurationSeconds("offline_after", offlineAfter)
	if err != nil {
		return 0, 0, err
	}
	if offlineSeconds <= staleSeconds {
		return 0, 0, errors.New("offline_after must be greater than stale_after")
	}
	return staleSeconds, offlineSeconds, nil
}

func clusterInstanceListQuery(extraWhere string) string {
	return fmt.Sprintf(`
		SELECT
			deployment_id,
			node_id,
			boot_id::text,
			desired_state,
			observed_state,
			CASE
				WHEN heartbeat_at <= statement_timestamp() - ($3 * INTERVAL '1 second')
					THEN 'offline'
				WHEN heartbeat_at <= statement_timestamp() - ($2 * INTERVAL '1 second')
					THEN 'stale'
				ELSE observed_state
			END AS derived_state,
			hostname,
			version,
			commit_sha,
			build_date,
			config_fingerprint,
			secret_fingerprint,
			cache_versions,
			started_at,
			heartbeat_at,
			statement_timestamp() AS database_time,
			cpu_percent,
			rss_bytes,
			memory_limit_bytes,
			goroutine_count,
			fd_open,
			fd_limit,
			active_http,
			active_sse,
			active_websocket,
			db_open_connections,
			db_in_use_connections,
			db_idle_connections,
			db_wait_count,
			db_max_open_connections,
			redis_pool_connections,
			redis_idle_connections,
			redis_pool_size,
			database_healthy,
			redis_healthy,
			cache_healthy,
			migration_healthy,
			last_error,
			created_at,
			updated_at
		FROM cluster_instances
		WHERE deployment_id = $1%s
		ORDER BY node_id ASC
	`, extraWhere)
}

func scanClusterInstance(scanner clusterRowScanner) (*service.ClusterInstance, error) {
	var (
		instance         service.ClusterInstance
		cacheVersionsRaw []byte
	)
	err := scanner.Scan(
		&instance.DeploymentID,
		&instance.NodeID,
		&instance.BootID,
		&instance.DesiredState,
		&instance.ObservedState,
		&instance.DerivedState,
		&instance.Hostname,
		&instance.Version,
		&instance.CommitSHA,
		&instance.BuildDate,
		&instance.ConfigFingerprint,
		&instance.SecretFingerprint,
		&cacheVersionsRaw,
		&instance.StartedAt,
		&instance.HeartbeatAt,
		&instance.DatabaseTime,
		&instance.CPUPercent,
		&instance.RSSBytes,
		&instance.MemoryLimitBytes,
		&instance.GoroutineCount,
		&instance.FDOpen,
		&instance.FDLimit,
		&instance.ActiveHTTP,
		&instance.ActiveSSE,
		&instance.ActiveWebSocket,
		&instance.DBOpenConnections,
		&instance.DBInUseConnections,
		&instance.DBIdleConnections,
		&instance.DBWaitCount,
		&instance.DBMaxOpenConnections,
		&instance.RedisPoolConnections,
		&instance.RedisIdleConnections,
		&instance.RedisPoolSize,
		&instance.DatabaseHealthy,
		&instance.RedisHealthy,
		&instance.CacheHealthy,
		&instance.MigrationHealthy,
		&instance.LastError,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(cacheVersionsRaw, &instance.CacheVersions); err != nil {
		return nil, fmt.Errorf("decode cluster cache_versions: %w", err)
	}
	if instance.CacheVersions == nil {
		return nil, errors.New("cluster cache_versions must be a JSON object")
	}
	return &instance, nil
}

func encodeClusterCacheVersions(versions map[string]int64) ([]byte, error) {
	if versions == nil {
		versions = map[string]int64{}
	}
	for cacheKey, version := range versions {
		if err := validateClusterCacheKey(cacheKey); err != nil {
			return nil, err
		}
		if version < 0 {
			return nil, fmt.Errorf("cache version for %q must be non-negative", cacheKey)
		}
	}
	encoded, err := json.Marshal(versions)
	if err != nil {
		return nil, fmt.Errorf("encode cluster cache_versions: %w", err)
	}
	return encoded, nil
}
