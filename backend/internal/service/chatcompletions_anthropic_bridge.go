package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// This file implements a DIRECT bridge between Anthropic Messages and OpenAI
// Chat Completions, skipping the Responses API intermediate representation.
//
// The existing chat-fallback path (forwardAnthropicViaRawChatCompletions) chains
// two Responses-anchored bridges — Anthropic→Responses→ChatCompletions on the
// request side and CC→Responses→Anthropic on the response side — so every
// streaming token runs through two state machines. For force-chat accounts
// (third-party OpenAI-compatible upstreams that only speak /v1/chat/completions)
// the Responses layer is pure overhead: these upstreams never see Responses
// semantics, and the clients reaching them via /v1/messages use standard
// function tools (no custom/tool_search/namespace Codex constructs).
//
// The direct bridge collapses both directions into a single conversion each:
//
//	Request:  Anthropic Messages → Chat Completions
//	Response: CC chunk/response → Anthropic events/response
//
// Helper functions from the Responses bridges (anthropicImageToDataURI,
// extractAnthropicTextFromBlocks, fromResponsesCallID, sanitizeAnthropicToolUseInput,
// parseAnthropicSystemContentParts, isReasoningModel, mapAnthropicEffortToResponses,
// normalizeToolParameters) are reused so the conversion semantics stay identical.

// ---------------------------------------------------------------------------
// Request: apicompat.AnthropicRequest → apicompat.ChatCompletionsRequest
// ---------------------------------------------------------------------------

// AnthropicToChatCompletionsRequest converts an Anthropic Messages request
// directly into a Chat Completions request, without transiting the Responses
// API. It is semantically equivalent to composing AnthropicToResponses +
// ResponsesToChatCompletionsRequest but avoids materializing the intermediate
// ResponsesRequest and the extra marshal/unmarshal cycle.
func AnthropicToChatCompletionsRequest(req *apicompat.AnthropicRequest) (*apicompat.ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("anthropic request is nil")
	}

	messages, err := anthropicToChatMessages(req.System, req.Messages)
	if err != nil {
		return nil, err
	}

	out := &apicompat.ChatCompletionsRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   req.Stream,
	}

	// Sampling params: reasoning models (gpt-5.x) reject temperature/top_p.
	if !isReasoningModel(req.Model) {
		out.Temperature = req.Temperature
		out.TopP = req.TopP
	}

	if req.MaxTokens > 0 {
		v := req.MaxTokens
		if v < minMaxOutputTokens {
			v = minMaxOutputTokens
		}
		out.MaxCompletionTokens = &v
	}

	// Tools: Anthropic input_schema is a JSON Schema, directly usable as Chat
	// function parameters. Server tools (web_search_*) have no Chat Completions
	// equivalent and are dropped (mirrors responsesToolsToChatTools).
	if len(req.Tools) > 0 {
		tools := anthropicToolsToChatTools(req.Tools)
		if len(tools) > 0 {
			out.Tools = tools
		}
	}

	// tool_choice is only forwarded when tools survived the conversion
	// (upstream rejects tool_choice without tools).
	if len(out.Tools) > 0 && len(req.ToolChoice) > 0 {
		tc, err := convertAnthropicToolChoiceToChat(req.ToolChoice)
		if err != nil {
			return nil, fmt.Errorf("convert tool_choice: %w", err)
		}
		out.ToolChoice = tc
	}

	// Reasoning effort: output_config.effort maps 1:1 (max→xhigh). thinking.type
	// itself is ignored (the Responses bridge behaves identically).
	effort := "medium"
	if req.OutputConfig != nil && req.OutputConfig.Effort != "" {
		effort = req.OutputConfig.Effort
	}
	out.ReasoningEffort = mapAnthropicEffortToResponses(effort)

	parallelToolCalls := true
	out.ParallelToolCalls = &parallelToolCalls

	return out, nil
}

// anthropicToChatMessages converts the Anthropic system field + message list
// into Chat Completions messages. It mirrors convertAnthropicToResponsesInput +
// responsesInputToChatMessages but produces apicompat.ChatMessage directly.
func anthropicToChatMessages(system json.RawMessage, msgs []apicompat.AnthropicMessage) ([]apicompat.ChatMessage, error) {
	var messages []apicompat.ChatMessage

	// System prompt → system message. parseAnthropicSystemContentParts handles
	// both string and []block forms and filters the billing header.
	if len(system) > 0 {
		sysParts, err := parseAnthropicSystemContentParts(system)
		if err != nil {
			return nil, err
		}
		if len(sysParts) > 0 {
			text := joinResponsesContentPartText(sysParts)
			if text != "" {
				content, _ := json.Marshal(text)
				messages = append(messages, apicompat.ChatMessage{Role: "system", Content: content})
			}
		}
	}

	for _, m := range msgs {
		converted, err := anthropicMsgToChatMessages(m)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	}

	return normalizeChatMessages(messages), nil
}

// anthropicMsgToChatMessages converts one Anthropic message into one or more
// Chat messages. tool_result blocks become standalone "tool" role messages
// (the Chat Completions convention); text/image blocks stay in a user message;
// assistant tool_use blocks become tool_calls on the assistant message.
func anthropicMsgToChatMessages(m apicompat.AnthropicMessage) ([]apicompat.ChatMessage, error) {
	switch m.Role {
	case "assistant":
		return anthropicAssistantToChatMessages(m.Content)
	default: // "user" and any unknown role
		return anthropicUserToChatMessages(m.Content)
	}
}

