package devin

import (
	"bytes"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/Wei-Shaw/sub2api/internal/pkg/devinproto"
)

// BuildRequest 把中间请求投影为上游 wire 格式（GetChatMessageRequest）。
// token 只用于构造 metadata；模型必须是已解析的上游 model uid（router
// 模型须先经 ResolveModel/AssignModel 得到真实 uid 与 jwt 再传入）。
func (c *Client) BuildRequest(req *ChatRequest) (*devinproto.GetChatMessageRequest, error) {
	trajectoryID, cascadeID := DeriveSessionIDs(req)

	// tool_choice=none 上游是真禁用：工具声明与描述注入都是噪音，不进 wire。
	noTools := req.ToolChoice != nil && req.ToolChoice.Mode == ToolChoiceNone
	systemPrompt := req.SystemPrompt
	if !noTools && len(req.Tools) > 0 {
		injected, err := withToolDescriptions(systemPrompt, req.Tools)
		if err != nil {
			return nil, err
		}
		systemPrompt = injected
	}

	// 缺省采样参数与真实 CLI 抓包一致；客户端显式提供的值透传覆盖。
	completion := &devinproto.ExaCodeiumCommonPb_CompletionConfiguration{
		NumCompletions: proto.Uint64(1),
		MaxTokens:      proto.Uint64(128000),
		MaxNewlines:    proto.Uint64(400),
		Temperature:    proto.Float64(1),
		TopK:           proto.Uint64(40),
		TopP:           proto.Float64(0.95),
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		completion.MaxTokens = proto.Uint64(uint64(*req.MaxTokens))
	}
	if req.Temperature != nil {
		completion.Temperature = req.Temperature
	}
	if req.TopP != nil {
		completion.TopP = req.TopP
	}
	if req.TopK != nil {
		completion.TopK = proto.Uint64(uint64(*req.TopK))
	}
	if len(req.StopSequences) > 0 {
		completion.StopPatterns = req.StopSequences
	}
	if req.Seed != nil {
		completion.Seed = proto.Uint64(uint64(*req.Seed))
	}

	result := &devinproto.GetChatMessageRequest{
		Metadata:                 c.metadata(metadataFingerprintBytes),
		Prompt:                   proto.String(systemPrompt),
		SystemPromptCacheOptions: ephemeralCacheOptions(),
		ChatModelUid:             proto.String(req.Model),
		RequestType:              devinproto.ChatMessageRequestType_CHAT_MESSAGE_REQUEST_TYPE_CASCADE.Enum(),
		Configuration:            completion,
		TrajectoryReference: &devinproto.ExaCortexPb_CortexTrajectoryReference{
			TrajectoryId:   proto.String(trajectoryID),
			StepIndex:      proto.Int32(nextStepIndex(trajectoryID)),
			TrajectoryType: devinproto.ExaCortexPb_CortexTrajectoryType_ExaCortexPb_CortexTrajectoryType_CORTEX_TRAJECTORY_TYPE_CASCADE.Enum(),
			StepType:       devinproto.ExaCortexPb_CortexStepType_ExaCortexPb_CortexStepType_CORTEX_STEP_TYPE_USER_INPUT.Enum(),
		},
		CascadeId:   proto.String(cascadeID),
		PlannerMode: devinproto.ExaCodeiumCommonPb_ConversationalPlannerMode_ExaCodeiumCommonPb_ConversationalPlannerMode_CONVERSATIONAL_PLANNER_MODE_DEFAULT.Enum(),
		ExecutionId: proto.String(uuidV4()),
	}

	if choice := req.ToolChoice; choice != nil {
		switch choice.Mode {
		case ToolChoiceNone, ToolChoiceRequired:
			result.ToolChoice = &devinproto.ExaChatPb_ChatToolChoice{
				Choice: &devinproto.ExaChatPb_ChatToolChoice_OptionName{OptionName: choice.Mode},
			}
		case ToolChoiceNamed:
			found := false
			for _, tool := range req.Tools {
				if tool.Name == choice.ToolName {
					found = true
					break
				}
			}
			if !found {
				return nil, &Failure{StatusCode: 400, Code: "invalid_argument", ClientFault: true,
					Message: fmt.Sprintf("tool_choice names tool %q which is not in the tools list", choice.ToolName)}
			}
			result.ToolChoice = &devinproto.ExaChatPb_ChatToolChoice{
				Choice: &devinproto.ExaChatPb_ChatToolChoice_ToolName{ToolName: choice.ToolName},
			}
		}
	}
	if req.DisableParallelToolCalls {
		result.DisableParallelToolCalls = proto.Bool(true)
	}

	// 上游只可靠接受「当前轮」图片：历史图进 Images 会 invalid_argument。
	// 当前轮 = 最后一条 assistant 消息之后的所有消息。
	lastAssistantIndex := -1
	for index, message := range req.Messages {
		if message.Role == "assistant" {
			lastAssistantIndex = index
		}
	}
	for index, message := range req.Messages {
		converted, err := convertMessage(&message, index > lastAssistantIndex)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", index, err)
		}
		result.ChatMessagePrompts = append(result.ChatMessagePrompts, converted...)
	}
	// 上游要求 call→result 紧邻配对；客户端历史常是「全部调用→全部结果」的
	// 分组结构，按 call id 重排成交错配对。
	result.ChatMessagePrompts = pairToolCallsWithResults(result.ChatMessagePrompts)

	if !noTools {
		for _, tool := range req.Tools {
			converted, err := convertToolDefinition(tool)
			if err != nil {
				return nil, err
			}
			result.Tools = append(result.Tools, converted)
		}
	}
	// 最后一条消息标记 EPHEMERAL 断点：下轮新消息命中前缀缓存。
	if n := len(result.ChatMessagePrompts); n > 0 {
		result.ChatMessagePrompts[n-1].PromptCacheOptions = ephemeralCacheOptions()
	}
	if req.ModelAssignmentJWT != "" {
		result.ModelAssignmentJwt = proto.String(req.ModelAssignmentJWT)
	}
	return result, nil
}

