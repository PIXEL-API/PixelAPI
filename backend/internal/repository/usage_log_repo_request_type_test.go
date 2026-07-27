package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDashboardHourlyRangeBounds_Rolling24Hours(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 34, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	start := now.Add(-24 * time.Hour)

	fullStart, fullEnd, effectiveEnd, buckets := dashboardHourlyRangeBounds(start, now, now)
	if buckets != 23 {
		t.Fatalf("buckets = %d, want 23", buckets)
	}
	if got, want := fullStart, time.Date(2026, 7, 9, 5, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("fullStart = %s, want %s", got, want)
	}
	if got, want := fullEnd, time.Date(2026, 7, 10, 4, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("fullEnd = %s, want %s", got, want)
	}
	if !effectiveEnd.Equal(now) {
		t.Fatalf("effectiveEnd = %s, want %s", effectiveEnd, now)
	}
}

func TestDashboardHourlyRangeBounds_ClampsFutureEnd(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 20, 0, 0, time.UTC)
	start := now.Add(-2 * time.Hour)
	_, _, effectiveEnd, buckets := dashboardHourlyRangeBounds(start, now.Add(12*time.Hour), now)
	if !effectiveEnd.Equal(now) {
		t.Fatalf("effectiveEnd = %s, want %s", effectiveEnd, now)
	}
	if buckets != 1 {
		t.Fatalf("buckets = %d, want 1", buckets)
	}
}

func TestShouldUseDashboardHourlyRange_RejectsOldRange(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	options := service.DashboardStatsRangeOptions{UseHourlyAggregates: true, HourlyMaxAge: 31 * 24 * time.Hour}
	if shouldUseDashboardHourlyRange(now.Add(-32*24*time.Hour), now, now, options) {
		t.Fatal("range older than hourly coverage safety window must not use recent aggregates")
	}
	if !shouldUseDashboardHourlyRange(now.Add(-24*time.Hour), now, now, options) {
		t.Fatal("recent rolling range should use hourly aggregates")
	}
	if shouldUseDashboardHourlyRange(now.Add(-24*time.Hour), now, now, service.DashboardStatsRangeOptions{}) {
		t.Fatal("disabled hourly aggregation must not use aggregate tables")
	}
}

func TestDashboardHourlyRangeBounds_UsesConfiguredHalfHourOffset(t *testing.T) {
	loc := time.FixedZone("UTC+5:30", 5*60*60+30*60)
	now := time.Date(2026, 7, 10, 12, 34, 0, 0, loc)
	start := now.Add(-24 * time.Hour)

	fullStart, fullEnd, _, buckets := dashboardHourlyRangeBoundsInLocation(start, now, now, loc)
	require.Equal(t, 23, buckets)
	require.Equal(t, time.Date(2026, 7, 9, 7, 30, 0, 0, time.UTC), fullStart.UTC())
	require.Equal(t, time.Date(2026, 7, 10, 6, 30, 0, 0, time.UTC), fullEnd.UTC())
}

