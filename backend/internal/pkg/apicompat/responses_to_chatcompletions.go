package apicompat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResponsesToChatCompletionsRequest preserves request-level controls when a
// Responses client is routed to a Chat Completions-only upstream.
func ResponsesToChatCompletionsRequest(req *ResponsesRequest) (*ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
	}
	messages, err := responsesInputToChatMessages(req.Instructions, req.Input)
	if err != nil {
		return nil, err
	}
	out := &ChatCompletionsRequest{
		Model:               req.Model,
		Messages:            messages,
		MaxCompletionTokens: req.MaxOutputTokens,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		Stream:              req.Stream,
		ParallelToolCalls:   req.ParallelToolCalls,
		ServiceTier:         req.ServiceTier,
		PromptCacheKey:      req.PromptCacheKey,
		PromptCacheOptions:  req.PromptCacheOptions,
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	for _, tool := range req.Tools {
		if !strings.EqualFold(strings.TrimSpace(tool.Type), "x_search") {
			continue
		}
		out.Tools = append(out.Tools, ChatTool{
			Type:                     "x_search",
			AllowedXHandles:          tool.AllowedXHandles,
			ExcludedXHandles:         tool.ExcludedXHandles,
			FromDate:                 tool.FromDate,
			ToDate:                   tool.ToDate,
			EnableImageUnderstanding: tool.EnableImageUnderstanding,
			EnableVideoUnderstanding: tool.EnableVideoUnderstanding,
		})
	}
	if len(out.Tools) > 0 && len(req.ToolChoice) > 0 {
		if choice := responsesXSearchToolChoice(req.ToolChoice); len(choice) > 0 {
			out.ToolChoice = choice
		}
	}
	return out, nil
}

func responsesXSearchToolChoice(raw json.RawMessage) json.RawMessage {
	var choiceString string
	if json.Unmarshal(raw, &choiceString) == nil {
		if strings.EqualFold(strings.TrimSpace(choiceString), "x_search") {
			return append(json.RawMessage(nil), raw...)
		}
		return nil
	}
	var choice struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &choice) != nil || !strings.EqualFold(strings.TrimSpace(choice.Type), "x_search") {
		return nil
	}
	return json.RawMessage(`{"type":"x_search"}`)
}

func responsesInputToChatMessages(instructions string, input json.RawMessage) ([]ChatMessage, error) {
	messages := make([]ChatMessage, 0, 4)
	if strings.TrimSpace(instructions) != "" {
		content, _ := json.Marshal(instructions)
		messages = append(messages, ChatMessage{Role: "system", Content: content})
	}
	input = bytesTrimSpace(input)
	if len(input) == 0 || string(input) == "null" {
		return messages, nil
	}
	var text string
	if err := json.Unmarshal(input, &text); err == nil {
		content, _ := json.Marshal(text)
		return append(messages, ChatMessage{Role: "user", Content: content}), nil
	}
	var items []ResponsesInputItem
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("parse responses input: %w", err)
	}
	for _, item := range items {
		switch item.Type {
		case "function_call":
			toolCall := ChatToolCall{ID: item.CallID, Type: "function", Function: ChatFunctionCall{Name: item.Name, Arguments: item.Arguments}}
			if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" && len(messages[len(messages)-1].ToolCalls) > 0 {
				messages[len(messages)-1].ToolCalls = append(messages[len(messages)-1].ToolCalls, toolCall)
			} else {
				messages = append(messages, ChatMessage{Role: "assistant", ToolCalls: []ChatToolCall{toolCall}})
			}
		case "function_call_output":
			content, _ := json.Marshal(item.Output)
			messages = append(messages, ChatMessage{Role: "tool", ToolCallID: item.CallID, Content: content})
		case "reasoning":
			continue
		default:
			if item.Type != "" && item.Type != "message" {
				continue
			}
			role := item.Role
			if role == "" {
				role = "user"
			}
			messages = append(messages, ChatMessage{Role: role, Content: item.Content})
		}
	}
	return messages, nil
}

func bytesTrimSpace(value []byte) []byte {
	return []byte(strings.TrimSpace(string(value)))
}

