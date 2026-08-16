package handler

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	userAgentIdentityPrivateGroupID int64 = 8201
	userAgentIdentityPublicGroupID  int64 = 8202
)

type userAgentIdentityShareRepo struct {
	service.AccountRepository
	accounts map[int64]*service.Account
}

func cloneUserAgentIdentityShareAccount(account *service.Account) *service.Account {
	if account == nil {
		return nil
	}
	clone := *account
	clone.Credentials = make(map[string]any, len(account.Credentials))
	for key, value := range account.Credentials {
		clone.Credentials[key] = value
	}
	clone.Extra = make(map[string]any, len(account.Extra))
	for key, value := range account.Extra {
		clone.Extra[key] = value
	}
	clone.GroupIDs = append([]int64(nil), account.GroupIDs...)
	if account.OwnerUserID != nil {
		ownerUserID := *account.OwnerUserID
		clone.OwnerUserID = &ownerUserID
	}
	if account.ExternalPlacement != nil {
		placement := *account.ExternalPlacement
		clone.ExternalPlacement = &placement
	}
	return &clone
}

func (r *userAgentIdentityShareRepo) GetByID(_ context.Context, accountID int64) (*service.Account, error) {
	account := r.accounts[accountID]
	if account == nil {
		return nil, service.ErrAccountNotFound
	}
	return cloneUserAgentIdentityShareAccount(account), nil
}

func (r *userAgentIdentityShareRepo) GetByIDs(_ context.Context, accountIDs []int64) ([]*service.Account, error) {
	accounts := make([]*service.Account, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		if account := r.accounts[accountID]; account != nil {
			accounts = append(accounts, cloneUserAgentIdentityShareAccount(account))
		}
	}
	return accounts, nil
}

func (r *userAgentIdentityShareRepo) ListOwnedAccountIDs(
	_ context.Context,
	ownerUserID int64,
	accountIDs []int64,
) ([]int64, error) {
	ownedIDs := make([]int64, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		account := r.accounts[accountID]
		if account == nil || account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
			continue
		}
		ownedIDs = append(ownedIDs, accountID)
	}
	return ownedIDs, nil
}

func (r *userAgentIdentityShareRepo) Update(_ context.Context, account *service.Account) error {
	r.accounts[account.ID] = cloneUserAgentIdentityShareAccount(account)
	return nil
}

func (r *userAgentIdentityShareRepo) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	account := r.accounts[accountID]
	if account == nil {
		return service.ErrAccountNotFound
	}
	account.GroupIDs = append([]int64(nil), groupIDs...)
	return nil
}

func (r *userAgentIdentityShareRepo) IsAccountShareModeListingAccount(context.Context, int64) (bool, error) {
	return false, nil
}

func (r *userAgentIdentityShareRepo) ListOwnedWithFilters(
	_ context.Context,
	ownerUserID int64,
	params pagination.PaginationParams,
	platform, accountType, _ string,
	_ string,
	_, _ int64,
	_ string,
) ([]service.Account, *pagination.PaginationResult, error) {
	accounts := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
			continue
		}
		if platform != "" && account.Platform != platform {
			continue
		}
		if accountType != "" && account.Type != accountType {
			continue
		}
		accounts = append(accounts, *cloneUserAgentIdentityShareAccount(account))
	}
	total := int64(len(accounts))
	start := params.Offset()
	if start >= len(accounts) {
		return []service.Account{}, &pagination.PaginationResult{Total: total}, nil
	}
	end := start + params.Limit()
	if end > len(accounts) {
		end = len(accounts)
	}
	return accounts[start:end], &pagination.PaginationResult{Total: total}, nil
}

type userAgentIdentityPrivateGroupProvisioner struct{}

type userAgentIdentityPlacementRepo struct {
	service.AccountShareModeRepository
	service.AccountShareRoomRepository
	accountRepo *userAgentIdentityShareRepo
	// convertErr 非空时按账号 id 匹配（含 0 表示全部）返回该错误，用于验证
	// 批量转换失败项透出 reason/message。
	convertErr    error
	convertErrFor int64
}

func (r *userAgentIdentityPlacementRepo) HasRoomAccount(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *userAgentIdentityPlacementRepo) IsModeGroup(context.Context, int64) (bool, error) {
	return false, nil
}

