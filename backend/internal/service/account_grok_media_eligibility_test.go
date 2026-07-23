package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestGrokMediaGenerationEligibility(t *testing.T) {
	weeklyUsagePercent := 12.5
	paidBilling := &xai.BillingSummary{
		UsagePercent:     &weeklyUsagePercent,
		StatusCode:       http.StatusOK,
		WeeklyStatusCode: http.StatusOK,
	}
	freeBilling := &xai.BillingSummary{
		StatusCode:        http.StatusOK,
		WeeklyStatusCode:  http.StatusOK,
		MonthlyStatusCode: http.StatusOK,
		MonthlyUpdatedAt:  "2026-07-17T00:00:00Z",
	}
	inconclusiveBilling := &xai.BillingSummary{
		StatusCode:        http.StatusOK,
		WeeklyStatusCode:  http.StatusOK,
		MonthlyStatusCode: http.StatusBadGateway,
		Partial:           true,
		FailedWindows:     []string{"monthly"},
	}

	tests := []struct {
		name       string
		account    *Account
		want       bool
		wantReason string
	}{
		{name: "nil account", account: nil, wantReason: "not_grok"},
		{name: "non grok", account: &Account{Platform: PlatformOpenAI}, wantReason: "not_grok"},
		{name: "non oauth", account: &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}, want: true, wantReason: "non_oauth"},
		{name: "oauth unobserved", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}, wantReason: "billing_unobserved"},
		{name: "paid evidence", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: paidBilling}}, want: true, wantReason: "eligible"},
		{name: "free tier", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: freeBilling}}, wantReason: "billing_free_tier"},
		{name: "inconclusive", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: inconclusiveBilling}}, wantReason: "billing_inconclusive"},
		{name: "aggregate forbidden", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: &xai.BillingSummary{StatusCode: http.StatusForbidden}}}, wantReason: "billing_forbidden"},
		{name: "weekly forbidden", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: &xai.BillingSummary{StatusCode: http.StatusOK, WeeklyStatusCode: http.StatusForbidden, MonthlyStatusCode: http.StatusOK}}}, wantReason: "billing_forbidden"},
		{name: "monthly forbidden", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: &xai.BillingSummary{StatusCode: http.StatusOK, WeeklyStatusCode: http.StatusOK, MonthlyStatusCode: http.StatusForbidden}}}, wantReason: "billing_forbidden"},
		{name: "malformed snapshot", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{grokBillingExtraKey: make(chan int)}}, wantReason: "billing_unobserved"},
		{name: "malformed override ignored", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{GrokMediaEligibleExtraKey: "true", grokBillingExtraKey: paidBilling}}, want: true, wantReason: "eligible"},
		{name: "override disabled", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{GrokMediaEligibleExtraKey: false}}, wantReason: "override_disabled"},
		{name: "override enabled", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{GrokMediaEligibleExtraKey: true, grokBillingExtraKey: &xai.BillingSummary{StatusCode: http.StatusForbidden}}}, want: true, wantReason: "override_enabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eligible, reason := tt.account.GrokMediaGenerationEligibility()
			require.Equal(t, tt.want, eligible)
			require.Equal(t, tt.wantReason, reason)
		})
	}
}

func TestGrokMediaEndpointCapabilityKeepsOnlyUnobservedOAuthAsProbeCandidate(t *testing.T) {
	unobserved := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	require.True(t, unobserved.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration))

	inconclusive := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{grokBillingExtraKey: &xai.BillingSummary{
			StatusCode: http.StatusOK,
			Partial:    true,
		}},
	}
	require.False(t, inconclusive.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration))
	require.False(t, (&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration))
}
