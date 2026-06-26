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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
)

const (
	AccountShareModeGroupPlatformOpenAI    = PlatformOpenAI
	AccountShareModeGroupPlatformAnthropic = PlatformAnthropic
	AccountShareModePolicyPlatformUnified  = "account_share_mode"

	AccountShareListingStatusActive   = "active"
	AccountShareListingStatusPaused   = "paused"
	AccountShareListingStatusDisabled = "disabled"

	AccountShareMembershipStatusActive = "active"
	AccountShareMembershipStatusQueued = "queued"
	AccountShareMembershipStatusEnded  = "ended"

	AccountShareModeDefaultMinBalance             = 1.0
	AccountShareModeDefaultPlatformShareRatio     = 0.10
	AccountShareModeDefaultOwnerShareRatio        = 0.90
	AccountShareModeOwnerSelfUseMultiplier        = 0.005
	AccountShareModeDefaultCodexLimitPercent      = CodexQuotaDefaultLimitPercent
	AccountShareModeMinSeats                      = 2
	AccountShareModeMaxSeats                      = 12
	AccountShareModeDefaultPerUserConcurrency     = 5
	AccountShareModeDefaultAccountConcurrency     = 20
	AccountShareModeMaxAccountConcurrency         = 50
	AccountShareModeSeatPrepayDuration            = time.Minute
	AccountShareModeSeatWaiverWindowMax           = time.Hour
	AccountShareModeSeatWaiverSettlementGrace     = 15 * time.Minute
	AccountShareModeSeatBillingInterval           = 15 * time.Second
	AccountShareModeSeatBillingBatchSize          = 100
	AccountShareModeEndMembershipTokenTTL         = 2 * time.Minute
	AccountShareModeMaxIdleTimeoutMinutes         = 10080
	AccountShareModeLastRequestTouchInterval      = 30 * time.Second
	AccountShareModeEditSessionTTL                = 10 * time.Minute
	AccountShareModeQueueMaxItems                 = 5
	AccountShareModeDispatchCooldown              = 5 * time.Minute
	AccountShareRecommendationDefaultLimit        = 5
	AccountShareRecommendationMaxLimit            = 10
	AccountShareRecommendationMaxRequests         = 1000000
	AccountShareRecommendationMaxActiveHours      = 720
	AccountShareRecommendationMaxTokensPerUnit    = 2000000
	AccountShareRecommendationPageSize            = 1000
	AccountShareRecommendationUsageProfileDays    = 3
	AccountShareRecommendationUsageProfileMaxDays = 7
	AccountShareModeListingTabUsing               = "using"
	AccountShareModeListingTabHistory             = "history"
	AccountShareModeListingTabAll                 = "all"
	AccountShareModeListingTabMine                = "mine"
	AccountShareListingSortDefault                = "default"
	AccountShareListingSortAccountConcurrency     = "account_concurrency"
	AccountShareListingSortPerUserConcurrency     = "per_user_concurrency"
	AccountShareListingSortMinBalanceRequired     = "min_balance_required"
	AccountShareListingSortHourlyRate             = "hourly_rate"
	AccountShareListingSortHourlyFeeWaiver        = "hourly_fee_waiver"
	AccountShareListingSortRateMultiplier         = "rate_multiplier"
	AccountShareListingSortRemainingSeats         = "remaining_seats"
	AccountShareListingSortRating                 = "rating"
	AccountShareListingSortUpdatedAt              = "updated_at"
	AccountShareListingSortOrderAsc               = "asc"
	AccountShareListingSortOrderDesc              = "desc"
	AccountShareListingFeatureHourlyFeeWaiver     = "hourly_fee_waiver"
	AccountShareListingFeatureImageGeneration     = "image_generation"
	AccountShareListingFeatureNoHourlyFee         = "no_hourly_fee"
	AccountShareListingFeatureCodexCLIOnly        = "codex_cli_only"
	AccountShareListingFeatureNonCodexCLIOnly     = "non_codex_cli_only"
	AccountShareListingFeatureAvailable           = "available"
	AccountShareSpendRangeToday                   = "today"
	AccountShareSpendRangeCurrentMembership       = "current_membership"
	AccountShareSpendRangeSevenDays               = "7d"
	AccountShareMembershipEndReasonManual         = "manual"
	AccountShareMembershipEndReasonIdleTimeout    = "idle_timeout"
	AccountShareMembershipEndReasonPrepay         = "prepay_insufficient"
	AccountShareMembershipEndReasonUnavailable    = "account_unavailable"
	AccountShareReviewCommentStatusNone           = "none"
	AccountShareReviewCommentStatusPending        = "pending"
	AccountShareReviewCommentStatusApproved       = "approved"
	AccountShareReviewCommentStatusRejected       = "rejected"
	AccountShareReviewCommentStatusFailed         = "failed"
	AccountShareReviewMaxCommentRunes             = 1000
	AccountShareReviewModerationInterval          = 15 * time.Second
	AccountShareReviewModerationBatchSize         = 20
	AccountShareReviewModerationMaxAttempts       = 5
	accountShareModeContextBindingMissingError    = "该分组未绑定账号"
	accountShareModeEndMembershipTokenAction      = "account_share_mode:end_membership:v1"
)

var accountShareModeDefaultAllowedModels = []string{
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"codex-auto-review",
}

var accountShareModeAnthropicDefaultAllowedModels = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-fable-5",
	"claude-opus-4-6",
	"claude-haiku-4-5",
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
	ErrAccountShareQueueFull                   = infraerrors.Conflict("ACCOUNT_SHARE_QUEUE_FULL", "account share reservation queue is full")
	ErrAccountShareQueueInvalid                = infraerrors.BadRequest("ACCOUNT_SHARE_QUEUE_INVALID", "account share reservation queue is invalid")
	ErrAccountShareAPIKeyMustUseModeGroup      = infraerrors.BadRequest("ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP", "api key must use account mode group")
	ErrAccountShareBalanceBelowMinimum         = infraerrors.Forbidden("ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM", "user balance is below account share minimum")
	ErrAccountSharePerUserConcurrencyExceeded  = infraerrors.TooManyRequests("ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED", "account share per-user concurrency exceeded")
	ErrAccountShareModeUnsupportedModel        = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL", "account share account does not support requested model")
	ErrAccountShareModeOpenAIOnly              = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_OPENAI_ONLY", "account share mode only supports OpenAI OAuth accounts")
	ErrAccountShareModeProxyRequired           = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_PROXY_REQUIRED", "proxy is required before account share OAuth login")
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
	ErrAccountShareModeInvalidIdleTimeout      = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT", "idle_timeout_minutes must be between 1 and 10080")
	ErrAccountShareListingInUse                = infraerrors.Conflict("ACCOUNT_SHARE_LISTING_IN_USE", "account share listing has active seats")
	ErrAccountShareListingEditing              = infraerrors.Conflict("ACCOUNT_SHARE_LISTING_EDITING", "account share listing is being edited")
	ErrAccountShareEditSessionRequired         = infraerrors.BadRequest("ACCOUNT_SHARE_EDIT_SESSION_REQUIRED", "account share edit session is required")
	ErrAccountShareEditSessionInvalid          = infraerrors.Conflict("ACCOUNT_SHARE_EDIT_SESSION_INVALID", "account share edit session is invalid or expired")
	ErrAccountShareRelistAccountUnavailable    = infraerrors.BadRequest("ACCOUNT_SHARE_RELIST_ACCOUNT_UNAVAILABLE", "账号测试通过，但账号状态仍不可调度，请先启用账号或恢复调度后重试")
	ErrAccountShareReviewInvalidScore          = infraerrors.BadRequest("ACCOUNT_SHARE_REVIEW_INVALID_SCORE", "评分必须在 0-10 之间")
	ErrAccountShareReviewCommentTooLong        = infraerrors.BadRequest("ACCOUNT_SHARE_REVIEW_COMMENT_TOO_LONG", "评论最多 1000 个字符")
	ErrAccountShareReviewAlreadyExists         = infraerrors.Conflict("ACCOUNT_SHARE_REVIEW_ALREADY_EXISTS", "该次使用已评分")
	ErrAccountShareReviewNoUsage               = infraerrors.BadRequest("ACCOUNT_SHARE_REVIEW_NO_USAGE", "该次使用没有实际请求记录，不能评分")
	ErrAccountShareReviewSelfUse               = infraerrors.BadRequest("ACCOUNT_SHARE_REVIEW_SELF_USE", "不能评价自己上架的账号")
	ErrAccountShareReviewIdentityMissing       = infraerrors.BadRequest("ACCOUNT_SHARE_REVIEW_IDENTITY_MISSING", "该账号缺少邮箱身份，不能评分")
	ErrAccountShareCommentReviewUnavailable    = infraerrors.BadRequest("ACCOUNT_SHARE_COMMENT_REVIEW_UNAVAILABLE", "评论审核未启用或配置不完整，暂时不能提交评论")
	ErrAccountShareRecommendationInvalid       = infraerrors.BadRequest("ACCOUNT_SHARE_RECOMMENDATION_INVALID", "账号推荐测算参数无效")
	ErrAccountShareSpendInvalidRange           = infraerrors.BadRequest("ACCOUNT_SHARE_SPEND_INVALID_RANGE", "invalid account share spend range")
)

func accountShareModeUnsupportedModelError(requestedModel string) error {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ErrAccountShareModeUnsupportedModel
	}
	return fmt.Errorf("%w: %s", ErrAccountShareModeUnsupportedModel, requestedModel)
}

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

func (s *accountShareModeRequestState) clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved = false
	s.membership = nil
	s.listing = nil
	s.err = nil
}

