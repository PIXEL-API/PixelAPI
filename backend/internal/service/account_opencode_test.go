//go:build unit

package service

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestOpencodeAccountBaseURLAndApiKey(t *testing.T) {
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
		},
	}

	if got := account.GetOpencodeBaseURL(); got != OpencodeDefaultBaseURL {
		t.Fatalf("base url = %q, want %q", got, OpencodeDefaultBaseURL)
	}
	if got := account.GetOpencodeApiKey(); got != "opencode-secret" {
		t.Fatalf("api key = %q, want opencode-secret", got)
	}
	// GetOpenAIApiKey 对 opencode apikey 账号也应返回 api_key（供上游鉴权复用）。
	if got := account.GetOpenAIApiKey(); got != "opencode-secret" {
		t.Fatalf("GetOpenAIApiKey = %q, want opencode-secret", got)
	}
}

func TestOpencodeHelpersRejectNonOpencode(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "openai-secret",
		},
	}
	if account.GetOpencodeBaseURL() != "" {
		t.Fatal("expected empty base url for non-opencode account")
	}
	if account.GetOpencodeApiKey() != "" {
		t.Fatal("expected empty api key for non-opencode account")
	}
}

func TestNormalizeOpencodeGoModelID(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{name: "trim whitespace", model: "  deepseek-v4-flash  ", want: "deepseek-v4-flash"},
		{name: "opencode prefix", model: "opencode/grok-4.6", want: "grok-4.6"},
		{name: "opencode go prefix and long context", model: " opencode-go/grok-4.5[1m] ", want: "grok-4.5"},
		{name: "repeated long context suffix", model: "qwen3.8-flash[1m][1M]", want: "qwen3.8-flash"},
		{name: "ordinary slash is preserved", model: "custom/vendor-model", want: "custom/vendor-model"},
		{name: "known prefix is removed only once", model: "opencode/opencode-go/grok-4.6", want: "opencode-go/grok-4.6"},
		{name: "empty", model: "  ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeOpencodeGoModelID(tt.model); got != tt.want {
				t.Fatalf("NormalizeOpencodeGoModelID(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestResolveOpencodeGoModelSpec(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		protocol   OpencodeGoProtocol
		deprecated bool
		found      bool
	}{
		{name: "chat", model: "deepseek-v4-flash", protocol: OpencodeGoProtocolChat, found: true},
		{name: "messages", model: "opencode-go/minimax-m3[1m]", protocol: OpencodeGoProtocolMessages, found: true},
		{name: "documented qwen messages", model: "opencode-go/qwen3.7-plus", protocol: OpencodeGoProtocolMessages, found: true},
		{name: "responses", model: "opencode/grok-4.6", protocol: OpencodeGoProtocolResponses, found: true},
		{name: "deprecated", model: "grok-4.5", protocol: OpencodeGoProtocolResponses, deprecated: true, found: true},
		{name: "unknown", model: "future-model", found: false},
		{name: "unknown slash model", model: "custom/deepseek-v4-flash", found: false},
		{name: "empty", model: "", found: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, found := ResolveOpencodeGoModelSpec(tt.model)
			if found != tt.found {
				t.Fatalf("ResolveOpencodeGoModelSpec(%q) found = %v, want %v", tt.model, found, tt.found)
			}
			if !tt.found {
				if spec != (OpencodeGoModelSpec{}) {
					t.Fatalf("unknown model spec = %+v, want zero value", spec)
				}
				return
			}
			if spec.ID != NormalizeOpencodeGoModelID(tt.model) || spec.Protocol != tt.protocol || spec.Deprecated != tt.deprecated {
				t.Fatalf("resolved spec = %+v, want protocol=%q deprecated=%v", spec, tt.protocol, tt.deprecated)
			}
		})
	}
}

