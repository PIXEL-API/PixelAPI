package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

type UserAccountHandler struct {
	accountService          *service.AccountService
	accountUsageService     *service.AccountUsageService
	accountTestService      *service.AccountTestService
	rateLimitService        *service.RateLimitService
	settingService          *service.SettingService
	concurrencyService      *service.ConcurrencyService
	oauthService            *service.OAuthService
	openaiOAuthService      *service.OpenAIOAuthService
	openaiQuotaService      *service.OpenAIQuotaService
	userModerationService   *service.UserContentModerationService
	geminiOAuthService      *service.GeminiOAuthService
	antigravityOAuthService *service.AntigravityOAuthService
	grokOAuthService        *service.GrokOAuthService
	grokTokenProvider       *service.GrokTokenProvider
	sessionLimitCache       service.SessionLimitCache
	rpmCache                service.RPMCache
	accountBatchTaskService *service.AccountBatchTaskService
}

func NewUserAccountHandler(
	accountService *service.AccountService,
	accountUsageService *service.AccountUsageService,
	accountTestService *service.AccountTestService,
	rateLimitService *service.RateLimitService,
	settingService *service.SettingService,
	oauthService *service.OAuthService,
	openaiOAuthService *service.OpenAIOAuthService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
	accountBatchTaskServices ...*service.AccountBatchTaskService,
) *UserAccountHandler {
	var accountBatchTaskService *service.AccountBatchTaskService
	if len(accountBatchTaskServices) > 0 {
		accountBatchTaskService = accountBatchTaskServices[0]
	}
	h := &UserAccountHandler{
		accountService:          accountService,
		accountUsageService:     accountUsageService,
		accountTestService:      accountTestService,
		rateLimitService:        rateLimitService,
		settingService:          settingService,
		oauthService:            oauthService,
		openaiOAuthService:      openaiOAuthService,
		geminiOAuthService:      geminiOAuthService,
		antigravityOAuthService: antigravityOAuthService,
		accountBatchTaskService: accountBatchTaskService,
	}
	h.registerAccountBatchExecutors()
	return h
}

func (h *UserAccountHandler) SetRuntimeCapacityProviders(
	concurrencyService *service.ConcurrencyService,
	sessionLimitCache service.SessionLimitCache,
	rpmCache service.RPMCache,
) {
	if h == nil {
		return
	}
	h.concurrencyService = concurrencyService
	h.sessionLimitCache = sessionLimitCache
	h.rpmCache = rpmCache
}

func (h *UserAccountHandler) SetOpenAIQuotaService(openaiQuotaService *service.OpenAIQuotaService) {
	h.openaiQuotaService = openaiQuotaService
}

func (h *UserAccountHandler) SetUserContentModerationService(userModerationService *service.UserContentModerationService) {
	h.userModerationService = userModerationService
}

func (h *UserAccountHandler) SetGrokOAuthService(grokOAuthService *service.GrokOAuthService) {
	h.grokOAuthService = grokOAuthService
}

func (h *UserAccountHandler) SetGrokTokenProvider(grokTokenProvider *service.GrokTokenProvider) {
	h.grokTokenProvider = grokTokenProvider
}

type createUserAccountRequest struct {
	Name               string         `json:"name" binding:"required"`
	Notes              *string        `json:"notes"`
	Platform           string         `json:"platform" binding:"required"`
	AccountLevel       string         `json:"account_level"`
	Type               string         `json:"type" binding:"required,oneof=oauth apikey"`
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
	Platform           string   `json:"platform" binding:"required,oneof=anthropic openai gemini antigravity grok opencode"`
	OpenAIAuthMode     string   `json:"openai_auth_mode" binding:"omitempty,oneof=oauth personal_access_token agent_identity"`
	AccountLevel       string   `json:"account_level"`
	ProxyID            *int64   `json:"proxy_id"`
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
	AccountLevel       *string         `json:"account_level"`
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
	AccountLevel   *string        `json:"account_level"`
	ShareMode      *string        `json:"share_mode" binding:"omitempty,oneof=private public"`
	GroupIDs       *[]int64       `json:"group_ids"`
	Credentials    map[string]any `json:"credentials"`
	Extra          map[string]any `json:"extra"`
}

type convertUserAccountExternalPlacementRequest struct {
	Target         string `json:"target" binding:"required,oneof=private public_pool room"`
	RoomID         *int64 `json:"room_id"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

type convertUserAccountExternalPlacementBatchRequest struct {
	AccountIDs     []int64 `json:"account_ids" binding:"required"`
	Target         string  `json:"target" binding:"required,oneof=private public_pool room"`
	RoomID         *int64  `json:"room_id"`
	IdempotencyKey string  `json:"idempotency_key" binding:"required,max=96"`
}

type bulkDeleteUserAccountsRequest struct {
	AccountIDs []int64 `json:"account_ids"`
	// Force 为 true 时，若账号仍挂在广场房间，删除前自动把账号从房间退出。
	Force bool `json:"force"`
}

type userAccountWithRuntime struct {
	*dto.Account
	CurrentConcurrency int `json:"current_concurrency"`
	// 以下字段仅对 Anthropic OAuth/SetupToken 账号有效，且仅在启用相应功能时返回。
	CurrentWindowCost *float64 `json:"current_window_cost,omitempty"`
	ActiveSessions    *int     `json:"active_sessions,omitempty"`
	CurrentRPM        *int     `json:"current_rpm,omitempty"`
}

type userAccountBatchTaskRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

const userOwnedDefaultConcurrency = 3
const userOwnedDefaultPriority = 1
const userExternalPlacementBatchMaxAccounts = 1000

const (
	userOpenAIAuthModeOAuth               = "oauth"
	userOpenAIAuthModePersonalAccessToken = "personal_access_token"
	userOpenAIAuthModeAgentIdentity       = "agent_identity"
)

type userOAuthProxyRequest struct {
	ProxyID *int64 `json:"proxy_id"`
}

type userExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
}

type userOpenAIGenerateAuthURLRequest struct {
	ProxyID      *int64 `json:"proxy_id"`
	AccountLevel string `json:"account_level"`
	RedirectURI  string `json:"redirect_uri"`
}

type userOpenAIExchangeCodeRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	Code         string `json:"code" binding:"required"`
	State        string `json:"state" binding:"required"`
	RedirectURI  string `json:"redirect_uri"`
	ProxyID      *int64 `json:"proxy_id"`
	AccountLevel string `json:"account_level"`
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

type userGrokGenerateAuthURLRequest struct {
	ProxyID      *int64 `json:"proxy_id"`
	AccountLevel string `json:"account_level"`
	RedirectURI  string `json:"redirect_uri"`
}

type userGrokExchangeCodeRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	State        string `json:"state"`
	Code         string `json:"code" binding:"required"`
	RedirectURI  string `json:"redirect_uri"`
	ProxyID      *int64 `json:"proxy_id"`
	AccountLevel string `json:"account_level"`
}

type userBatchTodayStatsRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required"`
}

type userTestAccountRequest struct {
	ModelID string `json:"model_id"`
	Prompt  string `json:"prompt"`
	Mode    string `json:"mode"`
}

const userPublicShareValidationTimeout = 30 * time.Second
const userAccountBatchConnectionTestTimeout = 90 * time.Second

const userGeminiDefaultTestModel = "gemini-2.5-flash"

func userAccountConnectionTestModel(account *service.Account) string {
	if account == nil {
		return ""
	}
	if account.Platform == service.PlatformGemini || account.Platform == service.PlatformAntigravity {
		return userGeminiDefaultTestModel
	}
	return ""
}

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

// userOAuthProxyScope 构造用户 OAuth 登录/重新授权时的代理筛选范围。
//
// 带上调用者的遗留归属豁免：迁移 256 之前用户可以自行上传代理，这些代理的
// owner_user_id 被有意保留。不带豁免时，账号上绑定的自有代理在 GetVisibleByID
// 里查不到，老用户重新授权会直接 ErrProxyNotFound。放开的只是「调用者自己的」
// 代理，跨用户仍然不可见（visibleProxyPredicate 用的是等值匹配）。
func userOAuthProxyScope(c *gin.Context, platform, accountLevel string) service.ProxyScope {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		return service.NewProxyScope(platform, accountLevel)
	}
	return service.NewOwnedProxyScope(platform, accountLevel, subject.UserID)
}

