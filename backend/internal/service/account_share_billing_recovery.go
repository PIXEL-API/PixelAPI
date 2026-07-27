package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	accountShareBillingRecoveryBatchSize = 100

	accountShareBillingRecoveryReasonCode = "runtime_lease_expired_without_usage"
)

type accountShareBillingRecoveryResult struct {
	Examined          int
	Escalated         int
	ActiveLease       int
	UnknownLeaseState int
	ConcurrentChange  int
}

// recoverStaleBillingIntentsOnce handles only in_flight intents whose durable
// state is older than the runtime slot TTL. Redis remains authoritative: a
// missing capability, Redis error, or positive lease count never escalates.
func (s *AccountShareModeService) recoverStaleBillingIntentsOnce(
	ctx context.Context,
	now time.Time,
	checkLease func(context.Context) error,
) (accountShareBillingRecoveryResult, error) {
	result := accountShareBillingRecoveryResult{}
	if s == nil || s.billingIntentRepository == nil {
		return result, ErrServiceUnavailable
	}
	if checkLease == nil {
		checkLease = func(context.Context) error { return nil }
	}
	if err := checkLease(ctx); err != nil {
		return result, err
	}
	leaseTTL, err := s.accountShareRuntimeLeaseTTL()
	if err != nil {
		return result, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	staleBefore := now.Add(-leaseTTL)
	candidates, err := s.billingIntentRepository.ListStaleForAttention(
		ctx,
		staleBefore,
		accountShareBillingRecoveryBatchSize,
	)
	if err != nil {
		return result, err
	}

	var recoveryErrors []error
	for _, candidate := range candidates {
		result.Examined++
		if err := checkLease(ctx); err != nil {
			return result, errors.Join(append(recoveryErrors, err)...)
		}
		if candidate.ID <= 0 ||
			candidate.MembershipID <= 0 ||
			candidate.StateToken <= 0 ||
			candidate.Status != AccountShareBillingIntentStatusInFlight ||
			candidate.UpdatedAt.After(staleBefore) {
			recoveryErrors = append(
				recoveryErrors,
				fmt.Errorf("invalid stale billing intent candidate id=%d", candidate.ID),
			)
			continue
		}

		hasLease, leaseErr := s.hasActiveMembershipLease(ctx, candidate.MembershipID)
		if leaseErr != nil {
			result.UnknownLeaseState++
			recoveryErrors = append(
				recoveryErrors,
				fmt.Errorf("inspect billing intent %d runtime lease: %w", candidate.ID, leaseErr),
			)
			continue
		}
		if hasLease {
			result.ActiveLease++
			continue
		}
		if err := checkLease(ctx); err != nil {
			return result, errors.Join(append(recoveryErrors, err)...)
		}

		state, escalateErr := s.billingIntentRepository.EscalateStaleToNeedsAttention(
			ctx,
			EscalateAccountShareBillingIntentInput{
				AccountShareBillingIntentTransition: AccountShareBillingIntentTransition{
					ID:                 candidate.ID,
					ExpectedStateToken: candidate.StateToken,
				},
				ReasonCode:    accountShareBillingRecoveryReasonCode,
				ReasonMessage: "runtime membership lease expired before a durable usage payload was recorded",
				StaleBefore:   staleBefore,
			},
		)
		if errors.Is(escalateErr, ErrAccountShareBillingIntentStateConflict) ||
			errors.Is(escalateErr, ErrAccountShareBillingIntentNotFound) {
			result.ConcurrentChange++
			continue
		}
		if escalateErr != nil {
			recoveryErrors = append(
				recoveryErrors,
				fmt.Errorf("escalate stale billing intent %d: %w", candidate.ID, escalateErr),
			)
			continue
		}
		if state == nil || state.Status != AccountShareBillingIntentStatusNeedsAttention {
			recoveryErrors = append(
				recoveryErrors,
				fmt.Errorf("escalate stale billing intent %d returned an invalid state", candidate.ID),
			)
			continue
		}
		result.Escalated++
	}
	return result, errors.Join(recoveryErrors...)
}

func (s *AccountShareModeService) accountShareRuntimeLeaseTTL() (time.Duration, error) {
	if s == nil || s.concurrencyService == nil || s.concurrencyService.cache == nil {
		return 0, ErrServiceUnavailable
	}
	cache, ok := s.concurrencyService.cache.(accountShareRuntimeLeaseCache)
	if !ok {
		return 0, ErrServiceUnavailable
	}
	ttl := cache.SlotLeaseTTL()
	if ttl <= 0 {
		return 0, fmt.Errorf("%w: invalid account share runtime lease TTL", ErrServiceUnavailable)
	}
	return ttl, nil
}

func (s *AccountShareModeService) ListBillingIntentsNeedingAttention(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	params pagination.PaginationParams,
) ([]AccountShareBillingIntentAdminRecord, *pagination.PaginationResult, error) {
	if actorUserID <= 0 || !actorIsAdmin {
		return nil, nil, ErrAccountShareBillingAdminRequired
	}
	repo, err := s.accountShareBillingAdminRepository()
	if err != nil {
		return nil, nil, err
	}
	params = normalizeAccountShareBillingAdminPagination(params)
	items, total, err := repo.ListNeedsAttentionForAdmin(ctx, params.Offset(), params.PageSize)
	if err != nil {
		return nil, nil, mapAccountShareBillingAdminError(err)
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

func (s *AccountShareModeService) GetBillingIntentForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	intentID int64,
) (*AccountShareBillingIntentAdminRecord, error) {
	if actorUserID <= 0 || !actorIsAdmin {
		return nil, ErrAccountShareBillingAdminRequired
	}
	if intentID <= 0 {
		return nil, ErrAccountShareBillingWaiverInvalid
	}
	repo, err := s.accountShareBillingAdminRepository()
	if err != nil {
		return nil, err
	}
	record, err := repo.GetForAdmin(ctx, intentID)
	if err != nil {
		return nil, mapAccountShareBillingAdminError(err)
	}
	return record, nil
}

func (s *AccountShareModeService) WaiveBillingIntentForAdmin(
	ctx context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	intentID int64,
	input WaiveAccountShareBillingIntentInput,
) (*AccountShareBillingIntentWaiverResult, error) {
	if actorUserID <= 0 || !actorIsAdmin {
		return nil, ErrAccountShareBillingAdminRequired
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if intentID <= 0 || input.ExpectedStateToken <= 0 {
		return nil, ErrAccountShareBillingWaiverInvalid
	}
	if !input.Confirmed {
		return nil, ErrAccountShareBillingWaiverConfirmationRequired
	}
	if input.Reason == "" {
		return nil, ErrAccountShareBillingWaiverReasonRequired
	}
	if !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 1000 {
		return nil, ErrAccountShareBillingWaiverInvalid
	}
	repo, err := s.accountShareBillingAdminRepository()
	if err != nil {
		return nil, err
	}
	result, err := repo.WaiveNeedsAttention(ctx, WaiveAccountShareBillingIntentRepositoryInput{
		IntentID:           intentID,
		ExpectedStateToken: input.ExpectedStateToken,
		ActorUserID:        actorUserID,
		Reason:             input.Reason,
	})
	if err != nil {
		return nil, mapAccountShareBillingAdminError(err)
	}
	if result == nil ||
		result.Intent.Status != AccountShareBillingIntentStatusCancelled ||
		result.Waiver.IntentID != intentID {
		return nil, ErrServiceUnavailable
	}
	return result, nil
}

func (s *AccountShareModeService) accountShareBillingAdminRepository() (
	AccountShareBillingIntentAdminRepository,
	error,
) {
	if s == nil || s.billingIntentRepository == nil {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.billingIntentRepository.(AccountShareBillingIntentAdminRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return repo, nil
}

func mapAccountShareBillingAdminError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrAccountShareBillingIntentNotFound):
		return ErrAccountShareBillingAdminIntentNotFound
	case errors.Is(err, ErrAccountShareBillingIntentStateConflict),
		errors.Is(err, ErrAccountShareBillingIntentConflict):
		return ErrAccountShareBillingAdminConflict
	case errors.Is(err, ErrAccountShareBillingIntentInvalid):
		return ErrAccountShareBillingWaiverInvalid
	default:
		return err
	}
}

func logAccountShareBillingRecoveryError(err error) {
	if err != nil {
		log.Printf("account_share_mode: stale billing intent recovery deferred: %v", err)
	}
}
