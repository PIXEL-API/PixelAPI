package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userDashboardTimeRangeRepoCapture struct {
	service.UsageLogRepository
	trendStartTime          time.Time
	trendEndTime            time.Time
	modelStartTime          time.Time
	modelEndTime            time.Time
	accountSharingStartTime time.Time
	accountSharingEndTime   time.Time
	trendLocation           *time.Location
	accountSharingLocation  *time.Location
}

func (r *userDashboardTimeRangeRepoCapture) GetUserUsageTrendByUserID(
	_ context.Context,
	_ int64,
	startTime, endTime time.Time,
	_ string,
	location *time.Location,
) ([]usagestats.TrendDataPoint, error) {
	r.trendStartTime = startTime
	r.trendEndTime = endTime
	r.trendLocation = location
	return []usagestats.TrendDataPoint{}, nil
}

func (r *userDashboardTimeRangeRepoCapture) GetUserModelStats(
	_ context.Context,
	_ int64,
	startTime, endTime time.Time,
) ([]usagestats.ModelStat, error) {
	r.modelStartTime = startTime
	r.modelEndTime = endTime
	return []usagestats.ModelStat{}, nil
}

func (r *userDashboardTimeRangeRepoCapture) GetUserAccountSharingDashboard(
	_ context.Context,
	_ int64,
	startTime, endTime time.Time,
	_ string,
	location *time.Location,
) (*usagestats.AccountSharingDashboardStats, error) {
	r.accountSharingStartTime = startTime
	r.accountSharingEndTime = endTime
	r.accountSharingLocation = location
	return &usagestats.AccountSharingDashboardStats{}, nil
}

func newUserDashboardTimeRangeTestRouter(repo *userDashboardTimeRangeRepoCapture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageService := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageService, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/usage/dashboard/trend", handler.DashboardTrend)
	router.GET("/usage/dashboard/models", handler.DashboardModels)
	router.GET("/usage/dashboard/account-sharing", handler.DashboardAccountSharing)
	return router
}

func TestUserDashboardEndpointsUseExactTimeRange(t *testing.T) {
	repo := &userDashboardTimeRangeRepoCapture{}
	router := newUserDashboardTimeRangeTestRouter(repo)
	wantStart := time.Date(2026, time.July, 22, 12, 34, 56, 789000000, time.UTC)
	wantEnd := wantStart.Add(24 * time.Hour)
	query := url.Values{
		"start_time": {wantStart.Format(time.RFC3339Nano)},
		"end_time":   {wantEnd.Format(time.RFC3339Nano)},
		"timezone":   {"Asia/Shanghai"},
	}.Encode()

	tests := []struct {
		name     string
		path     string
		captured func() (time.Time, time.Time)
	}{
		{
			name: "trend",
			path: "/usage/dashboard/trend",
			captured: func() (time.Time, time.Time) {
				return repo.trendStartTime, repo.trendEndTime
			},
		},
		{
			name: "models",
			path: "/usage/dashboard/models",
			captured: func() (time.Time, time.Time) {
				return repo.modelStartTime, repo.modelEndTime
			},
		},
		{
			name: "account sharing",
			path: "/usage/dashboard/account-sharing",
			captured: func() (time.Time, time.Time) {
				return repo.accountSharingStartTime, repo.accountSharingEndTime
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path+"?"+query, nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			gotStart, gotEnd := tt.captured()
			require.True(t, gotStart.Equal(wantStart), "start time = %s, want %s", gotStart, wantStart)
			require.True(t, gotEnd.Equal(wantEnd), "end time = %s, want %s", gotEnd, wantEnd)
			require.Equal(t, "Asia/Shanghai", gotStart.Location().String())
			require.Equal(t, "Asia/Shanghai", gotEnd.Location().String())
		})
	}
	require.Equal(t, "Asia/Shanghai", repo.trendLocation.String())
	require.Equal(t, "Asia/Shanghai", repo.accountSharingLocation.String())
}

func assertUserDashboardEndpointsReject(t *testing.T, query url.Values, message string) {
	t.Helper()
	repo := &userDashboardTimeRangeRepoCapture{}
	router := newUserDashboardTimeRangeTestRouter(repo)

	for _, path := range []string{
		"/usage/dashboard/trend",
		"/usage/dashboard/models",
		"/usage/dashboard/account-sharing",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path+"?"+query.Encode(), nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), message)
		})
	}
}

func TestUserDashboardEndpointsRejectIncompleteExactTimeRange(t *testing.T) {
	assertUserDashboardEndpointsReject(t, url.Values{
		"start_time": {"2026-07-22T12:34:56Z"},
		"start_date": {"2026-07-01"},
		"end_date":   {"2026-07-07"},
		"timezone":   {"Asia/Shanghai"},
	}, "start_time and end_time must be provided together")
}

