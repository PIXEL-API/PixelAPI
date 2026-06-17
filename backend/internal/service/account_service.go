package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrAccountNotFound                           = infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	ErrAccountNilInput                           = infraerrors.BadRequest("ACCOUNT_NIL_INPUT", "account input cannot be nil")
	ErrCodexQuotaLimitPercentInvalid             = infraerrors.BadRequest("CODEX_QUOTA_LIMIT_PERCENT_INVALID", "Codex quota limit percent must be between 1 and 100")
	ErrOwnedAccountAlreadyExists                 = infraerrors.Conflict("OWNED_ACCOUNT_ALREADY_EXISTS", "account already exists")
	ErrOwnedAccountTypeNotAllowed                = infraerrors.BadRequest("OWNED_ACCOUNT_TYPE_NOT_ALLOWED", "user accounts only support official OAuth accounts")
	ErrOwnedAccountCredentialsInvalid            = infraerrors.BadRequest("OWNED_ACCOUNT_CREDENTIALS_INVALID", "OAuth account credentials must include an access token")
	ErrOwnedAccountCredentialsNotAllowed         = infraerrors.BadRequest("OWNED_ACCOUNT_CREDENTIALS_NOT_ALLOWED", "user accounts cannot include API keys, custom URLs, upstream endpoints, cookies or manual session credentials")
	ErrOwnedAccountConcurrencyOutOfRange         = infraerrors.BadRequest("OWNED_ACCOUNT_CONCURRENCY_OUT_OF_RANGE", "personal account concurrency must be between 3 and 50")
	ErrOwnedAccountLoadFactorOutOfRange          = infraerrors.BadRequest("OWNED_ACCOUNT_LOAD_FACTOR_OUT_OF_RANGE", fmt.Sprintf("personal account load factor must be between 1 and %d", AccountMaxLoadFactor))
	ErrOwnedAccountLoadFactorCreditsUnavailable  = infraerrors.InternalServer("OWNED_ACCOUNT_LOAD_FACTOR_CREDITS_UNAVAILABLE", "load factor credit accounting is unavailable")
	ErrOwnedAccountLoadFactorCreditsInsufficient = infraerrors.BadRequest("OWNED_ACCOUNT_LOAD_FACTOR_CREDITS_INSUFFICIENT", "load factor credits are insufficient")
	ErrOwnedAccountLevelNotAllowed               = infraerrors.BadRequest("OWNED_ACCOUNT_LEVEL_NOT_ALLOWED", "user accounts cannot manually change account level")
	ErrOwnedOpenAIAccountLevelRequired           = infraerrors.BadRequest("OWNED_OPENAI_ACCOUNT_LEVEL_REQUIRED", "OpenAI user accounts must select an account level before import")
	ErrOwnedOpenAIAccountProxyRequired           = infraerrors.BadRequest("OWNED_OPENAI_ACCOUNT_PROXY_REQUIRED", "Pro OpenAI user accounts must use account login with a selected proxy IP")
	ErrOwnedAccountGroupPlatformMismatch         = infraerrors.BadRequest("OWNED_ACCOUNT_GROUP_PLATFORM_MISMATCH", "account group platform does not match account platform")
	ErrOwnedAccountGroupValidationUnavailable    = infraerrors.InternalServer("OWNED_ACCOUNT_GROUP_VALIDATION_UNAVAILABLE", "owned account group validation is unavailable")
	ErrOwnedAccountPublicPoolUnavailable         = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_POOL_UNAVAILABLE", "public shared account pool group is not configured for this account platform")
	ErrOwnedAccountPublicPolicyUnavailable       = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_POLICY_UNAVAILABLE", "account share policy is not configured for this public account pool")
	ErrOwnedAccountPublicValidationFailed        = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED", "public account validation failed")
	ErrOwnedAccountShareModeOnly                 = infraerrors.BadRequest("OWNED_ACCOUNT_SHARE_MODE_ONLY", "account share mode accounts cannot be moved to the public shared account pool")
	ErrOwnedAccountShareModeBoundaryUnavailable  = infraerrors.InternalServer("OWNED_ACCOUNT_SHARE_MODE_BOUNDARY_UNAVAILABLE", "account share mode boundary check is unavailable")
)

const AccountListGroupUngrouped int64 = -1
const AccountListProxyUnassigned int64 = -1
const AccountPrivacyModeUnsetFilter = "__unset__"
const ownedPersonalMinConcurrency = 3
const ownedPersonalMaxConcurrency = 50
const ownedPersonalDefaultConcurrency = ownedPersonalMinConcurrency
const ownedPersonalDefaultPriority = 1
const ownedPersonalDefaultOpenAICompactMode = "force_on"
const ownedPersonalDefaultOpenAIWSMode = OpenAIWSIngressModeOff
const accountQuotaPoolDashboardCacheTTL = 15 * time.Second

const (
	AccountLevelUnknown = domain.AccountLevelUnknown
	AccountLevelFree    = domain.AccountLevelFree
	AccountLevelPlus    = domain.AccountLevelPlus
	AccountLevelPro     = domain.AccountLevelPro
	AccountLevelTeam    = domain.AccountLevelTeam
)

type AccountRepository interface {
	Create(ctx context.Context, account *Account) error
	GetByID(ctx context.Context, id int64) (*Account, error)
	// GetByIDs fetches accounts by IDs in a single query.
	// It should return all accounts found (missing IDs are ignored).
	GetByIDs(ctx context.Context, ids []int64) ([]*Account, error)
	// ExistsByID 检查账号是否存在，仅返回布尔值，用于删除前的轻量级存在性检查
	ExistsByID(ctx context.Context, id int64) (bool, error)
	// GetByCRSAccountID finds an account previously synced from CRS.
	// Returns (nil, nil) if not found.
	GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error)
	// FindByExtraField 根据 extra 字段中的键值对查找账号
	FindByExtraField(ctx context.Context, key string, value any) ([]Account, error)
	// ListCRSAccountIDs returns a map of crs_account_id -> local account ID
	// for all accounts that have been synced from CRS.
	ListCRSAccountIDs(ctx context.Context) (map[string]int64, error)
	Update(ctx context.Context, account *Account) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error)
	ListByGroup(ctx context.Context, groupID int64) ([]Account, error)
	ListActive(ctx context.Context) ([]Account, error)
	ListByPlatform(ctx context.Context, platform string) ([]Account, error)

	UpdateLastUsed(ctx context.Context, id int64) error
	BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error
	SetError(ctx context.Context, id int64, errorMsg string) error
	ClearError(ctx context.Context, id int64) error
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
	AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error)
	BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error

	ListSchedulable(ctx context.Context) ([]Account, error)
	ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error)
	ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error)
	ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error)
	ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]Account, error)
	ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error)
	ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error)
	ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error)

	SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error
	SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time) error
	SetOverloaded(ctx context.Context, id int64, until time.Time) error
	SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error
	ClearTempUnschedulable(ctx context.Context, id int64) error
	ClearRateLimit(ctx context.Context, id int64) error
	ClearAntigravityQuotaScopes(ctx context.Context, id int64) error
	ClearModelRateLimits(ctx context.Context, id int64) error
	UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error)
	// IncrementQuotaUsed 原子递增 API Key 账号的配额用量（总/日/周）
	IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error
	// ResetQuotaUsed 重置 API Key 账号所有维度的配额用量为 0
	ResetQuotaUsed(ctx context.Context, id int64) error
}

// AccountBulkUpdate describes the fields that can be updated in a bulk operation.
// Nil pointers mean "do not change".
type AccountBulkUpdate struct {
	Name           *string
	ProxyID        *int64
	Concurrency    *int
	Priority       *int
	RateMultiplier *float64
	LoadFactor     *int
	Status         *string
	Schedulable    *bool
	AccountLevel   *string
	Credentials    map[string]any
	Extra          map[string]any
}

// CreateAccountRequest 创建账号请求
type CreateAccountRequest struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes"`
	Platform           string         `json:"platform"`
	AccountLevel       string         `json:"account_level"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra"`
	ShareMode          string         `json:"share_mode"`
	ProxyID            *int64         `json:"proxy_id"`
	Concurrency        int            `json:"concurrency"`
	LoadFactor         *int           `json:"load_factor"`
	Priority           int            `json:"priority"`
	GroupIDs           []int64        `json:"group_ids"`
	ExpiresAt          *time.Time     `json:"expires_at"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired"`
}

// UpdateAccountRequest 更新账号请求
type UpdateAccountRequest struct {
	Name               *string         `json:"name"`
	Notes              *string         `json:"notes"`
	AccountLevel       *string         `json:"account_level"`
	Credentials        *map[string]any `json:"credentials"`
	Extra              *map[string]any `json:"extra"`
	ShareMode          *string         `json:"share_mode"`
	ProxyID            *int64          `json:"proxy_id"`
	Concurrency        *int            `json:"concurrency"`
	LoadFactor         *int            `json:"load_factor"`
	Priority           *int            `json:"priority"`
	Status             *string         `json:"status"`
	Schedulable        *bool           `json:"schedulable"`
	GroupIDs           *[]int64        `json:"group_ids"`
	ExpiresAt          *time.Time      `json:"expires_at"`
	ClearExpiresAt     bool            `json:"-"`
	AutoPauseOnExpired *bool           `json:"auto_pause_on_expired"`
}

type OwnedPublicShareApprovalOptions struct {
	AllowRateLimited bool
}

// AccountService 账号管理服务
type AccountService struct {
	accountRepo             AccountRepository
	groupRepo               GroupRepository
	userRepo                accountUserRepository
	userSubRepo             accountSubscriptionLookupRepository
	accountSharePolicyRepo  AccountSharePolicyRepository
	privateGroupProvisioner UserPrivateGroupProvisioner
	systemNoticeService     *SystemNoticeService
	quotaPoolDashboardCache accountQuotaPoolDashboardCache
}

