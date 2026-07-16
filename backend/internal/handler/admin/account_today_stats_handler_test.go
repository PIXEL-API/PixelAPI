package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountTodayStatsUsageRepoStub struct {
	service.UsageLogRepository
	calls atomic.Int32
}

func (s *accountTodayStatsUsageRepoStub) GetAccountWindowStatsBatch(context.Context, []int64, time.Time) (map[int64]*usagestats.AccountStats, error) {
	s.calls.Add(1)
	return map[int64]*usagestats.AccountStats{1: {Requests: 4}}, nil
}

func TestAccountBatchTodayStatsUsesSnapshotGetOrLoad(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousCache := accountTodayStatsBatchCache
	accountTodayStatsBatchCache = newSnapshotCache(30 * time.Second)
	defer func() { accountTodayStatsBatchCache = previousCache }()

	repo := &accountTodayStatsUsageRepoStub{}
	usageSvc := service.NewAccountUsageService(nil, repo, nil, nil, nil, service.NewUsageCache(), nil, nil)
	handler := &AccountHandler{accountUsageService: usageSvc}
	router := gin.New()
	router.POST("/accounts/today-stats/batch", handler.GetBatchTodayStats)

	request := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/accounts/today-stats/batch", bytes.NewBufferString(`{"account_ids":[1,1]}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		return rec
	}

	first := request()
	second := request()

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, "miss", first.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "hit", second.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, first.Header().Get("ETag"), second.Header().Get("ETag"))
	require.Equal(t, int32(1), repo.calls.Load())
}