func TestDashboardHourlyAggregatesReady_RequiresWatermarkCoverage(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	fullStart := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	fullEnd := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT[[:space:]]+\\(SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark").
		WillReturnRows(sqlmock.NewRows([]string{"last_aggregated_at", "coverage_start"}).
			AddRow(fullEnd.Add(-time.Minute), fullStart.Add(-time.Hour)))
	ready, err := repo.dashboardHourlyAggregatesReady(context.Background(), fullStart, fullEnd)
	require.NoError(t, err)
	require.False(t, ready)

	mock.ExpectQuery("SELECT[[:space:]]+\\(SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark").
		WillReturnRows(sqlmock.NewRows([]string{"last_aggregated_at", "coverage_start"}).
			AddRow(fullEnd, fullStart.Add(time.Hour)))
	ready, err = repo.dashboardHourlyAggregatesReady(context.Background(), fullStart, fullEnd)
	require.NoError(t, err)
	require.False(t, ready)

	mock.ExpectQuery("SELECT[[:space:]]+\\(SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark").
		WillReturnRows(sqlmock.NewRows([]string{"last_aggregated_at", "coverage_start"}).
			AddRow(fullEnd, nil))
	ready, err = repo.dashboardHourlyAggregatesReady(context.Background(), fullStart, fullEnd)
	require.NoError(t, err)
	require.False(t, ready)

	mock.ExpectQuery("SELECT[[:space:]]+\\(SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark").
		WillReturnRows(sqlmock.NewRows([]string{"last_aggregated_at", "coverage_start"}).
			AddRow(fullEnd, fullStart))
	ready, err = repo.dashboardHourlyAggregatesReady(context.Background(), fullStart, fullEnd)
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFillDashboardUsageStatsWithSnapshots_FallsBackWhenHourlyCoverageStartsTooLate(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	now := time.Date(2026, 7, 10, 12, 34, 0, 0, time.UTC)
	start := now.Add(-24 * time.Hour)
	fullStart, fullEnd, _, _ := dashboardHourlyRangeBounds(start, now, now)

	mock.ExpectQuery("SELECT[[:space:]]+\\(SELECT last_aggregated_at FROM usage_dashboard_aggregation_watermark").
		WillReturnRows(sqlmock.NewRows([]string{"last_aggregated_at", "coverage_start"}).
			AddRow(fullEnd, fullStart.Add(time.Hour)))
	mock.ExpectQuery("COUNT\\(\\*\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"total_account_cost",
			"total_duration_ms",
			"today_requests",
			"today_input_tokens",
			"today_output_tokens",
			"today_cache_creation_tokens",
			"today_cache_read_tokens",
			"today_cost",
			"today_actual_cost",
			"today_account_cost",
		}).AddRow(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17))
	mock.ExpectQuery("COUNT\\(DISTINCT CASE").
		WillReturnRows(sqlmock.NewRows([]string{"active_users", "hourly_active_users"}).AddRow(2, 1))

	stats := &DashboardStats{}
	err := repo.fillDashboardUsageStatsWithSnapshots(
		context.Background(),
		stats,
		start,
		now,
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		now,
		service.DashboardStatsRangeOptions{UseHourlyAggregates: true, HourlyMaxAge: 31 * 24 * time.Hour},
	)

	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-1",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		InputTokens:    10,
		OutputTokens:   20,
		TotalCost:      1,
		ActualCost:     1,
		BillingType:    service.BillingTypeBalance,
		RequestType:    service.RequestTypeWSV2,
		Stream:         false,
		OpenAIWSMode:   false,
		CreatedAt:      createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(
			log.UserID,
			log.APIKeyID,
			log.AccountID,
			log.RequestID,
			log.Model,
			log.RequestedModel,
			sqlmock.AnyArg(), // upstream_model
			sqlmock.AnyArg(), // group_id
			sqlmock.AnyArg(), // subscription_id
			log.InputTokens,
			log.OutputTokens,
			log.CacheCreationTokens,
			log.CacheReadTokens,
			log.CacheCreation5mTokens,
			log.CacheCreation1hTokens,
			log.ImageOutputTokens,
			log.ImageOutputCost,
			log.ImageInputTokens,
			log.ImageInputCost,
			log.InputCost,
			log.OutputCost,
			log.CacheCreationCost,
			log.CacheReadCost,
			log.TotalCost,
			log.ActualCost,
			log.RateMultiplier,
			log.RateMultiplierSource,
			log.AccountRateMultiplier,
			log.BillingType,
			int16(service.RequestTypeWSV2),
			true,
			true,
			sqlmock.AnyArg(), // duration_ms
			sqlmock.AnyArg(), // first_token_ms
			sqlmock.AnyArg(), // user_agent
			sqlmock.AnyArg(), // ip_address
			log.ImageCount,
			sqlmock.AnyArg(), // image_size
			log.VideoCount,
			sqlmock.AnyArg(), // video_resolution
			sqlmock.AnyArg(), // video_duration_seconds
			sqlmock.AnyArg(), // service_tier
			sqlmock.AnyArg(), // reasoning_effort
			sqlmock.AnyArg(), // inbound_endpoint
			sqlmock.AnyArg(), // upstream_endpoint
			log.CacheTTLOverridden,
			sqlmock.AnyArg(), // channel_id
			sqlmock.AnyArg(), // model_mapping_chain
			sqlmock.AnyArg(), // billing_tier
			sqlmock.AnyArg(), // billing_mode
			sqlmock.AnyArg(), // account_stats_cost
			createdAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(99), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, int64(99), log.ID)
	require.Nil(t, log.ServiceTier)
	require.Equal(t, service.RequestTypeWSV2, log.RequestType)
	require.True(t, log.Stream)
	require.True(t, log.OpenAIWSMode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryCreate_PersistsServiceTier(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	serviceTier := "priority"
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-service-tier",
		Model:          "gpt-5.4",
		RequestedModel: "gpt-5.4",
		ServiceTier:    &serviceTier,
		CreatedAt:      createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(
			log.UserID,
			log.APIKeyID,
			log.AccountID,
			log.RequestID,
			log.Model,
			log.RequestedModel,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			log.InputTokens,
			log.OutputTokens,
			log.CacheCreationTokens,
			log.CacheReadTokens,
			log.CacheCreation5mTokens,
			log.CacheCreation1hTokens,
			log.ImageOutputTokens,
			log.ImageOutputCost,
			log.ImageInputTokens,
			log.ImageInputCost,
			log.InputCost,
			log.OutputCost,
			log.CacheCreationCost,
			log.CacheReadCost,
			log.TotalCost,
			log.ActualCost,
			log.RateMultiplier,
			log.RateMultiplierSource,
			log.AccountRateMultiplier,
			log.BillingType,
			int16(service.RequestTypeSync),
			false,
			false,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			log.ImageCount,
			sqlmock.AnyArg(),
			log.VideoCount,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			serviceTier,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			log.CacheTTLOverridden,
			sqlmock.AnyArg(), // channel_id
			sqlmock.AnyArg(), // model_mapping_chain
			sqlmock.AnyArg(), // billing_tier
			sqlmock.AnyArg(), // billing_mode
			sqlmock.AnyArg(), // account_stats_cost
			createdAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(100), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsageLogBestEffortInsertQuery_IncludesRequestedModelColumn(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-query",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC),
	})

	query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	require.Contains(t, query, "INSERT INTO usage_logs (")
	require.Contains(t, query, "\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,")
	require.Contains(t, query, "\n\t\t\trequest_id,\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,")
	require.Len(t, args, len(prepared.args))
	require.Equal(t, prepared.args[5], args[5])
}

func TestExecUsageLogInsertNoResult_PersistsRequestedModel(t *testing.T) {
	db, mock := newSQLMock(t)
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-exec",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC),
	})

	mock.ExpectExec("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := execUsageLogInsertNoResult(context.Background(), db, prepared)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareUsageLogInsert_ArgCountMatchesTypes(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-arg-count",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC),
	})

	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
}

