package devin

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"

	"github.com/Wei-Shaw/sub2api/internal/pkg/devinproto"
)

// ChatStream 包装上游服务端流：Receive 逐帧解码为 Event 序列。
// 正常收尾产出 EventDone（Message 为完整聚合结果），失败产出 EventError；
// 两类终态事件产出后 Next 返回 io.EOF。
type ChatStream struct {
	stream   *connect.ServerStreamForClient[devinproto.GetChatMessageResponse]
	decoder  *responseDecoder
	finished bool
}

// Next 返回下一批解码事件；流结束且无剩余事件时返回 io.EOF。
func (s *ChatStream) Next() ([]Event, error) {
	for {
		if s.finished {
			return nil, io.EOF
		}
		if !s.stream.Receive() {
			events := s.decoder.finish(s.stream.Err())
			s.finished = true
			_ = s.stream.Close()
			if len(events) == 0 {
				return nil, io.EOF
			}
			return events, nil
		}
		if events := s.decoder.decode(s.stream.Msg()); len(events) > 0 {
			return events, nil
		}
	}
}

// Close 终止流并释放上游连接。
func (s *ChatStream) Close() {
	s.finished = true
	if s.stream != nil {
		_ = s.stream.Close()
	}
}

// toolState 累计一次工具调用的参数片段与事件状态。
type toolState struct {
	call          ToolCall
	index         int // 在结果 ToolCalls 中的位置（OpenAI tool_calls index）
	eventID       string
	arguments     strings.Builder
	emitted       bool
	placeholderID bool // 首帧无 id 时的合成占位 id
}

// responseDecoder 把上游响应帧解释为有序事件并累计完整响应。
type responseDecoder struct {
	result AssistantResult

	textBuilder strings.Builder
	textEmitted int
	textOpen    bool

	thinkingBuilder    strings.Builder
	thinkingSigBuilder strings.Builder
	thinkingOpen       bool

	tools []*toolState

	hasStopReason bool
	stopReason    string

	stopPatterns     []string
	maxPatternLen    int
	stoppedByPattern bool
	stopSequence     string

	providerRefusal bool
	finished        bool
}

// newResponseDecoder 建解码器；stopPatterns 由 SetStopPatterns 注入。
func newResponseDecoder() *responseDecoder {
	return &responseDecoder{}
}

// SetStopPatterns 设置客户端停止序列：上游 stop pattern 实测不保证生效，
// 解码层对累计文本做本地截断兜底。须在首帧前调用。
func (s *ChatStream) SetStopPatterns(patterns []string) {
	filtered := patterns[:0:0]
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		filtered = append(filtered, pattern)
		if len(pattern) > s.decoder.maxPatternLen {
			s.decoder.maxPatternLen = len(pattern)
		}
	}
	s.decoder.stopPatterns = filtered
}

// decode 解释一帧上游响应：先刷元数据/usage，再按思考、文本、工具增量、
// 停止原因依次产出事件。finished 后或停止序列截断后的帧只更新元数据。
func (d *responseDecoder) decode(response *devinproto.GetChatMessageResponse) []Event {
	if d.finished {
		return nil
	}
	d.updateMetadata(response)
	if d.stoppedByPattern {
		return nil
	}
	events := make([]Event, 0, 4)
	// 签名作为尾随帧发送；思考块已关闭时合并回结果而不是新开块。
	if sig := response.GetDeltaSignature(); sig != "" && response.GetDeltaThinking() == "" && !d.thinkingOpen {
		d.thinkingSigBuilder.WriteString(sig)
		if sigType := response.GetDeltaSignatureType(); sigType != "" {
			d.result.SignatureType = sigType
		}
		d.result.Redacted = d.result.Redacted || response.GetThinkingRedacted()
		d.result.Signature = d.thinkingSigBuilder.String()
	} else if response.GetDeltaThinking() != "" || response.GetDeltaSignature() != "" || response.GetThinkingRedacted() {
		events = d.endText(events)
		events = d.decodeThinking(events, response)
	}
	if delta := response.GetDeltaText(); delta != "" {
		events = d.endThinking(events)
		events = d.decodeText(events, delta)
	}
	for _, toolDelta := range response.GetDeltaToolCalls() {
		events = d.endThinking(events)
		events = d.endText(events)
		events = d.decodeTool(events, toolDelta)
	}
	if reason := response.GetStopReason(); reason != devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_UNSPECIFIED {
		d.hasStopReason = true
		d.stopReason = mapStopReason(reason)
	}
	return events
}

