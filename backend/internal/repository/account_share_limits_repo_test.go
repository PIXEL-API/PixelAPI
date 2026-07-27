package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetAccountShareQuotaUsageCountsOnlyCurrentRoomProjection(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})

	const ownerUserID int64 = 42
	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"live_rooms",
			"room_creates_24_hours",
			"owner_room_accounts",
			"largest_room_accounts",
		}).AddRow(2, 3, 7, 4))

	repo := &accountShareModeRepository{db: db}
	got, err := repo.GetAccountShareQuotaUsage(context.Background(), ownerUserID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 2, got.LiveRooms)
	require.Equal(t, 3, got.RoomCreates24Hours)
	require.Equal(t, 7, got.OwnerRoomAccounts)
	require.Equal(t, 4, got.LargestRoomAccounts)
}