func TestOpencodeDefaultModelSlugs(t *testing.T) {
	models := OpencodeDefaultModelSlugs()
	wantModels := []string{
		"minimax-m3", "minimax-m2.7", "minimax-m2.5",
		"kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "longcat-2.0", "kimi-k2.5",
		"glm-5.2", "glm-5.3-flash", "glm-5.3", "glm-5.1", "glm-5",
		"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp",
		"qwen3.7-max", "qwen3.8-max", "qwen3.8-flash", "qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus",
		"mimo-v2-pro", "mimo-v2-omni", "mimo-v2.5-pro", "mimo-v2.5",
		"hy4-preview", "hy3", "hy3-preview",
		"gpt-5.6-luna", "grok-4.5", "grok-4.6", "muse-spark-1.2-contributor",
	}
	if !slices.Equal(models, wantModels) {
		t.Fatalf("model snapshot mismatch\n got: %v\nwant: %v", models, wantModels)
	}

	seen := make(map[string]struct{}, len(models))
	deprecated := make(map[string]bool, 7)
	messagesModels := map[string]bool{
		"minimax-m3":    true,
		"minimax-m2.7":  true,
		"minimax-m2.5":  true,
		"qwen3.7-max":   true,
		"qwen3.8-max":   true,
		"qwen3.8-flash": true,
		"qwen3.7-plus":  true,
		"qwen3.6-plus":  true,
	}
	responsesModels := map[string]bool{
		"gpt-5.6-luna":               true,
		"grok-4.5":                   true,
		"grok-4.6":                   true,
		"muse-spark-1.2-contributor": true,
	}
	for _, model := range models {
		if _, exists := seen[model]; exists {
			t.Fatalf("duplicate model %q", model)
		}
		seen[model] = struct{}{}

		spec, found := ResolveOpencodeGoModelSpec(model)
		if !found {
			t.Fatalf("catalog model %q cannot be resolved", model)
		}
		wantProtocol := OpencodeGoProtocolChat
		if messagesModels[model] {
			wantProtocol = OpencodeGoProtocolMessages
		} else if responsesModels[model] {
			wantProtocol = OpencodeGoProtocolResponses
		}
		if spec.Protocol != wantProtocol {
			t.Fatalf("model %q protocol = %q, want %q", model, spec.Protocol, wantProtocol)
		}
		if spec.Deprecated {
			deprecated[model] = true
		}
	}

	for _, model := range []string{
		"grok-4.5",
		"minimax-m2.5",
		"qwen3.5-plus",
		"glm-5",
		"kimi-k2.5",
		"mimo-v2-pro",
		"mimo-v2-omni",
	} {
		if !deprecated[model] {
			t.Errorf("model %q must be marked deprecated", model)
		}
	}
	if len(deprecated) != 7 {
		t.Fatalf("deprecated model count = %d, want 7", len(deprecated))
	}

	models[0] = "mutated"
	if fresh := OpencodeDefaultModelSlugs(); len(fresh) != 33 || fresh[0] == "mutated" {
		t.Fatalf("OpencodeDefaultModelSlugs did not return an independent copy: %v", fresh)
	}
}

func TestIsAllowedOwnedAccountTypeForOpencode(t *testing.T) {
	if !isAllowedOwnedAccountType(PlatformOpencode, AccountTypeAPIKey) {
		t.Fatal("expected opencode apikey to be allowed")
	}
	if isAllowedOwnedAccountType(PlatformOpencode, AccountTypeOAuth) {
		t.Fatal("expected opencode oauth to be rejected")
	}
	if isAllowedOwnedAccountType(PlatformOpenAI, AccountTypeAPIKey) {
		t.Fatal("expected openai apikey to be rejected (only OAuth)")
	}
	if !isAllowedOwnedAccountType(PlatformOpenAI, AccountTypeOAuth) {
		t.Fatal("expected openai oauth to be allowed")
	}
}

func TestOpencodeQuotaProtectionActive(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	account := &Account{
		Platform:    PlatformOpencode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"opencode_5h_used_percent": 100.0,
			"opencode_5h_reset_at":     resetAt.Format(time.RFC3339),
		},
	}

	if !account.IsOpencodeQuotaProtectionActiveAt(now) {
		t.Fatal("expected opencode quota protection to be active at 100% usage")
	}
	if got := account.OpencodeQuotaProtectionReasonAt(now); got != OpencodeQuotaWindow5h {
		t.Fatalf("reason = %q, want %q", got, OpencodeQuotaWindow5h)
	}
	if account.IsSchedulableAt(now) {
		t.Fatal("expected account to be unschedulable while opencode quota protection active")
	}
}

