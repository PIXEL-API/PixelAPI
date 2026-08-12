//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayServiceRecordUsageResponseModelBillsCheaperIdentifiedModel(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(
		usageRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
	)
	svc.cfg.RunMode = config.RunModeSimple

	tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}
	baselineCost, err := svc.billingService.CalculateCost("claude-opus-4.5", tokens, 1.1)
	require.NoError(t, err)
	responseCost, err := svc.billingService.CalculateCost("claude-sonnet-4", tokens, 1.1)
	require.NoError(t, err)
	require.Less(t, responseCost.TotalCost, baselineCost.TotalCost)

	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:                            "gateway_response_model_downgrade",
			Usage:                                ClaudeUsage{InputTokens: 100, OutputTokens: 50},
			Model:                                "claude-opus-4.5",
			UpstreamModel:                        "claude-opus-4.5",
			UpstreamResponseModel:                "claude-sonnet-4",
			UpstreamResponseModelBillingEligible: true,
			Duration:                             time.Second,
		},
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformAnthropic},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "claude-opus-4.5",
			ChannelMappedModel: "claude-opus-4.5",
			BillingModelSource: BillingModelSourceResponse,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, responseCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
	require.NotNil(t, usageRepo.lastLog.UpstreamResponseModel)
	require.Equal(t, "claude-sonnet-4", *usageRepo.lastLog.UpstreamResponseModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModelMismatch)
	require.True(t, *usageRepo.lastLog.UpstreamModelMismatch)
}

func TestGatewayServiceRecordUsageResponseModelSafeFallbacks(t *testing.T) {
	tests := []struct {
		name          string
		responseModel string
		conflict      bool
		source        string
	}{
		{
			name:          "more expensive declaration",
			responseModel: "claude-opus-4.5",
			source:        BillingModelSourceResponse,
		},
		{
			name:          "unidentified family guess",
			responseModel: "totally-made-up-haiku-v9",
			source:        BillingModelSourceResponse,
		},
		{
			name:          "conflicting declaration",
			responseModel: "claude-sonnet-4",
			conflict:      true,
			source:        BillingModelSourceResponse,
		},
		{
			name:          "channel did not opt in",
			responseModel: "claude-sonnet-4",
			source:        BillingModelSourceChannelMapped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newGatewayRecordUsageServiceForTest(
				usageRepo,
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
			)
			svc.cfg.RunMode = config.RunModeSimple
			tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}
			baselineCost, err := svc.billingService.CalculateCost("claude-sonnet-4", tokens, 1.1)
			require.NoError(t, err)

			err = svc.RecordUsage(context.Background(), &RecordUsageInput{
				Result: &ForwardResult{
					RequestID:                            "gateway_response_model_fallback_" + tt.name,
					Usage:                                ClaudeUsage{InputTokens: 100, OutputTokens: 50},
					Model:                                "claude-sonnet-4",
					UpstreamModel:                        "claude-sonnet-4",
					UpstreamResponseModel:                tt.responseModel,
					UpstreamResponseModelConflict:        tt.conflict,
					UpstreamResponseModelBillingEligible: true,
					Duration:                             time.Second,
				},
				APIKey:  &APIKey{ID: 501, Quota: 100},
				User:    &User{ID: 601},
				Account: &Account{ID: 701, Platform: PlatformAnthropic},
				ChannelUsageFields: ChannelUsageFields{
					OriginalModel:      "claude-sonnet-4",
					ChannelMappedModel: "claude-sonnet-4",
					BillingModelSource: tt.source,
				},
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, baselineCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
		})
	}
}

func TestGatewayServiceRecordUsageResponseModelDoesNotChangeImageBilling(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(
		usageRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
	)
	svc.cfg.RunMode = config.RunModeSimple
	result := &ForwardResult{
		RequestID:                            "gateway_response_model_image",
		Model:                                "gemini-3-pro-image",
		UpstreamModel:                        "gemini-3-pro-image",
		UpstreamResponseModel:                "claude-sonnet-4",
		UpstreamResponseModelBillingEligible: true,
		ImageCount:                           1,
		ImageSize:                            "1K",
		Duration:                             time.Second,
	}
	expected, err := svc.calculateRecordUsageCost(context.Background(), result, &APIKey{ID: 501}, result.Model, 1.1, &recordUsageOpts{})
	require.NoError(t, err)

	err = svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result:  result,
		APIKey:  &APIKey{ID: 501, Quota: 100},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformGemini},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      result.Model,
			ChannelMappedModel: result.Model,
			BillingModelSource: BillingModelSourceResponse,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, expected.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
}

func TestGatewayResponseModelPricingRejectsNonTokenChannelModes(t *testing.T) {
	groupID := int64(902)
	for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage} {
		t.Run(string(mode), func(t *testing.T) {
			price := 0.01
			channelService := newOpenAIRecordUsageChannelServiceForTest(&channelCache{
				pricingByGroupModel: map[channelModelKey]*ChannelModelPricing{
					{groupID: groupID, model: "response-model"}: {
						BillingMode:     mode,
						PerRequestPrice: &price,
					},
				},
				wildcardByGroupPlatform: map[channelGroupPlatformKey][]*wildcardPricingEntry{},
				mappingByGroupModel:     map[channelModelKey]string{},
				wildcardMappingByGP:     map[channelGroupPlatformKey][]*wildcardMappingEntry{},
				channelByGroupID: map[int64]*Channel{
					groupID: {ID: groupID, Status: StatusActive},
				},
				groupPlatform: map[int64]string{groupID: ""},
			})
			svc := newGatewayRecordUsageServiceForTest(
				&openAIRecordUsageLogRepoStub{},
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
			)
			svc.channelService = channelService
			svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

			identified, channelPriced := svc.hasIdentifiedResponseModelPricing(
				context.Background(),
				"response-model",
				&APIKey{Group: &Group{ID: groupID}},
			)
			require.False(t, identified)
			require.False(t, channelPriced)
		})
	}
}
