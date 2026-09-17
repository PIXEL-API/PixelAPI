// Package devin 提供 Devin 上游（ApiServerService，Connect-RPC + Protobuf）的
// 协议客户端：请求编码、流式解码、模型目录与 router 模型解析。
//
// 上游形态参考 devin2api 的实现结论：
//   - 端点默认为 https://server.codeium.com（Devin CLI 同款）
//   - 认证为 HTTP Basic "<token>-<token>" + metadata.api_key
//   - GetChatMessage 为服务端流式；GetCliModelConfigs/AssignModel 为一元调用
package devin

import "encoding/json"

// Image 是用户消息中的图片内容块。
type Image struct {
	// Data 为图片数据：纯 base64 或 data:URL（编码层会剥掉 data: 前缀）。
	Data string
	// MIMEType 缺省按 image/png 处理。
	MIMEType string
}

// ToolCall 是助手消息中的一次工具调用。
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON 文本；freeform 调用为原文
}

// ToolDefinition 是客户端声明的一个 function 工具。
type ToolDefinition struct {
	Name        string
	Description string
	// Parameters 为 JSON Schema 原文；上游拒绝含自然语言注解
	//（description/title/$comment/x-*）的 schema，编码层会做剥离。
	Parameters json.RawMessage
	Strict     bool
}

// ToolChoice 的合法取值。
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"
	ToolChoiceNamed    = "named"
)

// ToolChoice 指定工具调用约束；Mode=ToolChoiceNamed 时 ToolName 必须命中 Tools。
type ToolChoice struct {
	Mode     string
	ToolName string
}

// ChatMessage 是中间形态的一轮对话消息。
type ChatMessage struct {
	Role string // "user" / "assistant" / "tool"（system 由调用方并入 SystemPrompt）

	Text   string
	Images []Image

	// assistant 消息
	ToolCalls     []ToolCall
	Thinking      string
	Signature     string // thinking 签名，回放时必须原样回传
	SignatureType string
	Redacted      bool
	OutputID      string // 上游 output_id，与签名绑定回传

	// tool 消息
	ToolCallID string
	IsError    bool
}

// ChatRequest 是一次 GetChatMessage 调用的完整输入。
type ChatRequest struct {
	Model        string // 已解析的上游 model uid（router 由服务层先经 AssignModel 解析）
	SystemPrompt string
	Messages     []ChatMessage

	MaxTokens   *int64
	Temperature *float64
	TopP        *float64
	TopK        *int
	Seed        *int64
	// StopSequences 同时透传上游与本地截断（上游 stop pattern 实测不保证生效）。
	StopSequences []string

	Tools                    []ToolDefinition
	ToolChoice               *ToolChoice
	DisableParallelToolCalls bool

	// SessionKey 是同一会话的稳定标识（sticky hash / prompt_cache_key），
	// 用于派生 trajectory/cascade ID；为空时按系统提示+首条消息内容派生。
	SessionKey string

	// ModelAssignmentJWT 为 router 模型经 AssignModel 得到的 assignment jwt。
	ModelAssignmentJWT string
}

// Usage 是上游返回的计费用量快照（累计语义，取末帧值）。
type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	CacheWriteTokens  int64
	ModelUID          string
	BillingModelUID   string
	ResponseModel     string // actual_model_uid
	ResponseID        string // message_id
	UpstreamRequestID string
	// CommittedAcuCost 为上游承诺的 ACU 成本读数，供对账参考。
	CommittedAcuCost float64
	// ProviderRefusal 表示上游声明 provider 拒绝了本次请求。
	ProviderRefusal bool
}

// StopReason 归一化停止原因（与 OpenAI finish_reason 对齐的取值）。
const (
	StopReasonStop          = "stop"
	StopReasonLength        = "length"
	StopReasonToolCalls     = "tool_calls"
	StopReasonContentFilter = "content_filter"
	StopReasonError         = "error"
)

// EventKind 标识流式事件类型。
type EventKind int

const (
	EventTextDelta      EventKind = iota // Text: 正文增量
	EventReasoningDelta                  // Text: 思考增量
	EventToolCallStart                   // 工具调用开始：ToolIndex/ToolCallID/ToolName
	EventToolCallDelta                   // Text: 工具参数 JSON 片段，ToolIndex 定位
	EventDone                            // 正常收尾：Message 为完整聚合结果
	EventError                           // 失败收尾：Err 携带分类错误
)

// Event 是解码层产出的一个流式事件。
type Event struct {
	Kind         EventKind
	Text         string
	ContentIndex int
	ToolCallID   string
	ToolName     string
	Message      *AssistantResult
	Err          error
}

// AssistantResult 是 Done 事件携带的完整聚合响应。
type AssistantResult struct {
	Text              string
	Reasoning         string
	Signature         string
	SignatureType     string
	Redacted          bool
	OutputID          string
	ToolCalls         []ToolCall
	StopReason        string
	StopSequence      string
	Usage             Usage
	ErrorMessage      string
	ResponseID        string
	UpstreamRequestID string
	ResponseModel     string
}

// ModelInfo 是模型目录中一个可调度模型的描述。
type ModelInfo struct {
	ID                        string
	OwnedBy                   string
	SupportsImages            bool
	SupportsToolCalls         bool
	SupportsParallelToolCalls bool
	SupportsThinking          bool
	IsModelRouter             bool
	ContextTokens             int
	MaxOutputTokens           int
}
