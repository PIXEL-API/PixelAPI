package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The upstream Codex facade interprets raw strings as access tokens. This test
// freezes the established local endpoint profile: raw /import-credentials text
// remains an OpenAI refresh token and must not change when the facade is added.
func TestAccountCredentialImportRawTextRemainsOpenAIRefreshToken(t *testing.T) {
	sources, errs := ParseAccountCredentialImportContents([]string{"local-refresh-token"})

	require.Empty(t, errs)
	require.Len(t, sources, 1)
	require.Equal(t, AccountCredentialImportKindOpenAIRefreshToken, sources[0].Kind)
	require.Equal(t, "local-refresh-token", sources[0].Token)
}
