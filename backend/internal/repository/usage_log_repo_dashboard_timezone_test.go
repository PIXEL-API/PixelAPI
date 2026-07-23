package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestDashboardBucketExpressionsKeepRollbackHoursDistinct(t *testing.T) {
	hourKey := dashboardBucketKeyExpression("hour", "created_at")
	hourLabel := dashboardBucketLabelExpression("hour", "bucket_key")
	dayKey := dashboardBucketKeyExpression("day", "created_at")

	require.Equal(t, "DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC')", hourKey)
	require.Equal(
		t,
		"TO_CHAR((bucket_key AT TIME ZONE 'UTC') AT TIME ZONE $4, 'YYYY-MM-DD HH24:MI')",
		hourLabel,
	)
	require.Equal(t, "DATE_TRUNC('day', created_at AT TIME ZONE $4)", dayKey)

	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	firstPhysicalHour := time.Date(2026, time.November, 1, 5, 0, 0, 0, time.UTC)
	secondPhysicalHour := firstPhysicalHour.Add(time.Hour)
	require.Equal(
		t,
		firstPhysicalHour.In(location).Format("2006-01-02 15:04"),
		secondPhysicalHour.In(location).Format("2006-01-02 15:04"),
		"the rollback hours intentionally share a display label",
	)
	require.NotEqual(t, firstPhysicalHour, secondPhysicalHour, "their UTC bucket keys must remain distinct")
}

func TestGetUserUsageTrendByUserIDUsesParameterizedTimezoneAndAbsoluteHourBucket(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)

	mock.ExpectQuery(`(?s)DATE_TRUNC\('hour', created_at AT TIME ZONE 'UTC'\) AS bucket_key.*GROUP BY bucket_key.*TO_CHAR\(\(bucket_key AT TIME ZONE 'UTC'\) AT TIME ZONE \$4, 'YYYY-MM-DD HH24:MI'\) AS date.*ORDER BY bucket_key ASC`).
		WithArgs(int64(42), start, end, "America/New_York").
		WillReturnRows(sqlmock.NewRows([]string{
			"date",
			"requests",
			"input_tokens",
			"output_tokens",
			"cache_creation_tokens",
			"cache_read_tokens",
			"total_tokens",
			"cost",
			"actual_cost",
		}))

	trend, err := repo.GetUserUsageTrendByUserID(
		context.Background(),
		42,
		start,
		end,
		"hour",
		location,
	)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserAccountSharingTrendJoinsOnAbsoluteHourBucket(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)

	mock.ExpectQuery(`(?s)DATE_TRUNC\('hour', ul\.created_at AT TIME ZONE 'UTC'\) AS bucket_key.*DATE_TRUNC\('hour', created_at AT TIME ZONE 'UTC'\) AS bucket_key.*FULL OUTER JOIN external_usage e ON e\.bucket_key = s\.bucket_key.*ORDER BY COALESCE\(s\.bucket_key, e\.bucket_key\) ASC`).
		WithArgs(int64(42), start, end, "America/New_York").
		WillReturnRows(sqlmock.NewRows([]string{
			"date",
			"self_requests",
			"self_tokens",
			"self_actual_cost",
			"self_account_cost",
			"external_requests",
			"external_consumer_charge",
			"external_account_cost",
			"external_owner_credit",
			"external_platform_fee",
		}))

	trend, err := repo.getUserAccountSharingTrend(
		context.Background(),
		42,
		start,
		end,
		"hour",
		location,
	)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}
