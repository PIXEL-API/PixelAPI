package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositorySumAccountShareSeatCostByAPIKeyUsesSplitIndexedPaths(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("WITH target_memberships AS MATERIALIZED")).
		WithArgs(int64(42), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(1.75))

	total, err := repo.sumAccountShareSeatCost(context.Background(), accountShareSeatCostFilter{
		APIKeyID:  42,
		StartTime: &start,
		EndTime:   &end,
	})
	require.NoError(t, err)
	require.Equal(t, 1.75, total)
	require.NoError(t, mock.ExpectationsWereMet())

	query := accountShareSeatCostSourceForAPIKeyFilter("$1", nil)
	require.Contains(t, query, "NULLIF(l.metadata->>'membership_id', '') IS NOT NULL")
	require.Contains(t, query, "l.ref_type = 'account_share_membership'")
	require.Contains(t, query, "NULLIF(l.metadata->>'membership_id', '') IS NULL")
	require.NotContains(t, query, "COALESCE(")
}

func TestUsageLogRepositorySumAccountShareSeatCostsByUserUsesLiteralPartialIndexPredicate(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	today := start.Add(12 * time.Hour)

	mock.ExpectQuery("account_share_mode_seat_prepay").
		WithArgs(sqlmock.AnyArg(), start, end, today).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "total_cost", "today_cost"}).
			AddRow(int64(7), 2.5, 0.5))

	totals, err := repo.sumAccountShareSeatCostsByUser(context.Background(), []int64{7}, start, end, today)
	require.NoError(t, err)
	require.Equal(t, accountShareSeatCostTotals{Total: 2.5, Today: 0.5}, totals[7])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositorySumAccountShareSeatCostsByAPIKeyUsesSplitIndexedPathsAndTypedTimeBounds(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	today := start.Add(12 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("l.created_at >= LEAST($2::timestamptz, $4::timestamptz)")).
		WithArgs(sqlmock.AnyArg(), start, end, today).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "total_cost", "today_cost"}).
			AddRow(int64(11), 3.5, 1.25))

	totals, err := repo.sumAccountShareSeatCostsByAPIKey(context.Background(), []int64{11}, start, end, today)
	require.NoError(t, err)
	require.Equal(t, accountShareSeatCostTotals{Total: 3.5, Today: 1.25}, totals[11])
	require.NoError(t, mock.ExpectationsWereMet())
}
