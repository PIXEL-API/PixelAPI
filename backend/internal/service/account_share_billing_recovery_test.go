package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestRecoverStaleBillingIntentsFailsClosedWhenRedisErrors(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	repo := &accountShareBillingRecoveryRepoStub{
		candidates: []AccountShareBillingIntentAttentionCandidate{
			staleBillingIntentCandidate(now.Add(-30 * 24 * time.Hour)),
		},
	}
	cache := &accountShareBillingRecoveryCacheStub{
		ttl:      time.Minute,
		countErr: errors.New("redis unavailable"),
	}
	svc := accountShareBillingRecoveryTestService(repo, cache)

	result, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)
	require.ErrorContains(t, err, "redis unavailable")
	require.Equal(t, 1, result.UnknownLeaseState)
	require.Empty(t, repo.escalations)
}

func TestRecoverStaleBillingIntentsKeepsIntentInFlightWhileLeaseExists(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	repo := &accountShareBillingRecoveryRepoStub{
		candidates: []AccountShareBillingIntentAttentionCandidate{
			staleBillingIntentCandidate(now.Add(-2 * time.Minute)),
		},
	}
	cache := &accountShareBillingRecoveryCacheStub{ttl: time.Minute, count: 1}
	svc := accountShareBillingRecoveryTestService(repo, cache)

	result, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.ActiveLease)
	require.Empty(t, repo.escalations)
	require.Equal(t, 1, cache.countCalls)
}

func TestRecoverStaleBillingIntentsKeepsVeryOldIntentInFlightWhileLeaseExists(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	candidate := staleBillingIntentCandidate(now.Add(-30 * 24 * time.Hour))
	repo := &accountShareBillingRecoveryRepoStub{
		candidates: []AccountShareBillingIntentAttentionCandidate{candidate},
	}
	cache := &accountShareBillingRecoveryCacheStub{ttl: time.Minute, count: 1}
	svc := accountShareBillingRecoveryTestService(repo, cache)

	result, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)

	require.NoError(t, err)
	require.Zero(t, result.Escalated)
	require.Equal(t, 1, result.ActiveLease)
	require.Empty(t, repo.escalations)
	require.Equal(t, 1, cache.countCalls)
}

func TestRecoverStaleBillingIntentsEscalatesWithCutoffAndStateTokenOnlyAfterZeroLease(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	candidate := staleBillingIntentCandidate(now.Add(-2 * time.Minute))
	repo := &accountShareBillingRecoveryRepoStub{
		candidates: []AccountShareBillingIntentAttentionCandidate{candidate},
	}
	cache := &accountShareBillingRecoveryCacheStub{ttl: time.Minute}
	svc := accountShareBillingRecoveryTestService(repo, cache)
	guardCalls := 0

	result, err := svc.recoverStaleBillingIntentsOnce(
		context.Background(),
		now,
		func(context.Context) error {
			guardCalls++
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, result.Escalated)
	require.Len(t, repo.escalations, 1)
	require.Equal(t, candidate.ID, repo.escalations[0].ID)
	require.Equal(t, candidate.StateToken, repo.escalations[0].ExpectedStateToken)
	require.Equal(t, now.Add(-time.Minute), repo.escalations[0].StaleBefore)
	require.Equal(t, accountShareBillingRecoveryReasonCode, repo.escalations[0].ReasonCode)
	require.GreaterOrEqual(t, guardCalls, 3)
}

func TestRecoverStaleBillingIntentsCancelsStaleCreatedWithStateTokenCAS(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	candidate := staleBillingIntentCandidate(now.Add(-accountShareBillingCreatedRecoveryDelay - time.Second))
	candidate.Status = AccountShareBillingIntentStatusCreated
	candidate.ForwardStartedAt = nil
	repo := &accountShareBillingRecoveryRepoStub{
		candidates: []AccountShareBillingIntentAttentionCandidate{candidate},
	}
	cache := &accountShareBillingRecoveryCacheStub{ttl: time.Minute}
	svc := accountShareBillingRecoveryTestService(repo, cache)

	result, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)

	require.NoError(t, err)
	require.Equal(t, 1, result.CreatedCancelled)
	require.Empty(t, repo.escalations)
	require.Len(t, repo.cancellations, 1)
	require.Equal(t, candidate.ID, repo.cancellations[0].ID)
	require.Equal(t, candidate.StateToken, repo.cancellations[0].ExpectedStateToken)
	require.Zero(t, cache.countCalls, "created intents never started forwarding and need no runtime lease lookup")
}