func ChatUsageToResponsesUsage(usage *ChatUsage) *ResponsesUsage {
	if usage == nil {
		return nil
	}
	out := &ResponsesUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	if details := usage.PromptTokensDetails; details != nil && (details.CachedTokens > 0 || details.CacheCreationTokens > 0 || details.CacheWriteTokens > 0) {
		out.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens:        details.CachedTokens,
			CacheCreationTokens: details.CacheCreationTokens,
			CacheWriteTokens:    details.CacheWriteTokens,
		}
		if details.CacheWriteTokens > 0 {
			out.CacheCreationInputTokens = details.CacheWriteTokens
		} else {
			out.CacheCreationInputTokens = details.CacheCreationTokens
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Non-streaming: ResponsesResponse → ChatCompletionsResponse
// ---------------------------------------------------------------------------

// ResponsesToChatCompletions converts a Responses API response into a Chat
// Completions response. Text output items are concatenated into
// choices[0].message.content; function_call items become tool_calls.
func ResponsesToChatCompletions(resp *ResponsesResponse, model string) *ChatCompletionsResponse {
	id := resp.ID
	if id == "" {
		id = generateChatCmplID()
	}

	out := &ChatCompletionsResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
	}

	var contentText string
	var reasoningText string
	var toolCalls []ChatToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					contentText += part.Text
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, ChatToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ChatFunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		case "reasoning":
			for _, s := range item.Summary {
				if s.Type == "summary_text" && s.Text != "" {
					reasoningText += s.Text
				}
			}
		case "web_search_call":
			// silently consumed — results already incorporated into text output
		}
	}

	msg := ChatMessage{Role: "assistant"}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	if contentText != "" {
		raw, _ := json.Marshal(contentText)
		msg.Content = raw
	}
	if reasoningText != "" {
		msg.ReasoningContent = reasoningText
	}

	finishReason := responsesStatusToChatFinishReason(resp.Status, resp.IncompleteDetails, toolCalls)

	out.Choices = []ChatChoice{{
		Index:        0,
		Message:      msg,
		FinishReason: finishReason,
	}}

	if resp.Usage != nil {
		usage := &ChatUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
		if resp.Usage.InputTokensDetails != nil && (resp.Usage.InputTokensDetails.CachedTokens > 0 ||
			resp.Usage.InputTokensDetails.CacheCreationTokens > 0 || resp.Usage.InputTokensDetails.CacheWriteTokens > 0) {
			usage.PromptTokensDetails = &ChatTokenDetails{
				CachedTokens:        resp.Usage.InputTokensDetails.CachedTokens,
				CacheCreationTokens: resp.Usage.InputTokensDetails.CacheCreationTokens,
				CacheWriteTokens:    resp.Usage.InputTokensDetails.CacheWriteTokens,
			}
		}
		if resp.Usage.CacheCreationInputTokens > 0 {
			if usage.PromptTokensDetails == nil {
				usage.PromptTokensDetails = &ChatTokenDetails{}
			}
			if usage.PromptTokensDetails.CacheWriteTokens == 0 && usage.PromptTokensDetails.CacheCreationTokens == 0 {
				usage.PromptTokensDetails.CacheCreationTokens = resp.Usage.CacheCreationInputTokens
			}
		}
		out.Usage = usage
	}

	return out
}

func responsesStatusToChatFinishReason(status string, details *ResponsesIncompleteDetails, toolCalls []ChatToolCall) string {
	switch status {
	case "incomplete":
		if details != nil {
			switch details.Reason {
			case "max_output_tokens":
				return "length"
			case "content_filter":
				return "content_filter"
			}
		}
		return "stop"
	case "completed":
		if len(toolCalls) > 0 {
			return "tool_calls"
		}
		return "stop"
	default:
		return "stop"
	}
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesStreamEvent → []ChatCompletionsChunk (stateful converter)
// ---------------------------------------------------------------------------

// ResponsesEventToChatState tracks state for converting a sequence of Responses
// SSE events into Chat Completions SSE chunks.
type ResponsesEventToChatState struct {
	ID                     string
	Model                  string
	Created                int64
	SentRole               bool
	SawToolCall            bool
	SawText                bool
	Finalized              bool        // true after finish chunk has been emitted
	NextToolCallIndex      int         // next sequential tool_call index to assign
	OutputIndexToToolIndex map[int]int // Responses output_index → Chat tool_calls index
	IncludeUsage           bool
	Usage                  *ChatUsage
}

// NewResponsesEventToChatState returns an initialised stream state.
func NewResponsesEventToChatState() *ResponsesEventToChatState {
	return &ResponsesEventToChatState{
		ID:                     generateChatCmplID(),
		Created:                time.Now().Unix(),
		OutputIndexToToolIndex: make(map[int]int),
	}
}

// ResponsesEventToChatChunks converts a single Responses SSE event into zero
// or more Chat Completions chunks, updating state as it goes.
func ResponsesEventToChatChunks(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	switch evt.Type {
	case "response.created":
		return resToChatHandleCreated(evt, state)
	case "response.output_text.delta":
		return resToChatHandleTextDelta(evt, state)
	case "response.output_item.added":
		return resToChatHandleOutputItemAdded(evt, state)
	case "response.function_call_arguments.delta":
		return resToChatHandleFuncArgsDelta(evt, state)
	case "response.reasoning_summary_text.delta":
		return resToChatHandleReasoningDelta(evt, state)
	case "response.reasoning_summary_text.done":
		return nil
	case "response.completed", "response.incomplete", "response.failed":
		return resToChatHandleCompleted(evt, state)
	default:
		return nil
	}
}

// FinalizeResponsesChatStream emits a final chunk with finish_reason if the
// stream ended without a proper completion event (e.g. upstream disconnect).
// It is idempotent: if a completion event already emitted the finish chunk,
// this returns nil.
func FinalizeResponsesChatStream(state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if state.Finalized {
		return nil
	}
	state.Finalized = true

	finishReason := "stop"
	if state.SawToolCall {
		finishReason = "tool_calls"
	}

	chunks := []ChatCompletionsChunk{makeChatFinishChunk(state, finishReason)}

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:      state.ID,
			Object:  "chat.completion.chunk",
			Created: state.Created,
			Model:   state.Model,
			Choices: []ChatChunkChoice{},
			Usage:   state.Usage,
		})
	}

	return chunks
}

