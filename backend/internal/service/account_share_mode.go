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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
)

const (
	AccountShareModeGroupPlatformOpenAI    = PlatformOpenAI
	AccountShareModeGroupPlatformAnthropic = PlatformAnthropic

	AccountShareListingStatusActive = "active"
	AccountShareListingStatusPaused = "paused"
	// AccountShareListingStatusDisabled remains readable and writable during
	// the expand observation window so the previous binary stays rollback-safe.
	AccountShareListingStatusDisabled = "disabled"

	AccountShareMembershipStatusActive = "active"
	AccountShareMembershipStatusQueued = "queued"
	AccountShareMembershipStatusEnded  = "ended"

	AccountShareSnapshotQualityExact             = "exact"
	AccountShareSnapshotQualityBackfilledCurrent = "backfilled_current"
	AccountShareSnapshotQualityUnknown           = "unknown"

	AccountShareModeDefaultMinBalance               = 1.0
	AccountShareModeDefaultCodexLimitPercent        = CodexQuotaDefaultLimitPercent
	AccountShareModeMinSeats                        = 1
	AccountShareModeMaxSeats                        = 30
	AccountShareModeDefaultPerUserConcurrency       = 5
	AccountShareModeMaxPerUserConcurrency           = 50
	AccountShareModeDefaultAccountConcurrency       = 20
	AccountShareModeMaxAccountConcurrency           = 50
	AccountShareModeSeatPrepayDuration              = time.Minute
	AccountShareModeSeatWaiverWindowMax             = time.Hour
	AccountShareModeSeatWaiverSettlementGrace       = 15 * time.Minute
	AccountShareModeSeatWaiverCompensationDelay     = 10 * time.Minute
	AccountShareModeSeatWaiverCompensationInterval  = 10 * time.Minute
	AccountShareModeSeatWaiverCompensationTimeout   = 2 * time.Minute
	AccountShareModeSeatWaiverCompensationBatchSize = 50
	// 单轮软预算:批间检查,超过即收口,必须小于 CompensationTimeout,
	// 留出最后一批评估事务的余量。
	AccountShareModeSeatWaiverCompensationRoundBudget = 80 * time.Second
	// 迟到 usage 反查的回看窗口:正常迟到落账是分钟级,72h 纯粹是
	// worker 连续故障的容忍余量(覆盖整个周末档)。超过该窗口的漏评
	// 走 playbook 把 waiver_evaluated_at 置 NULL 由积压分支兜底。
	AccountShareModeSeatWaiverLateUsageLookback = 72 * time.Hour
	// 由"迟到条目创建时间"推导"结算窗口终点下界"时的松弛量,
	// 必须大于任何单请求的时长上限(现实上限 10 分钟)。
	AccountShareModeSeatWaiverLateUsageSlack      = 24 * time.Hour
	AccountShareModeSeatBillingInterval           = 15 * time.Second
	AccountShareModeSeatBillingBatchSize          = 100
	// 孤儿 binding 清扫频率：低优先兜底 worker，处理历史遗留脏数据即可，不必高频。
	AccountShareModeOrphanBindingCleanupInterval = 10 * time.Minute
	// ending 结算超时兜底阈值：Redis lease 持续不可用时，超过该时长强制结算。
	// 比在途 slot 的 TTL（默认 30 分钟）短，用户不必等满 slot 回收。
	AccountShareModeEndSettlementForceTimeout = 10 * time.Minute
	AccountShareModeJoinIntentTTL             = 2 * time.Minute
	AccountShareModeEndMembershipTokenTTL         = 2 * time.Minute
	AccountShareModeMaxIdleTimeoutMinutes         = 10080
	AccountShareModeLastRequestTouchInterval      = 30 * time.Second
	AccountShareModeRequestHeartbeatInterval      = 15 * time.Second
	AccountShareModeMembershipTouchTimeout        = 5 * time.Second
	AccountShareModeEditSessionTTL                = 10 * time.Minute
	AccountShareModeQueueMaxItems                 = 5
	AccountShareModeRoomQueueMinimum              = 20
	AccountShareModeRoomQueueMaximum              = 100
	AccountShareModeRoomQueuePerSeat              = 10
	// 排队成员的保留期限：入队/降级重排队后经过该时长仍未被激活则自动释放。
	// 前端 queueIdleTimeoutSummary 的「预约最长保留 2 小时」文案与此对齐，改这里要同步改前端。
	AccountShareModeQueueExpiryDuration = 2 * time.Hour
	AccountShareModeDispatchCooldown              = 5 * time.Minute
	AccountShareModeConnectivityTestTimeout       = 90 * time.Second
	AccountShareModeImageConnectivityTestTimeout  = 10 * time.Minute
	AccountShareRecommendationDefaultLimit        = 5
	AccountShareRecommendationMaxLimit            = 10
	AccountShareRecommendationMaxRequests         = 1000000
	AccountShareRecommendationMaxActiveHours      = 720
	AccountShareRecommendationMaxTokensPerUnit    = 2000000
	AccountShareRecommendationPageSize            = 1000
	AccountShareRecommendationUsageProfileDays    = 3
	AccountShareRecommendationUsageProfileMaxDays = 7
	AccountShareRoomNameMaxRunes                  = 100
	AccountShareAccountSampleScopeRepresentative  = "representative"
	AccountShareQuotaSummaryScopeRoom             = "room"
	AccountShareModeListingTabUsing               = "using"
	AccountShareModeListingTabHistory             = "history"
	AccountShareModeListingTabAll                 = "all"
	AccountShareModeListingTabMine                = "mine"
	AccountShareModeListingTabArchive             = "archive"
	AccountExternalPlacementPrivate               = "private"
	AccountExternalPlacementPublicPool            = "public_pool"
	AccountExternalPlacementRoom                  = "room"
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
	AccountShareWaiverProgressStatusInProgress    = "in_progress"
	AccountShareWaiverProgressStatusMet           = "met"
	AccountShareSpendRangeToday                   = "today"
	AccountShareSpendRangeCurrentMembership       = "current_membership"
	AccountShareSpendRangeSevenDays               = "7d"
	AccountShareMembershipEndReasonManual         = "manual"
	AccountShareMembershipEndReasonIdleTimeout    = "idle_timeout"
	AccountShareMembershipEndReasonPrepay         = "prepay_insufficient"
	AccountShareMembershipEndReasonUnavailable    = "account_unavailable"
	AccountShareMembershipEndReasonQueueExpired   = "queue_expired"
	AccountShareMembershipEndReasonRoomDraining   = "room_draining"
	AccountShareReviewCommentStatusNone           = "none"
	AccountShareReviewCommentStatusPending        = "pending"
	AccountShareReviewCommentStatusApproved       = "approved"
	AccountShareReviewCommentStatusRejected       = "rejected"
	AccountShareReviewCommentStatusFailed         = "failed"
	AccountShareReviewMaxCommentRunes             = 1000
	AccountShareReviewModerationInterval          = 15 * time.Second
	AccountShareReviewModerationBatchSize         = 20
	AccountShareReviewModerationMaxAttempts       = 5
	AccountShareRoomBatchMaxAccounts              = 1000
	accountShareSeatBillingTaskName               = "account_share_seat_billing"
	accountShareBillingIntentTaskName             = "account_share_billing_intents"
	accountShareSeatWaiverCompensationTaskName    = "account_share_seat_waiver_compensation"
	accountShareRoomLifecycleFinalizerTaskName    = "account_share_room_lifecycle_finalizer"
	accountShareRoomValidationTaskName            = "account_share_room_validation"
	accountShareOrphanBindingCleanupTaskName      = "account_share_orphan_binding_cleanup"
	accountShareReviewModerationTaskName          = "account_share_review_moderation"
	accountShareModeContextBindingMissingError    = "该分组未绑定账号"
	accountShareModeJoinIntentTokenAction         = "account_share_mode:join_listing:v1"
	accountShareModeEndMembershipTokenAction      = "account_share_mode:end_membership:v2"
)

var accountShareModeDefaultAllowedModels = []string{
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"codex-auto-review",
}

var accountShareModeAnthropicDefaultAllowedModels = []string{
	"claude-sonnet-5",
	"claude-sonnet-4-6",
	"claude-opus-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-fable-5",
	"claude-opus-4-6",
	"claude-haiku-4-5",
}

var (
	ErrAccountShareModeGroupUnbound             = infraerrors.New(http.StatusBadRequest, "ACCOUNT_SHARE_MODE_GROUP_UNBOUND", accountShareModeContextBindingMissingError)
	ErrAccountShareModeGroupUnavailable         = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE", "account share mode group is not configured")
	ErrAccountSharePrivateGroupUnavailable      = infraerrors.BadRequest("ACCOUNT_SHARE_PRIVATE_GROUP_UNAVAILABLE", "account owner private group is not configured")
	ErrAccountShareListingNotFound              = infraerrors.NotFound("ACCOUNT_SHARE_LISTING_NOT_FOUND", "account share listing not found")
	ErrAccountShareMembershipNotFound           = infraerrors.NotFound("ACCOUNT_SHARE_MEMBERSHIP_NOT_FOUND", "account share membership not found")
	ErrAccountShareBillingSnapshotMismatch      = infraerrors.InternalServer("ACCOUNT_SHARE_BILLING_SNAPSHOT_MISMATCH", "account share billing snapshot does not match the locked membership")
	ErrAccountShareRoomOwnerMismatch            = infraerrors.Forbidden("ACCOUNT_SHARE_ROOM_OWNER_MISMATCH", "account and room must belong to the same owner")
	ErrAccountShareRoomPlatformMismatch         = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_PLATFORM_MISMATCH", "account and room platforms do not match")
	ErrAccountShareRoomLevelMismatch            = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_LEVEL_MISMATCH", "all accounts in a room must have the same account level")
	ErrAccountShareRoomUnknownLevel             = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_UNKNOWN_LEVEL", "accounts with an unknown level cannot be added to a room")
	ErrAccountShareRoomAccountConfigUnsupported = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_ACCOUNT_CONFIG_UNSUPPORTED", "proxy and account concurrency must be edited on individual accounts")
	ErrAccountShareRoomModeRequired             = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_MODE_REQUIRED", "account must use the platform account mode before it can join a room")
	ErrAccountShareRoomAccountConflict          = infraerrors.Conflict("ACCOUNT_SHARE_ROOM_ACCOUNT_CONFLICT", "account already belongs to another room")
	ErrAccountShareRoomAccountAttached          = infraerrors.Conflict("ACCOUNT_SHARE_ROOM_ACCOUNT_ATTACHED", "account must leave its room before changing account mode")
	ErrAccountExternalPlacementInvalid          = infraerrors.BadRequest("ACCOUNT_EXTERNAL_PLACEMENT_INVALID", "invalid external placement target")
	ErrAccountExternalPlacementBusy             = infraerrors.Conflict("ACCOUNT_EXTERNAL_PLACEMENT_BUSY", "account has an in-flight room request; retry after it drains")
	ErrAccountExternalPlacementConflict         = infraerrors.Conflict("ACCOUNT_EXTERNAL_PLACEMENT_CONFLICT", "account already has a different external placement")
	ErrAccountExternalPlacementIdempotency      = infraerrors.Conflict("ACCOUNT_EXTERNAL_PLACEMENT_IDEMPOTENCY_CONFLICT", "idempotency key was already used for a different conversion")
	ErrAccountShareListingNotActive             = infraerrors.BadRequest("ACCOUNT_SHARE_LISTING_NOT_ACTIVE", "account share listing is not active")
	ErrAccountShareListingFull                  = infraerrors.BadRequest("ACCOUNT_SHARE_LISTING_FULL", "account share listing is full")
	ErrAccountShareOwnerCannotJoin              = infraerrors.BadRequest("ACCOUNT_SHARE_OWNER_CANNOT_JOIN", "owner cannot join own shared account")
	ErrAccountShareAlreadyUsing                 = infraerrors.Conflict("ACCOUNT_SHARE_ALREADY_USING", "user is already using an account share listing")
	ErrAccountShareAPIKeyAlreadyBound           = infraerrors.Conflict("ACCOUNT_SHARE_API_KEY_ALREADY_BOUND", "api key is already bound to an account share listing")
	ErrAccountShareQueueFull                    = infraerrors.Conflict("ACCOUNT_SHARE_QUEUE_FULL", "account share reservation queue is full")
	ErrAccountShareRoomQueueLimitExceeded       = infraerrors.Conflict("ACCOUNT_SHARE_ROOM_QUEUE_LIMIT_EXCEEDED", "account share room reservation queue is full")
	ErrAccountShareQueueInvalid                 = infraerrors.BadRequest("ACCOUNT_SHARE_QUEUE_INVALID", "account share reservation queue is invalid")
	ErrAccountShareAPIKeyMustUseModeGroup       = infraerrors.BadRequest("ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP", "api key must use account mode group")
	ErrAccountShareBalanceBelowMinimum          = infraerrors.Forbidden("ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM", "user balance is below account share minimum")
	ErrAccountSharePerUserConcurrencyExceeded   = infraerrors.TooManyRequests("ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED", "account share per-user concurrency exceeded")
	ErrAccountShareModeUnsupportedModel         = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL", "account share account does not support requested model")
	ErrAccountShareModeOpenAIOnly               = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_OPENAI_ONLY", "account share mode only supports OpenAI OAuth accounts")
	ErrAccountShareModeProxyRequired            = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_PROXY_REQUIRED", "proxy is required before account share OAuth login")
	ErrAccountShareModeAllowedModelsRequired    = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_MODELS_REQUIRED", "at least one allowed model is required")
	ErrAccountShareModeInvalidSeats             = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_SEATS", "seat_limit must be between 1 and 30")
	ErrAccountShareModeInvalidRateMultiplier    = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_RATE_MULTIPLIER", "rate_multiplier must be non-negative")
	ErrAccountShareModeInvalidConcurrency       = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_CONCURRENCY", "concurrency must be positive and no greater than 50")
	ErrAccountShareModeInvalidHourlyRate        = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_HOURLY_RATE", "hourly_rate must be non-negative")
	ErrAccountShareModeInvalidMinBalance        = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_MIN_BALANCE", "min_balance_required must be non-negative")
	ErrAccountShareModeInvalidWaiverMinimum     = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_WAIVER_MINIMUM", "hourly_fee_waiver_minimum must be non-negative")
	ErrAccountShareModePrepayInsufficient       = infraerrors.Forbidden("ACCOUNT_SHARE_MODE_PREPAY_INSUFFICIENT", "balance is insufficient for account share seat prepayment")
	ErrAccountShareAccountUnavailable           = infraerrors.Forbidden("ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE", "account share account is unavailable")
	ErrAccountShareModeInvalidName              = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_NAME", "account share room name must be between 1 and 100 characters and must not contain whitespace")
	ErrAccountShareModeDuplicateName            = infraerrors.Conflict("ACCOUNT_SHARE_MODE_DUPLICATE_NAME", "account share account name already exists")
	ErrAccountShareModeInvalidPolicyRatio       = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_POLICY_RATIO", "account share mode policy ratios must be between 0 and 1 and sum to at most 1")
	ErrAccountShareModeInvalidProxy             = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_PROXY", "invalid proxy configuration")
	ErrAccountShareModePublicPoolAccount        = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_PUBLIC_POOL_ACCOUNT", "public shared pool accounts cannot be used for account share mode")
	ErrAccountShareJoinIntentRequired           = infraerrors.BadRequest("ACCOUNT_SHARE_JOIN_INTENT_REQUIRED", "account share join intent is required")
	ErrAccountShareJoinIntentInvalid            = infraerrors.Forbidden("ACCOUNT_SHARE_JOIN_INTENT_INVALID", "account share join intent is invalid or expired")
	ErrAccountShareJoinIntentConsumed           = infraerrors.Conflict("ACCOUNT_SHARE_JOIN_INTENT_CONSUMED", "account share join intent has already been consumed")
	ErrAccountShareJoinTermsChanged             = infraerrors.Conflict("ACCOUNT_SHARE_JOIN_TERMS_CHANGED", "account share room terms changed; review the latest terms and try again")
	ErrAccountShareMembershipEnding             = infraerrors.Conflict("ACCOUNT_SHARE_MEMBERSHIP_ENDING", "the previous room membership is still completing exit settlement")
	ErrAccountShareQueueConfirmationRequired    = infraerrors.Conflict("ACCOUNT_SHARE_QUEUE_CONFIRMATION_REQUIRED", "joining this room requires explicit queue confirmation")
	ErrAccountShareEndTokenRequired             = infraerrors.BadRequest("ACCOUNT_SHARE_END_TOKEN_REQUIRED", "account share end confirmation token is required")
	ErrAccountShareEndTokenInvalid              = infraerrors.Forbidden("ACCOUNT_SHARE_END_TOKEN_INVALID", "account share end confirmation token is invalid or expired")
	ErrAccountShareEndStateConflict             = infraerrors.Conflict("ACCOUNT_SHARE_END_STATE_CONFLICT", "account share membership changed after end confirmation; refresh and try again")
	// ErrAccountShareBillingBindingUnavailable 绑定/条款快照不可用（原属已删除的 billing intent 体系，
	// 仍被绑定与条款校验路径使用）
	ErrAccountShareBillingBindingUnavailable   = errors.New("account share billing binding is no longer active")
	ErrAccountShareModeInvalidIdleTimeout      = infraerrors.BadRequest("ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT", "idle_timeout_minutes must be between 1 and 10080")
	ErrAccountShareListingInUse                = infraerrors.Conflict("ACCOUNT_SHARE_LISTING_IN_USE", "account share listing has active seats")
	ErrAccountShareListingEditing              = infraerrors.Conflict("ACCOUNT_SHARE_LISTING_EDITING", "account share listing is being edited")
	ErrAccountShareEditSessionRequired         = infraerrors.BadRequest("ACCOUNT_SHARE_EDIT_SESSION_REQUIRED", "account share edit session is required")
	ErrAccountShareEditSessionInvalid          = infraerrors.Conflict("ACCOUNT_SHARE_EDIT_SESSION_INVALID", "account share edit session is invalid or expired")
	ErrAccountShareExpectedVersionRequired     = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_EXPECTED_VERSION_REQUIRED", "expected_version is required")
	ErrAccountShareVersionConflict             = infraerrors.Conflict("ACCOUNT_SHARE_ROOM_VERSION_CONFLICT", "account share room version conflict")
	ErrAccountShareForceAdminRequired          = infraerrors.Forbidden("ACCOUNT_SHARE_ROOM_FORCE_ADMIN_REQUIRED", "only an administrator can force an account share room update")
	ErrAccountShareUpdateReasonRequired        = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_UPDATE_REASON_REQUIRED", "update reason is required")
	ErrAccountShareForceReasonRequired         = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_FORCE_REASON_REQUIRED", "force update reason is required")
	ErrAccountShareForceConfirmationRequired   = infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_FORCE_CONFIRMATION_REQUIRED", "force update confirmation is required")
	ErrAccountShareUpdateRequiresPaused        = infraerrors.Conflict("ACCOUNT_SHARE_ROOM_UPDATE_REQUIRES_PAUSED", "contract updates require an empty active or paused room with no active, queued, or ending memberships")
	ErrAccountShareConsumerProtectionViolation = infraerrors.Conflict("ACCOUNT_SHARE_CONSUMER_PROTECTION_VIOLATION", "the update would reduce rights already granted to consumers")
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
	ID                                      int64                       `json:"id"`
	RowVersion                              int64                       `json:"row_version"`
	CurrentRevisionID                       *int64                      `json:"current_revision_id,omitempty"`
	Deleted                                 bool                        `json:"deleted"`
	AccountID                               int64                       `json:"account_id,omitempty"`
	RoomName                                string                      `json:"room_name"`
	AccountCount                            int                         `json:"account_count"`
	HealthyAccountCount                     int                         `json:"healthy_account_count"`
	AccountSampleScope                      string                      `json:"account_sample_scope"`
	QuotaSummary                            *AccountShareQuotaSummary   `json:"quota_summary,omitempty"`
	Accounts                                []AccountShareRoomAccount   `json:"accounts,omitempty"`
	Platform                                string                      `json:"platform"`
	OwnerUserID                             int64                       `json:"owner_user_id"`
	OwnerUsername                           string                      `json:"owner_username,omitempty"`
	AccountName                             string                      `json:"account_name,omitempty"`
	ProxyID                                 *int64                      `json:"proxy_id,omitempty"`
	Proxy                                   *AccountShareListingProxy   `json:"proxy,omitempty"`
	Status                                  string                      `json:"status"`
	SeatLimit                               int                         `json:"seat_limit"`
	ActiveSeats                             int                         `json:"active_seats"`
	AccountIdentityID                       *int64                      `json:"account_identity_id,omitempty"`
	RatingCount                             int                         `json:"rating_count"`
	RatingScoreSum                          int                         `json:"rating_score_sum"`
	RatingAvg                               float64                     `json:"rating_avg"`
	RateMultiplier                          float64                     `json:"rate_multiplier"`
	AllowedModels                           []string                    `json:"allowed_models"`
	PerUserConcurrency                      int                         `json:"per_user_concurrency"`
	AccountConcurrency                      int                         `json:"account_concurrency"`
	RepresentativeAccountConcurrency        int                         `json:"-"`
	RepresentativeAccountAutoPauseOnExpired bool                        `json:"-"`
	HourlyRate                              float64                     `json:"hourly_rate"`
	HourlyFeeWaiverMinimum                  float64                     `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired                      float64                     `json:"min_balance_required"`
	CodexCLIOnly                            bool                        `json:"codex_cli_only"`
	Codex5hLimitPercent                     float64                     `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent                     float64                     `json:"codex_7d_limit_percent"`
	Anthropic5hLimitPercent                 float64                     `json:"anthropic_5h_limit_percent,omitempty"`
	Anthropic7dLimitPercent                 float64                     `json:"anthropic_7d_limit_percent,omitempty"`
	AccountLevel                            string                      `json:"account_level,omitempty"`
	AccountPlanType                         string                      `json:"account_plan_type,omitempty"`
	AccountStatus                           string                      `json:"account_status,omitempty"`
	AccountSchedulable                      bool                        `json:"account_schedulable"`
	CurrentConcurrency                      int                         `json:"current_concurrency"`
	RuntimeLoadKnown                        bool                        `json:"runtime_load_known"`
	AccountExpiresAt                        *time.Time                  `json:"account_expires_at,omitempty"`
	SubscriptionExpiresAt                   *time.Time                  `json:"subscription_expires_at,omitempty"`
	AccountLastUsedAt                       *time.Time                  `json:"account_last_used_at,omitempty"`
	RateLimitedAt                           *time.Time                  `json:"rate_limited_at,omitempty"`
	RateLimitResetAt                        *time.Time                  `json:"rate_limit_reset_at,omitempty"`
	OverloadUntil                           *time.Time                  `json:"overload_until,omitempty"`
	TempUnschedulableUntil                  *time.Time                  `json:"temp_unschedulable_until,omitempty"`
	TempUnschedulableReason                 string                      `json:"temp_unschedulable_reason,omitempty"`
	CodexQuotaProtectionReason              *string                     `json:"codex_quota_protection_reason,omitempty"`
	CodexQuotaProtectionResetAt             *time.Time                  `json:"codex_quota_protection_reset_at,omitempty"`
	Codex5hUsage                            *UsageProgress              `json:"codex_5h_usage,omitempty"`
	Codex7dUsage                            *UsageProgress              `json:"codex_7d_usage,omitempty"`
	CodexUsageUpdatedAt                     *time.Time                  `json:"codex_usage_updated_at,omitempty"`
	AnthropicQuotaProtectionReason          *string                     `json:"anthropic_quota_protection_reason,omitempty"`
	AnthropicQuotaProtectionResetAt         *time.Time                  `json:"anthropic_quota_protection_reset_at,omitempty"`
	Anthropic5hUsage                        *UsageProgress              `json:"anthropic_5h_usage,omitempty"`
	Anthropic7dUsage                        *UsageProgress              `json:"anthropic_7d_usage,omitempty"`
	AnthropicUsageUpdatedAt                 *time.Time                  `json:"anthropic_usage_updated_at,omitempty"`
	OpencodeQuotaProtectionReason           *string                     `json:"opencode_quota_protection_reason,omitempty"`
	OpencodeQuotaProtectionResetAt          *time.Time                  `json:"opencode_quota_protection_reset_at,omitempty"`
	Opencode5hUsage                         *UsageProgress              `json:"opencode_5h_usage,omitempty"`
	Opencode7dUsage                         *UsageProgress              `json:"opencode_7d_usage,omitempty"`
	Opencode30dUsage                        *UsageProgress              `json:"opencode_30d_usage,omitempty"`
	OpencodeUsageUpdatedAt                  *time.Time                  `json:"opencode_usage_updated_at,omitempty"`
	CurrentMembershipID                     *int64                      `json:"current_membership_id,omitempty"`
	CurrentAPIKeyID                         *int64                      `json:"current_api_key_id,omitempty"`
	CurrentAPIKeyName                       string                      `json:"current_api_key_name,omitempty"`
	CurrentJoinedAt                         *time.Time                  `json:"current_joined_at,omitempty"`
	CurrentPaidUntil                        *time.Time                  `json:"current_paid_until,omitempty"`
	CurrentBilledUntil                      *time.Time                  `json:"current_billed_until,omitempty"`
	CurrentIdleTimeoutMinutes               *int                        `json:"current_idle_timeout_minutes,omitempty"`
	CurrentLastRequestAt                    *time.Time                  `json:"current_last_request_at,omitempty"`
	CurrentIdleExpiresAt                    *time.Time                  `json:"current_idle_expires_at,omitempty"`
	CurrentWaiverProgress                   *AccountShareWaiverProgress `json:"current_waiver_progress,omitempty"`
	QueueMembershipID                       *int64                      `json:"queue_membership_id,omitempty"`
	QueueAPIKeyID                           *int64                      `json:"queue_api_key_id,omitempty"`
	QueueAPIKeyName                         string                      `json:"queue_api_key_name,omitempty"`
	QueueRank                               *int                        `json:"queue_rank,omitempty"`
	QueueStatus                             string                      `json:"queue_status,omitempty"`
	QueueEndingOperationID                  string                      `json:"queue_ending_operation_id,omitempty"`
	QueueEndingOperationStatus              string                      `json:"queue_ending_operation_status,omitempty"`
	QueueSettlementStatus                   string                      `json:"queue_settlement_status,omitempty"`
	QueueIdleTimeoutMinutes                 *int                        `json:"queue_idle_timeout_minutes,omitempty"`
	QueueDispatchCooldownUntil              *time.Time                  `json:"queue_dispatch_cooldown_until,omitempty"`
	LastUsedMembershipID                    *int64                      `json:"last_used_membership_id,omitempty"`
	LastUsedAt                              *time.Time                  `json:"last_used_at,omitempty"`
	HistorySnapshotQuality                  string                      `json:"history_snapshot_quality,omitempty"`
	EditingByUserID                         *int64                      `json:"editing_by_user_id,omitempty"`
	EditingByUsername                       string                      `json:"editing_by_username,omitempty"`
	EditingExpiresAt                        *time.Time                  `json:"editing_expires_at,omitempty"`
	EditingMine                             bool                        `json:"editing_mine"`
	EditSessionID                           string                      `json:"edit_session_id,omitempty"`
	CreatedAt                               time.Time                   `json:"created_at"`
	UpdatedAt                               time.Time                   `json:"updated_at"`
}