func TestUserDashboardEndpointsRejectMixedExactAndCalendarRanges(t *testing.T) {
	assertUserDashboardEndpointsReject(t, url.Values{
		"start_time": {"2026-07-22T12:34:56Z"},
		"end_time":   {"2026-07-23T12:34:56Z"},
		"start_date": {"2026-07-01"},
		"end_date":   {"2026-07-07"},
		"timezone":   {"Asia/Shanghai"},
	}, "start_time/end_time cannot be combined with start_date/end_date")
}

func TestUserDashboardEndpointsRejectInvalidTimezone(t *testing.T) {
	assertUserDashboardEndpointsReject(t, url.Values{
		"start_time": {"2026-07-22T12:34:56Z"},
		"end_time":   {"2026-07-23T12:34:56Z"},
		"timezone":   {"Mars/Olympus_Mons"},
	}, "invalid timezone")
}

func TestUserDashboardEndpointsRejectReversedExactRange(t *testing.T) {
	assertUserDashboardEndpointsReject(t, url.Values{
		"start_time": {"2026-07-23T12:34:56Z"},
		"end_time":   {"2026-07-22T12:34:56Z"},
		"timezone":   {"Asia/Shanghai"},
	}, "end_time must be after start_time")
}

func TestUserDashboardTrendKeepsCalendarDateRangeCompatibility(t *testing.T) {
	repo := &userDashboardTimeRangeRepoCapture{}
	router := newUserDashboardTimeRangeTestRouter(repo)
	query := url.Values{
		"start_date": {"2026-07-01"},
		"end_date":   {"2026-07-07"},
		"timezone":   {"Asia/Shanghai"},
	}.Encode()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/usage/dashboard/trend?"+query, nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	wantLocation, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.July, 1, 0, 0, 0, 0, wantLocation), repo.trendStartTime)
	require.Equal(t, time.Date(2026, time.July, 8, 0, 0, 0, 0, wantLocation), repo.trendEndTime)
	require.Equal(t, wantLocation, repo.trendLocation)
}

func TestUserDashboardTrendKeepsCalendarDayAcrossDST(t *testing.T) {
	repo := &userDashboardTimeRangeRepoCapture{}
	router := newUserDashboardTimeRangeTestRouter(repo)
	query := url.Values{
		"start_date": {"2026-03-08"},
		"end_date":   {"2026-03-08"},
		"timezone":   {"America/New_York"},
	}.Encode()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/usage/dashboard/trend?"+query, nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	wantLocation, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.March, 8, 0, 0, 0, 0, wantLocation), repo.trendStartTime)
	require.Equal(t, time.Date(2026, time.March, 9, 0, 0, 0, 0, wantLocation), repo.trendEndTime)
	require.Equal(t, 23*time.Hour, repo.trendEndTime.Sub(repo.trendStartTime))
}

func decodeUserDashboardResponseRange(t *testing.T, recorder *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body struct {
		Data struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body.Data.StartDate, body.Data.EndDate
}

func TestUserDashboardEndpointsFormatExactRangeInClientTimezone(t *testing.T) {
	repo := &userDashboardTimeRangeRepoCapture{}
	router := newUserDashboardTimeRangeTestRouter(repo)
	query := url.Values{
		"start_time": {"2026-07-21T16:30:00Z"},
		"end_time":   {"2026-07-22T16:30:00Z"},
		"timezone":   {"Asia/Shanghai"},
	}.Encode()

	for _, path := range []string{
		"/usage/dashboard/trend",
		"/usage/dashboard/models",
		"/usage/dashboard/account-sharing",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			startDate, endDate := decodeUserDashboardResponseRange(t, recorder)
			require.Equal(t, "2026-07-22", startDate)
			require.Equal(t, "2026-07-23", endDate)
		})
	}
}

func TestUserDashboardExactRangeRemainsTwentyFourHoursAcrossDST(t *testing.T) {
	repo := &userDashboardTimeRangeRepoCapture{}
	router := newUserDashboardTimeRangeTestRouter(repo)
	query := url.Values{
		"start_time": {"2026-03-08T00:30:00-05:00"},
		"end_time":   {"2026-03-09T01:30:00-04:00"},
		"timezone":   {"America/New_York"},
	}.Encode()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/usage/dashboard/trend?"+query, nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 24*time.Hour, repo.trendEndTime.Sub(repo.trendStartTime))
	require.Equal(t, "America/New_York", repo.trendStartTime.Location().String())
	require.Equal(t, 0, repo.trendStartTime.Hour())
	require.Equal(t, 1, repo.trendEndTime.Hour())
	startDate, endDate := decodeUserDashboardResponseRange(t, recorder)
	require.Equal(t, "2026-03-08", startDate)
	require.Equal(t, "2026-03-09", endDate)
}
