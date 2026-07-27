package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountShareModeRepository) GetAccountShareQuotaUsage(ctx context.Context, ownerUserID int64) (*service.AccountShareQuotaUsage, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	return getAccountShareQuotaUsageWithQueryer(ctx, r.db, ownerUserID)
}

func getAccountShareQuotaUsageWithQueryer(
	ctx context.Context,
	queryer accountShareQuotaQueryer,
	ownerUserID int64,
) (*service.AccountShareQuotaUsage, error) {
	if queryer == nil {
		return nil, service.ErrServiceUnavailable
	}
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	usage := &service.AccountShareQuotaUsage{}
	if err := queryer.QueryRowContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)::int
				FROM account_share_listings listing
				WHERE listing.owner_user_id = $1
					AND listing.deleted_at IS NULL
			),
			(
				SELECT COUNT(*)::int
				FROM account_share_listings listing
				WHERE listing.owner_user_id = $1
					AND listing.created_at >= NOW() - INTERVAL '24 hours'
			),
			(
				SELECT COUNT(*)::int
				FROM account_share_room_accounts room_account
				JOIN account_share_listings listing ON listing.id = room_account.listing_id
				WHERE listing.owner_user_id = $1
					AND listing.deleted_at IS NULL
					AND room_account.state IN ('active', 'draining')
			),
			(
				SELECT COALESCE(MAX(room_account_count), 0)::int
				FROM (
					SELECT COUNT(*)::int AS room_account_count
					FROM account_share_room_accounts room_account
					JOIN account_share_listings listing ON listing.id = room_account.listing_id
					WHERE listing.owner_user_id = $1
						AND listing.deleted_at IS NULL
						AND room_account.state IN ('active', 'draining')
					GROUP BY room_account.listing_id
				) room_counts
			)
	`, ownerUserID).Scan(
		&usage.LiveRooms,
		&usage.RoomCreates24Hours,
		&usage.OwnerRoomAccounts,
		&usage.LargestRoomAccounts,
	); err != nil {
		return nil, err
	}
	return usage, nil
}

var _ accountShareQuotaQueryer = (*sql.DB)(nil)
var _ accountShareQuotaQueryer = (*sql.Tx)(nil)
