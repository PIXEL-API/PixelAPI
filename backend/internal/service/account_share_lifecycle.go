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
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AccountShareListingStatusValidating = "validating"
	AccountShareListingStatusDraining   = "draining"
	AccountShareListingStatusSuspended  = "suspended"

	AccountShareMembershipStatusEnding = "ending"

	AccountShareRoomHealthHealthy     = "healthy"
	AccountShareRoomHealthDegraded    = "degraded"
	AccountShareRoomHealthUnavailable = "unavailable"

	AccountShareRoomActionDrain    = "drain"
	AccountShareRoomActionActivate = "activate"
	AccountShareRoomActionSuspend  = "suspend"
	AccountShareRoomActionDelete   = "delete"

	AccountShareRoomOperationActionDrain  = "drain_room"
	AccountShareRoomOperationActionDelete = "delete_room"

	AccountShareRoomDeleteTokenTTL = 2 * time.Minute

	accountShareRoomDeleteTokenAction = "account_share_room:delete:v1"

	accountShareRoomActivationMaxConcurrency = 4
	accountShareRoomValidationRecoveryDelay  = AccountShareModeImageConnectivityTestTimeout + time.Minute
	accountShareRoomValidationBatchSize      = 1
	accountShareRoomValidationInterval       = 15 * time.Second
	accountShareRoomValidationWorkerTimeout  = AccountShareModeImageConnectivityTestTimeout + 2*time.Minute
)

var (
	ErrAccountShareRoomNoChanges = infraerrors.BadRequest(
		"ACCOUNT_SHARE_ROOM_NO_CHANGES",
		"at least one room field must be changed",
	)
	ErrAccountShareRoomLifecycleCommandRequired = infraerrors.BadRequest(
		"ACCOUNT_SHARE_ROOM_LIFECYCLE_COMMAND_REQUIRED",
		"room status must be changed through a lifecycle command",
	)
	ErrAccountShareRoomConflictingFields = infraerrors.BadRequest(
		"ACCOUNT_SHARE_ROOM_CONFLICTING_FIELDS",
		"the request contains conflicting room fields",
	)
	ErrAccountShareRoomInvalidTransition = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_INVALID_TRANSITION",
		"room lifecycle transition is not allowed",
	)
	ErrAccountShareRoomOperationConflict = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_OPERATION_CONFLICT",
		"another room operation is still in progress",
	)
	ErrAccountShareRoomDeleteBlocked = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_DELETE_BLOCKED",
		"room cannot be deleted until all blockers are cleared",
	)
	ErrAccountShareRoomDeleteTokenRequired = infraerrors.BadRequest(
		"ACCOUNT_SHARE_ROOM_DELETION_TOKEN_REQUIRED",
		"room deletion confirmation token is required",
	)
	ErrAccountShareRoomDeleteTokenInvalid = infraerrors.Forbidden(
		"ACCOUNT_SHARE_ROOM_DELETION_TOKEN_INVALID",
		"room deletion confirmation token is invalid or expired",
	)
	ErrAccountShareRoomDeleted = infraerrors.New(
		http.StatusGone,
		"ACCOUNT_SHARE_ROOM_DELETED",
		"account share room has been deleted",
	)
	ErrAccountShareRoomReviewIdentityMissing = infraerrors.Conflict(
		"ACCOUNT_SHARE_ROOM_REVIEW_IDENTITY_MISSING",
		"房间存在可评价的历史使用记录，但账号身份尚未固化；请先刷新房间账号凭证后再删除",
	)
	ErrAccountShareRoomReasonRequired = infraerrors.BadRequest(
		"ACCOUNT_SHARE_ROOM_REASON_REQUIRED",
		"a reason is required for this room operation",
	)
	ErrAccountShareRuntimeDependencyUnavailable = infraerrors.ServiceUnavailable(
		"ACCOUNT_SHARE_RUNTIME_DEPENDENCY_UNAVAILABLE",
		"account share runtime state is unavailable",
	)
	ErrAccountShareLifecycleRolloutDisabled = infraerrors.ServiceUnavailable(
		"ACCOUNT_SHARE_LIFECYCLE_ROLLOUT_DISABLED",
		"account share room lifecycle commands are not enabled",
	)
)

type AccountShareRoomBlockers struct {
	ActiveMembershipCount          int    `json:"active_membership_count"`
	QueuedMembershipCount          int    `json:"queued_membership_count"`
	EndingMembershipCount          int    `json:"ending_membership_count"`
	InFlightRequestCount           int    `json:"in_flight_request_count"`
	PendingBillingIntentCount      int    `json:"pending_billing_intent_count"`
	SynchronousBillingPendingCount int    `json:"synchronous_billing_pending_count"`
	ValidEditSession               bool   `json:"valid_edit_session"`
	ConflictingOperation           bool   `json:"conflicting_operation"`
	ConflictingOperationID         string `json:"conflicting_operation_id,omitempty"`
	RuntimeDependencyUnavailable   bool   `json:"runtime_dependency_unavailable"`
}

func (b AccountShareRoomBlockers) Any() bool {
	return b.ActiveMembershipCount > 0 ||
		b.QueuedMembershipCount > 0 ||
		b.EndingMembershipCount > 0 ||
		b.InFlightRequestCount > 0 ||
		b.PendingBillingIntentCount > 0 ||
		b.SynchronousBillingPendingCount > 0 ||
		b.ValidEditSession ||
		b.ConflictingOperation ||
		b.RuntimeDependencyUnavailable
}