func userGrokOAuthProxyScope(c *gin.Context, accountLevel string) service.ProxyScope {
	normalized := service.NormalizeAccountLevel(accountLevel)
	if !service.IsUserSelectableGrokAccountLevel(normalized) {
		normalized = service.AccountLevelUnknown
	}
	return userOAuthProxyScope(c, service.PlatformGrok, normalized)
}

func (h *UserAccountHandler) requireUserOAuthProxy(c *gin.Context, scope service.ProxyScope, proxyID *int64) bool {
	if proxyID == nil || *proxyID <= 0 {
		response.BadRequest(c, "proxy_id is required for user OAuth login")
		return false
	}
	if h.accountService == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return false
	}
	if err := h.accountService.EnsureOwnedProxyUsableForLogin(c.Request.Context(), scope, *proxyID); err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	return true
}

func (h *UserAccountHandler) validateUserOpenAIOAuthProxy(c *gin.Context, ownerUserID int64, accountLevel string, proxyID *int64) bool {
	levelConfigs, err := h.openAIAccountLevelConfigs(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	targetLevel := service.NormalizeAccountLevel(accountLevel)
	if !service.IsUserSelectableOpenAIAccountLevelWithConfigs(targetLevel, levelConfigs) {
		response.BadRequest(c, "OpenAI account level must be selected before login")
		return false
	}
	if !service.RequiresUserOpenAIProxyLoginWithConfigs(targetLevel, levelConfigs) {
		return rejectUserProxyID(c, proxyID)
	}
	return h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformOpenAI, targetLevel), proxyID)
}

func rejectUserManualCredentialAuth(c *gin.Context) {
	response.BadRequest(c, "manual credential account creation is not allowed for user accounts; use official OAuth or import OAuth credentials")
}

func (h *UserAccountHandler) openAIAccountLevelConfigs(ctx context.Context) ([]service.OpenAIAccountLevelConfig, error) {
	if h == nil || h.settingService == nil {
		return service.DefaultOpenAIAccountLevelConfigs(), nil
	}
	return h.settingService.GetOpenAIAccountLevelConfigs(ctx)
}

func (h *UserAccountHandler) prepareUserAccountRequest(c *gin.Context, ownerUserID int64, req *createUserAccountRequest) bool {
	if req == nil {
		response.BadRequest(c, "Invalid account request")
		return false
	}
	// 用户端自有账号仅 opencode 平台放开 apikey 类型，其余平台仍强制 OAuth。
	if req.Type == service.AccountTypeAPIKey && req.Platform != service.PlatformOpencode {
		response.BadRequest(c, "API key accounts are only supported for the opencode platform")
		return false
	}
	levelConfigs, err := h.openAIAccountLevelConfigs(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if req.Platform == service.PlatformGrok {
		targetLevel := service.NormalizeAccountLevel(req.AccountLevel)
		if !service.IsUserSelectableGrokAccountLevel(targetLevel) {
			response.BadRequest(c, "Grok account level must be Free or Heavy")
			return false
		}
		req.AccountLevel = targetLevel
		return h.requireUserOAuthProxy(c, userOAuthProxyScope(c, req.Platform, targetLevel), req.ProxyID)
	}
	if req.Platform != service.PlatformOpenAI {
		if service.RequiresUserAccountOAuthProxyWithConfigs(req.Platform, service.AccountLevelUnknown, levelConfigs) {
			req.AccountLevel = service.AccountLevelUnknown
			return h.requireUserOAuthProxy(c, userOAuthProxyScope(c, req.Platform, service.AccountLevelUnknown), req.ProxyID)
		}
		req.AccountLevel = service.AccountLevelUnknown
		return rejectUserProxyID(c, req.ProxyID)
	}

	targetLevel := service.NormalizeAccountLevel(req.AccountLevel)
	if !service.IsUserSelectableOpenAIAccountLevelWithConfigs(targetLevel, levelConfigs) {
		response.BadRequest(c, "OpenAI account level must be selected before import")
		return false
	}
	req.AccountLevel = targetLevel
	if !service.RequiresUserOpenAIProxyLoginWithConfigs(targetLevel, levelConfigs) {
		return rejectUserProxyID(c, req.ProxyID)
	}
	if h.openaiOAuthService == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return false
	}
	if err := h.openaiOAuthService.EnsureProxyVisibleToUser(c.Request.Context(), userOAuthProxyScope(c, req.Platform, req.AccountLevel), req.ProxyID); err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	return true
}

func normalizeUserCredentialImportTargetLevel(req *importUserAccountCredentialsRequest, configs []service.OpenAIAccountLevelConfig) {
	if req == nil {
		return
	}
	targetLevel := service.NormalizeAccountLevel(req.AccountLevel)
	if req.Platform == service.PlatformGrok && service.IsUserSelectableGrokAccountLevel(targetLevel) {
		req.AccountLevel = targetLevel
		return
	}
	if req.Platform == service.PlatformOpenAI && service.IsUserSelectableOpenAIAccountLevelWithConfigs(targetLevel, configs) {
		req.AccountLevel = targetLevel
		return
	}
	req.AccountLevel = service.AccountLevelUnknown
}

func credentialImportSourceIsOpenAI(source service.AccountCredentialImportSource) bool {
	return source.Platform == service.PlatformOpenAI || source.Kind == service.AccountCredentialImportKindOpenAIRefreshToken
}

func enrichUserK12CredentialImportSource(source *service.AccountCredentialImportSource, accountLevel string) error {
	if source == nil ||
		source.Kind != service.AccountCredentialImportKindOAuthCredentials ||
		source.Platform != service.PlatformOpenAI ||
		service.NormalizeAccountLevel(accountLevel) != service.AccountLevelK12 {
		return nil
	}
	return service.EnrichOpenAIOAuthCredentialsFromIDToken(source.Credentials)
}

func credentialImportSourcePlatform(source service.AccountCredentialImportSource) string {
	switch source.Kind {
	case service.AccountCredentialImportKindOpenAIRefreshToken:
		return service.PlatformOpenAI
	case service.AccountCredentialImportKindClaudeSessionKey:
		return service.PlatformAnthropic
	case service.AccountCredentialImportKindOpencodeAPIKey:
		return service.PlatformOpencode
	default:
		return strings.TrimSpace(source.Platform)
	}
}

func validateCredentialImportTargetPlatform(defaults importUserAccountCredentialsRequest, source service.AccountCredentialImportSource) error {
	targetPlatform := strings.TrimSpace(defaults.Platform)
	sourcePlatform := credentialImportSourcePlatform(source)
	if targetPlatform == "" {
		return infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_PLATFORM_REQUIRED", "导入账号前请先选择平台")
	}
	if sourcePlatform == "" {
		return infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_PLATFORM_UNKNOWN", "无法确认导入内容的平台，请检查凭证格式")
	}
	if sourcePlatform != targetPlatform {
		return infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_PLATFORM_MISMATCH", "导入内容平台与所选平台不一致，请选择正确平台后重试").WithMetadata(map[string]string{
			"target_platform": targetPlatform,
			"source_platform": sourcePlatform,
		})
	}
	return nil
}

func validateOpenAIImportTargetLevel(defaults importUserAccountCredentialsRequest, configs []service.OpenAIAccountLevelConfig) (string, error) {
	targetLevel := service.NormalizeAccountLevel(defaults.AccountLevel)
	if !service.IsUserSelectableOpenAIAccountLevelWithConfigs(targetLevel, configs) {
		return "", service.ErrOwnedOpenAIAccountLevelRequired
	}
	return targetLevel, nil
}

func validateGrokImportTargetLevel(defaults importUserAccountCredentialsRequest) (string, error) {
	targetLevel := service.NormalizeAccountLevel(defaults.AccountLevel)
	if !service.IsUserSelectableGrokAccountLevel(targetLevel) {
		return "", service.ErrOwnedGrokAccountLevelRequired
	}
	return targetLevel, nil
}