type AccountShareListing struct {
	ID                              int64                     `json:"id"`
	AccountID                       int64                     `json:"account_id"`
	Platform                        string                    `json:"platform"`
	OwnerUserID                     int64                     `json:"owner_user_id"`
	OwnerUsername                   string                    `json:"owner_username,omitempty"`
	AccountName                     string                    `json:"account_name,omitempty"`
	ProxyID                         *int64                    `json:"proxy_id,omitempty"`
	Proxy                           *AccountShareListingProxy `json:"proxy,omitempty"`
	Status                          string                    `json:"status"`
	SeatLimit                       int                       `json:"seat_limit"`
	ActiveSeats                     int                       `json:"active_seats"`
	AccountIdentityID               *int64                    `json:"account_identity_id,omitempty"`
	RatingCount                     int                       `json:"rating_count"`
	RatingScoreSum                  int                       `json:"rating_score_sum"`
	RatingAvg                       float64                   `json:"rating_avg"`
	RateMultiplier                  float64                   `json:"rate_multiplier"`
	AllowedModels                   []string                  `json:"allowed_models"`
	PerUserConcurrency              int                       `json:"per_user_concurrency"`
	AccountConcurrency              int                       `json:"account_concurrency"`
	HourlyRate                      float64                   `json:"hourly_rate"`
	HourlyFeeWaiverMinimum          float64                   `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired              float64                   `json:"min_balance_required"`
	CodexCLIOnly                    bool                      `json:"codex_cli_only"`
	Codex5hLimitPercent             float64                   `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent             float64                   `json:"codex_7d_limit_percent"`
	Anthropic5hLimitPercent         float64                   `json:"anthropic_5h_limit_percent,omitempty"`
	Anthropic7dLimitPercent         float64                   `json:"anthropic_7d_limit_percent,omitempty"`
	AccountLevel                    string                    `json:"account_level,omitempty"`
	AccountPlanType                 string                    `json:"account_plan_type,omitempty"`
	AccountStatus                   string                    `json:"account_status,omitempty"`
	AccountSchedulable              bool                      `json:"account_schedulable"`
	CurrentConcurrency              int                       `json:"current_concurrency"`
	AccountExpiresAt                *time.Time                `json:"account_expires_at,omitempty"`
	SubscriptionExpiresAt           *time.Time                `json:"subscription_expires_at,omitempty"`
	AccountLastUsedAt               *time.Time                `json:"account_last_used_at,omitempty"`
	RateLimitedAt                   *time.Time                `json:"rate_limited_at,omitempty"`
	RateLimitResetAt                *time.Time                `json:"rate_limit_reset_at,omitempty"`
	OverloadUntil                   *time.Time                `json:"overload_until,omitempty"`
	TempUnschedulableUntil          *time.Time                `json:"temp_unschedulable_until,omitempty"`
	TempUnschedulableReason         string                    `json:"temp_unschedulable_reason,omitempty"`
	CodexQuotaProtectionReason      *string                   `json:"codex_quota_protection_reason,omitempty"`
	CodexQuotaProtectionResetAt     *time.Time                `json:"codex_quota_protection_reset_at,omitempty"`
	Codex5hUsage                    *UsageProgress            `json:"codex_5h_usage,omitempty"`
	Codex7dUsage                    *UsageProgress            `json:"codex_7d_usage,omitempty"`
	CodexUsageUpdatedAt             *time.Time                `json:"codex_usage_updated_at,omitempty"`
	AnthropicQuotaProtectionReason  *string                   `json:"anthropic_quota_protection_reason,omitempty"`
	AnthropicQuotaProtectionResetAt *time.Time                `json:"anthropic_quota_protection_reset_at,omitempty"`
	Anthropic5hUsage                *UsageProgress            `json:"anthropic_5h_usage,omitempty"`
	Anthropic7dUsage                *UsageProgress            `json:"anthropic_7d_usage,omitempty"`
	AnthropicUsageUpdatedAt         *time.Time                `json:"anthropic_usage_updated_at,omitempty"`
	CurrentMembershipID             *int64                    `json:"current_membership_id,omitempty"`
	CurrentAPIKeyID                 *int64                    `json:"current_api_key_id,omitempty"`
	CurrentJoinedAt                 *time.Time                `json:"current_joined_at,omitempty"`
	CurrentPaidUntil                *time.Time                `json:"current_paid_until,omitempty"`
	CurrentBilledUntil              *time.Time                `json:"current_billed_until,omitempty"`
	CurrentIdleTimeoutMinutes       *int                      `json:"current_idle_timeout_minutes,omitempty"`
	CurrentLastRequestAt            *time.Time                `json:"current_last_request_at,omitempty"`
	CurrentIdleExpiresAt            *time.Time                `json:"current_idle_expires_at,omitempty"`
	QueueMembershipID               *int64                    `json:"queue_membership_id,omitempty"`
	QueueAPIKeyID                   *int64                    `json:"queue_api_key_id,omitempty"`
	QueueRank                       *int                      `json:"queue_rank,omitempty"`
	QueueStatus                     string                    `json:"queue_status,omitempty"`
	QueueIdleTimeoutMinutes         *int                      `json:"queue_idle_timeout_minutes,omitempty"`
	QueueDispatchCooldownUntil      *time.Time                `json:"queue_dispatch_cooldown_until,omitempty"`
	LastUsedMembershipID            *int64                    `json:"last_used_membership_id,omitempty"`
	LastUsedAt                      *time.Time                `json:"last_used_at,omitempty"`
	EditingByUserID                 *int64                    `json:"editing_by_user_id,omitempty"`
	EditingByUsername               string                    `json:"editing_by_username,omitempty"`
	EditingExpiresAt                *time.Time                `json:"editing_expires_at,omitempty"`
	EditingMine                     bool                      `json:"editing_mine"`
	EditSessionID                   string                    `json:"edit_session_id,omitempty"`
	CreatedAt                       time.Time                 `json:"created_at"`
	UpdatedAt                       time.Time                 `json:"updated_at"`
}

type AccountShareRecommendationInput struct {
	Platform                       string  `json:"platform"`
	Model                          string  `json:"model"`
	APIKeyID                       int64   `json:"api_key_id,omitempty"`
	RequestCount                   int     `json:"request_count"`
	ActiveHours                    float64 `json:"active_hours"`
	InputTokensPerRequest          int     `json:"input_tokens_per_request"`
	OutputTokensPerRequest         int     `json:"output_tokens_per_request"`
	CacheCreationTokensPerRequest  int     `json:"cache_creation_tokens_per_request"`
	CacheReadTokensPerRequest      int     `json:"cache_read_tokens_per_request"`
	ImageInputTokensPerRequest     int     `json:"image_input_tokens_per_request"`
	ImageCacheReadTokensPerRequest int     `json:"image_cache_read_tokens_per_request"`
	ImageOutputTokensPerRequest    int     `json:"image_output_tokens_per_request"`
	SizeTier                       string  `json:"size_tier,omitempty"`
	ServiceTier                    string  `json:"service_tier,omitempty"`
	Limit                          int     `json:"limit"`
}

type AccountShareRecommendationUsage struct {
	Platform             string  `json:"platform"`
	Model                string  `json:"model"`
	APIKeyID             int64   `json:"api_key_id,omitempty"`
	RequestCount         int     `json:"request_count"`
	ActiveHours          float64 `json:"active_hours"`
	InputTokens          int     `json:"input_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	CacheCreationTokens  int     `json:"cache_creation_tokens"`
	CacheReadTokens      int     `json:"cache_read_tokens"`
	ImageInputTokens     int     `json:"image_input_tokens"`
	ImageCacheReadTokens int     `json:"image_cache_read_tokens"`
	ImageOutputTokens    int     `json:"image_output_tokens"`
	SizeTier             string  `json:"size_tier,omitempty"`
	ServiceTier          string  `json:"service_tier,omitempty"`
	Limit                int     `json:"limit"`
}

type AccountShareRecommendationUsageProfileInput struct {
	Platform string
	Model    string
	Days     int
}

type AccountShareRecommendationUsageProfileStats struct {
	TotalRequests            int64
	TotalInputTokens         int64
	TotalOutputTokens        int64
	TotalCacheCreationTokens int64
	TotalCacheReadTokens     int64
	TotalImageOutputTokens   int64
	ActiveHourBuckets        int64
	ModelMatched             bool
}

type AccountShareRecommendationUsageProfile struct {
	Platform                      string    `json:"platform"`
	Model                         string    `json:"model,omitempty"`
	Days                          int       `json:"days"`
	StartTime                     time.Time `json:"start_time"`
	EndTime                       time.Time `json:"end_time"`
	HasHistory                    bool      `json:"has_history"`
	ModelMatched                  bool      `json:"model_matched"`
	UsedModelFallback             bool      `json:"used_model_fallback"`
	Capped                        bool      `json:"capped"`
	TotalRequests                 int64     `json:"total_requests"`
	ActiveHourBuckets             int64     `json:"active_hour_buckets"`
	RequestCount                  int       `json:"request_count"`
	ActiveHours                   float64   `json:"active_hours"`
	InputTokensPerRequest         int       `json:"input_tokens_per_request"`
	OutputTokensPerRequest        int       `json:"output_tokens_per_request"`
	CacheCreationTokensPerRequest int       `json:"cache_creation_tokens_per_request"`
	CacheReadTokensPerRequest     int       `json:"cache_read_tokens_per_request"`
	ImageOutputTokensPerRequest   int       `json:"image_output_tokens_per_request"`
}

type AccountShareRecommendationEstimate struct {
	BillingMode             string  `json:"billing_mode"`
	BaseRequestCost         float64 `json:"base_request_cost"`
	RequestCost             float64 `json:"request_cost"`
	PerRequestCost          float64 `json:"per_request_cost"`
	HourlyGrossCost         float64 `json:"hourly_gross_cost"`
	HourlyWaivedCost        float64 `json:"hourly_waived_cost"`
	HourlyNetCost           float64 `json:"hourly_net_cost"`
	WaiverRequiredAmount    float64 `json:"waiver_required_amount"`
	WaiverUsageAmount       float64 `json:"waiver_usage_amount"`
	WaiverEligible          bool    `json:"waiver_eligible"`
	TotalCost               float64 `json:"total_cost"`
	UpfrontRequired         float64 `json:"upfront_required"`
	EffectiveRateMultiplier float64 `json:"effective_rate_multiplier"`
	EffectiveHourlyRate     float64 `json:"effective_hourly_rate"`
	OwnerSelfUse            bool    `json:"owner_self_use"`
}

type AccountShareRecommendationCandidate struct {
	Rank     int                                `json:"rank"`
	Listing  AccountShareListing                `json:"listing"`
	Estimate AccountShareRecommendationEstimate `json:"estimate"`
	Score    float64                            `json:"score"`
	Tags     []string                           `json:"tags"`
	Reasons  []string                           `json:"reasons"`
	Warnings []string                           `json:"warnings,omitempty"`
}

type AccountShareRecommendationResult struct {
	Input          AccountShareRecommendationUsage       `json:"input"`
	CandidateCount int                                   `json:"candidate_count"`
	Items          []AccountShareRecommendationCandidate `json:"items"`
	Recommended    *AccountShareRecommendationCandidate  `json:"recommended,omitempty"`
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
	QueueRank                      int        `json:"queue_rank"`
	HourlyRateSnapshot             float64    `json:"hourly_rate_snapshot"`
	HourlyFeeWaiverMinimumSnapshot float64    `json:"hourly_fee_waiver_minimum_snapshot"`
	IdleTimeoutMinutes             int        `json:"idle_timeout_minutes"`
	JoinedAt                       time.Time  `json:"joined_at"`
	LastRequestAt                  *time.Time `json:"last_request_at,omitempty"`
	EndedAt                        *time.Time `json:"ended_at,omitempty"`
	EndedReason                    string     `json:"ended_reason,omitempty"`
	PaidUntil                      *time.Time `json:"paid_until,omitempty"`
	BilledUntil                    *time.Time `json:"billed_until,omitempty"`
	DispatchFailedAt               *time.Time `json:"dispatch_failed_at,omitempty"`
	DispatchCooldownUntil          *time.Time `json:"dispatch_cooldown_until,omitempty"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`
}

type AccountShareReview struct {
	ID                  int64     `json:"id"`
	AccountIdentityID   int64     `json:"account_identity_id"`
	ListingID           int64     `json:"listing_id,omitempty"`
	AccountID           int64     `json:"account_id,omitempty"`
	MembershipID        int64     `json:"membership_id,omitempty"`
	OwnerUserID         int64     `json:"owner_user_id"`
	OwnerUsername       string    `json:"owner_username,omitempty"`
	ConsumerUserID      int64     `json:"consumer_user_id,omitempty"`
	ConsumerUsername    string    `json:"consumer_username,omitempty"`
	AccountName         string    `json:"account_name,omitempty"`
	Platform            string    `json:"platform,omitempty"`
	Score               int       `json:"score"`
	Comment             string    `json:"comment,omitempty"`
	CommentStatus       string    `json:"comment_status"`
	CommentRejectReason string    `json:"comment_reject_reason,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type AccountShareMySpendInput struct {
	ListingID    int64
	MembershipID *int64
	Range        string
	Timezone     string
	Now          time.Time
}

