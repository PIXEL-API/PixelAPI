package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const (
	SubsiteStatusPending     = "pending"
	SubsiteStatusActive      = "active"
	SubsiteStatusMaintenance = "maintenance"
	SubsiteStatusUnhealthy   = "unhealthy"
	SubsiteStatusDisabled    = "disabled"

	AccountLeaseStatusActive   = "active"
	AccountLeaseStatusRenewing = "renewing"
	AccountLeaseStatusDraining = "draining"
	AccountLeaseStatusReleased = "released"
	AccountLeaseStatusExpired  = "expired"
	AccountLeaseStatusRevoked  = "revoked"

	QuotaReservationStatusReserved = "reserved"
	QuotaReservationStatusSettled  = "settled"
	QuotaReservationStatusCanceled = "canceled"
	QuotaReservationStatusExpired  = "expired"

	SubsiteAuthHeaderID        = "X-Subsite-ID"
	SubsiteAuthHeaderTimestamp = "X-Subsite-Timestamp"
	SubsiteAuthHeaderNonce     = "X-Subsite-Nonce"
	SubsiteAuthHeaderBodySHA   = "X-Subsite-Body-SHA256"
	SubsiteAuthHeaderSignature = "X-Subsite-Signature"

	DefaultSubsiteHeartbeatTimeout       = 45 * time.Second
	DefaultSubsiteMaintenanceInterval    = 15 * time.Second
	DefaultSubsiteMaintenanceRunTimeout  = 10 * time.Second
	DefaultSubsiteReservationExpiryGrace = 24 * time.Hour

	DefaultSubsiteEstimatedInputTokens           = 4096
	DefaultSubsiteEstimatedOutputTokens          = 8192
	DefaultSubsiteEstimatedUnboundedOutputTokens = 128000
)

var (
	ErrSubsiteNotFound                    = infraerrors.NotFound("SUBSITE_NOT_FOUND", "subsite not found")
	ErrSubsiteInvalidInput                = infraerrors.BadRequest("SUBSITE_INVALID_INPUT", "invalid subsite input")
	ErrSubsiteInvalidStatus               = infraerrors.Forbidden("SUBSITE_INVALID_STATUS", "subsite status does not allow this operation")
	ErrSubsiteAuthRequired                = infraerrors.Unauthorized("SUBSITE_AUTH_REQUIRED", "subsite signature is required")
	ErrSubsiteAuthInvalid                 = infraerrors.Unauthorized("SUBSITE_AUTH_INVALID", "subsite signature is invalid")
	ErrSubsiteNonceReplay                 = infraerrors.Unauthorized("SUBSITE_NONCE_REPLAY", "subsite signature nonce has already been used")
	ErrAccountLeaseNotFound               = infraerrors.NotFound("ACCOUNT_LEASE_NOT_FOUND", "account lease not found")
	ErrAccountLeaseConflict               = infraerrors.Conflict("ACCOUNT_LEASE_CONFLICT", "account already has an effective lease")
	ErrAccountLeaseInUse                  = infraerrors.Conflict("ACCOUNT_LEASE_IN_USE", "account lease still has active reservations")
	ErrAccountLeaseInvalidStatus          = infraerrors.Forbidden("ACCOUNT_LEASE_INVALID_STATUS", "account lease status does not allow this operation")
	ErrQuotaReservationNotFound           = infraerrors.NotFound("QUOTA_RESERVATION_NOT_FOUND", "quota reservation not found")
	ErrQuotaReservationConflict           = infraerrors.Conflict("QUOTA_RESERVATION_CONFLICT", "quota reservation conflict")
	ErrQuotaReservationCostRequired       = infraerrors.BadRequest("QUOTA_RESERVATION_COST_REQUIRED", "estimated cost must be greater than zero")
	ErrQuotaReservationInsufficientFunds  = infraerrors.Forbidden("QUOTA_RESERVATION_INSUFFICIENT_FUNDS", "insufficient available balance for reservation")
	ErrSubsiteAuthorizeNoLease            = infraerrors.ServiceUnavailable("SUBSITE_NO_ACCOUNT_LEASE", "no active account lease is available for this subsite")
	ErrSubsiteAuthorizeGroupRequired      = infraerrors.Forbidden("SUBSITE_GROUP_REQUIRED", "api key must be bound to a group for subsite routing")
	ErrSubsiteAuthorizeModelMismatch      = infraerrors.BadRequest("SUBSITE_MODEL_MISMATCH", "requested model or platform is not available on the selected lease")
	ErrSubsiteLeaseCapacityExceeded       = infraerrors.TooManyRequests("SUBSITE_LEASE_CAPACITY_EXCEEDED", "subsite account lease capacity is exhausted")
	ErrSubsiteUsageBatchEmpty             = infraerrors.BadRequest("SUBSITE_USAGE_BATCH_EMPTY", "usage batch is empty")
	ErrSubsiteUsageReservationMismatch    = infraerrors.Conflict("SUBSITE_USAGE_RESERVATION_MISMATCH", "usage item does not match its reservation")
	ErrSubsiteUsagePayloadFingerprintMiss = infraerrors.BadRequest("SUBSITE_USAGE_FINGERPRINT_REQUIRED", "usage request fingerprint is required")
	ErrSubsiteUsageReservationNotActive   = infraerrors.Conflict("SUBSITE_USAGE_RESERVATION_NOT_ACTIVE", "usage reservation is not active")
	ErrSubsiteUsageCostExceedsReservation = infraerrors.Conflict("SUBSITE_USAGE_COST_EXCEEDS_RESERVATION", "usage cost exceeds reserved maximum")
)

type Subsite struct {
	ID               int64          `json:"id"`
	SubsiteID        string         `json:"subsite_id"`
	Name             string         `json:"name"`
	PublicURL        string         `json:"public_url"`
	Region           string         `json:"region"`
	Capabilities     []string       `json:"capabilities"`
	Status           string         `json:"status"`
	SecretHash       string         `json:"-"`
	SecretCiphertext string         `json:"-"`
	MaxQPS           int            `json:"max_qps"`
	MaxConcurrency   int            `json:"max_concurrency"`
	Version          string         `json:"version"`
	LastHeartbeatAt  *time.Time     `json:"last_heartbeat_at,omitempty"`
	HealthScore      int            `json:"health_score"`
	LastSeenIP       string         `json:"last_seen_ip"`
	Metadata         map[string]any `json:"metadata"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        *time.Time     `json:"deleted_at,omitempty"`
}

type AccountLease struct {
	ID             int64      `json:"id"`
	LeaseID        string     `json:"lease_id"`
	SubsiteID      string     `json:"subsite_id"`
	AccountID      int64      `json:"account_id"`
	GroupID        int64      `json:"group_id"`
	AccountName    string     `json:"account_name,omitempty"`
	GroupName      string     `json:"group_name,omitempty"`
	Platform       string     `json:"platform"`
	Status         string     `json:"status"`
	MaxConcurrency int        `json:"max_concurrency"`
	MaxRequests    int        `json:"max_requests"`
	MaxTokens      int64      `json:"max_tokens"`
	UsedRequests   int64      `json:"used_requests"`
	UsedTokens     int64      `json:"used_tokens"`
	AssignedAt     time.Time  `json:"assigned_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	RenewedAt      *time.Time `json:"renewed_at,omitempty"`
	ReleasedAt     *time.Time `json:"released_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type QuotaReservation struct {
	ID                 int64      `json:"id"`
	ReservationID      string     `json:"reservation_id"`
	RequestID          string     `json:"request_id"`
	SubsiteID          string     `json:"subsite_id"`
	LeaseID            string     `json:"lease_id"`
	AccountID          int64      `json:"account_id"`
	APIKeyID           int64      `json:"api_key_id"`
	UserID             int64      `json:"user_id"`
	GroupID            *int64     `json:"group_id,omitempty"`
	SubscriptionID     *int64     `json:"subscription_id,omitempty"`
	Platform           string     `json:"platform"`
	RequestedModel     string     `json:"requested_model"`
	MappedModel        string     `json:"mapped_model"`
	EstimatedCost      float64    `json:"estimated_cost"`
	ReservedRequests   int64      `json:"reserved_requests"`
	ReservedTokens     int64      `json:"reserved_tokens"`
	ActiveRequestUnits int64      `json:"active_request_units"`
	ActualCost         *float64   `json:"actual_cost,omitempty"`
	BillingType        int8       `json:"billing_type"`
	Status             string     `json:"status"`
	RequestFingerprint string     `json:"request_fingerprint"`
	ExpiresAt          time.Time  `json:"expires_at"`
	SettledAt          *time.Time `json:"settled_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type SubsiteHeartbeat struct {
	ID             int64          `json:"id"`
	SubsiteID      string         `json:"subsite_id"`
	Status         string         `json:"status"`
	Version        string         `json:"version"`
	ActiveRequests int            `json:"active_requests"`
	QueuedUsage    int            `json:"queued_usage"`
	QPS            float64        `json:"qps"`
	CPUPercent     float64        `json:"cpu_percent"`
	MemoryBytes    int64          `json:"memory_bytes"`
	Metadata       map[string]any `json:"metadata"`
	ReportedAt     time.Time      `json:"reported_at"`
	RemoteIP       string         `json:"remote_ip"`
	CreatedAt      time.Time      `json:"created_at"`
}

type CreateSubsiteInput struct {
	SubsiteID      string         `json:"subsite_id"`
	Name           string         `json:"name"`
	PublicURL      string         `json:"public_url"`
	Region         string         `json:"region"`
	Capabilities   []string       `json:"capabilities"`
	MaxQPS         int            `json:"max_qps"`
	MaxConcurrency int            `json:"max_concurrency"`
	Version        string         `json:"version"`
	Metadata       map[string]any `json:"metadata"`
}

type CreateSubsiteResult struct {
	Subsite *Subsite `json:"subsite"`
	Secret  string   `json:"secret"`
}

type ResetSubsiteSecretResult struct {
	Subsite *Subsite `json:"subsite"`
	Secret  string   `json:"secret"`
}