// anthropicUserToChatMessages handles an Anthropic user message. Content may be
// a plain string or an array of blocks. tool_result blocks are extracted into
// standalone "tool" role messages; images inside tool_results are lifted into a
// follow-up user message as image_url parts (the Responses bridge does the same
// — function_call_output only accepts strings, so images must travel separately).
func anthropicUserToChatMessages(raw json.RawMessage) ([]apicompat.ChatMessage, error) {
	// Plain string → single user message.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		content, _ := json.Marshal(s)
		return []apicompat.ChatMessage{{Role: "user", Content: content}}, nil
	}

	var blocks []apicompat.AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	var out []apicompat.ChatMessage
	var toolResultImageParts []apicompat.ChatContentPart

	// tool_result → "tool" role messages, text extracted; images deferred.
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		text, imageParts := convertToolResultOutput(b)
		content, _ := json.Marshal(text)
		out = append(out, apicompat.ChatMessage{
			Role:       "tool",
			Content:    content,
			ToolCallID: b.ToolUseID,
		})
		for _, ip := range imageParts {
			toolResultImageParts = append(toolResultImageParts, apicompat.ChatContentPart{
				Type:     "image_url",
				ImageURL: &apicompat.ChatImageURL{URL: ip.ImageURL},
			})
		}
	}

	// Remaining text + image blocks → user message with content parts.
	var parts []apicompat.ChatContentPart
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, apicompat.ChatContentPart{Type: "text", Text: b.Text})
			}
		case "image":
			if uri := anthropicImageToDataURI(b.Source); uri != "" {
				parts = append(parts, apicompat.ChatContentPart{
					Type:     "image_url",
					ImageURL: &apicompat.ChatImageURL{URL: uri},
				})
			}
		}
	}
	parts = append(parts, toolResultImageParts...)

	if len(parts) > 0 {
		// Mixed/structured content → array form; single text → string form
		// (normalizeChatMessages will collapse a single-text-part array to a
		// plain string if the upstream prefers it).
		content, err := json.Marshal(parts)
		if err != nil {
			return nil, err
		}
		out = append(out, apicompat.ChatMessage{Role: "user", Content: content})
	}

	return out, nil
}

// anthropicAssistantToChatMessages handles an Anthropic assistant message.
// Text content → assistant message content; tool_use blocks → tool_calls on the
// same assistant message; thinking blocks are dropped (Chat Completions has no
// inbound thinking field, matching anthropicAssistantToResponses).
func anthropicAssistantToChatMessages(raw json.RawMessage) ([]apicompat.ChatMessage, error) {
	// Plain string → single assistant message.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		content, _ := json.Marshal(s)
		return []apicompat.ChatMessage{{Role: "assistant", Content: content}}, nil
	}

	var blocks []apicompat.AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	msg := apicompat.ChatMessage{Role: "assistant"}
	text := extractAnthropicTextFromBlocks(blocks)
	if text != "" {
		content, _ := json.Marshal(text)
		msg.Content = content
	}

	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		args := "{}"
		if len(b.Input) > 0 {
			args = string(b.Input)
		}
		msg.ToolCalls = append(msg.ToolCalls, apicompat.ChatToolCall{
			ID:   b.ID,
			Type: "function",
			Function: apicompat.ChatFunctionCall{
				Name:      b.Name,
				Arguments: args,
			},
		})
	}

	return []apicompat.ChatMessage{msg}, nil
}

// anthropicToolsToChatTools maps Anthropic tool definitions to Chat Completions
// function tools. Server-side tools (web_search_*) are dropped — they have no
// Chat Completions equivalent.
func anthropicToolsToChatTools(tools []apicompat.AnthropicTool) []apicompat.ChatTool {
	var out []apicompat.ChatTool
	for _, t := range tools {
		if strings.HasPrefix(t.Type, "web_search") {
			continue
		}
		out = append(out, apicompat.ChatTool{
			Type: "function",
			Function: &apicompat.ChatFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  normalizeToolParameters(t.InputSchema),
				Strict:      boolPtr(false),
			},
		})
	}
	return out
}

// convertAnthropicToolChoiceToChat maps Anthropic tool_choice to Chat
// Completions tool_choice.
//
//	{"type":"auto"}            → "auto"
//	{"type":"any"}             → "required"
//	{"type":"none"}            → "none"
//	{"type":"tool","name":"X"} → {"type":"function","function":{"name":"X"}}
func convertAnthropicToolChoiceToChat(raw json.RawMessage) (json.RawMessage, error) {
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil, err
	}

	switch tc.Type {
	case "auto":
		return json.Marshal("auto")
	case "any":
		return json.Marshal("required")
	case "none":
		return json.Marshal("none")
	case "tool":
		return json.Marshal(map[string]any{
			"type":     "function",
			"function": map[string]string{"name": tc.Name},
		})
	default:
		return raw, nil
	}
}