type accountQuotaPoolDashboardCache struct {
	mu      sync.Mutex
	userID  int64
	expires time.Time
	value   *UserAccountQuotaPoolDashboard
}

type groupExistenceBatchChecker interface {
	ExistsByIDs(ctx context.Context, ids []int64) (map[int64]bool, error)
}

type accountUserRepository interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

type accountSubscriptionLookupRepository interface {
	GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
}

type ownedAccountFilterRepository interface {
	ListOwnedWithFilters(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error)
}

type ownedLoadFactorCreditAccountRepository interface {
	UpdateOwnedAccountWithLoadFactorCredits(ctx context.Context, ownerUserID int64, account *Account) (*Account, error)
}

type accountQuotaPoolRepository interface {
	ListQuotaPoolAccounts(ctx context.Context, ownerUserID int64) ([]Account, error)
}

type accountShareModeListingAccountRepository interface {
	IsAccountShareModeListingAccount(ctx context.Context, accountID int64) (bool, error)
}

type ownedAccountDuplicateKey struct {
	Name  string
	Value string
}

type AccountListFilters struct {
	Platform    string
	AccountType string
	Status      string
	Search      string
	GroupID     int64
	ProxyID     int64
	PrivacyMode string
}

type BulkUpdateOwnedAccountsInput struct {
	AccountIDs   []int64
	Concurrency  *int
	Priority     *int
	LoadFactor   *int
	Status       string
	Schedulable  *bool
	AccountLevel *string
	ShareMode    *string
	GroupIDs     *[]int64
	Credentials  map[string]any
	Extra        map[string]any
}

// NewAccountService 创建账号服务实例
func NewAccountService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
) *AccountService {
	return &AccountService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		userRepo:    userRepo,
		userSubRepo: userSubRepo,
	}
}

func (s *AccountService) SetUserPrivateGroupProvisioner(provisioner UserPrivateGroupProvisioner) {
	if s == nil {
		return
	}
	s.privateGroupProvisioner = provisioner
}

func (s *AccountService) SetAccountSharePolicyRepository(repo AccountSharePolicyRepository) {
	if s == nil {
		return
	}
	s.accountSharePolicyRepo = repo
}

func (s *AccountService) SetSystemNoticeService(noticeService *SystemNoticeService) {
	if s == nil {
		return
	}
	s.systemNoticeService = noticeService
}

// Create 创建账号
func (s *AccountService) Create(ctx context.Context, req CreateAccountRequest) (*Account, error) {
	extra, err := NormalizeCodexQuotaLimitExtra(req.Platform, req.Type, req.Extra)
	if err != nil {
		return nil, err
	}
	req.Extra = extra

	// 验证分组是否存在（如果指定了分组）
	if len(req.GroupIDs) > 0 {
		if err := s.validateGroupIDsExist(ctx, req.GroupIDs); err != nil {
			return nil, err
		}
	}

	// 创建账号
	account := &Account{
		Name:         req.Name,
		Notes:        normalizeAccountNotes(req.Notes),
		Platform:     req.Platform,
		AccountLevel: NormalizeOpenAIAccountLevel(req.Platform, req.AccountLevel, req.Credentials, req.Extra),
		Type:         req.Type,
		Credentials:  req.Credentials,
		Extra:        req.Extra,
		ShareMode:    NormalizeAccountShareMode(req.ShareMode),
		ProxyID:      req.ProxyID,
		Concurrency:  req.Concurrency,
		LoadFactor:   normalizeLoadFactor(req.LoadFactor),
		Priority:     req.Priority,
		Status:       StatusActive,
		ExpiresAt:    req.ExpiresAt,
	}
	if req.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *req.AutoPauseOnExpired
	} else {
		account.AutoPauseOnExpired = true
	}
	concurrency, err := NormalizeOpenAIPlusConcurrency(account.Platform, account.AccountLevel, account.Concurrency)
	if err != nil {
		return nil, err
	}
	account.Concurrency = concurrency
	if err := ValidateAccountLoadFactor(account.LoadFactor); err != nil {
		return nil, err
	}

	// require_oauth_only 检查：apikey 类型账号不可加入限制分组
	if requiresOAuthOnlyGroupCheck(account.Type) && len(req.GroupIDs) > 0 {
		for _, gid := range req.GroupIDs {
			g, err := s.groupRepo.GetByID(ctx, gid)
			if err != nil {
				return nil, err
			}
			if isOAuthOnlyGroup(g) {
				return nil, fmt.Errorf("group [%s] only allows OAuth accounts", g.Name)
			}
		}
	}

	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}

	// 绑定分组
	if len(req.GroupIDs) > 0 {
		if err := s.accountRepo.BindGroups(ctx, account.ID, req.GroupIDs); err != nil {
			return nil, fmt.Errorf("bind groups: %w", err)
		}
		account.GroupIDs = append([]int64(nil), req.GroupIDs...)
	}

	s.notifyAccountCreated(ctx, account)
	return account, nil
}

// GetByID 根据ID获取账号
func (s *AccountService) GetByID(ctx context.Context, id int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return account, nil
}

// List 获取账号列表
func (s *AccountService) List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	accounts, pagination, err := s.accountRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list accounts: %w", err)
	}
	return accounts, pagination, nil
}

func (s *AccountService) ListOwned(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, filters AccountListFilters) ([]Account, *pagination.PaginationResult, error) {
	if ownerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	repo, ok := s.accountRepo.(ownedAccountFilterRepository)
	if !ok {
		return nil, nil, fmt.Errorf("owned account listing is not supported by repository")
	}
	accounts, result, err := repo.ListOwnedWithFilters(ctx, ownerUserID, params, filters.Platform, filters.AccountType, filters.Status, filters.Search, filters.GroupID, filters.ProxyID, filters.PrivacyMode)
	if err != nil {
		return nil, nil, fmt.Errorf("list owned accounts: %w", err)
	}
	return accounts, result, nil
}

func (s *AccountService) GetOwnedByID(ctx context.Context, ownerUserID, accountID int64) (*Account, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

func (s *AccountService) EnsureOwnedAccountCanEnterPublicShare(ctx context.Context, ownerUserID, accountID int64) error {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return err
	}
	return s.ensureAccountCanEnterPublicShare(ctx, account)
}

func (s *AccountService) CreateOwned(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*Account, error) {
	return s.createOwned(ctx, ownerUserID, req)
}

func (s *AccountService) ImportOwned(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*Account, error) {
	return s.createOwned(ctx, ownerUserID, req)
}

func (s *AccountService) createOwned(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*Account, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	targetLevel := NormalizeAccountLevel(req.AccountLevel)
	preserveProxy := req.Platform == PlatformOpenAI && RequiresUserOpenAIProxyLogin(targetLevel)
	proxyID := req.ProxyID
	if err := applyOwnedPersonalAccountTemplateToCreate(&req); err != nil {
		return nil, err
	}
	if req.Platform == PlatformOpenAI {
		if !IsUserSelectableOpenAIAccountLevel(targetLevel) {
			return nil, ErrOwnedOpenAIAccountLevelRequired
		}
		if preserveProxy {
			if proxyID == nil || *proxyID <= 0 {
				return nil, ErrOwnedOpenAIAccountProxyRequired
			}
			req.ProxyID = proxyID
		}
		req.AccountLevel = targetLevel
	} else {
		req.AccountLevel = AccountLevelUnknown
	}
	if err := validateOwnedAccountSource(req.Type, req.Credentials, req.Extra); err != nil {
		return nil, err
	}
	extra, err := NormalizeCodexQuotaLimitExtra(req.Platform, req.Type, req.Extra)
	if err != nil {
		return nil, err
	}
	req.Extra = extra
	shareMode := NormalizeAccountShareMode(req.ShareMode)
	groupIDs, err := s.initialOwnedAccountGroupIDs(ctx, ownerUserID, req.Platform, req.Type, shareMode, req.GroupIDs)
	if err != nil {
		return nil, err
	}

	shareStatus := AccountShareStatusApproved
	if shareMode == AccountShareModePublic {
		shareStatus = AccountShareStatusPending
	}

	accountLevel, err := resolveOwnedOpenAIAccountLevel(req.Platform, targetLevel, req.Credentials, req.Extra)
	if err != nil {
		return nil, err
	}

	account := &Account{
		Name:                  req.Name,
		Notes:                 normalizeAccountNotes(req.Notes),
		Platform:              req.Platform,
		AccountLevel:          accountLevel,
		Type:                  req.Type,
		Credentials:           req.Credentials,
		Extra:                 req.Extra,
		OwnerUserID:           &ownerUserID,
		ShareMode:             shareMode,
		ShareStatus:           shareStatus,
		ProxyID:               req.ProxyID,
		Concurrency:           req.Concurrency,
		LoadFactor:            normalizeLoadFactor(req.LoadFactor),
		LoadFactorPaidCeiling: OwnedPersonalDefaultLoadFactor,
		Priority:              req.Priority,
		Status:                StatusActive,
		ExpiresAt:             req.ExpiresAt,
		AutoPauseOnExpired:    true,
		Schedulable:           true,
	}
	if req.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *req.AutoPauseOnExpired
	}
	concurrency, err := NormalizeOpenAIPlusConcurrency(account.Platform, account.AccountLevel, account.Concurrency)
	if err != nil {
		return nil, err
	}
	account.Concurrency = concurrency
	if err := ValidateAccountLoadFactor(account.LoadFactor); err != nil {
		return nil, err
	}
	if err := s.ensureOwnedAccountNotDuplicate(ctx, ownerUserID, account, 0); err != nil {
		return nil, err
	}

	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	if len(groupIDs) > 0 {
		if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
			return nil, fmt.Errorf("bind groups: %w", err)
		}
		account.GroupIDs = append([]int64(nil), groupIDs...)
	}
	s.notifyAccountCreated(ctx, account)
	return account, nil
}