func TestPrepareUsageLogInsert_PersistsImageInputUsage(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:           1,
		APIKeyID:         2,
		AccountID:        3,
		RequestID:        "req-image-input",
		Model:            "gpt-image-2",
		ImageInputTokens: 352,
		ImageInputCost:   0.002816,
		CreatedAt:        time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC),
	})

	require.Equal(t, 352, prepared.args[17])
	require.InDelta(t, 0.002816, prepared.args[18], 1e-15)
}

func TestUsageBillingUsageLogInsertQuery_ArgCountMatchesPreparedInsert(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:               1,
		APIKeyID:             2,
		AccountID:            3,
		RequestID:            "req-billing-arg-count",
		Model:                "gpt-5",
		RequestedModel:       "gpt-5",
		RateMultiplierSource: service.RateMultiplierSourceNewUserGroup,
		CreatedAt:            time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC),
	})

	query := usageBillingUsageLogInsertQuery()
	matches := placeholderPattern.FindAllString(query, -1)
	seen := make(map[int]struct{}, len(matches))
	maxPlaceholder := 0
	for _, match := range matches {
		n, err := strconv.Atoi(strings.TrimPrefix(match, "$"))
		require.NoError(t, err)
		seen[n] = struct{}{}
		if n > maxPlaceholder {
			maxPlaceholder = n
		}
	}

	require.Contains(t, query, "rate_multiplier_source")
	require.Len(t, matches, len(usageLogInsertArgTypes))
	require.Len(t, seen, len(usageLogInsertArgTypes))
	require.Equal(t, len(usageLogInsertArgTypes), maxPlaceholder)
	require.Len(t, prepared.args, maxPlaceholder)
}

