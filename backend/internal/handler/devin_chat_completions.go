package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	devinpkg "github.com/Wei-Shaw/sub2api/internal/pkg/devin"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// forwardDevinChatCompletions 把 OpenAI chat/completions 请求桥接到 Devin
// 内部 Connect RPC（ApiServerService.GetChatMessage），产出与
// ForwardAsChatCompletions 同构的 OpenAIForwardResult，计费/日志/调度上报
// 全部复用既有链路。
//
// 错误契约：
//   - 尚未向客户端写字节 → *service.UpstreamFailoverError（默认切换下一账号）；
//     客户端错误（ClientFault）与流已开始后的错误 → NextAccountStop。
//   - 凭证失效 → Stage=AccountAuth，换账号重试并标记账号 error。
func (h *OpenAIGatewayHandler) forwardDevinChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	sessionKey string,
	streamStarted *bool,
) (*service.OpenAIForwardResult, error) {
	if h.devinGatewayService == nil {
		return nil, &service.UpstreamFailoverError{
			StatusCode:        http.StatusBadGateway,
			Reason:            service.GatewayFailureReason("devin_service_unavailable"),
			NextAccountAction: service.NextAccountStop,
		}
	}
	chatReq, reqModel, reqStream, err := parseDevinChatRequest(body)
	if err != nil {
		return nil, &service.UpstreamFailoverError{
			StatusCode:        http.StatusBadRequest,
			Scope:             service.GatewayFailureScopeRequest,
			Reason:            service.GatewayFailureReason("devin_request_invalid"),
			NextAccountAction: service.NextAccountStop,
			ClientStatusCode:  http.StatusBadRequest,
			ClientMessage:     err.Error(),
		}
	}
	chatReq.SessionKey = sessionKey

	// 账号级模型映射（与 OpenAI 转发一致）：body 里的模型是渠道映射结果，
	// 再经账号映射得到最终上游模型名。
	upstreamModel := account.GetMappedModel(reqModel)
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = reqModel
	}

	chat, err := h.devinGatewayService.Chat(ctx, account, chatReq, upstreamModel)
	if err != nil {
		if failure := devinpkg.Classify(err); failure != nil && failure.CredentialFailed {
			h.devinGatewayService.MarkCredentialFailure(ctx, account, failure)
		}
		return nil, h.devinFailoverError(err, false)
	}
	defer chat.Stream.Close()

	result := &service.OpenAIForwardResult{
		Model:            reqModel,
		UpstreamModel:    chat.UpstreamModel,
		UpstreamEndpoint: EndpointChatCompletions,
		Stream:           reqStream,
	}
	start := time.Now()
	if reqStream {
		err = h.pumpDevinStream(ctx, c, account, chat.Stream, reqModel, result, streamStarted)
	} else {
		err = h.collectDevinResponse(ctx, c, account, chat.Stream, reqModel, result, streamStarted)
	}
	result.Duration = time.Since(start)
	if err != nil {
		return result, h.devinFailoverError(err, *streamStarted)
	}
	result.BillingUsageComplete = result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0
	return result, nil
}

// isDevinGroupRequest 判定当前 API Key 是否绑定 Devin 分组。
// Devin 桥只实现 /v1/chat/completions；messages/responses/ws 端点必须提前拒绝，
// 避免请求落到 OpenAI/Anthropic 转发器后打到不支持的 wire 协议。
func isDevinGroupRequest(c *gin.Context) bool {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	return ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == service.PlatformDevin
}

// devinFailoverError 把 devin 错误分类为 UpstreamFailoverError。
// streamAlreadyWritten 为 true 时禁止换号（避免重复输出污染客户端流）。
func (h *OpenAIGatewayHandler) devinFailoverError(err error, streamAlreadyWritten bool) error {
	failure := devinpkg.Classify(err)
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"type": failure.Code, "message": failure.Message},
	})
	failover := &service.UpstreamFailoverError{
		StatusCode:   failure.StatusCode,
		ResponseBody: body,
		Reason:       service.GatewayFailureReason("devin_" + failure.Code),
	}
	switch {
	case failure.CredentialFailed:
		failover.Stage = service.GatewayFailureStageAccountAuth
		failover.Scope = service.GatewayFailureScopeAccount
		failover.ClientStatusCode = http.StatusBadGateway
		failover.ClientMessage = "Upstream credential rejected"
	case failure.ClientFault:
		failover.Scope = service.GatewayFailureScopeRequest
		failover.NextAccountAction = service.NextAccountStop
		failover.ClientStatusCode = failure.StatusCode
		failover.ClientMessage = failure.Message
	case streamAlreadyWritten:
		failover.NextAccountAction = service.NextAccountStop
		failover.ClientMessage = "Upstream stream interrupted"
	}
	return failover
}