func TestOpencodeQuotaProtectionPicksLatestReset(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	fiveHourReset := now.Add(2 * time.Hour)
	monthReset := now.Add(30 * 24 * time.Hour)
	account := &Account{
		Platform:    PlatformOpencode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"opencode_5h_used_percent":  100.0,
			"opencode_5h_reset_at":      fiveHourReset.Format(time.RFC3339),
			"opencode_30d_used_percent": 100.0,
			"opencode_30d_reset_at":     monthReset.Format(time.RFC3339),
		},
	}

	if got := account.OpencodeQuotaProtectionReasonAt(now); got != OpencodeQuotaWindow30d {
		t.Fatalf("reason = %q, want %q", got, OpencodeQuotaWindow30d)
	}
	if got := account.OpencodeQuotaProtectionResetAt(now); got == nil || !got.Equal(monthReset) {
		t.Fatalf("reset_at = %v, want %v", got, monthReset)
	}
}

func TestOpencodeQuotaProtectionIgnoresExpiredWindow(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Platform:    PlatformOpencode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"opencode_5h_used_percent": 100.0,
			"opencode_5h_reset_at":     now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	if account.IsOpencodeQuotaProtectionActiveAt(now) {
		t.Fatal("did not expect protection after window reset")
	}
	if !account.IsSchedulableAt(now) {
		t.Fatal("expected account to be schedulable after window reset")
	}
}

func TestOpencodeQuotaProtectionBelowLimit(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Platform:    PlatformOpencode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"opencode_5h_used_percent": 99.9,
			"opencode_5h_reset_at":     now.Add(time.Hour).Format(time.RFC3339),
		},
	}

	if account.IsOpencodeQuotaProtectionActiveAt(now) {
		t.Fatal("did not expect protection below default 100% limit")
	}
	if !account.IsSchedulableAt(now) {
		t.Fatal("expected account to remain schedulable below limit")
	}
}

func TestBuildOpenAIMessagesURL(t *testing.T) {
	if got := buildOpenAIMessagesURL(OpencodeDefaultBaseURL); got != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("messages url = %q", got)
	}
	// 末尾已带 /messages 时不重复追加。
	if got := buildOpenAIMessagesURL("https://opencode.ai/zen/go/v1/messages"); got != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("messages url = %q", got)
	}
	// 无版本段时补 /v1/messages。
	if got := buildOpenAIMessagesURL("https://example.com/api"); got != "https://example.com/api/v1/messages" {
		t.Fatalf("messages url = %q", got)
	}
}

func TestOpencodeChatCompletionsAndResponsesURLs(t *testing.T) {
	if got := buildOpenAIChatCompletionsURL(OpencodeDefaultBaseURL); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("chat completions url = %q", got)
	}
	if got := buildOpenAIResponsesURL(OpencodeDefaultBaseURL); got != "https://opencode.ai/zen/go/v1/responses" {
		t.Fatalf("responses url = %q", got)
	}
}

func TestParseOpencodeUsageWindowsArray(t *testing.T) {
	body := []byte(`{
		"windows": [
			{"window": "5h", "percent": 50, "resets_at": "2026-08-16T12:00:00Z"},
			{"window": "7d", "used_percent": 75, "reset_at": "2026-08-20T12:00:00Z"},
			{"window": "30d", "percent": 10}
		]
	}`)
	snapshot := ParseOpencodeUsage(body)
	if snapshot == nil {
		t.Fatal("expected snapshot to be parsed")
	}
	if snapshot.Window5h == nil || snapshot.Window5h.Percent == nil || *snapshot.Window5h.Percent != 50 {
		t.Fatalf("window5h = %+v", snapshot.Window5h)
	}
	if snapshot.Window7d == nil || snapshot.Window7d.Percent == nil || *snapshot.Window7d.Percent != 75 {
		t.Fatalf("window7d = %+v", snapshot.Window7d)
	}
	if snapshot.Window30d == nil || snapshot.Window30d.Percent == nil || *snapshot.Window30d.Percent != 10 {
		t.Fatalf("window30d = %+v", snapshot.Window30d)
	}
}

