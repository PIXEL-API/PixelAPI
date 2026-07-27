package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingAdminHandlerRejectsNonAdminAtServiceBoundary(t *testing.T) {
	repo := &accountShareBillingAdminHandlerRepoStub{}
	handler := newAccountShareBillingAdminTestHandler(repo)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/account-share/billing-intents/100/waive",
		strings.NewReader(`{"expected_state_token":4,"reason":"人工确认无法恢复","confirmed":true}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "100"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)

	handler.WaiveBillingIntentForAdmin(c)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Zero(t, repo.waiveCalls)
}

func TestAccountShareBillingAdminHandlerWaivesWithAdminIdentityAndCAS(t *testing.T) {
	repo := &accountShareBillingAdminHandlerRepoStub{
		result: &service.AccountShareBillingIntentWaiverResult{
			Intent: service.AccountShareBillingIntentAdminRecord{
				ID:         100,
				Status:     service.AccountShareBillingIntentStatusCancelled,
				StateToken: 5,
			},
			Waiver: service.AccountShareBillingIntentAdminWaiver{
				ID:                  200,
				IntentID:            100,
				ActorUserIDSnapshot: 42,
				Reason:              "人工确认无法恢复",
				PreviousStatus:      service.AccountShareBillingIntentStatusNeedsAttention,
				ResultingStatus:     service.AccountShareBillingIntentStatusCancelled,
				PreviousStateToken:  4,
				ResultingStateToken: 5,
			},
		},
	}
	handler := newAccountShareBillingAdminTestHandler(repo)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/account-share/billing-intents/100/waive",
		strings.NewReader(`{"expected_state_token":4,"reason":"人工确认无法恢复","confirmed":true}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "100"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleAdmin)

	handler.WaiveBillingIntentForAdmin(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, repo.waiveCalls)
	require.Equal(t, int64(100), repo.input.IntentID)
	require.Equal(t, int64(4), repo.input.ExpectedStateToken)
	require.Equal(t, int64(42), repo.input.ActorUserID)
	require.Equal(t, "人工确认无法恢复", repo.input.Reason)
	require.Contains(t, recorder.Body.String(), `"status":"cancelled"`)
}

func TestAccountShareBillingAdminHandlerListsNeedsAttentionWithPagination(t *testing.T) {
	repo := &accountShareBillingAdminHandlerRepoStub{
		items: []service.AccountShareBillingIntentAdminRecord{
			{
				ID:           100,
				MembershipID: 11,
				ListingID:    12,
				Status:       service.AccountShareBillingIntentStatusNeedsAttention,
				StateToken:   4,
			},
		},
		total: 6,
	}
	handler := newAccountShareBillingAdminTestHandler(repo)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/account-share/billing-intents/needs-attention?page=2&page_size=5",
		nil,
	)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleAdmin)

	handler.ListBillingIntentsNeedingAttention(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, repo.listCalls)
	require.Equal(t, 5, repo.listOffset)
	require.Equal(t, 5, repo.listLimit)
	require.Contains(t, recorder.Body.String(), `"status":"needs_attention"`)
	require.Contains(t, recorder.Body.String(), `"total":6`)
}

func TestAccountShareBillingAdminHandlerGetsNonSensitiveDetail(t *testing.T) {
	repo := &accountShareBillingAdminHandlerRepoStub{
		detail: &service.AccountShareBillingIntentAdminRecord{
			ID:           100,
			RequestID:    "request-100",
			MembershipID: 11,
			ListingID:    12,
			Status:       service.AccountShareBillingIntentStatusNeedsAttention,
			StateToken:   4,
		},
	}
	handler := newAccountShareBillingAdminTestHandler(repo)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/account-share/billing-intents/100",
		nil,
	)
	c.Params = []gin.Param{{Key: "id", Value: "100"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleAdmin)

	handler.GetBillingIntentForAdmin(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, repo.getCalls)
	require.Equal(t, int64(100), repo.getID)
	require.Contains(t, recorder.Body.String(), `"state_token":4`)
	require.NotContains(t, recorder.Body.String(), "command_payload")
	require.NotContains(t, recorder.Body.String(), "usage_payload")
}

func newAccountShareBillingAdminTestHandler(
	repo *accountShareBillingAdminHandlerRepoStub,
) *AccountShareModeHandler {
	gin.SetMode(gin.TestMode)
	svc := service.NewAccountShareModeService(nil, nil, nil, nil, nil, nil)
	svc.SetBillingIntentRepository(repo)
	return NewAccountShareModeHandler(svc)
}

type accountShareBillingAdminHandlerRepoStub struct {
	service.AccountShareBillingIntentRepository
	items      []service.AccountShareBillingIntentAdminRecord
	total      int64
	detail     *service.AccountShareBillingIntentAdminRecord
	result     *service.AccountShareBillingIntentWaiverResult
	err        error
	listCalls  int
	listOffset int
	listLimit  int
	getCalls   int
	getID      int64
	waiveCalls int
	input      service.WaiveAccountShareBillingIntentRepositoryInput
}

func (s *accountShareBillingAdminHandlerRepoStub) ListNeedsAttentionForAdmin(
	_ context.Context,
	offset int,
	limit int,
) ([]service.AccountShareBillingIntentAdminRecord, int64, error) {
	s.listCalls++
	s.listOffset = offset
	s.listLimit = limit
	return append([]service.AccountShareBillingIntentAdminRecord(nil), s.items...), s.total, s.err
}

func (s *accountShareBillingAdminHandlerRepoStub) GetForAdmin(
	_ context.Context,
	intentID int64,
) (*service.AccountShareBillingIntentAdminRecord, error) {
	s.getCalls++
	s.getID = intentID
	if s.detail == nil && s.err == nil {
		return nil, service.ErrAccountShareBillingIntentNotFound
	}
	return s.detail, s.err
}

func (s *accountShareBillingAdminHandlerRepoStub) WaiveNeedsAttention(
	_ context.Context,
	input service.WaiveAccountShareBillingIntentRepositoryInput,
) (*service.AccountShareBillingIntentWaiverResult, error) {
	s.waiveCalls++
	s.input = input
	return s.result, s.err
}
