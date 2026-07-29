package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingAdminRepositoryListsOnlyNeedsAttention(t *testing.T) {
	repo, mock, cleanup := newAccountShareBillingIntentRepositorySQLMock(t)
	defer cleanup()
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT COUNT\(\*\).*status = 'needs_attention'`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`FROM account_share_request_billing_intents AS intent.*status = 'needs_attention'`).
		WithArgs(0, 20).
		WillReturnRows(accountShareBillingAdminRecordRows(now))

	items, total, err := repo.ListNeedsAttentionForAdmin(context.Background(), 0, 20)

	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "needs_attention", items[0].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingAdminRepositoryGetsNonSensitiveDetailByID(t *testing.T) {
	repo, mock, cleanup := newAccountShareBillingIntentRepositorySQLMock(t)
	defer cleanup()
	now := time.Now().UTC()
	mock.ExpectQuery(`FROM account_share_request_billing_intents AS intent.*intent.id = \$1`).
		WithArgs(int64(100)).
		WillReturnRows(accountShareBillingAdminRecordRows(now))

	record, err := repo.GetForAdmin(context.Background(), 100)

	require.NoError(t, err)
	require.Equal(t, int64(100), record.ID)
	require.Equal(t, int64(4), record.StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func accountShareBillingAdminRecordRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "request_id", "dispatch_id", "attempt_no", "api_key_id",
		"membership_id", "listing_id", "account_id", "status", "state_token",
		"last_error_code", "last_error_message", "forward_started_at",
		"completed_at", "created_at", "updated_at",
	}).AddRow(
		int64(100), "request-100", "dispatch-100", 1, int64(10),
		int64(11), int64(12), int64(13), "needs_attention", int64(4),
		"usage_missing", "usage detail missing", now.Add(-time.Minute),
		nil, now.Add(-time.Hour), now,
	)
}

func newAccountShareBillingIntentRepositorySQLMock(
	t *testing.T,
) (*accountShareBillingIntentRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo, ok := NewAccountShareBillingIntentRepository(db).(*accountShareBillingIntentRepository)
	require.True(t, ok)
	return repo, mock, func() {
		_ = db.Close()
	}
}