func isAllowedOwnedAccountType(accountType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(accountType))
	return normalized == AccountTypeOAuth
}

func validateOwnedAccountSource(accountType string, credentials, extra map[string]any) error {
	if !isAllowedOwnedAccountType(accountType) {
		return ErrOwnedAccountTypeNotAllowed
	}
	if !hasNonEmptyStringField(credentials, "access_token") {
		return ErrOwnedAccountCredentialsInvalid
	}
	if field, ok := findDisallowedOwnedAccountField(credentials); ok {
		return ErrOwnedAccountCredentialsNotAllowed.WithMetadata(map[string]string{
			"section": "credentials",
			"field":   field,
		})
	}
	if field, ok := findDisallowedOwnedAccountField(extra); ok {
		return ErrOwnedAccountCredentialsNotAllowed.WithMetadata(map[string]string{
			"section": "extra",
			"field":   field,
		})
	}
	return nil
}

func resolveOwnedOpenAIAccountLevel(platform, targetLevel string, credentials, extra map[string]any) (string, error) {
	if platform != PlatformOpenAI {
		return AccountLevelUnknown, nil
	}

	target := NormalizeAccountLevel(targetLevel)
	if !IsUserSelectableOpenAIAccountLevel(target) {
		return "", ErrOwnedOpenAIAccountLevelRequired
	}

	actual := InferOpenAIAccountLevel(credentials, extra)
	if !IsConcreteAccountLevel(actual) {
		actual = AccountLevelFree
		if target == AccountLevelFree {
			return actual, nil
		}
		return "", infraerrors.BadRequest(
			"OWNED_OPENAI_ACCOUNT_LEVEL_UNCONFIRMED",
			"无法确认账号等级，已自动分配到FREE分组或请通过账号登录",
		).WithMetadata(map[string]string{
			"target_level": target,
			"actual_level": actual,
		})
	}

	actual = NormalizeAccountLevel(actual)
	if actual != target {
		return "", infraerrors.BadRequest(
			"OWNED_OPENAI_ACCOUNT_LEVEL_MISMATCH",
			fmt.Sprintf("账号等级不匹配：选择的是 %s，实际识别为 %s", target, actual),
		).WithMetadata(map[string]string{
			"target_level": target,
			"actual_level": actual,
		})
	}
	return actual, nil
}

func hasNonEmptyStringField(values map[string]any, key string) bool {
	if len(values) == 0 {
		return false
	}
	value, ok := values[key]
	if !ok {
		return false
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func findDisallowedOwnedAccountField(values map[string]any) (string, bool) {
	return findDisallowedCredentialContent(values, credentialSafetyOptions{
		AllowOAuthTokenValues:  true,
		AllowOAuthMetadataURLs: true,
	})
}

func normalizeLoadFactor(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	normalized := *value
	return &normalized
}

func ownedPersonalDefaultModelMapping(platform string) map[string]any {
	models := make([]string, 0)
	switch platform {
	case PlatformOpenAI:
		models = append(models, openai.DefaultModelIDs()...)
		models = append(models, "gpt-5.2-2025-12-11", "gpt-5.2-chat-latest", "gpt-5.2-pro", "gpt-5.2-pro-2025-12-11", "gpt-4o-audio-preview", "gpt-4o-realtime-preview")
	case PlatformAnthropic:
		models = append(models, claude.DefaultModelIDs()...)
		models = append(models, "claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20240620", "claude-3-5-haiku-20241022", "claude-3-7-sonnet-20250219", "claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-opus-4-1-20250805")
	case PlatformGemini:
		for _, model := range geminicli.DefaultModels {
			models = append(models, model.ID)
		}
	case PlatformAntigravity:
		for _, model := range antigravity.DefaultModels() {
			models = append(models, model.ID)
		}
	}
	if len(models) == 0 {
		return map[string]any{}
	}
	mapping := make(map[string]any, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || strings.Contains(model, "*") {
			continue
		}
		mapping[model] = model
	}
	return mapping
}

func applyOwnedPersonalAccountTemplateToMaps(platform string, credentials, extra map[string]any) (map[string]any, map[string]any) {
	nextCredentials := make(map[string]any, len(credentials)+1)
	for key, value := range credentials {
		nextCredentials[key] = value
	}
	nextCredentials["model_mapping"] = ownedPersonalDefaultModelMapping(platform)
	delete(nextCredentials, "compact_model_mapping")

	nextExtra := make(map[string]any, len(extra)+6)
	for key, value := range extra {
		nextExtra[key] = value
	}
	if platform == PlatformOpenAI {
		nextExtra["openai_oauth_responses_websockets_v2_mode"] = ownedPersonalDefaultOpenAIWSMode
		nextExtra["openai_oauth_responses_websockets_v2_enabled"] = false
		nextExtra["openai_passthrough"] = false
		nextExtra["openai_oauth_passthrough"] = false
		nextExtra["codex_cli_only"] = false
		nextExtra["openai_compact_mode"] = ownedPersonalDefaultOpenAICompactMode
		delete(nextExtra, "responses_websockets_v2_enabled")
		delete(nextExtra, "openai_ws_enabled")
	}
	return nextCredentials, nextExtra
}

func normalizeOwnedPersonalAccountConcurrency(concurrency int) int {
	if concurrency <= 0 {
		return ownedPersonalDefaultConcurrency
	}
	return concurrency
}

func validateOwnedPersonalAccountConcurrency(concurrency int) error {
	if concurrency < ownedPersonalMinConcurrency || concurrency > ownedPersonalMaxConcurrency {
		return ErrOwnedAccountConcurrencyOutOfRange
	}
	return nil
}

func validateOwnedPersonalAccountLoadFactor(loadFactor int) error {
	if loadFactor <= 0 || loadFactor > AccountMaxLoadFactor {
		return ErrOwnedAccountLoadFactorOutOfRange
	}
	return nil
}

func applyOwnedPersonalAccountTemplateToCreate(req *CreateAccountRequest) error {
	if req == nil {
		return nil
	}
	req.Concurrency = normalizeOwnedPersonalAccountConcurrency(req.Concurrency)
	if err := validateOwnedPersonalAccountConcurrency(req.Concurrency); err != nil {
		return err
	}
	loadFactor := OwnedPersonalDefaultLoadFactor
	req.LoadFactor = &loadFactor
	if req.Priority <= 0 {
		req.Priority = ownedPersonalDefaultPriority
	}
	autoPause := true
	req.AutoPauseOnExpired = &autoPause
	req.GroupIDs = nil
	req.ProxyID = nil
	req.Credentials, req.Extra = applyOwnedPersonalAccountTemplateToMaps(req.Platform, req.Credentials, req.Extra)
	return nil
}

func sanitizeOwnedPersonalAccountUpdate(account *Account, req *UpdateAccountRequest) error {
	if account == nil || req == nil {
		return nil
	}
	req.GroupIDs = nil
	req.ProxyID = nil
	if req.Concurrency != nil {
		if err := validateOwnedPersonalAccountConcurrency(*req.Concurrency); err != nil {
			return err
		}
	}
	if req.LoadFactor != nil {
		if err := validateOwnedPersonalAccountLoadFactor(*req.LoadFactor); err != nil {
			return err
		}
	}
	req.AutoPauseOnExpired = nil
	if req.Priority != nil && *req.Priority <= 0 {
		priority := ownedPersonalDefaultPriority
		req.Priority = &priority
	}
	if req.Credentials != nil {
		nextCredentials := mergeAccountMap(nil, *req.Credentials)
		if nextCredentials == nil {
			nextCredentials = map[string]any{}
		}
		preserveOwnedPersonalCredentialPolicy(account, nextCredentials)
		req.Credentials = &nextCredentials
	}
	if req.Extra != nil {
		nextExtra := mergeAccountMap(nil, *req.Extra)
		if nextExtra == nil {
			nextExtra = map[string]any{}
		}
		preserveOwnedPersonalExtraPolicy(account, nextExtra)
		req.Extra = &nextExtra
	}
	return nil
}

var ownedPersonalLockedCredentialKeys = []string{
	"model_mapping",
	"compact_model_mapping",
}

var ownedPersonalLockedOpenAIExtraKeys = []string{
	"openai_oauth_responses_websockets_v2_mode",
	"openai_oauth_responses_websockets_v2_enabled",
	"openai_apikey_responses_websockets_v2_mode",
	"openai_apikey_responses_websockets_v2_enabled",
	"responses_websockets_v2_enabled",
	"openai_ws_enabled",
	"openai_passthrough",
	"openai_oauth_passthrough",
	"openai_compact_mode",
	"codex_cli_only",
}

func preserveOwnedPersonalCredentialPolicy(account *Account, nextCredentials map[string]any) {
	if account == nil || nextCredentials == nil {
		return
	}
	for _, key := range ownedPersonalLockedCredentialKeys {
		preserveMapKey(account.Credentials, nextCredentials, key)
	}
}

func preserveOwnedPersonalExtraPolicy(account *Account, nextExtra map[string]any) {
	if account == nil || nextExtra == nil || account.Platform != PlatformOpenAI {
		return
	}
	for _, key := range ownedPersonalLockedOpenAIExtraKeys {
		preserveMapKey(account.Extra, nextExtra, key)
	}
}

func preserveMapKey(current map[string]any, next map[string]any, key string) {
	if value, exists := current[key]; exists {
		next[key] = value
		return
	}
	delete(next, key)
}

func NormalizeCodexQuotaLimitExtra(platform, accountType string, extra map[string]any) (map[string]any, error) {
	if len(extra) == 0 {
		return extra, nil
	}
	next := extra
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
		delete(next, "codex_5h_limit_percent")
		delete(next, "codex_7d_limit_percent")
		return next, nil
	}
	for _, key := range []string{"codex_5h_limit_percent", "codex_7d_limit_percent"} {
		value, ok, err := normalizeCodexQuotaLimitPercentValue(next[key])
		if err != nil {
			return nil, err
		}
		if !ok || value == CodexQuotaDefaultLimitPercent {
			delete(next, key)
			continue
		}
		next[key] = value
	}
	return next, nil
}

func normalizeCodexQuotaLimitPercentValue(raw any) (float64, bool, error) {
	if raw == nil {
		return 0, false, nil
	}
	if s, ok := raw.(string); ok && strings.TrimSpace(s) == "" {
		return 0, false, nil
	}
	value := parseExtraFloat64(raw)
	if value < CodexQuotaMinLimitPercent || value > CodexQuotaMaxLimitPercent {
		return 0, false, ErrCodexQuotaLimitPercentInvalid
	}
	return value, true, nil
}

func NormalizeCodexQuotaLimitBulkExtra(accounts []*Account, extra map[string]any) error {
	if !hasCodexQuotaLimitExtraKeys(extra) {
		return nil
	}
	for _, account := range accounts {
		if account == nil {
			continue
		}
		if !account.IsOpenAIOAuth() {
			return ErrCodexQuotaLimitPercentInvalid
		}
	}
	for _, key := range []string{"codex_5h_limit_percent", "codex_7d_limit_percent"} {
		raw, ok := extra[key]
		if !ok {
			continue
		}
		value, exists, err := normalizeCodexQuotaLimitPercentValue(raw)
		if err != nil {
			return err
		}
		if !exists {
			extra[key] = CodexQuotaDefaultLimitPercent
			continue
		}
		extra[key] = value
	}
	return nil
}

func hasCodexQuotaLimitExtraKeys(extra map[string]any) bool {
	if len(extra) == 0 {
		return false
	}
	_, has5h := extra["codex_5h_limit_percent"]
	_, has7d := extra["codex_7d_limit_percent"]
	return has5h || has7d
}

// ListByPlatform 根据平台获取账号列表
func (s *AccountService) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	accounts, err := s.accountRepo.ListByPlatform(ctx, platform)
	if err != nil {
		return nil, fmt.Errorf("list accounts by platform: %w", err)
	}
	return accounts, nil
}