func TestParseOpencodeUsageNamedFields(t *testing.T) {
	body := []byte(`{
		"five_hour": {"used_percent": 40, "resetsAt": "2026-08-16T12:00:00Z"},
		"weekly": {"percent": 60},
		"monthly": {"percent": 80}
	}`)
	snapshot := ParseOpencodeUsage(body)
	if snapshot == nil {
		t.Fatal("expected snapshot to be parsed")
	}
	if snapshot.Window5h == nil || *snapshot.Window5h.Percent != 40 {
		t.Fatalf("window5h = %+v", snapshot.Window5h)
	}
	if snapshot.Window7d == nil || *snapshot.Window7d.Percent != 60 {
		t.Fatalf("window7d = %+v", snapshot.Window7d)
	}
	if snapshot.Window30d == nil || *snapshot.Window30d.Percent != 80 {
		t.Fatalf("window30d = %+v", snapshot.Window30d)
	}
}

func TestParseOpencodeUsageRealStructure(t *testing.T) {
	// 真实响应（2026-08-16 实测 GET /zen/go/v1/usage）。
	body := []byte(`{"usage":{"rolling":{"status":"ok","percent":45,"resetsAt":"2026-08-16T08:22:05Z"},"weekly":{"status":"ok","percent":80,"resetsAt":"2026-08-17T00:00:00Z"},"monthly":{"status":"ok","percent":0,"resetsAt":"2026-09-15T16:47:49Z"}}}`)
	snapshot := ParseOpencodeUsage(body)
	if snapshot == nil {
		t.Fatal("expected snapshot parsed from real structure")
	}
	if snapshot.Window5h == nil || snapshot.Window5h.Percent == nil || *snapshot.Window5h.Percent != 45 {
		t.Fatalf("window5h = %+v, want percent 45", snapshot.Window5h)
	}
	if snapshot.Window7d == nil || snapshot.Window7d.Percent == nil || *snapshot.Window7d.Percent != 80 {
		t.Fatalf("window7d = %+v, want percent 80", snapshot.Window7d)
	}
	if snapshot.Window30d == nil || snapshot.Window30d.Percent == nil || *snapshot.Window30d.Percent != 0 {
		t.Fatalf("window30d = %+v, want percent 0", snapshot.Window30d)
	}
	if snapshot.Window5h.ResetsAt == nil {
		t.Fatal("expected window5h resetsAt parsed")
	}
}

func TestParseOpencodeUsageDefensiveOnEmpty(t *testing.T) {
	if snapshot := ParseOpencodeUsage([]byte(`{"unrelated": 1}`)); snapshot != nil {
		t.Fatal("expected nil snapshot for unrecognized payload")
	}
	if snapshot := ParseOpencodeUsage([]byte(`not json`)); snapshot != nil {
		t.Fatal("expected nil snapshot for invalid json")
	}
	if snapshot := ParseOpencodeUsage(nil); snapshot != nil {
		t.Fatal("expected nil snapshot for nil body")
	}
}

func TestBuildOpencodeUsageExtraUpdates(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	snapshot := &OpencodeUsageSnapshot{
		UpdatedAt: now.Format(time.RFC3339),
		Window5h:  &OpencodeUsageWindow{Window: OpencodeQuotaWindow5h, Percent: floatPtr(50), ResetsAt: &now},
	}
	updates := buildOpencodeUsageExtraUpdates(snapshot, now)
	if updates == nil {
		t.Fatal("expected extra updates")
	}
	if got := updates["opencode_5h_used_percent"]; got != 50.0 {
		t.Fatalf("5h used percent = %v, want 50", got)
	}
	if got := updates["opencode_5h_reset_at"]; got != now.Format(time.RFC3339) {
		t.Fatalf("5h reset at = %v", got)
	}
	if _, ok := updates["opencode_usage_updated_at"]; !ok {
		t.Fatal("expected opencode_usage_updated_at key")
	}
}

