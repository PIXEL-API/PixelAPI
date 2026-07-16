package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureCodexIdentityHeaders(t *testing.T) {
	headers := make(http.Header)

	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
	require.Equal(t, codexCLIUserAgent, headers.Get("user-agent"))
	require.Equal(t, codexCLIVersion, headers.Get("version"))
	require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
}

func TestEnsureCodexIdentityHeadersPreservesFinalOfficialUserAgent(t *testing.T) {
	const tuiUA = "codex-tui/9.9.9 (Mac OS X 14.0; arm64) iTerm (codex-tui; 9.9.9)"
	headers := make(http.Header)
	headers.Set("user-agent", tuiUA)
	headers.Set("version", "9.9.9")
	headers.Set("OpenAI-Beta", "assistants=v2")

	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex-tui", headers.Get("originator"))
	require.Equal(t, tuiUA, headers.Get("user-agent"))
	require.Equal(t, "9.9.9", headers.Get("version"))
	require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
}

func TestEnforceCodexIdentityHeaders(t *testing.T) {
	const tuiUA = "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)"
	tests := []struct {
		name           string
		originator     string
		userAgent      string
		version        string
		wantOriginator string
		wantUA         string
		wantVersion    string
	}{
		{
			name:           "originator follows final official UA",
			originator:     "codex_cli_rs",
			userAgent:      tuiUA,
			wantOriginator: "codex-tui",
			wantUA:         tuiUA,
		},
		{
			name:           "third party identity falls back as a pair",
			originator:     "opencode",
			userAgent:      "luna/1.0.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         codexCLIUserAgent,
		},
		{
			name:           "missing UA falls back as a pair",
			originator:     "codex_vscode",
			wantOriginator: "codex_cli_rs",
			wantUA:         codexCLIUserAgent,
		},
		{
			name:           "trailer restores overridden identity",
			originator:     "cccc",
			userAgent:      "cccc/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
			wantOriginator: "codex-tui",
			wantUA:         "codex-tui/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
		},
		{
			name:           "low version is raised",
			originator:     "codex_cli_rs",
			userAgent:      "codex_cli_rs/0.125.0",
			version:        "0.125.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/0.125.0",
			wantVersion:    codexCLIVersion,
		},
		{
			name:           "supported version is preserved",
			originator:     "codex_cli_rs",
			userAgent:      "codex_cli_rs/0.145.0",
			version:        "0.145.0",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/0.145.0",
			wantVersion:    "0.145.0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("originator", test.originator)
			headers.Set("user-agent", test.userAgent)
			headers.Set("version", test.version)

			enforceCodexIdentityHeaders(headers)

			require.Equal(t, test.wantOriginator, headers.Get("originator"))
			require.Equal(t, test.wantUA, headers.Get("user-agent"))
			require.Equal(t, test.wantVersion, headers.Get("version"))
		})
	}
}

func TestEnforceCodexIdentityHeadersWithoutOriginatorIsNoop(t *testing.T) {
	headers := make(http.Header)
	headers.Set("user-agent", "third-party-client/1.0.0")

	enforceCodexIdentityHeaders(headers)

	require.Empty(t, headers.Get("originator"))
	require.Equal(t, "third-party-client/1.0.0", headers.Get("user-agent"))
}
