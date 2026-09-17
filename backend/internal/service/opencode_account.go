package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	OpencodeAccountModeGo  = "go"
	OpencodeAccountModeZen = "zen"
	OpencodeZenBaseURL     = "https://opencode.ai/zen/v1"
)

func (a *Account) GetOpencodeAccountMode() string {
	if !a.IsOpencodeApiKey() {
		return ""
	}
	mode := strings.TrimSpace(a.GetCredential("account_mode"))
	if mode == "" {
		return OpencodeAccountModeGo
	}
	return mode
}

func (a *Account) IsOpencodeZen() bool {
	return a.IsOpencodeApiKey() && a.GetOpencodeAccountMode() == OpencodeAccountModeZen
}

func (a *Account) IsOpencodeGoPlan() bool {
	return a.IsOpencodeApiKey() && a.GetOpencodeAccountMode() == OpencodeAccountModeGo
}

func validateOpencodeAccountConfiguration(platform, accountType string, credentials map[string]any) error {
	if platform != PlatformOpencode {
		return nil
	}
	if accountType != AccountTypeAPIKey {
		return infraerrors.BadRequest("INVALID_OPENCODE_ACCOUNT_TYPE", "OpenCode requires an API key account")
	}
	if raw, exists := credentials["account_mode"]; exists {
		mode, ok := raw.(string)
		if !ok || (mode != OpencodeAccountModeGo && mode != OpencodeAccountModeZen) {
			return infraerrors.BadRequest("INVALID_OPENCODE_ACCOUNT_MODE", "OpenCode account_mode must be go or zen")
		}
	}
	return nil
}

var opencodeZenModelByID = func() map[string]OpencodeGoModelSpec {
	groups := map[OpencodeGoProtocol][]string{
		OpencodeGoProtocolResponses: {
			"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
			"gpt-5.5", "gpt-5.5-pro", "gpt-5.4", "gpt-5.4-pro", "gpt-5.4-mini", "gpt-5.4-nano",
			"gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2", "gpt-5.2-codex",
			"gpt-5.1", "gpt-5.1-codex", "gpt-5.1-codex-max", "gpt-5.1-codex-mini",
			"gpt-5", "gpt-5-codex", "gpt-5-nano", "grok-4.6", "grok-4.5", "grok-build-0.1",
			"muse-spark-1.3", "muse-spark-1.2",
		},
		OpencodeGoProtocolMessages: {
			"claude-fable-5-1", "claude-fable-5", "claude-opus-5", "claude-opus-4-8",
			"claude-opus-4-7", "claude-opus-4-6", "claude-opus-4-5", "claude-sonnet-5",
			"claude-sonnet-4-6", "claude-sonnet-4-5", "claude-haiku-4-5",
			"qwen3.7-max", "qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus",
		},
		OpencodeGoProtocolChat: {
			"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp",
			"minimax-m3", "minimax-m2.7", "minimax-m2.5", "glm-5.3-flash", "glm-5.3",
			"glm-5.2", "glm-5.1", "glm-5", "kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "kimi-k3", "big-pickle",
		},
	}
	models := make(map[string]OpencodeGoModelSpec)
	for protocol, ids := range groups {
		for _, id := range ids {
			models[id] = OpencodeGoModelSpec{ID: id, Protocol: protocol, NoTopP: id == "gpt-5.6-luna"}
		}
	}
	return models
}()

func opencodeModelSpec(account *Account, model string) (OpencodeGoModelSpec, bool) {
	if account.IsOpencodeZen() {
		spec, ok := opencodeZenModelByID[model]
		return spec, ok
	}
	if account.IsOpencodeGoPlan() {
		spec, ok := opencodeGoModelByID[model]
		return spec, ok
	}
	return OpencodeGoModelSpec{}, false
}
