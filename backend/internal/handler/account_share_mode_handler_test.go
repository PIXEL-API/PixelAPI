package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountShareUpdateRepositoryStub struct {
	service.AccountShareModeRepository

	actorUserID int64
	actorAdmin  bool
	listingID   int64
	input       service.UpdateAccountShareListingInput
	updateCalls int
}

type accountShareEditSessionRepositoryStub struct {
	service.AccountShareModeRepository

	beginCalls   int
	releaseCalls int
}

func (s *accountShareEditSessionRepositoryStub) GetRoomManagementState(
	_ context.Context,
	viewerUserID int64,
	_ bool,
	listingID int64,
) (*service.AccountShareRoomManagementState, error) {
	return &service.AccountShareRoomManagementState{
		ListingID:       listingID,
		OwnerUserID:     viewerUserID,
		LifecycleStatus: service.AccountShareListingStatusPaused,
	}, nil
}

type accountShareRoomBatchHandlerRepoStub struct {
	service.AccountShareModeRepository
	service.AccountShareRoomRepository

	attachInput  service.BatchAccountShareRoomAccountsInput
	attachCalls  int
	attachResult *service.BulkUpdateAccountsResult
	attachErr    error
	detachInput  service.BatchAccountShareRoomAccountsInput
	detachCalls  int
	detachErr    error
}

type accountShareRoomBatchConcurrencyCacheStub struct {
	service.ConcurrencyCache
}

func (accountShareRoomBatchConcurrencyCacheStub) GetAccountConcurrencyBatch(
	_ context.Context,
	accountIDs []int64,
) (map[int64]int, error) {
	result := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = 0
	}
	return result, nil
}

type accountShareEndHandlerRepoStub struct {
	service.AccountShareModeRepository
	snapshot *service.AccountShareMembership
	result   *service.AccountShareMembership
}

type accountShareHistoryHandlerRepoStub struct {
	service.AccountShareModeRepository
	entries        []service.AccountShareMembershipHistoryEntry
	result         *pagination.PaginationResult
	consumerUserID int64
	params         pagination.PaginationParams
}

type accountShareBindingStatusHandlerRepoStub struct {
	service.AccountShareModeRepository
	service.APIKeyRepository

	key         *service.APIKey
	memberships []service.AccountShareMembership
	consumerID  int64
	apiKeyID    int64
}

func (s *accountShareBindingStatusHandlerRepoStub) GetByID(_ context.Context, _ int64) (*service.APIKey, error) {
	key := *s.key
	return &key, nil
}

func (s *accountShareBindingStatusHandlerRepoStub) ListAPIKeyBindingMemberships(
	_ context.Context,
	consumerUserID int64,
	apiKeyID int64,
) ([]service.AccountShareMembership, error) {
	s.consumerID = consumerUserID
	s.apiKeyID = apiKeyID
	return append([]service.AccountShareMembership(nil), s.memberships...), nil
}

type accountShareVisibleListingHandlerRepoStub struct {
	service.AccountShareModeRepository

	viewerUserID  int64
	viewerIsAdmin bool
	listingID     int64
}

func (s *accountShareVisibleListingHandlerRepoStub) GetVisibleListingByID(
	_ context.Context,
	listingID int64,
	viewerUserID int64,
	viewerIsAdmin bool,
) (*service.AccountShareListing, error) {
	s.viewerUserID = viewerUserID
	s.viewerIsAdmin = viewerIsAdmin
	s.listingID = listingID
	return &service.AccountShareListing{
		ID:          listingID,
		OwnerUserID: 700,
		Status:      service.AccountShareListingStatusPaused,
	}, nil
}

func (s *accountShareHistoryHandlerRepoStub) ListMembershipHistory(
	_ context.Context,
	consumerUserID int64,
	params pagination.PaginationParams,
) ([]service.AccountShareMembershipHistoryEntry, *pagination.PaginationResult, error) {
	s.consumerUserID = consumerUserID
	s.params = params
	return append([]service.AccountShareMembershipHistoryEntry(nil), s.entries...), s.result, nil
}

type accountShareReviewHandlerRepoStub struct {
	service.AccountShareModeRepository
	viewerUserID  int64
	viewerIsAdmin bool
	listingID     int64
	params        pagination.PaginationParams
	submitCalls   int
}

func (s *accountShareReviewHandlerRepoStub) SubmitReview(
	_ context.Context,
	consumerUserID int64,
	membershipID int64,
	input service.SubmitAccountShareReviewInput,
) (*service.AccountShareReview, error) {
	s.submitCalls++
	return &service.AccountShareReview{
		ID:             99,
		MembershipID:   membershipID,
		ConsumerUserID: consumerUserID,
		Score:          input.Score,
		Comment:        input.Comment,
	}, nil
}

