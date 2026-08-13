package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupUsageSummaryRepoCapture struct {
	service.UsageLogRepository
	groupIDs []int64
}

func (r *groupUsageSummaryRepoCapture) GetAllGroupUsageSummary(
	ctx context.Context,
	todayStart time.Time,
	groupIDs []int64,
) ([]usagestats.GroupUsageSummary, error) {
	r.groupIDs = append([]int64(nil), groupIDs...)
	return []usagestats.GroupUsageSummary{{GroupID: 1, TotalCost: 2.5, TodayCost: 0.5}}, nil
}

func newGroupUsageSummaryRouter(repo *groupUsageSummaryRepoCapture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	dashboardService := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewGroupHandler(nil, dashboardService, nil, nil)
	router := gin.New()
	router.GET("/admin/groups/usage-summary", handler.GetUsageSummary)
	return router
}

func TestGroupUsageSummaryHandlerParsesAndDeduplicatesGroupIDs(t *testing.T) {
	repo := &groupUsageSummaryRepoCapture{}
	router := newGroupUsageSummaryRouter(repo)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/groups/usage-summary?timezone=UTC&group_ids=1,2,1,3", nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{1, 2, 3}, repo.groupIDs)
	require.Contains(t, recorder.Body.String(), `"group_id":1`)
}

func TestGroupUsageSummaryHandlerKeepsOmittedGroupIDsContract(t *testing.T) {
	repo := &groupUsageSummaryRepoCapture{}
	router := newGroupUsageSummaryRouter(repo)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/groups/usage-summary?timezone=UTC", nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Nil(t, repo.groupIDs)
}

func TestGroupUsageSummaryHandlerRejectsInvalidGroupIDs(t *testing.T) {
	tests := []string{"abc", "0", "-1", "1,,2"}
	for _, groupIDs := range tests {
		t.Run(groupIDs, func(t *testing.T) {
			repo := &groupUsageSummaryRepoCapture{}
			router := newGroupUsageSummaryRouter(repo)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/admin/groups/usage-summary?group_ids="+groupIDs, nil)

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Nil(t, repo.groupIDs)
		})
	}
}

func TestGroupUsageSummaryHandlerRejectsMoreThanMaximumIDs(t *testing.T) {
	values := make([]string, maxGroupUsageSummaryIDs+1)
	for index := range values {
		values[index] = strconv.Itoa(index + 1)
	}
	repo := &groupUsageSummaryRepoCapture{}
	router := newGroupUsageSummaryRouter(repo)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/groups/usage-summary?group_ids="+strings.Join(values, ","), nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "exceeds maximum")
	require.Nil(t, repo.groupIDs)
}
