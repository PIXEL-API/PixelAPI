package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildAccountShareUsageBillingCommandReconstructsCompleteSnapshot(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")

	command, err := BuildAccountShareUsageBillingCommand(item)
	require.NoError(t, err)
	require.Equal(t, item.RequestID, command.RequestID)
	require.Equal(t, item.APIKeyID, command.APIKeyID)
	require.Len(t, command.RequestFingerprint, 64)
	require.Equal(t, item.Command.RequestPayloadHash, command.RequestPayloadHash)
	require.Equal(t, item.ConsumerUserID, command.UserID)
	require.Equal(t, item.AccountID, command.AccountID)
	require.InDelta(t, 0.25, command.BalanceCost, 1e-12)
	require.True(t, command.PreferPointsBilling)
	require.True(t, command.ShareSnapshotCaptured)
	require.Equal(t, AccountShareModePrivate, command.ShareModeSnapshot)
	require.Equal(t, AccountShareStatusApproved, command.ShareStatusSnapshot)

	require.NotNil(t, command.AccountShareModeSettlement)
	require.Equal(t, item.MembershipID, command.AccountShareModeSettlement.MembershipID)
	require.Equal(t, item.ListingID, command.AccountShareModeSettlement.ListingID)
	require.InDelta(t, 0.25, command.AccountShareModeSettlement.BaseCharge, 1e-12)
	require.InDelta(t, 0.5, command.AccountShareModeSettlement.HourlyRate, 1e-12)
	require.InDelta(t, 0.7, command.AccountShareModeSettlement.OwnerShareRatio, 1e-12)

	require.NotNil(t, command.UsageLog)
	require.Equal(t, "gpt-5.6", command.UsageLog.Model)
	require.Equal(t, item.Command.RequestedModel, command.UsageLog.RequestedModel)
	require.Equal(t, RequestTypeStream, command.UsageLog.RequestType)
	require.True(t, command.UsageLog.Stream)
	require.False(t, command.UsageLog.OpenAIWSMode)
	require.Equal(t, 123, command.UsageLog.InputTokens)
	require.Equal(t, 45, command.UsageLog.OutputTokens)
	require.InDelta(t, 0.2, command.UsageLog.InputCost, 1e-12)
	require.InDelta(t, 0.05, command.UsageLog.OutputCost, 1e-12)
	require.NotNil(t, command.UsageLog.AccountStatsCost)
	require.InDelta(t, 0.25, *command.UsageLog.AccountStatsCost, 1e-12)
	require.Nil(t, command.UsageLog.UserAgent)
	require.Nil(t, command.UsageLog.IPAddress)
}

func TestBuildAccountShareUsageBillingCommandPreservesHistoricalV2WireContract(t *testing.T) {
	item := accountShareBillingWorkerHistoricalV2TestItem(t, "worker-a")

	command, err := BuildAccountShareUsageBillingCommand(item)

	require.NoError(t, err)
	require.Equal(t, AccountShareBillingCommandSchemaV2, item.Command.SchemaVersion)
	require.True(t, item.Command.SettlementEnabled)
	require.NotNil(t, command.AccountShareModeSettlement)
	require.Equal(t, item.MembershipID, command.AccountShareModeSettlement.MembershipID)
}

func TestAccountShareBillingWorkerRunOnceSettlesHistoricalV2WireCommand(t *testing.T) {
	item := accountShareBillingWorkerHistoricalV2TestItem(t, "worker-a")
	intentRepo := &accountShareBillingWorkerIntentRepoStub{
		claimItems: []AccountShareBillingIntentWorkItem{item},
	}
	billingRepo := &accountShareBillingWorkerUsageRepoStub{
		apply: func(_ context.Context, command *UsageBillingCommand) (*UsageBillingApplyResult, error) {
			require.NotNil(t, command.AccountShareModeSettlement)
			require.Equal(t, item.MembershipID, command.AccountShareModeSettlement.MembershipID)
			return &UsageBillingApplyResult{Applied: true}, nil
		},
	}
	worker := newAccountShareBillingWorkerForTest(t, intentRepo, billingRepo, "worker-a")

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{Claimed: 1, Settled: 1}, result)
	require.Len(t, intentRepo.settled, 1)
	require.Empty(t, intentRepo.failed)
}

