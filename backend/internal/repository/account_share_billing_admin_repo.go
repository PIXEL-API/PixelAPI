package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountShareBillingIntentAdminRepository = (*accountShareBillingIntentRepository)(nil)

const accountShareBillingIntentAdminColumns = `
	intent.id,
	intent.request_id,
	intent.dispatch_id::text,
	intent.attempt_no,
	intent.api_key_id_snapshot,
	intent.membership_id,
	intent.listing_id,
	intent.account_id_snapshot,
	intent.status,
	intent.state_token,
	COALESCE(intent.last_error_code, ''),
	COALESCE(intent.last_error_message, ''),
	intent.forward_started_at,
	intent.completed_at,
	intent.created_at,
	intent.updated_at`

const accountShareBillingIntentWaiverColumns = `
	waiver.id,
	waiver.intent_id,
	waiver.listing_id,
	waiver.membership_id,
	waiver.actor_user_id_snapshot,
	waiver.reason,
	waiver.action,
	waiver.previous_status,
	waiver.resulting_status,
	waiver.previous_state_token,
	waiver.resulting_state_token,
	waiver.created_at`

func (r *accountShareBillingIntentRepository) ListNeedsAttentionForAdmin(
	ctx context.Context,
	offset int,
	limit int,
) ([]service.AccountShareBillingIntentAdminRecord, int64, error) {
	if err := r.validate(); err != nil {
		return nil, 0, err
	}
	if offset < 0 || limit <= 0 || limit > service.AccountShareBillingAdminMaxPageSize {
		return nil, 0, service.ErrAccountShareBillingIntentInvalid
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)::bigint
		FROM account_share_request_billing_intents
		WHERE status = 'needs_attention'
	`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountShareBillingIntentAdminColumns+`
		FROM account_share_request_billing_intents AS intent
		WHERE intent.status = 'needs_attention'
		ORDER BY intent.updated_at DESC, intent.id DESC
		OFFSET $1
		LIMIT $2
	`, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.AccountShareBillingIntentAdminRecord, 0, limit)
	for rows.Next() {
		item, scanErr := scanAccountShareBillingIntentAdminRecord(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *accountShareBillingIntentRepository) GetForAdmin(
	ctx context.Context,
	intentID int64,
) (*service.AccountShareBillingIntentAdminRecord, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if intentID <= 0 {
		return nil, service.ErrAccountShareBillingIntentInvalid
	}
	record, err := scanAccountShareBillingIntentAdminRecord(r.db.QueryRowContext(ctx, `
		SELECT `+accountShareBillingIntentAdminColumns+`
		FROM account_share_request_billing_intents AS intent
		WHERE intent.id = $1
	`, intentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareBillingIntentNotFound
	}
	return record, err
}

func (r *accountShareBillingIntentRepository) WaiveNeedsAttention(
	ctx context.Context,
	input service.WaiveAccountShareBillingIntentRepositoryInput,
) (*service.AccountShareBillingIntentWaiverResult, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.IntentID <= 0 || input.ExpectedStateToken <= 0 || input.ActorUserID <= 0 || input.Reason == "" {
		return nil, service.ErrAccountShareBillingIntentInvalid
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := scanAccountShareBillingIntentAdminRecord(tx.QueryRowContext(ctx, `
		SELECT `+accountShareBillingIntentAdminColumns+`
		FROM account_share_request_billing_intents AS intent
		WHERE intent.id = $1
		FOR UPDATE
	`, input.IntentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareBillingIntentNotFound
	}
	if err != nil {
		return nil, err
	}

	if current.Status == service.AccountShareBillingIntentStatusCancelled {
		waiver, waiverErr := scanAccountShareBillingIntentAdminWaiver(tx.QueryRowContext(ctx, `
			SELECT `+accountShareBillingIntentWaiverColumns+`
			FROM account_share_billing_intent_admin_waivers AS waiver
			WHERE waiver.intent_id = $1
		`, input.IntentID))
		if waiverErr != nil {
			if errors.Is(waiverErr, sql.ErrNoRows) {
				return nil, service.ErrAccountShareBillingIntentStateConflict
			}
			return nil, waiverErr
		}
		if waiver.IntentID != current.ID ||
			waiver.ListingID != current.ListingID ||
			waiver.MembershipID != current.MembershipID ||
			waiver.Action != "waive" ||
			waiver.ActorUserIDSnapshot != input.ActorUserID ||
			waiver.Reason != input.Reason ||
			waiver.PreviousStateToken != input.ExpectedStateToken ||
			waiver.ResultingStateToken != current.StateToken ||
			waiver.PreviousStatus != service.AccountShareBillingIntentStatusNeedsAttention ||
			waiver.ResultingStatus != service.AccountShareBillingIntentStatusCancelled {
			return nil, service.ErrAccountShareBillingIntentStateConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.AccountShareBillingIntentWaiverResult{
			Intent: *current,
			Waiver: *waiver,
		}, nil
	}

	if current.Status != service.AccountShareBillingIntentStatusNeedsAttention ||
		current.StateToken != input.ExpectedStateToken {
		return nil, service.ErrAccountShareBillingIntentStateConflict
	}

	waiver, err := scanAccountShareBillingIntentAdminWaiver(tx.QueryRowContext(ctx, `
		INSERT INTO account_share_billing_intent_admin_waivers AS waiver (
			intent_id,
			listing_id,
			membership_id,
			actor_user_id,
			actor_user_id_snapshot,
			reason,
			action,
			previous_status,
			resulting_status,
			previous_state_token,
			resulting_state_token
		)
		SELECT
			intent.id,
			intent.listing_id,
			intent.membership_id,
			$2,
			$2,
			$3,
			'waive',
			intent.status,
			'cancelled',
			intent.state_token,
			intent.state_token + 1
		FROM account_share_request_billing_intents AS intent
		WHERE intent.id = $1
			AND intent.status = 'needs_attention'
			AND intent.state_token = $4
		RETURNING `+accountShareBillingIntentWaiverColumns,
		input.IntentID,
		input.ActorUserID,
		input.Reason,
		input.ExpectedStateToken,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareBillingIntentStateConflict
	}
	if err != nil {
		return nil, err
	}

	updated, err := scanAccountShareBillingIntentAdminRecord(tx.QueryRowContext(ctx, `
		UPDATE account_share_request_billing_intents AS intent
		SET status = 'cancelled',
			state_token = intent.state_token + 1,
			admin_waiver_audit_id = $2,
			lease_owner = NULL,
			lease_expires_at = NULL,
			next_attempt_at = NULL,
			last_error_code = 'admin_waived',
			last_error_message = NULL,
			updated_at = clock_timestamp()
		WHERE intent.id = $1
			AND intent.status = 'needs_attention'
			AND intent.state_token = $3
		RETURNING `+accountShareBillingIntentAdminColumns,
		input.IntentID,
		waiver.ID,
		input.ExpectedStateToken,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareBillingIntentStateConflict
	}
	if err != nil {
		return nil, err
	}
	if updated.StateToken != waiver.ResultingStateToken {
		return nil, service.ErrAccountShareBillingIntentStateConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.AccountShareBillingIntentWaiverResult{
		Intent: *updated,
		Waiver: *waiver,
	}, nil
}

func scanAccountShareBillingIntentAdminRecord(
	scanner sqlScanner,
) (*service.AccountShareBillingIntentAdminRecord, error) {
	record := &service.AccountShareBillingIntentAdminRecord{}
	var forwardStartedAt, completedAt sql.NullTime
	if err := scanner.Scan(
		&record.ID,
		&record.RequestID,
		&record.DispatchID,
		&record.AttemptNo,
		&record.APIKeyID,
		&record.MembershipID,
		&record.ListingID,
		&record.AccountID,
		&record.Status,
		&record.StateToken,
		&record.LastErrorCode,
		&record.LastErrorMessage,
		&forwardStartedAt,
		&completedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.ForwardStartedAt = accountShareBillingNullTimePointer(forwardStartedAt)
	record.CompletedAt = accountShareBillingNullTimePointer(completedAt)
	return record, nil
}

func scanAccountShareBillingIntentAdminWaiver(
	scanner sqlScanner,
) (*service.AccountShareBillingIntentAdminWaiver, error) {
	waiver := &service.AccountShareBillingIntentAdminWaiver{}
	if err := scanner.Scan(
		&waiver.ID,
		&waiver.IntentID,
		&waiver.ListingID,
		&waiver.MembershipID,
		&waiver.ActorUserIDSnapshot,
		&waiver.Reason,
		&waiver.Action,
		&waiver.PreviousStatus,
		&waiver.ResultingStatus,
		&waiver.PreviousStateToken,
		&waiver.ResultingStateToken,
		&waiver.CreatedAt,
	); err != nil {
		return nil, err
	}
	return waiver, nil
}