func TestOpencodeTLSFingerprintAndUserAgent(t *testing.T) {
	opencode := &Account{Platform: PlatformOpencode, Type: AccountTypeAPIKey}
	if !opencode.IsTLSFingerprintEnabled() {
		t.Fatal("opencode account should enable TLS fingerprint by default")
	}
	if got := opencode.GetOpenAIUserAgent(); got != "opencode/1.0" {
		t.Fatalf("opencode user agent = %q, want opencode/1.0", got)
	}

	// OpenAI 平台保持原语义：默认不启用指纹、UA 从凭证读取。
	openai := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	if openai.IsTLSFingerprintEnabled() {
		t.Fatal("openai account should not enable TLS fingerprint by default")
	}
	if openai.GetOpenAIUserAgent() != "" {
		t.Fatalf("openai user agent should be empty, got %q", openai.GetOpenAIUserAgent())
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestDeriveOpencodeAPIKeyImportName(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"sk-abcdefghijklmnop", "sk-abc**nop"},
		{"abcdefghijklmnop", "sk-abc**nop"},
		{"sk-abcdefgh", "sk-abc**fgh"},
		{"sk-abc", "sk-abc"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := DeriveOpencodeAPIKeyImportName(tc.key); got != tc.want {
			t.Fatalf("DeriveOpencodeAPIKeyImportName(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestParseOpencodeCredentialImportContents(t *testing.T) {
	sources, errs := ParseOpencodeCredentialImportContents([]string{
		"sk-abcdefghijklmnop\nsk-qrstuvwxyz123456",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(sources))
	}
	first := sources[0]
	if first.Kind != AccountCredentialImportKindOpencodeAPIKey {
		t.Fatalf("kind = %q, want %q", first.Kind, AccountCredentialImportKindOpencodeAPIKey)
	}
	if first.Platform != PlatformOpencode {
		t.Fatalf("platform = %q, want %q", first.Platform, PlatformOpencode)
	}
	if first.Token != "sk-abcdefghijklmnop" {
		t.Fatalf("token = %q", first.Token)
	}
	if first.Name != "sk-abc**nop" {
		t.Fatalf("name = %q, want sk-abc**nop", first.Name)
	}
}

func TestRefreshOpencodeUsageIfStale_Guards(t *testing.T) {
	svc := &AccountUsageService{
		cache:       NewUsageCache(),
		accountRepo: &accountUsageCodexProbeRepo{},
	}

	// 非 opencode 账号：不进 probe 门（throttle 不记录）。
	svc.refreshOpencodeUsageIfStale(context.Background(), &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
	})
	if _, found := svc.cache.openAIProbeCache.Load(int64(1)); found {
		t.Fatal("non-opencode account must not enter probe gate")
	}

	// 非 stale 的 opencode 账号：不进 probe 门。
	fresh := time.Now().UTC().Format(time.RFC3339)
	svc.refreshOpencodeUsageIfStale(context.Background(), &Account{
		ID: 2, Platform: PlatformOpencode, Type: AccountTypeAPIKey,
		Extra: map[string]any{"opencode_usage_updated_at": fresh},
	})
	if _, found := svc.cache.openAIProbeCache.Load(int64(2)); found {
		t.Fatal("non-stale opencode account must not enter probe gate")
	}

	// stale 的 opencode 账号：进入 probe 门（throttle 记录时间戳）。
	// 拉取会因无 api_key 而短路失败，但守卫已放行——这正是同步刷新的触发点。
	svc.refreshOpencodeUsageIfStale(context.Background(), &Account{
		ID: 3, Platform: PlatformOpencode, Type: AccountTypeAPIKey,
		Extra: map[string]any{},
	})
	if _, found := svc.cache.openAIProbeCache.Load(int64(3)); !found {
		t.Fatal("stale opencode account must enter probe gate")
	}
}
