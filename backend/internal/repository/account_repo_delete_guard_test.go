package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLoadAccountDeletionBlockersCollectsStructuredMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT room_account\.listing_id, room_account\.state, COALESCE\(listing\.room_name, ''\).*FROM account_share_room_accounts room_account.*LEFT JOIN account_share_listings listing.*WHERE room_account\.account_id = \$1.*ORDER BY room_account\.listing_id`).
		WithArgs(int64(55)).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id", "state", "room_name"}).
			AddRow(int64(91), "failed", "共享,房间"))
	mock.ExpectQuery(`(?s)SELECT id, listing_id, status, COUNT\(\*\) OVER \(\).*account_share_memberships.*status IN \('active', 'queued', 'ending'\)`).
		WithArgs(int64(55), accountDeletionBlockerSampleLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "status", "count"}).
			AddRow(int64(1001), int64(91), "active", int64(3)).
			AddRow(int64(1002), int64(91), "ending", int64(3)))
	mock.ExpectQuery(`(?s)SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.account_share_membership_account_bindings").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT id, membership_id, listing_id, COUNT\(\*\) OVER \(\).*account_share_membership_account_bindings.*unbound_at IS NULL`).
		WithArgs(int64(55), accountDeletionBlockerSampleLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "membership_id", "listing_id", "count"}).
			AddRow(int64(3001), int64(1003), int64(91), int64(2)))
	mock.ExpectQuery(`(?s)SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.account_share_request_billing_intents").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT id, status, COUNT\(\*\) OVER \(\).*account_share_request_billing_intents.*status NOT IN \('settled', 'cancelled'\)`).
		WithArgs(int64(55), accountDeletionBlockerSampleLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "count"}).
			AddRow(int64(2001), "needs_attention", int64(1)))

	blockers, err := loadAccountDeletionBlockers(context.Background(), db, 55)

	require.NoError(t, err)
	require.True(t, blockers.hasAny())
	appErr := infraerrors.FromError(blockers.conflictError(55))
	require.ErrorIs(t, appErr, service.ErrAccountDeletionBlocked)
	require.Equal(t, "55", appErr.Metadata["account_id"])
	require.Equal(t, "room_account,live_membership,open_binding,pending_billing_intent", appErr.Metadata["blocker_types"])
	require.Equal(t, "91", appErr.Metadata["room_listing_ids"])
	require.Equal(t, "failed", appErr.Metadata["room_account_states"])
	require.Equal(t, "共享 房间", appErr.Metadata["room_listing_names"])
	require.Equal(t, "3", appErr.Metadata["live_membership_count"])
	require.Equal(t, "1001,1002", appErr.Metadata["live_membership_sample_ids"])
	require.Equal(t, "true", appErr.Metadata["live_membership_sample_truncated"])
	require.Equal(t, "2", appErr.Metadata["open_binding_count"])
	require.Equal(t, "3001", appErr.Metadata["open_binding_sample_ids"])
	require.Equal(t, "1003", appErr.Metadata["open_binding_membership_sample_ids"])
	require.Equal(t, "91", appErr.Metadata["open_binding_listing_sample_ids"])
	require.Equal(t, "true", appErr.Metadata["open_binding_sample_truncated"])
	require.Equal(t, "1", appErr.Metadata["pending_billing_intent_count"])
	require.Equal(t, "2001", appErr.Metadata["pending_billing_intent_sample_ids"])
	require.Equal(t, "needs_attention", appErr.Metadata["pending_billing_intent_sample_states"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadAccountDeletionBlockersSkipsOptionalQueriesWhenTablesDoNotExist(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT room_account\.listing_id, room_account\.state.*account_share_room_accounts room_account`).
		WithArgs(int64(55)).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id", "state", "room_name"}))
	mock.ExpectQuery(`(?s)SELECT id, listing_id, status, COUNT\(\*\) OVER \(\).*account_share_memberships`).
		WithArgs(int64(55), accountDeletionBlockerSampleLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "status", "count"}))
	mock.ExpectQuery(`(?s)SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.account_share_membership_account_bindings").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`(?s)SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.account_share_request_billing_intents").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	blockers, err := loadAccountDeletionBlockers(context.Background(), db, 55)

	require.NoError(t, err)
	require.False(t, blockers.hasAny())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadAccountDeletionBlockersFailsClosedWhenSchemaDetectionFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT room_account\.listing_id, room_account\.state.*account_share_room_accounts room_account`).
		WithArgs(int64(55)).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id", "state", "room_name"}))
	mock.ExpectQuery(`(?s)SELECT id, listing_id, status, COUNT\(\*\) OVER \(\).*account_share_memberships`).
		WithArgs(int64(55), accountDeletionBlockerSampleLimit).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "status", "count"}))
	mock.ExpectQuery(`(?s)SELECT to_regclass\(\$1\) IS NOT NULL`).
		WithArgs("public.account_share_membership_account_bindings").
		WillReturnError(errors.New("catalog unavailable"))

	blockers, err := loadAccountDeletionBlockers(context.Background(), db, 55)

	require.Error(t, err)
	require.ErrorContains(t, err, `detect optional account-share table "public.account_share_membership_account_bindings"`)
	require.False(t, blockers.hasAny())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizeAccountDeletionIDsSortsAndDeduplicates(t *testing.T) {
	require.Equal(t, []int64{3, 7, 9}, normalizeAccountDeletionIDs([]int64{9, 3, 7, 3, 9}))
}

func TestValidateLockedAccountDeletionOwnership(t *testing.T) {
	expectedOwnerUserID := int64(9)
	otherOwnerUserID := int64(10)

	require.NoError(t, validateLockedAccountDeletionOwnership(
		[]*dbent.Account{{ID: 55, OwnerUserID: &expectedOwnerUserID}},
		&expectedOwnerUserID,
	))
	require.NoError(t, validateLockedAccountDeletionOwnership(
		[]*dbent.Account{{ID: 55, OwnerUserID: &otherOwnerUserID}},
		nil,
	))
	require.ErrorIs(t, validateLockedAccountDeletionOwnership(
		[]*dbent.Account{{ID: 55, OwnerUserID: &otherOwnerUserID}},
		&expectedOwnerUserID,
	), service.ErrAccountNotFound)
	require.ErrorIs(t, validateLockedAccountDeletionOwnership(
		[]*dbent.Account{{ID: 55}},
		&expectedOwnerUserID,
	), service.ErrAccountNotFound)
}
