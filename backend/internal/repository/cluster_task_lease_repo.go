package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const clusterTaskLeaseColumns = `
	deployment_id,
	task_name,
	COALESCE(owner_node_id, ''),
	COALESCE(owner_boot_id::text, ''),
	fencing_token,
	lease_expires_at,
	last_acquired_at,
	last_renewed_at,
	last_released_at,
	last_success_at,
	last_error,
	last_duration_ms,
	clock_timestamp() AS database_time,
	created_at,
	updated_at`

func (r *clusterRepository) AcquireTaskLease(
	ctx context.Context,
	deploymentID, taskName, nodeID, bootID string,
	leaseDuration time.Duration,
) (*service.ClusterTaskLease, bool, error) {
	if err := r.validate(); err != nil {
		return nil, false, err
	}
	if err := validateClusterLeaseIdentity(deploymentID, taskName, nodeID, bootID); err != nil {
		return nil, false, err
	}
	leaseSeconds, err := clusterDurationSeconds("lease_duration", leaseDuration)
	if err != nil {
		return nil, false, err
	}

	lease, err := clusterQueryOne(
		ctx,
		r.db,
		`
		INSERT INTO cluster_task_leases (
			deployment_id,
			task_name,
			owner_node_id,
			owner_boot_id,
			fencing_token,
			lease_expires_at,
			last_acquired_at,
			last_renewed_at
		) VALUES (
			$1,
			$2,
			$3,
			$4::uuid,
			1,
			clock_timestamp() + ($5 * INTERVAL '1 second'),
			clock_timestamp(),
			clock_timestamp()
		)
		ON CONFLICT (deployment_id, task_name) DO UPDATE
		SET
			owner_node_id = EXCLUDED.owner_node_id,
			owner_boot_id = EXCLUDED.owner_boot_id,
			fencing_token = cluster_task_leases.fencing_token + 1,
			lease_expires_at = EXCLUDED.lease_expires_at,
			last_acquired_at = clock_timestamp(),
			last_renewed_at = clock_timestamp(),
			last_error = '',
			updated_at = clock_timestamp()
		WHERE cluster_task_leases.lease_expires_at IS NULL
		OR cluster_task_leases.lease_expires_at <= clock_timestamp()
		RETURNING `+clusterTaskLeaseColumns,
		[]any{deploymentID, taskName, nodeID, bootID, leaseSeconds},
		scanClusterTaskLease,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return lease, true, nil
}

func (r *clusterRepository) RenewTaskLease(
	ctx context.Context,
	deploymentID, taskName, nodeID, bootID string,
	fencingToken int64,
	leaseDuration time.Duration,
) (bool, error) {
	if err := r.validate(); err != nil {
		return false, err
	}
	if err := validateClusterLeaseIdentity(deploymentID, taskName, nodeID, bootID); err != nil {
		return false, err
	}
	if fencingToken <= 0 {
		return false, service.ErrClusterTaskLeaseNotAcquired
	}
	leaseSeconds, err := clusterDurationSeconds("lease_duration", leaseDuration)
	if err != nil {
		return false, err
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE cluster_task_leases
		SET
			lease_expires_at = clock_timestamp() + ($6 * INTERVAL '1 second'),
			last_renewed_at = clock_timestamp(),
			updated_at = clock_timestamp()
		WHERE deployment_id = $1
			AND task_name = $2
			AND owner_node_id = $3
			AND owner_boot_id = $4::uuid
			AND fencing_token = $5
			AND lease_expires_at > clock_timestamp()
	`, deploymentID, taskName, nodeID, bootID, fencingToken, leaseSeconds)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *clusterRepository) ReleaseTaskLease(
	ctx context.Context,
	deploymentID, taskName, nodeID, bootID string,
	fencingToken int64,
	succeeded bool,
	resultError string,
	duration time.Duration,
) (bool, error) {
	if err := r.validate(); err != nil {
		return false, err
	}
	if err := validateClusterLeaseIdentity(deploymentID, taskName, nodeID, bootID); err != nil {
		return false, err
	}
	if fencingToken <= 0 {
		return false, service.ErrClusterTaskLeaseNotAcquired
	}
	if duration < 0 {
		return false, errors.New("task duration must be non-negative")
	}

	result, err := r.db.ExecContext(ctx, `
		UPDATE cluster_task_leases
		SET
			owner_node_id = NULL,
			owner_boot_id = NULL,
			lease_expires_at = NULL,
			last_released_at = clock_timestamp(),
			last_success_at = CASE WHEN $6 THEN clock_timestamp() ELSE last_success_at END,
			last_error = CASE WHEN $6 THEN '' ELSE $7 END,
			last_duration_ms = $8,
			updated_at = clock_timestamp()
		WHERE deployment_id = $1
			AND task_name = $2
			AND owner_node_id = $3
			AND owner_boot_id = $4::uuid
			AND fencing_token = $5
			AND lease_expires_at > clock_timestamp()
	`, deploymentID, taskName, nodeID, bootID, fencingToken, succeeded, resultError, duration.Milliseconds())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *clusterRepository) ListTaskLeases(ctx context.Context, deploymentID string) ([]service.ClusterTaskLease, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+clusterTaskLeaseColumns+`
		FROM cluster_task_leases
		WHERE deployment_id = $1
		ORDER BY task_name ASC
	`, deploymentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	leases := make([]service.ClusterTaskLease, 0)
	for rows.Next() {
		lease, scanErr := scanClusterTaskLease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		leases = append(leases, *lease)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return leases, nil
}

func validateClusterLeaseIdentity(deploymentID, taskName, nodeID, bootID string) error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "deployment_id", value: deploymentID},
		{name: "task_name", value: taskName},
		{name: "node_id", value: nodeID},
		{name: "boot_id", value: bootID},
	}
	for _, field := range fields {
		if err := validateClusterRequired(field.name, field.value); err != nil {
			return err
		}
	}
	if _, err := uuid.Parse(bootID); err != nil {
		return fmt.Errorf("invalid boot_id: %w", err)
	}
	return nil
}

func scanClusterTaskLease(scanner clusterRowScanner) (*service.ClusterTaskLease, error) {
	var (
		lease          service.ClusterTaskLease
		leaseExpiresAt sql.NullTime
		lastAcquiredAt sql.NullTime
		lastRenewedAt  sql.NullTime
		lastReleasedAt sql.NullTime
		lastSuccessAt  sql.NullTime
		lastDurationMs sql.NullInt64
	)
	err := scanner.Scan(
		&lease.DeploymentID,
		&lease.TaskName,
		&lease.OwnerNodeID,
		&lease.OwnerBootID,
		&lease.FencingToken,
		&leaseExpiresAt,
		&lastAcquiredAt,
		&lastRenewedAt,
		&lastReleasedAt,
		&lastSuccessAt,
		&lease.LastError,
		&lastDurationMs,
		&lease.DatabaseTime,
		&lease.CreatedAt,
		&lease.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	lease.LeaseExpiresAt = clusterTimePointer(leaseExpiresAt)
	lease.LastAcquiredAt = clusterTimePointer(lastAcquiredAt)
	lease.LastRenewedAt = clusterTimePointer(lastRenewedAt)
	lease.LastReleasedAt = clusterTimePointer(lastReleasedAt)
	lease.LastSuccessAt = clusterTimePointer(lastSuccessAt)
	if lastDurationMs.Valid {
		value := lastDurationMs.Int64
		lease.LastDurationMs = &value
	}
	return &lease, nil
}

func clusterTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