// ChatChunkToSSE formats a ChatCompletionsChunk as an SSE data line.
func ChatChunkToSSE(chunk ChatCompletionsChunk) (string, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", data), nil
}

// --- internal handlers ---

func resToChatHandleCreated(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Response != nil {
		if evt.Response.ID != "" {
			state.ID = evt.Response.ID
		}
		if state.Model == "" && evt.Response.Model != "" {
			state.Model = evt.Response.Model
		}
	}
	// Emit the role chunk.
	if state.SentRole {
		return nil
	}
	state.SentRole = true

	role := "assistant"
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Role: role})}
}

func resToChatHandleTextDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	state.SawText = true
	content := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{Content: &content})}
}

func resToChatHandleOutputItemAdded(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Item == nil || evt.Item.Type != "function_call" {
		return nil
	}

	state.SawToolCall = true
	idx := state.NextToolCallIndex
	state.OutputIndexToToolIndex[evt.OutputIndex] = idx
	state.NextToolCallIndex++

	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			ID:    evt.Item.CallID,
			Type:  "function",
			Function: ChatFunctionCall{
				Name: evt.Item.Name,
			},
		}},
	})}
}

func resToChatHandleFuncArgsDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}

	idx, ok := state.OutputIndexToToolIndex[evt.OutputIndex]
	if !ok {
		return nil
	}

	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{
		ToolCalls: []ChatToolCall{{
			Index: &idx,
			Function: ChatFunctionCall{
				Arguments: evt.Delta,
			},
		}},
	})}
}

func resToChatHandleReasoningDelta(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	if evt.Delta == "" {
		return nil
	}
	reasoning := evt.Delta
	return []ChatCompletionsChunk{makeChatDeltaChunk(state, ChatDelta{ReasoningContent: &reasoning})}
}

func resToChatHandleCompleted(evt *ResponsesStreamEvent, state *ResponsesEventToChatState) []ChatCompletionsChunk {
	state.Finalized = true
	finishReason := "stop"

	if evt.Response != nil {
		if evt.Response.Usage != nil {
			u := evt.Response.Usage
			usage := &ChatUsage{
				PromptTokens:     u.InputTokens,
				CompletionTokens: u.OutputTokens,
				TotalTokens:      u.InputTokens + u.OutputTokens,
			}
			if u.InputTokensDetails != nil && (u.InputTokensDetails.CachedTokens > 0 ||
				u.InputTokensDetails.CacheCreationTokens > 0 || u.InputTokensDetails.CacheWriteTokens > 0) {
				usage.PromptTokensDetails = &ChatTokenDetails{
					CachedTokens:        u.InputTokensDetails.CachedTokens,
					CacheCreationTokens: u.InputTokensDetails.CacheCreationTokens,
					CacheWriteTokens:    u.InputTokensDetails.CacheWriteTokens,
				}
			}
			if u.CacheCreationInputTokens > 0 {
				if usage.PromptTokensDetails == nil {
					usage.PromptTokensDetails = &ChatTokenDetails{}
				}
				if usage.PromptTokensDetails.CacheWriteTokens == 0 && usage.PromptTokensDetails.CacheCreationTokens == 0 {
					usage.PromptTokensDetails.CacheCreationTokens = u.CacheCreationInputTokens
				}
			}
			state.Usage = usage
		}

		switch evt.Response.Status {
		case "incomplete":
			if evt.Response.IncompleteDetails != nil {
				switch evt.Response.IncompleteDetails.Reason {
				case "max_output_tokens":
					finishReason = "length"
				case "content_filter":
					finishReason = "content_filter"
				}
			}
		case "completed":
			if state.SawToolCall {
				finishReason = "tool_calls"
			}
		}
	} else if state.SawToolCall {
		finishReason = "tool_calls"
	}

	var chunks []ChatCompletionsChunk
	chunks = append(chunks, makeChatFinishChunk(state, finishReason))

	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:      state.ID,
			Object:  "chat.completion.chunk",
			Created: state.Created,
			Model:   state.Model,
			Choices: []ChatChunkChoice{},
			Usage:   state.Usage,
		})
	}

	return chunks
}

