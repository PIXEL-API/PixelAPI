//go:build unit

package service

import (
	"context"
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

func TestOpencodeSupportsAnthropicMessagesFormat(t *testing.T) {
	if opencodeSupportsAnthropicMessagesFormat("grok-4.5") {
		t.Fatal("grok-4.5 must be treated as chat-only (not anthropic-messages capable)")
	}
	if opencodeSupportsAnthropicMessagesFormat("grok-4.5[1m]") {
		t.Fatal("grok-4.5[1m] should still route to chat-completions conversion")
	}
	if !opencodeSupportsAnthropicMessagesFormat("deepseek-v4-flash") {
		t.Fatal("deepseek-v4-flash should keep the native anthropic messages path")
	}
	if !opencodeSupportsAnthropicMessagesFormat("") {
		t.Fatal("empty model should keep the native anthropic messages path")
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
