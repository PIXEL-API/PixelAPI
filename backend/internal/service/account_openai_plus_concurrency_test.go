package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIPlusConcurrency_DefaultAndMax(t *testing.T) {
	got, err := NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, 0)
	require.NoError(t, err)
	require.Equal(t, OpenAIPlusDefaultConcurrency, got)

	got, err = NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, OpenAIPlusMaxConcurrency)
	require.NoError(t, err)
	require.Equal(t, OpenAIPlusMaxConcurrency, got)

	_, err = NormalizeOpenAIPlusConcurrency(PlatformOpenAI, AccountLevelPlus, OpenAIPlusMaxConcurrency+1)
	require.Error(t, err)
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
	require.Equal(t, AccountLevelPlus, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPro,
		map[string]any{"plan_type": "plus"},
		nil,
	))
	require.Equal(t, AccountLevelPro, NormalizeOpenAIAccountLevel(
		PlatformOpenAI,
		AccountLevelPro,
		nil,
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

func TestValidateOpenAIPlusLoadFactor_Max(t *testing.T) {
	loadFactor := OpenAIPlusMaxConcurrency + 1
	require.Error(t, ValidateOpenAIPlusLoadFactor(PlatformOpenAI, AccountLevelPlus, &loadFactor))

	require.NoError(t, ValidateOpenAIPlusLoadFactor(PlatformOpenAI, AccountLevelPro, &loadFactor))
	require.NoError(t, ValidateOpenAIPlusLoadFactor(PlatformAnthropic, AccountLevelPlus, &loadFactor))
}