func TestRecoverStaleBillingIntentsScansPastActiveLeasePage(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	first := staleBillingIntentCandidate(now.Add(-3 * time.Minute))
	first.ID = 100
	first.MembershipID = 1000
	second := staleBillingIntentCandidate(now.Add(-2 * time.Minute))
	second.ID = 200
	second.MembershipID = 2000
	repo := &accountShareBillingRecoveryRepoStub{
		pages: [][]AccountShareBillingIntentAttentionCandidate{
			{first},
			{second},
			nil,
		},
	}
	cache := &accountShareBillingRecoveryCacheStub{
		ttl: time.Minute,
		countByMembership: map[int64]int{
			first.MembershipID: 1,
		},
	}
	svc := accountShareBillingRecoveryTestService(repo, cache)

	result, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)

	require.NoError(t, err)
	require.Equal(t, 2, result.Examined)
	require.Equal(t, 1, result.ActiveLease)
	require.Equal(t, 1, result.Escalated)
	require.Len(t, repo.listInputs, 3)
	require.Nil(t, repo.listInputs[0].After)
	require.NotNil(t, repo.listInputs[1].After)
	require.Equal(t, first.ID, repo.listInputs[1].After.ID)
	require.Equal(t, first.UpdatedAt, repo.listInputs[1].After.UpdatedAt)
	require.Len(t, repo.escalations, 1)
	require.Equal(t, second.ID, repo.escalations[0].ID)
}

func TestRecoverStaleBillingIntentsContinuesFromPriorCursorAfterMaxPages(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	pages := make([][]AccountShareBillingIntentAttentionCandidate, 0, accountShareBillingRecoveryMaxPages+2)
	countByMembership := make(map[int64]int, accountShareBillingRecoveryMaxPages+1)
	for index := 0; index < accountShareBillingRecoveryMaxPages+1; index++ {
		candidate := staleBillingIntentCandidate(now.Add(-3*time.Minute + time.Duration(index)*time.Second))
		candidate.ID = int64(index + 1)
		candidate.MembershipID = int64(1000 + index)
		pages = append(pages, []AccountShareBillingIntentAttentionCandidate{candidate})
		countByMembership[candidate.MembershipID] = 1
	}
	pages = append(pages, nil)
	repo := &accountShareBillingRecoveryRepoStub{pages: pages}
	svc := accountShareBillingRecoveryTestService(repo, &accountShareBillingRecoveryCacheStub{
		ttl:               time.Minute,
		countByMembership: countByMembership,
	})

	first, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)
	require.NoError(t, err)
	require.Equal(t, accountShareBillingRecoveryMaxPages, first.ScanPages)
	require.Equal(t, accountShareBillingRecoveryMaxPages, first.Examined)

	second, err := svc.recoverStaleBillingIntentsOnce(context.Background(), now, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, second.ScanPages, 1)
	require.GreaterOrEqual(t, second.Examined, 1)
	require.GreaterOrEqual(t, len(repo.listInputs), accountShareBillingRecoveryMaxPages+2)
	require.NotNil(t, repo.listInputs[accountShareBillingRecoveryMaxPages].After)
	require.Equal(
		t,
		int64(accountShareBillingRecoveryMaxPages),
		repo.listInputs[accountShareBillingRecoveryMaxPages].After.ID,
	)
}

func TestRecoverStaleBillingIntentsFailsClosedWithoutRuntimeLeaseCapability(t *testing.T) {
	repo := &accountShareBillingRecoveryRepoStub{}
	svc := &AccountShareModeService{
		billingIntentRepository: repo,
		concurrencyService:      NewConcurrencyService(&accountShareBillingRecoveryUnsupportedCache{}),
	}

	result, err := svc.recoverStaleBillingIntentsOnce(
		context.Background(),
		time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC),
		nil,
	)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.Zero(t, result.Escalated)
	require.Zero(t, repo.listCalls)
	require.Empty(t, repo.escalations)
}