func (r *userAgentIdentityPlacementRepo) BeginExternalPlacementDrain(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *userAgentIdentityPlacementRepo) RestoreExternalPlacementAfterDrain(context.Context, int64, int64) error {
	return nil
}

func (r *userAgentIdentityPlacementRepo) ConvertExternalPlacement(_ context.Context, input service.ConvertAccountExternalPlacementInput) (*service.ConvertAccountExternalPlacementResult, error) {
	if r.convertErr != nil && (r.convertErrFor == 0 || r.convertErrFor == input.AccountID) {
		return nil, r.convertErr
	}
	account, err := r.accountRepo.GetByID(context.Background(), input.AccountID)
	if err != nil {
		return nil, err
	}
	previous := account.ExternalPlacement
	if previous == nil {
		previous = &service.AccountExternalPlacement{Target: service.AccountExternalPlacementPrivate, State: "active"}
	}
	if err := r.accountRepo.BindGroups(context.Background(), account.ID, input.GroupIDs); err != nil {
		return nil, err
	}
	account.GroupIDs = append([]int64(nil), input.GroupIDs...)
	account.ShareMode = service.AccountShareModePrivate
	account.ShareStatus = service.AccountShareStatusApproved
	account.ExternalPlacement = &service.AccountExternalPlacement{
		Target:  service.AccountExternalPlacementPrivate,
		State:   "active",
		Version: previous.Version + 1,
	}
	if input.Target == service.AccountExternalPlacementPublicPool {
		account.ShareMode = service.AccountShareModePublic
		account.ExternalPlacement.Target = service.AccountExternalPlacementPublicPool
		account.ExternalPlacement.PublicGroupID = input.PublicGroupID
	}
	if err := r.accountRepo.Update(context.Background(), account); err != nil {
		return nil, err
	}
	return &service.ConvertAccountExternalPlacementResult{
		AccountID: account.ID,
		Previous:  previous,
		Current:   account.ExternalPlacement,
	}, nil
}

func (userAgentIdentityPrivateGroupProvisioner) ProvisionUserPrivateGroups(context.Context, int64) error {
	return nil
}

func (userAgentIdentityPrivateGroupProvisioner) GetActiveUserPrivateGroup(context.Context, int64, string) (*service.Group, error) {
	return &service.Group{
		ID:       userAgentIdentityPrivateGroupID,
		Name:     "OpenAI private",
		Platform: service.PlatformOpenAI,
		Status:   service.StatusActive,
	}, nil
}

type userAgentIdentityPublicGroupRepo struct {
	service.GroupRepository
}

func (userAgentIdentityPublicGroupRepo) ListActiveByPlatform(_ context.Context, platform string) ([]service.Group, error) {
	if platform != service.PlatformOpenAI {
		return nil, nil
	}
	return []service.Group{{
		ID:                   userAgentIdentityPublicGroupID,
		Name:                 "TEAM shared pool",
		Platform:             service.PlatformOpenAI,
		Status:               service.StatusActive,
		Scope:                service.GroupScopePublic,
		RequiredAccountLevel: service.AccountLevelTeam,
	}}, nil
}

func (userAgentIdentityPublicGroupRepo) IsModeGroup(context.Context, int64) (bool, error) {
	return false, nil
}

type userAgentIdentitySharePolicyRepo struct {
	service.AccountSharePolicyRepository
}

func (userAgentIdentitySharePolicyRepo) ResolveEnabledAccountSharePolicy(context.Context, int64, *int64, string, *int64) (*service.AccountSharePolicy, error) {
	return &service.AccountSharePolicy{ID: 1, Enabled: true, OwnerShareRatio: 0.7}, nil
}

type userAgentIdentityValidationUpstream struct {
	statusCode        int
	body              string
	calls             int
	lastAuthorization string
}

func (u *userAgentIdentityValidationUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.response(req), nil
}

func (u *userAgentIdentityValidationUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.response(req), nil
}