type AccountShareMySpendQuery struct {
	ListingID    int64
	ConsumerID   int64
	MembershipID *int64
	Range        string
	StartTime    time.Time
	EndTime      time.Time
}

type AccountShareMySpendListing struct {
	ID            int64  `json:"id"`
	AccountID     int64  `json:"account_id"`
	AccountName   string `json:"account_name,omitempty"`
	Platform      string `json:"platform"`
	OwnerUserID   int64  `json:"owner_user_id"`
	OwnerUsername string `json:"owner_username,omitempty"`
}

type AccountShareMySpendMembership struct {
	ID                 int64      `json:"id"`
	APIKeyID           int64      `json:"api_key_id"`
	Status             string     `json:"status"`
	QueueRank          int        `json:"queue_rank"`
	JoinedAt           time.Time  `json:"joined_at"`
	LastRequestAt      *time.Time `json:"last_request_at,omitempty"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	EndedReason        string     `json:"ended_reason,omitempty"`
	PaidUntil          *time.Time `json:"paid_until,omitempty"`
	BilledUntil        *time.Time `json:"billed_until,omitempty"`
	HourlyRate         float64    `json:"hourly_rate"`
	WaiverMinimum      float64    `json:"waiver_minimum"`
	IdleTimeoutMinutes int        `json:"idle_timeout_minutes"`
}

type AccountShareMySpendModelBreakdown struct {
	Model               string  `json:"model"`
	RequestCount        int64   `json:"request_count"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	RequestCost         float64 `json:"request_cost"`
	AverageRequestCost  float64 `json:"average_request_cost"`
}

type AccountShareMySpendSummary struct {
	Range               string                              `json:"range"`
	StartTime           time.Time                           `json:"start_time"`
	EndTime             time.Time                           `json:"end_time"`
	Listing             AccountShareMySpendListing          `json:"listing"`
	Membership          *AccountShareMySpendMembership      `json:"membership,omitempty"`
	RequestCount        int64                               `json:"request_count"`
	InputTokens         int64                               `json:"input_tokens"`
	OutputTokens        int64                               `json:"output_tokens"`
	CacheCreationTokens int64                               `json:"cache_creation_tokens"`
	CacheReadTokens     int64                               `json:"cache_read_tokens"`
	TotalTokens         int64                               `json:"total_tokens"`
	RequestCost         float64                             `json:"request_cost"`
	HourlyCharge        float64                             `json:"hourly_charge"`
	HourlyRefund        float64                             `json:"hourly_refund"`
	HourlyWaiverRefund  float64                             `json:"hourly_waiver_refund"`
	HourlyNetCost       float64                             `json:"hourly_net_cost"`
	TotalCost           float64                             `json:"total_cost"`
	LastActivityAt      *time.Time                          `json:"last_activity_at,omitempty"`
	ModelBreakdown      []AccountShareMySpendModelBreakdown `json:"model_breakdown"`
}

type SubmitAccountShareReviewInput struct {
	Score   int
	Comment string
}

type AccountShareReviewModerationResult struct {
	Passed        bool
	RejectReason  string
	ModelSnapshot string
	URLSnapshot   string
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
	Tab           string
	Platform      string
	SeatLimit     int
	SeatLimits    []int
	Search        string
	Status        string
	AvailableOnly bool
	Models        []string
	AccountLevel  string
	OwnerUserID   int64
	FeatureTags   []string
	SortBy        string
	SortOrder     string
	Sorts         []AccountShareListingSortCriterion
	ViewerIsAdmin bool
	SkipTotal     bool
}

type AccountShareListingSortCriterion struct {
	SortBy    string
	SortOrder string
}

type CreateAccountShareListingInput struct {
	Name                    string
	Notes                   *string
	ProxyID                 int64
	Concurrency             int
	SeatLimit               int
	RateMultiplier          float64
	AllowedModels           []string
	PerUserConcurrency      int
	HourlyRate              float64
	HourlyFeeWaiverMinimum  float64
	MinBalanceRequired      *float64
	CodexCLIOnly            bool
	Codex5hLimitPercent     float64
	Codex7dLimitPercent     float64
	Anthropic5hLimitPercent float64
	Anthropic7dLimitPercent float64
	TokenInfo               *OpenAITokenInfo
	AnthropicTokenInfo      *TokenInfo
	AutoPauseOnExpired      *bool
	ExpiresAt               *time.Time
}

type UpdateAccountShareListingInput struct {
	Name                    *string
	ProxyID                 *int64
	Status                  *string
	SeatLimit               *int
	RateMultiplier          *float64
	AllowedModels           *[]string
	PerUserConcurrency      *int
	HourlyRate              *float64
	HourlyFeeWaiverMinimum  *float64
	MinBalanceRequired      *float64
	CodexCLIOnly            *bool
	Codex5hLimitPercent     *float64
	Codex7dLimitPercent     *float64
	Anthropic5hLimitPercent *float64
	Anthropic7dLimitPercent *float64
	Concurrency             *int
	EditSessionID           string
	ForceActiveEdit         bool
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
	CreatePlatformListing(ctx context.Context, account *Account, listing *AccountShareListing, modeGroupID int64) (*AccountShareListing, error)
	GetListingByID(ctx context.Context, listingID int64, viewerUserID int64) (*AccountShareListing, error)
	GetListingByAccountID(ctx context.Context, accountID int64) (*AccountShareListing, error)
	ListListings(ctx context.Context, viewerUserID int64, filters AccountShareListingFilters, params pagination.PaginationParams) ([]AccountShareListing, *pagination.PaginationResult, error)
	GetMySpendSummary(ctx context.Context, query AccountShareMySpendQuery) (*AccountShareMySpendSummary, error)
	BeginListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input BeginAccountShareListingEditInput) (*AccountShareListing, error)
	ReleaseListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, sessionID string) (*AccountShareListing, error)
	UpdateListing(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input UpdateAccountShareListingInput) (*AccountShareListing, error)
	JoinListing(ctx context.Context, consumerUserID int64, apiKeyID int64, listingID int64, idleTimeoutMinutes int) (*AccountShareMembership, error)
	EndMembership(ctx context.Context, consumerUserID int64, membershipID int64) (*AccountShareMembership, error)
	UpdateMembershipIdleTimeout(ctx context.Context, consumerUserID int64, membershipID int64, idleTimeoutMinutes int) (*AccountShareMembership, error)
	SubmitReview(ctx context.Context, consumerUserID int64, membershipID int64, input SubmitAccountShareReviewInput) (*AccountShareReview, error)
	ListListingReviews(ctx context.Context, viewerUserID int64, listingID int64, params pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error)
	ListOwnerReviews(ctx context.Context, viewerUserID int64, ownerUserID int64, params pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error)
	ClaimPendingReviewModerations(ctx context.Context, now time.Time, limit int) ([]AccountShareReview, error)
	CompleteReviewModeration(ctx context.Context, reviewID int64, result AccountShareReviewModerationResult) error
	FailReviewModeration(ctx context.Context, reviewID int64, reason string, nextRetryAt time.Time, maxAttempts int) error
	ListMembershipQueue(ctx context.Context, consumerUserID int64, apiKeyID int64) ([]AccountShareMembership, error)
	ReorderMembershipQueue(ctx context.Context, consumerUserID int64, apiKeyID int64, membershipIDs []int64) ([]AccountShareMembership, error)
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
	ActivateNextQueuedMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64, afterRank int, now time.Time) (*AccountShareMembership, *AccountShareListing, error)
	SuspendMembershipForDispatchFailure(ctx context.Context, membershipID int64, failedAt time.Time, cooldownUntil time.Time) (*AccountShareMembership, error)
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

type accountShareRecommendationUsageProfileRepository interface {
	GetAccountShareRecommendationUsageProfile(ctx context.Context, userID int64, model string, startTime, endTime time.Time) (*AccountShareRecommendationUsageProfileStats, error)
}

type AccountShareModeService struct {
	repo                 AccountShareModeRepository
	accountRepo          AccountRepository
	apiKeyRepo           APIKeyRepository
	userRepo             UserRepository
	proxyRepo            AccountShareModeProxyRepository
	usageProfileRepo     accountShareRecommendationUsageProfileRepository
	openaiOAuthService   *OpenAIOAuthService
	oauthService         *OAuthService
	accountTestService   accountShareConnectivityTester
	rateLimitService     accountShareAccountStateRecovery
	concurrencyService   *ConcurrencyService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
	billingService       *BillingService
	modelPricingResolver *ModelPricingResolver
	reviewSettingRepo    SettingRepository
	reviewHTTPClient     *http.Client
	actionTokenSecret    []byte
	seatBillingStopCh    chan struct{}
	seatBillingStopOnce  sync.Once
	seatBillingStartOnce sync.Once
	seatBillingWG        sync.WaitGroup
	reviewStopCh         chan struct{}
	reviewStopOnce       sync.Once
	reviewStartOnce      sync.Once
	reviewWG             sync.WaitGroup
	lastRequestTouchL1   sync.Map
}