func ephemeralCacheOptions() *devinproto.ExaChatPb_PromptCacheOptions {
	return &devinproto.ExaChatPb_PromptCacheOptions{
		Type: devinproto.ExaChatPb_CacheControlType_ExaChatPb_CacheControlType_CACHE_CONTROL_TYPE_EPHEMERAL.Enum(),
	}
}

// DeriveSessionIDs 为请求派生上游 trajectory/cascade ID：SessionKey 优先做种，
// 否则用「系统提示头 4KB + 首条消息文本头 1KB」的内容哈希——同会话多轮
// 前缀不变 → 派生值稳定，命中上游 prompt 缓存更稳。
// 导出原因：AssignModel 的 assignment jwt 绑 cascade_id，服务层须用与本
// 函数一致的派生值，禁止在别处复刻该逻辑。
func DeriveSessionIDs(req *ChatRequest) (trajectoryID, cascadeID string) {
	var seed bytes.Buffer
	if req.SessionKey != "" {
		seed.WriteString(req.SessionKey)
	} else {
		head := req.SystemPrompt
		if len(head) > 4096 {
			head = head[:4096]
		}
		seed.WriteString(head)
		for _, message := range req.Messages {
			text := message.Text
			if text == "" {
				continue
			}
			if len(text) > 1024 {
				text = text[:1024]
			}
			seed.WriteByte(0)
			seed.WriteString(text)
			break
		}
	}
	sum := sha256.Sum256(seed.Bytes())
	return uuidFromBytes(sum[:16]), uuidFromBytes(sum[16:32])
}

