package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunRevenueSummaryQueriesLimitsConcurrency(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	queries := make([]func(context.Context) error, 4)
	for i := range queries {
		queries[i] = func(ctx context.Context) error {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				previous := maxActive.Load()
				if current <= previous || maxActive.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- runRevenueSummaryQueries(context.Background(), 2, queries...)
	}()

	<-started
	<-started
	require.Equal(t, int32(2), maxActive.Load())
	select {
	case <-started:
		t.Fatal("more than two revenue summary queries started concurrently")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-done)
	require.Equal(t, int32(2), maxActive.Load())
}

func TestRunRevenueSummaryQueriesReturnsOriginalError(t *testing.T) {
	wantErr := errors.New("revenue query failed")
	err := runRevenueSummaryQueries(context.Background(), 2,
		func(context.Context) error { return wantErr },
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	)

	require.ErrorIs(t, err, wantErr)
}

func TestRevenueSnapshotDateRangeUsesShanghaiDay(t *testing.T) {
	params := RevenueQueryParams{
		StartTime: time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2024, 1, 2, 16, 0, 0, 0, time.UTC),
	}

	startDate, endDate := revenueSnapshotDateRange(params)

	require.Equal(t, "2024-01-02", startDate)
	require.Equal(t, "2024-01-03", endDate)
}

func TestShouldUseRevenueDailySnapshotsRequiresShanghaiFullDay(t *testing.T) {
	loc := revenueSnapshotBusinessLocation()
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)

	require.True(t, shouldUseRevenueDailySnapshots(RevenueQueryParams{
		StartTime:   start.UTC(),
		EndTime:     end.UTC(),
		Granularity: RevenueGranularityDay,
		Timezone:    revenueSnapshotBusinessTimezone,
	}))
	require.False(t, shouldUseRevenueDailySnapshots(RevenueQueryParams{
		StartTime:   start.UTC(),
		EndTime:     end.UTC(),
		Granularity: RevenueGranularityDay,
		Timezone:    "UTC",
	}))
	require.False(t, shouldUseRevenueDailySnapshots(RevenueQueryParams{
		StartTime:   start.Add(time.Hour),
		EndTime:     end,
		Granularity: RevenueGranularityDay,
		Timezone:    revenueSnapshotBusinessTimezone,
	}))
}
