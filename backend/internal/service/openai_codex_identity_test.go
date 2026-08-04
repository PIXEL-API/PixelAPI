package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
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
	const vscodeUA = "codex_vscode/9.9.9 (Mac OS X 14.0; arm64) vscode (codex_vscode; 9.9.9)"
	headers := make(http.Header)
	headers.Set("user-agent", vscodeUA)
	headers.Set("version", "9.9.9")
	headers.Set("OpenAI-Beta", "assistants=v2")

	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex_vscode", headers.Get("originator"))
	require.Equal(t, vscodeUA, headers.Get("user-agent"))
	require.Equal(t, "9.9.9", headers.Get("version"))
	require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
}

// 降载桶身份（codex-tui）出站前会被改写为 CLI 身份，但版本 / OS / 架构 / 终端指纹保留。
func TestEnsureCodexIdentityHeadersNormalizesLoadShedIdentity(t *testing.T) {
	headers := make(http.Header)
	headers.Set("user-agent", "codex-tui/9.9.9 (Mac OS X 14.0; arm64) iTerm (codex-tui; 9.9.9)")
	headers.Set("version", "9.9.9")

	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
	require.Equal(t, "codex_cli_rs/9.9.9 (Mac OS X 14.0; arm64) iTerm", headers.Get("user-agent"))
	require.Equal(t, "9.9.9", headers.Get("version"))
}

func TestEnforceCodexIdentityHeaders(t *testing.T) {
	const tuiUA = "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)"
	// codex-tui 落在上游降载桶，收口时统一改写为 CLI 身份（保留版本/OS/架构/终端指纹）。
	const tuiNormalizedUA = "codex_cli_rs/0.140.2 (Mac OS X 14.0; arm64) iTerm"
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
			name:           "originator follows final official UA then normalizes",
			originator:     "codex_cli_rs",
			userAgent:      tuiUA,
			wantOriginator: "codex_cli_rs",
			wantUA:         tuiNormalizedUA,
		},
		{
			name:           "load-shed identity is rewritten to CLI identity",
			originator:     "codex-tui",
			userAgent:      tuiUA,
			wantOriginator: "codex_cli_rs",
			wantUA:         tuiNormalizedUA,
		},
		{
			name:           "non load-shed official identity is preserved",
			originator:     "codex_vscode",
			userAgent:      "codex_vscode/1.2.3 (Ubuntu 22.4.0; x86_64) vscode (codex_vscode; 1.2.3)",
			wantOriginator: "codex_vscode",
			wantUA:         "codex_vscode/1.2.3 (Ubuntu 22.4.0; x86_64) vscode (codex_vscode; 1.2.3)",
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
			name:           "trailer restores overridden identity then normalizes",
			originator:     "cccc",
			userAgent:      "cccc/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
			wantOriginator: "codex_cli_rs",
			wantUA:         "codex_cli_rs/0.142.0 (Ubuntu 22.4.0; x86_64) screen",
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

// 开关是进程级快照，零值 Config（测试 / 工具手工构造，不经 viper）必须落在「归一化开启」
// 一侧，否则任意一处零值构造都会静默关掉全局保护。
//
// 不得给本文件的开关类用例加 t.Parallel()：它们改写进程级状态。
func TestCodexOriginatorNormalizationZeroValueConfigKeepsItEnabled(t *testing.T) {
	var cfg config.Config
	require.False(t, cfg.Gateway.DisableCodexOriginatorNormalization,
		"零值必须表示归一化开启；若改为正向命名的 NormalizeCodexOriginator，零值会静默关闭保护")

	SetCodexOriginatorNormalizationEnabled(!cfg.Gateway.DisableCodexOriginatorNormalization)
	t.Cleanup(func() { SetCodexOriginatorNormalizationEnabled(true) })

	headers := make(http.Header)
	headers.Set("originator", "codex-tui")
	headers.Set("user-agent", "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)")

	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
}

// 关闭归一化后必须完整退回配对语义：降载身份逐字保留，供上游调整分桶后回滚使用。
func TestEnforceCodexIdentityHeadersNormalizationDisabled(t *testing.T) {
	const tuiUA = "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)"

	SetCodexOriginatorNormalizationEnabled(false)
	t.Cleanup(func() { SetCodexOriginatorNormalizationEnabled(true) })

	headers := make(http.Header)
	headers.Set("originator", "codex-tui")
	headers.Set("user-agent", tuiUA)

	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex-tui", headers.Get("originator"))
	require.Equal(t, tuiUA, headers.Get("user-agent"))
}

// 归一化必须是幂等的：重复收口（如透传路径先后经过多次改写）不得反复裁剪 UA。
func TestEnforceCodexIdentityHeadersNormalizationIsIdempotent(t *testing.T) {
	headers := make(http.Header)
	headers.Set("originator", "codex-tui")
	headers.Set("user-agent", "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)")

	enforceCodexIdentityHeaders(headers)
	first := headers.Get("user-agent")
	enforceCodexIdentityHeaders(headers)

	require.Equal(t, first, headers.Get("user-agent"))
	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
}

func TestEnforceCodexIdentityHeadersWithoutOriginatorIsNoop(t *testing.T) {
	headers := make(http.Header)
	headers.Set("user-agent", "third-party-client/1.0.0")

	enforceCodexIdentityHeaders(headers)

	require.Empty(t, headers.Get("originator"))
	require.Equal(t, "third-party-client/1.0.0", headers.Get("user-agent"))
}
