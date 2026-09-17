//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultPrivateGroupAllowMessagesDispatch(t *testing.T) {
	require.True(t, defaultPrivateGroupAllowMessagesDispatch(PlatformOpenAI))
	require.True(t, defaultPrivateGroupAllowMessagesDispatch(" OpenAI "))
	require.True(t, defaultPrivateGroupAllowMessagesDispatch(PlatformOpencode))
	require.True(t, defaultPrivateGroupAllowMessagesDispatch(" opencode "))

	require.False(t, defaultPrivateGroupAllowMessagesDispatch(PlatformAnthropic))
	require.False(t, defaultPrivateGroupAllowMessagesDispatch(PlatformGemini))
	require.False(t, defaultPrivateGroupAllowMessagesDispatch(PlatformAntigravity))
	require.False(t, defaultPrivateGroupAllowMessagesDispatch(PlatformGrok))
}

func TestSupportedUserPrivateGroupPlatformsIncludesAllAccountPlatforms(t *testing.T) {
	require.Equal(t, []string{
		PlatformAnthropic,
		PlatformOpenAI,
		PlatformGemini,
		PlatformAntigravity,
		PlatformGrok,
		PlatformOpencode,
		PlatformKimi,
		PlatformZhipu,
		PlatformDeepseek,
		PlatformMiniMax,
		PlatformQwen,
		PlatformDevin,
		PlatformAPIAggregation,
	}, SupportedUserPrivateGroupPlatforms())
	require.True(t, IsSupportedUserPrivateGroupPlatform(PlatformGrok))
	require.True(t, IsSupportedUserPrivateGroupPlatform(" Grok "))
	require.True(t, IsSupportedUserPrivateGroupPlatform(PlatformOpencode))
	require.True(t, IsSupportedUserPrivateGroupPlatform(PlatformDevin))
	// Devin 桥接只实现 /v1/chat/completions，私有分组不放行 /v1/messages 调度。
	require.False(t, defaultPrivateGroupAllowMessagesDispatch(PlatformDevin))
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformQwen, PlatformAPIAggregation} {
		require.True(t, IsSupportedUserPrivateGroupPlatform(platform))
		require.True(t, defaultPrivateGroupAllowMessagesDispatch(platform))
	}
	for _, platform := range SupportedUserPrivateGroupPlatforms() {
		require.True(t, IsSupportedAccountPlatform(platform))
	}
	require.False(t, IsSupportedAccountPlatform("unsupported"))
}

func TestBuildUserPrivateGroupUsesPositiveNewUserRateMultiplierDefault(t *testing.T) {
	group := buildUserPrivateGroup(42, PlatformOpenAI, &UserPrivateGroupTemplate{
		RateMultiplier: 1,
	})

	require.Equal(t, 1.0, group.NewUserRateMultiplier)
	require.False(t, group.NewUserRateEnabled)
}