func TestBuildAccountShareUsageBillingCommandRejectsTamperedSnapshot(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")
	item.Usage.InputTokens++

	_, err := BuildAccountShareUsageBillingCommand(item)
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
	require.Contains(t, err.Error(), "hash mismatch")
}

func TestAccountShareBillingWorkerSettlesReplayWithExistingUsageLogID(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")
	usageLogID := int64(91)
	intentRepo := &accountShareBillingWorkerIntentRepoStub{claimItems: []AccountShareBillingIntentWorkItem{item}}
	billingRepo := &accountShareBillingWorkerUsageRepoStub{
		apply: func(_ context.Context, command *UsageBillingCommand) (*UsageBillingApplyResult, error) {
			require.Equal(t, item.RequestID, command.RequestID)
			return &UsageBillingApplyResult{Applied: false, UsageLogID: &usageLogID}, nil
		},
	}
	worker := newAccountShareBillingWorkerForTest(t, intentRepo, billingRepo, "worker-a")

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{Claimed: 1, Settled: 1}, result)
	require.Len(t, intentRepo.settled, 1)
	require.Equal(t, item.StateToken, intentRepo.settled[0].ExpectedStateToken)
	require.Equal(t, item.LeaseToken, intentRepo.settled[0].LeaseToken)
	require.Equal(t, "worker-a", intentRepo.settled[0].WorkerID)
	require.NotNil(t, intentRepo.settled[0].UsageLogID)
	require.Equal(t, usageLogID, *intentRepo.settled[0].UsageLogID)
	require.Empty(t, intentRepo.failed)
}

func TestAccountShareBillingWorkerSchedulesSanitizedRetryForTemporaryFailure(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")
	item.AttemptCount = 2
	intentRepo := &accountShareBillingWorkerIntentRepoStub{claimItems: []AccountShareBillingIntentWorkItem{item}}
	billingRepo := &accountShareBillingWorkerUsageRepoStub{
		apply: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
			return nil, errors.New("dial tcp secret.internal:5432: connection reset")
		},
	}
	worker := newAccountShareBillingWorkerForTest(t, intentRepo, billingRepo, "worker-a")
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{Claimed: 1, RetryScheduled: 1}, result)
	require.Len(t, intentRepo.failed, 1)
	failure := intentRepo.failed[0]
	require.False(t, failure.NeedsAttention)
	require.Equal(t, "billing_apply_temporary", failure.ErrorCode)
	require.NotContains(t, failure.ErrorMessage, "secret.internal")
	require.NotNil(t, failure.RetryAt)
	require.Equal(t, now.Add(10*time.Second), *failure.RetryAt)
}

func TestAccountShareBillingWorkerRetriesWhenPostCommitFinalizationFails(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")
	intentRepo := &accountShareBillingWorkerIntentRepoStub{
		claimItems: []AccountShareBillingIntentWorkItem{item},
	}
	billingRepo := &accountShareBillingWorkerUsageRepoStub{
		apply: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
			return &UsageBillingApplyResult{Applied: true}, nil
		},
	}
	finalizeErr := errors.New("cache invalidation failed")
	worker, err := NewAccountShareBillingWorker(
		intentRepo,
		billingRepo,
		accountShareBillingWorkerFinalizerStub{
			finalize: func(context.Context, *UsageBillingCommand, *UsageBillingApplyResult) error {
				return finalizeErr
			},
		},
		AccountShareBillingWorkerConfig{
			WorkerID:      "worker-a",
			BatchSize:     10,
			LeaseDuration: 6 * time.Second,
			MaxAttempts:   4,
			RetryBase:     5 * time.Second,
			RetryMax:      time.Minute,
		},
	)
	require.NoError(t, err)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{Claimed: 1, RetryScheduled: 1}, result)
	require.Empty(t, intentRepo.settled)
	require.Len(t, intentRepo.failed, 1)
	require.Equal(t, "billing_apply_temporary", intentRepo.failed[0].ErrorCode)
}

func TestAccountShareBillingWorkerEscalatesFingerprintConflict(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")
	intentRepo := &accountShareBillingWorkerIntentRepoStub{claimItems: []AccountShareBillingIntentWorkItem{item}}
	billingRepo := &accountShareBillingWorkerUsageRepoStub{
		apply: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
			return nil, ErrUsageBillingRequestConflict
		},
	}
	worker := newAccountShareBillingWorkerForTest(t, intentRepo, billingRepo, "worker-a")

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{Claimed: 1, NeedsAttention: 1}, result)
	require.Len(t, intentRepo.failed, 1)
	require.True(t, intentRepo.failed[0].NeedsAttention)
	require.Nil(t, intentRepo.failed[0].RetryAt)
	require.Equal(t, "billing_fingerprint_conflict", intentRepo.failed[0].ErrorCode)
}

