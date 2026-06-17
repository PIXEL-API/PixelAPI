package service

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newUsageBalanceLedgerSQLMock(t *testing.T) (*UsageService, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	return NewUsageService(nil, nil, client, nil), mock
}

func TestUsageServiceListBalanceLedgerFiltersAndPreservesDecimalText(t *testing.T) {
	svc, mock := newUsageBalanceLedgerSQLMock(t)
	ctx := context.Background()
	userID := int64(42)
	start := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	createdAt := start.Add(2 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM user_balance_ledger l WHERE l.user_id = $1 AND l.direction = $2 AND l.reason = $3 AND l.created_at >= $4 AND l.created_at < $5")).
		WithArgs(userID, "credit", "account_share_income", start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))

	mock.ExpectQuery(`(?s)SELECT\s+l\.id,\s+l\.user_id,\s+l\.direction,\s+l\.amount::text,\s+l\.reason,\s+l\.ref_type,\s+l\.ref_id,\s+l\.balance_after::text,\s+l\.metadata,\s+l\.created_at,\s+u\.email,\s+u\.username,\s+u\.status\s+FROM user_balance_ledger l\s+LEFT JOIN users u ON u\.id = l\.user_id\s+WHERE l\.user_id = \$1 AND l\.direction = \$2 AND l\.reason = \$3 AND l\.created_at >= \$4 AND l\.created_at < \$5\s+ORDER BY l\.created_at DESC, l\.id DESC\s+LIMIT \$6 OFFSET \$7`).
		WithArgs(userID, "credit", "account_share_income", start, end, 2, 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"user_id",
			"direction",
			"amount",
			"reason",
			"ref_type",
			"ref_id",
			"balance_after",
			"metadata",
			"created_at",
			"email",
			"username",
			"status",
		}).AddRow(
			int64(11),
			userID,
			"credit",
			"0.1234567891",
			"account_share_income",
			"usage_log",
			int64(99),
			"10.0000000001",
			[]byte(`{"request_id":"req-income","consumer_user_id":9}`),
			createdAt,
			"user@example.com",
			"demo",
			"active",
		))

	entries, result, err := svc.ListBalanceLedger(ctx, pagination.PaginationParams{
		Page:      2,
		PageSize:  2,
		SortOrder: pagination.SortOrderDesc,
	}, UserBalanceLedgerFilters{
		UserID:     userID,
		Direction:  "credit",
		Reason:     "account_share_income",
		StartTime:  &start,
		EndTime:    &end,
		ExactTotal: true,
	})

	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "0.1234567891", entries[0].Amount)
	require.Equal(t, "10.0000000001", entries[0].BalanceAfter)
	require.Equal(t, int64(99), *entries[0].RefID)
	require.NotNil(t, entries[0].User)
	require.Equal(t, "user@example.com", entries[0].User.Email)
	require.Equal(t, "req-income", entries[0].Metadata["request_id"])
	require.Equal(t, float64(9), entries[0].Metadata["consumer_user_id"])
	require.Equal(t, int64(3), result.Total)
	require.Equal(t, 2, result.Page)
	require.Equal(t, 2, result.PageSize)
	require.Equal(t, 2, result.Pages)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageServiceListBalanceLedgerFastPaginationWithoutExactTotal(t *testing.T) {
	svc, mock := newUsageBalanceLedgerSQLMock(t)
	ctx := context.Background()
	userID := int64(42)
	start := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(`(?s)SELECT\s+l\.id,\s+l\.user_id,\s+l\.direction,\s+l\.amount::text,\s+l\.reason,\s+l\.ref_type,\s+l\.ref_id,\s+l\.balance_after::text,\s+l\.metadata,\s+l\.created_at,\s+u\.email,\s+u\.username,\s+u\.status\s+FROM user_balance_ledger l`).
		WithArgs(userID, "credit", start, end, 3, 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"user_id",
			"direction",
			"amount",
			"reason",
			"ref_type",
			"ref_id",
			"balance_after",
			"metadata",
			"created_at",
			"email",
			"username",
			"status",
		}).AddRow(
			int64(12), userID, "credit", "0.11", "account_share_income", "usage_log", int64(101), "10.1",
			[]byte(`{"request_id":"req-12"}`), start.Add(3*time.Hour), "user2@example.com", "demo", "active",
		).AddRow(
			int64(11), userID, "credit", "0.22", "account_share_income", "usage_log", int64(102), "10.2",
			[]byte(`{"request_id":"req-11"}`), start.Add(2*time.Hour), "user2@example.com", "demo", "active",
		).AddRow(
			int64(10), userID, "credit", "0.33", "account_share_income", "usage_log", int64(103), "10.3",
			[]byte(`{"request_id":"req-10"}`), start.Add(1*time.Hour), "user2@example.com", "demo", "active",
		))

	entries, result, err := svc.ListBalanceLedger(ctx, pagination.PaginationParams{
		Page:      2,
		PageSize:  2,
		SortOrder: pagination.SortOrderDesc,
	}, UserBalanceLedgerFilters{
		UserID:    userID,
		Direction: "credit",
		StartTime: &start,
		EndTime:   &end,
	})

	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, int64(5), result.Total)
	require.Equal(t, 2, result.Page)
	require.Equal(t, 2, result.PageSize)
	require.Equal(t, 3, result.Pages)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageServiceListBalanceLedgerRejectsInvalidDirection(t *testing.T) {
	svc, mock := newUsageBalanceLedgerSQLMock(t)

	entries, result, err := svc.ListBalanceLedger(context.Background(), pagination.DefaultPagination(), UserBalanceLedgerFilters{
		UserID:    42,
		Direction: "sideways",
	})

	require.Error(t, err)
	require.Nil(t, entries)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}