// ListByGroup 根据分组获取账号列表
func (s *AccountService) ListByGroup(ctx context.Context, groupID int64) ([]Account, error) {
	accounts, err := s.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list accounts by group: %w", err)
	}
	return accounts, nil
}

// Update 更新账号
func (s *AccountService) Update(ctx context.Context, id int64, req UpdateAccountRequest) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	before := cloneAccountForNotice(account)

	// 更新字段
	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Notes != nil {
		account.Notes = normalizeAccountNotes(req.Notes)
	}

	if req.Credentials != nil {
		account.Credentials = MergePreservingSensitiveCreds(account.Credentials, *req.Credentials)
	}

	if req.Extra != nil {
		extra, err := NormalizeCodexQuotaLimitExtra(account.Platform, account.Type, *req.Extra)
		if err != nil {
			return nil, err
		}
		account.Extra = extra
	}
	if req.AccountLevel != nil {
		account.AccountLevel = NormalizeAccountLevel(*req.AccountLevel)
	}

	if req.ProxyID != nil {
		account.ProxyID = req.ProxyID
	}

	if req.Concurrency != nil {
		account.Concurrency = *req.Concurrency
	}

	if req.LoadFactor != nil {
		account.LoadFactor = normalizeLoadFactor(req.LoadFactor)
	}
	account.AccountLevel = NormalizeOpenAIAccountLevel(account.Platform, account.AccountLevel, account.Credentials, account.Extra)
	if err := ValidateOpenAIPlusConcurrency(account.Platform, account.AccountLevel, account.Concurrency); err != nil {
		return nil, err
	}
	if err := ValidateAccountLoadFactor(account.LoadFactor); err != nil {
		return nil, err
	}

	if req.Priority != nil {
		account.Priority = *req.Priority
	}

	if req.Status != nil {
		account.Status = *req.Status
	}
	if req.Schedulable != nil {
		account.Schedulable = *req.Schedulable
	}
	if req.ClearExpiresAt {
		account.ExpiresAt = nil
	} else if req.ExpiresAt != nil {
		account.ExpiresAt = req.ExpiresAt
	}
	if req.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *req.AutoPauseOnExpired
	}
	if req.ShareMode != nil {
		account.ShareMode = NormalizeAccountShareMode(*req.ShareMode)
	}

	// 先验证分组是否存在（在任何写操作之前）
	if req.GroupIDs != nil {
		if err := s.validateGroupIDsExist(ctx, *req.GroupIDs); err != nil {
			return nil, err
		}
	}

	// require_oauth_only 检查必须在任何写操作前完成，避免账号已更新但分组绑定失败。
	if req.GroupIDs != nil && requiresOAuthOnlyGroupCheck(account.Type) {
		for _, gid := range *req.GroupIDs {
			g, err := s.groupRepo.GetByID(ctx, gid)
			if err != nil {
				return nil, err
			}
			if isOAuthOnlyGroup(g) {
				return nil, fmt.Errorf("group [%s] only allows OAuth accounts", g.Name)
			}
		}
	}

	// 执行更新
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}

	// 绑定分组
	if req.GroupIDs != nil {
		if err := s.accountRepo.BindGroups(ctx, account.ID, *req.GroupIDs); err != nil {
			return nil, fmt.Errorf("bind groups: %w", err)
		}
		account.GroupIDs = append([]int64(nil), *req.GroupIDs...)
	}

	s.notifyAccountChanged(ctx, before, account)
	return account, nil
}

func (s *AccountService) UpdateOwned(ctx context.Context, ownerUserID, accountID int64, req UpdateAccountRequest) (*Account, error) {
	if req.AccountLevel != nil {
		return nil, ErrOwnedAccountLevelNotAllowed
	}
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	before := cloneAccountForNotice(account)
	if err := sanitizeOwnedPersonalAccountUpdate(account, &req); err != nil {
		return nil, err
	}

	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Notes != nil {
		account.Notes = normalizeAccountNotes(req.Notes)
	}
	if req.Credentials != nil {
		account.Credentials = MergePreservingSensitiveCreds(account.Credentials, *req.Credentials)
	}
	if req.Extra != nil {
		extra, err := NormalizeCodexQuotaLimitExtra(account.Platform, account.Type, *req.Extra)
		if err != nil {
			return nil, err
		}
		account.Extra = extra
	}
	if req.ProxyID != nil {
		account.ProxyID = req.ProxyID
	}
	if req.Concurrency != nil {
		account.Concurrency = *req.Concurrency
	}
	if req.LoadFactor != nil {
		account.LoadFactor = normalizeLoadFactor(req.LoadFactor)
	}
	account.AccountLevel = NormalizeOpenAIAccountLevel(account.Platform, account.AccountLevel, account.Credentials, account.Extra)
	if err := ValidateOpenAIPlusConcurrency(account.Platform, account.AccountLevel, account.Concurrency); err != nil {
		return nil, err
	}
	if err := ValidateAccountLoadFactor(account.LoadFactor); err != nil {
		return nil, err
	}
	if req.Priority != nil {
		account.Priority = *req.Priority
	}
	if req.Status != nil {
		switch *req.Status {
		case StatusActive, StatusDisabled:
			account.Status = *req.Status
		default:
			return nil, fmt.Errorf("invalid account status: %s", *req.Status)
		}
	}
	if req.Schedulable != nil {
		account.Schedulable = *req.Schedulable
	}
	if req.ClearExpiresAt {
		account.ExpiresAt = nil
	} else if req.ExpiresAt != nil {
		account.ExpiresAt = req.ExpiresAt
	}
	if req.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *req.AutoPauseOnExpired
	}
	shouldBindGroups := false
	var groupIDs []int64
	if req.ShareMode != nil {
		nextMode := NormalizeAccountShareMode(*req.ShareMode)
		managedGroupIDs, err := s.managedOwnedAccountGroupIDsForShareMode(ctx, ownerUserID, account, nextMode)
		if err != nil {
			return nil, err
		}
		if nextMode == AccountShareModePrivate {
			account.ShareMode = AccountShareModePrivate
			account.ShareStatus = AccountShareStatusApproved
			account.ErrorMessage = ""
		} else if account.IsPublicShareApproved() {
			account.ShareMode = AccountShareModePublic
		} else {
			account.ShareMode = AccountShareModePublic
			account.ShareStatus = AccountShareStatusPending
		}
		groupIDs = managedGroupIDs
		shouldBindGroups = true
	}
	if err := validateOwnedAccountSource(account.Type, account.Credentials, account.Extra); err != nil {
		return nil, err
	}
	if req.Credentials != nil || req.Extra != nil {
		if err := s.ensureOwnedAccountNotDuplicate(ctx, ownerUserID, account, account.ID); err != nil {
			return nil, err
		}
	}

	if !shouldBindGroups && req.GroupIDs != nil {
		return nil, ErrGroupNotAllowed
	}
	if !shouldBindGroups && account.IsPublicShareApproved() && (req.AccountLevel != nil || req.Credentials != nil || req.Extra != nil) {
		publicGroup, err := s.resolveOwnedPublicShareGroup(ctx, account)
		if err != nil {
			return nil, err
		}
		groupIDs, err = s.publicOwnedAccountGroupIDs(ctx, ownerUserID, account, publicGroup)
		if err != nil {
			return nil, err
		}
		shouldBindGroups = true
	}
	if req.LoadFactor != nil {
		repo, ok := s.accountRepo.(ownedLoadFactorCreditAccountRepository)
		if !ok {
			return nil, ErrOwnedAccountLoadFactorCreditsUnavailable
		}
		account, err = repo.UpdateOwnedAccountWithLoadFactorCredits(ctx, ownerUserID, account)
		if err != nil {
			return nil, fmt.Errorf("update account: %w", err)
		}
	} else if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	if shouldBindGroups {
		if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
			return nil, fmt.Errorf("bind groups: %w", err)
		}
		account.GroupIDs = append([]int64(nil), groupIDs...)
	}
	s.notifyAccountChanged(ctx, before, account)
	return account, nil
}

