package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// SessionContext 粘性会话上下文，用于区分不同来源的请求。
// 仅在 GenerateSessionHash 第 3 级 fallback（消息内容 hash）时混入，
// 避免不同用户发送相同消息产生相同 hash 导致账号集中。
type SessionContext struct {
	ClientIP  string
	UserAgent string
	APIKeyID  int64
}

// ParsedRequest 保存网关请求的预解析结果
//
// 性能优化说明：
// 原实现在多个位置重复解析请求体（Handler、Service 各解析一次）：
// 1. gateway_handler.go 解析获取 model 和 stream
// 2. gateway_service.go 再次解析获取 system、messages、metadata
// 3. GenerateSessionHash 又一次解析获取会话哈希所需字段
//
// 新实现一次解析，多处复用：
// 1. 在 Handler 层统一调用 ParseGatewayRequest 一次性解析
// 2. 将解析结果 ParsedRequest 传递给 Service 层
// 3. 避免重复 json.Unmarshal，减少 CPU 和内存开销
type ParsedRequest struct {
	Body            []byte          // 原始请求体（保留用于转发）
	Model           string          // 请求的模型名称
	Stream          bool            // 是否为流式请求
	MetadataUserID  string          // metadata.user_id（用于会话亲和）
	System          any             // system 字段内容
	Messages        []any           // messages 数组
	HasSystem       bool            // 是否包含 system 字段（包含 null 也视为显式传入）
	ThinkingEnabled bool            // 是否开启 thinking（部分平台会影响最终模型名）
	MaxTokens       int             // max_tokens 值（用于探测请求拦截）
	SessionContext  *SessionContext // 可选：请求上下文区分因子（nil 时行为不变）
}

// ParseGatewayRequest 解析网关请求体并返回结构化结果。
// protocol 指定请求协议格式（domain.PlatformAnthropic / domain.PlatformGemini），
// 不同协议使用不同的 system/messages 字段名。
func ParseGatewayRequest(body []byte, protocol string) (*ParsedRequest, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	parsed := &ParsedRequest{
		Body: body,
	}

	if rawModel, exists := req["model"]; exists {
		model, ok := rawModel.(string)
		if !ok {
			return nil, fmt.Errorf("invalid model field type")
		}
		parsed.Model = model
	}
	if rawStream, exists := req["stream"]; exists {
		stream, ok := rawStream.(bool)
		if !ok {
			return nil, fmt.Errorf("invalid stream field type")
		}
		parsed.Stream = stream
	}
	if metadata, ok := req["metadata"].(map[string]any); ok {
		if userID, ok := metadata["user_id"].(string); ok {
			parsed.MetadataUserID = userID
		}
	}

	switch protocol {
	case domain.PlatformGemini:
		// Gemini 原生格式: systemInstruction.parts / contents
		if sysInst, ok := req["systemInstruction"].(map[string]any); ok {
			if parts, ok := sysInst["parts"].([]any); ok {
				parsed.System = parts
			}
		}
		if contents, ok := req["contents"].([]any); ok {
			parsed.Messages = contents
		}
	default:
		// Anthropic / OpenAI 格式: system / messages
		// system 字段只要存在就视为显式提供（即使为 null），
		// 以避免客户端传 null 时被默认 system 误注入。
		if system, ok := req["system"]; ok {
			parsed.HasSystem = true
			parsed.System = system
		}
		if messages, ok := req["messages"].([]any); ok {
			parsed.Messages = messages
		}
	}

	// thinking: {type: "enabled" | "adaptive"}
	if rawThinking, ok := req["thinking"].(map[string]any); ok {
		if t, ok := rawThinking["type"].(string); ok && (t == "enabled" || t == "adaptive") {
			parsed.ThinkingEnabled = true
		}
	}

	// max_tokens
	if rawMaxTokens, exists := req["max_tokens"]; exists {
		if maxTokens, ok := parseIntegralNumber(rawMaxTokens); ok {
			parsed.MaxTokens = maxTokens
		}
	}

	return parsed, nil
}

// parseIntegralNumber 将 JSON 解码后的数字安全转换为 int。
// 仅接受“整数值”的输入，小数/NaN/Inf/越界值都会返回 false。
func parseIntegralNumber(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return 0, false
		}
		if v > float64(math.MaxInt) || v < float64(math.MinInt) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		if v > int64(math.MaxInt) || v < int64(math.MinInt) {
			return 0, false
		}
		return int(v), true
	case json.Number:
		i64, err := v.Int64()
		if err != nil {
			return 0, false
		}
		if i64 > int64(math.MaxInt) || i64 < int64(math.MinInt) {
			return 0, false
		}
		return int(i64), true
	default:
		return 0, false
	}
}

// FilterThinkingBlocks removes thinking blocks from request body
// Returns filtered body or original body if filtering fails (fail-safe)
// This prevents 400 errors from invalid thinking block signatures
//
// 策略：
//   - 当 thinking.type 不是 "enabled"/"adaptive"：移除所有 thinking 相关块
//   - 当 thinking.type 是 "enabled"/"adaptive"：仅移除缺失/无效 signature 的 thinking 块（避免 400）
//     (blocks with missing/empty/dummy signatures that would cause 400 errors)
func FilterThinkingBlocks(body []byte) []byte {
	return filterThinkingBlocksInternal(body, false)
}

