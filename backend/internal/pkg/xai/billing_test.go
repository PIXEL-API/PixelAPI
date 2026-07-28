package xai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBillingURLWithValidator(t *testing.T) {
	weeklyURL, err := BuildBillingURLWithValidator(DefaultCLIBaseURL, true, ValidateTrustedBaseURL)
	require.NoError(t, err)
	require.Equal(t, DefaultCLIBaseURL+BillingWeeklyPath, weeklyURL)

	monthlyURL, err := BuildBillingURLWithValidator(
		"https://relay.example.test/tenant/xai/v1",
		false,
		ValidateBaseURL,
	)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/tenant/xai/v1"+BillingMonthlyPath, monthlyURL)

	_, err = BuildBillingURLWithValidator("https://relay.example.test/v1", true, ValidateTrustedBaseURL)
	require.Error(t, err)
}
