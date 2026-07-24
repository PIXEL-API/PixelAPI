package handler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AccountShareModeHandler struct {
	service *service.AccountShareModeService
}

func NewAccountShareModeHandler(svc *service.AccountShareModeService) *AccountShareModeHandler {
	return &AccountShareModeHandler{service: svc}
}

type accountShareOpenAIAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

type accountShareOpenAIExchangeCodeRequest struct {
	SessionID              string   `json:"session_id" binding:"required"`
	Code                   string   `json:"code" binding:"required"`
	State                  string   `json:"state" binding:"required"`
	RedirectURI            string   `json:"redirect_uri"`
	ProxyID                *int64   `json:"proxy_id"`
	Name                   string   `json:"name"`
	Notes                  *string  `json:"notes"`
	Concurrency            int      `json:"concurrency"`
	SeatLimit              int      `json:"seat_limit"`
	RateMultiplier         float64  `json:"rate_multiplier"`
	AllowedModels          []string `json:"allowed_models"`
	PerUserConcurrency     int      `json:"per_user_concurrency"`
	HourlyRate             float64  `json:"hourly_rate"`
	HourlyFeeWaiverMinimum float64  `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired     *float64 `json:"min_balance_required"`
	CodexCLIOnly           bool     `json:"codex_cli_only"`
	Codex5hLimitPercent    float64  `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent    float64  `json:"codex_7d_limit_percent"`
	AutoPauseOnExpired     *bool    `json:"auto_pause_on_expired"`
}

type accountShareAnthropicAuthURLRequest struct {
	ProxyID *int64 `json:"proxy_id"`
}

type accountShareAnthropicExchangeCodeRequest struct {
	SessionID               string   `json:"session_id" binding:"required"`
	Code                    string   `json:"code" binding:"required"`
	ProxyID                 *int64   `json:"proxy_id"`
	Name                    string   `json:"name"`
	Notes                   *string  `json:"notes"`
	Concurrency             int      `json:"concurrency"`
	SeatLimit               int      `json:"seat_limit"`
	RateMultiplier          float64  `json:"rate_multiplier"`
	AllowedModels           []string `json:"allowed_models"`
	PerUserConcurrency      int      `json:"per_user_concurrency"`
	HourlyRate              float64  `json:"hourly_rate"`
	HourlyFeeWaiverMinimum  float64  `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired      *float64 `json:"min_balance_required"`
	Anthropic5hLimitPercent float64  `json:"anthropic_5h_limit_percent"`
	Anthropic7dLimitPercent float64  `json:"anthropic_7d_limit_percent"`
	AutoPauseOnExpired      *bool    `json:"auto_pause_on_expired"`
}

type accountShareProxyCreateRequest struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type accountShareProxyUpdateRequest struct {
	Name     string  `json:"name"`
	Protocol string  `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host     string  `json:"host" binding:"required"`
	Port     int     `json:"port" binding:"required,min=1,max=65535"`
	Username string  `json:"username"`
	Password *string `json:"password"`
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

type accountShareListingUpdateRequest struct {
	Name                    *string   `json:"name"`
	ProxyID                 *int64    `json:"proxy_id"`
	Status                  *string   `json:"status"`
	SeatLimit               *int      `json:"seat_limit"`
	RateMultiplier          *float64  `json:"rate_multiplier"`
	AllowedModels           *[]string `json:"allowed_models"`
	PerUserConcurrency      *int      `json:"per_user_concurrency"`
	HourlyRate              *float64  `json:"hourly_rate"`
	HourlyFeeWaiverMinimum  *float64  `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired      *float64  `json:"min_balance_required"`
	CodexCLIOnly            *bool     `json:"codex_cli_only"`
	Codex5hLimitPercent     *float64  `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent     *float64  `json:"codex_7d_limit_percent"`
	Anthropic5hLimitPercent *float64  `json:"anthropic_5h_limit_percent"`
	Anthropic7dLimitPercent *float64  `json:"anthropic_7d_limit_percent"`
	Concurrency             *int      `json:"concurrency"`
	EditSessionID           string    `json:"edit_session_id"`
	ForceActiveEdit         bool      `json:"force_active_edit"`
}

type accountShareListingEditSessionRequest struct {
	SessionID string `json:"session_id"`
	Force     bool   `json:"force"`
}

type accountShareJoinRequest struct {
	APIKeyID           int64 `json:"api_key_id" binding:"required"`
	IdleTimeoutMinutes int   `json:"idle_timeout_minutes"`
}

type accountShareEndRequest struct {
	Token string `json:"token" binding:"required"`
}

type accountShareReviewSubmitRequest struct {
	Score   *int   `json:"score" binding:"required"`
	Comment string `json:"comment"`
}

type accountShareIdleTimeoutUpdateRequest struct {
	IdleTimeoutMinutes int `json:"idle_timeout_minutes"`
}

type accountShareQueueReorderRequest struct {
	APIKeyID      int64   `json:"api_key_id" binding:"required"`
	MembershipIDs []int64 `json:"membership_ids" binding:"required"`
}

type accountShareRoomCreateRequest struct {
	AccountID               int64    `json:"account_id" binding:"required"`
	IdempotencyKey          string   `json:"idempotency_key" binding:"required,max=128"`
	RoomName                string   `json:"room_name" binding:"required"`
	SeatLimit               int      `json:"seat_limit"`
	RateMultiplier          float64  `json:"rate_multiplier"`
	AllowedModels           []string `json:"allowed_models"`
	PerUserConcurrency      int      `json:"per_user_concurrency"`
	HourlyRate              float64  `json:"hourly_rate"`
	HourlyFeeWaiverMinimum  float64  `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired      *float64 `json:"min_balance_required"`
	CodexCLIOnly            bool     `json:"codex_cli_only"`
	Codex5hLimitPercent     float64  `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent     float64  `json:"codex_7d_limit_percent"`
	Anthropic5hLimitPercent float64  `json:"anthropic_5h_limit_percent"`
	Anthropic7dLimitPercent float64  `json:"anthropic_7d_limit_percent"`
}

func (h *AccountShareModeHandler) ListModeGroups(c *gin.Context) {
	groups, err := h.service.ListModeGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, groups)
}