func (s *accountShareReviewHandlerRepoStub) ListListingReviews(
	_ context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
	params pagination.PaginationParams,
) ([]service.AccountShareReview, *pagination.PaginationResult, error) {
	s.viewerUserID = viewerUserID
	s.viewerIsAdmin = viewerIsAdmin
	s.listingID = listingID
	s.params = params
	return []service.AccountShareReview{}, &pagination.PaginationResult{
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

func (s *accountShareEndHandlerRepoStub) GetMembershipForEnd(context.Context, int64, int64) (*service.AccountShareMembership, error) {
	if s.snapshot == nil {
		return nil, service.ErrAccountShareMembershipNotFound
	}
	snapshot := *s.snapshot
	return &snapshot, nil
}

func (s *accountShareEndHandlerRepoStub) BeginMembershipEnd(
	_ context.Context,
	input service.BeginAccountShareMembershipEndInput,
) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
	if s.result == nil {
		return nil, nil, service.ErrAccountShareMembershipNotFound
	}
	result := *s.result
	if result.EndingOperationID == "" {
		result.EndingOperationID = input.OperationID
	}
	return &result, nil, nil
}

func (s *accountShareEndHandlerRepoStub) FinalizeMembershipEnd(
	context.Context,
	int64,
	string,
) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, bool, error) {
	return nil, nil, false, nil
}

func (s *accountShareEndHandlerRepoStub) ListEndingMembershipCandidates(
	context.Context,
	int,
) ([]service.AccountShareEndingMembershipCandidate, error) {
	return nil, nil
}

type accountShareEndHandlerConcurrencyCache struct {
	service.ConcurrencyCache
	active int
}

func (s *accountShareEndHandlerConcurrencyCache) AcquireAccountShareMembershipSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (s *accountShareEndHandlerConcurrencyCache) ReleaseAccountShareMembershipSlot(context.Context, int64, string) error {
	return nil
}

func (s *accountShareEndHandlerConcurrencyCache) GetAccountShareMembershipConcurrency(context.Context, int64) (int, error) {
	return s.active, nil
}

func (s *accountShareRoomBatchHandlerRepoStub) AttachRoomAccountsAtomic(
	_ context.Context,
	input service.BatchAccountShareRoomAccountsInput,
) (*service.BulkUpdateAccountsResult, error) {
	s.attachCalls++
	s.attachInput = input
	if s.attachErr != nil {
		return nil, s.attachErr
	}
	if s.attachResult != nil {
		return s.attachResult, nil
	}
	result := &service.BulkUpdateAccountsResult{
		Success:    len(input.AccountIDs),
		SuccessIDs: append([]int64(nil), input.AccountIDs...),
		FailedIDs:  []int64{},
		Results:    make([]service.BulkUpdateAccountResult, 0, len(input.AccountIDs)),
	}
	for _, accountID := range input.AccountIDs {
		result.Results = append(result.Results, service.BulkUpdateAccountResult{AccountID: accountID, Success: true})
	}
	return result, nil
}

func (s *accountShareRoomBatchHandlerRepoStub) DetachRoomAccountsAtomic(
	_ context.Context,
	input service.BatchAccountShareRoomAccountsInput,
) (*service.AccountShareSeatBillingResult, error) {
	s.detachCalls++
	s.detachInput = input
	return nil, s.detachErr
}

func (s *accountShareUpdateRepositoryStub) UpdateListing(
	_ context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input service.UpdateAccountShareListingInput,
) (*service.AccountShareListing, error) {
	s.actorUserID = actorUserID
	s.actorAdmin = actorIsAdmin
	s.listingID = listingID
	s.input = input
	s.updateCalls++
	return &service.AccountShareListing{
		ID:         listingID,
		RowVersion: *input.ExpectedVersion + 1,
		RoomName:   "updated-room",
	}, nil
}

func (s *accountShareEditSessionRepositoryStub) BeginListingEdit(
	_ context.Context,
	actorUserID int64,
	_ bool,
	listingID int64,
	input service.BeginAccountShareListingEditInput,
) (*service.AccountShareListing, error) {
	s.beginCalls++
	return &service.AccountShareListing{
		ID:              listingID,
		OwnerUserID:     actorUserID,
		EditSessionID:   input.SessionID,
		EditingByUserID: &actorUserID,
	}, nil
}

func (s *accountShareEditSessionRepositoryStub) ReleaseListingEdit(
	_ context.Context,
	actorUserID int64,
	_ bool,
	listingID int64,
	_ string,
) (*service.AccountShareListing, error) {
	s.releaseCalls++
	return &service.AccountShareListing{
		ID:          listingID,
		OwnerUserID: actorUserID,
	}, nil
}

type accountShareHandlerErrorEnvelope struct {
	Code     int               `json:"code"`
	Reason   string            `json:"reason"`
	Metadata map[string]string `json:"metadata"`
}