// uuidFromBytes 把 16 字节格式化为 v4 UUID（固定 version/variant 位）。
func uuidFromBytes(b []byte) string {
	var out [16]byte
	copy(out[:], b)
	out[6] = (out[6] & 0x0f) | 0x40
	out[8] = (out[8] & 0x3f) | 0x80
	var buf [36]byte
	hex.Encode(buf[0:8], out[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], out[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], out[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], out[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], out[10:16])
	return string(buf[:])
}

// uuidV4 生成随机 v4 UUID；crypto 失败时返回定零值（理论不可达）。
func uuidV4() string {
	var value [16]byte
	_, _ = cryptorand.Read(value[:])
	return uuidFromBytes(value[:])
}

// stepIndexRegistry 按 trajectory_id 记录会话内单调递增的 step_index
// （真实 CLI 行为）；容量封顶防止 map 无界增长。
var stepIndexRegistry = struct {
	sync.Mutex
	counts map[string]int32
}{counts: make(map[string]int32)}

func nextStepIndex(trajectoryID string) int32 {
	stepIndexRegistry.Lock()
	defer stepIndexRegistry.Unlock()
	if len(stepIndexRegistry.counts) >= 65536 {
		stepIndexRegistry.counts = make(map[string]int32)
	}
	stepIndexRegistry.counts[trajectoryID]++
	return stepIndexRegistry.counts[trajectoryID]
}

var (
	userMessageSource      = devinproto.ExaCodeiumCommonPb_ChatMessageSource_ExaCodeiumCommonPb_ChatMessageSource_CHAT_MESSAGE_SOURCE_USER
	toolMessageSource      = devinproto.ExaCodeiumCommonPb_ChatMessageSource_ExaCodeiumCommonPb_ChatMessageSource_CHAT_MESSAGE_SOURCE_TOOL
	assistantMessageSource = devinproto.ExaCodeiumCommonPb_ChatMessageSource_ExaCodeiumCommonPb_ChatMessageSource_CHAT_MESSAGE_SOURCE_SYSTEM
)

// convertMessage 把一条中间消息转为 ChatMessagePrompt。
// attachImages 为 true 时才把图片写入 Images（仅最新用户轮）；历史图降级为文本占位。
func convertMessage(message *ChatMessage, attachImages bool) ([]*devinproto.ExaChatPb_ChatMessagePrompt, error) {
	switch message.Role {
	case "user":
		return []*devinproto.ExaChatPb_ChatMessagePrompt{promptForTextImages(userMessageSource, message.Text, message.Images, attachImages)}, nil
	case "assistant":
		// 一个助手回合合并为单条 prompt（真实 CLI wire 形态）：文本/思考/
		// 签名/工具调用同体携带；完全空的消息跳过（上游见空回复退化）。
		if message.Text == "" && message.Thinking == "" && len(message.ToolCalls) == 0 &&
			message.Signature == "" && !message.Redacted && message.OutputID == "" {
			return nil, nil
		}
		prompt := &devinproto.ExaChatPb_ChatMessagePrompt{
			MessageId: proto.String(uuidV4()),
			Source:    assistantMessageSource.Enum(),
		}
		if message.Text != "" {
			prompt.Prompt = proto.String(message.Text)
		}
		// signature_type/output_id 必须随签名原样回传（错配触发 invalid_argument）。
		if message.Thinking != "" || message.Redacted || message.Signature != "" || message.OutputID != "" {
			if message.Thinking != "" {
				prompt.Thinking = proto.String(message.Thinking)
			}
			if message.Signature != "" {
				prompt.Signature = proto.String(message.Signature)
			}
			if message.SignatureType != "" {
				prompt.SignatureType = proto.String(message.SignatureType)
			}
			if message.OutputID != "" {
				prompt.OutputId = proto.String(message.OutputID)
			}
			prompt.ThinkingRedacted = proto.Bool(message.Redacted)
		}
		for _, call := range message.ToolCalls {
			prompt.ToolCalls = append(prompt.ToolCalls, &devinproto.ExaCodeiumCommonPb_ChatToolCall{
				Id:            proto.String(call.ID),
				Name:          proto.String(call.Name),
				ArgumentsJson: proto.String(call.Arguments),
			})
		}
		return []*devinproto.ExaChatPb_ChatMessagePrompt{prompt}, nil
	case "tool":
		prompt := promptForTextImages(toolMessageSource, message.Text, message.Images, attachImages)
		if prompt.GetPrompt() == "" {
			// 上游不接受空的工具结果文本。
			prompt.Prompt = proto.String("[tool result]")
		}
		prompt.ToolCallId = proto.String(message.ToolCallID)
		prompt.ToolResultIsError = proto.Bool(message.IsError)
		return []*devinproto.ExaChatPb_ChatMessagePrompt{prompt}, nil
	default:
		return nil, fmt.Errorf("unsupported message role %q", message.Role)
	}
}