func TestCoalesceTrimmedString(t *testing.T) {
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{}, "fallback"))
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{Valid: true, String: "   "}, "fallback"))
	require.Equal(t, "value", coalesceTrimmedString(sql.NullString{Valid: true, String: "value"}, "fallback"))
}

func anySliceToDriverValues(values []any) []driver.Value {
	out := make([]driver.Value, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func TestUsageLogRepositoryListWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeWSV2)
	stream := false
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
		ExactTotal:  true,
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\) ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs(requestType, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUsageTrendWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeStream)
	stream := true

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost"}))

	trend, err := repo.GetUsageTrendWithFilters(context.Background(), start, end, "day", 0, 0, 0, 0, "", &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeWSV2)
	stream := false

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "account_cost"}))

	stats, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountUsageStatsUsesIdentityScope(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end := start.Add(72 * time.Hour)

	mock.ExpectQuery(`WITH current_account AS`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(10)).AddRow(int64(11)))
	mock.ExpectQuery(`SELECT\s+MIN\(available_from\)`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"available_from", "available_to", "requests", "tokens", "cost"}).
			AddRow(start, end, int64(2), int64(165), 1.50))
	mock.ExpectQuery(`WITH daily_usage AS`).
		WithArgs(sqlmock.AnyArg(), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "tokens", "cost", "actual_cost", "user_cost", "total_duration_ms"}).
			AddRow("2025-01-15", int64(1), int64(150), 1.25, 1.25, 1.25, int64(120)).
			AddRow("2025-01-16", int64(1), int64(15), 0.25, 0.25, 0.25, int64(120)))
	mock.ExpectQuery(`WITH target_owner AS.*sm\.owner_user_id = \(SELECT owner_user_id FROM target_owner\).*sm\.account_id = ANY\(\$8\)`).
		WithArgs(
			int64(11),
			sqlmock.AnyArg(),
			accountShareSeatPrepayReason,
			accountShareSeatRefundReason,
			accountShareSeatWaiverRefundReason,
			start,
			end,
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"date", "hourly_cost", "share_consumer_cost", "owner_income"}).
			AddRow("2025-01-16", 0.75, 1.20, 1.08).
			AddRow("2025-01-17", -0.25, -0.30, -0.27))
	mock.ExpectQuery(`SELECT\s+COALESCE\(NULLIF\(TRIM\(requested_model\)`).
		WithArgs(sqlmock.AnyArg(), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "account_cost"}).
			AddRow("gpt-5", int64(2), int64(110), int64(55), int64(0), int64(0), int64(165), 1.50, 1.50, 1.20))
	mock.ExpectQuery(`SELECT\s+COALESCE\(NULLIF\(TRIM\(inbound_endpoint\)`).
		WithArgs(sqlmock.AnyArg(), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost", "account_cost"}).
			AddRow("/v1/responses", int64(2), int64(165), 1.50, 1.50, 1.20))
	mock.ExpectQuery(`SELECT\s+COALESCE\(NULLIF\(TRIM\(upstream_endpoint\)`).
		WithArgs(sqlmock.AnyArg(), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost", "account_cost"}))

	resp, err := repo.GetAccountUsageStats(context.Background(), 11, start, end)
	require.NoError(t, err)
	require.Len(t, resp.History, 3)
	require.Equal(t, 3, resp.Summary.Days)
	require.Equal(t, int64(2), resp.Summary.TotalRequests)
	require.Equal(t, int64(165), resp.Summary.TotalTokens)
	require.InDelta(t, 1.50, resp.Summary.TotalRequestUserCost, 0.000001)
	require.InDelta(t, 0.50, resp.Summary.TotalHourlyCost, 0.000001)
	require.InDelta(t, 2.00, resp.Summary.TotalUserCost, 0.000001)
	require.InDelta(t, 0.90, resp.Summary.TotalShareConsumerCost, 0.000001)
	require.InDelta(t, 0.81, resp.Summary.TotalOwnerIncome, 0.000001)
	require.InDelta(t, -0.69, resp.Summary.TotalOwnerNetIncome, 0.000001)
	require.InDelta(t, 1.00, resp.History[1].UserCost, 0.000001)
	require.InDelta(t, 1.08, resp.History[1].OwnerIncome, 0.000001)
	require.InDelta(t, 0.83, resp.History[1].OwnerNetIncome, 0.000001)
	require.InDelta(t, -0.25, resp.History[2].HourlyCost, 0.000001)
	require.InDelta(t, -0.27, resp.History[2].OwnerIncome, 0.000001)
	require.Equal(t, 2, resp.Lifetime.SourceAccountCount)
	require.InDelta(t, 1.50, resp.Lifetime.TotalCost, 0.000001)
	require.Len(t, resp.Models, 1)
	require.Equal(t, int64(2), resp.Models[0].Requests)
	require.InDelta(t, 1.50, resp.Models[0].ActualCost, 0.000001)
	require.InDelta(t, 1.20, resp.Models[0].AccountCost, 0.000001)
	require.Len(t, resp.Endpoints, 1)
	require.InDelta(t, 1.50, resp.Endpoints[0].ActualCost, 0.000001)
	require.InDelta(t, 1.20, resp.Endpoints[0].AccountCost, 0.000001)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeSync)
	stream := true
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
	}

	mock.ExpectQuery("FROM usage_logs\\s+WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"total_account_cost",
			"avg_duration_ms",
		}).AddRow(int64(1), int64(2), int64(3), int64(4), int64(1), int64(3), 1.2, 1.0, 1.2, 20.0))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(inbound_endpoint\\), ''\\), 'unknown'\\) AS endpoint").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(upstream_endpoint\\), ''\\), 'unknown'\\) AS endpoint").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT CONCAT\\(").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, int64(9), stats.TotalTokens)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost should always be returned")
	require.Equal(t, 1.2, *stats.TotalAccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_logs").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"model", "requests", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).
			AddRow("claude-opus-4-6", int64(10), int64(100), int64(200), int64(5), int64(3), int64(308), 2.5, 2.0, 1.8).
			AddRow("claude-sonnet-4-6", int64(5), int64(50), int64(100), int64(0), int64(0), int64(150), 1.0, 0.8, 0.7))

	results, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "claude-opus-4-6", results[0].Model)
	require.Equal(t, 2.5, results[0].Cost)
	require.Equal(t, 2.0, results[0].ActualCost)
	require.Equal(t, 1.8, results[0].AccountCost)
	require.Equal(t, "claude-sonnet-4-6", results[1].Model)
	require.Equal(t, 0.7, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetGroupStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_logs").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"group_id", "group_name", "requests", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).
			AddRow(int64(1), "azure-cc", int64(100), int64(5000), 10.0, 8.5, 7.2).
			AddRow(int64(2), "max", int64(50), int64(2000), 5.0, 4.0, 3.5))

	results, err := repo.GetGroupStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, int64(1), results[0].GroupID)
	require.Equal(t, "azure-cc", results[0].GroupName)
	require.Equal(t, 10.0, results[0].Cost)
	require.Equal(t, 8.5, results[0].ActualCost)
	require.Equal(t, 7.2, results[0].AccountCost)
	require.Equal(t, int64(2), results[1].GroupID)
	require.Equal(t, 3.5, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersAlwaysReturnsAccountCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	// No AccountID filter set - TotalAccountCost should still be returned
	filters := usagestats.UsageLogFilters{}

	mock.ExpectQuery("FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens",
			"total_cache_tokens", "total_cache_creation_tokens", "total_cache_read_tokens", "total_cost", "total_actual_cost",
			"total_account_cost", "avg_duration_ms",
		}).AddRow(int64(50), int64(1000), int64(2000), int64(100), int64(25), int64(75), 15.0, 12.5, 11.0, 100.0))
	mock.ExpectQuery("FROM user_balance_ledger").
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(1.25))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(inbound_endpoint\\)").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(upstream_endpoint\\)").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT CONCAT\\(").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost must always be returned, even without AccountID filter")
	require.Equal(t, 11.0, *stats.TotalAccountCost)
	require.Equal(t, 12.5, stats.TotalRequestActualCost)
	require.Equal(t, 1.25, stats.TotalHourlyCost)
	require.Equal(t, 13.75, stats.TotalActualCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUserSpendingRanking(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	rows := sqlmock.NewRows([]string{"user_id", "email", "actual_cost", "requests", "tokens", "total_actual_cost", "total_requests", "total_tokens"}).
		AddRow(int64(2), "beta@example.com", 12.5, int64(9), int64(900), 40.0, int64(30), int64(2600)).
		AddRow(int64(1), "alpha@example.com", 12.5, int64(8), int64(800), 40.0, int64(30), int64(2600)).
		AddRow(int64(3), "gamma@example.com", 4.25, int64(5), int64(300), 40.0, int64(30), int64(2600))

	mock.ExpectQuery("WITH user_spend AS \\(").
		WithArgs(start, end, 12).
		WillReturnRows(rows)

	got, err := repo.GetUserSpendingRanking(context.Background(), start, end, 12)
	require.NoError(t, err)
	require.Equal(t, &usagestats.UserSpendingRankingResponse{
		Ranking: []usagestats.UserSpendingRankingItem{
			{UserID: 2, Email: "beta@example.com", ActualCost: 12.5, Requests: 9, Tokens: 900},
			{UserID: 1, Email: "alpha@example.com", ActualCost: 12.5, Requests: 8, Tokens: 800},
			{UserID: 3, Email: "gamma@example.com", ActualCost: 4.25, Requests: 5, Tokens: 300},
		},
		TotalActualCost: 40.0,
		TotalRequests:   30,
		TotalTokens:     2600,
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAccountShareRecommendationUsageProfilePrefersModelMatch(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	end := start.Add(72 * time.Hour)

	rows := sqlmock.NewRows([]string{
		"all_requests",
		"all_input_tokens",
		"all_output_tokens",
		"all_cache_creation_tokens",
		"all_cache_read_tokens",
		"all_image_output_tokens",
		"all_active_hour_buckets",
		"model_requests",
		"model_input_tokens",
		"model_output_tokens",
		"model_cache_creation_tokens",
		"model_cache_read_tokens",
		"model_image_output_tokens",
		"model_active_hour_buckets",
	}).AddRow(
		int64(300),
		int64(9000),
		int64(3000),
		int64(600),
		int64(1200),
		int64(0),
		int64(9),
		int64(30),
		int64(1200),
		int64(450),
		int64(90),
		int64(150),
		int64(0),
		int64(2),
	)

	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\) AS all_requests").
		WithArgs(int64(42), start, end, "gpt-5.5", sqlmock.AnyArg()).
		WillReturnRows(rows)

	got, err := repo.GetAccountShareRecommendationUsageProfile(context.Background(), 42, "gpt-5.5", start, end)
	require.NoError(t, err)
	require.True(t, got.ModelMatched)
	require.Equal(t, int64(30), got.TotalRequests)
	require.Equal(t, int64(1200), got.TotalInputTokens)
	require.Equal(t, int64(450), got.TotalOutputTokens)
	require.Equal(t, int64(90), got.TotalCacheCreationTokens)
	require.Equal(t, int64(150), got.TotalCacheReadTokens)
	require.Equal(t, int64(2), got.ActiveHourBuckets)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildRequestTypeFilterConditionLegacyFallback(t *testing.T) {
	tests := []struct {
		name      string
		request   int16
		wantWhere string
		wantArg   int16
	}{
		{
			name:      "sync_with_legacy_fallback",
			request:   int16(service.RequestTypeSync),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeSync),
		},
		{
			name:      "stream_with_legacy_fallback",
			request:   int16(service.RequestTypeStream),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeStream),
		},
		{
			name:      "ws_v2_with_legacy_fallback",
			request:   int16(service.RequestTypeWSV2),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND openai_ws_mode = TRUE))",
			wantArg:   int16(service.RequestTypeWSV2),
		},
		{
			name:      "invalid_request_type_normalized_to_unknown",
			request:   int16(99),
			wantWhere: "request_type = $3",
			wantArg:   int16(service.RequestTypeUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildRequestTypeFilterCondition(3, tt.request)
			require.Equal(t, tt.wantWhere, where)
			require.Equal(t, []any{tt.wantArg}, args)
		})
	}
}