// parseDevinChatRequest 把 OpenAI chat/completions 请求体投影为 Devin 中间请求。
// 返回 (请求, 客户端请求模型名, 是否流式)。
func parseDevinChatRequest(body []byte) (*devinpkg.ChatRequest, string, bool, error) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return nil, "", false, errors.New("messages is required")
	}

	req := &devinpkg.ChatRequest{}
	var systemParts []string
	for _, message := range messages.Array() {
		role := message.Get("role").String()
		switch role {
		case "system", "developer":
			if text := devinContentText(message.Get("content")); text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			text, images, err := devinUserContent(message.Get("content"))
			if err != nil {
				return nil, "", false, err
			}
			req.Messages = append(req.Messages, devinpkg.ChatMessage{Role: "user", Text: text, Images: images})
		case "assistant":
			msg := devinpkg.ChatMessage{Role: "assistant", Text: devinContentText(message.Get("content"))}
			for _, call := range message.Get("tool_calls").Array() {
				fn := call.Get("function")
				msg.ToolCalls = append(msg.ToolCalls, devinpkg.ToolCall{
					ID:        call.Get("id").String(),
					Name:      fn.Get("name").String(),
					Arguments: fn.Get("arguments").String(),
				})
			}
			req.Messages = append(req.Messages, msg)
		case "tool":
			req.Messages = append(req.Messages, devinpkg.ChatMessage{
				Role:       "tool",
				Text:       devinContentText(message.Get("content")),
				ToolCallID: message.Get("tool_call_id").String(),
			})
		default:
			return nil, "", false, fmt.Errorf("unsupported message role %q", role)
		}
	}
	req.SystemPrompt = strings.Join(systemParts, "\n\n")

	// 采样参数：max_completion_tokens 优先于 max_tokens（OpenAI 语义）。
	if v := gjson.GetBytes(body, "max_completion_tokens"); v.Exists() && v.Type == gjson.Number {
		n := v.Int()
		req.MaxTokens = &n
	} else if v := gjson.GetBytes(body, "max_tokens"); v.Exists() && v.Type == gjson.Number {
		n := v.Int()
		req.MaxTokens = &n
	}
	if v := gjson.GetBytes(body, "temperature"); v.Exists() && v.Type == gjson.Number {
		f := v.Float()
		req.Temperature = &f
	}
	if v := gjson.GetBytes(body, "top_p"); v.Exists() && v.Type == gjson.Number {
		f := v.Float()
		req.TopP = &f
	}
	if v := gjson.GetBytes(body, "top_k"); v.Exists() && v.Type == gjson.Number {
		n := int(v.Int())
		req.TopK = &n
	}
	if v := gjson.GetBytes(body, "seed"); v.Exists() && v.Type == gjson.Number {
		n := v.Int()
		req.Seed = &n
	}
	if stop := gjson.GetBytes(body, "stop"); stop.Exists() {
		if stop.IsArray() {
			for _, item := range stop.Array() {
				if s := strings.TrimSpace(item.String()); s != "" {
					req.StopSequences = append(req.StopSequences, s)
				}
			}
		} else if s := strings.TrimSpace(stop.String()); s != "" {
			req.StopSequences = append(req.StopSequences, s)
		}
	}

	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		if tool.Get("type").String() != "function" {
			return nil, "", false, fmt.Errorf("unsupported tool type %q (only function)", tool.Get("type").String())
		}
		fn := tool.Get("function")
		name := strings.TrimSpace(fn.Get("name").String())
		if name == "" {
			return nil, "", false, errors.New("tool function.name is required")
		}
		def := devinpkg.ToolDefinition{Name: name, Description: fn.Get("description").String(), Strict: fn.Get("strict").Bool()}
		if params := fn.Get("parameters"); params.Exists() {
			def.Parameters = json.RawMessage(params.Raw)
		}
		req.Tools = append(req.Tools, def)
	}

	if choice := gjson.GetBytes(body, "tool_choice"); choice.Exists() {
		switch {
		case choice.Type == gjson.String:
			switch choice.String() {
			case "auto":
				req.ToolChoice = &devinpkg.ToolChoice{Mode: devinpkg.ToolChoiceAuto}
			case "none":
				req.ToolChoice = &devinpkg.ToolChoice{Mode: devinpkg.ToolChoiceNone}
			case "required":
				req.ToolChoice = &devinpkg.ToolChoice{Mode: devinpkg.ToolChoiceRequired}
			default:
				return nil, "", false, fmt.Errorf("unsupported tool_choice %q", choice.String())
			}
		case choice.IsObject():
			name := choice.Get("function.name").String()
			if name == "" {
				return nil, "", false, errors.New("tool_choice.function.name is required")
			}
			req.ToolChoice = &devinpkg.ToolChoice{Mode: devinpkg.ToolChoiceNamed, ToolName: name}
		}
	}
	if v := gjson.GetBytes(body, "parallel_tool_calls"); v.Exists() && !v.Bool() {
		req.DisableParallelToolCalls = true
	}

	return req, gjson.GetBytes(body, "model").String(), gjson.GetBytes(body, "stream").Bool(), nil
}

