package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrAccountNotFound                        = infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	ErrAccountNilInput                        = infraerrors.BadRequest("ACCOUNT_NIL_INPUT", "account input cannot be nil")
	ErrOwnedAccountTypeNotAllowed             = infraerrors.BadRequest("OWNED_ACCOUNT_TYPE_NOT_ALLOWED", "user accounts only support official OAuth accounts")
	ErrOwnedAccountCredentialsInvalid         = infraerrors.BadRequest("OWNED_ACCOUNT_CREDENTIALS_INVALID", "OAuth account credentials must include an access token")
	ErrOwnedAccountCredentialsNotAllowed      = infraerrors.BadRequest("OWNED_ACCOUNT_CREDENTIALS_NOT_ALLOWED", "user accounts cannot include API keys, custom URLs, upstream endpoints, cookies or manual session credentials")
	ErrOwnedAccountGroupPlatformMismatch      = infraerrors.BadRequest("OWNED_ACCOUNT_GROUP_PLATFORM_MISMATCH", "account group platform does not match account platform")
	ErrOwnedAccountGroupValidationUnavailable = infraerrors.InternalServer("OWNED_ACCOUNT_GROUP_VALIDATION_UNAVAILABLE", "owned account group validation is unavailable")
	ErrOwnedAccountPublicPoolUnavailable      = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_POOL_UNAVAILABLE", "Plus shared account pool group is not configured for this account platform")
	ErrOwnedAccountPublicPolicyUnavailable    = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_POLICY_UNAVAILABLE", "account share policy is not configured for this public account pool")
	ErrOwnedAccountPublicValidationFailed     = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED", "public account validation failed")
)

const AccountListGroupUngrouped int64 = -1
const AccountPrivacyModeUnsetFilter = "__unset__"

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
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error)
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
	Credentials    map[string]any
	Extra          map[string]any
}

// CreateAccountRequest 创建账号请求
type CreateAccountRequest struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes"`
	Platform           string         `json:"platform"`
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
	ListOwnedWithFilters(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error)
}

type AccountListFilters struct {
	Platform    string
	AccountType string
	Status      string
	Search      string
	GroupID     int64
	PrivacyMode string
}

type BulkUpdateOwnedAccountsInput struct {
	AccountIDs  []int64
	Concurrency *int
	Priority    *int
	LoadFactor  *int
	Status      string
	Schedulable *bool
	GroupIDs    *[]int64
	Credentials map[string]any
	Extra       map[string]any
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

// Create 创建账号
func (s *AccountService) Create(ctx context.Context, req CreateAccountRequest) (*Account, error) {
	// 验证分组是否存在（如果指定了分组）
	if len(req.GroupIDs) > 0 {
		if err := s.validateGroupIDsExist(ctx, req.GroupIDs); err != nil {
			return nil, err
		}
	}

	// 创建账号
	account := &Account{
		Name:        req.Name,
		Notes:       normalizeAccountNotes(req.Notes),
		Platform:    req.Platform,
		Type:        req.Type,
		Credentials: req.Credentials,
		Extra:       req.Extra,
		ShareMode:   NormalizeAccountShareMode(req.ShareMode),
		ProxyID:     req.ProxyID,
		Concurrency: req.Concurrency,
		LoadFactor:  normalizeLoadFactor(req.LoadFactor),
		Priority:    req.Priority,
		Status:      StatusActive,
		ExpiresAt:   req.ExpiresAt,
	}
	if req.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *req.AutoPauseOnExpired
	} else {
		account.AutoPauseOnExpired = true
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
	}

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
	accounts, result, err := repo.ListOwnedWithFilters(ctx, ownerUserID, params, filters.Platform, filters.AccountType, filters.Status, filters.Search, filters.GroupID, filters.PrivacyMode)
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
	if err := validateOwnedAccountSource(req.Type, req.Credentials, req.Extra); err != nil {
		return nil, err
	}
	shareMode := NormalizeAccountShareMode(req.ShareMode)
	groupIDs, err := s.initialOwnedAccountGroupIDs(ctx, ownerUserID, req.Platform, req.Type, shareMode, req.GroupIDs)
	if err != nil {
		return nil, err
	}

	shareStatus := AccountShareStatusApproved
	if shareMode == AccountShareModePublic {
		shareStatus = AccountShareStatusPending
	}

	account := &Account{
		Name:               req.Name,
		Notes:              normalizeAccountNotes(req.Notes),
		Platform:           req.Platform,
		Type:               req.Type,
		Credentials:        req.Credentials,
		Extra:              req.Extra,
		OwnerUserID:        &ownerUserID,
		ShareMode:          shareMode,
		ShareStatus:        shareStatus,
		ProxyID:            req.ProxyID,
		Concurrency:        req.Concurrency,
		LoadFactor:         normalizeLoadFactor(req.LoadFactor),
		Priority:           req.Priority,
		Status:             StatusActive,
		ExpiresAt:          req.ExpiresAt,
		AutoPauseOnExpired: true,
		Schedulable:        true,
	}
	if req.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *req.AutoPauseOnExpired
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
	return findDisallowedCredentialContent(values, credentialSafetyOptions{AllowOAuthTokenValues: true})
}

