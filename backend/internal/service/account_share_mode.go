package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const (
	AccountShareModeGroupPlatformOpenAI = PlatformOpenAI

	AccountShareListingStatusActive   = "active"
	AccountShareListingStatusPaused   = "paused"
	AccountShareListingStatusDisabled = "disabled"

	AccountShareMembershipStatusActive = "active"
	AccountShareMembershipStatusEnded  = "ended"

	AccountShareModeDefaultMinBalance          = 1.0
	AccountShareModeDefaultPlatformShareRatio  = 0.10
	AccountShareModeDefaultOwnerShareRatio     = 0.90
	AccountShareModeDefaultCodexLimitPercent   = CodexQuotaDefaultLimitPercent
	AccountShareModeMinSeats                   = 2
	AccountShareModeMaxSeats                   = 12
	AccountShareModeDefaultPerUserConcurrency  = 5
	AccountShareModeDefaultAccountConcurrency  = 20
	AccountShareModeMaxAccountConcurrency      = 50
	AccountShareModeSeatPrepayDuration         = time.Minute
	AccountShareModeSeatBillingInterval        = 15 * time.Second
	AccountShareModeSeatBillingBatchSize       = 100
	AccountShareModeEndMembershipTokenTTL      = 2 * time.Minute
	AccountShareModeMaxIdleTimeoutMinutes      = 10080
	AccountShareModeLastRequestTouchInterval   = 30 * time.Second
	AccountShareModeEditSessionTTL             = 10 * time.Minute
	AccountShareModeListingTabUsing            = "using"
	AccountShareModeListingTabHistory          = "history"
	AccountShareModeListingTabAll              = "all"
	AccountShareModeListingTabMine             = "mine"
	AccountShareMembershipEndReasonManual      = "manual"
	AccountShareMembershipEndReasonIdleTimeout = "idle_timeout"
	AccountShareMembershipEndReasonPrepay      = "prepay_insufficient"
	AccountShareMembershipEndReasonUnavailable = "account_unavailable"
	accountShareModeContextBindingMissingError = "该分组未绑定账号"
	accountShareModeEndMembershipTokenAction   = "account_share_mode:end_membership:v1"
)

var accountShareModeDefaultAllowedModels = []string{
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"codex-auto-review",
}

var (
	ErrAccountShareModeGroupUnbound            = infraerrors.New(http.StatusBadRequest, "ACCOUNT_SHARE_MODE_GROUP_UNBOUND", accountShareModeContextBindingMissingError)
	ErrAccountShareModeGroupUnavailable        = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE", "account share mode group is not configured")
	ErrAccountShareListingNotFound             = infraerrors.NotFound("ACCOUNT_SHARE_LISTING_NOT_FOUND", "account share listing not found")
	ErrAccountShareListingNotActive            = infraerrors.BadRequest("ACCOUNT_SHARE_LISTING_NOT_ACTIVE", "account share listing is not active")
	ErrAccountShareListingFull                 = infraerrors.BadRequest("ACCOUNT_SHARE_LISTING_FULL", "account share listing is full")
	ErrAccountShareOwnerCannotJoin             = infraerrors.BadRequest("ACCOUNT_SHARE_OWNER_CANNOT_JOIN", "owner cannot join own shared account")
	ErrAccountShareAlreadyUsing                = infraerrors.Conflict("ACCOUNT_SHARE_ALREADY_USING", "user is already using an account share listing")
	ErrAccountShareAPIKeyAlreadyBound          = infraerrors.Conflict("ACCOUNT_SHARE_API_KEY_ALREADY_BOUND", "api key is already bound to an account share listing")
	ErrAccountShareAPIKeyMustUseModeGroup      = infraerrors.BadRequest("ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP", "api key must use account mode group")
	ErrAccountShareBalanceBelowMinimum         = infraerrors.Forbidden("ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM", "user balance is below account share minimum")
	ErrAccountSharePerUserConcurrencyExceeded  = infraerrors.TooManyRequests("ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED", "account share per-user concurrency exceeded")
	ErrAccountShareModeOpenAIOnly              = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_OPENAI_ONLY", "account share mode only supports OpenAI OAuth accounts")
	ErrAccountShareModeProxyRequired           = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_PROXY_REQUIRED", "proxy is required before OpenAI OAuth login")
	ErrAccountShareModeAllowedModelsRequired   = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_MODELS_REQUIRED", "at least one allowed model is required")
	ErrAccountShareModeInvalidSeats            = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_SEATS", "seat_limit must be between 2 and 12")
	ErrAccountShareModeInvalidRateMultiplier   = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_RATE_MULTIPLIER", "rate_multiplier must be non-negative")
	ErrAccountShareModeInvalidConcurrency      = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_CONCURRENCY", "concurrency must be positive and no greater than 50")
	ErrAccountShareModeInsufficientConcurrency = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INSUFFICIENT_CONCURRENCY", "concurrency must be at least per_user_concurrency multiplied by seat_limit")
	ErrAccountShareModeInvalidHourlyRate       = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_HOURLY_RATE", "hourly_rate must be non-negative")
	ErrAccountShareModeInvalidMinBalance       = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_MIN_BALANCE", "min_balance_required must be non-negative")
	ErrAccountShareModeInvalidWaiverMinimum    = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_WAIVER_MINIMUM", "hourly_fee_waiver_minimum must be non-negative")
	ErrAccountShareModePrepayInsufficient      = infraerrors.Forbidden("ACCOUNT_SHARE_MODE_PREPAY_INSUFFICIENT", "balance is insufficient for account share seat prepayment")
	ErrAccountShareAccountUnavailable          = infraerrors.Forbidden("ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE", "account share account is unavailable")
	ErrAccountShareModeInvalidName             = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_NAME", "account share account name must not contain whitespace")
	ErrAccountShareModeDuplicateName           = infraerrors.Conflict("ACCOUNT_SHARE_MODE_DUPLICATE_NAME", "account share account name already exists")
	ErrAccountShareModeInvalidPolicyRatio      = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_POLICY_RATIO", "account share mode policy ratios must be between 0 and 1 and sum to at most 1")
	ErrAccountShareModeInvalidProxy            = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_PROXY", "invalid proxy configuration")
	ErrAccountShareModePublicPoolAccount       = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_PUBLIC_POOL_ACCOUNT", "public shared pool accounts cannot be used for account share mode")
	ErrAccountShareEndTokenRequired            = infraerrors.BadRequest("ACCOUNT_SHARE_END_TOKEN_REQUIRED", "account share end confirmation token is required")
	ErrAccountShareEndTokenInvalid             = infraerrors.Forbidden("ACCOUNT_SHARE_END_TOKEN_INVALID", "account share end confirmation token is invalid or expired")
	ErrAccountShareModeInvalidIdleTimeout      = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT", "idle_timeout_minutes must be between 0 and 10080")
	ErrAccountShareListingInUse                = infraerrors.Conflict("ACCOUNT_SHARE_LISTING_IN_USE", "account share listing has active seats")
	ErrAccountShareListingEditing              = infraerrors.Conflict("ACCOUNT_SHARE_LISTING_EDITING", "account share listing is being edited")
	ErrAccountShareEditSessionRequired         = infraerrors.BadRequest("ACCOUNT_SHARE_EDIT_SESSION_REQUIRED", "account share edit session is required")
	ErrAccountShareEditSessionInvalid          = infraerrors.Conflict("ACCOUNT_SHARE_EDIT_SESSION_INVALID", "account share edit session is invalid or expired")
	ErrAccountShareRelistAccountUnavailable    = infraerrors.BadRequest("ACCOUNT_SHARE_RELIST_ACCOUNT_UNAVAILABLE", "账号测试通过，但账号状态仍不可调度，请先启用账号或恢复调度后重试")
)

type accountShareConnectivityTester interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

type accountShareAccountStateRecovery interface {
	RecoverAccountAfterSuccessfulTest(ctx context.Context, accountID int64) (*SuccessfulTestRecoveryResult, error)
}

type accountShareModeRequestContextKey struct{}

type AccountShareModeRequestContext struct {
	UserID   int64
	APIKeyID int64
	state    *accountShareModeRequestState
}

type accountShareModeRequestState struct {
	mu         sync.RWMutex
	userID     int64
	apiKeyID   int64
	groupID    int64
	resolved   bool
	membership *AccountShareMembership
	listing    *AccountShareListing
	err        error
}

func WithAccountShareModeRequest(ctx context.Context, userID, apiKeyID int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, accountShareModeRequestContextKey{}, AccountShareModeRequestContext{
		UserID:   userID,
		APIKeyID: apiKeyID,
		state: &accountShareModeRequestState{
			userID:   userID,
			apiKeyID: apiKeyID,
		},
	})
}

func WithAccountShareModeRequestFromContext(ctx context.Context, source context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, ok := AccountShareModeRequestFromContext(source)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, accountShareModeRequestContextKey{}, requestCtx)
}

func AccountShareModeRequestFromContext(ctx context.Context) (AccountShareModeRequestContext, bool) {
	if ctx == nil {
		return AccountShareModeRequestContext{}, false
	}
	value, ok := ctx.Value(accountShareModeRequestContextKey{}).(AccountShareModeRequestContext)
	return value, ok && value.UserID > 0 && value.APIKeyID > 0
}

