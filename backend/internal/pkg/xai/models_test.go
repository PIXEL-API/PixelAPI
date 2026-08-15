package xai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokModelHelpers(t *testing.T) {
	t.Parallel()

	require.True(t, IsGrokModelID("x-ai/grok-4.3"))
	require.False(t, IsGrokModelID("gpt-5"))
	require.True(t, IsGrokTextResponsesModelID("grok/grok-4.20-multi-agent"))
	require.False(t, IsGrokTextResponsesModelID("grok-imagine-video"))
	require.Equal(t, "grok-4.3", ResolveGrokTextResponsesModelID("grok", "grok-4.3"))
	require.Equal(t, "grok-4.20-multi-agent-0309", ResolveGrokTextResponsesModelID("xai/grok-4.20-multi-agent"))
}

func TestCanonicalImagineVideoModel(t *testing.T) {
	t.Parallel()

	require.Equal(t, DefaultImagineVideoModel, CanonicalImagineVideoModel("grok-imagine-video"))
	require.Equal(t, DefaultImagineVideo15Model, CanonicalImagineVideoModel("grok-imagine-video-1.5"))
	require.Equal(t, DefaultImagineVideo15Model, CanonicalImagineVideoModel("grok-imagine-video-1.5-preview"))
	require.Equal(t, DefaultImagineVideo15Model, CanonicalImagineVideoModel("xai/grok-video-1.5"))
	require.Equal(t, "grok-imagine-video-2", CanonicalImagineVideoModel("grok-imagine-video-2"))
}