func TestAccountShareBillingAdminServiceRejectsNonAdminAndInvalidWaiver(t *testing.T) {
	repo := &accountShareBillingAdminRepoStub{}
	svc := &AccountShareModeService{billingIntentRepository: repo}

	_, _, err := svc.ListBillingIntentsNeedingAttention(
		context.Background(),
		42,
		false,
		pagination.PaginationParams{},
	)
	require.ErrorIs(t, err, ErrAccountShareBillingAdminRequired)
	_, err = svc.GetBillingIntentForAdmin(context.Background(), 42, false, 100)
	require.ErrorIs(t, err, ErrAccountShareBillingAdminRequired)
	_, err = svc.WaiveBillingIntentForAdmin(
		context.Background(),
		42,
		false,
		100,
		WaiveAccountShareBillingIntentInput{
			ExpectedStateToken: 4,
			Reason:             "not allowed",
			Confirmed:          true,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareBillingAdminRequired)

	_, err = svc.WaiveBillingIntentForAdmin(
		context.Background(),
		42,
		true,
		100,
		WaiveAccountShareBillingIntentInput{
			ExpectedStateToken: 4,
			Reason:             " ",
			Confirmed:          true,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareBillingWaiverReasonRequired)
	_, err = svc.WaiveBillingIntentForAdmin(
		context.Background(),
		42,
		true,
		100,
		WaiveAccountShareBillingIntentInput{
			ExpectedStateToken: 4,
			Reason:             "人工确认无法恢复",
			Confirmed:          false,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareBillingWaiverConfirmationRequired)
	require.Zero(t, repo.listCalls)
	require.Zero(t, repo.getCalls)
	require.Zero(t, repo.waiveCalls)
}

func TestAccountShareBillingAdminServiceListsAndWaivesUsingCAS(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	record := AccountShareBillingIntentAdminRecord{
		ID:           100,
		Status:       AccountShareBillingIntentStatusNeedsAttention,
		StateToken:   4,
		MembershipID: 11,
		ListingID:    12,
		CreatedAt:    now.Add(-time.Hour),
		UpdatedAt:    now,
	}
	repo := &accountShareBillingAdminRepoStub{
		items: []AccountShareBillingIntentAdminRecord{record},
		total: 1,
		waiverResult: &AccountShareBillingIntentWaiverResult{
			Intent: AccountShareBillingIntentAdminRecord{
				ID:           100,
				Status:       AccountShareBillingIntentStatusCancelled,
				StateToken:   5,
				MembershipID: 11,
				ListingID:    12,
			},
			Waiver: AccountShareBillingIntentAdminWaiver{
				ID:                  200,
				IntentID:            100,
				ActorUserIDSnapshot: 42,
				Reason:              "人工确认无法恢复",
				PreviousStatus:      AccountShareBillingIntentStatusNeedsAttention,
				ResultingStatus:     AccountShareBillingIntentStatusCancelled,
				PreviousStateToken:  4,
				ResultingStateToken: 5,
			},
		},
	}
	svc := &AccountShareModeService{billingIntentRepository: repo}

	items, page, err := svc.ListBillingIntentsNeedingAttention(
		context.Background(),
		42,
		true,
		pagination.PaginationParams{Page: 1, PageSize: 1000},
	)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, AccountShareBillingAdminMaxPageSize, page.PageSize)
	require.Equal(t, AccountShareBillingAdminMaxPageSize, repo.listLimit)

	result, err := svc.WaiveBillingIntentForAdmin(
		context.Background(),
		42,
		true,
		100,
		WaiveAccountShareBillingIntentInput{
			ExpectedStateToken: 4,
			Reason:             "  人工确认无法恢复  ",
			Confirmed:          true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, AccountShareBillingIntentStatusCancelled, result.Intent.Status)
	require.Equal(t, int64(4), repo.waiveInput.ExpectedStateToken)
	require.Equal(t, int64(42), repo.waiveInput.ActorUserID)
	require.Equal(t, "人工确认无法恢复", repo.waiveInput.Reason)
}

func TestAccountShareBillingAdminServiceMapsCASConflict(t *testing.T) {
	repo := &accountShareBillingAdminRepoStub{
		waiveErr: ErrAccountShareBillingIntentStateConflict,
	}
	svc := &AccountShareModeService{billingIntentRepository: repo}

	_, err := svc.WaiveBillingIntentForAdmin(
		context.Background(),
		42,
		true,
		100,
		WaiveAccountShareBillingIntentInput{
			ExpectedStateToken: 4,
			Reason:             "人工确认无法恢复",
			Confirmed:          true,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareBillingAdminConflict)
}

func staleBillingIntentCandidate(updatedAt time.Time) AccountShareBillingIntentAttentionCandidate {
	return AccountShareBillingIntentAttentionCandidate{
		AccountShareBillingIntentState: AccountShareBillingIntentState{
			ID:           100,
			MembershipID: 11,
			Status:       AccountShareBillingIntentStatusInFlight,
			StateToken:   4,
			UpdatedAt:    updatedAt,
		},
	}
}

func accountShareBillingRecoveryTestService(
	repo *accountShareBillingRecoveryRepoStub,
	cache ConcurrencyCache,
) *AccountShareModeService {
	return &AccountShareModeService{
		billingIntentRepository: repo,
		concurrencyService:      NewConcurrencyService(cache),
	}
}

type accountShareBillingRecoveryRepoStub struct {
	AccountShareBillingIntentRepository
	candidates    []AccountShareBillingIntentAttentionCandidate
	pages         [][]AccountShareBillingIntentAttentionCandidate
	listErr       error
	escalateErr   error
	cancelErr     error
	listCalls     int
	listInputs    []ListAccountShareBillingRecoveryCandidatesInput
	escalations   []EscalateAccountShareBillingIntentInput
	cancellations []AccountShareBillingIntentTransition
}

func (s *accountShareBillingRecoveryRepoStub) ListRecoveryCandidates(
	_ context.Context,
	input ListAccountShareBillingRecoveryCandidatesInput,
) ([]AccountShareBillingIntentAttentionCandidate, error) {
	s.listInputs = append(s.listInputs, input)
	callIndex := s.listCalls
	s.listCalls++
	if callIndex < len(s.pages) {
		return append([]AccountShareBillingIntentAttentionCandidate(nil), s.pages[callIndex]...), s.listErr
	}
	if callIndex > 0 {
		return nil, s.listErr
	}
	return append([]AccountShareBillingIntentAttentionCandidate(nil), s.candidates...), s.listErr
}

func (s *accountShareBillingRecoveryRepoStub) CancelCreated(
	_ context.Context,
	input AccountShareBillingIntentTransition,
	_ string,
	_ string,
) (*AccountShareBillingIntentState, error) {
	s.cancellations = append(s.cancellations, input)
	if s.cancelErr != nil {
		return nil, s.cancelErr
	}
	return &AccountShareBillingIntentState{
		ID:         input.ID,
		Status:     AccountShareBillingIntentStatusCancelled,
		StateToken: input.ExpectedStateToken + 1,
	}, nil
}

func (s *accountShareBillingRecoveryRepoStub) EscalateStaleToNeedsAttention(
	_ context.Context,
	input EscalateAccountShareBillingIntentInput,
) (*AccountShareBillingIntentState, error) {
	s.escalations = append(s.escalations, input)
	if s.escalateErr != nil {
		return nil, s.escalateErr
	}
	return &AccountShareBillingIntentState{
		ID:         input.ID,
		Status:     AccountShareBillingIntentStatusNeedsAttention,
		StateToken: input.ExpectedStateToken + 1,
	}, nil
}

type accountShareBillingRecoveryCacheStub struct {
	ConcurrencyCache
	ttl               time.Duration
	count             int
	countByMembership map[int64]int
	countErr          error
	countCalls        int
}

func (s *accountShareBillingRecoveryCacheStub) AcquireAccountShareMembershipSlot(
	context.Context,
	int64,
	int,
	string,
) (bool, error) {
	return true, nil
}

func (s *accountShareBillingRecoveryCacheStub) ReleaseAccountShareMembershipSlot(
	context.Context,
	int64,
	string,
) error {
	return nil
}

func (s *accountShareBillingRecoveryCacheStub) GetAccountShareMembershipConcurrency(
	_ context.Context,
	membershipID int64,
) (int, error) {
	s.countCalls++
	if s.countByMembership != nil {
		return s.countByMembership[membershipID], s.countErr
	}
	return s.count, s.countErr
}

func (s *accountShareBillingRecoveryCacheStub) RefreshAccountSlot(
	context.Context,
	int64,
	string,
) (bool, error) {
	return true, nil
}

func (s *accountShareBillingRecoveryCacheStub) RefreshAccountShareMembershipSlot(
	context.Context,
	int64,
	string,
) (bool, error) {
	return true, nil
}

func (s *accountShareBillingRecoveryCacheStub) SlotLeaseTTL() time.Duration {
	return s.ttl
}

type accountShareBillingRecoveryUnsupportedCache struct {
	ConcurrencyCache
}

type accountShareBillingAdminRepoStub struct {
	AccountShareBillingIntentRepository
	items        []AccountShareBillingIntentAdminRecord
	total        int64
	listErr      error
	getResult    *AccountShareBillingIntentAdminRecord
	getErr       error
	waiverResult *AccountShareBillingIntentWaiverResult
	waiveErr     error
	listCalls    int
	listOffset   int
	listLimit    int
	getCalls     int
	waiveCalls   int
	waiveInput   WaiveAccountShareBillingIntentRepositoryInput
}

func (s *accountShareBillingAdminRepoStub) ListNeedsAttentionForAdmin(
	_ context.Context,
	offset int,
	limit int,
) ([]AccountShareBillingIntentAdminRecord, int64, error) {
	s.listCalls++
	s.listOffset = offset
	s.listLimit = limit
	return append([]AccountShareBillingIntentAdminRecord(nil), s.items...), s.total, s.listErr
}

func (s *accountShareBillingAdminRepoStub) GetForAdmin(
	context.Context,
	int64,
) (*AccountShareBillingIntentAdminRecord, error) {
	s.getCalls++
	return s.getResult, s.getErr
}

func (s *accountShareBillingAdminRepoStub) WaiveNeedsAttention(
	_ context.Context,
	input WaiveAccountShareBillingIntentRepositoryInput,
) (*AccountShareBillingIntentWaiverResult, error) {
	s.waiveCalls++
	s.waiveInput = input
	return s.waiverResult, s.waiveErr
}
