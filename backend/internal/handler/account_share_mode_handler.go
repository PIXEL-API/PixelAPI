package handler

import (
	"math"
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

type accountShareProxyCreateRequest struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type accountShareListingUpdateRequest struct {
	Name                   *string   `json:"name"`
	ProxyID                *int64    `json:"proxy_id"`
	Status                 *string   `json:"status"`
	SeatLimit              *int      `json:"seat_limit"`
	RateMultiplier         *float64  `json:"rate_multiplier"`
	AllowedModels          *[]string `json:"allowed_models"`
	PerUserConcurrency     *int      `json:"per_user_concurrency"`
	HourlyRate             *float64  `json:"hourly_rate"`
	HourlyFeeWaiverMinimum *float64  `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired     *float64  `json:"min_balance_required"`
	CodexCLIOnly           *bool     `json:"codex_cli_only"`
	Codex5hLimitPercent    *float64  `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent    *float64  `json:"codex_7d_limit_percent"`
	Concurrency            *int      `json:"concurrency"`
	EditSessionID          string    `json:"edit_session_id"`
	ForceActiveEdit        bool      `json:"force_active_edit"`
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

type accountShareIdleTimeoutUpdateRequest struct {
	IdleTimeoutMinutes int `json:"idle_timeout_minutes"`
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
	perUserConcurrencyMin, err := parseOptionalIntQuery(c, "per_user_concurrency_min")
	if err != nil {
		response.BadRequest(c, "Invalid per_user_concurrency_min")
		return
	}
	perUserConcurrencyMax, err := parseOptionalIntQuery(c, "per_user_concurrency_max")
	if err != nil {
		response.BadRequest(c, "Invalid per_user_concurrency_max")
		return
	}
	if invalidIntRange(perUserConcurrencyMin, perUserConcurrencyMax) {
		response.BadRequest(c, "Invalid per_user_concurrency range")
		return
	}
	minBalanceRequiredMin, err := parseOptionalFloatQuery(c, "min_balance_required_min")
	if err != nil {
		response.BadRequest(c, "Invalid min_balance_required_min")
		return
	}
	minBalanceRequiredMax, err := parseOptionalFloatQuery(c, "min_balance_required_max")
	if err != nil {
		response.BadRequest(c, "Invalid min_balance_required_max")
		return
	}
	if invalidFloatRange(minBalanceRequiredMin, minBalanceRequiredMax) {
		response.BadRequest(c, "Invalid min_balance_required range")
		return
	}
	hourlyRateMin, err := parseOptionalFloatQuery(c, "hourly_rate_min")
	if err != nil {
		response.BadRequest(c, "Invalid hourly_rate_min")
		return
	}
	hourlyRateMax, err := parseOptionalFloatQuery(c, "hourly_rate_max")
	if err != nil {
		response.BadRequest(c, "Invalid hourly_rate_max")
		return
	}
	if invalidFloatRange(hourlyRateMin, hourlyRateMax) {
		response.BadRequest(c, "Invalid hourly_rate range")
		return
	}
	hourlyFeeWaiverMin, err := parseOptionalFloatQuery(c, "hourly_fee_waiver_min")
	if err != nil {
		response.BadRequest(c, "Invalid hourly_fee_waiver_min")
		return
	}
	hourlyFeeWaiverMax, err := parseOptionalFloatQuery(c, "hourly_fee_waiver_max")
	if err != nil {
		response.BadRequest(c, "Invalid hourly_fee_waiver_max")
		return
	}
	if invalidFloatRange(hourlyFeeWaiverMin, hourlyFeeWaiverMax) {
		response.BadRequest(c, "Invalid hourly_fee_waiver range")
		return
	}
	availableOnly, err := parseOptionalBoolQuery(c, "available_only")
	if err != nil {
		response.BadRequest(c, "Invalid available_only")
		return
	}
	listings, result, err := h.service.ListListings(c.Request.Context(), subject.UserID, role == service.RoleAdmin, service.AccountShareListingFilters{
		Tab:                   c.DefaultQuery("tab", service.AccountShareModeListingTabAll),
		SeatLimit:             seatLimit,
		Search:                c.Query("search"),
		Status:                c.Query("status"),
		AvailableOnly:         availableOnly,
		PerUserConcurrencyMin: perUserConcurrencyMin,
		PerUserConcurrencyMax: perUserConcurrencyMax,
		MinBalanceRequiredMin: minBalanceRequiredMin,
		MinBalanceRequiredMax: minBalanceRequiredMax,
		HourlyRateMin:         hourlyRateMin,
		HourlyRateMax:         hourlyRateMax,
		HourlyFeeWaiverMin:    hourlyFeeWaiverMin,
		HourlyFeeWaiverMax:    hourlyFeeWaiverMax,
		Models:                parseCSVQuery(c, "models"),
		AccountLevel:          c.Query("account_level"),
	}, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, listings, result.Total, result.Page, result.PageSize)
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
		Name:                   req.Name,
		ProxyID:                req.ProxyID,
		Status:                 req.Status,
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
		Concurrency:            req.Concurrency,
		EditSessionID:          req.EditSessionID,
		ForceActiveEdit:        req.ForceActiveEdit,
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

func parseOptionalIntQuery(c *gin.Context, name string) (*int, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		if err == nil {
			err = strconv.ErrSyntax
		}
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalFloatQuery(c *gin.Context, name string) (*float64, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		if err == nil {
			err = strconv.ErrSyntax
		}
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalBoolQuery(c *gin.Context, name string) (bool, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func invalidIntRange(minValue, maxValue *int) bool {
	return minValue != nil && maxValue != nil && *minValue > *maxValue
}

func invalidFloatRange(minValue, maxValue *float64) bool {
	return minValue != nil && maxValue != nil && *minValue > *maxValue
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
