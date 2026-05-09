package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type UserAccountHandler struct {
	accountService          *service.AccountService
	accountUsageService     *service.AccountUsageService
	accountTestService      *service.AccountTestService
	oauthService            *service.OAuthService
	openaiOAuthService      *service.OpenAIOAuthService
	geminiOAuthService      *service.GeminiOAuthService
	antigravityOAuthService *service.AntigravityOAuthService
}

func NewUserAccountHandler(
	accountService *service.AccountService,
	accountUsageService *service.AccountUsageService,
	accountTestService *service.AccountTestService,
	oauthService *service.OAuthService,
	openaiOAuthService *service.OpenAIOAuthService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
) *UserAccountHandler {
	return &UserAccountHandler{
		accountService:          accountService,
		accountUsageService:     accountUsageService,
		accountTestService:      accountTestService,
		oauthService:            oauthService,
		openaiOAuthService:      openaiOAuthService,
		geminiOAuthService:      geminiOAuthService,
		antigravityOAuthService: antigravityOAuthService,
	}
}

type createUserAccountRequest struct {
	Name               string         `json:"name" binding:"required"`
	Notes              *string        `json:"notes"`
	Platform           string         `json:"platform" binding:"required"`
	AccountLevel       string         `json:"account_level" binding:"omitempty,oneof=unknown free plus pro team"`
	Type               string         `json:"type" binding:"required,oneof=oauth"`
	Credentials        map[string]any `json:"credentials" binding:"required"`
	Extra              map[string]any `json:"extra"`
	ShareMode          string         `json:"share_mode" binding:"omitempty,oneof=private public"`
	ProxyID            *int64         `json:"proxy_id"`
	Concurrency        int            `json:"concurrency"`
	LoadFactor         *int           `json:"load_factor"`
	Priority           int            `json:"priority"`
	GroupIDs           []int64        `json:"group_ids"`
	ExpiresAt          *int64         `json:"expires_at"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired"`
}

type importUserAccountCredentialsRequest struct {
	Contents           []string `json:"contents" binding:"required"`
	ShareMode          string   `json:"share_mode" binding:"omitempty,oneof=private public"`
	Concurrency        int      `json:"concurrency"`
	LoadFactor         *int     `json:"load_factor"`
	Priority           int      `json:"priority"`
	GroupIDs           []int64  `json:"group_ids"`
	ExpiresAt          *int64   `json:"expires_at"`
	AutoPauseOnExpired *bool    `json:"auto_pause_on_expired"`
}

type updateUserAccountRequest struct {
	Name               *string         `json:"name"`
	Notes              *string         `json:"notes"`
	AccountLevel       *string         `json:"account_level" binding:"omitempty,oneof=unknown free plus pro team"`
	Credentials        *map[string]any `json:"credentials"`
	Extra              *map[string]any `json:"extra"`
	ShareMode          *string         `json:"share_mode" binding:"omitempty,oneof=private public"`
	ProxyID            *int64          `json:"proxy_id"`
	Concurrency        *int            `json:"concurrency"`
	LoadFactor         *int            `json:"load_factor"`
	Priority           *int            `json:"priority"`
	Status             *string         `json:"status" binding:"omitempty,oneof=active disabled inactive"`
	Schedulable        *bool           `json:"schedulable"`
	GroupIDs           *[]int64        `json:"group_ids"`
	ExpiresAt          *int64          `json:"expires_at"`
	AutoPauseOnExpired *bool           `json:"auto_pause_on_expired"`
}

type bulkUpdateUserAccountsRequest struct {
	AccountIDs     []int64        `json:"account_ids"`
	ProxyID        *int64         `json:"proxy_id"`
	Concurrency    *int           `json:"concurrency"`
	LoadFactor     *int           `json:"load_factor"`
	Priority       *int           `json:"priority"`
	RateMultiplier *float64       `json:"rate_multiplier"`
	Status         string         `json:"status" binding:"omitempty,oneof=active disabled inactive"`
	Schedulable    *bool          `json:"schedulable"`
	AccountLevel   *string        `json:"account_level" binding:"omitempty,oneof=unknown free plus pro team"`
	ShareMode      *string        `json:"share_mode" binding:"omitempty,oneof=private public"`
	GroupIDs       *[]int64       `json:"group_ids"`
	Credentials    map[string]any `json:"credentials"`
	Extra          map[string]any `json:"extra"`
}

type bulkDeleteUserAccountsRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

type userOAuthProxyRequest struct {
	ProxyID *int64 `json:"proxy_id"`
}

type userExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
}

type userOpenAIGenerateAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

type userOpenAIExchangeCodeRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	State       string `json:"state" binding:"required"`
	RedirectURI string `json:"redirect_uri"`
	ProxyID     *int64 `json:"proxy_id"`
}

type userGeminiGenerateAuthURLRequest struct {
	ProxyID   *int64 `json:"proxy_id"`
	ProjectID string `json:"project_id"`
	OAuthType string `json:"oauth_type"`
	TierID    string `json:"tier_id"`
}

type userGeminiExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	Code      string `json:"code" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
	OAuthType string `json:"oauth_type"`
	TierID    string `json:"tier_id"`
}

type userAntigravityGenerateAuthURLRequest struct {
	ProxyID *int64 `json:"proxy_id"`
}

type userAntigravityExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	Code      string `json:"code" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
}

type userBatchTodayStatsRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required"`
}

const userPublicShareValidationTimeout = 30 * time.Second

func bindOptionalJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		response.BadRequest(c, "Invalid request: "+err.Error())
		return false
	}
	return true
}

func requireUserAccountAuth(c *gin.Context) bool {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return false
	}
	return true
}

func rejectUserProxyID(c *gin.Context, proxyID *int64) bool {
	if proxyID == nil {
		return true
	}
	response.BadRequest(c, "proxy_id is not allowed for user accounts")
	return false
}

func rejectUserManualCredentialAuth(c *gin.Context) {
	response.BadRequest(c, "manual credential account creation is not allowed for user accounts; use official OAuth or import OAuth credentials")
}

func userUnixSecondsToTime(value *int64) *time.Time {
	if value == nil || *value <= 0 {
		return nil
	}
	t := time.Unix(*value, 0).UTC()
	return &t
}

func normalizeUserAccountIDList(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}

	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func removeInt64(values []int64, target int64) []int64 {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func normalizeUserAccountStatus(status *string) *string {
	if status == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*status))
	if normalized == "inactive" {
		normalized = service.StatusDisabled
	}
	return &normalized
}

func publicShareValidationErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) && strings.TrimSpace(appErr.Message) != "" {
		return strings.TrimSpace(appErr.Message)
	}
	return strings.TrimSpace(err.Error())
}

func isOpenAIUsageLimitReachedValidationError(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" || !strings.Contains(normalized, "usage_limit_reached") {
		return false
	}
	return strings.Contains(normalized, "api returned 429")
}

func (h *UserAccountHandler) activateOwnedPublicShareIfRequested(ctx context.Context, ownerUserID int64, account *service.Account) (*service.Account, error) {
	if account == nil || service.NormalizeAccountShareMode(account.ShareMode) != service.AccountShareModePublic {
		return account, nil
	}
	if service.NormalizeAccountShareStatus(account.ShareStatus) == service.AccountShareStatusApproved {
		return account, nil
	}

	reason := ""
	allowRateLimitedApproval := false
	if h.accountTestService == nil {
		reason = "account test service is unavailable"
	} else {
		testCtx, cancel := context.WithTimeout(ctx, userPublicShareValidationTimeout)
		defer cancel()
		result, err := h.accountTestService.RunTestBackground(testCtx, account.ID, "")
		switch {
		case err != nil:
			reason = publicShareValidationErrorMessage(err)
		case result == nil:
			reason = "account test did not return a result"
		case strings.TrimSpace(result.Status) != "success":
			reason = strings.TrimSpace(result.ErrorMessage)
			if reason == "" {
				reason = "account test failed"
			}
		}
	}
	if isOpenAIUsageLimitReachedValidationError(reason) {
		reason = ""
		allowRateLimitedApproval = true
	}
	if reason != "" {
		return h.accountService.MarkOwnedPublicSharePending(ctx, ownerUserID, account.ID, reason)
	}

	approved, err := h.accountService.ApproveOwnedPublicShareWithOptions(ctx, ownerUserID, account.ID, service.OwnedPublicShareApprovalOptions{
		AllowRateLimited: allowRateLimitedApproval,
	})
	if err != nil {
		return h.accountService.MarkOwnedPublicSharePending(ctx, ownerUserID, account.ID, publicShareValidationErrorMessage(err))
	}
	return approved, nil
}