func (h *AccountShareModeHandler) GenerateOpenAIAuthURL(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareOpenAIAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.GenerateOpenAIAuthURL(c.Request.Context(), subject.UserID, req.ProxyID, req.RedirectURI)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountShareModeHandler) ExchangeOpenAICode(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareOpenAIExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxyID := int64(0)
	if req.ProxyID != nil {
		proxyID = *req.ProxyID
	}
	listing, err := h.service.ExchangeOpenAICodeAndCreateListing(
		c.Request.Context(),
		subject.UserID,
		&service.OpenAIExchangeCodeInput{
			SessionID:   req.SessionID,
			Code:        req.Code,
			State:       req.State,
			RedirectURI: req.RedirectURI,
			ProxyID:     req.ProxyID,
		},
		service.CreateAccountShareListingInput{
			Name:                   strings.TrimSpace(req.Name),
			Notes:                  req.Notes,
			ProxyID:                proxyID,
			Concurrency:            req.Concurrency,
			SeatLimit:              req.SeatLimit,
			RateMultiplier:         req.RateMultiplier,
			AllowedModels:          req.AllowedModels,
			PerUserConcurrency:     req.PerUserConcurrency,
			HourlyRate:             req.HourlyRate,
			HourlyFeeWaiverMinimum: req.HourlyFeeWaiverMinimum,
			MinBalanceRequired:     req.MinBalanceRequired,
			CodexCLIOnly:           req.CodexCLIOnly,
			Codex5hLimitPercent:    req.Codex5hLimitPercent,
			Codex7dLimitPercent:    req.Codex7dLimitPercent,
			AutoPauseOnExpired:     req.AutoPauseOnExpired,
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, listing)
}

func (h *AccountShareModeHandler) GenerateAnthropicAuthURL(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareAnthropicAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.GenerateAnthropicAuthURL(c.Request.Context(), subject.UserID, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountShareModeHandler) ExchangeAnthropicCode(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareAnthropicExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxyID := int64(0)
	if req.ProxyID != nil {
		proxyID = *req.ProxyID
	}
	listing, err := h.service.ExchangeAnthropicCodeAndCreateListing(
		c.Request.Context(),
		subject.UserID,
		&service.ExchangeCodeInput{
			SessionID: req.SessionID,
			Code:      req.Code,
			ProxyID:   req.ProxyID,
		},
		service.CreateAccountShareListingInput{
			Name:                    strings.TrimSpace(req.Name),
			Notes:                   req.Notes,
			ProxyID:                 proxyID,
			Concurrency:             req.Concurrency,
			SeatLimit:               req.SeatLimit,
			RateMultiplier:          req.RateMultiplier,
			AllowedModels:           req.AllowedModels,
			PerUserConcurrency:      req.PerUserConcurrency,
			HourlyRate:              req.HourlyRate,
			HourlyFeeWaiverMinimum:  req.HourlyFeeWaiverMinimum,
			MinBalanceRequired:      req.MinBalanceRequired,
			Anthropic5hLimitPercent: req.Anthropic5hLimitPercent,
			Anthropic7dLimitPercent: req.Anthropic7dLimitPercent,
			AutoPauseOnExpired:      req.AutoPauseOnExpired,
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, listing)
}

func (h *AccountShareModeHandler) ListListings(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	page, pageSize := response.ParsePagination(c)
	seatLimit := 0
	if raw := strings.TrimSpace(c.Query("seat_limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			seatLimit = parsed
		}
	}
	availableOnly, err := parseOptionalBoolQuery(c, "available_only")
	if err != nil {
		response.BadRequest(c, "Invalid available_only")
		return
	}
	seatLimits, err := parseAccountShareSeatLimitsQuery(c)
	if err != nil {
		response.BadRequest(c, "Invalid seat_limits")
		return
	}
	ownerUserID := int64(0)
	if raw := strings.TrimSpace(c.Query("owner_user_id")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid owner_user_id")
			return
		}
		ownerUserID = parsed
	}
	featureTags, err := parseAccountShareFeatureTagsQuery(c)
	if err != nil {
		response.BadRequest(c, "Invalid feature_tags")
		return
	}
	sorts, err := parseAccountShareSortsQuery(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	listings, result, err := h.service.ListListings(c.Request.Context(), subject.UserID, role == service.RoleAdmin, service.AccountShareListingFilters{
		Tab:           c.DefaultQuery("tab", service.AccountShareModeListingTabAll),
		Platform:      c.Query("platform"),
		SeatLimit:     seatLimit,
		SeatLimits:    seatLimits,
		Search:        c.Query("search"),
		Status:        c.Query("status"),
		AvailableOnly: availableOnly,
		Models:        parseCSVQuery(c, "models"),
		AccountLevel:  c.Query("account_level"),
		OwnerUserID:   ownerUserID,
		FeatureTags:   featureTags,
		Sorts:         sorts,
	}, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, listings, result.Total, result.Page, result.PageSize)
}

func (h *AccountShareModeHandler) RecommendListings(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req service.AccountShareRecommendationInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid recommendation request")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	result, err := h.service.RecommendListings(c.Request.Context(), subject.UserID, role == service.RoleAdmin, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountShareModeHandler) GetRecommendationUsageProfile(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	days := service.AccountShareRecommendationUsageProfileDays
	if rawDays := strings.TrimSpace(c.Query("days")); rawDays != "" {
		parsed, err := strconv.Atoi(rawDays)
		if err != nil {
			response.BadRequest(c, "Invalid days")
			return
		}
		days = parsed
	}
	profile, err := h.service.GetRecommendationUsageProfile(c.Request.Context(), subject.UserID, service.AccountShareRecommendationUsageProfileInput{
		Platform: c.DefaultQuery("platform", service.PlatformOpenAI),
		Model:    c.Query("model"),
		Days:     days,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, profile)
}

func (h *AccountShareModeHandler) ListAvailableProxies(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	proxies, err := h.service.ListAvailableProxies(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.ProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyWithAccountCountFromService(&proxies[i]))
	}
	response.Success(c, out)
}

func (h *AccountShareModeHandler) CreateProxy(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareProxyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxy, err := h.service.CreateUserProxy(c.Request.Context(), subject.UserID, service.CreateAccountShareProxyInput{
		Name:     strings.TrimSpace(req.Name),
		Protocol: strings.TrimSpace(req.Protocol),
		Host:     strings.TrimSpace(req.Host),
		Port:     req.Port,
		Username: strings.TrimSpace(req.Username),
		Password: strings.TrimSpace(req.Password),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.ProxyFromService(proxy))
}

func (h *AccountShareModeHandler) UpdateProxy(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	proxyID, err := parseInt64Param(c, "id")
	if err != nil || proxyID <= 0 {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}
	var req accountShareProxyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxy, err := h.service.UpdateUserProxy(c.Request.Context(), subject.UserID, proxyID, service.UpdateAccountShareProxyInput{
		Name:     strings.TrimSpace(req.Name),
		Protocol: strings.TrimSpace(req.Protocol),
		Host:     strings.TrimSpace(req.Host),
		Port:     req.Port,
		Username: strings.TrimSpace(req.Username),
		Password: trimOptionalString(req.Password),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyFromService(proxy))
}

func (h *AccountShareModeHandler) DeleteProxy(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	proxyID, err := parseInt64Param(c, "id")
	if err != nil || proxyID <= 0 {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}
	if err := h.service.DeleteUserProxy(c.Request.Context(), subject.UserID, proxyID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy deleted successfully"})
}

func (h *AccountShareModeHandler) GetListing(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	listing, err := h.service.GetListing(c.Request.Context(), subject.UserID, listingID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, listing)
}

func (h *AccountShareModeHandler) CreateRoom(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareRoomCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	listing, err := h.service.CreateRoomFromOwnedAccount(c.Request.Context(), subject.UserID, service.CreateAccountShareRoomInput{
		AccountID:               req.AccountID,
		IdempotencyKey:          req.IdempotencyKey,
		RoomName:                req.RoomName,
		SeatLimit:               req.SeatLimit,
		RateMultiplier:          req.RateMultiplier,
		AllowedModels:           req.AllowedModels,
		PerUserConcurrency:      req.PerUserConcurrency,
		HourlyRate:              req.HourlyRate,
		HourlyFeeWaiverMinimum:  req.HourlyFeeWaiverMinimum,
		MinBalanceRequired:      req.MinBalanceRequired,
		CodexCLIOnly:            req.CodexCLIOnly,
		Codex5hLimitPercent:     req.Codex5hLimitPercent,
		Codex7dLimitPercent:     req.Codex7dLimitPercent,
		Anthropic5hLimitPercent: req.Anthropic5hLimitPercent,
		Anthropic7dLimitPercent: req.Anthropic7dLimitPercent,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, listing)
}

func (h *AccountShareModeHandler) ListRoomAccounts(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	accounts, err := h.service.ListRoomAccounts(c.Request.Context(), subject.UserID, role == service.RoleAdmin, listingID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, accounts)
}

func (h *AccountShareModeHandler) GetMySpendSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	var membershipID *int64
	if raw := strings.TrimSpace(c.Query("membership_id")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid membership_id")
			return
		}
		membershipID = &parsed
	}
	summary, err := h.service.GetMySpendSummary(c.Request.Context(), subject.UserID, service.AccountShareMySpendInput{
		ListingID:    listingID,
		MembershipID: membershipID,
		Range:        c.DefaultQuery("range", service.AccountShareSpendRangeCurrentMembership),
		Timezone:     c.Query("timezone"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *AccountShareModeHandler) UpdateListing(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	var req accountShareListingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	listing, err := h.service.UpdateListing(c.Request.Context(), subject.UserID, role == service.RoleAdmin, listingID, service.UpdateAccountShareListingInput{
		Name:                    req.Name,
		ProxyID:                 req.ProxyID,
		Status:                  req.Status,
		SeatLimit:               req.SeatLimit,
		RateMultiplier:          req.RateMultiplier,
		AllowedModels:           req.AllowedModels,
		PerUserConcurrency:      req.PerUserConcurrency,
		HourlyRate:              req.HourlyRate,
		HourlyFeeWaiverMinimum:  req.HourlyFeeWaiverMinimum,
		MinBalanceRequired:      req.MinBalanceRequired,
		CodexCLIOnly:            req.CodexCLIOnly,
		Codex5hLimitPercent:     req.Codex5hLimitPercent,
		Codex7dLimitPercent:     req.Codex7dLimitPercent,
		Anthropic5hLimitPercent: req.Anthropic5hLimitPercent,
		Anthropic7dLimitPercent: req.Anthropic7dLimitPercent,
		Concurrency:             req.Concurrency,
		EditSessionID:           req.EditSessionID,
		ForceActiveEdit:         req.ForceActiveEdit,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, listing)
}

func (h *AccountShareModeHandler) BeginListingEdit(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	var req accountShareListingEditSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	listing, err := h.service.BeginListingEdit(c.Request.Context(), subject.UserID, role == service.RoleAdmin, listingID, req.SessionID, req.Force)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, listing)
}

func (h *AccountShareModeHandler) ReleaseListingEdit(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	var req accountShareListingEditSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	listing, err := h.service.ReleaseListingEdit(c.Request.Context(), subject.UserID, role == service.RoleAdmin, listingID, req.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, listing)
}

func (h *AccountShareModeHandler) JoinListing(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	var req accountShareJoinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	membership, err := h.service.JoinListing(c.Request.Context(), subject.UserID, listingID, req.APIKeyID, req.IdleTimeoutMinutes)
	if err != nil {
		logger.FromContext(c.Request.Context()).Warn("account share join failed",
			zap.String("component", "account_share.audit"),
			zap.Int64("user_id", subject.UserID),
			zap.Int64("listing_id", listingID),
			zap.Int64("api_key_id", req.APIKeyID),
			zap.Int("status_code", infraerrors.Code(err)),
			zap.String("reason", infraerrors.Reason(err)),
			zap.String("error", err.Error()),
		)
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, membership)
}

func (h *AccountShareModeHandler) UpdateMembershipIdleTimeout(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	membershipID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid membership ID")
		return
	}
	var req accountShareIdleTimeoutUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	membership, err := h.service.UpdateMembershipIdleTimeout(c.Request.Context(), subject.UserID, membershipID, req.IdleTimeoutMinutes)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, membership)
}

func (h *AccountShareModeHandler) ListMembershipQueue(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	apiKeyID, err := parseInt64Param(c, "apiKeyID")
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	memberships, err := h.service.ListMembershipQueue(c.Request.Context(), subject.UserID, apiKeyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, memberships)
}

func (h *AccountShareModeHandler) ReorderMembershipQueue(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accountShareQueueReorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	memberships, err := h.service.ReorderMembershipQueue(c.Request.Context(), subject.UserID, req.APIKeyID, req.MembershipIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, memberships)
}

func (h *AccountShareModeHandler) CreateEndMembershipIntent(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	membershipID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid membership ID")
		return
	}
	intent, err := h.service.CreateEndMembershipToken(c.Request.Context(), subject.UserID, membershipID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	logger.FromContext(c.Request.Context()).Info("account share end intent created",
		zap.String("component", "account_share.audit"),
		zap.Int64("user_id", subject.UserID),
		zap.Int64("membership_id", membershipID),
		zap.String("client_ip", c.ClientIP()),
		zap.String("user_agent", c.Request.UserAgent()),
		zap.String("referer", c.Request.Referer()),
	)
	response.Success(c, intent)
}

func (h *AccountShareModeHandler) EndMembership(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	membershipID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid membership ID")
		return
	}
	var req accountShareEndRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrAccountShareEndTokenRequired)
		return
	}
	membership, err := h.service.EndMembership(c.Request.Context(), subject.UserID, membershipID, req.Token)
	if err != nil {
		logger.FromContext(c.Request.Context()).Warn("account share end failed",
			zap.String("component", "account_share.audit"),
			zap.Int64("user_id", subject.UserID),
			zap.Int64("membership_id", membershipID),
			zap.Int("status_code", infraerrors.Code(err)),
			zap.String("reason", infraerrors.Reason(err)),
			zap.String("error", err.Error()),
			zap.String("client_ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.String("referer", c.Request.Referer()),
		)
		response.ErrorFrom(c, err)
		return
	}
	logger.FromContext(c.Request.Context()).Info("account share membership ended",
		zap.String("component", "account_share.audit"),
		zap.Int64("user_id", subject.UserID),
		zap.Int64("membership_id", membershipID),
		zap.Int64("api_key_id", membership.APIKeyID),
		zap.String("client_ip", c.ClientIP()),
		zap.String("user_agent", c.Request.UserAgent()),
		zap.String("referer", c.Request.Referer()),
	)
	response.Success(c, membership)
}

func (h *AccountShareModeHandler) SubmitReview(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	membershipID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid membership ID")
		return
	}
	var req accountShareReviewSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.Score == nil {
		response.BadRequest(c, "score is required")
		return
	}
	review, err := h.service.SubmitReview(c.Request.Context(), subject.UserID, membershipID, service.SubmitAccountShareReviewInput{
		Score:   *req.Score,
		Comment: strings.TrimSpace(req.Comment),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, review)
}

func (h *AccountShareModeHandler) ListListingReviews(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return
	}
	page, pageSize := response.ParsePagination(c)
	reviews, result, err := h.service.ListListingReviews(c.Request.Context(), subject.UserID, listingID, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, reviews, result.Total, result.Page, result.PageSize)
}

func (h *AccountShareModeHandler) ListOwnerReviews(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	ownerUserID, err := parseInt64Param(c, "owner_id")
	if err != nil {
		response.BadRequest(c, "Invalid owner ID")
		return
	}
	page, pageSize := response.ParsePagination(c)
	reviews, result, err := h.service.ListOwnerReviews(c.Request.Context(), subject.UserID, ownerUserID, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, reviews, result.Total, result.Page, result.PageSize)
}

func requireAccountShareAuth(c *gin.Context) bool {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return false
	}
	return true
}

func parseInt64Param(c *gin.Context, name string) (int64, error) {
	return strconv.ParseInt(c.Param(name), 10, 64)
}

func parseOptionalBoolQuery(c *gin.Context, name string) (bool, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func parseAccountShareSeatLimitsQuery(c *gin.Context) ([]int, error) {
	values := parseCSVQuery(c, "seat_limits")
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, raw := range values {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < service.AccountShareModeMinSeats || parsed > service.AccountShareModeMaxSeats {
			if err == nil {
				err = strconv.ErrSyntax
			}
			return nil, err
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		out = append(out, parsed)
	}
	return out, nil
}

func parseAccountShareFeatureTagsQuery(c *gin.Context) ([]string, error) {
	values := parseCSVQuery(c, "feature_tags")
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := service.NormalizeAccountShareListingFeatureTag(value)
		if normalized == "" {
			return nil, strconv.ErrSyntax
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func parseAccountShareSortsQuery(c *gin.Context) ([]service.AccountShareListingSortCriterion, error) {
	values := parseCSVQuery(c, "sorts")
	if len(values) == 0 {
		sortBy, sortOrder, err := parseAccountShareSortQuery(c)
		if err != nil || sortBy == "" {
			return nil, err
		}
		return []service.AccountShareListingSortCriterion{{SortBy: sortBy, SortOrder: sortOrder}}, nil
	}
	out := make([]service.AccountShareListingSortCriterion, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("Invalid sorts")
		}
		sortBy := service.NormalizeAccountShareListingSortBy(parts[0])
		sortOrder := service.NormalizeAccountShareListingSortOrder(parts[1])
		if sortBy == "" || sortOrder == "" {
			return nil, fmt.Errorf("Invalid sorts")
		}
		if _, ok := seen[sortBy]; ok {
			return nil, fmt.Errorf("Invalid sorts")
		}
		seen[sortBy] = struct{}{}
		out = append(out, service.AccountShareListingSortCriterion{SortBy: sortBy, SortOrder: sortOrder})
	}
	return out, nil
}

func parseAccountShareSortQuery(c *gin.Context) (string, string, error) {
	rawSortBy := strings.TrimSpace(c.Query("sort_by"))
	rawSortOrder := strings.TrimSpace(c.Query("sort_order"))
	if rawSortBy == "" && rawSortOrder == "" {
		return "", "", nil
	}
	sortBy := service.NormalizeAccountShareListingSortBy(rawSortBy)
	if sortBy == "" {
		return "", "", fmt.Errorf("Invalid sort_by")
	}
	sortOrder := service.NormalizeAccountShareListingSortOrder(rawSortOrder)
	if sortOrder == "" {
		return "", "", fmt.Errorf("Invalid sort_order")
	}
	return sortBy, sortOrder, nil
}

func parseCSVQuery(c *gin.Context, name string) []string {
	values := append([]string{}, c.QueryArray(name)...)
	values = append(values, c.QueryArray(name+"[]")...)
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}
