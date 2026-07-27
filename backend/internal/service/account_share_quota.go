package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	AccountShareQuotaScopeGlobal = "global"
	AccountShareQuotaScopeOwner  = "owner"

	AccountShareQuotaPolicyStatusActive  = "active"
	AccountShareQuotaPolicyStatusRevoked = "revoked"

	AccountShareQuotaPolicyKindDefault     = "default"
	AccountShareQuotaPolicyKindManual      = "manual"
	AccountShareQuotaPolicyKindGrandfather = "grandfather"

	AccountShareQuotaReasonMaxRunes = 1000
	AccountShareQuotaMaximumValue   = 1_000_000
)

var (
	ErrAccountShareQuotaAdminRequired = infraerrors.Forbidden(
		"ACCOUNT_SHARE_QUOTA_ADMIN_REQUIRED",
		"administrator permission is required to manage account share quotas",
	)
	ErrAccountShareQuotaInvalid = infraerrors.BadRequest(
		"ACCOUNT_SHARE_QUOTA_INVALID",
		"account share quota configuration is invalid",
	)
	ErrAccountShareQuotaReasonRequired = infraerrors.BadRequest(
		"ACCOUNT_SHARE_QUOTA_REASON_REQUIRED",
		"a reason is required to change account share quotas",
	)
	ErrAccountShareQuotaConfirmationRequired = infraerrors.BadRequest(
		"ACCOUNT_SHARE_QUOTA_CONFIRMATION_REQUIRED",
		"account share quota changes require explicit confirmation",
	)
	ErrAccountShareQuotaExpectedVersionRequired = infraerrors.BadRequest(
		"ACCOUNT_SHARE_QUOTA_EXPECTED_VERSION_REQUIRED",
		"expected_version is required to change account share quotas",
	)
	ErrAccountShareQuotaVersionConflict = infraerrors.Conflict(
		"ACCOUNT_SHARE_QUOTA_VERSION_CONFLICT",
		"account share quota policy changed; refresh and confirm again",
	)
	ErrAccountShareQuotaConfigurationUnavailable = infraerrors.ServiceUnavailable(
		"ACCOUNT_SHARE_QUOTA_CONFIGURATION_UNAVAILABLE",
		"account share quota configuration is unavailable",
	)
	ErrAccountShareQuotaOverrideNotFound = infraerrors.NotFound(
		"ACCOUNT_SHARE_QUOTA_OVERRIDE_NOT_FOUND",
		"account share quota override was not found",
	)
	ErrAccountShareQuotaOverrideNotActive = infraerrors.Conflict(
		"ACCOUNT_SHARE_QUOTA_OVERRIDE_NOT_ACTIVE",
		"account share quota override is not active",
	)
	ErrAccountShareQuotaGrandfatherGrowthBlocked = infraerrors.Conflict(
		"ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED",
		"grandfathered account share quotas allow management and reduction only",
	)
	ErrAccountShareQuotaHistoricalGrowthBlocked = infraerrors.Conflict(
		"ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED",
		"historical account share usage exceeds the effective quota; only management and reduction are allowed",
	)
	ErrAccountShareQuotaNotCandidate = infraerrors.Conflict(
		"ACCOUNT_SHARE_QUOTA_NOT_A_CANDIDATE",
		"the owner does not currently exceed the effective account share quota",
	)
	ErrAccountShareQuotaGrandfatherAlreadyActive = infraerrors.Conflict(
		"ACCOUNT_SHARE_QUOTA_GRANDFATHER_ALREADY_ACTIVE",
		"an active grandfather quota policy already covers this owner",
	)
)

type AccountShareQuotaLimits struct {
	MaxLiveRooms            int `json:"max_live_rooms"`
	MaxRoomCreates24Hours   int `json:"max_room_creates_24_hours"`
	MaxAccountsPerRoom      int `json:"max_accounts_per_room"`
	MaxRoomAccountsPerOwner int `json:"max_room_accounts_per_owner"`
}

func DefaultAccountShareQuotaLimits() AccountShareQuotaLimits {
	return AccountShareQuotaLimits{
		MaxLiveRooms:            AccountShareDefaultMaxLiveRooms,
		MaxRoomCreates24Hours:   AccountShareDefaultMaxRoomCreatesPer24Hours,
		MaxAccountsPerRoom:      AccountShareDefaultMaxAccountsPerRoom,
		MaxRoomAccountsPerOwner: AccountShareDefaultMaxRoomAccountsPerOwner,
	}
}