// FilterThinkingBlocksForRetry strips thinking-related constructs for retry scenarios.
//
// Why:
//   - Upstreams may reject historical `thinking`/`redacted_thinking` blocks due to invalid/missing signatures.
//   - Anthropic extended thinking has a structural constraint: when top-level `thinking` is enabled and the
//     final message is an assistant prefill, the assistant content must start with a thinking block.
//   - If we remove thinking blocks but keep top-level `thinking` enabled, we can trigger:
//     "Expected `thinking` or `redacted_thinking`, but found `text`"
//
// Strategy (B: preserve content as text):
//   - Disable top-level `thinking` (remove `thinking` field).
//   - Convert `thinking` blocks to `text` blocks (preserve the thinking content).
//   - Remove `redacted_thinking` blocks (cannot be converted to text).
//   - Ensure no message ends up with empty content.
func FilterThinkingBlocksForRetry(body []byte) []byte {
	hasThinkingContent := bytes.Contains(body, []byte(`"type":"thinking"`)) ||
		bytes.Contains(body, []byte(`"type": "thinking"`)) ||
		bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) ||
		bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) ||
		bytes.Contains(body, []byte(`"thinking":`)) ||
		bytes.Contains(body, []byte(`"thinking" :`))

	// Also check for empty content arrays that need fixing.
	// Note: This is a heuristic check; the actual empty content handling is done below.
	hasEmptyContent := bytes.Contains(body, []byte(`"content":[]`)) ||
		bytes.Contains(body, []byte(`"content": []`)) ||
		bytes.Contains(body, []byte(`"content" : []`)) ||
		bytes.Contains(body, []byte(`"content" :[]`))

	// Fast path: nothing to process
	if !hasThinkingContent && !hasEmptyContent {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	modified := false

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	// Disable top-level thinking mode for retry to avoid structural/signature constraints upstream.
	if _, exists := req["thinking"]; exists {
		delete(req, "thinking")
		modified = true
	}

	newMessages := make([]any, 0, len(messages))

	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			// String content or other format - keep as is
			newMessages = append(newMessages, msg)
			continue
		}

		newContent := make([]any, 0, len(content))
		modifiedThisMsg := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)

			// Convert thinking blocks to text (preserve content) and drop redacted_thinking.
			switch blockType {
			case "thinking":
				modifiedThisMsg = true
				thinkingText, _ := blockMap["thinking"].(string)
				if thinkingText == "" {
					continue
				}
				newContent = append(newContent, map[string]any{
					"type": "text",
					"text": thinkingText,
				})
				continue
			case "redacted_thinking":
				modifiedThisMsg = true
				continue
			}

			// Handle blocks without type discriminator but with a "thinking" field.
			if blockType == "" {
				if rawThinking, hasThinking := blockMap["thinking"]; hasThinking {
					modifiedThisMsg = true
					switch v := rawThinking.(type) {
					case string:
						if v != "" {
							newContent = append(newContent, map[string]any{"type": "text", "text": v})
						}
					default:
						if b, err := json.Marshal(v); err == nil && len(b) > 0 {
							newContent = append(newContent, map[string]any{"type": "text", "text": string(b)})
						}
					}
					continue
				}
			}

			newContent = append(newContent, block)
		}

		// Handle empty content: either from filtering or originally empty
		if len(newContent) == 0 {
			modified = true
			placeholder := "(content removed)"
			if role == "assistant" {
				placeholder = "(assistant content removed)"
			}
			newContent = append(newContent, map[string]any{
				"type": "text",
				"text": placeholder,
			})
			msgMap["content"] = newContent
		} else if modifiedThisMsg {
			modified = true
			msgMap["content"] = newContent
		}
		newMessages = append(newMessages, msgMap)
	}

	if modified {
		req["messages"] = newMessages
	} else {
		// Avoid rewriting JSON when no changes are needed.
		return body
	}

	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

