package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var ErrUsageBillingRequestIDRequired = errors.New("usage billing request_id is required")
var ErrUsageBillingRequestConflict = errors.New("usage billing request fingerprint conflict")

const (
	UsageBillingErrorPricingMissing = "pricing_missing"
	UsageBillingErrorFailed         = "billing_failed"
)

// SUB2API returns its gateway-generated correlation ID separately from the
// model provider's x-request-id. Retain that ID for cross-gateway reconciliation.
// Other providers keep the request ID already extracted by their adapter.
func upstreamUsageRequestID(headers http.Header, providerRequestID string) *string {
	if id := strings.TrimSpace(headers.Get("x-client-request-id")); id != "" {
		return &id
	}
	return optionalTrimmedStringPtr(providerRequestID)
}

func usageBillingErrorCode(err error) *string {
	code := UsageBillingErrorFailed
	if isUsagePricingUnavailableError(err) {
		code = UsageBillingErrorPricingMissing
	}
	return &code
}

// UsageBillingCommand describes one billable request that must be applied at most once.
type UsageBillingCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string

	UserID              int64
	AccountID           int64
	GroupID             *int64
	SubscriptionID      *int64
	AccountType         string
	Model               string
	ServiceTier         string
	ReasoningEffort     string
	BillingType         int8
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	MediaType           string

	BalanceCost                float64
	PreferPointsBilling        bool
	SubscriptionCost           float64
	PrivateGroupCommissionCost float64
	APIKeyQuotaCost            float64
	APIKeyRateLimitCost        float64
	AccountQuotaCost           float64

	ShareSnapshotCaptured bool
	ShareOwnerUserID      *int64
	ShareModeSnapshot     string
	ShareStatusSnapshot   string
	SharePlatform         string
	SharePolicyID         *int64
	SharePolicyVersion    int
	OwnerShareRatio       float64
	InviteShareRatio      float64
	UsageOccurredAt       time.Time

	AccountShareModeSettlement *AccountShareModeBillingSnapshot

	UsageLog *UsageLog
}

func (c *UsageBillingCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildUsageBillingFingerprint(c)
	}
}

func buildUsageBillingFingerprint(c *UsageBillingCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%s|%d|%0.10f|%t|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f",
		c.UserID,
		c.AccountID,
		c.APIKeyID,
		strings.TrimSpace(c.AccountType),
		strings.TrimSpace(c.Model),
		strings.TrimSpace(c.ServiceTier),
		strings.TrimSpace(c.ReasoningEffort),
		c.BillingType,
		c.InputTokens,
		c.OutputTokens,
		c.CacheCreationTokens,
		c.CacheReadTokens,
		c.ImageCount,
		strings.TrimSpace(c.MediaType),
		valueOrZero(c.SubscriptionID),
		c.BalanceCost,
		c.PreferPointsBilling,
		c.SubscriptionCost,
		c.PrivateGroupCommissionCost,
		c.APIKeyQuotaCost,
		c.APIKeyRateLimitCost,
		c.AccountQuotaCost,
	)
	if snapshot := c.AccountShareModeSettlement; snapshot != nil {
		raw += fmt.Sprintf(
			"|account_share_mode|%d|%d|%d|%d|%d|%0.10f|%0.10f|%0.10f|%0.10f|%d|%d|%0.10f|%0.10f|%0.10f|%d",
			snapshot.MembershipID,
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.ConsumerUserID,
			snapshot.BaseCharge,
			snapshot.HourlyCharge,
			snapshot.TotalCharge,
			snapshot.RateMultiplier,
			valueOrZero(snapshot.PolicyID),
			snapshot.PolicyVersion,
			snapshot.OwnerShareRatio,
			snapshot.InviteShareRatio,
			snapshot.PlatformShareRatio,
			snapshot.DurationMs,
		)
	}
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func HashUsageRequestPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// AccountQuotaState holds the post-increment quota state returned by the DB transaction.
// All values are post-update (i.e., already include the increment).
type AccountQuotaState struct {
	TotalUsed   float64
	TotalLimit  float64
	DailyUsed   float64
	DailyLimit  float64
	WeeklyUsed  float64
	WeeklyLimit float64
}

type UsageBillingApplyResult struct {
	Applied              bool
	APIKeyQuotaExhausted bool
	NewBalance           *float64           // post-deduction balance (nil = no balance deduction)
	NewPointsBalance     *float64           // post-deduction points balance (nil = no points deduction)
	PointsDeducted       float64            // points deducted for the request
	BalanceDeducted      float64            // balance deducted for the request
	CommissionDeducted   float64            // balance deducted for private-group commission
	QuotaState           *AccountQuotaState // post-increment quota state (nil = no quota increment)
	UsageLogID           *int64             // persisted usage log id when the billing transaction wrote one
	BalanceCreditUserIDs []int64            // users credited by settlement side effects; callers should invalidate balance caches
	// BalanceOverdrafted 表示本次扣款把余额扣成了负数（扣款前余额不足）。
	// 钱照扣不误——账已经用掉了——但这个标记让上层能打点告警并对账。
	BalanceOverdrafted bool
}

type UsageBillingRepository interface {
	Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error)
}