func (l AccountShareQuotaLimits) Valid() bool {
	values := [...]int{
		l.MaxLiveRooms,
		l.MaxRoomCreates24Hours,
		l.MaxAccountsPerRoom,
		l.MaxRoomAccountsPerOwner,
	}
	for _, value := range values {
		if value <= 0 || value > AccountShareQuotaMaximumValue {
			return false
		}
	}
	return l.MaxRoomAccountsPerOwner >= l.MaxAccountsPerRoom
}

type AccountShareQuotaPolicy struct {
	ID                  int64                   `json:"id"`
	ScopeType           string                  `json:"scope_type"`
	OwnerUserID         *int64                  `json:"owner_user_id,omitempty"`
	Version             int64                   `json:"version"`
	Status              string                  `json:"status"`
	OverrideKind        string                  `json:"override_kind"`
	Limits              AccountShareQuotaLimits `json:"limits"`
	EffectiveAt         time.Time               `json:"effective_at"`
	ExpiresAt           *time.Time              `json:"expires_at,omitempty"`
	Reason              string                  `json:"reason"`
	ActorUserID         *int64                  `json:"actor_user_id,omitempty"`
	ActorUserIDSnapshot int64                   `json:"actor_user_id_snapshot"`
	CreatedAt           time.Time               `json:"created_at"`
}

type AccountShareResolvedQuota struct {
	Limits            AccountShareQuotaLimits `json:"limits"`
	Source            string                  `json:"source"`
	PolicyID          int64                   `json:"policy_id"`
	PolicyVersion     int64                   `json:"policy_version"`
	OverrideKind      string                  `json:"override_kind"`
	OverrideExpiresAt *time.Time              `json:"override_expires_at,omitempty"`
	GrowthBlocked     bool                    `json:"growth_blocked"`
}

type AccountShareQuotaAdminState struct {
	GlobalPolicy   AccountShareQuotaPolicy   `json:"global_policy"`
	OwnerPolicy    *AccountShareQuotaPolicy  `json:"owner_policy,omitempty"`
	EffectiveQuota AccountShareResolvedQuota `json:"effective_quota"`
	Usage          AccountShareQuotaUsage    `json:"usage"`
}

const AccountShareGrandfatherBatchMaximumItems = 100

type AccountShareGrandfatherCandidate struct {
	OwnerUserID        int64                     `json:"owner_user_id"`
	Usage              AccountShareQuotaUsage    `json:"usage"`
	ExceededDimensions []string                  `json:"exceeded_dimensions"`
	EffectiveQuota     AccountShareResolvedQuota `json:"effective_quota"`
	LatestOwnerVersion int64                     `json:"latest_owner_version"`
	SuggestedLimits    AccountShareQuotaLimits   `json:"suggested_limits"`
	PreviewFingerprint string                    `json:"preview_fingerprint"`
	AsOf               time.Time                 `json:"as_of"`
}

type AccountShareGrandfatherCandidateItem struct {
	OwnerUserID        int64                  `json:"owner_user_id"`
	ExpectedVersion    int64                  `json:"expected_version"`
	PreviewUsage       AccountShareQuotaUsage `json:"preview_usage"`
	PreviewFingerprint string                 `json:"preview_fingerprint"`
}

type BatchGrandfatherAccountShareQuotaInput struct {
	Items     []AccountShareGrandfatherCandidateItem `json:"items"`
	ExpiresAt *time.Time                             `json:"expires_at"`
	Reason    string                                 `json:"reason"`
	Confirmed bool                                   `json:"confirmed"`
}