// joinResponsesContentPartText concatenates the text of input_text parts. Used
// for the system prompt where parseAnthropicSystemContentParts returns
// apicompat.ResponsesContentPart values.
func joinResponsesContentPartText(parts []apicompat.ResponsesContentPart) string {
	var texts []string
	for _, p := range parts {
		if p.Type == "input_text" && p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n\n")
}

// ---------------------------------------------------------------------------
// Non-streaming response: apicompat.ChatCompletionsResponse → apicompat.AnthropicResponse
// ---------------------------------------------------------------------------

// ChatCompletionsResponseToAnthropic converts a Chat Completions response
// directly into an Anthropic Messages response, without materializing a
// ResponsesResponse. It is semantically equivalent to composing
// ChatCompletionsResponseToResponses + ResponsesToAnthropic.
func ChatCompletionsResponseToAnthropic(resp *apicompat.ChatCompletionsResponse, model string) *apicompat.AnthropicResponse {
	out := &apicompat.AnthropicResponse{
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	if resp != nil {
		if out.ID == "" {
			out.ID = resp.ID
		}
		if out.Model == "" {
			out.Model = resp.Model
		}

		if len(resp.Choices) > 0 {
			choice := resp.Choices[0]
			out.Content = chatMessageToAnthropicBlocks(choice.Message)
			out.StopReason = chatFinishReasonToAnthropicStopReason(choice.FinishReason, out.Content)
		}
		if resp.Usage != nil {
			out.Usage = chatUsageToAnthropicUsage(resp.Usage)
		}
	}

	if len(out.Content) == 0 {
		out.Content = []apicompat.AnthropicContentBlock{{Type: "text", Text: ""}}
	}
	// Empty choices and nil responses never enter the branch above. Strict
	// Anthropic clients reject an empty stop_reason, while the former
	// Responses-based conversion reports a completed turn.
	if out.StopReason == "" {
		out.StopReason = chatFinishReasonToAnthropicStopReason("", out.Content)
	}
	// Preserve parity with the former double-conversion path, which always
	// generated a response identifier when the upstream omitted one.
	if out.ID == "" {
		out.ID = "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}

	return out
}

// chatMessageToAnthropicBlocks converts a Chat Completions message into
// Anthropic content blocks. Reasoning content → thinking block; text content →
// text block; tool_calls → tool_use blocks. Mirrors chatMessageToResponsesOutput
// + the reasoning→thinking mapping in ResponsesToAnthropic.
func chatMessageToAnthropicBlocks(message apicompat.ChatMessage) []apicompat.AnthropicContentBlock {
	var blocks []apicompat.AnthropicContentBlock

	if message.ReasoningContent != "" {
		blocks = append(blocks, apicompat.AnthropicContentBlock{
			Type:     "thinking",
			Thinking: message.ReasoningContent,
		})
	}

	text := chatMessageContentText(message.Content)
	// DeepSeek reasoning-only fallback: when there is no text and no tool calls,
	// surface the reasoning content as visible text so the turn isn't empty.
	if text == "" && strings.TrimSpace(message.ReasoningContent) != "" && len(message.ToolCalls) == 0 {
		text = message.ReasoningContent
	}
	if text != "" || len(message.ToolCalls) == 0 {
		blocks = append(blocks, apicompat.AnthropicContentBlock{Type: "text", Text: text})
	}

	for _, toolCall := range message.ToolCalls {
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		blocks = append(blocks, apicompat.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    fromResponsesCallID(toolCall.ID),
			Name:  toolCall.Function.Name,
			Input: sanitizeAnthropicToolUseInput(toolCall.Function.Name, arguments),
		})
	}

	return blocks
}

// chatFinishReasonToAnthropicStopReason maps Chat Completions finish_reason to
// Anthropic stop_reason.
//
//	"stop"           → "end_turn" (or "tool_use" if tool_use blocks present)
//	"length"         → "max_tokens"
//	"tool_calls"     → "tool_use"
//	"content_filter" → "end_turn"
func chatFinishReasonToAnthropicStopReason(reason string, blocks []apicompat.AnthropicContentBlock) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "stop":
		if containsAnthropicToolUseBlock(blocks) {
			return "tool_use"
		}
		return "end_turn"
	default:
		return "end_turn"
	}
}

// chatUsageToAnthropicUsage converts Chat Completions token usage to Anthropic
// usage shape. Mirrors ChatUsageToResponsesUsage + anthropicUsageFromResponsesUsage.
func chatUsageToAnthropicUsage(usage *apicompat.ChatUsage) apicompat.AnthropicUsage {
	if usage == nil {
		return apicompat.AnthropicUsage{}
	}

	cachedTokens := 0
	cacheCreationTokens := 0
	if usage.PromptTokensDetails != nil {
		cachedTokens = usage.PromptTokensDetails.CachedTokens
		cacheCreationTokens = chatCacheCreationTokens(usage.PromptTokensDetails)
	}

	inputTokens := usage.PromptTokens - cachedTokens - cacheCreationTokens
	if inputTokens < 0 {
		inputTokens = 0
	}

	return apicompat.AnthropicUsage{
		InputTokens:              inputTokens,
		OutputTokens:             usage.CompletionTokens,
		CacheReadInputTokens:     cachedTokens,
		CacheCreationInputTokens: cacheCreationTokens,
	}
}

// ---------------------------------------------------------------------------
// Streaming: apicompat.ChatCompletionsChunk → []apicompat.AnthropicStreamEvent (stateful converter)
// ---------------------------------------------------------------------------

