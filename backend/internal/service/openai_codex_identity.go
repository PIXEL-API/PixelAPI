package service

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// codexUpstreamMinVersion is the minimum version header accepted by the
// ChatGPT Codex backend when a version header is present.
const codexUpstreamMinVersion = "0.144.0"

// codexOriginatorNormalization 控制 enforceCodexIdentityHeaders 是否把落在上游降载桶的
// Codex 身份改写为 CLI 身份，由 gateway.disable_codex_originator_normalization 在服务构造时取反发布。
// 默认开启：降载桶命中会让上游回 server_is_overloaded，网关据此判定瞬时故障并冷却账号。
var codexOriginatorNormalization = func() *atomic.Bool {
	v := &atomic.Bool{}
	v.Store(true)
	return v
}()

// SetCodexOriginatorNormalizationEnabled 发布 Codex 降载身份归一化开关。
// enforceCodexIdentityHeaders 是所有出站路径共用的纯函数收口点，无法在热路径注入配置，
// 故由持有配置的服务在构造时发布进程级快照。
func SetCodexOriginatorNormalizationEnabled(enabled bool) {
	codexOriginatorNormalization.Store(enabled)
}

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
		headers.Set("originator", openai.CodexCLIOriginator)
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
		originator, pairedUA = openai.CodexCLIOriginator, codexCLIUserAgent
	}
	// 配对之后再做降载身份归一化：上游按 originator 分桶调度容量，命中降载桶的请求会被回
	// server_is_overloaded，网关据此判定瞬时上游故障并冷却账号（对外表现为账号过载不可用），
	// 故这类身份统一改写为 CLI 身份——只替换身份段，保留版本 / OS / 架构 / 终端指纹。
	if codexOriginatorNormalization.Load() {
		originator, pairedUA, _ = openai.NormalizeCodexClientIdentityToCLI(originator, pairedUA)
	}
	headers.Set("user-agent", pairedUA)
	headers.Set("originator", originator)

	if version := strings.TrimSpace(headers.Get("version")); version != "" && CompareVersions(version, codexUpstreamMinVersion) < 0 {
		headers.Set("version", codexCLIVersion)
	}
}