// FilterSignatureSensitiveBlocksForRetry is a stronger retry filter for cases where upstream errors indicate
// signature/thought_signature validation issues involving tool blocks.
//
// This performs everything in FilterThinkingBlocksForRetry, plus:
//   - Convert `tool_use` blocks to text (name/id/input) so we stop sending structured tool calls.
//   - Convert `tool_result` blocks to text so we keep tool results visible without tool semantics.
//
// Use this only when needed: converting tool blocks to text changes model behaviour and can increase the
// risk of prompt injection (tool output becomes plain conversation text).
func FilterSignatureSensitiveBlocksForRetry(body []byte) []byte {
	// Fast path: only run when we see likely relevant constructs.
	if !bytes.Contains(body, []byte(`"type":"thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"tool_use"`)) &&
		!bytes.Contains(body, []byte(`"type": "tool_use"`)) &&
		!bytes.Contains(body, []byte(`"type":"tool_result"`)) &&
		!bytes.Contains(body, []byte(`"type": "tool_result"`)) &&
		!bytes.Contains(body, []byte(`"thinking":`)) &&
		!bytes.Contains(body, []byte(`"thinking" :`)) {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	modified := false

	// Disable top-level thinking for retry to avoid structural/signature constraints upstream.
	if _, exists := req["thinking"]; exists {
		delete(req, "thinking")
		modified = true
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	newMessages := make([]any, 0, len(messages))

	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		newContent := make([]any, 0, len(content))
		modifiedThisMsg := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "thinking":
				modifiedThisMsg = true
				thinkingText, _ := blockMap["thinking"].(string)
				if thinkingText == "" {
					continue
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": thinkingText})
				continue
			case "redacted_thinking":
				modifiedThisMsg = true
				continue
			case "tool_use":
				modifiedThisMsg = true
				name, _ := blockMap["name"].(string)
				id, _ := blockMap["id"].(string)
				input := blockMap["input"]
				inputJSON, _ := json.Marshal(input)
				text := "(tool_use)"
				if name != "" {
					text += " name=" + name
				}
				if id != "" {
					text += " id=" + id
				}
				if len(inputJSON) > 0 && string(inputJSON) != "null" {
					text += " input=" + string(inputJSON)
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": text})
				continue
			case "tool_result":
				modifiedThisMsg = true
				toolUseID, _ := blockMap["tool_use_id"].(string)
				isError, _ := blockMap["is_error"].(bool)
				content := blockMap["content"]
				contentJSON, _ := json.Marshal(content)
				text := "(tool_result)"
				if toolUseID != "" {
					text += " tool_use_id=" + toolUseID
				}
				if isError {
					text += " is_error=true"
				}
				if len(contentJSON) > 0 && string(contentJSON) != "null" {
					text += "\n" + string(contentJSON)
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": text})
				continue
			}

			if blockType == "" {
				if rawThinking, hasThinking := blockMap["thinking"]; hasThinking {
					modifiedThisMsg = true
					switch v := rawThinking.(type) {
					case string:
						if v != "" {
							newContent = append(newContent, map[string]any{"type": "text", "text": v})
						}
					default:
						if b, err := json.Marshal(v); err == nil && len(b) > 0 {
							newContent = append(newContent, map[string]any{"type": "text", "text": string(b)})
						}
					}
					continue
				}
			}

			newContent = append(newContent, block)
		}

		if modifiedThisMsg {
			modified = true
			if len(newContent) == 0 {
				placeholder := "(content removed)"
				if role == "assistant" {
					placeholder = "(assistant content removed)"
				}
				newContent = append(newContent, map[string]any{"type": "text", "text": placeholder})
			}
			msgMap["content"] = newContent
		}

		newMessages = append(newMessages, msgMap)
	}

	if !modified {
		return body
	}

	req["messages"] = newMessages
	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}

// filterThinkingBlocksInternal removes invalid thinking blocks from request
// 策略：
//   - 当 thinking.type 不是 "enabled"/"adaptive"：移除所有 thinking 相关块
//   - 当 thinking.type 是 "enabled"/"adaptive"：仅移除缺失/无效 signature 的 thinking 块
func filterThinkingBlocksInternal(body []byte, _ bool) []byte {
	// Fast path: if body doesn't contain "thinking", skip parsing
	if !bytes.Contains(body, []byte(`"type":"thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "thinking"`)) &&
		!bytes.Contains(body, []byte(`"type":"redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"type": "redacted_thinking"`)) &&
		!bytes.Contains(body, []byte(`"thinking":`)) &&
		!bytes.Contains(body, []byte(`"thinking" :`)) {
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	// Check if thinking is enabled
	thinkingEnabled := false
	if thinking, ok := req["thinking"].(map[string]any); ok {
		if thinkType, ok := thinking["type"].(string); ok && (thinkType == "enabled" || thinkType == "adaptive") {
			thinkingEnabled = true
		}
	}

	messages, ok := req["messages"].([]any)
	if !ok {
		return body
	}

	filtered := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}

		newContent := make([]any, 0, len(content))
		filteredThisMessage := false

		for _, block := range content {
			blockMap, ok := block.(map[string]any)
			if !ok {
				newContent = append(newContent, block)
				continue
			}

			blockType, _ := blockMap["type"].(string)

			if blockType == "thinking" || blockType == "redacted_thinking" {
				// When thinking is enabled and this is an assistant message,
				// only keep thinking blocks with valid signatures
				if thinkingEnabled && role == "assistant" {
					signature, _ := blockMap["signature"].(string)
					if signature != "" && signature != antigravity.DummyThoughtSignature {
						newContent = append(newContent, block)
						continue
					}
				}
				filtered = true
				filteredThisMessage = true
				continue
			}

			// Handle blocks without type discriminator but with "thinking" key
			if blockType == "" {
				if _, hasThinking := blockMap["thinking"]; hasThinking {
					filtered = true
					filteredThisMessage = true
					continue
				}
			}

			newContent = append(newContent, block)
		}

		if filteredThisMessage {
			msgMap["content"] = newContent
		}
	}

	if !filtered {
		return body
	}

	newBody, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return newBody
}