func normalizeLoadFactor(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	normalized := *value
	return &normalized
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

	// 更新字段
	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Notes != nil {
		account.Notes = normalizeAccountNotes(req.Notes)
	}

	if req.Credentials != nil {
		account.Credentials = *req.Credentials
	}

	if req.Extra != nil {
		account.Extra = *req.Extra
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
	}

	return account, nil
}

func (s *AccountService) UpdateOwned(ctx context.Context, ownerUserID, accountID int64, req UpdateAccountRequest) (*Account, error) {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Notes != nil {
		account.Notes = normalizeAccountNotes(req.Notes)
	}
	if req.Credentials != nil {
		account.Credentials = *req.Credentials
	}
	if req.Extra != nil {
		account.Extra = *req.Extra
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

	if !shouldBindGroups && req.GroupIDs != nil {
		var err error
		groupIDs, err = s.validateOwnedAccountGroupBinding(ctx, ownerUserID, account.Platform, account.Type, *req.GroupIDs)
		if err != nil {
			return nil, err
		}
		shouldBindGroups = true
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	if shouldBindGroups {
		if err := s.accountRepo.BindGroups(ctx, account.ID, groupIDs); err != nil {
			return nil, fmt.Errorf("bind groups: %w", err)
		}
		account.GroupIDs = append([]int64(nil), groupIDs...)
	}
	return account, nil
}

func (s *AccountService) DeleteOwned(ctx context.Context, ownerUserID, accountID int64) error {
	if _, err := s.GetOwnedByID(ctx, ownerUserID, accountID); err != nil {
		return err
	}
	if err := s.accountRepo.Delete(ctx, accountID); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

// Delete 删除账号
// 优化：使用 ExistsByID 替代 GetByID 进行存在性检查，
// 避免加载完整账号对象及其关联数据，提升删除操作的性能
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

	if input.Concurrency != nil && *input.Concurrency <= 0 {
		return nil, fmt.Errorf("concurrency must be > 0")
	}
	if input.Priority != nil && *input.Priority <= 0 {
		return nil, fmt.Errorf("priority must be > 0")
	}
	if input.LoadFactor != nil && *input.LoadFactor > 10000 {
		return nil, fmt.Errorf("load_factor must be <= 10000")
	}
	status, err := normalizeOwnedBulkStatus(input.Status)
	if err != nil {
		return nil, err
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

	validatedGroupIDsByAccount := make(map[int64][]int64, len(accountIDs))
	for _, accountID := range accountIDs {
		account := accountsByID[accountID]
		if account == nil || account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
			return nil, ErrAccountNotFound
		}

		nextCredentials := mergeAccountMap(account.Credentials, input.Credentials)
		nextExtra := mergeAccountMap(account.Extra, input.Extra)
		if err := validateOwnedAccountSource(account.Type, nextCredentials, nextExtra); err != nil {
			return nil, err
		}

		if input.GroupIDs != nil {
			groupIDs, err := s.validateOwnedAccountGroupBinding(ctx, ownerUserID, account.Platform, account.Type, *input.GroupIDs)
			if err != nil {
				return nil, err
			}
			validatedGroupIDsByAccount[accountID] = groupIDs
		}
	}

	repoUpdates := AccountBulkUpdate{
		Concurrency: input.Concurrency,
		Priority:    input.Priority,
		LoadFactor:  input.LoadFactor,
		Schedulable: input.Schedulable,
		Credentials: input.Credentials,
		Extra:       input.Extra,
	}
	if status != "" {
		repoUpdates.Status = &status
	}

	if _, err := s.accountRepo.BulkUpdate(ctx, accountIDs, repoUpdates); err != nil {
		return nil, fmt.Errorf("bulk update accounts: %w", err)
	}

	for _, accountID := range accountIDs {
		entry := BulkUpdateAccountResult{AccountID: accountID}
		if input.GroupIDs != nil {
			if err := s.accountRepo.BindGroups(ctx, accountID, validatedGroupIDsByAccount[accountID]); err != nil {
				entry.Success = false
				entry.Error = err.Error()
				result.Failed++
				result.FailedIDs = append(result.FailedIDs, accountID)
				result.Results = append(result.Results, entry)
				continue
			}
		}
		entry.Success = true
		result.Success++
		result.SuccessIDs = append(result.SuccessIDs, accountID)
		result.Results = append(result.Results, entry)
	}

	return result, nil
}

func (s *AccountService) Delete(ctx context.Context, id int64) error {
	// 使用轻量级的存在性检查，而非加载完整账号对象
	exists, err := s.accountRepo.ExistsByID(ctx, id)
	if err != nil {
		return fmt.Errorf("check account: %w", err)
	}
	// 明确返回账号不存在错误，便于调用方区分错误类型
	if !exists {
		return ErrAccountNotFound
	}

	if err := s.accountRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}

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
	if NormalizeAccountShareMode(shareMode) == AccountShareModePublic || len(requestedGroupIDs) == 0 {
		privateGroup, err := s.getPrivateGroupForOwnedAccount(ctx, ownerUserID, platform)
		if err != nil {
			return nil, err
		}
		return []int64{privateGroup.ID}, nil
	}
	return s.validateOwnedAccountGroupBinding(ctx, ownerUserID, platform, accountType, requestedGroupIDs)
}

func (s *AccountService) managedOwnedAccountGroupIDsForShareMode(ctx context.Context, ownerUserID int64, account *Account, nextMode string) ([]int64, error) {
	if account == nil {
		return nil, ErrAccountNotFound
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

func (s *AccountService) ApproveOwnedPublicShare(ctx context.Context, ownerUserID, accountID int64) (*Account, error) {
	return s.ApproveOwnedPublicShareWithOptions(ctx, ownerUserID, accountID, OwnedPublicShareApprovalOptions{})
}

func (s *AccountService) ApproveOwnedPublicShareWithOptions(ctx context.Context, ownerUserID, accountID int64, opts OwnedPublicShareApprovalOptions) (*Account, error) {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	if err := validateOwnedAccountSource(account.Type, account.Credentials, account.Extra); err != nil {
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
	return account, nil
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
	for i := range groups {
		group := groups[i]
		if isOwnedPublicSharePoolGroup(&group, platform) {
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
	return isPlusSharedPoolGroupName(group.Name)
}

func isPlusSharedPoolGroupName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer(" ", "", "-", "", "_", "", "（", "(", "）", ")", "【", "", "】", "").Replace(normalized)
	if normalized == "plus" || normalized == "pluspool" || normalized == "plussharedpool" || normalized == "plus共享池" || normalized == "plus共享号池" {
		return true
	}
	if !strings.Contains(normalized, "plus") {
		return false
	}
	return strings.Contains(normalized, "shared") ||
		strings.Contains(normalized, "pool") ||
		strings.Contains(normalized, "共享") ||
		strings.Contains(normalized, "号池")
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
