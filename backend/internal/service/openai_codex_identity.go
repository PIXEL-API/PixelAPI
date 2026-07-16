package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// codexUpstreamMinVersion is the minimum version header accepted by the
// ChatGPT Codex backend when a version header is present.
const codexUpstreamMinVersion = "0.144.0"

// ensureCodexIdentityHeaders fills the identity headers required by the OAuth
// Messages compatibility bridge. Existing User-Agent and version values are
// preserved for the final pairing step below.
func ensureCodexIdentityHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	if strings.TrimSpace(headers.Get("user-agent")) == "" {
		headers.Set("user-agent", codexCLIUserAgent)
	}
	if strings.TrimSpace(headers.Get("originator")) == "" {
		headers.Set("originator", "codex_cli_rs")
	}
	if strings.TrimSpace(headers.Get("version")) == "" {
		headers.Set("version", codexCLIVersion)
	}
	headers.Set("OpenAI-Beta", "responses=experimental")
}

// enforceCodexIdentityHeaders pairs originator with the final outbound
// User-Agent. It must run after client, account, and ForceCodexCLI User-Agent
// overrides. Requests without originator are intentionally left unchanged;
// callers that require a complete identity must call ensure first.
func enforceCodexIdentityHeaders(headers http.Header) {
	if headers == nil || strings.TrimSpace(headers.Get("originator")) == "" {
		return
	}

	originator, pairedUA, ok := openai.PairCodexClientIdentity(headers.Get("user-agent"))
	if !ok {
		originator, pairedUA = "codex_cli_rs", codexCLIUserAgent
	}
	headers.Set("user-agent", pairedUA)
	headers.Set("originator", originator)

	if version := strings.TrimSpace(headers.Get("version")); version != "" && CompareVersions(version, codexUpstreamMinVersion) < 0 {
		headers.Set("version", codexCLIVersion)
	}
}
