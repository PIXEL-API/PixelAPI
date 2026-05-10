package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type subsiteRepository struct {
	db *sql.DB
}

func NewSubsiteRepository(db *sql.DB) service.SubsiteRepository {
	return &subsiteRepository{db: db}
}

func (r *subsiteRepository) Create(ctx context.Context, subsite *service.Subsite) error {
	if subsite == nil {
		return service.ErrSubsiteInvalidInput
	}
	capabilitiesJSON, err := json.Marshal(subsite.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal subsite capabilities: %w", err)
	}
	metadataJSON, err := json.Marshal(subsite.Metadata)
	if err != nil {
		return fmt.Errorf("marshal subsite metadata: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO subsites (
			subsite_id, name, public_url, region, capabilities, status,
			secret_hash, secret_ciphertext, max_qps, max_concurrency,
			version, health_score, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, updated_at
	`,
		subsite.SubsiteID,
		subsite.Name,
		subsite.PublicURL,
		subsite.Region,
		capabilitiesJSON,
		subsite.Status,
		subsite.SecretHash,
		subsite.SecretCiphertext,
		subsite.MaxQPS,
		subsite.MaxConcurrency,
		subsite.Version,
		subsite.HealthScore,
		metadataJSON,
	).Scan(&subsite.ID, &subsite.CreatedAt, &subsite.UpdatedAt)
	if err != nil {
		if isUniqueConstraintViolation(err) {
			return service.ErrQuotaReservationConflict.WithCause(err)
		}
		return fmt.Errorf("insert subsite: %w", err)
	}
	return nil
}

func (r *subsiteRepository) GetBySubsiteID(ctx context.Context, subsiteID string) (*service.Subsite, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, subsite_id, name, public_url, region, capabilities, status,
			secret_hash, secret_ciphertext, max_qps, max_concurrency, version,
			last_heartbeat_at, health_score, last_seen_ip, metadata,
			created_at, updated_at, deleted_at
		FROM subsites
		WHERE subsite_id = $1 AND deleted_at IS NULL
	`, strings.TrimSpace(subsiteID))
	subsite, err := scanSubsite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSubsiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get subsite: %w", err)
	}
	return subsite, nil
}

func (r *subsiteRepository) List(ctx context.Context, params pagination.PaginationParams, filter service.ListSubsitesFilter) ([]service.Subsite, *pagination.PaginationResult, error) {
	where := []string{"deleted_at IS NULL"}
	args := []any{}
	argIdx := 1
	if filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.Status))
		argIdx++
	}
	if filter.Search != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR subsite_id ILIKE $%d OR public_url ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, "%"+escapeLike(strings.TrimSpace(filter.Search))+"%")
		argIdx++
	}
	whereClause := strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM subsites WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count subsites: %w", err)
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.Limit()
	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, subsite_id, name, public_url, region, capabilities, status,
			secret_hash, secret_ciphertext, max_qps, max_concurrency, version,
			last_heartbeat_at, health_score, last_seen_ip, metadata,
			created_at, updated_at, deleted_at
		FROM subsites
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	rows, err := r.db.QueryContext(ctx, query, append(args, pageSize, offset)...)
	if err != nil {
		return nil, nil, fmt.Errorf("list subsites: %w", err)
	}
	defer rows.Close()
	items := make([]service.Subsite, 0)
	for rows.Next() {
		subsite, err := scanSubsite(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, *subsite)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if pages < 1 {
		pages = 1
	}
	return items, &pagination.PaginationResult{Total: total, Page: page, PageSize: pageSize, Pages: pages}, nil
}