func performAccountShareListingUpdate(
	t *testing.T,
	handler *AccountShareModeHandler,
	role string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(
			newUserMemoryIdempotencyRepoStub(),
			service.DefaultIdempotencyConfig(),
		),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/v1/account-share/listings/7", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Idempotency-Key", "update-listing-once")
	c.Params = []gin.Param{{Key: "id", Value: "7"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), role)

	handler.UpdateListing(c)
	return recorder
}

func TestAccountShareModeHandlerListMembershipHistoryScopesConsumerAndPagination(t *testing.T) {
	repo := &accountShareHistoryHandlerRepoStub{
		entries: []service.AccountShareMembershipHistoryEntry{
			{
				MembershipID:    11,
				ListingID:       7,
				RoomDeleted:     true,
				SnapshotQuality: service.AccountShareSnapshotQualityUnknown,
			},
			{
				MembershipID:    12,
				ListingID:       7,
				RoomDeleted:     true,
				SnapshotQuality: service.AccountShareSnapshotQualityUnknown,
			},
		},
		result: &pagination.PaginationResult{
			Total:    2,
			Page:     2,
			PageSize: 5,
			Pages:    1,
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/account-share/history/memberships?page=2&page_size=5",
		nil,
	)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})

	handler.ListMembershipHistory(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.consumerUserID != 42 ||
		repo.params.Page != 2 ||
		repo.params.PageSize != 5 {
		t.Fatalf("unexpected history scope: consumer=%d params=%#v", repo.consumerUserID, repo.params)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Items    []service.AccountShareMembershipHistoryEntry `json:"items"`
			Total    int64                                        `json:"total"`
			Page     int                                          `json:"page"`
			PageSize int                                          `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 ||
		envelope.Data.Total != 2 ||
		envelope.Data.Page != 2 ||
		envelope.Data.PageSize != 5 ||
		len(envelope.Data.Items) != 2 ||
		envelope.Data.Items[0].MembershipID != 11 ||
		envelope.Data.Items[1].MembershipID != 12 ||
		envelope.Data.Items[0].SnapshotQuality != service.AccountShareSnapshotQualityUnknown ||
		envelope.Data.Items[1].SnapshotQuality != service.AccountShareSnapshotQualityUnknown {
		t.Fatalf("unexpected history response: %#v", envelope)
	}
}

func TestAccountShareModeHandlerGetAPIKeyBindingStatusIncludesEnding(t *testing.T) {
	repo := &accountShareBindingStatusHandlerRepoStub{
		key: &service.APIKey{ID: 42, UserID: 7},
		memberships: []service.AccountShareMembership{
			{ID: 1, APIKeyID: 42, Status: service.AccountShareMembershipStatusActive},
			{ID: 2, APIKeyID: 42, Status: service.AccountShareMembershipStatusQueued},
			{
				ID:                    3,
				APIKeyID:              42,
				Status:                service.AccountShareMembershipStatusEnding,
				SettlementStatus:      "pending",
				EndingOperationID:     "00000000-0000-4000-8000-000000000003",
				EndingOperationStatus: "needs_attention",
			},
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, repo, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/account-share/api-key-bindings/42/status",
		nil,
	)
	c.Params = []gin.Param{{Key: "apiKeyID", Value: "42"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})

	handler.GetAPIKeyBindingStatus(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.consumerID != 7 || repo.apiKeyID != 42 {
		t.Fatalf("unexpected binding status scope: consumer=%d api_key=%d", repo.consumerID, repo.apiKeyID)
	}
	var envelope struct {
		Data service.AccountShareAPIKeyBindingStatus `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.APIKeyID != 42 ||
		envelope.Data.ActiveCount != 1 ||
		envelope.Data.QueuedCount != 1 ||
		envelope.Data.EndingCount != 1 ||
		envelope.Data.BlockingCount != 3 ||
		len(envelope.Data.Memberships) != 3 ||
		envelope.Data.Memberships[2].EndingOperationStatus != "needs_attention" {
		t.Fatalf("unexpected binding status response: %#v", envelope.Data)
	}
}

func TestAccountShareModeHandlerGetListingPassesViewerRoleToVisibilityQuery(t *testing.T) {
	for _, tt := range []struct {
		name      string
		role      string
		wantAdmin bool
	}{
		{name: "ordinary user", role: service.RoleUser},
		{name: "administrator", role: service.RoleAdmin, wantAdmin: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountShareVisibleListingHandlerRepoStub{}
			svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
			handler := NewAccountShareModeHandler(svc)
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account-share/listings/7", nil)
			c.Params = []gin.Param{{Key: "id", Value: "7"}}
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
			c.Set(string(middleware2.ContextKeyUserRole), tt.role)

			handler.GetListing(c)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if repo.viewerUserID != 42 || repo.viewerIsAdmin != tt.wantAdmin || repo.listingID != 7 {
				t.Fatalf(
					"unexpected visibility query: viewer=%d admin=%t listing=%d",
					repo.viewerUserID,
					repo.viewerIsAdmin,
					repo.listingID,
				)
			}
		})
	}
}

