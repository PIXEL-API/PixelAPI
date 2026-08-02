//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

// 分组刚建好还没绑账号时 GetAvailableModels 返回空，Models 会走兜底。
// 兜底必须按分组平台分流，否则 grok/gemini/antigravity 分组拉到的是一串
// 根本调不通的 claude-* 模型，看起来像分组平台配错了。
func TestDefaultModelsForPlatform_UsesPlatformOwnList(t *testing.T) {
	t.Parallel()

	require.Equal(t, openai.DefaultModels, defaultModelsForPlatform(service.PlatformOpenAI))
	require.Equal(t, xai.DefaultModels(), defaultModelsForPlatform(service.PlatformGrok))
	require.Equal(t, antigravity.DefaultModels(), defaultModelsForPlatform(service.PlatformAntigravity))

	// Anthropic 与未知平台维持原有行为。
	require.Equal(t, claude.DefaultModels, defaultModelsForPlatform(service.PlatformAnthropic))
	require.Equal(t, claude.DefaultModels, defaultModelsForPlatform(""))
	require.Equal(t, claude.DefaultModels, defaultModelsForPlatform("some-future-platform"))
}

func TestDefaultModelsForPlatform_GrokNeverReturnsClaudeModels(t *testing.T) {
	t.Parallel()

	models, ok := defaultModelsForPlatform(service.PlatformGrok).([]xai.Model)
	require.True(t, ok, "grok fallback must keep the OpenAI-compatible shape")
	require.NotEmpty(t, models)

	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	require.Contains(t, ids, "grok-4.5")
	for _, claudeModel := range claude.DefaultModels {
		require.NotContains(t, ids, claudeModel.ID)
	}
}

func TestDefaultModelsForPlatform_GeminiMirrorsV1BetaFallback(t *testing.T) {
	t.Parallel()

	models, ok := defaultModelsForPlatform(service.PlatformGemini).([]claude.Model)
	require.True(t, ok, "gemini fallback on the Claude-compatible endpoint must use the Claude shape")
	require.Len(t, models, len(gemini.DefaultModels()))

	ids := make([]string, 0, len(models))
	for _, model := range models {
		require.Equal(t, "model", model.Type)
		require.NotEmpty(t, model.DisplayName)
		require.NotContains(t, model.ID, "models/", "the models/ prefix must be stripped")
		ids = append(ids, model.ID)
	}
	require.Contains(t, ids, "gemini-2.5-pro")
	for _, claudeModel := range claude.DefaultModels {
		require.NotContains(t, ids, claudeModel.ID)
	}
}

// 平台自己的兜底列表不能是空的，否则客户端会拿到一个空模型下拉框。
func TestDefaultModelsForPlatform_AllKnownPlatformsAreNonEmpty(t *testing.T) {
	t.Parallel()

	for _, platform := range []string{
		service.PlatformAnthropic,
		service.PlatformOpenAI,
		service.PlatformGemini,
		service.PlatformAntigravity,
		service.PlatformGrok,
	} {
		require.NotEmpty(t, defaultModelsForPlatform(platform), "platform %s has an empty fallback model list", platform)
	}
}
