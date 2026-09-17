package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	devinpkg "github.com/Wei-Shaw/sub2api/internal/pkg/devin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Devin 协议桥接：上游只有 Connect-RPC chat 一条通道，这里把 OpenAI Responses
// 与 Anthropic Messages 请求先归一化为 Chat Completions，复用 devin 转发核心，
// 再把产出的 chat.completion.chunk 流二次转换回客户端协议。
//
// WebSocket（Responses WS v2）不在桥接范围内——它是有状态长连协议，保持 501。

// devinBridgeOutput 是 devin 事件流到下游协议的写出器。
type devinBridgeOutput struct {
	// writeChunk 消费一个 chat.completion.chunk JSON（流式）。
	writeChunk func(chunkJSON []byte) error
	// finishStream 流正常终止时的协议收尾（[DONE] 或终止事件）。
	finishStream func() error
	// writeFull 消费完整 chat.completion JSON（非流式）。
	writeFull func(ccJSON []byte) error
}

// forwardDevinResponses 处理 Devin 分组的 POST /v1/responses。
func (h *OpenAIGatewayHandler) forwardDevinResponses(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	sessionKey string,
	streamStarted *bool,
) (*service.OpenAIForwardResult, error) {
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		return nil, devinBridgeRequestError("Failed to parse request body")
	}
	effectiveTools, err := apicompat.EffectiveResponsesTools(&responsesReq)
	if err != nil {
		return nil, devinBridgeRequestError(err.Error())
	}
	chatReq, err := service.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		return nil, devinBridgeRequestError(err.Error())
	}
	ccBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal devin bridge request: %w", err)
	}
	out := devinResponsesOutput(
		c,
		strings.TrimSpace(chatReq.Model),
		apicompat.CustomToolNames(effectiveTools),
		apicompat.HasToolSearchTool(effectiveTools),
		apicompat.NamespaceToolNames(effectiveTools),
	)
	return h.forwardDevinBridged(ctx, c, account, ccBody, sessionKey, streamStarted, out)
}

// forwardDevinAnthropic 处理 Devin 分组的 POST /v1/messages。
func (h *OpenAIGatewayHandler) forwardDevinAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	sessionKey string,
	streamStarted *bool,
) (*service.OpenAIForwardResult, error) {
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		return nil, devinBridgeRequestError("Failed to parse request body")
	}
	chatReq, err := service.AnthropicToChatCompletionsRequest(&anthropicReq)
	if err != nil {
		return nil, devinBridgeRequestError(err.Error())
	}
	ccBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal devin bridge request: %w", err)
	}
	out := devinAnthropicOutput(c, strings.TrimSpace(chatReq.Model))
	return h.forwardDevinBridged(ctx, c, account, ccBody, sessionKey, streamStarted, out)
}

// forwardDevinBridged 是桥接路径的公共核心：解析 CC 请求 → devin 上游 →
// 按 out 写出器把 CC 事件流/聚合响应转换成目标协议。
func (h *OpenAIGatewayHandler) forwardDevinBridged(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	ccBody []byte,
	sessionKey string,
	streamStarted *bool,
	out *devinBridgeOutput,
) (*service.OpenAIForwardResult, error) {
	if h.devinGatewayService == nil {
		return nil, &service.UpstreamFailoverError{
			StatusCode:        http.StatusBadGateway,
			Reason:            service.GatewayFailureReason("devin_service_unavailable"),
			NextAccountAction: service.NextAccountStop,
		}
	}
	chatReq, reqModel, reqStream, err := parseDevinChatRequest(ccBody)
	if err != nil {
		return nil, devinBridgeRequestError(err.Error())
	}
	chatReq.SessionKey = sessionKey

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
		err = h.pumpDevinStreamCore(ctx, account, chat.Stream, reqModel, result, streamStarted, out.writeChunk, out.finishStream)
	} else {
		var response map[string]any
		response, err = h.collectDevinCCResponse(ctx, account, chat.Stream, reqModel, result)
		if err == nil {
			*streamStarted = true
			var ccJSON []byte
			if ccJSON, err = json.Marshal(response); err == nil {
				err = out.writeFull(ccJSON)
			}
		}
	}
	result.Duration = time.Since(start)
	if err != nil {
		return result, h.devinFailoverError(err, *streamStarted)
	}
	result.BillingUsageComplete = result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0
	return result, nil
}

