package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strconv"
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

	"golang.org/x/sync/singleflight"
)

var (
	ErrAccountNotFound                            = infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	ErrAccountNilInput                            = infraerrors.BadRequest("ACCOUNT_NIL_INPUT", "account input cannot be nil")
	ErrAccountPlatformUnsupported                 = infraerrors.BadRequest("ACCOUNT_PLATFORM_UNSUPPORTED", "account platform is not supported")
	ErrCodexQuotaLimitPercentInvalid              = infraerrors.BadRequest("CODEX_QUOTA_LIMIT_PERCENT_INVALID", "Codex quota limit percent must be between 1 and 100")
	ErrOwnedAccountAlreadyExists                  = infraerrors.Conflict("OWNED_ACCOUNT_ALREADY_EXISTS", "account already exists")
	ErrOwnedAccountTypeNotAllowed                 = infraerrors.BadRequest("OWNED_ACCOUNT_TYPE_NOT_ALLOWED", "user accounts only support official OAuth accounts")
	ErrOwnedAccountCredentialsInvalid             = infraerrors.BadRequest("OWNED_ACCOUNT_CREDENTIALS_INVALID", "OAuth account credentials must include an access token")
	ErrOwnedAccountCredentialsNotAllowed          = infraerrors.BadRequest("OWNED_ACCOUNT_CREDENTIALS_NOT_ALLOWED", "user accounts cannot include API keys, custom URLs, upstream endpoints, cookies or manual session credentials")
	ErrOwnedAgentIdentityCredentialsInvalid       = infraerrors.BadRequest("OWNED_AGENT_IDENTITY_CREDENTIALS_INVALID", "Codex Agent Identity credentials are invalid")
	ErrOwnedAccountConcurrencyOutOfRange          = infraerrors.BadRequest("OWNED_ACCOUNT_CONCURRENCY_OUT_OF_RANGE", "personal account concurrency must be between 1 and 30")
	ErrOwnedAccountLoadFactorOutOfRange           = infraerrors.BadRequest("OWNED_ACCOUNT_LOAD_FACTOR_OUT_OF_RANGE", fmt.Sprintf("personal account load factor must be between 1 and %d", AccountMaxLoadFactor))
	ErrOwnedAccountLoadFactorCreditsUnavailable   = infraerrors.InternalServer("OWNED_ACCOUNT_LOAD_FACTOR_CREDITS_UNAVAILABLE", "load factor credit accounting is unavailable")
	ErrOwnedAccountLoadFactorCreditsInsufficient  = infraerrors.BadRequest("OWNED_ACCOUNT_LOAD_FACTOR_CREDITS_INSUFFICIENT", "load factor credits are insufficient")
	ErrOwnedAccountLevelNotAllowed                = infraerrors.BadRequest("OWNED_ACCOUNT_LEVEL_NOT_ALLOWED", "user accounts cannot manually change account level")
	ErrOwnedOpenAIAccountLevelRequired            = infraerrors.BadRequest("OWNED_OPENAI_ACCOUNT_LEVEL_REQUIRED", "OpenAI user accounts must select an account level before import")
	ErrOwnedAccountProxyRequired                  = infraerrors.BadRequest("OWNED_ACCOUNT_PROXY_REQUIRED", "user OAuth accounts must use account login with a selected proxy IP")
	ErrOwnedOpenAIAccountProxyRequired            = ErrOwnedAccountProxyRequired
	ErrOwnedAccountGroupPlatformMismatch          = infraerrors.BadRequest("OWNED_ACCOUNT_GROUP_PLATFORM_MISMATCH", "account group platform does not match account platform")
	ErrOwnedAccountGroupValidationUnavailable     = infraerrors.InternalServer("OWNED_ACCOUNT_GROUP_VALIDATION_UNAVAILABLE", "owned account group validation is unavailable")
	ErrOwnedAccountPublicPoolUnavailable          = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_POOL_UNAVAILABLE", "public shared account pool group is not configured for this account platform")
	ErrOwnedAccountPublicPolicyUnavailable        = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_POLICY_UNAVAILABLE", "account share policy is not configured for this public account pool")
	ErrOwnedAccountPublicValidationFailed         = infraerrors.BadRequest("OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED", "public account validation failed")
	ErrOwnedAccountShareModeOnly                  = infraerrors.BadRequest("OWNED_ACCOUNT_SHARE_MODE_ONLY", "account share mode accounts cannot be moved to the public shared account pool")
	ErrOwnedAccountPlacementConversionRequired    = infraerrors.BadRequest("OWNED_ACCOUNT_PLACEMENT_CONVERSION_REQUIRED", "share mode changes must use the external placement conversion endpoint")
	ErrOwnedAgentIdentityLookupUnavailable        = infraerrors.InternalServer("OWNED_AGENT_IDENTITY_LOOKUP_UNAVAILABLE", "Codex Agent Identity account lookup is unavailable")
	ErrOwnedAgentIdentityWSInvalidatorUnavailable = infraerrors.InternalServer("OWNED_AGENT_IDENTITY_WS_INVALIDATOR_UNAVAILABLE", "Codex Agent Identity connection invalidation is unavailable")
	ErrOwnedAccountShareModeBoundaryUnavailable   = infraerrors.InternalServer("OWNED_ACCOUNT_SHARE_MODE_BOUNDARY_UNAVAILABLE", "account share mode boundary check is unavailable")
	ErrOwnedAccountProxyValidationUnavailable     = infraerrors.InternalServer("OWNED_ACCOUNT_PROXY_VALIDATION_UNAVAILABLE", "owned account proxy validation is unavailable")
	ErrAccountDeletionBlocked                     = infraerrors.Conflict("ACCOUNT_DELETION_BLOCKED", "account cannot be deleted while account-share usage is still active")
	ErrAccountDeletionGuardUnavailable            = infraerrors.InternalServer("ACCOUNT_DELETION_GUARD_UNAVAILABLE", "account deletion safety check is unavailable")
	ErrAccountMutationBlocked                     = infraerrors.Conflict("ACCOUNT_MUTATION_BLOCKED_BY_ROOM", "account-sensitive settings cannot be changed while the account is assigned to an active room")
	ErrAccountMutationForceRequired               = infraerrors.Conflict("ACCOUNT_MUTATION_FORCE_REQUIRED", "administrator confirmation is required to change room-assigned account settings")
	ErrAccountMutationVersionConflict             = infraerrors.Conflict("ACCOUNT_MUTATION_VERSION_CONFLICT", "the room changed after it was loaded; refresh and confirm again")
	ErrAccountMutationGuardUnavailable            = infraerrors.InternalServer("ACCOUNT_MUTATION_GUARD_UNAVAILABLE", "account mutation safety check is unavailable")
	ErrAccountMutationStale                       = infraerrors.Conflict("ACCOUNT_MUTATION_STALE", "the account changed after it was loaded; refresh and try again")
	ErrAccountMutationSystemIntentInvalid         = infraerrors.InternalServer("ACCOUNT_MUTATION_SYSTEM_INTENT_INVALID", "system account mutation exceeded its allowed fields")
	ErrCRSPreviewSnapshotUnavailable              = infraerrors.InternalServer("CRS_PREVIEW_SNAPSHOT_UNAVAILABLE", "CRS account room snapshot lookup is unavailable")
)

const AccountListGroupUngrouped int64 = -1
const AccountListProxyUnassigned int64 = -1
const AccountPrivacyModeUnsetFilter = "__unset__"
const ownedPersonalMinConcurrency = 1
const ownedPersonalMaxConcurrency = 30
const ownedPersonalDefaultConcurrency = ownedPersonalMinConcurrency
const ownedPersonalDefaultPriority = 1
const ownedPersonalDefaultOpenAICompactMode = "force_on"
const ownedPersonalDefaultOpenAIWSMode = OpenAIWSIngressModeOff

// 覆盖前端 60 秒轮询周期，避免每次轮询都重建个人与公共额度池。
const accountQuotaPoolDashboardCacheTTL = 90 * time.Second

// accountQuotaPoolDashboardCacheMaxEntries bounds the per-user dashboard cache
// so a large user base cannot grow it without limit. Each entry only holds the
// small aggregated summary, not the raw account rows.
const accountQuotaPoolDashboardCacheMaxEntries = 4096

const (
	AccountLevelUnknown = domain.AccountLevelUnknown
	AccountLevelFree    = domain.AccountLevelFree
	AccountLevelPlus    = domain.AccountLevelPlus
	AccountLevelPro     = domain.AccountLevelPro
	AccountLevelTeam    = domain.AccountLevelTeam
	AccountLevelK12     = domain.AccountLevelK12
)

// CRSAccountRoomBindingSnapshot is the optimistic-concurrency state exposed by
// CRS preview for a non-deleted room that currently uses a local account.
type CRSAccountRoomBindingSnapshot struct {
	ListingID  int64 `json:"listing_id"`
	RowVersion int64 `json:"row_version"`
}

// CRSAccountPreviewSnapshot is a read-only local snapshot used to classify CRS
// accounts and prepare the explicit administrator force-edit contract.
type CRSAccountPreviewSnapshot struct {
	CRSAccountID   string
	LocalAccountID int64
	RoomBindings   []CRSAccountRoomBindingSnapshot
}

// CRSPreviewSnapshotRepository is deliberately separate from AccountRepository.
// Preview must fail closed when this capability is absent rather than treating a
// potentially room-bound account as safe.
type CRSPreviewSnapshotRepository interface {
	ListCRSAccountPreviewSnapshots(ctx context.Context) ([]CRSAccountPreviewSnapshot, error)
}

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

// AccountDeletionGuardRepository owns the atomic safety boundary for physical
// account deletion. Implementations must lock the target account rows, inspect
// all account-share blockers, and delete only when every target is safe.
//
// This remains a separate capability so legacy/test repositories cannot gain an
// unsafe default implementation. AccountService fails closed when it is absent.
type AccountDeletionGuardRepository interface {
	DeleteIfUnblocked(ctx context.Context, accountID int64) error
	DeleteManyIfUnblocked(ctx context.Context, accountIDs []int64) error
}