// ChatCompletionsToAnthropicStreamState tracks state while converting Chat
// Completions SSE chunks directly into Anthropic SSE events. It collapses the
// ChatCompletionsToResponsesStreamState + ResponsesEventToAnthropicState pair
// into one state machine.
type ChatCompletionsToAnthropicStreamState struct {
	MessageStartSent bool
	MessageStopSent  bool

	// Current content block lifecycle.
	ContentBlockIndex   int
	ContentBlockOpen    bool
	CurrentBlockType    string // "text" | "thinking" | "tool_use"
	CurrentToolName     string
	CurrentToolArgs     string
	CurrentToolHadDelta bool
	HasToolCall         bool

	// Tool calls keyed by the upstream tool_call index. The Anthropic block
	// index assigned at content_block_start time is stored so later argument
	// deltas for the same tool land on the right block.
	toolBlockIndex    map[int]int
	toolAnnounced     map[int]bool
	toolName          map[int]string
	toolArgs          map[int]string
	toolHadDelta      map[int]bool
	pendingToolCallID map[int]string // call ID received before the name (deferred announce)

	// Reasoning (DeepSeek-style): reasoning_content streamed before content.
	// No separate reasoning block index — it uses ContentBlockIndex like the
	// Responses bridge's ReasoningIndex, but since blocks are sequential we
	// reuse the single ContentBlockIndex counter.

	FinishReason string

	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int

	ResponseID string
	Model      string
	Created    int64
}

// NewChatCompletionsToAnthropicStreamState returns an initialized stream state.
func NewChatCompletionsToAnthropicStreamState(model string) *ChatCompletionsToAnthropicStreamState {
	return &ChatCompletionsToAnthropicStreamState{
		ResponseID:        generateResponsesID(),
		Model:             model,
		Created:           time.Now().Unix(),
		toolBlockIndex:    make(map[int]int),
		toolAnnounced:     make(map[int]bool),
		toolName:          make(map[int]string),
		toolArgs:          make(map[int]string),
		toolHadDelta:      make(map[int]bool),
		pendingToolCallID: make(map[int]string),
	}
}

// ChatCompletionsChunkToAnthropicEvents converts one Chat Completions stream
// chunk into zero or more Anthropic stream events, updating state as it goes.
func ChatCompletionsChunkToAnthropicEvents(
	chunk *apicompat.ChatCompletionsChunk,
	state *ChatCompletionsToAnthropicStreamState,
) []apicompat.AnthropicStreamEvent {
	if chunk == nil || state == nil {
		return nil
	}
	if chunk.ID != "" {
		state.ResponseID = chunk.ID
	}
	if state.Model == "" && chunk.Model != "" {
		state.Model = chunk.Model
	}

	// Usage in a streaming chunk (include_usage) arrives in its own chunk,
	// often with empty choices. Capture it for the finalize message_delta.
	if chunk.Usage != nil {
		u := chatUsageToAnthropicUsage(chunk.Usage)
		state.InputTokens = u.InputTokens
		state.OutputTokens = u.OutputTokens
		state.CacheReadInputTokens = u.CacheReadInputTokens
		state.CacheCreationInputTokens = u.CacheCreationInputTokens
	}

	var events []apicompat.AnthropicStreamEvent
	events = append(events, ensureCCAnthropicMessageStart(state)...)

	for _, choice := range chunk.Choices {
		// Reasoning content → thinking block.
		if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			events = append(events, ensureCCAnthropicThinkingBlock(state)...)
			events = append(events, ccAnthropicDelta(state, &apicompat.AnthropicDelta{
				Type:     "thinking_delta",
				Thinking: *choice.Delta.ReasoningContent,
			})...)
		}

		// Text content → text block (closes any open thinking block first).
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			events = append(events, closeCCAnthropicBlockIfOpen(state, "thinking")...)
			events = append(events, ensureCCAnthropicTextBlock(state)...)
			events = append(events, ccAnthropicDelta(state, &apicompat.AnthropicDelta{
				Type: "text_delta",
				Text: *choice.Delta.Content,
			})...)
		}

		// Tool calls → tool_use blocks.
		for _, toolCall := range choice.Delta.ToolCalls {
			events = append(events, closeCCAnthropicBlockIfOpen(state, "thinking")...)
			events = append(events, handleCCAnthropicToolCall(state, &toolCall)...)
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			state.FinishReason = *choice.FinishReason
		}
	}

	return events
}

// FinalizeChatCompletionsAnthropicStream emits terminal Anthropic events
// (close open blocks + message_delta + message_stop) when the stream ends.
func FinalizeChatCompletionsAnthropicStream(state *ChatCompletionsToAnthropicStreamState) []apicompat.AnthropicStreamEvent {
	if state == nil || state.MessageStopSent {
		return nil
	}

	var events []apicompat.AnthropicStreamEvent
	if !state.MessageStartSent {
		events = append(events, ensureCCAnthropicMessageStart(state)...)
	}
	events = append(events, closeCCAnthropicBlock(state)...)

	stopReason := ccFinishReasonToAnthropicStopReason(state.FinishReason, state.HasToolCall)

	events = append(events,
		apicompat.AnthropicStreamEvent{
			Type: "message_delta",
			Delta: &apicompat.AnthropicDelta{
				StopReason: stopReason,
			},
			Usage: &apicompat.AnthropicUsage{
				InputTokens:              state.InputTokens,
				OutputTokens:             state.OutputTokens,
				CacheReadInputTokens:     state.CacheReadInputTokens,
				CacheCreationInputTokens: state.CacheCreationInputTokens,
			},
		},
		apicompat.AnthropicStreamEvent{Type: "message_stop"},
	)
	state.MessageStopSent = true
	return events
}