func TestAccountShareModeHandlerListDeletedReviewsPassesAdminRole(t *testing.T) {
	repo := &accountShareReviewHandlerRepoStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/account-share/listings/7/reviews?page=3&page_size=4",
		nil,
	)
	c.Params = []gin.Param{{Key: "id", Value: "7"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 900})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleAdmin)

	handler.ListListingReviews(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.viewerUserID != 900 ||
		!repo.viewerIsAdmin ||
		repo.listingID != 7 ||
		repo.params.Page != 3 ||
		repo.params.PageSize != 4 {
		t.Fatalf(
			"unexpected review scope: viewer=%d admin=%t listing=%d params=%#v",
			repo.viewerUserID,
			repo.viewerIsAdmin,
			repo.listingID,
			repo.params,
		)
	}
}

func TestAccountShareModeHandlerSubmitReviewReplaysWithoutDuplicateReview(t *testing.T) {
	repo := &accountShareReviewHandlerRepoStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)
	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(
			newUserMemoryIdempotencyRepoStub(),
			service.DefaultIdempotencyConfig(),
		),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	router := gin.New()
	router.Use(withUserSubject(42))
	router.POST("/api/v1/account-share/memberships/:id/review", handler.SubmitReview)

	call := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/account-share/memberships/7/review",
			bytes.NewBufferString(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "submit-review-once")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := call(`{"score":9}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusCreated, first.Body.String())
	}
	replay := call(`{"score":9}`)
	if replay.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, want %d; body=%s", replay.Code, http.StatusCreated, replay.Body.String())
	}
	if replay.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatalf("replay header = %q, want true", replay.Header().Get("X-Idempotency-Replayed"))
	}
	conflict := call(`{"score":8}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
	}
	if repo.submitCalls != 1 {
		t.Fatalf("repository submit calls = %d, want 1", repo.submitCalls)
	}
}

func TestAccountShareModeHandlerUpdateListingRejectsMissingExpectedVersion(t *testing.T) {
	repo := &accountShareUpdateRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareListingUpdate(t, handler, service.RoleUser, `{"name":"updated-room"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var envelope accountShareHandlerErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Reason != "ACCOUNT_SHARE_ROOM_EXPECTED_VERSION_REQUIRED" {
		t.Fatalf("reason = %q, want expected-version error", envelope.Reason)
	}
	if envelope.Metadata["field"] != "expected_version" {
		t.Fatalf("metadata = %#v, want expected_version field", envelope.Metadata)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("repository update calls = %d, want 0", repo.updateCalls)
	}
}

func TestAccountShareModeHandlerUpdateListingRejectsMissingAuditReason(t *testing.T) {
	repo := &accountShareUpdateRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareListingUpdate(
		t,
		handler,
		service.RoleUser,
		`{"name":"updated-room","expected_version":3}`,
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var envelope accountShareHandlerErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Reason != "ACCOUNT_SHARE_ROOM_UPDATE_REASON_REQUIRED" {
		t.Fatalf("reason = %q, want update-reason error", envelope.Reason)
	}
	if envelope.Metadata["field"] != "reason" {
		t.Fatalf("metadata = %#v, want reason field", envelope.Metadata)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("repository update calls = %d, want 0", repo.updateCalls)
	}
}

func TestAccountShareModeHandlerUpdateListingRejectsIncompleteAdminForceConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantReason string
		wantField  string
	}{
		{
			name:       "missing reason",
			body:       `{"name":"updated-room","expected_version":3,"force_active_edit":true,"confirmed":true}`,
			wantReason: "ACCOUNT_SHARE_ROOM_FORCE_REASON_REQUIRED",
			wantField:  "reason",
		},
		{
			name:       "missing confirmation",
			body:       `{"name":"updated-room","expected_version":3,"force_active_edit":true,"reason":"risk review"}`,
			wantReason: "ACCOUNT_SHARE_ROOM_FORCE_CONFIRMATION_REQUIRED",
			wantField:  "confirmed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountShareUpdateRepositoryStub{}
			svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
			handler := NewAccountShareModeHandler(svc)

			recorder := performAccountShareListingUpdate(t, handler, service.RoleAdmin, tt.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var envelope accountShareHandlerErrorEnvelope
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.Reason != tt.wantReason || envelope.Metadata["field"] != tt.wantField {
				t.Fatalf("unexpected error envelope: %#v", envelope)
			}
			if repo.updateCalls != 0 {
				t.Fatalf("repository update calls = %d, want 0", repo.updateCalls)
			}
		})
	}
}

func TestAccountShareModeHandlerUpdateListingPassesVersionAndAdminAuditFields(t *testing.T) {
	repo := &accountShareUpdateRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareListingUpdate(t, handler, service.RoleAdmin, `{
		"seat_limit": 3,
		"allowed_models": [" gpt-5 ", "gpt-5"],
		"edit_session_id": " edit-1 ",
		"expected_version": 9,
		"force_active_edit": true,
		"reason": " risk review ",
		"confirmed": true
	}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.updateCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1", repo.updateCalls)
	}
	if repo.actorUserID != 42 || !repo.actorAdmin || repo.listingID != 7 {
		t.Fatalf("unexpected actor/listing: user=%d admin=%v listing=%d", repo.actorUserID, repo.actorAdmin, repo.listingID)
	}
	if repo.input.ExpectedVersion == nil || *repo.input.ExpectedVersion != 9 {
		t.Fatalf("expected version = %v, want 9", repo.input.ExpectedVersion)
	}
	if !repo.input.ForceActiveEdit || !repo.input.Confirmed || repo.input.Reason != "risk review" {
		t.Fatalf("unexpected force audit fields: %+v", repo.input)
	}
	if repo.input.EditSessionID != "edit-1" {
		t.Fatalf("edit session = %q, want edit-1", repo.input.EditSessionID)
	}
	if repo.input.SeatLimit == nil || *repo.input.SeatLimit != 3 {
		t.Fatalf("seat limit = %v, want 3", repo.input.SeatLimit)
	}
	if repo.input.AllowedModels == nil || len(*repo.input.AllowedModels) != 1 || (*repo.input.AllowedModels)[0] != "gpt-5" {
		t.Fatalf("allowed models = %v, want normalized gpt-5", repo.input.AllowedModels)
	}
}

