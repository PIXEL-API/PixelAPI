//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type duplicateAccountAdminServiceStub struct {
	service.AdminService
	account      *service.Account
	calls        int
	accountID    int64
	actorScope   string
	operationKey string
}

func (s *duplicateAccountAdminServiceStub) DuplicateAccount(_ context.Context, accountID int64, actorScope, operationKey string) (*service.Account, error) {
	s.calls++
	s.accountID = accountID
	s.actorScope = actorScope
	s.operationKey = operationKey
	return s.account, nil
}

func (s *duplicateAccountAdminServiceStub) RecoverDuplicateAccount(context.Context, int64, string, string) (*service.Account, error) {
	return nil, nil
}

func setupDuplicateAccountRouter(t *testing.T, svc service.AdminService) *gin.Engine {
	t.Helper()
	previousCoordinator := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previousCoordinator) })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 77})
		c.Next()
	})
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/:id/duplicate", handler.Duplicate)
	return router
}

func TestDuplicateAccountHandlerReplaysWithoutExposingCredentials(t *testing.T) {
	svc := &duplicateAccountAdminServiceStub{account: &service.Account{
		ID:          43,
		Name:        "primary (Copy)",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: false,
		Credentials: map[string]any{"api_key": "top-secret"},
	}}
	router := setupDuplicateAccountRouter(t, svc)

	call := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/42/duplicate", nil)
		request.Header.Set("Idempotency-Key", "duplicate-account-42")
		router.ServeHTTP(recorder, request)
		return recorder
	}
	first := call()
	second := call()

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, 1, svc.calls)
	require.Equal(t, int64(42), svc.accountID)
	require.Equal(t, "admin:77", svc.actorScope)
	require.Equal(t, "duplicate-account-42", svc.operationKey)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.NotContains(t, first.Body.String(), "top-secret")

	var body struct {
		Data struct {
			Credentials map[string]any `json:"credentials"`
			Schedulable bool           `json:"schedulable"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &body))
	require.Empty(t, body.Data.Credentials)
	require.False(t, body.Data.Schedulable)
}

func TestDuplicateAccountHandlerRequiresIdempotencyKey(t *testing.T) {
	svc := &duplicateAccountAdminServiceStub{account: &service.Account{ID: 43}}
	router := setupDuplicateAccountRouter(t, svc)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/42/duplicate", nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, svc.calls)
}

func TestDuplicateAccountHandlerRejectsInvalidID(t *testing.T) {
	svc := &duplicateAccountAdminServiceStub{}
	router := setupDuplicateAccountRouter(t, svc)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/not-a-number/duplicate", nil)
	request.Header.Set("Idempotency-Key", "invalid-id")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, svc.calls)
}
