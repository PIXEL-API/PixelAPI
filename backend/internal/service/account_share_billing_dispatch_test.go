package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyUsageBillingPersistsDurableAccountShareReadyIntentWithoutDirectBilling(t *testing.T) {
	intentRepo := &accountShareBillingDispatchIntentRepoStub{}
	directRepo := &accountShareBillingDispatchUsageRepoStub{}
	barrier := newAccountShareBillingReleaseBarrier(time.Second)
	dispatch := &AccountShareBillingDispatch{
		repository: intentRepo,
		barrier:    barrier,
		command: AccountShareBillingCommand{
			SchemaVersion:     AccountShareBillingCommandSchemaV3,
			RateMultiplier:    "1.25",
			SettlementEnabled: true,
		},
		state: AccountShareBillingIntentState{
			ID:           91,
			Status:       AccountShareBillingIntentStatusInFlight,
			StateToken:   2,
			APIKeyID:     31,
			MembershipID: 41,
		},
	}
	ctx := context.WithValue(
		context.Background(),
		accountShareModeRequestContextKey{},
		AccountShareModeRequestContext{
			UserID:   21,
			APIKeyID: 31,
			state: &accountShareModeRequestState{
				userID:          21,
				apiKeyID:        31,
				billingDispatch: dispatch,
			},
		},
	)
	usageLog := &UsageLog{
		UserID:         21,
		APIKeyID:       31,
		AccountID:      51,
		RequestID:      "provider-request-1",
		Model:          "gpt-5.4",
		ActualCost:     0.75,
		TotalCost:      0.6,
		RateMultiplier: 1.25,
		RequestType:    RequestTypeStream,
		Stream:         true,
		CreatedAt:      time.Now().UTC(),
	}
	handled, err := applyUsageBilling(
		ctx,
		usageLog.RequestID,
		usageLog,
		&postUsageBillingParams{
			Cost:    &CostBreakdown{ActualCost: 0.75, TotalCost: 0.6},
			User:    &User{ID: 21},
			APIKey:  &APIKey{ID: 31},
			Account: &Account{ID: 51, Type: AccountTypeOAuth},
		},
		&billingDeps{},
		directRepo,
	)
	require.NoError(t, err)
	require.True(t, handled)
	require.Zero(t, directRepo.applyCalls)
	require.Len(t, intentRepo.ready, 1)
	require.Equal(t, int64(91), intentRepo.ready[0].ID)
	require.Equal(t, int64(2), intentRepo.ready[0].ExpectedStateToken)
	require.Equal(t, "0.75", intentRepo.ready[0].Usage.BaseCharge)
	require.Equal(t, "0", intentRepo.ready[0].Usage.HourlyCharge)
	require.Equal(t, "0.75", intentRepo.ready[0].Usage.TotalCharge)
	require.True(t, intentRepo.ready[0].ResponseSummary.Streamed)
	require.True(t, barrier.completed())

	rateMultiplier, ok, err := accountShareBillingRateMultiplierFromContext(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.InDelta(t, 1.25, rateMultiplier, 1e-12)
}

func TestAccountShareBillingDispatchKeepsRuntimeLeaseUntilTransientReadyWriteRecovers(t *testing.T) {
	intentRepo := &recoveringAccountShareBillingDispatchIntentRepoStub{}
	barrier := newAccountShareBillingReleaseBarrier(20 * time.Millisecond)
	var releases atomic.Int32
	newSlot := func() *AcquireResult {
		return &AcquireResult{
			Acquired: true,
			ReleaseFunc: func() {
				releases.Add(1)
			},
			RefreshFunc: func(context.Context) (bool, error) {
				return true, nil
			},
			LeaseTTL: time.Second,
		}
	}
	lease, err := NewAccountShareRuntimeLease(context.Background(), newSlot(), newSlot())
	require.NoError(t, err)

	dispatch := &AccountShareBillingDispatch{
		repository:  intentRepo,
		barrier:     barrier,
		recoveryCtx: lease.Context(),
		command: AccountShareBillingCommand{
			RoutedModel:    "gpt-5.4",
			RateMultiplier: "1",
		},
		state: AccountShareBillingIntentState{
			ID:           91,
			Status:       AccountShareBillingIntentStatusInFlight,
			StateToken:   2,
			APIKeyID:     31,
			MembershipID: 41,
		},
	}
	require.NoError(t, lease.setAccountShareBillingBarrier(barrier))

	billingCtx, cancelBilling := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err = dispatch.markReadyWithoutUsage(billingCtx, AccountShareBillingResponseSummaryV1{
		SchemaVersion: AccountShareBillingResponseSchemaV1,
	})
	cancelBilling()
	require.Error(t, err)
	require.True(t, barrier.recovering())

	lease.Release()
	time.Sleep(100 * time.Millisecond)
	require.Zero(t, releases.Load(), "runtime slots were released before the ready payload became durable")

	intentRepo.available.Store(true)
	require.Eventually(t, func() bool {
		return releases.Load() == 2
	}, 3*time.Second, 10*time.Millisecond)
	require.True(t, barrier.completed())
	require.GreaterOrEqual(t, intentRepo.calls.Load(), int32(2))
}

type accountShareBillingDispatchIntentRepoStub struct {
	ready []MarkAccountShareBillingIntentReadyInput
}

func (s *accountShareBillingDispatchIntentRepoStub) CreatePrepared(context.Context, CreateAccountShareBillingIntentInput) (*AccountShareBillingIntentState, bool, error) {
	return nil, false, errors.New("unexpected CreatePrepared")
}

func (s *accountShareBillingDispatchIntentRepoStub) MarkInFlight(context.Context, AccountShareBillingIntentTransition) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("unexpected MarkInFlight")
}

