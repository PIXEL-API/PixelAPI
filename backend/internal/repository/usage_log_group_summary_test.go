package repository

import (
	"context"
	"database/sql/driver"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type int64ArrayArgument []int64

func (expected int64ArrayArgument) Match(value driver.Value) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	if len(expected) == 0 {
		return text == "{}"
	}
	want := "{"
	for index, item := range expected {
		if index > 0 {
			want += ","
		}
		want += strconv.FormatInt(item, 10)
	}
	want += "}"
	return text == want
}

func TestGetAllGroupUsageSummaryUsesAggregateAndRequestedGroups(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	todayStart := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("WITH requested_groups AS").
		WithArgs(todayStart, int64ArrayArgument{2, 9}).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "total_cost", "today_cost"}).
			AddRow(int64(2), 12.5, 1.25).
			AddRow(int64(9), 7.75, 0.5))

	results, err := repo.GetAllGroupUsageSummary(context.Background(), todayStart, []int64{2, 0, 9, 2, -1})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, int64(2), results[0].GroupID)
	require.Equal(t, 12.5, results[0].TotalCost)
	require.Equal(t, 1.25, results[0].TodayCost)
	require.Equal(t, int64(9), results[1].GroupID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAllGroupUsageSummaryKeepsLegacyAllGroupsContract(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	todayStart := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("LEFT JOIN group_usage_cost_totals").
		WithArgs(todayStart, int64ArrayArgument{}).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "total_cost", "today_cost"}))

	results, err := repo.GetAllGroupUsageSummary(context.Background(), todayStart, nil)
	require.NoError(t, err)
	require.Empty(t, results)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizePositiveInt64s(t *testing.T) {
	require.Equal(t, []int64{4, 2, 7}, normalizePositiveInt64s([]int64{4, 0, 2, 4, -3, 7, 2}))
	require.Empty(t, normalizePositiveInt64s(nil))
}