func resolveUserOpenAICredentialImportMode(
	req importUserAccountCredentialsRequest,
	sources []service.AccountCredentialImportSource,
) (string, error) {
	declaredMode := strings.ToLower(strings.TrimSpace(req.OpenAIAuthMode))
	agentIdentityCount := 0
	personalAccessTokenCount := 0
	for _, source := range sources {
		if source.Kind == service.AccountCredentialImportKindOpenAIAgentIdentity {
			agentIdentityCount++
		}
		if source.Kind == service.AccountCredentialImportKindOpenAIPersonalAccessToken {
			personalAccessTokenCount++
		}
	}

	if declaredMode == userOpenAIAuthModeAgentIdentity {
		if req.Platform != service.PlatformOpenAI {
			return "", infraerrors.BadRequest("OWNED_AGENT_IDENTITY_PLATFORM_INVALID", "Codex Agent Identity 仅支持 OpenAI 平台")
		}
		if agentIdentityCount != len(sources) {
			return "", infraerrors.BadRequest("OWNED_AGENT_IDENTITY_CONTENT_INVALID", "Agent Identity 模式只接受 Agent Identity JSON 凭证")
		}
		return userOpenAIAuthModeAgentIdentity, nil
	}
	if declaredMode == userOpenAIAuthModePersonalAccessToken {
		if req.Platform != service.PlatformOpenAI || personalAccessTokenCount != len(sources) {
			return "", infraerrors.BadRequest("OWNED_CODEX_PAT_CONTENT_INVALID", "Codex PAT 模式只接受 OpenAI Personal Access Token 导出凭证")
		}
		return userOpenAIAuthModePersonalAccessToken, nil
	}
	if declaredMode == userOpenAIAuthModeOAuth && (agentIdentityCount > 0 || personalAccessTokenCount > 0) {
		return "", infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_AUTH_MODE_MISMATCH", "导入凭证与所选 OpenAI 认证模式不一致")
	}
	if agentIdentityCount == 0 && personalAccessTokenCount == 0 {
		return userOpenAIAuthModeOAuth, nil
	}
	if req.Platform != service.PlatformOpenAI || (agentIdentityCount != len(sources) && personalAccessTokenCount != len(sources)) {
		return "", infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_AUTH_MODE_MIXED", "不同 OpenAI 认证模式的凭证不能混合导入")
	}
	if agentIdentityCount == len(sources) {
		return userOpenAIAuthModeAgentIdentity, nil
	}
	return userOpenAIAuthModePersonalAccessToken, nil
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

func credentialImportFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) && strings.TrimSpace(appErr.Message) != "" {
		return strings.TrimSpace(appErr.Message)
	}
	return "账号导入失败，请检查凭证格式或稍后重试"
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

func (h *UserAccountHandler) registerAccountBatchExecutors() {
	if h == nil || h.accountBatchTaskService == nil {
		return
	}
	h.accountBatchTaskService.RegisterExecutor(service.AccountBatchTaskOperationUserRefreshCredentials, h.executeUserRefreshCredentialsTaskItem)
	h.accountBatchTaskService.RegisterExecutor(service.AccountBatchTaskOperationUserTestConnection, h.executeUserTestConnectionTaskItem)
	h.accountBatchTaskService.RegisterExecutor(service.AccountBatchTaskOperationUserRevalidateShare, h.executeUserRevalidateShareTaskItem)
	h.accountBatchTaskService.RegisterExecutor(service.AccountBatchTaskOperationUserSetPublicShare, h.executeUserSetPublicShareTaskItem)
}

func (h *UserAccountHandler) executeUserRefreshCredentialsTaskItem(ctx context.Context, task *service.AccountBatchTask, item service.AccountBatchTaskItem) (map[string]any, error) {
	if task == nil || task.OwnerUserID == nil {
		return nil, service.ErrAccountNotFound
	}
	account, err := h.accountService.GetOwnedByID(ctx, *task.OwnerUserID, item.AccountID)
	if err != nil {
		return nil, err
	}
	updated, warning, err := h.refreshOwnedAccount(ctx, *task.OwnerUserID, account)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"account_id": updated.ID}
	if strings.TrimSpace(warning) != "" {
		result["warning"] = warning
	}
	return result, nil
}

func (h *UserAccountHandler) executeUserTestConnectionTaskItem(ctx context.Context, task *service.AccountBatchTask, item service.AccountBatchTaskItem) (map[string]any, error) {
	if task == nil || task.OwnerUserID == nil {
		return nil, service.ErrAccountNotFound
	}
	if h.accountTestService == nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_TEST_SERVICE_UNAVAILABLE", "account test service is unavailable")
	}
	account, err := h.accountService.GetOwnedByID(ctx, *task.OwnerUserID, item.AccountID)
	if err != nil {
		return nil, err
	}

	testCtx, cancel := context.WithTimeout(ctx, userAccountBatchConnectionTestTimeout)
	defer cancel()
	testResult, err := h.accountTestService.RunTestBackground(testCtx, item.AccountID, userAccountConnectionTestModel(account))
	if err != nil {
		return nil, err
	}
	if testResult == nil {
		return nil, errors.New("account test did not return a result")
	}
	if strings.TrimSpace(testResult.Status) != "success" {
		message := strings.TrimSpace(testResult.ErrorMessage)
		if message == "" {
			message = "account test failed"
		}
		return nil, errors.New(message)
	}

	result := map[string]any{
		"account_id": item.AccountID,
		"status":     testResult.Status,
		"latency_ms": testResult.LatencyMs,
	}
	if h.rateLimitService != nil {
		recovery, err := h.rateLimitService.RecoverAccountAfterSuccessfulTest(ctx, item.AccountID)
		if err != nil {
			return nil, fmt.Errorf("recover account after successful test: %w", err)
		}
		if recovery != nil {
			result["cleared_error"] = recovery.ClearedError
			result["cleared_rate_limit"] = recovery.ClearedRateLimit
		}
	}
	return result, nil
}

func (h *UserAccountHandler) executeUserRevalidateShareTaskItem(ctx context.Context, task *service.AccountBatchTask, item service.AccountBatchTaskItem) (map[string]any, error) {
	if task == nil || task.OwnerUserID == nil {
		return nil, service.ErrAccountNotFound
	}
	account, err := h.accountService.GetOwnedByID(ctx, *task.OwnerUserID, item.AccountID)
	if err != nil {
		return nil, err
	}
	if service.NormalizeAccountShareMode(account.ShareMode) != service.AccountShareModePublic {
		return nil, fmt.Errorf("only public shared accounts can be revalidated")
	}
	updated, err := h.activateOwnedPublicShareIfRequested(ctx, *task.OwnerUserID, account)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"account_id":   updated.ID,
		"share_status": updated.ShareStatus,
	}, nil
}

func (h *UserAccountHandler) executeUserSetPublicShareTaskItem(ctx context.Context, task *service.AccountBatchTask, item service.AccountBatchTaskItem) (map[string]any, error) {
	if task == nil || task.OwnerUserID == nil {
		return nil, service.ErrAccountNotFound
	}
	account, err := h.accountService.MarkOwnedPublicSharePending(ctx, *task.OwnerUserID, item.AccountID, "")
	if err != nil {
		return nil, err
	}
	updated, err := h.activateOwnedPublicShareIfRequested(ctx, *task.OwnerUserID, account)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"account_id":   updated.ID,
		"share_mode":   updated.ShareMode,
		"share_status": updated.ShareStatus,
	}, nil
}

func (h *UserAccountHandler) buildAccountResponseWithRuntime(ctx context.Context, account *service.Account) userAccountWithRuntime {
	item := userAccountWithRuntime{
		Account: dto.AccountFromServiceForUser(account),
	}
	if account == nil {
		return item
	}

	if h.concurrencyService != nil {
		if counts, err := h.concurrencyService.GetAccountConcurrencyBatch(ctx, []int64{account.ID}); err == nil && counts != nil {
			item.CurrentConcurrency = counts[account.ID]
		}
	}

	if account.IsAnthropicOAuthOrSetupToken() {
		if h.accountUsageService != nil && account.GetWindowCostLimit() > 0 {
			startTime := account.GetCurrentWindowStartTime()
			if stats, err := h.accountUsageService.GetAccountWindowStats(ctx, account.ID, startTime); err == nil && stats != nil {
				cost := stats.StandardCost
				item.CurrentWindowCost = &cost
			}
		}

		if h.sessionLimitCache != nil && account.GetMaxSessions() > 0 {
			idleTimeouts := map[int64]time.Duration{
				account.ID: time.Duration(account.GetSessionIdleTimeoutMinutes()) * time.Minute,
			}
			if sessions, err := h.sessionLimitCache.GetActiveSessionCountBatch(ctx, []int64{account.ID}, idleTimeouts); err == nil && sessions != nil {
				if count, ok := sessions[account.ID]; ok {
					item.ActiveSessions = &count
				}
			}
		}

		if h.rpmCache != nil && account.GetBaseRPM() > 0 {
			if rpm, err := h.rpmCache.GetRPM(ctx, account.ID); err == nil {
				item.CurrentRPM = &rpm
			}
		}
	}

	return item
}