func (u *userAgentIdentityValidationUpstream) response(req *http.Request) *http.Response {
	u.calls++
	u.lastAuthorization = req.Header.Get("Authorization")
	statusCode := u.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	body := u.body
	if body == "" && statusCode == http.StatusOK {
		body = "data: {\"type\":\"response.completed\"}\n\n"
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type userAgentIdentityWSInvalidationRecorder struct {
	accountIDs []int64
}

func (r *userAgentIdentityWSInvalidationRecorder) InvalidateAgentIdentityWSConnections(accountID int64) {
	r.accountIDs = append(r.accountIDs, accountID)
}

func userAgentIdentityCredentials(t *testing.T, runtimeID, taskID string) map[string]any {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return map[string]any{
		"auth_mode":          service.OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":   runtimeID,
		"agent_private_key":  base64.StdEncoding.EncodeToString(der),
		"task_id":            taskID,
		"chatgpt_account_id": "team-handler-test",
		"chatgpt_user_id":    "member-handler-test",
		"plan_type":          service.AccountLevelTeam,
	}
}

func newUserAgentIdentityShareAccount(t *testing.T, ownerUserID int64, shareMode, shareStatus string) *service.Account {
	t.Helper()
	groupIDs := []int64{userAgentIdentityPrivateGroupID}
	if shareMode == service.AccountShareModePublic && shareStatus == service.AccountShareStatusApproved {
		groupIDs = append(groupIDs, userAgentIdentityPublicGroupID)
	}
	account := &service.Account{
		ID:           1,
		Name:         "Agent Identity",
		OwnerUserID:  &ownerUserID,
		Platform:     service.PlatformOpenAI,
		Type:         service.AccountTypeOAuth,
		AccountLevel: service.AccountLevelTeam,
		Credentials:  userAgentIdentityCredentials(t, "runtime-old", "task-old"),
		Extra:        map[string]any{},
		ShareMode:    shareMode,
		ShareStatus:  shareStatus,
		Concurrency:  3,
		Priority:     5,
		Status:       service.StatusActive,
		Schedulable:  true,
		GroupIDs:     groupIDs,
	}
	if shareMode == service.AccountShareModePublic && shareStatus == service.AccountShareStatusApproved {
		publicGroupID := userAgentIdentityPublicGroupID
		account.ExternalPlacement = &service.AccountExternalPlacement{
			Target:        service.AccountExternalPlacementPublicPool,
			PublicGroupID: &publicGroupID,
			State:         "active",
			Version:       1,
		}
	}
	return account
}

func newUserAgentIdentityShareHandler(
	t *testing.T,
	account *service.Account,
	upstreamStatus int,
	upstreamBody string,
) (*UserAccountHandler, *userAgentIdentityShareRepo, *userAgentIdentityValidationUpstream, *userAgentIdentityWSInvalidationRecorder, *userAgentIdentityPlacementRepo) {
	t.Helper()
	repo := &userAgentIdentityShareRepo{
		accounts: map[int64]*service.Account{account.ID: cloneUserAgentIdentityShareAccount(account)},
	}
	invalidator := &userAgentIdentityWSInvalidationRecorder{}
	invalidatorProxy := service.NewAgentIdentityWSInvalidatorProxy()
	invalidatorProxy.SetTarget(invalidator)
	accountService := service.NewAccountService(repo, userAgentIdentityPublicGroupRepo{}, nil, nil, nil)
	accountService.SetUserPrivateGroupProvisioner(userAgentIdentityPrivateGroupProvisioner{})
	accountService.SetAccountSharePolicyRepository(userAgentIdentitySharePolicyRepo{})
	placementRepo := &userAgentIdentityPlacementRepo{accountRepo: repo}
	accountService.SetAccountShareModeRepository(placementRepo)
	accountService.SetAgentIdentityWSInvalidator(invalidatorProxy)
	upstream := &userAgentIdentityValidationUpstream{statusCode: upstreamStatus, body: upstreamBody}
	accountTestService := service.NewAccountTestService(repo, nil, nil, nil, upstream, nil, nil, nil, invalidatorProxy)
	handler := NewUserAccountHandler(accountService, nil, accountTestService, nil, nil, nil, nil, nil, nil, nil)
	return handler, repo, upstream, invalidator, placementRepo
}

func runUserAgentIdentityUpdateRequest(t *testing.T, handler *UserAccountHandler, ownerUserID int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	router := gin.New()
	router.PUT("/accounts/:id", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: ownerUserID})
		handler.Update(c)
	})
	request := httptest.NewRequest(http.MethodPut, "/accounts/1", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestIsOpenAIUsageLimitReachedValidationError(t *testing.T) {
	require.True(t, isOpenAIUsageLimitReachedValidationError(`API returned 429: {"error":{"type":"usage_limit_reached","message":"The usage limit has been reached"}}`))
	require.True(t, isOpenAIUsageLimitReachedValidationError(`API returned 429: {"error": {"type": "usage_limit_reached"}}`))
	require.False(t, isOpenAIUsageLimitReachedValidationError(`API returned 429: {"error":{"type":"rate_limit_exceeded"}}`))
	require.False(t, isOpenAIUsageLimitReachedValidationError(`API returned 401: {"error":{"type":"usage_limit_reached"}}`))
	require.False(t, isOpenAIUsageLimitReachedValidationError(`Request failed: dial tcp timeout`))
}

func TestUserAccountHandlerUpdateAgentIdentityPrivateToPublicRequiresPlacementConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)

	account := newUserAgentIdentityShareAccount(t, ownerUserID, service.AccountShareModePrivate, service.AccountShareStatusApproved)
	handler, repo, upstream, _, _ := newUserAgentIdentityShareHandler(t, account, http.StatusOK, "")

	recorder := runUserAgentIdentityUpdateRequest(t, handler, ownerUserID, map[string]any{"share_mode": service.AccountShareModePublic})

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "OWNED_ACCOUNT_PLACEMENT_CONVERSION_REQUIRED")
	require.Zero(t, upstream.calls)
	stored := repo.accounts[account.ID]
	require.Equal(t, service.AccountShareModePrivate, stored.ShareMode)
	require.Equal(t, service.AccountShareStatusApproved, stored.ShareStatus)
}

