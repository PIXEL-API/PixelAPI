package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIPlusConcurrency_DefaultAndAdminConfiguredValue(t *testing.T) {
	got, err := NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, 0)
	require.NoError(t, err)
	require.Equal(t, OpenAIPlusDefaultConcurrency, got)

	got, err = NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, 5)
	require.NoError(t, err)
	require.Equal(t, 5, got)
}

func TestDefaultOAuthAccountConcurrencyForPlatform(t *testing.T) {
	require.Equal(t, OpenAIPlusDefaultConcurrency, DefaultOAuthAccountConcurrencyForPlatform(PlatformOpenAI))
	require.Equal(t, OAuthAccountDefaultConcurrency, DefaultOAuthAccountConcurrencyForPlatform(PlatformAnthropic))
	require.Equal(t, OAuthAccountDefaultConcurrency, DefaultOAuthAccountConcurrencyForPlatform(PlatformGemini))
}

func TestNormalizeOpenAIAccountLevel_FromPlanType(t *testing.T) {
	require.Equal(t, AccountLevelPlus, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "plus"},
		nil,
	))
	require.Equal(t, AccountLevelK12, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "k12"},
		nil,
	))
	require.Equal(t, AccountLevelK12, NormalizeOpenAIPlanAccountLevel("chatgpt-k12"))
}

func TestOpenAIAccountLevelConfigsDynamicAliasAndDisabledHistory(t *testing.T) {
	configs, err := ValidateOpenAIAccountLevelConfigs([]OpenAIAccountLevelConfig{
		{Key: "student", Label: "Student", Aliases: []string{"edu-plan", "chatgptstudent*"}, Enabled: true, SortOrder: 10},
		{Key: "legacy", Label: "Legacy", Aliases: []string{"legacy"}, Enabled: false, SortOrder: 20},
	})
	require.NoError(t, err)

	require.Equal(t, "student", NormalizeOpenAIPlanAccountLevelWithConfigs("chatgptstudent-v2", configs))
	require.Equal(t, "student", NormalizeOpenAIAccountLevelWithConfigs(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"plan_type": "edu-plan"},
		nil,
		configs,
	))
	require.Equal(t, "legacy", NormalizeOpenAIAccountLevelWithConfigs(
		PlatformOpenAI,
		"legacy",
		map[string]any{"plan_type": "edu-plan"},
		nil,
		configs,
	))
	require.NoError(t, ValidateConfiguredOpenAIAccountLevel(PlatformOpenAI, "legacy", configs))
	require.False(t, IsUserSelectableOpenAIAccountLevelWithConfigs("legacy", configs))
}

func TestValidateOpenAIAccountLevelConfigsRejectsAliasConflict(t *testing.T) {
	_, err := ValidateOpenAIAccountLevelConfigs([]OpenAIAccountLevelConfig{
		{Key: "alpha", Label: "Alpha", Aliases: []string{"shared-alias"}, Enabled: true, SortOrder: 10},
		{Key: "beta", Label: "Beta", Aliases: []string{"shared_alias"}, Enabled: true, SortOrder: 20},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "sharedalias")
}

func TestNormalizeOpenAIAccountLevel_ManualLevelTakesPriority(t *testing.T) {
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPro,
		map[string]any{"plan_type": "plus"},
		nil,
	))
	require.Equal(t, AccountLevelUnknown, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelUnknown,
		map[string]any{"account_level": "pro"},
		nil,
	))
	require.Equal(t, AccountLevelUnknown, NormalizeOpenAIAccountLevel(
		PlatformAnthropic,
		AccountLevelUnknown,
		map[string]any{"plan_type": "plus"},
		nil,
	))
}

func TestValidateAccountLoadFactor_Max(t *testing.T) {
	loadFactor := AccountMaxLoadFactor + 1
	require.Error(t, ValidateAccountLoadFactor(&loadFactor))

	loadFactor = AccountMaxLoadFactor
	require.NoError(t, ValidateAccountLoadFactor(&loadFactor))
}
