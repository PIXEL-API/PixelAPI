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