func (s *accountShareModeRequestState) get(userID, apiKeyID, groupID int64) (*AccountShareMembership, *AccountShareListing, error, bool) {
	if s == nil {
		return nil, nil, nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.userID != userID || s.apiKeyID != apiKeyID || s.groupID != groupID || !s.resolved {
		return nil, nil, nil, false
	}
	return s.membership, s.listing, s.err, true
}

func (s *accountShareModeRequestState) set(userID, apiKeyID, groupID int64, membership *AccountShareMembership, listing *AccountShareListing, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userID = userID
	s.apiKeyID = apiKeyID
	s.groupID = groupID
	s.resolved = true
	s.membership = membership
	s.listing = listing
	s.err = err
}

type AccountShareListing struct {
	ID                          int64                     `json:"id"`
	AccountID                   int64                     `json:"account_id"`
	OwnerUserID                 int64                     `json:"owner_user_id"`
	OwnerUsername               string                    `json:"owner_username,omitempty"`
	AccountName                 string                    `json:"account_name,omitempty"`
	ProxyID                     *int64                    `json:"proxy_id,omitempty"`
	Proxy                       *AccountShareListingProxy `json:"proxy,omitempty"`
	Status                      string                    `json:"status"`
	SeatLimit                   int                       `json:"seat_limit"`
	ActiveSeats                 int                       `json:"active_seats"`
	RateMultiplier              float64                   `json:"rate_multiplier"`
	AllowedModels               []string                  `json:"allowed_models"`
	PerUserConcurrency          int                       `json:"per_user_concurrency"`
	AccountConcurrency          int                       `json:"account_concurrency"`
	HourlyRate                  float64                   `json:"hourly_rate"`
	HourlyFeeWaiverMinimum      float64                   `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired          float64                   `json:"min_balance_required"`
	CodexCLIOnly                bool                      `json:"codex_cli_only"`
	Codex5hLimitPercent         float64                   `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent         float64                   `json:"codex_7d_limit_percent"`
	AccountLevel                string                    `json:"account_level,omitempty"`
	AccountPlanType             string                    `json:"account_plan_type,omitempty"`
	AccountStatus               string                    `json:"account_status,omitempty"`
	AccountSchedulable          bool                      `json:"account_schedulable"`
	CurrentConcurrency          int                       `json:"current_concurrency"`
	AccountExpiresAt            *time.Time                `json:"account_expires_at,omitempty"`
	SubscriptionExpiresAt       *time.Time                `json:"subscription_expires_at,omitempty"`
	AccountLastUsedAt           *time.Time                `json:"account_last_used_at,omitempty"`
	RateLimitedAt               *time.Time                `json:"rate_limited_at,omitempty"`
	RateLimitResetAt            *time.Time                `json:"rate_limit_reset_at,omitempty"`
	OverloadUntil               *time.Time                `json:"overload_until,omitempty"`
	TempUnschedulableUntil      *time.Time                `json:"temp_unschedulable_until,omitempty"`
	TempUnschedulableReason     string                    `json:"temp_unschedulable_reason,omitempty"`
	CodexQuotaProtectionReason  *string                   `json:"codex_quota_protection_reason,omitempty"`
	CodexQuotaProtectionResetAt *time.Time                `json:"codex_quota_protection_reset_at,omitempty"`
	Codex5hUsage                *UsageProgress            `json:"codex_5h_usage,omitempty"`
	Codex7dUsage                *UsageProgress            `json:"codex_7d_usage,omitempty"`
	CodexUsageUpdatedAt         *time.Time                `json:"codex_usage_updated_at,omitempty"`
	CurrentMembershipID         *int64                    `json:"current_membership_id,omitempty"`
	CurrentAPIKeyID             *int64                    `json:"current_api_key_id,omitempty"`
	CurrentJoinedAt             *time.Time                `json:"current_joined_at,omitempty"`
	CurrentPaidUntil            *time.Time                `json:"current_paid_until,omitempty"`
	CurrentBilledUntil          *time.Time                `json:"current_billed_until,omitempty"`
	CurrentIdleTimeoutMinutes   *int                      `json:"current_idle_timeout_minutes,omitempty"`
	CurrentLastRequestAt        *time.Time                `json:"current_last_request_at,omitempty"`
	CurrentIdleExpiresAt        *time.Time                `json:"current_idle_expires_at,omitempty"`
	LastUsedMembershipID        *int64                    `json:"last_used_membership_id,omitempty"`
	LastUsedAt                  *time.Time                `json:"last_used_at,omitempty"`
	EditingByUserID             *int64                    `json:"editing_by_user_id,omitempty"`
	EditingByUsername           string                    `json:"editing_by_username,omitempty"`
	EditingExpiresAt            *time.Time                `json:"editing_expires_at,omitempty"`
	EditingMine                 bool                      `json:"editing_mine"`
	EditSessionID               string                    `json:"edit_session_id,omitempty"`
	CreatedAt                   time.Time                 `json:"created_at"`
	UpdatedAt                   time.Time                 `json:"updated_at"`
}

type AccountShareListingProxy struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Protocol    string    `json:"protocol"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Username    string    `json:"username"`
	OwnerUserID *int64    `json:"owner_user_id,omitempty"`
	Status      string    `json:"status"`
	MaxAccounts int       `json:"max_accounts"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AccountShareMembership struct {
	ID                             int64      `json:"id"`
	ListingID                      int64      `json:"listing_id"`
	AccountID                      int64      `json:"account_id"`
	OwnerUserID                    int64      `json:"owner_user_id,omitempty"`
	ConsumerUserID                 int64      `json:"consumer_user_id"`
	APIKeyID                       int64      `json:"api_key_id"`
	Status                         string     `json:"status"`
	HourlyRateSnapshot             float64    `json:"hourly_rate_snapshot"`
	HourlyFeeWaiverMinimumSnapshot float64    `json:"hourly_fee_waiver_minimum_snapshot"`
	IdleTimeoutMinutes             int        `json:"idle_timeout_minutes"`
	JoinedAt                       time.Time  `json:"joined_at"`
	LastRequestAt                  *time.Time `json:"last_request_at,omitempty"`
	EndedAt                        *time.Time `json:"ended_at,omitempty"`
	EndedReason                    string     `json:"ended_reason,omitempty"`
	PaidUntil                      *time.Time `json:"paid_until,omitempty"`
	BilledUntil                    *time.Time `json:"billed_until,omitempty"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`
}

type AccountShareEndMembershipToken struct {
	MembershipID int64     `json:"membership_id"`
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type accountShareEndMembershipTokenClaims struct {
	Action       string `json:"action"`
	ConsumerID   int64  `json:"consumer_user_id"`
	MembershipID int64  `json:"membership_id"`
	ExpiresAt    int64  `json:"expires_at"`
}

type AccountShareSeatBillingResult struct {
	Processed            int
	DebitUserIDs         []int64
	CreditUserIDs        []int64
	EndedConsumerUserIDs []int64
}

type AccountShareListingMaintenanceResult struct {
	Processed int
}

type AccountShareIdleMembershipFilter struct {
	ConsumerUserID int64
	APIKeyID       int64
	ListingID      int64
}

type AccountShareIdleMembershipCandidate struct {
	MembershipID int64
	Deadline     time.Time
}

type AccountShareModePolicy struct {
	ID                 int64   `json:"id,omitempty"`
	Platform           string  `json:"platform"`
	PlatformShareRatio float64 `json:"platform_share_ratio"`
	OwnerShareRatio    float64 `json:"owner_share_ratio"`
	Enabled            bool    `json:"enabled"`
	Version            int     `json:"version"`
}

type AccountShareModeBillingSnapshot struct {
	MembershipID       int64
	ListingID          int64
	AccountID          int64
	OwnerUserID        int64
	ConsumerUserID     int64
	APIKeyID           int64
	BaseCharge         float64
	HourlyCharge       float64
	TotalCharge        float64
	RateMultiplier     float64
	HourlyRate         float64
	OwnerShareRatio    float64
	PlatformShareRatio float64
	DurationMs         int
}

type AccountShareListingFilters struct {
	Tab                   string
	SeatLimit             int
	Search                string
	Status                string
	AvailableOnly         bool
	PerUserConcurrencyMin *int
	PerUserConcurrencyMax *int
	MinBalanceRequiredMin *float64
	MinBalanceRequiredMax *float64
	HourlyRateMin         *float64
	HourlyRateMax         *float64
	HourlyFeeWaiverMin    *float64
	HourlyFeeWaiverMax    *float64
	Models                []string
	AccountLevel          string
	ViewerIsAdmin         bool
}

type CreateAccountShareListingInput struct {
	Name                   string
	Notes                  *string
	ProxyID                int64
	Concurrency            int
	SeatLimit              int
	RateMultiplier         float64
	AllowedModels          []string
	PerUserConcurrency     int
	HourlyRate             float64
	HourlyFeeWaiverMinimum float64
	MinBalanceRequired     *float64
	CodexCLIOnly           bool
	Codex5hLimitPercent    float64
	Codex7dLimitPercent    float64
	TokenInfo              *OpenAITokenInfo
	AutoPauseOnExpired     *bool
	ExpiresAt              *time.Time
}

type UpdateAccountShareListingInput struct {
	Name                   *string
	ProxyID                *int64
	Status                 *string
	SeatLimit              *int
	RateMultiplier         *float64
	AllowedModels          *[]string
	PerUserConcurrency     *int
	HourlyRate             *float64
	HourlyFeeWaiverMinimum *float64
	MinBalanceRequired     *float64
	CodexCLIOnly           *bool
	Codex5hLimitPercent    *float64
	Codex7dLimitPercent    *float64
	Concurrency            *int
	EditSessionID          string
	ForceActiveEdit        bool
}

type BeginAccountShareListingEditInput struct {
	SessionID string
	Force     bool
	Expires   time.Time
}

type UpdateAccountShareModePolicyInput struct {
	Platform           string
	PlatformShareRatio *float64
	OwnerShareRatio    *float64
	Enabled            *bool
}

type CreateAccountShareProxyInput struct {
	Name     string
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
}

type AccountShareModeRepository interface {
	EnsureModeGroup(ctx context.Context, platform string) (*Group, error)
	GetModeGroup(ctx context.Context, platform string) (*Group, error)
	IsModeGroup(ctx context.Context, groupID int64) (bool, error)
	EnsureListingNameAvailable(ctx context.Context, ownerUserID int64, accountName string) error
	CreateOpenAIListing(ctx context.Context, account *Account, listing *AccountShareListing, modeGroupID int64) (*AccountShareListing, error)
	GetListingByID(ctx context.Context, listingID int64, viewerUserID int64) (*AccountShareListing, error)
	GetListingByAccountID(ctx context.Context, accountID int64) (*AccountShareListing, error)
	ListListings(ctx context.Context, viewerUserID int64, filters AccountShareListingFilters, params pagination.PaginationParams) ([]AccountShareListing, *pagination.PaginationResult, error)
	BeginListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input BeginAccountShareListingEditInput) (*AccountShareListing, error)
	ReleaseListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, sessionID string) (*AccountShareListing, error)
	UpdateListing(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input UpdateAccountShareListingInput) (*AccountShareListing, error)
	JoinListing(ctx context.Context, consumerUserID int64, apiKeyID int64, listingID int64, idleTimeoutMinutes int) (*AccountShareMembership, error)
	EndMembership(ctx context.Context, consumerUserID int64, membershipID int64) (*AccountShareMembership, error)
	UpdateMembershipIdleTimeout(ctx context.Context, consumerUserID int64, membershipID int64, idleTimeoutMinutes int) (*AccountShareMembership, error)
	TouchMembershipLastRequest(ctx context.Context, membershipID int64, at time.Time) error
	ListIdleMembershipCandidates(ctx context.Context, now time.Time, filter AccountShareIdleMembershipFilter, limit int) ([]AccountShareIdleMembershipCandidate, error)
	EndIdleMembership(ctx context.Context, membershipID int64, endedAt time.Time) (*AccountShareMembership, error)
	ProcessUnavailableMemberships(ctx context.Context, now time.Time, limit int) (*AccountShareSeatBillingResult, error)
	EndUnavailableAccountMemberships(ctx context.Context, accountID int64, endedAt time.Time, limit int) (*AccountShareSeatBillingResult, error)
	DisablePermanentlyUnavailableListings(ctx context.Context, now time.Time, limit int) (*AccountShareListingMaintenanceResult, error)
	ProcessSeatBilling(ctx context.Context, now time.Time, limit int) (*AccountShareSeatBillingResult, error)
	ProcessSeatBillingForJoin(ctx context.Context, now time.Time, consumerUserID, apiKeyID, listingID int64) (*AccountShareSeatBillingResult, error)
	ProcessSeatBillingForRequest(ctx context.Context, now time.Time, consumerUserID, apiKeyID int64) (*AccountShareSeatBillingResult, error)
	GetActiveMembershipForAPIKey(ctx context.Context, apiKeyID int64) (*AccountShareMembership, *AccountShareListing, error)
	GetActiveMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (*AccountShareMembership, *AccountShareListing, error)
	ResolvePolicy(ctx context.Context, platform string) (*AccountShareModePolicy, error)
	UpsertPolicy(ctx context.Context, input UpdateAccountShareModePolicyInput) (*AccountShareModePolicy, error)
}

