package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	AccountShareBillingIntentStatusCreated        = "created"
	AccountShareBillingIntentStatusInFlight       = "in_flight"
	AccountShareBillingIntentStatusReady          = "ready"
	AccountShareBillingIntentStatusProcessing     = "processing"
	AccountShareBillingIntentStatusSettled        = "settled"
	AccountShareBillingIntentStatusCancelled      = "cancelled"
	AccountShareBillingIntentStatusFailed         = "failed"
	AccountShareBillingIntentStatusNeedsAttention = "needs_attention"

	AccountShareBillingCommandSchemaV2  = 2
	AccountShareBillingCommandSchemaV3  = 3
	AccountShareBillingUsageSchemaV2    = 2
	AccountShareBillingResponseSchemaV1 = 1

	AccountShareBillingIntentDefaultClaimLimit = 10
	AccountShareBillingIntentMaxClaimLimit     = 100
	AccountShareBillingAdminMaxPageSize        = 100
)

var (
	ErrAccountShareBillingIntentInvalid       = errors.New("invalid account share billing intent")
	ErrAccountShareBillingIntentNotFound      = errors.New("account share billing intent not found")
	ErrAccountShareBillingIntentConflict      = errors.New("account share billing intent fingerprint conflict")
	ErrAccountShareBillingIntentStateConflict = errors.New("account share billing intent state token conflict")
	ErrAccountShareBillingIntentLeaseLost     = errors.New("account share billing intent worker lease lost")
	ErrAccountShareBillingBindingUnavailable  = errors.New("account share billing binding is no longer active")
	ErrAccountShareBillingPreTerminalCommit   = errors.New("account share billing was not durable before response completion")

	ErrAccountShareBillingAdminRequired = infraerrors.Forbidden(
		"ACCOUNT_SHARE_BILLING_ADMIN_REQUIRED",
		"administrator permission is required",
	)
	ErrAccountShareBillingAdminIntentNotFound = infraerrors.NotFound(
		"ACCOUNT_SHARE_BILLING_INTENT_NOT_FOUND",
		"account share billing intent was not found",
	)
	ErrAccountShareBillingAdminConflict = infraerrors.Conflict(
		"ACCOUNT_SHARE_BILLING_INTENT_ADMIN_CONFLICT",
		"account share billing intent state or resolution has changed",
	)
)

// AccountShareBillingCommand is the normalized pre-forward billing snapshot.
// New intents persist schema V3. DecodeAccountShareBillingCommand also maps
// historical V2 payloads into this projection with their original schema
// version and the legacy settlement behavior preserved.
//
// The payload deliberately excludes credentials, proxy secrets, tokens, raw
// headers, raw request bodies, IP addresses, and user-agent strings.
// RequestPayloadHash may contain only a SHA-256 digest, never the request.
type AccountShareBillingCommand struct {
	SchemaVersion         int    `json:"schema_version"`
	RequestPayloadHash    string `json:"request_payload_hash"`
	GroupID               *int64 `json:"group_id"`
	SubscriptionID        *int64 `json:"subscription_id"`
	AccountType           string `json:"account_type"`
	RequestedModel        string `json:"requested_model"`
	RoutedModel           string `json:"routed_model"`
	InboundEndpoint       string `json:"inbound_endpoint"`
	UpstreamEndpoint      string `json:"upstream_endpoint"`
	RequestType           string `json:"request_type"`
	ServiceTier           string `json:"service_tier"`
	ReasoningEffort       string `json:"reasoning_effort"`
	BillingType           int16  `json:"billing_type"`
	PreferPointsBilling   bool   `json:"prefer_points_billing"`
	RateMultiplier        string `json:"rate_multiplier"`
	RateMultiplierSource  string `json:"rate_multiplier_source"`
	AccountRateMultiplier string `json:"account_rate_multiplier"`
	HourlyRate            string `json:"hourly_rate"`
	OwnerShareRatio       string `json:"owner_share_ratio"`
	InviteShareRatio      string `json:"invite_share_ratio"`
	PlatformShareRatio    string `json:"platform_share_ratio"`
	PolicyID              *int64 `json:"policy_id"`
	PolicyVersion         int    `json:"policy_version"`
	ChannelID             *int64 `json:"channel_id"`
	ModelMappingChain     string `json:"model_mapping_chain"`
	SettlementEnabled     bool   `json:"settlement_enabled"`
	ShareModeSnapshot     string `json:"share_mode_snapshot"`
	ShareStatusSnapshot   string `json:"share_status_snapshot"`
	SharePlatformSnapshot string `json:"share_platform_snapshot"`
}

// accountShareBillingCommandV2Wire is the immutable historical V2 wire shape.
// Keep this explicit instead of aliasing AccountShareBillingCommand: the
// absence of settlement_enabled is part of the signed payload contract.
type accountShareBillingCommandV2Wire struct {
	SchemaVersion         int    `json:"schema_version"`
	RequestPayloadHash    string `json:"request_payload_hash"`
	GroupID               *int64 `json:"group_id"`
	SubscriptionID        *int64 `json:"subscription_id"`
	AccountType           string `json:"account_type"`
	RequestedModel        string `json:"requested_model"`
	RoutedModel           string `json:"routed_model"`
	InboundEndpoint       string `json:"inbound_endpoint"`
	UpstreamEndpoint      string `json:"upstream_endpoint"`
	RequestType           string `json:"request_type"`
	ServiceTier           string `json:"service_tier"`
	ReasoningEffort       string `json:"reasoning_effort"`
	BillingType           int16  `json:"billing_type"`
	PreferPointsBilling   bool   `json:"prefer_points_billing"`
	RateMultiplier        string `json:"rate_multiplier"`
	RateMultiplierSource  string `json:"rate_multiplier_source"`
	AccountRateMultiplier string `json:"account_rate_multiplier"`
	HourlyRate            string `json:"hourly_rate"`
	OwnerShareRatio       string `json:"owner_share_ratio"`
	InviteShareRatio      string `json:"invite_share_ratio"`
	PlatformShareRatio    string `json:"platform_share_ratio"`
	PolicyID              *int64 `json:"policy_id"`
	PolicyVersion         int    `json:"policy_version"`
	ChannelID             *int64 `json:"channel_id"`
	ModelMappingChain     string `json:"model_mapping_chain"`
	ShareModeSnapshot     string `json:"share_mode_snapshot"`
	ShareStatusSnapshot   string `json:"share_status_snapshot"`
	SharePlatformSnapshot string `json:"share_platform_snapshot"`
}

