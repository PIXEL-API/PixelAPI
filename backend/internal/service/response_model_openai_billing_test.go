//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsageResponseModelBillsCheaperIdentifiedModel(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(
		usageRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	svc.cfg.RunMode = config.RunModeSimple

	usage := OpenAIUsage{InputTokens: 20, OutputTokens: 10}
	baselineCost := expectedOpenAICost(t, svc, "gpt-5.5", usage, 1.1)
	responseCost := expectedOpenAICost(t, svc, "gpt-5.4-nano", usage, 1.1)
	require.Less(t, responseCost.TotalCost, baselineCost.TotalCost)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:                            "openai_response_model_downgrade",
			Usage:                                usage,
			Model:                                "gpt-5.5",
			UpstreamModel:                        "gpt-5.5",
			UpstreamResponseModel:                "gpt-5.4-nano",
			UpstreamResponseModelBillingEligible: true,
			Duration:                             time.Second,
		},
		APIKey:  &APIKey{ID: 10},
		User:    &User{ID: 20},
		Account: &Account{ID: 30, Platform: PlatformOpenAI},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "gpt-5.5",
			ChannelMappedModel: "gpt-5.5",
			BillingModelSource: BillingModelSourceResponse,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, responseCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
	require.NotNil(t, usageRepo.lastLog.UpstreamResponseModel)
	require.Equal(t, "gpt-5.4-nano", *usageRepo.lastLog.UpstreamResponseModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModelMismatch)
	require.True(t, *usageRepo.lastLog.UpstreamModelMismatch)
}

func TestOpenAIGatewayServiceRecordUsageResponseModelSafeFallbacks(t *testing.T) {
	tests := []struct {
		name          string
		responseModel string
		conflict      bool
		source        string
	}{
		{
			name:          "more expensive declaration",
			responseModel: "gpt-5.5",
			source:        BillingModelSourceResponse,
		},
		{
			name:          "unidentified family guess",
			responseModel: "totally-made-up-haiku-v9",
			source:        BillingModelSourceResponse,
		},
		{
			name:          "conflicting declaration",
			responseModel: "gpt-5.4-nano",
			conflict:      true,
			source:        BillingModelSourceResponse,
		},
		{
			name:          "channel did not opt in",
			responseModel: "gpt-5.4-nano",
			source:        BillingModelSourceChannelMapped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(
				usageRepo,
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
				nil,
			)
			svc.cfg.RunMode = config.RunModeSimple
			usage := OpenAIUsage{InputTokens: 20, OutputTokens: 10}
			baselineCost := expectedOpenAICost(t, svc, "gpt-5.4-nano", usage, 1.1)

			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID:                            "openai_response_model_fallback_" + tt.name,
					Usage:                                usage,
					Model:                                "gpt-5.4-nano",
					UpstreamModel:                        "gpt-5.4-nano",
					UpstreamResponseModel:                tt.responseModel,
					UpstreamResponseModelConflict:        tt.conflict,
					UpstreamResponseModelBillingEligible: true,
					Duration:                             time.Second,
				},
				APIKey:  &APIKey{ID: 10},
				User:    &User{ID: 20},
				Account: &Account{ID: 30, Platform: PlatformOpenAI},
				ChannelUsageFields: ChannelUsageFields{
					OriginalModel:      "gpt-5.4-nano",
					ChannelMappedModel: "gpt-5.4-nano",
					BillingModelSource: tt.source,
				},
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, baselineCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsageResponseModelDoesNotChangeNonTokenBilling(t *testing.T) {
	tests := []struct {
		name  string
		model string
		apply func(*OpenAIForwardResult)
	}{
		{
			name:  "image",
			model: "gpt-image-2",
			apply: func(result *OpenAIForwardResult) {
				result.ImageCount = 1
				result.ImageSize = "1024x1024"
			},
		},
		{
			name:  "video",
			model: "grok-imagine-video",
			apply: func(result *OpenAIForwardResult) {
				result.VideoCount = 1
				result.VideoResolution = "720p"
				result.VideoDurationSeconds = 10
			},
		},
		{
			name:  "web search",
			model: "gpt-5.5",
			apply: func(result *OpenAIForwardResult) {
				result.WebSearchCalls = 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(
				usageRepo,
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
				nil,
			)
			svc.cfg.RunMode = config.RunModeSimple
			result := &OpenAIForwardResult{
				RequestID:                            "openai_response_model_non_token_" + tt.name,
				Model:                                tt.model,
				UpstreamModel:                        tt.model,
				UpstreamResponseModel:                "gpt-5.4-nano",
				UpstreamResponseModelBillingEligible: true,
				Duration:                             time.Second,
			}
			tt.apply(result)
			tokens, _ := openAIUsageTokens(result.Usage)
			expected, err := svc.calculateOpenAIRecordUsageCost(
				context.Background(), result, &APIKey{ID: 10}, tt.model,
				1.1, 1.1, 1.1, tokens, "",
			)
			require.NoError(t, err)

			err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result:  result,
				APIKey:  &APIKey{ID: 10},
				User:    &User{ID: 20},
				Account: &Account{ID: 30, Platform: PlatformOpenAI},
				ChannelUsageFields: ChannelUsageFields{
					OriginalModel:      tt.model,
					ChannelMappedModel: tt.model,
					BillingModelSource: BillingModelSourceResponse,
				},
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, expected.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
		})
	}
}

func TestOpenAIResponseModelPricingRejectsNonTokenChannelModes(t *testing.T) {
	groupID := int64(901)
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
			svc := newOpenAIRecordUsageServiceForTest(
				&openAIRecordUsageLogRepoStub{},
				&openAIRecordUsageUserRepoStub{},
				&openAIRecordUsageSubRepoStub{},
				nil,
			)
			svc.channelService = channelService
			svc.resolver = NewModelPricingResolver(channelService, svc.billingService)

			identified, channelPriced := svc.hasIdentifiedOpenAIResponsePricing(
				context.Background(),
				"response-model",
				&APIKey{Group: &Group{ID: groupID}},
			)
			require.False(t, identified)
			require.False(t, channelPriced)
		})
	}
}