// devinContentText 提取 OpenAI content 字段的纯文本（string 或 text parts 拼接）。
func devinContentText(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var parts []string
	for _, part := range content.Array() {
		switch part.Get("type").String() {
		case "text", "output_text":
			parts = append(parts, part.Get("text").String())
		}
	}
	return strings.Join(parts, "\n")
}

// devinUserContent 提取 user 消息的文本与图片；图片仅接受 data: URL。
func devinUserContent(content gjson.Result) (string, []devinpkg.Image, error) {
	if !content.Exists() || content.Type == gjson.String {
		return devinContentText(content), nil, nil
	}
	if !content.IsArray() {
		return "", nil, errors.New("unsupported user message content")
	}
	var texts []string
	var images []devinpkg.Image
	for _, part := range content.Array() {
		switch part.Get("type").String() {
		case "text":
			texts = append(texts, part.Get("text").String())
		case "image_url":
			url := part.Get("image_url.url").String()
			if !strings.HasPrefix(url, "data:") {
				return "", nil, errors.New("image_url must be a data: URL (remote image fetch is not supported for Devin)")
			}
			mime := "image/png"
			if head, _, ok := strings.Cut(strings.TrimPrefix(url, "data:"), ";"); ok && head != "" {
				mime = head
			}
			images = append(images, devinpkg.Image{Data: url, MIMEType: mime})
		default:
			return "", nil, fmt.Errorf("unsupported user content part type %q", part.Get("type").String())
		}
	}
	return strings.Join(texts, "\n"), images, nil
}

// devinChunk 是 chat.completion.chunk 的最小投影。
func devinChunkJSON(id, model string, created int64, delta map[string]any, finishReason *string, usage map[string]any) []byte {
	choice := map[string]any{"index": 0, "delta": delta}
	if finishReason != nil {
		choice["finish_reason"] = *finishReason
	} else {
		choice["finish_reason"] = nil
	}
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{choice},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	data, _ := json.Marshal(chunk)
	return data
}

func devinUsageJSON(usage *devinpkg.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	prompt := usage.InputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	result := map[string]any{
		"prompt_tokens":     prompt,
		"completion_tokens": usage.OutputTokens,
		"total_tokens":      prompt + usage.OutputTokens,
	}
	if usage.CacheReadTokens > 0 {
		result["prompt_tokens_details"] = map[string]any{"cached_tokens": usage.CacheReadTokens}
	}
	return result
}