type AccountShareModeProxyRepository interface {
	Create(ctx context.Context, proxy *Proxy) error
	GetVisibleByID(ctx context.Context, userID, id int64) (*Proxy, error)
	ListActiveVisibleWithAccountCount(ctx context.Context, userID int64) ([]ProxyWithAccountCount, error)
	FindVisibleActiveByEndpoint(ctx context.Context, userID int64, protocol, host string, port int, username, password string) (*Proxy, error)
	CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error)
}

type AccountShareModeService struct {
	repo                 AccountShareModeRepository
	accountRepo          AccountRepository
	apiKeyRepo           APIKeyRepository
	userRepo             UserRepository
	proxyRepo            AccountShareModeProxyRepository
	openaiOAuthService   *OpenAIOAuthService
	accountTestService   accountShareConnectivityTester
	rateLimitService     accountShareAccountStateRecovery
	concurrencyService   *ConcurrencyService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
	actionTokenSecret    []byte
	seatBillingStopCh    chan struct{}
	seatBillingStopOnce  sync.Once
	seatBillingStartOnce sync.Once
	seatBillingWG        sync.WaitGroup
	lastRequestTouchL1   sync.Map
}

func NewAccountShareModeService(
	repo AccountShareModeRepository,
	accountRepo AccountRepository,
	apiKeyRepo APIKeyRepository,
	userRepo UserRepository,
	proxyRepo AccountShareModeProxyRepository,
	openaiOAuthService *OpenAIOAuthService,
) *AccountShareModeService {
	return &AccountShareModeService{
		repo:               repo,
		accountRepo:        accountRepo,
		apiKeyRepo:         apiKeyRepo,
		userRepo:           userRepo,
		proxyRepo:          proxyRepo,
		openaiOAuthService: openaiOAuthService,
		seatBillingStopCh:  make(chan struct{}),
	}
}

func (s *AccountShareModeService) SetRuntimeDependencies(concurrencyService *ConcurrencyService, invalidator APIKeyAuthCacheInvalidator, accountTestService accountShareConnectivityTester, rateLimitService accountShareAccountStateRecovery) {
	if s == nil {
		return
	}
	s.concurrencyService = concurrencyService
	s.authCacheInvalidator = invalidator
	s.accountTestService = accountTestService
	s.rateLimitService = rateLimitService
}

func (s *AccountShareModeService) SetBillingCacheService(billingCacheService *BillingCacheService) {
	if s == nil {
		return
	}
	s.billingCacheService = billingCacheService
}

func (s *AccountShareModeService) SetActionTokenSecret(secret string) {
	if s == nil {
		return
	}
	s.actionTokenSecret = []byte(strings.TrimSpace(secret))
}

func (s *AccountShareModeService) StartSeatBillingWorker() {
	if s == nil || s.repo == nil {
		return
	}
	s.seatBillingStartOnce.Do(func() {
		s.seatBillingWG.Add(1)
		go s.runSeatBillingWorker()
	})
}

func (s *AccountShareModeService) StopSeatBillingWorker() {
	if s == nil {
		return
	}
	s.seatBillingStopOnce.Do(func() {
		close(s.seatBillingStopCh)
	})
	s.seatBillingWG.Wait()
}

func (s *AccountShareModeService) runSeatBillingWorker() {
	defer s.seatBillingWG.Done()
	ticker := time.NewTicker(AccountShareModeSeatBillingInterval)
	defer ticker.Stop()

	s.processSeatBillingOnce()
	for {
		select {
		case <-ticker.C:
			s.processSeatBillingOnce()
		case <-s.seatBillingStopCh:
			return
		}
	}
}

func (s *AccountShareModeService) processSeatBillingOnce() {
	if s == nil || s.repo == nil {
		return
	}
	s.processUnavailableMembershipsOnce()
	s.processPermanentlyUnavailableListingsOnce()
	s.processIdleMembershipsOnce()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, err := s.repo.ProcessSeatBilling(ctx, time.Now().UTC(), AccountShareModeSeatBillingBatchSize)
		cancel()
		if err != nil {
			log.Printf("account_share_mode: process prepaid seat billing failed: %v", err)
			return
		}
		s.invalidateSeatBillingCaches(result)
		if result == nil || result.Processed < AccountShareModeSeatBillingBatchSize {
			return
		}
	}
}

func (s *AccountShareModeService) processUnavailableMembershipsOnce() {
	if s == nil || s.repo == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, err := s.repo.ProcessUnavailableMemberships(ctx, time.Now().UTC(), AccountShareModeSeatBillingBatchSize)
		cancel()
		if err != nil {
			log.Printf("account_share_mode: process unavailable memberships failed: %v", err)
			return
		}
		s.invalidateSeatBillingCaches(result)
		if result == nil || result.Processed < AccountShareModeSeatBillingBatchSize {
			return
		}
	}
}

func (s *AccountShareModeService) processPermanentlyUnavailableListingsOnce() {
	if s == nil || s.repo == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, err := s.repo.DisablePermanentlyUnavailableListings(ctx, time.Now().UTC(), AccountShareModeSeatBillingBatchSize)
		cancel()
		if err != nil {
			log.Printf("account_share_mode: disable permanently unavailable listings failed: %v", err)
			return
		}
		if result == nil || result.Processed < AccountShareModeSeatBillingBatchSize {
			return
		}
	}
}

func (s *AccountShareModeService) processIdleMembershipsOnce() {
	if s == nil || s.repo == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, err := s.processIdleMemberships(ctx, time.Now().UTC(), AccountShareIdleMembershipFilter{}, AccountShareModeSeatBillingBatchSize)
		cancel()
		if err != nil {
			log.Printf("account_share_mode: process idle memberships failed: %v", err)
			return
		}
		if result == nil || result.Processed < AccountShareModeSeatBillingBatchSize {
			return
		}
	}
}

func (s *AccountShareModeService) processIdleMemberships(ctx context.Context, now time.Time, filter AccountShareIdleMembershipFilter, limit int) (*AccountShareSeatBillingResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	if limit <= 0 {
		limit = AccountShareModeSeatBillingBatchSize
	}
	candidates, err := s.repo.ListIdleMembershipCandidates(ctx, now, filter, limit)
	if err != nil {
		return nil, err
	}
	result := &AccountShareSeatBillingResult{Processed: len(candidates)}
	for _, candidate := range candidates {
		if candidate.MembershipID <= 0 {
			continue
		}
		active, err := s.membershipHasActiveConcurrency(ctx, candidate.MembershipID)
		if err != nil {
			return result, err
		}
		if active {
			continue
		}
		membership, err := s.repo.EndIdleMembership(ctx, candidate.MembershipID, candidate.Deadline)
		if err != nil {
			if errors.Is(err, ErrAccountShareListingNotFound) {
				continue
			}
			return result, err
		}
		if membership == nil {
			continue
		}
		result.DebitUserIDs = append(result.DebitUserIDs, membership.ConsumerUserID)
		result.CreditUserIDs = append(result.CreditUserIDs, membership.OwnerUserID)
		result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, membership.ConsumerUserID)
	}
	s.invalidateSeatBillingCaches(result)
	return result, nil
}

func (s *AccountShareModeService) invalidateSeatBillingCaches(result *AccountShareSeatBillingResult) {
	if s == nil || result == nil {
		return
	}
	if s.billingCacheService != nil {
		for _, userID := range uniquePositiveInt64s(append(result.DebitUserIDs, result.CreditUserIDs...)) {
			if err := s.billingCacheService.InvalidateUserBalance(context.Background(), userID); err != nil {
				log.Printf("account_share_mode: invalidate user balance cache failed: user=%d err=%v", userID, err)
			}
		}
	}
	if s.authCacheInvalidator != nil {
		for _, userID := range uniquePositiveInt64s(result.EndedConsumerUserIDs) {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(context.Background(), userID)
		}
	}
}

