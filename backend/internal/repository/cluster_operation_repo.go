package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const clusterOperationColumns = `
	id::text,
	deployment_id,
	idempotency_key::text,
	request_fingerprint,
	operation_type,
	COALESCE(target_node_id, ''),
	COALESCE(cache_scope, ''),
	reason,
	actor_user_id,
	actor_name,
	status,
	attempt_token,
	COALESCE(claimed_by_node_id, ''),
	COALESCE(claimed_by_boot_id::text, ''),
	claim_expires_at,
	claimed_at,
	completed_at,
	result,
	error_message,
	created_at,
	updated_at`

func (r *clusterRepository) CreateOperation(ctx context.Context, input service.CreateClusterOperationInput) (*service.ClusterOperation, bool, error) {
	if err := r.validate(); err != nil {
		return nil, false, err
	}
	prepared, err := prepareCreateClusterOperation(input)
	if err != nil {
		return nil, false, err
	}
	return createClusterOperation(ctx, r.db, prepared)
}

func prepareCreateClusterOperation(input service.CreateClusterOperationInput) (service.CreateClusterOperationInput, error) {
	if err := validateCreateClusterOperation(input); err != nil {
		return service.CreateClusterOperationInput{}, err
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	} else if _, err := uuid.Parse(input.ID); err != nil {
		return service.CreateClusterOperationInput{}, fmt.Errorf("invalid operation id: %w", err)
	}
	return input, nil
}

func createClusterOperation(
	ctx context.Context,
	queryer clusterQueryRower,
	input service.CreateClusterOperationInput,
) (*service.ClusterOperation, bool, error) {
	operation, err := clusterQueryOne(
		ctx,
		queryer,
		`
		INSERT INTO cluster_operations (
			id,
			deployment_id,
			idempotency_key,
			request_fingerprint,
			operation_type,
			target_node_id,
			cache_scope,
			reason,
			actor_user_id,
			actor_name
		) VALUES (
			$1::uuid,
			$2,
			$3::uuid,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10
		)
		ON CONFLICT (deployment_id, idempotency_key) DO NOTHING
		RETURNING `+clusterOperationColumns,
		[]any{
			input.ID,
			input.DeploymentID,
			input.IdempotencyKey,
			input.RequestFingerprint,
			input.Type,
			clusterNullableString(input.TargetNodeID),
			clusterNullableString(input.CacheScope),
			strings.TrimSpace(input.Reason),
			input.ActorUserID,
			strings.TrimSpace(input.ActorName),
		},
		scanClusterOperation,
	)
	if err == nil {
		return operation, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	operation, err = queryOperationByIdempotencyKey(ctx, queryer, input.DeploymentID, input.IdempotencyKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, service.ErrClusterOperationNotFound
		}
		return nil, false, err
	}
	if operation.RequestFingerprint != input.RequestFingerprint {
		return nil, false, service.ErrClusterOperationConflict
	}
	return operation, false, nil
}

func (r *clusterRepository) GetOperation(ctx context.Context, deploymentID, operationID string) (*service.ClusterOperation, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(operationID); err != nil {
		return nil, fmt.Errorf("invalid operation_id: %w", err)
	}

	operation, err := clusterQueryOne(
		ctx,
		r.db,
		`
		SELECT `+clusterOperationColumns+`
		FROM cluster_operations
		WHERE deployment_id = $1 AND id = $2::uuid
		`,
		[]any{deploymentID, operationID},
		scanClusterOperation,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrClusterOperationNotFound
	}
	return operation, err
}