type AccountShareQuotaSummary struct {
	Scope         string                         `json:"scope"`
	AttachedCount int                            `json:"attached_count"`
	EligibleCount int                            `json:"eligible_count"`
	Window5h      AccountShareQuotaWindowSummary `json:"window_5h"`
	Window7d      AccountShareQuotaWindowSummary `json:"window_7d"`
}

type AccountShareQuotaWindowSummary struct {
	KnownCount             int        `json:"known_count"`
	MinUtilization         *float64   `json:"min_utilization"`
	MaxUtilization         *float64   `json:"max_utilization"`
	AverageUtilization     *float64   `json:"average_utilization"`
	MaxUtilizationResetsAt *time.Time `json:"max_utilization_resets_at"`
	Partial                bool       `json:"partial"`
}

type AccountShareRoomQuotaSnapshot struct {
	ListingID int64
	Window5h  *UsageProgress
	Window7d  *UsageProgress
}

type AccountShareRoomAccount struct {
	AccountID          int64      `json:"account_id"`
	AccountName        string     `json:"account_name"`
	Platform           string     `json:"platform"`
	AccountLevel       string     `json:"account_level"`
	Status             string     `json:"status"`
	Schedulable        bool       `json:"schedulable"`
	CurrentConcurrency int        `json:"current_concurrency"`
	Priority           int        `json:"priority"`
	PlacementState     string     `json:"placement_state"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
}

type BatchAccountShareRoomAccountsInput struct {
	ListingID      int64
	AccountIDs     []int64
	OwnerUserID    int64
	IdempotencyKey string
}

type AccountExternalPlacement struct {
	Target        string `json:"target"`
	RoomID        *int64 `json:"room_id,omitempty"`
	RoomName      string `json:"room_name,omitempty"`
	PublicGroupID *int64 `json:"public_group_id,omitempty"`
	State         string `json:"state"`
	Version       int64  `json:"version"`
}

type ConvertAccountExternalPlacementInput struct {
	AccountID      int64
	OwnerUserID    int64
	Target         string
	RoomID         *int64
	IdempotencyKey string
	GroupIDs       []int64
	PublicGroupID  *int64
}

type ConvertAccountExternalPlacementResult struct {
	AccountID         int64                          `json:"account_id"`
	Previous          *AccountExternalPlacement      `json:"previous"`
	Current           *AccountExternalPlacement      `json:"current"`
	Unchanged         bool                           `json:"unchanged"`
	SeatBillingResult *AccountShareSeatBillingResult `json:"-"`
}

type CreateAccountShareRoomInput struct {
	AccountID               int64
	IdempotencyKey          string
	RoomName                string
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
}

type AccountShareWaiverProgress struct {
	Enabled                  bool       `json:"enabled"`
	Status                   string     `json:"status"`
	WindowStart              time.Time  `json:"window_start"`
	WindowEnd                time.Time  `json:"window_end"`
	Now                      time.Time  `json:"now"`
	ElapsedSeconds           int64      `json:"elapsed_seconds"`
	RemainingSeconds         int64      `json:"remaining_seconds"`
	RequiredAmount           float64    `json:"required_amount"`
	UsageAmount              float64    `json:"usage_amount"`
	RemainingAmount          float64    `json:"remaining_amount"`
	ProgressPercent          float64    `json:"progress_percent"`
	HourlyRate               float64    `json:"hourly_rate"`
	WaiverMinimum            float64    `json:"waiver_minimum"`
	EstimatedHourlyFeeRefund float64    `json:"estimated_hourly_fee_refund"`
	RequestCount             int64      `json:"request_count"`
	LastRequestAt            *time.Time `json:"last_request_at,omitempty"`
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
	// TotalCacheReadTokens is the provider-reported aggregate. Historical
	// usage_logs cannot reliably split its text and image cache components.
	TotalCacheReadTokens   int64
	TotalImageInputTokens  int64
	TotalImageOutputTokens int64
	ActiveHourBuckets      int64
	ModelMatched           bool
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
	// CacheReadTokensPerRequest is informational aggregate history only. It
	// must not be treated as text-cache usage without an authoritative split.
	CacheReadTokensPerRequest   int `json:"cache_read_tokens_per_request"`
	ImageInputTokensPerRequest  int `json:"image_input_tokens_per_request"`
	ImageOutputTokensPerRequest int `json:"image_output_tokens_per_request"`
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

type AccountShareRecommendationScoreBreakdown struct {
	CostSavingScore   float64 `json:"cost_saving_score"`
	StabilityScore    float64 `json:"stability_score"`
	AvailabilityScore float64 `json:"availability_score"`
	RiskControlScore  float64 `json:"risk_control_score"`
	OverallScore      float64 `json:"overall_score"`
}

type AccountShareRecommendationCandidate struct {
	Rank           int                                      `json:"rank"`
	Listing        AccountShareListing                      `json:"listing"`
	Estimate       AccountShareRecommendationEstimate       `json:"estimate"`
	Score          float64                                  `json:"score"`
	ScoreBreakdown AccountShareRecommendationScoreBreakdown `json:"score_breakdown"`
	Tags           []string                                 `json:"tags"`
	Reasons        []string                                 `json:"reasons"`
	Warnings       []string                                 `json:"warnings,omitempty"`
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
	ID                             int64                             `json:"id"`
	ListingID                      int64                             `json:"listing_id"`
	ListingRevisionID              *int64                            `json:"listing_revision_id,omitempty"`
	ListingVersionSnapshot         *int64                            `json:"listing_version_snapshot,omitempty"`
	AccountID                      int64                             `json:"account_id"`
	OwnerUserID                    int64                             `json:"owner_user_id,omitempty"`
	RoomNameSnapshot               string                            `json:"room_name_snapshot,omitempty"`
	OwnerUserIDSnapshot            *int64                            `json:"owner_user_id_snapshot,omitempty"`
	OwnerUsernameSnapshot          string                            `json:"owner_username_snapshot,omitempty"`
	PlatformSnapshot               string                            `json:"platform_snapshot,omitempty"`
	AccountLevelSnapshot           string                            `json:"account_level_snapshot,omitempty"`
	APIKeyNameSnapshot             string                            `json:"api_key_name_snapshot,omitempty"`
	TermsSnapshot                  *AccountShareListingTermsSnapshot `json:"terms_snapshot,omitempty"`
	SnapshotQuality                string                            `json:"snapshot_quality,omitempty"`
	ConsumerUserID                 int64                             `json:"consumer_user_id"`
	APIKeyID                       int64                             `json:"api_key_id"`
	Status                         string                            `json:"status"`
	QueueRank                      int                               `json:"queue_rank"`
	HourlyRateSnapshot             float64                           `json:"hourly_rate_snapshot"`
	HourlyFeeWaiverMinimumSnapshot float64                           `json:"hourly_fee_waiver_minimum_snapshot"`
	IdleTimeoutMinutes             int                               `json:"idle_timeout_minutes"`
	JoinedAt                       time.Time                         `json:"joined_at"`
	LastRequestAt                  *time.Time                        `json:"last_request_at,omitempty"`
	EndedAt                        *time.Time                        `json:"ended_at,omitempty"`
	EndedReason                    string                            `json:"ended_reason,omitempty"`
	PaidUntil                      *time.Time                        `json:"paid_until,omitempty"`
	BilledUntil                    *time.Time                        `json:"billed_until,omitempty"`
	WaiverWindowStartedAt          *time.Time                        `json:"waiver_window_started_at,omitempty"`
	WaiverWindowUsageAmount        float64                           `json:"waiver_window_usage_amount"`
	WaiverWindowRequestCount       int64                             `json:"waiver_window_request_count"`
	WaiverWindowLastRequestAt      *time.Time                        `json:"waiver_window_last_request_at,omitempty"`
	DispatchFailedAt               *time.Time                        `json:"dispatch_failed_at,omitempty"`
	DispatchCooldownUntil          *time.Time                        `json:"dispatch_cooldown_until,omitempty"`
	EndingRequestedAt              *time.Time                        `json:"ending_requested_at,omitempty"`
	EndingReason                   string                            `json:"ending_reason,omitempty"`
	SettlementStatus               string                            `json:"settlement_status,omitempty"`
	EndingOperationID              string                            `json:"ending_operation_id,omitempty"`
	EndingOperationStatus          string                            `json:"ending_operation_status,omitempty"`
	CreatedAt                      time.Time                         `json:"created_at"`
	UpdatedAt                      time.Time                         `json:"updated_at"`
}

type AccountShareAPIKeyBindingStatus struct {
	APIKeyID      int64                    `json:"api_key_id"`
	ActiveCount   int                      `json:"active_count"`
	QueuedCount   int                      `json:"queued_count"`
	EndingCount   int                      `json:"ending_count"`
	BlockingCount int                      `json:"blocking_count"`
	Memberships   []AccountShareMembership `json:"memberships"`
}

type AccountShareMembershipHistoryReview struct {
	ID                  int64      `json:"id"`
	Score               int        `json:"score"`
	Comment             string     `json:"comment,omitempty"`
	CommentStatus       string     `json:"comment_status"`
	CommentRejectReason string     `json:"comment_reject_reason,omitempty"`
	CreatedAt           *time.Time `json:"created_at,omitempty"`
}

// AccountShareMembershipHistoryEntry is an immutable, membership-scoped
// history record. It intentionally does not depend on the current room account
// assignment, so it remains readable after the room is soft-deleted or its
// accounts are detached.
type AccountShareMembershipHistoryEntry struct {
	MembershipID                  int64                                `json:"membership_id"`
	ListingID                     int64                                `json:"listing_id"`
	ListingRevisionID             *int64                               `json:"listing_revision_id,omitempty"`
	ListingVersionSnapshot        *int64                               `json:"listing_version_snapshot,omitempty"`
	RoomName                      string                               `json:"room_name"`
	RoomDeleted                   bool                                 `json:"room_deleted"`
	RoomDeletedAt                 *time.Time                           `json:"room_deleted_at,omitempty"`
	OwnerUserID                   int64                                `json:"owner_user_id"`
	OwnerUsername                 string                               `json:"owner_username,omitempty"`
	Platform                      string                               `json:"platform"`
	AccountLevel                  string                               `json:"account_level,omitempty"`
	AccountID                     int64                                `json:"account_id,omitempty"`
	AccountName                   string                               `json:"account_name,omitempty"`
	ConfiguredConcurrencySnapshot int                                  `json:"configured_concurrency_snapshot,omitempty"`
	APIKeyID                      int64                                `json:"api_key_id"`
	APIKeyName                    string                               `json:"api_key_name,omitempty"`
	Status                        string                               `json:"status"`
	JoinedAt                      time.Time                            `json:"joined_at"`
	LastRequestAt                 *time.Time                           `json:"last_request_at,omitempty"`
	EndedAt                       *time.Time                           `json:"ended_at,omitempty"`
	EndedReason                   string                               `json:"ended_reason,omitempty"`
	PaidUntil                     *time.Time                           `json:"paid_until,omitempty"`
	BilledUntil                   *time.Time                           `json:"billed_until,omitempty"`
	HourlyRateSnapshot            float64                              `json:"hourly_rate_snapshot"`
	HourlyFeeWaiverMinimum        float64                              `json:"hourly_fee_waiver_minimum_snapshot"`
	IdleTimeoutMinutes            int                                  `json:"idle_timeout_minutes"`
	UsageRequestCount             int64                                `json:"usage_request_count"`
	UsageRequestCost              float64                              `json:"usage_request_cost"`
	TermsSnapshot                 *AccountShareListingTermsSnapshot    `json:"terms_snapshot,omitempty"`
	SnapshotQuality               string                               `json:"snapshot_quality"`
	Review                        *AccountShareMembershipHistoryReview `json:"review,omitempty"`
}

type AccountShareMembershipRuntimeBinding struct {
	BindingID           int64 `json:"binding_id"`
	MembershipID        int64 `json:"membership_id"`
	ListingID           int64 `json:"listing_id"`
	AccountID           int64 `json:"account_id"`
	ListingRevisionID   int64 `json:"listing_revision_id"`
	TermsRevisionNumber int64 `json:"terms_revision_number"`
	RoutingGeneration   int64 `json:"routing_generation"`
}

type AccountShareListingTermsSnapshot struct {
	ListingRevisionID       int64    `json:"listing_revision_id"`
	RowVersion              int64    `json:"row_version"`
	SchemaVersion           int      `json:"schema_version"`
	RoomName                string   `json:"room_name"`
	Status                  string   `json:"status"`
	SeatLimit               int      `json:"seat_limit"`
	RateMultiplier          float64  `json:"rate_multiplier"`
	AllowedModels           []string `json:"allowed_models"`
	PerUserConcurrency      int      `json:"per_user_concurrency"`
	HourlyRate              float64  `json:"hourly_rate"`
	HourlyFeeWaiverMinimum  float64  `json:"hourly_fee_waiver_minimum"`
	MinBalanceRequired      float64  `json:"min_balance_required"`
	CodexCLIOnly            bool     `json:"codex_cli_only"`
	Codex5hLimitPercent     float64  `json:"codex_5h_limit_percent"`
	Codex7dLimitPercent     float64  `json:"codex_7d_limit_percent"`
	Anthropic5hLimitPercent float64  `json:"anthropic_5h_limit_percent,omitempty"`
	Anthropic7dLimitPercent float64  `json:"anthropic_7d_limit_percent,omitempty"`
}

type AccountShareReview struct {
	ID                  int64     `json:"id"`
	AccountIdentityID   int64     `json:"account_identity_id,omitempty"`
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
	APIKeyName         string     `json:"api_key_name,omitempty"`
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
	OperationID  string    `json:"operation_id"`
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type CreateAccountShareJoinIntentInput struct {
	APIKeyID           int64
	IdleTimeoutMinutes int
	AcceptQueue        bool
}

type CompleteAccountShareJoinInput struct {
	APIKeyID           int64
	IdleTimeoutMinutes int
	IntentToken        string
	ExpectedVersion    int64
	ExpectedRevisionID int64
	AcceptQueue        bool
}

type AccountShareJoinIntent struct {
	ListingID          int64                             `json:"listing_id"`
	APIKeyID           int64                             `json:"api_key_id"`
	Token              string                            `json:"token"`
	ExpiresAt          time.Time                         `json:"expires_at"`
	ExpectedVersion    int64                             `json:"expected_version"`
	ExpectedRevisionID int64                             `json:"expected_revision_id,omitempty"`
	AcceptQueue        bool                              `json:"accept_queue"`
	QueueMayBeRequired bool                              `json:"queue_may_be_required"`
	Terms              *AccountShareListingTermsSnapshot `json:"terms"`
}

type AccountShareJoinRepositoryInput struct {
	ConsumerUserID     int64
	APIKeyID           int64
	ListingID          int64
	IdleTimeoutMinutes int
	ExpectedVersion    int64
	ExpectedRevisionID int64
	AcceptQueue        bool
	IntentIssuedAt     time.Time
	IntentNonce        string
	AcceptedTerms      *AccountShareListingTermsSnapshot
}

type accountShareJoinIntentTokenClaims struct {
	Action             string                           `json:"action"`
	ConsumerID         int64                            `json:"consumer_user_id"`
	ListingID          int64                            `json:"listing_id"`
	APIKeyID           int64                            `json:"api_key_id"`
	IdleTimeoutMinutes int                              `json:"idle_timeout_minutes"`
	ExpectedVersion    int64                            `json:"expected_version"`
	ExpectedRevisionID int64                            `json:"expected_revision_id,omitempty"`
	AcceptQueue        bool                             `json:"accept_queue"`
	Terms              AccountShareListingTermsSnapshot `json:"terms"`
	Nonce              string                           `json:"nonce"`
	IssuedAt           int64                            `json:"issued_at"`
	ExpiresAt          int64                            `json:"expires_at"`
}

type accountShareEndMembershipTokenClaims struct {
	Action           string `json:"action"`
	ConsumerID       int64  `json:"consumer_user_id"`
	MembershipID     int64  `json:"membership_id"`
	MembershipStatus string `json:"membership_status"`
	OperationID      string `json:"operation_id"`
	Nonce            string `json:"nonce"`
	ExpiresAt        int64  `json:"expires_at"`
}

type BeginAccountShareMembershipEndInput struct {
	ConsumerUserID           int64
	MembershipID             int64
	ExpectedMembershipStatus string
	OperationID              string
}

type AccountShareEndingMembershipCandidate struct {
	MembershipID      int64
	OperationID       string
	EndingRequestedAt time.Time
	// LastRequestAt 是结束结算兜底的在途信号：在途请求的心跳会通过 DB 持续 touch
	// last_request_at（与 Redis lease 无关），强制 finalize 前据此判断是否仍有请求在跑。
	LastRequestAt time.Time
}

type AccountShareSeatBillingResult struct {
	Processed            int
	DebitUserIDs         []int64
	CreditUserIDs        []int64
	EndedConsumerUserIDs []int64
}

// AccountShareSeatWaiverBatch 是 waiver 补偿单批的结果。
// Matched 是候选查询返回的行数(含逐行评估时被跳过的行),
// 游标是本批最后一行的 (period_ended_at, id),供轮内 keyset 续扫。
type AccountShareSeatWaiverBatch struct {
	Billing             *AccountShareSeatBillingResult
	Matched             int
	CursorPeriodEndedAt time.Time
	CursorID            int64
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

type AccountShareModeGroup struct {
	GroupID  int64  `json:"group_id"`
	Platform string `json:"platform"`
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
	PolicyID           *int64
	PolicyVersion      int
	OwnerShareRatio    float64
	InviteShareRatio   float64
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
	AccountLevels []OpenAIAccountLevelConfig
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
	ExpectedVersion         *int64
	Reason                  string
	Confirmed               bool
}

type BeginAccountShareListingEditInput struct {
	SessionID string
	Force     bool
	Expires   time.Time
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
	EnsureListingRevisionTerms(ctx context.Context, listingID int64) (*AccountShareListingTermsSnapshot, error)
	JoinListing(ctx context.Context, input AccountShareJoinRepositoryInput) (*AccountShareMembership, error)
	GetMembershipForEnd(ctx context.Context, consumerUserID int64, membershipID int64) (*AccountShareMembership, error)
	BeginMembershipEnd(ctx context.Context, input BeginAccountShareMembershipEndInput) (*AccountShareMembership, *AccountShareSeatBillingResult, error)
	FinalizeMembershipEnd(ctx context.Context, membershipID int64, operationID string) (*AccountShareMembership, *AccountShareSeatBillingResult, bool, error)
	ListEndingMembershipCandidates(ctx context.Context, limit int) ([]AccountShareEndingMembershipCandidate, error)
	UpdateMembershipIdleTimeout(ctx context.Context, consumerUserID int64, membershipID int64, idleTimeoutMinutes int) (*AccountShareMembership, error)
	SubmitReview(ctx context.Context, consumerUserID int64, membershipID int64, input SubmitAccountShareReviewInput) (*AccountShareReview, error)
	ListListingReviews(ctx context.Context, viewerUserID int64, viewerIsAdmin bool, listingID int64, params pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error)
	ListOwnerReviews(ctx context.Context, viewerUserID int64, ownerUserID int64, params pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error)
	ClaimPendingReviewModerations(ctx context.Context, now time.Time, limit int) ([]AccountShareReview, error)
	BeginReviewModerationAttempt(ctx context.Context, reviewID int64, maxAttempts int) (bool, error)
	CompleteReviewModeration(ctx context.Context, reviewID int64, result AccountShareReviewModerationResult) error
	FailReviewModeration(ctx context.Context, reviewID int64, reason string, nextRetryAt time.Time, maxAttempts int) error
	ListMembershipQueue(ctx context.Context, consumerUserID int64, apiKeyID int64) ([]AccountShareMembership, error)
	ListAPIKeyBindingMemberships(ctx context.Context, consumerUserID int64, apiKeyID int64) ([]AccountShareMembership, error)
	ReorderMembershipQueue(ctx context.Context, consumerUserID int64, apiKeyID int64, membershipIDs []int64) ([]AccountShareMembership, error)
	TouchMembershipLastRequest(ctx context.Context, membershipID int64, at time.Time) error
	ListIdleMembershipCandidates(ctx context.Context, now time.Time, filter AccountShareIdleMembershipFilter, limit int) ([]AccountShareIdleMembershipCandidate, error)
	EndIdleMembership(ctx context.Context, membershipID int64, endedAt time.Time) (*AccountShareMembership, *AccountShareSeatBillingResult, error)
	ProcessUnavailableMemberships(ctx context.Context, now time.Time, limit int) (*AccountShareSeatBillingResult, error)
	ListRecoverableUnavailableMembershipIDs(ctx context.Context, now time.Time, limit int) ([]int64, error)
	SuspendRecoverableUnavailableMembership(ctx context.Context, membershipID int64, unavailableAt time.Time) (*AccountShareMembership, *AccountShareSeatBillingResult, error)
	EndUnavailableAccountMemberships(ctx context.Context, accountID int64, endedAt time.Time, limit int) (*AccountShareSeatBillingResult, error)
	DisablePermanentlyUnavailableListings(ctx context.Context, now time.Time, limit int) (*AccountShareListingMaintenanceResult, error)
	ProcessSeatBilling(ctx context.Context, now time.Time, limit int) (*AccountShareSeatBillingResult, error)
	ProcessSeatWaiverBacklogCompensations(ctx context.Context, now time.Time, limit int, cursorPeriodEndedAt time.Time, cursorID int64) (*AccountShareSeatWaiverBatch, error)
	ProcessSeatWaiverLateUsageCompensations(ctx context.Context, now time.Time, limit int, usageSince, windowSince time.Time, cursorPeriodEndedAt time.Time, cursorID int64) (*AccountShareSeatWaiverBatch, error)
	ProcessSeatBillingForJoin(ctx context.Context, now time.Time, consumerUserID, apiKeyID, listingID int64) (*AccountShareSeatBillingResult, error)
	ProcessSeatBillingForRequest(ctx context.Context, now time.Time, consumerUserID, apiKeyID int64) (*AccountShareSeatBillingResult, error)
	GetActiveMembershipForAPIKey(ctx context.Context, apiKeyID int64) (*AccountShareMembership, *AccountShareListing, error)
	GetActiveMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (*AccountShareMembership, *AccountShareListing, error)
	ActivateNextQueuedMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64, afterRank int, now time.Time) (*AccountShareMembership, *AccountShareListing, error)
	SuspendMembershipForDispatchFailure(ctx context.Context, membershipID int64, failedAt time.Time, cooldownUntil time.Time) (*AccountShareMembership, *AccountShareSeatBillingResult, error)
	ResolvePolicy(ctx context.Context) (*AccountSharePolicy, error)
}

type AccountShareHistoryRepository interface {
	ListMembershipHistory(
		ctx context.Context,
		consumerUserID int64,
		params pagination.PaginationParams,
	) ([]AccountShareMembershipHistoryEntry, *pagination.PaginationResult, error)
}

type AccountShareRoomRepository interface {
	CreateRoomFromOwnedAccount(ctx context.Context, ownerUserID, accountID, modeGroupID int64, idempotencyKey string, listing *AccountShareListing) (*AccountShareListing, error)
	ListRoomAccounts(ctx context.Context, listingID, viewerUserID int64, viewerIsAdmin bool) ([]AccountShareRoomAccount, error)
	AttachRoomAccountsAtomic(ctx context.Context, input BatchAccountShareRoomAccountsInput) error
	DetachRoomAccountsAtomic(ctx context.Context, input BatchAccountShareRoomAccountsInput) (*AccountShareSeatBillingResult, error)
	HasRoomAccount(ctx context.Context, ownerUserID, accountID int64) (bool, error)
	GetExternalPlacement(ctx context.Context, ownerUserID, accountID int64) (*AccountExternalPlacement, error)
	BeginExternalPlacementDrain(ctx context.Context, ownerUserID, accountID int64) (bool, error)
	RestoreExternalPlacementAfterDrain(ctx context.Context, ownerUserID, accountID int64) error
	ConvertExternalPlacement(ctx context.Context, input ConvertAccountExternalPlacementInput) (*ConvertAccountExternalPlacementResult, error)
	RebindMembershipToHealthyRoomAccount(ctx context.Context, membershipID, currentAccountID int64, now time.Time) (bool, error)
}

type accountShareRoomCreationIdempotencyRepository interface {
	FindRoomCreationByIdempotency(
		ctx context.Context,
		ownerUserID, accountID int64,
		idempotencyKey string,
		listing *AccountShareListing,
	) (*AccountShareListing, error)
}

// accountShareOrphanBindingCleanupRepository 是可选接口：实现它的仓库提供孤儿 binding
// 清扫能力（兜底处理历史遗留的未闭合 binding），不实现则清扫 worker 静默跳过。
type accountShareOrphanBindingCleanupRepository interface {
	CleanupOrphanMembershipBindings(ctx context.Context, now time.Time, limit int) (int, error)
}

type accountShareVisibleListingRepository interface {
	GetVisibleListingByID(
		ctx context.Context,
		listingID int64,
		viewerUserID int64,
		viewerIsAdmin bool,
	) (*AccountShareListing, error)
}

type accountShareRoomRuntimeAccountsRepository interface {
	ListRoomRuntimeAccounts(
		ctx context.Context,
		listingIDs []int64,
		now time.Time,
	) (map[int64][]AccountWithConcurrency, error)
}

type accountShareRoomQuotaRepository interface {
	ListRoomQuotaSnapshots(
		ctx context.Context,
		listingIDs []int64,
		now time.Time,
	) (map[int64][]AccountShareRoomQuotaSnapshot, error)
}

type accountShareReviewDetailAuthorizationRepository interface {
	CanViewListingReviewDetails(
		ctx context.Context,
		viewerUserID int64,
		viewerIsAdmin bool,
		listingID int64,
	) (bool, error)
}

type AccountShareRuntimeBindingRepository interface {
	GetOpenMembershipRuntimeBinding(
		ctx context.Context,
		membershipID int64,
		accountID int64,
	) (*AccountShareMembershipRuntimeBinding, error)
}

type AccountShareModeProxyRepository interface {
	GetVisibleByID(ctx context.Context, scope ProxyScope, id int64) (*Proxy, error)
	ListActiveVisibleWithAccountCount(ctx context.Context, scope ProxyScope) ([]ProxyWithAccountCount, error)
	CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error)
}

type accountShareRecommendationUsageProfileRepository interface {
	GetAccountShareRecommendationUsageProfile(ctx context.Context, userID int64, platform, model string, startTime, endTime time.Time) (*AccountShareRecommendationUsageProfileStats, error)
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
	settingService       *SettingService
	reviewSettingRepo    SettingRepository
	reviewHTTPClient     *http.Client
	taskExecutor         *ClusterTaskExecutor
	actionTokenSecret    []byte
	seatBillingCtx       context.Context
	seatBillingCancel    context.CancelFunc
	seatBillingStopCh    chan struct{}
	seatBillingStopOnce  sync.Once
	seatBillingStartOnce sync.Once
	seatBillingWG        sync.WaitGroup
	// 迟到 usage 反查的高水位,仅 waiver 补偿 worker 单 goroutine 读写。
	// 重启/租约切换后归零,退化为 Lookback 下限——只多扫不漏扫。
	seatWaiverLateUsageHWM time.Time
	roomLifecycleCursorMu  sync.Mutex
	roomLifecycleAfterID   int64
	reviewCtx              context.Context
	reviewCancel           context.CancelFunc
	reviewStopCh           chan struct{}
	reviewStopOnce         sync.Once
	reviewStartOnce        sync.Once
	reviewWG               sync.WaitGroup
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
	seatBillingCtx, seatBillingCancel := context.WithCancel(context.Background())
	reviewCtx, reviewCancel := context.WithCancel(context.Background())
	return &AccountShareModeService{
		repo:               repo,
		accountRepo:        accountRepo,
		apiKeyRepo:         apiKeyRepo,
		userRepo:           userRepo,
		proxyRepo:          proxyRepo,
		openaiOAuthService: openaiOAuthService,
		oauthService:       oauthService,
		seatBillingCtx:     seatBillingCtx,
		seatBillingCancel:  seatBillingCancel,
		seatBillingStopCh:  make(chan struct{}),
		reviewCtx:          reviewCtx,
		reviewCancel:       reviewCancel,
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

func (s *AccountShareModeService) SetSettingService(settingService *SettingService) {
	if s == nil {
		return
	}
	s.settingService = settingService
}

func (s *AccountShareModeService) ResolveOwnerSelfUseMultiplier(ctx context.Context) (float64, error) {
	if s == nil || s.settingService == nil {
		return 0, ErrServiceUnavailable
	}
	settings, err := s.settingService.GetAllSettings(ctx)
	if err != nil {
		return 0, err
	}
	if settings == nil {
		return 0, ErrServiceUnavailable
	}
	ratio := settings.UserPrivateGroupCommissionRate
	if invalidNonNegativeFloat(ratio) || ratio > 1 {
		return 0, fmt.Errorf("invalid %s: %v", SettingKeyUserPrivateGroupCommissionRate, ratio)
	}
	return ratio, nil
}

func (s *AccountShareModeService) openAIAccountLevelConfigs(ctx context.Context) ([]OpenAIAccountLevelConfig, error) {
	if s == nil || s.settingService == nil {
		return DefaultOpenAIAccountLevelConfigs(), nil
	}
	return s.settingService.GetOpenAIAccountLevelConfigs(ctx)
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

func (s *AccountShareModeService) initialListingStatus() string {
	// 灰度已收敛：lifecycle 合约是唯一形态，新房间一律先验证。
	return AccountShareListingStatusValidating
}

func (s *AccountShareModeService) StartSeatBillingWorker() {
	if s == nil || s.repo == nil {
		return
	}
	s.seatBillingStartOnce.Do(func() {
		s.seatBillingWG.Add(5)
		go s.runSeatBillingWorker()
		go s.runSeatWaiverCompensationWorker()
		go s.runRoomLifecycleFinalizerWorker()
		go s.runRoomValidationWorker()
		go s.runOrphanBindingCleanupWorker()
	})
}

func (s *AccountShareModeService) StopSeatBillingWorker() {
	if s == nil {
		return
	}
	s.seatBillingStopOnce.Do(func() {
		if s.seatBillingCancel != nil {
			s.seatBillingCancel()
		}
		close(s.seatBillingStopCh)
	})
	s.seatBillingWG.Wait()
}

func (s *AccountShareModeService) seatBillingWorkerContext() context.Context {
	if s != nil && s.seatBillingCtx != nil {
		return s.seatBillingCtx
	}
	return context.Background()
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

func (s *AccountShareModeService) runSeatWaiverCompensationWorker() {
	defer s.seatBillingWG.Done()
	ticker := time.NewTicker(AccountShareModeSeatWaiverCompensationInterval)
	defer ticker.Stop()

	s.processSeatWaiverCompensationsOnce()
	for {
		select {
		case <-ticker.C:
			s.processSeatWaiverCompensationsOnce()
		case <-s.seatBillingStopCh:
			return
		}
	}
}

func (s *AccountShareModeService) runRoomLifecycleFinalizerWorker() {
	defer s.seatBillingWG.Done()
	ticker := time.NewTicker(AccountShareModeSeatBillingInterval)
	defer ticker.Stop()

	s.processRoomLifecycleFinalizationOnce()
	for {
		select {
		case <-ticker.C:
			s.processRoomLifecycleFinalizationOnce()
		case <-s.seatBillingStopCh:
			return
		}
	}
}

// runOrphanBindingCleanupWorker 兜底清扫历史遗留的孤儿 binding（membership 已 ended
// 但 binding 未闭合）。正常结束路径现已全部关闭 binding，本 worker 只处理存量脏数据，
// 防止账号/房间删除被不可解析的未闭合 binding 永久阻塞。
func (s *AccountShareModeService) runOrphanBindingCleanupWorker() {
	defer s.seatBillingWG.Done()
	ticker := time.NewTicker(AccountShareModeOrphanBindingCleanupInterval)
	defer ticker.Stop()

	s.processOrphanBindingCleanupOnce()
	for {
		select {
		case <-ticker.C:
			s.processOrphanBindingCleanupOnce()
		case <-s.seatBillingStopCh:
			return
		}
	}
}

func (s *AccountShareModeService) processOrphanBindingCleanupOnce() {
	if s == nil || s.repo == nil {
		return
	}
	if _, ok := s.repo.(accountShareOrphanBindingCleanupRepository); !ok {
		return
	}
	// 孤儿清扫与其它周期性 worker 一致，走集群 lease 避免多实例重复执行。
	// CleanupOrphanMembershipBindings 本身幂等（FOR UPDATE + 二次 0 行），但复用
	// taskExecutor 统一多实例协调与可观测性。taskExecutor 为 nil 时退化为单实例直跑，
	// 保证测试/最小化装配下清扫仍能工作。
	if s.taskExecutor != nil {
		ctx, cancel := context.WithTimeout(s.seatBillingWorkerContext(), AccountShareModeMembershipTouchTimeout*3)
		defer cancel()
		_, err := s.taskExecutor.Run(ctx, accountShareOrphanBindingCleanupTaskName, func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
			if err := guard.Check(taskCtx); err != nil {
				return err
			}
			s.processOrphanBindingCleanupBatch(taskCtx)
			return guard.Check(taskCtx)
		})
		if err != nil {
			log.Printf("account_share_mode: orphan binding cleanup lease failed: %v", err)
		}
		return
	}
	s.processOrphanBindingCleanupBatch(s.seatBillingWorkerContext())
}

func (s *AccountShareModeService) processOrphanBindingCleanupBatch(ctx context.Context) {
	cleanupRepo, ok := s.repo.(accountShareOrphanBindingCleanupRepository)
	if !ok {
		return
	}
	cleaned, err := cleanupRepo.CleanupOrphanMembershipBindings(ctx, time.Now().UTC(), AccountShareModeSeatBillingBatchSize)
	if err != nil {
		log.Printf("account_share_mode: orphan binding cleanup failed: %v", err)
		return
	}
	if cleaned > 0 {
		log.Printf("account_share_mode: cleaned %d orphan membership bindings", cleaned)
	}
}

func (s *AccountShareModeService) processRoomLifecycleFinalizationOnce() {
	if s == nil || s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.seatBillingWorkerContext(), 2*time.Minute)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, accountShareRoomLifecycleFinalizerTaskName, func(
		taskCtx context.Context,
		guard *ClusterLeaseGuard,
	) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		s.processRoomLifecycleOnce(taskCtx)
		return guard.Check(taskCtx)
	})
	if err != nil {
		log.Printf("account_share_mode: room lifecycle finalizer lease failed: %v", err)
	}
}

func (s *AccountShareModeService) processSeatBillingOnce() {
	if s == nil || s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.seatBillingWorkerContext(), 5*time.Minute)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, accountShareSeatBillingTaskName, func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		return s.processSeatBillingOnceLeased(taskCtx, guard)
	})
	if err != nil {
		log.Printf("account_share_mode: seat billing lease failed: %v", err)
	}
}

func (s *AccountShareModeService) processSeatBillingOnceLeased(ctx context.Context, guard *ClusterLeaseGuard) error {
	if err := guard.Check(ctx); err != nil {
		return err
	}
	s.processUnavailableMembershipsOnce(ctx)
	if err := guard.Check(ctx); err != nil {
		return err
	}
	s.processPermanentlyUnavailableListingsOnce(ctx)
	if err := guard.Check(ctx); err != nil {
		return err
	}
	s.processRecoverableUnavailableMembershipsOnce(ctx)
	if err := guard.Check(ctx); err != nil {
		return err
	}
	s.processIdleMembershipsOnce(ctx)
	for {
		if err := guard.Check(ctx); err != nil {
			return err
		}
		batchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := s.repo.ProcessSeatBilling(batchCtx, time.Now().UTC(), AccountShareModeSeatBillingBatchSize)
		cancel()
		if err != nil {
			return fmt.Errorf("process prepaid seat billing: %w", err)
		}
		s.invalidateSeatBillingCaches(result)
		if result == nil || result.Processed < AccountShareModeSeatBillingBatchSize {
			break
		}
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	s.processEndingMembershipsOnce(ctx)
	if err := guard.Check(ctx); err != nil {
		return err
	}
	return nil
}

func (s *AccountShareModeService) processEndingMembershipsOnce(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	candidates, err := s.repo.ListEndingMembershipCandidates(ctx, AccountShareModeSeatBillingBatchSize)
	if err != nil {
		log.Printf("account_share_mode: list ending memberships failed: %v", err)
		return
	}
	for _, candidate := range candidates {
		if candidate.MembershipID <= 0 || strings.TrimSpace(candidate.OperationID) == "" {
			continue
		}
		hasLease, leaseErr := s.hasActiveMembershipLease(ctx, candidate.MembershipID)
		if leaseErr != nil {
			// Redis 是运行时并发租约权威，不可用时默认 fail-closed（避免在有在途请求时
			// 中途结算）。但结束结算不能无限期停摆：当 ending 已持续超过阈值时，需要一条
			// 有界兜底路径。这里用两个信号共同决定是否强行走 finalize：
			//
			//   1. ending 已超过阈值（AccountShareModeEndSettlementForceTimeout）；
			//   2. 结束请求之后没有仍在 touch 的在途请求。在途请求的心跳与 Redis lease 无关，
			//      会持续通过 DB 刷新 last_request_at；若 last_request_at 晚于结束请求时间，
			//      说明确有请求在跑，本轮跳过（下一轮仍会重估，直到请求自然结束）。
			//
			// 二者同时满足才 finalize。这样 DB 侧检查（last_request_at 心跳）真正兜住了
			// 「Redis 断连期间有长请求在跑」的边界——不再依赖已被删除的 billing intent 检查。
			// 代价是：Redis 断连且请求真在跑时，结束结算会继续等待，但这是 fail-closed 应有的
			// 行为（宁慢勿错），且请求结束后下一轮即可完成结算。
			now := time.Now().UTC()
			if !candidate.EndingRequestedAt.IsZero() &&
				now.Sub(candidate.EndingRequestedAt) >= AccountShareModeEndSettlementForceTimeout &&
				!candidate.LastRequestAt.After(candidate.EndingRequestedAt) {
				log.Printf("account_share_mode: force finalize ending membership %d after %s despite lease unknown: %v",
					candidate.MembershipID, AccountShareModeEndSettlementForceTimeout, leaseErr)
				membership, billing, finalized, finalizeErr := s.repo.FinalizeMembershipEnd(ctx, candidate.MembershipID, candidate.OperationID)
				if finalizeErr != nil {
					log.Printf("account_share_mode: force finalize ending membership %d failed: %v", candidate.MembershipID, finalizeErr)
					continue
				}
				if !finalized {
					continue
				}
				s.invalidateMembershipEndCaches(ctx, membership, billing)
			}
			continue
		}
		if hasLease {
			continue
		}
		membership, billing, finalized, finalizeErr := s.repo.FinalizeMembershipEnd(ctx, candidate.MembershipID, candidate.OperationID)
		if finalizeErr != nil {
			log.Printf("account_share_mode: finalize ending membership %d failed: %v", candidate.MembershipID, finalizeErr)
			continue
		}
		if !finalized {
			continue
		}
		s.invalidateMembershipEndCaches(ctx, membership, billing)
	}
}

func (s *AccountShareModeService) processSeatWaiverCompensationsOnce() {
	if s == nil || s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.seatBillingWorkerContext(), AccountShareModeSeatWaiverCompensationTimeout)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, accountShareSeatWaiverCompensationTaskName, func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		return s.runSeatWaiverCompensationRound(taskCtx, guard)
	})
	if err != nil {
		log.Printf("account_share_mode: process seat waiver compensations failed: %v", err)
	}
}

// runSeatWaiverCompensationRound 两阶段消化 waiver 补偿:
// 阶段1 排干未评估积压(迁移 203 回炉的历史行),阶段2 反查迟到 usage 触发的重评。
// 两阶段共用轮内软预算与 keyset 游标;阶段2 的高水位仅在该阶段排干时推进,
// 截断时冻结,保证不漏。
func (s *AccountShareModeService) runSeatWaiverCompensationRound(taskCtx context.Context, guard *ClusterLeaseGuard) error {
	roundStart := time.Now().UTC()
	deadline := roundStart.Add(AccountShareModeSeatWaiverCompensationRoundBudget)
	batchSize := AccountShareModeSeatWaiverCompensationBatchSize

	var cursorEndedAt time.Time
	var cursorID int64
	for {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		batch, err := s.repo.ProcessSeatWaiverBacklogCompensations(taskCtx, time.Now().UTC(), batchSize, cursorEndedAt, cursorID)
		if err != nil {
			return fmt.Errorf("process seat waiver backlog compensations: %w", err)
		}
		if batch != nil {
			s.invalidateSeatBillingCaches(batch.Billing)
		}
		if batch == nil || batch.Matched < batchSize {
			break
		}
		cursorEndedAt, cursorID = batch.CursorPeriodEndedAt, batch.CursorID
		if time.Now().UTC().After(deadline) {
			// 积压未排干,本轮预算已尽:阶段2 留待下轮,HWM 不动。
			return nil
		}
	}

	usageSince := roundStart.Add(-AccountShareModeSeatWaiverLateUsageLookback)
	if hwm := s.seatWaiverLateUsageHWM; !hwm.IsZero() && hwm.After(usageSince) {
		usageSince = hwm
	}
	windowSince := usageSince.Add(-AccountShareModeSeatWaiverLateUsageSlack)
	cursorEndedAt, cursorID = time.Time{}, 0
	for {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		batch, err := s.repo.ProcessSeatWaiverLateUsageCompensations(taskCtx, time.Now().UTC(), batchSize, usageSince, windowSince, cursorEndedAt, cursorID)
		if err != nil {
			return fmt.Errorf("process seat waiver late usage compensations: %w", err)
		}
		if batch != nil {
			s.invalidateSeatBillingCaches(batch.Billing)
		}
		if batch == nil || batch.Matched < batchSize {
			// 排干:推进高水位,留一个补偿延迟的余量覆盖在途落账。
			s.seatWaiverLateUsageHWM = roundStart.Add(-AccountShareModeSeatWaiverCompensationDelay)
			return nil
		}
		cursorEndedAt, cursorID = batch.CursorPeriodEndedAt, batch.CursorID
		if time.Now().UTC().After(deadline) {
			return nil
		}
	}
}

func (s *AccountShareModeService) processUnavailableMembershipsOnce(parentCtx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
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

func (s *AccountShareModeService) processPermanentlyUnavailableListingsOnce(parentCtx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
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

func (s *AccountShareModeService) processRecoverableUnavailableMembershipsOnce(parentCtx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	result, err := s.processRecoverableUnavailableMemberships(ctx, time.Now().UTC(), AccountShareModeSeatBillingBatchSize)
	cancel()
	if err != nil {
		log.Printf("account_share_mode: suspend recoverable unavailable memberships failed: %v", err)
		return
	}
	s.invalidateSeatBillingCaches(result)
}

func (s *AccountShareModeService) processRecoverableUnavailableMemberships(ctx context.Context, now time.Time, limit int) (*AccountShareSeatBillingResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	if limit <= 0 {
		limit = AccountShareModeSeatBillingBatchSize
	}
	now = now.UTC()
	membershipIDs, err := s.repo.ListRecoverableUnavailableMembershipIDs(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	result := &AccountShareSeatBillingResult{Processed: len(membershipIDs)}
	for _, membershipID := range membershipIDs {
		if membershipID <= 0 {
			continue
		}
		active, err := s.membershipHasActiveConcurrency(ctx, membershipID)
		if err != nil {
			return result, err
		}
		if active {
			continue
		}
		membership, billing, err := s.repo.SuspendRecoverableUnavailableMembership(ctx, membershipID, now)
		if err != nil {
			if errors.Is(err, ErrAccountShareListingNotFound) {
				continue
			}
			return result, err
		}
		if membership == nil {
			continue
		}
		appendAccountShareSeatBillingResult(result, billing)
	}
	return result, nil
}

func (s *AccountShareModeService) processIdleMembershipsOnce(parentCtx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
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
		membership, billing, err := s.repo.EndIdleMembership(ctx, candidate.MembershipID, candidate.Deadline)
		if err != nil {
			if errors.Is(err, ErrAccountShareListingNotFound) {
				continue
			}
			return result, err
		}
		if membership == nil {
			continue
		}
		appendAccountShareSeatBillingResult(result, billing)
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

func appendAccountShareSeatBillingResult(target, source *AccountShareSeatBillingResult) {
	if target == nil || source == nil {
		return
	}
	target.DebitUserIDs = append(target.DebitUserIDs, source.DebitUserIDs...)
	target.CreditUserIDs = append(target.CreditUserIDs, source.CreditUserIDs...)
	target.EndedConsumerUserIDs = append(target.EndedConsumerUserIDs, source.EndedConsumerUserIDs...)
}

func (s *AccountShareModeService) EnsureModeGroup(ctx context.Context, platform string) (*Group, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAccountShareModeGroupUnavailable
	}
	return s.repo.EnsureModeGroup(ctx, platform)
}

func (s *AccountShareModeService) ListModeGroups(ctx context.Context) ([]AccountShareModeGroup, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAccountShareModeGroupUnavailable
	}
	platforms := [...]string{PlatformOpenAI, PlatformAnthropic}
	groups := make([]AccountShareModeGroup, 0, len(platforms))
	for _, platform := range platforms {
		group, err := s.repo.GetModeGroup(ctx, platform)
		if err != nil {
			return nil, err
		}
		if group == nil || group.ID <= 0 {
			return nil, ErrAccountShareModeGroupUnavailable
		}
		groups = append(groups, AccountShareModeGroup{GroupID: group.ID, Platform: platform})
	}
	return groups, nil
}

func (s *AccountShareModeService) GetOpenAIModeGroup(ctx context.Context) (*Group, error) {
	return s.EnsureModeGroup(ctx, PlatformOpenAI)
}

func (s *AccountShareModeService) IsModeGroup(ctx context.Context, groupID int64) bool {
	ok, err := s.IsModeGroupChecked(ctx, groupID)
	return err == nil && ok
}

// IsModeGroupChecked 与 IsModeGroup 判定相同，但把查询错误暴露给调用方。
// IsModeGroup 会把"查询失败"和"不是模式分组"都折叠成 false，调用方无法区分；
// 需要缓存判定结果的调用方必须用这个版本，否则会把一次失败缓存成长期的错误答案。
func (s *AccountShareModeService) IsModeGroupChecked(ctx context.Context, groupID int64) (bool, error) {
	if s == nil || s.repo == nil || groupID <= 0 {
		return false, nil
	}
	return s.repo.IsModeGroup(ctx, groupID)
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
	if err := s.ensureProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(PlatformOpenAI, AccountLevelUnknown, ownerUserID), *proxyID); err != nil {
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
	if err := s.ensureProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(PlatformAnthropic, AccountLevelUnknown, ownerUserID), *proxyID); err != nil {
		return nil, err
	}
	return s.oauthService.GenerateAuthURL(ctx, proxyID)
}

// ListAvailableProxies 按账号平台与等级返回用户可选的平台代理。
// scope 为空平台时仅返回通用代理，空等级时仅返回所有等级可用的代理。
func (s *AccountShareModeService) ListAvailableProxies(ctx context.Context, scope ProxyScope) ([]ProxyWithAccountCount, error) {
	if s == nil || s.proxyRepo == nil {
		return []ProxyWithAccountCount{}, nil
	}
	return s.proxyRepo.ListActiveVisibleWithAccountCount(ctx, scope)
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
	if err := s.ensureProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(PlatformOpenAI, AccountLevelUnknown, ownerUserID), input.ProxyID); err != nil {
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
	if err := s.ensureProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(PlatformAnthropic, AccountLevelUnknown, ownerUserID), input.ProxyID); err != nil {
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
	if err := s.ensureProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(PlatformOpenAI, AccountLevelUnknown, ownerUserID), input.ProxyID); err != nil {
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
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	account := &Account{
		Name:                  accountName,
		Notes:                 normalizeAccountNotes(input.Notes),
		Platform:              PlatformOpenAI,
		AccountLevel:          NormalizeOpenAIAccountLevelWithConfigs(PlatformOpenAI, AccountLevelUnknown, credentials, extra, levelConfigs),
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
	if err := validateOwnedAccountSourceForPlatform(account.Platform, account.Type, account.Credentials, account.Extra); err != nil {
		return nil, err
	}
	listing := &AccountShareListing{
		OwnerUserID:            ownerUserID,
		Status:                 s.initialListingStatus(),
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
	normalizeAccountShareListingAccountLevelWithConfigs(created, levelConfigs)
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
	if err := s.ensureProxyAvailableForNewAccount(ctx, NewOwnedProxyScope(PlatformAnthropic, AccountLevelUnknown, ownerUserID), input.ProxyID); err != nil {
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
	if err := validateOwnedAccountSourceForPlatform(account.Platform, account.Type, account.Credentials, account.Extra); err != nil {
		return nil, err
	}
	listing := &AccountShareListing{
		OwnerUserID:             ownerUserID,
		Status:                  s.initialListingStatus(),
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

func (s *AccountShareModeService) CreateRoomFromOwnedAccount(ctx context.Context, ownerUserID int64, input CreateAccountShareRoomInput) (*AccountShareListing, error) {
	if ownerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.AccountID <= 0 {
		return nil, ErrAccountNotFound
	}
	roomName := strings.TrimSpace(input.RoomName)
	if roomName == "" {
		return nil, ErrAccountShareModeInvalidName
	}
	if err := validateAccountShareAccountName(roomName); err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, ErrAccountExternalPlacementInvalid.WithMetadata(map[string]string{"field": "idempotency_key"})
	}
	if s == nil || s.repo == nil || s.accountRepo == nil {
		return nil, ErrServiceUnavailable
	}
	roomRepo, ok := s.repo.(AccountShareRoomRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
		return nil, ErrAccountShareRoomOwnerMismatch
	}
	allowedModels := normalizeAllowedModelsOrDefaultForPlatform(account.Platform, input.AllowedModels)
	perUserConcurrency := normalizePositiveInt(input.PerUserConcurrency, AccountShareModeDefaultPerUserConcurrency)
	codex5hLimitPercent := normalizeCodexLimitPercent(input.Codex5hLimitPercent)
	codex7dLimitPercent := normalizeCodexLimitPercent(input.Codex7dLimitPercent)
	if account.Platform == PlatformAnthropic {
		codex5hLimitPercent = normalizeAnthropicLimitPercent(input.Anthropic5hLimitPercent)
		codex7dLimitPercent = normalizeAnthropicLimitPercent(input.Anthropic7dLimitPercent)
	}
	listing := &AccountShareListing{
		AccountID:               account.ID,
		AccountName:             account.Name,
		RoomName:                roomName,
		Platform:                strings.ToLower(strings.TrimSpace(account.Platform)),
		OwnerUserID:             ownerUserID,
		Status:                  s.initialListingStatus(),
		SeatLimit:               input.SeatLimit,
		RateMultiplier:          input.RateMultiplier,
		AllowedModels:           allowedModels,
		PerUserConcurrency:      perUserConcurrency,
		AccountConcurrency:      account.Concurrency,
		HourlyRate:              input.HourlyRate,
		HourlyFeeWaiverMinimum:  input.HourlyFeeWaiverMinimum,
		MinBalanceRequired:      minBalanceValue(input.MinBalanceRequired),
		CodexCLIOnly:            input.CodexCLIOnly,
		Codex5hLimitPercent:     codex5hLimitPercent,
		Codex7dLimitPercent:     codex7dLimitPercent,
		Anthropic5hLimitPercent: normalizeAnthropicLimitPercent(input.Anthropic5hLimitPercent),
		Anthropic7dLimitPercent: normalizeAnthropicLimitPercent(input.Anthropic7dLimitPercent),
	}
	if idempotencyRepo, ok := s.repo.(accountShareRoomCreationIdempotencyRepository); ok {
		existing, findErr := idempotencyRepo.FindRoomCreationByIdempotency(
			ctx,
			ownerUserID,
			account.ID,
			idempotencyKey,
			listing,
		)
		if findErr != nil {
			return nil, findErr
		}
		if existing != nil {
			s.enrichListingRuntime(ctx, existing)
			return existing, nil
		}
	}
	if normalizeAccountShareListingPlatform(account.Platform) == "" {
		return nil, ErrAccountPlatformUnsupported
	}
	if !account.IsSchedulableAt(time.Now().UTC()) {
		return nil, ErrAccountShareAccountUnavailable
	}
	for _, model := range allowedModels {
		if !account.IsModelSupported(model) {
			return nil, ErrAccountShareModeUnsupportedModel.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(account.ID, 10),
				"model":      model,
			})
		}
	}
	accountLevel := NormalizeAccountLevel(account.AccountLevel)
	var levelConfigs []OpenAIAccountLevelConfig
	if account.Platform == PlatformOpenAI {
		levelConfigs, err = s.openAIAccountLevelConfigs(ctx)
		if err != nil {
			return nil, err
		}
		accountLevel = NormalizeOpenAIAccountLevelWithConfigs(account.Platform, account.AccountLevel, account.Credentials, account.Extra, levelConfigs)
	}
	if accountLevel == AccountLevelUnknown {
		return nil, ErrAccountShareRoomUnknownLevel
	}
	if err := validateAccountShareListingConfig(
		input.SeatLimit,
		input.RateMultiplier,
		allowedModels,
		perUserConcurrency,
		account.Concurrency,
		input.HourlyRate,
		input.HourlyFeeWaiverMinimum,
		minBalanceValue(input.MinBalanceRequired),
		input.Codex5hLimitPercent,
		input.Codex7dLimitPercent,
	); err != nil {
		return nil, err
	}
	modeGroup, err := s.repo.EnsureModeGroup(ctx, account.Platform)
	if err != nil {
		return nil, err
	}
	if modeGroup == nil || modeGroup.ID <= 0 {
		return nil, ErrAccountShareModeGroupUnavailable
	}
	drained := false
	if account.ExternalPlacement != nil && account.ExternalPlacement.Target == AccountExternalPlacementPublicPool {
		// 公共号池账号建房间是收敛性操作：repo 层 CreateRoomFromOwnedAccount 在同一事务
		// 内原子改写 placement 与房间绑定，现有公共在途请求会自然结束。跳过在途空闲检查
		// （与 ConvertOwnedExternalPlacement 的 room 目标一致）——热门公共池账号在途
		// 恒 > 0，等待「归零」既不必要也等不到，只会把建房间永久卡死。
		drained, err = roomRepo.BeginExternalPlacementDrain(ctx, ownerUserID, account.ID)
		if err != nil {
			return nil, err
		}
		if drained {
			defer func() {
				if !drained {
					return
				}
				if restoreErr := roomRepo.RestoreExternalPlacementAfterDrain(context.WithoutCancel(ctx), ownerUserID, account.ID); restoreErr != nil {
					log.Printf("account_share_mode: restore placement after room creation failed: account=%d err=%v", account.ID, restoreErr)
				}
			}()
		}
	}
	listing.AccountLevel = accountLevel
	created, err := roomRepo.CreateRoomFromOwnedAccount(ctx, ownerUserID, account.ID, modeGroup.ID, idempotencyKey, listing)
	if err != nil {
		return nil, err
	}
	drained = false
	normalizeAccountShareListingAccountLevelWithConfigs(created, levelConfigs)
	s.enrichListingRuntime(ctx, created)
	s.schedulePostCreateConnectivityTest(created)
	return created, nil
}

func (s *AccountShareModeService) ListRoomAccounts(ctx context.Context, viewerUserID int64, viewerIsAdmin bool, listingID int64) ([]AccountShareRoomAccount, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	roomRepo, ok := s.repo.(AccountShareRoomRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return roomRepo.ListRoomAccounts(ctx, listingID, viewerUserID, viewerIsAdmin)
}

func (s *AccountShareModeService) AttachRoomAccounts(ctx context.Context, input BatchAccountShareRoomAccountsInput) (*BulkUpdateAccountsResult, error) {
	return s.mutateRoomAccounts(ctx, input, true)
}

func (s *AccountShareModeService) DetachRoomAccounts(ctx context.Context, input BatchAccountShareRoomAccountsInput) (*BulkUpdateAccountsResult, error) {
	return s.mutateRoomAccounts(ctx, input, false)
}

func (s *AccountShareModeService) mutateRoomAccounts(ctx context.Context, input BatchAccountShareRoomAccountsInput, attach bool) (*BulkUpdateAccountsResult, error) {
	if input.OwnerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.ListingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	accountIDs := uniquePositiveInt64s(input.AccountIDs)
	if len(accountIDs) == 0 || len(accountIDs) > AccountShareRoomBatchMaxAccounts {
		return nil, ErrAccountExternalPlacementInvalid.WithMetadata(map[string]string{"field": "account_ids"})
	}
	idempotencyKey, err := NormalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	roomRepo, ok := s.repo.(AccountShareRoomRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	input.AccountIDs = accountIDs
	input.IdempotencyKey = idempotencyKey
	if attach {
		err = roomRepo.AttachRoomAccountsAtomic(ctx, input)
	} else {
		if s.concurrencyService == nil {
			return nil, ErrServiceUnavailable
		}
		inFlightByAccount, concurrencyErr := s.concurrencyService.GetAccountConcurrencyBatch(ctx, accountIDs)
		if concurrencyErr != nil {
			return nil, concurrencyErr
		}
		for _, accountID := range accountIDs {
			if inFlightByAccount[accountID] > 0 {
				return nil, ErrAccountShareListingInUse.WithMetadata(map[string]string{
					"blocker":               "account_in_flight",
					"account_id":            strconv.FormatInt(accountID, 10),
					"in_flight_concurrency": strconv.Itoa(inFlightByAccount[accountID]),
				})
			}
		}
		var billing *AccountShareSeatBillingResult
		billing, err = roomRepo.DetachRoomAccountsAtomic(ctx, input)
		if err == nil {
			s.invalidateSeatBillingCaches(billing)
		}
	}
	if err != nil {
		return nil, err
	}
	result := &BulkUpdateAccountsResult{
		Success:    len(accountIDs),
		Failed:     0,
		SuccessIDs: append([]int64(nil), accountIDs...),
		FailedIDs:  make([]int64, 0),
		Results:    make([]BulkUpdateAccountResult, 0, len(accountIDs)),
	}
	for _, accountID := range accountIDs {
		result.Results = append(result.Results, BulkUpdateAccountResult{
			AccountID: accountID,
			Success:   true,
		})
	}
	return result, nil
}

func (s *AccountShareModeService) ListListings(ctx context.Context, viewerUserID int64, viewerIsAdmin bool, filters AccountShareListingFilters, params pagination.PaginationParams) ([]AccountShareListing, *pagination.PaginationResult, error) {
	return s.listListings(ctx, viewerUserID, viewerIsAdmin, filters, params, true)
}

func (s *AccountShareModeService) listListings(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	filters AccountShareListingFilters,
	params pagination.PaginationParams,
	projectForViewer bool,
) ([]AccountShareListing, *pagination.PaginationResult, error) {
	if viewerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, nil, ErrServiceUnavailable
	}
	normalized := normalizeListingFilters(filters)
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, nil, err
	}
	normalized.AccountLevels = levelConfigs
	normalized.ViewerIsAdmin = viewerIsAdmin
	// 普通用户浏览广场（tab=all）默认只看可用房间：状态 active + 账号健康 +
	// 有空余座位 + 无编辑锁。不可用的房间（已暂停/账号不可调度/无空位等）不应刷屏，
	// 用户切到「显示全部」才可见全部 active 房间。号主管理视图（mine/using/
	// history/archive）保持全量，号主需要看到自己的全部房间来维护。
	// 注入条件：普通用户 + tab=all + 未显式指定状态过滤器（status 为空）且未显式
	// 请求 available_only。用户选了「已上架」(status=active 不带 available_only) 表示
	// 想看全部上架房间（含暂时不可用），此时尊重其意图、不做可用性过滤。
	if !viewerIsAdmin &&
		normalized.Tab == AccountShareModeListingTabAll &&
		normalized.Status == "" &&
		!normalized.AvailableOnly {
		normalized.AvailableOnly = true
	}
	listings, result, err := s.repo.ListListings(ctx, viewerUserID, normalized, params)
	if err != nil {
		return nil, nil, err
	}
	normalizeAccountShareListingsAccountLevelWithConfigs(listings, levelConfigs)
	// History and archive projections are immutable snapshots. Enriching them
	// with the current account load would leak unrelated live state after an
	// account is detached, reused by another room, or the room is deleted.
	if normalized.Tab != AccountShareModeListingTabHistory &&
		normalized.Tab != AccountShareModeListingTabArchive {
		s.enrichListingsRuntime(ctx, listings)
	}
	if projectForViewer {
		for i := range listings {
			projectAccountShareListingForViewer(&listings[i], viewerUserID, viewerIsAdmin)
		}
	}
	return listings, result, nil
}

func (s *AccountShareModeService) ListMembershipHistory(
	ctx context.Context,
	consumerUserID int64,
	params pagination.PaginationParams,
) ([]AccountShareMembershipHistoryEntry, *pagination.PaginationResult, error) {
	if consumerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, nil, ErrServiceUnavailable
	}
	repo, ok := s.repo.(AccountShareHistoryRepository)
	if !ok {
		return nil, nil, ErrServiceUnavailable
	}
	return repo.ListMembershipHistory(ctx, consumerUserID, params)
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
	stats, err := s.usageProfileRepo.GetAccountShareRecommendationUsageProfile(
		ctx,
		viewerUserID,
		normalized.Platform,
		normalized.Model,
		startTime,
		endTime,
	)
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
		listings, pageResult, err := s.listListings(ctx, viewerUserID, viewerIsAdmin, AccountShareListingFilters{
			Tab:       AccountShareModeListingTabAll,
			Platform:  normalized.Platform,
			Status:    AccountShareListingStatusActive,
			SkipTotal: true,
		}, pagination.PaginationParams{Page: page, PageSize: AccountShareRecommendationPageSize}, false)
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
			scoreBreakdown := buildAccountShareRecommendationScoreBreakdown(listing, estimate, warnings)
			candidate := AccountShareRecommendationCandidate{
				Listing:        listing,
				Estimate:       estimate,
				Score:          scoreBreakdown.OverallScore,
				ScoreBreakdown: scoreBreakdown,
				Tags:           tags,
				Reasons:        reasons,
				Warnings:       warnings,
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
	candidates = accountShareRecommendationSelectCandidates(candidates, normalized.Limit)
	applyAccountShareRecommendationSmartLabels(candidates)
	for i := range candidates {
		candidates[i].Rank = i + 1
		if i == 0 {
			candidates[i].Tags = prependUniqueString(candidates[i].Tags, "最省额度")
			candidates[i].Reasons = prependUniqueString(candidates[i].Reasons, "按当前测算预计每小时额度最低")
		}
		projectAccountShareListingForViewer(&candidates[i].Listing, viewerUserID, viewerIsAdmin)
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
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	normalizeAccountShareListingAccountLevelWithConfigs(listing, levelConfigs)
	s.enrichListingRuntime(ctx, listing)
	return listing, nil
}

func projectAccountShareListingForViewer(
	listing *AccountShareListing,
	viewerUserID int64,
	viewerIsAdmin bool,
) {
	if listing == nil || viewerIsAdmin || (viewerUserID > 0 && listing.OwnerUserID == viewerUserID) {
		return
	}
	listing.AccountID = 0
	listing.AccountName = ""
	listing.AccountIdentityID = nil
	listing.Accounts = nil
	listing.ProxyID = nil
	listing.Proxy = nil
}

func (s *AccountShareModeService) GetVisibleListing(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
) (*AccountShareListing, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	visibleRepo, ok := s.repo.(accountShareVisibleListingRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	listing, err := visibleRepo.GetVisibleListingByID(ctx, listingID, viewerUserID, viewerIsAdmin)
	if err != nil {
		return nil, err
	}
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	normalizeAccountShareListingAccountLevelWithConfigs(listing, levelConfigs)
	s.enrichListingRuntime(ctx, listing)
	projectAccountShareListingForViewer(listing, viewerUserID, viewerIsAdmin)
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
		var err error
		rateMultiplier, err = s.ResolveOwnerSelfUseMultiplier(ctx)
		if err != nil {
			return AccountShareRecommendationEstimate{}, err
		}
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
	if !actorIsAdmin || !force {
		state, err := s.GetRoomManagementState(ctx, actorUserID, actorIsAdmin, listingID)
		if err != nil {
			return nil, err
		}
		blockers := state.Blockers
		// An existing edit lease is resolved atomically by BeginListingEdit:
		// the same actor/session may renew it, while another session is rejected.
		blockers.ValidEditSession = false
		if blockers.ConflictingOperation {
			return nil, ErrAccountShareRoomOperationConflict.WithMetadata(blockers.Metadata())
		}
		if blockers.Any() {
			return nil, ErrAccountShareListingInUse.WithMetadata(blockers.Metadata())
		}
	}
	listing, err := s.repo.BeginListingEdit(ctx, actorUserID, actorIsAdmin, listingID, BeginAccountShareListingEditInput{
		SessionID: sessionID,
		Force:     force,
		Expires:   time.Now().UTC().Add(AccountShareModeEditSessionTTL),
	})
	if err != nil {
		return nil, err
	}
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	normalizeAccountShareListingAccountLevelWithConfigs(listing, levelConfigs)
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
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	normalizeAccountShareListingAccountLevelWithConfigs(listing, levelConfigs)
	s.enrichListingRuntime(ctx, listing)
	return listing, nil
}

func (s *AccountShareModeService) UpdateListing(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input UpdateAccountShareListingInput) (*AccountShareListing, error) {
	if actorUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.ExpectedVersion == nil || *input.ExpectedVersion <= 0 {
		return nil, ErrAccountShareExpectedVersionRequired.WithMetadata(map[string]string{"field": "expected_version"})
	}
	if input.Status != nil {
		return nil, ErrAccountShareRoomLifecycleCommandRequired
	}
	if input.ProxyID != nil || input.Concurrency != nil {
		return nil, ErrAccountShareRoomAccountConfigUnsupported
	}
	if (input.Codex5hLimitPercent != nil && input.Anthropic5hLimitPercent != nil) ||
		(input.Codex7dLimitPercent != nil && input.Anthropic7dLimitPercent != nil) {
		return nil, ErrAccountShareRoomConflictingFields
	}
	if !hasAccountShareModeConfigUpdate(input) {
		return nil, ErrAccountShareRoomNoChanges
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.EditSessionID = strings.TrimSpace(input.EditSessionID)
	if input.ForceActiveEdit && !actorIsAdmin {
		return nil, ErrAccountShareForceAdminRequired
	}
	if input.Reason == "" {
		if input.ForceActiveEdit {
			return nil, ErrAccountShareForceReasonRequired.WithMetadata(map[string]string{"field": "reason"})
		}
		return nil, ErrAccountShareUpdateReasonRequired.WithMetadata(map[string]string{"field": "reason"})
	}
	if input.ForceActiveEdit {
		if !input.Confirmed {
			return nil, ErrAccountShareForceConfirmationRequired.WithMetadata(map[string]string{"field": "confirmed"})
		}
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
	if !actorIsAdmin && !isAccountShareModeModelOnlyUpdate(input) && !isAccountShareModeOwnerConfigUpdate(input) {
		return nil, ErrInsufficientPerms
	}
	// 这里刻意不再做「合约字段必须带 edit_session_id」的前置判定。
	// 仓储层对同一批字段有更完整的裁决：没带编辑锁时会先算一遍
	// accountShareListingUpdateProtectsConsumers（只降费 / 提并发 / 加模型 / 不伤现有席位
	// 地减席位即放行），算不过才要求编辑锁。前置判定的条件与那条免锁分支的进入条件
	// 逐字相同，等于把整条「消费者安全更新」堵死，房间一有人用就永远保存不了。
	if input.SeatLimit != nil && (*input.SeatLimit < AccountShareModeMinSeats || *input.SeatLimit > AccountShareModeMaxSeats) {
		return nil, ErrAccountShareModeInvalidSeats
	}
	if input.RateMultiplier != nil && invalidNonNegativeFloat(*input.RateMultiplier) {
		return nil, ErrAccountShareModeInvalidRateMultiplier
	}
	if input.PerUserConcurrency != nil && (*input.PerUserConcurrency <= 0 || *input.PerUserConcurrency > AccountShareModeMaxPerUserConcurrency) {
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
	if input.PerUserConcurrency != nil {
		current, err := s.repo.GetListingByID(ctx, listingID, actorUserID)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, ErrAccountShareListingNotFound
		}
		// 编辑弹窗是整表单提交，per_user_concurrency 永远随请求带上。只有真正改动它时
		// 才需要用房间容量卡上限，否则「只改房间名」也会被这道校验连坐。
		if *input.PerUserConcurrency != current.PerUserConcurrency {
			ceiling, err := s.roomConfiguredConcurrencyCeiling(ctx, actorUserID, actorIsAdmin, listingID)
			if err != nil {
				return nil, err
			}
			if ceiling <= 0 {
				ceiling = current.AccountConcurrency
			}
			if ceiling > 0 && *input.PerUserConcurrency > ceiling {
				return nil, ErrAccountShareModeInvalidConcurrency.WithMetadata(map[string]string{
					"field":   "per_user_concurrency",
					"maximum": strconv.Itoa(ceiling),
				})
			}
		}
	}
	listing, err := s.repo.UpdateListing(ctx, actorUserID, actorIsAdmin, listingID, input)
	if err != nil {
		return nil, err
	}
	levelConfigs, err := s.openAIAccountLevelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	normalizeAccountShareListingAccountLevelWithConfigs(listing, levelConfigs)
	s.enrichListingRuntime(ctx, listing)
	return listing, nil
}

// roomConfiguredConcurrencyCeiling 返回房间内账号「配置并发」之和，作为单用户并发的上限。
//
// 刻意不用 listing.AccountConcurrency：那个值在 SQL 里按健康度过滤过（限流、额度保护、
// 不可调度的账号都不计入），房间账号临时全部不可调度时会变成 0，于是任何取值都超标 ——
// 房主连改个房间名都会被打成「并发非法」，且提示是误导性的「不能超过 50」。
// 房间容量本身是配置属性，不该随账号的临时健康状态漂移。
func (s *AccountShareModeService) roomConfiguredConcurrencyCeiling(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
) (int, error) {
	repo, err := s.roomManagementStateRepository()
	if err != nil {
		// 仓储没有实现房间管理状态查询：返回 0 让调用方退回 listing.AccountConcurrency 兜底。
		// 这只是一道 UX 护栏（全局 50 上限与运行时派发限流都还在），不该因为一次辅助查询
		// 拿不到就把房主的配置保存整个打死。
		return 0, nil
	}
	state, err := repo.GetRoomManagementState(ctx, actorUserID, actorIsAdmin, listingID)
	if err != nil {
		return 0, err
	}
	if state == nil {
		return 0, nil
	}
	return state.ConfiguredTotalConcurrency, nil
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

func hasAccountShareModeContractUpdate(input UpdateAccountShareListingInput) bool {
	return input.SeatLimit != nil ||
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
		input.Anthropic7dLimitPercent != nil
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

	modelID := firstAllowedModel(listing.AllowedModels)
	testCtx, cancel := context.WithTimeout(ctx, accountShareConnectivityTestTimeout(modelID))
	defer cancel()
	result, err := s.accountTestService.RunTestBackground(testCtx, listing.AccountID, modelID)
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

func accountShareConnectivityTestTimeout(modelID string) time.Duration {
	if isOpenAIImageModel(strings.TrimSpace(modelID)) {
		return AccountShareModeImageConnectivityTestTimeout
	}
	return AccountShareModeConnectivityTestTimeout
}

func isTransientAccountShareConnectivityFailure(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	for _, marker := range []string{
		"context deadline exceeded",
		"context canceled",
		"request failed",
		"failed to read",
		"timeout",
		"timed out",
		"temporary",
		"temporarily",
		"try again",
		"connection reset",
		"unexpected eof",
		"rate limit",
		"rate_limit",
		"too many requests",
		"overloaded",
		"capacity",
		"returned 408",
		"returned 425",
		"returned 429",
		"returned 5",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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
	if s == nil || len(listings) == 0 {
		return
	}
	now := time.Now().UTC()
	listingIDs := make([]int64, 0, len(listings))
	for i := range listings {
		listings[i].AccountSampleScope = AccountShareAccountSampleScopeRepresentative
		if listings[i].ID > 0 {
			listingIDs = append(listingIDs, listings[i].ID)
		}
	}
	s.enrichListingsQuotaSummary(ctx, listings, listingIDs, now)
	if s.concurrencyService == nil {
		return
	}
	if roomRuntimeRepo, ok := s.repo.(accountShareRoomRuntimeAccountsRepository); ok {
		accountsByListing, err := roomRuntimeRepo.ListRoomRuntimeAccounts(ctx, listingIDs, now)
		if err != nil {
			log.Printf("[AccountShareMode] list room runtime accounts failed: %v", err)
			return
		}
		accountsByID := make(map[int64]AccountWithConcurrency)
		for _, accounts := range accountsByListing {
			for _, account := range accounts {
				if account.ID <= 0 {
					continue
				}
				accountsByID[account.ID] = account
			}
		}
		accounts := make([]AccountWithConcurrency, 0, len(accountsByID))
		for _, account := range accountsByID {
			accounts = append(accounts, account)
		}
		if len(accounts) == 0 {
			for i := range listings {
				listings[i].AccountConcurrency = 0
				listings[i].CurrentConcurrency = 0
				listings[i].RuntimeLoadKnown = true
			}
			return
		}
		loadByAccountID, err := s.concurrencyService.GetAccountsLoadBatch(ctx, accounts)
		if err != nil {
			log.Printf("[AccountShareMode] get room accounts runtime load failed: %v", err)
			return
		}
		for i := range listings {
			roomAccounts := accountsByListing[listings[i].ID]
			if len(roomAccounts) == 0 {
				listings[i].AccountConcurrency = 0
				listings[i].CurrentConcurrency = 0
				listings[i].RuntimeLoadKnown = true
				continue
			}
			totalConcurrency := 0
			currentConcurrency := 0
			loadComplete := true
			for _, account := range roomAccounts {
				totalConcurrency += account.MaxConcurrency
				load := loadByAccountID[account.ID]
				if load == nil {
					loadComplete = false
					continue
				}
				currentConcurrency += load.CurrentConcurrency
			}
			if !loadComplete {
				continue
			}
			listings[i].AccountConcurrency = totalConcurrency
			listings[i].CurrentConcurrency = currentConcurrency
			listings[i].RuntimeLoadKnown = true
		}
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
			listings[i].RuntimeLoadKnown = true
		}
	}
}

func (s *AccountShareModeService) enrichListingsQuotaSummary(
	ctx context.Context,
	listings []AccountShareListing,
	listingIDs []int64,
	now time.Time,
) {
	quotaRepo, ok := s.repo.(accountShareRoomQuotaRepository)
	if !ok || len(listingIDs) == 0 {
		return
	}
	snapshotsByListing, err := quotaRepo.ListRoomQuotaSnapshots(ctx, listingIDs, now)
	if err != nil {
		log.Printf("[AccountShareMode] list room quota snapshots failed: %v", err)
		return
	}
	for i := range listings {
		attachedCount := listings[i].AccountCount
		if attachedCount < 0 {
			attachedCount = 0
		}
		snapshots := snapshotsByListing[listings[i].ID]
		listings[i].QuotaSummary = &AccountShareQuotaSummary{
			Scope:         AccountShareQuotaSummaryScopeRoom,
			AttachedCount: attachedCount,
			EligibleCount: listings[i].HealthyAccountCount,
			Window5h:      buildAccountShareQuotaWindowSummary(snapshots, attachedCount, true),
			Window7d:      buildAccountShareQuotaWindowSummary(snapshots, attachedCount, false),
		}
	}
}

func buildAccountShareQuotaWindowSummary(
	snapshots []AccountShareRoomQuotaSnapshot,
	attachedCount int,
	fiveHour bool,
) AccountShareQuotaWindowSummary {
	summary := AccountShareQuotaWindowSummary{}
	totalUtilization := 0.0
	for i := range snapshots {
		progress := snapshots[i].Window7d
		if fiveHour {
			progress = snapshots[i].Window5h
		}
		if progress == nil || math.IsNaN(progress.Utilization) || math.IsInf(progress.Utilization, 0) {
			continue
		}
		utilization := progress.Utilization
		if summary.KnownCount == 0 || utilization < *summary.MinUtilization {
			value := utilization
			summary.MinUtilization = &value
		}
		if summary.KnownCount == 0 || utilization > *summary.MaxUtilization {
			value := utilization
			summary.MaxUtilization = &value
			summary.MaxUtilizationResetsAt = nil
			if progress.ResetsAt != nil {
				resetAt := progress.ResetsAt.UTC()
				summary.MaxUtilizationResetsAt = &resetAt
			}
		}
		totalUtilization += utilization
		summary.KnownCount++
	}
	if summary.KnownCount > 0 {
		average := totalUtilization / float64(summary.KnownCount)
		summary.AverageUtilization = &average
	}
	summary.Partial = summary.KnownCount < attachedCount
	return summary
}

func normalizeAccountShareListingAccountLevelWithConfigs(listing *AccountShareListing, configs []OpenAIAccountLevelConfig) {
	if listing == nil {
		return
	}
	listings := []AccountShareListing{*listing}
	normalizeAccountShareListingsAccountLevelWithConfigs(listings, configs)
	*listing = listings[0]
}

func normalizeAccountShareListingsAccountLevelWithConfigs(listings []AccountShareListing, configs []OpenAIAccountLevelConfig) {
	if len(listings) == 0 {
		return
	}
	normalizedConfigs := NormalizeOpenAIAccountLevelConfigs(configs)
	for i := range listings {
		if listings[i].Platform != PlatformOpenAI {
			continue
		}
		level := NormalizeAccountLevel(listings[i].AccountLevel)
		if OpenAIAccountLevelConfigByKeyIncludingDisabled(normalizedConfigs, level) == nil {
			if inferred := NormalizeOpenAIPlanAccountLevelWithConfigs(listings[i].AccountPlanType, normalizedConfigs); inferred != AccountLevelUnknown {
				level = inferred
			}
		}
		listings[i].AccountLevel = level
	}
}

type accountShareJoinPreparation struct {
	apiKey       *APIKey
	user         *User
	listing      *AccountShareListing
	ownerSelfUse bool
	now          time.Time
}

func (s *AccountShareModeService) CreateJoinIntent(
	ctx context.Context,
	consumerUserID, listingID int64,
	input CreateAccountShareJoinIntentInput,
) (*AccountShareJoinIntent, error) {
	_, err := s.prepareAccountShareJoin(
		ctx,
		consumerUserID,
		listingID,
		input.APIKeyID,
		input.IdleTimeoutMinutes,
	)
	if err != nil {
		return nil, err
	}
	if len(s.actionTokenSecret) < 32 {
		return nil, ErrServiceUnavailable
	}
	terms, err := s.repo.EnsureListingRevisionTerms(ctx, listingID)
	if err != nil {
		return nil, err
	}
	if terms == nil || terms.ListingRevisionID <= 0 || terms.RowVersion <= 0 {
		return nil, fmt.Errorf("account share listing %d immutable terms are unavailable", listingID)
	}
	// Legacy listings may receive their first immutable revision while the
	// intent is being created. Reload all join preconditions so the signed
	// confirmation is based on the same revision the user sees.
	preparation, err := s.prepareAccountShareJoin(
		ctx,
		consumerUserID,
		listingID,
		input.APIKeyID,
		input.IdleTimeoutMinutes,
	)
	if err != nil {
		return nil, err
	}
	if !accountShareListingMatchesJoinTerms(preparation.listing, *terms) {
		return nil, ErrAccountShareJoinTermsChanged.WithMetadata(map[string]string{
			"expected_version": fmt.Sprintf("%d", terms.RowVersion),
			"actual_version":   fmt.Sprintf("%d", preparation.listing.RowVersion),
		})
	}
	listing := preparation.listing
	now := preparation.now
	expiresAt := now.Add(AccountShareModeJoinIntentTTL)
	claims := accountShareJoinIntentTokenClaims{
		Action:             accountShareModeJoinIntentTokenAction,
		ConsumerID:         consumerUserID,
		ListingID:          listingID,
		APIKeyID:           input.APIKeyID,
		IdleTimeoutMinutes: input.IdleTimeoutMinutes,
		ExpectedVersion:    terms.RowVersion,
		ExpectedRevisionID: terms.ListingRevisionID,
		AcceptQueue:        input.AcceptQueue,
		Terms:              *terms,
		Nonce:              uuid.NewString(),
		IssuedAt:           now.UnixNano(),
		ExpiresAt:          expiresAt.UnixNano(),
	}
	token, err := s.signAccountShareActionToken(claims)
	if err != nil {
		return nil, err
	}
	queueMayBeRequired := !preparation.ownerSelfUse &&
		listing.CurrentMembershipID == nil &&
		(listing.QueueMembershipID != nil || listing.ActiveSeats >= listing.SeatLimit)
	// 跨房 ending 强制排队：同一 key 上若存在「其它房间」的退出结算中 membership，
	// 唯一索引 uq_account_share_memberships_live_api_key 会强制本次加入进入排队
	// （repo JoinListing 的 hasLiveMembership 把 active+ending 都视为占用）。
	// 这里把该情况提前纳入 queueMayBeRequired，让确认弹窗如实提示「需要预约队列」，
	// 避免用户以为可直接加入、提交时才被后端拒绝。
	if !preparation.ownerSelfUse && !queueMayBeRequired && listing.CurrentMembershipID == nil {
		memberships, listErr := s.repo.ListAPIKeyBindingMemberships(ctx, consumerUserID, input.APIKeyID)
		if listErr != nil {
			return nil, listErr
		}
		for _, membership := range memberships {
			if membership.Status == AccountShareMembershipStatusEnding && membership.ListingID != listingID {
				queueMayBeRequired = true
				break
			}
		}
	}
	return &AccountShareJoinIntent{
		ListingID:          listingID,
		APIKeyID:           input.APIKeyID,
		Token:              token,
		ExpiresAt:          expiresAt,
		ExpectedVersion:    terms.RowVersion,
		ExpectedRevisionID: terms.ListingRevisionID,
		AcceptQueue:        input.AcceptQueue,
		QueueMayBeRequired: queueMayBeRequired,
		Terms:              terms,
	}, nil
}

// JoinListing is retained as a source-compatible entry point for internal
// callers. Public joins require CompleteJoinListing and a signed join intent.
func (s *AccountShareModeService) JoinListing(ctx context.Context, consumerUserID, listingID, apiKeyID int64, idleTimeoutMinutes int) (*AccountShareMembership, error) {
	return s.CompleteJoinListing(ctx, consumerUserID, listingID, CompleteAccountShareJoinInput{
		APIKeyID:           apiKeyID,
		IdleTimeoutMinutes: idleTimeoutMinutes,
	})
}

func (s *AccountShareModeService) CompleteJoinListing(
	ctx context.Context,
	consumerUserID, listingID int64,
	input CompleteAccountShareJoinInput,
) (*AccountShareMembership, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if input.APIKeyID <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	if err := validateAccountShareIdleTimeoutMinutes(input.IdleTimeoutMinutes); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.IntentToken) == "" {
		return nil, ErrAccountShareJoinIntentRequired
	}
	now := time.Now().UTC()
	claims, err := s.validateJoinIntentToken(
		input.IntentToken,
		consumerUserID,
		listingID,
		input.APIKeyID,
		input.IdleTimeoutMinutes,
		now,
	)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion != claims.ExpectedVersion ||
		input.ExpectedRevisionID != claims.ExpectedRevisionID ||
		input.AcceptQueue != claims.AcceptQueue {
		return nil, ErrAccountShareJoinIntentInvalid
	}
	preparation, err := s.prepareAccountShareJoin(
		ctx,
		consumerUserID,
		listingID,
		input.APIKeyID,
		input.IdleTimeoutMinutes,
	)
	if err != nil {
		return nil, err
	}
	if !accountShareListingMatchesJoinTerms(preparation.listing, claims.Terms) {
		return nil, ErrAccountShareJoinTermsChanged.WithMetadata(map[string]string{
			"expected_version": fmt.Sprintf("%d", claims.ExpectedVersion),
			"actual_version":   fmt.Sprintf("%d", preparation.listing.RowVersion),
		})
	}

	result, err := s.repo.ProcessSeatBillingForJoin(ctx, now, consumerUserID, input.APIKeyID, listingID)
	if err != nil {
		log.Printf("account_share_mode: join failed stage=seat_billing user_id=%d listing_id=%d api_key_id=%d account_id=%d err=%v",
			consumerUserID,
			listingID,
			input.APIKeyID,
			preparation.listing.AccountID,
			err,
		)
		return nil, err
	}
	s.invalidateSeatBillingCaches(result)
	if _, err := s.processIdleMemberships(ctx, now, AccountShareIdleMembershipFilter{
		ConsumerUserID: consumerUserID,
		APIKeyID:       input.APIKeyID,
		ListingID:      listingID,
	}, AccountShareModeSeatBillingBatchSize); err != nil {
		return nil, err
	}
	issuedAt := time.Unix(0, claims.IssuedAt).UTC()
	membership, err := s.repo.JoinListing(ctx, AccountShareJoinRepositoryInput{
		ConsumerUserID:     consumerUserID,
		APIKeyID:           input.APIKeyID,
		ListingID:          listingID,
		IdleTimeoutMinutes: input.IdleTimeoutMinutes,
		ExpectedVersion:    claims.ExpectedVersion,
		ExpectedRevisionID: claims.ExpectedRevisionID,
		AcceptQueue:        claims.AcceptQueue,
		IntentIssuedAt:     issuedAt,
		IntentNonce:        claims.Nonce,
		AcceptedTerms:      &claims.Terms,
	})
	if err != nil {
		log.Printf("account_share_mode: join failed stage=repo_join user_id=%d listing_id=%d api_key_id=%d account_id=%d err=%v",
			consumerUserID,
			listingID,
			input.APIKeyID,
			preparation.listing.AccountID,
			err,
		)
		return nil, err
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, preparation.apiKey.Key)
	}
	if !preparation.ownerSelfUse {
		s.invalidateSeatBillingCaches(&AccountShareSeatBillingResult{DebitUserIDs: []int64{consumerUserID}})
	}
	return membership, nil
}

func (s *AccountShareModeService) prepareAccountShareJoin(
	ctx context.Context,
	consumerUserID, listingID, apiKeyID int64,
	idleTimeoutMinutes int,
) (*accountShareJoinPreparation, error) {
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
	if err := validateAccountShareModeAPIKey(apiKey); err != nil {
		return nil, err
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
	if listing.QueueStatus == AccountShareMembershipStatusEnding {
		return nil, ErrAccountShareMembershipEnding
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
		return nil, ErrAccountShareAccountUnavailable
	}
	if !ownerSelfUse && user.Balance < listing.MinBalanceRequired {
		return nil, ErrAccountShareBalanceBelowMinimum
	}
	return &accountShareJoinPreparation{
		apiKey:       apiKey,
		user:         user,
		listing:      listing,
		ownerSelfUse: ownerSelfUse,
		now:          now,
	}, nil
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
	if err := s.ensureAPIKeyOwnedByUser(ctx, consumerUserID, apiKeyID); err != nil {
		return nil, err
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
	if err := s.ensureAPIKeyOwnedByUser(ctx, consumerUserID, apiKeyID); err != nil {
		return nil, err
	}
	return s.repo.ListMembershipQueue(ctx, consumerUserID, apiKeyID)
}

func (s *AccountShareModeService) GetAPIKeyBindingStatus(ctx context.Context, consumerUserID, apiKeyID int64) (*AccountShareAPIKeyBindingStatus, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if apiKeyID <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	if s == nil || s.repo == nil || s.apiKeyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	if err := s.ensureAPIKeyOwnedByUser(ctx, consumerUserID, apiKeyID); err != nil {
		return nil, err
	}

	memberships, err := s.repo.ListAPIKeyBindingMemberships(ctx, consumerUserID, apiKeyID)
	if err != nil {
		return nil, err
	}
	status := &AccountShareAPIKeyBindingStatus{
		APIKeyID:    apiKeyID,
		Memberships: memberships,
	}
	for i := range memberships {
		switch memberships[i].Status {
		case AccountShareMembershipStatusActive:
			status.ActiveCount++
		case AccountShareMembershipStatusQueued:
			status.QueuedCount++
		case AccountShareMembershipStatusEnding:
			status.EndingCount++
		default:
			return nil, fmt.Errorf(
				"unexpected account-share binding membership status %q for api key %d",
				memberships[i].Status,
				apiKeyID,
			)
		}
	}
	status.BlockingCount = status.ActiveCount + status.QueuedCount + status.EndingCount
	return status, nil
}

func (s *AccountShareModeService) ensureAPIKeyOwnedByUser(ctx context.Context, userID, apiKeyID int64) error {
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return err
	}
	if apiKey.UserID != userID {
		return ErrInsufficientPerms
	}
	return nil
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
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	membership, err := s.repo.GetMembershipForEnd(ctx, consumerUserID, membershipID)
	if err != nil {
		return nil, err
	}
	if membership == nil || membership.ID != membershipID || membership.ConsumerUserID != consumerUserID {
		return nil, ErrAccountShareMembershipNotFound
	}
	switch membership.Status {
	case AccountShareMembershipStatusActive, AccountShareMembershipStatusQueued:
	case AccountShareMembershipStatusEnding:
		if strings.TrimSpace(membership.EndingOperationID) == "" {
			return nil, ErrAccountShareEndStateConflict
		}
	default:
		return nil, ErrAccountShareEndStateConflict
	}
	expiresAt := time.Now().UTC().Add(AccountShareModeEndMembershipTokenTTL)
	operationID := strings.TrimSpace(membership.EndingOperationID)
	if operationID == "" {
		operationID = uuid.NewString()
	}
	claims := accountShareEndMembershipTokenClaims{
		Action:           accountShareModeEndMembershipTokenAction,
		ConsumerID:       consumerUserID,
		MembershipID:     membershipID,
		MembershipStatus: membership.Status,
		OperationID:      operationID,
		Nonce:            uuid.NewString(),
		ExpiresAt:        expiresAt.Unix(),
	}
	token, err := s.signEndMembershipToken(claims)
	if err != nil {
		return nil, err
	}
	return &AccountShareEndMembershipToken{
		MembershipID: membershipID,
		OperationID:  operationID,
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
	// 单阶段结束：confirmationToken 仅为旧前端兼容而保留，不再校验——
	// 结束动作按成员当前状态幂等收口，没有任何"确认后状态变化"可冲突。
	_ = confirmationToken
	membership, billing, err := s.repo.BeginMembershipEnd(ctx, BeginAccountShareMembershipEndInput{
		ConsumerUserID: consumerUserID,
		MembershipID:   membershipID,
		OperationID:    uuid.NewString(),
	})
	if err != nil {
		return nil, err
	}
	if membership == nil {
		return nil, ErrServiceUnavailable
	}
	s.invalidateMembershipEndCaches(ctx, membership, billing)
	if membership.Status == AccountShareMembershipStatusEnded {
		return membership, nil
	}
	if membership.Status != AccountShareMembershipStatusEnding {
		return nil, ErrAccountShareEndStateConflict
	}
	operationID := strings.TrimSpace(membership.EndingOperationID)
	if operationID == "" {
		return nil, ErrAccountShareEndStateConflict
	}
	hasLease, leaseErr := s.hasActiveMembershipLease(ctx, membership.ID)
	if leaseErr != nil || hasLease {
		// Once the durable ending fence exists, an unavailable Redis lease
		// check must never degrade to synchronous settlement.
		return membership, nil
	}
	finalizedMembership, finalizedBilling, finalized, err := s.repo.FinalizeMembershipEnd(ctx, membership.ID, operationID)
	if err != nil {
		return nil, err
	}
	if !finalized {
		return finalizedMembership, nil
	}
	s.invalidateMembershipEndCaches(ctx, finalizedMembership, finalizedBilling)
	return finalizedMembership, nil
}

func (s *AccountShareModeService) hasActiveMembershipLease(ctx context.Context, membershipID int64) (bool, error) {
	if s == nil || s.concurrencyService == nil || s.concurrencyService.cache == nil || membershipID <= 0 {
		return false, ErrServiceUnavailable
	}
	cache, ok := s.concurrencyService.cache.(accountShareMembershipConcurrencyCache)
	if !ok {
		return false, ErrServiceUnavailable
	}
	count, err := cache.GetAccountShareMembershipConcurrency(ctx, membershipID)
	if err != nil {
		return false, err
	}
	if count < 0 {
		return false, fmt.Errorf("invalid account share membership lease count: %d", count)
	}
	return count > 0, nil
}

func (s *AccountShareModeService) invalidateMembershipEndCaches(
	ctx context.Context,
	membership *AccountShareMembership,
	billing *AccountShareSeatBillingResult,
) {
	if s == nil {
		return
	}
	if membership != nil && s.authCacheInvalidator != nil && membership.APIKeyID > 0 && s.apiKeyRepo != nil {
		if key, keyErr := s.apiKeyRepo.GetByID(ctx, membership.APIKeyID); keyErr == nil && key != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key.Key)
		}
	}
	s.invalidateSeatBillingCaches(billing)
}

func accountShareJoinTermsFromListing(listing *AccountShareListing, revisionID int64) AccountShareListingTermsSnapshot {
	if listing == nil {
		return AccountShareListingTermsSnapshot{}
	}
	return AccountShareListingTermsSnapshot{
		ListingRevisionID:       revisionID,
		RowVersion:              listing.RowVersion,
		SchemaVersion:           1,
		RoomName:                listing.RoomName,
		Status:                  listing.Status,
		SeatLimit:               listing.SeatLimit,
		RateMultiplier:          listing.RateMultiplier,
		AllowedModels:           append([]string(nil), listing.AllowedModels...),
		PerUserConcurrency:      listing.PerUserConcurrency,
		HourlyRate:              listing.HourlyRate,
		HourlyFeeWaiverMinimum:  listing.HourlyFeeWaiverMinimum,
		MinBalanceRequired:      listing.MinBalanceRequired,
		CodexCLIOnly:            listing.CodexCLIOnly,
		Codex5hLimitPercent:     listing.Codex5hLimitPercent,
		Codex7dLimitPercent:     listing.Codex7dLimitPercent,
		Anthropic5hLimitPercent: listing.Anthropic5hLimitPercent,
		Anthropic7dLimitPercent: listing.Anthropic7dLimitPercent,
	}
}

func accountShareListingMatchesJoinTerms(listing *AccountShareListing, terms AccountShareListingTermsSnapshot) bool {
	if listing == nil ||
		listing.RowVersion != terms.RowVersion ||
		listing.RoomName != terms.RoomName ||
		listing.Status != terms.Status ||
		listing.SeatLimit != terms.SeatLimit ||
		listing.RateMultiplier != terms.RateMultiplier ||
		listing.PerUserConcurrency != terms.PerUserConcurrency ||
		listing.HourlyRate != terms.HourlyRate ||
		listing.HourlyFeeWaiverMinimum != terms.HourlyFeeWaiverMinimum ||
		listing.MinBalanceRequired != terms.MinBalanceRequired ||
		listing.CodexCLIOnly != terms.CodexCLIOnly ||
		listing.Codex5hLimitPercent != terms.Codex5hLimitPercent ||
		listing.Codex7dLimitPercent != terms.Codex7dLimitPercent ||
		listing.Anthropic5hLimitPercent != terms.Anthropic5hLimitPercent ||
		listing.Anthropic7dLimitPercent != terms.Anthropic7dLimitPercent {
		return false
	}
	currentRevisionID := int64(0)
	if listing.CurrentRevisionID != nil {
		currentRevisionID = *listing.CurrentRevisionID
	}
	if currentRevisionID != terms.ListingRevisionID {
		return false
	}
	leftModels := normalizeAllowedModels(listing.AllowedModels)
	rightModels := normalizeAllowedModels(terms.AllowedModels)
	if len(leftModels) != len(rightModels) {
		return false
	}
	for index := range leftModels {
		if leftModels[index] != rightModels[index] {
			return false
		}
	}
	return true
}

func (s *AccountShareModeService) signAccountShareActionToken(value any) (string, error) {
	if len(s.actionTokenSecret) < 32 {
		return "", ErrServiceUnavailable
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal account share action token: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.actionTokenSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	signature := mac.Sum(nil)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *AccountShareModeService) decodeAccountShareActionToken(token string, target any, invalidError error) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return invalidError
	}
	if len(s.actionTokenSecret) < 32 {
		return ErrServiceUnavailable
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return invalidError
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return invalidError
	}
	mac := hmac.New(sha256.New, s.actionTokenSecret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return invalidError
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return invalidError
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return invalidError
	}
	return nil
}

func (s *AccountShareModeService) validateJoinIntentToken(
	token string,
	consumerUserID, listingID, apiKeyID int64,
	idleTimeoutMinutes int,
	now time.Time,
) (accountShareJoinIntentTokenClaims, error) {
	var claims accountShareJoinIntentTokenClaims
	if strings.TrimSpace(token) == "" {
		return claims, ErrAccountShareJoinIntentRequired
	}
	if err := s.decodeAccountShareActionToken(token, &claims, ErrAccountShareJoinIntentInvalid); err != nil {
		return claims, err
	}
	if claims.Action != accountShareModeJoinIntentTokenAction ||
		claims.ConsumerID != consumerUserID ||
		claims.ListingID != listingID ||
		claims.APIKeyID != apiKeyID ||
		claims.IdleTimeoutMinutes != idleTimeoutMinutes ||
		claims.ExpectedVersion <= 0 ||
		claims.Terms.RowVersion != claims.ExpectedVersion ||
		claims.Terms.ListingRevisionID != claims.ExpectedRevisionID ||
		claims.IssuedAt <= 0 ||
		claims.ExpiresAt <= claims.IssuedAt ||
		claims.ExpiresAt <= now.UnixNano() ||
		time.Duration(claims.ExpiresAt-claims.IssuedAt) > AccountShareModeJoinIntentTTL ||
		claims.IssuedAt > now.Add(30*time.Second).UnixNano() {
		return claims, ErrAccountShareJoinIntentInvalid
	}
	if _, err := uuid.Parse(claims.Nonce); err != nil {
		return claims, ErrAccountShareJoinIntentInvalid
	}
	return claims, nil
}

func (s *AccountShareModeService) signEndMembershipToken(claims accountShareEndMembershipTokenClaims) (string, error) {
	return s.signAccountShareActionToken(claims)
}

func (s *AccountShareModeService) validateEndMembershipToken(token string, consumerUserID, membershipID int64, now time.Time) (accountShareEndMembershipTokenClaims, error) {
	var claims accountShareEndMembershipTokenClaims
	token = strings.TrimSpace(token)
	if token == "" {
		return claims, ErrAccountShareEndTokenRequired
	}
	if err := s.decodeAccountShareActionToken(token, &claims, ErrAccountShareEndTokenInvalid); err != nil {
		return claims, err
	}
	if claims.Action != accountShareModeEndMembershipTokenAction ||
		claims.ConsumerID != consumerUserID ||
		claims.MembershipID != membershipID ||
		(claims.MembershipStatus != AccountShareMembershipStatusActive &&
			claims.MembershipStatus != AccountShareMembershipStatusQueued &&
			claims.MembershipStatus != AccountShareMembershipStatusEnding) ||
		strings.TrimSpace(claims.Nonce) == "" ||
		claims.ExpiresAt <= now.Unix() {
		return claims, ErrAccountShareEndTokenInvalid
	}
	if _, err := uuid.Parse(claims.OperationID); err != nil {
		return claims, ErrAccountShareEndTokenInvalid
	}
	if _, err := uuid.Parse(claims.Nonce); err != nil {
		return claims, ErrAccountShareEndTokenInvalid
	}
	return claims, nil
}

func validateAccountShareIdleTimeoutMinutes(value int) error {
	if value <= 0 || value > AccountShareModeMaxIdleTimeoutMinutes {
		return ErrAccountShareModeInvalidIdleTimeout
	}
	return nil
}

func validateAccountShareModeAPIKey(apiKey *APIKey) error {
	if apiKey == nil {
		return ErrAPIKeyNotFound
	}
	if apiKey.Status != StatusAPIKeyActive {
		switch apiKey.Status {
		case StatusAPIKeyExpired:
			return ErrAPIKeyExpired
		case StatusAPIKeyQuotaExhausted:
			return ErrAPIKeyQuotaExhausted
		default:
			return ErrAPIKeyInactive
		}
	}
	if apiKey.IsExpired() {
		return ErrAPIKeyExpired
	}
	if apiKey.IsQuotaExhausted() {
		return ErrAPIKeyQuotaExhausted
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
			rebound, err := s.rebindMembershipToHealthyRoomAccount(ctx, membership, now)
			if err != nil {
				return nil, nil, err
			}
			if rebound {
				continue
			}
			afterRank = membership.QueueRank
			result, suspended, err := s.suspendMembershipForDispatchFailure(ctx, membership, now)
			if err != nil {
				return nil, nil, err
			}
			if !suspended {
				lastErr = ErrNoAvailableAccounts
				break
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

func (s *AccountShareModeService) rebindMembershipToHealthyRoomAccount(ctx context.Context, membership *AccountShareMembership, now time.Time) (bool, error) {
	if s == nil || membership == nil || membership.ID <= 0 || membership.AccountID <= 0 {
		return false, nil
	}
	roomRepo, ok := s.repo.(AccountShareRoomRepository)
	if !ok {
		return false, nil
	}
	active, err := s.membershipHasActiveConcurrency(ctx, membership.ID)
	if err != nil {
		return false, err
	}
	if active {
		return false, nil
	}
	return roomRepo.RebindMembershipToHealthyRoomAccount(ctx, membership.ID, membership.AccountID, now)
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
		activationErr := err
		if !errors.Is(activationErr, ErrAccountShareAPIKeyAlreadyBound) && !errors.Is(activationErr, ErrAccountShareListingNotFound) {
			return nil, nil, activationErr
		}
		membership, listing, err = s.repo.GetActiveMembershipForRequest(ctx, userID, apiKeyID, groupID)
		if err == nil && membership != nil && listing != nil {
			return membership, listing, nil
		}
		if err != nil && !errors.Is(err, ErrAccountShareListingNotFound) {
			return nil, nil, err
		}
		return nil, nil, activationErr
	}
	return membership, listing, nil
}

func (s *AccountShareModeService) deferMembershipForDispatchRetry(ctx context.Context, requestCtx AccountShareModeRequestContext, membership *AccountShareMembership, now time.Time) (bool, error) {
	if s == nil || membership == nil || membership.ID <= 0 {
		return false, nil
	}
	result, suspended, err := s.suspendMembershipForDispatchFailure(ctx, membership, now)
	if err != nil {
		return false, err
	}
	if !suspended {
		return false, nil
	}
	s.invalidateSeatBillingCaches(result)
	if requestCtx.state != nil {
		requestCtx.state.clear()
	}
	return true, nil
}

func (s *AccountShareModeService) suspendMembershipForDispatchFailure(ctx context.Context, membership *AccountShareMembership, now time.Time) (*AccountShareSeatBillingResult, bool, error) {
	if s == nil || s.repo == nil || membership == nil || membership.ID <= 0 {
		return &AccountShareSeatBillingResult{}, false, nil
	}
	active, err := s.membershipHasActiveConcurrency(ctx, membership.ID)
	if err != nil {
		return nil, false, err
	}
	if active {
		log.Printf("account_share_mode: dispatch suspension skipped for active membership: membership_id=%d", membership.ID)
		return &AccountShareSeatBillingResult{}, false, nil
	}
	suspended, billing, err := s.repo.SuspendMembershipForDispatchFailure(ctx, membership.ID, now, now.Add(AccountShareModeDispatchCooldown))
	if err != nil {
		return nil, false, err
	}
	if suspended == nil {
		return &AccountShareSeatBillingResult{}, false, nil
	}
	return billing, true, nil
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
	ended, billing, err := s.repo.EndIdleMembership(ctx, membership.ID, *deadline)
	if err != nil {
		if errors.Is(err, ErrAccountShareListingNotFound) {
			return true, nil
		}
		return false, err
	}
	if ended != nil {
		s.invalidateSeatBillingCaches(billing)
	}
	return true, nil
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
		if listing.AccountID > 0 && listing.RepresentativeAccountConcurrency <= 0 {
			return true
		}
		if listing.RepresentativeAccountAutoPauseOnExpired &&
			listing.AccountExpiresAt != nil &&
			!now.Before(*listing.AccountExpiresAt) {
			return true
		}
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
	if s == nil ||
		s.accountTestService == nil ||
		s.rateLimitService == nil ||
		s.accountRepo == nil ||
		listing == nil ||
		listing.ID <= 0 ||
		listing.OwnerUserID <= 0 ||
		listing.Status != AccountShareListingStatusValidating {
		return
	}
	validationListing := *listing
	validationListing.AllowedModels = append([]string(nil), listing.AllowedModels...)
	timeout := accountShareConnectivityTestTimeout(firstAllowedModel(validationListing.AllowedModels)) + time.Minute
	go func() {
		testCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if _, _, err := s.finalizeRoomValidation(testCtx, &validationListing, 0, true, nil); err != nil {
			log.Printf(
				"account_share_mode: finalize post-create room validation failed: listing_id=%d err=%v",
				validationListing.ID,
				err,
			)
		}
	}()
}

func (s *AccountShareModeService) AcquireMembershipSlot(ctx context.Context, membershipID int64, maxConcurrency int) (*AcquireResult, error) {
	if s == nil ||
		s.repo == nil ||
		s.concurrencyService == nil ||
		membershipID <= 0 ||
		maxConcurrency <= 0 {
		return nil, ErrAccountShareRuntimeLeaseUnavailable
	}
	result, err := s.concurrencyService.AcquireAccountShareMembershipSlot(ctx, membershipID, maxConcurrency)
	if err != nil || result == nil || !result.Acquired {
		return result, err
	}
	if result.ReleaseFunc == nil || result.RefreshFunc == nil || result.LeaseTTL <= 0 {
		if result.ReleaseFunc != nil {
			result.ReleaseFunc()
		}
		return nil, ErrAccountShareRuntimeLeaseUnavailable
	}
	underlyingRelease := result.ReleaseFunc
	if err := s.forceTouchMembershipLastRequest(membershipID, time.Now().UTC()); err != nil {
		if underlyingRelease != nil {
			underlyingRelease()
		}
		return nil, err
	}
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	var releaseOnce sync.Once
	go s.runMembershipHeartbeat(ctx.Done(), membershipID, AccountShareModeRequestHeartbeatInterval, stopHeartbeat, heartbeatDone)
	result.ReleaseFunc = func() {
		releaseOnce.Do(func() {
			close(stopHeartbeat)
			<-heartbeatDone
			completedAt := time.Now().UTC()
			if err := s.forceTouchMembershipLastRequest(membershipID, completedAt); err != nil {
				log.Printf("account_share_mode: touch membership completion failed: membership_id=%d err=%v", membershipID, err)
			}
			if underlyingRelease != nil {
				underlyingRelease()
			}
		})
	}
	return result, nil
}

func (s *AccountShareModeService) runMembershipHeartbeat(ctxDone <-chan struct{}, membershipID int64, interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	if interval <= 0 {
		interval = AccountShareModeRequestHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.forceTouchMembershipLastRequest(membershipID, time.Now().UTC()); err != nil {
				log.Printf("account_share_mode: membership heartbeat failed: membership_id=%d err=%v", membershipID, err)
			}
		case <-stop:
			return
		case <-ctxDone:
			return
		}
	}
}

func (s *AccountShareModeService) forceTouchMembershipLastRequest(membershipID int64, at time.Time) error {
	if s == nil || s.repo == nil || membershipID <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), AccountShareModeMembershipTouchTimeout)
	defer cancel()
	now := at.UTC()
	if err := s.repo.TouchMembershipLastRequest(ctx, membershipID, now); err != nil {
		return err
	}
	return nil
}

func (s *AccountShareModeService) ResolvePolicy(ctx context.Context) (*AccountSharePolicy, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	return s.repo.ResolvePolicy(ctx)
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
	if perUserConcurrency <= 0 ||
		perUserConcurrency > AccountShareModeMaxPerUserConcurrency ||
		accountConcurrency <= 0 ||
		accountConcurrency > AccountShareModeMaxAccountConcurrency ||
		perUserConcurrency > accountConcurrency {
		return ErrAccountShareModeInvalidConcurrency
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

func AccountShareRoomQueueLimit(seatLimit int) int {
	limit := seatLimit * AccountShareModeRoomQueuePerSeat
	if limit < AccountShareModeRoomQueueMinimum {
		return AccountShareModeRoomQueueMinimum
	}
	if limit > AccountShareModeRoomQueueMaximum {
		return AccountShareModeRoomQueueMaximum
	}
	return limit
}

func validateAccountShareAccountName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if utf8.RuneCountInString(name) > AccountShareRoomNameMaxRunes ||
		strings.IndexFunc(name, unicode.IsSpace) >= 0 {
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
	// 展示既有房源的代理快照：附带遗留归属豁免，让老用户绑定的自有代理仍可见。
	proxy, err := s.proxyRepo.GetVisibleByID(ctx, NewOwnedProxyScope(listing.Platform, listing.AccountLevel, listing.OwnerUserID), *listing.ProxyID)
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

func (s *AccountShareModeService) ensureProxyAvailableForNewAccount(ctx context.Context, scope ProxyScope, proxyID int64) error {
	proxy, err := s.loadVisibleActiveProxyForScope(ctx, scope, proxyID)
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

func (s *AccountShareModeService) loadVisibleActiveProxyForScope(ctx context.Context, scope ProxyScope, proxyID int64) (*Proxy, error) {
	if proxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if s == nil || s.proxyRepo == nil {
		return nil, ErrServiceUnavailable
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
	case AccountShareModeListingTabUsing,
		AccountShareModeListingTabHistory,
		AccountShareModeListingTabAll,
		AccountShareModeListingTabMine,
		AccountShareModeListingTabArchive:
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
	case AccountShareListingStatusActive,
		AccountShareListingStatusPaused,
		AccountShareListingStatusDisabled,
		AccountShareListingStatusSuspended,
		"all":
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
		AccountLevels: filters.AccountLevels,
	}
}

func normalizeAccountShareListingPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformOpenAI:
		return PlatformOpenAI
	case PlatformAnthropic:
		return PlatformAnthropic
	case PlatformOpencode:
		return PlatformOpencode
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
	textInputTokens := accountShareNonNegativeTokenDifference(stats.TotalInputTokens, stats.TotalImageInputTokens)
	textOutputTokens := accountShareNonNegativeTokenDifference(stats.TotalOutputTokens, stats.TotalImageOutputTokens)
	inputTokens, cappedInput := accountShareRecommendationProfileCeilPerRequest(textInputTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	outputTokens, cappedOutput := accountShareRecommendationProfileCeilPerRequest(textOutputTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	cacheCreationTokens, cappedCacheCreation := accountShareRecommendationProfileCeilPerRequest(stats.TotalCacheCreationTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	cacheReadTokens, cappedCacheRead := accountShareRecommendationProfileCeilPerRequest(stats.TotalCacheReadTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
	imageInputTokens, cappedImageInput := accountShareRecommendationProfileCeilPerRequest(stats.TotalImageInputTokens, stats.TotalRequests, AccountShareRecommendationMaxTokensPerUnit)
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
		Capped:                        cappedRequests || cappedInput || cappedOutput || cappedCacheCreation || cappedCacheRead || cappedImageInput || cappedImageOutput,
		TotalRequests:                 stats.TotalRequests,
		ActiveHourBuckets:             stats.ActiveHourBuckets,
		RequestCount:                  requestCount,
		ActiveHours:                   activeHours,
		InputTokensPerRequest:         inputTokens,
		OutputTokensPerRequest:        outputTokens,
		CacheCreationTokensPerRequest: cacheCreationTokens,
		CacheReadTokensPerRequest:     cacheReadTokens,
		ImageInputTokensPerRequest:    imageInputTokens,
		ImageOutputTokensPerRequest:   imageOutputTokens,
	}
}

func accountShareNonNegativeTokenDifference(total, component int64) int64 {
	if total <= 0 {
		return 0
	}
	if component <= 0 {
		return total
	}
	if total <= component {
		return 0
	}
	return total - component
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

func accountShareListingAllowsModel(listing *AccountShareListing, model string) bool {
	if listing == nil {
		return false
	}
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

func accountShareListingSupportsRecommendationModel(listing AccountShareListing, model string) bool {
	return accountShareListingAllowsModel(&listing, model)
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
	if !listing.RuntimeLoadKnown {
		warnings = append(warnings, "实时并发状态暂不可用，推荐分数未计入并发余量")
	}
	if remainingSeats <= 0 && !estimate.OwnerSelfUse {
		warnings = append(warnings, "当前没有空闲席位，可能需要排队等待")
	}
	return tags, reasons, warnings
}

func buildAccountShareRecommendationScoreBreakdown(listing AccountShareListing, estimate AccountShareRecommendationEstimate, warnings []string) AccountShareRecommendationScoreBreakdown {
	remainingSeats := listing.SeatLimit - listing.ActiveSeats
	if remainingSeats < 0 {
		remainingSeats = 0
	}
	accountConcurrency := listing.AccountConcurrency
	if accountConcurrency <= 0 {
		accountConcurrency = AccountShareModeDefaultAccountConcurrency
	}
	availableConcurrency := 0
	if listing.RuntimeLoadKnown {
		availableConcurrency = accountConcurrency - listing.CurrentConcurrency
		if availableConcurrency < 0 {
			availableConcurrency = 0
		}
	}

	costSavingScore := 100.0
	if estimate.TotalCost > 0 {
		costSavingScore -= math.Min(estimate.TotalCost*120, 72)
	}
	if estimate.RequestCost > 0 {
		hourlyShare := estimate.HourlyNetCost / math.Max(estimate.RequestCost+estimate.HourlyNetCost, 0.0000001)
		costSavingScore -= math.Min(hourlyShare*20, 20)
	}
	if estimate.EffectiveRateMultiplier <= 1 {
		costSavingScore += 8
	}
	if estimate.WaiverEligible {
		costSavingScore += 7
	}
	if estimate.OwnerSelfUse {
		costSavingScore += 12
	}

	stabilityScore := 55.0
	stabilityScore += math.Min(float64(listing.PerUserConcurrency), 12) * 2.2
	if listing.RuntimeLoadKnown {
		stabilityScore += math.Min(float64(availableConcurrency), 30) * 0.75
	}
	if listing.RatingCount > 0 {
		stabilityScore += math.Min(listing.RatingAvg, 10) * 1.7
		stabilityScore += math.Min(float64(listing.RatingCount), 30) * 0.35
	}
	if listing.RateLimitedAt != nil || listing.OverloadUntil != nil || listing.TempUnschedulableUntil != nil {
		stabilityScore -= 18
	}

	seatRatio := 0.0
	if listing.SeatLimit > 0 {
		seatRatio = float64(remainingSeats) / float64(listing.SeatLimit)
	}
	availabilityScore := 45.0 + seatRatio*35
	if listing.RuntimeLoadKnown {
		concurrencyRatio := float64(availableConcurrency) / math.Max(float64(accountConcurrency), 1)
		availabilityScore += math.Min(concurrencyRatio, 1) * 20
	}
	if remainingSeats <= 0 && !estimate.OwnerSelfUse {
		availabilityScore -= 28
	}
	if listing.CurrentMembershipID != nil || listing.QueueMembershipID != nil {
		availabilityScore += 6
	}
	if estimate.OwnerSelfUse {
		availabilityScore += 10
	}

	riskControlScore := 100.0
	riskControlScore -= math.Min(math.Max(estimate.EffectiveRateMultiplier-1, 0)*12, 36)
	if estimate.EffectiveHourlyRate > 0 && estimate.HourlyNetCost > estimate.RequestCost {
		riskControlScore -= 18
	}
	riskControlScore -= math.Min(float64(len(warnings))*10, 30)
	riskControlScore -= accountShareRecommendationQuotaRiskPenalty(listing)
	if listing.MinBalanceRequired > 0 {
		riskControlScore -= math.Min(listing.MinBalanceRequired*2, 12)
	}
	if estimate.OwnerSelfUse {
		riskControlScore += 8
	}

	costSavingScore = clampAccountShareRecommendationScore(costSavingScore)
	stabilityScore = clampAccountShareRecommendationScore(stabilityScore)
	availabilityScore = clampAccountShareRecommendationScore(availabilityScore)
	riskControlScore = clampAccountShareRecommendationScore(riskControlScore)
	overall := costSavingScore*0.4 + stabilityScore*0.24 + availabilityScore*0.2 + riskControlScore*0.16

	return AccountShareRecommendationScoreBreakdown{
		CostSavingScore:   roundAccountShareRecommendationScore(costSavingScore),
		StabilityScore:    roundAccountShareRecommendationScore(stabilityScore),
		AvailabilityScore: roundAccountShareRecommendationScore(availabilityScore),
		RiskControlScore:  roundAccountShareRecommendationScore(riskControlScore),
		OverallScore:      roundAccountShareRecommendationScore(overall),
	}
}

func applyAccountShareRecommendationSmartLabels(candidates []AccountShareRecommendationCandidate) {
	if len(candidates) == 0 {
		return
	}
	bestStable := -1
	bestValue := -1
	for i := range candidates {
		if bestStable < 0 || candidates[i].ScoreBreakdown.StabilityScore > candidates[bestStable].ScoreBreakdown.StabilityScore ||
			(candidates[i].ScoreBreakdown.StabilityScore == candidates[bestStable].ScoreBreakdown.StabilityScore && accountShareRecommendationCandidateRanksBefore(candidates[i], candidates[bestStable])) {
			bestStable = i
		}
		if bestValue < 0 || candidates[i].ScoreBreakdown.OverallScore > candidates[bestValue].ScoreBreakdown.OverallScore ||
			(candidates[i].ScoreBreakdown.OverallScore == candidates[bestValue].ScoreBreakdown.OverallScore && accountShareRecommendationCandidateRanksBefore(candidates[i], candidates[bestValue])) {
			bestValue = i
		}
	}
	if bestStable >= 0 {
		candidates[bestStable].Tags = prependUniqueString(candidates[bestStable].Tags, "最稳妥")
		candidates[bestStable].Reasons = prependUniqueString(candidates[bestStable].Reasons, "并发、席位、评分和风险控制综合更稳")
	}
	if bestValue >= 0 {
		candidates[bestValue].Tags = prependUniqueString(candidates[bestValue].Tags, "性价比最高")
		candidates[bestValue].Reasons = prependUniqueString(candidates[bestValue].Reasons, "省钱、稳定、可用和风险控制综合分最高")
	}
}

func accountShareRecommendationSelectCandidates(candidates []AccountShareRecommendationCandidate, limit int) []AccountShareRecommendationCandidate {
	if limit <= 0 || len(candidates) <= limit {
		return candidates
	}
	selected := make([]AccountShareRecommendationCandidate, 0, limit)
	seen := make(map[string]struct{}, limit)
	add := func(candidate AccountShareRecommendationCandidate) {
		if len(selected) >= limit {
			return
		}
		key := accountShareRecommendationSelectionKey(candidate)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		selected = append(selected, candidate)
	}

	costSlots := accountShareRecommendationCostSlotCount(limit)
	for _, candidate := range candidates {
		if len(selected) >= costSlots {
			break
		}
		add(candidate)
	}

	accountShareRecommendationAddBestCandidate(&selected, seen, candidates, limit, func(left, right AccountShareRecommendationCandidate) bool {
		if left.ScoreBreakdown.OverallScore != right.ScoreBreakdown.OverallScore {
			return left.ScoreBreakdown.OverallScore > right.ScoreBreakdown.OverallScore
		}
		return accountShareRecommendationCandidateRanksBefore(left, right)
	})
	accountShareRecommendationAddBestCandidate(&selected, seen, candidates, limit, func(left, right AccountShareRecommendationCandidate) bool {
		if left.ScoreBreakdown.StabilityScore != right.ScoreBreakdown.StabilityScore {
			return left.ScoreBreakdown.StabilityScore > right.ScoreBreakdown.StabilityScore
		}
		return accountShareRecommendationCandidateRanksBefore(left, right)
	})
	accountShareRecommendationAddBestCandidate(&selected, seen, candidates, limit, func(left, right AccountShareRecommendationCandidate) bool {
		if left.ScoreBreakdown.AvailabilityScore != right.ScoreBreakdown.AvailabilityScore {
			return left.ScoreBreakdown.AvailabilityScore > right.ScoreBreakdown.AvailabilityScore
		}
		return accountShareRecommendationCandidateRanksBefore(left, right)
	})
	accountShareRecommendationAddBestCandidate(&selected, seen, candidates, limit, func(left, right AccountShareRecommendationCandidate) bool {
		if left.ScoreBreakdown.RiskControlScore != right.ScoreBreakdown.RiskControlScore {
			return left.ScoreBreakdown.RiskControlScore > right.ScoreBreakdown.RiskControlScore
		}
		return accountShareRecommendationCandidateRanksBefore(left, right)
	})

	for _, candidate := range candidates {
		if len(selected) >= limit {
			break
		}
		add(candidate)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return accountShareRecommendationCandidateRanksBefore(selected[i], selected[j])
	})
	return selected
}

func accountShareRecommendationCostSlotCount(limit int) int {
	switch {
	case limit >= 8:
		return limit - 4
	case limit >= 5:
		return limit - 3
	case limit >= 3:
		return limit - 1
	default:
		return 1
	}
}

func accountShareRecommendationAddBestCandidate(selected *[]AccountShareRecommendationCandidate, seen map[string]struct{}, candidates []AccountShareRecommendationCandidate, limit int, better func(AccountShareRecommendationCandidate, AccountShareRecommendationCandidate) bool) {
	if limit <= 0 || len(*selected) >= limit || len(candidates) == 0 {
		return
	}
	bestIndex := -1
	for i, candidate := range candidates {
		key := accountShareRecommendationSelectionKey(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		if bestIndex < 0 || better(candidate, candidates[bestIndex]) {
			bestIndex = i
		}
	}
	if bestIndex < 0 {
		return
	}
	key := accountShareRecommendationSelectionKey(candidates[bestIndex])
	seen[key] = struct{}{}
	*selected = append(*selected, candidates[bestIndex])
}

func accountShareRecommendationCandidateRanksBefore(left, right AccountShareRecommendationCandidate) bool {
	if left.Estimate.TotalCost != right.Estimate.TotalCost {
		return left.Estimate.TotalCost < right.Estimate.TotalCost
	}
	if left.Estimate.RequestCost != right.Estimate.RequestCost {
		return left.Estimate.RequestCost < right.Estimate.RequestCost
	}
	if left.Estimate.HourlyNetCost != right.Estimate.HourlyNetCost {
		return left.Estimate.HourlyNetCost < right.Estimate.HourlyNetCost
	}
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.Listing.RatingAvg != right.Listing.RatingAvg {
		return left.Listing.RatingAvg > right.Listing.RatingAvg
	}
	return left.Listing.ID < right.Listing.ID
}

func accountShareRecommendationSelectionKey(candidate AccountShareRecommendationCandidate) string {
	return accountShareRecommendationCandidateDedupeKey(candidate.Listing)
}

func accountShareRecommendationQuotaRiskPenalty(listing AccountShareListing) float64 {
	utilizations := make([]float64, 0, 4)
	if listing.QuotaSummary != nil {
		if listing.QuotaSummary.Window5h.MaxUtilization != nil {
			utilizations = append(utilizations, *listing.QuotaSummary.Window5h.MaxUtilization)
		}
		if listing.QuotaSummary.Window7d.MaxUtilization != nil {
			utilizations = append(utilizations, *listing.QuotaSummary.Window7d.MaxUtilization)
		}
	} else {
		progresses := []*UsageProgress{
			listing.Codex5hUsage,
			listing.Codex7dUsage,
			listing.Anthropic5hUsage,
			listing.Anthropic7dUsage,
		}
		for _, progress := range progresses {
			if progress != nil {
				utilizations = append(utilizations, progress.Utilization)
			}
		}
	}

	penalty := 0.0
	for _, utilization := range utilizations {
		if math.IsNaN(utilization) || math.IsInf(utilization, 0) {
			continue
		}
		if utilization <= 70 {
			continue
		}
		penalty += math.Min((utilization-70)*0.45, 18)
	}
	return math.Min(penalty, 30)
}

func clampAccountShareRecommendationScore(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func roundAccountShareRecommendationScore(value float64) float64 {
	return math.Round(value*10) / 10
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
	normalized := NormalizeAccountLevel(level)
	if normalized == AccountLevelUnknown {
		return ""
	}
	return normalized
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

func BuildAccountShareModeBillingSnapshot(membership *AccountShareMembership, listing *AccountShareListing, policy *AccountSharePolicy, baseCharge, hourlyCharge float64, durationMs int) *AccountShareModeBillingSnapshot {
	if membership == nil || listing == nil {
		return nil
	}
	if IsAccountShareModeOwnerSelfUse(membership, listing) {
		return nil
	}
	ownerRatio := 0.0
	inviteRatio := 0.0
	platformRatio := 1.0
	var policyID *int64
	policyVersion := 0
	if policy != nil {
		id := policy.ID
		policyID = &id
		policyVersion = policy.Version
		ownerRatio = policy.OwnerShareRatio
		inviteRatio = policy.InviteShareRatio
		platformRatio = math.Max(0, 1-ownerRatio-inviteRatio)
	}
	totalCharge := baseCharge + hourlyCharge
	if totalCharge < 0 {
		totalCharge = 0
	}
	return &AccountShareModeBillingSnapshot{
		MembershipID:       membership.ID,
		ListingID:          listing.ID,
		AccountID:          membership.AccountID,
		OwnerUserID:        listing.OwnerUserID,
		ConsumerUserID:     membership.ConsumerUserID,
		APIKeyID:           membership.APIKeyID,
		BaseCharge:         baseCharge,
		HourlyCharge:       hourlyCharge,
		TotalCharge:        totalCharge,
		RateMultiplier:     listing.RateMultiplier,
		HourlyRate:         listing.HourlyRate,
		PolicyID:           policyID,
		PolicyVersion:      policyVersion,
		OwnerShareRatio:    ownerRatio,
		InviteShareRatio:   inviteRatio,
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
