package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSanitizeAnthropicBodyForBetaTokens(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-5","context_management":{"edits":[]},"messages":[]}`)

	kept, changed := sanitizeAnthropicBodyForBetaTokens(body, anthropicBetaContextManagementToken)
	require.False(t, changed)
	require.True(t, gjson.GetBytes(kept, "context_management").Exists())

	stripped, changed := sanitizeAnthropicBodyForBetaTokens(body, "fine-grained-tool-streaming-2025-05-14")
	require.True(t, changed)
	require.False(t, gjson.GetBytes(stripped, "context_management").Exists())
}

func TestDefaultFingerprintMatchesClaudeCodeConstants(t *testing.T) {
	require.Equal(t, "claude-cli/"+claude.CLICurrentVersion+" (external, cli)", defaultFingerprint.UserAgent)
	require.Equal(t, claude.DefaultHeaders["X-Stainless-Package-Version"], defaultFingerprint.StainlessPackageVersion)
}

func TestBuildUpstreamRequestUsesHeaderOverrideForBodySanitization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Header.Set("anthropic-beta", anthropicBetaContextManagementToken)

	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"anthropic-beta": "fine-grained-tool-streaming-2025-05-14",
			},
		},
	}
	body := []byte(`{"model":"claude-sonnet-5","context_management":{"edits":[]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}

	req, err := svc.buildUpstreamRequest(
		context.Background(), ctx, account, body, "token", "api_key", "claude-sonnet-5", false, false,
	)
	require.NoError(t, err)
	require.Equal(t, "fine-grained-tool-streaming-2025-05-14", getHeaderRaw(req.Header, "anthropic-beta"))

	wireBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(wireBody, "context_management").Exists())
}

func TestAnthropicPassthroughAndCountTokensSanitizeWithFinalBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		ctx.Request.Header.Set("anthropic-beta", anthropicBetaContextManagementToken)
		return ctx
	}
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"anthropic-beta": "fine-grained-tool-streaming-2025-05-14",
			},
		},
	}
	body := []byte(`{"model":"claude-sonnet-5","context_management":{"edits":[]},"messages":[]}`)
	svc := &GatewayService{cfg: &config.Config{}}

	builders := map[string]func() (*http.Request, error){
		"messages passthrough": func() (*http.Request, error) {
			return svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(
				context.Background(), newContext(), account, body, "token",
			)
		},
		"count tokens passthrough": func() (*http.Request, error) {
			return svc.buildCountTokensRequestAnthropicAPIKeyPassthrough(
				context.Background(), newContext(), account, body, "token",
			)
		},
		"count tokens": func() (*http.Request, error) {
			return svc.buildCountTokensRequest(
				context.Background(), newContext(), account, body, "token", "api_key", "claude-sonnet-5", false,
			)
		},
	}

	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			req, err := build()
			require.NoError(t, err)
			require.Equal(t, "fine-grained-tool-streaming-2025-05-14", getHeaderRaw(req.Header, "anthropic-beta"))
			wireBody, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(wireBody, "context_management").Exists())
		})
	}
}

func TestFilterVertexBetaTokens(t *testing.T) {
	got := filterVertexBetaTokens(
		"interleaved-thinking-2025-05-14,prompt-caching-scope-2026-01-05,context-management-2025-06-27,context-management-2025-06-27",
		map[string]struct{}{"interleaved-thinking-2025-05-14": {}},
	)
	require.Equal(t, "context-management-2025-06-27", got)
	require.Empty(t, filterVertexBetaTokens("redact-thinking-2026-02-12", nil))
}

func TestBillableModelWithFallback(t *testing.T) {
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	apiKey := &APIKey{}
	ctx := context.Background()

	require.Equal(t, "claude-sonnet-4",
		svc.billableModelWithFallback(ctx, apiKey, "team/best", "claude-sonnet-4"))
	require.Equal(t, "claude-opus-4",
		svc.billableModelWithFallback(ctx, apiKey, "claude-opus-4", "claude-sonnet-4"))
	require.Equal(t, "team/best",
		svc.billableModelWithFallback(ctx, apiKey, "team/best", "another/alias"))
	require.False(t, (&GatewayService{}).hasResolvableTokenPricing(ctx, "claude-sonnet-4", apiKey))
}

func TestAccountPoolModeRetryStatusOverride(t *testing.T) {
	account := &Account{Credentials: map[string]any{
		"pool_mode_retry_status_codes": []any{418},
	}}
	require.True(t, account.IsPoolModeRetryableStatus(418))
	require.False(t, account.IsPoolModeRetryableStatus(429))

	defaultAccount := &Account{}
	require.True(t, defaultAccount.IsPoolModeRetryableStatus(429))
	require.False(t, defaultAccount.IsPoolModeRetryableStatus(418))
}
