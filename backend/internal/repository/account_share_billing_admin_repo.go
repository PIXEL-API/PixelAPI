package repository

import (
	"context"
	"database/sql"
	"errors"

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
