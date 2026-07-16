package admin

import (
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

type accountListAdminServiceStub struct {
	service.AdminService
	accounts []service.Account
}

func (s *accountListAdminServiceStub) ListAccounts(context.Context, int, int, string, string, string, string, string, int64, int64, string, string, string) ([]service.Account, int64, error) {
	return append([]service.Account(nil), s.accounts...), int64(len(s.accounts)), nil
}

type accountListUsageRepoStub struct {
	service.UsageLogRepository
	calls atomic.Int32
}

func (s *accountListUsageRepoStub) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	s.calls.Add(1)
	return &usagestats.AccountStats{StandardCost: 2.5}, nil
}

func TestAccountListLiteSkipsWindowCostAggregation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageRepo := &accountListUsageRepoStub{}
	usageSvc := service.NewAccountUsageService(nil, usageRepo, nil, nil, nil, service.NewUsageCache(), nil, nil)
	handler := &AccountHandler{
		adminService: &accountListAdminServiceStub{accounts: []service.Account{{
			ID:          1,
			Name:        "owned-account",
			Platform:    service.PlatformAnthropic,
			Type:        service.AccountTypeSetupToken,
			Status:      service.StatusActive,
			Schedulable: true,
			Extra:       map[string]any{"window_cost_limit": 10.0},
		}}},
		accountUsageService: usageSvc,
	}
	router := gin.New()
	router.GET("/accounts", handler.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/accounts?lite=1", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, usageRepo.calls.Load(), "lite=1 不应执行窗口费用聚合")

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int32(1), usageRepo.calls.Load(), "非 lite 列表仍应返回已启用的窗口费用")
}