type usageLogScannerStub struct {
	values []any
}

func (s usageLogScannerStub) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan arg count mismatch: got %d want %d", len(dest), len(s.values))
	}
	for i := range dest {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Ptr {
			return fmt.Errorf("dest[%d] is not pointer", i)
		}
		dv.Elem().Set(reflect.ValueOf(s.values[i]))
	}
	return nil
}

func TestScanUsageLogRequestTypeAndLegacyFallback(t *testing.T) {
	t.Run("request_type_ws_v2_overrides_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(1),  // id
			int64(10), // user_id
			int64(20), // api_key_id
			int64(30), // account_id
			sql.NullString{Valid: true, String: "req-1"},
			"gpt-5", // model
			sql.NullString{Valid: true, String: "gpt-5"}, // requested_model
			sql.NullString{},  // upstream_model
			sql.NullInt64{},   // group_id
			sql.NullInt64{},   // subscription_id
			1,                 // input_tokens
			2,                 // output_tokens
			3,                 // cache_creation_tokens
			4,                 // cache_read_tokens
			5,                 // cache_creation_5m_tokens
			6,                 // cache_creation_1h_tokens
			0,                 // image_output_tokens
			0.0,               // image_output_cost
			0,                 // image_input_tokens
			0.0,               // image_input_cost
			0.1,               // input_cost
			0.2,               // output_cost
			0.3,               // cache_creation_cost
			0.4,               // cache_read_cost
			1.0,               // total_cost
			0.9,               // actual_cost
			1.0,               // rate_multiplier
			sql.NullString{},  // rate_multiplier_source
			sql.NullFloat64{}, // account_rate_multiplier
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeWSV2),
			false, // legacy stream
			false, // legacy openai ws
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			0,                // video_count
			sql.NullString{}, // video_resolution
			sql.NullInt64{},  // video_duration_seconds
			sql.NullString{Valid: true, String: "priority"},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
			now,
		}})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
		require.Equal(t, service.RequestTypeWSV2, log.RequestType)
		require.True(t, log.Stream)
		require.True(t, log.OpenAIWSMode)
	})

	t.Run("request_type_unknown_falls_back_to_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(2),
			int64(11),
			int64(21),
			int64(31),
			sql.NullString{Valid: true, String: "req-2"},
			"gpt-5",
			sql.NullString{Valid: true, String: "gpt-5"},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullInt64{},
			1, 2, 3, 4, 5, 6,
			0, 0.0, // image_output_tokens, image_output_cost
			0, 0.0, // image_input_tokens, image_input_cost
			0.1, 0.2, 0.3, 0.4, 1.0, 0.9,
			1.0,
			sql.NullString{}, // rate_multiplier_source
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeUnknown),
			true,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			0,                // video_count
			sql.NullString{}, // video_resolution
			sql.NullInt64{},  // video_duration_seconds
			sql.NullString{Valid: true, String: "flex"},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
			now,
		}})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "flex", *log.ServiceTier)
		require.Equal(t, service.RequestTypeStream, log.RequestType)
		require.True(t, log.Stream)
		require.False(t, log.OpenAIWSMode)
	})

	t.Run("service_tier_is_scanned", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(3),
			int64(12),
			int64(22),
			int64(32),
			sql.NullString{Valid: true, String: "req-3"},
			"gpt-5.4",
			sql.NullString{Valid: true, String: "gpt-5.4"},
			sql.NullString{},
			sql.NullInt64{},
			sql.NullInt64{},
			1, 2, 3, 4, 5, 6,
			0, 0.0, // image_output_tokens, image_output_cost
			0, 0.0, // image_input_tokens, image_input_cost
			0.1, 0.2, 0.3, 0.4, 1.0, 0.9,
			1.0,
			sql.NullString{}, // rate_multiplier_source
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeSync),
			false,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			1, // video_count
			sql.NullString{Valid: true, String: "720p"}, // video_resolution
			sql.NullInt64{Valid: true, Int64: 8},        // video_duration_seconds
			sql.NullString{Valid: true, String: "priority"},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
			now,
		}})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
		require.Equal(t, 1, log.VideoCount)
		require.NotNil(t, log.VideoResolution)
		require.Equal(t, "720p", *log.VideoResolution)
		require.NotNil(t, log.VideoDurationSeconds)
		require.Equal(t, 8, *log.VideoDurationSeconds)
	})

}