func (h *UserAccountHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}
	filters := service.AccountListFilters{
		Platform:    strings.TrimSpace(c.Query("platform")),
		AccountType: strings.TrimSpace(c.Query("type")),
		Status:      strings.TrimSpace(c.Query("status")),
		Search:      strings.TrimSpace(c.Query("search")),
		PrivacyMode: strings.TrimSpace(c.Query("privacy_mode")),
	}
	if groupIDStr := strings.TrimSpace(c.Query("group_id")); groupIDStr != "" {
		groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filters.GroupID = groupID
	}

	accounts, result, err := h.accountService.ListOwned(c.Request.Context(), subject.UserID, params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.Account, 0, len(accounts))
	for i := range accounts {
		out = append(out, *dto.AccountFromService(&accounts[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

func (h *UserAccountHandler) GetQuotaPoolDashboard(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	dashboard, err := h.accountService.GetQuotaPoolDashboard(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dashboard)
}

func (h *UserAccountHandler) GetByID(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *UserAccountHandler) GetUsage(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if _, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	source := c.DefaultQuery("source", "active")
	var usage *service.UsageInfo
	if source == "passive" {
		usage, err = h.accountUsageService.GetPassiveUsage(c.Request.Context(), accountID)
	} else {
		usage, err = h.accountUsageService.GetUsage(c.Request.Context(), accountID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usage)
}

func (h *UserAccountHandler) GetTodayStats(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if _, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	stats, err := h.accountUsageService.GetTodayStats(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
}

func (h *UserAccountHandler) GetBatchTodayStats(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req userBatchTodayStatsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.Success(c, gin.H{"stats": map[string]any{}})
		return
	}

	for _, accountID := range accountIDs {
		if _, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	stats, err := h.accountUsageService.GetTodayStatsBatch(c.Request.Context(), accountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"stats": stats})
}

func (h *UserAccountHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createUserAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = service.DefaultOAuthAccountConcurrencyForPlatform(req.Platform)
	}
	if req.Priority <= 0 {
		req.Priority = 50
	}

	executeUserIdempotentJSON(c, "user.accounts.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		account, err := h.accountService.CreateOwned(ctx, subject.UserID, service.CreateAccountRequest{
			Name:               req.Name,
			Notes:              req.Notes,
			Platform:           req.Platform,
			AccountLevel:       req.AccountLevel,
			Type:               req.Type,
			Credentials:        req.Credentials,
			Extra:              req.Extra,
			ShareMode:          req.ShareMode,
			ProxyID:            nil,
			Concurrency:        req.Concurrency,
			LoadFactor:         req.LoadFactor,
			Priority:           req.Priority,
			GroupIDs:           req.GroupIDs,
			ExpiresAt:          userUnixSecondsToTime(req.ExpiresAt),
			AutoPauseOnExpired: req.AutoPauseOnExpired,
		})
		if err != nil {
			return nil, err
		}
		account, err = h.activateOwnedPublicShareIfRequested(ctx, subject.UserID, account)
		if err != nil {
			return nil, err
		}
		return dto.AccountFromService(account), nil
	})
}

func (h *UserAccountHandler) Import(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createUserAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = service.DefaultOAuthAccountConcurrencyForPlatform(req.Platform)
	}
	if req.Priority <= 0 {
		req.Priority = 50
	}

	executeUserIdempotentJSON(c, "user.accounts.import", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		account, err := h.accountService.ImportOwned(ctx, subject.UserID, service.CreateAccountRequest{
			Name:               req.Name,
			Notes:              req.Notes,
			Platform:           req.Platform,
			AccountLevel:       req.AccountLevel,
			Type:               req.Type,
			Credentials:        req.Credentials,
			Extra:              req.Extra,
			ShareMode:          req.ShareMode,
			ProxyID:            nil,
			Concurrency:        req.Concurrency,
			LoadFactor:         req.LoadFactor,
			Priority:           req.Priority,
			GroupIDs:           req.GroupIDs,
			ExpiresAt:          userUnixSecondsToTime(req.ExpiresAt),
			AutoPauseOnExpired: req.AutoPauseOnExpired,
		})
		if err != nil {
			return nil, err
		}
		account, err = h.activateOwnedPublicShareIfRequested(ctx, subject.UserID, account)
		if err != nil {
			return nil, err
		}
		return dto.AccountFromService(account), nil
	})
}

