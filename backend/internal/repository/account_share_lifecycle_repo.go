package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	accountShareRoomOperationStatusPending   = "pending"
	accountShareRoomOperationStatusSucceeded = "succeeded"
	accountShareRoomOperationActionDrain     = "drain_room"
	accountShareRoomOperationActionDelete    = "delete_room"
)

type lockedAccountShareLifecycleListing struct {
	ID                 int64
	OwnerUserID        int64
	AccountIdentityID  sql.NullInt64
	RoomName           string
	Status             string
	RowVersion         int64
	PendingOperationID sql.NullString
	DeleteRequestID    sql.NullString
	DeletedAt          sql.NullTime
	EditSessionID      sql.NullString
	EditingExpiresAt   sql.NullTime
	DeleteReason       sql.NullString
	DeletedByUserID    sql.NullInt64
}

func (r *accountShareModeRepository) GetRoomManagementState(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
) (*service.AccountShareRoomManagementState, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if listingID <= 0 || (!viewerIsAdmin && viewerUserID <= 0) {
		return nil, service.ErrAccountShareListingNotFound
	}

	unavailableCondition := accountShareAccountUnavailableConditionSQL("NOW()")
	query := fmt.Sprintf(`
		WITH membership_stats AS (
			SELECT
				COUNT(*) FILTER (WHERE membership.status = 'active')::int AS active_count,
				COUNT(*) FILTER (
					WHERE membership.status = 'active'
						AND membership.consumer_user_id <> scoped_listing.owner_user_id
				)::int AS consumer_active_count,
				COUNT(*) FILTER (WHERE membership.status = 'queued')::int AS queued_count,
				COUNT(*) FILTER (WHERE membership.status = 'ending')::int AS ending_count,
				COUNT(*) FILTER (
					WHERE membership.status = 'ending'
						AND membership.consumer_user_id <> scoped_listing.owner_user_id
				)::int AS consumer_ending_count,
				COUNT(*) FILTER (
					WHERE membership.settlement_status IN ('pending', 'processing', 'failed')
				)::int AS synchronous_billing_pending_count,
				COALESCE(
					ARRAY_AGG(membership.id ORDER BY membership.id)
						FILTER (WHERE membership.status IN ('active', 'ending')),
					ARRAY[]::bigint[]
				) AS runtime_membership_ids
			FROM account_share_memberships membership
			JOIN account_share_listings scoped_listing
				ON scoped_listing.id = membership.listing_id
			WHERE membership.listing_id = $1
				AND membership.deleted_at IS NULL
		),
		room_stats AS (
			SELECT
				COUNT(*)::int AS account_count,
				COALESCE(SUM(a.concurrency), 0)::int AS configured_total_concurrency,
				COALESCE(SUM(a.concurrency) FILTER (
					WHERE room_account.state = 'active'
						AND a.deleted_at IS NULL
						AND NOT %s
				), 0)::int AS eligible_total_concurrency,
				COUNT(*) FILTER (
					WHERE room_account.state = 'active'
						AND a.deleted_at IS NULL
						AND NOT %s
				)::int AS eligible_account_count,
				COALESCE(ARRAY_AGG(room_account.account_id ORDER BY room_account.account_id), ARRAY[]::bigint[])
					AS runtime_account_ids
			FROM account_share_room_accounts room_account
			JOIN accounts a ON a.id = room_account.account_id
			WHERE room_account.listing_id = $1
		),
		billing_stats AS (
			SELECT COUNT(*)::int AS pending_count
			FROM account_share_request_billing_intents
			WHERE listing_id = $1
				AND status NOT IN ('settled', 'cancelled')
		)
		SELECT
			listing.id,
			COALESCE(listing.room_name, ''),
			listing.owner_user_id,
			listing.row_version,
			listing.status,
			CASE
				WHEN COALESCE(room_stats.eligible_account_count, 0) = 0 THEN 'unavailable'
				WHEN room_stats.eligible_account_count < room_stats.account_count THEN 'degraded'
				ELSE 'healthy'
			END AS health_state,
			COALESCE(listing.status_reason_code, ''),
			COALESCE(listing.status_reason, ''),
			listing.seat_limit,
			COALESCE(membership_stats.consumer_active_count, 0),
			COALESCE(membership_stats.consumer_ending_count, 0),
			GREATEST(
				0,
				listing.seat_limit
					- COALESCE(membership_stats.consumer_active_count, 0)
					- COALESCE(membership_stats.consumer_ending_count, 0)
			)::int,
			COALESCE(membership_stats.queued_count, 0),
			COALESCE(room_stats.account_count, 0),
			COALESCE(room_stats.configured_total_concurrency, 0),
			COALESCE(room_stats.eligible_total_concurrency, 0),
			COALESCE(billing_stats.pending_count, 0),
			COALESCE(membership_stats.synchronous_billing_pending_count, 0),
			COALESCE(membership_stats.active_count, 0),
			COALESCE(membership_stats.ending_count, 0),
			(
				listing.edit_session_id IS NOT NULL
				AND listing.editing_expires_at IS NOT NULL
				AND listing.editing_expires_at > NOW()
			) AS valid_edit_session,
			COALESCE(open_operation.id IS NOT NULL, FALSE) AS conflicting_operation,
			COALESCE(open_operation.id::text, ''),
			COALESCE(membership_stats.runtime_membership_ids, ARRAY[]::bigint[]),
			COALESCE(room_stats.runtime_account_ids, ARRAY[]::bigint[]),
			listing.deleted_at
		FROM account_share_listings listing
		CROSS JOIN membership_stats
		CROSS JOIN room_stats
		CROSS JOIN billing_stats
		LEFT JOIN account_share_room_operations open_operation
			ON open_operation.id = listing.pending_operation_id
			AND open_operation.status IN ('pending', 'running', 'needs_attention')
		WHERE listing.id = $1
			AND ($2::boolean OR listing.owner_user_id = $3)
	`, unavailableCondition, unavailableCondition)

	state := &service.AccountShareRoomManagementState{}
	var (
		runtimeMembershipIDs pq.Int64Array
		runtimeAccountIDs    pq.Int64Array
		deletedAt            sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, query, listingID, viewerIsAdmin, viewerUserID).Scan(
		&state.ListingID,
		&state.RoomName,
		&state.OwnerUserID,
		&state.RowVersion,
		&state.LifecycleStatus,
		&state.HealthState,
		&state.StatusReasonCode,
		&state.StatusReason,
		&state.SeatLimit,
		&state.ActiveSeats,
		&state.EndingSeats,
		&state.AdmissionRemainingSeats,
		&state.QueuedMembershipCount,
		&state.RoomAccountCount,
		&state.ConfiguredTotalConcurrency,
		&state.EligibleTotalConcurrency,
		&state.PendingBillingIntentCount,
		&state.Blockers.SynchronousBillingPendingCount,
		&state.Blockers.ActiveMembershipCount,
		&state.Blockers.EndingMembershipCount,
		&state.Blockers.ValidEditSession,
		&state.Blockers.ConflictingOperation,
		&state.Blockers.ConflictingOperationID,
		&runtimeMembershipIDs,
		&runtimeAccountIDs,
		&deletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	state.Blockers.QueuedMembershipCount = state.QueuedMembershipCount
	state.Blockers.PendingBillingIntentCount = state.PendingBillingIntentCount
	state.PendingOperationID = state.Blockers.ConflictingOperationID
	state.RuntimeMembershipIDs = append([]int64(nil), runtimeMembershipIDs...)
	state.RuntimeAccountIDs = append([]int64(nil), runtimeAccountIDs...)
	if deletedAt.Valid {
		value := deletedAt.Time.UTC()
		state.DeletedAt = &value
	}
	return state, nil
}

func (r *accountShareModeRepository) TransitionRoomLifecycle(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	command string,
	input service.AccountShareRoomLifecycleCommandInput,
) (*service.AccountShareListing, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	listing, err := lockAccountShareLifecycleListingInTx(ctx, tx, listingID, actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if listing.DeletedAt.Valid {
		return nil, service.ErrAccountShareRoomDeleted
	}
	if listing.RowVersion != input.ExpectedVersion {
		return nil, accountShareVersionConflict(input.ExpectedVersion, listing.RowVersion)
	}
	if listing.PendingOperationID.Valid {
		return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
			"operation_id": listing.PendingOperationID.String,
		})
	}
	if listing.EditSessionID.Valid && listing.EditingExpiresAt.Valid &&
		listing.EditingExpiresAt.Time.After(time.Now().UTC()) {
		return nil, service.ErrAccountShareListingEditing
	}

	command = strings.ToLower(strings.TrimSpace(command))
	reason := strings.TrimSpace(input.Reason)
	nextStatus := ""
	statusReasonCode := ""
	eventType := ""
	source := ""
	var operationID string
	switch command {
	case service.AccountShareRoomActionDrain:
		if listing.Status != service.AccountShareListingStatusActive {
			return nil, service.ErrAccountShareRoomInvalidTransition
		}
		nextStatus = service.AccountShareListingStatusDraining
		statusReasonCode = "owner_delisted"
		eventType = "listing.delisted"
		source = "delist_room"
	case service.AccountShareRoomActionActivate:
		if listing.Status != service.AccountShareListingStatusPaused &&
			listing.Status != service.AccountShareListingStatusDraining &&
			!(actorIsAdmin && listing.Status == service.AccountShareListingStatusSuspended) {
			return nil, service.ErrAccountShareRoomInvalidTransition
		}
		nextStatus = service.AccountShareListingStatusValidating
		statusReasonCode = "activation_validation"
		eventType = "listing.validation_started"
		source = "activate_room"
	case "validation-pass":
		if listing.Status != service.AccountShareListingStatusValidating {
			return nil, service.ErrAccountShareRoomInvalidTransition
		}
		nextStatus = service.AccountShareListingStatusActive
		eventType = "listing.activated"
		source = "validation_pass"
	case "validation-fail":
		if listing.Status != service.AccountShareListingStatusValidating {
			return nil, service.ErrAccountShareRoomInvalidTransition
		}
		nextStatus = service.AccountShareListingStatusPaused
		statusReasonCode = "validation_failed"
		eventType = "listing.validation_failed"
		source = "validation_fail"
	case service.AccountShareRoomActionSuspend:
		if !actorIsAdmin {
			return nil, service.ErrInsufficientPerms
		}
		switch listing.Status {
		case service.AccountShareListingStatusActive,
			service.AccountShareListingStatusDraining,
			service.AccountShareListingStatusPaused,
			service.AccountShareListingStatusValidating:
		default:
			return nil, service.ErrAccountShareRoomInvalidTransition
		}
		nextStatus = service.AccountShareListingStatusSuspended
		statusReasonCode = "admin_suspended"
		eventType = "listing.suspended"
		source = "suspend_room"
	default:
		return nil, service.ErrAccountShareRoomInvalidTransition
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET status = $1::varchar(20),
			row_version = row_version + 1,
			status_reason_code = $2,
			status_reason = $3,
			validated_at = CASE WHEN $1::varchar(20) = 'active'::varchar(20) THEN NOW() ELSE validated_at END,
			draining_at = CASE WHEN $1::varchar(20) = 'draining'::varchar(20) THEN NOW() ELSE draining_at END,
			paused_at = CASE WHEN $1::varchar(20) = 'paused'::varchar(20) THEN NOW() ELSE paused_at END,
			suspended_at = CASE WHEN $1::varchar(20) = 'suspended'::varchar(20) THEN NOW() ELSE suspended_at END,
			pending_operation_id = $4::uuid,
			updated_at = NOW()
		WHERE id = $5
			AND row_version = $6
			AND deleted_at IS NULL
	`, nextStatus, nullableEmptyString(statusReasonCode), nullableEmptyString(reason), nullableEmptyString(operationID), listing.ID, listing.RowVersion)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, accountShareVersionConflict(input.ExpectedVersion, listing.RowVersion)
	}

	eventPayload := map[string]any{
		"command":     command,
		"from_status": listing.Status,
		"to_status":   nextStatus,
	}
	if operationID != "" {
		eventPayload["operation_id"] = operationID
	}
	if _, _, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listing.ID,
		actorUserID,
		actorIsAdmin,
		source,
		reason,
		actorIsAdmin && command == service.AccountShareRoomActionSuspend,
		eventType,
		eventPayload,
		operationID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetListingByID(ctx, listing.ID, listing.OwnerUserID)
}

func (r *accountShareModeRepository) FinalizeDrainingRoom(
	ctx context.Context,
	listingID int64,
	expectedVersion int64,
) (*service.AccountShareListing, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	listing, err := lockAccountShareLifecycleListingInTx(ctx, tx, listingID, 0, true)
	if err != nil {
		return nil, err
	}
	if listing.DeletedAt.Valid {
		return nil, service.ErrAccountShareRoomDeleted
	}
	if listing.Status != service.AccountShareListingStatusDraining || !listing.PendingOperationID.Valid {
		return nil, service.ErrAccountShareRoomInvalidTransition
	}
	if expectedVersion > 0 && listing.RowVersion != expectedVersion {
		return nil, accountShareVersionConflict(expectedVersion, listing.RowVersion)
	}
	blockers, err := accountShareLifecycleDatabaseBlockersInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if listing.EditSessionID.Valid && listing.EditingExpiresAt.Valid &&
		listing.EditingExpiresAt.Time.After(time.Now().UTC()) {
		blockers.ValidEditSession = true
	}
	if blockers.ActiveMembershipCount > 0 ||
		blockers.QueuedMembershipCount > 0 ||
		blockers.EndingMembershipCount > 0 ||
		blockers.PendingBillingIntentCount > 0 ||
		blockers.SynchronousBillingPendingCount > 0 ||
		blockers.ValidEditSession {
		return nil, service.ErrAccountShareRoomDeleteBlocked.WithMetadata(blockers.Metadata())
	}

	operationID := listing.PendingOperationID.String
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET status = 'paused',
			row_version = row_version + 1,
			paused_at = NOW(),
			status_reason_code = 'drain_complete',
			status_reason = NULL,
			pending_operation_id = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND row_version = $2
			AND pending_operation_id = $3::uuid
			AND deleted_at IS NULL
	`, listing.ID, listing.RowVersion, operationID)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != 1 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	_, finalVersion, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listing.ID,
		0,
		false,
		"drain_finalize",
		"",
		false,
		"listing.drain_completed",
		map[string]any{"operation_id": operationID},
		operationID,
	)
	if err != nil {
		return nil, err
	}
	resultPayload, err := json.Marshal(map[string]any{
		"lifecycle_status": service.AccountShareListingStatusPaused,
		"row_version":      finalVersion,
	})
	if err != nil {
		return nil, err
	}
	if err := completeAccountShareRoomOperationInTx(ctx, tx, operationID, finalVersion, resultPayload); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetListingByID(ctx, listing.ID, listing.OwnerUserID)
}