// updateMetadata 刷响应元数据与 usage；usage 是累计快照，
// 显式零字段不覆盖已入账的非零值。
func (d *responseDecoder) updateMetadata(response *devinproto.GetChatMessageResponse) {
	if response.MessageId != nil {
		d.result.ResponseID = response.GetMessageId()
	}
	if id := response.GetOutputId(); id != "" {
		d.result.OutputID = id
	}
	if id := response.GetRequestId(); id != "" && d.result.UpstreamRequestID == "" {
		d.result.UpstreamRequestID = id
	}
	if response.ActualModelUid != nil {
		d.result.ResponseModel = response.GetActualModelUid()
	}
	usage := response.GetUsage()
	if usage == nil {
		if response.CommittedAcuCost != nil {
			d.result.Usage.CommittedAcuCost = response.GetCommittedAcuCost()
		}
		return
	}
	u := &d.result.Usage
	if u.ResponseModel == "" && usage.ModelUid != nil {
		u.ResponseModel = usage.GetModelUid()
	}
	if usage.GetInputTokens() != 0 || u.InputTokens == 0 {
		u.InputTokens = int64(usage.GetInputTokens())
	}
	if usage.GetOutputTokens() != 0 || u.OutputTokens == 0 {
		u.OutputTokens = int64(usage.GetOutputTokens())
	}
	if usage.GetCacheReadTokens() != 0 || u.CacheReadTokens == 0 {
		u.CacheReadTokens = int64(usage.GetCacheReadTokens())
	}
	if usage.GetCacheWriteTokens() != 0 || u.CacheWriteTokens == 0 {
		u.CacheWriteTokens = int64(usage.GetCacheWriteTokens())
	}
	if billing := usage.GetBillingModelUid(); billing != "" {
		u.BillingModelUID = billing
	}
	if usage.GetProviderRefusal() {
		d.providerRefusal = true
		u.ProviderRefusal = true
	}
	if response.CommittedAcuCost != nil {
		u.CommittedAcuCost = response.GetCommittedAcuCost()
	}
	if d.result.ResponseModel == "" {
		d.result.ResponseModel = u.ResponseModel
	}
}

// decodeThinking 处理思考增量：正文/签名分别累计，正文产增量事件。
func (d *responseDecoder) decodeThinking(events []Event, response *devinproto.GetChatMessageResponse) []Event {
	if !d.thinkingOpen {
		d.thinkingBuilder.Reset()
		d.thinkingSigBuilder.Reset()
		d.result.Redacted = d.result.Redacted || response.GetThinkingRedacted()
		d.thinkingOpen = true
	}
	if delta := response.GetDeltaThinking(); delta != "" {
		d.thinkingBuilder.WriteString(delta)
		d.result.Reasoning = d.thinkingBuilder.String()
		events = append(events, Event{Kind: EventReasoningDelta, Text: delta})
	}
	if sig := response.GetDeltaSignature(); sig != "" {
		d.thinkingSigBuilder.WriteString(sig)
	}
	if sigType := response.GetDeltaSignatureType(); sigType != "" {
		d.result.SignatureType = sigType
	}
	d.result.Redacted = d.result.Redacted || response.GetThinkingRedacted()
	d.result.Signature = d.thinkingSigBuilder.String()
	return events
}

// decodeText 处理文本增量：累计正文并按停止序列 holdback 窗口下发。
func (d *responseDecoder) decodeText(events []Event, delta string) []Event {
	if !d.textOpen {
		d.textBuilder.Reset()
		d.textEmitted = 0
		d.textOpen = true
	}
	d.textBuilder.WriteString(delta)
	if len(d.stopPatterns) == 0 {
		d.textEmitted += len(delta)
		d.result.Text = d.textBuilder.String()
		return append(events, Event{Kind: EventTextDelta, Text: delta})
	}
	return d.scanTextForStops(events)
}