func (s *AccountService) SetOwnedOpenAIAccountLevel(ctx context.Context, ownerUserID, accountID int64, accountLevel, reason string) (*Account, error) {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil, infraerrors.BadRequest("OWNED_ACCOUNT_LEVEL_UNSUPPORTED", "account level verification only supports OpenAI OAuth accounts")
	}

	level := NormalizeAccountLevel(accountLevel)
	if level != AccountLevelFree && level != AccountLevelPlus {
		return nil, infraerrors.BadRequest("OWNED_ACCOUNT_LEVEL_INVALID", "user accounts can only set free or plus levels")
	}
	if err := validateOwnedAccountSource(account.Type, account.Credentials, account.Extra); err != nil {
		return nil, err
	}

	before := cloneAccountForNotice(account)
	account.AccountLevel = level
	if level == AccountLevelPlus {
		concurrency, err := NormalizeOpenAIPlusConcurrency(account.Platform, account.AccountLevel, account.Concurrency)
		if err != nil {
			return nil, err
		}
		account.Concurrency = concurrency
	}
	if err := ValidateOpenAIPlusConcurrency(account.Platform, account.AccountLevel, account.Concurrency); err != nil {
		return nil, err
	}

	shouldBindGroups := false
	var groupIDs []int64
	if account.IsPublicShareApproved() {
		publicGroup, err := s.resolveOwnedPublicShareGroup(ctx, account)
		if err == nil {
			groupIDs, err = s.publicOwnedAccountGroupIDs(ctx, ownerUserID, account, publicGroup)
			if err != nil {
				return nil, err
			}
			shouldBindGroups = true
		} else if level == AccountLevelFree {
			groupIDs, err = s.initialOwnedAccountGroupIDs(ctx, ownerUserID, account.Platform, account.Type, account.ShareMode, nil)
			if err != nil {
				return nil, err
			}
			account.ShareStatus = AccountShareStatusSuspended
			account.ErrorMessage = strings.TrimSpace(reason)
			if account.ErrorMessage == "" {
				account.ErrorMessage = "OpenAI account level was changed to free and no compatible public sharing pool is available"
			}
			shouldBindGroups = true
		} else {
			return nil, err
		}
	}

	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update owned OpenAI account level: %w", err)
	}
	if shouldBindGroups {
		if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
			return nil, fmt.Errorf("bind account groups after level update: %w", err)
		}
		account.GroupIDs = append([]int64(nil), groupIDs...)
	}
	s.notifyAccountChanged(ctx, before, account)
	return account, nil
}

func (s *AccountService) DeleteOwned(ctx context.Context, ownerUserID, accountID int64) error {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return err
	}
	if err := s.accountRepo.Delete(ctx, accountID); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	s.notifyAccountDeleted(ctx, account)
	return nil
}

// Delete 删除账号
// 优化：使用 ExistsByID 替代 GetByID 进行存在性检查，
// 避免加载完整账号对象及其关联数据，提升删除操作的性能
func (s *AccountService) BulkDeleteOwned(ctx context.Context, ownerUserID int64, accountIDs []int64) (*BulkUpdateAccountsResult, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	ids := normalizeOwnedBulkAccountIDs(accountIDs)
	result := &BulkUpdateAccountsResult{
		SuccessIDs: make([]int64, 0, len(ids)),
		FailedIDs:  make([]int64, 0, len(ids)),
		Results:    make([]BulkUpdateAccountResult, 0, len(ids)),
	}
	for _, accountID := range ids {
		entry := BulkUpdateAccountResult{AccountID: accountID}
		if err := s.DeleteOwned(ctx, ownerUserID, accountID); err != nil {
			entry.Error = err.Error()
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, accountID)
		} else {
			entry.Success = true
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, accountID)
		}
		result.Results = append(result.Results, entry)
	}
	return result, nil
}

func normalizeOwnedBulkAccountIDs(ids []int64) []int64 {
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
	return out
}

func normalizeOwnedBulkStatus(status string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized == "" {
		return "", nil
	}
	if normalized == "inactive" {
		normalized = StatusDisabled
	}
	switch normalized {
	case StatusActive, StatusDisabled:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid account status: %s", status)
	}
}

func mergeAccountMap(current map[string]any, updates map[string]any) map[string]any {
	if len(current) == 0 && len(updates) == 0 {
		return nil
	}
	next := make(map[string]any, len(current)+len(updates))
	for key, value := range current {
		next[key] = value
	}
	for key, value := range updates {
		next[key] = value
	}
	return next
}

func mergeAccountMapPreservingSensitiveCreds(current map[string]any, updates map[string]any) map[string]any {
	if len(updates) == 0 {
		return mergeAccountMap(current, updates)
	}
	return MergePreservingSensitiveCreds(current, mergeAccountMap(current, updates))
}

