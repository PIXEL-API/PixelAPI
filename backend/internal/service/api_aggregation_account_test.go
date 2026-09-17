package service

import (
	"testing"
)

func TestValidateAPIAggregationUpstreamURLRejectsNonPublicTargets(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"http://api.example.com",                   // 非 https
		"https://localhost",                        // localhost
		"https://foo.localhost",                    // localhost 后缀
		"https://gateway.internal",                 // 内部域名
		"https://nas.local",                        // mDNS 本地域
		"https://127.0.0.1",                        // loopback
		"https://10.0.0.8",                         // RFC1918
		"https://172.16.0.1",                       // RFC1918
		"https://192.168.1.1",                      // RFC1918
		"https://169.254.1.1",                      // link-local
		"https://100.64.0.1",                       // CGNAT
		"https://192.0.2.10",                       // 文档保留段
		"https://198.51.100.5",                     // 文档保留段
		"https://198.18.0.1",                       // 基准测试段
		"https://240.1.2.3",                        // 保留段
		"https://255.255.255.255",                  // 受限广播
		"https://[::1]",                            // IPv6 loopback
		"https://[fd00::1]",                        // IPv6 ULA
		"https://[fe80::1]",                        // IPv6 link-local
		"https://this-host-does-not-exist.invalid", // DNS 解析失败
	}
	for _, raw := range cases {
		if got, err := validateAPIAggregationUpstreamURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected, got %q", raw, got)
		}
	}
}

func TestValidateAPIAggregationUpstreamURLAcceptsPublicHTTPS(t *testing.T) {
	t.Parallel()
	got, err := validateAPIAggregationUpstreamURL("https://8.8.8.8/")
	if err != nil {
		t.Fatalf("expected public https literal to pass, got %v", err)
	}
	if got != "https://8.8.8.8" {
		t.Fatalf("unexpected normalization: %q", got)
	}
}

func TestValidateAPIAggregationUpstreamURLsNormalizesCredentialShape(t *testing.T) {
	t.Parallel()
	credentials := map[string]any{
		"api_key":  "sk-test",
		"base_url": "https://8.8.8.8/v1/",
		"api_base_urls": map[string]any{
			"chat_completions": "https://8.8.8.8/cc",
			"responses":        "https://8.8.8.8",
			"anthropic":        "https://8.8.8.8/anthropic",
		},
		"api_protocol": "adaptive",
	}
	if err := validateAPIAggregationUpstreamURLs(credentials); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if credentials["base_url"] != "https://8.8.8.8/v1" {
		t.Fatalf("base_url not normalized: %v", credentials["base_url"])
	}
}

func TestValidateAPIAggregationUpstreamURLsRejectsPrivateProtocolOverride(t *testing.T) {
	t.Parallel()
	credentials := map[string]any{
		"api_key":  "sk-test",
		"base_url": "https://8.8.8.8",
		"api_base_urls": map[string]any{
			"anthropic": "https://10.0.0.1",
		},
	}
	if err := validateAPIAggregationUpstreamURLs(credentials); err == nil {
		t.Fatal("expected private api_base_urls entry to be rejected")
	}
}

func TestValidateAPIAggregationUpstreamURLsRejectsUnknownProtocolKey(t *testing.T) {
	t.Parallel()
	credentials := map[string]any{
		"api_key":  "sk-test",
		"base_url": "https://8.8.8.8",
		"api_base_urls": map[string]any{
			"grpc": "https://8.8.8.8",
		},
	}
	if err := validateAPIAggregationUpstreamURLs(credentials); err == nil {
		t.Fatal("expected unknown api_base_urls key to be rejected")
	}
}

func TestAPIAggregationAccountURLResolution(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformAPIAggregation,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-upstream",
			"base_url": "https://agg.example.com",
			"api_base_urls": map[string]any{
				"anthropic": "https://agg-claude.example.com",
			},
		},
	}
	if got := account.GetAPIProtocol(); got != APIProtocolAdaptive {
		t.Fatalf("default protocol should be adaptive, got %q", got)
	}
	if got := account.GetCNProtocolBaseURL(APIProtocolChatCompletions); got != "https://agg.example.com" {
		t.Fatalf("chat_completions should fall back to base_url, got %q", got)
	}
	if got := account.GetCNProtocolBaseURL(APIProtocolResponses); got != "https://agg.example.com" {
		t.Fatalf("responses should fall back to base_url, got %q", got)
	}
	if got := account.GetAnthropicProtocolBaseURL(); got != "https://agg-claude.example.com" {
		t.Fatalf("anthropic should use protocol override, got %q", got)
	}
	if got := account.GetOpenAIBaseURL(); got != "https://agg.example.com" {
		t.Fatalf("GetOpenAIBaseURL should return base_url, got %q", got)
	}
	if !account.HasProtocolBaseURL(APIProtocolAnthropic) {
		t.Fatal("expected anthropic protocol override to be detected")
	}
	if account.HasProtocolBaseURL(APIProtocolResponses) {
		t.Fatal("responses override is not configured")
	}
}

func TestAPIAggregationAccountNeverFallsBackToOpenAIDefault(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform:    PlatformAPIAggregation,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-upstream"},
	}
	if got := account.GetOpenAIBaseURL(); got != "" {
		t.Fatalf("missing base_url must not fall back to OpenAI, got %q", got)
	}
	if got := account.GetCNProtocolBaseURL(APIProtocolChatCompletions); got != "" {
		t.Fatalf("missing base_url must not fall back, got %q", got)
	}
}

func TestValidateOwnedAccountSourceAPIAggregation(t *testing.T) {
	t.Parallel()
	err := validateOwnedAccountSourceForPlatform(PlatformAPIAggregation, AccountTypeAPIKey, map[string]any{
		"api_key": "sk-test",
	}, nil)
	if err == nil {
		t.Fatal("missing base_url must be rejected")
	}
	err = validateOwnedAccountSourceForPlatform(PlatformAPIAggregation, AccountTypeAPIKey, map[string]any{
		"api_key":  "sk-test",
		"base_url": "https://8.8.8.8",
	}, nil)
	if err != nil {
		t.Fatalf("valid credentials rejected: %v", err)
	}
	err = validateOwnedAccountSourceForPlatform(PlatformAPIAggregation, AccountTypeOAuth, map[string]any{
		"api_key":  "sk-test",
		"base_url": "https://8.8.8.8",
	}, nil)
	if err == nil {
		t.Fatal("oauth account type must be rejected for api_aggregation")
	}
}