// scanTextForStops 在累计文本中查找停止序列：命中则截断并标记
// stoppedByPattern；未命中时保留尾部 maxPatternLen-1 字节不下发
// （可能是跨帧的不完整前缀），残续字节回退到 rune 边界防半个 UTF-8。
func (d *responseDecoder) scanTextForStops(events []Event) []Event {
	text := d.textBuilder.String()
	earliest := -1
	for _, pattern := range d.stopPatterns {
		if idx := strings.Index(text[d.textEmitted:], pattern); idx >= 0 {
			pos := d.textEmitted + idx
			if earliest < 0 || pos < earliest {
				earliest = pos
				d.stopSequence = pattern
			}
		}
	}
	if earliest >= 0 {
		if earliest > d.textEmitted {
			events = d.emitTextDelta(events, text[d.textEmitted:earliest])
		}
		d.stoppedByPattern = true
		d.textBuilder.Reset()
		d.textBuilder.WriteString(text[:earliest])
		d.result.Text = text[:earliest]
		return d.endText(events)
	}
	if safe := len(text) - d.maxPatternLen + 1; safe > d.textEmitted {
		for safe < len(text) && !utf8.RuneStart(text[safe]) {
			safe--
		}
		if safe > d.textEmitted {
			events = d.emitTextDelta(events, text[d.textEmitted:safe])
		}
	}
	return events
}

func (d *responseDecoder) emitTextDelta(events []Event, delta string) []Event {
	d.textEmitted += len(delta)
	d.result.Text = d.textBuilder.String()
	return append(events, Event{Kind: EventTextDelta, Text: delta})
}

// decodeTool 处理工具调用增量：按 id 定位或新建 toolState；首帧无 id 时
// 合成占位 id，真实 id 晚到只回填最终消息、不改事件 id。
func (d *responseDecoder) decodeTool(events []Event, delta *devinproto.ExaCodeiumCommonPb_ChatToolCall) []Event {
	state := d.findTool(delta)
	if state == nil {
		id := delta.GetId()
		placeholder := id == ""
		if placeholder {
			id = "call_" + strconv.Itoa(len(d.tools))
		}
		state = &toolState{
			call:          ToolCall{ID: id, Name: delta.GetName()},
			index:         len(d.tools),
			eventID:       id,
			placeholderID: placeholder,
		}
		d.tools = append(d.tools, state)
	}
	if delta.GetName() != "" {
		state.call.Name = delta.GetName()
	}
	if state.placeholderID && delta.GetId() != "" {
		state.placeholderID = false
		state.call.ID = delta.GetId()
	}
	fragment := delta.GetArgumentsJson()
	hasFragment := delta.ArgumentsJson != nil
	// invalid_json_str 通道携带 freeform 原文（非 JSON），原样透传。
	if invalid := delta.GetInvalidJsonStr(); invalid != "" {
		fragment = invalid
		hasFragment = true
	}
	if hasFragment {
		state.arguments.WriteString(fragment)
	}
	if !state.emitted {
		state.emitted = true
		events = append(events, Event{
			Kind: EventToolCallStart, ContentIndex: state.index,
			ToolCallID: state.eventID, ToolName: state.call.Name,
		})
	}
	if hasFragment {
		events = append(events, Event{
			Kind: EventToolCallDelta, ContentIndex: state.index,
			ToolCallID: state.eventID, Text: fragment,
		})
	}
	return events
}

// findTool 定位工具调用增量所属 state：带 id 帧按 id 匹配；无 id 帧在
// 末位调用仍持占位 id 时归并给它（上游同一调用的帧连续发送）。
func (d *responseDecoder) findTool(delta *devinproto.ExaCodeiumCommonPb_ChatToolCall) *toolState {
	id := delta.GetId()
	for _, state := range d.tools {
		if id != "" && state.call.ID == id {
			return state
		}
	}
	if len(d.tools) == 0 {
		return nil
	}
	last := d.tools[len(d.tools)-1]
	if id == "" && last.placeholderID {
		return last
	}
	if id != "" && last.placeholderID && delta.GetName() == last.call.Name {
		// 迟到的真实 id：与占位调用同名时视作同一调用。
		return last
	}
	return nil
}

func (d *responseDecoder) endThinking(events []Event) []Event {
	if !d.thinkingOpen {
		return events
	}
	d.thinkingOpen = false
	d.result.Reasoning = d.thinkingBuilder.String()
	d.result.Signature = d.thinkingSigBuilder.String()
	return events
}

func (d *responseDecoder) endText(events []Event) []Event {
	if !d.textOpen {
		return events
	}
	d.textOpen = false
	pending := d.textBuilder.String()
	if d.textEmitted < len(pending) {
		events = d.emitTextDelta(events, pending[d.textEmitted:])
	}
	d.result.Text = d.textBuilder.String()
	return events
}

