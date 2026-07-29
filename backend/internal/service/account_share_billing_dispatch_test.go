package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestForwardResultBillingGateSubmitsOnlyFirstResult(t *testing.T) {
	var calls atomic.Int32
	gate := NewForwardResultBillingGate(func(result *ForwardResult) error {
		calls.Add(1)
		require.Equal(t, "req-first", result.RequestID)
		return nil
	})
	ctx := WithForwardResultBillingGate(context.Background(), gate)

	handled, err := CommitForwardResultBillingGateBeforeTerminal(ctx, &ForwardResult{
		RequestID:            "req-first",
		Usage:                ClaudeUsage{InputTokens: 1},
		BillingUsageComplete: true,
	})
	require.True(t, handled)
	require.NoError(t, err)
	handled, err = CommitForwardResultBillingGateBeforeTerminal(ctx, &ForwardResult{
		RequestID:            "req-second",
		Usage:                ClaudeUsage{InputTokens: 2},
		BillingUsageComplete: true,
	})
	require.True(t, handled)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenAIForwardResultBillingGateCachesFailure(t *testing.T) {
	submitErr := errors.New("billing unavailable")
	var calls atomic.Int32
	gate := NewOpenAIForwardResultBillingGate(func(result *OpenAIForwardResult) error {
		calls.Add(1)
		require.Equal(t, "req-first", result.RequestID)
		return submitErr
	})
	ctx := WithOpenAIForwardResultBillingGate(context.Background(), gate)

	handled, firstErr := CommitOpenAIForwardResultBillingGateBeforeTerminal(ctx, &OpenAIForwardResult{
		RequestID:            "req-first",
		Usage:                OpenAIUsage{InputTokens: 1},
		BillingUsageComplete: true,
	})
	require.True(t, handled)
	require.ErrorIs(t, firstErr, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, firstErr, submitErr)
	handled, secondErr := CommitOpenAIForwardResultBillingGateBeforeTerminal(ctx, &OpenAIForwardResult{
		RequestID:            "req-second",
		Usage:                OpenAIUsage{InputTokens: 2},
		BillingUsageComplete: true,
	})
	require.True(t, handled)
	require.Same(t, firstErr, secondErr)
	require.EqualValues(t, 1, calls.Load())
}

func TestForwardResultBillingGateRejectsIncompleteUsageBeforeSubmit(t *testing.T) {
	var calls atomic.Int32
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(*ForwardResult) error {
		calls.Add(1)
		return nil
	}))

	handled, err := CommitForwardResultBillingGateBeforeTerminal(ctx, &ForwardResult{
		RequestID: "req-incomplete",
		Usage:     ClaudeUsage{InputTokens: 1},
	})

	require.True(t, handled)
	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, err, ErrAccountShareBillingUsageValidation)
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
	require.Zero(t, calls.Load())
}

func TestForwardResultBillingGateAllowsExplicitZeroUsageBeforeTerminal(t *testing.T) {
	var calls atomic.Int32
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		calls.Add(1)
		require.Zero(t, result.Usage.InputTokens)
		require.Zero(t, result.Usage.OutputTokens)
		require.True(t, result.BillingUsageComplete)
		return nil
	}))

	handled, err := CommitForwardResultBillingGateBeforeTerminal(ctx, &ForwardResult{
		RequestID:            "req-zero",
		BillingUsageComplete: true,
	})

	require.True(t, handled)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestForwardResultBillingGateAllowsImageCountBeforeTerminal(t *testing.T) {
	var calls atomic.Int32
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		calls.Add(1)
		require.Equal(t, 1, result.ImageCount)
		return nil
	}))

	handled, err := CommitForwardResultBillingGateBeforeTerminal(ctx, &ForwardResult{
		RequestID:  "req-image",
		ImageCount: 1,
	})

	require.True(t, handled)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenAIForwardResultBillingGateRejectsIncompleteUsageBeforeSubmit(t *testing.T) {
	var calls atomic.Int32
	ctx := WithOpenAIForwardResultBillingGate(context.Background(), NewOpenAIForwardResultBillingGate(func(*OpenAIForwardResult) error {
		calls.Add(1)
		return nil
	}))

	handled, err := CommitOpenAIForwardResultBillingGateBeforeTerminal(ctx, &OpenAIForwardResult{
		RequestID: "req-incomplete",
		Usage:     OpenAIUsage{InputTokens: 1},
	})

	require.True(t, handled)
	require.ErrorIs(t, err, ErrAccountShareBillingPreTerminalCommit)
	require.ErrorIs(t, err, ErrAccountShareBillingUsageValidation)
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
	require.Zero(t, calls.Load())
}