func accountDuplicateIdentityKeys(account *Account) []ownedAccountDuplicateKey {
	if account == nil {
		return nil
	}
	keys := make([]ownedAccountDuplicateKey, 0, 3)
	add := func(name, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		keys = append(keys, ownedAccountDuplicateKey{Name: name, Value: value})
	}
	addFolded := func(name, value string) {
		add(name, strings.ToLower(strings.TrimSpace(value)))
	}
	switch account.Platform {
	case PlatformOpenAI:
		if account.Type != AccountTypeOAuth {
			return nil
		}
		orgID := strings.ToLower(strings.TrimSpace(account.GetOpenAIOrganizationID()))
		chatGPTUserID := account.GetChatGPTUserID()
		chatGPTAccountID := account.GetChatGPTAccountID()
		if orgID != "" {
			if strings.TrimSpace(chatGPTUserID) != "" {
				add("openai.org_user", orgID+"|"+strings.TrimSpace(chatGPTUserID))
			} else if strings.TrimSpace(chatGPTAccountID) != "" {
				add("openai.org_account", orgID+"|"+strings.TrimSpace(chatGPTAccountID))
			}
		} else if strings.TrimSpace(chatGPTUserID) != "" {
			add("openai.chatgpt_user_id", chatGPTUserID)
		} else {
			add("openai.chatgpt_account_id", chatGPTAccountID)
		}
		if len(keys) == 0 {
			addFolded("openai.email", account.GetCredential("email"))
		}
	case PlatformAnthropic:
		if account.Type != AccountTypeOAuth {
			return nil
		}
		orgUUID := strings.ToLower(strings.TrimSpace(account.GetClaudeOrgUUID()))
		accountUUID := strings.ToLower(strings.TrimSpace(account.GetClaudeAccountUUID()))
		if orgUUID != "" && accountUUID != "" {
			add("anthropic.org_account", orgUUID+"|"+accountUUID)
		} else if accountUUID != "" {
			add("anthropic.account_uuid", accountUUID)
		} else {
			add("anthropic.org_uuid", orgUUID)
		}
		if len(keys) == 0 {
			addFolded("anthropic.email_address", account.GetCredential("email_address"))
		}
	case PlatformGemini:
		if account.Type != AccountTypeOAuth {
			return nil
		}
		projectID := strings.ToLower(strings.TrimSpace(account.GetCredential("project_id")))
		oauthType := strings.TrimSpace(account.GeminiOAuthType())
		if projectID != "" {
			if oauthType == "" {
				oauthType = "code_assist"
			}
			add("gemini.project", strings.ToLower(oauthType)+"|"+projectID)
		}
	case PlatformAntigravity:
		if account.Type != AccountTypeOAuth {
			return nil
		}
		addFolded("antigravity.project_id", account.GetCredential("project_id"))
		if len(keys) == 0 {
			addFolded("antigravity.email", account.GetCredential("email"))
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func duplicateOwnedAccountError(platform string, key ownedAccountDuplicateKey, existingAccountID int64) error {
	return ErrOwnedAccountAlreadyExists.WithMetadata(map[string]string{
		"platform":            platform,
		"identity":            key.Name,
		"existing_account_id": fmt.Sprintf("%d", existingAccountID),
	})
}

func (s *AccountService) ensureOwnedAccountNotDuplicate(ctx context.Context, ownerUserID int64, candidate *Account, skipAccountIDs ...int64) error {
	candidateKeys := accountDuplicateIdentityKeys(candidate)
	if len(candidateKeys) == 0 {
		return nil
	}
	skipIDs := make(map[int64]struct{}, len(skipAccountIDs))
	for _, id := range skipAccountIDs {
		if id > 0 {
			skipIDs[id] = struct{}{}
		}
	}
	repo, ok := s.accountRepo.(ownedAccountFilterRepository)
	if !ok {
		return ErrOwnedAccountGroupValidationUnavailable
	}
	page := 1
	for {
		accounts, result, err := repo.ListOwnedWithFilters(
			ctx,
			ownerUserID,
			pagination.PaginationParams{Page: page, PageSize: 1000, SortBy: "id", SortOrder: pagination.SortOrderAsc},
			candidate.Platform,
			candidate.Type,
			"",
			"",
			0,
			0,
			"",
		)
		if err != nil {
			return fmt.Errorf("check owned account duplicate: %w", err)
		}
		for i := range accounts {
			existing := &accounts[i]
			if _, ok := skipIDs[existing.ID]; ok {
				continue
			}
			existingKeys := accountDuplicateIdentityKeys(existing)
			for _, candidateKey := range candidateKeys {
				for _, existingKey := range existingKeys {
					if existingKey.Name == candidateKey.Name && existingKey.Value == candidateKey.Value {
						return duplicateOwnedAccountError(candidate.Platform, candidateKey, existing.ID)
					}
				}
			}
		}
		if result == nil || int64(page*1000) >= result.Total || len(accounts) == 0 {
			return nil
		}
		page++
	}
}

func ensureOwnedAccountBatchNotDuplicate(accounts []*Account) error {
	seen := make(map[ownedAccountDuplicateKey]int64)
	for _, account := range accounts {
		if account == nil {
			continue
		}
		for _, key := range accountDuplicateIdentityKeys(account) {
			if existingID, ok := seen[key]; ok && existingID != account.ID {
				return duplicateOwnedAccountError(account.Platform, key, existingID)
			}
			seen[key] = account.ID
		}
	}
	return nil
}

func (s *AccountService) BulkUpdateOwned(ctx context.Context, ownerUserID int64, input *BulkUpdateOwnedAccountsInput) (*BulkUpdateAccountsResult, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input == nil {
		return nil, ErrAccountNilInput
	}

	accountIDs := normalizeOwnedBulkAccountIDs(input.AccountIDs)
	result := &BulkUpdateAccountsResult{
		SuccessIDs: make([]int64, 0, len(accountIDs)),
		FailedIDs:  make([]int64, 0, len(accountIDs)),
		Results:    make([]BulkUpdateAccountResult, 0, len(accountIDs)),
	}
	if len(accountIDs) == 0 {
		return result, nil
	}

	if input.Concurrency != nil {
		if err := validateOwnedPersonalAccountConcurrency(*input.Concurrency); err != nil {
			return nil, err
		}
	}
	if input.Priority != nil && *input.Priority <= 0 {
		return nil, fmt.Errorf("priority must be > 0")
	}
	if input.LoadFactor != nil {
		if err := validateOwnedPersonalAccountLoadFactor(*input.LoadFactor); err != nil {
			return nil, err
		}
	}
	if input.GroupIDs != nil {
		return nil, ErrGroupNotAllowed
	}
	if input.AccountLevel != nil {
		return nil, ErrOwnedAccountLevelNotAllowed
	}
	status, err := normalizeOwnedBulkStatus(input.Status)
	if err != nil {
		return nil, err
	}
	shareMode := ""
	if input.ShareMode != nil {
		shareMode = NormalizeAccountShareMode(*input.ShareMode)
	}

	accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	accountsByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			accountsByID[account.ID] = account
		}
	}

	if input.Credentials == nil {
		input.Credentials = map[string]any{}
	}
	if input.Extra == nil {
		input.Extra = map[string]any{}
	}
	updatedIdentityAccounts := make([]*Account, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		account := accountsByID[accountID]
		if account == nil || account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
			return nil, ErrAccountNotFound
		}

		nextCredentials := mergeAccountMap(account.Credentials, input.Credentials)
		nextExtra := mergeAccountMap(account.Extra, input.Extra)
		nextCredentials, nextExtra = applyOwnedPersonalAccountTemplateToMaps(account.Platform, nextCredentials, nextExtra)
		nextExtra, err = NormalizeCodexQuotaLimitExtra(account.Platform, account.Type, nextExtra)
		if err != nil {
			return nil, err
		}
		nextAccount := *account
		nextAccount.Credentials = nextCredentials
		nextAccount.Extra = nextExtra
		if err := validateOwnedAccountSource(account.Type, nextCredentials, nextExtra); err != nil {
			return nil, err
		}
		nextConcurrency := normalizeOwnedPersonalAccountConcurrency(account.Concurrency)
		if input.Concurrency != nil {
			nextConcurrency = *input.Concurrency
		}
		nextLoadFactor := account.LoadFactor
		if input.LoadFactor != nil {
			nextLoadFactor = normalizeLoadFactor(input.LoadFactor)
		}
		nextAccountLevel := NormalizeOpenAIAccountLevel(account.Platform, account.AccountLevel, nextCredentials, nextExtra)
		if input.AccountLevel != nil {
			nextAccountLevel = NormalizeAccountLevel(*input.AccountLevel)
		}
		if err := ValidateOpenAIPlusConcurrency(account.Platform, nextAccountLevel, nextConcurrency); err != nil {
			return nil, err
		}
		if err := ValidateAccountLoadFactor(nextLoadFactor); err != nil {
			return nil, err
		}
		if len(input.Credentials) > 0 || len(input.Extra) > 0 {
			if err := s.ensureOwnedAccountNotDuplicate(ctx, ownerUserID, &nextAccount, accountIDs...); err != nil {
				return nil, err
			}
			updatedIdentityAccounts = append(updatedIdentityAccounts, &nextAccount)
		}
	}
	if err := ensureOwnedAccountBatchNotDuplicate(updatedIdentityAccounts); err != nil {
		return nil, err
	}

	requiresPerAccountUpdate := input.LoadFactor != nil || shareMode != "" || len(input.Credentials) > 0 || len(input.Extra) > 0
	if requiresPerAccountUpdate {
		for _, accountID := range accountIDs {
			account := accountsByID[accountID]
			entry := BulkUpdateAccountResult{AccountID: accountID}
			updateReq := UpdateAccountRequest{
				Concurrency:  input.Concurrency,
				LoadFactor:   input.LoadFactor,
				Priority:     input.Priority,
				Schedulable:  input.Schedulable,
				AccountLevel: input.AccountLevel,
			}
			if status != "" {
				updateReq.Status = &status
			}
			if shareMode != "" {
				updateReq.ShareMode = &shareMode
			}
			if len(input.Credentials) > 0 {
				credentials := mergeAccountMap(account.Credentials, input.Credentials)
				credentials, _ = applyOwnedPersonalAccountTemplateToMaps(account.Platform, credentials, account.Extra)
				updateReq.Credentials = &credentials
			}
			if len(input.Extra) > 0 {
				extra := mergeAccountMap(account.Extra, input.Extra)
				_, extra = applyOwnedPersonalAccountTemplateToMaps(account.Platform, account.Credentials, extra)
				extra, err = NormalizeCodexQuotaLimitExtra(account.Platform, account.Type, extra)
				if err != nil {
					entry.Error = err.Error()
					result.Failed++
					result.FailedIDs = append(result.FailedIDs, accountID)
					result.Results = append(result.Results, entry)
					continue
				}
				updateReq.Extra = &extra
			}
			if _, err := s.UpdateOwned(ctx, ownerUserID, accountID, updateReq); err != nil {
				entry.Error = err.Error()
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, accountID)
				result.Results = append(result.Results, entry)
				continue
			}
			entry.Success = true
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, accountID)
			result.Results = append(result.Results, entry)
		}
		return result, nil
	}

	repoUpdates := AccountBulkUpdate{
		Concurrency: input.Concurrency,
		Priority:    input.Priority,
		LoadFactor:  nil,
		Schedulable: input.Schedulable,
		Credentials: map[string]any{},
		Extra:       map[string]any{},
	}
	if input.AccountLevel != nil {
		level := NormalizeAccountLevel(*input.AccountLevel)
		repoUpdates.AccountLevel = &level
	}
	if status != "" {
		repoUpdates.Status = &status
	}

	updated, err := s.accountRepo.BulkUpdate(ctx, accountIDs, repoUpdates)
	if err != nil {
		return nil, fmt.Errorf("bulk update owned accounts: %w", err)
	}
	if updated != int64(len(accountIDs)) {
		return nil, ErrAccountNotFound
	}
	for _, accountID := range accountIDs {
		entry := BulkUpdateAccountResult{AccountID: accountID, Success: true}
		result.Success++
		result.SuccessIDs = append(result.SuccessIDs, accountID)
		result.Results = append(result.Results, entry)
	}
	s.notifyBulkOwnedAccountsChanged(ctx, accountsByID, accountIDs)

	return result, nil
}

func (s *AccountService) Delete(ctx context.Context, id int64) error {
	account, getErr := s.accountRepo.GetByID(ctx, id)
	if getErr != nil {
		exists, err := s.accountRepo.ExistsByID(ctx, id)
		if err != nil {
			return fmt.Errorf("check account: %w", err)
		}
		if !exists {
			return ErrAccountNotFound
		}
	} else if account == nil {
		return ErrAccountNotFound
	}

	if err := s.accountRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}

	s.notifyAccountDeleted(ctx, account)
	return nil
}

func (s *AccountService) validateGroupIDsExist(ctx context.Context, groupIDs []int64) error {
	if len(groupIDs) == 0 {
		return nil
	}
	if s.groupRepo == nil {
		return fmt.Errorf("group repository not configured")
	}

	if batchChecker, ok := s.groupRepo.(groupExistenceBatchChecker); ok {
		existsByID, err := batchChecker.ExistsByIDs(ctx, groupIDs)
		if err != nil {
			return fmt.Errorf("check groups exists: %w", err)
		}
		for _, groupID := range groupIDs {
			if groupID <= 0 {
				return fmt.Errorf("get group: %w", ErrGroupNotFound)
			}
			if !existsByID[groupID] {
				return fmt.Errorf("get group: %w", ErrGroupNotFound)
			}
		}
		return nil
	}

	for _, groupID := range groupIDs {
		_, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			return fmt.Errorf("get group: %w", err)
		}
	}
	return nil
}