func TestAccountShareModeHandlerUpdateListingReplaysWithoutSecondMutation(t *testing.T) {
	repo := &accountShareUpdateRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)
	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(
			newUserMemoryIdempotencyRepoStub(),
			service.DefaultIdempotencyConfig(),
		),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Set(string(middleware2.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	router.PATCH("/api/v1/account-share/listings/:id", handler.UpdateListing)

	call := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPatch,
			"/api/v1/account-share/listings/7",
			bytes.NewBufferString(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "update-listing-once")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := call(`{"name":"updated-room","expected_version":9,"reason":"idempotency replay test"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	replay := call(`{"name":"updated-room","expected_version":9,"reason":"idempotency replay test"}`)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d; body=%s", replay.Code, http.StatusOK, replay.Body.String())
	}
	if replay.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatalf("replay header = %q, want true", replay.Header().Get("X-Idempotency-Replayed"))
	}
	conflict := call(`{"name":"different-room","expected_version":9,"reason":"idempotency replay test"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
	}
	if repo.updateCalls != 1 {
		t.Fatalf("repository update calls = %d, want 1", repo.updateCalls)
	}
}

func TestAccountShareModeHandlerEditSessionMutationsReplaySafely(t *testing.T) {
	tests := []struct {
		name      string
		route     string
		path      string
		firstBody string
		otherBody string
		invoke    func(*AccountShareModeHandler) gin.HandlerFunc
		callCount func(*accountShareEditSessionRepositoryStub) int
	}{
		{
			name:      "begin",
			route:     "/api/v1/account-share/listings/:id/edit-session",
			path:      "/api/v1/account-share/listings/7/edit-session",
			firstBody: `{"session_id":"edit-session-1"}`,
			otherBody: `{"session_id":"edit-session-2"}`,
			invoke: func(handler *AccountShareModeHandler) gin.HandlerFunc {
				return handler.BeginListingEdit
			},
			callCount: func(repo *accountShareEditSessionRepositoryStub) int {
				return repo.beginCalls
			},
		},
		{
			name:      "release",
			route:     "/api/v1/account-share/listings/:id/edit-session/release",
			path:      "/api/v1/account-share/listings/7/edit-session/release",
			firstBody: `{"session_id":"edit-session-1"}`,
			otherBody: `{"session_id":"edit-session-2"}`,
			invoke: func(handler *AccountShareModeHandler) gin.HandlerFunc {
				return handler.ReleaseListingEdit
			},
			callCount: func(repo *accountShareEditSessionRepositoryStub) int {
				return repo.releaseCalls
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountShareEditSessionRepositoryStub{}
			svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
			svc.SetRuntimeDependencies(
				service.NewConcurrencyService(&accountShareEndHandlerConcurrencyCache{}),
				nil,
				nil,
				nil,
			)
			handler := NewAccountShareModeHandler(svc)
			service.SetDefaultIdempotencyCoordinator(
				service.NewIdempotencyCoordinator(
					newUserMemoryIdempotencyRepoStub(),
					service.DefaultIdempotencyConfig(),
				),
			)
			t.Cleanup(func() {
				service.SetDefaultIdempotencyCoordinator(nil)
			})

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
				c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)
				c.Next()
			})
			router.POST(tt.route, tt.invoke(handler))

			call := func(body string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", "edit-session-once")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				return recorder
			}

			first := call(tt.firstBody)
			if first.Code != http.StatusOK {
				t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
			}
			replay := call(tt.firstBody)
			if replay.Code != http.StatusOK {
				t.Fatalf("replay status = %d, want %d; body=%s", replay.Code, http.StatusOK, replay.Body.String())
			}
			if replay.Header().Get("X-Idempotency-Replayed") != "true" {
				t.Fatalf("replay header = %q, want true", replay.Header().Get("X-Idempotency-Replayed"))
			}
			conflict := call(tt.otherBody)
			if conflict.Code != http.StatusConflict {
				t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
			}
			if calls := tt.callCount(repo); calls != 1 {
				t.Fatalf("repository calls = %d, want 1", calls)
			}
		})
	}
}