func TestOpenAIForwardResultBillingGateAllowsExplicitZeroUsageBeforeTerminal(t *testing.T) {
	var calls atomic.Int32
	ctx := WithOpenAIForwardResultBillingGate(context.Background(), NewOpenAIForwardResultBillingGate(func(result *OpenAIForwardResult) error {
		calls.Add(1)
		require.Zero(t, result.Usage.InputTokens)
		require.Zero(t, result.Usage.OutputTokens)
		require.True(t, result.BillingUsageComplete)
		return nil
	}))

	handled, err := CommitOpenAIForwardResultBillingGateBeforeTerminal(ctx, &OpenAIForwardResult{
		RequestID:            "req-zero",
		BillingUsageComplete: true,
	})

	require.True(t, handled)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestForwardResultBillingGateAllowsPartialUsageAfterStreamFailure(t *testing.T) {
	var calls atomic.Int32
	ctx := WithForwardResultBillingGate(context.Background(), NewForwardResultBillingGate(func(result *ForwardResult) error {
		calls.Add(1)
		require.Equal(t, 3, result.Usage.InputTokens)
		require.False(t, result.BillingUsageComplete)
		return nil
	}))

	handled, err := CommitForwardResultBillingGate(ctx, &ForwardResult{
		RequestID: "req-partial",
		Usage:     ClaudeUsage{InputTokens: 3},
	})

	require.True(t, handled)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenAIForwardResultBillingGateAllowsPartialUsageAfterStreamFailure(t *testing.T) {
	var calls atomic.Int32
	ctx := WithOpenAIForwardResultBillingGate(context.Background(), NewOpenAIForwardResultBillingGate(func(result *OpenAIForwardResult) error {
		calls.Add(1)
		require.Equal(t, 4, result.Usage.InputTokens)
		require.False(t, result.BillingUsageComplete)
		return nil
	}))

	handled, err := CommitOpenAIForwardResultBillingGate(ctx, &OpenAIForwardResult{
		RequestID: "req-partial",
		Usage:     OpenAIUsage{InputTokens: 4},
	})

	require.True(t, handled)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

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

func TestMarkAccountShareBillingDispatchReadyNoChargeKeepsUsageAndZerosFinancialFields(t *testing.T) {
	intentRepo := &accountShareBillingDispatchIntentRepoStub{}
	dispatch := &AccountShareBillingDispatch{
		repository: intentRepo,
		barrier:    newAccountShareBillingReleaseBarrier(time.Second),
		command: AccountShareBillingCommand{
			SchemaVersion:     AccountShareBillingCommandSchemaV3,
			RateMultiplier:    "1.5",
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
	ctx := WithAccountShareBillingDispatch(context.Background(), dispatch)
	statsCost := 0.3
	command := &UsageBillingCommand{
		APIKeyID:                   31,
		UserID:                     21,
		AccountID:                  51,
		BalanceCost:                1.25,
		SubscriptionCost:           2.5,
		PrivateGroupCommissionCost: 0.2,
		APIKeyQuotaCost:            1.25,
		APIKeyRateLimitCost:        1.25,
		AccountQuotaCost:           1.25,
		UsageOccurredAt:            time.Now().UTC(),
		UsageLog: &UsageLog{
			RequestID:        "provider-request-simple",
			Model:            "gpt-5.4",
			InputTokens:      123,
			OutputTokens:     45,
			InputCost:        0.7,
			OutputCost:       0.55,
			TotalCost:        1.25,
			ActualCost:       1.25,
			AccountStatsCost: &statsCost,
			RateMultiplier:   1.5,
			RequestType:      RequestTypeStream,
			Stream:           true,
		},
	}

	handled, err := markAccountShareBillingDispatchReadyNoCharge(ctx, command)

	require.NoError(t, err)
	require.True(t, handled)
	require.Len(t, intentRepo.ready, 1)
	usage := intentRepo.ready[0].Usage
	require.Equal(t, int64(123), usage.InputTokens)
	require.Equal(t, int64(45), usage.OutputTokens)
	require.Equal(t, "0", usage.InputCost)
	require.Equal(t, "0", usage.OutputCost)
	require.Equal(t, "0", usage.TotalCost)
	require.Equal(t, "0", usage.ActualCost)
	require.Nil(t, usage.AccountStatsCost)
	require.Equal(t, "0", usage.BalanceCost)
	require.Equal(t, "0", usage.SubscriptionCost)
	require.Equal(t, "0", usage.PrivateGroupCommissionCost)
	require.Equal(t, "0", usage.APIKeyQuotaCost)
	require.Equal(t, "0", usage.APIKeyRateLimitCost)
	require.Equal(t, "0", usage.AccountQuotaCost)
	require.Equal(t, "0", usage.BaseCharge)
	require.Equal(t, "0", usage.TotalCharge)
}

func TestAccountShareBillingRateMultiplierFromContext(t *testing.T) {
	tests := []struct {
		name           string
		rateMultiplier string
		want           float64
		wantErr        bool
	}{
		{name: "inexact decimal 0.9", rateMultiplier: "0.9", want: 0.9},
		{name: "inexact decimal 0.005", rateMultiplier: "0.005", want: 0.005},
		{name: "inexact decimal 1.1", rateMultiplier: "1.1", want: 1.1},
		{name: "zero", rateMultiplier: "0", want: 0},
		{name: "negative", rateMultiplier: "-0.1", wantErr: true},
		{name: "overflow", rateMultiplier: "1e10000", wantErr: true},
		{name: "underflow", rateMultiplier: "1e-10000", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dispatch := &AccountShareBillingDispatch{
				command: AccountShareBillingCommand{RateMultiplier: tt.rateMultiplier},
			}
			ctx := WithAccountShareBillingDispatch(context.Background(), dispatch)

			got, ok, err := accountShareBillingRateMultiplierFromContext(ctx)
			require.True(t, ok)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tt.want, got, 1e-12)
		})
	}
}

func TestAccountShareBillingRevenueRatiosUseExactDecimalRemainder(t *testing.T) {
	tests := []struct {
		name         string
		owner        float64
		invite       float64
		wantOwner    string
		wantInvite   string
		wantPlatform string
	}{
		{
			name:         "seventy percent owner share",
			owner:        0.7,
			wantOwner:    "0.7",
			wantInvite:   "0",
			wantPlatform: "0.3",
		},
		{
			name:         "owner and invite shares",
			owner:        0.7,
			invite:       0.1,
			wantOwner:    "0.7",
			wantInvite:   "0.1",
			wantPlatform: "0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, invite, platform := accountShareBillingRevenueRatios(tt.owner, tt.invite)

			require.Equal(t, tt.wantOwner, owner)
			require.Equal(t, tt.wantInvite, invite)
			require.Equal(t, tt.wantPlatform, platform)

			ownerDecimal, err := decimal.NewFromString(owner)
			require.NoError(t, err)
			inviteDecimal, err := decimal.NewFromString(invite)
			require.NoError(t, err)
			platformDecimal, err := decimal.NewFromString(platform)
			require.NoError(t, err)
			require.True(t, ownerDecimal.Add(inviteDecimal).Add(platformDecimal).Equal(decimal.NewFromInt(1)))
		})
	}
}

func TestRetryAccountShareBillingRecordUsage(t *testing.T) {
	t.Run("non durable dispatch runs once", func(t *testing.T) {
		var calls atomic.Int32
		errExpected := errors.New("temporary")
		err := retryAccountShareBillingRecordUsage(context.Background(), func(context.Context) error {
			calls.Add(1)
			return errExpected
		})

		require.ErrorIs(t, err, errExpected)
		require.Equal(t, int32(1), calls.Load())
	})

	t.Run("durable dispatch retries temporary failure", func(t *testing.T) {
		ctx := WithAccountShareBillingDispatch(context.Background(), &AccountShareBillingDispatch{})
		var calls atomic.Int32
		err := retryAccountShareBillingRecordUsage(ctx, func(context.Context) error {
			if calls.Add(1) < 3 {
				return errors.New("temporary")
			}
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, int32(3), calls.Load())
	})

	t.Run("durable dispatch does not retry permanent failure", func(t *testing.T) {
		ctx := WithAccountShareBillingDispatch(context.Background(), &AccountShareBillingDispatch{})
		var calls atomic.Int32
		err := retryAccountShareBillingRecordUsage(ctx, func(context.Context) error {
			calls.Add(1)
			return ErrModelPricingUnavailable
		})

		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Equal(t, int32(1), calls.Load())
	})

	t.Run("canceled context stops before first attempt", func(t *testing.T) {
		baseCtx, cancel := context.WithCancel(context.Background())
		cancel()
		ctx := WithAccountShareBillingDispatch(baseCtx, &AccountShareBillingDispatch{})
		var calls atomic.Int32
		err := retryAccountShareBillingRecordUsage(ctx, func(context.Context) error {
			calls.Add(1)
			return errors.New("must not run")
		})

		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, calls.Load())
	})
}

func TestAccountShareBillingModelForDispatch(t *testing.T) {
	require.Equal(t, "requested-model", accountShareBillingModelForDispatch(
		ChannelUsageFields{
			BillingModelSource: BillingModelSourceRequested,
			OriginalModel:      "requested-model",
		},
		"request-fallback",
		"upstream-model",
	))
	require.Equal(t, "mapped-model", accountShareBillingModelForDispatch(
		ChannelUsageFields{
			BillingModelSource: BillingModelSourceChannelMapped,
			ChannelMappedModel: "mapped-model",
		},
		"requested-model",
		"upstream-model",
	))
	require.Equal(t, "upstream-model", accountShareBillingModelForDispatch(
		ChannelUsageFields{},
		"requested-model",
		"upstream-model",
	))
	require.Equal(t, "requested-model", accountShareBillingModelForDispatch(
		ChannelUsageFields{},
		"requested-model",
		"",
	))
}

func TestModelPricingResolverHasConfiguredPricing(t *testing.T) {
	billingService := &BillingService{fallbackPrices: map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 1e-6},
	}}
	require.True(t, NewModelPricingResolver(nil, billingService).HasConfiguredPricing(
		context.Background(),
		PricingInput{Model: "claude-sonnet-4"},
	))
	require.False(t, NewModelPricingResolver(nil, billingService).HasConfiguredPricing(
		context.Background(),
		PricingInput{Model: "missing-model"},
	))

	groupID := int64(100)
	zero := 0.0
	tests := []struct {
		name    string
		pricing ChannelModelPricing
		want    bool
	}{
		{
			name: "explicit zero channel price is configured",
			pricing: ChannelModelPricing{
				BillingMode: BillingModeToken,
				InputPrice:  &zero,
			},
			want: true,
		},
		{
			name:    "empty channel pricing is not configured",
			pricing: ChannelModelPricing{BillingMode: BillingModeToken},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := "channel-only-model"
			channelService := &ChannelService{}
			channelService.cache.Store(&channelCache{
				loadedAt: time.Now(),
				channelByGroupID: map[int64]*Channel{
					groupID: {ID: 1, Status: StatusActive},
				},
				groupPlatform: map[int64]string{
					groupID: PlatformAnthropic,
				},
				pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
					{
						groupID:  groupID,
						platform: PlatformAnthropic,
						model:    model,
					}: &tt.pricing,
				},
			})
			resolver := NewModelPricingResolver(channelService, billingService)
			require.Equal(t, tt.want, resolver.HasConfiguredPricing(
				context.Background(),
				PricingInput{Model: model, GroupID: &groupID},
			))
		})
	}
}

func TestGatewayServiceRecordUsageUnpricedModelFailsClosed(t *testing.T) {
	service := &GatewayService{
		billingService: &BillingService{fallbackPrices: map[string]*ModelPricing{}},
	}
	err := service.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "unpriced-gateway-model",
			Model:     "unknown-model-without-price",
			Usage: ClaudeUsage{
				InputTokens:  5,
				OutputTokens: 2,
			},
		},
		APIKey:  &APIKey{ID: 1},
		User:    &User{ID: 2},
		Account: &Account{ID: 3},
	})

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
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

func (s *accountShareBillingDispatchIntentRepoStub) ListRecoveryCandidates(context.Context, ListAccountShareBillingRecoveryCandidatesInput) ([]AccountShareBillingIntentAttentionCandidate, error) {
	return nil, errors.New("unexpected ListRecoveryCandidates")
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
