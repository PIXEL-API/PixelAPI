package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeOpenAICodexPATMetadata(t *testing.T) {
	input := map[string]any{
		"accessToken": "at-test-token",
		"metadata": map[string]any{
			"refresh-token": "stale-refresh-token",
			"token_copy":    "Bearer at-test-token",
			"keep":          "safe",
		},
		"agent.private.key": "must-not-survive",
		"model_mapping": map[string]any{
			"gpt-5": "gpt-5-codex",
		},
		"values": []any{"at-test-token", "safe", map[string]any{
			"chatgptAccountId": "untrusted-account",
			"keep":             true,
		}},
	}

	got := sanitizeOpenAICodexPATMetadata(input, "at-test-token")

	require.NotContains(t, got, "accessToken")
	require.NotContains(t, got, "agent.private.key")
	require.Equal(t, map[string]any{"keep": "safe"}, got["metadata"])
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-codex"}, got["model_mapping"])
	require.Equal(t, []any{"safe", map[string]any{"keep": true}}, got["values"])
}

func TestSanitizeOpenAICodexPATMetadataReturnsNilForProtectedOnlyInput(t *testing.T) {
	got := sanitizeOpenAICodexPATMetadata(map[string]any{
		"openai_auth_mode": "personal_access_token",
		"nested": map[string]any{
			"token": "at-test-token",
		},
	}, "at-test-token")

	require.Nil(t, got)
}