// promptForContent 把文本+图片内容投影为单条 prompt。
func promptForTextImages(source devinproto.ExaCodeiumCommonPb_ChatMessageSource, text string, images []Image, attachImages bool) *devinproto.ExaChatPb_ChatMessagePrompt {
	prompt := &devinproto.ExaChatPb_ChatMessagePrompt{
		MessageId: proto.String(uuidV4()),
		Source:    source.Enum(),
	}
	var builder strings.Builder
	builder.WriteString(text)
	for _, image := range images {
		if !attachImages {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString("[Image omitted from history]")
			continue
		}
		data := image.Data
		if strings.HasPrefix(data, "data:") {
			if _, encoded, ok := strings.Cut(data, ","); ok {
				data = encoded
			}
		}
		mimeType := image.MIMEType
		if mimeType == "" {
			mimeType = "image/png"
		}
		prompt.Images = append(prompt.Images, &devinproto.ExaCodeiumCommonPb_ImageData{
			Base64Data: proto.String(data),
			MimeType:   proto.String(mimeType),
		})
	}
	prompt.Prompt = proto.String(builder.String())
	return prompt
}

// pairToolCallsWithResults 把「连续调用 + 连续结果」的分组序列重排为
// call_i, result_i 交错序列；已配对的序列保持不变，孤立结果原样保留。
func pairToolCallsWithResults(prompts []*devinproto.ExaChatPb_ChatMessagePrompt) []*devinproto.ExaChatPb_ChatMessagePrompt {
	isCallPrompt := func(p *devinproto.ExaChatPb_ChatMessagePrompt) bool {
		return p.GetSource() == assistantMessageSource && len(p.GetToolCalls()) > 0
	}
	isResultPrompt := func(p *devinproto.ExaChatPb_ChatMessagePrompt) bool {
		return p.GetSource() == toolMessageSource
	}
	var out []*devinproto.ExaChatPb_ChatMessagePrompt
	for i := 0; i < len(prompts); {
		if !isCallPrompt(prompts[i]) {
			out = append(out, prompts[i])
			i++
			continue
		}
		var calls []*devinproto.ExaChatPb_ChatMessagePrompt
		for i < len(prompts) && isCallPrompt(prompts[i]) {
			calls = append(calls, prompts[i])
			i++
		}
		byID := make(map[string][]*devinproto.ExaChatPb_ChatMessagePrompt)
		j := i
		for j < len(prompts) && isResultPrompt(prompts[j]) {
			id := prompts[j].GetToolCallId()
			byID[id] = append(byID[id], prompts[j])
			j++
		}
		consumed := make(map[*devinproto.ExaChatPb_ChatMessagePrompt]struct{}, len(calls))
		for _, callPrompt := range calls {
			out = append(out, callPrompt)
			for _, call := range callPrompt.GetToolCalls() {
				queue := byID[call.GetId()]
				if len(queue) == 0 {
					continue
				}
				out = append(out, queue[0])
				consumed[queue[0]] = struct{}{}
				byID[call.GetId()] = queue[1:]
			}
		}
		for k := i; k < j; k++ {
			if _, ok := consumed[prompts[k]]; !ok {
				out = append(out, prompts[k])
			}
		}
		i = j
	}
	return out
}