type AccountOwnedDeletionGuardRepository interface {
	DeleteOwnedIfUnblocked(ctx context.Context, ownerUserID, accountID int64) error
	DeleteManyOwnedIfUnblocked(ctx context.Context, ownerUserID int64, accountIDs []int64) error
}

const (
	AccountMutationIntentOwner              = "owner_edit"
	AccountMutationIntentAdmin              = "admin_edit"
	AccountMutationIntentSystemTokenRefresh = "system_token_refresh"
)

type AccountMutationGuardTarget struct {
	AccountID         int64
	ExpectedUpdatedAt time.Time
	After             *Account
	GroupIDs          []int64
}

type AccountMutationGuardRequest struct {
	Targets                 []AccountMutationGuardTarget
	ActorUserID             int64
	ActorIsAdmin            bool
	Intent                  string
	ForceActiveEdit         bool
	Confirmed               bool
	Reason                  string
	ExpectedListingVersion  *int64
	ExpectedListingVersions map[int64]int64
	OperationID             string
}

// AccountMutationGuardRepository owns the atomic account/room boundary. The
// implementation discovers room bindings without locks, then locks referenced
// live rooms before account rows in a stable order. It revalidates bindings and
// optimistic versions inside the transaction, runs mutate, and appends
// immutable room events for administrator-forced changes.
type AccountMutationGuardRepository interface {
	WithAccountMutationGuard(ctx context.Context, request AccountMutationGuardRequest, mutate func(context.Context) error) error
}

type accountMutationGuardContextKey struct{}

func WithAccountMutationGuardContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, accountMutationGuardContextKey{}, true)
}

func AccountMutationGuardActive(ctx context.Context) bool {
	active, _ := ctx.Value(accountMutationGuardContextKey{}).(bool)
	return active
}

type AccountMutationDiff struct {
	ChangedFields         []string
	CredentialChangedKeys []string
	ExtraChangedKeys      []string
	Sensitive             bool
}

var systemTokenRefreshCredentialKeys = map[string]struct{}{
	"_token_version":          {},
	"access_token":            {},
	"refresh_token":           {},
	"id_token":                {},
	"expires_at":              {},
	"expires_in":              {},
	"token_type":              {},
	"scope":                   {},
	"client_id":               {},
	"email":                   {},
	"email_address":           {},
	"chatgpt_account_id":      {},
	"chatgpt_user_id":         {},
	"organization_id":         {},
	"plan_type":               {},
	"subscription_expires_at": {},
	"project_id":              {},
	"tier_id":                 {},
	"oauth_type":              {},
	"subscription_tier":       {},
	"entitlement_status":      {},
	"base_url":                {},
	"task_id":                 {},
	"drive_storage_limit":     {},
	"drive_storage_usage":     {},
	"drive_tier_updated_at":   {},
}

func ClassifyAccountMutation(before, after *Account, beforeGroupIDs, afterGroupIDs []int64) AccountMutationDiff {
	if before == nil || after == nil {
		return AccountMutationDiff{Sensitive: true, ChangedFields: []string{"account"}}
	}
	diff := AccountMutationDiff{}
	add := func(field string, sensitive bool) {
		diff.ChangedFields = append(diff.ChangedFields, field)
		diff.Sensitive = diff.Sensitive || sensitive
	}
	if before.Name != after.Name {
		add("name", false)
	}
	if !reflect.DeepEqual(before.Notes, after.Notes) {
		add("notes", false)
	}
	if before.Platform != after.Platform {
		add("platform", true)
	}
	if NormalizeAccountLevel(before.AccountLevel) != NormalizeAccountLevel(after.AccountLevel) {
		add("account_level", true)
	}
	if before.Type != after.Type {
		add("type", true)
	}
	diff.CredentialChangedKeys = changedAccountMapKeys(before.Credentials, after.Credentials)
	if len(diff.CredentialChangedKeys) > 0 {
		add("credentials", true)
	}
	diff.ExtraChangedKeys = changedAccountMapKeys(before.Extra, after.Extra)
	if len(diff.ExtraChangedKeys) > 0 {
		extraSensitive := false
		for _, key := range diff.ExtraChangedKeys {
			if key != "privacy_mode" {
				extraSensitive = true
				break
			}
		}
		add("extra", extraSensitive)
	}
	if !equalOptionalInt64(before.OwnerUserID, after.OwnerUserID) {
		add("owner_user_id", true)
	}
	if NormalizeAccountShareMode(before.ShareMode) != NormalizeAccountShareMode(after.ShareMode) {
		add("share_mode", true)
	}
	if NormalizeAccountShareStatus(before.ShareStatus) != NormalizeAccountShareStatus(after.ShareStatus) {
		add("share_status", true)
	}
	if !equalOptionalInt64(before.SharePolicyID, after.SharePolicyID) {
		add("share_policy_id", true)
	}
	if !equalOptionalInt64(before.ProxyID, after.ProxyID) {
		add("proxy_id", true)
	}
	if before.Concurrency != after.Concurrency {
		add("concurrency", after.Concurrency < before.Concurrency)
	}
	if before.Priority != after.Priority {
		add("priority", false)
	}
	if !equalOptionalFloat64(before.RateMultiplier, after.RateMultiplier) {
		add("rate_multiplier", true)
	}
	if !equalOptionalInt(before.LoadFactor, after.LoadFactor) {
		add("load_factor", true)
	}
	if before.Status != after.Status {
		add("status", before.Status == StatusActive && after.Status != StatusActive)
	}
	if before.Schedulable != after.Schedulable {
		add("schedulable", before.Schedulable && !after.Schedulable)
	}
	if !equalOptionalTime(before.ExpiresAt, after.ExpiresAt) {
		add("expires_at", true)
	}
	if before.AutoPauseOnExpired != after.AutoPauseOnExpired {
		add("auto_pause_on_expired", true)
	}
	if !equalNormalizedAccountGroupIDs(beforeGroupIDs, afterGroupIDs) {
		add("group_ids", true)
	}
	sort.Strings(diff.ChangedFields)
	return diff
}

func AccountMutationAllowedForSystemTokenRefresh(diff AccountMutationDiff) bool {
	if len(diff.ChangedFields) == 0 {
		return true
	}
	if len(diff.ChangedFields) != 1 || diff.ChangedFields[0] != "credentials" {
		return false
	}
	for _, key := range diff.CredentialChangedKeys {
		if _, ok := systemTokenRefreshCredentialKeys[strings.ToLower(strings.TrimSpace(key))]; !ok {
			return false
		}
	}
	return true
}

