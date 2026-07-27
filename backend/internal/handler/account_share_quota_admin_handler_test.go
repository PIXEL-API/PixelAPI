package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountShareQuotaHandlerRepositoryStub struct {
	service.AccountShareModeRepository

	globalPolicy service.AccountShareQuotaPolicy
	ownerPolicy  *service.AccountShareQuotaPolicy
	state        service.AccountShareQuotaAdminState
	auditItems   []service.AccountShareQuotaPolicy
	auditTotal   int64

	appendCalls int
	appendInput service.AppendAccountShareQuotaPolicyInput
	stateCalls  int
	auditCalls  int
	auditScope  string
	auditOwner  *int64
	auditParams pagination.PaginationParams
	applyCalls  int
	applyResult *service.AccountShareGrandfatherBatchItemResult
}

func (r *accountShareQuotaHandlerRepositoryStub) ResolveAccountShareQuota(
	context.Context,
	int64,
	time.Time,
) (*service.AccountShareResolvedQuota, error) {
	resolved := r.state.EffectiveQuota
	return &resolved, nil
}

func (r *accountShareQuotaHandlerRepositoryStub) GetLatestAccountShareQuotaPolicy(
	_ context.Context,
	scopeType string,
	_ *int64,
) (*service.AccountShareQuotaPolicy, error) {
	if scopeType == service.AccountShareQuotaScopeGlobal {
		policy := r.globalPolicy
		return &policy, nil
	}
	if r.ownerPolicy == nil {
		return nil, nil
	}
	policy := *r.ownerPolicy
	return &policy, nil
}

func (r *accountShareQuotaHandlerRepositoryStub) GetAccountShareQuotaAdminState(
	context.Context,
	int64,
	time.Time,
) (*service.AccountShareQuotaAdminState, error) {
	r.stateCalls++
	state := r.state
	return &state, nil
}

func (r *accountShareQuotaHandlerRepositoryStub) AppendAccountShareQuotaPolicyRevision(
	_ context.Context,
	input service.AppendAccountShareQuotaPolicyInput,
) (*service.AccountShareQuotaPolicy, error) {
	r.appendCalls++
	r.appendInput = input
	return &service.AccountShareQuotaPolicy{
		ID:                  99,
		ScopeType:           input.ScopeType,
		OwnerUserID:         input.OwnerUserID,
		Version:             input.ExpectedVersion + 1,
		Status:              input.Status,
		OverrideKind:        input.OverrideKind,
		Limits:              input.Limits,
		EffectiveAt:         input.EffectiveAt,
		ExpiresAt:           input.ExpiresAt,
		Reason:              input.Reason,
		ActorUserIDSnapshot: input.ActorUserID,
	}, nil
}

func (r *accountShareQuotaHandlerRepositoryStub) ListAccountShareQuotaPolicyRevisions(
	_ context.Context,
	scopeType string,
	ownerUserID *int64,
	params pagination.PaginationParams,
) ([]service.AccountShareQuotaPolicy, int64, error) {
	r.auditCalls++
	r.auditScope = scopeType
	r.auditParams = params
	if ownerUserID != nil {
		ownerID := *ownerUserID
		r.auditOwner = &ownerID
	}
	return append([]service.AccountShareQuotaPolicy(nil), r.auditItems...), r.auditTotal, nil
}

func (r *accountShareQuotaHandlerRepositoryStub) ListAccountShareGrandfatherCandidates(
	context.Context,
	time.Time,
	pagination.PaginationParams,
) ([]service.AccountShareGrandfatherCandidate, int64, error) {
	return nil, 0, nil
}

func (r *accountShareQuotaHandlerRepositoryStub) ApplyAccountShareGrandfatherCandidate(
	_ context.Context,
	input service.ApplyAccountShareGrandfatherCandidateInput,
) (*service.AccountShareGrandfatherBatchItemResult, error) {
	r.applyCalls++
	if r.applyResult != nil {
		result := *r.applyResult
		result.OwnerUserID = input.Item.OwnerUserID
		return &result, nil
	}
	return &service.AccountShareGrandfatherBatchItemResult{
		OwnerUserID:   input.Item.OwnerUserID,
		Status:        "applied",
		PolicyID:      100 + input.Item.OwnerUserID,
		PolicyVersion: input.Item.ExpectedVersion + 1,
		ExpiresAt:     &input.ExpiresAt,
	}, nil
}

