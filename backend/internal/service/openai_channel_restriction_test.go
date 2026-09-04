//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAISelectAccountForModelWithExclusions_ChannelMappedRestrictionRejectsEarly(t *testing.T) {
	t.Parallel()

	channelSvc := newTestChannelService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"gpt-4o"}},
		},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: {"gpt-4.1": "o3-mini"},
		},
	}, map[int64]string{10: PlatformOpenAI}))

	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true},
		}},
		channelService: channelSvc,
	}

	groupID := int64(10)
	_, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "", "gpt-4.1", nil)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.True(t, IsOpenAIAccountSelectionExhausted(err))
	require.Contains(t, err.Error(), "channel pricing restriction")
}

func TestOpenAIAccountShareModeChannelRestrictionDoesNotBecomeRouteExhaustion(t *testing.T) {
	t.Parallel()

	groupID := int64(11)
	modeGroup := true
	shareRepo := &accountShareModeRepoStub{modeGroup: &modeGroup}
	channelSvc := newTestChannelService(makeStandardRepo(Channel{
		ID:                 2,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"gpt-4o"}},
		},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: {"gpt-4.1": "o3-mini"},
		},
	}, map[int64]string{groupID: PlatformOpenAI}))
	svc := &OpenAIGatewayService{
		accountRepo:             stubOpenAIAccountRepo{accounts: []Account{{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}}},
		accountShareModeService: &AccountShareModeService{repo: shareRepo},
		channelService:          channelSvc,
	}

	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-4.1", nil, OpenAIUpstreamTransportAny, false)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrAccountShareModeSelection)
	require.ErrorIs(t, err, ErrAccountShareModeUnsupportedModel)
	require.False(t, IsOpenAIAccountSelectionExhausted(err))
	require.Zero(t, shareRepo.bindingCalls)
}

func TestOpenAIAccountShareModeChannelLookupErrorPreservesCause(t *testing.T) {
	t.Parallel()

	groupID := int64(12)
	modeGroup := true
	lookupErr := errors.New("channel repository unavailable")
	shareRepo := &accountShareModeRepoStub{modeGroup: &modeGroup}
	channelSvc := newTestChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) {
			return nil, lookupErr
		},
	})
	svc := &OpenAIGatewayService{
		accountRepo:             stubOpenAIAccountRepo{accounts: []Account{{ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}}},
		accountShareModeService: &AccountShareModeService{repo: shareRepo},
		channelService:          channelSvc,
	}

	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-4.1", nil, OpenAIUpstreamTransportAny, false)

	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrAccountShareModeSelection)
	require.ErrorIs(t, err, lookupErr)
	require.False(t, IsOpenAIAccountSelectionExhausted(err))
	require.Zero(t, shareRepo.bindingCalls)
}

func TestOpenAIRevalidateSelectedAccountForDispatch_ChannelRestrictionPrecedesTemporaryState(t *testing.T) {
	t.Parallel()

	groupID := int64(10)
	rateLimitedUntil := time.Now().Add(time.Hour)
	account := Account{
		ID:               71,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		RateLimitResetAt: &rateLimitedUntil,
		GroupIDs:         []int64{groupID},
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-4.1": "gpt-4o"},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"o3-mini"}},
		},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: {"gpt-4.1": "gpt-4o"},
		},
	}, map[int64]string{groupID: PlatformOpenAI}))
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{account}},
		channelService: channelSvc,
	}

	latest, err := svc.RevalidateSelectedOpenAIAccountForDispatch(
		context.Background(),
		&groupID,
		&account,
		OpenAIAccountDispatchRequirements{RequestedModel: "gpt-4.1", RequiredTransport: OpenAIUpstreamTransportAny},
	)
	require.Nil(t, latest)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.True(t, IsOpenAIDispatchAccountUnavailable(err))
	require.True(t, IsOpenAIWSContinuationPermanentError(err), "stable channel policy must take precedence over a temporary rate limit")
}

func TestOpenAIRevalidateSelectedAccountForDispatch_ChannelLookupErrorIsInfrastructure(t *testing.T) {
	t.Parallel()

	groupID := int64(10)
	account := Account{
		ID:          72,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-4.1": "gpt-4.1"}},
	}
	lookupErr := errors.New("channel repository unavailable")
	channelSvc := newTestChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) {
			return nil, lookupErr
		},
	})
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{account}},
		channelService: channelSvc,
	}

	latest, err := svc.RevalidateSelectedOpenAIAccountForDispatch(
		context.Background(),
		&groupID,
		&account,
		OpenAIAccountDispatchRequirements{RequestedModel: "gpt-4.1", RequiredTransport: OpenAIUpstreamTransportAny},
	)
	require.Nil(t, latest)
	require.ErrorIs(t, err, lookupErr)
	require.False(t, IsOpenAIDispatchAccountUnavailable(err))
	require.False(t, IsOpenAIWSContinuationPermanentError(err))
	require.NotErrorIs(t, err, ErrNoAvailableAccounts)
}

func TestOpenAIRevalidateSelectedAccountForDispatch_UpstreamChannelRestrictionIsDispatchLocal(t *testing.T) {
	t.Parallel()

	groupID := int64(10)
	account := Account{
		ID:          73,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-4.1": "gpt-4o"},
		},
	}
	channelSvc := newTestChannelService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"o3-mini"}},
		},
	}, map[int64]string{groupID: PlatformOpenAI}))
	svc := &OpenAIGatewayService{
		accountRepo:    stubOpenAIAccountRepo{accounts: []Account{account}},
		channelService: channelSvc,
	}

	latest, err := svc.RevalidateSelectedOpenAIAccountForDispatch(
		context.Background(),
		&groupID,
		&account,
		OpenAIAccountDispatchRequirements{RequestedModel: "gpt-4.1", RequiredTransport: OpenAIUpstreamTransportAny},
	)
	require.Nil(t, latest)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.True(t, IsOpenAIDispatchAccountUnavailable(err))
	require.True(t, IsOpenAIWSContinuationPermanentError(err))
}

func TestOpenAISelectAccountForModelWithExclusions_UpstreamRestrictionSkipsDisallowedAccount(t *testing.T) {
	t.Parallel()

	channelSvc := newTestChannelService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"o3-mini"}},
		},
	}, map[int64]string{10: PlatformOpenAI}))

	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    10,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "gpt-4o"},
				},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    20,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "o3-mini"},
				},
			},
		}},
		channelService: channelSvc,
	}

	groupID := int64(10)
	account, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "", "gpt-4.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
}

func TestOpenAISelectAccountForModelWithExclusions_StickyRestrictedUpstreamFallsBack(t *testing.T) {
	t.Parallel()

	channelSvc := newTestChannelService(makeStandardRepo(Channel{
		ID:                 1,
		Status:             StatusActive,
		GroupIDs:           []int64{10},
		RestrictModels:     true,
		BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"o3-mini"}},
		},
	}, map[int64]string{10: PlatformOpenAI}))

	cache := &stubGatewayCache{
		sessionBindings: map[string]int64{"openai:sticky-session": 1},
	}
	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    10,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "gpt-4o"},
				},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Priority:    20,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-4.1": "o3-mini"},
				},
			},
		}},
		channelService: channelSvc,
		cache:          cache,
	}

	groupID := int64(10)
	account, err := svc.SelectAccountForModelWithExclusions(context.Background(), &groupID, "sticky-session", "gpt-4.1", nil)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(2), account.ID)
	require.Equal(t, 1, cache.deletedSessions["openai:sticky-session"])
	require.Equal(t, int64(2), cache.sessionBindings["openai:sticky-session"])
}