// finish 在流终止时产出收尾事件：错误透传分类；正常收尾先补齐 open 块
// 与工具调用的最终参数，再产 Done。
func (d *responseDecoder) finish(upstreamErr error) []Event {
	if d.finished {
		return nil
	}
	d.finished = true
	if upstreamErr != nil {
		return d.fail(upstreamErr)
	}
	hasContent := d.textBuilder.Len() > 0 || d.thinkingBuilder.Len() > 0 || len(d.tools) > 0 ||
		d.result.Text != "" || d.result.Reasoning != ""
	if !d.hasStopReason && !hasContent {
		return d.fail(errors.New("devin stream ended without generated content"))
	}
	reason := d.stopReason
	if d.stoppedByPattern {
		reason = StopReasonStop
		d.result.StopSequence = d.stopSequence
	} else if !d.hasStopReason {
		// 正常收尾必带 stopReason 帧；干净 EOF 却没有停止原因说明流被截断，
		// 合成 stop 会把半完成的任务伪装成正常结束。
		return d.fail(errors.New("devin stream ended without stop reason"))
	}
	if reason == StopReasonError {
		if d.providerRefusal {
			return d.fail(errors.New("upstream provider refused the request (provider_refusal)"))
		}
		return d.fail(errors.New("devin stopped with an error"))
	}
	return d.complete(reason)
}

// complete 产出 Done：物化全部工具调用的最终参数（XML 泄漏修复+非法
// JSON 兜底 {}），聚合完整响应进 Message。
func (d *responseDecoder) complete(reason string) []Event {
	events := make([]Event, 0, len(d.tools)+1)
	events = d.endThinking(events)
	events = d.endText(events)
	d.result.StopReason = reason
	d.result.ToolCalls = make([]ToolCall, 0, len(d.tools))
	for _, state := range d.tools {
		state.call.Arguments = state.arguments.String()
		if !isJSONObject(state.call.Arguments) {
			// 模型偶尔把 XML 参数语法泄漏进 arguments（上游实测），
			// 先解回 JSON，解不开兜底 {}。
			if repaired, ok := repairLeakedXMLArguments(state.call.Arguments); ok {
				state.call.Arguments = repaired
			} else {
				state.call.Arguments = "{}"
			}
		}
		d.result.ToolCalls = append(d.result.ToolCalls, state.call)
	}
	return append(events, Event{Kind: EventDone, Message: &d.result})
}

func (d *responseDecoder) fail(err error) []Event {
	d.result.StopReason = StopReasonError
	d.result.ErrorMessage = err.Error()
	return []Event{{Kind: EventError, Message: &d.result, Err: err}}
}

// mapStopReason 把上游 stop_reason 枚举归一到 OpenAI finish_reason 语义。
func mapStopReason(reason devinproto.ExaCodeiumCommonPb_StopReason) string {
	switch reason {
	case devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_MAX_TOKENS,
		devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_MAX_NEWLINES,
		devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_INCOMPLETE,
		devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_PARTIAL:
		return StopReasonLength
	case devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_FUNCTION_CALL:
		return StopReasonToolCalls
	case devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_CONTENT_FILTER:
		return StopReasonContentFilter
	case devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_ERROR,
		devinproto.ExaCodeiumCommonPb_StopReason_ExaCodeiumCommonPb_StopReason_STOP_REASON_NONFINITE_LOGIT_OR_PROB:
		return StopReasonError
	default:
		return StopReasonStop
	}
}

// isJSONObject 判断文本是否为完整 JSON 对象。
func isJSONObject(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	var object map[string]any
	return json.Unmarshal([]byte(trimmed), &object) == nil
}

// leakedXMLParameterPattern 匹配泄漏进 arguments 的 XML 参数片段。
var leakedXMLParameterPattern = regexp.MustCompile(`<(?:antml:)?parameter\s+name="([A-Za-z_][\w-]*)"[^>]*>([\s\S]*?)</(?:antml:)?parameter>`)

// repairLeakedXMLArguments 把混进 arguments 的 XML 参数标签提取成 JSON 对象。
func repairLeakedXMLArguments(raw string) (string, bool) {
	matches := leakedXMLParameterPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return "", false
	}
	object := make(map[string]string, len(matches))
	for _, match := range matches {
		object[match[1]] = strings.TrimSpace(match[2])
	}
	data, _ := json.Marshal(object)
	return string(data), true
}