func TestAccountShareBillingWorkerDoesNotDowngradeAfterApplyWhenMarkSettledLosesLease(t *testing.T) {
	item := accountShareBillingWorkerTestItem(t, "worker-a")
	intentRepo := &accountShareBillingWorkerIntentRepoStub{
		claimItems:     []AccountShareBillingIntentWorkItem{item},
		markSettledErr: ErrAccountShareBillingIntentLeaseLost,
	}
	billingRepo := &accountShareBillingWorkerUsageRepoStub{
		apply: func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
			return &UsageBillingApplyResult{Applied: true}, nil
		},
	}
	worker := newAccountShareBillingWorkerForTest(t, intentRepo, billingRepo, "worker-a")

	result, err := worker.RunOnce(context.Background())
	require.ErrorIs(t, err, ErrAccountShareBillingIntentLeaseLost)
	require.Equal(t, AccountShareBillingWorkerRunResult{Claimed: 1}, result)
	require.Empty(t, intentRepo.failed)
}

func TestAccountShareBillingWorkerRunUntilDrainedAggregatesFullAndPartialBatches(t *testing.T) {
	intentRepo := &accountShareBillingWorkerIntentRepoStub{
		claimBatches: [][]AccountShareBillingIntentWorkItem{
			accountShareBillingWorkerTestBatch(t, "worker-a", 2, 100),
			accountShareBillingWorkerTestBatch(t, "worker-a", 2, 200),
			accountShareBillingWorkerTestBatch(t, "worker-a", 2, 300),
			accountShareBillingWorkerTestBatch(t, "worker-a", 1, 400),
		},
	}
	worker := newAccountShareBillingWorkerForTest(
		t,
		intentRepo,
		&accountShareBillingWorkerUsageRepoStub{},
		"worker-a",
	)
	worker.config.BatchSize = 2
	beforeBatchCalls := 0

	result, err := worker.RunUntilDrained(context.Background(), time.Minute, func(context.Context) error {
		beforeBatchCalls++
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{
		Batches: 4,
		Claimed: 7,
		Settled: 7,
	}, result)
	require.Equal(t, 4, intentRepo.claimCalls)
	require.Equal(t, 4, beforeBatchCalls)
}

func TestAccountShareBillingWorkerRunUntilDrainedStopsBeforeClaimWhenBudgetExhausted(t *testing.T) {
	intentRepo := &accountShareBillingWorkerIntentRepoStub{
		claimBatches: [][]AccountShareBillingIntentWorkItem{
			accountShareBillingWorkerTestBatch(t, "worker-a", 2, 100),
			accountShareBillingWorkerTestBatch(t, "worker-a", 2, 200),
		},
	}
	worker := newAccountShareBillingWorkerForTest(
		t,
		intentRepo,
		&accountShareBillingWorkerUsageRepoStub{},
		"worker-a",
	)
	worker.config.BatchSize = 2
	startedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	nowCalls := 0
	worker.now = func() time.Time {
		nowCalls++
		if nowCalls == 1 {
			return startedAt
		}
		return startedAt.Add(5 * time.Second)
	}
	beforeBatchCalls := 0

	result, err := worker.RunUntilDrained(context.Background(), 5*time.Second, func(context.Context) error {
		beforeBatchCalls++
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{
		Batches:         1,
		Claimed:         2,
		Settled:         2,
		BudgetExhausted: true,
		BacklogLikely:   true,
	}, result)
	require.Equal(t, 1, intentRepo.claimCalls)
	require.Equal(t, 1, beforeBatchCalls)
}

func TestAccountShareBillingWorkerRunUntilDrainedStopsOnBeforeBatchAndRunOnceError(t *testing.T) {
	t.Run("before batch", func(t *testing.T) {
		intentRepo := &accountShareBillingWorkerIntentRepoStub{
			claimItems: accountShareBillingWorkerTestBatch(t, "worker-a", 1, 100),
		}
		worker := newAccountShareBillingWorkerForTest(
			t,
			intentRepo,
			&accountShareBillingWorkerUsageRepoStub{},
			"worker-a",
		)
		beforeBatchErr := errors.New("lease guard failed")

		result, err := worker.RunUntilDrained(context.Background(), time.Minute, func(context.Context) error {
			return beforeBatchErr
		})

		require.ErrorIs(t, err, beforeBatchErr)
		require.Equal(t, AccountShareBillingWorkerRunResult{}, result)
		require.Zero(t, intentRepo.claimCalls)
	})

	t.Run("run once", func(t *testing.T) {
		runOnceErr := errors.New("claim failed")
		intentRepo := &accountShareBillingWorkerIntentRepoStub{
			claimErrors: []error{runOnceErr},
		}
		worker := newAccountShareBillingWorkerForTest(
			t,
			intentRepo,
			&accountShareBillingWorkerUsageRepoStub{},
			"worker-a",
		)
		beforeBatchCalls := 0

		result, err := worker.RunUntilDrained(context.Background(), time.Minute, func(context.Context) error {
			beforeBatchCalls++
			return nil
		})

		require.ErrorIs(t, err, runOnceErr)
		require.Equal(t, AccountShareBillingWorkerRunResult{Batches: 1}, result)
		require.Equal(t, 1, intentRepo.claimCalls)
		require.Equal(t, 1, beforeBatchCalls)
	})
}

func TestAccountShareBillingWorkerRunUntilDrainedStopsAtMaximumBatches(t *testing.T) {
	claimBatches := make([][]AccountShareBillingIntentWorkItem, AccountShareBillingWorkerMaxDrainBatches+1)
	for i := range claimBatches {
		claimBatches[i] = accountShareBillingWorkerTestBatch(t, "worker-a", 1, int64(100+i))
	}
	intentRepo := &accountShareBillingWorkerIntentRepoStub{claimBatches: claimBatches}
	worker := newAccountShareBillingWorkerForTest(
		t,
		intentRepo,
		&accountShareBillingWorkerUsageRepoStub{},
		"worker-a",
	)
	worker.config.BatchSize = 1
	beforeBatchCalls := 0

	result, err := worker.RunUntilDrained(context.Background(), time.Hour, func(context.Context) error {
		beforeBatchCalls++
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, AccountShareBillingWorkerRunResult{
		Batches:       AccountShareBillingWorkerMaxDrainBatches,
		Claimed:       AccountShareBillingWorkerMaxDrainBatches,
		Settled:       AccountShareBillingWorkerMaxDrainBatches,
		BacklogLikely: true,
	}, result)
	require.Equal(t, AccountShareBillingWorkerMaxDrainBatches, intentRepo.claimCalls)
	require.Equal(t, AccountShareBillingWorkerMaxDrainBatches, beforeBatchCalls)
}

func newAccountShareBillingWorkerForTest(
	t *testing.T,
	intentRepository AccountShareBillingIntentRepository,
	billingRepository UsageBillingRepository,
	workerID string,
) *AccountShareBillingWorker {
	t.Helper()
	worker, err := NewAccountShareBillingWorker(intentRepository, billingRepository, accountShareBillingWorkerFinalizerStub{}, AccountShareBillingWorkerConfig{
		WorkerID:      workerID,
		BatchSize:     10,
		LeaseDuration: 6 * time.Second,
		MaxAttempts:   4,
		RetryBase:     5 * time.Second,
		RetryMax:      time.Minute,
	})
	require.NoError(t, err)
	return worker
}

type accountShareBillingWorkerFinalizerStub struct {
	finalize func(context.Context, *UsageBillingCommand, *UsageBillingApplyResult) error
}

func (s accountShareBillingWorkerFinalizerStub) Finalize(
	ctx context.Context,
	command *UsageBillingCommand,
	result *UsageBillingApplyResult,
) error {
	if s.finalize == nil {
		return nil
	}
	return s.finalize(ctx, command, result)
}

func accountShareBillingWorkerTestItem(t *testing.T, workerID string) AccountShareBillingIntentWorkItem {
	t.Helper()
	prepared, err := PrepareAccountShareBillingIntent(validAccountShareBillingIntentInput())
	require.NoError(t, err)
	readyInput := validAccountShareBillingReadyInput()
	accountStatsCost := "0.25"
	readyInput.Usage.AccountStatsCost = &accountStatsCost
	readyInput.Usage.ServiceTier = "priority"
	readyInput.Usage.ReasoningEffort = "high"
	readyInput.Usage.BillingMode = "token"
	preparedReady, err := PrepareAccountShareBillingIntentReady(readyInput)
	require.NoError(t, err)
	usage, err := DecodeAccountShareBillingUsage(preparedReady.UsageJSON, preparedReady.UsageHash)
	require.NoError(t, err)

	return AccountShareBillingIntentWorkItem{
		AccountShareBillingIntentState: AccountShareBillingIntentState{
			ID:              100,
			RequestID:       prepared.RequestID,
			ClientRequestID: prepared.ClientRequestID,
			DispatchID:      prepared.DispatchID,
			AttemptNo:       prepared.AttemptNo,
			APIKeyID:        prepared.APIKeyID,
			MembershipID:    prepared.MembershipID,
			Status:          AccountShareBillingIntentStatusProcessing,
			StateToken:      4,
			AttemptCount:    1,
			LeaseToken:      2,
			LeaseOwner:      workerID,
		},
		ListingID:           prepared.ListingID,
		AccountID:           prepared.AccountID,
		BindingID:           prepared.BindingID,
		ListingRevisionID:   prepared.ListingRevisionID,
		TermsRevisionNumber: prepared.TermsRevisionNumber,
		ActorUserID:         prepared.ActorUserID,
		ActorRole:           prepared.ActorRole,
		ConsumerUserID:      prepared.ConsumerUserID,
		OwnerUserID:         prepared.OwnerUserID,
		Command:             prepared.Command,
		CommandHash:         prepared.CommandHash,
		RequestFingerprint:  prepared.RequestFingerprint,
		Usage:               usage,
		UsageHash:           preparedReady.UsageHash,
		ResponseSummary:     readyInput.ResponseSummary,
	}
}

func accountShareBillingWorkerHistoricalV2TestItem(t *testing.T, workerID string) AccountShareBillingIntentWorkItem {
	t.Helper()
	item := accountShareBillingWorkerTestItem(t, workerID)
	legacyCommand := item.Command
	legacyCommand.SchemaVersion = AccountShareBillingCommandSchemaV2
	legacyPayload, err := json.Marshal(accountShareBillingCommandV2WireFromRuntime(legacyCommand))
	require.NoError(t, err)
	legacyHash := hashAccountShareBillingPayload(legacyPayload)
	decoded, err := DecodeAccountShareBillingCommand(legacyPayload, legacyHash)
	require.NoError(t, err)

	item.Command = decoded
	item.CommandHash = legacyHash
	item.RequestFingerprint, err = buildAccountShareBillingRequestFingerprint(CreateAccountShareBillingIntentInput{
		RequestID:           item.RequestID,
		ClientRequestID:     item.ClientRequestID,
		DispatchID:          item.DispatchID,
		AttemptNo:           item.AttemptNo,
		APIKeyID:            item.APIKeyID,
		MembershipID:        item.MembershipID,
		ListingID:           item.ListingID,
		AccountID:           item.AccountID,
		BindingID:           item.BindingID,
		ListingRevisionID:   item.ListingRevisionID,
		TermsRevisionNumber: item.TermsRevisionNumber,
		ActorUserID:         item.ActorUserID,
		ActorRole:           item.ActorRole,
		ConsumerUserID:      item.ConsumerUserID,
		OwnerUserID:         item.OwnerUserID,
	}, legacyHash)
	require.NoError(t, err)
	return item
}

func accountShareBillingWorkerTestBatch(
	t *testing.T,
	workerID string,
	count int,
	firstID int64,
) []AccountShareBillingIntentWorkItem {
	t.Helper()
	items := make([]AccountShareBillingIntentWorkItem, count)
	for i := range items {
		items[i] = accountShareBillingWorkerTestItem(t, workerID)
		items[i].ID = firstID + int64(i)
	}
	return items
}

type accountShareBillingWorkerUsageRepoStub struct {
	apply func(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error)
}

func (s *accountShareBillingWorkerUsageRepoStub) Apply(ctx context.Context, command *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	if s.apply == nil {
		return &UsageBillingApplyResult{}, nil
	}
	return s.apply(ctx, command)
}

type accountShareBillingWorkerIntentRepoStub struct {
	claimItems     []AccountShareBillingIntentWorkItem
	claimBatches   [][]AccountShareBillingIntentWorkItem
	claimErr       error
	claimErrors    []error
	claimCalls     int
	onClaim        func(int, ClaimAccountShareBillingIntentsInput)
	markSettledErr error
	markFailedErr  error
	settled        []MarkAccountShareBillingIntentSettledInput
	failed         []MarkAccountShareBillingIntentFailedInput
	renewed        []AccountShareBillingIntentLeaseTransition
}

func (s *accountShareBillingWorkerIntentRepoStub) CreatePrepared(context.Context, CreateAccountShareBillingIntentInput) (*AccountShareBillingIntentState, bool, error) {
	return nil, false, errors.New("not implemented")
}

func (s *accountShareBillingWorkerIntentRepoStub) MarkInFlight(context.Context, AccountShareBillingIntentTransition) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("not implemented")
}

func (s *accountShareBillingWorkerIntentRepoStub) MarkReady(context.Context, MarkAccountShareBillingIntentReadyInput) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("not implemented")
}

func (s *accountShareBillingWorkerIntentRepoStub) CancelCreated(context.Context, AccountShareBillingIntentTransition, string, string) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("not implemented")
}

