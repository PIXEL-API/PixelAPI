package service

import (
	"context"
	"net/http"

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

// doOpenAIAccountUpstream keeps the ordinary OpenAI/Grok transport unchanged,
// while ensuring every OpenCode forwarding path uses the account's TLS
// fingerprint profile. DoWithTLS deliberately degrades to Do when no profile
// service is injected, which preserves lightweight unit-test construction.
func (s *OpenAIGatewayService) doOpenAIAccountUpstream(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if account.IsOpencode() {
		return s.httpUpstream.DoWithTLS(
			req,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.resolveOpenAIAccountTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
}

// refreshOpencodeUsageIfStale 在调度选号时同步刷新 opencode 账号的用量窗口（若 stale），
// 让达限判定（IsOpencodeQuotaProtectionActiveAt）用最新 percent 而非异步拉取的旧值。
func (s *OpenAIGatewayService) refreshOpencodeUsageIfStale(ctx context.Context, account *Account) {
	if s == nil || s.accountUsageService == nil {
		return
	}
	s.accountUsageService.refreshOpencodeUsageIfStale(ctx, account)
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