func (s *AccountService) getPrivateGroupForOwnedAccount(ctx context.Context, ownerUserID int64, platform string) (*Group, error) {
	if s.privateGroupProvisioner == nil {
		return nil, ErrOwnedAccountGroupValidationUnavailable
	}
	group, err := s.privateGroupProvisioner.GetActiveUserPrivateGroup(ctx, ownerUserID, platform)
	if err == nil {
		return group, nil
	}
	if !errors.Is(err, ErrGroupNotFound) && !errors.Is(err, ErrGroupNotAllowed) {
		return nil, err
	}
	if provisionErr := s.privateGroupProvisioner.ProvisionUserPrivateGroups(ctx, ownerUserID); provisionErr != nil {
		return nil, provisionErr
	}
	group, err = s.privateGroupProvisioner.GetActiveUserPrivateGroup(ctx, ownerUserID, platform)
	if err != nil {
		return nil, err
	}
	return group, nil
}

func (s *AccountService) initialOwnedAccountGroupIDs(ctx context.Context, ownerUserID int64, platform, accountType, shareMode string, requestedGroupIDs []int64) ([]int64, error) {
	privateGroup, err := s.getPrivateGroupForOwnedAccount(ctx, ownerUserID, platform)
	if err != nil {
		return nil, err
	}
	return []int64{privateGroup.ID}, nil
}

func (s *AccountService) managedOwnedAccountGroupIDsForShareMode(ctx context.Context, ownerUserID int64, account *Account, nextMode string) ([]int64, error) {
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if NormalizeAccountShareMode(nextMode) == AccountShareModePublic {
		if err := s.ensureAccountCanEnterPublicShare(ctx, account); err != nil {
			return nil, err
		}
	}
	if NormalizeAccountShareMode(nextMode) == AccountShareModePublic && account.IsPublicShareApproved() {
		publicGroup, err := s.resolveOwnedPublicShareGroup(ctx, account)
		if err != nil {
			return nil, err
		}
		return s.publicOwnedAccountGroupIDs(ctx, ownerUserID, account, publicGroup)
	}
	return s.initialOwnedAccountGroupIDs(ctx, ownerUserID, account.Platform, account.Type, nextMode, nil)
}

func (s *AccountService) ensureAccountCanEnterPublicShare(ctx context.Context, account *Account) error {
	if account == nil {
		return ErrAccountNotFound
	}
	if account.AccountShareModeListingID != nil && *account.AccountShareModeListingID > 0 {
		return ErrOwnedAccountShareModeOnly
	}
	repo, ok := s.accountRepo.(accountShareModeListingAccountRepository)
	if !ok {
		return ErrOwnedAccountShareModeBoundaryUnavailable
	}
	isModeListingAccount, err := repo.IsAccountShareModeListingAccount(ctx, account.ID)
	if err != nil {
		return fmt.Errorf("check account share mode boundary: %w", err)
	}
	if isModeListingAccount {
		return ErrOwnedAccountShareModeOnly
	}
	return nil
}

func (s *AccountService) ApproveOwnedPublicShare(ctx context.Context, ownerUserID, accountID int64) (*Account, error) {
	return s.ApproveOwnedPublicShareWithOptions(ctx, ownerUserID, accountID, OwnedPublicShareApprovalOptions{})
}

func (s *AccountService) ApproveOwnedPublicShareWithOptions(ctx context.Context, ownerUserID, accountID int64, opts OwnedPublicShareApprovalOptions) (*Account, error) {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	before := cloneAccountForNotice(account)
	if err := validateOwnedAccountSource(account.Type, account.Credentials, account.Extra); err != nil {
		return nil, err
	}
	if err := s.ensureAccountCanEnterPublicShare(ctx, account); err != nil {
		return nil, err
	}
	if !isOwnedAccountPublicShareApprovable(account, opts.AllowRateLimited) {
		return nil, ErrOwnedAccountPublicValidationFailed.WithMetadata(map[string]string{
			"reason": "account is not active or schedulable",
		})
	}

	publicGroup, err := s.resolveOwnedPublicShareGroup(ctx, account)
	if err != nil {
		return nil, err
	}
	if err := s.validateOwnedPublicSharePolicy(ctx, account, publicGroup); err != nil {
		return nil, err
	}
	groupIDs, err := s.publicOwnedAccountGroupIDs(ctx, ownerUserID, account, publicGroup)
	if err != nil {
		return nil, err
	}

	account.ShareMode = AccountShareModePublic
	account.ShareStatus = AccountShareStatusApproved
	account.ErrorMessage = ""
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account public share status: %w", err)
	}
	if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
		return nil, fmt.Errorf("bind public account groups: %w", err)
	}
	account.GroupIDs = append([]int64(nil), groupIDs...)
	s.notifyAccountChanged(ctx, before, account)
	return account, nil
}

func isOwnedAccountPublicShareApprovable(account *Account, allowRateLimited bool) bool {
	if account == nil {
		return false
	}
	if account.IsSchedulable() {
		return true
	}
	if !allowRateLimited || account.RateLimitResetAt == nil || !time.Now().Before(*account.RateLimitResetAt) {
		return false
	}
	copy := *account
	copy.RateLimitedAt = nil
	copy.RateLimitResetAt = nil
	return copy.IsSchedulable()
}

func (s *AccountService) MarkOwnedPublicSharePending(ctx context.Context, ownerUserID, accountID int64, reason string) (*Account, error) {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	before := cloneAccountForNotice(account)
	if err := s.ensureAccountCanEnterPublicShare(ctx, account); err != nil {
		return nil, err
	}
	groupIDs, err := s.initialOwnedAccountGroupIDs(ctx, ownerUserID, account.Platform, account.Type, AccountShareModePublic, nil)
	if err != nil {
		return nil, err
	}
	account.ShareMode = AccountShareModePublic
	account.ShareStatus = AccountShareStatusPending
	account.ErrorMessage = strings.TrimSpace(reason)
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account public share status: %w", err)
	}
	if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
		return nil, fmt.Errorf("bind pending account groups: %w", err)
	}
	account.GroupIDs = append([]int64(nil), groupIDs...)
	s.notifyAccountChanged(ctx, before, account)
	return account, nil
}

func (s *AccountService) AutoRepairSuspectedOpenAIFreeAccount(ctx context.Context, accountID int64, maxWeeklyLimitUSD float64, reason string) (*Account, bool, error) {
	if s == nil || s.accountRepo == nil {
		return nil, false, ErrOwnedAccountGroupValidationUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, false, fmt.Errorf("get account: %w", err)
	}
	if !ShouldRepairSuspectedOpenAIFreeAccount(account, maxWeeklyLimitUSD, time.Now()) {
		return account, false, nil
	}
	before := cloneAccountForNotice(account)

	account.AccountLevel = AccountLevelFree
	if account.ShareMode == AccountShareModePublic {
		account.ShareStatus = AccountShareStatusSuspended
	}
	message := strings.TrimSpace(reason)
	if message == "" {
		message = "OpenAI Codex weekly quota exhausted under free-account threshold; public sharing suspended pending review"
	}
	account.ErrorMessage = message

	groupIDs := account.GroupIDs
	if account.OwnerUserID != nil {
		groupIDs, err = s.repairedOpenAIAccountGroupIDs(ctx, account)
		if err != nil {
			return nil, false, err
		}
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, false, fmt.Errorf("update account suspected free repair: %w", err)
	}
	if account.OwnerUserID != nil {
		if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
			return nil, false, fmt.Errorf("bind repaired account groups: %w", err)
		}
		account.GroupIDs = append([]int64(nil), groupIDs...)
	}
	s.notifyAccountChanged(ctx, before, account)
	return account, true, nil
}

func (s *AccountService) notifyAccountCreated(ctx context.Context, account *Account) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyAccountCreated(ctx, account)
}

func (s *AccountService) notifyAccountDeleted(ctx context.Context, account *Account) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyAccountDeleted(ctx, account)
}

func (s *AccountService) notifyAccountChanged(ctx context.Context, before, after *Account) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyAccountChanged(ctx, before, after)
}

func (s *AccountService) notifyBulkOwnedAccountsChanged(ctx context.Context, beforeByID map[int64]*Account, accountIDs []int64) {
	if s == nil || s.systemNoticeService == nil || len(accountIDs) == 0 {
		return
	}
	afterAccounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		slog.Warn("account.system_notice_bulk_reload_failed", "error", err)
		return
	}
	for _, after := range afterAccounts {
		if after == nil {
			continue
		}
		s.notifyAccountChanged(ctx, beforeByID[after.ID], after)
	}
}

func cloneAccountForNotice(account *Account) *Account {
	if account == nil {
		return nil
	}
	clone := *account
	if account.OwnerUserID != nil {
		ownerID := *account.OwnerUserID
		clone.OwnerUserID = &ownerID
	}
	clone.GroupIDs = append([]int64(nil), account.GroupIDs...)
	return &clone
}