// toolDescription 注入段预算：超软顶降级为纯名清单，再过硬顶报错。
const (
	toolPreambleSoftBytes = 24000
	toolPreambleHardBytes = 48000
)

// withToolDescriptions 把工具说明追加到 system prompt——上游 wire 上
// ChatToolDefinition.Description 只携带工具名（上游对工具 schema/描述中的
// 自然语言注解有拒绝行为），真实说明走系统提示词注入。
func withToolDescriptions(systemPrompt string, tools []ToolDefinition) (string, error) {
	var entries []string
	for _, tool := range tools {
		description := strings.TrimSpace(tool.Description)
		if description == "" {
			continue
		}
		entries = append(entries, "<tool name=\""+escapeXMLAttr(tool.Name)+"\">\n"+escapeXMLText(description)+"\n</tool>")
	}
	if len(entries) == 0 {
		return systemPrompt, nil
	}
	section := "# tools descriptions\n" + strings.Join(entries, "\n")
	if len(section) > toolPreambleSoftBytes {
		var names strings.Builder
		names.WriteString("# available tools:")
		for _, tool := range tools {
			names.WriteString(" ")
			names.WriteString(tool.Name)
		}
		section = names.String()
	}
	if len(section) > toolPreambleHardBytes {
		return "", &Failure{StatusCode: 400, Code: "invalid_argument", ClientFault: true,
			Message: fmt.Sprintf("tool_preamble_too_large: tool list needs %d bytes even as a bare name list (limit %d); reduce the number of tools", len(section), toolPreambleHardBytes)}
	}
	trimmed := strings.TrimRight(systemPrompt, "\r\n")
	if strings.TrimSpace(trimmed) == "" {
		return section, nil
	}
	return trimmed + "\n\n" + section, nil
}

func escapeXMLAttr(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return replacer.Replace(value)
}

func escapeXMLText(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
}

// convertToolDefinition 把工具定义转为上游 wire 形态：Description 只带名字，
// schema 剥掉上游拒绝的注解键并做 $ref inline/裸属性 map 归一。
func convertToolDefinition(tool ToolDefinition) (*devinproto.ExaChatPb_ChatToolDefinition, error) {
	schema := tool.Parameters
	if len(schema) == 0 {
		schema = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	cleaned, err := sanitizeToolSchema(schema)
	if err != nil {
		return nil, fmt.Errorf("sanitize devin tool %q schema: %w", tool.Name, err)
	}
	converted := &devinproto.ExaChatPb_ChatToolDefinition{
		Name:             proto.String(tool.Name),
		Description:      proto.String(tool.Name),
		JsonSchemaString: proto.String(string(cleaned)),
	}
	if tool.Strict {
		converted.Strict = proto.Bool(true)
	}
	return converted, nil
}

// sanitizeToolSchema 先剥注解键（description/title/$comment/x-*，在 properties
// 键名层级除外），再做 $ref inline 展开与裸属性 map 归一。
func sanitizeToolSchema(schema json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(schema, &value); err != nil {
		return nil, err
	}
	value = stripSchemaAnnotations(value, false)
	value = normalizeSchemaValue(value, value, map[string]bool{}, 0)
	if object, ok := value.(map[string]any); ok {
		delete(object, "$defs")
		delete(object, "definitions")
		delete(object, "$schema")
		if isBarePropertyMap(object) {
			value = map[string]any{"type": "object", "properties": object}
		}
	}
	return json.Marshal(value)
}

// stripSchemaAnnotations 递归剥注解键；propertyNames 标记当前处于
// properties 键名层级——属性名本身是要保留的键而非注解。
func stripSchemaAnnotations(value any, propertyNames bool) any {
	switch typed := value.(type) {
	case []any:
		for index, item := range typed {
			typed[index] = stripSchemaAnnotations(item, false)
		}
		return typed
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, child := range typed {
			if isNaturalLanguageAnnotation(key) && !propertyNames {
				continue
			}
			if isSchemaLiteral(key) && !propertyNames {
				cleaned[key] = child
				continue
			}
			cleaned[key] = stripSchemaAnnotations(child, key == "properties")
		}
		return cleaned
	default:
		return value
	}
}

func isSchemaLiteral(key string) bool {
	switch key {
	case "const", "default", "enum", "example", "examples":
		return true
	default:
		return false
	}
}

func isNaturalLanguageAnnotation(key string) bool {
	switch key {
	case "description", "title", "$comment":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(key), "x-")
	}
}