func (h *UserAccountHandler) buildAccountListResponseWithRuntime(ctx context.Context, accounts []service.Account) []userAccountWithRuntime {
	out := make([]userAccountWithRuntime, len(accounts))
	if len(accounts) == 0 {
		return out
	}

	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	concurrencyCounts := make(map[int64]int)
	if h.concurrencyService != nil {
		if counts, err := h.concurrencyService.GetAccountConcurrencyBatch(ctx, accountIDs); err == nil && counts != nil {
			concurrencyCounts = counts
		}
	}

	windowCostAccountIDs := make([]int64, 0)
	sessionLimitAccountIDs := make([]int64, 0)
	rpmAccountIDs := make([]int64, 0)
	sessionIdleTimeouts := make(map[int64]time.Duration)
	for i := range accounts {
		acc := &accounts[i]
		if !acc.IsAnthropicOAuthOrSetupToken() {
			continue
		}
		if acc.GetWindowCostLimit() > 0 {
			windowCostAccountIDs = append(windowCostAccountIDs, acc.ID)
		}
		if acc.GetMaxSessions() > 0 {
			sessionLimitAccountIDs = append(sessionLimitAccountIDs, acc.ID)
			sessionIdleTimeouts[acc.ID] = time.Duration(acc.GetSessionIdleTimeoutMinutes()) * time.Minute
		}
		if acc.GetBaseRPM() > 0 {
			rpmAccountIDs = append(rpmAccountIDs, acc.ID)
		}
	}

	rpmCounts := make(map[int64]int)
	if len(rpmAccountIDs) > 0 && h.rpmCache != nil {
		if counts, err := h.rpmCache.GetRPMBatch(ctx, rpmAccountIDs); err == nil && counts != nil {
			rpmCounts = counts
		}
	}

	activeSessions := make(map[int64]int)
	if len(sessionLimitAccountIDs) > 0 && h.sessionLimitCache != nil {
		if sessions, err := h.sessionLimitCache.GetActiveSessionCountBatch(ctx, sessionLimitAccountIDs, sessionIdleTimeouts); err == nil && sessions != nil {
			activeSessions = sessions
		}
	}

	windowCosts := make(map[int64]float64)
	if len(windowCostAccountIDs) > 0 && h.accountUsageService != nil {
		var mu sync.Mutex
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(10)
		for i := range accounts {
			acc := &accounts[i]
			if !acc.IsAnthropicOAuthOrSetupToken() || acc.GetWindowCostLimit() <= 0 {
				continue
			}
			accCopy := acc
			g.Go(func() error {
				startTime := accCopy.GetCurrentWindowStartTime()
				stats, err := h.accountUsageService.GetAccountWindowStats(gctx, accCopy.ID, startTime)
				if err == nil && stats != nil {
					mu.Lock()
					windowCosts[accCopy.ID] = stats.StandardCost
					mu.Unlock()
				}
				return nil
			})
		}
		_ = g.Wait()
	}

	for i := range accounts {
		acc := &accounts[i]
		item := userAccountWithRuntime{
			Account:            dto.AccountFromServiceForUser(acc),
			CurrentConcurrency: concurrencyCounts[acc.ID],
		}
		if cost, ok := windowCosts[acc.ID]; ok {
			item.CurrentWindowCost = &cost
		}
		if count, ok := activeSessions[acc.ID]; ok {
			item.ActiveSessions = &count
		}
		if rpm, ok := rpmCounts[acc.ID]; ok {
			item.CurrentRPM = &rpm
		}
		out[i] = item
	}

	return out
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
	out := h.buildAccountListResponseWithRuntime(c.Request.Context(), accounts)
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
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
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

	account, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	source := strings.ToLower(strings.TrimSpace(c.DefaultQuery("source", "local")))
	var usage *service.UsageInfo
	switch source {
	case "local":
		usage, err = h.accountUsageService.GetLocalUsageForAccount(c.Request.Context(), account)
	case "passive":
		usage, err = h.accountUsageService.GetPassiveUsageForAccount(c.Request.Context(), account)
	case "active":
		usage, err = h.accountUsageService.GetUsageForAccount(c.Request.Context(), account)
	default:
		response.BadRequest(c, "Invalid usage source")
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usage)
}

func (h *UserAccountHandler) QueryOpenAIQuota(c *gin.Context) {
	accountID, ok := h.resolveOwnedAccountID(c)
	if !ok {
		return
	}
	if h.openaiQuotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}
	usage, err := h.openaiQuotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usage)
}

func (h *UserAccountHandler) ResetOpenAIQuota(c *gin.Context) {
	accountID, ok := h.resolveOwnedAccountID(c)
	if !ok {
		return
	}
	if h.openaiQuotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}
	result, err := h.openaiQuotaService.ResetCredit(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) resolveOwnedAccountID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	if _, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID); err != nil {
		response.ErrorFrom(c, err)
		return 0, false
	}
	return accountID, true
}

func (h *UserAccountHandler) GetStats(c *gin.Context) {
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

	startTime, endTime, err := usagestats.ResolveAccountStatsDateRange(
		c.Query("start_date"),
		c.Query("end_date"),
		c.Query("days"),
		time.Now(),
	)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	stats, err := h.accountUsageService.GetAccountUsageStats(c.Request.Context(), accountID, startTime, endTime)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
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

	if err := h.accountService.EnsureOwnedByIDs(c.Request.Context(), subject.UserID, accountIDs); err != nil {
		response.ErrorFrom(c, err)
		return
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
	if !h.prepareUserAccountRequest(c, subject.UserID, &req) {
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = userOwnedDefaultConcurrency
	}
	if req.Priority <= 0 {
		req.Priority = userOwnedDefaultPriority
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
			ProxyID:            req.ProxyID,
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
		return h.buildAccountResponseWithRuntime(ctx, account), nil
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
	if !h.prepareUserAccountRequest(c, subject.UserID, &req) {
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = userOwnedDefaultConcurrency
	}
	if req.Priority <= 0 {
		req.Priority = userOwnedDefaultPriority
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
			ProxyID:            req.ProxyID,
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
		return h.buildAccountResponseWithRuntime(ctx, account), nil
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
		req.Priority = userOwnedDefaultPriority
	}
	if req.Concurrency <= 0 {
		req.Concurrency = userOwnedDefaultConcurrency
	}

	sources, parseErrors := service.ParseAccountCredentialImportContents(req.Contents)
	if req.Platform == service.PlatformOpencode {
		sources, parseErrors = service.ParseOpencodeCredentialImportContents(req.Contents)
	}
	if len(sources) == 0 && len(parseErrors) == 0 {
		response.BadRequest(c, "No importable account credentials found")
		return
	}
	openAIImportMode, err := resolveUserOpenAICredentialImportMode(req, sources)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	levelConfigs, err := h.openAIAccountLevelConfigs(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	isAgentIdentityImport := openAIImportMode == userOpenAIAuthModeAgentIdentity
	if isAgentIdentityImport {
		req.AccountLevel = service.AccountLevelUnknown
		req.ProxyID = nil
		req.ShareMode = service.AccountShareModePrivate
		req.ExpiresAt = nil
	} else {
		normalizeUserCredentialImportTargetLevel(&req, levelConfigs)
	}
	if !isAgentIdentityImport {
		if service.RequiresUserAccountOAuthProxyWithConfigs(req.Platform, req.AccountLevel, levelConfigs) {
			if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, req.Platform, req.AccountLevel), req.ProxyID) {
				return
			}
		} else if !rejectUserProxyID(c, req.ProxyID) {
			return
		}
	}
	importLimit, err := h.userAccountImportLimit(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if len(sources) > importLimit {
		response.BadRequest(c, fmt.Sprintf("Too many import items; maximum is %d", importLimit))
		return
	}

	result := service.AccountCredentialImportResult{
		Total:  len(sources) + len(parseErrors),
		Errors: []service.AccountCredentialImportError{},
	}
	result.Errors = append(result.Errors, parseErrors...)

	for idx, source := range sources {
		outcome, err := h.createOwnedAccountFromCredentialImportSource(c.Request.Context(), subject.UserID, source, req, idx+1)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, service.AccountCredentialImportError{
				Index:   len(parseErrors) + idx + 1,
				Kind:    string(source.Kind),
				Name:    source.Name,
				Message: credentialImportFailureMessage(err),
			})
			continue
		}
		if outcome != nil && outcome.Account != nil {
			if outcome.Updated {
				result.Updated++
			} else {
				result.Created++
			}
		}
	}
	result.Failed += len(parseErrors)
	response.Success(c, result)
}