func (h *UserAccountHandler) ImportCredentials(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req importUserAccountCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.Priority <= 0 {
		req.Priority = 50
	}

	sources, parseErrors := service.ParseAccountCredentialImportContents(req.Contents)
	if len(sources) == 0 && len(parseErrors) == 0 {
		response.BadRequest(c, "No importable account credentials found")
		return
	}
	if len(sources) > service.MaxAccountCredentialImportItems {
		response.BadRequest(c, fmt.Sprintf("Too many import items; maximum is %d", service.MaxAccountCredentialImportItems))
		return
	}

	result := service.AccountCredentialImportResult{
		Total:  len(sources) + len(parseErrors),
		Errors: []service.AccountCredentialImportError{},
	}
	result.Errors = append(result.Errors, parseErrors...)

	for idx, source := range sources {
		account, err := h.createOwnedAccountFromCredentialImportSource(c.Request.Context(), subject.UserID, source, req, idx+1)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, service.AccountCredentialImportError{
				Index:   len(parseErrors) + idx + 1,
				Kind:    string(source.Kind),
				Name:    source.Name,
				Message: err.Error(),
			})
			continue
		}
		if account != nil {
			result.Created++
		}
	}
	result.Failed += len(parseErrors)
	response.Success(c, result)
}

func (h *UserAccountHandler) createOwnedAccountFromCredentialImportSource(
	ctx context.Context,
	ownerUserID int64,
	source service.AccountCredentialImportSource,
	defaults importUserAccountCredentialsRequest,
	sequence int,
) (*service.Account, error) {
	req := service.CreateAccountRequest{
		Name:               strings.TrimSpace(source.Name),
		Notes:              source.Notes,
		Platform:           source.Platform,
		Type:               service.AccountTypeOAuth,
		Credentials:        source.Credentials,
		Extra:              source.Extra,
		ShareMode:          defaults.ShareMode,
		ProxyID:            nil,
		Concurrency:        defaults.Concurrency,
		LoadFactor:         defaults.LoadFactor,
		Priority:           defaults.Priority,
		GroupIDs:           defaults.GroupIDs,
		ExpiresAt:          userUnixSecondsToTime(defaults.ExpiresAt),
		AutoPauseOnExpired: defaults.AutoPauseOnExpired,
	}
	if req.Concurrency <= 0 {
		req.Concurrency = service.DefaultOAuthAccountConcurrencyForPlatform(req.Platform)
	}

	switch source.Kind {
	case service.AccountCredentialImportKindOAuthCredentials:
		if req.Name == "" {
			req.Name = service.DeriveAccountCredentialImportName(req.Platform, req.Credentials, req.Extra, sequence)
		}
	case service.AccountCredentialImportKindOpenAIRefreshToken:
		tokenInfo, err := h.openaiOAuthService.RefreshTokenWithClientID(ctx, source.Token, "", source.ClientID)
		if err != nil {
			return nil, fmt.Errorf("validate OpenAI refresh token: %w", err)
		}
		req.Platform = service.PlatformOpenAI
		req.Credentials = h.openaiOAuthService.BuildAccountCredentials(tokenInfo)
		req.Extra = service.BuildOpenAIAccountCredentialImportExtra(tokenInfo)
		if defaults.Concurrency <= 0 {
			req.Concurrency = service.DefaultOAuthAccountConcurrencyForPlatform(req.Platform)
		}
		if req.Name == "" {
			req.Name = strings.TrimSpace(tokenInfo.Email)
		}
		if req.Name == "" {
			req.Name = fmt.Sprintf("OpenAI OAuth Account #%d", sequence)
		}
	case service.AccountCredentialImportKindClaudeSessionKey:
		tokenInfo, err := h.oauthService.CookieAuth(ctx, &service.CookieAuthInput{
			SessionKey: source.Token,
			ProxyID:    nil,
			Scope:      "full",
		})
		if err != nil {
			return nil, fmt.Errorf("exchange Claude session key: %w", err)
		}
		req.Platform = service.PlatformAnthropic
		req.Credentials = service.BuildClaudeAccountCredentials(tokenInfo)
		req.Extra = service.BuildClaudeAccountCredentialImportExtra(tokenInfo)
		if defaults.Concurrency <= 0 {
			req.Concurrency = service.DefaultOAuthAccountConcurrencyForPlatform(req.Platform)
		}
		if req.Name == "" {
			req.Name = strings.TrimSpace(tokenInfo.EmailAddress)
		}
		if req.Name == "" {
			req.Name = fmt.Sprintf("Claude OAuth Account #%d", sequence)
		}
	default:
		return nil, fmt.Errorf("unsupported credential import kind")
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("account name is required")
	}
	account, err := h.accountService.ImportOwned(ctx, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	return h.activateOwnedPublicShareIfRequested(ctx, ownerUserID, account)
}

func (h *UserAccountHandler) Update(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req updateUserAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	status := normalizeUserAccountStatus(req.Status)
	account, err := h.accountService.UpdateOwned(c.Request.Context(), subject.UserID, accountID, service.UpdateAccountRequest{
		Name:               req.Name,
		Notes:              req.Notes,
		AccountLevel:       req.AccountLevel,
		Credentials:        req.Credentials,
		Extra:              req.Extra,
		ShareMode:          req.ShareMode,
		ProxyID:            nil,
		Concurrency:        req.Concurrency,
		LoadFactor:         req.LoadFactor,
		Priority:           req.Priority,
		Status:             status,
		Schedulable:        req.Schedulable,
		GroupIDs:           req.GroupIDs,
		ExpiresAt:          userUnixSecondsToTime(req.ExpiresAt),
		ClearExpiresAt:     req.ExpiresAt != nil && *req.ExpiresAt <= 0,
		AutoPauseOnExpired: req.AutoPauseOnExpired,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.ShareMode != nil && service.NormalizeAccountShareMode(*req.ShareMode) == service.AccountShareModePublic {
		account, err = h.activateOwnedPublicShareIfRequested(c.Request.Context(), subject.UserID, account)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *UserAccountHandler) BulkUpdate(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req bulkUpdateUserAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	if req.RateMultiplier != nil {
		response.BadRequest(c, "rate_multiplier is not allowed for user accounts")
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "inactive" {
		status = service.StatusDisabled
	}
	if req.Concurrency != nil && *req.Concurrency <= 0 {
		response.BadRequest(c, "concurrency must be > 0")
		return
	}
	if req.Priority != nil && *req.Priority <= 0 {
		response.BadRequest(c, "priority must be > 0")
		return
	}
	if req.LoadFactor != nil && *req.LoadFactor > 10000 {
		response.BadRequest(c, "load_factor must be <= 10000")
		return
	}

	hasUpdates := req.Concurrency != nil ||
		req.LoadFactor != nil ||
		req.Priority != nil ||
		status != "" ||
		req.Schedulable != nil ||
		req.AccountLevel != nil ||
		req.ShareMode != nil ||
		req.GroupIDs != nil ||
		len(req.Credentials) > 0 ||
		len(req.Extra) > 0
	if !hasUpdates {
		response.BadRequest(c, "No updates provided")
		return
	}

	result, err := h.accountService.BulkUpdateOwned(c.Request.Context(), subject.UserID, &service.BulkUpdateOwnedAccountsInput{
		AccountIDs:   accountIDs,
		Concurrency:  req.Concurrency,
		LoadFactor:   req.LoadFactor,
		Priority:     req.Priority,
		Status:       status,
		Schedulable:  req.Schedulable,
		AccountLevel: req.AccountLevel,
		ShareMode:    req.ShareMode,
		GroupIDs:     req.GroupIDs,
		Credentials:  req.Credentials,
		Extra:        req.Extra,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.ShareMode != nil && service.NormalizeAccountShareMode(*req.ShareMode) == service.AccountShareModePublic {
		for i := range result.Results {
			entry := &result.Results[i]
			if !entry.Success {
				continue
			}
			account, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, entry.AccountID)
			if err == nil {
				_, err = h.activateOwnedPublicShareIfRequested(c.Request.Context(), subject.UserID, account)
			}
			if err != nil {
				entry.Success = false
				entry.Error = err.Error()
				result.Success--
				result.Failed++
				result.SuccessIDs = removeInt64(result.SuccessIDs, entry.AccountID)
				result.FailedIDs = append(result.FailedIDs, entry.AccountID)
			}
		}
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if err := h.accountService.DeleteOwned(c.Request.Context(), subject.UserID, accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Account deleted successfully"})
}

func (h *UserAccountHandler) BulkDelete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req bulkDeleteUserAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	result, err := h.accountService.BulkDeleteOwned(c.Request.Context(), subject.UserID, accountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) GenerateAnthropicOAuthURL(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userOAuthProxyRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	result, err := h.oauthService.GenerateAuthURL(c.Request.Context(), nil)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) GenerateAnthropicSetupTokenURL(c *gin.Context) {
	rejectUserManualCredentialAuth(c)
}

func (h *UserAccountHandler) ExchangeAnthropicOAuthCode(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	tokenInfo, err := h.oauthService.ExchangeCode(c.Request.Context(), &service.ExchangeCodeInput{
		SessionID: req.SessionID,
		Code:      req.Code,
		ProxyID:   nil,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tokenInfo)
}

func (h *UserAccountHandler) ExchangeAnthropicSetupTokenCode(c *gin.Context) {
	rejectUserManualCredentialAuth(c)
}

func (h *UserAccountHandler) AnthropicCookieAuth(c *gin.Context) {
	rejectUserManualCredentialAuth(c)
}

func (h *UserAccountHandler) AnthropicSetupTokenCookieAuth(c *gin.Context) {
	rejectUserManualCredentialAuth(c)
}

func (h *UserAccountHandler) GenerateOpenAIOAuthURL(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userOpenAIGenerateAuthURLRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	result, err := h.openaiOAuthService.GenerateAuthURL(
		c.Request.Context(),
		nil,
		req.RedirectURI,
		service.PlatformOpenAI,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) ExchangeOpenAIOAuthCode(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userOpenAIExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	tokenInfo, err := h.openaiOAuthService.ExchangeCode(c.Request.Context(), &service.OpenAIExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     nil,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tokenInfo)
}

func (h *UserAccountHandler) RefreshOpenAIToken(c *gin.Context) {
	rejectUserManualCredentialAuth(c)
}

func (h *UserAccountHandler) GetGeminiOAuthCapabilities(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	response.Success(c, h.geminiOAuthService.GetOAuthConfig())
}

func (h *UserAccountHandler) GenerateGeminiOAuthURL(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userGeminiGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}

	oauthType := strings.TrimSpace(req.OAuthType)
	if oauthType == "" {
		oauthType = "code_assist"
	}
	if oauthType != "code_assist" && oauthType != "google_one" && oauthType != "ai_studio" {
		response.BadRequest(c, "Invalid oauth_type: must be 'code_assist', 'google_one', or 'ai_studio'")
		return
	}

	result, err := h.geminiOAuthService.GenerateAuthURL(
		c.Request.Context(),
		nil,
		deriveUserGeminiRedirectURI(c),
		req.ProjectID,
		oauthType,
		req.TierID,
	)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "OAuth client not configured") ||
			strings.Contains(msg, "requires your own OAuth Client") ||
			strings.Contains(msg, "requires a custom OAuth Client") ||
			strings.Contains(msg, "GEMINI_CLI_OAUTH_CLIENT_SECRET_MISSING") ||
			strings.Contains(msg, "built-in Gemini CLI OAuth client_secret is not configured") {
			response.BadRequest(c, "Failed to generate auth URL: "+msg)
			return
		}
		response.InternalError(c, "Failed to generate auth URL: "+msg)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) ExchangeGeminiOAuthCode(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userGeminiExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}

	oauthType := strings.TrimSpace(req.OAuthType)
	if oauthType == "" {
		oauthType = "code_assist"
	}
	if oauthType != "code_assist" && oauthType != "google_one" && oauthType != "ai_studio" {
		response.BadRequest(c, "Invalid oauth_type: must be 'code_assist', 'google_one', or 'ai_studio'")
		return
	}

	tokenInfo, err := h.geminiOAuthService.ExchangeCode(c.Request.Context(), &service.GeminiExchangeCodeInput{
		SessionID: req.SessionID,
		State:     req.State,
		Code:      req.Code,
		ProxyID:   nil,
		OAuthType: oauthType,
		TierID:    req.TierID,
	})
	if err != nil {
		response.BadRequest(c, "Failed to exchange code: "+err.Error())
		return
	}
	response.Success(c, tokenInfo)
}

func (h *UserAccountHandler) GenerateAntigravityOAuthURL(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userAntigravityGenerateAuthURLRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	result, err := h.antigravityOAuthService.GenerateAuthURL(c.Request.Context(), nil)
	if err != nil {
		response.InternalError(c, "Failed to generate auth URL: "+err.Error())
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) ExchangeAntigravityOAuthCode(c *gin.Context) {
	if !requireUserAccountAuth(c) {
		return
	}
	var req userAntigravityExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !rejectUserProxyID(c, req.ProxyID) {
		return
	}
	tokenInfo, err := h.antigravityOAuthService.ExchangeCode(c.Request.Context(), &service.AntigravityExchangeCodeInput{
		SessionID: req.SessionID,
		State:     req.State,
		Code:      req.Code,
		ProxyID:   nil,
	})
	if err != nil {
		response.BadRequest(c, "Failed to exchange code: "+err.Error())
		return
	}
	response.Success(c, tokenInfo)
}

func (h *UserAccountHandler) RefreshAntigravityToken(c *gin.Context) {
	rejectUserManualCredentialAuth(c)
}

func deriveUserGeminiRedirectURI(c *gin.Context) string {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin != "" {
		return strings.TrimRight(origin, "/") + "/auth/callback"
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if xfProto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); xfProto != "" {
		scheme = strings.TrimSpace(strings.Split(xfProto, ",")[0])
	}

	host := strings.TrimSpace(c.Request.Host)
	if xfHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); xfHost != "" {
		host = strings.TrimSpace(strings.Split(xfHost, ",")[0])
	}

	return fmt.Sprintf("%s://%s/auth/callback", scheme, host)
}