func (s *AccountShareModeService) EnsureModeGroup(ctx context.Context, platform string) (*Group, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAccountShareModeGroupUnavailable
	}
	return s.repo.EnsureModeGroup(ctx, platform)
}

func (s *AccountShareModeService) GetOpenAIModeGroup(ctx context.Context) (*Group, error) {
	return s.EnsureModeGroup(ctx, PlatformOpenAI)
}

func (s *AccountShareModeService) IsModeGroup(ctx context.Context, groupID int64) bool {
	if s == nil || s.repo == nil || groupID <= 0 {
		return false
	}
	ok, err := s.repo.IsModeGroup(ctx, groupID)
	return err == nil && ok
}

func (s *AccountShareModeService) GenerateOpenAIAuthURL(ctx context.Context, ownerUserID int64, proxyID *int64, redirectURI string) (*OpenAIAuthURLResult, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if proxyID == nil || *proxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if s == nil || s.openaiOAuthService == nil {
		return nil, ErrServiceUnavailable
	}
	if err := s.ensureProxyAvailableForNewAccount(ctx, ownerUserID, *proxyID); err != nil {
		return nil, err
	}
	return s.openaiOAuthService.GenerateAuthURL(ctx, proxyID, redirectURI, PlatformOpenAI)
}

func (s *AccountShareModeService) ListAvailableProxies(ctx context.Context, userID int64) ([]ProxyWithAccountCount, error) {
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.proxyRepo == nil {
		return []ProxyWithAccountCount{}, nil
	}
	return s.proxyRepo.ListActiveVisibleWithAccountCount(ctx, userID)
}

func (s *AccountShareModeService) CreateUserProxy(ctx context.Context, ownerUserID int64, input CreateAccountShareProxyInput) (*Proxy, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.proxyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	normalized, err := normalizeAccountShareProxyInput(ownerUserID, input)
	if err != nil {
		return nil, err
	}
	existing, err := s.proxyRepo.FindVisibleActiveByEndpoint(ctx, ownerUserID, normalized.Protocol, normalized.Host, normalized.Port, normalized.Username, normalized.Password)
	if err == nil && existing != nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, ErrProxyNotFound) {
		return nil, err
	}
	if err := s.proxyRepo.Create(ctx, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (s *AccountShareModeService) ExchangeOpenAICodeAndCreateListing(ctx context.Context, ownerUserID int64, exchange *OpenAIExchangeCodeInput, input CreateAccountShareListingInput) (*AccountShareListing, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if exchange == nil || exchange.ProxyID == nil || *exchange.ProxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if input.ProxyID <= 0 {
		input.ProxyID = *exchange.ProxyID
	}
	if input.ProxyID != *exchange.ProxyID {
		return nil, ErrAccountShareModeProxyRequired
	}
	if err := s.ensureProxyAvailableForNewAccount(ctx, ownerUserID, input.ProxyID); err != nil {
		return nil, err
	}
	if err := validateAccountShareAccountName(input.Name); err != nil {
		return nil, err
	}
	input.AllowedModels = normalizeAllowedModelsOrDefault(input.AllowedModels)
	if err := validateAccountShareListingConfig(input.SeatLimit, input.RateMultiplier, input.AllowedModels, input.PerUserConcurrency, input.Concurrency, input.HourlyRate, input.HourlyFeeWaiverMinimum, minBalanceValue(input.MinBalanceRequired), input.Codex5hLimitPercent, input.Codex7dLimitPercent); err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	accountName := compactAccountShareAccountName(input.Name)
	if accountName != "" {
		if err := s.repo.EnsureListingNameAvailable(ctx, ownerUserID, accountName); err != nil {
			return nil, err
		}
	}
	if s == nil || s.openaiOAuthService == nil {
		return nil, ErrServiceUnavailable
	}
	tokenInfo, err := s.openaiOAuthService.ExchangeCode(ctx, exchange)
	if err != nil {
		return nil, err
	}
	input.TokenInfo = tokenInfo
	return s.CreateOpenAIListingFromToken(ctx, ownerUserID, input)
}

func (s *AccountShareModeService) CreateOpenAIListingFromToken(ctx context.Context, ownerUserID int64, input CreateAccountShareListingInput) (*AccountShareListing, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.ProxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if input.TokenInfo == nil {
		return nil, ErrOwnedAccountCredentialsInvalid
	}
	if err := s.ensureProxyAvailableForNewAccount(ctx, ownerUserID, input.ProxyID); err != nil {
		return nil, err
	}
	if err := validateAccountShareAccountName(input.Name); err != nil {
		return nil, err
	}
	input.AllowedModels = normalizeAllowedModelsOrDefault(input.AllowedModels)
	if err := validateAccountShareListingConfig(input.SeatLimit, input.RateMultiplier, input.AllowedModels, input.PerUserConcurrency, input.Concurrency, input.HourlyRate, input.HourlyFeeWaiverMinimum, minBalanceValue(input.MinBalanceRequired), input.Codex5hLimitPercent, input.Codex7dLimitPercent); err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil || s.openaiOAuthService == nil {
		return nil, ErrServiceUnavailable
	}
	modeGroup, err := s.repo.EnsureModeGroup(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}
	if modeGroup == nil || modeGroup.ID <= 0 {
		return nil, ErrAccountShareModeGroupUnavailable
	}

	credentials := s.openaiOAuthService.BuildAccountCredentials(input.TokenInfo)
	credentials["model_mapping"] = AccountShareModeAllowedModelsMapping(input.AllowedModels)
	extra := BuildOpenAIAccountCredentialImportExtra(input.TokenInfo)
	extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	extra["openai_oauth_responses_websockets_v2_enabled"] = true
	extra["openai_passthrough"] = false
	extra["openai_oauth_passthrough"] = false
	extra["openai_compact_mode"] = OpenAICompactModeForceOn
	extra["codex_cli_only"] = input.CodexCLIOnly
	extra["account_share_mode"] = true
	if input.Codex5hLimitPercent <= 0 {
		input.Codex5hLimitPercent = AccountShareModeDefaultCodexLimitPercent
	}
	if input.Codex7dLimitPercent <= 0 {
		input.Codex7dLimitPercent = AccountShareModeDefaultCodexLimitPercent
	}
	extra["codex_5h_limit_percent"] = input.Codex5hLimitPercent
	extra["codex_7d_limit_percent"] = input.Codex7dLimitPercent
	normalizedExtra, err := NormalizeCodexQuotaLimitExtra(PlatformOpenAI, AccountTypeOAuth, extra)
	if err != nil {
		return nil, err
	}
	extra = normalizedExtra

	accountName := strings.TrimSpace(input.Name)
	if accountName == "" {
		accountName = DeriveAccountCredentialImportName(PlatformOpenAI, credentials, extra, 1)
	}
	accountName = compactAccountShareAccountName(accountName)
	concurrency := input.Concurrency
	if concurrency <= 0 {
		concurrency = AccountShareModeDefaultAccountConcurrency
	}
	account := &Account{
		Name:                  accountName,
		Notes:                 normalizeAccountNotes(input.Notes),
		Platform:              PlatformOpenAI,
		AccountLevel:          NormalizeOpenAIAccountLevel(PlatformOpenAI, AccountLevelUnknown, credentials, extra),
		Type:                  AccountTypeOAuth,
		Credentials:           credentials,
		Extra:                 extra,
		OwnerUserID:           &ownerUserID,
		ShareMode:             AccountShareModePrivate,
		ShareStatus:           AccountShareStatusApproved,
		ProxyID:               &input.ProxyID,
		Concurrency:           concurrency,
		LoadFactor:            nil,
		LoadFactorPaidCeiling: OwnedPersonalDefaultLoadFactor,
		Priority:              ownedPersonalDefaultPriority,
		Status:                StatusActive,
		ExpiresAt:             input.ExpiresAt,
		AutoPauseOnExpired:    true,
		Schedulable:           true,
		GroupIDs:              []int64{modeGroup.ID},
	}
	if input.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *input.AutoPauseOnExpired
	}
	if err := validateOwnedAccountSource(account.Type, account.Credentials, account.Extra); err != nil {
		return nil, err
	}
	listing := &AccountShareListing{
		OwnerUserID:            ownerUserID,
		Status:                 AccountShareListingStatusActive,
		SeatLimit:              input.SeatLimit,
		RateMultiplier:         input.RateMultiplier,
		AllowedModels:          input.AllowedModels,
		PerUserConcurrency:     normalizePositiveInt(input.PerUserConcurrency, AccountShareModeDefaultPerUserConcurrency),
		AccountConcurrency:     account.Concurrency,
		HourlyRate:             input.HourlyRate,
		HourlyFeeWaiverMinimum: input.HourlyFeeWaiverMinimum,
		MinBalanceRequired:     minBalanceValue(input.MinBalanceRequired),
		CodexCLIOnly:           input.CodexCLIOnly,
		Codex5hLimitPercent:    normalizeCodexLimitPercent(input.Codex5hLimitPercent),
		Codex7dLimitPercent:    normalizeCodexLimitPercent(input.Codex7dLimitPercent),
	}
	created, err := s.repo.CreateOpenAIListing(ctx, account, listing, modeGroup.ID)
	if err != nil {
		return nil, err
	}
	s.enrichListingRuntime(ctx, created)
	s.schedulePostCreateConnectivityTest(created)
	return created, nil
}

func (s *AccountShareModeService) ListListings(ctx context.Context, viewerUserID int64, viewerIsAdmin bool, filters AccountShareListingFilters, params pagination.PaginationParams) ([]AccountShareListing, *pagination.PaginationResult, error) {
	if viewerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, nil, ErrServiceUnavailable
	}
	normalized := normalizeListingFilters(filters)
	normalized.ViewerIsAdmin = viewerIsAdmin
	listings, result, err := s.repo.ListListings(ctx, viewerUserID, normalized, params)
	if err != nil {
		return nil, nil, err
	}
	s.enrichListingsRuntime(ctx, listings)
	return listings, result, nil
}

func (s *AccountShareModeService) GetListing(ctx context.Context, viewerUserID, listingID int64) (*AccountShareListing, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	listing, err := s.repo.GetListingByID(ctx, listingID, viewerUserID)
	if err != nil {
		return nil, err
	}
	s.enrichListingRuntime(ctx, listing)
	return listing, nil
}