func (b AccountShareRoomBlockers) Metadata() map[string]string {
	return map[string]string{
		"active_membership_count":           strconv.Itoa(b.ActiveMembershipCount),
		"queued_membership_count":           strconv.Itoa(b.QueuedMembershipCount),
		"ending_membership_count":           strconv.Itoa(b.EndingMembershipCount),
		"in_flight_request_count":           strconv.Itoa(b.InFlightRequestCount),
		"pending_billing_intent_count":      strconv.Itoa(b.PendingBillingIntentCount),
		"synchronous_billing_pending_count": strconv.Itoa(b.SynchronousBillingPendingCount),
		"valid_edit_session":                strconv.FormatBool(b.ValidEditSession),
		"conflicting_operation":             strconv.FormatBool(b.ConflictingOperation),
		"conflicting_operation_id":          b.ConflictingOperationID,
		"runtime_dependency_unavailable":    strconv.FormatBool(b.RuntimeDependencyUnavailable),
	}
}

type AccountShareRoomManagementState struct {
	ListingID                  int64                    `json:"listing_id"`
	RoomName                   string                   `json:"room_name"`
	OwnerUserID                int64                    `json:"-"`
	RowVersion                 int64                    `json:"row_version"`
	LifecycleStatus            string                   `json:"lifecycle_status"`
	HealthState                string                   `json:"health_state"`
	StatusReasonCode           string                   `json:"status_reason_code,omitempty"`
	StatusReason               string                   `json:"status_reason,omitempty"`
	SeatLimit                  int                      `json:"seat_limit"`
	ActiveSeats                int                      `json:"active_seats"`
	EndingSeats                int                      `json:"ending_seats"`
	AdmissionRemainingSeats    int                      `json:"admission_remaining_seats"`
	QueuedMembershipCount      int                      `json:"queued_membership_count"`
	RoomAccountCount           int                      `json:"room_account_count"`
	ConfiguredTotalConcurrency int                      `json:"configured_total_concurrency"`
	EligibleTotalConcurrency   int                      `json:"eligible_total_concurrency"`
	InFlightConcurrency        int                      `json:"in_flight_concurrency"`
	PendingBillingIntentCount  int                      `json:"pending_billing_intent_count"`
	AllowedActions             []string                 `json:"allowed_actions"`
	Blockers                   AccountShareRoomBlockers `json:"blockers"`
	PendingOperationID         string                   `json:"pending_operation_id,omitempty"`
	DeletedAt                  *time.Time               `json:"deleted_at,omitempty"`
	RuntimeMembershipIDs       []int64                  `json:"-"`
	RuntimeAccountIDs          []int64                  `json:"-"`
}

type AccountShareRoomLifecycleCommandInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
	Confirmed       bool   `json:"confirmed,omitempty"`
}

type AccountShareRoomDeleteIntentInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

type AccountShareRoomDeleteIntent struct {
	ListingID     int64                    `json:"listing_id"`
	RoomName      string                   `json:"room_name"`
	RowVersion    int64                    `json:"row_version"`
	CanDelete     bool                     `json:"can_delete"`
	AccountCount  int                      `json:"account_count"`
	Blockers      AccountShareRoomBlockers `json:"blockers"`
	Token         string                   `json:"token,omitempty"`
	ExpiresAt     *time.Time               `json:"expires_at,omitempty"`
	HistoryNotice string                   `json:"history_notice"`
}

type AccountShareRoomDeleteInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	RoomName        string `json:"room_name"`
	Token           string `json:"token"`
	Reason          string `json:"reason,omitempty"`
	Confirmed       bool   `json:"confirmed"`
	RequestID       string `json:"-"`
}