func TestExecuteAccountShareOAuthExchangeReplaysWithoutConsumingCodeTwice(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		path  string
	}{
		{
			name:  "openai",
			scope: "account_share_openai_exchange_create_room",
			path:  "/account-share/openai/exchange-code",
		},
		{
			name:  "anthropic",
			scope: "account_share_anthropic_exchange_create_room",
			path:  "/account-share/anthropic/exchange-code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newUserMemoryIdempotencyRepoStub()
			service.SetDefaultIdempotencyCoordinator(
				service.NewIdempotencyCoordinator(repo, service.DefaultIdempotencyConfig()),
			)
			t.Cleanup(func() {
				service.SetDefaultIdempotencyCoordinator(nil)
			})

			var exchangeCalls atomic.Int32
			router := gin.New()
			router.Use(withUserSubject(42))
			router.POST(tt.path, func(c *gin.Context) {
				var request struct {
					Code string `json:"code" binding:"required"`
				}
				if err := c.ShouldBindJSON(&request); err != nil {
					c.Status(http.StatusBadRequest)
					return
				}
				executeAccountShareOAuthExchange(
					c,
					tt.scope,
					request,
					func(context.Context) (any, error) {
						exchangeCalls.Add(1)
						return gin.H{"listing_id": int64(7)}, nil
					},
				)
			})

			call := func(code string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(
					http.MethodPost,
					tt.path,
					bytes.NewBufferString(`{"code":"`+code+`"}`),
				)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", "oauth-exchange-once")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				return recorder
			}

			first := call("one-time-code")
			if first.Code != http.StatusCreated {
				t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusCreated, first.Body.String())
			}
			second := call("one-time-code")
			if second.Code != http.StatusCreated {
				t.Fatalf("replay status = %d, want %d; body=%s", second.Code, http.StatusCreated, second.Body.String())
			}
			if second.Header().Get("X-Idempotency-Replayed") != "true" {
				t.Fatalf("replay header = %q, want true", second.Header().Get("X-Idempotency-Replayed"))
			}
			conflict := call("different-code")
			if conflict.Code != http.StatusConflict {
				t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
			}
			if exchangeCalls.Load() != 1 {
				t.Fatalf("OAuth exchange calls = %d, want 1", exchangeCalls.Load())
			}
		})
	}
}

