package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type invoiceRepository struct {
	db *sql.DB
}

func NewInvoiceRepository(db *sql.DB) service.InvoiceRepository {
	return &invoiceRepository{db: db}
}

func (r *invoiceRepository) ListProfiles(ctx context.Context, userID int64) ([]service.InvoiceProfile, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT `+invoiceProfileColumns+`
FROM invoice_profiles
WHERE user_id = $1
ORDER BY is_default DESC, created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.InvoiceProfile, 0)
	for rows.Next() {
		item, err := scanInvoiceProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *invoiceRepository) CreateProfile(ctx context.Context, userID int64, input service.InvoiceProfileInput) (*service.InvoiceProfile, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE invoice_profiles SET is_default = FALSE, updated_at = NOW() WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}
	profile, err := queryInvoiceProfile(ctx, tx, `
INSERT INTO invoice_profiles (
	user_id, invoice_type, buyer_type, title_name, tax_id, registered_address,
	registered_phone, bank_name, bank_account, recipient_email, recipient_phone, is_default
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, $11, $12
)
RETURNING `+invoiceProfileColumns,
		userID,
		input.InvoiceType,
		invoiceBuyerTypeForDB(input.InvoiceType),
		input.TitleName,
		input.TaxID,
		input.RegisteredAddress,
		input.RegisteredPhone,
		input.BankName,
		input.BankAccount,
		input.RecipientEmail,
		input.RecipientPhone,
		input.IsDefault,
	)
	if err != nil {
		if isInvoiceUniqueViolation(err) {
			return nil, service.ErrInvoiceDefaultConflict
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return profile, nil
}

func (r *invoiceRepository) UpdateProfile(ctx context.Context, userID, id int64, input service.InvoiceProfileInput) (*service.InvoiceProfile, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	if _, err := queryInvoiceProfile(ctx, tx, `
SELECT `+invoiceProfileColumns+`
FROM invoice_profiles
WHERE id = $1 AND user_id = $2
FOR UPDATE`, id, userID); err != nil {
		return nil, err
	}
	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE invoice_profiles SET is_default = FALSE, updated_at = NOW() WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}
	profile, err := queryInvoiceProfile(ctx, tx, `
UPDATE invoice_profiles
SET invoice_type = $1,
	buyer_type = $2,
	title_name = $3,
	tax_id = $4,
	registered_address = $5,
	registered_phone = $6,
	bank_name = $7,
	bank_account = $8,
	recipient_email = $9,
	recipient_phone = $10,
	is_default = $11,
	updated_at = NOW()
WHERE id = $12 AND user_id = $13
RETURNING `+invoiceProfileColumns,
		input.InvoiceType,
		invoiceBuyerTypeForDB(input.InvoiceType),
		input.TitleName,
		input.TaxID,
		input.RegisteredAddress,
		input.RegisteredPhone,
		input.BankName,
		input.BankAccount,
		input.RecipientEmail,
		input.RecipientPhone,
		input.IsDefault,
		id,
		userID,
	)
	if err != nil {
		if isInvoiceUniqueViolation(err) {
			return nil, service.ErrInvoiceDefaultConflict
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return profile, nil
}

func (r *invoiceRepository) DeleteProfile(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM invoice_profiles WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrInvoiceProfileNotFound
	}
	return nil
}

func (r *invoiceRepository) SetDefaultProfile(ctx context.Context, userID, id int64) (*service.InvoiceProfile, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	if _, err := queryInvoiceProfile(ctx, tx, `
SELECT `+invoiceProfileColumns+`
FROM invoice_profiles
WHERE id = $1 AND user_id = $2
FOR UPDATE`, id, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoice_profiles SET is_default = FALSE, updated_at = NOW() WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	profile, err := queryInvoiceProfile(ctx, tx, `
UPDATE invoice_profiles
SET is_default = TRUE, updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING `+invoiceProfileColumns, id, userID)
	if err != nil {
		if isInvoiceUniqueViolation(err) {
			return nil, service.ErrInvoiceDefaultConflict
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return profile, nil
}

func (r *invoiceRepository) ListEligibleSources(ctx context.Context, userID int64, page, pageSize int) ([]service.InvoiceEligibleSource, int64, error) {
	page, pageSize = normalizeInvoicePagination(page, pageSize)
	var total int64
	if err := r.db.QueryRowContext(ctx, eligibleInvoiceSourcesSQL+` SELECT COUNT(*) FROM sources`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, eligibleInvoiceSourcesSQL+`
SELECT source_type, source_id, source_no, source_label, item_type,
	entitlement_amount::double precision, invoice_amount::double precision,
	occurred_at, status
FROM sources
ORDER BY occurred_at DESC, source_id DESC
LIMIT $2 OFFSET $3`, userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.InvoiceEligibleSource, 0)
	for rows.Next() {
		var item service.InvoiceEligibleSource
		if err := rows.Scan(
			&item.SourceType,
			&item.SourceID,
			&item.SourceNo,
			&item.SourceLabel,
			&item.ItemType,
			&item.EntitlementAmount,
			&item.InvoiceAmount,
			&item.OccurredAt,
			&item.Status,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (r *invoiceRepository) CreateRequest(ctx context.Context, userID int64, input service.InvoiceRequestInput, requestNo string) (*service.InvoiceRequest, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	var userEmail string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&userEmail); err != nil {
		return nil, err
	}

	sources := make([]service.InvoiceEligibleSource, 0, len(input.SourceRefs))
	totalAmount := 0.0
	for _, ref := range input.SourceRefs {
		source, err := r.lockInvoiceSource(ctx, tx, userID, ref)
		if err != nil {
			return nil, err
		}
		amount, ok := normalizeInvoiceAmountForDB(source.InvoiceAmount)
		if !ok {
			return nil, service.ErrInvoiceAmountInvalid
		}
		source.InvoiceAmount = amount
		sources = append(sources, *source)
		totalAmount += amount
	}
	totalAmount, ok := normalizeInvoiceAmountForDB(totalAmount)
	if !ok {
		return nil, service.ErrInvoiceAmountInvalid
	}

	req, err := queryInvoiceRequest(ctx, tx, `
INSERT INTO invoice_requests (
	request_no, user_id, user_email, invoice_type, buyer_type, title_name, tax_id,
	registered_address, registered_phone, bank_name, bank_account, recipient_email,
	recipient_phone, amount, status
) VALUES (
	$1, $2, $3, $4, $5, $6, $7,
	$8, $9, $10, $11, $12,
	$13, $14, $15
)
RETURNING `+invoiceRequestColumns,
		requestNo,
		userID,
		userEmail,
		input.InvoiceType,
		invoiceBuyerTypeForDB(input.InvoiceType),
		input.TitleName,
		input.TaxID,
		input.RegisteredAddress,
		input.RegisteredPhone,
		input.BankName,
		input.BankAccount,
		input.RecipientEmail,
		input.RecipientPhone,
		totalAmount,
		service.InvoiceStatusPending,
	)
	if err != nil {
		return nil, err
	}

	for _, source := range sources {
		snapshot, err := json.Marshal(source)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO invoice_request_items (
	invoice_request_id, source_type, source_id, source_no, source_label, item_type,
	entitlement_amount, invoice_amount, occurred_at, snapshot, active
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10::jsonb, TRUE
)`,
			req.ID,
			source.SourceType,
			source.SourceID,
			source.SourceNo,
			source.SourceLabel,
			source.ItemType,
			source.EntitlementAmount,
			source.InvoiceAmount,
			source.OccurredAt,
			string(snapshot),
		); err != nil {
			if isInvoiceUniqueViolation(err) {
				return nil, service.ErrInvoiceSourceUnavailable
			}
			return nil, err
		}
	}
	if err := insertInvoiceEvent(ctx, tx, req.ID, nil, "submitted", "", nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetRequestByID(ctx, req.ID)
}

func (r *invoiceRepository) ListRequestsByUser(ctx context.Context, userID int64, params service.InvoiceRequestListParams) ([]service.InvoiceRequest, int64, error) {
	params.UserID = userID
	return r.listRequests(ctx, params, true)
}

func (r *invoiceRepository) GetRequestByUser(ctx context.Context, userID, id int64) (*service.InvoiceRequest, error) {
	req, err := queryInvoiceRequest(ctx, r.db, `
SELECT `+invoiceRequestColumns+`
FROM invoice_requests
WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return nil, err
	}
	return r.attachInvoiceItems(ctx, req)
}

func (r *invoiceRepository) CancelRequest(ctx context.Context, userID, id int64) (*service.InvoiceRequest, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	current, err := getInvoiceRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if current.UserID != userID {
		return nil, service.ErrInvoiceRequestNotFound
	}
	if current.Status != service.InvoiceStatusPending {
		return nil, service.ErrInvoiceCannotCancel
	}
	req, err := queryInvoiceRequest(ctx, tx, `
UPDATE invoice_requests
SET status = $1, processed_at = NOW(), updated_at = NOW()
WHERE id = $2
RETURNING `+invoiceRequestColumns,
		service.InvoiceStatusCancelled,
		id,
	)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoice_request_items SET active = FALSE WHERE invoice_request_id = $1`, id); err != nil {
		return nil, err
	}
	if err := insertInvoiceEvent(ctx, tx, id, nil, "cancelled", "", nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.attachInvoiceItems(ctx, req)
}

func (r *invoiceRepository) ListRequestsAdmin(ctx context.Context, params service.InvoiceRequestListParams) ([]service.InvoiceRequest, int64, error) {
	return r.listRequests(ctx, params, false)
}

func (r *invoiceRepository) GetRequestByID(ctx context.Context, id int64) (*service.InvoiceRequest, error) {
	req, err := queryInvoiceRequest(ctx, r.db, `
SELECT `+invoiceRequestColumns+`
FROM invoice_requests
WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return r.attachInvoiceItems(ctx, req)
}

func (r *invoiceRepository) IssueRequest(ctx context.Context, id, adminUserID int64, input service.InvoiceIssueInput) (*service.InvoiceRequest, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	current, err := getInvoiceRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != service.InvoiceStatusPending {
		return nil, service.ErrInvoiceCannotIssue
	}
	req, err := queryInvoiceRequest(ctx, tx, `
UPDATE invoice_requests
SET status = $1,
	invoice_number = $2,
	invoice_code = $3,
	invoice_file_url = $4,
	invoice_file_name = $5,
	issued_at = NOW(),
	admin_note = NULLIF($6, ''),
	processed_by_user_id = $7,
	processed_at = NOW(),
	updated_at = NOW()
WHERE id = $8
RETURNING `+invoiceRequestColumns,
		service.InvoiceStatusIssued,
		input.InvoiceNumber,
		input.InvoiceCode,
		input.InvoiceFileURL,
		input.InvoiceFileName,
		input.AdminNote,
		adminUserID,
		id,
	)
	if err != nil {
		return nil, err
	}
	if err := insertInvoiceEvent(ctx, tx, id, &adminUserID, "issued", input.AdminNote, map[string]any{
		"invoice_number": input.InvoiceNumber,
		"invoice_code":   input.InvoiceCode,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.attachInvoiceItems(ctx, req)
}

func (r *invoiceRepository) RejectRequest(ctx context.Context, id, adminUserID int64, reason, adminNote string) (*service.InvoiceRequest, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessDone(tx)

	current, err := getInvoiceRequestForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != service.InvoiceStatusPending {
		return nil, service.ErrInvoiceCannotReject
	}
	req, err := queryInvoiceRequest(ctx, tx, `
UPDATE invoice_requests
SET status = $1,
	rejected_reason = $2,
	admin_note = NULLIF($3, ''),
	processed_by_user_id = $4,
	processed_at = NOW(),
	updated_at = NOW()
WHERE id = $5
RETURNING `+invoiceRequestColumns,
		service.InvoiceStatusRejected,
		reason,
		adminNote,
		adminUserID,
		id,
	)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoice_request_items SET active = FALSE WHERE invoice_request_id = $1`, id); err != nil {
		return nil, err
	}
	if err := insertInvoiceEvent(ctx, tx, id, &adminUserID, "rejected", reason, map[string]any{"admin_note": adminNote}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.attachInvoiceItems(ctx, req)
}

func (r *invoiceRepository) lockInvoiceSource(ctx context.Context, tx *sql.Tx, userID int64, ref service.InvoiceSourceRef) (*service.InvoiceEligibleSource, error) {
	var row *sql.Row
	switch ref.SourceType {
	case service.InvoiceSourceTypePaymentOrder:
		row = tx.QueryRowContext(ctx, `
SELECT 'payment_order' AS source_type,
	id AS source_id,
	COALESCE(NULLIF(out_trade_no, ''), NULLIF(payment_trade_no, ''), NULLIF(recharge_code, ''), id::text) AS source_no,
	CASE order_type
		WHEN 'balance' THEN '余额充值'
		WHEN 'subscription' THEN '订阅购买'
		WHEN 'shop' THEN '商城订单'
		ELSE order_type
	END AS source_label,
	order_type AS item_type,
	amount::double precision AS entitlement_amount,
	GREATEST(pay_amount - refund_amount, 0)::double precision AS invoice_amount,
	COALESCE(paid_at, completed_at, updated_at, created_at) AS occurred_at,
	status
FROM payment_orders
WHERE id = $1
	AND user_id = $2
	AND status IN ('COMPLETED', 'PARTIALLY_REFUNDED')
	AND GREATEST(pay_amount - refund_amount, 0) > 0
	AND NOT EXISTS (
		SELECT 1 FROM invoice_request_items iri
		WHERE iri.source_type = 'payment_order'
			AND iri.source_id = payment_orders.id
			AND iri.active = TRUE
	)
FOR UPDATE`, ref.SourceID, userID)
	case service.InvoiceSourceTypeRedeemCode:
		row = tx.QueryRowContext(ctx, `
SELECT 'redeem_code' AS source_type,
	id AS source_id,
	code AS source_no,
	CASE type
		WHEN 'balance' THEN '兑换码充值余额'
		WHEN 'points' THEN '兑换码充值积分'
		ELSE type
	END AS source_label,
	type AS item_type,
	value::double precision AS entitlement_amount,
	value::double precision AS invoice_amount,
	COALESCE(used_at, created_at) AS occurred_at,
	status
FROM redeem_codes
WHERE id = $1
	AND used_by = $2
	AND status = 'used'
	AND type IN ('balance', 'points')
	AND value > 0
	AND NOT EXISTS (
		SELECT 1 FROM invoice_request_items iri
		WHERE iri.source_type = 'redeem_code'
			AND iri.source_id = redeem_codes.id
			AND iri.active = TRUE
	)
FOR UPDATE`, ref.SourceID, userID)
	default:
		return nil, service.ErrInvoiceSourceInvalid
	}
	source, err := scanInvoiceSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrInvoiceSourceUnavailable
	}
	return source, err
}

func (r *invoiceRepository) listRequests(ctx context.Context, params service.InvoiceRequestListParams, forceUser bool) ([]service.InvoiceRequest, int64, error) {
	page, pageSize := normalizeInvoicePagination(params.Page, params.PageSize)
	where, args := buildInvoiceRequestWhere(params, forceUser)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM invoice_requests"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT `+invoiceRequestColumns+`
FROM invoice_requests`+where+`
ORDER BY created_at DESC, id DESC
LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.InvoiceRequest, 0)
	for rows.Next() {
		item, err := scanInvoiceRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		if _, err := r.attachInvoiceItems(ctx, item); err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func buildInvoiceRequestWhere(params service.InvoiceRequestListParams, forceUser bool) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if forceUser || params.UserID > 0 {
		args = append(args, params.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		clauses = append(clauses, fmt.Sprintf("(request_no ILIKE $%d OR user_email ILIKE $%d OR title_name ILIKE $%d OR invoice_number ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (r *invoiceRepository) attachInvoiceItems(ctx context.Context, req *service.InvoiceRequest) (*service.InvoiceRequest, error) {
	if req == nil {
		return nil, service.ErrInvoiceRequestNotFound
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT `+invoiceRequestItemColumns+`
FROM invoice_request_items
WHERE invoice_request_id = $1
ORDER BY id`, req.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.InvoiceRequestItem, 0)
	for rows.Next() {
		item, err := scanInvoiceRequestItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	req.Items = items
	return req, nil
}

func getInvoiceRequestForUpdate(ctx context.Context, tx *sql.Tx, id int64) (*service.InvoiceRequest, error) {
	req, err := queryInvoiceRequest(ctx, tx, `
SELECT `+invoiceRequestColumns+`
FROM invoice_requests
WHERE id = $1
FOR UPDATE`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrInvoiceRequestNotFound
	}
	return req, err
}

func queryInvoiceProfile(ctx context.Context, q queryRower, query string, args ...any) (*service.InvoiceProfile, error) {
	profile, err := scanInvoiceProfile(q.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrInvoiceProfileNotFound
	}
	return profile, err
}

func queryInvoiceRequest(ctx context.Context, q queryRower, query string, args ...any) (*service.InvoiceRequest, error) {
	req, err := scanInvoiceRequest(q.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrInvoiceRequestNotFound
	}
	return req, err
}

type invoiceScanner interface {
	Scan(dest ...any) error
}

func scanInvoiceProfile(row invoiceScanner) (*service.InvoiceProfile, error) {
	var profile service.InvoiceProfile
	if err := row.Scan(
		&profile.ID,
		&profile.UserID,
		&profile.InvoiceType,
		&profile.BuyerType,
		&profile.TitleName,
		&profile.TaxID,
		&profile.RegisteredAddress,
		&profile.RegisteredPhone,
		&profile.BankName,
		&profile.BankAccount,
		&profile.RecipientEmail,
		&profile.RecipientPhone,
		&profile.IsDefault,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &profile, nil
}

func scanInvoiceRequest(row invoiceScanner) (*service.InvoiceRequest, error) {
	var req service.InvoiceRequest
	var issuedAt, processedAt sql.NullTime
	var rejectedReason, adminNote sql.NullString
	var processedBy sql.NullInt64
	if err := row.Scan(
		&req.ID,
		&req.RequestNo,
		&req.UserID,
		&req.UserEmail,
		&req.InvoiceType,
		&req.BuyerType,
		&req.TitleName,
		&req.TaxID,
		&req.RegisteredAddress,
		&req.RegisteredPhone,
		&req.BankName,
		&req.BankAccount,
		&req.RecipientEmail,
		&req.RecipientPhone,
		&req.Amount,
		&req.Currency,
		&req.Status,
		&req.InvoiceNumber,
		&req.InvoiceCode,
		&req.InvoiceFileURL,
		&req.InvoiceFileName,
		&issuedAt,
		&rejectedReason,
		&adminNote,
		&processedBy,
		&req.SubmittedAt,
		&processedAt,
		&req.CreatedAt,
		&req.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if issuedAt.Valid {
		req.IssuedAt = &issuedAt.Time
	}
	if rejectedReason.Valid {
		req.RejectedReason = &rejectedReason.String
	}
	if adminNote.Valid {
		req.AdminNote = &adminNote.String
	}
	if processedBy.Valid {
		req.ProcessedByUserID = &processedBy.Int64
	}
	if processedAt.Valid {
		req.ProcessedAt = &processedAt.Time
	}
	return &req, nil
}

func scanInvoiceRequestItem(row invoiceScanner) (*service.InvoiceRequestItem, error) {
	var item service.InvoiceRequestItem
	if err := row.Scan(
		&item.ID,
		&item.InvoiceRequestID,
		&item.SourceType,
		&item.SourceID,
		&item.SourceNo,
		&item.SourceLabel,
		&item.ItemType,
		&item.EntitlementAmount,
		&item.InvoiceAmount,
		&item.OccurredAt,
		&item.Active,
		&item.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &item, nil
}

func scanInvoiceSource(row invoiceScanner) (*service.InvoiceEligibleSource, error) {
	var source service.InvoiceEligibleSource
	if err := row.Scan(
		&source.SourceType,
		&source.SourceID,
		&source.SourceNo,
		&source.SourceLabel,
		&source.ItemType,
		&source.EntitlementAmount,
		&source.InvoiceAmount,
		&source.OccurredAt,
		&source.Status,
	); err != nil {
		return nil, err
	}
	return &source, nil
}

func insertInvoiceEvent(ctx context.Context, tx *sql.Tx, requestID int64, operatorID *int64, action, note string, metadata map[string]any) error {
	raw := "{}"
	if metadata != nil {
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		raw = string(encoded)
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO invoice_events (invoice_request_id, action, operator_user_id, note, metadata)
VALUES ($1, $2, $3, $4, $5::jsonb)`,
		requestID,
		action,
		sqlNullInt64(operatorID),
		strings.TrimSpace(note),
		raw,
	)
	return err
}

func sqlNullInt64(value *int64) sql.NullInt64 {
	if value == nil || *value <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func invoiceBuyerTypeForDB(invoiceType string) string {
	if invoiceType == service.InvoiceTypePersonalNormal {
		return service.InvoiceBuyerTypePersonal
	}
	return service.InvoiceBuyerTypeEnterprise
}

func normalizeInvoiceAmountForDB(amount float64) (float64, bool) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return 0, false
	}
	rounded := math.Round(amount*100) / 100
	return rounded, rounded > 0
}

func normalizeInvoicePagination(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	return page, pageSize
}

func isInvoiceUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

const invoiceProfileColumns = `
id, user_id, invoice_type, buyer_type, title_name, tax_id, registered_address,
registered_phone, bank_name, bank_account, recipient_email, recipient_phone,
is_default, created_at, updated_at`

const invoiceRequestColumns = `
id, request_no, user_id, user_email, invoice_type, buyer_type, title_name, tax_id,
registered_address, registered_phone, bank_name, bank_account, recipient_email,
recipient_phone, amount::double precision, currency, status, invoice_number,
invoice_code, invoice_file_url, invoice_file_name, issued_at, rejected_reason,
admin_note, processed_by_user_id, submitted_at, processed_at, created_at, updated_at`

const invoiceRequestItemColumns = `
id, invoice_request_id, source_type, source_id, source_no, source_label, item_type,
entitlement_amount::double precision, invoice_amount::double precision, occurred_at,
active, created_at`

const eligibleInvoiceSourcesSQL = `
WITH sources AS (
	SELECT
		'payment_order' AS source_type,
		po.id AS source_id,
		COALESCE(NULLIF(po.out_trade_no, ''), NULLIF(po.payment_trade_no, ''), NULLIF(po.recharge_code, ''), po.id::text) AS source_no,
		CASE po.order_type
			WHEN 'balance' THEN '余额充值'
			WHEN 'subscription' THEN '订阅购买'
			WHEN 'shop' THEN '商城订单'
			ELSE po.order_type
		END AS source_label,
		po.order_type AS item_type,
		po.amount AS entitlement_amount,
		GREATEST(po.pay_amount - po.refund_amount, 0) AS invoice_amount,
		COALESCE(po.paid_at, po.completed_at, po.updated_at, po.created_at) AS occurred_at,
		po.status AS status
	FROM payment_orders po
	WHERE po.user_id = $1
		AND po.status IN ('COMPLETED', 'PARTIALLY_REFUNDED')
		AND GREATEST(po.pay_amount - po.refund_amount, 0) > 0
		AND NOT EXISTS (
			SELECT 1 FROM invoice_request_items iri
			WHERE iri.source_type = 'payment_order'
				AND iri.source_id = po.id
				AND iri.active = TRUE
		)
	UNION ALL
	SELECT
		'redeem_code' AS source_type,
		rc.id AS source_id,
		rc.code AS source_no,
		CASE rc.type
			WHEN 'balance' THEN '兑换码充值余额'
			WHEN 'points' THEN '兑换码充值积分'
			ELSE rc.type
		END AS source_label,
		rc.type AS item_type,
		rc.value AS entitlement_amount,
		rc.value AS invoice_amount,
		COALESCE(rc.used_at, rc.created_at) AS occurred_at,
		rc.status AS status
	FROM redeem_codes rc
	WHERE rc.used_by = $1
		AND rc.status = 'used'
		AND rc.type IN ('balance', 'points')
		AND rc.value > 0
		AND NOT EXISTS (
			SELECT 1 FROM invoice_request_items iri
			WHERE iri.source_type = 'redeem_code'
				AND iri.source_id = rc.id
				AND iri.active = TRUE
		)
)`