func (s *AccountShareModeService) BeginListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, sessionID string, force bool) (*AccountShareListing, error) {
	if actorUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	if !actorIsAdmin && force {
		return nil, ErrInsufficientPerms
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	listing, err := s.repo.BeginListingEdit(ctx, actorUserID, actorIsAdmin, listingID, BeginAccountShareListingEditInput{
		SessionID: sessionID,
		Force:     force,
		Expires:   time.Now().UTC().Add(AccountShareModeEditSessionTTL),
	})
	if err != nil {
		return nil, err
	}
	s.enrichListingRuntime(ctx, listing)
	if err := s.attachListingEditProxy(ctx, listing); err != nil {
		if _, releaseErr := s.repo.ReleaseListingEdit(ctx, actorUserID, actorIsAdmin, listingID, sessionID); releaseErr != nil {
			log.Printf("[AccountShareMode] release edit session after proxy attach failure failed: listing_id=%d user_id=%d err=%v", listingID, actorUserID, releaseErr)
		}
		return nil, err
	}
	return listing, nil
}

func (s *AccountShareModeService) ReleaseListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, sessionID string) (*AccountShareListing, error) {
	if actorUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrAccountShareEditSessionRequired
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	listing, err := s.repo.ReleaseListingEdit(ctx, actorUserID, actorIsAdmin, listingID, sessionID)
	if err != nil {
		return nil, err
	}
	s.enrichListingRuntime(ctx, listing)
	return listing, nil
}

func (s *AccountShareModeService) UpdateListing(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input UpdateAccountShareListingInput) (*AccountShareListing, error) {
	if actorUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.Name != nil {
		name := compactAccountShareAccountName(*input.Name)
		if name == "" {
			return nil, ErrAccountShareModeInvalidName
		}
		if err := validateAccountShareAccountName(name); err != nil {
			return nil, err
		}
		input.Name = &name
	}
	if input.AllowedModels != nil {
		normalized := normalizeAllowedModels(*input.AllowedModels)
		if len(normalized) == 0 {
			return nil, ErrAccountShareModeAllowedModelsRequired
		}
		input.AllowedModels = &normalized
	}
	ownerRelist := !actorIsAdmin && isAccountShareModeOwnerRelistUpdate(input)
	if !actorIsAdmin && !ownerRelist && !isAccountShareModeModelOnlyUpdate(input) && !isAccountShareModeOwnerConfigUpdate(input) {
		return nil, ErrInsufficientPerms
	}
	if !actorIsAdmin && input.ForceActiveEdit {
		return nil, ErrInsufficientPerms
	}
	if requiresAccountShareModeEditSession(input) && strings.TrimSpace(input.EditSessionID) == "" {
		return nil, ErrAccountShareEditSessionRequired
	}
	input.EditSessionID = strings.TrimSpace(input.EditSessionID)
	if input.ProxyID != nil && *input.ProxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if input.SeatLimit != nil && (*input.SeatLimit < AccountShareModeMinSeats || *input.SeatLimit > AccountShareModeMaxSeats) {
		return nil, ErrAccountShareModeInvalidSeats
	}
	if input.RateMultiplier != nil && invalidNonNegativeFloat(*input.RateMultiplier) {
		return nil, ErrAccountShareModeInvalidRateMultiplier
	}
	if input.PerUserConcurrency != nil && *input.PerUserConcurrency <= 0 {
		return nil, ErrAccountShareModeInvalidConcurrency
	}
	if input.Concurrency != nil && (*input.Concurrency <= 0 || *input.Concurrency > AccountShareModeMaxAccountConcurrency) {
		return nil, ErrAccountShareModeInvalidConcurrency
	}
	if input.HourlyRate != nil && invalidNonNegativeFloat(*input.HourlyRate) {
		return nil, ErrAccountShareModeInvalidHourlyRate
	}
	if input.HourlyFeeWaiverMinimum != nil && invalidNonNegativeFloat(*input.HourlyFeeWaiverMinimum) {
		return nil, ErrAccountShareModeInvalidWaiverMinimum
	}
	if input.MinBalanceRequired != nil && invalidNonNegativeFloat(*input.MinBalanceRequired) {
		return nil, ErrAccountShareModeInvalidMinBalance
	}
	if input.Codex5hLimitPercent != nil && !isValidCodexLimitPercent(*input.Codex5hLimitPercent) {
		return nil, ErrCodexQuotaLimitPercentInvalid
	}
	if input.Codex7dLimitPercent != nil && !isValidCodexLimitPercent(*input.Codex7dLimitPercent) {
		return nil, ErrCodexQuotaLimitPercentInvalid
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	if ownerRelist {
		if err := s.validateOwnerRelist(ctx, actorUserID, listingID); err != nil {
			return nil, err
		}
	}
	listing, err := s.repo.UpdateListing(ctx, actorUserID, actorIsAdmin, listingID, input)
	if err != nil {
		return nil, err
	}
	s.enrichListingRuntime(ctx, listing)
	return listing, nil
}

func isAccountShareModeModelOnlyUpdate(input UpdateAccountShareListingInput) bool {
	return input.AllowedModels != nil &&
		input.Name == nil &&
		input.ProxyID == nil &&
		input.Status == nil &&
		input.SeatLimit == nil &&
		input.RateMultiplier == nil &&
		input.PerUserConcurrency == nil &&
		input.HourlyRate == nil &&
		input.HourlyFeeWaiverMinimum == nil &&
		input.MinBalanceRequired == nil &&
		input.CodexCLIOnly == nil &&
		input.Codex5hLimitPercent == nil &&
		input.Codex7dLimitPercent == nil &&
		input.Concurrency == nil &&
		!input.ForceActiveEdit
}

func isAccountShareModeOwnerRelistUpdate(input UpdateAccountShareListingInput) bool {
	if input.Status == nil || input.ForceActiveEdit {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(*input.Status))
	return status == AccountShareListingStatusActive && !hasAccountShareModeConfigUpdate(input)
}

func isAccountShareModeOwnerConfigUpdate(input UpdateAccountShareListingInput) bool {
	return input.Status == nil && hasAccountShareModeConfigUpdate(input)
}

func requiresAccountShareModeEditSession(input UpdateAccountShareListingInput) bool {
	return hasAccountShareModeConfigUpdate(input) && !isAccountShareModeModelOnlyUpdate(input)
}

func hasAccountShareModeConfigUpdate(input UpdateAccountShareListingInput) bool {
	return input.Name != nil ||
		input.ProxyID != nil ||
		input.SeatLimit != nil ||
		input.RateMultiplier != nil ||
		input.AllowedModels != nil ||
		input.PerUserConcurrency != nil ||
		input.HourlyRate != nil ||
		input.HourlyFeeWaiverMinimum != nil ||
		input.MinBalanceRequired != nil ||
		input.CodexCLIOnly != nil ||
		input.Codex5hLimitPercent != nil ||
		input.Codex7dLimitPercent != nil ||
		input.Concurrency != nil
}

func (s *AccountShareModeService) validateOwnerRelist(ctx context.Context, actorUserID, listingID int64) error {
	if s == nil || s.repo == nil || s.accountTestService == nil || s.rateLimitService == nil {
		return ErrServiceUnavailable
	}
	listing, err := s.repo.GetListingByID(ctx, listingID, actorUserID)
	if err != nil {
		return err
	}
	if listing == nil || listing.OwnerUserID != actorUserID {
		return ErrAccountShareListingNotFound
	}
	if listing.Status == AccountShareListingStatusActive {
		return nil
	}

	testCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := s.accountTestService.RunTestBackground(testCtx, listing.AccountID, firstAllowedModel(listing.AllowedModels))
	if err != nil {
		return accountShareRelistTestError(err.Error())
	}
	if result == nil {
		return accountShareRelistTestError("account test did not return a result")
	}
	if strings.TrimSpace(result.Status) != "success" {
		reason := strings.TrimSpace(result.ErrorMessage)
		if reason == "" {
			reason = "account test failed"
		}
		return accountShareRelistTestError(reason)
	}
	if _, err := s.rateLimitService.RecoverAccountAfterSuccessfulTest(ctx, listing.AccountID); err != nil {
		return err
	}
	refreshed, err := s.repo.GetListingByID(ctx, listingID, actorUserID)
	if err != nil {
		return err
	}
	if refreshed == nil || refreshed.OwnerUserID != actorUserID {
		return ErrAccountShareListingNotFound
	}
	if accountShareListingAccountUnavailableAt(refreshed, time.Now().UTC()) {
		return ErrAccountShareRelistAccountUnavailable
	}
	return nil
}

func accountShareRelistTestError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "account test failed"
	}
	return infraerrors.Newf(http.StatusBadRequest, "ACCOUNT_SHARE_RELIST_TEST_FAILED", "重新上架前自动测试失败：%s", reason)
}

func (s *AccountShareModeService) enrichListingRuntime(ctx context.Context, listing *AccountShareListing) {
	if listing == nil {
		return
	}
	listings := []AccountShareListing{*listing}
	s.enrichListingsRuntime(ctx, listings)
	*listing = listings[0]
}