func (s *accountShareBillingWorkerIntentRepoStub) ClaimReady(
	_ context.Context,
	input ClaimAccountShareBillingIntentsInput,
) ([]AccountShareBillingIntentWorkItem, error) {
	callIndex := s.claimCalls
	s.claimCalls++
	if s.onClaim != nil {
		s.onClaim(callIndex, input)
	}
	if callIndex < len(s.claimErrors) && s.claimErrors[callIndex] != nil {
		return nil, s.claimErrors[callIndex]
	}
	if callIndex < len(s.claimBatches) {
		return append([]AccountShareBillingIntentWorkItem(nil), s.claimBatches[callIndex]...), nil
	}
	return append([]AccountShareBillingIntentWorkItem(nil), s.claimItems...), s.claimErr
}

func (s *accountShareBillingWorkerIntentRepoStub) RenewProcessingLease(
	_ context.Context,
	transition AccountShareBillingIntentLeaseTransition,
	_ time.Duration,
) (*AccountShareBillingIntentState, error) {
	s.renewed = append(s.renewed, transition)
	return &AccountShareBillingIntentState{
		ID:         transition.ID,
		Status:     AccountShareBillingIntentStatusProcessing,
		StateToken: transition.ExpectedStateToken,
		LeaseToken: transition.LeaseToken,
		LeaseOwner: transition.WorkerID,
	}, nil
}

