package openai

import (
	"regexp"
	"strings"
)

// CodexCLIUserAgentPrefixes matches Codex CLI User-Agent patterns.
var CodexCLIUserAgentPrefixes = []string{
	"codex_vscode/",
	"codex_cli_rs/",
}

// codexOfficialClientUAPrefixes contains the exact first-party Codex User-Agent families.
// The "Codex " family is handled separately so trimming cannot broaden it to a bare
// "codex" prefix.
var codexOfficialClientUAPrefixes = []string{
	"codex_cli_rs/",
	"codex-tui/",
	"codex_vscode/",
	"codex_vscode_copilot/",
	"codex_app/",
	"codex_chatgpt_desktop/",
	"codex_atlas/",
	"codex_exec/",
	"codex_sdk_ts/",
}

const codexOfficialClientFamilyPrefix = "codex "

// codexOfficialClientOriginators mirrors known first-party clientInfo.name values.
// Exact matching prevents values such as codex_evil from being treated as official.
var codexOfficialClientOriginators = map[string]bool{
	"codex_cli_rs":          true,
	"codex-tui":             true,
	"codex_vscode":          true,
	"codex_vscode_copilot":  true,
	"codex_app":             true,
	"codex_chatgpt_desktop": true,
	"codex_atlas":           true,
	"codex_exec":            true,
	"codex_sdk_ts":          true,
}

// IsBrowserUserAgent reports whether a User-Agent has the browser-style Mozilla prefix.
func IsBrowserUserAgent(userAgent string) bool {
	ua := strings.TrimSpace(userAgent)
	return ua != "" && strings.HasPrefix(strings.ToLower(ua), "mozilla/")
}

// IsCodexCLIRequest checks if the User-Agent indicates a Codex CLI request.
func IsCodexCLIRequest(userAgent string) bool {
	ua := normalizeCodexClientHeader(userAgent)
	return ua != "" && matchCodexClientHeaderPrefixes(ua, CodexCLIUserAgentPrefixes)
}

// IsCodexOfficialClientRequest checks the known official Codex User-Agent families.
// This compatibility variant keeps the historical contains fallback.
func IsCodexOfficialClientRequest(userAgent string) bool {
	return isCodexOfficialClientRequest(userAgent, false)
}

// IsCodexOfficialClientRequestStrict only accepts official identities at the start
// of the User-Agent (plus the codex-rs trailer identity described below).
func IsCodexOfficialClientRequestStrict(userAgent string) bool {
	return isCodexOfficialClientRequest(userAgent, true)
}

func isCodexOfficialClientRequest(userAgent string, strict bool) bool {
	ua := normalizeCodexClientHeader(userAgent)
	if ua == "" {
		return false
	}
	if strict {
		if matchCodexClientHeaderStrictPrefixes(ua, codexOfficialClientUAPrefixes) {
			return true
		}
	} else if matchCodexClientHeaderPrefixes(ua, codexOfficialClientUAPrefixes) {
		return true
	}
	if strings.HasPrefix(ua, codexOfficialClientFamilyPrefix) {
		return true
	}
	if name := codexUATrailerName(ua); name != "" {
		return IsCodexOfficialClientOriginator(name)
	}
	return false
}

// codexUATrailerName extracts clientInfo.name from the last `(name; version)`
// group emitted by codex-rs. CODEX_INTERNAL_ORIGINATOR_OVERRIDE changes the UA
// prefix but leaves this trailer intact, so it can recover the real identity.
func codexUATrailerName(ua string) string {
	last := strings.LastIndex(ua, "(")
	if last < 0 {
		return ""
	}
	rest := ua[last+1:]
	closeIdx := strings.Index(rest, ")")
	if closeIdx < 0 {
		return ""
	}
	inner := strings.TrimSpace(rest[:closeIdx])
	if semi := strings.Index(inner, ";"); semi >= 0 {
		inner = strings.TrimSpace(inner[:semi])
	}
	return inner
}

// IsCodexOfficialClientOriginator uses an exact known set plus the official
// `Codex ` family. Arbitrary codex_* values are intentionally rejected.
func IsCodexOfficialClientOriginator(originator string) bool {
	v := normalizeCodexClientHeader(originator)
	if v == "" {
		return false
	}
	return codexOfficialClientOriginators[v] || strings.HasPrefix(v, codexOfficialClientFamilyPrefix)
}

// IsCodexOfficialClientByHeaders checks both official User-Agent and originator signals.
func IsCodexOfficialClientByHeaders(userAgent, originator string) bool {
	return IsCodexOfficialClientRequest(userAgent) || IsCodexOfficialClientOriginator(originator)
}

func normalizeCodexClientHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchCodexClientHeaderPrefixes(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		normalizedPrefix := normalizeCodexClientHeader(prefix)
		if normalizedPrefix == "" {
			continue
		}
		if strings.HasPrefix(value, normalizedPrefix) || strings.Contains(value, normalizedPrefix) {
			return true
		}
	}
	return false
}

func matchCodexClientHeaderStrictPrefixes(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if p := normalizeCodexClientHeader(prefix); p != "" && strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}

// PairCodexClientIdentity derives the upstream originator from the final
// User-Agent. The ChatGPT Codex backend requires both values to represent the
// same official client or it responds with 404.
func PairCodexClientIdentity(userAgent string) (originator string, pairedUA string, ok bool) {
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return "", "", false
	}
	if leading := strings.TrimSpace(ua[:slash]); isSaneCodexOriginator(leading) && IsCodexOfficialClientOriginator(leading) {
		leading = canonicalizeCodexOriginator(leading)
		return leading, leading + ua[slash:], true
	}
	if trailer := codexUATrailerName(ua); trailer != "" && !strings.ContainsRune(trailer, '/') &&
		isSaneCodexOriginator(trailer) && IsCodexOfficialClientOriginator(trailer) {
		trailer = canonicalizeCodexOriginator(trailer)
		return trailer, trailer + ua[slash:], true
	}
	return "", "", false
}

const codexOriginatorMaxLen = 64

func isSaneCodexOriginator(name string) bool {
	if name == "" || len(name) > codexOriginatorMaxLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func canonicalizeCodexOriginator(name string) string {
	if lower := normalizeCodexClientHeader(name); codexOfficialClientOriginators[lower] {
		return lower
	}
	return name
}

// CodexCLIOriginator 官方 Codex CLI 默认 originator（codex-rs DEFAULT_ORIGINATOR），
// 也是身份归一化的目标身份。
const CodexCLIOriginator = "codex_cli_rs"

// codexLoadShedOriginators：上游 /backend-api/codex 按 originator 分桶调度容量，命中降载桶的
// 请求即使 HTTP 200 也会立刻推 SSE `event: error`（code=server_is_overloaded）并以
// response.failed 收尾。2026-07-29 起 codex-tui 被观测到落入降载桶：同账号、同请求体、同 UA，
// 仅把 originator 换成 codex_cli_rs 即恢复正常（换言之 UA 不是判定因子，originator 才是）。
// 网关会把该错误判定为瞬时上游故障并让账号进入冷却，对外表现为「账号过载不可用」，
// 因此出站前需要把命中的身份改写为 CLI 身份。
//
// 该集合是上游容量策略的快照而非协议常量，上游调整分桶后需同步修订。
var codexLoadShedOriginators = map[string]bool{
	"codex-tui": true,
}

// IsCodexLoadShedOriginator 判断 originator 是否落在上游降载桶。
func IsCodexLoadShedOriginator(originator string) bool {
	return codexLoadShedOriginators[normalizeCodexClientHeader(originator)]
}

// NormalizeCodexClientIdentityToCLI 把落在降载桶的官方身份改写为 Codex CLI 身份：
// UA 首段替换为 codex_cli_rs，并裁掉尾部 `(name; version)` 客户端标识组（真实 CLI UA 无该组），
// 版本 / OS / 架构 / 终端指纹原样保留。返回配套的 originator 与 UA，未命中降载桶时 changed=false。
//
// 入参应为 PairCodexClientIdentity 输出的已配对身份；改写后 UA 首段与 originator 仍然配套，
// 不破坏上游的配对校验，且改写幂等。
func NormalizeCodexClientIdentityToCLI(originator, userAgent string) (string, string, bool) {
	if !IsCodexLoadShedOriginator(originator) {
		return originator, userAgent, false
	}
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return CodexCLIOriginator, ua, true
	}
	rest := ua[slash:]
	// 仅当尾部括号组确为官方客户端标识时才裁剪，避免误截合法 UA 尾巴（如 `(Ubuntu 22.4.0; x86_64)`）。
	if trailer := codexUATrailerName(ua); trailer != "" && IsCodexOfficialClientOriginator(trailer) {
		if open := strings.LastIndex(rest, "("); open > 0 {
			rest = strings.TrimRight(rest[:open], " ")
		}
	}
	return CodexCLIOriginator, CodexCLIOriginator + rest, true
}

var codexEngineVersionPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// ParseCodexEngineVersion extracts the leading semantic engine version from a
// codex-rs style User-Agent.
func ParseCodexEngineVersion(ua string) (string, bool) {
	ua = strings.TrimSpace(ua)
	slash := strings.IndexByte(ua, '/')
	if slash < 0 {
		return "", false
	}
	rest := ua[slash+1:]
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' || rest[i] == '(' {
			end = i
			break
		}
	}
	version := codexEngineVersionPattern.FindString(strings.TrimSpace(rest[:end]))
	return version, version != ""
}
