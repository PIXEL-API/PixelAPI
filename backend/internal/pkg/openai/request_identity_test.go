package openai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPairCodexClientIdentity(t *testing.T) {
	tests := []struct {
		name           string
		ua             string
		wantOriginator string
		wantUA         string
		wantOK         bool
	}{
		{
			name:           "cli identity",
			ua:             "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			wantOK:         true,
		},
		{
			name:           "tui identity",
			ua:             "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)",
			wantOriginator: "codex-tui",
			wantUA:         "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)",
			wantOK:         true,
		},
		{
			name:           "Codex family preserves case",
			ua:             "Codex Desktop/1.2.3",
			wantOriginator: "Codex Desktop",
			wantUA:         "Codex Desktop/1.2.3",
			wantOK:         true,
		},
		{
			name:           "override trailer restores identity",
			ua:             "cccc/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
			wantOriginator: "codex-tui",
			wantUA:         "codex-tui/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
			wantOK:         true,
		},
		{
			name:           "known identity is canonicalized",
			ua:             "CODEX_CLI_RS/1.0.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/1.0.0",
			wantOK:         true,
		},
		{name: "trailer slash rejected", ua: "foo/1.0 (Codex Desktop/2; 1.0)"},
		{name: "control byte rejected", ua: "Codex \x01evil/1.0.0"},
		{name: "non ASCII rejected", ua: "Codex évil/1.0.0"},
		{name: "overlong identity rejected", ua: "Codex " + strings.Repeat("a", 80) + "/1.0.0"},
		{name: "third party rejected", ua: "luna/1.0.0"},
		{name: "forged codex prefix rejected", ua: "codex_evil/1.0.0"},
		{name: "forged known prefix rejected", ua: "codex_cli_rs_evil/1.0.0"},
		{name: "browser rejected", ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
		{name: "missing slash rejected", ua: "curl"},
		{name: "empty rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originator, pairedUA, ok := PairCodexClientIdentity(tt.ua)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantOriginator, originator)
			require.Equal(t, tt.wantUA, pairedUA)
		})
	}
}

func TestIsCodexOfficialClientOriginatorRejectsForgedPrefixes(t *testing.T) {
	require.True(t, IsCodexOfficialClientOriginator("codex-tui"))
	require.True(t, IsCodexOfficialClientOriginator("Codex Desktop"))
	require.False(t, IsCodexOfficialClientOriginator("codex_evil"))
	require.False(t, IsCodexOfficialClientOriginator("evil-codex_cli_rs"))
}
