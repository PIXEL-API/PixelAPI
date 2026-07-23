package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

func openAICompatiblePlatformFromContext(ctx context.Context) string {
	if ctx != nil {
		if platform, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && platform == PlatformGrok {
			return PlatformGrok
		}
	}
	return PlatformOpenAI
}

func withGrokPlatform(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.ForcePlatform, PlatformGrok)
}

func (s *OpenAIGatewayService) SetGrokTokenProvider(provider *GrokTokenProvider) {
	if s == nil {
		return
	}
	s.grokTokenProvider = provider
}

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForGrok(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredEndpointCapability OpenAIEndpointCapability,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	ctx = withGrokPlatform(ctx)
	selection, decision, err := s.selectAccountWithScheduler(
		ctx,
		groupID,
		"",
		sessionHash,
		requestedModel,
		excludedIDs,
		OpenAIUpstreamTransportHTTPSSE,
		"",
		requiredEndpointCapability,
		false,
	)
	if err != nil || selection == nil || selection.Account == nil {
		return selection, decision, err
	}
	if selection.Account.Platform == PlatformGrok {
		selection.OpenAIDispatchRequirements = &OpenAIAccountDispatchRequirements{
			RequestedModel:             requestedModel,
			RequiredTransport:          OpenAIUpstreamTransportHTTPSSE,
			RequiredEndpointCapability: requiredEndpointCapability,
			RequiredPlatform:           PlatformGrok,
		}
		return selection, decision, nil
	}
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	return nil, decision, ErrNoAvailableAccounts
}