func TestUserAccountHandlerUpdateApprovedPublicAgentIdentityCredentialsRevalidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)
	account := newUserAgentIdentityShareAccount(t, ownerUserID, service.AccountShareModePublic, service.AccountShareStatusApproved)
	handler, repo, upstream, invalidator, _ := newUserAgentIdentityShareHandler(t, account, http.StatusOK, "")
	newCredentials := userAgentIdentityCredentials(t, "runtime-new", "task-new")

	recorder := runUserAgentIdentityUpdateRequest(t, handler, ownerUserID, map[string]any{"credentials": newCredentials})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, upstream.calls, "changed public credentials must trigger a fresh connection test")
	require.Equal(t, []int64{account.ID}, invalidator.accountIDs)
	stored := repo.accounts[account.ID]
	require.Equal(t, "runtime-new", stored.GetCredential("agent_runtime_id"))
	require.Equal(t, service.AccountShareModePublic, stored.ShareMode)
	require.Equal(t, service.AccountShareStatusApproved, stored.ShareStatus)
	require.Equal(t, []int64{userAgentIdentityPrivateGroupID, userAgentIdentityPublicGroupID}, stored.GroupIDs)
}

func TestUserAccountHandlerSetPublicShareExecutorValidatesAgentIdentityBeforeApproval(t *testing.T) {
	ownerUserID := int64(101)
	account := newUserAgentIdentityShareAccount(t, ownerUserID, service.AccountShareModePrivate, service.AccountShareStatusApproved)
	handler, repo, upstream, _, _ := newUserAgentIdentityShareHandler(t, account, http.StatusOK, "")
	task := &service.AccountBatchTask{
		Operation:   service.AccountBatchTaskOperationUserSetPublicShare,
		OwnerUserID: &ownerUserID,
	}

	result, err := handler.executeUserSetPublicShareTaskItem(context.Background(), task, service.AccountBatchTaskItem{AccountID: account.ID})

	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls, "the async executor must not bypass public-share validation")
	require.Equal(t, service.AccountShareModePublic, result["share_mode"])
	require.Equal(t, service.AccountShareStatusApproved, result["share_status"])
	stored := repo.accounts[account.ID]
	require.Equal(t, service.AccountShareStatusApproved, stored.ShareStatus)
	require.Equal(t, []int64{userAgentIdentityPrivateGroupID, userAgentIdentityPublicGroupID}, stored.GroupIDs)
}