type UpdateSubsiteInput struct {
	Name           *string         `json:"name"`
	PublicURL      *string         `json:"public_url"`
	Region         *string         `json:"region"`
	Capabilities   *[]string       `json:"capabilities"`
	MaxQPS         *int            `json:"max_qps"`
	MaxConcurrency *int            `json:"max_concurrency"`
	Version        *string         `json:"version"`
	Metadata       *map[string]any `json:"metadata"`
}

type ListSubsitesFilter struct {
	Status string
	Search string
}

type CreateAccountLeaseInput struct {
	SubsiteID      string     `json:"subsite_id"`
	AccountID      int64      `json:"account_id"`
	GroupID        int64      `json:"group_id"`
	MaxConcurrency int        `json:"max_concurrency"`
	MaxRequests    int        `json:"max_requests"`
	MaxTokens      int64      `json:"max_tokens"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

type RenewAccountLeaseInput struct {
	SubsiteID string    `json:"subsite_id,omitempty"`
	LeaseID   string    `json:"lease_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SubsiteHeartbeatInput struct {
	SubsiteID      string         `json:"subsite_id"`
	Status         string         `json:"status"`
	Version        string         `json:"version"`
	ActiveRequests int            `json:"active_requests"`
	QueuedUsage    int            `json:"queued_usage"`
	QPS            float64        `json:"qps"`
	CPUPercent     float64        `json:"cpu_percent"`
	MemoryBytes    int64          `json:"memory_bytes"`
	Metadata       map[string]any `json:"metadata"`
	RemoteIP       string         `json:"remote_ip"`
	ReportedAt     time.Time      `json:"reported_at"`
}

type AuthorizeSubsiteRequestInput struct {
	SubsiteID                      string   `json:"subsite_id"`
	APIKey                         string   `json:"api_key"`
	Platform                       string   `json:"platform"`
	RequestedModel                 string   `json:"requested_model"`
	MappedModel                    string   `json:"mapped_model"`
	EstimatedCost                  float64  `json:"estimated_cost"`
	EstimatedInputTokens           int      `json:"estimated_input_tokens,omitempty"`
	EstimatedOutputTokens          int      `json:"estimated_output_tokens,omitempty"`
	EstimatedCacheCreationTokens   int      `json:"estimated_cache_creation_tokens,omitempty"`
	EstimatedCacheCreation5mTokens int      `json:"estimated_cache_creation_5m_tokens,omitempty"`
	EstimatedCacheCreation1hTokens int      `json:"estimated_cache_creation_1h_tokens,omitempty"`
	EstimatedCacheReadTokens       int      `json:"estimated_cache_read_tokens,omitempty"`
	EstimatedImageOutputTokens     int      `json:"estimated_image_output_tokens,omitempty"`
	EstimatedImageCount            int      `json:"estimated_image_count,omitempty"`
	EstimatedImageSize             string   `json:"estimated_image_size,omitempty"`
	ServiceTier                    string   `json:"service_tier,omitempty"`
	ReasoningEffort                string   `json:"reasoning_effort,omitempty"`
	RequestFingerprint             string   `json:"request_fingerprint"`
	ClientIP                       string   `json:"client_ip"`
	UserAgent                      string   `json:"user_agent"`
	InboundEndpoint                string   `json:"inbound_endpoint"`
	PreferredLeaseID               string   `json:"preferred_lease_id,omitempty"`
	PreferredAccountID             int64    `json:"preferred_account_id,omitempty"`
	ExcludedLeaseIDs               []string `json:"excluded_lease_ids,omitempty"`
	ExcludedAccountIDs             []int64  `json:"excluded_account_ids,omitempty"`
}

type AuthorizeSubsiteResponse struct {
	RequestID      string             `json:"request_id"`
	ReservationID  string             `json:"reservation_id"`
	SubsiteID      string             `json:"subsite_id"`
	LeaseID        string             `json:"lease_id"`
	AccountID      int64              `json:"account_id"`
	APIKeyID       int64              `json:"api_key_id"`
	UserID         int64              `json:"user_id"`
	GroupID        *int64             `json:"group_id,omitempty"`
	SubscriptionID *int64             `json:"subscription_id,omitempty"`
	Platform       string             `json:"platform"`
	RequestedModel string             `json:"requested_model"`
	MappedModel    string             `json:"mapped_model"`
	MaxCost        float64            `json:"max_cost"`
	ExpiresAt      time.Time          `json:"expires_at"`
	BillingType    int8               `json:"billing_type"`
	Credential     CredentialSnapshot `json:"credential"`
}

type CredentialSnapshot struct {
	AccountType  string         `json:"account_type"`
	AccountLevel string         `json:"account_level"`
	Credentials  map[string]any `json:"credentials"`
	Extra        map[string]any `json:"extra"`
	ExpiresAt    time.Time      `json:"expires_at"`
}

type UsageIngestBatch struct {
	SubsiteID string            `json:"subsite_id"`
	Items     []UsageIngestItem `json:"items"`
}

