package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func openAICompatiblePlatformFromContext(ctx context.Context) string {
	if ctx != nil {
		switch platform, _ := ctx.Value(ctxkey.ForcePlatform).(string); platform {
		case PlatformGrok:
			return PlatformGrok
		case PlatformOpencode:
			return PlatformOpencode
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

// SetAccountUsageService 注入用量服务，供调度器在选号时对 opencode 账号惰性拉取用量窗口。
func (s *OpenAIGatewayService) SetAccountUsageService(usageService *AccountUsageService) {
	if s == nil {
		return
	}
	s.accountUsageService = usageService
}

// SetTLSFingerprintProfileService 注入 TLS 指纹服务，供 opencode 转发路径模拟
// 客户端 TLS 握手特征（Node.js 指纹），规避上游 browser signature 检测。
func (s *OpenAIGatewayService) SetTLSFingerprintProfileService(tlsFPProfileService *TLSFingerprintProfileService) {
	if s == nil {
		return
	}
	s.tlsFPProfileService = tlsFPProfileService
}

// resolveOpenAIAccountTLSProfile 安全解析账号的 TLS 指纹 profile。
// 服务未注入或账号未启用指纹时返回 nil，DoWithTLS 会退化为普通 Do。
func (s *OpenAIGatewayService) resolveOpenAIAccountTLSProfile(account *Account) *tlsfingerprint.Profile {
	if s == nil || s.tlsFPProfileService == nil {
		return nil
	}
	return s.tlsFPProfileService.ResolveTLSProfile(account)
}

// scheduleOpencodeUsageProbeIfStale 在调度选号时触发 opencode 账号的用量惰性刷新。
// 幂等、非阻塞：内部有 stale/throttle/在飞任务三重守卫。
func (s *OpenAIGatewayService) scheduleOpencodeUsageProbeIfStale(account *Account) {
	if s == nil || s.accountUsageService == nil {
		return
	}
	s.accountUsageService.ScheduleOpencodeUsageProbe(account)
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