func (command accountShareBillingCommandV2Wire) toRuntimeCommand() AccountShareBillingCommand {
	return AccountShareBillingCommand{
		SchemaVersion:         command.SchemaVersion,
		RequestPayloadHash:    command.RequestPayloadHash,
		GroupID:               command.GroupID,
		SubscriptionID:        command.SubscriptionID,
		AccountType:           command.AccountType,
		RequestedModel:        command.RequestedModel,
		RoutedModel:           command.RoutedModel,
		InboundEndpoint:       command.InboundEndpoint,
		UpstreamEndpoint:      command.UpstreamEndpoint,
		RequestType:           command.RequestType,
		ServiceTier:           command.ServiceTier,
		ReasoningEffort:       command.ReasoningEffort,
		BillingType:           command.BillingType,
		PreferPointsBilling:   command.PreferPointsBilling,
		RateMultiplier:        command.RateMultiplier,
		RateMultiplierSource:  command.RateMultiplierSource,
		AccountRateMultiplier: command.AccountRateMultiplier,
		HourlyRate:            command.HourlyRate,
		OwnerShareRatio:       command.OwnerShareRatio,
		InviteShareRatio:      command.InviteShareRatio,
		PlatformShareRatio:    command.PlatformShareRatio,
		PolicyID:              command.PolicyID,
		PolicyVersion:         command.PolicyVersion,
		ChannelID:             command.ChannelID,
		ModelMappingChain:     command.ModelMappingChain,
		SettlementEnabled:     true,
		ShareModeSnapshot:     command.ShareModeSnapshot,
		ShareStatusSnapshot:   command.ShareStatusSnapshot,
		SharePlatformSnapshot: command.SharePlatformSnapshot,
	}
}

func accountShareBillingCommandV2WireFromRuntime(command AccountShareBillingCommand) accountShareBillingCommandV2Wire {
	return accountShareBillingCommandV2Wire{
		SchemaVersion:         command.SchemaVersion,
		RequestPayloadHash:    command.RequestPayloadHash,
		GroupID:               command.GroupID,
		SubscriptionID:        command.SubscriptionID,
		AccountType:           command.AccountType,
		RequestedModel:        command.RequestedModel,
		RoutedModel:           command.RoutedModel,
		InboundEndpoint:       command.InboundEndpoint,
		UpstreamEndpoint:      command.UpstreamEndpoint,
		RequestType:           command.RequestType,
		ServiceTier:           command.ServiceTier,
		ReasoningEffort:       command.ReasoningEffort,
		BillingType:           command.BillingType,
		PreferPointsBilling:   command.PreferPointsBilling,
		RateMultiplier:        command.RateMultiplier,
		RateMultiplierSource:  command.RateMultiplierSource,
		AccountRateMultiplier: command.AccountRateMultiplier,
		HourlyRate:            command.HourlyRate,
		OwnerShareRatio:       command.OwnerShareRatio,
		InviteShareRatio:      command.InviteShareRatio,
		PlatformShareRatio:    command.PlatformShareRatio,
		PolicyID:              command.PolicyID,
		PolicyVersion:         command.PolicyVersion,
		ChannelID:             command.ChannelID,
		ModelMappingChain:     command.ModelMappingChain,
		ShareModeSnapshot:     command.ShareModeSnapshot,
		ShareStatusSnapshot:   command.ShareStatusSnapshot,
		SharePlatformSnapshot: command.SharePlatformSnapshot,
	}
}