// ensureCCAnthropicMessageStart emits message_start on the first event.
func ensureCCAnthropicMessageStart(state *ChatCompletionsToAnthropicStreamState) []apicompat.AnthropicStreamEvent {
	if state.MessageStartSent {
		return nil
	}
	state.MessageStartSent = true
	return []apicompat.AnthropicStreamEvent{{
		Type: "message_start",
		Message: &apicompat.AnthropicResponse{
			ID:      state.ResponseID,
			Type:    "message",
			Role:    "assistant",
			Content: []apicompat.AnthropicContentBlock{},
			Model:   state.Model,
			Usage:   apicompat.AnthropicUsage{InputTokens: 0, OutputTokens: 0},
		},
	}}
}

// ensureCCAnthropicThinkingBlock opens a thinking block if none is open.
func ensureCCAnthropicThinkingBlock(state *ChatCompletionsToAnthropicStreamState) []apicompat.AnthropicStreamEvent {
	if state.ContentBlockOpen && state.CurrentBlockType == "thinking" {
		return nil
	}
	events := closeCCAnthropicBlock(state)
	idx := state.ContentBlockIndex
	state.ContentBlockOpen = true
	state.CurrentBlockType = "thinking"
	events = append(events, apicompat.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &apicompat.AnthropicContentBlock{
			Type:     "thinking",
			Thinking: "",
		},
	})
	return events
}

// ensureCCAnthropicTextBlock opens a text block if none is open.
func ensureCCAnthropicTextBlock(state *ChatCompletionsToAnthropicStreamState) []apicompat.AnthropicStreamEvent {
	if state.ContentBlockOpen && state.CurrentBlockType == "text" {
		return nil
	}
	events := closeCCAnthropicBlock(state)
	idx := state.ContentBlockIndex
	state.ContentBlockOpen = true
	state.CurrentBlockType = "text"
	events = append(events, apicompat.AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx,
		ContentBlock: &apicompat.AnthropicContentBlock{
			Type: "text",
			Text: "",
		},
	})
	return events
}

// handleCCAnthropicToolCall processes one upstream tool_call delta. A new index
// opens a tool_use block (deferred if the name hasn't arrived yet); argument
// fragments emit input_json_delta on the tool's block.
func handleCCAnthropicToolCall(state *ChatCompletionsToAnthropicStreamState, toolCall *apicompat.ChatToolCall) []apicompat.AnthropicStreamEvent {
	idx := 0
	if toolCall.Index != nil {
		idx = *toolCall.Index
	}

	var events []apicompat.AnthropicStreamEvent

	if _, ok := state.toolBlockIndex[idx]; !ok {
		// New tool call. Close any open non-tool block first.
		events = append(events, closeCCAnthropicBlock(state)...)
		blockIdx := state.ContentBlockIndex
		state.toolBlockIndex[idx] = blockIdx
		state.HasToolCall = true

		// Open the tool_use block immediately if we have an ID + name; otherwise
		// defer the content_block_start until the name arrives.
		callID := toolCall.ID
		if callID == "" {
			callID = generateItemID()
		}
		name := toolCall.Function.Name
		if name != "" {
			state.toolAnnounced[idx] = true
			state.toolName[idx] = name
			state.CurrentToolName = name
			state.ContentBlockOpen = true
			state.CurrentBlockType = "tool_use"
			events = append(events, apicompat.AnthropicStreamEvent{
				Type:  "content_block_start",
				Index: &blockIdx,
				ContentBlock: &apicompat.AnthropicContentBlock{
					Type:  "tool_use",
					ID:    fromResponsesCallID(callID),
					Name:  name,
					Input: json.RawMessage("{}"),
				},
			})
		} else {
			state.toolAnnounced[idx] = false
			// Store the call ID so we can emit content_block_start when the
			// name arrives. We stash it in toolName prefixed with the ID marker
			// is unnecessary — keep the pending ID separately is cleaner, but
			// to avoid another map we re-derive: the next delta for this idx
			// with a name will announce. We still need the ID though.
			// Store ID in toolName as "id\x00" sentinel? No — add a field.
			state.pendingToolCallID[idx] = callID
		}
	} else {
		// Existing tool call: update ID/name if provided.
		if toolCall.Function.Name != "" && !state.toolAnnounced[idx] {
			blockIdx := state.toolBlockIndex[idx]
			name := toolCall.Function.Name
			state.toolAnnounced[idx] = true
			state.toolName[idx] = name
			state.CurrentToolName = name
			state.ContentBlockOpen = true
			state.CurrentBlockType = "tool_use"
			callID := state.pendingToolCallID[idx]
			if toolCall.ID != "" {
				callID = toolCall.ID
			}
			if callID == "" {
				callID = generateItemID()
			}
			events = append(events, apicompat.AnthropicStreamEvent{
				Type:  "content_block_start",
				Index: &blockIdx,
				ContentBlock: &apicompat.AnthropicContentBlock{
					Type:  "tool_use",
					ID:    fromResponsesCallID(callID),
					Name:  name,
					Input: json.RawMessage("{}"),
				},
			})
		}
	}

	// Argument fragment → input_json_delta on this tool's block.
	if toolCall.Function.Arguments != "" {
		state.toolArgs[idx] += toolCall.Function.Arguments
		state.CurrentToolArgs = state.toolArgs[idx]
		if blockIdx, ok := state.toolBlockIndex[idx]; ok && state.toolAnnounced[idx] {
			delta := toolCall.Function.Arguments
			if state.toolName[idx] == "Read" {
				if state.toolHadDelta[idx] || !json.Valid([]byte(state.toolArgs[idx])) {
					return events
				}
				delta = string(sanitizeAnthropicToolUseInput("Read", state.toolArgs[idx]))
			}
			state.toolHadDelta[idx] = true
			state.CurrentToolHadDelta = true
			events = append(events, apicompat.AnthropicStreamEvent{
				Type:  "content_block_delta",
				Index: &blockIdx,
				Delta: &apicompat.AnthropicDelta{
					Type:        "input_json_delta",
					PartialJSON: delta,
				},
			})
		}
	}

	return events
}