func (r *subsiteRepository) Update(ctx context.Context, subsite *service.Subsite) error {
	capabilitiesJSON, err := json.Marshal(subsite.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal subsite capabilities: %w", err)
	}
	metadataJSON, err := json.Marshal(subsite.Metadata)
	if err != nil {
		return fmt.Errorf("marshal subsite metadata: %w", err)
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE subsites
		SET name = $1,
			public_url = $2,
			region = $3,
			capabilities = $4,
			max_qps = $5,
			max_concurrency = $6,
			version = $7,
			metadata = $8,
			updated_at = NOW()
		WHERE subsite_id = $9 AND deleted_at IS NULL
	`,
		subsite.Name,
		subsite.PublicURL,
		subsite.Region,
		capabilitiesJSON,
		subsite.MaxQPS,
		subsite.MaxConcurrency,
		subsite.Version,
		metadataJSON,
		subsite.SubsiteID,
	)
	if err != nil {
		if isUniqueConstraintViolation(err) {
			return service.ErrQuotaReservationConflict.WithCause(err)
		}
		return fmt.Errorf("update subsite: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrSubsiteNotFound
	}
	return nil
}

func (r *subsiteRepository) UpdateStatus(ctx context.Context, subsiteID, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE subsites
		SET status = $1, updated_at = NOW()
		WHERE subsite_id = $2 AND deleted_at IS NULL
	`, strings.TrimSpace(status), strings.TrimSpace(subsiteID))
	if err != nil {
		return fmt.Errorf("update subsite status: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrSubsiteNotFound
	}
	return nil
}

func (r *subsiteRepository) RecordHeartbeat(ctx context.Context, heartbeat *service.SubsiteHeartbeat) error {
	if heartbeat == nil {
		return service.ErrSubsiteInvalidInput
	}
	metadataJSON, err := json.Marshal(heartbeat.Metadata)
	if err != nil {
		return fmt.Errorf("marshal heartbeat metadata: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin heartbeat tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	err = tx.QueryRowContext(ctx, `
		INSERT INTO subsite_heartbeats (
			subsite_id, status, version, active_requests, queued_usage, qps,
			cpu_percent, memory_bytes, metadata, reported_at, remote_ip
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at
	`,
		heartbeat.SubsiteID,
		heartbeat.Status,
		heartbeat.Version,
		heartbeat.ActiveRequests,
		heartbeat.QueuedUsage,
		heartbeat.QPS,
		heartbeat.CPUPercent,
		heartbeat.MemoryBytes,
		metadataJSON,
		heartbeat.ReportedAt,
		heartbeat.RemoteIP,
	).Scan(&heartbeat.ID, &heartbeat.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert subsite heartbeat: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE subsites
		SET status = CASE
				WHEN status = 'disabled' THEN status
				WHEN $2 IN ('active', 'maintenance', 'unhealthy') THEN $2
				ELSE status
			END,
			version = COALESCE(NULLIF($3, ''), version),
			last_heartbeat_at = $4,
			last_seen_ip = $5,
			health_score = CASE
				WHEN $2 = 'unhealthy' THEN 0
				WHEN $2 = 'maintenance' THEN 50
				ELSE 100
			END,
			updated_at = NOW()
		WHERE subsite_id = $1 AND deleted_at IS NULL
	`, heartbeat.SubsiteID, heartbeat.Status, heartbeat.Version, heartbeat.ReportedAt, heartbeat.RemoteIP)
	if err != nil {
		return fmt.Errorf("update subsite heartbeat: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrSubsiteNotFound
	}
	return tx.Commit()
}

type accountLeaseRepository struct {
	db *sql.DB
}

func NewAccountLeaseRepository(db *sql.DB) service.AccountLeaseRepository {
	return &accountLeaseRepository{db: db}
}

func (r *accountLeaseRepository) Create(ctx context.Context, lease *service.AccountLease) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO account_leases (
			lease_id, subsite_id, account_id, platform, status, max_concurrency,
			max_requests, max_tokens, assigned_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`,
		lease.LeaseID,
		lease.SubsiteID,
		lease.AccountID,
		lease.Platform,
		lease.Status,
		lease.MaxConcurrency,
		lease.MaxRequests,
		lease.MaxTokens,
		lease.AssignedAt,
		lease.ExpiresAt,
	).Scan(&lease.ID, &lease.CreatedAt, &lease.UpdatedAt)
	if err != nil {
		if isUniqueConstraintViolation(err) {
			return service.ErrAccountLeaseConflict.WithCause(err)
		}
		return fmt.Errorf("insert account lease: %w", err)
	}
	return nil
}

func (r *accountLeaseRepository) GetByLeaseID(ctx context.Context, leaseID string) (*service.AccountLease, error) {
	lease, err := scanAccountLease(r.db.QueryRowContext(ctx, `
		SELECT id, lease_id, subsite_id, account_id, platform, status, max_concurrency,
			max_requests, max_tokens, used_requests, used_tokens, assigned_at, expires_at,
			renewed_at, released_at, created_at, updated_at
		FROM account_leases
		WHERE lease_id = $1 AND deleted_at IS NULL
	`, strings.TrimSpace(leaseID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountLeaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account lease: %w", err)
	}
	return lease, nil
}

func (r *accountLeaseRepository) ListBySubsite(ctx context.Context, subsiteID string) ([]service.AccountLease, error) {
	return r.list(ctx, `
		SELECT id, lease_id, subsite_id, account_id, platform, status, max_concurrency,
			max_requests, max_tokens, used_requests, used_tokens, assigned_at, expires_at,
			renewed_at, released_at, created_at, updated_at
		FROM account_leases
		WHERE subsite_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, strings.TrimSpace(subsiteID))
}

func (r *accountLeaseRepository) ListActiveBySubsite(ctx context.Context, subsiteID string) ([]service.AccountLease, error) {
	return r.list(ctx, `
		SELECT id, lease_id, subsite_id, account_id, platform, status, max_concurrency,
			max_requests, max_tokens, used_requests, used_tokens, assigned_at, expires_at,
			renewed_at, released_at, created_at, updated_at
		FROM account_leases
		WHERE subsite_id = $1
		  AND deleted_at IS NULL
		  AND status IN ('active', 'renewing')
		  AND expires_at > NOW()
		ORDER BY assigned_at ASC
	`, strings.TrimSpace(subsiteID))
}

func (r *accountLeaseRepository) Renew(ctx context.Context, leaseID string, expiresAt time.Time) (*service.AccountLease, error) {
	return r.updateLease(ctx, `
		UPDATE account_leases
		SET status = 'renewing', expires_at = $1, renewed_at = NOW(), updated_at = NOW()
		WHERE lease_id = $2 AND deleted_at IS NULL AND status IN ('active', 'renewing')
	`, expiresAt, strings.TrimSpace(leaseID))
}

func (r *accountLeaseRepository) Release(ctx context.Context, leaseID string) (*service.AccountLease, error) {
	return r.updateLease(ctx, `
		UPDATE account_leases
		SET status = 'released', released_at = NOW(), updated_at = NOW()
		WHERE lease_id = $1 AND deleted_at IS NULL AND status IN ('active', 'renewing', 'draining')
	`, strings.TrimSpace(leaseID))
}

func (r *accountLeaseRepository) Drain(ctx context.Context, leaseID string) (*service.AccountLease, error) {
	return r.updateLease(ctx, `
		UPDATE account_leases
		SET status = 'draining', updated_at = NOW()
		WHERE lease_id = $1 AND deleted_at IS NULL AND status IN ('active', 'renewing')
	`, strings.TrimSpace(leaseID))
}

func (r *accountLeaseRepository) ExpireStale(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE account_leases
		SET status = 'expired', updated_at = NOW()
		WHERE deleted_at IS NULL
		  AND status IN ('active', 'renewing', 'draining')
		  AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire stale account leases: %w", err)
	}
	return res.RowsAffected()
}

func (r *accountLeaseRepository) list(ctx context.Context, query string, args ...any) ([]service.AccountLease, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list account leases: %w", err)
	}
	defer rows.Close()
	leases := make([]service.AccountLease, 0)
	for rows.Next() {
		lease, err := scanAccountLease(rows)
		if err != nil {
			return nil, err
		}
		leases = append(leases, *lease)
	}
	return leases, rows.Err()
}

func (r *accountLeaseRepository) updateLease(ctx context.Context, query string, args ...any) (*service.AccountLease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin lease tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update account lease: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, service.ErrAccountLeaseNotFound
	}
	var leaseID string
	switch v := args[len(args)-1].(type) {
	case string:
		leaseID = v
	default:
		leaseID = fmt.Sprint(v)
	}
	lease, err := scanAccountLease(tx.QueryRowContext(ctx, `
		SELECT id, lease_id, subsite_id, account_id, platform, status, max_concurrency,
			max_requests, max_tokens, used_requests, used_tokens, assigned_at, expires_at,
			renewed_at, released_at, created_at, updated_at
		FROM account_leases
		WHERE lease_id = $1 AND deleted_at IS NULL
	`, leaseID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return lease, nil
}

type quotaReservationRepository struct {
	db *sql.DB
}

func NewQuotaReservationRepository(db *sql.DB) service.QuotaReservationRepository {
	return &quotaReservationRepository{db: db}
}

func (r *quotaReservationRepository) Create(ctx context.Context, reservation *service.QuotaReservation) error {
	if reservation == nil {
		return service.ErrSubsiteInvalidInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin quota reservation tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if reservation.BillingType == service.BillingTypeBalance {
		var balance, reserved float64
		if err := tx.QueryRowContext(ctx, `
			SELECT balance
			FROM users
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, reservation.UserID).Scan(&balance); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return service.ErrUserNotFound
			}
			return fmt.Errorf("lock user balance: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(estimated_cost), 0)
			FROM quota_reservations
			WHERE user_id = $1
			  AND status = 'reserved'
			  AND expires_at > NOW()
		`, reservation.UserID).Scan(&reserved); err != nil {
			return fmt.Errorf("sum reserved balance: %w", err)
		}
		if balance-reserved < reservation.EstimatedCost {
			return service.ErrQuotaReservationInsufficientFunds
		}
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO quota_reservations (
			reservation_id, request_id, subsite_id, lease_id, account_id,
			api_key_id, user_id, group_id, subscription_id, platform,
			requested_model, mapped_model, estimated_cost, billing_type,
			status, request_fingerprint, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, created_at, updated_at
	`,
		reservation.ReservationID,
		reservation.RequestID,
		reservation.SubsiteID,
		reservation.LeaseID,
		reservation.AccountID,
		reservation.APIKeyID,
		reservation.UserID,
		reservation.GroupID,
		reservation.SubscriptionID,
		reservation.Platform,
		reservation.RequestedModel,
		reservation.MappedModel,
		reservation.EstimatedCost,
		reservation.BillingType,
		reservation.Status,
		reservation.RequestFingerprint,
		reservation.ExpiresAt,
	).Scan(&reservation.ID, &reservation.CreatedAt, &reservation.UpdatedAt)
	if err != nil {
		if isUniqueConstraintViolation(err) {
			return service.ErrQuotaReservationConflict.WithCause(err)
		}
		return fmt.Errorf("insert quota reservation: %w", err)
	}
	return tx.Commit()
}

func (r *quotaReservationRepository) GetByRequestID(ctx context.Context, requestID string) (*service.QuotaReservation, error) {
	reservation, err := scanQuotaReservation(r.db.QueryRowContext(ctx, quotaReservationSelectSQL()+" WHERE request_id = $1", strings.TrimSpace(requestID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrQuotaReservationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get quota reservation by request id: %w", err)
	}
	return reservation, nil
}

func (r *quotaReservationRepository) GetByReservationID(ctx context.Context, reservationID string) (*service.QuotaReservation, error) {
	reservation, err := scanQuotaReservation(r.db.QueryRowContext(ctx, quotaReservationSelectSQL()+" WHERE reservation_id = $1", strings.TrimSpace(reservationID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrQuotaReservationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get quota reservation by reservation id: %w", err)
	}
	return reservation, nil
}

func (r *quotaReservationRepository) Cancel(ctx context.Context, requestID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE quota_reservations
		SET status = 'canceled',
			updated_at = NOW()
		WHERE request_id = $1
		  AND status = 'reserved'
	`, strings.TrimSpace(requestID))
	if err != nil {
		return fmt.Errorf("cancel quota reservation: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrQuotaReservationNotFound
	}
	return nil
}

func (r *quotaReservationRepository) Settle(ctx context.Context, requestID string, actualCost float64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE quota_reservations
		SET status = 'settled',
			actual_cost = $1,
			settled_at = COALESCE(settled_at, NOW()),
			updated_at = NOW()
		WHERE request_id = $2
		  AND status IN ('reserved', 'settled')
	`, actualCost, strings.TrimSpace(requestID))
	if err != nil {
		return fmt.Errorf("settle quota reservation: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrQuotaReservationNotFound
	}
	return nil
}

type subsiteNonceStore struct {
	rdb *redis.Client
}

func NewSubsiteNonceStore(rdb *redis.Client) service.SubsiteNonceStore {
	return &subsiteNonceStore{rdb: rdb}
}

func (s *subsiteNonceStore) Claim(ctx context.Context, subsiteID, nonce string, ttl time.Duration) (bool, error) {
	if s == nil || s.rdb == nil {
		return false, errors.New("subsite nonce store redis client is nil")
	}
	key := "subsite:nonce:" + strings.TrimSpace(subsiteID) + ":" + strings.TrimSpace(nonce)
	ok, err := s.rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("claim subsite nonce: %w", err)
	}
	return ok, nil
}

func (r *quotaReservationRepository) ExpireStale(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE quota_reservations
		SET status = 'expired', updated_at = NOW()
		WHERE status = 'reserved' AND expires_at <= $1
	`, now)
	if err != nil {
		return 0, fmt.Errorf("expire stale quota reservations: %w", err)
	}
	return res.RowsAffected()
}

type subsiteRowScanner interface {
	Scan(dest ...any) error
}

func scanSubsite(row subsiteRowScanner) (*service.Subsite, error) {
	subsite := &service.Subsite{}
	var capabilitiesRaw, metadataRaw []byte
	err := row.Scan(
		&subsite.ID,
		&subsite.SubsiteID,
		&subsite.Name,
		&subsite.PublicURL,
		&subsite.Region,
		&capabilitiesRaw,
		&subsite.Status,
		&subsite.SecretHash,
		&subsite.SecretCiphertext,
		&subsite.MaxQPS,
		&subsite.MaxConcurrency,
		&subsite.Version,
		&subsite.LastHeartbeatAt,
		&subsite.HealthScore,
		&subsite.LastSeenIP,
		&metadataRaw,
		&subsite.CreatedAt,
		&subsite.UpdatedAt,
		&subsite.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	subsite.Capabilities = decodeStringSlice(capabilitiesRaw)
	subsite.Metadata = decodeJSONMap(metadataRaw)
	return subsite, nil
}

func scanAccountLease(row subsiteRowScanner) (*service.AccountLease, error) {
	lease := &service.AccountLease{}
	err := row.Scan(
		&lease.ID,
		&lease.LeaseID,
		&lease.SubsiteID,
		&lease.AccountID,
		&lease.Platform,
		&lease.Status,
		&lease.MaxConcurrency,
		&lease.MaxRequests,
		&lease.MaxTokens,
		&lease.UsedRequests,
		&lease.UsedTokens,
		&lease.AssignedAt,
		&lease.ExpiresAt,
		&lease.RenewedAt,
		&lease.ReleasedAt,
		&lease.CreatedAt,
		&lease.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func quotaReservationSelectSQL() string {
	return `
		SELECT id, reservation_id, request_id, subsite_id, lease_id, account_id,
			api_key_id, user_id, group_id, subscription_id, platform,
			requested_model, mapped_model, estimated_cost, actual_cost,
			billing_type, status, request_fingerprint, expires_at, settled_at,
			created_at, updated_at
		FROM quota_reservations
	`
}

func scanQuotaReservation(row subsiteRowScanner) (*service.QuotaReservation, error) {
	reservation := &service.QuotaReservation{}
	err := row.Scan(
		&reservation.ID,
		&reservation.ReservationID,
		&reservation.RequestID,
		&reservation.SubsiteID,
		&reservation.LeaseID,
		&reservation.AccountID,
		&reservation.APIKeyID,
		&reservation.UserID,
		&reservation.GroupID,
		&reservation.SubscriptionID,
		&reservation.Platform,
		&reservation.RequestedModel,
		&reservation.MappedModel,
		&reservation.EstimatedCost,
		&reservation.ActualCost,
		&reservation.BillingType,
		&reservation.Status,
		&reservation.RequestFingerprint,
		&reservation.ExpiresAt,
		&reservation.SettledAt,
		&reservation.CreatedAt,
		&reservation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return reservation, nil
}

func decodeStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func decodeJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