func TestExecuteAccountShareOAuthExchangeFailsClosedWithoutCoordinator(t *testing.T) {
	service.SetDefaultIdempotencyCoordinator(nil)

	var exchangeCalls atomic.Int32
	router := gin.New()
	router.Use(withUserSubject(42))
	router.POST("/account-share/openai/exchange-code", func(c *gin.Context) {
		executeAccountShareOAuthExchange(
			c,
			"account_share_openai_exchange_create_room",
			map[string]any{"code": "one-time-code"},
			func(context.Context) (any, error) {
				exchangeCalls.Add(1)
				return gin.H{"listing_id": int64(7)}, nil
			},
		)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/account-share/openai/exchange-code",
		bytes.NewBufferString(`{"code":"one-time-code"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "oauth-exchange-once")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	if exchangeCalls.Load() != 0 {
		t.Fatalf("OAuth exchange calls = %d, want 0", exchangeCalls.Load())
	}
}

func TestAccountShareModeHandlerJoinRejectsMissingConfirmedRevision(t *testing.T) {
	repo := &accountShareUpdateRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/account-share/listings/7/join",
		bytes.NewBufferString(`{
			"api_key_id": 3,
			"idle_timeout_minutes": 30,
			"intent_token": "signed-intent",
			"expected_version": 1,
			"accept_queue": true
		}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "7"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})

	handler.JoinListing(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func performAccountShareRoomBatchMutation(
	t *testing.T,
	handler *AccountShareModeHandler,
	attach bool,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(
			newUserMemoryIdempotencyRepoStub(),
			service.DefaultIdempotencyConfig(),
		),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/account-share/listings/700/accounts/batch",
		bytes.NewBufferString(body),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "700"}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)

	if attach {
		handler.AttachRoomAccounts(c)
	} else {
		handler.DetachRoomAccounts(c)
	}
	return recorder
}

func TestAccountShareModeHandlerAttachBatchReturnsAllSuccessResponse(t *testing.T) {
	repo := &accountShareRoomBatchHandlerRepoStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareRoomBatchMutation(
		t,
		handler,
		true,
		`{"account_ids":[11,10,11],"idempotency_key":"attach-handler"}`,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.attachCalls != 1 {
		t.Fatalf("attach repository calls = %d, want 1", repo.attachCalls)
	}
	if len(repo.attachInput.AccountIDs) != 2 ||
		repo.attachInput.AccountIDs[0] != 11 ||
		repo.attachInput.AccountIDs[1] != 10 {
		t.Fatalf("repository account IDs = %v, want [11 10]", repo.attachInput.AccountIDs)
	}
	var envelope struct {
		Data service.BulkUpdateAccountsResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Success != 2 ||
		envelope.Data.Failed != 0 ||
		len(envelope.Data.FailedIDs) != 0 ||
		len(envelope.Data.Results) != 2 {
		t.Fatalf("unexpected atomic success response: %#v", envelope.Data)
	}
	for _, item := range envelope.Data.Results {
		if !item.Success || item.Error != "" {
			t.Fatalf("unexpected item result: %#v", item)
		}
	}
}

func TestAccountShareModeHandlerAttachBatchReturnsPartialSuccessResponse(t *testing.T) {
	repo := &accountShareRoomBatchHandlerRepoStub{
		attachResult: &service.BulkUpdateAccountsResult{
			Success:    1,
			Failed:     1,
			SuccessIDs: []int64{10},
			FailedIDs:  []int64{11},
			Results: []service.BulkUpdateAccountResult{
				{AccountID: 10, Success: true},
				{
					AccountID: 11,
					Success:   false,
					Error:     service.ErrAccountShareAccountUnavailable.Message,
					Reason:    service.ErrAccountShareAccountUnavailable.Reason,
					Message:   service.ErrAccountShareAccountUnavailable.Message,
					Metadata:  map[string]string{"blocker": "overloaded"},
				},
			},
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareRoomBatchMutation(
		t,
		handler,
		true,
		`{"account_ids":[10,11],"idempotency_key":"attach-partial"}`,
	)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data service.BulkUpdateAccountsResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 1, envelope.Data.Success)
	require.Equal(t, 1, envelope.Data.Failed)
	require.Equal(t, []int64{10}, envelope.Data.SuccessIDs)
	require.Equal(t, []int64{11}, envelope.Data.FailedIDs)
	require.Len(t, envelope.Data.Results, 2)
	require.True(t, envelope.Data.Results[0].Success)
	require.Equal(t, "ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE", envelope.Data.Results[1].Reason)
	require.Equal(t, "overloaded", envelope.Data.Results[1].Metadata["blocker"])
}

func TestAccountShareModeHandlerAttachBatchFailureReturnsErrorWithoutPartialData(t *testing.T) {
	repo := &accountShareRoomBatchHandlerRepoStub{
		attachErr: service.ErrAccountShareRoomAccountConflict,
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareRoomBatchMutation(
		t,
		handler,
		true,
		`{"account_ids":[10,11],"idempotency_key":"attach-conflict"}`,
	)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if repo.attachCalls != 1 {
		t.Fatalf("attach repository calls = %d, want 1", repo.attachCalls)
	}
	var envelope struct {
		Reason string          `json:"reason"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Reason != "ACCOUNT_SHARE_ROOM_ACCOUNT_CONFLICT" {
		t.Fatalf("reason = %q, want account conflict", envelope.Reason)
	}
	if len(envelope.Data) != 0 && string(envelope.Data) != "null" {
		t.Fatalf("partial data must be absent on rollback, got %s", envelope.Data)
	}
}

func TestAccountShareModeHandlerDetachBatchUsesAtomicRepositoryCall(t *testing.T) {
	repo := &accountShareRoomBatchHandlerRepoStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetRuntimeDependencies(
		service.NewConcurrencyService(accountShareRoomBatchConcurrencyCacheStub{}),
		nil,
		nil,
		nil,
	)
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareRoomBatchMutation(
		t,
		handler,
		false,
		`{"account_ids":[10,11],"idempotency_key":"detach-handler"}`,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.detachCalls != 1 {
		t.Fatalf("detach repository calls = %d, want 1", repo.detachCalls)
	}
	if repo.detachInput.IdempotencyKey != "detach-handler" {
		t.Fatalf("idempotency key = %q, want detach-handler", repo.detachInput.IdempotencyKey)
	}
}

func TestAccountShareModeHandlerRoomBatchMutationsReplayAndRejectKeyReuse(t *testing.T) {
	tests := []struct {
		name      string
		route     string
		path      string
		handler   func(*AccountShareModeHandler) gin.HandlerFunc
		callCount func(*accountShareRoomBatchHandlerRepoStub) int
	}{
		{
			name:  "attach",
			route: "/api/v1/account-share/listings/:id/accounts/attach-batch",
			path:  "/api/v1/account-share/listings/700/accounts/attach-batch",
			handler: func(handler *AccountShareModeHandler) gin.HandlerFunc {
				return handler.AttachRoomAccounts
			},
			callCount: func(repo *accountShareRoomBatchHandlerRepoStub) int {
				return repo.attachCalls
			},
		},
		{
			name:  "detach",
			route: "/api/v1/account-share/listings/:id/accounts/detach-batch",
			path:  "/api/v1/account-share/listings/700/accounts/detach-batch",
			handler: func(handler *AccountShareModeHandler) gin.HandlerFunc {
				return handler.DetachRoomAccounts
			},
			callCount: func(repo *accountShareRoomBatchHandlerRepoStub) int {
				return repo.detachCalls
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountShareRoomBatchHandlerRepoStub{}
			svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
			if tt.name == "detach" {
				svc.SetRuntimeDependencies(
					service.NewConcurrencyService(accountShareRoomBatchConcurrencyCacheStub{}),
					nil,
					nil,
					nil,
				)
			}
			handler := NewAccountShareModeHandler(svc)
			service.SetDefaultIdempotencyCoordinator(
				service.NewIdempotencyCoordinator(
					newUserMemoryIdempotencyRepoStub(),
					service.DefaultIdempotencyConfig(),
				),
			)
			t.Cleanup(func() {
				service.SetDefaultIdempotencyCoordinator(nil)
			})

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
				c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)
				c.Next()
			})
			router.POST(tt.route, tt.handler(handler))

			call := func(accountIDs string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(
					http.MethodPost,
					tt.path,
					bytes.NewBufferString(
						`{"account_ids":`+accountIDs+`,"idempotency_key":"room-batch-once"}`,
					),
				)
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				return recorder
			}

			first := call(`[10,11]`)
			if first.Code != http.StatusOK {
				t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
			}
			replay := call(`[10,11]`)
			if replay.Code != http.StatusOK {
				t.Fatalf("replay status = %d, want %d; body=%s", replay.Code, http.StatusOK, replay.Body.String())
			}
			if replay.Header().Get("X-Idempotency-Replayed") != "true" {
				t.Fatalf("replay header = %q, want true", replay.Header().Get("X-Idempotency-Replayed"))
			}
			conflict := call(`[12]`)
			if conflict.Code != http.StatusConflict {
				t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
			}
			if calls := tt.callCount(repo); calls != 1 {
				t.Fatalf("repository calls = %d, want 1", calls)
			}
		})
	}
}

func TestAccountShareModeHandlerEndMembershipReturnsAcceptedWhileEnding(t *testing.T) {
	now := time.Date(2026, 7, 27, 7, 0, 0, 0, time.UTC)
	repo := &accountShareEndHandlerRepoStub{
		snapshot: &service.AccountShareMembership{
			ID:             7,
			ConsumerUserID: 42,
			Status:         service.AccountShareMembershipStatusActive,
			UpdatedAt:      now,
		},
		result: &service.AccountShareMembership{
			ID:             7,
			ConsumerUserID: 42,
			APIKeyID:       70,
			Status:         service.AccountShareMembershipStatusEnding,
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetActionTokenSecret(strings.Repeat("s", 32))
	svc.SetRuntimeDependencies(
		service.NewConcurrencyService(&accountShareEndHandlerConcurrencyCache{active: 1}),
		nil,
		nil,
		nil,
	)
	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 7)
	if err != nil {
		t.Fatalf("CreateEndMembershipToken: %v", err)
	}
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareMembershipEnd(t, handler, 7, intent.Token)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
}

func TestAccountShareModeHandlerEndMembershipReturnsOKWhenEnded(t *testing.T) {
	now := time.Date(2026, 7, 27, 7, 5, 0, 0, time.UTC)
	repo := &accountShareEndHandlerRepoStub{
		snapshot: &service.AccountShareMembership{
			ID:             8,
			ConsumerUserID: 42,
			Status:         service.AccountShareMembershipStatusQueued,
			UpdatedAt:      now,
		},
		result: &service.AccountShareMembership{
			ID:             8,
			ConsumerUserID: 42,
			APIKeyID:       80,
			Status:         service.AccountShareMembershipStatusEnded,
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetActionTokenSecret(strings.Repeat("s", 32))
	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 8)
	if err != nil {
		t.Fatalf("CreateEndMembershipToken: %v", err)
	}
	handler := NewAccountShareModeHandler(svc)

	recorder := performAccountShareMembershipEnd(t, handler, 8, intent.Token)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func performAccountShareMembershipEnd(
	t *testing.T,
	handler *AccountShareModeHandler,
	membershipID int64,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatalf("marshal end request: %v", err)
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/account-share/memberships/"+strconv.FormatInt(membershipID, 10)+"/end",
		bytes.NewReader(body),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: strconv.FormatInt(membershipID, 10)}}
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)
	handler.EndMembership(c)
	return recorder
}