// AccountShareBillingUsagePayloadV2 is the complete post-forward allowlist
// required to reconstruct UsageBillingCommand and its UsageLog without
// consulting mutable listing, binding, account multiplier, or pricing state.
// Raw upstream responses and provider error bodies are intentionally excluded.
type AccountShareBillingUsagePayloadV2 struct {
	SchemaVersion              int       `json:"schema_version"`
	UsageOccurredAt            time.Time `json:"usage_occurred_at"`
	Model                      string    `json:"model"`
	UpstreamModel              string    `json:"upstream_model"`
	ServiceTier                string    `json:"service_tier"`
	ReasoningEffort            string    `json:"reasoning_effort"`
	InputTokens                int64     `json:"input_tokens"`
	OutputTokens               int64     `json:"output_tokens"`
	CacheCreationTokens        int64     `json:"cache_creation_tokens"`
	CacheCreation5mTokens      int64     `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens      int64     `json:"cache_creation_1h_tokens"`
	CacheReadTokens            int64     `json:"cache_read_tokens"`
	ImageInputTokens           int64     `json:"image_input_tokens"`
	ImageOutputTokens          int64     `json:"image_output_tokens"`
	ImageCount                 int64     `json:"image_count"`
	ImageSize                  string    `json:"image_size"`
	MediaType                  string    `json:"media_type"`
	VideoCount                 int64     `json:"video_count"`
	VideoResolution            string    `json:"video_resolution"`
	VideoDurationSeconds       *int64    `json:"video_duration_seconds"`
	DurationMilliseconds       int64     `json:"duration_ms"`
	FirstTokenMilliseconds     *int64    `json:"first_token_ms"`
	BillingTier                string    `json:"billing_tier"`
	BillingMode                string    `json:"billing_mode"`
	CacheTTLOverridden         bool      `json:"cache_ttl_overridden"`
	AppliedRateMultiplier      string    `json:"applied_rate_multiplier"`
	InputCost                  string    `json:"input_cost"`
	OutputCost                 string    `json:"output_cost"`
	CacheCreationCost          string    `json:"cache_creation_cost"`
	CacheReadCost              string    `json:"cache_read_cost"`
	ImageInputCost             string    `json:"image_input_cost"`
	ImageOutputCost            string    `json:"image_output_cost"`
	TotalCost                  string    `json:"total_cost"`
	ActualCost                 string    `json:"actual_cost"`
	AccountStatsCost           *string   `json:"account_stats_cost"`
	BalanceCost                string    `json:"balance_cost"`
	SubscriptionCost           string    `json:"subscription_cost"`
	PrivateGroupCommissionCost string    `json:"private_group_commission_cost"`
	APIKeyQuotaCost            string    `json:"api_key_quota_cost"`
	APIKeyRateLimitCost        string    `json:"api_key_rate_limit_cost"`
	AccountQuotaCost           string    `json:"account_quota_cost"`
	BaseCharge                 string    `json:"base_charge"`
	HourlyCharge               string    `json:"hourly_charge"`
	TotalCharge                string    `json:"total_charge"`
}

// AccountShareBillingResponseSummaryV1 captures only non-sensitive response
// metadata needed for reconciliation and support.
type AccountShareBillingResponseSummaryV1 struct {
	SchemaVersion     int    `json:"schema_version"`
	HTTPStatus        int    `json:"http_status"`
	ProviderRequestID string `json:"provider_request_id"`
	FinishReason      string `json:"finish_reason"`
	Streamed          bool   `json:"streamed"`
	ErrorCode         string `json:"error_code"`
}

type CreateAccountShareBillingIntentInput struct {
	RequestID           string
	ClientRequestID     string
	DispatchID          string
	AttemptNo           int
	APIKeyID            int64
	MembershipID        int64
	ListingID           int64
	AccountID           int64
	BindingID           int64
	ListingRevisionID   int64
	TermsRevisionNumber int64
	ActorUserID         int64
	ActorRole           string
	ConsumerUserID      int64
	OwnerUserID         int64
	Command             AccountShareBillingCommand
}

type PreparedAccountShareBillingIntent struct {
	CreateAccountShareBillingIntentInput
	CommandJSON        json.RawMessage
	CommandHash        string
	RequestFingerprint string
}

type AccountShareBillingIntentState struct {
	ID              int64
	RequestID       string
	ClientRequestID string
	DispatchID      string
	AttemptNo       int
	APIKeyID        int64
	MembershipID    int64
	Status          string
	StateToken      int64
	AttemptCount    int
	LeaseToken      int64
	LeaseOwner      string
	LeaseExpiresAt  *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AccountShareBillingIntentTransition struct {
	ID                 int64
	ExpectedStateToken int64
}

type MarkAccountShareBillingIntentReadyInput struct {
	AccountShareBillingIntentTransition
	Usage           AccountShareBillingUsagePayloadV2
	ResponseSummary AccountShareBillingResponseSummaryV1
}

type PreparedAccountShareBillingIntentReady struct {
	UsageJSON           json.RawMessage
	UsageHash           string
	ResponseSummaryJSON json.RawMessage
}

type ClaimAccountShareBillingIntentsInput struct {
	WorkerID      string
	Limit         int
	LeaseDuration time.Duration
}

type AccountShareBillingIntentLeaseTransition struct {
	ID                 int64
	ExpectedStateToken int64
	LeaseToken         int64
	WorkerID           string
}

type MarkAccountShareBillingIntentSettledInput struct {
	AccountShareBillingIntentLeaseTransition
	UsageLogID *int64
}

type MarkAccountShareBillingIntentFailedInput struct {
	AccountShareBillingIntentLeaseTransition
	ErrorCode string
	// ErrorMessage is a bounded, sanitized operator summary. Callers must not
	// pass raw upstream bodies, headers, credentials, or proxy details.
	ErrorMessage   string
	RetryAt        *time.Time
	NeedsAttention bool
}

type EscalateAccountShareBillingIntentInput struct {
	AccountShareBillingIntentTransition
	ReasonCode    string
	ReasonMessage string
	StaleBefore   time.Time
}

type AccountShareBillingIntentWorkItem struct {
	AccountShareBillingIntentState
	ListingID           int64
	AccountID           int64
	BindingID           int64
	ListingRevisionID   int64
	TermsRevisionNumber int64
	ActorUserID         int64
	ActorRole           string
	ConsumerUserID      int64
	OwnerUserID         int64
	Command             AccountShareBillingCommand
	CommandHash         string
	RequestFingerprint  string
	Usage               AccountShareBillingUsagePayloadV2
	UsageHash           string
	ResponseSummary     AccountShareBillingResponseSummaryV1
}

type AccountShareBillingIntentAttentionCandidate struct {
	AccountShareBillingIntentState
	ReasonCode       string
	LastErrorCode    string
	LastErrorMessage string
	ForwardStartedAt *time.Time
	CompletedAt      *time.Time
	NextAttemptAt    *time.Time
}

type AccountShareBillingRecoveryCursor struct {
	UpdatedAt time.Time
	ID        int64
}

type ListAccountShareBillingRecoveryCandidatesInput struct {
	InFlightStaleBefore time.Time
	CreatedStaleBefore  time.Time
	After               *AccountShareBillingRecoveryCursor
	Limit               int
}

// AccountShareBillingIntentAdminRecord is the deliberately small, non-secret
// operator projection. It excludes command/usage payloads, credentials,
// request bodies, headers, proxy data, and wallet data.
type AccountShareBillingIntentAdminRecord struct {
	ID               int64      `json:"id"`
	RequestID        string     `json:"request_id"`
	DispatchID       string     `json:"dispatch_id"`
	AttemptNo        int        `json:"attempt_no"`
	APIKeyID         int64      `json:"api_key_id"`
	MembershipID     int64      `json:"membership_id"`
	ListingID        int64      `json:"listing_id"`
	AccountID        int64      `json:"account_id"`
	Status           string     `json:"status"`
	StateToken       int64      `json:"state_token"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	ForwardStartedAt *time.Time `json:"forward_started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type AccountShareBillingIntentRepository interface {
	CreatePrepared(ctx context.Context, input CreateAccountShareBillingIntentInput) (*AccountShareBillingIntentState, bool, error)
	MarkInFlight(ctx context.Context, input AccountShareBillingIntentTransition) (*AccountShareBillingIntentState, error)
	MarkReady(ctx context.Context, input MarkAccountShareBillingIntentReadyInput) (*AccountShareBillingIntentState, error)
	CancelCreated(ctx context.Context, input AccountShareBillingIntentTransition, reasonCode, reasonMessage string) (*AccountShareBillingIntentState, error)
	ClaimReady(ctx context.Context, input ClaimAccountShareBillingIntentsInput) ([]AccountShareBillingIntentWorkItem, error)
	RenewProcessingLease(ctx context.Context, input AccountShareBillingIntentLeaseTransition, leaseDuration time.Duration) (*AccountShareBillingIntentState, error)
	MarkSettled(ctx context.Context, input MarkAccountShareBillingIntentSettledInput) (*AccountShareBillingIntentState, error)
	MarkFailed(ctx context.Context, input MarkAccountShareBillingIntentFailedInput) (*AccountShareBillingIntentState, error)
	FailInFlightWithoutUsage(ctx context.Context, input AccountShareBillingIntentTransition, reasonCode, reasonMessage string) (*AccountShareBillingIntentState, error)
	EscalateStaleToNeedsAttention(ctx context.Context, input EscalateAccountShareBillingIntentInput) (*AccountShareBillingIntentState, error)
	CountPendingByMembership(ctx context.Context, membershipID int64) (int64, error)
	ListRecoveryCandidates(ctx context.Context, input ListAccountShareBillingRecoveryCandidatesInput) ([]AccountShareBillingIntentAttentionCandidate, error)
}

// AccountShareBillingIntentAdminRepository is intentionally separate from the
// hot-path repository contract. Gateway and worker test doubles do not need
// administrative capabilities, while the production repository must opt in.
type AccountShareBillingIntentAdminRepository interface {
	ListNeedsAttentionForAdmin(
		ctx context.Context,
		offset int,
		limit int,
	) ([]AccountShareBillingIntentAdminRecord, int64, error)
	GetForAdmin(ctx context.Context, intentID int64) (*AccountShareBillingIntentAdminRecord, error)
}

func IsAccountShareBillingIntentStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case AccountShareBillingIntentStatusCreated,
		AccountShareBillingIntentStatusInFlight,
		AccountShareBillingIntentStatusReady,
		AccountShareBillingIntentStatusProcessing,
		AccountShareBillingIntentStatusSettled,
		AccountShareBillingIntentStatusCancelled,
		AccountShareBillingIntentStatusFailed,
		AccountShareBillingIntentStatusNeedsAttention:
		return true
	default:
		return false
	}
}

func CanTransitionAccountShareBillingIntent(from, to string) bool {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	switch from {
	case AccountShareBillingIntentStatusCreated:
		return to == AccountShareBillingIntentStatusInFlight ||
			to == AccountShareBillingIntentStatusCancelled ||
			to == AccountShareBillingIntentStatusNeedsAttention
	case AccountShareBillingIntentStatusInFlight:
		return to == AccountShareBillingIntentStatusReady ||
			to == AccountShareBillingIntentStatusNeedsAttention
	case AccountShareBillingIntentStatusReady:
		return to == AccountShareBillingIntentStatusProcessing ||
			to == AccountShareBillingIntentStatusNeedsAttention
	case AccountShareBillingIntentStatusProcessing:
		return to == AccountShareBillingIntentStatusProcessing ||
			to == AccountShareBillingIntentStatusSettled ||
			to == AccountShareBillingIntentStatusFailed ||
			to == AccountShareBillingIntentStatusNeedsAttention
	case AccountShareBillingIntentStatusFailed:
		return to == AccountShareBillingIntentStatusProcessing ||
			to == AccountShareBillingIntentStatusNeedsAttention
	case AccountShareBillingIntentStatusNeedsAttention:
		return to == AccountShareBillingIntentStatusReady ||
			to == AccountShareBillingIntentStatusCancelled
	default:
		return false
	}
}

func PrepareAccountShareBillingIntent(input CreateAccountShareBillingIntentInput) (*PreparedAccountShareBillingIntent, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.DispatchID = strings.ToLower(strings.TrimSpace(input.DispatchID))
	input.ActorRole = strings.ToLower(strings.TrimSpace(input.ActorRole))
	if err := validateAccountShareBillingIdentifier("request_id", input.RequestID, 255); err != nil {
		return nil, err
	}
	if err := validateAccountShareBillingIdentifier("client_request_id", input.ClientRequestID, 255); err != nil {
		return nil, err
	}
	parsedDispatchID, err := uuid.Parse(input.DispatchID)
	if err != nil || parsedDispatchID == uuid.Nil {
		return nil, fmt.Errorf("%w: dispatch_id must be a non-zero UUID", ErrAccountShareBillingIntentInvalid)
	}
	input.DispatchID = parsedDispatchID.String()
	if input.AttemptNo <= 0 {
		return nil, fmt.Errorf("%w: attempt_no must be positive", ErrAccountShareBillingIntentInvalid)
	}
	for name, value := range map[string]int64{
		"api_key_id":            input.APIKeyID,
		"membership_id":         input.MembershipID,
		"listing_id":            input.ListingID,
		"account_id":            input.AccountID,
		"binding_id":            input.BindingID,
		"listing_revision_id":   input.ListingRevisionID,
		"terms_revision_number": input.TermsRevisionNumber,
		"consumer_user_id":      input.ConsumerUserID,
		"owner_user_id":         input.OwnerUserID,
	} {
		if value <= 0 {
			return nil, fmt.Errorf("%w: %s must be positive", ErrAccountShareBillingIntentInvalid, name)
		}
	}
	switch input.ActorRole {
	case "consumer", "owner", "admin":
		if input.ActorUserID <= 0 {
			return nil, fmt.Errorf("%w: actor_user_id must be positive for role %s", ErrAccountShareBillingIntentInvalid, input.ActorRole)
		}
	case "system":
		if input.ActorUserID < 0 {
			return nil, fmt.Errorf("%w: actor_user_id must not be negative", ErrAccountShareBillingIntentInvalid)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported actor_role", ErrAccountShareBillingIntentInvalid)
	}

	command, err := normalizeAccountShareBillingCommand(input.Command)
	if err != nil {
		return nil, err
	}
	input.Command = command
	commandJSON, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal command: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	commandHash := hashAccountShareBillingPayload(commandJSON)
	requestFingerprint, err := buildAccountShareBillingRequestFingerprint(input, commandHash)
	if err != nil {
		return nil, err
	}
	return &PreparedAccountShareBillingIntent{
		CreateAccountShareBillingIntentInput: input,
		CommandJSON:                          commandJSON,
		CommandHash:                          commandHash,
		RequestFingerprint:                   requestFingerprint,
	}, nil
}

func buildAccountShareBillingRequestFingerprint(input CreateAccountShareBillingIntentInput, commandHash string) (string, error) {
	fingerprintJSON, err := json.Marshal(struct {
		RequestID           string `json:"request_id"`
		ClientRequestID     string `json:"client_request_id"`
		DispatchID          string `json:"dispatch_id"`
		AttemptNo           int    `json:"attempt_no"`
		APIKeyID            int64  `json:"api_key_id"`
		MembershipID        int64  `json:"membership_id"`
		ListingID           int64  `json:"listing_id"`
		AccountID           int64  `json:"account_id"`
		BindingID           int64  `json:"binding_id"`
		ListingRevisionID   int64  `json:"listing_revision_id"`
		TermsRevisionNumber int64  `json:"terms_revision_number"`
		ActorUserID         int64  `json:"actor_user_id"`
		ActorRole           string `json:"actor_role"`
		ConsumerUserID      int64  `json:"consumer_user_id"`
		OwnerUserID         int64  `json:"owner_user_id"`
		CommandHash         string `json:"command_hash"`
	}{
		RequestID:           input.RequestID,
		ClientRequestID:     input.ClientRequestID,
		DispatchID:          input.DispatchID,
		AttemptNo:           input.AttemptNo,
		APIKeyID:            input.APIKeyID,
		MembershipID:        input.MembershipID,
		ListingID:           input.ListingID,
		AccountID:           input.AccountID,
		BindingID:           input.BindingID,
		ListingRevisionID:   input.ListingRevisionID,
		TermsRevisionNumber: input.TermsRevisionNumber,
		ActorUserID:         input.ActorUserID,
		ActorRole:           input.ActorRole,
		ConsumerUserID:      input.ConsumerUserID,
		OwnerUserID:         input.OwnerUserID,
		CommandHash:         commandHash,
	})
	if err != nil {
		return "", fmt.Errorf("%w: marshal fingerprint: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	return hashAccountShareBillingPayload(fingerprintJSON), nil
}

func PrepareAccountShareBillingIntentReady(input MarkAccountShareBillingIntentReadyInput) (*PreparedAccountShareBillingIntentReady, error) {
	if err := ValidateAccountShareBillingIntentTransition(input.AccountShareBillingIntentTransition); err != nil {
		return nil, err
	}
	usage, err := normalizeAccountShareBillingUsage(input.Usage)
	if err != nil {
		return nil, err
	}
	summary, err := normalizeAccountShareBillingResponse(input.ResponseSummary)
	if err != nil {
		return nil, err
	}
	usageJSON, err := json.Marshal(usage)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal usage: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal response summary: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	return &PreparedAccountShareBillingIntentReady{
		UsageJSON:           usageJSON,
		UsageHash:           hashAccountShareBillingPayload(usageJSON),
		ResponseSummaryJSON: summaryJSON,
	}, nil
}

func DecodeAccountShareBillingCommand(payload []byte, expectedHash string) (AccountShareBillingCommand, error) {
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: decode command envelope: %v", ErrAccountShareBillingIntentInvalid, err)
	}

	var (
		command AccountShareBillingCommand
		err     error
	)
	switch envelope.SchemaVersion {
	case AccountShareBillingCommandSchemaV2:
		var legacy accountShareBillingCommandV2Wire
		if err = decodeStrictAccountShareBillingJSON(payload, &legacy); err != nil {
			return AccountShareBillingCommand{}, fmt.Errorf("%w: decode command V2: %v", ErrAccountShareBillingIntentInvalid, err)
		}
		command = legacy.toRuntimeCommand()
	case AccountShareBillingCommandSchemaV3:
		if err = decodeStrictAccountShareBillingJSON(payload, &command); err != nil {
			return AccountShareBillingCommand{}, fmt.Errorf("%w: decode command V3: %v", ErrAccountShareBillingIntentInvalid, err)
		}
	default:
		return AccountShareBillingCommand{}, fmt.Errorf("%w: unsupported command schema_version", ErrAccountShareBillingIntentInvalid)
	}
	command, err = normalizePersistedAccountShareBillingCommand(command)
	if err != nil {
		return AccountShareBillingCommand{}, err
	}
	commandHash, err := canonicalAccountShareBillingCommandHash(command)
	if err != nil {
		return AccountShareBillingCommand{}, err
	}
	if commandHash != strings.TrimSpace(expectedHash) {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: command hash mismatch", ErrAccountShareBillingIntentInvalid)
	}
	return command, nil
}

// normalizePersistedAccountShareBillingCommand validates a command read from
// durable storage while preserving the immutable semantics of every supported
// wire schema. Historical V2 commands predate settlement_enabled, so their
// runtime projection must always retain the original settlement-on behavior.
func normalizePersistedAccountShareBillingCommand(command AccountShareBillingCommand) (AccountShareBillingCommand, error) {
	switch command.SchemaVersion {
	case AccountShareBillingCommandSchemaV2:
		command.SettlementEnabled = true
		return normalizeAccountShareBillingCommandFields(command)
	case AccountShareBillingCommandSchemaV3:
		return normalizeAccountShareBillingCommand(command)
	default:
		return AccountShareBillingCommand{}, fmt.Errorf("%w: unsupported command schema_version", ErrAccountShareBillingIntentInvalid)
	}
}

func canonicalAccountShareBillingCommandHash(command AccountShareBillingCommand) (string, error) {
	var wirePayload any
	switch command.SchemaVersion {
	case AccountShareBillingCommandSchemaV2:
		wirePayload = accountShareBillingCommandV2WireFromRuntime(command)
	case AccountShareBillingCommandSchemaV3:
		wirePayload = command
	default:
		return "", fmt.Errorf("%w: unsupported command schema_version", ErrAccountShareBillingIntentInvalid)
	}
	raw, err := json.Marshal(wirePayload)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize command: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	return hashAccountShareBillingPayload(raw), nil
}

func DecodeAccountShareBillingUsage(payload []byte, expectedHash string) (AccountShareBillingUsagePayloadV2, error) {
	var usage AccountShareBillingUsagePayloadV2
	if err := decodeStrictAccountShareBillingJSON(payload, &usage); err != nil {
		return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: decode usage: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	usage, err := normalizeAccountShareBillingUsage(usage)
	if err != nil {
		return AccountShareBillingUsagePayloadV2{}, err
	}
	canonical, err := json.Marshal(usage)
	if err != nil {
		return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: canonicalize usage: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	if hashAccountShareBillingPayload(canonical) != strings.TrimSpace(expectedHash) {
		return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: usage hash mismatch", ErrAccountShareBillingIntentInvalid)
	}
	return usage, nil
}

func DecodeAccountShareBillingResponseSummary(payload []byte) (AccountShareBillingResponseSummaryV1, error) {
	var summary AccountShareBillingResponseSummaryV1
	if err := decodeStrictAccountShareBillingJSON(payload, &summary); err != nil {
		return AccountShareBillingResponseSummaryV1{}, fmt.Errorf("%w: decode response summary: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	return normalizeAccountShareBillingResponse(summary)
}

func ValidateAccountShareBillingIntentTransition(input AccountShareBillingIntentTransition) error {
	if input.ID <= 0 || input.ExpectedStateToken <= 0 {
		return fmt.Errorf("%w: id and expected_state_token must be positive", ErrAccountShareBillingIntentInvalid)
	}
	return nil
}

func ValidateAccountShareBillingIntentLeaseTransition(input AccountShareBillingIntentLeaseTransition) error {
	if err := ValidateAccountShareBillingIntentTransition(AccountShareBillingIntentTransition{
		ID:                 input.ID,
		ExpectedStateToken: input.ExpectedStateToken,
	}); err != nil {
		return err
	}
	if input.LeaseToken <= 0 {
		return fmt.Errorf("%w: lease_token must be positive", ErrAccountShareBillingIntentInvalid)
	}
	return validateAccountShareBillingIdentifier("worker_id", strings.TrimSpace(input.WorkerID), 128)
}

func NormalizeAccountShareBillingClaim(input ClaimAccountShareBillingIntentsInput) (ClaimAccountShareBillingIntentsInput, error) {
	input.WorkerID = strings.TrimSpace(input.WorkerID)
	if err := validateAccountShareBillingIdentifier("worker_id", input.WorkerID, 128); err != nil {
		return ClaimAccountShareBillingIntentsInput{}, err
	}
	if input.Limit <= 0 {
		input.Limit = AccountShareBillingIntentDefaultClaimLimit
	}
	if input.Limit > AccountShareBillingIntentMaxClaimLimit {
		input.Limit = AccountShareBillingIntentMaxClaimLimit
	}
	if _, err := AccountShareBillingLeaseMilliseconds(input.LeaseDuration); err != nil {
		return ClaimAccountShareBillingIntentsInput{}, err
	}
	return input, nil
}

func AccountShareBillingLeaseMilliseconds(duration time.Duration) (int64, error) {
	if duration < 5*time.Second || duration > 5*time.Minute {
		return 0, fmt.Errorf("%w: lease_duration must be between 5 seconds and 5 minutes", ErrAccountShareBillingIntentInvalid)
	}
	milliseconds := duration.Milliseconds()
	if milliseconds <= 0 {
		return 0, fmt.Errorf("%w: lease_duration must be positive", ErrAccountShareBillingIntentInvalid)
	}
	return milliseconds, nil
}

func ValidateAccountShareBillingFailure(input MarkAccountShareBillingIntentFailedInput) (MarkAccountShareBillingIntentFailedInput, error) {
	if err := ValidateAccountShareBillingIntentLeaseTransition(input.AccountShareBillingIntentLeaseTransition); err != nil {
		return MarkAccountShareBillingIntentFailedInput{}, err
	}
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	input.ErrorMessage = strings.TrimSpace(input.ErrorMessage)
	if err := validateAccountShareBillingIdentifier("error_code", input.ErrorCode, 100); err != nil {
		return MarkAccountShareBillingIntentFailedInput{}, err
	}
	if !utf8.ValidString(input.ErrorMessage) || utf8.RuneCountInString(input.ErrorMessage) > 1000 {
		return MarkAccountShareBillingIntentFailedInput{}, fmt.Errorf("%w: error_message is invalid or too long", ErrAccountShareBillingIntentInvalid)
	}
	if input.NeedsAttention && input.RetryAt != nil {
		return MarkAccountShareBillingIntentFailedInput{}, fmt.Errorf("%w: needs_attention cannot have retry_at", ErrAccountShareBillingIntentInvalid)
	}
	if input.RetryAt != nil {
		retryAt := input.RetryAt.UTC()
		input.RetryAt = &retryAt
	}
	return input, nil
}

func ValidateAccountShareBillingEscalation(input EscalateAccountShareBillingIntentInput) (EscalateAccountShareBillingIntentInput, error) {
	transition, reasonCode, reasonMessage, err := validateAccountShareBillingIntentReason(
		input.AccountShareBillingIntentTransition,
		input.ReasonCode,
		input.ReasonMessage,
	)
	if err != nil {
		return EscalateAccountShareBillingIntentInput{}, err
	}
	if input.StaleBefore.IsZero() {
		return EscalateAccountShareBillingIntentInput{}, fmt.Errorf("%w: stale_before is required", ErrAccountShareBillingIntentInvalid)
	}
	input.AccountShareBillingIntentTransition = transition
	input.ReasonCode = reasonCode
	input.ReasonMessage = reasonMessage
	input.StaleBefore = input.StaleBefore.UTC()
	return input, nil
}

func ValidateAccountShareBillingCancellation(
	transition AccountShareBillingIntentTransition,
	reasonCode string,
	reasonMessage string,
) (AccountShareBillingIntentTransition, string, string, error) {
	return validateAccountShareBillingIntentReason(transition, reasonCode, reasonMessage)
}

func validateAccountShareBillingIntentReason(
	transition AccountShareBillingIntentTransition,
	reasonCode string,
	reasonMessage string,
) (AccountShareBillingIntentTransition, string, string, error) {
	if err := ValidateAccountShareBillingIntentTransition(transition); err != nil {
		return AccountShareBillingIntentTransition{}, "", "", err
	}
	reasonCode = strings.TrimSpace(reasonCode)
	reasonMessage = strings.TrimSpace(reasonMessage)
	if err := validateAccountShareBillingIdentifier("reason_code", reasonCode, 100); err != nil {
		return AccountShareBillingIntentTransition{}, "", "", err
	}
	if !utf8.ValidString(reasonMessage) || utf8.RuneCountInString(reasonMessage) > 1000 {
		return AccountShareBillingIntentTransition{}, "", "", fmt.Errorf("%w: reason_message is invalid or too long", ErrAccountShareBillingIntentInvalid)
	}
	return transition, reasonCode, reasonMessage, nil
}

func normalizeAccountShareBillingAdminPagination(params pagination.PaginationParams) pagination.PaginationParams {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = AccountShareBillingIntentDefaultClaimLimit
	}
	if params.PageSize > AccountShareBillingAdminMaxPageSize {
		params.PageSize = AccountShareBillingAdminMaxPageSize
	}
	return params
}

func normalizeAccountShareBillingCommand(command AccountShareBillingCommand) (AccountShareBillingCommand, error) {
	if command.SchemaVersion != AccountShareBillingCommandSchemaV3 {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: unsupported command schema_version", ErrAccountShareBillingIntentInvalid)
	}
	return normalizeAccountShareBillingCommandFields(command)
}

func normalizeAccountShareBillingCommandFields(command AccountShareBillingCommand) (AccountShareBillingCommand, error) {
	command.RequestPayloadHash = strings.ToLower(strings.TrimSpace(command.RequestPayloadHash))
	command.AccountType = strings.TrimSpace(command.AccountType)
	command.RequestedModel = strings.TrimSpace(command.RequestedModel)
	command.RoutedModel = strings.TrimSpace(command.RoutedModel)
	command.InboundEndpoint = strings.TrimSpace(command.InboundEndpoint)
	command.UpstreamEndpoint = strings.TrimSpace(command.UpstreamEndpoint)
	command.RequestType = strings.TrimSpace(command.RequestType)
	command.ServiceTier = strings.TrimSpace(command.ServiceTier)
	command.ReasoningEffort = strings.TrimSpace(command.ReasoningEffort)
	command.RateMultiplierSource = strings.TrimSpace(command.RateMultiplierSource)
	command.ModelMappingChain = strings.TrimSpace(command.ModelMappingChain)
	command.ShareModeSnapshot = strings.TrimSpace(command.ShareModeSnapshot)
	command.ShareStatusSnapshot = strings.TrimSpace(command.ShareStatusSnapshot)
	command.SharePlatformSnapshot = strings.TrimSpace(command.SharePlatformSnapshot)
	if command.RequestPayloadHash != "" {
		decoded, err := hex.DecodeString(command.RequestPayloadHash)
		if err != nil || len(decoded) != sha256.Size || len(command.RequestPayloadHash) != sha256.Size*2 {
			return AccountShareBillingCommand{}, fmt.Errorf("%w: request_payload_hash must be a SHA-256 hex digest", ErrAccountShareBillingIntentInvalid)
		}
	}
	for name, value := range map[string]string{
		"account_type":            command.AccountType,
		"requested_model":         command.RequestedModel,
		"routed_model":            command.RoutedModel,
		"inbound_endpoint":        command.InboundEndpoint,
		"upstream_endpoint":       command.UpstreamEndpoint,
		"request_type":            command.RequestType,
		"rate_multiplier_source":  command.RateMultiplierSource,
		"share_mode_snapshot":     command.ShareModeSnapshot,
		"share_status_snapshot":   command.ShareStatusSnapshot,
		"share_platform_snapshot": command.SharePlatformSnapshot,
	} {
		if err := validateAccountShareBillingIdentifier(name, value, 255); err != nil {
			return AccountShareBillingCommand{}, err
		}
	}
	requestType, err := ParseUsageRequestType(command.RequestType)
	if err != nil || requestType == RequestTypeUnknown {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: request_type must be sync, stream, or ws_v2", ErrAccountShareBillingIntentInvalid)
	}
	command.RequestType = requestType.String()
	for name, value := range map[string]string{
		"service_tier":        command.ServiceTier,
		"reasoning_effort":    command.ReasoningEffort,
		"model_mapping_chain": command.ModelMappingChain,
	} {
		maxRunes := 100
		if name == "model_mapping_chain" {
			maxRunes = 1000
		}
		if err := validateOptionalAccountShareBillingText(name, value, maxRunes); err != nil {
			return AccountShareBillingCommand{}, err
		}
	}
	if command.BillingType != int16(BillingTypeBalance) && command.BillingType != int16(BillingTypeSubscription) {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: unsupported billing_type", ErrAccountShareBillingIntentInvalid)
	}
	if command.PolicyVersion < 0 {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: policy_version must not be negative", ErrAccountShareBillingIntentInvalid)
	}
	for name, value := range map[string]*int64{
		"group_id":        command.GroupID,
		"subscription_id": command.SubscriptionID,
		"policy_id":       command.PolicyID,
		"channel_id":      command.ChannelID,
	} {
		if value != nil && *value <= 0 {
			return AccountShareBillingCommand{}, fmt.Errorf("%w: %s must be positive", ErrAccountShareBillingIntentInvalid, name)
		}
	}
	if command.BillingType == int16(BillingTypeSubscription) && command.SubscriptionID == nil {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: subscription_id is required for subscription billing", ErrAccountShareBillingIntentInvalid)
	}
	var rateMultiplier decimal.Decimal
	if command.RateMultiplier, rateMultiplier, err = normalizeAccountShareBillingDecimal("rate_multiplier", command.RateMultiplier, false); err != nil {
		return AccountShareBillingCommand{}, err
	}
	if !rateMultiplier.LessThan(decimal.New(1, 10)) {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: rate_multiplier exceeds numeric(20,10)", ErrAccountShareBillingIntentInvalid)
	}
	for name, value := range map[string]*string{
		"account_rate_multiplier": &command.AccountRateMultiplier,
		"hourly_rate":             &command.HourlyRate,
	} {
		normalized, parsed, parseErr := normalizeAccountShareBillingDecimal(name, *value, false)
		if parseErr != nil {
			return AccountShareBillingCommand{}, parseErr
		}
		if !parsed.LessThan(decimal.New(1, 10)) {
			return AccountShareBillingCommand{}, fmt.Errorf("%w: %s exceeds numeric(20,10)", ErrAccountShareBillingIntentInvalid, name)
		}
		*value = normalized
	}
	ratios := make([]decimal.Decimal, 0, 3)
	for _, item := range []struct {
		name  string
		value *string
	}{
		{name: "owner_share_ratio", value: &command.OwnerShareRatio},
		{name: "invite_share_ratio", value: &command.InviteShareRatio},
		{name: "platform_share_ratio", value: &command.PlatformShareRatio},
	} {
		var parsed decimal.Decimal
		*item.value, parsed, err = normalizeAccountShareBillingDecimal(item.name, *item.value, true)
		if err != nil {
			return AccountShareBillingCommand{}, err
		}
		ratios = append(ratios, parsed)
	}
	if ratios[0].Add(ratios[1]).Add(ratios[2]).GreaterThan(decimal.NewFromInt(1)) {
		return AccountShareBillingCommand{}, fmt.Errorf("%w: share ratios must not exceed 1", ErrAccountShareBillingIntentInvalid)
	}
	return command, nil
}

func normalizeAccountShareBillingUsage(usage AccountShareBillingUsagePayloadV2) (AccountShareBillingUsagePayloadV2, error) {
	if usage.SchemaVersion != AccountShareBillingUsageSchemaV2 {
		return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: unsupported usage schema_version", ErrAccountShareBillingIntentInvalid)
	}
	if usage.UsageOccurredAt.IsZero() {
		return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: usage_occurred_at is required", ErrAccountShareBillingIntentInvalid)
	}
	usage.UsageOccurredAt = usage.UsageOccurredAt.UTC()
	usage.Model = strings.TrimSpace(usage.Model)
	if err := validateAccountShareBillingIdentifier("model", usage.Model, 255); err != nil {
		return AccountShareBillingUsagePayloadV2{}, err
	}
	usage.UpstreamModel = strings.TrimSpace(usage.UpstreamModel)
	usage.ServiceTier = strings.TrimSpace(usage.ServiceTier)
	usage.ReasoningEffort = strings.TrimSpace(usage.ReasoningEffort)
	usage.BillingTier = strings.TrimSpace(usage.BillingTier)
	usage.BillingMode = strings.TrimSpace(usage.BillingMode)
	for name, value := range map[string]int64{
		"input_tokens":             usage.InputTokens,
		"output_tokens":            usage.OutputTokens,
		"cache_creation_tokens":    usage.CacheCreationTokens,
		"cache_creation_5m_tokens": usage.CacheCreation5mTokens,
		"cache_creation_1h_tokens": usage.CacheCreation1hTokens,
		"cache_read_tokens":        usage.CacheReadTokens,
		"image_input_tokens":       usage.ImageInputTokens,
		"image_output_tokens":      usage.ImageOutputTokens,
		"image_count":              usage.ImageCount,
		"video_count":              usage.VideoCount,
		"duration_ms":              usage.DurationMilliseconds,
	} {
		if value < 0 {
			return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: %s must not be negative", ErrAccountShareBillingIntentInvalid, name)
		}
	}
	for name, value := range map[string]*int64{
		"video_duration_seconds": usage.VideoDurationSeconds,
		"first_token_ms":         usage.FirstTokenMilliseconds,
	} {
		if value != nil && *value < 0 {
			return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: %s must not be negative", ErrAccountShareBillingIntentInvalid, name)
		}
	}
	for name, value := range map[string]*string{
		"applied_rate_multiplier":       &usage.AppliedRateMultiplier,
		"input_cost":                    &usage.InputCost,
		"output_cost":                   &usage.OutputCost,
		"cache_creation_cost":           &usage.CacheCreationCost,
		"cache_read_cost":               &usage.CacheReadCost,
		"image_input_cost":              &usage.ImageInputCost,
		"image_output_cost":             &usage.ImageOutputCost,
		"total_cost":                    &usage.TotalCost,
		"actual_cost":                   &usage.ActualCost,
		"balance_cost":                  &usage.BalanceCost,
		"subscription_cost":             &usage.SubscriptionCost,
		"private_group_commission_cost": &usage.PrivateGroupCommissionCost,
		"api_key_quota_cost":            &usage.APIKeyQuotaCost,
		"api_key_rate_limit_cost":       &usage.APIKeyRateLimitCost,
		"account_quota_cost":            &usage.AccountQuotaCost,
		"base_charge":                   &usage.BaseCharge,
		"hourly_charge":                 &usage.HourlyCharge,
		"total_charge":                  &usage.TotalCharge,
	} {
		normalized, parsed, err := normalizeAccountShareBillingDecimal(name, *value, false)
		if err != nil {
			return AccountShareBillingUsagePayloadV2{}, err
		}
		if !parsed.LessThan(decimal.New(1, 10)) {
			return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: %s exceeds numeric(20,10)", ErrAccountShareBillingIntentInvalid, name)
		}
		*value = normalized
	}
	if usage.AccountStatsCost != nil {
		normalized, parsed, err := normalizeAccountShareBillingDecimal("account_stats_cost", *usage.AccountStatsCost, false)
		if err != nil {
			return AccountShareBillingUsagePayloadV2{}, err
		}
		if !parsed.LessThan(decimal.New(1, 10)) {
			return AccountShareBillingUsagePayloadV2{}, fmt.Errorf("%w: account_stats_cost exceeds numeric(20,10)", ErrAccountShareBillingIntentInvalid)
		}
		usage.AccountStatsCost = &normalized
	}
	for name, value := range map[string]string{
		"upstream_model":   usage.UpstreamModel,
		"service_tier":     usage.ServiceTier,
		"reasoning_effort": usage.ReasoningEffort,
		"image_size":       usage.ImageSize,
		"media_type":       usage.MediaType,
		"video_resolution": usage.VideoResolution,
		"billing_tier":     usage.BillingTier,
		"billing_mode":     usage.BillingMode,
	} {
		maxRunes := 100
		if name == "upstream_model" {
			maxRunes = 255
		}
		if err := validateOptionalAccountShareBillingText(name, strings.TrimSpace(value), maxRunes); err != nil {
			return AccountShareBillingUsagePayloadV2{}, err
		}
	}
	usage.ImageSize = strings.TrimSpace(usage.ImageSize)
	usage.MediaType = strings.TrimSpace(usage.MediaType)
	usage.VideoResolution = strings.TrimSpace(usage.VideoResolution)
	return usage, nil
}

func normalizeAccountShareBillingResponse(summary AccountShareBillingResponseSummaryV1) (AccountShareBillingResponseSummaryV1, error) {
	if summary.SchemaVersion != AccountShareBillingResponseSchemaV1 {
		return AccountShareBillingResponseSummaryV1{}, fmt.Errorf("%w: unsupported response schema_version", ErrAccountShareBillingIntentInvalid)
	}
	if summary.HTTPStatus != 0 && (summary.HTTPStatus < 100 || summary.HTTPStatus > 599) {
		return AccountShareBillingResponseSummaryV1{}, fmt.Errorf("%w: invalid http_status", ErrAccountShareBillingIntentInvalid)
	}
	summary.ProviderRequestID = strings.TrimSpace(summary.ProviderRequestID)
	summary.FinishReason = strings.TrimSpace(summary.FinishReason)
	summary.ErrorCode = strings.TrimSpace(summary.ErrorCode)
	for name, value := range map[string]string{
		"provider_request_id": summary.ProviderRequestID,
		"finish_reason":       summary.FinishReason,
		"error_code":          summary.ErrorCode,
	} {
		if err := validateOptionalAccountShareBillingText(name, value, 255); err != nil {
			return AccountShareBillingResponseSummaryV1{}, err
		}
	}
	return summary, nil
}

func normalizeAccountShareBillingDecimal(name, value string, ratio bool) (string, decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "0"
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 64 {
		return "", decimal.Zero, fmt.Errorf("%w: %s decimal is invalid or too long", ErrAccountShareBillingIntentInvalid, name)
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() {
		return "", decimal.Zero, fmt.Errorf("%w: %s must be a non-negative decimal", ErrAccountShareBillingIntentInvalid, name)
	}
	if ratio && parsed.GreaterThan(decimal.NewFromInt(1)) {
		return "", decimal.Zero, fmt.Errorf("%w: %s must not exceed 1", ErrAccountShareBillingIntentInvalid, name)
	}
	return parsed.String(), parsed, nil
}

func validateAccountShareBillingIdentifier(name, value string, maxRunes int) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%w: %s is required, invalid, or too long", ErrAccountShareBillingIntentInvalid, name)
	}
	return nil
}

func validateOptionalAccountShareBillingText(name, value string, maxRunes int) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%w: %s is invalid or too long", ErrAccountShareBillingIntentInvalid, name)
	}
	return nil
}

func decodeStrictAccountShareBillingJSON(payload []byte, target any) error {
	if len(bytes.TrimSpace(payload)) == 0 {
		return errors.New("payload is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("payload contains multiple JSON values")
		}
		return err
	}
	return nil
}

func hashAccountShareBillingPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