func (s *accountShareBillingDispatchIntentRepoStub) MarkReady(_ context.Context, input MarkAccountShareBillingIntentReadyInput) (*AccountShareBillingIntentState, error) {
	s.ready = append(s.ready, input)
	return &AccountShareBillingIntentState{
		ID:         input.ID,
		Status:     AccountShareBillingIntentStatusReady,
		StateToken: input.ExpectedStateToken + 1,
		APIKeyID:   31,
	}, nil
}

func (s *accountShareBillingDispatchIntentRepoStub) CancelCreated(context.Context, AccountShareBillingIntentTransition, string, string) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("unexpected CancelCreated")
}

func (s *accountShareBillingDispatchIntentRepoStub) ClaimReady(context.Context, ClaimAccountShareBillingIntentsInput) ([]AccountShareBillingIntentWorkItem, error) {
	return nil, errors.New("unexpected ClaimReady")
}

func (s *accountShareBillingDispatchIntentRepoStub) RenewProcessingLease(context.Context, AccountShareBillingIntentLeaseTransition, time.Duration) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("unexpected RenewProcessingLease")
}

func (s *accountShareBillingDispatchIntentRepoStub) MarkSettled(context.Context, MarkAccountShareBillingIntentSettledInput) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("unexpected MarkSettled")
}

func (s *accountShareBillingDispatchIntentRepoStub) MarkFailed(context.Context, MarkAccountShareBillingIntentFailedInput) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("unexpected MarkFailed")
}

func (s *accountShareBillingDispatchIntentRepoStub) EscalateStaleToNeedsAttention(context.Context, EscalateAccountShareBillingIntentInput) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("unexpected EscalateStaleToNeedsAttention")
}

func (s *accountShareBillingDispatchIntentRepoStub) CountPendingByMembership(context.Context, int64) (int64, error) {
	return 0, errors.New("unexpected CountPendingByMembership")
}

func (s *accountShareBillingDispatchIntentRepoStub) ListStaleForAttention(context.Context, time.Time, int) ([]AccountShareBillingIntentAttentionCandidate, error) {
	return nil, errors.New("unexpected ListStaleForAttention")
}

type recoveringAccountShareBillingDispatchIntentRepoStub struct {
	accountShareBillingDispatchIntentRepoStub
	available atomic.Bool
	calls     atomic.Int32
}

func (s *recoveringAccountShareBillingDispatchIntentRepoStub) MarkReady(
	_ context.Context,
	input MarkAccountShareBillingIntentReadyInput,
) (*AccountShareBillingIntentState, error) {
	s.calls.Add(1)
	if !s.available.Load() {
		return nil, errors.New("database temporarily unavailable")
	}
	return &AccountShareBillingIntentState{
		ID:           input.ID,
		Status:       AccountShareBillingIntentStatusReady,
		StateToken:   input.ExpectedStateToken + 1,
		APIKeyID:     31,
		MembershipID: 41,
	}, nil
}

type accountShareBillingDispatchUsageRepoStub struct {
	applyCalls int
}

func (s *accountShareBillingDispatchUsageRepoStub) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.applyCalls++
	return nil, errors.New("direct billing must not run for durable account-share dispatch")
}