type UsageIngestItem struct {
	RequestID                  string    `json:"request_id"`
	ReservationID              string    `json:"reservation_id"`
	APIKeyID                   int64     `json:"api_key_id"`
	UserID                     int64     `json:"user_id"`
	AccountID                  int64     `json:"account_id"`
	GroupID                    *int64    `json:"group_id"`
	SubscriptionID             *int64    `json:"subscription_id"`
	AccountType                string    `json:"account_type"`
	Model                      string    `json:"model"`
	RequestedModel             string    `json:"requested_model"`
	UpstreamModel              *string   `json:"upstream_model"`
	ServiceTier                string    `json:"service_tier"`
	ReasoningEffort            string    `json:"reasoning_effort"`
	BillingType                int8      `json:"billing_type"`
	RequestType                int16     `json:"request_type"`
	InputTokens                int       `json:"input_tokens"`
	OutputTokens               int       `json:"output_tokens"`
	CacheCreationTokens        int       `json:"cache_creation_tokens"`
	CacheCreation5mTokens      int       `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens      int       `json:"cache_creation_1h_tokens"`
	CacheReadTokens            int       `json:"cache_read_tokens"`
	ImageOutputTokens          int       `json:"image_output_tokens"`
	ImageCount                 int       `json:"image_count"`
	ImageSize                  string    `json:"image_size"`
	MediaType                  string    `json:"media_type"`
	InputCost                  float64   `json:"input_cost"`
	OutputCost                 float64   `json:"output_cost"`
	CacheCreationCost          float64   `json:"cache_creation_cost"`
	CacheReadCost              float64   `json:"cache_read_cost"`
	ImageOutputCost            float64   `json:"image_output_cost"`
	TotalCost                  float64   `json:"total_cost"`
	BalanceCost                float64   `json:"balance_cost"`
	SubscriptionCost           float64   `json:"subscription_cost"`
	PrivateGroupCommissionCost float64   `json:"private_group_commission_cost"`
	RateMultiplier             float64   `json:"rate_multiplier"`
	AccountRateMultiplier      float64   `json:"account_rate_multiplier"`
	APIKeyQuotaCost            float64   `json:"api_key_quota_cost"`
	APIKeyRateLimitCost        float64   `json:"api_key_rate_limit_cost"`
	AccountQuotaCost           float64   `json:"account_quota_cost"`
	RequestFingerprint         string    `json:"request_fingerprint"`
	RequestPayloadHash         string    `json:"request_payload_hash"`
	InboundEndpoint            string    `json:"inbound_endpoint"`
	UpstreamEndpoint           string    `json:"upstream_endpoint"`
	UserAgent                  string    `json:"user_agent"`
	IPAddress                  string    `json:"ip_address"`
	DurationMs                 *int      `json:"duration_ms,omitempty"`
	FirstTokenMs               *int      `json:"first_token_ms,omitempty"`
	OccurredAt                 time.Time `json:"occurred_at"`

	costCalculatedByMaster bool
}

type UsageIngestResult struct {
	Accepted  int                     `json:"accepted"`
	Applied   int                     `json:"applied"`
	Duplicate int                     `json:"duplicate"`
	Failed    int                     `json:"failed"`
	Items     []UsageIngestItemResult `json:"items,omitempty"`
}

type UsageIngestItemResult struct {
	RequestID     string `json:"request_id"`
	ReservationID string `json:"reservation_id"`
	Applied       bool   `json:"applied"`
	Duplicate     bool   `json:"duplicate"`
	Error         string `json:"error,omitempty"`
}

type SubsiteMaintenanceInput struct {
	Now                    time.Time     `json:"now"`
	HeartbeatTimeout       time.Duration `json:"heartbeat_timeout"`
	ReservationExpiryGrace time.Duration `json:"reservation_expiry_grace"`
}

type SubsiteMaintenanceResult struct {
	ExpiredLeases              int64 `json:"expired_leases"`
	ExpiredReservations        int64 `json:"expired_reservations"`
	UnhealthySubsites          int64 `json:"unhealthy_subsites"`
	HeartbeatTimeoutSecs       int64 `json:"heartbeat_timeout_secs"`
	ReservationExpiryGraceSecs int64 `json:"reservation_expiry_grace_secs"`
}

type SubsiteRepository interface {
	Create(ctx context.Context, subsite *Subsite) error
	GetBySubsiteID(ctx context.Context, subsiteID string) (*Subsite, error)
	List(ctx context.Context, params pagination.PaginationParams, filter ListSubsitesFilter) ([]Subsite, *pagination.PaginationResult, error)
	Update(ctx context.Context, subsite *Subsite) error
	UpdateStatus(ctx context.Context, subsiteID, status string) error
	UpdateSecret(ctx context.Context, subsiteID, secretHash, secretCiphertext string) error
	RecordHeartbeat(ctx context.Context, heartbeat *SubsiteHeartbeat) error
	MarkHeartbeatTimeouts(ctx context.Context, before time.Time) (int64, error)
}

type AccountLeaseRepository interface {
	Create(ctx context.Context, lease *AccountLease) error
	GetByLeaseID(ctx context.Context, leaseID string) (*AccountLease, error)
	ListBySubsite(ctx context.Context, subsiteID string) ([]AccountLease, error)
	ListBySubsitePaginated(ctx context.Context, subsiteID string, params pagination.PaginationParams) ([]AccountLease, *pagination.PaginationResult, error)
	ListActiveBySubsite(ctx context.Context, subsiteID string) ([]AccountLease, error)
	ListActiveAccountIDsBySubsite(ctx context.Context, subsiteID string) ([]int64, error)
	UpdateLimitsForSubsite(ctx context.Context, subsiteID, leaseID string, maxConcurrency int, maxRequests int, maxTokens int64) (*AccountLease, error)
	Renew(ctx context.Context, leaseID string, expiresAt time.Time) (*AccountLease, error)
	RenewForSubsite(ctx context.Context, subsiteID, leaseID string, expiresAt time.Time) (*AccountLease, error)
	Release(ctx context.Context, leaseID string) (*AccountLease, error)
	ReleaseForSubsite(ctx context.Context, subsiteID, leaseID string) (*AccountLease, error)
	Drain(ctx context.Context, leaseID string) (*AccountLease, error)
	DrainForSubsite(ctx context.Context, subsiteID, leaseID string) (*AccountLease, error)
	DeleteForSubsite(ctx context.Context, subsiteID, leaseID string) (*AccountLease, error)
	IncrementUsage(ctx context.Context, leaseID string, requests int64, tokens int64) error
	ExpireStale(ctx context.Context, now time.Time) (int64, error)
}

type SubsiteSubscriptionAuthorizer interface {
	GetActiveSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	CheckUsageLimits(ctx context.Context, sub *UserSubscription, group *Group, additionalCost float64) error
	ValidateAndCheckLimits(sub *UserSubscription, group *Group) (needsMaintenance bool, err error)
	DoWindowMaintenance(sub *UserSubscription)
}

type QuotaReservationRepository interface {
	Create(ctx context.Context, reservation *QuotaReservation) error
	GetByRequestID(ctx context.Context, requestID string) (*QuotaReservation, error)
	GetByReservationID(ctx context.Context, reservationID string) (*QuotaReservation, error)
	Cancel(ctx context.Context, requestID string) error
	CancelForSubsite(ctx context.Context, subsiteID, requestID string) error
	Settle(ctx context.Context, requestID string, actualCost float64) error
	ExpireStale(ctx context.Context, now time.Time) (int64, error)
}

type SubsiteNonceStore interface {
	Claim(ctx context.Context, subsiteID, nonce string, ttl time.Duration) (bool, error)
}

type SubsiteService struct {
	repo      SubsiteRepository
	encryptor SecretEncryptor
}

func NewSubsiteService(repo SubsiteRepository, encryptor SecretEncryptor) *SubsiteService {
	return &SubsiteService{repo: repo, encryptor: encryptor}
}

func (s *SubsiteService) Create(ctx context.Context, input CreateSubsiteInput) (*CreateSubsiteResult, error) {
	if s == nil || s.repo == nil || s.encryptor == nil {
		return nil, errors.New("subsite service dependencies are nil")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.PublicURL = strings.TrimSpace(input.PublicURL)
	input.SubsiteID = strings.TrimSpace(input.SubsiteID)
	if input.Name == "" || input.PublicURL == "" {
		return nil, ErrSubsiteInvalidInput
	}
	if input.SubsiteID == "" {
		input.SubsiteID = "site_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	secret, err := generateSubsiteSecret()
	if err != nil {
		return nil, fmt.Errorf("generate subsite secret: %w", err)
	}
	encrypted, err := s.encryptor.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt subsite secret: %w", err)
	}
	now := time.Now()
	subsite := &Subsite{
		SubsiteID:        input.SubsiteID,
		Name:             input.Name,
		PublicURL:        input.PublicURL,
		Region:           strings.TrimSpace(input.Region),
		Capabilities:     normalizeStringList(input.Capabilities),
		Status:           SubsiteStatusPending,
		SecretHash:       hashSubsiteSecret(secret),
		SecretCiphertext: encrypted,
		MaxQPS:           clampNonNegativeInt(input.MaxQPS),
		MaxConcurrency:   clampNonNegativeInt(input.MaxConcurrency),
		Version:          strings.TrimSpace(input.Version),
		HealthScore:      100,
		Metadata:         normalizeSubsiteMap(input.Metadata),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repo.Create(ctx, subsite); err != nil {
		return nil, err
	}
	return &CreateSubsiteResult{Subsite: subsite, Secret: secret}, nil
}

func (s *SubsiteService) List(ctx context.Context, params pagination.PaginationParams, filter ListSubsitesFilter) ([]Subsite, *pagination.PaginationResult, error) {
	return s.repo.List(ctx, params, filter)
}

func (s *SubsiteService) Get(ctx context.Context, subsiteID string) (*Subsite, error) {
	return s.repo.GetBySubsiteID(ctx, strings.TrimSpace(subsiteID))
}

func (s *SubsiteService) Update(ctx context.Context, subsiteID string, input UpdateSubsiteInput) (*Subsite, error) {
	subsite, err := s.repo.GetBySubsiteID(ctx, strings.TrimSpace(subsiteID))
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		subsite.Name = strings.TrimSpace(*input.Name)
	}
	if input.PublicURL != nil {
		subsite.PublicURL = strings.TrimSpace(*input.PublicURL)
	}
	if input.Region != nil {
		subsite.Region = strings.TrimSpace(*input.Region)
	}
	if input.Capabilities != nil {
		subsite.Capabilities = normalizeStringList(*input.Capabilities)
	}
	if input.MaxQPS != nil {
		subsite.MaxQPS = clampNonNegativeInt(*input.MaxQPS)
	}
	if input.MaxConcurrency != nil {
		subsite.MaxConcurrency = clampNonNegativeInt(*input.MaxConcurrency)
	}
	if input.Version != nil {
		subsite.Version = strings.TrimSpace(*input.Version)
	}
	if input.Metadata != nil {
		subsite.Metadata = normalizeSubsiteMap(*input.Metadata)
	}
	if subsite.Name == "" || subsite.PublicURL == "" {
		return nil, ErrSubsiteInvalidInput
	}
	if err := s.repo.Update(ctx, subsite); err != nil {
		return nil, err
	}
	return subsite, nil
}

func (s *SubsiteService) Activate(ctx context.Context, subsiteID string) error {
	return s.repo.UpdateStatus(ctx, strings.TrimSpace(subsiteID), SubsiteStatusActive)
}

func (s *SubsiteService) Pause(ctx context.Context, subsiteID string) error {
	return s.repo.UpdateStatus(ctx, strings.TrimSpace(subsiteID), SubsiteStatusMaintenance)
}

func (s *SubsiteService) Resume(ctx context.Context, subsiteID string) error {
	return s.repo.UpdateStatus(ctx, strings.TrimSpace(subsiteID), SubsiteStatusActive)
}

func (s *SubsiteService) ResetSecret(ctx context.Context, subsiteID string) (*ResetSubsiteSecretResult, error) {
	if s == nil || s.repo == nil || s.encryptor == nil {
		return nil, errors.New("subsite service dependencies are nil")
	}
	subsiteID = strings.TrimSpace(subsiteID)
	if subsiteID == "" {
		return nil, ErrSubsiteInvalidInput
	}
	if _, err := s.repo.GetBySubsiteID(ctx, subsiteID); err != nil {
		return nil, err
	}
	secret, err := generateSubsiteSecret()
	if err != nil {
		return nil, fmt.Errorf("generate subsite secret: %w", err)
	}
	encrypted, err := s.encryptor.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt subsite secret: %w", err)
	}
	if err := s.repo.UpdateSecret(ctx, subsiteID, hashSubsiteSecret(secret), encrypted); err != nil {
		return nil, err
	}
	subsite, err := s.repo.GetBySubsiteID(ctx, subsiteID)
	if err != nil {
		return nil, err
	}
	return &ResetSubsiteSecretResult{Subsite: subsite, Secret: secret}, nil
}

func (s *SubsiteService) RecordHeartbeat(ctx context.Context, input SubsiteHeartbeatInput) (*Subsite, error) {
	if strings.TrimSpace(input.SubsiteID) == "" {
		return nil, ErrSubsiteInvalidInput
	}
	if strings.TrimSpace(input.Status) == "" {
		input.Status = SubsiteStatusActive
	}
	if input.ReportedAt.IsZero() {
		input.ReportedAt = time.Now()
	}
	hb := &SubsiteHeartbeat{
		SubsiteID:      strings.TrimSpace(input.SubsiteID),
		Status:         strings.TrimSpace(input.Status),
		Version:        strings.TrimSpace(input.Version),
		ActiveRequests: clampNonNegativeInt(input.ActiveRequests),
		QueuedUsage:    clampNonNegativeInt(input.QueuedUsage),
		QPS:            math.Max(0, input.QPS),
		CPUPercent:     math.Max(0, input.CPUPercent),
		MemoryBytes:    int64(math.Max(0, float64(input.MemoryBytes))),
		Metadata:       normalizeSubsiteMap(input.Metadata),
		ReportedAt:     input.ReportedAt,
		RemoteIP:       strings.TrimSpace(input.RemoteIP),
	}
	if err := s.repo.RecordHeartbeat(ctx, hb); err != nil {
		return nil, err
	}
	return s.repo.GetBySubsiteID(ctx, hb.SubsiteID)
}

func (s *SubsiteService) decryptSecret(ctx context.Context, subsiteID string) (string, error) {
	subsite, err := s.repo.GetBySubsiteID(ctx, strings.TrimSpace(subsiteID))
	if err != nil {
		return "", err
	}
	if subsite.Status == SubsiteStatusDisabled {
		return "", ErrSubsiteInvalidStatus
	}
	secret, err := s.encryptor.Decrypt(subsite.SecretCiphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt subsite secret: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(hashSubsiteSecret(secret)), []byte(subsite.SecretHash)) != 1 {
		return "", ErrSubsiteAuthInvalid
	}
	return secret, nil
}

type SubsiteAuthService struct {
	subsiteService *SubsiteService
	nonceStore     SubsiteNonceStore
	maxSkew        time.Duration
}

func NewSubsiteAuthService(subsiteService *SubsiteService, nonceStore SubsiteNonceStore) *SubsiteAuthService {
	return &SubsiteAuthService{subsiteService: subsiteService, nonceStore: nonceStore, maxSkew: 5 * time.Minute}
}

func (s *SubsiteAuthService) Verify(ctx context.Context, req SubsiteSignedRequest) error {
	if s == nil || s.subsiteService == nil || s.nonceStore == nil {
		return errors.New("subsite auth service dependencies are nil")
	}
	req.Normalize()
	if req.SubsiteID == "" || req.Timestamp == "" || req.Nonce == "" || req.BodySHA256 == "" || req.Signature == "" {
		return ErrSubsiteAuthRequired
	}
	ts, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		return ErrSubsiteAuthInvalid
	}
	now := time.Now()
	if ts.Before(now.Add(-s.maxSkew)) || ts.After(now.Add(s.maxSkew)) {
		return ErrSubsiteAuthInvalid
	}
	bodyHash := sha256.Sum256(req.Body)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(bodyHash[:])), []byte(req.BodySHA256)) != 1 {
		return ErrSubsiteAuthInvalid
	}
	secret, err := s.subsiteService.decryptSecret(ctx, req.SubsiteID)
	if err != nil {
		return err
	}
	expected := SignSubsiteRequest(secret, req.Method, req.Path, req.Timestamp, req.Nonce, req.BodySHA256)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(req.Signature)) != 1 {
		return ErrSubsiteAuthInvalid
	}
	claimed, err := s.nonceStore.Claim(ctx, req.SubsiteID, req.Nonce, s.maxSkew*2)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrSubsiteNonceReplay
	}
	return nil
}

type SubsiteSignedRequest struct {
	SubsiteID  string
	Method     string
	Path       string
	Timestamp  string
	Nonce      string
	BodySHA256 string
	Signature  string
	Body       []byte
}

func (r *SubsiteSignedRequest) Normalize() {
	r.SubsiteID = strings.TrimSpace(r.SubsiteID)
	r.Method = strings.ToUpper(strings.TrimSpace(r.Method))
	r.Path = strings.TrimSpace(r.Path)
	r.Timestamp = strings.TrimSpace(r.Timestamp)
	r.Nonce = strings.TrimSpace(r.Nonce)
	r.BodySHA256 = strings.ToLower(strings.TrimSpace(r.BodySHA256))
	r.Signature = strings.TrimSpace(r.Signature)
}

func SignSubsiteRequest(secret, method, path, timestamp, nonce, bodySHA256 string) string {
	canonical := strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		strings.TrimSpace(path),
		strings.TrimSpace(timestamp),
		strings.TrimSpace(nonce),
		strings.ToLower(strings.TrimSpace(bodySHA256)),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

type AccountLeaseService struct {
	leaseRepo   AccountLeaseRepository
	subsiteRepo SubsiteRepository
	accountRepo AccountRepository
	groupRepo   GroupRepository
}

func NewAccountLeaseService(leaseRepo AccountLeaseRepository, subsiteRepo SubsiteRepository, accountRepo AccountRepository, groupRepo GroupRepository) *AccountLeaseService {
	return &AccountLeaseService{
		leaseRepo:   leaseRepo,
		subsiteRepo: subsiteRepo,
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
	}
}

func (s *AccountLeaseService) Create(ctx context.Context, input CreateAccountLeaseInput) (*AccountLease, error) {
	if strings.TrimSpace(input.SubsiteID) == "" || input.AccountID <= 0 || input.GroupID <= 0 {
		return nil, ErrSubsiteInvalidInput
	}
	subsite, err := s.subsiteRepo.GetBySubsiteID(ctx, strings.TrimSpace(input.SubsiteID))
	if err != nil {
		return nil, err
	}
	if subsite.Status == SubsiteStatusDisabled {
		return nil, ErrSubsiteInvalidStatus
	}
	account, err := s.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, err
	}
	if !group.IsActive() || !account.IsActive() || !account.Schedulable {
		return nil, ErrAccountLeaseInvalidStatus
	}
	if !strings.EqualFold(group.Platform, account.Platform) {
		return nil, ErrSubsiteInvalidInput
	}
	if !containsInt64(account.GroupIDs, group.ID) {
		return nil, ErrSubsiteInvalidInput
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	if input.ExpiresAt != nil {
		expiresAt = *input.ExpiresAt
	}
	if !expiresAt.After(time.Now()) {
		return nil, ErrSubsiteInvalidInput
	}
	lease := &AccountLease{
		LeaseID:        "lease_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		SubsiteID:      subsite.SubsiteID,
		AccountID:      account.ID,
		GroupID:        group.ID,
		Platform:       account.Platform,
		Status:         AccountLeaseStatusActive,
		MaxConcurrency: clampNonNegativeInt(input.MaxConcurrency),
		MaxRequests:    clampNonNegativeInt(input.MaxRequests),
		MaxTokens:      maxInt64(0, input.MaxTokens),
		AssignedAt:     time.Now(),
		ExpiresAt:      expiresAt,
	}
	if err := s.leaseRepo.Create(ctx, lease); err != nil {
		return nil, err
	}
	return lease, nil
}

func (s *AccountLeaseService) ListBySubsite(ctx context.Context, subsiteID string) ([]AccountLease, error) {
	return s.leaseRepo.ListBySubsite(ctx, strings.TrimSpace(subsiteID))
}

func (s *AccountLeaseService) ListBySubsitePaginated(ctx context.Context, subsiteID string, params pagination.PaginationParams) ([]AccountLease, *pagination.PaginationResult, error) {
	return s.leaseRepo.ListBySubsitePaginated(ctx, strings.TrimSpace(subsiteID), params)
}

func (s *AccountLeaseService) ListActiveAccountIDsBySubsite(ctx context.Context, subsiteID string) ([]int64, error) {
	return s.leaseRepo.ListActiveAccountIDsBySubsite(ctx, strings.TrimSpace(subsiteID))
}

func (s *AccountLeaseService) UpdateLimitsForSubsite(ctx context.Context, subsiteID, leaseID string, maxConcurrency int, maxRequests int, maxTokens int64) (*AccountLease, error) {
	subsiteID = strings.TrimSpace(subsiteID)
	leaseID = strings.TrimSpace(leaseID)
	if subsiteID == "" || leaseID == "" || maxConcurrency < 0 || maxRequests < 0 || maxTokens < 0 {
		return nil, ErrSubsiteInvalidInput
	}
	return s.leaseRepo.UpdateLimitsForSubsite(ctx, subsiteID, leaseID, maxConcurrency, maxRequests, maxTokens)
}

func (s *AccountLeaseService) Renew(ctx context.Context, input RenewAccountLeaseInput) (*AccountLease, error) {
	if strings.TrimSpace(input.LeaseID) == "" || !input.ExpiresAt.After(time.Now()) {
		return nil, ErrSubsiteInvalidInput
	}
	if subsiteID := strings.TrimSpace(input.SubsiteID); subsiteID != "" {
		return s.leaseRepo.RenewForSubsite(ctx, subsiteID, strings.TrimSpace(input.LeaseID), input.ExpiresAt)
	}
	return s.leaseRepo.Renew(ctx, strings.TrimSpace(input.LeaseID), input.ExpiresAt)
}

func (s *AccountLeaseService) Release(ctx context.Context, leaseID string) (*AccountLease, error) {
	return s.leaseRepo.Release(ctx, strings.TrimSpace(leaseID))
}

func (s *AccountLeaseService) ReleaseForSubsite(ctx context.Context, subsiteID, leaseID string) (*AccountLease, error) {
	if strings.TrimSpace(subsiteID) == "" || strings.TrimSpace(leaseID) == "" {
		return nil, ErrSubsiteInvalidInput
	}
	return s.leaseRepo.ReleaseForSubsite(ctx, strings.TrimSpace(subsiteID), strings.TrimSpace(leaseID))
}

func (s *AccountLeaseService) Drain(ctx context.Context, leaseID string) (*AccountLease, error) {
	return s.leaseRepo.Drain(ctx, strings.TrimSpace(leaseID))
}

func (s *AccountLeaseService) DrainForSubsite(ctx context.Context, subsiteID, leaseID string) (*AccountLease, error) {
	if strings.TrimSpace(subsiteID) == "" || strings.TrimSpace(leaseID) == "" {
		return nil, ErrSubsiteInvalidInput
	}
	return s.leaseRepo.DrainForSubsite(ctx, strings.TrimSpace(subsiteID), strings.TrimSpace(leaseID))
}

func (s *AccountLeaseService) DeleteForSubsite(ctx context.Context, subsiteID, leaseID string) (*AccountLease, error) {
	if strings.TrimSpace(subsiteID) == "" || strings.TrimSpace(leaseID) == "" {
		return nil, ErrSubsiteInvalidInput
	}
	return s.leaseRepo.DeleteForSubsite(ctx, strings.TrimSpace(subsiteID), strings.TrimSpace(leaseID))
}

type RequestAuthorizeService struct {
	subsiteRepo     SubsiteRepository
	leaseRepo       AccountLeaseRepository
	reservationRepo QuotaReservationRepository
	apiKeyService   *APIKeyService
	subscriptionSvc SubsiteSubscriptionAuthorizer
	accountRepo     AccountRepository
	billingService  *BillingService
	resolver        *ModelPricingResolver
}

func NewRequestAuthorizeService(
	subsiteRepo SubsiteRepository,
	leaseRepo AccountLeaseRepository,
	reservationRepo QuotaReservationRepository,
	apiKeyService *APIKeyService,
	subscriptionSvc SubsiteSubscriptionAuthorizer,
	accountRepo AccountRepository,
	billingService *BillingService,
	resolver *ModelPricingResolver,
) *RequestAuthorizeService {
	return &RequestAuthorizeService{
		subsiteRepo:     subsiteRepo,
		leaseRepo:       leaseRepo,
		reservationRepo: reservationRepo,
		apiKeyService:   apiKeyService,
		subscriptionSvc: subscriptionSvc,
		accountRepo:     accountRepo,
		billingService:  billingService,
		resolver:        resolver,
	}
}

func (s *RequestAuthorizeService) Authorize(ctx context.Context, input AuthorizeSubsiteRequestInput) (*AuthorizeSubsiteResponse, error) {
	input.SubsiteID = strings.TrimSpace(input.SubsiteID)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Platform = strings.TrimSpace(input.Platform)
	input.RequestedModel = strings.TrimSpace(input.RequestedModel)
	input.MappedModel = strings.TrimSpace(input.MappedModel)
	input.RequestFingerprint = strings.TrimSpace(input.RequestFingerprint)
	input.PreferredLeaseID = strings.TrimSpace(input.PreferredLeaseID)
	input.EstimatedImageSize = strings.TrimSpace(input.EstimatedImageSize)
	input.ServiceTier = strings.TrimSpace(input.ServiceTier)
	input.ReasoningEffort = strings.TrimSpace(input.ReasoningEffort)
	input.InboundEndpoint = strings.TrimSpace(input.InboundEndpoint)
	if input.SubsiteID == "" || input.APIKey == "" || input.RequestFingerprint == "" {
		return nil, ErrSubsiteInvalidInput
	}
	subsite, err := s.subsiteRepo.GetBySubsiteID(ctx, input.SubsiteID)
	if err != nil {
		return nil, err
	}
	if subsite.Status != SubsiteStatusActive {
		return nil, ErrSubsiteInvalidStatus
	}
	apiKey, err := s.apiKeyService.GetByKey(ctx, input.APIKey)
	if err != nil {
		return nil, err
	}
	if !apiKey.IsActive() {
		return nil, ErrAPIKeyExpired
	}
	if apiKey.IsExpired() {
		return nil, ErrAPIKeyExpired
	}
	if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(input.ClientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
		if !allowed {
			return nil, infraerrors.Forbidden("ACCESS_DENIED", "access denied")
		}
	}
	if apiKey.User == nil {
		return nil, ErrUserNotFound
	}
	if !apiKey.User.IsActive() {
		return nil, ErrUserNotActive
	}

	mappedModel := s.resolveAuthorizeMappedModel(input, nil)
	estimatedCost, err := s.estimateAuthorizeCost(ctx, input, apiKey, mappedModel)
	if err != nil {
		return nil, err
	}
	if estimatedCost <= 0 {
		return nil, ErrQuotaReservationCostRequired
	}
	if apiKey.IsQuotaExhausted() || (apiKey.Quota > 0 && apiKey.QuotaUsed+estimatedCost > apiKey.Quota) {
		return nil, ErrAPIKeyQuotaExhausted
	}

	var subscription *UserSubscription
	billingType := BillingTypeBalance
	if apiKey.Group != nil && apiKey.Group.IsSubscriptionType() && s.subscriptionSvc != nil {
		subscription, err = s.subscriptionSvc.GetActiveSubscription(ctx, apiKey.User.ID, apiKey.Group.ID)
		if err != nil {
			return nil, err
		}
		needsMaintenance, err := s.subscriptionSvc.ValidateAndCheckLimits(subscription, apiKey.Group)
		if err != nil {
			return nil, err
		}
		if err := s.subscriptionSvc.CheckUsageLimits(ctx, subscription, apiKey.Group, estimatedCost); err != nil {
			return nil, err
		}
		if needsMaintenance {
			maintenanceCopy := *subscription
			s.subscriptionSvc.DoWindowMaintenance(&maintenanceCopy)
		}
		billingType = BillingTypeSubscription
	} else if apiKey.User.Balance < estimatedCost {
		return nil, ErrQuotaReservationInsufficientFunds
	}

	expiresAt := time.Now().Add(10 * time.Minute)
	groupID := apiKey.GroupID
	var subscriptionID *int64
	if subscription != nil {
		subscriptionID = &subscription.ID
	}
	lease, account, reservation, err := s.authorizeAgainstLeaseCandidates(ctx, input, apiKey, groupID, subscriptionID, estimatedCost, billingType, expiresAt)
	if err != nil {
		return nil, err
	}
	requestID := reservation.RequestID
	reservationID := reservation.ReservationID
	return &AuthorizeSubsiteResponse{
		RequestID:      requestID,
		ReservationID:  reservationID,
		SubsiteID:      subsite.SubsiteID,
		LeaseID:        lease.LeaseID,
		AccountID:      account.ID,
		APIKeyID:       apiKey.ID,
		UserID:         apiKey.User.ID,
		GroupID:        groupID,
		SubscriptionID: subscriptionID,
		Platform:       account.Platform,
		RequestedModel: input.RequestedModel,
		MappedModel:    mappedModel,
		MaxCost:        estimatedCost,
		ExpiresAt:      expiresAt,
		BillingType:    billingType,
		Credential: CredentialSnapshot{
			AccountType:  account.Type,
			AccountLevel: account.AccountLevel,
			Credentials:  copySubsiteMap(account.Credentials),
			Extra:        copySubsiteMap(account.Extra),
			ExpiresAt:    expiresAt,
		},
	}, nil
}

func (s *RequestAuthorizeService) Cancel(ctx context.Context, requestID string) error {
	return s.reservationRepo.Cancel(ctx, strings.TrimSpace(requestID))
}

func (s *RequestAuthorizeService) CancelForSubsite(ctx context.Context, subsiteID, requestID string) error {
	if strings.TrimSpace(subsiteID) == "" || strings.TrimSpace(requestID) == "" {
		return ErrSubsiteInvalidInput
	}
	return s.reservationRepo.CancelForSubsite(ctx, strings.TrimSpace(subsiteID), strings.TrimSpace(requestID))
}

func (s *RequestAuthorizeService) resolveAuthorizeMappedModel(input AuthorizeSubsiteRequestInput, account *Account) string {
	mappedModel := strings.TrimSpace(input.MappedModel)
	if mappedModel == "" {
		mappedModel = strings.TrimSpace(input.RequestedModel)
	}
	if account == nil || strings.TrimSpace(input.RequestedModel) == "" {
		return mappedModel
	}
	if account.Type == AccountTypeAPIKey {
		if resolved := strings.TrimSpace(account.GetMappedModel(input.RequestedModel)); resolved != "" {
			return resolved
		}
	}
	if account.Platform == PlatformAnthropic {
		if normalized := strings.TrimSpace(claude.NormalizeModelID(input.RequestedModel)); normalized != "" {
			return normalized
		}
	}
	return mappedModel
}

func (s *RequestAuthorizeService) estimateAuthorizeCost(ctx context.Context, input AuthorizeSubsiteRequestInput, apiKey *APIKey, mappedModel string) (float64, error) {
	if s == nil || s.billingService == nil || apiKey == nil || apiKey.User == nil {
		return 0, ErrQuotaReservationCostRequired
	}
	model := strings.TrimSpace(mappedModel)
	if model == "" {
		model = strings.TrimSpace(input.RequestedModel)
	}
	if model == "" {
		return 0, ErrQuotaReservationCostRequired
	}
	groupID := apiKey.GroupID
	if groupID == nil && apiKey.Group != nil {
		id := apiKey.Group.ID
		groupID = &id
	}
	multiplier := 1.0
	if apiKey.Group != nil {
		multiplier = apiKey.Group.RateMultiplier
	}
	if groupID != nil && s.apiKeyService != nil && s.apiKeyService.userGroupRateRepo != nil {
		if userMultiplier, err := s.apiKeyService.userGroupRateRepo.GetByUserAndGroup(ctx, apiKey.User.ID, *groupID); err == nil && userMultiplier != nil {
			multiplier = *userMultiplier
		}
	}
	isImage := isSubsiteAuthorizeImageRequest(input)
	if isImage {
		requestCount := maxInt(1, input.EstimatedImageCount)
		sizeTier := normalizeSubsiteAuthorizeImageSize(input.EstimatedImageSize)
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          model,
			GroupID:        groupID,
			RequestCount:   requestCount,
			SizeTier:       sizeTier,
			RateMultiplier: multiplier,
			ServiceTier:    strings.TrimSpace(input.ServiceTier),
			Resolver:       s.resolver,
		})
		if err == nil && cost != nil && cost.ActualCost > 0 {
			return cost.ActualCost, nil
		}
		var groupConfig *ImagePriceConfig
		if apiKey.Group != nil {
			groupConfig = &ImagePriceConfig{
				Price1K: apiKey.Group.ImagePrice1K,
				Price2K: apiKey.Group.ImagePrice2K,
				Price4K: apiKey.Group.ImagePrice4K,
			}
		}
		fallback := s.billingService.CalculateImageCost(model, sizeTier, requestCount, groupConfig, multiplier)
		if fallback == nil || fallback.ActualCost <= 0 {
			if err != nil {
				return 0, err
			}
			return 0, ErrQuotaReservationCostRequired
		}
		return fallback.ActualCost, nil
	}
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx:            ctx,
		Model:          model,
		GroupID:        groupID,
		Tokens:         authorizeEstimatedUsageTokens(input),
		RateMultiplier: multiplier,
		ServiceTier:    strings.TrimSpace(input.ServiceTier),
		Resolver:       s.resolver,
	})
	if err != nil {
		return 0, err
	}
	if cost == nil || cost.ActualCost <= 0 {
		return 0, ErrQuotaReservationCostRequired
	}
	return cost.ActualCost, nil
}

func authorizeEstimatedUsageTokens(input AuthorizeSubsiteRequestInput) UsageTokens {
	inputTokens := input.EstimatedInputTokens
	if inputTokens <= 0 {
		inputTokens = DefaultSubsiteEstimatedInputTokens
	}
	outputTokens := input.EstimatedOutputTokens
	if outputTokens <= 0 {
		outputTokens = DefaultSubsiteEstimatedOutputTokens
		if strings.Contains(strings.ToLower(input.InboundEndpoint), "responses") {
			outputTokens = DefaultSubsiteEstimatedUnboundedOutputTokens
		}
	}
	return UsageTokens{
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CacheCreationTokens:   maxInt(0, input.EstimatedCacheCreationTokens),
		CacheCreation5mTokens: maxInt(0, input.EstimatedCacheCreation5mTokens),
		CacheCreation1hTokens: maxInt(0, input.EstimatedCacheCreation1hTokens),
		CacheReadTokens:       maxInt(0, input.EstimatedCacheReadTokens),
		ImageOutputTokens:     maxInt(0, input.EstimatedImageOutputTokens),
	}
}

func authorizeEstimatedLeaseTokens(input AuthorizeSubsiteRequestInput) int64 {
	tokens := authorizeEstimatedUsageTokens(input)
	total := tokens.InputTokens +
		tokens.OutputTokens +
		tokens.CacheCreationTokens +
		tokens.CacheCreation5mTokens +
		tokens.CacheCreation1hTokens +
		tokens.CacheReadTokens +
		tokens.ImageOutputTokens
	if total < 0 {
		return 0
	}
	return int64(total)
}

func isSubsiteAuthorizeImageRequest(input AuthorizeSubsiteRequestInput) bool {
	return input.EstimatedImageCount > 0 || strings.Contains(strings.ToLower(input.InboundEndpoint), "/images/")
}

func normalizeSubsiteAuthorizeImageSize(size string) string {
	value := strings.ToUpper(strings.TrimSpace(size))
	switch value {
	case "1K", "2K", "4K", "HD":
		return value
	case "256X256", "512X512", "1024X1024":
		return "1K"
	case "1024X1536", "1536X1024", "1024X1792", "1792X1024":
		return "2K"
	case "2048X2048":
		return "4K"
	default:
		return "2K"
	}
}

func (s *RequestAuthorizeService) selectLease(ctx context.Context, input AuthorizeSubsiteRequestInput, apiKey *APIKey) (*AccountLease, *Account, error) {
	groupID := int64(0)
	if apiKey != nil {
		if apiKey.GroupID != nil {
			groupID = *apiKey.GroupID
		} else if apiKey.Group != nil {
			groupID = apiKey.Group.ID
		}
	}
	if groupID <= 0 {
		return nil, nil, ErrSubsiteAuthorizeGroupRequired
	}
	leases, err := s.leaseRepo.ListActiveBySubsite(ctx, input.SubsiteID)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	preferredOnly := input.PreferredLeaseID != "" || input.PreferredAccountID > 0
	for i := range leases {
		lease := &leases[i]
		if lease.Status != AccountLeaseStatusActive && lease.Status != AccountLeaseStatusRenewing {
			continue
		}
		if !lease.ExpiresAt.After(now) {
			continue
		}
		if lease.MaxRequests > 0 && lease.UsedRequests >= int64(lease.MaxRequests) {
			continue
		}
		if lease.MaxTokens > 0 && lease.UsedTokens >= lease.MaxTokens {
			continue
		}
		if lease.GroupID != groupID {
			continue
		}
		if input.PreferredLeaseID != "" && lease.LeaseID != input.PreferredLeaseID {
			continue
		}
		if input.PreferredAccountID > 0 && lease.AccountID != input.PreferredAccountID {
			continue
		}
		if containsStringTrimmed(input.ExcludedLeaseIDs, lease.LeaseID) {
			continue
		}
		if containsInt64(input.ExcludedAccountIDs, lease.AccountID) {
			continue
		}
		if input.Platform != "" && lease.Platform != "" && !strings.EqualFold(lease.Platform, input.Platform) {
			continue
		}
		account, err := s.accountRepo.GetByID(ctx, lease.AccountID)
		if err != nil {
			return nil, nil, err
		}
		if account == nil || !account.IsSchedulable() {
			continue
		}
		if input.Platform != "" && !strings.EqualFold(account.Platform, input.Platform) {
			continue
		}
		return lease, account, nil
	}
	if !preferredOnly && isPrivateSubsiteAutoLeaseEligible(apiKey) {
		lease, account, err := s.ensurePrivateLease(ctx, input, apiKey, groupID)
		if err != nil {
			return nil, nil, err
		}
		if lease != nil && account != nil {
			return lease, account, nil
		}
	}
	if preferredOnly {
		return nil, nil, ErrSubsiteAuthorizeNoLease
	}
	return nil, nil, ErrSubsiteAuthorizeNoLease
}

func isPrivateSubsiteAutoLeaseEligible(apiKey *APIKey) bool {
	if apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return false
	}
	if apiKey.User.ID <= 0 {
		return false
	}
	if !apiKey.Group.IsSubscriptionType() || !apiKey.Group.IsUserPrivateScope() {
		return false
	}
	if apiKey.Group.OwnerUserID == nil || *apiKey.Group.OwnerUserID != apiKey.User.ID {
		return false
	}
	return true
}

func (s *RequestAuthorizeService) authorizeAgainstLeaseCandidates(
	ctx context.Context,
	input AuthorizeSubsiteRequestInput,
	apiKey *APIKey,
	groupID *int64,
	subscriptionID *int64,
	estimatedCost float64,
	billingType int8,
	expiresAt time.Time,
) (*AccountLease, *Account, *QuotaReservation, error) {
	currentInput := input
	triedLeaseIDs := make(map[string]struct{})
	triedAccountIDs := make(map[int64]struct{})
	for {
		lease, account, err := s.selectLease(ctx, currentInput, apiKey)
		if err != nil {
			return nil, nil, nil, err
		}
		requestID := "subreq_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		reservationID := "qres_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		reservation := &QuotaReservation{
			ReservationID:      reservationID,
			RequestID:          requestID,
			SubsiteID:          strings.TrimSpace(input.SubsiteID),
			LeaseID:            lease.LeaseID,
			AccountID:          account.ID,
			APIKeyID:           apiKey.ID,
			UserID:             apiKey.User.ID,
			GroupID:            groupID,
			SubscriptionID:     subscriptionID,
			Platform:           account.Platform,
			RequestedModel:     input.RequestedModel,
			MappedModel:        s.resolveAuthorizeMappedModel(input, account),
			EstimatedCost:      estimatedCost,
			ReservedRequests:   1,
			ReservedTokens:     authorizeEstimatedLeaseTokens(input),
			ActiveRequestUnits: 1,
			BillingType:        billingType,
			Status:             QuotaReservationStatusReserved,
			RequestFingerprint: input.RequestFingerprint,
			ExpiresAt:          expiresAt,
		}
		if err := s.reservationRepo.Create(ctx, reservation); err != nil {
			if errors.Is(err, ErrSubsiteLeaseCapacityExceeded) && currentInput.PreferredLeaseID == "" && currentInput.PreferredAccountID <= 0 {
				if _, seen := triedLeaseIDs[lease.LeaseID]; !seen {
					triedLeaseIDs[lease.LeaseID] = struct{}{}
					currentInput.ExcludedLeaseIDs = append(currentInput.ExcludedLeaseIDs, lease.LeaseID)
				}
				if _, seen := triedAccountIDs[account.ID]; !seen {
					triedAccountIDs[account.ID] = struct{}{}
					currentInput.ExcludedAccountIDs = append(currentInput.ExcludedAccountIDs, account.ID)
				}
				continue
			}
			return nil, nil, nil, err
		}
		return lease, account, reservation, nil
	}
}

func (s *RequestAuthorizeService) ensurePrivateLease(ctx context.Context, input AuthorizeSubsiteRequestInput, apiKey *APIKey, groupID int64) (*AccountLease, *Account, error) {
	if s == nil || s.accountRepo == nil || s.leaseRepo == nil {
		return nil, nil, nil
	}
	if groupID <= 0 || strings.TrimSpace(input.SubsiteID) == "" {
		return nil, nil, nil
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, input.Platform)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	for i := range accounts {
		account := &accounts[i]
		if containsInt64(input.ExcludedAccountIDs, account.ID) {
			continue
		}
		if !isPrivateLeaseCandidate(apiKey.User.ID, input.Platform, account) {
			continue
		}
		lease := &AccountLease{
			LeaseID:        "lease_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			SubsiteID:      strings.TrimSpace(input.SubsiteID),
			AccountID:      account.ID,
			GroupID:        groupID,
			Platform:       account.Platform,
			Status:         AccountLeaseStatusActive,
			MaxConcurrency: 0,
			AssignedAt:     now,
			ExpiresAt:      now.Add(30 * time.Minute),
		}
		if err := s.leaseRepo.Create(ctx, lease); err != nil {
			if errors.Is(err, ErrAccountLeaseConflict) {
				continue
			}
			return nil, nil, err
		}
		return lease, account, nil
	}
	return nil, nil, nil
}

func isPrivateLeaseCandidate(ownerUserID int64, requestedPlatform string, account *Account) bool {
	if account == nil {
		return false
	}
	if !account.IsSchedulable() {
		return false
	}
	if ownerUserID <= 0 || account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
		return false
	}
	if NormalizeAccountShareMode(account.ShareMode) != AccountShareModePrivate {
		return false
	}
	if requestedPlatform != "" && !strings.EqualFold(account.Platform, requestedPlatform) {
		return false
	}
	return true
}

func containsStringTrimmed(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

type UsageIngestService struct {
	billingRepo     UsageBillingRepository
	reservationRepo QuotaReservationRepository
	billingService  *BillingService
	resolver        *ModelPricingResolver
	apiKeyService   *APIKeyService
	settingService  *SettingService
	accountRepo     AccountRepository
}

func NewUsageIngestService(
	billingRepo UsageBillingRepository,
	reservationRepo QuotaReservationRepository,
	billingService *BillingService,
	resolver *ModelPricingResolver,
	apiKeyService *APIKeyService,
	settingService *SettingService,
	accountRepo AccountRepository,
) *UsageIngestService {
	return &UsageIngestService{
		billingRepo:     billingRepo,
		reservationRepo: reservationRepo,
		billingService:  billingService,
		resolver:        resolver,
		apiKeyService:   apiKeyService,
		settingService:  settingService,
		accountRepo:     accountRepo,
	}
}

func (s *UsageIngestService) Ingest(ctx context.Context, batch UsageIngestBatch) (*UsageIngestResult, error) {
	if len(batch.Items) == 0 {
		return nil, ErrSubsiteUsageBatchEmpty
	}
	result := &UsageIngestResult{
		Accepted: len(batch.Items),
		Items:    make([]UsageIngestItemResult, 0, len(batch.Items)),
	}
	for i := range batch.Items {
		item := batch.Items[i]
		itemResult := UsageIngestItemResult{
			RequestID:     strings.TrimSpace(item.RequestID),
			ReservationID: strings.TrimSpace(item.ReservationID),
		}
		applied, duplicate, err := s.ingestOne(ctx, strings.TrimSpace(batch.SubsiteID), item)
		if err != nil {
			itemResult.Error = infraerrors.Reason(err)
			if itemResult.Error == "" {
				itemResult.Error = infraerrors.Message(err)
			}
			result.Failed++
			result.Items = append(result.Items, itemResult)
			continue
		}
		itemResult.Applied = applied
		itemResult.Duplicate = duplicate
		if applied {
			result.Applied++
		} else if duplicate {
			result.Duplicate++
		}
		result.Items = append(result.Items, itemResult)
	}
	return result, nil
}

func (s *UsageIngestService) ingestOne(ctx context.Context, batchSubsiteID string, item UsageIngestItem) (bool, bool, error) {
	if strings.TrimSpace(item.RequestFingerprint) == "" {
		return false, false, ErrSubsiteUsagePayloadFingerprintMiss
	}
	reservation, err := s.reservationRepo.GetByReservationID(ctx, item.ReservationID)
	if err != nil {
		return false, false, err
	}
	if reservation.SubsiteID != batchSubsiteID ||
		reservation.RequestID != strings.TrimSpace(item.RequestID) ||
		reservation.APIKeyID != item.APIKeyID ||
		reservation.UserID != item.UserID ||
		reservation.AccountID != item.AccountID ||
		reservation.BillingType != item.BillingType ||
		!sameInt64Ptr(reservation.GroupID, item.GroupID) ||
		!sameInt64Ptr(reservation.SubscriptionID, item.SubscriptionID) {
		return false, false, ErrSubsiteUsageReservationMismatch
	}
	enriched, err := s.enrichUsageCosts(ctx, item, reservation)
	if err != nil {
		return false, false, err
	}
	cmd := usageIngestItemToBillingCommand(enriched, reservation)
	alreadySettled, err := validateUsageReservation(reservation, cmd)
	if err != nil {
		return false, false, err
	}
	if alreadySettled {
		return false, true, nil
	}
	applyResult, err := s.billingRepo.Apply(ctx, cmd)
	if err != nil {
		return false, false, err
	}
	if applyResult != nil && applyResult.Applied {
		return true, false, nil
	}
	return false, true, nil
}

func (s *UsageIngestService) enrichUsageCosts(ctx context.Context, item UsageIngestItem, reservation *QuotaReservation) (UsageIngestItem, error) {
	if reservation != nil {
		item.RequestedModel = reservation.RequestedModel
		item.Model = reservation.MappedModel
		item.GroupID = reservation.GroupID
		item.SubscriptionID = reservation.SubscriptionID
		item.BillingType = reservation.BillingType
	}
	if s == nil || s.billingService == nil || s.apiKeyService == nil || s.accountRepo == nil {
		return item, ErrQuotaReservationCostRequired
	}
	apiKey, err := s.apiKeyService.GetByID(ctx, item.APIKeyID)
	if err != nil {
		return item, err
	}
	if apiKey == nil || apiKey.User == nil {
		return item, ErrSubsiteUsageReservationMismatch
	}
	account, err := s.accountRepo.GetByID(ctx, item.AccountID)
	if err != nil {
		return item, err
	}
	if account == nil {
		return item, ErrSubsiteUsageReservationMismatch
	}
	billingModel := ""
	if reservation != nil {
		billingModel = strings.TrimSpace(reservation.MappedModel)
	}
	if billingModel == "" {
		billingModel = strings.TrimSpace(item.Model)
	}
	if billingModel == "" {
		billingModel = strings.TrimSpace(item.RequestedModel)
	}
	if billingModel == "" {
		return item, ErrQuotaReservationCostRequired
	}
	item.Model = billingModel
	if strings.TrimSpace(item.RequestedModel) == "" {
		item.RequestedModel = billingModel
	}
	item.AccountType = account.Type
	var groupID *int64
	if reservation != nil {
		groupID = reservation.GroupID
	}
	if groupID == nil {
		groupID = apiKey.GroupID
	}
	multiplier := 1.0
	if apiKey.Group != nil {
		multiplier = apiKey.Group.RateMultiplier
	}
	if groupID != nil && s.apiKeyService.userGroupRateRepo != nil {
		if userMultiplier, err := s.apiKeyService.userGroupRateRepo.GetByUserAndGroup(ctx, apiKey.User.ID, *groupID); err == nil && userMultiplier != nil {
			multiplier = *userMultiplier
		}
	}
	accountRateMultiplier := account.BillingRateMultiplier()
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx:     ctx,
		Model:   billingModel,
		GroupID: groupID,
		Tokens: UsageTokens{
			InputTokens:           item.InputTokens,
			OutputTokens:          item.OutputTokens,
			CacheCreationTokens:   item.CacheCreationTokens,
			CacheCreation5mTokens: item.CacheCreation5mTokens,
			CacheCreation1hTokens: item.CacheCreation1hTokens,
			CacheReadTokens:       item.CacheReadTokens,
			ImageOutputTokens:     item.ImageOutputTokens,
		},
		RequestCount:   maxInt(1, item.ImageCount),
		SizeTier:       strings.TrimSpace(item.ImageSize),
		RateMultiplier: multiplier,
		ServiceTier:    strings.TrimSpace(item.ServiceTier),
		Resolver:       s.resolver,
	})
	if err != nil {
		return item, err
	}
	if cost == nil {
		return item, ErrQuotaReservationCostRequired
	}
	if item.BillingType == BillingTypeSubscription {
		item.SubscriptionCost = cost.ActualCost
	} else {
		item.BalanceCost = cost.ActualCost
	}
	item.InputCost = cost.InputCost
	item.OutputCost = cost.OutputCost
	item.CacheCreationCost = cost.CacheCreationCost
	item.CacheReadCost = cost.CacheReadCost
	item.ImageOutputCost = cost.ImageOutputCost
	item.TotalCost = cost.TotalCost
	item.APIKeyQuotaCost = cost.ActualCost
	item.APIKeyRateLimitCost = cost.ActualCost
	item.RateMultiplier = multiplier
	item.AccountRateMultiplier = accountRateMultiplier
	item.AccountQuotaCost = cost.TotalCost * accountRateMultiplier
	item.PrivateGroupCommissionCost = s.calculatePrivateGroupCommissionCost(ctx, apiKey, cost, item.BillingType)
	item.costCalculatedByMaster = true
	return item, nil
}

func (s *UsageIngestService) calculatePrivateGroupCommissionCost(ctx context.Context, apiKey *APIKey, cost *CostBreakdown, billingType int8) float64 {
	if s == nil || s.settingService == nil || apiKey == nil || apiKey.Group == nil || cost == nil {
		return 0
	}
	if billingType != BillingTypeSubscription || !apiKey.Group.IsUserPrivateScope() || cost.ActualCost <= 0 {
		return 0
	}
	settings, err := s.settingService.GetAllSettings(ctx)
	if err != nil || settings == nil {
		return 0
	}
	rate := settings.UserPrivateGroupCommissionRate
	if rate <= 0 {
		return 0
	}
	if rate > 1 {
		rate = 1
	}
	return cost.ActualCost * rate
}

func validateUsageReservation(reservation *QuotaReservation, cmd *UsageBillingCommand) (bool, error) {
	if reservation == nil || cmd == nil {
		return false, ErrSubsiteUsageReservationMismatch
	}
	if reservation.BillingType != cmd.BillingType {
		return false, ErrSubsiteUsageReservationMismatch
	}
	cmd.QuotaReservationID = reservation.ReservationID
	cmd.LeaseID = reservation.LeaseID
	if strings.TrimSpace(reservation.RequestFingerprint) != "" &&
		!strings.EqualFold(strings.TrimSpace(reservation.RequestFingerprint), strings.TrimSpace(cmd.RequestFingerprint)) {
		return false, ErrSubsiteUsageReservationMismatch
	}
	if cmd.BillingType == BillingTypeBalance && cmd.SubscriptionCost > 0 {
		return false, ErrSubsiteUsageReservationMismatch
	}
	if cmd.BillingType == BillingTypeSubscription && cmd.BalanceCost > 0 {
		return false, ErrSubsiteUsageReservationMismatch
	}
	actualCost := cmd.BalanceCost
	if cmd.SubscriptionCost > actualCost {
		actualCost = cmd.SubscriptionCost
	}
	if actualCost < 0 || actualCost-reservation.EstimatedCost > 0.0000000001 {
		return false, ErrSubsiteUsageCostExceedsReservation
	}
	if reservation.Status == QuotaReservationStatusSettled {
		if reservation.ActualCost != nil && math.Abs(*reservation.ActualCost-actualCost) > 0.0000000001 {
			return false, ErrSubsiteUsageReservationMismatch
		}
		return true, nil
	}
	if reservation.Status != QuotaReservationStatusReserved {
		return false, ErrSubsiteUsageReservationNotActive
	}
	return false, nil
}

type SubsiteMaintenanceService struct {
	subsiteRepo     SubsiteRepository
	leaseRepo       AccountLeaseRepository
	reservationRepo QuotaReservationRepository
	interval        time.Duration
	stopCh          chan struct{}
	startOnce       sync.Once
	stopOnce        sync.Once
	wg              sync.WaitGroup
}

func NewSubsiteMaintenanceService(
	subsiteRepo SubsiteRepository,
	leaseRepo AccountLeaseRepository,
	reservationRepo QuotaReservationRepository,
) *SubsiteMaintenanceService {
	return &SubsiteMaintenanceService{
		subsiteRepo:     subsiteRepo,
		leaseRepo:       leaseRepo,
		reservationRepo: reservationRepo,
		interval:        DefaultSubsiteMaintenanceInterval,
		stopCh:          make(chan struct{}),
	}
}

func (s *SubsiteMaintenanceService) Start() {
	if s == nil || s.subsiteRepo == nil || s.leaseRepo == nil || s.reservationRepo == nil || s.interval <= 0 {
		return
	}
	if s.stopCh == nil {
		s.stopCh = make(chan struct{})
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()

			s.runOnce()
			for {
				select {
				case <-ticker.C:
					s.runOnce()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

func (s *SubsiteMaintenanceService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SubsiteMaintenanceService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultSubsiteMaintenanceRunTimeout)
	defer cancel()

	result, err := s.RunOnce(ctx, SubsiteMaintenanceInput{})
	if err != nil {
		log.Printf("[SubsiteMaintenance] run failed: %v", err)
		return
	}
	if result.ExpiredLeases > 0 || result.ExpiredReservations > 0 || result.UnhealthySubsites > 0 {
		log.Printf(
			"[SubsiteMaintenance] expired_leases=%d expired_reservations=%d unhealthy_subsites=%d",
			result.ExpiredLeases,
			result.ExpiredReservations,
			result.UnhealthySubsites,
		)
	}
}

func (s *SubsiteMaintenanceService) RunOnce(ctx context.Context, input SubsiteMaintenanceInput) (*SubsiteMaintenanceResult, error) {
	if s == nil || s.subsiteRepo == nil || s.leaseRepo == nil || s.reservationRepo == nil {
		return nil, errors.New("subsite maintenance service dependencies are nil")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	heartbeatTimeout := input.HeartbeatTimeout
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = DefaultSubsiteHeartbeatTimeout
	}
	reservationExpiryGrace := input.ReservationExpiryGrace
	if reservationExpiryGrace <= 0 {
		reservationExpiryGrace = DefaultSubsiteReservationExpiryGrace
	}
	expiredLeases, err := s.leaseRepo.ExpireStale(ctx, now)
	if err != nil {
		return nil, err
	}
	expiredReservations, err := s.reservationRepo.ExpireStale(ctx, now.Add(-reservationExpiryGrace))
	if err != nil {
		return nil, err
	}
	unhealthySubsites, err := s.subsiteRepo.MarkHeartbeatTimeouts(ctx, now.Add(-heartbeatTimeout))
	if err != nil {
		return nil, err
	}
	return &SubsiteMaintenanceResult{
		ExpiredLeases:              expiredLeases,
		ExpiredReservations:        expiredReservations,
		UnhealthySubsites:          unhealthySubsites,
		HeartbeatTimeoutSecs:       int64(heartbeatTimeout.Seconds()),
		ReservationExpiryGraceSecs: int64(reservationExpiryGrace.Seconds()),
	}, nil
}

func usageIngestItemToBillingCommand(item UsageIngestItem, reservation *QuotaReservation) *UsageBillingCommand {
	occurredAt := item.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	item = ensureUsageIngestCostDefaults(item)
	var serviceTier, reasoningEffort, inboundEndpoint, upstreamEndpoint, userAgent, ipAddress, mediaType *string
	if strings.TrimSpace(item.ServiceTier) != "" {
		serviceTier = stringPtr(strings.TrimSpace(item.ServiceTier))
	}
	if strings.TrimSpace(item.ReasoningEffort) != "" {
		reasoningEffort = stringPtr(strings.TrimSpace(item.ReasoningEffort))
	}
	if strings.TrimSpace(item.InboundEndpoint) != "" {
		inboundEndpoint = stringPtr(strings.TrimSpace(item.InboundEndpoint))
	}
	if strings.TrimSpace(item.UpstreamEndpoint) != "" {
		upstreamEndpoint = stringPtr(strings.TrimSpace(item.UpstreamEndpoint))
	}
	if strings.TrimSpace(item.UserAgent) != "" {
		userAgent = stringPtr(strings.TrimSpace(item.UserAgent))
	}
	if strings.TrimSpace(item.IPAddress) != "" {
		ipAddress = stringPtr(strings.TrimSpace(item.IPAddress))
	}
	if strings.TrimSpace(item.MediaType) != "" {
		mediaType = stringPtr(strings.TrimSpace(item.MediaType))
	}
	usageLog := &UsageLog{
		UserID:                item.UserID,
		APIKeyID:              item.APIKeyID,
		AccountID:             item.AccountID,
		RequestID:             strings.TrimSpace(item.RequestID),
		Model:                 strings.TrimSpace(item.Model),
		RequestedModel:        strings.TrimSpace(item.RequestedModel),
		UpstreamModel:         item.UpstreamModel,
		GroupID:               item.GroupID,
		SubscriptionID:        item.SubscriptionID,
		InputTokens:           item.InputTokens,
		OutputTokens:          item.OutputTokens,
		CacheCreationTokens:   item.CacheCreationTokens,
		CacheCreation5mTokens: item.CacheCreation5mTokens,
		CacheCreation1hTokens: item.CacheCreation1hTokens,
		CacheReadTokens:       item.CacheReadTokens,
		ImageOutputTokens:     item.ImageOutputTokens,
		ImageOutputCost:       item.ImageOutputCost,
		InputCost:             item.InputCost,
		OutputCost:            item.OutputCost,
		CacheCreationCost:     item.CacheCreationCost,
		CacheReadCost:         item.CacheReadCost,
		TotalCost:             item.TotalCost,
		ActualCost:            math.Max(item.BalanceCost, item.SubscriptionCost),
		RateMultiplier:        item.RateMultiplier,
		AccountRateMultiplier: &item.AccountRateMultiplier,
		BillingType:           item.BillingType,
		RequestType:           RequestTypeFromInt16(item.RequestType),
		Stream:                RequestTypeFromInt16(item.RequestType) == RequestTypeStream,
		OpenAIWSMode:          RequestTypeFromInt16(item.RequestType) == RequestTypeWSV2,
		UserAgent:             userAgent,
		IPAddress:             ipAddress,
		ImageCount:            item.ImageCount,
		ImageSize:             optionalStringPtr(strings.TrimSpace(item.ImageSize)),
		MediaType:             mediaType,
		ServiceTier:           serviceTier,
		ReasoningEffort:       reasoningEffort,
		InboundEndpoint:       inboundEndpoint,
		UpstreamEndpoint:      upstreamEndpoint,
		DurationMs:            item.DurationMs,
		FirstTokenMs:          item.FirstTokenMs,
		CreatedAt:             occurredAt,
	}
	usageLog.SyncRequestTypeAndLegacyFields()
	cmd := &UsageBillingCommand{
		RequestID:                  strings.TrimSpace(item.RequestID),
		APIKeyID:                   item.APIKeyID,
		RequestFingerprint:         strings.TrimSpace(item.RequestFingerprint),
		RequestPayloadHash:         strings.TrimSpace(item.RequestPayloadHash),
		QuotaReservationID:         strings.TrimSpace(item.ReservationID),
		UserID:                     item.UserID,
		AccountID:                  item.AccountID,
		GroupID:                    item.GroupID,
		SubscriptionID:             item.SubscriptionID,
		AccountType:                strings.TrimSpace(item.AccountType),
		Model:                      strings.TrimSpace(item.Model),
		BillingType:                item.BillingType,
		InputTokens:                item.InputTokens,
		OutputTokens:               item.OutputTokens,
		CacheCreationTokens:        item.CacheCreationTokens,
		CacheReadTokens:            item.CacheReadTokens,
		ImageCount:                 item.ImageCount,
		MediaType:                  strings.TrimSpace(item.MediaType),
		BalanceCost:                item.BalanceCost,
		SubscriptionCost:           item.SubscriptionCost,
		PrivateGroupCommissionCost: item.PrivateGroupCommissionCost,
		APIKeyQuotaCost:            item.APIKeyQuotaCost,
		APIKeyRateLimitCost:        item.APIKeyRateLimitCost,
		AccountQuotaCost:           item.AccountQuotaCost,
		LeaseUsageRequests:         1,
		LeaseUsageTokens:           usageIngestItemLeaseTokenCount(item),
		UsageOccurredAt:            occurredAt,
		UsageLog:                   usageLog,
	}
	if reservation != nil && reservation.ReservedTokens > 0 {
		cmd.LeaseUsageTokens = minInt64(cmd.LeaseUsageTokens, reservation.ReservedTokens)
	}
	if serviceTier != nil {
		cmd.ServiceTier = *serviceTier
	}
	if reasoningEffort != nil {
		cmd.ReasoningEffort = *reasoningEffort
	}
	cmd.Normalize()
	return cmd
}

func usageIngestItemLeaseTokenCount(item UsageIngestItem) int64 {
	total := item.InputTokens +
		item.OutputTokens +
		item.CacheCreationTokens +
		item.CacheCreation5mTokens +
		item.CacheCreation1hTokens +
		item.CacheReadTokens +
		item.ImageOutputTokens
	if total < 0 {
		return 0
	}
	return int64(total)
}

func ensureUsageIngestCostDefaults(item UsageIngestItem) UsageIngestItem {
	actualCost := math.Max(item.BalanceCost, item.SubscriptionCost)
	componentTotal := item.InputCost + item.OutputCost + item.CacheCreationCost + item.CacheReadCost + item.ImageOutputCost
	if item.TotalCost < 0 {
		item.TotalCost = 0
	}
	if item.TotalCost == 0 {
		if componentTotal > 0 {
			item.TotalCost = componentTotal
		} else {
			item.TotalCost = actualCost
		}
	}
	if item.RateMultiplier < 0 {
		item.RateMultiplier = 0
	}
	if item.AccountRateMultiplier < 0 {
		item.AccountRateMultiplier = 0
	}
	if item.AccountQuotaCost < 0 {
		item.AccountQuotaCost = 0
	}
	if item.PrivateGroupCommissionCost < 0 {
		item.PrivateGroupCommissionCost = 0
	}
	if item.costCalculatedByMaster {
		return item
	}
	if item.RateMultiplier == 0 && actualCost > 0 {
		item.RateMultiplier = 1
	}
	if item.AccountRateMultiplier == 0 {
		if item.TotalCost > 0 && item.AccountQuotaCost > 0 {
			item.AccountRateMultiplier = item.AccountQuotaCost / item.TotalCost
		} else if item.AccountQuotaCost > 0 {
			item.AccountRateMultiplier = 1
		}
	}
	return item
}

func generateSubsiteSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashSubsiteSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeSubsiteMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return copySubsiteMap(in)
}

func copySubsiteMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func clampNonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func sameInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func stringPtr(v string) *string {
	return &v
}

func optionalStringPtr(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return stringPtr(strings.TrimSpace(v))
}