// ccAnthropicDelta emits a content_block_delta on the current block.
func ccAnthropicDelta(state *ChatCompletionsToAnthropicStreamState, delta *apicompat.AnthropicDelta) []apicompat.AnthropicStreamEvent {
	if !state.ContentBlockOpen {
		return nil
	}
	idx := state.ContentBlockIndex
	return []apicompat.AnthropicStreamEvent{{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: delta,
	}}
}

// closeCCAnthropicBlockIfOpen closes the current block only if it matches the
// given type (used to close a thinking block before opening text/tool).
func closeCCAnthropicBlockIfOpen(state *ChatCompletionsToAnthropicStreamState, blockType string) []apicompat.AnthropicStreamEvent {
	if !state.ContentBlockOpen || state.CurrentBlockType != blockType {
		return nil
	}
	return closeCCAnthropicBlock(state)
}

// closeCCAnthropicBlock closes the currently open content block.
func closeCCAnthropicBlock(state *ChatCompletionsToAnthropicStreamState) []apicompat.AnthropicStreamEvent {
	if !state.ContentBlockOpen {
		return nil
	}
	idx := state.ContentBlockIndex
	state.ContentBlockOpen = false
	state.ContentBlockIndex++
	state.CurrentBlockType = ""
	state.CurrentToolName = ""
	state.CurrentToolArgs = ""
	state.CurrentToolHadDelta = false
	return []apicompat.AnthropicStreamEvent{{
		Type:  "content_block_stop",
		Index: &idx,
	}}
}

// ccFinishReasonToAnthropicStopReason maps a Chat Completions finish_reason
// (captured during streaming) to an Anthropic stop_reason for message_delta.
func ccFinishReasonToAnthropicStopReason(reason string, hasToolCall bool) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "stop":
		if hasToolCall {
			return "tool_use"
		}
		return "end_turn"
	default:
		if hasToolCall {
			return "tool_use"
		}
		return "end_turn"
	}
}

const minMaxOutputTokens = 128

func parseAnthropicSystemContentParts(raw json.RawMessage) ([]apicompat.ResponsesContentPart, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" || isAnthropicBillingHeaderText(text) {
			return nil, nil
		}
		return []apicompat.ResponsesContentPart{{Type: "input_text", Text: text}}, nil
	}

	var blocks []apicompat.AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	parts := make([]apicompat.ResponsesContentPart, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" && !isAnthropicBillingHeaderText(block.Text) {
			parts = append(parts, apicompat.ResponsesContentPart{Type: "input_text", Text: block.Text})
		}
	}
	return parts, nil
}

func isAnthropicBillingHeaderText(text string) bool {
	return strings.HasPrefix(text, "x-anthropic-billing-header: ")
}

func fromResponsesCallID(id string) string {
	if after, ok := strings.CutPrefix(id, "fc_"); ok &&
		(strings.HasPrefix(after, "toolu_") || strings.HasPrefix(after, "call_")) {
		return after
	}
	return id
}

func anthropicImageToDataURI(source *apicompat.AnthropicImageSource) string {
	if source == nil || source.Data == "" {
		return ""
	}
	mediaType := source.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + source.Data
}

func convertToolResultOutput(block apicompat.AnthropicContentBlock) (string, []apicompat.ResponsesContentPart) {
	if len(block.Content) == 0 {
		return "(empty)", nil
	}
	var text string
	if err := json.Unmarshal(block.Content, &text); err == nil {
		if text == "" {
			text = "(empty)"
		}
		return text, nil
	}

	var inner []apicompat.AnthropicContentBlock
	if err := json.Unmarshal(block.Content, &inner); err != nil {
		return "(empty)", nil
	}
	var textParts []string
	var imageParts []apicompat.ResponsesContentPart
	for _, item := range inner {
		switch item.Type {
		case "text":
			if item.Text != "" {
				textParts = append(textParts, item.Text)
			}
		case "image":
			if uri := anthropicImageToDataURI(item.Source); uri != "" {
				imageParts = append(imageParts, apicompat.ResponsesContentPart{Type: "input_image", ImageURL: uri})
			}
		}
	}
	text = strings.Join(textParts, "\n\n")
	if text == "" {
		text = "(empty)"
	}
	return text, imageParts
}

