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
	"math"
	"strings"
	"time"

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
	ErrAccountLeaseInvalidStatus          = infraerrors.Forbidden("ACCOUNT_LEASE_INVALID_STATUS", "account lease status does not allow this operation")
	ErrQuotaReservationNotFound           = infraerrors.NotFound("QUOTA_RESERVATION_NOT_FOUND", "quota reservation not found")
	ErrQuotaReservationConflict           = infraerrors.Conflict("QUOTA_RESERVATION_CONFLICT", "quota reservation conflict")
	ErrQuotaReservationCostRequired       = infraerrors.BadRequest("QUOTA_RESERVATION_COST_REQUIRED", "estimated cost must be greater than zero")
	ErrQuotaReservationInsufficientFunds  = infraerrors.Forbidden("QUOTA_RESERVATION_INSUFFICIENT_FUNDS", "insufficient available balance for reservation")
	ErrSubsiteAuthorizeNoLease            = infraerrors.ServiceUnavailable("SUBSITE_NO_ACCOUNT_LEASE", "no active account lease is available for this subsite")
	ErrSubsiteAuthorizeModelMismatch      = infraerrors.BadRequest("SUBSITE_MODEL_MISMATCH", "requested model or platform is not available on the selected lease")
	ErrSubsiteUsageBatchEmpty             = infraerrors.BadRequest("SUBSITE_USAGE_BATCH_EMPTY", "usage batch is empty")
	ErrSubsiteUsageReservationMismatch    = infraerrors.Conflict("SUBSITE_USAGE_RESERVATION_MISMATCH", "usage item does not match its reservation")
	ErrSubsiteUsagePayloadFingerprintMiss = infraerrors.BadRequest("SUBSITE_USAGE_FINGERPRINT_REQUIRED", "usage request fingerprint is required")
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
	MaxConcurrency int        `json:"max_concurrency"`
	MaxRequests    int        `json:"max_requests"`
	MaxTokens      int64      `json:"max_tokens"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

type RenewAccountLeaseInput struct {
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
	SubsiteID          string  `json:"subsite_id"`
	APIKey             string  `json:"api_key"`
	Platform           string  `json:"platform"`
	RequestedModel     string  `json:"requested_model"`
	MappedModel        string  `json:"mapped_model"`
	EstimatedCost      float64 `json:"estimated_cost"`
	RequestFingerprint string  `json:"request_fingerprint"`
	ClientIP           string  `json:"client_ip"`
	UserAgent          string  `json:"user_agent"`
	InboundEndpoint    string  `json:"inbound_endpoint"`
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
	RequestID           string    `json:"request_id"`
	ReservationID       string    `json:"reservation_id"`
	APIKeyID            int64     `json:"api_key_id"`
	UserID              int64     `json:"user_id"`
	AccountID           int64     `json:"account_id"`
	GroupID             *int64    `json:"group_id"`
	SubscriptionID      *int64    `json:"subscription_id"`
	AccountType         string    `json:"account_type"`
	Model               string    `json:"model"`
	RequestedModel      string    `json:"requested_model"`
	UpstreamModel       *string   `json:"upstream_model"`
	ServiceTier         string    `json:"service_tier"`
	ReasoningEffort     string    `json:"reasoning_effort"`
	BillingType         int8      `json:"billing_type"`
	RequestType         int16     `json:"request_type"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CacheCreationTokens int       `json:"cache_creation_tokens"`
	CacheReadTokens     int       `json:"cache_read_tokens"`
	ImageCount          int       `json:"image_count"`
	MediaType           string    `json:"media_type"`
	BalanceCost         float64   `json:"balance_cost"`
	SubscriptionCost    float64   `json:"subscription_cost"`
	APIKeyQuotaCost     float64   `json:"api_key_quota_cost"`
	APIKeyRateLimitCost float64   `json:"api_key_rate_limit_cost"`
	AccountQuotaCost    float64   `json:"account_quota_cost"`
	RequestFingerprint  string    `json:"request_fingerprint"`
	RequestPayloadHash  string    `json:"request_payload_hash"`
	InboundEndpoint     string    `json:"inbound_endpoint"`
	UpstreamEndpoint    string    `json:"upstream_endpoint"`
	UserAgent           string    `json:"user_agent"`
	IPAddress           string    `json:"ip_address"`
	OccurredAt          time.Time `json:"occurred_at"`
}

