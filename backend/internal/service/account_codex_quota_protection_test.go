package service

import (
	"testing"
	"time"
)

func TestAccountCodexQuotaProtectionUsesConfigured5hLimit(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_limit_percent": 80.0,
			"codex_5h_used_percent":  80.0,
			"codex_5h_reset_at":      resetAt.Format(time.RFC3339),
		},
	}

	if !account.IsCodexQuotaProtectionActiveAt(now) {
		t.Fatal("expected codex quota protection to be active")
	}
	if got := account.CodexQuotaProtectionReasonAt(now); got != CodexQuotaWindow5h {
		t.Fatalf("reason = %q, want %q", got, CodexQuotaWindow5h)
	}
	if account.IsSchedulableAt(now) {
		t.Fatal("expected account to be unschedulable while quota protection is active")
	}
	if got := account.CodexQuotaProtectionResetAt(now); got == nil || !got.Equal(resetAt) {
		t.Fatalf("reset_at = %v, want %v", got, resetAt)
	}
}

func TestAccountCodexQuotaProtectionUsesLatestResetWhenBothWindowsProtected(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	fiveHourResetAt := now.Add(2 * time.Hour)
	sevenDayResetAt := now.Add(24 * time.Hour)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_limit_percent": 80.0,
			"codex_5h_used_percent":  90.0,
			"codex_5h_reset_at":      fiveHourResetAt.Format(time.RFC3339),
			"codex_7d_limit_percent": 80.0,
			"codex_7d_used_percent":  90.0,
			"codex_7d_reset_at":      sevenDayResetAt.Format(time.RFC3339),
		},
	}

	if got := account.CodexQuotaProtectionReasonAt(now); got != CodexQuotaWindow7d {
		t.Fatalf("reason = %q, want %q", got, CodexQuotaWindow7d)
	}
	if got := account.CodexQuotaProtectionResetAt(now); got == nil || !got.Equal(sevenDayResetAt) {
		t.Fatalf("reset_at = %v, want %v", got, sevenDayResetAt)
	}
}

func TestAccountCodexQuotaProtectionDefaultsTo100Percent(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_7d_used_percent": 99.9,
			"codex_7d_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
		},
	}

	if account.GetCodex7dLimitPercent() != CodexQuotaDefaultLimitPercent {
		t.Fatalf("default limit = %v, want %v", account.GetCodex7dLimitPercent(), CodexQuotaDefaultLimitPercent)
	}
	if account.IsCodexQuotaProtectionActiveAt(now) {
		t.Fatal("did not expect protection below default 100% limit")
	}

	account.Extra["codex_7d_used_percent"] = 100.0
	if !account.IsCodexQuotaProtectionActiveAt(now) {
		t.Fatal("expected protection at default 100% limit")
	}
}

func TestAccountCodexQuotaProtectionIgnoresExpiredWindow(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_limit_percent": 80.0,
			"codex_5h_used_percent":  95.0,
			"codex_5h_reset_at":      now.Add(-time.Minute).Format(time.RFC3339),
		},
	}

	if account.IsCodexQuotaProtectionActiveAt(now) {
		t.Fatal("did not expect protection after reset time")
	}
	if !account.IsSchedulableAt(now) {
		t.Fatal("expected account to be schedulable after reset time")
	}
}

func TestAccountSchedulableWithoutCodexQuotaProtection(t *testing.T) {
	now := time.Now().UTC()
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_limit_percent": 80.0,
			"codex_5h_used_percent":  81.0,
			"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
		},
	}

	if account.IsSchedulableAt(now) {
		t.Fatal("expected account to be blocked by codex quota protection")
	}
	if !account.IsSchedulableWithoutCodexQuotaProtection() {
		t.Fatal("expected account to remain eligible for scheduler snapshot storage")
	}
}

func TestNormalizeCodexQuotaLimitExtra(t *testing.T) {
	extra, err := NormalizeCodexQuotaLimitExtra(PlatformOpenAI, AccountTypeOAuth, map[string]any{
		"codex_5h_limit_percent": "80",
		"codex_7d_limit_percent": 100.0,
	})
	if err != nil {
		t.Fatalf("NormalizeCodexQuotaLimitExtra returned error: %v", err)
	}
	if got := extra["codex_5h_limit_percent"]; got != 80.0 {
		t.Fatalf("5h limit = %v, want 80", got)
	}
	if _, ok := extra["codex_7d_limit_percent"]; ok {
		t.Fatal("expected default 100% limit to be omitted")
	}

	if _, err := NormalizeCodexQuotaLimitExtra(PlatformOpenAI, AccountTypeOAuth, map[string]any{
		"codex_5h_limit_percent": 0,
	}); err == nil {
		t.Fatal("expected invalid zero limit to fail")
	}
}