func changedAccountMapKeys(before, after map[string]any) []string {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	changed := make([]string, 0, len(keys))
	for key := range keys {
		if !reflect.DeepEqual(before[key], after[key]) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

func equalOptionalInt64(left, right *int64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalInt(left, right *int) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalFloat64(left, right *float64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalTime(left, right *time.Time) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && left.Equal(*right))
}

func equalNormalizedAccountGroupIDs(left, right []int64) bool {
	normalize := func(values []int64) []int64 {
		seen := make(map[int64]struct{}, len(values))
		out := make([]int64, 0, len(values))
		for _, value := range values {
			if value <= 0 {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	return reflect.DeepEqual(normalize(left), normalize(right))
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
	MutationIntent     string          `json:"-"`
}

type OwnedPublicShareApprovalOptions struct {
	AllowRateLimited bool
}

type OwnedAccountImportResult struct {
	Account *Account
	Updated bool
}

// AccountService 账号管理服务
type AccountService struct {
	accountRepo                AccountRepository
	groupRepo                  GroupRepository
	userRepo                   accountUserRepository
	userSubRepo                accountSubscriptionLookupRepository
	accountSharePolicyRepo     AccountSharePolicyRepository
	accountShareModeGroups     accountShareModeGroupClassifier
	accountShareModeRepo       AccountShareModeRepository
	accountShareRoomRepo       AccountShareRoomRepository
	settingService             *SettingService
	privateGroupProvisioner    UserPrivateGroupProvisioner
	systemNoticeService        *SystemNoticeService
	proxyRepo                  ownedAccountProxyRepository
	agentIdentityWSInvalidator agentIdentityWSConnectionInvalidator
	quotaPoolDashboardCache    accountQuotaPoolDashboardCache
	concurrencyService         *ConcurrencyService
	accountShareBillingCache   accountShareSeatBillingCacheInvalidator
}

type accountShareSeatBillingCacheInvalidator interface {
	invalidateSeatBillingCaches(result *AccountShareSeatBillingResult)
}

type accountQuotaPoolDashboardCache struct {
	mu      sync.Mutex
	entries map[int64]accountQuotaPoolDashboardCacheEntry
	group   singleflight.Group
}

type accountQuotaPoolDashboardCacheEntry struct {
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

type ownedAccountProxyRepository interface {
	GetVisibleByID(ctx context.Context, scope ProxyScope, id int64) (*Proxy, error)
	CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error)
}

type ownedAccountProxyCapacityCreateRepository interface {
	CreateOwnedWithProxyCapacity(ctx context.Context, ownerUserID int64, account *Account) error
}

type ownedAccountFilterRepository interface {
	ListOwnedWithFilters(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error)
}

type ownedAccountIDBatchRepository interface {
	ListOwnedAccountIDs(ctx context.Context, ownerUserID int64, accountIDs []int64) ([]int64, error)
}

type ownedOpenAIAgentIdentityRepository interface {
	GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx context.Context, ownerUserID int64, chatGPTAccountID string) (*Account, error)
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

type accountShareModeGroupClassifier interface {
	IsModeGroup(ctx context.Context, groupID int64) (bool, error)
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
	proxyRepo ownedAccountProxyRepository,
) *AccountService {
	return &AccountService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		userRepo:    userRepo,
		userSubRepo: userSubRepo,
		proxyRepo:   proxyRepo,
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

func (s *AccountService) SetAccountShareModeRepository(repo AccountShareModeRepository) {
	if s == nil {
		return
	}
	s.accountShareModeGroups = repo
	s.accountShareModeRepo = repo
	if roomRepo, ok := repo.(AccountShareRoomRepository); ok {
		s.accountShareRoomRepo = roomRepo
	}
}

func (s *AccountService) SetConcurrencyService(concurrencyService *ConcurrencyService) {
	if s == nil {
		return
	}
	s.concurrencyService = concurrencyService
}

func (s *AccountService) SetAccountShareBillingCacheInvalidator(invalidator accountShareSeatBillingCacheInvalidator) {
	if s == nil {
		return
	}
	s.accountShareBillingCache = invalidator
}

func (s *AccountService) SetSettingService(settingService *SettingService) {
	if s == nil {
		return
	}
	s.settingService = settingService
}

func (s *AccountService) SetSystemNoticeService(noticeService *SystemNoticeService) {
	if s == nil {
		return
	}
	s.systemNoticeService = noticeService
}

func (s *AccountService) SetAgentIdentityWSInvalidator(invalidator agentIdentityWSConnectionInvalidator) {
	if s == nil {
		return
	}
	s.agentIdentityWSInvalidator = invalidator
}

func (s *AccountService) openAIAccountLevelConfigs(ctx context.Context) ([]OpenAIAccountLevelConfig, error) {
	if s == nil || s.settingService == nil {
		return DefaultOpenAIAccountLevelConfigs(), nil
	}
	configs, err := s.settingService.GetOpenAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	return configs, nil
}

// Create 创建账号
func (s *AccountService) Create(ctx context.Context, req CreateAccountRequest) (*Account, error) {
	extra, err := NormalizeCodexQuotaLimitExtra(req.Platform, req.Type, req.Extra)
	if err != nil {
		return nil, err
	}
	req.Extra = extra
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}

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
		AccountLevel: NormalizeOpenAIAccountLevelWithConfigs(req.Platform, req.AccountLevel, req.Credentials, req.Extra, levelConfigs),
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

// EnsureOwnedByIDs 在一次轻量查询中验证所有账号都属于指定用户。
// 只要任一 ID 不存在、已删除或属于其他用户，整批就以统一的不可见语义失败。
func (s *AccountService) EnsureOwnedByIDs(ctx context.Context, ownerUserID int64, accountIDs []int64) error {
	if ownerUserID <= 0 {
		return ErrUserNotFound
	}
	ids := normalizeOwnedBulkAccountIDs(accountIDs)
	if len(ids) == 0 {
		return nil
	}
	repo, ok := s.accountRepo.(ownedAccountIDBatchRepository)
	if !ok {
		return fmt.Errorf("batch owned account validation is not supported by repository")
	}
	ownedIDs, err := repo.ListOwnedAccountIDs(ctx, ownerUserID, ids)
	if err != nil {
		return fmt.Errorf("validate owned accounts: %w", err)
	}
	if len(ownedIDs) != len(ids) {
		return ErrAccountNotFound
	}
	owned := make(map[int64]struct{}, len(ownedIDs))
	for _, id := range ownedIDs {
		owned[id] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := owned[id]; !ok {
			return ErrAccountNotFound
		}
	}
	return nil
}

func (s *AccountService) EnsureOwnedAccountCanEnterPublicShare(ctx context.Context, ownerUserID, accountID int64) error {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return err
	}
	return s.ensureAccountCanEnterPublicShare(ctx, account)
}

func (s *AccountService) CreateOwned(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*Account, error) {
	if err := rejectOwnedAccountGrokManagedExtra(req.Extra); err != nil {
		return nil, err
	}
	return s.createOwned(ctx, ownerUserID, req)
}

func (s *AccountService) ImportOwned(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*Account, error) {
	result, err := s.ImportOwnedWithResult(ctx, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	return result.Account, nil
}

func (s *AccountService) ImportOwnedWithResult(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*OwnedAccountImportResult, error) {
	if err := rejectOwnedAccountGrokManagedExtra(req.Extra); err != nil {
		return nil, err
	}
	if !IsOpenAIAgentIdentityCredentials(req.Credentials) {
		account, err := s.createOwned(ctx, ownerUserID, req)
		if err != nil {
			return nil, err
		}
		return &OwnedAccountImportResult{Account: account}, nil
	}

	req.Credentials = normalizeOwnedAgentIdentityCredentials(req.Credentials)
	if err := validateOwnedAccountSourceForPlatform(req.Platform, req.Type, req.Credentials, req.Extra); err != nil {
		return nil, err
	}
	chatGPTAccountID := importStringField(req.Credentials, "chatgpt_account_id")
	repo, ok := s.accountRepo.(ownedOpenAIAgentIdentityRepository)
	if !ok {
		return nil, ErrOwnedAgentIdentityLookupUnavailable
	}

	existing, err := repo.GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx, ownerUserID, chatGPTAccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup owned Agent Identity account: %w", err)
	}
	if existing != nil {
		account, err := s.updateOwnedAgentIdentityImport(ctx, ownerUserID, existing, req)
		if err != nil {
			return nil, err
		}
		return &OwnedAccountImportResult{Account: account, Updated: true}, nil
	}

	account, err := s.createOwned(ctx, ownerUserID, req)
	if err == nil {
		return &OwnedAccountImportResult{Account: account}, nil
	}
	if !errors.Is(err, ErrOwnedAccountAlreadyExists) {
		return nil, err
	}

	// A concurrent import may have committed the same owner+Team identity after
	// the lookup above. The database unique index is the authority; after that
	// conflict becomes visible, converge on the committed row and update it.
	existing, lookupErr := repo.GetOwnedOpenAIAgentIdentityByChatGPTAccountID(ctx, ownerUserID, chatGPTAccountID)
	if lookupErr != nil {
		return nil, fmt.Errorf("reload concurrently imported Agent Identity account: %w", lookupErr)
	}
	if existing == nil {
		return nil, err
	}
	account, updateErr := s.updateOwnedAgentIdentityImport(ctx, ownerUserID, existing, req)
	if updateErr != nil {
		return nil, updateErr
	}
	return &OwnedAccountImportResult{Account: account, Updated: true}, nil
}

func (s *AccountService) updateOwnedAgentIdentityImport(
	ctx context.Context,
	ownerUserID int64,
	account *Account,
	req CreateAccountRequest,
) (*Account, error) {
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID || !account.IsOpenAIAgentIdentity() {
		return nil, ErrAccountNotFound
	}
	if s.agentIdentityWSInvalidator == nil {
		return nil, ErrOwnedAgentIdentityWSInvalidatorUnavailable
	}
	if err := validateOwnedAccountSourceForPlatform(req.Platform, req.Type, req.Credentials, req.Extra); err != nil {
		return nil, err
	}

	before := cloneAccountForNotice(account)
	// Rebuild from the Agent Identity allowlist instead of carrying the entire
	// historical credential payload forward. Older records may predate the
	// recursive credential guard and can contain stale OAuth tokens or other
	// fields that Agent Identity must never retain.
	nextCredentials := make(map[string]any)
	previousRuntimeID := strings.TrimSpace(account.GetCredential("agent_runtime_id"))
	nextRuntimeID := importStringField(req.Credentials, "agent_runtime_id")

	allowedCredentialKeys := []string{
		"auth_mode",
		"agent_runtime_id",
		"agent_private_key",
		"task_id",
		"chatgpt_account_id",
		"chatgpt_user_id",
		"email",
		"plan_type",
		"chatgpt_account_is_fedramp",
	}
	for _, key := range allowedCredentialKeys {
		if value, exists := account.Credentials[key]; exists {
			nextCredentials[key] = value
		}
	}
	for _, key := range allowedCredentialKeys {
		if key == "task_id" {
			continue
		}
		if value, exists := req.Credentials[key]; exists {
			nextCredentials[key] = value
		}
	}
	if taskID := importStringField(req.Credentials, "task_id"); taskID != "" {
		nextCredentials["task_id"] = taskID
	} else if previousRuntimeID != nextRuntimeID {
		delete(nextCredentials, "task_id")
	}
	if err := validateOwnedAccountSourceForPlatform(PlatformOpenAI, AccountTypeOAuth, nextCredentials, nil); err != nil {
		return nil, err
	}

	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	accountLevel := InferOpenAIAccountLevelWithConfigs(nextCredentials, account.Extra, levelConfigs)
	if OpenAIAccountLevelConfigByKey(levelConfigs, accountLevel) == nil {
		accountLevel = AccountLevelFree
	}

	account.Credentials = nextCredentials
	account.AccountLevel = accountLevel
	var groupIDs []int64
	if NormalizeAccountShareMode(before.ShareMode) == AccountShareModePublic {
		// Re-import replaces authentication material. Keep the owner's explicit
		// public-share intent, but remove the account from the public pool until
		// the handler has completed a fresh connectivity check and approval.
		groupIDs, err = s.prepareOwnedPublicShareRevalidation(ctx, ownerUserID, account)
	} else {
		account.ShareMode = AccountShareModePrivate
		account.ShareStatus = AccountShareStatusApproved
		account.ErrorMessage = ""
		groupIDs, err = s.initialOwnedAccountGroupIDs(ctx, ownerUserID, PlatformOpenAI, AccountTypeOAuth, AccountShareModePrivate, nil)
	}
	if err != nil {
		return nil, err
	}
	account.ExpiresAt = nil
	account.GroupIDs = append([]int64(nil), groupIDs...)
	if err := s.withAccountMutationGuard(ctx, AccountMutationGuardRequest{
		Targets: []AccountMutationGuardTarget{{
			AccountID:         account.ID,
			ExpectedUpdatedAt: before.UpdatedAt,
			After:             account,
			GroupIDs:          append([]int64(nil), groupIDs...),
		}},
		ActorUserID: ownerUserID,
		Intent:      AccountMutationIntentOwner,
	}, func(txCtx context.Context) error {
		if updateErr := s.accountRepo.Update(txCtx, account); updateErr != nil {
			return fmt.Errorf("update owned Agent Identity account: %w", updateErr)
		}
		if bindErr := s.accountRepo.BindGroups(txCtx, account.ID, groupIDs); bindErr != nil {
			return fmt.Errorf("bind private Agent Identity account group: %w", bindErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.agentIdentityWSInvalidator.InvalidateAgentIdentityWSConnections(account.ID)
	s.notifyAccountChanged(ctx, before, account)
	return account, nil
}

func (s *AccountService) EnsureOwnedProxyAvailableForNewAccount(ctx context.Context, scope ProxyScope, proxyID int64) error {
	return s.ensureOwnedProxyAvailableForNewAccount(ctx, scope, proxyID)
}

func (s *AccountService) EnsureOwnedProxyUsableForLogin(ctx context.Context, scope ProxyScope, proxyID int64) error {
	_, err := s.ensureOwnedProxyUsableForLogin(ctx, scope, proxyID)
	return err
}

func (s *AccountService) createOwned(ctx context.Context, ownerUserID int64, req CreateAccountRequest) (*Account, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if !IsSupportedAccountPlatform(req.Platform) {
		return nil, ErrAccountPlatformUnsupported
	}
	isAgentIdentity := IsOpenAIAgentIdentityCredentials(req.Credentials)
	if isAgentIdentity {
		req.Credentials = normalizeOwnedAgentIdentityCredentials(req.Credentials)
	}
	targetLevel := NormalizeAccountLevel(req.AccountLevel)
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	preserveProxy := !isAgentIdentity && RequiresUserAccountOAuthProxyWithConfigs(req.Platform, targetLevel, levelConfigs)
	proxyID := req.ProxyID
	if err := applyOwnedPersonalAccountTemplateToCreate(&req); err != nil {
		return nil, err
	}
	if err := validateOwnedAccountSourceForPlatform(req.Platform, req.Type, req.Credentials, req.Extra); err != nil {
		return nil, err
	}
	if isAgentIdentity {
		inferredLevel := InferOpenAIAccountLevelWithConfigs(req.Credentials, req.Extra, levelConfigs)
		if OpenAIAccountLevelConfigByKey(levelConfigs, inferredLevel) == nil {
			inferredLevel = AccountLevelFree
		}
		targetLevel = inferredLevel
		req.AccountLevel = inferredLevel
		req.ShareMode = AccountShareModePrivate
		req.ProxyID = nil
		req.ExpiresAt = nil
	} else if req.Platform == PlatformOpenAI {
		if !IsUserSelectableOpenAIAccountLevelWithConfigs(targetLevel, levelConfigs) {
			return nil, ErrOwnedOpenAIAccountLevelRequired
		}
		if preserveProxy {
			if proxyID == nil || *proxyID <= 0 {
				return nil, ErrOwnedAccountProxyRequired
			}
			req.ProxyID = proxyID
		}
		req.AccountLevel = targetLevel
	} else {
		if preserveProxy {
			if proxyID == nil || *proxyID <= 0 {
				return nil, ErrOwnedAccountProxyRequired
			}
			req.ProxyID = proxyID
		}
		req.AccountLevel = AccountLevelUnknown
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

	accountLevel, err := resolveOwnedOpenAIAccountLevel(req.Platform, targetLevel, req.Credentials, req.Extra, levelConfigs)
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
	if preserveProxy && account.ProxyID != nil {
		if creator, ok := s.accountRepo.(ownedAccountProxyCapacityCreateRepository); ok {
			if err := creator.CreateOwnedWithProxyCapacity(ctx, ownerUserID, account); err != nil {
				return nil, err
			}
		} else if err := s.ensureOwnedProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(account.Platform, account.AccountLevel, ownerUserID), *account.ProxyID); err != nil {
			return nil, err
		} else if err := s.accountRepo.Create(ctx, account); err != nil {
			return nil, fmt.Errorf("create account: %w", err)
		}
	} else if err := s.accountRepo.Create(ctx, account); err != nil {
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

func normalizeOwnedAgentIdentityCredentials(credentials map[string]any) map[string]any {
	normalized := mergeAccountMap(credentials, nil)
	normalized["auth_mode"] = OpenAIAuthModeAgentIdentity
	for _, key := range []string{
		"agent_runtime_id",
		"agent_private_key",
		"task_id",
		"chatgpt_account_id",
		"chatgpt_user_id",
		"email",
		"plan_type",
	} {
		if value, ok := normalized[key].(string); ok {
			normalized[key] = strings.TrimSpace(value)
		}
	}
	return normalized
}

func validateOwnedAccountSource(accountType string, credentials, extra map[string]any) error {
	return validateOwnedAccountSourceForPlatform("", accountType, credentials, extra)
}

func validateOwnedAccountSourceForPlatform(platform, accountType string, credentials, extra map[string]any) error {
	return validateOwnedAccountSourceScoped(platform, accountType, credentials, extra,
		ownedAccountSourceScope{Mode: ownedSourceScanFull})
}

// validateOwnedAccountSourceMutation 用于所有者更新账号：结构性检查（账号类型、
// Agent Identity 必填项、access_token 存在性）依然针对完整凭证，内容安全扫描只
// 针对本次请求相对库内快照引入或改动的部分。库里已经存在、这次没被碰过的值不再
// 参与扫描——那些值不是所有者写进去的，用它们拒绝所有者是错的。
func validateOwnedAccountSourceMutation(
	platform, accountType string,
	storedCredentials, storedExtra map[string]any,
	credentials, extra map[string]any,
) error {
	return validateOwnedAccountSourceScoped(platform, accountType, credentials, extra, ownedAccountSourceScope{
		Mode:              ownedSourceScanDelta,
		StoredCredentials: storedCredentials,
		StoredExtra:       storedExtra,
	})
}

func validateOwnedAccountSourceScoped(
	platform, accountType string,
	credentials, extra map[string]any,
	scope ownedAccountSourceScope,
) error {
	if !isAllowedOwnedAccountType(accountType) {
		return ErrOwnedAccountTypeNotAllowed
	}
	isAgentIdentity := IsOpenAIAgentIdentityCredentials(credentials)
	if isAgentIdentity {
		if platform != "" && platform != PlatformOpenAI {
			return ErrOwnedAgentIdentityCredentialsInvalid.WithMetadata(map[string]string{"field": "platform"})
		}
		for _, key := range []string{"agent_runtime_id", "agent_private_key", "chatgpt_account_id"} {
			if !hasNonEmptyStringField(credentials, key) {
				return ErrOwnedAgentIdentityCredentialsInvalid.WithMetadata(map[string]string{"field": key})
			}
		}
		for _, identifier := range []struct {
			field    string
			label    string
			required bool
		}{
			{field: "agent_runtime_id", label: "runtime id", required: true},
			{field: "chatgpt_account_id", label: "Team id", required: true},
			{field: "task_id", label: "task id"},
			{field: "chatgpt_user_id", label: "user id"},
		} {
			raw, exists := credentials[identifier.field]
			if !exists {
				continue
			}
			value, ok := raw.(string)
			if !ok {
				return ErrOwnedAgentIdentityCredentialsInvalid.WithMetadata(map[string]string{"field": identifier.field})
			}
			if strings.TrimSpace(value) == "" && !identifier.required {
				credentials[identifier.field] = ""
				continue
			}
			normalized, err := normalizeAgentIdentityIdentifier(identifier.label, value)
			if err != nil {
				return ErrOwnedAgentIdentityCredentialsInvalid.
					WithMetadata(map[string]string{"field": identifier.field}).
					WithCause(err)
			}
			credentials[identifier.field] = normalized
		}
		if err := ValidateOpenAIAgentIdentityPrivateKey(importStringField(credentials, "agent_private_key")); err != nil {
			return ErrOwnedAgentIdentityCredentialsInvalid.WithMetadata(map[string]string{"field": "agent_private_key"}).WithCause(err)
		}
		safetyCredentials := mergeAccountMap(scope.credentialsToScan(credentials), nil)
		removeImportMapField(safetyCredentials, "auth_mode")
		removeImportMapField(safetyCredentials, "authMode")
		if field, ok := findDisallowedOwnedAgentIdentityField(safetyCredentials); ok {
			return ErrOwnedAccountCredentialsNotAllowed.WithMetadata(map[string]string{
				"section": "credentials",
				"field":   field,
			})
		}
	} else {
		if !hasNonEmptyStringField(credentials, "access_token") {
			return ErrOwnedAccountCredentialsInvalid
		}
		if field, ok := findDisallowedOwnedAccountField(scope.credentialsToScan(credentials)); ok {
			return ErrOwnedAccountCredentialsNotAllowed.WithMetadata(map[string]string{
				"section": "credentials",
				"field":   field,
			})
		}
	}
	extraSafetyCheck := findDisallowedOwnedAccountField
	if isAgentIdentity {
		extraSafetyCheck = findDisallowedOwnedAgentIdentityField
	}
	if field, ok := extraSafetyCheck(scope.extraToScan(extra)); ok {
		return ErrOwnedAccountCredentialsNotAllowed.WithMetadata(map[string]string{
			"section": "extra",
			"field":   field,
		})
	}
	return nil
}

func resolveOwnedOpenAIAccountLevel(platform, targetLevel string, credentials, extra map[string]any, configs []OpenAIAccountLevelConfig) (string, error) {
	if platform != PlatformOpenAI {
		return AccountLevelUnknown, nil
	}

	target := NormalizeAccountLevel(targetLevel)
	if !IsUserSelectableOpenAIAccountLevelWithConfigs(target, configs) {
		return "", ErrOwnedOpenAIAccountLevelRequired
	}

	actual := InferOpenAIAccountLevelWithConfigs(credentials, extra, configs)
	if OpenAIAccountLevelConfigByKey(configs, actual) == nil {
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
	for _, key := range []string{
		CredentialKeyHeaderOverrideEnabled,
		CredentialKeyHeaderOverrides,
	} {
		if _, ok := values[key]; ok {
			return key, true
		}
	}
	return findDisallowedCredentialContent(values, credentialSafetyOptions{
		AllowOAuthTokenValues:  true,
		AllowOAuthMetadataURLs: true,
	})
}

func findDisallowedOwnedAgentIdentityField(values map[string]any) (string, bool) {
	return findDisallowedCredentialContent(values, credentialSafetyOptions{
		AllowOAuthTokenValues:    true,
		AllowOAuthMetadataURLs:   true,
		DisallowOAuthTokenFields: true,
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

func (s *AccountService) ensureOwnedProxyAvailableForNewAccount(ctx context.Context, scope ProxyScope, proxyID int64) error {
	proxy, err := s.ensureOwnedProxyUsableForLogin(ctx, scope, proxyID)
	if err != nil {
		return err
	}
	if proxy.MaxAccounts <= 0 {
		return nil
	}
	current, err := s.proxyRepo.CountAccountsByProxyID(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("count proxy accounts: %w", err)
	}
	limit := int64(proxy.MaxAccounts)
	if current+1 > limit {
		return ProxyAccountLimitExceededError(proxyID, current, limit, 1)
	}
	return nil
}

func (s *AccountService) ensureOwnedProxyUsableForLogin(ctx context.Context, scope ProxyScope, proxyID int64) (*Proxy, error) {
	if proxyID <= 0 {
		return nil, ErrOwnedAccountProxyRequired
	}
	if s == nil || s.proxyRepo == nil {
		return nil, ErrOwnedAccountProxyValidationUnavailable
	}
	proxy, err := s.proxyRepo.GetVisibleByID(ctx, scope, proxyID)
	if err != nil {
		return nil, err
	}
	if proxy == nil || !proxy.IsActive() {
		return nil, ErrProxyNotFound
	}
	return proxy, nil
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
		if err := preserveOwnedAccountGrokManagedExtra(account.Extra, nextExtra); err != nil {
			return err
		}
		preserveOwnedPersonalExtraPolicy(account, nextExtra)
		req.Extra = &nextExtra
	}
	return nil
}

func ownedPersonalAccountRequiresProxy(account *Account, levelConfigs []OpenAIAccountLevelConfig) bool {
	if account == nil {
		return false
	}
	if account.IsOpenAIAgentIdentity() {
		return false
	}
	return RequiresUserAccountOAuthProxyWithConfigs(account.Platform, account.AccountLevel, levelConfigs)
}

var ownedPersonalLockedCredentialKeys = []string{
	"model_mapping",
	"compact_model_mapping",
	CredentialKeyHeaderOverrideEnabled,
	CredentialKeyHeaderOverrides,
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
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
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
	account.AccountLevel = NormalizeOpenAIAccountLevelWithConfigs(account.Platform, account.AccountLevel, account.Credentials, account.Extra, levelConfigs)
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

// ownedAccountMutationRetryAttempts 是所有者更新遇到乐观锁冲突时的总尝试次数。
const ownedAccountMutationRetryAttempts = 3

// UpdateOwned 更新账号所有者可以自助修改的字段。
//
// 变更守卫用 updated_at 做乐观并发控制，而令牌刷新、用量快照、限流簿记这些后台
// 写入随时会推进同一行的 updated_at。对于不产生任何前置副作用的请求（切调度、
// 启停、改名、改优先级/并发），冲突后重读最新状态再试一次是安全的，否则用户在
// 账号繁忙时会随机看到"操作失败"。带凭证/额外配置/代理/共享变更的请求不重试：
// 它们在进入事务前就可能已经写过共享位或代理归属。
func (s *AccountService) UpdateOwned(ctx context.Context, ownerUserID, accountID int64, req UpdateAccountRequest) (*Account, error) {
	for attempt := 1; ; attempt++ {
		account, err := s.updateOwnedOnce(ctx, ownerUserID, accountID, req)
		if err == nil ||
			attempt >= ownedAccountMutationRetryAttempts ||
			!errors.Is(err, ErrAccountMutationStale) ||
			!ownedAccountUpdateIsRetryable(req) {
			return account, err
		}
		slog.Info("owned_account_update_retry_after_stale",
			"account_id", accountID,
			"owner_user_id", ownerUserID,
			"attempt", attempt,
		)
	}
}

// ownedAccountUpdateIsRetryable 判断该请求在失败后能否原样重放。
func ownedAccountUpdateIsRetryable(req UpdateAccountRequest) bool {
	return req.Credentials == nil &&
		req.Extra == nil &&
		req.ProxyID == nil &&
		req.ShareMode == nil &&
		req.GroupIDs == nil &&
		req.LoadFactor == nil &&
		req.AccountLevel == nil
}

func (s *AccountService) updateOwnedOnce(ctx context.Context, ownerUserID, accountID int64, req UpdateAccountRequest) (*Account, error) {
	if req.AccountLevel != nil {
		return nil, ErrOwnedAccountLevelNotAllowed
	}
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	before := cloneAccountForNotice(account)
	// cloneAccountForNotice 只是浅拷贝，两份 Account 共享同一批 map，不能当作
	// 安全扫描的基线。这里在任何改动发生之前单独复制一份库内快照。
	storedCredentials := mergeAccountMap(account.Credentials, nil)
	storedExtra := mergeAccountMap(account.Extra, nil)
	existingProxyID := account.ProxyID
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
	if req.Concurrency != nil {
		account.Concurrency = *req.Concurrency
	}
	if req.LoadFactor != nil {
		account.LoadFactor = normalizeLoadFactor(req.LoadFactor)
	}
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	account.AccountLevel = NormalizeOpenAIAccountLevelWithConfigs(account.Platform, account.AccountLevel, account.Credentials, account.Extra, levelConfigs)
	if strings.TrimSpace(req.MutationIntent) == AccountMutationIntentSystemTokenRefresh {
		account.AccountLevel = before.AccountLevel
	}
	if req.ProxyID != nil {
		proxyRequired := ownedPersonalAccountRequiresProxy(account, levelConfigs)
		if *req.ProxyID <= 0 {
			if proxyRequired {
				return nil, ErrOwnedAccountProxyRequired
			}
			account.ProxyID = nil
		} else if existingProxyID != nil && *existingProxyID == *req.ProxyID {
			// 未更换代理：沿用既有绑定，附带遗留归属豁免，避免老用户的自有代理掉线。
			if _, err := s.ensureOwnedProxyUsableForLogin(ctx, NewOwnedProxyScope(account.Platform, account.AccountLevel, ownerUserID), *req.ProxyID); err != nil {
				return nil, err
			}
		} else if err := s.ensureOwnedProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(account.Platform, account.AccountLevel, ownerUserID), *req.ProxyID); err != nil {
			return nil, err
		} else {
			proxyID := *req.ProxyID
			account.ProxyID = &proxyID
		}
	}
	if (req.Credentials != nil || req.Extra != nil) && ownedPersonalAccountRequiresProxy(account, levelConfigs) {
		if account.ProxyID == nil || *account.ProxyID <= 0 {
			return nil, ErrOwnedAccountProxyRequired
		}
		// 重新鉴权（更新凭据）时复核既有代理，附带遗留归属豁免。
		if _, err := s.ensureOwnedProxyUsableForLogin(ctx, NewOwnedProxyScope(account.Platform, account.AccountLevel, ownerUserID), *account.ProxyID); err != nil {
			return nil, err
		}
	}
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
	if err := validateOwnedAccountSourceMutation(
		account.Platform, account.Type,
		storedCredentials, storedExtra,
		account.Credentials, account.Extra,
	); err != nil {
		return nil, err
	}
	if req.Credentials != nil || req.Extra != nil {
		if err := s.ensureOwnedAccountNotDuplicate(ctx, ownerUserID, account, account.ID); err != nil {
			return nil, err
		}
	}
	if NormalizeAccountLevel(before.AccountLevel) != NormalizeAccountLevel(account.AccountLevel) &&
		accountHasExternalPlacement(before) {
		if NormalizeAccountShareMode(before.ShareMode) == AccountShareModePublic {
			groupIDs, err = s.prepareOwnedPublicShareRevalidation(ctx, ownerUserID, account)
			if err != nil {
				return nil, err
			}
			shouldBindGroups = true
		} else if err := s.convertOwnedExternalPlacementToPrivateForIdentityChange(ctx, ownerUserID, account); err != nil {
			return nil, err
		}
	}
	agentIdentityAuthChanged := ownedAgentIdentityAuthMaterialChanged(before, account)
	agentIdentityPublicAccessRevoked := ownedAgentIdentityPublicAccessRevoked(before, account)
	shouldInvalidateAgentIdentityWS := agentIdentityAuthChanged || agentIdentityPublicAccessRevoked
	if shouldInvalidateAgentIdentityWS && s.agentIdentityWSInvalidator == nil {
		return nil, ErrOwnedAgentIdentityWSInvalidatorUnavailable
	}
	if agentIdentityAuthChanged && NormalizeAccountShareMode(account.ShareMode) == AccountShareModePublic {
		groupIDs, err = s.prepareOwnedPublicShareRevalidation(ctx, ownerUserID, account)
		if err != nil {
			return nil, err
		}
		shouldBindGroups = true
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
	targetGroupIDs := append([]int64(nil), before.GroupIDs...)
	if shouldBindGroups {
		targetGroupIDs = append([]int64(nil), groupIDs...)
	}
	intent := strings.TrimSpace(req.MutationIntent)
	if intent == "" {
		intent = AccountMutationIntentOwner
	}
	guardRequest := AccountMutationGuardRequest{
		Targets: []AccountMutationGuardTarget{{
			AccountID:         account.ID,
			ExpectedUpdatedAt: before.UpdatedAt,
			After:             account,
			GroupIDs:          targetGroupIDs,
		}},
		ActorUserID: ownerUserID,
		Intent:      intent,
	}
	_, atomicMutationGuard := s.accountRepo.(AccountMutationGuardRepository)
	if err := s.withAccountMutationGuard(ctx, guardRequest, func(txCtx context.Context) error {
		if req.LoadFactor != nil {
			repo, ok := s.accountRepo.(ownedLoadFactorCreditAccountRepository)
			if !ok {
				return ErrOwnedAccountLoadFactorCreditsUnavailable
			}
			var updateErr error
			account, updateErr = repo.UpdateOwnedAccountWithLoadFactorCredits(txCtx, ownerUserID, account)
			if updateErr != nil {
				return fmt.Errorf("update account: %w", updateErr)
			}
		} else if updateErr := s.accountRepo.Update(txCtx, account); updateErr != nil {
			return fmt.Errorf("update account: %w", updateErr)
		}
		if shouldInvalidateAgentIdentityWS && !atomicMutationGuard {
			s.agentIdentityWSInvalidator.InvalidateAgentIdentityWSConnections(account.ID)
		}
		if shouldBindGroups {
			if bindErr := s.accountRepo.BindGroups(txCtx, account.ID, groupIDs); bindErr != nil {
				return fmt.Errorf("bind groups: %w", bindErr)
			}
			account.GroupIDs = append([]int64(nil), groupIDs...)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if shouldInvalidateAgentIdentityWS && atomicMutationGuard && !AccountMutationGuardActive(ctx) {
		s.agentIdentityWSInvalidator.InvalidateAgentIdentityWSConnections(account.ID)
	}
	if !AccountMutationGuardActive(ctx) {
		s.notifyAccountChanged(ctx, before, account)
	}
	return account, nil
}

func (s *AccountService) withAccountMutationGuard(
	ctx context.Context,
	request AccountMutationGuardRequest,
	mutate func(context.Context) error,
) error {
	if AccountMutationGuardActive(ctx) {
		return mutate(ctx)
	}
	repo, ok := s.accountRepo.(AccountMutationGuardRepository)
	if ok && repo != nil {
		return repo.WithAccountMutationGuard(ctx, request, mutate)
	}
	for _, target := range request.Targets {
		if target.After == nil {
			continue
		}
		if target.After.AccountShareModeListingID != nil ||
			(target.After.ExternalPlacement != nil && target.After.ExternalPlacement.Target == AccountExternalPlacementRoom) {
			return ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
				"account_id": fmt.Sprintf("%d", target.AccountID),
			})
		}
	}
	// Lightweight test/legacy repositories cannot contain the SQL room
	// projection. Production accountRepository always implements the guard.
	return mutate(ctx)
}

func ownedAgentIdentityAuthMaterialChanged(before, after *Account) bool {
	beforeIsAgentIdentity := before != nil && before.IsOpenAIAgentIdentity()
	afterIsAgentIdentity := after != nil && after.IsOpenAIAgentIdentity()
	if !beforeIsAgentIdentity && !afterIsAgentIdentity {
		return false
	}
	if beforeIsAgentIdentity != afterIsAgentIdentity {
		return true
	}
	for _, key := range []string{
		"auth_mode",
		"agent_runtime_id",
		"agent_private_key",
		"task_id",
		"chatgpt_account_id",
		"chatgpt_user_id",
	} {
		if strings.TrimSpace(before.GetCredential(key)) != strings.TrimSpace(after.GetCredential(key)) {
			return true
		}
	}
	return false
}

func ownedAgentIdentityPublicAccessRevoked(before, after *Account) bool {
	return before != nil && after != nil && before.IsOpenAIAgentIdentity() && before.IsPublicShareApproved() && !after.IsPublicShareApproved()
}

func (s *AccountService) DeleteOwned(ctx context.Context, ownerUserID, accountID int64, force bool) error {
	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return err
	}
	deletionRepo, err := s.accountOwnedDeletionGuardRepository(map[string]string{
		"account_id": fmt.Sprintf("%d", accountID),
		"operation":  "delete_owned_account",
	})
	if err != nil {
		return err
	}
	deleteErr := deletionRepo.DeleteOwnedIfUnblocked(ctx, ownerUserID, accountID)
	if deleteErr != nil && force && isRoomAccountDeletionBlocked(deleteErr) {
		// 用户已在二次确认弹窗确认，尝试把账号从广场房间退出后再删除。
		if detachErr := s.detachRoomAccountsForDeletion(ctx, ownerUserID, deleteErr); detachErr != nil {
			return detachErr
		}
		deleteErr = deletionRepo.DeleteOwnedIfUnblocked(ctx, ownerUserID, accountID)
	}
	if deleteErr != nil {
		return fmt.Errorf("delete account: %w", deleteErr)
	}
	s.notifyAccountDeleted(ctx, account)
	return nil
}

func (s *AccountService) BulkDeleteOwned(ctx context.Context, ownerUserID int64, accountIDs []int64, force bool) (*BulkUpdateAccountsResult, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	ids := normalizeOwnedBulkAccountIDs(accountIDs)
	if len(ids) == 0 {
		return &BulkUpdateAccountsResult{
			SuccessIDs: []int64{},
			FailedIDs:  []int64{},
			Results:    []BulkUpdateAccountResult{},
		}, nil
	}

	deletionRepo, err := s.accountOwnedDeletionGuardRepository(map[string]string{
		"account_ids": joinInt64Metadata(ids),
		"operation":   "bulk_delete_owned_accounts",
	})
	if err != nil {
		return nil, err
	}

	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get accounts for bulk delete: %w", err)
	}
	accountsByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account == nil {
			return nil, ErrAccountNotFound
		}
		if account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
			return nil, ErrAccountNotFound
		}
		accountsByID[account.ID] = account
	}
	if len(accountsByID) != len(ids) {
		return nil, ErrAccountNotFound
	}
	for _, accountID := range ids {
		if accountsByID[accountID] == nil {
			return nil, ErrAccountNotFound
		}
	}

	deleteErr := deletionRepo.DeleteManyOwnedIfUnblocked(ctx, ownerUserID, ids)
	if deleteErr != nil && force {
		// 原子批量删除一次只报告一个被房间占用的账号。用户已确认，逐个退房后重试，
		// 直到删除成功或遇到无法自动解决的拦截（如房间无健康替补账号）。
		for attempt := 0; attempt < len(ids) && deleteErr != nil && isRoomAccountDeletionBlocked(deleteErr); attempt++ {
			if detachErr := s.detachRoomAccountsForDeletion(ctx, ownerUserID, deleteErr); detachErr != nil {
				return nil, detachErr
			}
			deleteErr = deletionRepo.DeleteManyOwnedIfUnblocked(ctx, ownerUserID, ids)
		}
	}
	if deleteErr != nil {
		return nil, fmt.Errorf("bulk delete accounts: %w", deleteErr)
	}

	result := &BulkUpdateAccountsResult{
		SuccessIDs: make([]int64, 0, len(ids)),
		FailedIDs:  []int64{},
		Results:    make([]BulkUpdateAccountResult, 0, len(ids)),
	}
	for _, accountID := range ids {
		entry := BulkUpdateAccountResult{AccountID: accountID, Success: true}
		result.Success++
		result.SuccessIDs = append(result.SuccessIDs, accountID)
		result.Results = append(result.Results, entry)
		s.notifyAccountDeleted(ctx, accountsByID[accountID])
	}
	return result, nil
}

func (s *AccountService) accountDeletionGuardRepository(metadata map[string]string) (AccountDeletionGuardRepository, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrAccountDeletionGuardUnavailable.WithMetadata(metadata)
	}
	repo, ok := s.accountRepo.(AccountDeletionGuardRepository)
	if !ok || repo == nil {
		return nil, ErrAccountDeletionGuardUnavailable.WithMetadata(metadata)
	}
	return repo, nil
}

func (s *AccountService) accountOwnedDeletionGuardRepository(metadata map[string]string) (AccountOwnedDeletionGuardRepository, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrAccountDeletionGuardUnavailable.WithMetadata(metadata)
	}
	repo, ok := s.accountRepo.(AccountOwnedDeletionGuardRepository)
	if !ok || repo == nil {
		return nil, ErrAccountDeletionGuardUnavailable.WithMetadata(metadata)
	}
	return repo, nil
}

// isRoomAccountDeletionBlocked 判断删除守卫返回的错误是否是「账号仍挂在广场房间」这类
// 可以通过退房自动解除的拦截。只有 blocker_types 里含 room_account 才返回 true。
func isRoomAccountDeletionBlocked(err error) bool {
	if !errors.Is(err, ErrAccountDeletionBlocked) {
		return false
	}
	appErr := infraerrors.FromError(err)
	if appErr == nil {
		return false
	}
	return roomListingIDsFromBlocker(appErr) != nil
}

// roomListingIDsFromBlocker 从删除守卫的 metadata 里解析出账号当前所在的房间 listing ID。
func roomListingIDsFromBlocker(appErr *infraerrors.ApplicationError) []int64 {
	if appErr == nil {
		return nil
	}
	raw := strings.TrimSpace(appErr.Metadata["room_listing_ids"])
	if raw == "" {
		return nil
	}
	ids := make([]int64, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, convErr := strconv.ParseInt(part, 10, 64)
		if convErr != nil || id <= 0 {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// detachRoomAccountsForDeletion 在用户确认强制删除后，把被拦截账号从其所在的广场房间退出。
// 退房复用 DetachRoomAccountsAtomic：内部会先尝试把活跃租户重绑到房间内其它健康账号，
// 只有房间没有可接替的健康账号时才会失败（no_healthy_replacement_account），该错误原样透传，
// 由上层告知号主房间仍有租户在用、无法删除。
func (s *AccountService) detachRoomAccountsForDeletion(ctx context.Context, ownerUserID int64, blockedErr error) error {
	if s == nil || s.accountShareRoomRepo == nil {
		return ErrOwnedAccountShareModeBoundaryUnavailable
	}
	appErr := infraerrors.FromError(blockedErr)
	listingIDs := roomListingIDsFromBlocker(appErr)
	if len(listingIDs) == 0 {
		return blockedErr
	}
	accountID, _ := strconv.ParseInt(strings.TrimSpace(appErr.Metadata["account_id"]), 10, 64)
	if accountID <= 0 {
		return blockedErr
	}
	// 退房会打断账号上正在进行的会话，若账号仍有在途请求则拒绝，避免中断活跃调用。
	if s.concurrencyService != nil {
		inFlight, concErr := s.concurrencyService.GetAccountConcurrencyBatch(ctx, []int64{accountID})
		if concErr != nil {
			return concErr
		}
		if inFlight[accountID] > 0 {
			return ErrAccountShareListingInUse.WithMetadata(map[string]string{
				"blocker":               "account_in_flight",
				"account_id":            strconv.FormatInt(accountID, 10),
				"in_flight_concurrency": strconv.Itoa(inFlight[accountID]),
			})
		}
	}
	for _, listingID := range listingIDs {
		input := BatchAccountShareRoomAccountsInput{
			ListingID:      listingID,
			AccountIDs:     []int64{accountID},
			OwnerUserID:    ownerUserID,
			IdempotencyKey: fmt.Sprintf("account-delete-detach-%d-%d", accountID, listingID),
		}
		billing, detachErr := s.accountShareRoomRepo.DetachRoomAccountsAtomic(ctx, input)
		if detachErr != nil {
			return detachErr
		}
		if s.accountShareBillingCache != nil {
			s.accountShareBillingCache.invalidateSeatBillingCaches(billing)
		}
	}
	return nil
}

func joinInt64Metadata(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ",")
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
		if account.IsOpenAIAgentIdentity() {
			add("openai.agent_identity_team", account.GetChatGPTAccountID())
			return keys
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
	if err := rejectOwnedAccountGrokManagedExtra(input.Extra); err != nil {
		return nil, err
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
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	updatedIdentityAccounts := make([]*Account, 0, len(accountIDs))
	agentIdentityWSInvalidationIDs := make([]int64, 0, len(accountIDs))
	guardTargets := make([]AccountMutationGuardTarget, 0, len(accountIDs))
	// 单个账号自身的校验失败只淘汰它自己：批量切调度不该因为选中列表里有一个
	// 状态异常的账号，就让其余账号一起不生效。归属校验等安全性错误仍然整批中止。
	applyIDs := make([]int64, 0, len(accountIDs))
	recordBulkFailure := func(accountID int64, cause error) {
		result.Failed++
		result.FailedIDs = append(result.FailedIDs, accountID)
		result.Results = append(result.Results, BulkUpdateAccountResult{
			AccountID: accountID,
			Success:   false,
			Error:     cause.Error(),
		})
	}
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
			recordBulkFailure(accountID, err)
			continue
		}
		nextAccount := *account
		nextAccount.Credentials = nextCredentials
		nextAccount.Extra = nextExtra
		if err := validateOwnedAccountSourceMutation(
			account.Platform, account.Type,
			account.Credentials, account.Extra,
			nextCredentials, nextExtra,
		); err != nil {
			recordBulkFailure(accountID, err)
			continue
		}
		nextConcurrency := normalizeOwnedPersonalAccountConcurrency(account.Concurrency)
		if input.Concurrency != nil {
			nextConcurrency = *input.Concurrency
		}
		nextLoadFactor := account.LoadFactor
		if input.LoadFactor != nil {
			nextLoadFactor = normalizeLoadFactor(input.LoadFactor)
		}
		nextAccountLevel := NormalizeOpenAIAccountLevelWithConfigs(account.Platform, account.AccountLevel, nextCredentials, nextExtra, levelConfigs)
		if input.AccountLevel != nil {
			nextAccountLevel = NormalizeAccountLevel(*input.AccountLevel)
		}
		if err := ValidateOpenAIPlusConcurrency(account.Platform, nextAccountLevel, nextConcurrency); err != nil {
			recordBulkFailure(accountID, err)
			continue
		}
		if err := ValidateAccountLoadFactor(nextLoadFactor); err != nil {
			recordBulkFailure(accountID, err)
			continue
		}
		nextAccount.Concurrency = nextConcurrency
		nextAccount.LoadFactor = nextLoadFactor
		nextAccount.AccountLevel = nextAccountLevel
		if input.Priority != nil {
			nextAccount.Priority = *input.Priority
		}
		if status != "" {
			nextAccount.Status = status
		}
		if input.Schedulable != nil {
			nextAccount.Schedulable = *input.Schedulable
		}
		if shareMode != "" {
			nextAccount.ShareMode = shareMode
		}
		if len(input.Credentials) > 0 || len(input.Extra) > 0 {
			// 身份冲突是"这批请求本身"的性质：同一份凭据会写到所有选中账号上，
			// 撞车说明请求写错了，整批拒绝而不是逐个淘汰。
			if err := s.ensureOwnedAccountNotDuplicate(ctx, ownerUserID, &nextAccount, accountIDs...); err != nil {
				return nil, err
			}
			updatedIdentityAccounts = append(updatedIdentityAccounts, &nextAccount)
		}
		if ownedAgentIdentityAuthMaterialChanged(account, &nextAccount) ||
			ownedAgentIdentityPublicAccessRevoked(account, &nextAccount) {
			agentIdentityWSInvalidationIDs = append(agentIdentityWSInvalidationIDs, account.ID)
		}
		guardTargets = append(guardTargets, AccountMutationGuardTarget{
			AccountID:         account.ID,
			ExpectedUpdatedAt: account.UpdatedAt,
			After:             &nextAccount,
			GroupIDs:          append([]int64(nil), account.GroupIDs...),
		})
		applyIDs = append(applyIDs, accountID)
	}
	if err := ensureOwnedAccountBatchNotDuplicate(updatedIdentityAccounts); err != nil {
		return nil, err
	}
	if len(applyIDs) == 0 {
		return result, nil
	}

	requiresPerAccountUpdate := input.LoadFactor != nil || shareMode != "" || len(input.Credentials) > 0 || len(input.Extra) > 0
	if requiresPerAccountUpdate {
		guardRequest := AccountMutationGuardRequest{
			Targets:     guardTargets,
			ActorUserID: ownerUserID,
			Intent:      AccountMutationIntentOwner,
		}
		if err := s.withAccountMutationGuard(ctx, guardRequest, func(txCtx context.Context) error {
			for _, accountID := range applyIDs {
				account := accountsByID[accountID]
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
					extra, normalizeErr := NormalizeCodexQuotaLimitExtra(account.Platform, account.Type, extra)
					if normalizeErr != nil {
						return normalizeErr
					}
					updateReq.Extra = &extra
				}
				if _, updateErr := s.UpdateOwned(txCtx, ownerUserID, accountID, updateReq); updateErr != nil {
					return updateErr
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
		if _, atomicGuard := s.accountRepo.(AccountMutationGuardRepository); atomicGuard {
			for _, accountID := range agentIdentityWSInvalidationIDs {
				s.agentIdentityWSInvalidator.InvalidateAgentIdentityWSConnections(accountID)
			}
		}
		for _, accountID := range applyIDs {
			entry := BulkUpdateAccountResult{AccountID: accountID, Success: true}
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, accountID)
			result.Results = append(result.Results, entry)
		}
		s.notifyBulkOwnedAccountsChanged(ctx, accountsByID, applyIDs)
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

	if err := s.withAccountMutationGuard(ctx, AccountMutationGuardRequest{
		Targets:     guardTargets,
		ActorUserID: ownerUserID,
		Intent:      AccountMutationIntentOwner,
	}, func(txCtx context.Context) error {
		updated, updateErr := s.accountRepo.BulkUpdate(txCtx, applyIDs, repoUpdates)
		if updateErr != nil {
			return fmt.Errorf("bulk update owned accounts: %w", updateErr)
		}
		if updated != int64(len(applyIDs)) {
			return ErrAccountNotFound
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for _, accountID := range applyIDs {
		entry := BulkUpdateAccountResult{AccountID: accountID, Success: true}
		result.Success++
		result.SuccessIDs = append(result.SuccessIDs, accountID)
		result.Results = append(result.Results, entry)
	}
	s.notifyBulkOwnedAccountsChanged(ctx, accountsByID, applyIDs)

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

	deletionRepo, err := s.accountDeletionGuardRepository(map[string]string{
		"account_id": fmt.Sprintf("%d", id),
		"operation":  "delete_account",
	})
	if err != nil {
		return err
	}
	if err := deletionRepo.DeleteIfUnblocked(ctx, id); err != nil {
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

func (s *AccountService) ConvertOwnedExternalPlacement(ctx context.Context, ownerUserID, accountID int64, input ConvertAccountExternalPlacementInput) (*ConvertAccountExternalPlacementResult, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if accountID <= 0 {
		return nil, ErrAccountNotFound
	}
	if s == nil || s.accountRepo == nil || s.accountShareModeRepo == nil || s.accountShareRoomRepo == nil {
		return nil, ErrOwnedAccountShareModeBoundaryUnavailable
	}
	target := strings.ToLower(strings.TrimSpace(input.Target))
	switch target {
	case AccountExternalPlacementPrivate, AccountExternalPlacementPublicPool, AccountExternalPlacementRoom:
	default:
		return nil, ErrAccountExternalPlacementInvalid
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, ErrAccountExternalPlacementInvalid.WithMetadata(map[string]string{"field": "idempotency_key"})
	}
	if input.RoomID != nil {
		return nil, ErrAccountExternalPlacementInvalid.WithMetadata(map[string]string{"field": "room_id"})
	}

	account, err := s.GetOwnedByID(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	if target != AccountExternalPlacementRoom {
		attached, err := s.accountShareRoomRepo.HasRoomAccount(ctx, ownerUserID, accountID)
		if err != nil {
			return nil, err
		}
		if attached {
			return nil, ErrAccountShareRoomAccountAttached
		}
	}
	previousAccount := cloneAccountForNotice(account)
	placementChanged := !accountExternalPlacementMatchesTarget(account.ExternalPlacement, target, input.RoomID)
	drained := false
	if placementChanged {
		drained, err = s.accountShareRoomRepo.BeginExternalPlacementDrain(ctx, ownerUserID, accountID)
		if err != nil {
			return nil, err
		}
		if drained {
			defer func() {
				if !drained {
					return
				}
				if restoreErr := s.accountShareRoomRepo.RestoreExternalPlacementAfterDrain(context.WithoutCancel(ctx), ownerUserID, accountID); restoreErr != nil {
					slog.Error("account.external_placement_restore_failed", "account_id", accountID, "error", restoreErr)
				}
			}()
			if err := s.ensureOwnedAccountExternalPlacementIdle(ctx, account); err != nil {
				return nil, err
			}
		}
	}

	privateGroup, err := s.getPrivateGroupForOwnedAccount(ctx, ownerUserID, account.Platform)
	if err != nil {
		return nil, err
	}
	groupIDs := []int64{privateGroup.ID}
	var publicGroupID *int64

	switch target {
	case AccountExternalPlacementPublicPool:
		if err := validateOwnedAccountSourceForPlatform(account.Platform, account.Type, account.Credentials, account.Extra); err != nil {
			return nil, err
		}
		if !isOwnedAccountPublicShareApprovable(account, false) {
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
		groupIDs, err = s.publicOwnedAccountGroupIDs(ctx, ownerUserID, account, publicGroup)
		if err != nil {
			return nil, err
		}
		publicGroupID = &publicGroup.ID
	case AccountExternalPlacementRoom:
		accountLevel, err := s.canonicalOwnedAccountRoomLevel(ctx, account)
		if err != nil {
			return nil, err
		}
		if accountLevel == AccountLevelUnknown {
			return nil, ErrAccountShareRoomUnknownLevel
		}
		modeGroup, err := s.accountShareModeRepo.GetModeGroup(ctx, account.Platform)
		if err != nil {
			return nil, err
		}
		if modeGroup == nil || modeGroup.ID <= 0 {
			return nil, ErrAccountShareModeGroupUnavailable
		}
		groupIDs = []int64{privateGroup.ID, modeGroup.ID}
	}

	result, err := s.accountShareRoomRepo.ConvertExternalPlacement(ctx, ConvertAccountExternalPlacementInput{
		AccountID:      accountID,
		OwnerUserID:    ownerUserID,
		Target:         target,
		RoomID:         input.RoomID,
		IdempotencyKey: idempotencyKey,
		GroupIDs:       uniquePositiveInt64s(groupIDs),
		PublicGroupID:  publicGroupID,
	})
	if err != nil {
		return nil, err
	}
	if drained {
		if err := s.accountShareRoomRepo.RestoreExternalPlacementAfterDrain(
			context.WithoutCancel(ctx),
			ownerUserID,
			accountID,
		); err != nil {
			return nil, err
		}
		drained = false
	}
	if s.accountShareBillingCache != nil {
		s.accountShareBillingCache.invalidateSeatBillingCaches(result.SeatBillingResult)
	}
	updated, getErr := s.accountRepo.GetByID(ctx, accountID)
	if getErr == nil && updated != nil {
		s.notifyAccountChanged(ctx, previousAccount, updated)
	}
	return result, nil
}

func accountExternalPlacementMatchesTarget(placement *AccountExternalPlacement, target string, roomID *int64) bool {
	currentTarget := AccountExternalPlacementPrivate
	if placement != nil && strings.TrimSpace(placement.Target) != "" {
		currentTarget = strings.ToLower(strings.TrimSpace(placement.Target))
	}
	if currentTarget != target {
		return false
	}
	if placement != nil && placement.State == "draining" {
		return false
	}
	if target != AccountExternalPlacementRoom {
		return true
	}
	return placement != nil && placement.RoomID != nil && roomID != nil && *placement.RoomID == *roomID
}

func (s *AccountService) ensureOwnedAccountExternalPlacementIdle(ctx context.Context, account *Account) error {
	if s == nil {
		return ErrServiceUnavailable
	}
	return ensureAccountExternalPlacementIdle(ctx, s.concurrencyService, account)
}

func ensureAccountExternalPlacementIdle(ctx context.Context, concurrencyService *ConcurrencyService, account *Account) error {
	if account == nil || account.ID <= 0 {
		return ErrAccountExternalPlacementInvalid
	}
	if concurrencyService == nil {
		return ErrServiceUnavailable
	}
	loadByAccountID, err := concurrencyService.GetAccountsLoadBatch(ctx, []AccountWithConcurrency{{
		ID:             account.ID,
		MaxConcurrency: account.Concurrency,
	}})
	if err != nil {
		return err
	}
	load := loadByAccountID[account.ID]
	if load != nil && (load.CurrentConcurrency > 0 || load.WaitingCount > 0) {
		return ErrAccountExternalPlacementBusy
	}
	return nil
}

func (s *AccountService) canonicalOwnedAccountRoomLevel(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return AccountLevelUnknown, ErrAccountNotFound
	}
	if account.Platform != PlatformOpenAI {
		return NormalizeAccountLevel(account.AccountLevel), nil
	}
	configs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return AccountLevelUnknown, err
	}
	return NormalizeOpenAIAccountLevelWithConfigs(account.Platform, account.AccountLevel, account.Credentials, account.Extra, configs), nil
}

func (s *AccountService) prepareOwnedPublicShareRevalidation(ctx context.Context, ownerUserID int64, account *Account) ([]int64, error) {
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if err := s.convertOwnedExternalPlacementToPrivateForIdentityChange(ctx, ownerUserID, account); err != nil {
		return nil, err
	}
	groupIDs, err := s.initialOwnedAccountGroupIDs(ctx, ownerUserID, account.Platform, account.Type, AccountShareModePublic, nil)
	if err != nil {
		return nil, err
	}
	account.ShareMode = AccountShareModePublic
	account.ShareStatus = AccountShareStatusPending
	account.ErrorMessage = ""
	return groupIDs, nil
}

func accountHasExternalPlacement(account *Account) bool {
	if account == nil || account.ExternalPlacement == nil {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(account.ExternalPlacement.Target))
	return target == AccountExternalPlacementPublicPool || target == AccountExternalPlacementRoom
}

func (s *AccountService) convertOwnedExternalPlacementToPrivateForIdentityChange(ctx context.Context, ownerUserID int64, account *Account) error {
	if !accountHasExternalPlacement(account) {
		return nil
	}
	result, err := s.ConvertOwnedExternalPlacement(ctx, ownerUserID, account.ID, ConvertAccountExternalPlacementInput{
		Target:         AccountExternalPlacementPrivate,
		IdempotencyKey: fmt.Sprintf("identity-change:%d:%d", account.ID, time.Now().UTC().UnixNano()),
	})
	if err != nil {
		return err
	}
	if result == nil || result.Current == nil || result.Current.Target != AccountExternalPlacementPrivate {
		return ErrAccountExternalPlacementConflict
	}
	account.ExternalPlacement = result.Current
	return nil
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
	if err := validateOwnedAccountSourceForPlatform(account.Platform, account.Type, account.Credentials, account.Extra); err != nil {
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
	_, err = s.ConvertOwnedExternalPlacement(ctx, ownerUserID, account.ID, ConvertAccountExternalPlacementInput{
		Target:         AccountExternalPlacementPublicPool,
		IdempotencyKey: fmt.Sprintf("public-approval:%d:%d", account.ID, time.Now().UTC().UnixNano()),
	})
	if err != nil {
		return nil, err
	}
	return s.GetOwnedByID(ctx, ownerUserID, account.ID)
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
	groupIDs, err := s.prepareOwnedPublicShareRevalidation(ctx, ownerUserID, account)
	if err != nil {
		return nil, err
	}
	account.ErrorMessage = strings.TrimSpace(reason)
	shouldInvalidateAgentIdentityWS := ownedAgentIdentityPublicAccessRevoked(before, account)
	if shouldInvalidateAgentIdentityWS && s.agentIdentityWSInvalidator == nil {
		return nil, ErrOwnedAgentIdentityWSInvalidatorUnavailable
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("update account public share status: %w", err)
	}
	if shouldInvalidateAgentIdentityWS {
		s.agentIdentityWSInvalidator.InvalidateAgentIdentityWSConnections(account.ID)
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

	if accountHasExternalPlacement(before) {
		if account.OwnerUserID == nil {
			return nil, false, ErrAccountExternalPlacementConflict
		}
		if err := s.convertOwnedExternalPlacementToPrivateForIdentityChange(ctx, *account.OwnerUserID, account); err != nil {
			return nil, false, err
		}
	}
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
	shouldInvalidateAgentIdentityWS := ownedAgentIdentityPublicAccessRevoked(before, account)
	if shouldInvalidateAgentIdentityWS && s.agentIdentityWSInvalidator == nil {
		return nil, false, ErrOwnedAgentIdentityWSInvalidatorUnavailable
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, false, fmt.Errorf("update account suspected free repair: %w", err)
	}
	if shouldInvalidateAgentIdentityWS {
		s.agentIdentityWSInvalidator.InvalidateAgentIdentityWSConnections(account.ID)
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
		eligible, err := s.isOwnedPublicSharePoolGroup(ctx, &group, account.Platform)
		if err != nil {
			return nil, err
		}
		if !eligible {
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
		levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
		if err != nil {
			return nil, err
		}
		accountLevel := NormalizeOpenAISharedPoolAccountLevel(NormalizeOpenAIAccountLevelWithConfigs(account.Platform, account.AccountLevel, account.Credentials, account.Extra, levelConfigs))
		if OpenAISharedPoolLevelRankWithConfigs(accountLevel, levelConfigs) == 0 {
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
			if requiredLevel == "" || !CanOpenAIAccountJoinSharedPoolWithConfigs(accountLevel, requiredLevel, levelConfigs) {
				continue
			}
			eligible, err := s.isOwnedPublicSharePoolGroup(ctx, &group, platform)
			if err != nil {
				return nil, err
			}
			if !eligible {
				continue
			}
			requiredRank := OpenAISharedPoolLevelRankWithConfigs(requiredLevel, levelConfigs)
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
		if NormalizeRequiredAccountLevel(group.RequiredAccountLevel) != "" {
			continue
		}
		eligible, err := s.isOwnedPublicSharePoolGroup(ctx, &group, platform)
		if err != nil {
			return nil, err
		}
		if eligible {
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

func (s *AccountService) isOwnedPublicSharePoolGroup(ctx context.Context, group *Group, platform string) (bool, error) {
	if !isOwnedPublicSharePoolGroup(group, platform) {
		return false, nil
	}
	classifier := s.accountShareModeGroups
	if classifier == nil {
		classifier, _ = s.groupRepo.(accountShareModeGroupClassifier)
	}
	if classifier == nil {
		return false, ErrOwnedAccountGroupValidationUnavailable
	}
	isModeGroup, err := classifier.IsModeGroup(ctx, group.ID)
	if err != nil {
		return false, fmt.Errorf("classify account share mode group: %w", err)
	}
	return !isModeGroup, nil
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
	case PlatformOpenAI, PlatformAntigravity, PlatformAnthropic, PlatformGemini, PlatformGrok:
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