func (s *accountShareBillingWorkerIntentRepoStub) MarkSettled(
	_ context.Context,
	input MarkAccountShareBillingIntentSettledInput,
) (*AccountShareBillingIntentState, error) {
	s.settled = append(s.settled, input)
	if s.markSettledErr != nil {
		return nil, s.markSettledErr
	}
	return &AccountShareBillingIntentState{
		ID:         input.ID,
		Status:     AccountShareBillingIntentStatusSettled,
		StateToken: input.ExpectedStateToken + 1,
	}, nil
}

func (s *accountShareBillingWorkerIntentRepoStub) MarkFailed(
	_ context.Context,
	input MarkAccountShareBillingIntentFailedInput,
) (*AccountShareBillingIntentState, error) {
	s.failed = append(s.failed, input)
	if s.markFailedErr != nil {
		return nil, s.markFailedErr
	}
	status := AccountShareBillingIntentStatusFailed
	if input.NeedsAttention {
		status = AccountShareBillingIntentStatusNeedsAttention
	}
	return &AccountShareBillingIntentState{
		ID:         input.ID,
		Status:     status,
		StateToken: input.ExpectedStateToken + 1,
	}, nil
}

func (s *accountShareBillingWorkerIntentRepoStub) EscalateStaleToNeedsAttention(context.Context, EscalateAccountShareBillingIntentInput) (*AccountShareBillingIntentState, error) {
	return nil, errors.New("not implemented")
}

func (s *accountShareBillingWorkerIntentRepoStub) CountPendingByMembership(context.Context, int64) (int64, error) {
	return 0, errors.New("not implemented")
}

func (s *accountShareBillingWorkerIntentRepoStub) ListStaleForAttention(context.Context, time.Time, int) ([]AccountShareBillingIntentAttentionCandidate, error) {
	return nil, errors.New("not implemented")
}