func (s *AccountShareModeService) enrichListingsRuntime(ctx context.Context, listings []AccountShareListing) {
	if s == nil || s.concurrencyService == nil || len(listings) == 0 {
		return
	}
	seen := make(map[int64]struct{}, len(listings))
	accounts := make([]AccountWithConcurrency, 0, len(listings))
	for i := range listings {
		accountID := listings[i].AccountID
		if accountID <= 0 {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		accounts = append(accounts, AccountWithConcurrency{
			ID:             accountID,
			MaxConcurrency: listings[i].AccountConcurrency,
		})
	}
	if len(accounts) == 0 {
		return
	}
	loadByAccountID, err := s.concurrencyService.GetAccountsLoadBatch(ctx, accounts)
	if err != nil {
		log.Printf("[AccountShareMode] get account runtime load failed: %v", err)
		return
	}
	for i := range listings {
		if load := loadByAccountID[listings[i].AccountID]; load != nil {
			listings[i].CurrentConcurrency = load.CurrentConcurrency
		}
	}
}

func (s *AccountShareModeService) JoinListing(ctx context.Context, consumerUserID, listingID, apiKeyID int64, idleTimeoutMinutes int) (*AccountShareMembership, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if apiKeyID <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	if err := validateAccountShareIdleTimeoutMinutes(idleTimeoutMinutes); err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil || s.apiKeyRepo == nil || s.userRepo == nil {
		return nil, ErrServiceUnavailable
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey.UserID != consumerUserID {
		return nil, ErrInsufficientPerms
	}
	if apiKey.GroupID == nil || *apiKey.GroupID <= 0 || !s.IsModeGroup(ctx, *apiKey.GroupID) {
		return nil, ErrAccountShareAPIKeyMustUseModeGroup
	}
	user, err := s.userRepo.GetByID(ctx, consumerUserID)
	if err != nil {
		return nil, err
	}
	listing, err := s.repo.GetListingByID(ctx, listingID, consumerUserID)
	if err != nil {
		return nil, err
	}
	if listing.OwnerUserID == consumerUserID {
		return nil, ErrAccountShareOwnerCannotJoin
	}
	if listing.Status != AccountShareListingStatusActive {
		return nil, ErrAccountShareListingNotActive
	}
	now := time.Now().UTC()
	if accountShareListingAccountUnavailableAt(listing, now) {
		log.Printf("account_share_mode: join rejected stage=service_precheck_unavailable user_id=%d listing_id=%d api_key_id=%d account_id=%d account_status=%q account_schedulable=%t overload_until=%s rate_limit_reset_at=%s temp_unschedulable_until=%s codex_reason=%s codex_reset_at=%s",
			consumerUserID,
			listingID,
			apiKeyID,
			listing.AccountID,
			listing.AccountStatus,
			listing.AccountSchedulable,
			accountShareLogTimePtr(listing.OverloadUntil),
			accountShareLogTimePtr(listing.RateLimitResetAt),
			accountShareLogTimePtr(listing.TempUnschedulableUntil),
			accountShareLogStringPtr(listing.CodexQuotaProtectionReason),
			accountShareLogTimePtr(listing.CodexQuotaProtectionResetAt),
		)
		result, err := s.repo.EndUnavailableAccountMemberships(ctx, listing.AccountID, now, AccountShareModeSeatBillingBatchSize)
		if err != nil {
			return nil, err
		}
		s.invalidateSeatBillingCaches(result)
		return nil, ErrAccountShareAccountUnavailable
	}
	if user.Balance < listing.MinBalanceRequired {
		return nil, ErrAccountShareBalanceBelowMinimum
	}
	result, err := s.repo.ProcessSeatBillingForJoin(ctx, now, consumerUserID, apiKeyID, listingID)
	if err != nil {
		log.Printf("account_share_mode: join failed stage=seat_billing user_id=%d listing_id=%d api_key_id=%d account_id=%d err=%v",
			consumerUserID,
			listingID,
			apiKeyID,
			listing.AccountID,
			err,
		)
		return nil, err
	}
	s.invalidateSeatBillingCaches(result)
	if _, err := s.processIdleMemberships(ctx, now, AccountShareIdleMembershipFilter{
		ConsumerUserID: consumerUserID,
		APIKeyID:       apiKeyID,
		ListingID:      listingID,
	}, AccountShareModeSeatBillingBatchSize); err != nil {
		return nil, err
	}
	membership, err := s.repo.JoinListing(ctx, consumerUserID, apiKeyID, listingID, idleTimeoutMinutes)
	if err != nil {
		log.Printf("account_share_mode: join failed stage=repo_join user_id=%d listing_id=%d api_key_id=%d account_id=%d err=%v",
			consumerUserID,
			listingID,
			apiKeyID,
			listing.AccountID,
			err,
		)
		return nil, err
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}
	s.invalidateSeatBillingCaches(&AccountShareSeatBillingResult{DebitUserIDs: []int64{consumerUserID}})
	return membership, nil
}

func (s *AccountShareModeService) UpdateMembershipIdleTimeout(ctx context.Context, consumerUserID, membershipID int64, idleTimeoutMinutes int) (*AccountShareMembership, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if membershipID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if err := validateAccountShareIdleTimeoutMinutes(idleTimeoutMinutes); err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	membership, err := s.repo.UpdateMembershipIdleTimeout(ctx, consumerUserID, membershipID, idleTimeoutMinutes)
	if err != nil {
		return nil, err
	}
	return membership, nil
}

func (s *AccountShareModeService) CreateEndMembershipToken(ctx context.Context, consumerUserID, membershipID int64) (*AccountShareEndMembershipToken, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if membershipID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if s == nil {
		return nil, ErrServiceUnavailable
	}
	expiresAt := time.Now().UTC().Add(AccountShareModeEndMembershipTokenTTL)
	claims := accountShareEndMembershipTokenClaims{
		Action:       accountShareModeEndMembershipTokenAction,
		ConsumerID:   consumerUserID,
		MembershipID: membershipID,
		ExpiresAt:    expiresAt.Unix(),
	}
	token, err := s.signEndMembershipToken(claims)
	if err != nil {
		return nil, err
	}
	return &AccountShareEndMembershipToken{
		MembershipID: membershipID,
		Token:        token,
		ExpiresAt:    expiresAt,
	}, nil
}

func (s *AccountShareModeService) EndMembership(ctx context.Context, consumerUserID, membershipID int64, confirmationToken string) (*AccountShareMembership, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	if err := s.validateEndMembershipToken(confirmationToken, consumerUserID, membershipID, time.Now().UTC()); err != nil {
		return nil, err
	}
	membership, err := s.repo.EndMembership(ctx, consumerUserID, membershipID)
	if err != nil {
		return nil, err
	}
	if s.authCacheInvalidator != nil && membership.APIKeyID > 0 && s.apiKeyRepo != nil {
		if key, keyErr := s.apiKeyRepo.GetByID(ctx, membership.APIKeyID); keyErr == nil && key != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key.Key)
		}
	}
	s.invalidateSeatBillingCaches(&AccountShareSeatBillingResult{
		DebitUserIDs:  []int64{membership.ConsumerUserID},
		CreditUserIDs: []int64{membership.OwnerUserID},
	})
	return membership, nil
}

func (s *AccountShareModeService) signEndMembershipToken(claims accountShareEndMembershipTokenClaims) (string, error) {
	if len(s.actionTokenSecret) < 32 {
		return "", ErrServiceUnavailable
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal account share end token: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.actionTokenSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	signature := mac.Sum(nil)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *AccountShareModeService) validateEndMembershipToken(token string, consumerUserID, membershipID int64, now time.Time) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrAccountShareEndTokenRequired
	}
	if len(s.actionTokenSecret) < 32 {
		return ErrServiceUnavailable
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ErrAccountShareEndTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrAccountShareEndTokenInvalid
	}
	mac := hmac.New(sha256.New, s.actionTokenSecret)
	_, _ = mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	if !hmac.Equal(signature, expected) {
		return ErrAccountShareEndTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrAccountShareEndTokenInvalid
	}
	var claims accountShareEndMembershipTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ErrAccountShareEndTokenInvalid
	}
	if claims.Action != accountShareModeEndMembershipTokenAction ||
		claims.ConsumerID != consumerUserID ||
		claims.MembershipID != membershipID ||
		claims.ExpiresAt <= now.Unix() {
		return ErrAccountShareEndTokenInvalid
	}
	return nil
}

func validateAccountShareIdleTimeoutMinutes(value int) error {
	if value < 0 || value > AccountShareModeMaxIdleTimeoutMinutes {
		return ErrAccountShareModeInvalidIdleTimeout
	}
	return nil
}

func (s *AccountShareModeService) ResolveActiveBindingForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (*AccountShareMembership, *AccountShareListing, error) {
	if s == nil || s.repo == nil || groupID <= 0 {
		return nil, nil, nil
	}
	if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
		if membership, listing, err, cached := requestCtx.state.get(userID, apiKeyID, groupID); cached {
			return membership, listing, err
		}
	}
	isMode, err := s.repo.IsModeGroup(ctx, groupID)
	if err != nil || !isMode {
		if err == nil {
			if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
				requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, nil)
			}
		}
		return nil, nil, err
	}
	if userID <= 0 || apiKeyID <= 0 {
		if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
			requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, ErrAccountShareModeGroupUnbound)
		}
		return nil, nil, ErrAccountShareModeGroupUnbound
	}
	membership, listing, err := s.repo.GetActiveMembershipForRequest(ctx, userID, apiKeyID, groupID)
	if err != nil {
		if errors.Is(err, ErrAccountShareListingNotFound) {
			catchupResult, catchupErr := s.repo.ProcessSeatBillingForRequest(ctx, time.Now().UTC(), userID, apiKeyID)
			if catchupErr != nil {
				return nil, nil, catchupErr
			}
			s.invalidateSeatBillingCaches(catchupResult)
			membership, listing, err = s.repo.GetActiveMembershipForRequest(ctx, userID, apiKeyID, groupID)
			if err == nil && membership != nil && listing != nil {
				if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
					requestCtx.state.set(userID, apiKeyID, groupID, membership, listing, nil)
				}
				return membership, listing, nil
			}
			if err == nil || errors.Is(err, ErrAccountShareListingNotFound) {
				if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
					requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, ErrAccountShareModeGroupUnbound)
				}
				return nil, nil, ErrAccountShareModeGroupUnbound
			}
		}
		return nil, nil, err
	}
	if membership == nil || listing == nil {
		if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
			requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, ErrAccountShareModeGroupUnbound)
		}
		return nil, nil, ErrAccountShareModeGroupUnbound
	}
	now := time.Now().UTC()
	if accountShareListingAccountUnavailableAt(listing, now) {
		result, err := s.repo.EndUnavailableAccountMemberships(ctx, listing.AccountID, now, AccountShareModeSeatBillingBatchSize)
		if err != nil {
			return nil, nil, err
		}
		s.invalidateSeatBillingCaches(result)
		if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
			requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, ErrAccountShareModeGroupUnbound)
		}
		return nil, nil, ErrAccountShareModeGroupUnbound
	}
	ended, err := s.endIdleMembershipForRequest(ctx, membership, now)
	if err != nil {
		return nil, nil, err
	}
	if ended {
		if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
			requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, ErrAccountShareModeGroupUnbound)
		}
		return nil, nil, ErrAccountShareModeGroupUnbound
	}
	if err := s.touchMembershipLastRequest(ctx, membership.ID, now); err != nil {
		return nil, nil, err
	}
	if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
		requestCtx.state.set(userID, apiKeyID, groupID, membership, listing, nil)
	}
	return membership, listing, nil
}