type AccountShareRoomOperation struct {
	ID              string         `json:"id"`
	ListingID       int64          `json:"listing_id"`
	MembershipID    *int64         `json:"membership_id,omitempty"`
	ActorUserID     int64          `json:"-"`
	ActorRole       string         `json:"-"`
	Action          string         `json:"action"`
	Status          string         `json:"status"`
	ExpectedVersion *int64         `json:"expected_version,omitempty"`
	StartVersion    *int64         `json:"start_version,omitempty"`
	FinalVersion    *int64         `json:"final_version,omitempty"`
	Blocker         map[string]any `json:"blocker"`
	Result          map[string]any `json:"result"`
	ErrorCode       string         `json:"error_code,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type accountShareRoomDeleteClaims struct {
	Action      string `json:"action"`
	ListingID   int64  `json:"listing_id"`
	ActorUserID int64  `json:"actor_user_id"`
	RowVersion  int64  `json:"row_version"`
	RoomName    string `json:"room_name"`
	ExpiresAt   int64  `json:"expires_at"`
}

type accountShareRoomManagementStateRepository interface {
	GetRoomManagementState(
		ctx context.Context,
		viewerUserID int64,
		viewerIsAdmin bool,
		listingID int64,
	) (*AccountShareRoomManagementState, error)
}

type accountShareRoomAccountLister interface {
	ListRoomAccounts(
		ctx context.Context,
		listingID int64,
		viewerUserID int64,
		viewerIsAdmin bool,
	) ([]AccountShareRoomAccount, error)
}

type accountShareLifecycleRepository interface {
	accountShareRoomManagementStateRepository
	TransitionRoomLifecycle(
		ctx context.Context,
		actorUserID int64,
		actorIsAdmin bool,
		listingID int64,
		command string,
		input AccountShareRoomLifecycleCommandInput,
	) (*AccountShareListing, error)
	FinalizeDrainingRoom(
		ctx context.Context,
		listingID int64,
		expectedVersion int64,
	) (*AccountShareListing, error)
	ClearRoomMembersForDrain(
		ctx context.Context,
		actorUserID int64,
		actorIsAdmin bool,
		listingID int64,
	) (*AccountShareSeatBillingResult, error)
	ListOpenRoomLifecycleListingIDs(ctx context.Context, afterID int64, limit int) ([]int64, error)
	ListValidatingRoomIDs(ctx context.Context, staleBefore time.Time, limit int) ([]int64, error)
	FindRoomDeleteOperation(
		ctx context.Context,
		actorUserID int64,
		actorIsAdmin bool,
		listingID int64,
		requestID string,
	) (*AccountShareRoomOperation, error)
	SoftDeleteRoom(
		ctx context.Context,
		actorUserID int64,
		actorIsAdmin bool,
		listingID int64,
		input AccountShareRoomDeleteInput,
	) (*AccountShareRoomOperation, error)
	FinalizeRoomDeletion(
		ctx context.Context,
		listingID int64,
		operationID string,
	) (*AccountShareRoomOperation, error)
	ListPendingRoomDeletionOperations(
		ctx context.Context,
		limit int,
	) ([]AccountShareRoomOperation, error)
	GetRoomOperation(
		ctx context.Context,
		viewerUserID int64,
		viewerIsAdmin bool,
		operationID string,
	) (*AccountShareRoomOperation, error)
}

func (s *AccountShareModeService) roomManagementStateRepository() (accountShareRoomManagementStateRepository, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.repo.(accountShareRoomManagementStateRepository)
	if !ok || repo == nil {
		return nil, ErrServiceUnavailable
	}
	return repo, nil
}

func (s *AccountShareModeService) lifecycleRepository() (accountShareLifecycleRepository, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.repo.(accountShareLifecycleRepository)
	if !ok || repo == nil {
		return nil, ErrServiceUnavailable
	}
	return repo, nil
}

func (s *AccountShareModeService) GetRoomManagementState(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
) (*AccountShareRoomManagementState, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	repo, err := s.roomManagementStateRepository()
	if err != nil {
		return nil, err
	}
	state, err := repo.GetRoomManagementState(ctx, viewerUserID, viewerIsAdmin, listingID)
	if err != nil {
		return nil, err
	}
	if state.DeletedAt != nil {
		state.HealthState = AccountShareRoomHealthUnavailable
		state.AdmissionRemainingSeats = 0
		state.AllowedActions = []string{}
		return state, nil
	}
	if err := s.hydrateRoomRuntimeState(ctx, state); err != nil {
		return nil, err
	}
	state.AllowedActions = accountShareRoomAllowedActions(state, viewerIsAdmin)
	return state, nil
}

func (s *AccountShareModeService) DrainRoom(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input AccountShareRoomLifecycleCommandInput,
) (*AccountShareRoomManagementState, error) {
	if err := validateAccountShareLifecycleCommand(actorUserID, listingID, input.ExpectedVersion); err != nil {
		return nil, err
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return nil, err
	}
	if _, err := repo.TransitionRoomLifecycle(
		ctx,
		actorUserID,
		actorIsAdmin,
		listingID,
		AccountShareRoomActionDrain,
		input,
	); err != nil {
		return nil, err
	}
	// 同步清退全部成员（排队直接终结、活跃结算+退款）。失败不阻塞下架——
	// finalizer 每 15s 会对残留成员重跑清退直至收口。
	if billing, err := repo.ClearRoomMembersForDrain(ctx, actorUserID, actorIsAdmin, listingID); err != nil {
		log.Printf("account_share_mode: drain member clearing failed (finalizer will retry): listing=%d err=%v", listingID, err)
	} else {
		s.invalidateSeatBillingCaches(billing)
	}
	return s.GetRoomManagementState(ctx, actorUserID, actorIsAdmin, listingID)
}

func (s *AccountShareModeService) ActivateRoom(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input AccountShareRoomLifecycleCommandInput,
) (*AccountShareRoomManagementState, error) {
	if err := validateAccountShareLifecycleCommand(actorUserID, listingID, input.ExpectedVersion); err != nil {
		return nil, err
	}
	if s == nil ||
		s.accountTestService == nil ||
		s.rateLimitService == nil ||
		s.accountRepo == nil {
		return nil, ErrServiceUnavailable
	}
	if roomAccountLister, ok := s.repo.(accountShareRoomAccountLister); !ok || roomAccountLister == nil {
		return nil, ErrServiceUnavailable
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return nil, err
	}
	validating, err := repo.TransitionRoomLifecycle(
		ctx,
		actorUserID,
		actorIsAdmin,
		listingID,
		AccountShareRoomActionActivate,
		input,
	)
	if err != nil {
		return nil, err
	}
	validationCtx, cancelValidation := context.WithTimeout(
		context.WithoutCancel(ctx),
		accountShareRoomValidationWorkerTimeout,
	)
	defer cancelValidation()
	_, validationReason, err := s.finalizeRoomValidation(
		validationCtx,
		validating,
		actorUserID,
		actorIsAdmin,
		nil,
	)
	if err != nil {
		return nil, err
	}
	state, stateErr := s.GetRoomManagementState(validationCtx, actorUserID, actorIsAdmin, listingID)
	if validationReason != "" {
		if stateErr != nil {
			return nil, stateErr
		}
		return state, accountShareActivationValidationError(validationReason)
	}
	return state, stateErr
}

func (s *AccountShareModeService) finalizeRoomValidation(
	ctx context.Context,
	listing *AccountShareListing,
	actorUserID int64,
	actorIsAdmin bool,
	commitGuard *ClusterLeaseGuard,
) (*AccountShareListing, string, error) {
	if listing == nil || listing.ID <= 0 || listing.RowVersion <= 0 {
		return nil, "", ErrAccountShareListingNotFound
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return nil, "", err
	}
	validationReason := ""
	if validationErr := s.validateRoomActivation(ctx, listing); validationErr != nil {
		validationReason = strings.TrimSpace(validationErr.Error())
	}
	command := "validation-pass"
	if validationReason != "" {
		command = "validation-fail"
	}
	if commitGuard != nil {
		if err := commitGuard.Check(ctx); err != nil {
			return nil, validationReason, err
		}
	}
	updated, err := repo.TransitionRoomLifecycle(
		ctx,
		actorUserID,
		actorIsAdmin,
		listing.ID,
		command,
		AccountShareRoomLifecycleCommandInput{
			ExpectedVersion: listing.RowVersion,
			Reason:          validationReason,
			Confirmed:       true,
		},
	)
	if err != nil {
		return nil, validationReason, err
	}
	return updated, validationReason, nil
}

func (s *AccountShareModeService) SuspendRoom(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input AccountShareRoomLifecycleCommandInput,
) (*AccountShareRoomManagementState, error) {
	if err := validateAccountShareLifecycleCommand(actorUserID, listingID, input.ExpectedVersion); err != nil {
		return nil, err
	}
	if !actorIsAdmin {
		return nil, ErrInsufficientPerms
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		return nil, ErrAccountShareRoomReasonRequired
	}
	if !input.Confirmed {
		return nil, ErrAccountShareForceConfirmationRequired.WithMetadata(map[string]string{"field": "confirmed"})
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return nil, err
	}
	if _, err := repo.TransitionRoomLifecycle(
		ctx,
		actorUserID,
		true,
		listingID,
		AccountShareRoomActionSuspend,
		input,
	); err != nil {
		return nil, err
	}
	return s.GetRoomManagementState(ctx, actorUserID, true, listingID)
}

func (s *AccountShareModeService) CreateRoomDeleteIntent(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input AccountShareRoomDeleteIntentInput,
) (*AccountShareRoomDeleteIntent, error) {
	if actorUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if input.ExpectedVersion <= 0 {
		return nil, ErrAccountShareExpectedVersionRequired.WithMetadata(map[string]string{"field": "expected_version"})
	}
	if actorIsAdmin && strings.TrimSpace(input.Reason) == "" {
		return nil, ErrAccountShareRoomReasonRequired
	}
	state, err := s.GetRoomManagementState(ctx, actorUserID, actorIsAdmin, listingID)
	if err != nil {
		return nil, err
	}
	if state.RowVersion != input.ExpectedVersion {
		return nil, ErrAccountShareVersionConflict.WithMetadata(map[string]string{
			"expected_version": strconv.FormatInt(input.ExpectedVersion, 10),
			"actual_version":   strconv.FormatInt(state.RowVersion, 10),
		})
	}
	intent := &AccountShareRoomDeleteIntent{
		ListingID:     listingID,
		RoomName:      state.RoomName,
		RowVersion:    state.RowVersion,
		CanDelete:     !state.Blockers.Any(),
		AccountCount:  state.RoomAccountCount,
		Blockers:      state.Blockers,
		HistoryNotice: "删除后房间不可恢复，但历史消费、结算和评价会继续保留。",
	}
	if !intent.CanDelete {
		return intent, nil
	}
	expiresAt := time.Now().UTC().Add(AccountShareRoomDeleteTokenTTL)
	token, err := s.signRoomDeleteToken(accountShareRoomDeleteClaims{
		Action:      accountShareRoomDeleteTokenAction,
		ListingID:   listingID,
		ActorUserID: actorUserID,
		RowVersion:  state.RowVersion,
		RoomName:    state.RoomName,
		ExpiresAt:   expiresAt.Unix(),
	})
	if err != nil {
		return nil, err
	}
	intent.Token = token
	intent.ExpiresAt = &expiresAt
	return intent, nil
}

func (s *AccountShareModeService) DeleteRoom(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input AccountShareRoomDeleteInput,
) (*AccountShareRoomOperation, error) {
	if actorUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	repo, err := s.lifecycleRepository()
	if err != nil {
		return nil, err
	}
	if input.RequestID != "" && len(input.RequestID) <= 128 {
		existing, err := repo.FindRoomDeleteOperation(
			ctx,
			actorUserID,
			actorIsAdmin,
			listingID,
			input.RequestID,
		)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return s.tryFinalizeRoomDeletion(ctx, repo, existing)
		}
	}
	input.RoomName = strings.TrimSpace(input.RoomName)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ExpectedVersion <= 0 {
		return nil, ErrAccountShareExpectedVersionRequired.WithMetadata(map[string]string{"field": "expected_version"})
	}
	if !input.Confirmed {
		return nil, ErrAccountShareForceConfirmationRequired.WithMetadata(map[string]string{"field": "confirmed"})
	}
	if actorIsAdmin && input.Reason == "" {
		return nil, ErrAccountShareRoomReasonRequired
	}
	if err := s.validateRoomDeleteToken(
		input.Token,
		actorUserID,
		listingID,
		input.ExpectedVersion,
		input.RoomName,
		time.Now().UTC(),
	); err != nil {
		return nil, err
	}
	state, err := s.GetRoomManagementState(ctx, actorUserID, actorIsAdmin, listingID)
	if err != nil {
		return nil, err
	}
	if state.RowVersion != input.ExpectedVersion {
		return nil, ErrAccountShareVersionConflict.WithMetadata(map[string]string{
			"expected_version": strconv.FormatInt(input.ExpectedVersion, 10),
			"actual_version":   strconv.FormatInt(state.RowVersion, 10),
		})
	}
	if state.RoomName != input.RoomName {
		return nil, ErrAccountShareRoomDeleteTokenInvalid
	}
	if state.Blockers.Any() {
		return nil, ErrAccountShareRoomDeleteBlocked.WithMetadata(state.Blockers.Metadata())
	}
	operation, err := repo.SoftDeleteRoom(ctx, actorUserID, actorIsAdmin, listingID, input)
	if err != nil {
		return nil, err
	}
	return s.tryFinalizeRoomDeletion(ctx, repo, operation)
}

func (s *AccountShareModeService) GetRoomOperation(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	operationID string,
) (*AccountShareRoomOperation, error) {
	if viewerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return nil, ErrAccountShareRoomOperationConflict
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return nil, err
	}
	return repo.GetRoomOperation(ctx, viewerUserID, viewerIsAdmin, operationID)
}

func (s *AccountShareModeService) hydrateRoomRuntimeState(
	ctx context.Context,
	state *AccountShareRoomManagementState,
) error {
	if state == nil {
		return ErrAccountShareListingNotFound
	}
	if s == nil || s.concurrencyService == nil {
		state.Blockers.RuntimeDependencyUnavailable = true
		return ErrAccountShareRuntimeDependencyUnavailable
	}
	if len(state.RuntimeAccountIDs) > 0 {
		accountIDs := append([]int64(nil), state.RuntimeAccountIDs...)
		sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
		counts, err := s.concurrencyService.GetAccountConcurrencyBatch(ctx, accountIDs)
		if err != nil || counts == nil {
			state.Blockers.RuntimeDependencyUnavailable = true
			if err == nil {
				err = ErrAccountShareRuntimeDependencyUnavailable
			}
			return ErrAccountShareRuntimeDependencyUnavailable.WithCause(err)
		}
		total := 0
		for _, accountID := range accountIDs {
			if count := counts[accountID]; count > 0 {
				total += count
			}
		}
		state.InFlightConcurrency = total
		state.Blockers.InFlightRequestCount = total
		return nil
	}

	total := 0
	ids := append([]int64(nil), state.RuntimeMembershipIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, membershipID := range ids {
		if membershipID <= 0 {
			continue
		}
		count, err := s.concurrencyService.GetAccountShareMembershipConcurrency(ctx, membershipID)
		if err != nil {
			state.Blockers.RuntimeDependencyUnavailable = true
			return ErrAccountShareRuntimeDependencyUnavailable.WithCause(err)
		}
		if count > 0 {
			total += count
		}
	}
	state.InFlightConcurrency = total
	state.Blockers.InFlightRequestCount = total
	return nil
}

func (s *AccountShareModeService) tryFinalizeRoomDeletion(
	ctx context.Context,
	repo accountShareLifecycleRepository,
	operation *AccountShareRoomOperation,
) (*AccountShareRoomOperation, error) {
	if operation == nil || operation.ListingID <= 0 || strings.TrimSpace(operation.ID) == "" {
		return nil, ErrAccountShareRoomOperationConflict
	}
	if operation.Status == "succeeded" || operation.Status == "failed" || operation.Status == "cancelled" {
		return operation, nil
	}

	state, err := repo.GetRoomManagementState(
		ctx,
		operationActorUserID(operation),
		operationActorIsAdmin(operation),
		operation.ListingID,
	)
	if err != nil {
		return operation, nil
	}
	if err := s.hydrateRoomRuntimeState(ctx, state); err != nil {
		return operation, nil
	}
	if !roomDeletionReadyForOperation(state, operation.ID) {
		return operation, nil
	}
	finalized, err := repo.FinalizeRoomDeletion(ctx, operation.ListingID, operation.ID)
	if err != nil {
		if errors.Is(err, ErrAccountShareRoomDeleteBlocked) ||
			errors.Is(err, ErrAccountShareRoomOperationConflict) ||
			errors.Is(err, ErrAccountShareVersionConflict) {
			return operation, nil
		}
		return nil, err
	}
	return finalized, nil
}

func roomDeletionReadyForOperation(state *AccountShareRoomManagementState, operationID string) bool {
	if state == nil || state.DeletedAt != nil || strings.TrimSpace(operationID) == "" {
		return false
	}
	blockers := state.Blockers
	if blockers.ConflictingOperation && blockers.ConflictingOperationID != operationID {
		return false
	}
	blockers.ConflictingOperation = false
	blockers.ConflictingOperationID = ""
	return !blockers.Any()
}

func operationActorUserID(operation *AccountShareRoomOperation) int64 {
	if operation == nil {
		return 0
	}
	return operation.ActorUserID
}

func operationActorIsAdmin(operation *AccountShareRoomOperation) bool {
	return operation != nil && operation.ActorRole == "admin"
}

func (s *AccountShareModeService) processRoomLifecycleOnce(ctx context.Context, guard *ClusterLeaseGuard) error {
	if s == nil {
		return ErrServiceUnavailable
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return err
	}
	afterID := s.roomLifecycleCursor()
	listingIDs, err := repo.ListOpenRoomLifecycleListingIDs(ctx, afterID, AccountShareModeSeatBillingBatchSize)
	if err != nil {
		return fmt.Errorf("list open room lifecycle operations after listing %d: %w", afterID, err)
	}
	if len(listingIDs) == 0 && afterID > 0 {
		s.setRoomLifecycleCursor(0)
		listingIDs, err = repo.ListOpenRoomLifecycleListingIDs(ctx, 0, AccountShareModeSeatBillingBatchSize)
		if err != nil {
			return fmt.Errorf("restart open room lifecycle operation scan: %w", err)
		}
	}
	if len(listingIDs) > 0 {
		s.setRoomLifecycleCursor(listingIDs[len(listingIDs)-1])
	}
	processingErrors := make([]error, 0)
	for _, listingID := range listingIDs {
		state, err := repo.GetRoomManagementState(ctx, 0, true, listingID)
		if err != nil {
			processingErrors = append(processingErrors, fmt.Errorf("load room lifecycle state for listing %d: %w", listingID, err))
			continue
		}
		if state == nil || strings.TrimSpace(state.PendingOperationID) == "" {
			processingErrors = append(processingErrors, fmt.Errorf("listing %d has an open lifecycle operation without a pending operation pointer", listingID))
			continue
		}
		hydrateErr := s.hydrateRoomRuntimeState(ctx, state)
		operation, err := repo.GetRoomOperation(ctx, 0, true, state.PendingOperationID)
		if err != nil {
			processingErrors = append(processingErrors, fmt.Errorf("load room lifecycle operation %s for listing %d: %w", state.PendingOperationID, listingID, err))
			continue
		}
		if operation == nil || operation.ListingID != listingID {
			processingErrors = append(processingErrors, fmt.Errorf("listing %d lifecycle operation %s is inconsistent", listingID, state.PendingOperationID))
			continue
		}
		switch operation.Action {
		case AccountShareRoomOperationActionDelete:
			if hydrateErr != nil {
				processingErrors = append(processingErrors, fmt.Errorf("hydrate delete operation %s for listing %d: %w", operation.ID, listingID, hydrateErr))
				continue
			}
			if !roomDeletionReadyForOperation(state, operation.ID) {
				continue
			}
			if leaseErr := guard.Check(ctx); leaseErr != nil {
				return errors.Join(errors.Join(processingErrors...), leaseErr)
			}
			if _, finalizeErr := repo.FinalizeRoomDeletion(ctx, operation.ListingID, operation.ID); finalizeErr != nil &&
				!isExpectedRoomLifecycleFinalizationError(finalizeErr) {
				processingErrors = append(processingErrors, fmt.Errorf("finalize delete operation %s for listing %d: %w", operation.ID, listingID, finalizeErr))
			}
		case AccountShareRoomOperationActionDrain:
			// 残留成员重清退：排空事务与"派发失败降级"并发时可能漏掉一个
			// 恰在降级中的成员，这里幂等重跑清退直至归零。
			if state.Blockers.QueuedMembershipCount > 0 || state.Blockers.ActiveMembershipCount > 0 {
				if leaseErr := guard.Check(ctx); leaseErr != nil {
					return errors.Join(errors.Join(processingErrors...), leaseErr)
				}
				billing, clearErr := repo.ClearRoomMembersForDrain(ctx, 0, true, listingID)
				if clearErr != nil {
					log.Printf("account_share_mode: drain member re-clearing failed: listing=%d err=%v", listingID, clearErr)
					processingErrors = append(processingErrors, fmt.Errorf("re-clear drain members for listing %d: %w", listingID, clearErr))
				} else {
					s.invalidateSeatBillingCaches(billing)
				}
				continue
			}
			if hydrateErr == nil && roomDeletionReadyForOperation(state, operation.ID) {
				if leaseErr := guard.Check(ctx); leaseErr != nil {
					return errors.Join(errors.Join(processingErrors...), leaseErr)
				}
				if _, finalizeErr := repo.FinalizeDrainingRoom(ctx, listingID, state.RowVersion); finalizeErr != nil {
					log.Printf("account_share_mode: drain finalize failed: listing=%d err=%v", listingID, finalizeErr)
					if !isExpectedRoomLifecycleFinalizationError(finalizeErr) {
						processingErrors = append(processingErrors, fmt.Errorf("finalize drain operation %s for listing %d: %w", operation.ID, listingID, finalizeErr))
					}
				}
				continue
			}
			// 30 分钟强制收口兜底：DB 侧成员/结算已清零、仅剩运行时侧
			// blocker（在途请求计数或运行时依赖不可用/hydrate 失败）时
			// 不允许无限等待。FinalizeDrainingRoom 内部仍会复核全部 DB
			// blocker，强制只是跳过运行时侧的不确定性。
			if time.Since(operation.CreatedAt) > 30*time.Minute &&
				state.Blockers.EndingMembershipCount == 0 &&
				state.Blockers.SynchronousBillingPendingCount == 0 {
				log.Printf("account_share_mode: drain force-finalize after timeout: listing=%d blockers=%v", listingID, state.Blockers.Metadata())
				if leaseErr := guard.Check(ctx); leaseErr != nil {
					return errors.Join(errors.Join(processingErrors...), leaseErr)
				}
				if _, finalizeErr := repo.FinalizeDrainingRoom(ctx, listingID, state.RowVersion); finalizeErr != nil {
					log.Printf("account_share_mode: drain force-finalize failed: listing=%d err=%v", listingID, finalizeErr)
					if !isExpectedRoomLifecycleFinalizationError(finalizeErr) {
						processingErrors = append(processingErrors, fmt.Errorf("force-finalize drain operation %s for listing %d: %w", operation.ID, listingID, finalizeErr))
					}
				}
				continue
			}
			log.Printf("account_share_mode: drain finalize waiting: listing=%d hydrate_err=%v blockers=%v", listingID, hydrateErr, state.Blockers.Metadata())
			if hydrateErr != nil {
				processingErrors = append(processingErrors, fmt.Errorf("hydrate drain operation %s for listing %d: %w", operation.ID, listingID, hydrateErr))
			}
		default:
			processingErrors = append(processingErrors, fmt.Errorf("listing %d has unsupported open lifecycle operation %s action %q", listingID, operation.ID, operation.Action))
		}
	}
	return errors.Join(processingErrors...)
}

func isExpectedRoomLifecycleFinalizationError(err error) bool {
	return errors.Is(err, ErrAccountShareRoomDeleteBlocked) ||
		errors.Is(err, ErrAccountShareRoomOperationConflict) ||
		errors.Is(err, ErrAccountShareVersionConflict) ||
		errors.Is(err, ErrAccountShareRoomInvalidTransition) ||
		errors.Is(err, ErrAccountShareRoomDeleted)
}

func (s *AccountShareModeService) roomLifecycleCursor() int64 {
	if s == nil {
		return 0
	}
	s.roomLifecycleCursorMu.Lock()
	defer s.roomLifecycleCursorMu.Unlock()
	return s.roomLifecycleAfterID
}

func (s *AccountShareModeService) setRoomLifecycleCursor(afterID int64) {
	if s == nil {
		return
	}
	if afterID < 0 {
		afterID = 0
	}
	s.roomLifecycleCursorMu.Lock()
	s.roomLifecycleAfterID = afterID
	s.roomLifecycleCursorMu.Unlock()
}

func (s *AccountShareModeService) runRoomValidationWorker() {
	defer s.seatBillingWG.Done()
	ticker := time.NewTicker(accountShareRoomValidationInterval)
	defer ticker.Stop()

	s.processRoomValidationOnce()
	for {
		select {
		case <-ticker.C:
			s.processRoomValidationOnce()
		case <-s.seatBillingStopCh:
			return
		}
	}
}

func (s *AccountShareModeService) processRoomValidationOnce() {
	if s == nil ||
		s.repo == nil ||
		s.accountRepo == nil ||
		s.accountTestService == nil ||
		s.rateLimitService == nil ||
		s.taskExecutor == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.seatBillingWorkerContext(), accountShareRoomValidationWorkerTimeout)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, accountShareRoomValidationTaskName, func(
		taskCtx context.Context,
		guard *ClusterLeaseGuard,
	) error {
		return s.processRoomValidationOnceLeased(taskCtx, guard)
	})
	if err != nil {
		log.Printf("account_share_mode: room validation lease failed: %v", err)
	}
}

func (s *AccountShareModeService) processRoomValidationOnceLeased(
	ctx context.Context,
	guard *ClusterLeaseGuard,
) error {
	if guard == nil {
		return ErrServiceUnavailable
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	repo, err := s.lifecycleRepository()
	if err != nil {
		return err
	}
	staleBefore := time.Now().UTC().Add(-accountShareRoomValidationRecoveryDelay)
	listingIDs, err := repo.ListValidatingRoomIDs(ctx, staleBefore, accountShareRoomValidationBatchSize)
	if err != nil {
		return err
	}
	for _, listingID := range listingIDs {
		if err := guard.Check(ctx); err != nil {
			return err
		}
		listing, err := s.repo.GetListingByID(ctx, listingID, 0)
		if errors.Is(err, ErrAccountShareListingNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if listing == nil || listing.Status != AccountShareListingStatusValidating {
			continue
		}
		if _, _, err := s.finalizeRoomValidation(ctx, listing, 0, true, guard); err != nil {
			if errors.Is(err, ErrAccountShareRoomInvalidTransition) ||
				errors.Is(err, ErrAccountShareVersionConflict) ||
				errors.Is(err, ErrAccountShareRoomOperationConflict) {
				continue
			}
			return err
		}
	}
	return guard.Check(ctx)
}

func accountShareRoomAllowedActions(state *AccountShareRoomManagementState, viewerIsAdmin bool) []string {
	if state == nil || state.DeletedAt != nil || state.Blockers.ConflictingOperation {
		return []string{}
	}
	actions := make([]string, 0, 4)
	switch state.LifecycleStatus {
	case AccountShareListingStatusActive:
		actions = append(actions, AccountShareRoomActionDrain)
		if viewerIsAdmin {
			actions = append(actions, AccountShareRoomActionSuspend)
		}
	case AccountShareListingStatusDraining:
		actions = append(actions, AccountShareRoomActionActivate)
		if viewerIsAdmin {
			actions = append(actions, AccountShareRoomActionSuspend)
		}
	case AccountShareListingStatusPaused:
		actions = append(actions, AccountShareRoomActionActivate)
	case AccountShareListingStatusSuspended:
		if viewerIsAdmin {
			actions = append(actions, AccountShareRoomActionActivate)
		}
	}
	if !state.Blockers.Any() {
		switch state.LifecycleStatus {
		case AccountShareListingStatusActive, AccountShareListingStatusDraining, AccountShareListingStatusPaused, AccountShareListingStatusSuspended:
			actions = append(actions, AccountShareRoomActionDelete)
		}
	}
	sort.Strings(actions)
	return actions
}

func validateAccountShareLifecycleCommand(actorUserID, listingID, expectedVersion int64) error {
	if actorUserID <= 0 {
		return ErrUserNotFound
	}
	if listingID <= 0 {
		return ErrAccountShareListingNotFound
	}
	if expectedVersion <= 0 {
		return ErrAccountShareExpectedVersionRequired.WithMetadata(map[string]string{"field": "expected_version"})
	}
	return nil
}

func (s *AccountShareModeService) validateRoomActivation(
	ctx context.Context,
	listing *AccountShareListing,
) error {
	if listing == nil || listing.ID <= 0 || listing.OwnerUserID <= 0 {
		return ErrAccountShareAccountUnavailable
	}
	allowedModels := normalizeAllowedModels(listing.AllowedModels)
	if len(allowedModels) == 0 {
		return ErrAccountShareModeAllowedModelsRequired
	}
	if s == nil || s.repo == nil || s.accountRepo == nil {
		return ErrServiceUnavailable
	}
	roomAccountLister, ok := s.repo.(accountShareRoomAccountLister)
	if !ok || roomAccountLister == nil {
		return ErrServiceUnavailable
	}
	roomAccounts, err := roomAccountLister.ListRoomAccounts(
		ctx,
		listing.ID,
		listing.OwnerUserID,
		false,
	)
	if err != nil {
		return err
	}
	if len(roomAccounts) == 0 {
		return ErrAccountShareRelistAccountUnavailable
	}
	accountIDs := make([]int64, 0, len(roomAccounts))
	for _, roomAccount := range roomAccounts {
		if roomAccount.AccountID <= 0 {
			return ErrAccountShareAccountUnavailable
		}
		accountIDs = append(accountIDs, roomAccount.AccountID)
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return err
	}
	accountsByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil && account.ID > 0 {
			accountsByID[account.ID] = account
		}
	}
	if len(accountsByID) != len(accountIDs) {
		return ErrAccountShareAccountUnavailable
	}

	routableAccounts := make([]*Account, 0, len(roomAccounts))
	for _, roomAccount := range roomAccounts {
		account := accountsByID[roomAccount.AccountID]
		if account == nil {
			return ErrAccountShareAccountUnavailable
		}
		for _, model := range allowedModels {
			if account.IsModelSupported(model) {
				continue
			}
			return ErrAccountShareModeUnsupportedModel.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(account.ID, 10),
				"model":      model,
			})
		}
		if accountShareRoomAccountIsRoutable(roomAccount) {
			routableAccounts = append(routableAccounts, account)
		}
	}
	if len(routableAccounts) == 0 {
		return ErrAccountShareRelistAccountUnavailable
	}

	connectivityModel := accountShareRoomConnectivityTestModel(listing.Platform, allowedModels)
	// OpenCode 房间的恢复校验只需要确认账号凭证和上游连通性，不应使用房间
	// 白名单中的任意模型作为探针。白名单首项可能是区域、套餐或上游状态
	// 不稳定的模型（例如 grok-4.5），会把模型级失败误判成账号不可用。
	// OpenCode 账号测试服务的默认探针是 deepseek-v4-flash，这里显式固定
	// 使用同一个模型，避免房间恢复流程传入首个白名单模型覆盖该默认值。
	validationCtx, validationCancel := context.WithTimeout(
		ctx,
		accountShareConnectivityTestTimeout(connectivityModel),
	)
	defer validationCancel()
	type connectivityValidationResult struct {
		index int
		err   error
	}
	jobs := make(chan int, len(routableAccounts))
	results := make(chan connectivityValidationResult, len(routableAccounts))
	workerCount := accountShareRoomActivationMaxConcurrency
	if workerCount > len(routableAccounts) {
		workerCount = len(routableAccounts)
	}
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		go func() {
			for accountIndex := range jobs {
				account := routableAccounts[accountIndex]
				result, testErr := s.accountTestService.RunTestBackground(
					validationCtx,
					account.ID,
					connectivityModel,
				)
				if testErr != nil {
					results <- connectivityValidationResult{
						index: accountIndex,
						err: fmt.Errorf(
							"账号 %d 的模型 %s 连通性测试失败: %w",
							account.ID,
							connectivityModel,
							testErr,
						),
					}
					continue
				}
				if result == nil || strings.TrimSpace(result.Status) != "success" {
					reason := "账号连通性测试未通过"
					if result != nil && strings.TrimSpace(result.ErrorMessage) != "" {
						reason = strings.TrimSpace(result.ErrorMessage)
					}
					results <- connectivityValidationResult{
						index: accountIndex,
						err: fmt.Errorf(
							"账号 %d 的模型 %s 连通性测试失败: %s",
							account.ID,
							connectivityModel,
							reason,
						),
					}
					continue
				}
				results <- connectivityValidationResult{index: accountIndex}
			}
		}()
	}
	for accountIndex := range routableAccounts {
		jobs <- accountIndex
	}
	close(jobs)
	validationErrors := make([]error, len(routableAccounts))
	for range routableAccounts {
		result := <-results
		validationErrors[result.index] = result.err
		if result.err != nil {
			validationCancel()
		}
	}
	close(results)
	for _, validationErr := range validationErrors {
		if validationErr != nil {
			return validationErr
		}
	}

	testedAccountIDs := make(map[int64]struct{}, len(routableAccounts))
	for _, account := range routableAccounts {
		if _, err := s.rateLimitService.RecoverAccountAfterSuccessfulTest(ctx, account.ID); err != nil {
			return err
		}
		testedAccountIDs[account.ID] = struct{}{}
	}

	refreshedRoomAccounts, err := roomAccountLister.ListRoomAccounts(
		ctx,
		listing.ID,
		listing.OwnerUserID,
		false,
	)
	if err != nil {
		return err
	}
	currentRoutableCount := 0
	for _, roomAccount := range refreshedRoomAccounts {
		if !accountShareRoomAccountIsRoutable(roomAccount) {
			continue
		}
		currentRoutableCount++
		if _, tested := testedAccountIDs[roomAccount.AccountID]; !tested {
			return ErrAccountShareRelistAccountUnavailable
		}
	}
	if currentRoutableCount == 0 {
		return ErrAccountShareRelistAccountUnavailable
	}
	return nil
}

func accountShareRoomAccountIsRoutable(account AccountShareRoomAccount) bool {
	return strings.EqualFold(strings.TrimSpace(account.PlacementState), "active") &&
		strings.EqualFold(strings.TrimSpace(account.Status), StatusActive) &&
		account.Schedulable &&
		account.CurrentConcurrency > 0
}

func accountShareActivationValidationError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "room validation failed"
	}
	return infraerrors.BadRequest("ACCOUNT_SHARE_ROOM_VALIDATION_FAILED", reason)
}

func (s *AccountShareModeService) signRoomDeleteToken(claims accountShareRoomDeleteClaims) (string, error) {
	if s == nil || len(s.actionTokenSecret) < 32 {
		return "", ErrServiceUnavailable
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.actionTokenSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *AccountShareModeService) validateRoomDeleteToken(
	token string,
	actorUserID int64,
	listingID int64,
	rowVersion int64,
	roomName string,
	now time.Time,
) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrAccountShareRoomDeleteTokenRequired
	}
	if s == nil || len(s.actionTokenSecret) < 32 {
		return ErrServiceUnavailable
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ErrAccountShareRoomDeleteTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrAccountShareRoomDeleteTokenInvalid
	}
	mac := hmac.New(sha256.New, s.actionTokenSecret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return ErrAccountShareRoomDeleteTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrAccountShareRoomDeleteTokenInvalid
	}
	var claims accountShareRoomDeleteClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ErrAccountShareRoomDeleteTokenInvalid
	}
	if claims.Action != accountShareRoomDeleteTokenAction ||
		claims.ActorUserID != actorUserID ||
		claims.ListingID != listingID ||
		claims.RowVersion != rowVersion ||
		claims.RoomName != strings.TrimSpace(roomName) ||
		claims.ExpiresAt <= now.Unix() {
		return ErrAccountShareRoomDeleteTokenInvalid
	}
	return nil
}
