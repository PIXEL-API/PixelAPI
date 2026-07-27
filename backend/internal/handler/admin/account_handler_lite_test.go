package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
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

type crsPreviewHandlerAccountRepoStub struct {
	service.AccountRepository
}

func (s *crsPreviewHandlerAccountRepoStub) ListCRSAccountPreviewSnapshots(
	context.Context,
) ([]service.CRSAccountPreviewSnapshot, error) {
	return []service.CRSAccountPreviewSnapshot{{
		CRSAccountID:   "claude-room",
		LocalAccountID: 44,
		RoomBindings: []service.CRSAccountRoomBindingSnapshot{{
			ListingID:  71,
			RowVersion: 6,
		}},
	}}, nil
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

func TestSyncFromCRSRequestMapsAuthenticatedAdminMutationContract(t *testing.T) {
	var req SyncFromCRSRequest
	err := json.Unmarshal([]byte(`{
		"base_url":"https://crs.example.com",
		"username":"admin",
		"password":"secret",
		"sync_proxies":false,
		"selected_account_ids":["account-1"],
		"preview_token":"signed-preview-token",
		"actor_admin_id":999,
		"force_active_edit":true,
		"confirmed":true,
		"reason":"同步房间账号",
		"expected_version":7,
		"expected_versions":{"41":7},
		"operation_id":"untrusted-body-operation"
	}`), &req)
	require.NoError(t, err)

	input := req.toServiceInput(42, "trusted-header-operation")

	require.Equal(t, "https://crs.example.com", input.BaseURL)
	require.Equal(t, "admin", input.Username)
	require.Equal(t, "secret", input.Password)
	require.False(t, input.SyncProxies)
	require.Equal(t, []string{"account-1"}, input.SelectedAccountIDs)
	require.Equal(t, int64(42), input.ActorAdminID, "actor identity must come from authenticated context")
	require.True(t, input.ForceActiveEdit)
	require.True(t, input.Confirmed)
	require.Equal(t, "同步房间账号", input.Reason)
	require.NotNil(t, input.ExpectedVersion)
	require.Equal(t, int64(7), *input.ExpectedVersion)
	require.Equal(t, map[int64]int64{41: 7}, input.ExpectedVersions)
	require.Equal(t, "trusted-header-operation", input.OperationID)
	require.Equal(t, "signed-preview-token", input.PreviewToken)
	require.NotNil(t, input.ValidateResponseCapacity)
}

func TestSyncFromCRSRejectsMissingAuthenticatedAdminBeforeServiceCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &AccountHandler{}
	router := gin.New()
	router.POST("/accounts/sync/crs", handler.SyncFromCRS)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/sync/crs",
		strings.NewReader(`{
			"base_url":"https://crs.example.com",
			"username":"admin",
			"password":"secret"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSyncFromCRSRequiresIdempotencyKeyDuringObserveOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	handler := &AccountHandler{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/accounts/sync/crs", handler.SyncFromCRS)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/sync/crs",
		strings.NewReader(`{
			"base_url":"https://crs.example.com",
			"username":"admin",
			"password":"secret",
			"preview_token":"signed-preview-token"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var envelope struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "IDEMPOTENCY_KEY_REQUIRED", envelope.Reason)
}

func TestSyncFromCRSMapsServiceDomainErrorWithinFixedIdempotencyScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idempotencyRepo := newMemoryIdempotencyRepoStub()
	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(idempotencyRepo, service.DefaultIdempotencyConfig()),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	crsService := service.NewCRSSyncService(
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("s", 32)}},
	)
	handler := &AccountHandler{crsSyncService: crsService}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/accounts/sync/crs", handler.SyncFromCRS)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/sync/crs",
		strings.NewReader(`{
			"base_url":"https://crs.example.com",
			"username":"admin",
			"password":"secret"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "crs-domain-error")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var envelope struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "CRS_PREVIEW_TOKEN_REQUIRED", envelope.Reason)

	idempotencyRepo.mu.Lock()
	defer idempotencyRepo.mu.Unlock()
	require.Len(t, idempotencyRepo.data, 1)
	for _, record := range idempotencyRepo.data {
		require.Equal(t, adminCRSSyncIdempotencyScope, record.Scope)
	}
}

func TestPreviewFromCRSReturnsRoomForceEditContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	crsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/web/auth/login":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "preview-token",
			}))
		case "/admin/sync/export-accounts":
			require.Equal(t, "Bearer preview-token", r.Header.Get("Authorization"))
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"claudeAccounts": []map[string]any{{
						"kind":     "claude",
						"id":       "claude-room",
						"name":     "Room Claude",
						"authType": service.AccountTypeSetupToken,
					}},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer crsServer.Close()

	cfg := &config.Config{
		JWT: config.JWTConfig{Secret: strings.Repeat("s", 32)},
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           false,
				AllowInsecureHTTP: true,
			},
		},
	}
	crsService := service.NewCRSSyncService(
		&crsPreviewHandlerAccountRepoStub{},
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	handler := &AccountHandler{crsSyncService: crsService}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/accounts/sync/crs/preview", handler.PreviewFromCRS)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/sync/crs/preview",
		strings.NewReader(`{
			"base_url":"`+crsServer.URL+`",
			"username":"admin",
			"password":"secret"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data struct {
			PreviewToken     string `json:"preview_token"`
			ExpiresAt        int64  `json:"expires_at"`
			ExistingAccounts []struct {
				CRSAccountID            string                                  `json:"crs_account_id"`
				LocalAccountID          int64                                   `json:"local_account_id"`
				RequiresForceActiveEdit bool                                    `json:"requires_force_active_edit"`
				RoomBindings            []service.CRSAccountRoomBindingSnapshot `json:"room_bindings"`
			} `json:"existing_accounts"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.PreviewToken)
	require.Positive(t, envelope.Data.ExpiresAt)
	require.Len(t, envelope.Data.ExistingAccounts, 1)
	existing := envelope.Data.ExistingAccounts[0]
	require.Equal(t, "claude-room", existing.CRSAccountID)
	require.Equal(t, int64(44), existing.LocalAccountID)
	require.True(t, existing.RequiresForceActiveEdit)
	require.Equal(t, []service.CRSAccountRoomBindingSnapshot{{
		ListingID:  71,
		RowVersion: 6,
	}}, existing.RoomBindings)
}

func TestPreviewFromCRSMapsServiceDomainError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	crsService := service.NewCRSSyncService(
		&crsPreviewHandlerAccountRepoStub{},
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)
	handler := &AccountHandler{crsSyncService: crsService}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/accounts/sync/crs/preview", handler.PreviewFromCRS)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/sync/crs/preview",
		strings.NewReader(`{
			"base_url":"https://crs.example.com",
			"username":"admin",
			"password":"secret"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	var envelope struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "CRS_PREVIEW_SIGNING_UNAVAILABLE", envelope.Reason)
}

func TestAdminAccountMutationConfirmationAppliesTierRefreshGuard(t *testing.T) {
	expectedVersion := int64(7)
	confirmation := AdminAccountMutationConfirmation{
		ForceActiveEdit:  true,
		Confirmed:        true,
		Reason:           "refresh Google One storage tier",
		ExpectedVersion:  &expectedVersion,
		ExpectedVersions: map[int64]int64{41: 7},
	}
	input := &service.UpdateAccountInput{
		Credentials: map[string]any{"tier_id": "2tb"},
		Extra:       map[string]any{"drive_storage_limit": int64(2)},
	}

	confirmation.apply(input, 9, "tier-refresh-operation")

	require.Equal(t, int64(9), input.ActorAdminID)
	require.Equal(t, service.AccountMutationIntentAdmin, input.MutationIntent)
	require.True(t, input.ForceActiveEdit)
	require.True(t, input.Confirmed)
	require.Equal(t, confirmation.Reason, input.Reason)
	require.Same(t, confirmation.ExpectedVersion, input.ExpectedVersion)
	require.Equal(t, confirmation.ExpectedVersions, input.ExpectedVersions)
	require.Equal(t, "tier-refresh-operation", input.OperationID)
}

func TestBatchRefreshTierRequestDecodesEmbeddedAdminConfirmation(t *testing.T) {
	var req BatchRefreshTierRequest
	err := json.Unmarshal([]byte(`{
		"account_ids":[11,12],
		"force_active_edit":true,
		"confirmed":true,
		"reason":"scheduled tier refresh",
		"expected_versions":{"41":7}
	}`), &req)

	require.NoError(t, err)
	require.Equal(t, []int64{11, 12}, req.AccountIDs)
	require.True(t, req.ForceActiveEdit)
	require.True(t, req.Confirmed)
	require.Equal(t, "scheduled tier refresh", req.Reason)
	require.Equal(t, map[int64]int64{41: 7}, req.ExpectedVersions)
}

func TestBatchRefreshTierRejectsMalformedJSONWithoutRefreshingAllAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := &AccountHandler{adminService: adminSvc}
	router := gin.New()
	router.POST("/accounts/batch-refresh-tier", handler.BatchRefreshTier)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/batch-refresh-tier",
		strings.NewReader(`{"account_ids":[`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, adminSvc.lastListAccounts.calls)
}

func TestBatchRefreshTierKeepsEmptyBodyCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := &AccountHandler{adminService: adminSvc}
	router := gin.New()
	router.POST("/accounts/batch-refresh-tier", handler.BatchRefreshTier)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/accounts/batch-refresh-tier", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, adminSvc.lastListAccounts.calls)
	require.Contains(t, recorder.Body.String(), `"total":0`)
}
