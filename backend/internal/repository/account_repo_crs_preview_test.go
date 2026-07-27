package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestListCRSAccountPreviewSnapshotsUsesSingleStableReadOnlyQuery(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actual), " "))
		required := []string{
			"from accounts account_row",
			"left join account_share_room_accounts room_account",
			"left join account_share_listings listing",
			"listing.deleted_at is null",
			"account_row.deleted_at is null",
			"order by account_row.id, listing.id nulls last",
		}
		for _, fragment := range required {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("query is missing %q: %s", fragment, normalized)
			}
		}
		if strings.Contains(normalized, "for update") {
			return fmt.Errorf("preview query must not acquire row locks: %s", normalized)
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("crs-preview").
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"crs_account_id",
			"listing_id",
			"row_version",
		}).
			AddRow(int64(7), "crs-b", int64(12), int64(4)).
			AddRow(int64(7), "crs-b", int64(12), int64(4)).
			AddRow(int64(7), "crs-b", int64(15), int64(9)).
			AddRow(int64(9), "crs-a", nil, nil))

	repo := newAccountRepositoryWithSQL(nil, db, nil)
	got, err := repo.ListCRSAccountPreviewSnapshots(context.Background())

	require.NoError(t, err)
	require.Equal(t, []service.CRSAccountPreviewSnapshot{
		{
			CRSAccountID:   "crs-b",
			LocalAccountID: 7,
			RoomBindings: []service.CRSAccountRoomBindingSnapshot{
				{ListingID: 12, RowVersion: 4},
				{ListingID: 15, RowVersion: 9},
			},
		},
		{
			CRSAccountID:   "crs-a",
			LocalAccountID: 9,
			RoomBindings:   []service.CRSAccountRoomBindingSnapshot{},
		},
	}, got)
	require.NoError(t, mock.ExpectationsWereMet(), "preview should use exactly one batch query")
}

func TestListCRSAccountPreviewSnapshotsFailsClosedWithoutSQLExecutor(t *testing.T) {
	repo := newAccountRepositoryWithSQL(nil, nil, nil)

	_, err := repo.ListCRSAccountPreviewSnapshots(context.Background())

	require.ErrorIs(t, err, service.ErrCRSPreviewSnapshotUnavailable)
}