func TestUserAccountHandlerConvertExternalPlacementBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)
	first := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	second := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	second.ID = 2
	second.Name = "Agent Identity 2"

	handler, repo, _, _, _ := newUserAgentIdentityShareHandler(t, first, http.StatusOK, "")
	repo.accounts[second.ID] = cloneUserAgentIdentityShareAccount(second)

	router := gin.New()
	router.POST("/accounts/external-placement:convert-batch", func(c *gin.Context) {
		c.Set(
			string(middleware2.ContextKeyUser),
			middleware2.AuthSubject{UserID: ownerUserID},
		)
		handler.ConvertExternalPlacementBatch(c)
	})
	body := []byte(`{
		"account_ids":[2,1,2],
		"target":"public_pool",
		"idempotency_key":"batch-placement-test"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/external-placement:convert-batch",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Success    int     `json:"success"`
			Failed     int     `json:"failed"`
			SuccessIDs []int64 `json:"success_ids"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, 2, envelope.Data.Success)
	require.Zero(t, envelope.Data.Failed)
	require.Equal(t, []int64{1, 2}, envelope.Data.SuccessIDs)
	for _, accountID := range []int64{1, 2} {
		stored := repo.accounts[accountID]
		require.Equal(t, service.AccountShareModePublic, stored.ShareMode)
		require.NotNil(t, stored.ExternalPlacement)
		require.Equal(
			t,
			service.AccountExternalPlacementPublicPool,
			stored.ExternalPlacement.Target,
		)
	}
}

// 批量转换部分失败时，失败项必须透出结构化错误码（reason）与用户可读 message，
// 前端才能按 reason 映射中文文案（此前只返回 error 英文原文，批量场景根本读不到原因）。
func TestUserAccountHandlerConvertExternalPlacementBatchExposesFailureReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)
	first := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	second := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	second.ID = 2
	second.Name = "Agent Identity 2"

	handler, repo, _, _, placementRepo := newUserAgentIdentityShareHandler(t, first, http.StatusOK, "")
	repo.accounts[second.ID] = cloneUserAgentIdentityShareAccount(second)
	// 账号 2 转换失败，注入一个带 reason 的结构化错误。
	placementRepo.convertErr = infraerrors.BadRequest(
		"OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED",
		"public account validation failed",
	)
	placementRepo.convertErrFor = 2

	router := gin.New()
	router.POST("/accounts/external-placement:convert-batch", func(c *gin.Context) {
		c.Set(
			string(middleware2.ContextKeyUser),
			middleware2.AuthSubject{UserID: ownerUserID},
		)
		handler.ConvertExternalPlacementBatch(c)
	})
	body := []byte(`{
		"account_ids":[1,2],
		"target":"public_pool",
		"idempotency_key":"batch-placement-reason-test"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/external-placement:convert-batch",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Success    int     `json:"success"`
			Failed     int     `json:"failed"`
			SuccessIDs []int64 `json:"success_ids"`
			FailedIDs  []int64 `json:"failed_ids"`
			Results    []struct {
				AccountID int64  `json:"account_id"`
				Success   bool   `json:"success"`
				Error     string `json:"error"`
				Reason    string `json:"reason"`
				Message   string `json:"message"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, 1, envelope.Data.Success)
	require.Equal(t, 1, envelope.Data.Failed)
	require.Equal(t, []int64{1}, envelope.Data.SuccessIDs)
	require.Equal(t, []int64{2}, envelope.Data.FailedIDs)

	var failed *struct {
		AccountID int64  `json:"account_id"`
		Success   bool   `json:"success"`
		Error     string `json:"error"`
		Reason    string `json:"reason"`
		Message   string `json:"message"`
	}
	for i := range envelope.Data.Results {
		if !envelope.Data.Results[i].Success {
			failed = &envelope.Data.Results[i]
			break
		}
	}
	require.NotNil(t, failed, "batch results must include a failed entry")
	require.Equal(t, int64(2), failed.AccountID)
	require.Equal(t, "OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED", failed.Reason)
	require.Equal(t, "public account validation failed", failed.Message)
	require.Contains(t, failed.Error, "OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED")
	// 成功的账号仍完成转换。
	require.Equal(t, service.AccountShareModePublic, repo.accounts[1].ShareMode)
	require.Equal(t, service.AccountShareModePrivate, repo.accounts[2].ShareMode)
}