func (s *AccountShareModeService) endIdleMembershipForRequest(ctx context.Context, membership *AccountShareMembership, now time.Time) (bool, error) {
	if s == nil || s.repo == nil || membership == nil || membership.ID <= 0 || membership.IdleTimeoutMinutes <= 0 {
		return false, nil
	}
	deadline := membershipIdleDeadline(membership)
	if deadline == nil || deadline.After(now) {
		return false, nil
	}
	active, err := s.membershipHasActiveConcurrency(ctx, membership.ID)
	if err != nil {
		return false, err
	}
	if active {
		return false, nil
	}
	ended, err := s.repo.EndIdleMembership(ctx, membership.ID, *deadline)
	if err != nil {
		if errors.Is(err, ErrAccountShareListingNotFound) {
			return true, nil
		}
		return false, err
	}
	if ended != nil {
		s.invalidateSeatBillingCaches(&AccountShareSeatBillingResult{
			DebitUserIDs:         []int64{ended.ConsumerUserID},
			CreditUserIDs:        []int64{ended.OwnerUserID},
			EndedConsumerUserIDs: []int64{ended.ConsumerUserID},
		})
	}
	return true, nil
}

func (s *AccountShareModeService) touchMembershipLastRequest(ctx context.Context, membershipID int64, at time.Time) error {
	if s == nil || s.repo == nil || membershipID <= 0 {
		return nil
	}
	now := at.UTC()
	if v, ok := s.lastRequestTouchL1.Load(membershipID); ok {
		if nextAllowedAt, ok := v.(time.Time); ok && now.Before(nextAllowedAt) {
			return nil
		}
	}
	if err := s.repo.TouchMembershipLastRequest(ctx, membershipID, now); err != nil {
		return err
	}
	s.lastRequestTouchL1.Store(membershipID, now.Add(AccountShareModeLastRequestTouchInterval))
	return nil
}

func (s *AccountShareModeService) membershipHasActiveConcurrency(ctx context.Context, membershipID int64) (bool, error) {
	if s == nil || s.concurrencyService == nil || membershipID <= 0 {
		return false, nil
	}
	current, err := s.concurrencyService.GetAccountShareMembershipConcurrency(ctx, membershipID)
	if err != nil {
		return false, err
	}
	return current > 0, nil
}

func membershipIdleDeadline(membership *AccountShareMembership) *time.Time {
	if membership == nil || membership.IdleTimeoutMinutes <= 0 {
		return nil
	}
	base := membership.JoinedAt
	if membership.LastRequestAt != nil {
		base = *membership.LastRequestAt
	}
	deadline := base.Add(time.Duration(membership.IdleTimeoutMinutes) * time.Minute)
	return &deadline
}

func accountShareListingAccountUnavailableAt(listing *AccountShareListing, now time.Time) bool {
	if listing == nil {
		return false
	}
	if listing.AccountStatus != "" {
		if listing.AccountStatus != StatusActive || !listing.AccountSchedulable {
			return true
		}
	}
	if listing.OverloadUntil != nil && now.Before(*listing.OverloadUntil) {
		return true
	}
	if listing.RateLimitResetAt != nil && now.Before(*listing.RateLimitResetAt) {
		return true
	}
	if listing.TempUnschedulableUntil != nil && now.Before(*listing.TempUnschedulableUntil) {
		return true
	}
	if listing.CodexQuotaProtectionReason != nil && strings.TrimSpace(*listing.CodexQuotaProtectionReason) != "" {
		return listing.CodexQuotaProtectionResetAt == nil || now.Before(*listing.CodexQuotaProtectionResetAt)
	}
	return false
}

func accountShareLogTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func accountShareLogStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (s *AccountShareModeService) schedulePostCreateConnectivityTest(listing *AccountShareListing) {
	if s == nil || s.accountTestService == nil || s.accountRepo == nil || listing == nil || listing.AccountID <= 0 {
		return
	}
	accountID := listing.AccountID
	modelID := firstAllowedModel(listing.AllowedModels)
	go func() {
		testCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		result, err := s.accountTestService.RunTestBackground(testCtx, accountID, modelID)
		errorMessage := ""
		if err != nil {
			errorMessage = strings.TrimSpace(err.Error())
		}
		if errorMessage == "" && result != nil && result.Status != "success" {
			errorMessage = strings.TrimSpace(result.ErrorMessage)
			if errorMessage == "" {
				errorMessage = "account share mode post-create connectivity test failed"
			}
		}
		if errorMessage == "" {
			return
		}

		writeCtx, writeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer writeCancel()
		if err := s.accountRepo.SetError(writeCtx, accountID, errorMessage); err != nil {
			log.Printf("account_share_mode: mark account %d error after connectivity test failed: %v", accountID, err)
		}
	}()
}

func (s *AccountShareModeService) AcquireMembershipSlot(ctx context.Context, membershipID int64, maxConcurrency int) (*AcquireResult, error) {
	if s == nil || s.concurrencyService == nil {
		return &AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	return s.concurrencyService.AcquireAccountShareMembershipSlot(ctx, membershipID, maxConcurrency)
}

func (s *AccountShareModeService) ResolvePolicy(ctx context.Context, platform string) (*AccountShareModePolicy, error) {
	if s == nil || s.repo == nil {
		return &AccountShareModePolicy{
			Platform:           strings.TrimSpace(platform),
			PlatformShareRatio: AccountShareModeDefaultPlatformShareRatio,
			OwnerShareRatio:    AccountShareModeDefaultOwnerShareRatio,
			Enabled:            true,
			Version:            1,
		}, nil
	}
	return s.repo.ResolvePolicy(ctx, platform)
}

func (s *AccountShareModeService) GetPolicy(ctx context.Context, platform string) (*AccountShareModePolicy, error) {
	return s.ResolvePolicy(ctx, normalizeAccountShareModePolicyPlatform(platform))
}

func (s *AccountShareModeService) UpdatePolicy(ctx context.Context, input UpdateAccountShareModePolicyInput) (*AccountShareModePolicy, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	platform := normalizeAccountShareModePolicyPlatform(input.Platform)
	if platform != PlatformOpenAI {
		return nil, ErrAccountShareModeOpenAIOnly
	}
	current, err := s.ResolvePolicy(ctx, platform)
	if err != nil {
		return nil, err
	}
	platformRatio := AccountShareModeDefaultPlatformShareRatio
	ownerRatio := AccountShareModeDefaultOwnerShareRatio
	enabled := true
	if current != nil {
		platformRatio = current.PlatformShareRatio
		ownerRatio = current.OwnerShareRatio
		enabled = current.Enabled
	}
	if input.PlatformShareRatio != nil {
		platformRatio = *input.PlatformShareRatio
	}
	if input.OwnerShareRatio != nil {
		ownerRatio = *input.OwnerShareRatio
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if invalidPolicyRatio(platformRatio, ownerRatio) {
		return nil, ErrAccountShareModeInvalidPolicyRatio
	}
	return s.repo.UpsertPolicy(ctx, UpdateAccountShareModePolicyInput{
		Platform:           platform,
		PlatformShareRatio: &platformRatio,
		OwnerShareRatio:    &ownerRatio,
		Enabled:            &enabled,
	})
}

func validateAccountShareListingConfig(seatLimit int, rateMultiplier float64, allowedModels []string, perUserConcurrency, accountConcurrency int, hourlyRate, hourlyFeeWaiverMinimum, minBalance, codex5h, codex7d float64) error {
	if seatLimit < AccountShareModeMinSeats || seatLimit > AccountShareModeMaxSeats {
		return ErrAccountShareModeInvalidSeats
	}
	if invalidNonNegativeFloat(rateMultiplier) {
		return ErrAccountShareModeInvalidRateMultiplier
	}
	if len(normalizeAllowedModels(allowedModels)) == 0 {
		return ErrAccountShareModeAllowedModelsRequired
	}
	if perUserConcurrency <= 0 || accountConcurrency <= 0 || accountConcurrency > AccountShareModeMaxAccountConcurrency {
		return ErrAccountShareModeInvalidConcurrency
	}
	if accountConcurrency < perUserConcurrency*seatLimit {
		return ErrAccountShareModeInsufficientConcurrency
	}
	if invalidNonNegativeFloat(hourlyRate) {
		return ErrAccountShareModeInvalidHourlyRate
	}
	if invalidNonNegativeFloat(hourlyFeeWaiverMinimum) {
		return ErrAccountShareModeInvalidWaiverMinimum
	}
	if invalidNonNegativeFloat(minBalance) {
		return ErrAccountShareModeInvalidMinBalance
	}
	if codex5h > 0 && !isValidCodexLimitPercent(codex5h) {
		return ErrCodexQuotaLimitPercentInvalid
	}
	if codex7d > 0 && !isValidCodexLimitPercent(codex7d) {
		return ErrCodexQuotaLimitPercentInvalid
	}
	return nil
}

func validateAccountShareAccountName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.IndexFunc(name, unicode.IsSpace) >= 0 {
		return ErrAccountShareModeInvalidName
	}
	return nil
}

func compactAccountShareAccountName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.Join(strings.Fields(name), "")
}