// fillDevinUsage 把上游累计 usage 投影为 OpenAIUsage。
// 上游 input_tokens 是不含 cache 的裸输入；OpenAIUsage.InputTokens 语义为
// 含 cache 的总输入（openAIUsageTokens 内部会再减出裸输入），因此这里加回。
func fillDevinUsage(result *service.OpenAIForwardResult, usage devinpkg.Usage) {
	result.Usage = service.OpenAIUsage{
		InputTokens:              int(usage.InputTokens + usage.CacheReadTokens + usage.CacheWriteTokens),
		OutputTokens:             int(usage.OutputTokens),
		CacheReadInputTokens:     int(usage.CacheReadTokens),
		CacheCreationInputTokens: int(usage.CacheWriteTokens),
	}
	if usage.ResponseID != "" {
		result.ResponseID = usage.ResponseID
	}
	if usage.UpstreamRequestID != "" {
		result.RequestID = usage.UpstreamRequestID
	}
	if usage.BillingModelUID != "" {
		result.BillingModel = usage.BillingModelUID
	}
	if usage.ResponseModel != "" {
		result.UpstreamResponseModel = usage.ResponseModel
	}
}

// pumpDevinStream 把 Devin 事件流转写为 OpenAI chat.completion.chunk SSE。
func (h *OpenAIGatewayHandler) pumpDevinStream(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	stream *devinpkg.ChatStream,
	reqModel string,
	result *service.OpenAIForwardResult,
	streamStarted *bool,
) error {
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")

	flusher, _ := c.Writer.(http.Flusher)
	return h.pumpDevinStreamCore(ctx, account, stream, reqModel, result, streamStarted,
		func(chunk []byte) error {
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", chunk); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		},
		func() error {
			if _, err := fmt.Fprint(c.Writer, "data: [DONE]\n\n"); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		},
	)
}

// pumpDevinStreamCore 把 devin 事件流转成连续的 chat.completion.chunk JSON，
// 由 emitChunk 决定下游协议形态（OpenAI SSE / Responses SSE / Anthropic SSE），
// finish 负责协议收尾（[DONE] 或各协议的终止事件）。
func (h *OpenAIGatewayHandler) pumpDevinStreamCore(
	ctx context.Context,
	account *service.Account,
	stream *devinpkg.ChatStream,
	reqModel string,
	result *service.OpenAIForwardResult,
	streamStarted *bool,
	emitChunk func(chunkJSON []byte) error,
	finish func() error,
) error {
	created := time.Now().Unix()
	responseID := "chatcmpl-" + strings.ReplaceAll(strings.ToLower(fmt.Sprintf("%x", created)), " ", "")
	roleSent := false
	toolIndexes := make(map[string]int)
	firstTokenAt := time.Now()

	writeChunk := func(delta map[string]any, finishReason *string, usage map[string]any) error {
		return emitChunk(devinChunkJSON(responseID, reqModel, created, delta, finishReason, usage))
	}
	sendRole := func() error {
		if roleSent {
			return nil
		}
		roleSent = true
		return writeChunk(map[string]any{"role": "assistant"}, nil, nil)
	}

	for {
		events, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		for _, event := range events {
			switch event.Kind {
			case devinpkg.EventTextDelta:
				if err := sendRole(); err != nil {
					return err
				}
				*streamStarted = true
				if result.FirstTokenMs == nil {
					ms := int(time.Since(firstTokenAt).Milliseconds())
					result.FirstTokenMs = &ms
				}
				if err := writeChunk(map[string]any{"content": event.Text}, nil, nil); err != nil {
					return err
				}
			case devinpkg.EventReasoningDelta:
				if err := sendRole(); err != nil {
					return err
				}
				*streamStarted = true
				if err := writeChunk(map[string]any{"reasoning_content": event.Text}, nil, nil); err != nil {
					return err
				}
			case devinpkg.EventToolCallStart:
				if err := sendRole(); err != nil {
					return err
				}
				*streamStarted = true
				index, ok := toolIndexes[event.ToolCallID]
				if !ok {
					index = len(toolIndexes)
					toolIndexes[event.ToolCallID] = index
				}
				delta := map[string]any{"tool_calls": []any{map[string]any{
					"index": index,
					"id":    event.ToolCallID,
					"type":  "function",
					"function": map[string]any{
						"name":      event.ToolName,
						"arguments": "",
					},
				}}}
				if err := writeChunk(delta, nil, nil); err != nil {
					return err
				}
			case devinpkg.EventToolCallDelta:
				index, ok := toolIndexes[event.ToolCallID]
				if !ok {
					continue
				}
				delta := map[string]any{"tool_calls": []any{map[string]any{
					"index":    index,
					"function": map[string]any{"arguments": event.Text},
				}}}
				if err := writeChunk(delta, nil, nil); err != nil {
					return err
				}
			case devinpkg.EventDone:
				if event.Message != nil {
					fillDevinUsage(result, event.Message.Usage)
					finish := event.Message.StopReason
					if finish == "" {
						finish = devinpkg.StopReasonStop
					}
					if err := sendRole(); err != nil {
						return err
					}
					if err := writeChunk(map[string]any{}, &finish, devinUsageJSON(&event.Message.Usage)); err != nil {
						return err
					}
				}
				return finish()
			case devinpkg.EventError:
				if event.Message != nil {
					fillDevinUsage(result, event.Message.Usage)
				}
				if failure := devinpkg.Classify(event.Err); failure != nil && failure.CredentialFailed {
					h.devinGatewayService.MarkCredentialFailure(ctx, account, failure)
				}
				if event.Err != nil {
					return event.Err
				}
				return errors.New("devin stream error")
			}
		}
	}
	return errors.New("devin stream ended without a terminal event")
}

// collectDevinResponse 聚合非流式响应为单个 chat.completion JSON。
func (h *OpenAIGatewayHandler) collectDevinResponse(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	stream *devinpkg.ChatStream,
	reqModel string,
	result *service.OpenAIForwardResult,
	streamStarted *bool,
) error {
	response, err := h.collectDevinCCResponse(ctx, account, stream, reqModel, result)
	if err != nil {
		return err
	}
	*streamStarted = true
	c.Header("Content-Type", "application/json")
	c.JSON(http.StatusOK, response)
	return nil
}

// collectDevinCCResponse 排空 devin 流并聚合为 chat.completion JSON map，
// 供 OpenAI 直写或协议桥接（Responses/Anthropic）二次转换。
func (h *OpenAIGatewayHandler) collectDevinCCResponse(
	ctx context.Context,
	account *service.Account,
	stream *devinpkg.ChatStream,
	reqModel string,
	result *service.OpenAIForwardResult,
) (map[string]any, error) {
	var done *devinpkg.AssistantResult
	for {
		events, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			switch event.Kind {
			case devinpkg.EventDone:
				done = event.Message
			case devinpkg.EventError:
				if event.Message != nil {
					fillDevinUsage(result, event.Message.Usage)
				}
				if failure := devinpkg.Classify(event.Err); failure != nil && failure.CredentialFailed {
					h.devinGatewayService.MarkCredentialFailure(ctx, account, failure)
				}
				if event.Err != nil {
					return nil, event.Err
				}
				return nil, errors.New("devin stream error")
			}
		}
	}
	if done == nil {
		return nil, errors.New("devin stream ended without a terminal event")
	}
	fillDevinUsage(result, done.Usage)
	firstMs := int(result.Duration.Milliseconds())
	result.FirstTokenMs = &firstMs

	message := map[string]any{"role": "assistant"}
	if done.Text != "" {
		message["content"] = done.Text
	} else {
		message["content"] = nil
	}
	if done.Reasoning != "" {
		message["reasoning_content"] = done.Reasoning
	}
	if len(done.ToolCalls) > 0 {
		calls := make([]any, 0, len(done.ToolCalls))
		for i, call := range done.ToolCalls {
			calls = append(calls, map[string]any{
				"index": i,
				"id":    call.ID,
				"type":  "function",
				"function": map[string]any{
					"name":      call.Name,
					"arguments": call.Arguments,
				},
			})
		}
		message["tool_calls"] = calls
	}
	finish := done.StopReason
	if finish == "" {
		finish = devinpkg.StopReasonStop
	}
	response := map[string]any{
		"id":      "chatcmpl-" + done.ResponseID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   reqModel,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
		"usage": devinUsageJSON(&done.Usage),
	}
	if response["id"] == "chatcmpl-" {
		response["id"] = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	return response, nil
}