func newAccountShareQuotaAdminTestRouter(
	handler *AccountShareModeHandler,
	role string,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(
			string(middleware2.ContextKeyUser),
			middleware2.AuthSubject{UserID: 900},
		)
		c.Set(string(middleware2.ContextKeyUserRole), role)
		c.Next()
	})
	router.GET(
		"/api/v1/admin/account-share/quotas/owners/:owner_id",
		handler.GetOwnerQuotaForAdmin,
	)
	router.PUT(
		"/api/v1/admin/account-share/quotas/global",
		handler.UpdateGlobalQuotaForAdmin,
	)
	router.GET(
		"/api/v1/admin/account-share/quotas/audit",
		handler.ListQuotaAuditForAdmin,
	)
	router.GET(
		"/api/v1/admin/account-share/quotas/grandfather-candidates",
		handler.ListGrandfatherCandidatesForAdmin,
	)
	router.POST(
		"/api/v1/admin/account-share/quotas/grandfather/batch",
		handler.BatchGrandfatherQuotaForAdmin,
	)
	return router
}

func TestAccountShareQuotaAdminHandlerOwnerStateUsesStableJSONContract(t *testing.T) {
	limits := service.DefaultAccountShareQuotaLimits()
	repo := &accountShareQuotaHandlerRepositoryStub{
		state: service.AccountShareQuotaAdminState{
			GlobalPolicy: service.AccountShareQuotaPolicy{
				ID:           1,
				ScopeType:    service.AccountShareQuotaScopeGlobal,
				Version:      1,
				Status:       service.AccountShareQuotaPolicyStatusActive,
				OverrideKind: service.AccountShareQuotaPolicyKindDefault,
				Limits:       limits,
			},
			EffectiveQuota: service.AccountShareResolvedQuota{
				Limits:        limits,
				Source:        service.AccountShareQuotaScopeGlobal,
				PolicyID:      1,
				PolicyVersion: 1,
				OverrideKind:  service.AccountShareQuotaPolicyKindDefault,
			},
			Usage: service.AccountShareQuotaUsage{
				LiveRooms:           2,
				RoomCreates24Hours:  3,
				OwnerRoomAccounts:   4,
				LargestRoomAccounts: 2,
			},
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	router := newAccountShareQuotaAdminTestRouter(
		NewAccountShareModeHandler(svc),
		service.RoleAdmin,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/account-share/quotas/owners/42",
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Usage map[string]int `json:"usage"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, map[string]int{
		"live_rooms":            2,
		"room_creates_24_hours": 3,
		"owner_room_accounts":   4,
		"largest_room_accounts": 2,
	}, envelope.Data.Usage)
	require.Equal(t, 1, repo.stateCalls)
}

func TestAccountShareQuotaAdminHandlerRejectsNonAdminBeforeRepository(t *testing.T) {
	repo := &accountShareQuotaHandlerRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	router := newAccountShareQuotaAdminTestRouter(
		NewAccountShareModeHandler(svc),
		service.RoleUser,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/account-share/quotas/owners/42",
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Zero(t, repo.stateCalls)
}

func TestAccountShareQuotaAdminHandlerGlobalMutationIsIdempotent(t *testing.T) {
	repo := &accountShareQuotaHandlerRepositoryStub{}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	router := newAccountShareQuotaAdminTestRouter(
		NewAccountShareModeHandler(svc),
		service.RoleAdmin,
	)
	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(
			newUserMemoryIdempotencyRepoStub(),
			service.DefaultIdempotencyConfig(),
		),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	const body = `{
		"limits": {
			"max_live_rooms": 6,
			"max_room_creates_24_hours": 7,
			"max_accounts_per_room": 20,
			"max_room_accounts_per_owner": 120
		},
		"expected_version": 1,
		"reason": "容量评估通过",
		"confirmed": true
	}`
	call := func(idempotencyKey string, payload string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPut,
			"/api/v1/admin/account-share/quotas/global",
			bytes.NewBufferString(payload),
		)
		request.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	missingKey := call("", body)
	require.Equal(t, http.StatusBadRequest, missingKey.Code, missingKey.Body.String())
	require.Zero(t, repo.appendCalls)

	first := call("quota-global-v2", body)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	replay := call("quota-global-v2", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, repo.appendCalls)
	require.Equal(t, int64(900), repo.appendInput.ActorUserID)
	require.Equal(t, int64(1), repo.appendInput.ExpectedVersion)
	require.Equal(t, "容量评估通过", repo.appendInput.Reason)

	conflict := call(
		"quota-global-v2",
		`{
			"limits": {
				"max_live_rooms": 8,
				"max_room_creates_24_hours": 7,
				"max_accounts_per_room": 20,
				"max_room_accounts_per_owner": 120
			},
			"expected_version": 1,
			"reason": "容量评估通过",
			"confirmed": true
		}`,
	)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Equal(t, 1, repo.appendCalls)
}

func TestAccountShareQuotaAdminHandlerBatchReplayPreservesFullResponse(t *testing.T) {
	repo := &accountShareQuotaHandlerRepositoryStub{
		applyResult: &service.AccountShareGrandfatherBatchItemResult{
			Status:     "conflict",
			ResultCode: "ACCOUNT_SHARE_QUOTA_CANDIDATE_STALE",
			Message:    "candidate usage or effective quota changed; refresh the preview",
		},
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	router := newAccountShareQuotaAdminTestRouter(
		NewAccountShareModeHandler(svc),
		service.RoleAdmin,
	)
	service.SetDefaultIdempotencyCoordinator(
		service.NewIdempotencyCoordinator(
			newUserMemoryIdempotencyRepoStub(),
			service.DefaultIdempotencyConfig(),
		),
	)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	body := `{
		"items": [{
			"owner_user_id": 42,
			"expected_version": 3,
			"preview_usage": {
				"live_rooms": 6,
				"room_creates_24_hours": 5,
				"owner_room_accounts": 100,
				"largest_room_accounts": 20
			},
			"preview_fingerprint": "candidate-42"
		}],
		"expires_at": "` + expiresAt + `",
		"reason": "历史超限冻结",
		"confirmed": true
	}`
	call := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/admin/account-share/quotas/grandfather/batch",
			bytes.NewBufferString(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "quota-grandfather-batch-v1")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := call()
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	replay := call()
	require.Equal(t, first.Code, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, repo.applyCalls)

	var envelope struct {
		Code int `json:"code"`
		Data []struct {
			ResultCode string `json:"result_code"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Len(t, envelope.Data, 1)
	require.Equal(t, "ACCOUNT_SHARE_QUOTA_CANDIDATE_STALE", envelope.Data[0].ResultCode)
}

func TestAccountShareQuotaAdminHandlerAuditScopesOwnerAndPagination(t *testing.T) {
	ownerUserID := int64(42)
	repo := &accountShareQuotaHandlerRepositoryStub{
		auditItems: []service.AccountShareQuotaPolicy{
			{
				ID:          2,
				ScopeType:   service.AccountShareQuotaScopeOwner,
				OwnerUserID: &ownerUserID,
				Version:     2,
			},
		},
		auditTotal: 7,
	}
	svc := service.NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	router := newAccountShareQuotaAdminTestRouter(
		NewAccountShareModeHandler(svc),
		service.RoleAdmin,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/account-share/quotas/audit?scope_type=owner&owner_id=42&page=2&page_size=3",
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, repo.auditCalls)
	require.Equal(t, service.AccountShareQuotaScopeOwner, repo.auditScope)
	require.NotNil(t, repo.auditOwner)
	require.Equal(t, ownerUserID, *repo.auditOwner)
	require.Equal(t, 2, repo.auditParams.Page)
	require.Equal(t, 3, repo.auditParams.PageSize)

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, int64(7), envelope.Data.Total)
	require.Equal(t, 2, envelope.Data.Page)
	require.Equal(t, 3, envelope.Data.PageSize)
}