type UsageIngestResult struct {
	Accepted  int `json:"accepted"`
	Applied   int `json:"applied"`
	Duplicate int `json:"duplicate"`
}

type SubsiteRepository interface {
	Create(ctx context.Context, subsite *Subsite) error
	GetBySubsiteID(ctx context.Context, subsiteID string) (*Subsite, error)
	List(ctx context.Context, params pagination.PaginationParams, filter ListSubsitesFilter) ([]Subsite, *pagination.PaginationResult, error)
	Update(ctx context.Context, subsite *Subsite) error
	UpdateStatus(ctx context.Context, subsiteID, status string) error
	RecordHeartbeat(ctx context.Context, heartbeat *SubsiteHeartbeat) error
}

type AccountLeaseRepository interface {
	Create(ctx context.Context, lease *AccountLease) error
	GetByLeaseID(ctx context.Context, leaseID string) (*AccountLease, error)
	ListBySubsite(ctx context.Context, subsiteID string) ([]AccountLease, error)
	ListActiveBySubsite(ctx context.Context, subsiteID string) ([]AccountLease, error)
	Renew(ctx context.Context, leaseID string, expiresAt time.Time) (*AccountLease, error)
	Release(ctx context.Context, leaseID string) (*AccountLease, error)
	Drain(ctx context.Context, leaseID string) (*AccountLease, error)
	ExpireStale(ctx context.Context, now time.Time) (int64, error)
}

type QuotaReservationRepository interface {
	Create(ctx context.Context, reservation *QuotaReservation) error
	GetByRequestID(ctx context.Context, requestID string) (*QuotaReservation, error)
	GetByReservationID(ctx context.Context, reservationID string) (*QuotaReservation, error)
	Cancel(ctx context.Context, requestID string) error
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
}

func NewAccountLeaseService(leaseRepo AccountLeaseRepository, subsiteRepo SubsiteRepository, accountRepo AccountRepository) *AccountLeaseService {
	return &AccountLeaseService{leaseRepo: leaseRepo, subsiteRepo: subsiteRepo, accountRepo: accountRepo}
}