func makeChatDeltaChunk(state *ResponsesEventToChatState, delta ChatDelta) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: nil,
		}},
	}
}

func makeChatFinishChunk(state *ResponsesEventToChatState, finishReason string) ChatCompletionsChunk {
	empty := ""
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{Content: &empty},
			FinishReason: &finishReason,
		}},
	}
}

// generateChatCmplID returns a "chatcmpl-" prefixed random hex ID.
func generateChatCmplID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "chatcmpl-" + hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// BufferedResponseAccumulator: accumulates SSE delta events for non-streaming
// paths where the terminal event may have empty output.
// ---------------------------------------------------------------------------

type bufferedFuncCall struct {
	CallID string
	Name   string
	Args   strings.Builder
}

// BufferedResponseAccumulator collects content from Responses SSE delta events
// so that non-streaming handlers can reconstruct output when the terminal event
// (response.completed / response.done) carries an empty output array.
type BufferedResponseAccumulator struct {
	text                 strings.Builder
	reasoning            strings.Builder
	funcCalls            []bufferedFuncCall
	outputIndexToFuncIdx map[int]int
}

// NewBufferedResponseAccumulator returns an initialised accumulator.
func NewBufferedResponseAccumulator() *BufferedResponseAccumulator {
	return &BufferedResponseAccumulator{
		outputIndexToFuncIdx: make(map[int]int),
	}
}

// ProcessEvent inspects a single Responses SSE event and accumulates any
// content it carries. Only delta events that contribute to the final output
// are handled; all other event types are silently ignored.
func (a *BufferedResponseAccumulator) ProcessEvent(event *ResponsesStreamEvent) {
	switch event.Type {
	case "response.output_text.delta":
		if event.Delta != "" {
			_, _ = a.text.WriteString(event.Delta)
		}
	case "response.output_item.added":
		if event.Item != nil && event.Item.Type == "function_call" {
			idx := len(a.funcCalls)
			a.outputIndexToFuncIdx[event.OutputIndex] = idx
			a.funcCalls = append(a.funcCalls, bufferedFuncCall{
				CallID: event.Item.CallID,
				Name:   event.Item.Name,
			})
		}
	case "response.function_call_arguments.delta":
		if event.Delta != "" {
			if idx, ok := a.outputIndexToFuncIdx[event.OutputIndex]; ok {
				_, _ = a.funcCalls[idx].Args.WriteString(event.Delta)
			}
		}
	case "response.reasoning_summary_text.delta":
		if event.Delta != "" {
			_, _ = a.reasoning.WriteString(event.Delta)
		}
	}
}

// HasContent reports whether any content has been accumulated.
func (a *BufferedResponseAccumulator) HasContent() bool {
	return a.text.Len() > 0 || len(a.funcCalls) > 0 || a.reasoning.Len() > 0
}

// BuildOutput constructs a []ResponsesOutput from the accumulated delta
// content. The order matches what ResponsesToChatCompletions expects:
// reasoning → message → function_calls.
func (a *BufferedResponseAccumulator) BuildOutput() []ResponsesOutput {
	var out []ResponsesOutput

	if a.reasoning.Len() > 0 {
		out = append(out, ResponsesOutput{
			Type: "reasoning",
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: a.reasoning.String(),
			}},
		})
	}

	if a.text.Len() > 0 {
		out = append(out, ResponsesOutput{
			Type: "message",
			Role: "assistant",
			Content: []ResponsesContentPart{{
				Type: "output_text",
				Text: a.text.String(),
			}},
		})
	}

	for i := range a.funcCalls {
		out = append(out, ResponsesOutput{
			Type:      "function_call",
			CallID:    a.funcCalls[i].CallID,
			Name:      a.funcCalls[i].Name,
			Arguments: a.funcCalls[i].Args.String(),
		})
	}

	return out
}

// SupplementResponseOutput fills resp.Output from accumulated delta content
// when the terminal event delivered an empty output array. If resp.Output is
// already populated, this is a no-op (preserves backward compatibility).
func (a *BufferedResponseAccumulator) SupplementResponseOutput(resp *ResponsesResponse) {
	if resp == nil || len(resp.Output) > 0 {
		return
	}
	if !a.HasContent() {
		return
	}
	resp.Output = a.BuildOutput()
}