func (h *UserAccountHandler) userAccountImportLimit(ctx context.Context) (int, error) {
	if h.settingService == nil {
		return 0, fmt.Errorf("setting service is required for user account import limit")
	}
	return h.settingService.GetUserAccountImportLimit(ctx)
}

func (h *UserAccountHandler) createOwnedAccountFromCredentialImportSource(
	ctx context.Context,
	ownerUserID int64,
	source service.AccountCredentialImportSource,
	defaults importUserAccountCredentialsRequest,
	sequence int,
) (*service.OwnedAccountImportResult, error) {
	var validatedPersonalAccessTokenInfo *service.OpenAITokenInfo
	if err := validateCredentialImportTargetPlatform(defaults, source); err != nil {
		return nil, err
	}

	targetAccountLevel := service.AccountLevelUnknown
	var levelConfigs []service.OpenAIAccountLevelConfig
	isAgentIdentity := source.Kind == service.AccountCredentialImportKindOpenAIAgentIdentity
	if credentialImportSourceIsOpenAI(source) && !isAgentIdentity {
		var err error
		levelConfigs, err = h.openAIAccountLevelConfigs(ctx)
		if err != nil {
			return nil, err
		}
		targetLevel, err := validateOpenAIImportTargetLevel(defaults, levelConfigs)
		if err != nil {
			return nil, err
		}
		targetAccountLevel = targetLevel
	} else if source.Platform == service.PlatformGrok {
		targetLevel, err := validateGrokImportTargetLevel(defaults)
		if err != nil {
			return nil, err
		}
		targetAccountLevel = targetLevel
	}
	if err := enrichUserK12CredentialImportSource(&source, targetAccountLevel); err != nil {
		slog.Debug(
			"owned_k12_import_enrich_id_token_decode_failed",
			"sequence",
			sequence,
			"error",
			err,
		)
	}

	req := service.CreateAccountRequest{
		Name:               strings.TrimSpace(source.Name),
		Notes:              source.Notes,
		Platform:           source.Platform,
		AccountLevel:       targetAccountLevel,
		Type:               service.AccountTypeOAuth,
		Credentials:        source.Credentials,
		Extra:              source.Extra,
		ShareMode:          defaults.ShareMode,
		ProxyID:            defaults.ProxyID,
		Concurrency:        defaults.Concurrency,
		LoadFactor:         defaults.LoadFactor,
		Priority:           defaults.Priority,
		GroupIDs:           defaults.GroupIDs,
		ExpiresAt:          userUnixSecondsToTime(defaults.ExpiresAt),
		AutoPauseOnExpired: defaults.AutoPauseOnExpired,
	}
	if req.Concurrency <= 0 {
		req.Concurrency = userOwnedDefaultConcurrency
	}

	switch source.Kind {
	case service.AccountCredentialImportKindOAuthCredentials:
		if req.Name == "" {
			req.Name = service.DeriveAccountCredentialImportName(req.Platform, req.Credentials, req.Extra, sequence)
		}
		if req.Platform == service.PlatformOpenAI {
			if err := h.verifyOwnedOpenAIOAuthImportLevel(ctx, ownerUserID, &req, defaults, targetAccountLevel, levelConfigs); err != nil {
				return nil, err
			}
		}
	case service.AccountCredentialImportKindOpenAIRefreshToken:
		tokenInfo, err := h.openaiOAuthService.RefreshTokenWithClientID(ctx, source.Token, "", source.ClientID)
		if err != nil {
			return nil, infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_OPENAI_REFRESH_FAILED", "OpenAI Refresh Token 校验失败，请检查账号凭证后重试")
		}
		req.Platform = service.PlatformOpenAI
		req.Credentials = h.openaiOAuthService.BuildAccountCredentials(tokenInfo)
		req.Extra = service.BuildOpenAIAccountCredentialImportExtra(tokenInfo)
		if defaults.Concurrency <= 0 {
			req.Concurrency = userOwnedDefaultConcurrency
		}
		if req.Name == "" {
			req.Name = strings.TrimSpace(tokenInfo.Email)
		}
		if req.Name == "" {
			req.Name = fmt.Sprintf("OpenAI OAuth Account #%d", sequence)
		}
	case service.AccountCredentialImportKindOpenAIPersonalAccessToken:
		if h.openaiOAuthService == nil {
			return nil, service.ErrServiceUnavailable
		}
		proxyURL, err := h.openaiOAuthService.VisibleProxyURLForUser(
			ctx,
			service.NewOwnedProxyScope(service.PlatformOpenAI, targetAccountLevel, ownerUserID),
			defaults.ProxyID,
		)
		if err != nil {
			return nil, err
		}
		tokenInfo, err := h.openaiOAuthService.ValidateCodexPersonalAccessToken(ctx, source.Token, proxyURL)
		if err != nil {
			return nil, infraerrors.BadRequest("OWNED_CODEX_PAT_VALIDATE_FAILED", "Codex Personal Access Token 校验失败，请检查令牌或代理后重试")
		}
		req.Platform = service.PlatformOpenAI
		validatedPersonalAccessTokenInfo = tokenInfo
		req.Credentials = service.BuildOpenAIPersonalAccessTokenCredentials(tokenInfo)
		req.Extra = service.BuildOpenAIAccountCredentialImportExtra(tokenInfo)
		if req.Name == "" {
			req.Name = strings.TrimSpace(tokenInfo.Email)
		}
		if req.Name == "" {
			req.Name = fmt.Sprintf("Codex PAT Account #%d", sequence)
		}
	case service.AccountCredentialImportKindOpenAIAgentIdentity:
		req.Platform = service.PlatformOpenAI
		req.AccountLevel = service.AccountLevelUnknown
		req.ShareMode = service.AccountShareModePrivate
		req.ProxyID = nil
		req.ExpiresAt = nil
		if req.Name == "" {
			req.Name = service.DeriveAccountCredentialImportName(req.Platform, req.Credentials, req.Extra, sequence)
		}
	case service.AccountCredentialImportKindClaudeSessionKey:
		tokenInfo, err := h.oauthService.CookieAuth(ctx, &service.CookieAuthInput{
			SessionKey: source.Token,
			ProxyID:    defaults.ProxyID,
			Scope:      "full",
		})
		if err != nil {
			return nil, infraerrors.BadRequest("OWNED_ACCOUNT_IMPORT_CLAUDE_SESSION_FAILED", "Claude Session Key 兑换失败，请检查账号凭证后重试")
		}
		req.Platform = service.PlatformAnthropic
		req.Credentials = service.BuildClaudeAccountCredentials(tokenInfo)
		req.Extra = service.BuildClaudeAccountCredentialImportExtra(tokenInfo)
		if defaults.Concurrency <= 0 {
			req.Concurrency = userOwnedDefaultConcurrency
		}
		if req.Name == "" {
			req.Name = strings.TrimSpace(tokenInfo.EmailAddress)
		}
		if req.Name == "" {
			req.Name = fmt.Sprintf("Claude OAuth Account #%d", sequence)
		}
	case service.AccountCredentialImportKindOpencodeAPIKey:
		req.Platform = service.PlatformOpencode
		req.AccountLevel = service.AccountLevelUnknown
		req.Type = service.AccountTypeAPIKey
		req.Credentials = map[string]any{"api_key": source.Token}
		if req.Name == "" {
			req.Name = service.DeriveOpencodeAPIKeyImportName(source.Token)
		}
	default:
		return nil, fmt.Errorf("unsupported credential import kind")
	}

	if req.Platform == service.PlatformOpenAI {
		req.AccountLevel = targetAccountLevel
		if source.Kind != service.AccountCredentialImportKindOpenAIPersonalAccessToken {
			resolvedExpiresAt, forceAutoPause, err := service.ResolveOpenAIAccessTokenOnlyLifecycle(
				req.Credentials,
				req.ExpiresAt,
			)
			if err != nil {
				return nil, err
			}
			req.ExpiresAt = resolvedExpiresAt
			if forceAutoPause {
				enabled := true
				req.AutoPauseOnExpired = &enabled
			}
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("account name is required")
	}
	var outcome *service.OwnedAccountImportResult
	var err error
	if source.Kind == service.AccountCredentialImportKindOpenAIPersonalAccessToken {
		outcome, err = h.accountService.ImportOwnedValidatedPersonalAccessTokenWithResult(ctx, ownerUserID, req, validatedPersonalAccessTokenInfo)
	} else {
		outcome, err = h.accountService.ImportOwnedWithResult(ctx, ownerUserID, req)
	}
	if err != nil {
		return nil, err
	}
	account, err := h.activateOwnedPublicShareIfRequested(ctx, ownerUserID, outcome.Account)
	if err != nil {
		return nil, err
	}
	outcome.Account = account
	return outcome, nil
}

// resolveOwnedOpenAIImportLevel 用探测到的真实 plan_type 严格匹配用户所选等级。
// probeFailed 表示探测失败；探测失败时仅 free/unknown 放行，付费等级拒绝。
// 探测成功但 plan_type 无法映射到已知等级时同样拒绝，避免给未知订阅发放付费等级。
func resolveOwnedOpenAIImportLevel(
	probePlanType string,
	probeFailed bool,
	targetAccountLevel string,
	levelConfigs []service.OpenAIAccountLevelConfig,
) (string, error) {
	target := service.NormalizeAccountLevel(targetAccountLevel)
	if probeFailed {
		switch target {
		case service.AccountLevelFree, service.AccountLevelUnknown:
			return target, nil
		default:
			return "", infraerrors.BadRequest("OWNED_OPENAI_IMPORT_LEVEL_VERIFY_FAILED", "无法验证账号真实订阅等级，请稍后重试")
		}
	}
	realLevel := service.NormalizeOpenAIPlanAccountLevelWithConfigs(probePlanType, levelConfigs)
	if realLevel == service.AccountLevelUnknown {
		return "", infraerrors.BadRequest("OWNED_OPENAI_IMPORT_LEVEL_UNRECOGNIZED", "无法识别账号真实订阅等级，请稍后重试")
	}
	if target != realLevel {
		return "", infraerrors.BadRequest("OWNED_OPENAI_IMPORT_LEVEL_MISMATCH",
			fmt.Sprintf("所选等级与账号真实订阅（%s）不符", strings.TrimSpace(probePlanType)))
	}
	return realLevel, nil
}

// verifyOwnedOpenAIOAuthImportLevel 对 OpenAI OAuth 凭证（access_token 直传）导入
// 探测真实 plan_type 并严格匹配用户所选等级，防止用户手写 plan_type 伪装等级。
func (h *UserAccountHandler) verifyOwnedOpenAIOAuthImportLevel(
	ctx context.Context,
	ownerUserID int64,
	req *service.CreateAccountRequest,
	defaults importUserAccountCredentialsRequest,
	targetAccountLevel string,
	levelConfigs []service.OpenAIAccountLevelConfig,
) error {
	if h.openaiOAuthService == nil {
		return service.ErrServiceUnavailable
	}
	accessToken, _ := req.Credentials["access_token"].(string)
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return infraerrors.BadRequest("OWNED_OPENAI_IMPORT_LEVEL_REQUIRED", "账号凭证缺少 access_token")
	}

	proxyURL, err := h.openaiOAuthService.VisibleProxyURLForUser(
		ctx,
		service.NewOwnedProxyScope(service.PlatformOpenAI, targetAccountLevel, ownerUserID),
		defaults.ProxyID,
	)
	if err != nil {
		return err
	}

	probe, probeErr := h.openaiOAuthService.ProbeChatGPTAccountInfo(ctx, accessToken, proxyURL)
	probePlanType := ""
	if probe != nil {
		probePlanType = strings.TrimSpace(probe.PlanType)
	}
	if _, err := resolveOwnedOpenAIImportLevel(probePlanType, probeErr != nil, targetAccountLevel, levelConfigs); err != nil {
		return err
	}

	// 探测成功：用真实 plan_type 覆盖用户手写的值，防止假 plan_type 残留到后续推断。
	if probePlanType != "" {
		req.Credentials["plan_type"] = probePlanType
		if _, ok := req.Credentials["chatgpt_plan_type"]; ok {
			req.Credentials["chatgpt_plan_type"] = probePlanType
		}
	}
	return nil
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
	if req.ShareMode != nil {
		response.ErrorFrom(c, service.ErrOwnedAccountPlacementConversionRequired)
		return
	}
	status := normalizeUserAccountStatus(req.Status)
	account, err := h.accountService.UpdateOwned(c.Request.Context(), subject.UserID, accountID, service.UpdateAccountRequest{
		Name:               req.Name,
		Notes:              req.Notes,
		AccountLevel:       req.AccountLevel,
		Credentials:        req.Credentials,
		Extra:              req.Extra,
		ProxyID:            req.ProxyID,
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
	changedPublicAgentIdentityCredentials := req.Credentials != nil && account.IsOpenAIAgentIdentity() &&
		service.NormalizeAccountShareMode(account.ShareMode) == service.AccountShareModePublic
	if changedPublicAgentIdentityCredentials {
		account, err = h.activateOwnedPublicShareIfRequested(c.Request.Context(), subject.UserID, account)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

func (h *UserAccountHandler) ConvertExternalPlacement(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req convertUserAccountExternalPlacementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.accountService.ConvertOwnedExternalPlacement(c.Request.Context(), subject.UserID, accountID, service.ConvertAccountExternalPlacementInput{
		Target:         req.Target,
		RoomID:         req.RoomID,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) ConvertExternalPlacementBatch(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req convertUserAccountExternalPlacementBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	if len(accountIDs) > userExternalPlacementBatchMaxAccounts {
		response.BadRequest(c, fmt.Sprintf(
			"too many account_ids; maximum is %d",
			userExternalPlacementBatchMaxAccounts,
		))
		return
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 96 {
		response.BadRequest(c, "idempotency_key must contain 1 to 96 characters")
		return
	}
	// room 目标与单账号路径语义一致：不指定房间，由 service/repo 按「默认房间」匹配
	// （ConvertOwnedExternalPlacement 对非空 RoomID 直接拒绝）。这里只做非法 room_id
	// 的兜底校验，不强制必填——否则与单账号 convert 自相矛盾，批量入房永远 400。
	if req.RoomID != nil && *req.RoomID > 0 {
		response.BadRequest(c, "room_id is not supported for batch placement")
		return
	}

	// 先一次性确认全部账号均归当前用户所有，避免越权账号引发部分更新或 N+1 查询。
	if err := h.accountService.EnsureOwnedByIDs(
		c.Request.Context(),
		subject.UserID,
		accountIDs,
	); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	result := &service.BulkUpdateAccountsResult{
		SuccessIDs: make([]int64, 0, len(accountIDs)),
		FailedIDs:  make([]int64, 0),
		Results:    make([]service.BulkUpdateAccountResult, 0, len(accountIDs)),
	}
	for _, accountID := range accountIDs {
		item := service.BulkUpdateAccountResult{AccountID: accountID}
		_, err := h.accountService.ConvertOwnedExternalPlacement(
			c.Request.Context(),
			subject.UserID,
			accountID,
			service.ConvertAccountExternalPlacementInput{
				Target:         req.Target,
				RoomID:         req.RoomID,
				IdempotencyKey: fmt.Sprintf("%s:%d", idempotencyKey, accountID),
			},
		)
		if err != nil {
			item.Error = err.Error()
			item.Reason = infraerrors.Reason(err)
			// infraerrors.Message 对非 ApplicationError（裸 DB/Redis 错误）返回固定
			// "internal error"，会遮蔽 err.Error() 里的真实原因。reason 为空时直接用
			// 完整错误文本，让前端明细显示可读原因而非通用占位。
			if item.Reason != "" {
				item.Message = infraerrors.Message(err)
			} else {
				item.Message = err.Error()
			}
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, accountID)
		} else {
			item.Success = true
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, accountID)
		}
		result.Results = append(result.Results, item)
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) RevalidatePublicShare(c *gin.Context) {
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
	if service.NormalizeAccountShareMode(account.ShareMode) != service.AccountShareModePublic {
		response.BadRequest(c, "Only public shared accounts can be revalidated")
		return
	}
	account, err = h.activateOwnedPublicShareIfRequested(c.Request.Context(), subject.UserID, account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

func (h *UserAccountHandler) CreateBatchRefreshTask(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.accountBatchTaskService == nil {
		response.Error(c, 503, "Account batch task service is unavailable")
		return
	}
	var req userAccountBatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	for _, accountID := range accountIDs {
		if _, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	ownerUserID := subject.UserID
	task, err := h.accountBatchTaskService.CreateTask(c.Request.Context(), service.CreateAccountBatchTaskInput{
		Scope:       service.AccountBatchTaskScopeUser,
		Operation:   service.AccountBatchTaskOperationUserRefreshCredentials,
		AccountIDs:  accountIDs,
		CreatedBy:   subject.UserID,
		OwnerUserID: &ownerUserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

func (h *UserAccountHandler) CreateBatchTestConnectionTask(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.accountBatchTaskService == nil {
		response.Error(c, 503, "Account batch task service is unavailable")
		return
	}
	if h.accountTestService == nil {
		response.Error(c, 503, "Account test service is unavailable")
		return
	}
	var req userAccountBatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	for _, accountID := range accountIDs {
		if _, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	ownerUserID := subject.UserID
	task, err := h.accountBatchTaskService.CreateTask(c.Request.Context(), service.CreateAccountBatchTaskInput{
		Scope:       service.AccountBatchTaskScopeUser,
		Operation:   service.AccountBatchTaskOperationUserTestConnection,
		AccountIDs:  accountIDs,
		CreatedBy:   subject.UserID,
		OwnerUserID: &ownerUserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

func (h *UserAccountHandler) CreateBatchRevalidatePublicShareTask(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.accountBatchTaskService == nil {
		response.Error(c, 503, "Account batch task service is unavailable")
		return
	}
	var req userAccountBatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	accountIDs := normalizeUserAccountIDList(req.AccountIDs)
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	for _, accountID := range accountIDs {
		account, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if service.NormalizeAccountShareMode(account.ShareMode) != service.AccountShareModePublic {
			response.BadRequest(c, "Only public shared accounts can be revalidated")
			return
		}
	}
	ownerUserID := subject.UserID
	task, err := h.accountBatchTaskService.CreateTask(c.Request.Context(), service.CreateAccountBatchTaskInput{
		Scope:       service.AccountBatchTaskScopeUser,
		Operation:   service.AccountBatchTaskOperationUserRevalidateShare,
		AccountIDs:  accountIDs,
		CreatedBy:   subject.UserID,
		OwnerUserID: &ownerUserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

func (h *UserAccountHandler) GetBatchTask(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.accountBatchTaskService == nil {
		response.Error(c, 503, "Account batch task service is unavailable")
		return
	}
	taskID, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid task ID")
		return
	}
	task, err := h.accountBatchTaskService.GetTask(c.Request.Context(), taskID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if task.Scope != service.AccountBatchTaskScopeUser || task.OwnerUserID == nil || *task.OwnerUserID != subject.UserID {
		response.NotFound(c, "Account batch task not found")
		return
	}
	response.Success(c, task)
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
	if req.ShareMode != nil {
		response.ErrorFrom(c, service.ErrOwnedAccountPlacementConversionRequired)
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
		GroupIDs:     req.GroupIDs,
		Credentials:  req.Credentials,
		Extra:        req.Extra,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	revalidatePublicAgentIdentity := len(req.Credentials) > 0
	if revalidatePublicAgentIdentity {
		for i := range result.Results {
			entry := &result.Results[i]
			if !entry.Success {
				continue
			}
			account, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, entry.AccountID)
			shouldActivate := account != nil && service.NormalizeAccountShareMode(account.ShareMode) == service.AccountShareModePublic &&
				account.IsOpenAIAgentIdentity()
			if err == nil && shouldActivate {
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
	force, _ := strconv.ParseBool(c.Query("force"))
	if err := h.accountService.DeleteOwned(c.Request.Context(), subject.UserID, accountID, force); err != nil {
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
	result, err := h.accountService.BulkDeleteOwned(c.Request.Context(), subject.UserID, accountIDs, req.Force)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) Test(c *gin.Context) {
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

	var req userTestAccountRequest
	_ = c.ShouldBindJSON(&req)

	if err := h.accountTestService.TestAccountConnection(c, accountID, req.ModelID, req.Prompt, req.Mode); err != nil {
		return
	}

	if h.rateLimitService != nil {
		if _, err := h.rateLimitService.RecoverAccountAfterSuccessfulTest(c.Request.Context(), accountID); err != nil {
			_ = c.Error(err)
		}
	}
}

// GetAvailableModels handles getting available models for a user-owned account.
// GET /api/v1/accounts/:id/models
// 复用 service.AvailableTestModels，与管理员端「测试连接」模型列表保持同一口径。
func (h *UserAccountHandler) GetAvailableModels(c *gin.Context) {
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

	models, supported := service.AvailableTestModels(account)
	if !supported {
		response.BadRequest(c, "Unsupported account platform: "+account.Platform)
		return
	}
	response.Success(c, models)
}

func (h *UserAccountHandler) RecoverState(c *gin.Context) {
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
	if h.rateLimitService == nil {
		response.Error(c, 503, "Rate limit service unavailable")
		return
	}
	if _, err := h.rateLimitService.RecoverAccountState(c.Request.Context(), accountID, service.AccountRecoveryOptions{
		InvalidateToken: true,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	account, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

func (h *UserAccountHandler) refreshOwnedAccount(ctx context.Context, ownerUserID int64, account *service.Account) (*service.Account, string, error) {
	if account == nil {
		return nil, "", service.ErrAccountNotFound
	}
	if !account.IsOAuth() {
		return nil, "", infraerrors.BadRequest("NOT_OAUTH", "cannot refresh non-OAuth account")
	}

	var newCredentials map[string]any
	var refreshedAccount *service.Account
	switch {
	case account.IsOpenAI():
		tokenInfo, err := h.openaiOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", err
		}
		newCredentials = h.openaiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
		newCredentials = service.NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)
	case account.Platform == service.PlatformGemini:
		tokenInfo, err := h.geminiOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", fmt.Errorf("failed to refresh credentials: %w", err)
		}
		newCredentials = h.geminiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
	case account.Platform == service.PlatformAntigravity:
		tokenInfo, err := h.antigravityOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", err
		}
		newCredentials = h.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
		if newProjectID, _ := newCredentials["project_id"].(string); newProjectID == "" {
			if oldProjectID := strings.TrimSpace(account.GetCredential("project_id")); oldProjectID != "" {
				newCredentials["project_id"] = oldProjectID
			}
		}
		if tokenInfo.ProjectIDMissing {
			updatedAccount, updateErr := h.accountService.UpdateOwned(ctx, ownerUserID, account.ID, service.UpdateAccountRequest{
				Credentials:    &newCredentials,
				MutationIntent: service.AccountMutationIntentSystemTokenRefresh,
			})
			if updateErr != nil {
				return nil, "", fmt.Errorf("failed to update credentials: %w", updateErr)
			}
			_, _ = h.setOwnedAccountPrivacy(ctx, ownerUserID, updatedAccount)
			return updatedAccount, "missing_project_id_temporary", nil
		}
	case account.Platform == service.PlatformGrok:
		if h.grokTokenProvider == nil {
			return nil, "", infraerrors.ServiceUnavailable("GROK_TOKEN_PROVIDER_UNAVAILABLE", "grok token provider unavailable")
		}
		var err error
		refreshedAccount, err = h.grokTokenProvider.RefreshNow(ctx, account)
		if err != nil {
			return nil, "", err
		}
	default:
		tokenInfo, err := h.oauthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", err
		}
		newCredentials = make(map[string]any)
		for k, v := range account.Credentials {
			newCredentials[k] = v
		}
		newCredentials["access_token"] = tokenInfo.AccessToken
		newCredentials["token_type"] = tokenInfo.TokenType
		newCredentials["expires_in"] = strconv.FormatInt(tokenInfo.ExpiresIn, 10)
		newCredentials["expires_at"] = strconv.FormatInt(tokenInfo.ExpiresAt, 10)
		if strings.TrimSpace(tokenInfo.RefreshToken) != "" {
			newCredentials["refresh_token"] = tokenInfo.RefreshToken
		}
		if strings.TrimSpace(tokenInfo.Scope) != "" {
			newCredentials["scope"] = tokenInfo.Scope
		}
	}

	updatedAccount := refreshedAccount
	if updatedAccount == nil {
		var err error
		updatedAccount, err = h.accountService.UpdateOwned(ctx, ownerUserID, account.ID, service.UpdateAccountRequest{
			Credentials:    &newCredentials,
			MutationIntent: service.AccountMutationIntentSystemTokenRefresh,
		})
		if err != nil {
			return nil, "", err
		}
	} else {
		var err error
		if h.rateLimitService == nil {
			return nil, "", infraerrors.ServiceUnavailable("RATE_LIMIT_SERVICE_UNAVAILABLE", "rate limit service unavailable")
		}
		if _, err = h.rateLimitService.RecoverAccountState(ctx, account.ID, service.AccountRecoveryOptions{
			InvalidateToken: true,
		}); err != nil {
			return nil, "", fmt.Errorf("failed to recover account state after refreshing credentials: %w", err)
		}
		updatedAccount, err = h.accountService.GetOwnedByID(ctx, ownerUserID, account.ID)
		if err != nil {
			return nil, "", err
		}
	}

	_, _ = h.setOwnedAccountPrivacy(ctx, ownerUserID, updatedAccount)
	return updatedAccount, "", nil
}

func (h *UserAccountHandler) Refresh(c *gin.Context) {
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

	updatedAccount, warning, err := h.refreshOwnedAccount(c.Request.Context(), subject.UserID, account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if warning == "missing_project_id_temporary" {
		response.Success(c, gin.H{
			"account": h.buildAccountResponseWithRuntime(c.Request.Context(), updatedAccount),
			"message": "Token refreshed successfully, but project_id could not be retrieved (will retry automatically)",
			"warning": "missing_project_id_temporary",
		})
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), updatedAccount))
}

func (h *UserAccountHandler) setOwnedAccountPrivacy(ctx context.Context, ownerUserID int64, account *service.Account) (string, error) {
	if account == nil {
		return "", service.ErrAccountNotFound
	}
	if account.Type != service.AccountTypeOAuth {
		return "", infraerrors.BadRequest("PRIVACY_UNSUPPORTED", "Only OAuth accounts support privacy setting")
	}

	mode := ""
	switch account.Platform {
	case service.PlatformOpenAI:
		if h.openaiOAuthService == nil || h.openaiOAuthService.PrivacyClientFactory() == nil {
			return "", infraerrors.BadRequest("PRIVACY_UNAVAILABLE", "privacy client is unavailable")
		}
		token, _ := account.Credentials["access_token"].(string)
		if token == "" {
			return "", infraerrors.BadRequest("PRIVACY_TOKEN_MISSING", "Cannot set privacy: missing access_token")
		}
		proxyURL, err := h.openaiOAuthService.VisibleProxyURLForUser(ctx, service.NewOwnedProxyScope(account.Platform, account.AccountLevel, ownerUserID), account.ProxyID)
		if err != nil {
			return "", err
		}
		mode = service.DisableOpenAITraining(ctx, h.openaiOAuthService.PrivacyClientFactory(), token, proxyURL)
	case service.PlatformAntigravity:
		token, _ := account.Credentials["access_token"].(string)
		if token == "" {
			return "", infraerrors.BadRequest("PRIVACY_TOKEN_MISSING", "Cannot set privacy: missing access_token")
		}
		projectID, _ := account.Credentials["project_id"].(string)
		mode = service.SetAntigravityPrivacy(ctx, token, projectID, "")
	default:
		return "", infraerrors.BadRequest("PRIVACY_UNSUPPORTED", "Only OpenAI and Antigravity OAuth accounts support privacy setting")
	}
	if mode == "" {
		return "", infraerrors.BadRequest("PRIVACY_FAILED", "Cannot set privacy")
	}

	extra := make(map[string]any, len(account.Extra)+1)
	for k, v := range account.Extra {
		extra[k] = v
	}
	extra["privacy_mode"] = mode
	if _, err := h.accountService.UpdateOwned(ctx, ownerUserID, account.ID, service.UpdateAccountRequest{Extra: &extra}); err != nil {
		return "", err
	}
	return mode, nil
}

func (h *UserAccountHandler) SetPrivacy(c *gin.Context) {
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

	mode, err := h.setOwnedAccountPrivacy(c.Request.Context(), subject.UserID, account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	updated, err := h.accountService.GetOwnedByID(c.Request.Context(), subject.UserID, accountID)
	if err != nil {
		if account.Extra == nil {
			account.Extra = make(map[string]any)
		}
		account.Extra["privacy_mode"] = mode
		response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), updated))
}

func (h *UserAccountHandler) GenerateAnthropicOAuthURL(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userOAuthProxyRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformAnthropic, service.AccountLevelUnknown), req.ProxyID) {
		return
	}
	result, err := h.oauthService.GenerateAuthURL(c.Request.Context(), req.ProxyID)
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
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformAnthropic, service.AccountLevelUnknown), req.ProxyID) {
		return
	}
	tokenInfo, err := h.oauthService.ExchangeCode(c.Request.Context(), &service.ExchangeCodeInput{
		SessionID: req.SessionID,
		Code:      req.Code,
		ProxyID:   req.ProxyID,
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
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userOpenAIGenerateAuthURLRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if h.openaiOAuthService == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	if !h.validateUserOpenAIOAuthProxy(c, subject.UserID, req.AccountLevel, req.ProxyID) {
		return
	}
	result, err := h.openaiOAuthService.GenerateAuthURL(
		c.Request.Context(),
		req.ProxyID,
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
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userOpenAIExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.openaiOAuthService == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	if !h.validateUserOpenAIOAuthProxy(c, subject.UserID, req.AccountLevel, req.ProxyID) {
		return
	}
	tokenInfo, err := h.openaiOAuthService.ExchangeCode(c.Request.Context(), &service.OpenAIExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
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
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userGeminiGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformGemini, service.AccountLevelUnknown), req.ProxyID) {
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
		req.ProxyID,
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
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userGeminiExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformGemini, service.AccountLevelUnknown), req.ProxyID) {
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
		ProxyID:   req.ProxyID,
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
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userAntigravityGenerateAuthURLRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformAntigravity, service.AccountLevelUnknown), req.ProxyID) {
		return
	}
	result, err := h.antigravityOAuthService.GenerateAuthURL(c.Request.Context(), req.ProxyID)
	if err != nil {
		response.InternalError(c, "Failed to generate auth URL: "+err.Error())
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) ExchangeAntigravityOAuthCode(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userAntigravityExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !h.requireUserOAuthProxy(c, userOAuthProxyScope(c, service.PlatformAntigravity, service.AccountLevelUnknown), req.ProxyID) {
		return
	}
	tokenInfo, err := h.antigravityOAuthService.ExchangeCode(c.Request.Context(), &service.AntigravityExchangeCodeInput{
		SessionID: req.SessionID,
		State:     req.State,
		Code:      req.Code,
		ProxyID:   req.ProxyID,
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

func (h *UserAccountHandler) GenerateGrokOAuthURL(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userGrokGenerateAuthURLRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	if h.grokOAuthService == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	if !h.requireUserOAuthProxy(c, userGrokOAuthProxyScope(c, req.AccountLevel), req.ProxyID) {
		return
	}
	result, err := h.grokOAuthService.GenerateAuthURL(c.Request.Context(), req.ProxyID, req.RedirectURI)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *UserAccountHandler) ExchangeGrokOAuthCode(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req userGrokExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.grokOAuthService == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return
	}
	if !h.requireUserOAuthProxy(c, userGrokOAuthProxyScope(c, req.AccountLevel), req.ProxyID) {
		return
	}
	tokenInfo, err := h.grokOAuthService.ExchangeCode(c.Request.Context(), &service.GrokExchangeCodeInput{
		SessionID:   req.SessionID,
		State:       req.State,
		Code:        req.Code,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tokenInfo)
}

func (h *UserAccountHandler) RefreshGrokToken(c *gin.Context) {
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