func (s *AccountLeaseService) Create(ctx context.Context, input CreateAccountLeaseInput) (*AccountLease, error) {
	if strings.TrimSpace(input.SubsiteID) == "" || input.AccountID <= 0 {
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
	if !account.IsActive() {
		return nil, ErrAccountLeaseInvalidStatus
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
		Platform:       account.Platform,
		Status:         AccountLeaseStatusActive,
		MaxConcurrency: maxInt(1, input.MaxConcurrency),
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

func (s *AccountLeaseService) Renew(ctx context.Context, input RenewAccountLeaseInput) (*AccountLease, error) {
	if strings.TrimSpace(input.LeaseID) == "" || !input.ExpiresAt.After(time.Now()) {
		return nil, ErrSubsiteInvalidInput
	}
	return s.leaseRepo.Renew(ctx, strings.TrimSpace(input.LeaseID), input.ExpiresAt)
}

func (s *AccountLeaseService) Release(ctx context.Context, leaseID string) (*AccountLease, error) {
	return s.leaseRepo.Release(ctx, strings.TrimSpace(leaseID))
}

func (s *AccountLeaseService) Drain(ctx context.Context, leaseID string) (*AccountLease, error) {
	return s.leaseRepo.Drain(ctx, strings.TrimSpace(leaseID))
}

type RequestAuthorizeService struct {
	subsiteRepo     SubsiteRepository
	leaseRepo       AccountLeaseRepository
	reservationRepo QuotaReservationRepository
	apiKeyService   *APIKeyService
	subscriptionSvc *SubscriptionService
	accountRepo     AccountRepository
}

func NewRequestAuthorizeService(
	subsiteRepo SubsiteRepository,
	leaseRepo AccountLeaseRepository,
	reservationRepo QuotaReservationRepository,
	apiKeyService *APIKeyService,
	subscriptionSvc *SubscriptionService,
	accountRepo AccountRepository,
) *RequestAuthorizeService {
	return &RequestAuthorizeService{
		subsiteRepo:     subsiteRepo,
		leaseRepo:       leaseRepo,
		reservationRepo: reservationRepo,
		apiKeyService:   apiKeyService,
		subscriptionSvc: subscriptionSvc,
		accountRepo:     accountRepo,
	}
}

func (s *RequestAuthorizeService) Authorize(ctx context.Context, input AuthorizeSubsiteRequestInput) (*AuthorizeSubsiteResponse, error) {
	input.SubsiteID = strings.TrimSpace(input.SubsiteID)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Platform = strings.TrimSpace(input.Platform)
	input.RequestedModel = strings.TrimSpace(input.RequestedModel)
	input.MappedModel = strings.TrimSpace(input.MappedModel)
	input.RequestFingerprint = strings.TrimSpace(input.RequestFingerprint)
	if input.SubsiteID == "" || input.APIKey == "" || input.EstimatedCost <= 0 || input.RequestFingerprint == "" {
		return nil, ErrQuotaReservationCostRequired
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
	if apiKey.IsQuotaExhausted() || (apiKey.Quota > 0 && apiKey.QuotaUsed+input.EstimatedCost > apiKey.Quota) {
		return nil, ErrAPIKeyQuotaExhausted
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
		if err := s.subscriptionSvc.CheckUsageLimits(ctx, subscription, apiKey.Group, input.EstimatedCost); err != nil {
			return nil, err
		}
		if needsMaintenance {
			maintenanceCopy := *subscription
			s.subscriptionSvc.DoWindowMaintenance(&maintenanceCopy)
		}
		billingType = BillingTypeSubscription
	} else if apiKey.User.Balance < input.EstimatedCost {
		return nil, ErrQuotaReservationInsufficientFunds
	}

	lease, account, err := s.selectLease(ctx, input)
	if err != nil {
		return nil, err
	}
	requestID := "subreq_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	reservationID := "qres_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	expiresAt := time.Now().Add(10 * time.Minute)
	groupID := apiKey.GroupID
	var subscriptionID *int64
	if subscription != nil {
		subscriptionID = &subscription.ID
	}
	mappedModel := input.MappedModel
	if mappedModel == "" {
		mappedModel = input.RequestedModel
	}
	reservation := &QuotaReservation{
		ReservationID:      reservationID,
		RequestID:          requestID,
		SubsiteID:          subsite.SubsiteID,
		LeaseID:            lease.LeaseID,
		AccountID:          account.ID,
		APIKeyID:           apiKey.ID,
		UserID:             apiKey.User.ID,
		GroupID:            groupID,
		SubscriptionID:     subscriptionID,
		Platform:           account.Platform,
		RequestedModel:     input.RequestedModel,
		MappedModel:        mappedModel,
		EstimatedCost:      input.EstimatedCost,
		BillingType:        billingType,
		Status:             QuotaReservationStatusReserved,
		RequestFingerprint: input.RequestFingerprint,
		ExpiresAt:          expiresAt,
	}
	if err := s.reservationRepo.Create(ctx, reservation); err != nil {
		return nil, err
	}
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
		MaxCost:        input.EstimatedCost,
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

func (s *RequestAuthorizeService) selectLease(ctx context.Context, input AuthorizeSubsiteRequestInput) (*AccountLease, *Account, error) {
	leases, err := s.leaseRepo.ListActiveBySubsite(ctx, input.SubsiteID)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	for i := range leases {
		lease := &leases[i]
		if lease.Status != AccountLeaseStatusActive && lease.Status != AccountLeaseStatusRenewing {
			continue
		}
		if !lease.ExpiresAt.After(now) {
			continue
		}
		if input.Platform != "" && lease.Platform != "" && !strings.EqualFold(lease.Platform, input.Platform) {
			continue
		}
		account, err := s.accountRepo.GetByID(ctx, lease.AccountID)
		if err != nil {
			return nil, nil, err
		}
		if !account.IsActive() || !account.Schedulable {
			continue
		}
		if input.Platform != "" && !strings.EqualFold(account.Platform, input.Platform) {
			continue
		}
		return lease, account, nil
	}
	return nil, nil, ErrSubsiteAuthorizeNoLease
}

type UsageIngestService struct {
	billingRepo     UsageBillingRepository
	reservationRepo QuotaReservationRepository
}

func NewUsageIngestService(billingRepo UsageBillingRepository, reservationRepo QuotaReservationRepository) *UsageIngestService {
	return &UsageIngestService{billingRepo: billingRepo, reservationRepo: reservationRepo}
}

func (s *UsageIngestService) Ingest(ctx context.Context, batch UsageIngestBatch) (*UsageIngestResult, error) {
	if len(batch.Items) == 0 {
		return nil, ErrSubsiteUsageBatchEmpty
	}
	result := &UsageIngestResult{Accepted: len(batch.Items)}
	for i := range batch.Items {
		item := batch.Items[i]
		if strings.TrimSpace(item.RequestFingerprint) == "" {
			return nil, ErrSubsiteUsagePayloadFingerprintMiss
		}
		reservation, err := s.reservationRepo.GetByReservationID(ctx, item.ReservationID)
		if err != nil {
			return nil, err
		}
		if reservation.SubsiteID != strings.TrimSpace(batch.SubsiteID) ||
			reservation.RequestID != strings.TrimSpace(item.RequestID) ||
			reservation.APIKeyID != item.APIKeyID ||
			reservation.AccountID != item.AccountID {
			return nil, ErrSubsiteUsageReservationMismatch
		}
		cmd := usageIngestItemToBillingCommand(item)
		applyResult, err := s.billingRepo.Apply(ctx, cmd)
		if err != nil {
			return nil, err
		}
		actualCost := cmd.BalanceCost
		if cmd.SubscriptionCost > actualCost {
			actualCost = cmd.SubscriptionCost
		}
		if err := s.reservationRepo.Settle(ctx, item.RequestID, actualCost); err != nil {
			return nil, err
		}
		if applyResult != nil && applyResult.Applied {
			result.Applied++
		} else {
			result.Duplicate++
		}
	}
	return result, nil
}

func usageIngestItemToBillingCommand(item UsageIngestItem) *UsageBillingCommand {
	occurredAt := item.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
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
		UserID:              item.UserID,
		APIKeyID:            item.APIKeyID,
		AccountID:           item.AccountID,
		RequestID:           strings.TrimSpace(item.RequestID),
		Model:               strings.TrimSpace(item.Model),
		RequestedModel:      strings.TrimSpace(item.RequestedModel),
		UpstreamModel:       item.UpstreamModel,
		GroupID:             item.GroupID,
		SubscriptionID:      item.SubscriptionID,
		InputTokens:         item.InputTokens,
		OutputTokens:        item.OutputTokens,
		CacheCreationTokens: item.CacheCreationTokens,
		CacheReadTokens:     item.CacheReadTokens,
		TotalCost:           math.Max(item.BalanceCost, item.SubscriptionCost),
		ActualCost:          math.Max(item.BalanceCost, item.SubscriptionCost),
		BillingType:         item.BillingType,
		RequestType:         RequestTypeFromInt16(item.RequestType),
		Stream:              RequestTypeFromInt16(item.RequestType) == RequestTypeStream,
		OpenAIWSMode:        RequestTypeFromInt16(item.RequestType) == RequestTypeWSV2,
		UserAgent:           userAgent,
		IPAddress:           ipAddress,
		ImageCount:          item.ImageCount,
		MediaType:           mediaType,
		ServiceTier:         serviceTier,
		ReasoningEffort:     reasoningEffort,
		InboundEndpoint:     inboundEndpoint,
		UpstreamEndpoint:    upstreamEndpoint,
		CreatedAt:           occurredAt,
	}
	usageLog.SyncRequestTypeAndLegacyFields()
	cmd := &UsageBillingCommand{
		RequestID:           strings.TrimSpace(item.RequestID),
		APIKeyID:            item.APIKeyID,
		RequestFingerprint:  strings.TrimSpace(item.RequestFingerprint),
		RequestPayloadHash:  strings.TrimSpace(item.RequestPayloadHash),
		UserID:              item.UserID,
		AccountID:           item.AccountID,
		GroupID:             item.GroupID,
		SubscriptionID:      item.SubscriptionID,
		AccountType:         strings.TrimSpace(item.AccountType),
		Model:               strings.TrimSpace(item.Model),
		BillingType:         item.BillingType,
		InputTokens:         item.InputTokens,
		OutputTokens:        item.OutputTokens,
		CacheCreationTokens: item.CacheCreationTokens,
		CacheReadTokens:     item.CacheReadTokens,
		ImageCount:          item.ImageCount,
		MediaType:           strings.TrimSpace(item.MediaType),
		BalanceCost:         item.BalanceCost,
		SubscriptionCost:    item.SubscriptionCost,
		APIKeyQuotaCost:     item.APIKeyQuotaCost,
		APIKeyRateLimitCost: item.APIKeyRateLimitCost,
		AccountQuotaCost:    item.AccountQuotaCost,
		UsageOccurredAt:     occurredAt,
		UsageLog:            usageLog,
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

func stringPtr(v string) *string {
	return &v
}