// 非 ApplicationError（裸 DB/Redis 错误）时 message 不能是固定的 "internal error"——
// 那会遮蔽 err.Error() 里的真实原因。reason 为空时 message 必须回退到真实错误文本。
func TestUserAccountHandlerConvertExternalPlacementBatchExposesRawErrorMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)
	first := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	second := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	second.ID = 2
	second.Name = "Agent Identity 2"

	handler, repo, _, _, placementRepo := newUserAgentIdentityShareHandler(t, first, http.StatusOK, "")
	repo.accounts[second.ID] = cloneUserAgentIdentityShareAccount(second)
	// 注入裸错误（非 infraerrors.ApplicationError），模拟 repo 层 DB 抖动。
	placementRepo.convertErr = errors.New("relation account_groups does not exist")
	placementRepo.convertErrFor = 2

	router := gin.New()
	router.POST("/accounts/external-placement:convert-batch", func(c *gin.Context) {
		c.Set(
			string(middleware2.ContextKeyUser),
			middleware2.AuthSubject{UserID: ownerUserID},
		)
		handler.ConvertExternalPlacementBatch(c)
	})
	body := []byte(`{
		"account_ids":[1,2],
		"target":"public_pool",
		"idempotency_key":"batch-placement-raw-error-test"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/external-placement:convert-batch",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Failed  int `json:"failed"`
			Results []struct {
				AccountID int64  `json:"account_id"`
				Success   bool   `json:"success"`
				Reason    string `json:"reason"`
				Message   string `json:"message"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 1, envelope.Data.Failed)
	var failed *struct {
		AccountID int64  `json:"account_id"`
		Success   bool   `json:"success"`
		Reason    string `json:"reason"`
		Message   string `json:"message"`
	}
	for i := range envelope.Data.Results {
		if !envelope.Data.Results[i].Success {
			failed = &envelope.Data.Results[i]
			break
		}
	}
	require.NotNil(t, failed)
	require.Equal(t, int64(2), failed.AccountID)
	require.Empty(t, failed.Reason)
	require.Contains(t, failed.Message, "relation account_groups does not exist")
}

func TestUserAccountHandlerConvertExternalPlacementBatchRejectsForeignAccountBeforeChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ownerUserID := int64(101)
	foreignOwnerUserID := int64(202)
	owned := newUserAgentIdentityShareAccount(
		t,
		ownerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	foreign := newUserAgentIdentityShareAccount(
		t,
		foreignOwnerUserID,
		service.AccountShareModePrivate,
		service.AccountShareStatusApproved,
	)
	foreign.ID = 2

	handler, repo, _, _, _ := newUserAgentIdentityShareHandler(t, owned, http.StatusOK, "")
	repo.accounts[foreign.ID] = cloneUserAgentIdentityShareAccount(foreign)

	router := gin.New()
	router.POST("/accounts/external-placement:convert-batch", func(c *gin.Context) {
		c.Set(
			string(middleware2.ContextKeyUser),
			middleware2.AuthSubject{UserID: ownerUserID},
		)
		handler.ConvertExternalPlacementBatch(c)
	})
	body := []byte(`{
		"account_ids":[1,2],
		"target":"public_pool",
		"idempotency_key":"batch-placement-foreign-test"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/accounts/external-placement:convert-batch",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
	require.Equal(t, service.AccountShareModePrivate, repo.accounts[owned.ID].ShareMode)
	require.Nil(t, repo.accounts[owned.ID].ExternalPlacement)
	require.Equal(t, service.AccountShareModePrivate, repo.accounts[foreign.ID].ShareMode)
	require.Nil(t, repo.accounts[foreign.ID].ExternalPlacement)
}

func (userAgentIdentityPublicGroupRepo) ListActiveByScope(context.Context, string) ([]service.Group, error) {
	return nil, nil
}

func (userAgentIdentityPublicGroupRepo) ListActiveByPlatformAndScope(context.Context, string, string) ([]service.Group, error) {
	return nil, nil
}