func (r *accountShareModeRepository) ListDrainingRoomIDs(ctx context.Context, afterID int64, limit int) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if afterID < 0 {
		afterID = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM account_share_listings
		WHERE status = 'draining'
			AND pending_operation_id IS NOT NULL
			AND deleted_at IS NULL
			AND id > $1
		ORDER BY id ASC
		LIMIT $2
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *accountShareModeRepository) FindRoomDeleteOperation(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	requestID string,
) (*service.AccountShareRoomOperation, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	requestID = strings.TrimSpace(requestID)
	if listingID <= 0 || requestID == "" || (!actorIsAdmin && actorUserID <= 0) {
		return nil, nil
	}
	operation, err := scanAccountShareRoomOperation(r.db.QueryRowContext(ctx, `
		SELECT
			operation.id::text,
			operation.listing_id,
			operation.membership_id,
			operation.actor_user_id,
			operation.actor_role,
			operation.action,
			operation.status,
			operation.expected_version,
			operation.start_version,
			operation.final_version,
			operation.blocker,
			operation.result,
			COALESCE(operation.error_code, ''),
			COALESCE(operation.error_message, ''),
			operation.created_at,
			operation.started_at,
			operation.completed_at,
			operation.updated_at
		FROM account_share_room_operations operation
		JOIN account_share_listings listing ON listing.id = operation.listing_id
		WHERE operation.listing_id = $1
			AND operation.action = 'delete_room'
			AND operation.request_id = $2
			AND ($3::boolean OR listing.owner_user_id = $4)
		ORDER BY operation.created_at DESC, operation.id DESC
		LIMIT 1
	`, listingID, requestID, actorIsAdmin, actorUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return operation, nil
}

func (r *accountShareModeRepository) ListValidatingRoomIDs(
	ctx context.Context,
	staleBefore time.Time,
	limit int,
) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if limit <= 0 || limit > 100 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM account_share_listings
		WHERE status = 'validating'
			AND pending_operation_id IS NULL
			AND deleted_at IS NULL
			AND updated_at <= $1
		ORDER BY updated_at ASC, id ASC
		LIMIT $2
	`, staleBefore.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SoftDeleteRoom is Tx A of deletion: it durably claims the operation and
// fences new joins/dispatches by moving the listing to draining. Final removal
// of the live projection is deliberately performed by FinalizeRoomDeletion.
func (r *accountShareModeRepository) SoftDeleteRoom(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input service.AccountShareRoomDeleteInput,
) (*service.AccountShareRoomOperation, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" || len(requestID) > 128 {
		return nil, service.ErrIdempotencyKeyRequired
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	listing, err := lockAccountShareLifecycleListingInTx(ctx, tx, listingID, actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if listing.DeleteRequestID.Valid && listing.DeleteRequestID.String == requestID {
		operationID := listing.PendingOperationID.String
		if operationID == "" {
			operationID, err = findAccountShareDeleteOperationIDInTx(ctx, tx, listing.ID, requestID)
			if err != nil {
				return nil, err
			}
		}
		operation, err := getAccountShareRoomOperationInTx(ctx, tx, operationID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return operation, nil
	}
	if listing.DeletedAt.Valid {
		return nil, service.ErrAccountShareRoomDeleted
	}
	if listing.RowVersion != input.ExpectedVersion {
		return nil, accountShareVersionConflict(input.ExpectedVersion, listing.RowVersion)
	}
	if listing.PendingOperationID.Valid {
		return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
			"operation_id": listing.PendingOperationID.String,
		})
	}
	switch listing.Status {
	case service.AccountShareListingStatusActive,
		service.AccountShareListingStatusPaused,
		service.AccountShareListingStatusSuspended:
	default:
		return nil, service.ErrAccountShareRoomInvalidTransition
	}

	blockers, err := accountShareLifecycleDatabaseBlockersInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if listing.EditSessionID.Valid && listing.EditingExpiresAt.Valid &&
		listing.EditingExpiresAt.Time.After(time.Now().UTC()) {
		blockers.ValidEditSession = true
	}
	if blockers.Any() {
		return nil, service.ErrAccountShareRoomDeleteBlocked.WithMetadata(blockers.Metadata())
	}
	if err := ensureAccountShareDeletionReviewIdentityInTx(ctx, tx, listing); err != nil {
		return nil, err
	}

	operationID := uuid.NewString()
	actorRole := accountShareRevisionActorRole(actorUserID, actorIsAdmin)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_share_room_operations (
			id, listing_id, action, actor_user_id, actor_role, source,
			request_id, expected_version, start_version, status,
			blocker, result, created_at, updated_at
		)
		VALUES (
			$1::uuid, $2, 'delete_room', $3, $4, 'api',
			$5, $6, $7, 'pending',
			'{}'::jsonb, '{}'::jsonb, NOW(), NOW()
		)
	`,
		operationID,
		listing.ID,
		nullablePositiveInt64(actorUserID),
		actorRole,
		requestID,
		listing.RowVersion,
		listing.RowVersion+1,
	); err != nil {
		return nil, translateAccountShareLifecyclePersistenceError(err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET status = 'draining',
			row_version = row_version + 1,
			draining_at = NOW(),
			status_reason_code = 'delete_requested',
			status_reason = $1,
			pending_operation_id = $2::uuid,
			deleted_by_user_id = $3,
			delete_reason = $1,
			delete_request_id = $4,
			edit_session_id = NULL,
			editing_by_user_id = NULL,
			editing_started_at = NULL,
			editing_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $5
			AND row_version = $6
			AND pending_operation_id IS NULL
			AND deleted_at IS NULL
	`, nullableEmptyString(input.Reason), operationID, nullablePositiveInt64(actorUserID), requestID, listing.ID, listing.RowVersion)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != 1 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	if _, _, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listing.ID,
		actorUserID,
		actorIsAdmin,
		"delete_request",
		input.Reason,
		false,
		"listing.delete_requested",
		map[string]any{
			"operation_id": operationID,
			"request_id":   requestID,
			"from_status":  listing.Status,
		},
		operationID,
	); err != nil {
		return nil, err
	}
	operation, err := getAccountShareRoomOperationInTx(ctx, tx, operationID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return operation, nil
}

func (r *accountShareModeRepository) FinalizeRoomDeletion(
	ctx context.Context,
	listingID int64,
	operationID string,
) (*service.AccountShareRoomOperation, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	operationID = strings.TrimSpace(operationID)
	if listingID <= 0 || operationID == "" {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	listing, err := lockAccountShareLifecycleListingInTx(ctx, tx, listingID, 0, true)
	if err != nil {
		return nil, err
	}
	if listing.DeletedAt.Valid {
		operation, operationErr := getAccountShareRoomOperationInTx(ctx, tx, operationID)
		if operationErr != nil {
			return nil, operationErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return operation, nil
	}
	if !listing.PendingOperationID.Valid || listing.PendingOperationID.String != operationID {
		return nil, service.ErrAccountShareRoomOperationConflict
	}

	accountIDs, err := lockAccountShareRoomProjectionInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if err := lockAccountShareAccountsInTx(ctx, tx, accountIDs); err != nil {
		return nil, err
	}
	liveMembershipIDs, err := lockLiveAccountShareMembershipIDsInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if len(liveMembershipIDs) > 0 {
		blockers, blockersErr := accountShareLifecycleDatabaseBlockersInTx(ctx, tx, listing.ID)
		if blockersErr != nil {
			return nil, blockersErr
		}
		return nil, service.ErrAccountShareRoomDeleteBlocked.WithMetadata(blockers.Metadata())
	}
	openBindingIDs, err := lockOpenAccountShareBindingIDsInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	pendingIntentIDs, err := lockPendingAccountShareBillingIntentIDsInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if len(pendingIntentIDs) > 0 {
		return nil, service.ErrAccountShareRoomDeleteBlocked.WithMetadata(map[string]string{
			"pending_billing_intent_count": fmt.Sprintf("%d", len(pendingIntentIDs)),
		})
	}
	blockers, err := accountShareLifecycleDatabaseBlockersInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if blockers.SynchronousBillingPendingCount > 0 {
		return nil, service.ErrAccountShareRoomDeleteBlocked.WithMetadata(blockers.Metadata())
	}
	operation, err := getAccountShareRoomOperationInTx(ctx, tx, operationID)
	if err != nil {
		return nil, err
	}
	if operation.Action != accountShareRoomOperationActionDelete ||
		operation.ListingID != listing.ID ||
		(operation.Status != accountShareRoomOperationStatusPending && operation.Status != "running" && operation.Status != "needs_attention") {
		return nil, service.ErrAccountShareRoomOperationConflict
	}

	now := time.Now().UTC()
	actorRole := operation.ActorRole
	if actorRole == "" {
		actorRole = "system"
	}
	if len(openBindingIDs) > 0 {
		result, err := tx.ExecContext(ctx, `
			UPDATE account_share_membership_account_bindings
			SET unbound_at = $1,
				unbound_by_user_id = $2,
				unbound_by_role = $3,
				unbind_reason = 'room_deleted'
			WHERE id = ANY($4::bigint[])
				AND unbound_at IS NULL
		`, now, nullablePositiveInt64(operation.ActorUserID), actorRole, pq.Array(openBindingIDs))
		if err != nil {
			return nil, err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
			return nil, rowsErr
		} else if affected != int64(len(openBindingIDs)) {
			return nil, fmt.Errorf("close room bindings affected %d rows, expected %d", affected, len(openBindingIDs))
		}
	}
	assignmentIDs, err := lockOpenAccountShareAssignmentIDsInTx(ctx, tx, listing.ID)
	if err != nil {
		return nil, err
	}
	if len(assignmentIDs) > 0 {
		result, err := tx.ExecContext(ctx, `
			UPDATE account_share_room_account_assignments
			SET detached_at = $1,
				detached_by_user_id = $2,
				detached_by_role = $3,
				detach_reason = 'room_deleted'
			WHERE id = ANY($4::bigint[])
				AND detached_at IS NULL
		`, now, nullablePositiveInt64(operation.ActorUserID), actorRole, pq.Array(assignmentIDs))
		if err != nil {
			return nil, err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
			return nil, rowsErr
		} else if affected != int64(len(assignmentIDs)) {
			return nil, fmt.Errorf("close room assignments affected %d rows, expected %d", affected, len(assignmentIDs))
		}
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM account_share_room_accounts
		WHERE listing_id = $1
	`, listing.ID)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != int64(len(accountIDs)) {
		return nil, fmt.Errorf("delete room account projection affected %d rows, expected %d", affected, len(accountIDs))
	}

	result, err = tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET row_version = row_version + 1,
			updated_at = $1
		WHERE id = $2
			AND pending_operation_id = $3::uuid
			AND deleted_at IS NULL
	`, now, listing.ID, operationID)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != 1 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	deletedRevisionID, finalVersion, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listing.ID,
		operation.ActorUserID,
		operation.ActorRole == "admin",
		"delete_finalize",
		listing.DeleteReason.String,
		false,
		"listing.delete_completed",
		map[string]any{
			"operation_id":  operationID,
			"account_count": len(accountIDs),
		},
		operationID,
	)
	if err != nil {
		return nil, err
	}
	deletionSnapshot := map[string]any{
		"schema_version":      1,
		"listing_id":          listing.ID,
		"room_name":           listing.RoomName,
		"owner_user_id":       listing.OwnerUserID,
		"lifecycle_status":    listing.Status,
		"account_count":       len(accountIDs),
		"deleted_at":          now.Format(time.RFC3339Nano),
		"deleted_revision_id": deletedRevisionID,
	}
	deletionSnapshotJSON, err := json.Marshal(deletionSnapshot)
	if err != nil {
		return nil, err
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET deleted_at = $1,
			deleted_revision_id = $2,
			deletion_snapshot = $3::jsonb,
			pending_operation_id = NULL,
			edit_session_id = NULL,
			editing_by_user_id = NULL,
			editing_started_at = NULL,
			editing_expires_at = NULL,
			updated_at = $1
		WHERE id = $4
			AND row_version = $5
			AND pending_operation_id = $6::uuid
			AND deleted_at IS NULL
	`, now, deletedRevisionID, string(deletionSnapshotJSON), listing.ID, finalVersion, operationID)
	if err != nil {
		return nil, translateAccountShareLifecyclePersistenceError(err)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if affected != 1 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	for _, accountID := range accountIDs {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			return nil, err
		}
	}
	resultPayload, err := json.Marshal(map[string]any{
		"deleted_at":          now.Format(time.RFC3339Nano),
		"deleted_revision_id": deletedRevisionID,
		"account_count":       len(accountIDs),
		"row_version":         finalVersion,
	})
	if err != nil {
		return nil, err
	}
	if err := completeAccountShareRoomOperationInTx(ctx, tx, operationID, finalVersion, resultPayload); err != nil {
		return nil, err
	}
	operation, err = getAccountShareRoomOperationInTx(ctx, tx, operationID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return operation, nil
}

func (r *accountShareModeRepository) ListPendingRoomDeletionOperations(
	ctx context.Context,
	limit int,
) ([]service.AccountShareRoomOperation, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id::text, listing_id, membership_id, actor_user_id, actor_role,
			action, status, expected_version, start_version, final_version,
			blocker, result, COALESCE(error_code, ''), COALESCE(error_message, ''),
			created_at, started_at, completed_at, updated_at
		FROM account_share_room_operations
		WHERE action = 'delete_room'
			AND status IN ('pending', 'running', 'needs_attention')
		ORDER BY created_at ASC, id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	operations := make([]service.AccountShareRoomOperation, 0, limit)
	for rows.Next() {
		operation, err := scanAccountShareRoomOperation(rows)
		if err != nil {
			return nil, err
		}
		operations = append(operations, *operation)
	}
	return operations, rows.Err()
}

func (r *accountShareModeRepository) GetRoomOperation(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	operationID string,
) (*service.AccountShareRoomOperation, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" || (!viewerIsAdmin && viewerUserID <= 0) {
		return nil, service.ErrAccountShareListingNotFound
	}
	operation, err := scanAccountShareRoomOperation(r.db.QueryRowContext(ctx, `
		SELECT
			operation.id::text,
			operation.listing_id,
			operation.membership_id,
			operation.actor_user_id,
			operation.actor_role,
			operation.action,
			operation.status,
			operation.expected_version,
			operation.start_version,
			operation.final_version,
			operation.blocker,
			operation.result,
			COALESCE(operation.error_code, ''),
			COALESCE(operation.error_message, ''),
			operation.created_at,
			operation.started_at,
			operation.completed_at,
			operation.updated_at
		FROM account_share_room_operations operation
		JOIN account_share_listings listing ON listing.id = operation.listing_id
		LEFT JOIN account_share_memberships membership ON membership.id = operation.membership_id
		WHERE operation.id = $1::uuid
			AND (
				$2::boolean
				OR listing.owner_user_id = $3
				OR operation.actor_user_id = $3
				OR membership.consumer_user_id = $3
			)
	`, operationID, viewerIsAdmin, viewerUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return operation, nil
}

func lockAccountShareLifecycleListingInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	actorUserID int64,
	actorIsAdmin bool,
) (*lockedAccountShareLifecycleListing, error) {
	listing := &lockedAccountShareLifecycleListing{}
	err := tx.QueryRowContext(ctx, `
		SELECT
			id,
			owner_user_id,
			account_identity_id,
			COALESCE(room_name, ''),
			status,
			row_version,
			pending_operation_id::text,
			delete_request_id,
			deleted_at,
			edit_session_id,
			editing_expires_at,
			delete_reason,
			deleted_by_user_id
		FROM account_share_listings
		WHERE id = $1
			AND ($2::boolean OR owner_user_id = $3)
		FOR UPDATE
	`, listingID, actorIsAdmin, actorUserID).Scan(
		&listing.ID,
		&listing.OwnerUserID,
		&listing.AccountIdentityID,
		&listing.RoomName,
		&listing.Status,
		&listing.RowVersion,
		&listing.PendingOperationID,
		&listing.DeleteRequestID,
		&listing.DeletedAt,
		&listing.EditSessionID,
		&listing.EditingExpiresAt,
		&listing.DeleteReason,
		&listing.DeletedByUserID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return listing, nil
}

func ensureAccountShareDeletionReviewIdentityInTx(
	ctx context.Context,
	tx *sql.Tx,
	listing *lockedAccountShareLifecycleListing,
) error {
	if tx == nil || listing == nil || listing.ID <= 0 {
		return service.ErrAccountShareRoomOperationConflict
	}
	if listing.AccountIdentityID.Valid && listing.AccountIdentityID.Int64 > 0 {
		return nil
	}

	var reviewIdentityRequired bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_memberships membership
			WHERE membership.listing_id = $1
				AND membership.status = 'ended'
				AND membership.last_request_at IS NOT NULL
				AND membership.consumer_user_id <> $2
				AND membership.deleted_at IS NULL
		)
	`, listing.ID, listing.OwnerUserID).Scan(&reviewIdentityRequired); err != nil {
		return err
	}
	if !reviewIdentityRequired {
		return nil
	}

	var (
		accountID             int64
		accountName           string
		accountPlatform       string
		accountCredentialsRaw []byte
		accountExtraRaw       []byte
	)
	err := tx.QueryRowContext(ctx, `
		WITH candidate_accounts AS (
			SELECT
				COALESCE(history_binding.account_id, history_binding.account_id_snapshot, membership.account_id) AS account_id,
				COALESCE(membership.ended_at, membership.updated_at, membership.joined_at) AS used_at,
				0 AS source_priority
			FROM account_share_memberships membership
			LEFT JOIN LATERAL (
				SELECT binding.account_id, binding.account_id_snapshot
				FROM account_share_membership_account_bindings binding
				WHERE binding.membership_id = membership.id
					AND binding.listing_id = membership.listing_id
				ORDER BY binding.routing_generation DESC, binding.id DESC
				LIMIT 1
			) history_binding ON TRUE
			WHERE membership.listing_id = $1
				AND membership.status = 'ended'
				AND membership.last_request_at IS NOT NULL
				AND membership.consumer_user_id <> $2
				AND membership.deleted_at IS NULL

			UNION ALL

			SELECT
				room_account.account_id,
				room_account.updated_at AS used_at,
				1 AS source_priority
			FROM account_share_room_accounts room_account
			WHERE room_account.listing_id = $1
		)
		SELECT
			account.id,
			COALESCE(account.name, ''),
			COALESCE(account.platform, ''),
			account.credentials,
			account.extra
		FROM candidate_accounts candidate
		JOIN accounts account ON account.id = candidate.account_id
		WHERE BTRIM(COALESCE(
			NULLIF(account.credentials ->> 'email', ''),
			NULLIF(account.credentials ->> 'email_address', ''),
			NULLIF(account.extra ->> 'email', ''),
			NULLIF(account.extra ->> 'email_address', ''),
			''
		)) <> ''
		ORDER BY candidate.source_priority ASC, candidate.used_at DESC NULLS LAST, account.id ASC
		LIMIT 1
	`, listing.ID, listing.OwnerUserID).Scan(
		&accountID,
		&accountName,
		&accountPlatform,
		&accountCredentialsRaw,
		&accountExtraRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAccountShareRoomReviewIdentityMissing
	}
	if err != nil {
		return err
	}
	accountCredentials, err := unmarshalAccountShareJSONMap(accountCredentialsRaw)
	if err != nil {
		return err
	}
	accountExtra, err := unmarshalAccountShareJSONMap(accountExtraRaw)
	if err != nil {
		return err
	}
	identityID, err := ensureAccountShareAccountIdentityInTx(ctx, tx, &service.Account{
		ID:          accountID,
		Name:        accountName,
		Platform:    accountPlatform,
		Credentials: accountCredentials,
		Extra:       accountExtra,
	})
	if err != nil {
		return err
	}
	if identityID == nil || *identityID <= 0 {
		return service.ErrAccountShareRoomReviewIdentityMissing
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET account_identity_id = $1
		WHERE id = $2
			AND account_identity_id IS NULL
			AND deleted_at IS NULL
	`, *identityID, listing.ID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrAccountShareRoomOperationConflict
	}
	listing.AccountIdentityID = sql.NullInt64{Int64: *identityID, Valid: true}
	return nil
}

func accountShareLifecycleDatabaseBlockersInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
) (service.AccountShareRoomBlockers, error) {
	blockers := service.AccountShareRoomBlockers{}
	err := tx.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'active')::int,
			COUNT(*) FILTER (WHERE status = 'queued')::int,
			COUNT(*) FILTER (WHERE status = 'ending')::int,
			COUNT(*) FILTER (
				WHERE settlement_status IN ('pending', 'processing', 'failed')
			)::int
		FROM account_share_memberships
		WHERE listing_id = $1
			AND deleted_at IS NULL
	`, listingID).Scan(
		&blockers.ActiveMembershipCount,
		&blockers.QueuedMembershipCount,
		&blockers.EndingMembershipCount,
		&blockers.SynchronousBillingPendingCount,
	)
	if err != nil {
		return blockers, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*)::int
		FROM account_share_request_billing_intents
		WHERE listing_id = $1
			AND status NOT IN ('settled', 'cancelled')
	`, listingID).Scan(&blockers.PendingBillingIntentCount)
	return blockers, err
}

func endQueuedMembershipsForRoomDrainInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	actorUserID int64,
	actorRole string,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM account_share_memberships
		WHERE listing_id = $1
			AND status = 'queued'
			AND deleted_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, listingID)
	if err != nil {
		return err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_share_membership_account_bindings
		SET unbound_at = NOW(),
			unbound_by_user_id = $1,
			unbound_by_role = $2,
			unbind_reason = 'room_draining'
		WHERE membership_id = ANY($3::bigint[])
			AND unbound_at IS NULL
	`, nullablePositiveInt64(actorUserID), actorRole, pq.Array(ids)); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_memberships
		SET status = 'ended',
			ended_at = NOW(),
			ended_reason = $2,
			settlement_status = 'not_required',
			updated_at = NOW()
		WHERE id = ANY($1::bigint[])
			AND status = 'queued'
			AND deleted_at IS NULL
	`, pq.Array(ids), service.AccountShareMembershipEndReasonRoomDraining)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != int64(len(ids)) {
		return fmt.Errorf("end queued room memberships affected %d rows, expected %d", affected, len(ids))
	}
	return nil
}