func (r *clusterRepository) ClaimPendingOperations(
	ctx context.Context,
	deploymentID, nodeID, bootID string,
	limit int,
	claimDuration time.Duration,
) ([]service.ClusterOperation, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterWorkerIdentity(deploymentID, nodeID, bootID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	claimSeconds, err := clusterDurationSeconds("claim_duration", claimDuration)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT id AS operation_id
			FROM cluster_operations
			WHERE deployment_id = $1
				AND (
					target_node_id = $2
					OR (operation_type = 'cache_refresh' AND target_node_id IS NULL)
				)
				AND (
					status = 'pending'
					OR (
						status = 'running'
						AND claim_expires_at <= clock_timestamp()
					)
				)
			ORDER BY created_at ASC, id ASC
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		)
		UPDATE cluster_operations AS operation
		SET
			status = 'running',
			attempt_token = operation.attempt_token + 1,
			claimed_by_node_id = $2,
			claimed_by_boot_id = $3::uuid,
			claim_expires_at = clock_timestamp() + ($5 * INTERVAL '1 second'),
			claimed_at = clock_timestamp(),
			completed_at = NULL,
			error_message = '',
			updated_at = clock_timestamp()
		FROM candidates
		WHERE operation.id = candidates.operation_id
		RETURNING `+clusterOperationColumns,
		deploymentID,
		nodeID,
		bootID,
		limit,
		claimSeconds,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	operations := make([]service.ClusterOperation, 0, limit)
	for rows.Next() {
		operation, scanErr := scanClusterOperation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		operations = append(operations, *operation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return operations, nil
}

func (r *clusterRepository) CompleteOperation(
	ctx context.Context,
	deploymentID, operationID, nodeID, bootID string,
	attemptToken int64,
	succeeded bool,
	result, resultError string,
) (bool, error) {
	if err := r.validate(); err != nil {
		return false, err
	}
	if err := validateClusterWorkerIdentity(deploymentID, nodeID, bootID); err != nil {
		return false, err
	}
	if err := validateClusterRequired("operation_id", operationID); err != nil {
		return false, err
	}
	if _, err := uuid.Parse(operationID); err != nil {
		return false, fmt.Errorf("invalid operation_id: %w", err)
	}
	if attemptToken <= 0 {
		return false, service.ErrClusterOperationOwnerLost
	}

	status := service.ClusterOperationStatusFailed
	if succeeded {
		status = service.ClusterOperationStatusSucceeded
		resultError = ""
	}
	execResult, err := r.db.ExecContext(ctx, `
		UPDATE cluster_operations
		SET
			status = $6,
			claim_expires_at = NULL,
			completed_at = clock_timestamp(),
			result = $7,
			error_message = $8,
			updated_at = clock_timestamp()
		WHERE deployment_id = $1
			AND id = $2::uuid
			AND claimed_by_node_id = $3
			AND claimed_by_boot_id = $4::uuid
			AND attempt_token = $5
			AND status = 'running'
			AND claim_expires_at > clock_timestamp()
	`,
		deploymentID,
		operationID,
		nodeID,
		bootID,
		attemptToken,
		status,
		result,
		resultError,
	)
	if err != nil {
		return false, err
	}
	affected, err := execResult.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (r *clusterRepository) ListOperations(ctx context.Context, filter service.ClusterOperationFilter) ([]service.ClusterOperation, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", filter.DeploymentID); err != nil {
		return nil, err
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		return nil, errors.New("offset must be non-negative")
	}

	args := []any{filter.DeploymentID}
	where := []string{"deployment_id = $1"}
	addFilter := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		args = append(args, strings.TrimSpace(value))
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addFilter("status", filter.Status)
	addFilter("operation_type", filter.Type)
	addFilter("target_node_id", filter.TargetNodeID)
	args = append(args, filter.Limit, filter.Offset)

	query := `
		SELECT ` + clusterOperationColumns + `
		FROM cluster_operations
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY created_at DESC, id DESC
		LIMIT $` + fmt.Sprint(len(args)-1) + `
		OFFSET $` + fmt.Sprint(len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	operations := make([]service.ClusterOperation, 0, filter.Limit)
	for rows.Next() {
		operation, scanErr := scanClusterOperation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		operations = append(operations, *operation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return operations, nil
}

func (r *clusterRepository) getOperationByIdempotencyKey(ctx context.Context, deploymentID, idempotencyKey string) (*service.ClusterOperation, error) {
	operation, err := queryOperationByIdempotencyKey(ctx, r.db, deploymentID, idempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrClusterOperationNotFound
	}
	return operation, err
}

func queryOperationByIdempotencyKey(
	ctx context.Context,
	queryer clusterQueryRower,
	deploymentID, idempotencyKey string,
) (*service.ClusterOperation, error) {
	operation, err := clusterQueryOne(
		ctx,
		queryer,
		`
		SELECT `+clusterOperationColumns+`
		FROM cluster_operations
		WHERE deployment_id = $1 AND idempotency_key = $2::uuid
		`,
		[]any{deploymentID, idempotencyKey},
		scanClusterOperation,
	)
	return operation, err
}

func validateCreateClusterOperation(input service.CreateClusterOperationInput) error {
	if err := validateClusterRequired("deployment_id", input.DeploymentID); err != nil {
		return err
	}
	if _, err := uuid.Parse(input.IdempotencyKey); err != nil {
		return fmt.Errorf("invalid idempotency_key: %w", err)
	}
	fingerprint, err := hex.DecodeString(input.RequestFingerprint)
	if err != nil || len(fingerprint) != 32 {
		return errors.New("request_fingerprint must be a 64-character hexadecimal SHA-256 digest")
	}
	switch input.Type {
	case service.ClusterOperationTypeDrain, service.ClusterOperationTypeResume:
		if err := validateClusterRequired("target_node_id", input.TargetNodeID); err != nil {
			return err
		}
		if strings.TrimSpace(input.CacheScope) != "" {
			return errors.New("cache_scope is only valid for cache_refresh operations")
		}
	case service.ClusterOperationTypeCacheRefresh:
		switch input.CacheScope {
		case service.ClusterCacheKeyChannelRouting,
			service.ClusterCacheKeyRuntimeSettings,
			service.ClusterCacheKeyPolicyMetadata,
			service.ClusterCacheScopeAllSafe:
		default:
			return fmt.Errorf("invalid cache_scope %q", input.CacheScope)
		}
	default:
		return fmt.Errorf("invalid operation_type %q", input.Type)
	}
	reasonLength := utf8.RuneCountInString(strings.TrimSpace(input.Reason))
	if reasonLength < 8 || reasonLength > 500 {
		return errors.New("reason must contain between 8 and 500 characters")
	}
	if input.ActorUserID <= 0 {
		return errors.New("actor_user_id must be positive")
	}
	return nil
}

func validateClusterWorkerIdentity(deploymentID, nodeID, bootID string) error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "deployment_id", value: deploymentID},
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

func scanClusterOperation(scanner clusterRowScanner) (*service.ClusterOperation, error) {
	var (
		operation      service.ClusterOperation
		claimExpiresAt sql.NullTime
		claimedAt      sql.NullTime
		completedAt    sql.NullTime
	)
	err := scanner.Scan(
		&operation.ID,
		&operation.DeploymentID,
		&operation.IdempotencyKey,
		&operation.RequestFingerprint,
		&operation.Type,
		&operation.TargetNodeID,
		&operation.CacheScope,
		&operation.Reason,
		&operation.ActorUserID,
		&operation.ActorName,
		&operation.Status,
		&operation.AttemptToken,
		&operation.ClaimedByNodeID,
		&operation.ClaimedByBootID,
		&claimExpiresAt,
		&claimedAt,
		&completedAt,
		&operation.Result,
		&operation.ErrorMessage,
		&operation.CreatedAt,
		&operation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	operation.ClaimExpiresAt = clusterTimePointer(claimExpiresAt)
	operation.ClaimedAt = clusterTimePointer(claimedAt)
	operation.CompletedAt = clusterTimePointer(completedAt)
	return &operation, nil
}