func NewAccountShareModeService(
	repo AccountShareModeRepository,
	accountRepo AccountRepository,
	apiKeyRepo APIKeyRepository,
	userRepo UserRepository,
	proxyRepo AccountShareModeProxyRepository,
	openaiOAuthService *OpenAIOAuthService,
	oauthServices ...*OAuthService,
) *AccountShareModeService {
	var oauthService *OAuthService
	if len(oauthServices) > 0 {
		oauthService = oauthServices[0]
	}
	return &AccountShareModeService{
		repo:               repo,
		accountRepo:        accountRepo,
		apiKeyRepo:         apiKeyRepo,
		userRepo:           userRepo,
		proxyRepo:          proxyRepo,
		openaiOAuthService: openaiOAuthService,
		oauthService:       oauthService,
		seatBillingStopCh:  make(chan struct{}),
		reviewStopCh:       make(chan struct{}),
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

func (s *AccountShareModeService) SetRecommendationPricingDependencies(billingService *BillingService, resolver *ModelPricingResolver) {
	if s == nil {
		return
	}
	s.billingService = billingService
	s.modelPricingResolver = resolver
}

func (s *AccountShareModeService) SetRecommendationUsageProfileRepository(repo accountShareRecommendationUsageProfileRepository) {
	if s == nil {
		return
	}
	s.usageProfileRepo = repo
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

func (s *AccountShareModeService) GenerateAnthropicAuthURL(ctx context.Context, ownerUserID int64, proxyID *int64) (*GenerateAuthURLResult, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if proxyID == nil || *proxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if s == nil || s.oauthService == nil {
		return nil, ErrServiceUnavailable
	}
	if err := s.ensureProxyAvailableForNewAccount(ctx, ownerUserID, *proxyID); err != nil {
		return nil, err
	}
	return s.oauthService.GenerateAuthURL(ctx, proxyID)
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

func (s *AccountShareModeService) ExchangeAnthropicCodeAndCreateListing(ctx context.Context, ownerUserID int64, exchange *ExchangeCodeInput, input CreateAccountShareListingInput) (*AccountShareListing, error) {
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
	input.AllowedModels = normalizeAllowedModelsOrDefaultForPlatform(PlatformAnthropic, input.AllowedModels)
	input.Anthropic5hLimitPercent = normalizeAnthropicLimitPercent(input.Anthropic5hLimitPercent)
	input.Anthropic7dLimitPercent = normalizeAnthropicLimitPercent(input.Anthropic7dLimitPercent)
	if err := validateAccountShareListingConfig(input.SeatLimit, input.RateMultiplier, input.AllowedModels, input.PerUserConcurrency, input.Concurrency, input.HourlyRate, input.HourlyFeeWaiverMinimum, minBalanceValue(input.MinBalanceRequired), input.Anthropic5hLimitPercent, input.Anthropic7dLimitPercent); err != nil {
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
	if s == nil || s.oauthService == nil {
		return nil, ErrServiceUnavailable
	}
	tokenInfo, err := s.oauthService.ExchangeCode(ctx, exchange)
	if err != nil {
		return nil, err
	}
	input.AnthropicTokenInfo = tokenInfo
	return s.CreateAnthropicListingFromToken(ctx, ownerUserID, input)
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
	created, err := s.repo.CreatePlatformListing(ctx, account, listing, modeGroup.ID)
	if err != nil {
		return nil, err
	}
	s.enrichListingRuntime(ctx, created)
	s.schedulePostCreateConnectivityTest(created)
	return created, nil
}

func (s *AccountShareModeService) CreateAnthropicListingFromToken(ctx context.Context, ownerUserID int64, input CreateAccountShareListingInput) (*AccountShareListing, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.ProxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if input.AnthropicTokenInfo == nil {
		return nil, ErrOwnedAccountCredentialsInvalid
	}
	if err := s.ensureProxyAvailableForNewAccount(ctx, ownerUserID, input.ProxyID); err != nil {
		return nil, err
	}
	if err := validateAccountShareAccountName(input.Name); err != nil {
		return nil, err
	}
	input.AllowedModels = normalizeAllowedModelsOrDefaultForPlatform(PlatformAnthropic, input.AllowedModels)
	input.Anthropic5hLimitPercent = normalizeAnthropicLimitPercent(input.Anthropic5hLimitPercent)
	input.Anthropic7dLimitPercent = normalizeAnthropicLimitPercent(input.Anthropic7dLimitPercent)
	if err := validateAccountShareListingConfig(input.SeatLimit, input.RateMultiplier, input.AllowedModels, input.PerUserConcurrency, input.Concurrency, input.HourlyRate, input.HourlyFeeWaiverMinimum, minBalanceValue(input.MinBalanceRequired), input.Anthropic5hLimitPercent, input.Anthropic7dLimitPercent); err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil || s.oauthService == nil {
		return nil, ErrServiceUnavailable
	}
	modeGroup, err := s.repo.EnsureModeGroup(ctx, PlatformAnthropic)
	if err != nil {
		return nil, err
	}
	if modeGroup == nil || modeGroup.ID <= 0 {
		return nil, ErrAccountShareModeGroupUnavailable
	}

	credentials := BuildClaudeAccountCredentials(input.AnthropicTokenInfo)
	credentials["model_mapping"] = AccountShareModeAllowedModelsMapping(input.AllowedModels)
	extra := BuildClaudeAccountCredentialImportExtra(input.AnthropicTokenInfo)
	extra["account_share_mode"] = true
	extra["anthropic_5h_limit_percent"] = input.Anthropic5hLimitPercent
	extra["anthropic_7d_limit_percent"] = input.Anthropic7dLimitPercent

	accountName := strings.TrimSpace(input.Name)
	if accountName == "" {
		accountName = DeriveAccountCredentialImportName(PlatformAnthropic, credentials, extra, 1)
	}
	accountName = compactAccountShareAccountName(accountName)
	concurrency := input.Concurrency
	if concurrency <= 0 {
		concurrency = AccountShareModeDefaultAccountConcurrency
	}
	account := &Account{
		Name:                  accountName,
		Notes:                 normalizeAccountNotes(input.Notes),
		Platform:              PlatformAnthropic,
		AccountLevel:          AccountLevelUnknown,
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
		OwnerUserID:             ownerUserID,
		Status:                  AccountShareListingStatusActive,
		SeatLimit:               input.SeatLimit,
		RateMultiplier:          input.RateMultiplier,
		AllowedModels:           input.AllowedModels,
		PerUserConcurrency:      normalizePositiveInt(input.PerUserConcurrency, AccountShareModeDefaultPerUserConcurrency),
		AccountConcurrency:      account.Concurrency,
		HourlyRate:              input.HourlyRate,
		HourlyFeeWaiverMinimum:  input.HourlyFeeWaiverMinimum,
		MinBalanceRequired:      minBalanceValue(input.MinBalanceRequired),
		Codex5hLimitPercent:     input.Anthropic5hLimitPercent,
		Codex7dLimitPercent:     input.Anthropic7dLimitPercent,
		Anthropic5hLimitPercent: input.Anthropic5hLimitPercent,
		Anthropic7dLimitPercent: input.Anthropic7dLimitPercent,
	}
	created, err := s.repo.CreatePlatformListing(ctx, account, listing, modeGroup.ID)
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

func (s *AccountShareModeService) GetMySpendSummary(ctx context.Context, viewerUserID int64, input AccountShareMySpendInput) (*AccountShareMySpendSummary, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.ListingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	spendRange, err := normalizeAccountShareSpendRange(input.Range)
	if err != nil {
		return nil, err
	}
	now := input.Now
	if now.IsZero() {
		now = timezone.NowInUserLocation(input.Timezone)
	}
	query := AccountShareMySpendQuery{
		ListingID:    input.ListingID,
		ConsumerID:   viewerUserID,
		MembershipID: input.MembershipID,
		Range:        spendRange,
		EndTime:      now,
	}
	switch spendRange {
	case AccountShareSpendRangeToday:
		query.StartTime = timezone.StartOfDayInUserLocation(now, input.Timezone)
	case AccountShareSpendRangeSevenDays:
		query.StartTime = now.AddDate(0, 0, -7)
	case AccountShareSpendRangeCurrentMembership:
		// The repository resolves the membership window because it owns membership persistence.
	default:
		return nil, ErrAccountShareSpendInvalidRange
	}
	summary, err := s.repo.GetMySpendSummary(ctx, query)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, ErrAccountShareListingNotFound
	}
	return summary, nil
}

func (s *AccountShareModeService) GetRecommendationUsageProfile(ctx context.Context, viewerUserID int64, input AccountShareRecommendationUsageProfileInput) (*AccountShareRecommendationUsageProfile, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.usageProfileRepo == nil {
		return nil, ErrServiceUnavailable
	}
	normalized, err := normalizeAccountShareRecommendationUsageProfileInput(input)
	if err != nil {
		return nil, err
	}

	endTime := time.Now().UTC()
	startTime := endTime.Add(-time.Duration(normalized.Days) * 24 * time.Hour)
	stats, err := s.usageProfileRepo.GetAccountShareRecommendationUsageProfile(ctx, viewerUserID, normalized.Model, startTime, endTime)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &AccountShareRecommendationUsageProfileStats{}
	}
	return buildAccountShareRecommendationUsageProfile(normalized, startTime, endTime, stats), nil
}

func (s *AccountShareModeService) RecommendListings(ctx context.Context, viewerUserID int64, viewerIsAdmin bool, input AccountShareRecommendationInput) (*AccountShareRecommendationResult, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil || s.billingService == nil || s.modelPricingResolver == nil {
		return nil, ErrServiceUnavailable
	}
	normalized, err := normalizeAccountShareRecommendationInput(input)
	if err != nil {
		return nil, err
	}
	groupID, err := s.resolveRecommendationGroupID(ctx, viewerUserID, normalized.Platform, normalized.APIKeyID)
	if err != nil {
		return nil, err
	}
	resolvedPricing := s.modelPricingResolver.Resolve(ctx, PricingInput{
		Model:   normalized.Model,
		GroupID: groupID,
	})

	now := time.Now().UTC()
	candidatesByAccount := make(map[string]AccountShareRecommendationCandidate)
	for page := 1; ; page++ {
		listings, pageResult, err := s.ListListings(ctx, viewerUserID, viewerIsAdmin, AccountShareListingFilters{
			Tab:       AccountShareModeListingTabAll,
			Platform:  normalized.Platform,
			Status:    AccountShareListingStatusActive,
			SkipTotal: true,
		}, pagination.PaginationParams{Page: page, PageSize: AccountShareRecommendationPageSize})
		if err != nil {
			return nil, err
		}
		for _, listing := range listings {
			if listing.Status != AccountShareListingStatusActive {
				continue
			}
			if listingPlatform := normalizeAccountShareListingPlatform(listing.Platform); listingPlatform != normalized.Platform {
				continue
			}
			if !accountShareListingSupportsRecommendationModel(listing, normalized.Model) {
				continue
			}
			if listing.ActiveSeats >= listing.SeatLimit && listing.OwnerUserID != viewerUserID {
				continue
			}
			if listing.EditingExpiresAt != nil && now.Before(*listing.EditingExpiresAt) {
				continue
			}
			if accountShareListingAccountUnavailableAt(&listing, now) {
				continue
			}
			estimate, err := s.estimateAccountShareRecommendationCost(ctx, viewerUserID, groupID, resolvedPricing, normalized, listing)
			if err != nil {
				return nil, err
			}
			tags, reasons, warnings := buildAccountShareRecommendationMessages(listing, estimate)
			candidate := AccountShareRecommendationCandidate{
				Listing:  listing,
				Estimate: estimate,
				Score:    accountShareRecommendationScore(listing, estimate, warnings),
				Tags:     tags,
				Reasons:  reasons,
				Warnings: warnings,
			}
			dedupeKey := accountShareRecommendationCandidateDedupeKey(listing)
			if existing, ok := candidatesByAccount[dedupeKey]; ok && !accountShareRecommendationCandidateRanksBefore(candidate, existing) {
				continue
			}
			candidatesByAccount[dedupeKey] = candidate
		}
		if pageResult == nil {
			if len(listings) < AccountShareRecommendationPageSize {
				break
			}
			continue
		}
		if page >= pageResult.Pages {
			break
		}
	}
	candidates := make([]AccountShareRecommendationCandidate, 0, len(candidatesByAccount))
	for _, candidate := range candidatesByAccount {
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return accountShareRecommendationCandidateRanksBefore(candidates[i], candidates[j])
	})
	if len(candidates) > normalized.Limit {
		candidates = candidates[:normalized.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
		if i == 0 {
			candidates[i].Tags = prependUniqueString(candidates[i].Tags, "推荐")
			candidates[i].Reasons = prependUniqueString(candidates[i].Reasons, "综合费用、可用席位、评分与并发后最匹配当前测算")
		}
	}

	var recommended *AccountShareRecommendationCandidate
	if len(candidates) > 0 {
		best := candidates[0]
		recommended = &best
	}
	return &AccountShareRecommendationResult{
		Input:          buildAccountShareRecommendationUsage(normalized),
		CandidateCount: len(candidatesByAccount),
		Items:          candidates,
		Recommended:    recommended,
	}, nil
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

func (s *AccountShareModeService) resolveRecommendationGroupID(ctx context.Context, viewerUserID int64, platform string, apiKeyID int64) (*int64, error) {
	if apiKeyID <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	if s == nil || s.repo == nil || s.apiKeyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	key, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	if key == nil || key.UserID != viewerUserID {
		return nil, ErrAPIKeyNotFound
	}
	if key.GroupID == nil || *key.GroupID <= 0 {
		return nil, ErrAccountShareAPIKeyMustUseModeGroup
	}
	modeGroup, err := s.repo.GetModeGroup(ctx, platform)
	if err != nil {
		return nil, err
	}
	if modeGroup == nil || modeGroup.ID != *key.GroupID {
		return nil, ErrAccountShareAPIKeyMustUseModeGroup
	}
	groupID := *key.GroupID
	return &groupID, nil
}

func (s *AccountShareModeService) estimateAccountShareRecommendationCost(ctx context.Context, viewerUserID int64, groupID *int64, resolved *ResolvedPricing, input AccountShareRecommendationInput, listing AccountShareListing) (AccountShareRecommendationEstimate, error) {
	if resolved == nil {
		resolved = s.modelPricingResolver.Resolve(ctx, PricingInput{
			Model:   input.Model,
			GroupID: groupID,
		})
	}
	rateMultiplier := listing.RateMultiplier
	hourlyRate := listing.HourlyRate
	waiverMinimum := listing.HourlyFeeWaiverMinimum
	minBalanceRequired := listing.MinBalanceRequired
	ownerSelfUse := listing.OwnerUserID == viewerUserID
	if ownerSelfUse {
		rateMultiplier = AccountShareModeOwnerSelfUseMultiplier
		hourlyRate = 0
		waiverMinimum = 0
		minBalanceRequired = 0
	}
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx:            ctx,
		Model:          input.Model,
		GroupID:        groupID,
		Tokens:         buildAccountShareRecommendationTokens(input),
		RequestCount:   input.RequestCount,
		SizeTier:       input.SizeTier,
		RateMultiplier: rateMultiplier,
		ServiceTier:    input.ServiceTier,
		Resolver:       s.modelPricingResolver,
		Resolved:       resolved,
	})
	if err != nil {
		if errors.Is(err, ErrModelPricingUnavailable) {
			return AccountShareRecommendationEstimate{}, ErrAccountShareRecommendationInvalid.WithMetadata(map[string]string{
				"field":   "model",
				"message": "当前模型缺少可用于测算的定价配置",
			}).WithCause(err)
		}
		return AccountShareRecommendationEstimate{}, err
	}
	if cost == nil {
		return AccountShareRecommendationEstimate{}, ErrAccountShareRecommendationInvalid.WithMetadata(map[string]string{
			"field":   "model",
			"message": "当前模型无法生成费用明细",
		})
	}
	activeMs := accountShareRecommendationDurationMs(input.ActiveHours)
	hourlyGross := AccountShareHourlyCharge(hourlyRate, activeMs)
	waiverRequired := waiverMinimum * input.ActiveHours
	waiverEligible := waiverRequired > 0 && cost.ActualCost >= waiverRequired
	hourlyWaived := 0.0
	if waiverEligible {
		hourlyWaived = hourlyGross
	}
	hourlyNet := hourlyGross - hourlyWaived
	if hourlyNet < 0 {
		hourlyNet = 0
	}
	prepay := AccountShareHourlyCharge(hourlyRate, int(AccountShareModeSeatPrepayDuration.Milliseconds()))
	billingMode := cost.BillingMode
	if billingMode == "" {
		billingMode = string(BillingModeToken)
	}
	perRequestCost := 0.0
	if input.RequestCount > 0 {
		perRequestCost = cost.ActualCost / float64(input.RequestCount)
	}
	return AccountShareRecommendationEstimate{
		BillingMode:             billingMode,
		BaseRequestCost:         cost.TotalCost,
		RequestCost:             cost.ActualCost,
		PerRequestCost:          perRequestCost,
		HourlyGrossCost:         hourlyGross,
		HourlyWaivedCost:        hourlyWaived,
		HourlyNetCost:           hourlyNet,
		WaiverRequiredAmount:    waiverRequired,
		WaiverUsageAmount:       cost.ActualCost,
		WaiverEligible:          waiverEligible,
		TotalCost:               cost.ActualCost + hourlyNet,
		UpfrontRequired:         minBalanceRequired + prepay,
		EffectiveRateMultiplier: rateMultiplier,
		EffectiveHourlyRate:     hourlyRate,
		OwnerSelfUse:            ownerSelfUse,
	}, nil
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
	if input.Anthropic5hLimitPercent != nil && !isValidAnthropicLimitPercent(*input.Anthropic5hLimitPercent) {
		return nil, ErrCodexQuotaLimitPercentInvalid
	}
	if input.Anthropic7dLimitPercent != nil && !isValidAnthropicLimitPercent(*input.Anthropic7dLimitPercent) {
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
		input.Anthropic5hLimitPercent == nil &&
		input.Anthropic7dLimitPercent == nil &&
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
		input.Anthropic5hLimitPercent != nil ||
		input.Anthropic7dLimitPercent != nil ||
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
	if err := s.ensureAPIKeyMatchesListingPlatform(ctx, apiKey, listing); err != nil {
		return nil, err
	}
	ownerSelfUse := IsAccountShareModeOwnerSelfUse(&AccountShareMembership{ConsumerUserID: consumerUserID}, listing)
	if listing.Status != AccountShareListingStatusActive {
		return nil, ErrAccountShareListingNotActive
	}
	now := time.Now().UTC()
	if accountShareListingAccountUnavailableAt(listing, now) {
		log.Printf("account_share_mode: join rejected stage=service_precheck_unavailable user_id=%d listing_id=%d api_key_id=%d account_id=%d account_status=%q account_schedulable=%t overload_until=%s rate_limit_reset_at=%s temp_unschedulable_until=%s codex_reason=%s codex_reset_at=%s anthropic_reason=%s anthropic_reset_at=%s",
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
			accountShareLogStringPtr(listing.AnthropicQuotaProtectionReason),
			accountShareLogTimePtr(listing.AnthropicQuotaProtectionResetAt),
		)
		result, err := s.repo.EndUnavailableAccountMemberships(ctx, listing.AccountID, now, AccountShareModeSeatBillingBatchSize)
		if err != nil {
			return nil, err
		}
		s.invalidateSeatBillingCaches(result)
		return nil, ErrAccountShareAccountUnavailable
	}
	if !ownerSelfUse && user.Balance < listing.MinBalanceRequired {
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
	if !ownerSelfUse {
		s.invalidateSeatBillingCaches(&AccountShareSeatBillingResult{DebitUserIDs: []int64{consumerUserID}})
	}
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

func (s *AccountShareModeService) ReorderMembershipQueue(ctx context.Context, consumerUserID, apiKeyID int64, membershipIDs []int64) ([]AccountShareMembership, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if apiKeyID <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	if len(membershipIDs) == 0 || len(membershipIDs) > AccountShareModeQueueMaxItems {
		return nil, ErrAccountShareQueueInvalid
	}
	seen := make(map[int64]struct{}, len(membershipIDs))
	for _, id := range membershipIDs {
		if id <= 0 {
			return nil, ErrAccountShareQueueInvalid
		}
		if _, ok := seen[id]; ok {
			return nil, ErrAccountShareQueueInvalid
		}
		seen[id] = struct{}{}
	}
	if s == nil || s.repo == nil || s.apiKeyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey.UserID != consumerUserID {
		return nil, ErrInsufficientPerms
	}
	return s.repo.ReorderMembershipQueue(ctx, consumerUserID, apiKeyID, membershipIDs)
}

func (s *AccountShareModeService) ListMembershipQueue(ctx context.Context, consumerUserID, apiKeyID int64) ([]AccountShareMembership, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if apiKeyID <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	if s == nil || s.repo == nil || s.apiKeyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey.UserID != consumerUserID {
		return nil, ErrInsufficientPerms
	}
	return s.repo.ListMembershipQueue(ctx, consumerUserID, apiKeyID)
}

func (s *AccountShareModeService) ensureAPIKeyMatchesListingPlatform(ctx context.Context, apiKey *APIKey, listing *AccountShareListing) error {
	if s == nil || s.repo == nil || apiKey == nil || listing == nil {
		return ErrServiceUnavailable
	}
	if apiKey.GroupID == nil || *apiKey.GroupID <= 0 {
		return ErrAccountShareAPIKeyMustUseModeGroup
	}
	platform := strings.ToLower(strings.TrimSpace(listing.Platform))
	if platform == "" {
		return ErrAccountShareAPIKeyMustUseModeGroup
	}
	modeGroup, err := s.repo.GetModeGroup(ctx, platform)
	if err != nil {
		return err
	}
	if modeGroup == nil || modeGroup.ID != *apiKey.GroupID {
		return ErrAccountShareAPIKeyMustUseModeGroup
	}
	return nil
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
	if value <= 0 || value > AccountShareModeMaxIdleTimeoutMinutes {
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
	now := time.Now().UTC()
	var afterRank int
	var lastErr error
	for attempt := 0; attempt < AccountShareModeQueueMaxItems; attempt++ {
		membership, listing, err := s.resolveActiveOrActivateQueuedBinding(ctx, userID, apiKeyID, groupID, afterRank, now)
		if err != nil {
			lastErr = err
			if errors.Is(err, ErrAccountShareListingNotFound) {
				break
			}
			return nil, nil, err
		}
		if membership == nil || listing == nil {
			lastErr = ErrAccountShareModeGroupUnbound
			break
		}
		if accountShareListingAccountUnavailableAt(listing, now) {
			afterRank = membership.QueueRank
			result, err := s.suspendMembershipForDispatchFailure(ctx, membership, now)
			if err != nil {
				return nil, nil, err
			}
			s.invalidateSeatBillingCaches(result)
			continue
		}
		ended, err := s.endIdleMembershipForRequest(ctx, membership, now)
		if err != nil {
			return nil, nil, err
		}
		if ended {
			afterRank = membership.QueueRank
			continue
		}
		if err := s.touchMembershipLastRequest(ctx, membership.ID, now); err != nil {
			return nil, nil, err
		}
		if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
			requestCtx.state.set(userID, apiKeyID, groupID, membership, listing, nil)
		}
		return membership, listing, nil
	}
	if lastErr == nil || errors.Is(lastErr, ErrAccountShareListingNotFound) {
		lastErr = ErrAccountShareModeGroupUnbound
	}
	if requestCtx, ok := AccountShareModeRequestFromContext(ctx); ok && requestCtx.state != nil {
		requestCtx.state.set(userID, apiKeyID, groupID, nil, nil, lastErr)
	}
	return nil, nil, lastErr
}

func (s *AccountShareModeService) resolveActiveOrActivateQueuedBinding(ctx context.Context, userID, apiKeyID, groupID int64, afterRank int, now time.Time) (*AccountShareMembership, *AccountShareListing, error) {
	membership, listing, err := s.repo.GetActiveMembershipForRequest(ctx, userID, apiKeyID, groupID)
	if err == nil && membership != nil && listing != nil {
		return membership, listing, nil
	}
	if err != nil && !errors.Is(err, ErrAccountShareListingNotFound) {
		return nil, nil, err
	}
	catchupResult, catchupErr := s.repo.ProcessSeatBillingForRequest(ctx, now, userID, apiKeyID)
	if catchupErr != nil {
		return nil, nil, catchupErr
	}
	s.invalidateSeatBillingCaches(catchupResult)
	membership, listing, err = s.repo.GetActiveMembershipForRequest(ctx, userID, apiKeyID, groupID)
	if err == nil && membership != nil && listing != nil {
		return membership, listing, nil
	}
	if err != nil && !errors.Is(err, ErrAccountShareListingNotFound) {
		return nil, nil, err
	}
	if _, err := s.processIdleMemberships(ctx, now, AccountShareIdleMembershipFilter{
		ConsumerUserID: userID,
		APIKeyID:       apiKeyID,
	}, AccountShareModeSeatBillingBatchSize); err != nil {
		return nil, nil, err
	}
	membership, listing, err = s.repo.GetActiveMembershipForRequest(ctx, userID, apiKeyID, groupID)
	if err == nil && membership != nil && listing != nil {
		return membership, listing, nil
	}
	if err != nil && !errors.Is(err, ErrAccountShareListingNotFound) {
		return nil, nil, err
	}
	membership, listing, err = s.repo.ActivateNextQueuedMembershipForRequest(ctx, userID, apiKeyID, groupID, afterRank, now)
	if err != nil {
		return nil, nil, err
	}
	return membership, listing, nil
}

func (s *AccountShareModeService) deferMembershipForDispatchRetry(ctx context.Context, requestCtx AccountShareModeRequestContext, membership *AccountShareMembership, now time.Time) error {
	if s == nil || membership == nil || membership.ID <= 0 {
		return nil
	}
	result, err := s.suspendMembershipForDispatchFailure(ctx, membership, now)
	if err != nil {
		return err
	}
	s.invalidateSeatBillingCaches(result)
	if requestCtx.state != nil {
		requestCtx.state.clear()
	}
	return nil
}

func (s *AccountShareModeService) suspendMembershipForDispatchFailure(ctx context.Context, membership *AccountShareMembership, now time.Time) (*AccountShareSeatBillingResult, error) {
	if s == nil || s.repo == nil || membership == nil || membership.ID <= 0 {
		return &AccountShareSeatBillingResult{}, nil
	}
	suspended, err := s.repo.SuspendMembershipForDispatchFailure(ctx, membership.ID, now, now.Add(AccountShareModeDispatchCooldown))
	if err != nil {
		return nil, err
	}
	if suspended == nil {
		return &AccountShareSeatBillingResult{}, nil
	}
	return &AccountShareSeatBillingResult{
		DebitUserIDs:         []int64{suspended.ConsumerUserID},
		CreditUserIDs:        []int64{suspended.OwnerUserID},
		EndedConsumerUserIDs: []int64{suspended.ConsumerUserID},
	}, nil
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
	if listing.AnthropicQuotaProtectionReason != nil && strings.TrimSpace(*listing.AnthropicQuotaProtectionReason) != "" {
		return listing.AnthropicQuotaProtectionResetAt == nil || now.Before(*listing.AnthropicQuotaProtectionResetAt)
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
	platform = normalizeAccountShareModePolicyPlatform(platform)
	if s == nil || s.repo == nil {
		return &AccountShareModePolicy{
			Platform:           platform,
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

func DefaultAccountShareModeAllowedModelsForPlatform(platform string) []string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformAnthropic:
		return append([]string(nil), accountShareModeAnthropicDefaultAllowedModels...)
	default:
		return DefaultAccountShareModeAllowedModels()
	}
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
	return normalizeAllowedModelsOrDefaultForPlatform(PlatformOpenAI, models)
}

func normalizeAllowedModelsOrDefaultForPlatform(platform string, models []string) []string {
	normalized := normalizeAllowedModels(models)
	if len(normalized) > 0 {
		return normalized
	}
	return DefaultAccountShareModeAllowedModelsForPlatform(platform)
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
	return AccountShareModePolicyPlatformUnified
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

func isValidAnthropicLimitPercent(value float64) bool {
	return value >= AnthropicQuotaMinLimitPercent && value <= AnthropicQuotaMaxLimitPercent && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func normalizeAnthropicLimitPercent(value float64) float64 {
	if value <= 0 {
		return AnthropicQuotaDefaultLimitPercent
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
	platform := normalizeAccountShareListingPlatform(filters.Platform)
	seatLimit := filters.SeatLimit
	if seatLimit < AccountShareModeMinSeats || seatLimit > AccountShareModeMaxSeats {
		seatLimit = 0
	}
	seatLimits := NormalizeAccountShareListingSeatLimits(filters.SeatLimits)
	if seatLimit > 0 && len(seatLimits) == 0 {
		seatLimits = []int{seatLimit}
	}
	status := strings.ToLower(strings.TrimSpace(filters.Status))
	switch status {
	case AccountShareListingStatusActive, AccountShareListingStatusPaused, AccountShareListingStatusDisabled, "all":
	default:
		status = ""
	}
	accountLevel := normalizeAccountShareListingFilterLevel(filters.AccountLevel)
	if platform != "" && platform != PlatformOpenAI {
		accountLevel = ""
	}
	featureTags := NormalizeAccountShareListingFeatureTags(filters.FeatureTags)
	if platform != "" && platform != PlatformOpenAI {
		featureTags = filterAccountShareListingFeatureTagsForPlatform(platform, featureTags)
	}
	sortBy := NormalizeAccountShareListingSortBy(filters.SortBy)
	sortOrder := NormalizeAccountShareListingSortOrder(filters.SortOrder)
	if sortBy == "" {
		sortOrder = ""
	}
	sorts := NormalizeAccountShareListingSorts(filters.Sorts)
	if len(sorts) == 0 && sortBy != "" && sortOrder != "" {
		sorts = []AccountShareListingSortCriterion{{SortBy: sortBy, SortOrder: sortOrder}}
	}
	if len(sorts) > 0 {
		sortBy = sorts[0].SortBy
		sortOrder = sorts[0].SortOrder
	}
	return AccountShareListingFilters{
		Tab:           tab,
		Platform:      platform,
		SeatLimit:     seatLimit,
		SeatLimits:    seatLimits,
		Search:        strings.TrimSpace(filters.Search),
		Status:        status,
		AvailableOnly: filters.AvailableOnly,
		Models:        normalizeAllowedModels(filters.Models),
		AccountLevel:  accountLevel,
		OwnerUserID:   filters.OwnerUserID,
		FeatureTags:   featureTags,
		SortBy:        sortBy,
		SortOrder:     sortOrder,
		Sorts:         sorts,
		ViewerIsAdmin: filters.ViewerIsAdmin,
		SkipTotal:     filters.SkipTotal,
	}
}

func normalizeAccountShareListingPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformOpenAI:
		return PlatformOpenAI
	case PlatformAnthropic:
		return PlatformAnthropic
	default:
		return ""
	}
}

func normalizeAccountShareSpendRange(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", AccountShareSpendRangeCurrentMembership:
		return AccountShareSpendRangeCurrentMembership, nil
	case AccountShareSpendRangeToday:
		return AccountShareSpendRangeToday, nil
	case AccountShareSpendRangeSevenDays:
		return AccountShareSpendRangeSevenDays, nil
	default:
		return "", ErrAccountShareSpendInvalidRange
	}
}

func normalizeAccountShareRecommendationUsageProfileInput(input AccountShareRecommendationUsageProfileInput) (AccountShareRecommendationUsageProfileInput, error) {
	platform := normalizeAccountShareListingPlatform(input.Platform)
	if platform == "" {
		return AccountShareRecommendationUsageProfileInput{}, accountShareRecommendationInvalidField("platform", "请选择账号平台")
	}
	days := input.Days
	if days <= 0 {
		days = AccountShareRecommendationUsageProfileDays
	}
	if days > AccountShareRecommendationUsageProfileMaxDays {
		return AccountShareRecommendationUsageProfileInput{}, accountShareRecommendationInvalidField("days", fmt.Sprintf("历史均值最多只能查询 %d 天", AccountShareRecommendationUsageProfileMaxDays))
	}
	return AccountShareRecommendationUsageProfileInput{
		Platform: platform,
		Model:    strings.TrimSpace(input.Model),
		Days:     days,
	}, nil
}

func normalizeAccountShareRecommendationInput(input AccountShareRecommendationInput) (AccountShareRecommendationInput, error) {
	platform := normalizeAccountShareListingPlatform(input.Platform)
	if platform == "" {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("platform", "请选择账号平台")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("model", "请选择需要测算的模型")
	}
	if input.APIKeyID <= 0 {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("api_key_id", "API Key 无效")
	}
	if input.RequestCount <= 0 || input.RequestCount > AccountShareRecommendationMaxRequests {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("request_count", fmt.Sprintf("请求次数必须在 1-%d 之间", AccountShareRecommendationMaxRequests))
	}
	if input.ActiveHours <= 0 || input.ActiveHours > AccountShareRecommendationMaxActiveHours || math.IsNaN(input.ActiveHours) || math.IsInf(input.ActiveHours, 0) {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("active_hours", fmt.Sprintf("使用时长必须在 1-%d 小时之间", AccountShareRecommendationMaxActiveHours))
	}
	if err := validateRecommendationTokenUnit("input_tokens_per_request", input.InputTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if err := validateRecommendationTokenUnit("output_tokens_per_request", input.OutputTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if err := validateRecommendationTokenUnit("cache_creation_tokens_per_request", input.CacheCreationTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if err := validateRecommendationTokenUnit("cache_read_tokens_per_request", input.CacheReadTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if err := validateRecommendationTokenUnit("image_input_tokens_per_request", input.ImageInputTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if err := validateRecommendationTokenUnit("image_cache_read_tokens_per_request", input.ImageCacheReadTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if err := validateRecommendationTokenUnit("image_output_tokens_per_request", input.ImageOutputTokensPerRequest); err != nil {
		return AccountShareRecommendationInput{}, err
	}
	if len(input.SizeTier) > 40 {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("size_tier", "规格层级过长")
	}
	if len(input.ServiceTier) > 40 {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("service_tier", "服务档位过长")
	}
	if input.Limit <= 0 {
		input.Limit = AccountShareRecommendationDefaultLimit
	}
	if input.Limit > AccountShareRecommendationMaxLimit {
		input.Limit = AccountShareRecommendationMaxLimit
	}
	input.Platform = platform
	input.Model = model
	input.SizeTier = strings.TrimSpace(input.SizeTier)
	input.ServiceTier = strings.TrimSpace(input.ServiceTier)
	if !recommendationTokenTotalsFit(input) {
		return AccountShareRecommendationInput{}, accountShareRecommendationInvalidField("request_count", "请求次数与单次 token 组合过大，无法可靠测算")
	}
	return input, nil
}

func validateRecommendationTokenUnit(field string, value int) error {
	if value < 0 || value > AccountShareRecommendationMaxTokensPerUnit {
		return accountShareRecommendationInvalidField(field, fmt.Sprintf("单次 token 必须在 0-%d 之间", AccountShareRecommendationMaxTokensPerUnit))
	}
	return nil
}

func recommendationTokenTotalsFit(input AccountShareRecommendationInput) bool {
	values := []int{
		input.InputTokensPerRequest,
		input.OutputTokensPerRequest,
		input.CacheCreationTokensPerRequest,
		input.CacheReadTokensPerRequest,
		input.ImageInputTokensPerRequest,
		input.ImageCacheReadTokensPerRequest,
		input.ImageOutputTokensPerRequest,
	}
	for _, value := range values {
		if !recommendationMultiplyFitsInt(value, input.RequestCount) {
			return false
		}
	}
	if !recommendationAddFitsInt(recommendationMultiplyToken(input.OutputTokensPerRequest, input.RequestCount), recommendationMultiplyToken(input.ImageOutputTokensPerRequest, input.RequestCount)) {
		return false
	}
	return recommendationAddFitsInt(recommendationMultiplyToken(input.CacheReadTokensPerRequest, input.RequestCount), recommendationMultiplyToken(input.ImageCacheReadTokensPerRequest, input.RequestCount))
}

func recommendationMultiplyFitsInt(value, multiplier int) bool {
	if value <= 0 || multiplier <= 0 {
		return true
	}
	return value <= int(^uint(0)>>1)/multiplier
}

func recommendationAddFitsInt(left, right int) bool {
	if right <= 0 {
		return true
	}
	return left <= int(^uint(0)>>1)-right
}

func recommendationMultiplyToken(value, requestCount int) int {
	if value <= 0 || requestCount <= 0 {
		return 0
	}
	return value * requestCount
}

func accountShareRecommendationInvalidField(field, message string) error {
	return ErrAccountShareRecommendationInvalid.WithMetadata(map[string]string{
		"field":   field,
		"message": message,
	})
}

func buildAccountShareRecommendationTokens(input AccountShareRecommendationInput) UsageTokens {
	imageCacheReadTokens := recommendationMultiplyToken(input.ImageCacheReadTokensPerRequest, input.RequestCount)
	textCacheReadTokens := recommendationMultiplyToken(input.CacheReadTokensPerRequest, input.RequestCount)
	imageOutputTokens := recommendationMultiplyToken(input.ImageOutputTokensPerRequest, input.RequestCount)
	textOutputTokens := recommendationMultiplyToken(input.OutputTokensPerRequest, input.RequestCount)
	return UsageTokens{
		InputTokens:          recommendationMultiplyToken(input.InputTokensPerRequest, input.RequestCount),
		ImageInputTokens:     recommendationMultiplyToken(input.ImageInputTokensPerRequest, input.RequestCount),
		OutputTokens:         textOutputTokens + imageOutputTokens,
		CacheCreationTokens:  recommendationMultiplyToken(input.CacheCreationTokensPerRequest, input.RequestCount),
		CacheReadTokens:      textCacheReadTokens + imageCacheReadTokens,
		ImageCacheReadTokens: imageCacheReadTokens,
		ImageOutputTokens:    imageOutputTokens,
	}
}

func buildAccountShareRecommendationUsage(input AccountShareRecommendationInput) AccountShareRecommendationUsage {
	return AccountShareRecommendationUsage{
		Platform:             input.Platform,
		Model:                input.Model,
		APIKeyID:             input.APIKeyID,
		RequestCount:         input.RequestCount,
		ActiveHours:          input.ActiveHours,
		InputTokens:          recommendationMultiplyToken(input.InputTokensPerRequest, input.RequestCount),
		OutputTokens:         recommendationMultiplyToken(input.OutputTokensPerRequest, input.RequestCount),
		CacheCreationTokens:  recommendationMultiplyToken(input.CacheCreationTokensPerRequest, input.RequestCount),
		CacheReadTokens:      recommendationMultiplyToken(input.CacheReadTokensPerRequest, input.RequestCount),
		ImageInputTokens:     recommendationMultiplyToken(input.ImageInputTokensPerRequest, input.RequestCount),
		ImageCacheReadTokens: recommendationMultiplyToken(input.ImageCacheReadTokensPerRequest, input.RequestCount),
		ImageOutputTokens:    recommendationMultiplyToken(input.ImageOutputTokensPerRequest, input.RequestCount),
		SizeTier:             input.SizeTier,
		ServiceTier:          input.ServiceTier,
		Limit:                input.Limit,
	}
}

func buildAccountShareRecommendationUsageProfile(input AccountShareRecommendationUsageProfileInput, startTime, endTime time.Time, stats *AccountShareRecommendationUsageProfileStats) *AccountShareRecommendationUsageProfile {
	requestCount, cappedRequests := accountShareRecommendationProfileCeilAverage(stats.TotalRequests, input.Days, AccountShareRecommendationMaxRequests)
	activeHours := 0.0
	if stats.TotalRequests > 0 && input.Days > 0 {
		activeHours = math.Ceil(float64(stats.ActiveHourBuckets) / float64(input.Days))
		if activeHours < 1 {
			activeHours = 1
		}
	}
	if activeHours > AccountShareRecommendationMaxActiveHours {
		activeHours = AccountShareRecommendationMaxActiveHours
	}
	inputTokens, cappedInput := accountShareRecommendationProfileCeilPerRequest(stats.TotalInputTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	outputTokens, cappedOutput := accountShareRecommendationProfileCeilPerRequest(stats.TotalOutputTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	cacheCreationTokens, cappedCacheCreation := accountShareRecommendationProfileCeilPerRequest(stats.TotalCacheCreationTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	cacheReadTokens, cappedCacheRead := accountShareRecommendationProfileCeilPerRequest(stats.TotalCacheReadTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	imageOutputTokens, cappedImageOutput := accountShareRecommendationProfileCeilPerRequest(stats.TotalImageOutputTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)

	return &AccountShareRecommendationUsageProfile{
		Platform:                      input.Platform,
		Model:                         input.Model,
		Days:                          input.Days,
		StartTime:                     startTime,
		EndTime:                       endTime,
		HasHistory:                    stats.TotalRequests > 0,
		ModelMatched:                  stats.ModelMatched,
		UsedModelFallback:             input.Model != "" && stats.TotalRequests > 0 && !stats.ModelMatched,
		Capped:                        cappedRequests || cappedInput || cappedOutput || cappedCacheCreation || cappedCacheRead || cappedImageOutput,
		TotalRequests:                 stats.TotalRequests,
		ActiveHourBuckets:             stats.ActiveHourBuckets,
		RequestCount:                  requestCount,
		ActiveHours:                   activeHours,
		InputTokensPerRequest:         inputTokens,
		OutputTokensPerRequest:        outputTokens,
		CacheCreationTokensPerRequest: cacheCreationTokens,
		CacheReadTokensPerRequest:     cacheReadTokens,
		ImageOutputTokensPerRequest:   imageOutputTokens,
	}
}

func accountShareRecommendationProfileCeilAverage(total int64, divisor int, max int) (int, bool) {
	if total <= 0 || divisor <= 0 {
		return 0, false
	}
	value := math.Ceil(float64(total) / float64(divisor))
	if value > float64(max) {
		return max, true
	}
	return int(value), false
}

func accountShareRecommendationProfileCeilPerRequest(total, requests int64, max int) (int, bool) {
	if total <= 0 || requests <= 0 {
		return 0, false
	}
	value := math.Ceil(float64(total) / float64(requests))
	if value > float64(max) {
		return max, true
	}
	return int(value), false
}

func accountShareRecommendationDurationMs(activeHours float64) int {
	if activeHours <= 0 {
		return 0
	}
	ms := activeHours * float64(time.Hour.Milliseconds())
	if ms > float64(int(^uint(0)>>1)) {
		return int(^uint(0) >> 1)
	}
	return int(math.Round(ms))
}

func accountShareListingSupportsRecommendationModel(listing AccountShareListing, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, pattern := range normalizeAllowedModels(listing.AllowedModels) {
		if matchModelPattern(pattern, model) {
			return true
		}
	}
	return false
}

func buildAccountShareRecommendationMessages(listing AccountShareListing, estimate AccountShareRecommendationEstimate) ([]string, []string, []string) {
	tags := make([]string, 0, 5)
	reasons := make([]string, 0, 5)
	warnings := make([]string, 0, 3)
	remainingSeats := listing.SeatLimit - listing.ActiveSeats
	if estimate.OwnerSelfUse {
		tags = append(tags, "自用低倍率")
		reasons = append(reasons, "这是你自己上架的账号，按自用倍率测算且不收小时费")
	}
	if estimate.HourlyNetCost <= 0 {
		tags = append(tags, "小时费低")
		if estimate.WaiverEligible {
			reasons = append(reasons, "预计请求消费已达到低消门槛，小时费可被抵免")
		} else {
			reasons = append(reasons, "该账号当前小时费为 0 或自用不收小时费")
		}
	}
	if estimate.EffectiveRateMultiplier <= 1 {
		tags = append(tags, "倍率友好")
		reasons = append(reasons, "账号倍率不高于 1x，请求消费更容易控制")
	}
	if remainingSeats > 0 {
		tags = append(tags, "有空位")
		reasons = append(reasons, fmt.Sprintf("当前剩余 %d 个可用席位", remainingSeats))
	}
	if listing.RatingCount > 0 && listing.RatingAvg >= 8 {
		tags = append(tags, "评分高")
		reasons = append(reasons, fmt.Sprintf("已有 %d 条评分，平均 %.1f", listing.RatingCount, listing.RatingAvg))
	}
	if listing.PerUserConcurrency >= AccountShareModeDefaultPerUserConcurrency {
		tags = append(tags, "并发稳定")
		reasons = append(reasons, fmt.Sprintf("单用户并发上限 %d", listing.PerUserConcurrency))
	}
	if !estimate.OwnerSelfUse && estimate.EffectiveRateMultiplier > 2 {
		warnings = append(warnings, fmt.Sprintf("倍率 %.2fx 偏高，请求消费会被明显放大", estimate.EffectiveRateMultiplier))
	}
	if !estimate.OwnerSelfUse && estimate.EffectiveHourlyRate > 0 && estimate.HourlyNetCost > estimate.RequestCost {
		warnings = append(warnings, "当前测算中小时费高于请求消费，长时间占用需要谨慎")
	}
	if remainingSeats <= 0 && !estimate.OwnerSelfUse {
		warnings = append(warnings, "当前没有空闲席位，可能需要排队等待")
	}
	return tags, reasons, warnings
}

func accountShareRecommendationScore(listing AccountShareListing, estimate AccountShareRecommendationEstimate, warnings []string) float64 {
	remainingSeats := listing.SeatLimit - listing.ActiveSeats
	if remainingSeats < 0 {
		remainingSeats = 0
	}
	totalCostPenalty := estimate.TotalCost * 100
	if totalCostPenalty > 500 {
		totalCostPenalty = 500
	}
	score := 1000 - totalCostPenalty
	score += float64(remainingSeats) * 18
	score += math.Min(float64(listing.PerUserConcurrency), 20) * 4
	score += math.Min(float64(listing.AccountConcurrency-listing.CurrentConcurrency), 50)
	if listing.RatingCount > 0 {
		score += math.Min(listing.RatingAvg, 10) * 8
		score += math.Min(float64(listing.RatingCount), 20)
	}
	if estimate.WaiverEligible {
		score += 20
	}
	if estimate.EffectiveHourlyRate <= 0 {
		score += 18
	}
	if estimate.EffectiveRateMultiplier <= 1 {
		score += 16
	}
	if estimate.OwnerSelfUse {
		score += 50
	}
	score -= float64(len(warnings)) * 25
	if score < 0 {
		return 0
	}
	return math.Round(score*100) / 100
}

func accountShareRecommendationCandidateRanksBefore(left, right AccountShareRecommendationCandidate) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.Estimate.TotalCost != right.Estimate.TotalCost {
		return left.Estimate.TotalCost < right.Estimate.TotalCost
	}
	if left.Listing.RatingAvg != right.Listing.RatingAvg {
		return left.Listing.RatingAvg > right.Listing.RatingAvg
	}
	return left.Listing.ID < right.Listing.ID
}

func accountShareRecommendationCandidateDedupeKey(listing AccountShareListing) string {
	if listing.AccountIdentityID != nil && *listing.AccountIdentityID > 0 {
		return fmt.Sprintf("identity:%d", *listing.AccountIdentityID)
	}
	if listing.AccountID > 0 {
		return fmt.Sprintf("account:%d", listing.AccountID)
	}
	return fmt.Sprintf("listing:%d", listing.ID)
}

func prependUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append([]string{value}, values...)
}

func NormalizeAccountShareListingSeatLimits(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value < AccountShareModeMinSeats || value > AccountShareModeMaxSeats {
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

func filterAccountShareListingFeatureTagsForPlatform(platform string, tags []string) []string {
	if platform == PlatformOpenAI || len(tags) == 0 {
		return tags
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		switch tag {
		case AccountShareListingFeatureImageGeneration, AccountShareListingFeatureCodexCLIOnly, AccountShareListingFeatureNonCodexCLIOnly:
			continue
		default:
			out = append(out, tag)
		}
	}
	return out
}

func NormalizeAccountShareListingFeatureTag(tag string) string {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case AccountShareListingFeatureHourlyFeeWaiver:
		return AccountShareListingFeatureHourlyFeeWaiver
	case AccountShareListingFeatureImageGeneration:
		return AccountShareListingFeatureImageGeneration
	case AccountShareListingFeatureNoHourlyFee:
		return AccountShareListingFeatureNoHourlyFee
	case AccountShareListingFeatureCodexCLIOnly:
		return AccountShareListingFeatureCodexCLIOnly
	case AccountShareListingFeatureNonCodexCLIOnly:
		return AccountShareListingFeatureNonCodexCLIOnly
	case AccountShareListingFeatureAvailable:
		return AccountShareListingFeatureAvailable
	default:
		return ""
	}
}

func NormalizeAccountShareListingFeatureTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized := NormalizeAccountShareListingFeatureTag(tag)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func NormalizeAccountShareListingSortBy(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "", AccountShareListingSortDefault:
		return ""
	case AccountShareListingSortAccountConcurrency:
		return AccountShareListingSortAccountConcurrency
	case AccountShareListingSortPerUserConcurrency:
		return AccountShareListingSortPerUserConcurrency
	case AccountShareListingSortMinBalanceRequired:
		return AccountShareListingSortMinBalanceRequired
	case AccountShareListingSortHourlyRate:
		return AccountShareListingSortHourlyRate
	case AccountShareListingSortHourlyFeeWaiver:
		return AccountShareListingSortHourlyFeeWaiver
	case AccountShareListingSortRateMultiplier:
		return AccountShareListingSortRateMultiplier
	case AccountShareListingSortRemainingSeats:
		return AccountShareListingSortRemainingSeats
	case AccountShareListingSortRating:
		return AccountShareListingSortRating
	case AccountShareListingSortUpdatedAt:
		return AccountShareListingSortUpdatedAt
	default:
		return ""
	}
}

func NormalizeAccountShareListingSortOrder(sortOrder string) string {
	switch strings.ToLower(strings.TrimSpace(sortOrder)) {
	case AccountShareListingSortOrderAsc:
		return AccountShareListingSortOrderAsc
	case AccountShareListingSortOrderDesc:
		return AccountShareListingSortOrderDesc
	default:
		return ""
	}
}

func NormalizeAccountShareListingSorts(sorts []AccountShareListingSortCriterion) []AccountShareListingSortCriterion {
	seen := make(map[string]struct{}, len(sorts))
	out := make([]AccountShareListingSortCriterion, 0, len(sorts))
	for _, sort := range sorts {
		sortBy := NormalizeAccountShareListingSortBy(sort.SortBy)
		sortOrder := NormalizeAccountShareListingSortOrder(sort.SortOrder)
		if sortBy == "" || sortOrder == "" {
			continue
		}
		if _, ok := seen[sortBy]; ok {
			continue
		}
		seen[sortBy] = struct{}{}
		out = append(out, AccountShareListingSortCriterion{SortBy: sortBy, SortOrder: sortOrder})
	}
	return out
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
	if IsAccountShareModeOwnerSelfUse(membership, listing) {
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

func IsAccountShareModeOwnerSelfUse(membership *AccountShareMembership, listing *AccountShareListing) bool {
	return membership != nil &&
		listing != nil &&
		membership.ConsumerUserID > 0 &&
		listing.OwnerUserID > 0 &&
		membership.ConsumerUserID == listing.OwnerUserID
}

func (s *AccountShareModeService) String() string {
	if s == nil {
		return "AccountShareModeService<nil>"
	}
	return fmt.Sprintf("AccountShareModeService<repo=%t>", s.repo != nil)
}
