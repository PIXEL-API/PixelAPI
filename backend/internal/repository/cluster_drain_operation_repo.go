package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type clusterDrainCandidate struct {
	nodeID           string
	desiredState     string
	observedState    string
	derivedState     string
	databaseHealthy  bool
	redisHealthy     bool
	cacheHealthy     bool
	migrationHealthy bool
}

func (r *clusterRepository) CreateDrainOperationSafely(
	ctx context.Context,
	input service.CreateClusterOperationInput,
	minimumReadyAfterDrain int,
	staleAfter time.Duration,
	offlineAfter time.Duration,
) (*service.ClusterOperation, bool, error) {
	if err := r.validate(); err != nil {
		return nil, false, err
	}
	prepared, err := prepareCreateClusterOperation(input)
	if err != nil {
		return nil, false, err
	}
	if prepared.Type != service.ClusterOperationTypeDrain {
		return nil, false, errors.New("safe drain operation requires operation_type drain")
	}
	if minimumReadyAfterDrain <= 0 {
		return nil, false, errors.New("minimum_ready_after_drain must be positive")
	}
	staleSeconds, offlineSeconds, err := validateClusterStatusDurations(staleAfter, offlineAfter)
	if err != nil {
		return nil, false, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Idempotent retries must return the original audit even after the target has
	// already started draining. Fingerprints are immutable, so this check is
	// safe before acquiring the deployment-wide instance locks.
	existing, err := queryOperationByIdempotencyKey(
		ctx,
		tx,
		prepared.DeploymentID,
		prepared.IdempotencyKey,
	)
	switch {
	case err == nil:
		if existing.RequestFingerprint != prepared.RequestFingerprint {
			return nil, false, service.ErrClusterOperationConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, false, err
	}

	instances, err := lockClusterDrainCandidates(
		ctx,
		tx,
		prepared.DeploymentID,
		staleSeconds,
		offlineSeconds,
	)
	if err != nil {
		return nil, false, err
	}

	// A concurrent retry may have inserted the operation while this transaction
	// waited for the deployment row locks. READ COMMITTED gives this statement a
	// fresh snapshot, so re-check before treating the already-reserved target as
	// a capacity conflict.
	existing, err = queryOperationByIdempotencyKey(
		ctx,
		tx,
		prepared.DeploymentID,
		prepared.IdempotencyKey,
	)
	switch {
	case err == nil:
		if existing.RequestFingerprint != prepared.RequestFingerprint {
			return nil, false, service.ErrClusterOperationConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return existing, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, false, err
	}

	var target *clusterDrainCandidate
	readyExcludingTarget := make(map[string]struct{}, len(instances))
	for i := range instances {
		instance := &instances[i]
		if instance.nodeID == prepared.TargetNodeID {
			target = instance
			continue
		}
		if clusterDrainCandidateReady(instance) {
			readyExcludingTarget[instance.nodeID] = struct{}{}
		}
	}
	if target == nil {
		return nil, false, service.ErrClusterInstanceNotFound
	}
	if !clusterDrainCandidateReady(target) || len(readyExcludingTarget) < minimumReadyAfterDrain {
		return nil, false, service.ErrClusterDrainCapacityUnsafe
	}

	// A pending/running drain already reserves that node's ready capacity. The
	// instance row locks serialize all safe-drain creators for this deployment;
	// this additional read prevents two sequentially committed commands from
	// both counting the same remaining ready nodes.
	reservedNodes, err := listReservedDrainNodes(ctx, tx, prepared.DeploymentID)
	if err != nil {
		return nil, false, err
	}
	if _, reserved := reservedNodes[prepared.TargetNodeID]; reserved {
		return nil, false, service.ErrClusterDrainCapacityUnsafe
	}
	for nodeID := range reservedNodes {
		delete(readyExcludingTarget, nodeID)
	}
	if len(readyExcludingTarget) < minimumReadyAfterDrain {
		return nil, false, service.ErrClusterDrainCapacityUnsafe
	}

	operation, created, err := createClusterOperation(ctx, tx, prepared)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return operation, created, nil
}

func lockClusterDrainCandidates(
	ctx context.Context,
	tx *sql.Tx,
	deploymentID string,
	staleSeconds, offlineSeconds int64,
) ([]clusterDrainCandidate, error) {
	// Every safe drain locks the same deployment rows in node_id order. This is
	// both the capacity serialization point and the deadlock-avoidance order.
	rows, err := tx.QueryContext(ctx, `
		SELECT
			node_id,
			desired_state,
			observed_state,
			CASE
				WHEN heartbeat_at <= statement_timestamp() - ($3 * INTERVAL '1 second')
					THEN 'offline'
				WHEN heartbeat_at <= statement_timestamp() - ($2 * INTERVAL '1 second')
					THEN 'stale'
				ELSE observed_state
			END AS derived_state,
			database_healthy,
			redis_healthy,
			cache_healthy,
			migration_healthy
		FROM cluster_instances
		WHERE deployment_id = $1
		ORDER BY node_id ASC
		FOR UPDATE
	`, deploymentID, staleSeconds, offlineSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	instances := make([]clusterDrainCandidate, 0)
	for rows.Next() {
		var instance clusterDrainCandidate
		if err := rows.Scan(
			&instance.nodeID,
			&instance.desiredState,
			&instance.observedState,
			&instance.derivedState,
			&instance.databaseHealthy,
			&instance.redisHealthy,
			&instance.cacheHealthy,
			&instance.migrationHealthy,
		); err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return instances, nil
}

func clusterDrainCandidateReady(instance *clusterDrainCandidate) bool {
	return instance != nil &&
		instance.desiredState == service.ClusterDesiredStateActive &&
		instance.observedState == service.ClusterObservedStateReady &&
		instance.derivedState == service.ClusterObservedStateReady &&
		instance.databaseHealthy &&
		instance.redisHealthy &&
		instance.cacheHealthy &&
		instance.migrationHealthy
}

func listReservedDrainNodes(ctx context.Context, tx *sql.Tx, deploymentID string) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT target_node_id
		FROM cluster_operations
		WHERE deployment_id = $1
			AND operation_type = 'drain'
			AND status IN ('pending', 'running')
			AND target_node_id IS NOT NULL
		ORDER BY target_node_id ASC, id ASC
	`, deploymentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	reserved := make(map[string]struct{})
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		reserved[nodeID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reserved, nil
}
