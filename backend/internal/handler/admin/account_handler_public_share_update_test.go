package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pendingOwnedAccountUpdateAdminService struct {
	*stubAdminService
	updatedAccount *service.Account
}

func (s *pendingOwnedAccountUpdateAdminService) UpdateAccount(context.Context, int64, *service.UpdateAccountInput) (*service.Account, error) {
	return s.updatedAccount, nil
}

func TestAccountHandlerUpdateEnqueuesOwnedPendingPublicShareValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)
	adminService := &pendingOwnedAccountUpdateAdminService{
		stubAdminService: newStubAdminService(),
		updatedAccount: &service.Account{
			ID:          7,
			Name:        "owned-pending",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			OwnerUserID: &ownerUserID,
			ShareMode:   service.AccountShareModePublic,
			ShareStatus: service.AccountShareStatusPending,
			Status:      service.StatusActive,
		},
	}
	handler := &AccountHandler{
		adminService:          adminService,
		accountService:        &service.AccountService{},
		accountTestService:    &service.AccountTestService{},
		publicShareValidation: make(chan ownedPublicShareValidationJob, 1),
	}
	// Keep the test deterministic: mark worker startup complete so the queued
	// job remains available for direct assertion instead of being consumed.
	handler.publicShareValidationOnce.Do(func() {})
	router := gin.New()
	router.PUT("/api/v1/admin/accounts/:id", handler.Update)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/7", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	select {
	case job := <-handler.publicShareValidation:
		require.Equal(t, int64(7), job.AccountID)
		require.Equal(t, ownerUserID, job.OwnerUserID)
	default:
		t.Fatal("expected pending owned account validation job")
	}
}
