package service

import "strings"

// OpencodeGoProtocol 表示 OpenCode Go 模型要求的上游请求协议。
type OpencodeGoProtocol string

const (
	OpencodeGoProtocolChat      OpencodeGoProtocol = "chat"
	OpencodeGoProtocolMessages  OpencodeGoProtocol = "messages"
	OpencodeGoProtocolResponses OpencodeGoProtocol = "responses"
)

// OpencodeGoModelSpec 描述 OpenCode Go 裸模型 ID 的协议能力和弃用状态。
type OpencodeGoModelSpec struct {
	ID         string
	Protocol   OpencodeGoProtocol
	Deprecated bool
}

// opencodeGoModelCatalog 是 2026-08-30 审核的 OpenCode Go 模型快照：
// 成员以公开 GET /zen/go/v1/models 当日返回的 33 个可用 ID 为准；当前模型协议
// 优先依据 OpenCode Go 官方文档的“API 端点”表，未列出的历史模型再与固定
// models.dev 快照交叉核对。因来源版本存在漂移，固定 models.dev 提交中的
// ox-alpha-free（deprecated）不在公开端点返回值中，而公开端点中的 hy3-preview
// 尚未出现在该提交；两者不互相替换。新模型必须先确认协议能力再加入；未知
// 模型不得按默认协议放行。
var opencodeGoModelCatalog = [...]OpencodeGoModelSpec{
	{ID: "minimax-m3", Protocol: OpencodeGoProtocolMessages},
	{ID: "minimax-m2.7", Protocol: OpencodeGoProtocolMessages},
	{ID: "minimax-m2.5", Protocol: OpencodeGoProtocolMessages, Deprecated: true},
	{ID: "kimi-k3", Protocol: OpencodeGoProtocolChat},
	{ID: "kimi-k2.7-code", Protocol: OpencodeGoProtocolChat},
	{ID: "kimi-k2.6", Protocol: OpencodeGoProtocolChat},
	{ID: "longcat-2.0", Protocol: OpencodeGoProtocolChat},
	{ID: "kimi-k2.5", Protocol: OpencodeGoProtocolChat, Deprecated: true},
	{ID: "glm-5.2", Protocol: OpencodeGoProtocolChat},
	{ID: "glm-5.3-flash", Protocol: OpencodeGoProtocolChat},
	{ID: "glm-5.3", Protocol: OpencodeGoProtocolChat},
	{ID: "glm-5.1", Protocol: OpencodeGoProtocolChat},
	{ID: "glm-5", Protocol: OpencodeGoProtocolChat, Deprecated: true},
	{ID: "deepseek-v4-pro", Protocol: OpencodeGoProtocolChat},
	{ID: "deepseek-v4-flash", Protocol: OpencodeGoProtocolChat},
	{ID: "deepseek-v4-flash-vision-exp", Protocol: OpencodeGoProtocolChat},
	{ID: "qwen3.7-max", Protocol: OpencodeGoProtocolMessages},
	{ID: "qwen3.8-max", Protocol: OpencodeGoProtocolMessages},
	{ID: "qwen3.8-flash", Protocol: OpencodeGoProtocolMessages},
	{ID: "qwen3.7-plus", Protocol: OpencodeGoProtocolMessages},
	{ID: "qwen3.6-plus", Protocol: OpencodeGoProtocolMessages},
	{ID: "qwen3.5-plus", Protocol: OpencodeGoProtocolChat, Deprecated: true},
	{ID: "mimo-v2-pro", Protocol: OpencodeGoProtocolChat, Deprecated: true},
	{ID: "mimo-v2-omni", Protocol: OpencodeGoProtocolChat, Deprecated: true},
	{ID: "mimo-v2.5-pro", Protocol: OpencodeGoProtocolChat},
	{ID: "mimo-v2.5", Protocol: OpencodeGoProtocolChat},
	{ID: "hy4-preview", Protocol: OpencodeGoProtocolChat},
	{ID: "hy3", Protocol: OpencodeGoProtocolChat},
	{ID: "hy3-preview", Protocol: OpencodeGoProtocolChat},
	{ID: "gpt-5.6-luna", Protocol: OpencodeGoProtocolResponses},
	{ID: "grok-4.5", Protocol: OpencodeGoProtocolResponses, Deprecated: true},
	{ID: "grok-4.6", Protocol: OpencodeGoProtocolResponses},
	{ID: "muse-spark-1.2-contributor", Protocol: OpencodeGoProtocolResponses},
}

var opencodeGoModelByID = func() map[string]OpencodeGoModelSpec {
	models := make(map[string]OpencodeGoModelSpec, len(opencodeGoModelCatalog))
	for _, spec := range opencodeGoModelCatalog {
		models[spec.ID] = spec
	}
	return models
}()

// NormalizeOpencodeGoModelID 只移除客户端空白、Claude Code 的 [1m]
// 后缀，以及 OpenCode 配置使用的已知 provider 前缀。普通含斜杠模型名保持不变。
func NormalizeOpencodeGoModelID(model string) string {
	model = strings.TrimSpace(model)
	switch {
	case strings.HasPrefix(model, "opencode-go/"):
		model = strings.TrimPrefix(model, "opencode-go/")
	case strings.HasPrefix(model, "opencode/"):
		model = strings.TrimPrefix(model, "opencode/")
	}
	return normalizeClaudeCodeLongContextModel(strings.TrimSpace(model))
}

// ResolveOpencodeGoModelSpec 返回已审核模型的协议规格。未知模型严格返回 false。
func ResolveOpencodeGoModelSpec(model string) (OpencodeGoModelSpec, bool) {
	normalized := NormalizeOpencodeGoModelID(model)
	if normalized == "" {
		return OpencodeGoModelSpec{}, false
	}
	spec, ok := opencodeGoModelByID[normalized]
	return spec, ok
}

// OpencodeDefaultModelSlugs 返回受版本控制目录中的裸模型 ID 副本。
func OpencodeDefaultModelSlugs() []string {
	models := make([]string, 0, len(opencodeGoModelCatalog))
	for _, spec := range opencodeGoModelCatalog {
		models = append(models, spec.ID)
	}
	return models
}