const maxSchemaRefDepth = 32

// normalizeSchemaValue 递归展开本地 $ref：root 是解析引用的根文档，
// resolving 检测循环引用，depth 封顶防病态嵌套无限膨胀。
// 循环或解不开的引用丢掉 $ref 键、保留同层其余约束。
func normalizeSchemaValue(value any, root any, resolving map[string]bool, depth int) any {
	if depth > maxSchemaRefDepth {
		return value
	}
	switch typed := value.(type) {
	case []any:
		for index, item := range typed {
			typed[index] = normalizeSchemaValue(item, root, resolving, depth+1)
		}
		return typed
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && strings.HasPrefix(ref, "#") {
			if target, found := resolveLocalRef(root, ref); found && !resolving[ref] {
				resolving[ref] = true
				resolved := normalizeSchemaValue(target, root, resolving, depth+1)
				delete(resolving, ref)
				delete(typed, "$ref")
				if resolvedObject, ok := resolved.(map[string]any); ok {
					for key, child := range resolvedObject {
						if _, exists := typed[key]; !exists {
							typed[key] = child
						}
					}
				}
			} else {
				delete(typed, "$ref")
			}
		}
		for key, child := range typed {
			if isSchemaLiteral(key) {
				continue
			}
			typed[key] = normalizeSchemaValue(child, root, resolving, depth+1)
		}
		return typed
	default:
		return value
	}
}

// resolveLocalRef 解析 "#/a/b" 形态的本地 JSON-pointer（处理 ~0/~1 转义）。
func resolveLocalRef(root any, ref string) (any, bool) {
	if ref == "#" {
		return root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	current := root
	for _, segment := range strings.Split(ref[2:], "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		if current, ok = object[segment]; !ok {
			return nil, false
		}
	}
	return current, true
}

// schemaKeywords 是「该 map 是不是 schema」的判定关键字集合（存在性判定）。
var schemaKeywords = map[string]bool{
	"type": true, "properties": true, "items": true, "required": true,
	"additionalProperties": true, "allOf": true, "anyOf": true, "oneOf": true,
	"not": true, "enum": true, "const": true, "format": true, "pattern": true,
	"minLength": true, "maxLength": true, "minimum": true, "maximum": true,
	"exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true,
	"minItems": true, "maxItems": true, "uniqueItems": true, "contains": true,
	"minProperties": true, "maxProperties": true, "patternProperties": true,
	"propertyNames": true, "dependentRequired": true, "dependentSchemas": true,
	"prefixItems": true, "if": true, "then": true, "else": true,
	"readOnly": true, "writeOnly": true, "deprecated": true,
	"description": true, "title": true, "default": true, "examples": true,
}

// isBarePropertyMap 判定对象是否是上游会拒绝的「裸属性 map」：
// 没有 schema 关键字、没有 $/x- 前缀键，且每个值都是对象。
func isBarePropertyMap(object map[string]any) bool {
	if len(object) == 0 {
		return false
	}
	for key, child := range object {
		if schemaKeywords[key] || strings.HasPrefix(key, "$") || strings.HasPrefix(strings.ToLower(key), "x-") {
			return false
		}
		if _, ok := child.(map[string]any); !ok {
			return false
		}
	}
	return true
}