type AccountShareGrandfatherBatchItemResult struct {
	OwnerUserID   int64      `json:"owner_user_id"`
	Status        string     `json:"status"`
	ResultCode    string     `json:"result_code,omitempty"`
	Message       string     `json:"message,omitempty"`
	PolicyID      int64      `json:"policy_id,omitempty"`
	PolicyVersion int64      `json:"policy_version,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

type ApplyAccountShareGrandfatherCandidateInput struct {
	Item        AccountShareGrandfatherCandidateItem
	ExpiresAt   time.Time
	Reason      string
	ActorUserID int64
}

type AppendAccountShareQuotaPolicyInput struct {
	ScopeType         string
	OwnerUserID       *int64
	ExpectedVersion   int64
	Status            string
	OverrideKind      string
	Limits            AccountShareQuotaLimits
	EffectiveAt       time.Time
	ExpiresAt         *time.Time
	Reason            string
	ActorUserID       int64
	DeriveGrandfather bool
}

type AccountShareQuotaAdminRepository interface {
	ResolveAccountShareQuota(
		ctx context.Context,
		ownerUserID int64,
		at time.Time,
	) (*AccountShareResolvedQuota, error)
	GetLatestAccountShareQuotaPolicy(
		ctx context.Context,
		scopeType string,
		ownerUserID *int64,
	) (*AccountShareQuotaPolicy, error)
	GetAccountShareQuotaAdminState(
		ctx context.Context,
		ownerUserID int64,
		at time.Time,
	) (*AccountShareQuotaAdminState, error)
	AppendAccountShareQuotaPolicyRevision(
		ctx context.Context,
		input AppendAccountShareQuotaPolicyInput,
	) (*AccountShareQuotaPolicy, error)
	ListAccountShareQuotaPolicyRevisions(
		ctx context.Context,
		scopeType string,
		ownerUserID *int64,
		params pagination.PaginationParams,
	) ([]AccountShareQuotaPolicy, int64, error)
	ListAccountShareGrandfatherCandidates(
		ctx context.Context,
		at time.Time,
		params pagination.PaginationParams,
	) ([]AccountShareGrandfatherCandidate, int64, error)
	ApplyAccountShareGrandfatherCandidate(
		ctx context.Context,
		input ApplyAccountShareGrandfatherCandidateInput,
	) (*AccountShareGrandfatherBatchItemResult, error)
}

type UpdateAccountShareGlobalQuotaInput struct {
	Limits          AccountShareQuotaLimits `json:"limits"`
	EffectiveAt     *time.Time              `json:"effective_at,omitempty"`
	ExpectedVersion int64                   `json:"expected_version"`
	Reason          string                  `json:"reason"`
	Confirmed       bool                    `json:"confirmed"`
}

type UpsertAccountShareOwnerQuotaInput struct {
	Limits          AccountShareQuotaLimits `json:"limits"`
	EffectiveAt     *time.Time              `json:"effective_at,omitempty"`
	ExpiresAt       *time.Time              `json:"expires_at"`
	ExpectedVersion int64                   `json:"expected_version"`
	Reason          string                  `json:"reason"`
	Confirmed       bool                    `json:"confirmed"`
}

type GrandfatherAccountShareOwnerQuotaInput struct {
	EffectiveAt     *time.Time `json:"effective_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at"`
	ExpectedVersion int64      `json:"expected_version"`
	Reason          string     `json:"reason"`
	Confirmed       bool       `json:"confirmed"`
}

type RevokeAccountShareOwnerQuotaInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
	Confirmed       bool   `json:"confirmed"`
}

func (s *AccountShareModeService) GetAccountShareGlobalQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
) (*AccountShareQuotaPolicy, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	return repo.GetLatestAccountShareQuotaPolicy(ctx, AccountShareQuotaScopeGlobal, nil)
}

func (s *AccountShareModeService) GetAccountShareOwnerQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	ownerUserID int64,
) (*AccountShareQuotaAdminState, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if ownerUserID <= 0 {
		return nil, ErrAccountShareQuotaInvalid
	}
	return repo.GetAccountShareQuotaAdminState(ctx, ownerUserID, time.Now().UTC())
}

func (s *AccountShareModeService) UpdateAccountShareGlobalQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	input UpdateAccountShareGlobalQuotaInput,
) (*AccountShareQuotaPolicy, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion <= 0 {
		return nil, ErrAccountShareQuotaExpectedVersionRequired
	}
	effectiveAt, err := validateAccountShareQuotaMutation(
		input.Limits,
		input.EffectiveAt,
		nil,
		input.ExpectedVersion,
		input.Reason,
		input.Confirmed,
		false,
	)
	if err != nil {
		return nil, err
	}
	return repo.AppendAccountShareQuotaPolicyRevision(ctx, AppendAccountShareQuotaPolicyInput{
		ScopeType:       AccountShareQuotaScopeGlobal,
		ExpectedVersion: input.ExpectedVersion,
		Status:          AccountShareQuotaPolicyStatusActive,
		OverrideKind:    AccountShareQuotaPolicyKindDefault,
		Limits:          input.Limits,
		EffectiveAt:     effectiveAt,
		Reason:          strings.TrimSpace(input.Reason),
		ActorUserID:     actorUserID,
	})
}

func (s *AccountShareModeService) UpsertAccountShareOwnerQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	ownerUserID int64,
	input UpsertAccountShareOwnerQuotaInput,
) (*AccountShareQuotaPolicy, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if ownerUserID <= 0 {
		return nil, ErrAccountShareQuotaInvalid
	}
	effectiveAt, err := validateAccountShareQuotaMutation(
		input.Limits,
		input.EffectiveAt,
		input.ExpiresAt,
		input.ExpectedVersion,
		input.Reason,
		input.Confirmed,
		true,
	)
	if err != nil {
		return nil, err
	}
	ownerID := ownerUserID
	return repo.AppendAccountShareQuotaPolicyRevision(ctx, AppendAccountShareQuotaPolicyInput{
		ScopeType:       AccountShareQuotaScopeOwner,
		OwnerUserID:     &ownerID,
		ExpectedVersion: input.ExpectedVersion,
		Status:          AccountShareQuotaPolicyStatusActive,
		OverrideKind:    AccountShareQuotaPolicyKindManual,
		Limits:          input.Limits,
		EffectiveAt:     effectiveAt,
		ExpiresAt:       input.ExpiresAt,
		Reason:          strings.TrimSpace(input.Reason),
		ActorUserID:     actorUserID,
	})
}

func (s *AccountShareModeService) GrandfatherAccountShareOwnerQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	ownerUserID int64,
	input GrandfatherAccountShareOwnerQuotaInput,
) (*AccountShareQuotaPolicy, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if ownerUserID <= 0 {
		return nil, ErrAccountShareQuotaInvalid
	}
	now := time.Now().UTC()
	if input.EffectiveAt != nil && input.EffectiveAt.After(now) {
		return nil, ErrAccountShareQuotaInvalid.WithMetadata(
			map[string]string{"field": "effective_at"},
		)
	}
	effectiveAt, err := validateAccountShareQuotaMutation(
		DefaultAccountShareQuotaLimits(),
		input.EffectiveAt,
		input.ExpiresAt,
		input.ExpectedVersion,
		input.Reason,
		input.Confirmed,
		true,
	)
	if err != nil {
		return nil, err
	}
	ownerID := ownerUserID
	return repo.AppendAccountShareQuotaPolicyRevision(ctx, AppendAccountShareQuotaPolicyInput{
		ScopeType:         AccountShareQuotaScopeOwner,
		OwnerUserID:       &ownerID,
		ExpectedVersion:   input.ExpectedVersion,
		Status:            AccountShareQuotaPolicyStatusActive,
		OverrideKind:      AccountShareQuotaPolicyKindGrandfather,
		EffectiveAt:       effectiveAt,
		ExpiresAt:         input.ExpiresAt,
		Reason:            strings.TrimSpace(input.Reason),
		ActorUserID:       actorUserID,
		DeriveGrandfather: true,
	})
}

func (s *AccountShareModeService) RevokeAccountShareOwnerQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	ownerUserID int64,
	input RevokeAccountShareOwnerQuotaInput,
) (*AccountShareQuotaPolicy, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if ownerUserID <= 0 {
		return nil, ErrAccountShareQuotaInvalid
	}
	if input.ExpectedVersion <= 0 {
		return nil, ErrAccountShareQuotaExpectedVersionRequired
	}
	if err := validateAccountShareQuotaReasonAndConfirmation(
		input.ExpectedVersion,
		input.Reason,
		input.Confirmed,
	); err != nil {
		return nil, err
	}
	latest, err := repo.GetLatestAccountShareQuotaPolicy(
		ctx,
		AccountShareQuotaScopeOwner,
		&ownerUserID,
	)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, ErrAccountShareQuotaOverrideNotFound
	}
	if latest.Status != AccountShareQuotaPolicyStatusActive {
		return nil, ErrAccountShareQuotaOverrideNotActive
	}
	return repo.AppendAccountShareQuotaPolicyRevision(ctx, AppendAccountShareQuotaPolicyInput{
		ScopeType:       AccountShareQuotaScopeOwner,
		OwnerUserID:     &ownerUserID,
		ExpectedVersion: input.ExpectedVersion,
		Status:          AccountShareQuotaPolicyStatusRevoked,
		OverrideKind:    latest.OverrideKind,
		Limits:          latest.Limits,
		EffectiveAt:     time.Now().UTC(),
		Reason:          strings.TrimSpace(input.Reason),
		ActorUserID:     actorUserID,
	})
}

func (s *AccountShareModeService) ListAccountShareQuotaAuditForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	scopeType string,
	ownerUserID *int64,
	params pagination.PaginationParams,
) ([]AccountShareQuotaPolicy, *pagination.PaginationResult, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, nil, err
	}
	scopeType = strings.ToLower(strings.TrimSpace(scopeType))
	if scopeType == "" {
		scopeType = AccountShareQuotaScopeGlobal
	}
	if scopeType != AccountShareQuotaScopeGlobal && scopeType != AccountShareQuotaScopeOwner {
		return nil, nil, ErrAccountShareQuotaInvalid
	}
	if scopeType == AccountShareQuotaScopeGlobal {
		ownerUserID = nil
	} else if ownerUserID == nil || *ownerUserID <= 0 {
		return nil, nil, ErrAccountShareQuotaInvalid
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	items, total, err := repo.ListAccountShareQuotaPolicyRevisions(
		ctx,
		scopeType,
		ownerUserID,
		params,
	)
	if err != nil {
		return nil, nil, err
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(params.PageSize) - 1) / int64(params.PageSize))
	}
	return items, &pagination.PaginationResult{
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    pages,
	}, nil
}

func (s *AccountShareModeService) ListAccountShareGrandfatherCandidatesForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	params pagination.PaginationParams,
) ([]AccountShareGrandfatherCandidate, *pagination.PaginationResult, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, nil, err
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	items, total, err := repo.ListAccountShareGrandfatherCandidates(ctx, time.Now().UTC(), params)
	if err != nil {
		return nil, nil, err
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(params.PageSize) - 1) / int64(params.PageSize))
	}
	return items, &pagination.PaginationResult{Total: total, Page: params.Page, PageSize: params.PageSize, Pages: pages}, nil
}

func (s *AccountShareModeService) BatchGrandfatherAccountShareQuotaForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	input BatchGrandfatherAccountShareQuotaInput,
) ([]AccountShareGrandfatherBatchItemResult, error) {
	repo, err := s.accountShareQuotaAdminRepository(actorUserID, actorIsAdmin)
	if err != nil {
		return nil, err
	}
	if err := validateAccountShareQuotaReasonAndConfirmation(0, input.Reason, input.Confirmed); err != nil {
		return nil, err
	}
	if input.ExpiresAt == nil || !input.ExpiresAt.After(time.Now().UTC()) {
		return nil, ErrAccountShareQuotaInvalid.WithMetadata(map[string]string{"field": "expires_at"})
	}
	if len(input.Items) == 0 || len(input.Items) > AccountShareGrandfatherBatchMaximumItems {
		return nil, ErrAccountShareQuotaInvalid.WithMetadata(map[string]string{"field": "items"})
	}
	items := make(map[int64]AccountShareGrandfatherCandidateItem, len(input.Items))
	for _, item := range input.Items {
		if item.OwnerUserID <= 0 || item.ExpectedVersion < 0 ||
			!item.PreviewUsage.Valid() || strings.TrimSpace(item.PreviewFingerprint) == "" {
			return nil, ErrAccountShareQuotaInvalid.WithMetadata(map[string]string{"field": "items"})
		}
		if existing, exists := items[item.OwnerUserID]; exists {
			if existing != item {
				return nil, ErrAccountShareQuotaInvalid.WithMetadata(
					map[string]string{"field": "items/duplicate_owner"},
				)
			}
			continue
		}
		items[item.OwnerUserID] = item
	}
	ownerIDs := make([]int64, 0, len(items))
	for ownerUserID := range items {
		ownerIDs = append(ownerIDs, ownerUserID)
	}
	sort.Slice(ownerIDs, func(i, j int) bool { return ownerIDs[i] < ownerIDs[j] })
	results := make([]AccountShareGrandfatherBatchItemResult, 0, len(ownerIDs))
	for _, ownerUserID := range ownerIDs {
		result, applyErr := repo.ApplyAccountShareGrandfatherCandidate(ctx, ApplyAccountShareGrandfatherCandidateInput{
			Item:        items[ownerUserID],
			ExpiresAt:   input.ExpiresAt.UTC(),
			Reason:      strings.TrimSpace(input.Reason),
			ActorUserID: actorUserID,
		})
		if applyErr != nil {
			resultCode := infraerrors.Reason(applyErr)
			if resultCode == "" {
				resultCode = "ACCOUNT_SHARE_QUOTA_APPLY_FAILED"
			}
			message := infraerrors.Message(applyErr)
			if strings.TrimSpace(message) == "" {
				message = "failed to apply the grandfather quota policy"
			}
			results = append(results, AccountShareGrandfatherBatchItemResult{
				OwnerUserID: ownerUserID,
				Status:      "failed",
				ResultCode:  resultCode,
				Message:     message,
			})
			continue
		}
		if result == nil {
			results = append(results, AccountShareGrandfatherBatchItemResult{
				OwnerUserID: ownerUserID,
				Status:      "failed",
				ResultCode:  "ACCOUNT_SHARE_QUOTA_APPLY_FAILED",
				Message:     "grandfather quota policy application returned no result",
			})
			continue
		}
		results = append(results, *result)
	}
	return results, nil
}

func BuildAccountShareGrandfatherCandidateFingerprint(
	ownerUserID, latestOwnerVersion int64,
	usage AccountShareQuotaUsage,
	quota AccountShareResolvedQuota,
) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf(
		"owner=%d|latest=%d|usage=%d,%d,%d,%d|quota=%s,%d,%d,%s,%d,%d,%d,%d",
		ownerUserID,
		latestOwnerVersion,
		usage.LiveRooms,
		usage.RoomCreates24Hours,
		usage.OwnerRoomAccounts,
		usage.LargestRoomAccounts,
		quota.Source,
		quota.PolicyID,
		quota.PolicyVersion,
		quota.OverrideKind,
		quota.Limits.MaxLiveRooms,
		quota.Limits.MaxRoomCreates24Hours,
		quota.Limits.MaxAccountsPerRoom,
		quota.Limits.MaxRoomAccountsPerOwner,
	))))
}

func (s *AccountShareModeService) accountShareQuotaAdminRepository(
	actorUserID int64,
	actorIsAdmin bool,
) (AccountShareQuotaAdminRepository, error) {
	if actorUserID <= 0 || !actorIsAdmin {
		return nil, ErrAccountShareQuotaAdminRequired
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.repo.(AccountShareQuotaAdminRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return repo, nil
}

func validateAccountShareQuotaMutation(
	limits AccountShareQuotaLimits,
	effectiveAt *time.Time,
	expiresAt *time.Time,
	expectedVersion int64,
	reason string,
	confirmed bool,
	requireExpiry bool,
) (time.Time, error) {
	if !limits.Valid() {
		return time.Time{}, ErrAccountShareQuotaInvalid
	}
	if err := validateAccountShareQuotaReasonAndConfirmation(
		expectedVersion,
		reason,
		confirmed,
	); err != nil {
		return time.Time{}, err
	}
	effective := time.Now().UTC()
	if effectiveAt != nil {
		effective = effectiveAt.UTC()
	}
	if requireExpiry {
		if expiresAt == nil ||
			!expiresAt.After(effective) ||
			!expiresAt.After(time.Now().UTC()) {
			return time.Time{}, ErrAccountShareQuotaInvalid.WithMetadata(
				map[string]string{"field": "expires_at"},
			)
		}
	} else if expiresAt != nil {
		return time.Time{}, ErrAccountShareQuotaInvalid.WithMetadata(
			map[string]string{"field": "expires_at"},
		)
	}
	return effective, nil
}

func validateAccountShareQuotaReasonAndConfirmation(
	expectedVersion int64,
	reason string,
	confirmed bool,
) error {
	if !confirmed {
		return ErrAccountShareQuotaConfirmationRequired
	}
	if expectedVersion < 0 {
		return ErrAccountShareQuotaExpectedVersionRequired
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrAccountShareQuotaReasonRequired
	}
	if !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > AccountShareQuotaReasonMaxRunes {
		return ErrAccountShareQuotaInvalid.WithMetadata(map[string]string{"field": "reason"})
	}
	return nil
}