func extractAnthropicTextFromBlocks(blocks []apicompat.AnthropicContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func mapAnthropicEffortToResponses(effort string) string {
	if effort == "max" {
		return "xhigh"
	}
	return effort
}

func isReasoningModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5")
}

func normalizeToolParameters(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 || string(schema) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(schema, &object); err != nil || string(object["type"]) != `"object"` {
		return schema
	}
	if _, exists := object["properties"]; exists {
		return schema
	}
	object["properties"] = json.RawMessage(`{}`)
	normalized, err := json.Marshal(object)
	if err != nil {
		return schema
	}
	return normalized
}

func sanitizeAnthropicToolUseInput(name, raw string) json.RawMessage {
	if name != "Read" || raw == "" {
		return json.RawMessage(raw)
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return json.RawMessage(raw)
	}
	pages, exists := input["pages"]
	if !exists || string(pages) != `""` {
		return json.RawMessage(raw)
	}
	delete(input, "pages")
	sanitized, err := json.Marshal(input)
	if err != nil {
		return json.RawMessage(raw)
	}
	return sanitized
}

func containsAnthropicToolUseBlock(blocks []apicompat.AnthropicContentBlock) bool {
	for _, block := range blocks {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) forwardAnthropicViaRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	startTime := time.Now()
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := strings.TrimSpace(anthropicReq.Model)
	if originalModel == "" {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in request")
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	clientStream := anthropicReq.Stream

	chatReq, err := AnthropicToChatCompletionsRequest(&anthropicReq)
	if err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert anthropic to chat completions: %w", err)
	}
	billingModel := resolveOpenAIForwardModel(account, anthropicReq.Model, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	chatReq.Model = upstreamModel
	chatReq.Stream = clientStream
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	serviceTier := extractOpenAIServiceTierFromBody(body)
	forwardResult := &OpenAIForwardResult{
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          clientStream,
	}
	ctx = withOpenAIForwardResultBillingState(ctx, c, forwardResult, startTime, openAIResponseImageBillingConfig{})

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}

	apiKey := strings.TrimSpace(account.GetOpenAIApiKey())
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := strings.TrimSpace(account.GetOpenAIBaseURL())
	if account.IsOpencode() {
		baseURL = account.GetOpencodeBaseURL()
	} else if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	upstreamCtx, releaseUpstreamCtx := s.detachOpenAIUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, buildOpenAIChatCompletionsURL(validatedURL), bytes.NewReader(chatBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	if clientStream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			if !openaiChatRawAllowedHeaders[strings.ToLower(key)] {
				continue
			}
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}
	}
	if userAgent := strings.TrimSpace(account.GetOpenAIUserAgent()); userAgent != "" {
		upstreamReq.Header.Set("user-agent", userAgent)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.resolveOpenAIAccountTLSProfile(account))
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, Kind: "request_error", Message: safeErr})
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(resp, c, account, false, writeAnthropicError)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"), Kind: "failover", Message: upstreamMsg})
			s.handleOpenAIAccountUpstreamErrorForModel(ctx, account, originalModel, resp.StatusCode, resp.Header, respBody)
			return nil, newOpenAIUpstreamFailoverError(
				resp.StatusCode,
				resp.Header,
				respBody,
				upstreamMsg,
				shouldRetryOpenAIOnSamePoolAccount(account, resp.StatusCode, upstreamMsg, respBody),
			)
		}
		return s.handleAnthropicErrorResponse(resp, c, account, billingModel)
	}

	if clientStream {
		return s.streamDirectChatCompletionsAsAnthropic(ctx, c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferDirectChatCompletionsAsAnthropic(ctx, c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferDirectChatCompletionsAsAnthropic(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	originalModel, billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	readCtx, cancelRead := s.detachedNonStreamingReadContext(ctx)
	defer cancelRead()
	respBody, err := ReadUpstreamResponseBodyWithContext(readCtx, resp.Body, s.cfg, c, anthropicTooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeAnthropicError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(respBody, strings.TrimSpace(gjson.GetBytes(respBody, "type").String()))
	var chatResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		writeAnthropicError(c, http.StatusBadGateway, "api_error", "Failed to parse upstream response")
		return nil, fmt.Errorf("parse chat completions response: %w", err)
	}
	markObservedUpstreamResponseModelBillingEligible(c)
	usage := OpenAIUsage{}
	if parsed := openAIUsageFromChatCompletionsUsage(string(respBody)); parsed != nil {
		usage = *parsed
	}
	result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:            requestID,
		usage:                &usage,
		responseHeaders:      resp.Header,
		billingUsageComplete: openAIChatCompletionsBillingUsageComplete(respBody),
	})
	result.Model = originalModel
	result.BillingModel = billingModel
	result.UpstreamModel = upstreamModel
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.ReasoningEffort = reasoningEffort
	result.ServiceTier = serviceTier
	result.Stream = false
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.JSON(http.StatusOK, ChatCompletionsResponseToAnthropic(&chatResp, originalModel))
	return result, nil
}

func (s *OpenAIGatewayService) streamDirectChatCompletionsAsAnthropic(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	originalModel, billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	state := NewChatCompletionsToAnthropicStreamState(originalModel)
	var usage OpenAIUsage
	var billingUsageObservation openAIChatCompletionsBillingUsageObservation
	sawDone := false
	var firstTokenMs *int
	clientDisconnected := false
	var cancelDisconnectedDrain context.CancelFunc
	defer func() {
		if cancelDisconnectedDrain != nil {
			cancelDisconnectedDrain()
		}
	}()
	writeHeaders := func() {
		if c.Writer.Written() {
			return
		}
		if s.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		}
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(http.StatusOK)
	}
	emit := func(events []apicompat.AnthropicStreamEvent) {
		if clientDisconnected {
			return
		}
		for _, event := range events {
			payload, err := apicompat.ResponsesAnthropicEventToSSE(event)
			if err != nil {
				continue
			}
			writeHeaders()
			if _, err := fmt.Fprint(c.Writer, payload); err != nil {
				clientDisconnected = true
				cancelDisconnectedDrain = s.startDisconnectedStreamDrainDeadline(ctx, resp.Body, requestID)
				return
			}
		}
		if len(events) > 0 && !clientDisconnected {
			c.Writer.Flush()
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	for scanner.Scan() {
		payload, ok := extractOpenAISSEDataLine(scanner.Text())
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			sawDone = true
			continue
		}
		observer.ObserveOpenAI([]byte(payload), strings.TrimSpace(gjson.Get(payload, "type").String()))
		billingUsageObservation.observePayload([]byte(payload))
		if parsed := extractOpenAIChatStreamUsage(payload); parsed != nil {
			usage = *parsed
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			logger.L().Warn("openai messages chat fallback: failed to parse stream chunk", zap.Error(err), zap.String("request_id", requestID))
			continue
		}
		if firstTokenMs == nil && !isOpenAIChatUsageOnlyStreamChunk(payload) && chatChunkStartsResponsesOutput(&chunk) {
			milliseconds := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &milliseconds
		}
		emit(ChatCompletionsChunkToAnthropicEvents(&chunk, state))
	}
	if err := scanner.Err(); err != nil {
		result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
			requestID:            requestID,
			usage:                &usage,
			firstTokenMs:         firstTokenMs,
			responseHeaders:      resp.Header,
			billingUsageComplete: billingUsageObservation.complete(),
		})
		result.Model = originalModel
		result.BillingModel = billingModel
		result.UpstreamModel = upstreamModel
		result.UpstreamResponseModel = observedUpstreamResponseModel(c)
		result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
		result.ReasoningEffort = reasoningEffort
		result.ServiceTier = serviceTier
		result.Stream = true
		return result, fmt.Errorf("stream usage incomplete: %w", err)
	}
	result := updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:            requestID,
		usage:                &usage,
		firstTokenMs:         firstTokenMs,
		responseHeaders:      resp.Header,
		billingUsageComplete: billingUsageObservation.complete(),
	})
	result.Model = originalModel
	result.BillingModel = billingModel
	result.UpstreamModel = upstreamModel
	result.UpstreamResponseModel = observedUpstreamResponseModel(c)
	result.UpstreamResponseModelConflict = observedUpstreamResponseModelConflict(c)
	result.ReasoningEffort = reasoningEffort
	result.ServiceTier = serviceTier
	result.Stream = true
	if clientDisconnected {
		streamErr := s.clientDisconnectIncompleteUsageError(ctx)
		if streamErr == nil && !billingUsageObservation.complete() {
			streamErr = errors.New("stream usage incomplete after disconnect: missing terminal usage")
		}
		return result, streamErr
	}
	if !sawDone {
		return result, errors.New("upstream chat completions stream ended without [DONE]")
	}
	finalEvents := FinalizeChatCompletionsAnthropicStream(state)
	finalPayloads := make([]string, 0, len(finalEvents))
	for _, event := range finalEvents {
		payload, err := apicompat.ResponsesAnthropicEventToSSE(event)
		if err != nil {
			return result, fmt.Errorf("marshal final Anthropic stream event: %w", err)
		}
		finalPayloads = append(finalPayloads, payload)
	}
	if !clientDisconnected {
		for _, payload := range finalPayloads {
			writeHeaders()
			if _, err := fmt.Fprint(c.Writer, payload); err != nil {
				clientDisconnected = true
				break
			}
		}
		if len(finalPayloads) > 0 && !clientDisconnected {
			c.Writer.Flush()
		}
	}
	if clientDisconnected {
		return result, errors.New("client disconnected while writing final Anthropic stream event")
	}
	markObservedUpstreamResponseModelBillingEligible(c)
	result = updateOpenAIForwardResultBillingState(ctx, openAIForwardResultSnapshot{
		requestID:            requestID,
		usage:                &usage,
		firstTokenMs:         firstTokenMs,
		responseHeaders:      resp.Header,
		billingUsageComplete: billingUsageObservation.complete(),
	})
	result.Model = originalModel
	result.BillingModel = billingModel
	result.UpstreamModel = upstreamModel
	result.ReasoningEffort = reasoningEffort
	result.ServiceTier = serviceTier
	result.Stream = true
	return result, nil
}