func lockAccountShareRoomProjectionInTx(ctx context.Context, tx *sql.Tx, listingID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT account_id
		FROM account_share_room_accounts
		WHERE listing_id = $1
		ORDER BY account_id ASC
		FOR UPDATE
	`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func lockAccountShareAccountsInTx(ctx context.Context, tx *sql.Tx, accountIDs []int64) error {
	if len(accountIDs) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM accounts
		WHERE id = ANY($1::bigint[])
		ORDER BY id ASC
		FOR UPDATE
	`, pq.Array(accountIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func lockLiveAccountShareMembershipIDsInTx(ctx context.Context, tx *sql.Tx, listingID int64) ([]int64, error) {
	return lockAccountShareIDsInTx(ctx, tx, `
		SELECT id
		FROM account_share_memberships
		WHERE listing_id = $1
			AND status IN ('active', 'queued', 'ending')
			AND deleted_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, listingID)
}

func lockOpenAccountShareBindingIDsInTx(ctx context.Context, tx *sql.Tx, listingID int64) ([]int64, error) {
	return lockAccountShareIDsInTx(ctx, tx, `
		SELECT id
		FROM account_share_membership_account_bindings
		WHERE listing_id = $1
			AND unbound_at IS NULL
		ORDER BY membership_id ASC, id ASC
		FOR UPDATE
	`, listingID)
}

func lockPendingAccountShareBillingIntentIDsInTx(ctx context.Context, tx *sql.Tx, listingID int64) ([]int64, error) {
	return lockAccountShareIDsInTx(ctx, tx, `
		SELECT id
		FROM account_share_request_billing_intents
		WHERE listing_id = $1
			AND status NOT IN ('settled', 'cancelled')
		ORDER BY request_id ASC, api_key_id_snapshot ASC, id ASC
		FOR UPDATE
	`, listingID)
}

func lockOpenAccountShareAssignmentIDsInTx(ctx context.Context, tx *sql.Tx, listingID int64) ([]int64, error) {
	return lockAccountShareIDsInTx(ctx, tx, `
		SELECT id
		FROM account_share_room_account_assignments
		WHERE listing_id = $1
			AND detached_at IS NULL
		ORDER BY account_id_snapshot ASC, id ASC
		FOR UPDATE
	`, listingID)
}

func lockAccountShareIDsInTx(ctx context.Context, tx *sql.Tx, query string, listingID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func completeAccountShareRoomOperationInTx(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
	finalVersion int64,
	resultPayload []byte,
) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_room_operations
		SET status = 'succeeded',
			final_version = $1,
			result = $2::jsonb,
			blocker = '{}'::jsonb,
			error_code = NULL,
			error_message = NULL,
			lease_owner = NULL,
			lease_expires_at = NULL,
			completed_at = NOW(),
			updated_at = NOW(),
			state_token = state_token + 1
		WHERE id = $3::uuid
			AND status IN ('pending', 'running', 'needs_attention')
	`, finalVersion, string(resultPayload), operationID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrAccountShareRoomOperationConflict
	}
	return nil
}

type accountShareRoomOperationScanner interface {
	Scan(dest ...any) error
}

func scanAccountShareRoomOperation(scanner accountShareRoomOperationScanner) (*service.AccountShareRoomOperation, error) {
	operation := &service.AccountShareRoomOperation{}
	var (
		membershipID sql.NullInt64
		actorUserID  sql.NullInt64
		expected     sql.NullInt64
		start        sql.NullInt64
		final        sql.NullInt64
		startedAt    sql.NullTime
		completedAt  sql.NullTime
		blockerJSON  []byte
		resultJSON   []byte
	)
	err := scanner.Scan(
		&operation.ID,
		&operation.ListingID,
		&membershipID,
		&actorUserID,
		&operation.ActorRole,
		&operation.Action,
		&operation.Status,
		&expected,
		&start,
		&final,
		&blockerJSON,
		&resultJSON,
		&operation.ErrorCode,
		&operation.ErrorMessage,
		&operation.CreatedAt,
		&startedAt,
		&completedAt,
		&operation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	operation.MembershipID = sqlNullInt64Ptr(membershipID)
	if actorUserID.Valid {
		operation.ActorUserID = actorUserID.Int64
	}
	operation.ExpectedVersion = sqlNullInt64Ptr(expected)
	operation.StartVersion = sqlNullInt64Ptr(start)
	operation.FinalVersion = sqlNullInt64Ptr(final)
	if startedAt.Valid {
		value := startedAt.Time.UTC()
		operation.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		operation.CompletedAt = &value
	}
	operation.Blocker = map[string]any{}
	operation.Result = map[string]any{}
	if len(blockerJSON) > 0 {
		if err := json.Unmarshal(blockerJSON, &operation.Blocker); err != nil {
			return nil, err
		}
	}
	if len(resultJSON) > 0 {
		if err := json.Unmarshal(resultJSON, &operation.Result); err != nil {
			return nil, err
		}
	}
	return operation, nil
}

func getAccountShareRoomOperationInTx(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
) (*service.AccountShareRoomOperation, error) {
	operation, err := scanAccountShareRoomOperation(tx.QueryRowContext(ctx, `
		SELECT
			id::text, listing_id, membership_id, actor_user_id, actor_role,
			action, status, expected_version, start_version, final_version,
			blocker, result, COALESCE(error_code, ''), COALESCE(error_message, ''),
			created_at, started_at, completed_at, updated_at
		FROM account_share_room_operations
		WHERE id = $1::uuid
	`, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	return operation, err
}

func findAccountShareDeleteOperationIDInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	requestID string,
) (string, error) {
	var operationID string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM account_share_room_operations
		WHERE listing_id = $1
			AND action = 'delete_room'
			AND request_id = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, listingID, requestID).Scan(&operationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", service.ErrAccountShareRoomOperationConflict
	}
	return operationID, err
}

func translateAccountShareLifecyclePersistenceError(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		switch pqErr.Constraint {
		case "uq_account_share_room_operations_open_listing",
			"uq_account_share_room_operations_open_room_listing",
			"uq_account_share_listings_pending_operation",
			"uq_account_share_listings_delete_request":
			return service.ErrAccountShareRoomOperationConflict
		}
	}
	return err
}