// devinBridgeRequestError 构造客户端请求错误（不可换账号重试）。
func devinBridgeRequestError(message string) error {
	return &service.UpstreamFailoverError{
		StatusCode:        http.StatusBadRequest,
		Scope:             service.GatewayFailureScopeRequest,
		Reason:            service.GatewayFailureReason("devin_request_invalid"),
		NextAccountAction: service.NextAccountStop,
		ClientStatusCode:  http.StatusBadRequest,
		ClientMessage:     message,
	}
}

// devinResponsesOutput 把 chat.completion.chunk 流转写为 Responses SSE，
// 非流式聚合为 Responses 响应 JSON。
func devinResponsesOutput(
	c *gin.Context,
	model string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
) *devinBridgeOutput {
	state := service.NewChatCompletionsToResponsesStreamState(model)
	state.CustomTools = customTools
	state.ToolSearchDeclared = toolSearch
	state.NamespaceTools = namespaceTools

	headersSent := false
	flusher, _ := c.Writer.(http.Flusher)
	writeEvents := func(events []apicompat.ResponsesStreamEvent) error {
		if len(events) == 0 {
			return nil
		}
		if !headersSent {
			headersSent = true
			header := c.Writer.Header()
			header.Set("Content-Type", "text/event-stream")
			header.Set("Cache-Control", "no-cache")
			header.Set("Connection", "keep-alive")
			header.Set("X-Accel-Buffering", "no")
			c.Writer.WriteHeader(http.StatusOK)
		}
		for _, event := range events {
			sse, err := apicompat.ResponsesEventToSSE(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				return err
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	return &devinBridgeOutput{
		writeChunk: func(chunkJSON []byte) error {
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal(chunkJSON, &chunk); err != nil {
				return nil // 单 chunk 损坏不致命，跳过
			}
			return writeEvents(service.ChatCompletionsChunkToResponsesEvents(&chunk, state))
		},
		finishStream: func() error {
			return writeEvents(service.FinalizeChatCompletionsResponsesStream(state))
		},
		writeFull: func(ccJSON []byte) error {
			var ccResp apicompat.ChatCompletionsResponse
			if err := json.Unmarshal(ccJSON, &ccResp); err != nil {
				return fmt.Errorf("parse aggregated chat response: %w", err)
			}
			c.JSON(http.StatusOK, service.ChatCompletionsResponseToResponses(&ccResp, model, customTools, toolSearch, namespaceTools))
			return nil
		},
	}
}

// devinAnthropicOutput 把 chat.completion.chunk 流转写为 Anthropic Messages SSE，
// 非流式聚合为 Anthropic message JSON。
func devinAnthropicOutput(c *gin.Context, model string) *devinBridgeOutput {
	state := service.NewChatCompletionsToAnthropicStreamState(model)

	headersSent := false
	flusher, _ := c.Writer.(http.Flusher)
	writeEvents := func(events []apicompat.AnthropicStreamEvent) error {
		if len(events) == 0 {
			return nil
		}
		if !headersSent {
			headersSent = true
			header := c.Writer.Header()
			header.Set("Content-Type", "text/event-stream")
			header.Set("Cache-Control", "no-cache")
			header.Set("Connection", "keep-alive")
			header.Set("X-Accel-Buffering", "no")
			c.Writer.WriteHeader(http.StatusOK)
		}
		for _, event := range events {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				return err
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	return &devinBridgeOutput{
		writeChunk: func(chunkJSON []byte) error {
			var chunk apicompat.ChatCompletionsChunk
			if err := json.Unmarshal(chunkJSON, &chunk); err != nil {
				return nil
			}
			return writeEvents(service.ChatCompletionsChunkToAnthropicEvents(&chunk, state))
		},
		finishStream: func() error {
			return writeEvents(service.FinalizeChatCompletionsAnthropicStream(state))
		},
		writeFull: func(ccJSON []byte) error {
			var ccResp apicompat.ChatCompletionsResponse
			if err := json.Unmarshal(ccJSON, &ccResp); err != nil {
				return fmt.Errorf("parse aggregated chat response: %w", err)
			}
			c.JSON(http.StatusOK, service.ChatCompletionsResponseToAnthropic(&ccResp, model))
			return nil
		},
	}
}