func normalizeAccountShareProxyInput(ownerUserID int64, input CreateAccountShareProxyInput) (*Proxy, error) {
	protocol := strings.ToLower(strings.TrimSpace(input.Protocol))
	switch protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, ErrAccountShareModeInvalidProxy
	}

	host := strings.TrimSpace(input.Host)
	if host == "" || strings.IndexFunc(host, unicode.IsSpace) >= 0 {
		return nil, ErrAccountShareModeInvalidProxy
	}
	if input.Port < 1 || input.Port > 65535 {
		return nil, ErrAccountShareModeInvalidProxy
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = fmt.Sprintf("我的代理 %s:%d", host, input.Port)
	}
	name = truncateRunes(name, 100)
	ownerID := ownerUserID
	return &Proxy{
		Name:        name,
		Protocol:    protocol,
		Host:        host,
		Port:        input.Port,
		Username:    strings.TrimSpace(input.Username),
		Password:    strings.TrimSpace(input.Password),
		OwnerUserID: &ownerID,
		Status:      StatusActive,
		MaxAccounts: 0,
	}, nil
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (s *AccountShareModeService) attachListingEditProxy(ctx context.Context, listing *AccountShareListing) error {
	if listing == nil || listing.ProxyID == nil || *listing.ProxyID <= 0 {
		return nil
	}
	if listing.OwnerUserID <= 0 {
		return ErrUserNotFound
	}
	if s == nil || s.proxyRepo == nil {
		return ErrServiceUnavailable
	}
	proxy, err := s.proxyRepo.GetVisibleByID(ctx, listing.OwnerUserID, *listing.ProxyID)
	if err != nil {
		return err
	}
	if proxy == nil {
		return ErrProxyNotFound
	}
	listing.Proxy = accountShareListingProxyFromService(proxy)
	return nil
}

func accountShareListingProxyFromService(proxy *Proxy) *AccountShareListingProxy {
	if proxy == nil {
		return nil
	}
	return &AccountShareListingProxy{
		ID:          proxy.ID,
		Name:        proxy.Name,
		Protocol:    proxy.Protocol,
		Host:        proxy.Host,
		Port:        proxy.Port,
		Username:    proxy.Username,
		OwnerUserID: proxy.OwnerUserID,
		Status:      proxy.Status,
		MaxAccounts: proxy.MaxAccounts,
		CreatedAt:   proxy.CreatedAt,
		UpdatedAt:   proxy.UpdatedAt,
	}
}

func (s *AccountShareModeService) ensureProxyVisibleToUser(ctx context.Context, ownerUserID, proxyID int64) error {
	_, err := s.loadVisibleActiveProxyForUser(ctx, ownerUserID, proxyID)
	return err
}

func (s *AccountShareModeService) ensureProxyAvailableForNewAccount(ctx context.Context, ownerUserID, proxyID int64) error {
	proxy, err := s.loadVisibleActiveProxyForUser(ctx, ownerUserID, proxyID)
	if err != nil {
		return err
	}
	if proxy.MaxAccounts <= 0 {
		return nil
	}
	current, err := s.proxyRepo.CountAccountsByProxyID(ctx, proxy.ID)
	if err != nil {
		return fmt.Errorf("count proxy accounts: %w", err)
	}
	limit := int64(proxy.MaxAccounts)
	if current+1 > limit {
		return ProxyAccountLimitExceededError(proxy.ID, current, limit, 1)
	}
	return nil
}

func (s *AccountShareModeService) loadVisibleActiveProxyForUser(ctx context.Context, ownerUserID, proxyID int64) (*Proxy, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if proxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if s == nil || s.proxyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	proxy, err := s.proxyRepo.GetVisibleByID(ctx, ownerUserID, proxyID)
	if err != nil {
		return nil, err
	}
	if proxy == nil || !proxy.IsActive() {
		return nil, ErrProxyNotFound
	}
	return proxy, nil
}

func DefaultAccountShareModeAllowedModels() []string {
	return append([]string(nil), accountShareModeDefaultAllowedModels...)
}

func AccountShareModeAllowedModelsMapping(models []string) map[string]any {
	normalized := normalizeAllowedModels(models)
	out := make(map[string]any, len(normalized))
	for _, model := range normalized {
		out[model] = model
	}
	return out
}

func normalizeAllowedModelsOrDefault(models []string) []string {
	normalized := normalizeAllowedModels(models)
	if len(normalized) > 0 {
		return normalized
	}
	return DefaultAccountShareModeAllowedModels()
}

func normalizeAllowedModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func normalizePositiveInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func firstAllowedModel(models []string) string {
	for _, model := range normalizeAllowedModels(models) {
		if model != "" {
			return model
		}
	}
	return ""
}

func minBalanceValue(value *float64) float64 {
	if value == nil {
		return AccountShareModeDefaultMinBalance
	}
	return *value
}

func invalidNonNegativeFloat(value float64) bool {
	return value < 0 || math.IsNaN(value) || math.IsInf(value, 0)
}

func invalidPolicyRatio(platformRatio, ownerRatio float64) bool {
	return invalidNonNegativeFloat(platformRatio) ||
		invalidNonNegativeFloat(ownerRatio) ||
		platformRatio > 1 ||
		ownerRatio > 1 ||
		platformRatio+ownerRatio > 1
}

func normalizeAccountShareModePolicyPlatform(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		return PlatformOpenAI
	}
	return platform
}

func isValidCodexLimitPercent(value float64) bool {
	return value >= CodexQuotaMinLimitPercent && value <= CodexQuotaMaxLimitPercent && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func normalizeCodexLimitPercent(value float64) float64 {
	if value <= 0 {
		return AccountShareModeDefaultCodexLimitPercent
	}
	return value
}

func normalizeListingFilters(filters AccountShareListingFilters) AccountShareListingFilters {
	tab := strings.ToLower(strings.TrimSpace(filters.Tab))
	switch tab {
	case AccountShareModeListingTabUsing, AccountShareModeListingTabHistory, AccountShareModeListingTabAll, AccountShareModeListingTabMine:
	default:
		tab = AccountShareModeListingTabAll
	}
	seatLimit := filters.SeatLimit
	if seatLimit < AccountShareModeMinSeats || seatLimit > AccountShareModeMaxSeats {
		seatLimit = 0
	}
	status := strings.ToLower(strings.TrimSpace(filters.Status))
	switch status {
	case AccountShareListingStatusActive, AccountShareListingStatusPaused, AccountShareListingStatusDisabled, "all":
	default:
		status = ""
	}
	accountLevel := normalizeAccountShareListingFilterLevel(filters.AccountLevel)
	return AccountShareListingFilters{
		Tab:                   tab,
		SeatLimit:             seatLimit,
		Search:                strings.TrimSpace(filters.Search),
		Status:                status,
		AvailableOnly:         filters.AvailableOnly,
		PerUserConcurrencyMin: normalizeNonNegativeIntPointer(filters.PerUserConcurrencyMin),
		PerUserConcurrencyMax: normalizeNonNegativeIntPointer(filters.PerUserConcurrencyMax),
		MinBalanceRequiredMin: normalizeNonNegativeFloatPointer(filters.MinBalanceRequiredMin),
		MinBalanceRequiredMax: normalizeNonNegativeFloatPointer(filters.MinBalanceRequiredMax),
		HourlyRateMin:         normalizeNonNegativeFloatPointer(filters.HourlyRateMin),
		HourlyRateMax:         normalizeNonNegativeFloatPointer(filters.HourlyRateMax),
		HourlyFeeWaiverMin:    normalizeNonNegativeFloatPointer(filters.HourlyFeeWaiverMin),
		HourlyFeeWaiverMax:    normalizeNonNegativeFloatPointer(filters.HourlyFeeWaiverMax),
		Models:                normalizeAllowedModels(filters.Models),
		AccountLevel:          accountLevel,
		ViewerIsAdmin:         filters.ViewerIsAdmin,
	}
}

func normalizeAccountShareListingFilterLevel(level string) string {
	raw := strings.ToLower(strings.TrimSpace(level))
	if raw == "" || raw == "all" {
		return ""
	}
	switch raw {
	case AccountLevelUnknown, AccountLevelFree, AccountLevelPlus, AccountLevelPro, AccountLevelTeam:
		return raw
	default:
		return ""
	}
}

func normalizeNonNegativeIntPointer(value *int) *int {
	if value == nil || *value < 0 {
		return nil
	}
	normalized := *value
	return &normalized
}

func normalizeNonNegativeFloatPointer(value *float64) *float64 {
	if value == nil || invalidNonNegativeFloat(*value) {
		return nil
	}
	normalized := *value
	return &normalized
}

func AccountShareHourlyCharge(hourlyRate float64, durationMs int) float64 {
	if hourlyRate <= 0 || durationMs <= 0 {
		return 0
	}
	return hourlyRate * float64(durationMs) / 3600000.0
}

func uniquePositiveInt64s(values []int64) []int64 {
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
	return out
}

func BuildAccountShareModeBillingSnapshot(membership *AccountShareMembership, listing *AccountShareListing, policy *AccountShareModePolicy, baseCharge, hourlyCharge float64, durationMs int) *AccountShareModeBillingSnapshot {
	if membership == nil || listing == nil {
		return nil
	}
	ownerRatio := AccountShareModeDefaultOwnerShareRatio
	platformRatio := AccountShareModeDefaultPlatformShareRatio
	if policy != nil {
		if policy.Enabled {
			ownerRatio = policy.OwnerShareRatio
			platformRatio = policy.PlatformShareRatio
		} else {
			ownerRatio = 0
			platformRatio = 1
		}
	}
	totalCharge := baseCharge + hourlyCharge
	if totalCharge < 0 {
		totalCharge = 0
	}
	return &AccountShareModeBillingSnapshot{
		MembershipID:       membership.ID,
		ListingID:          listing.ID,
		AccountID:          listing.AccountID,
		OwnerUserID:        listing.OwnerUserID,
		ConsumerUserID:     membership.ConsumerUserID,
		APIKeyID:           membership.APIKeyID,
		BaseCharge:         baseCharge,
		HourlyCharge:       hourlyCharge,
		TotalCharge:        totalCharge,
		RateMultiplier:     listing.RateMultiplier,
		HourlyRate:         listing.HourlyRate,
		OwnerShareRatio:    ownerRatio,
		PlatformShareRatio: platformRatio,
		DurationMs:         durationMs,
	}
}

func (s *AccountShareModeService) String() string {
	if s == nil {
		return "AccountShareModeService<nil>"
	}
	return fmt.Sprintf("AccountShareModeService<repo=%t>", s.repo != nil)
}