func (s *AccountService) repairedOpenAIAccountGroupIDs(ctx context.Context, account *Account) ([]int64, error) {
	if account == nil || account.OwnerUserID == nil {
		return nil, ErrAccountNotFound
	}
	privateGroup, err := s.getPrivateGroupForOwnedAccount(ctx, *account.OwnerUserID, account.Platform)
	if err != nil {
		return nil, err
	}
	groupIDs := []int64{privateGroup.ID}
	if s.groupRepo == nil {
		return normalizeGroupIDs(groupIDs)
	}
	groups, err := s.groupRepo.ListActiveByPlatform(ctx, account.Platform)
	if err != nil {
		return nil, fmt.Errorf("list public share groups: %w", err)
	}
	for i := range groups {
		group := groups[i]
		if !isOwnedPublicSharePoolGroup(&group, account.Platform) {
			continue
		}
		if NormalizeOpenAISharedPoolRequiredLevel(group.RequiredAccountLevel) == AccountLevelFree {
			groupIDs = append(groupIDs, group.ID)
			break
		}
	}
	return normalizeGroupIDs(groupIDs)
}

func ShouldRepairSuspectedOpenAIFreeAccount(account *Account, maxWeeklyLimitUSD float64, now time.Time) bool {
	if account == nil || maxWeeklyLimitUSD <= 0 {
		return false
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	if OpenAISharedPoolLevelRank(account.AccountLevel) <= OpenAISharedPoolLevelRank(AccountLevelFree) {
		return false
	}
	weeklyLimit := account.GetQuotaWeeklyLimit()
	if weeklyLimit <= 0 || weeklyLimit > maxWeeklyLimitUSD {
		return false
	}
	progress := buildCodexUsageProgressFromExtra(account.Extra, "7d", now)
	if progress == nil || progress.Utilization < 100 {
		return false
	}
	if progress.ResetsAt != nil && now.After(*progress.ResetsAt) {
		return false
	}
	return true
}

func (s *AccountService) resolveOwnedPublicShareGroup(ctx context.Context, account *Account) (*Group, error) {
	if s == nil || s.groupRepo == nil || account == nil {
		return nil, ErrOwnedAccountGroupValidationUnavailable
	}
	platform := strings.TrimSpace(account.Platform)
	if platform == "" {
		return nil, ErrOwnedAccountGroupPlatformMismatch
	}
	groups, err := s.groupRepo.ListActiveByPlatform(ctx, platform)
	if err != nil {
		return nil, fmt.Errorf("list public share groups: %w", err)
	}
	if account.Platform == PlatformOpenAI {
		accountLevel := NormalizeOpenAISharedPoolAccountLevel(NormalizeOpenAIAccountLevel(account.Platform, account.AccountLevel, account.Credentials, account.Extra))
		if OpenAISharedPoolLevelRank(accountLevel) == 0 {
			return nil, ErrOwnedAccountPublicPoolUnavailable.WithMetadata(map[string]string{
				"platform":      platform,
				"account_level": accountLevel,
			})
		}
		var matchedGroup *Group
		bestRank := 0
		for i := range groups {
			group := groups[i]
			requiredLevel := NormalizeOpenAISharedPoolRequiredLevel(group.RequiredAccountLevel)
			if requiredLevel == "" || !isOwnedPublicSharePoolGroup(&group, platform) || !CanOpenAIAccountJoinSharedPool(accountLevel, requiredLevel) {
				continue
			}
			requiredRank := OpenAISharedPoolLevelRank(requiredLevel)
			if matchedGroup == nil || requiredRank > bestRank {
				candidate := group
				matchedGroup = &candidate
				bestRank = requiredRank
			}
		}
		if matchedGroup != nil {
			return matchedGroup, nil
		}
		return nil, ErrOwnedAccountPublicPoolUnavailable.WithMetadata(map[string]string{
			"platform":      platform,
			"account_level": accountLevel,
		})
	}
	for i := range groups {
		group := groups[i]
		if isOwnedPublicSharePoolGroup(&group, platform) && NormalizeRequiredAccountLevel(group.RequiredAccountLevel) == "" {
			return &group, nil
		}
	}
	return nil, ErrOwnedAccountPublicPoolUnavailable.WithMetadata(map[string]string{
		"platform": platform,
	})
}

func isOwnedPublicSharePoolGroup(group *Group, platform string) bool {
	if group == nil || !group.IsActive() {
		return false
	}
	if group.OwnerUserID != nil || NormalizeGroupScope(group.Scope) != GroupScopePublic {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform)) {
		return false
	}
	return true
}

func (s *AccountService) validateOwnedPublicSharePolicy(ctx context.Context, account *Account, group *Group) error {
	if s == nil || s.accountSharePolicyRepo == nil {
		return ErrOwnedAccountPublicPolicyUnavailable
	}
	if account == nil || group == nil || group.ID <= 0 {
		return ErrOwnedAccountPublicPolicyUnavailable
	}
	groupID := group.ID
	policy, err := s.accountSharePolicyRepo.ResolveEnabledAccountSharePolicy(ctx, account.ID, &groupID, account.Platform, account.SharePolicyID)
	if err != nil {
		return fmt.Errorf("resolve account share policy: %w", err)
	}
	if policy == nil || policy.OwnerShareRatio <= 0 {
		return ErrOwnedAccountPublicPolicyUnavailable.WithMetadata(map[string]string{
			"platform": account.Platform,
			"group_id": fmt.Sprintf("%d", group.ID),
		})
	}
	return nil
}

func (s *AccountService) publicOwnedAccountGroupIDs(ctx context.Context, ownerUserID int64, account *Account, publicGroup *Group) ([]int64, error) {
	if account == nil || publicGroup == nil {
		return nil, ErrOwnedAccountPublicPoolUnavailable
	}
	privateGroup, err := s.getPrivateGroupForOwnedAccount(ctx, ownerUserID, account.Platform)
	if err != nil {
		return nil, err
	}
	return normalizeGroupIDs([]int64{privateGroup.ID, publicGroup.ID})
}

func (s *AccountService) validateOwnedAccountGroupBinding(ctx context.Context, ownerUserID int64, platform, accountType string, groupIDs []int64) ([]int64, error) {
	groupIDs, err := normalizeGroupIDs(groupIDs)
	if err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return nil, nil
	}
	if s.groupRepo == nil || s.userRepo == nil {
		return nil, ErrOwnedAccountGroupValidationUnavailable
	}

	user, err := s.userRepo.GetByID(ctx, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil || user.ID <= 0 {
		return nil, ErrUserNotFound
	}

	accountPlatform := strings.TrimSpace(platform)
	if accountPlatform == "" {
		return nil, ErrOwnedAccountGroupPlatformMismatch
	}
	for _, groupID := range groupIDs {
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			return nil, fmt.Errorf("get group: %w", err)
		}
		if group == nil || group.ID <= 0 {
			return nil, ErrGroupNotFound
		}
		if !group.IsActive() {
			return nil, ErrGroupNotAllowed
		}
		groupPlatform := strings.TrimSpace(group.Platform)
		if groupPlatform == "" || !strings.EqualFold(groupPlatform, accountPlatform) {
			return nil, ErrOwnedAccountGroupPlatformMismatch
		}
		if requiresOAuthOnlyGroupCheck(accountType) && isOAuthOnlyGroup(group) {
			return nil, ErrGroupNotAllowed
		}
		allowed, err := s.canUserBindOwnedAccountGroup(ctx, user, group)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrGroupNotAllowed
		}
	}
	return groupIDs, nil
}

func requiresOAuthOnlyGroupCheck(accountType string) bool {
	switch strings.ToLower(strings.TrimSpace(accountType)) {
	case AccountTypeOAuth, AccountTypeSetupToken:
		return false
	default:
		return true
	}
}

func isOAuthOnlyGroup(group *Group) bool {
	if group == nil || !group.RequireOAuthOnly {
		return false
	}
	switch group.Platform {
	case PlatformOpenAI, PlatformAntigravity, PlatformAnthropic, PlatformGemini:
		return true
	default:
		return false
	}
}

func normalizeGroupIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrGroupNotFound
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func (s *AccountService) canUserBindOwnedAccountGroup(ctx context.Context, user *User, group *Group) (bool, error) {
	if user == nil || group == nil {
		return false, nil
	}
	if group.IsSubscriptionType() {
		if s.userSubRepo == nil {
			return false, ErrOwnedAccountGroupValidationUnavailable
		}
		_, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, user.ID, group.ID)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, ErrSubscriptionNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("get active subscription: %w", err)
	}
	return user.CanBindGroup(group.ID, group.IsExclusive), nil
}

// UpdateStatus 更新账号状态
func (s *AccountService) UpdateStatus(ctx context.Context, id int64, status string, errorMessage string) error {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}

	account.Status = status
	account.ErrorMessage = errorMessage

	if err := s.accountRepo.Update(ctx, account); err != nil {
		return fmt.Errorf("update account: %w", err)
	}

	return nil
}

// UpdateLastUsed 更新最后使用时间
func (s *AccountService) UpdateLastUsed(ctx context.Context, id int64) error {
	if err := s.accountRepo.UpdateLastUsed(ctx, id); err != nil {
		return fmt.Errorf("update last used: %w", err)
	}
	return nil
}

// GetCredential 获取账号凭证（安全访问）
func (s *AccountService) GetCredential(ctx context.Context, id int64, key string) (string, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get account: %w", err)
	}

	return account.GetCredential(key), nil
}

// TestCredentials 测试账号凭证是否有效（需要实现具体平台的测试逻辑）
func (s *AccountService) TestCredentials(ctx context.Context, id int64) error {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}

	// 根据平台执行不同的测试逻辑
	switch account.Platform {
	case PlatformAnthropic:
		// TODO: 测试Anthropic API凭证
		return nil
	case PlatformOpenAI:
		// TODO: 测试OpenAI API凭证
		return nil
	case PlatformGemini:
		// TODO: 测试Gemini API凭证
		return nil
	default:
		return fmt.Errorf("unsupported platform: %s", account.Platform)
	}
}
