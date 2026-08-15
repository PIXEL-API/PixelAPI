package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// ResponsesToChatCompletionsRequest converts a Responses API request into a
// Chat Completions request for upstreams that only implement
// /v1/chat/completions.
func ResponsesToChatCompletionsRequest(req *apicompat.ResponsesRequest) (*apicompat.ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
	}

	messages, err := responsesInputToChatMessages(req.Instructions, req.Input)
	if err != nil {
		return nil, err
	}

	out := &apicompat.ChatCompletionsRequest{
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
	effectiveTools, err := apicompat.EffectiveResponsesTools(req)
	if err != nil {
		return nil, err
	}
	if len(effectiveTools) > 0 {
		out.Tools, err = responsesToolsToChatTools(effectiveTools)
		if err != nil {
			return nil, err
		}
	}
	if len(out.Tools) > 0 && len(req.ToolChoice) > 0 {
		declared := make(map[string]bool, len(out.Tools))
		for _, tool := range out.Tools {
			if tool.Function != nil {
				declared[tool.Function.Name] = true
			}
			if strings.EqualFold(strings.TrimSpace(tool.Type), "x_search") {
				declared["x_search"] = true
			}
		}
		if choice := responsesToolChoiceToChatToolChoice(req.ToolChoice, declared); len(choice) > 0 {
			out.ToolChoice = choice
		}
	}

	return out, nil
}

// responsesInputToChatMessages converts a Responses request's instructions +
// input[] into Chat Completions messages. It is a three-stage pipeline:
//
//	parse   — instructions become a system message; input[] is split into items
//	build   — buildChatMessagesFromItems walks items, attaching reasoning to the
//	          assistant message that produced a tool call, merging parallel tool
//	          calls into one assistant message, and skipping item types that have
//	          no Chat equivalent
//	normalize — normalizeChatMessages enforces the invariants DeepSeek requires
//
// The build + normalize split keeps every protocol rule in one place rather than
// scattered across per-item cases, and makes unknown future codex item types
// fail safe instead of leaking into the upstream request.
func responsesInputToChatMessages(instructions string, inputRaw json.RawMessage) ([]apicompat.ChatMessage, error) {
	var messages []apicompat.ChatMessage
	if strings.TrimSpace(instructions) != "" {
		content, _ := json.Marshal(instructions)
		messages = append(messages, apicompat.ChatMessage{Role: "system", Content: content})
	}

	inputRaw = bytesTrimSpace(inputRaw)
	if len(inputRaw) == 0 || string(inputRaw) == "null" {
		return messages, nil
	}

	// Bare string input is a single user turn.
	var inputText string
	if err := json.Unmarshal(inputRaw, &inputText); err == nil {
		content, _ := json.Marshal(inputText)
		messages = append(messages, apicompat.ChatMessage{Role: "user", Content: content})
		return messages, nil
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(inputRaw, &rawItems); err != nil {
		return nil, fmt.Errorf("parse responses input: %w", err)
	}

	built, err := buildChatMessagesFromItems(messages, rawItems)
	if err != nil {
		return nil, err
	}
	return normalizeChatMessages(built), nil
}

// buildChatMessagesFromItems walks the Responses input items and appends the
// corresponding Chat messages.
func buildChatMessagesFromItems(messages []apicompat.ChatMessage, rawItems []json.RawMessage) ([]apicompat.ChatMessage, error) {
	// pendingReasoning holds the reasoning text from a reasoning item until the
	// assistant message it belongs to is emitted. DeepSeek's thinking mode
	// requires the reasoning_content that produced a tool call to be passed back
	// on that assistant message; dropping it yields a 400. It only survives
	// across an assistant message (so a following tool call in the same turn
	// still receives it); any other role ends the thinking span.
	var pendingReasoning string

	for _, raw := range rawItems {
		raw = bytesTrimSpace(raw)
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}

		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			var text string
			if textErr := json.Unmarshal(raw, &text); textErr == nil {
				content, _ := json.Marshal(text)
				messages = append(messages, apicompat.ChatMessage{Role: "user", Content: content})
				pendingReasoning = ""
				continue
			}
			return nil, fmt.Errorf("parse responses input item: %w", err)
		}

		role := chatCompletionsBridgeRole(rawString(item["role"]))
		itemType := rawString(item["type"])
		switch itemType {
		case "reasoning":
			if txt := extractResponsesReasoningText(item); txt != "" {
				pendingReasoning = txt
			}
			continue
		case "function_call":
			arguments := rawString(item["arguments"])
			if strings.TrimSpace(arguments) == "" {
				arguments = "{}"
			}
			name := rawString(item["name"])
			if namespace := rawString(item["namespace"]); namespace != "" {
				name = apicompat.FlattenResponsesNamespaceToolName(namespace, name)
			}
			toolCall := apicompat.ChatToolCall{
				ID:   rawString(item["call_id"]),
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      name,
					Arguments: arguments,
				},
			}
			messages = appendAssistantToolCall(messages, toolCall, pendingReasoning)
			pendingReasoning = ""
			continue
		case "tool_search_call":
			arguments := strings.TrimSpace(string(bytesTrimSpace(item["arguments"])))
			if value := rawString(item["arguments"]); value != "" {
				arguments = value
			}
			if arguments == "" || arguments == "null" {
				arguments = "{}"
			}
			messages = appendAssistantToolCall(messages, apicompat.ChatToolCall{
				ID:   rawString(item["call_id"]),
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      toolSearchProxyName,
					Arguments: arguments,
				},
			}, pendingReasoning)
			pendingReasoning = ""
			continue
		case "custom_tool_call":
			arguments, _ := json.Marshal(map[string]string{"input": rawString(item["input"])})
			messages = appendAssistantToolCall(messages, apicompat.ChatToolCall{
				ID:   rawString(item["call_id"]),
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      rawString(item["name"]),
					Arguments: string(arguments),
				},
			}, pendingReasoning)
			pendingReasoning = ""
			continue
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			outputRaw := bytesTrimSpace(item["output"])
			outputText := rawString(outputRaw)
			if outputText == "" && len(outputRaw) > 0 && string(outputRaw) != "null" && string(outputRaw) != `""` {
				outputText = string(outputRaw)
			}
			content, _ := json.Marshal(outputText)
			messages = append(messages, apicompat.ChatMessage{
				Role:       "tool",
				ToolCallID: rawString(item["call_id"]),
				Content:    content,
			})
			pendingReasoning = ""
			continue
		case "input_text", "text":
			content, _ := json.Marshal(rawString(item["text"]))
			messages = append(messages, apicompat.ChatMessage{Role: "user", Content: content})
			pendingReasoning = ""
			continue
		case "input_image":
			content, err := chatContentFromSingleResponsesPart(itemType, item)
			if err != nil {
				return nil, err
			}
			messages = append(messages, apicompat.ChatMessage{Role: "user", Content: content})
			pendingReasoning = ""
			continue
		}

		// Only genuine message items become chat messages. Codex emits other
		// Responses item types with no Chat equivalent (web_search_call,
		// local_shell_call, custom tool calls, file_search_call, ...). Converting
		// them via the generic path would insert a spurious message between an
		// assistant tool_calls message and its tool reply, which DeepSeek rejects
		// ("insufficient tool messages following tool_calls message"). Skip them.
		if itemType != "" && itemType != "message" {
			pendingReasoning = ""
			continue
		}

		content := item["content"]
		if len(bytesTrimSpace(content)) == 0 {
			if text := rawString(item["text"]); text != "" {
				content, _ = json.Marshal(text)
			}
		}
		chatContent, err := responsesContentToChatContent(content, role)
		if err != nil {
			return nil, err
		}
		messages = append(messages, apicompat.ChatMessage{Role: role, Content: chatContent})
		// Reasoning only survives across an assistant text message.
		if role != "assistant" {
			pendingReasoning = ""
		}
	}

	return messages, nil
}

func appendAssistantToolCall(messages []apicompat.ChatMessage, toolCall apicompat.ChatToolCall, reasoning string) []apicompat.ChatMessage {
	if n := len(messages); n > 0 && messages[n-1].Role == "assistant" {
		messages[n-1].ToolCalls = append(messages[n-1].ToolCalls, toolCall)
		if messages[n-1].ReasoningContent == "" {
			messages[n-1].ReasoningContent = reasoning
		}
		return messages
	}
	return append(messages, apicompat.ChatMessage{
		Role:             "assistant",
		ToolCalls:        []apicompat.ChatToolCall{toolCall},
		ReasoningContent: reasoning,
	})
}

// normalizeChatMessages is the single place that enforces the tool-call
// invariant the DeepSeek / OpenAI Chat Completions schema requires: an assistant
// message with tool_calls must be immediately followed by one tool message per
// tool_call_id, in order, with nothing in between.
//
// Codex histories violate this in several ways that the builder alone can't fix:
//   - a non-tool message lands between an assistant tool_calls message and its
//     tool replies (e.g. an "Approved command prefix saved" system notice codex
//     injects mid tool-execution);
//   - a parallel tool_call's sibling output never arrives, or a call is left
//     dangling by a mid-execution reconnect (unanswered tool_call);
//   - a tool reply has no announcing assistant tool_call (orphan).
//
// It rebuilds the sequence so each assistant's answered tool_calls are followed
// directly by their replies (in call order); unanswered tool_calls are dropped
// (and an assistant left with neither tool_calls nor content is dropped); orphan
// tool replies and intervening messages are emitted in their natural position
// but never between an assistant tool_calls message and its replies.
func normalizeChatMessages(messages []apicompat.ChatMessage) []apicompat.ChatMessage {
	// Index every tool reply by its tool_call_id (last wins on duplicates).
	replies := make(map[string]apicompat.ChatMessage)
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID != "" {
			replies[m.ToolCallID] = m
		}
	}

	out := make([]apicompat.ChatMessage, 0, len(messages))
	for _, m := range messages {
		switch {
		case m.Role == "tool":
			// A bare tool message with no tool_call_id is a direct Chat
			// Completions passthrough; keep it in place. A tool reply whose id is
			// announced by an assistant is emitted right after that assistant
			// (skip the standalone occurrence). Any other tool reply is an orphan
			// and is dropped.
			if m.ToolCallID == "" {
				out = append(out, m)
			}
			continue
		case len(m.ToolCalls) > 0:
			kept := make([]apicompat.ChatToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					continue
				}
				if _, ok := replies[tc.ID]; ok {
					kept = append(kept, tc)
				}
			}
			if len(kept) == 0 {
				// No answered tool_calls left: keep as a plain message if it has
				// content, otherwise drop it entirely.
				if isBlankChatContent(m.Content) {
					continue
				}
				m.ToolCalls = nil
				out = append(out, m)
				continue
			}
			m.ToolCalls = kept
			out = append(out, m)
			for _, tc := range kept {
				out = append(out, replies[tc.ID])
			}
		default:
			out = append(out, m)
		}
	}
	return out
}

// isBlankChatContent reports whether a chat message content holds no usable text.
func isBlankChatContent(raw json.RawMessage) bool {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return true
	}
	return chatMessageContentText(raw) == ""
}

// extractResponsesReasoningText pulls the reasoning text out of a Responses
// reasoning item. The Chat→Responses bridge writes the upstream reasoning_content
// verbatim into the summary_text parts (see closeChatReasoningItem), so codex
// round-trips it there; prefer summary[].text and fall back to content.
func extractResponsesReasoningText(item map[string]json.RawMessage) string {
	var parts []string
	collect := func(raw json.RawMessage) {
		raw = bytesTrimSpace(raw)
		if len(raw) == 0 || string(raw) == "null" {
			return
		}
		var arr []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil {
			for _, p := range arr {
				if t := rawString(p["text"]); t != "" {
					parts = append(parts, t)
				}
			}
			return
		}
		if t := rawString(raw); t != "" {
			parts = append(parts, t)
		}
	}
	collect(item["summary"])
	if len(parts) == 0 {
		collect(item["content"])
	}
	return strings.Join(parts, "\n")
}

func chatCompletionsBridgeRole(role string) string {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return "user"
	}
	if strings.EqualFold(trimmed, "developer") {
		return "system"
	}
	return role
}

func responsesContentToChatContent(raw json.RawMessage, role string) (json.RawMessage, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		empty, _ := json.Marshal("")
		return empty, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return raw, nil
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal(raw, &rawParts); err == nil {
		return responsesContentPartsToChatContent(rawParts, role)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		return chatContentFromSingleResponsesPart(rawString(obj["type"]), obj)
	}

	return raw, nil
}

func responsesContentPartsToChatContent(rawParts []json.RawMessage, role string) (json.RawMessage, error) {
	var textParts []string
	var chatParts []apicompat.ChatContentPart
	hasNonText := false

	for _, rawPart := range rawParts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(rawPart, &part); err != nil {
			continue
		}
		partType := rawString(part["type"])
		switch partType {
		case "input_text", "output_text", "text", "":
			text := rawString(part["text"])
			if text == "" {
				continue
			}
			textParts = append(textParts, text)
			chatParts = append(chatParts, apicompat.ChatContentPart{Type: "text", Text: text})
		case "input_image", "image_url":
			imageURL := rawString(part["image_url"])
			if imageURL == "" {
				imageURL = rawNestedString(part["image_url"], "url")
			}
			if imageURL == "" {
				continue
			}
			hasNonText = true
			chatParts = append(chatParts, apicompat.ChatContentPart{
				Type:     "image_url",
				ImageURL: &apicompat.ChatImageURL{URL: imageURL},
			})
		}
	}

	if !hasNonText {
		joined, _ := json.Marshal(strings.Join(textParts, "\n\n"))
		return joined, nil
	}
	if role != "user" {
		joined, _ := json.Marshal(strings.Join(textParts, "\n\n"))
		return joined, nil
	}
	if len(chatParts) == 0 {
		empty, _ := json.Marshal("")
		return empty, nil
	}
	return json.Marshal(chatParts)
}

func chatContentFromSingleResponsesPart(partType string, part map[string]json.RawMessage) (json.RawMessage, error) {
	switch partType {
	case "input_image", "image_url":
		imageURL := rawString(part["image_url"])
		if imageURL == "" {
			imageURL = rawNestedString(part["image_url"], "url")
		}
		return json.Marshal([]apicompat.ChatContentPart{{
			Type:     "image_url",
			ImageURL: &apicompat.ChatImageURL{URL: imageURL},
		}})
	default:
		return json.Marshal(rawString(part["text"]))
	}
}

const customToolInputSchema = `{"type":"object","properties":{"input":{"type":"string","description":"The raw input for this tool, passed through verbatim."}},"required":["input"]}`

const toolSearchProxyName = "tool_search"

const toolSearchProxySchema = `{"type":"object","properties":{"query":{"type":"string","description":"Search query for tools or connectors to load."},"limit":{"type":"integer","description":"Maximum number of tool groups to return."}},"required":["query"]}`

func responsesToolsToChatTools(tools []apicompat.ResponsesTool) ([]apicompat.ChatTool, error) {
	topLevel := make(map[string]bool)
	for _, tool := range tools {
		if (tool.Type == "function" || tool.Type == "custom") && tool.Name != "" {
			topLevel[tool.Name] = true
		}
	}

	flatOwner := make(map[string]apicompat.NamespacedToolName)
	toolSearchDeclared := false
	out := make([]apicompat.ChatTool, 0, len(tools))
	for _, tool := range tools {
		switch tool.Type {
		case "function":
			out = append(out, apicompat.ChatTool{
				Type: "function",
				Function: &apicompat.ChatFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
					Strict:      tool.Strict,
				},
			})
		case "custom":
			out = append(out, apicompat.ChatTool{
				Type: "function",
				Function: &apicompat.ChatFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  json.RawMessage(customToolInputSchema),
				},
			})
		case "tool_search":
			if topLevel[toolSearchProxyName] {
				return nil, fmt.Errorf("built-in tool_search conflicts with a declared tool named %q; this upstream cannot disambiguate them, rename the tool", toolSearchProxyName)
			}
			if toolSearchDeclared {
				continue
			}
			toolSearchDeclared = true
			out = append(out, toolSearchProxyChatTool())
		case "namespace":
			flattened, err := namespaceChildrenToChatTools(tool, topLevel, flatOwner)
			if err != nil {
				return nil, err
			}
			out = append(out, flattened...)
		case "x_search":
			out = append(out, apicompat.ChatTool{
				Type:                     "x_search",
				AllowedXHandles:          tool.AllowedXHandles,
				ExcludedXHandles:         tool.ExcludedXHandles,
				FromDate:                 tool.FromDate,
				ToDate:                   tool.ToDate,
				EnableImageUnderstanding: tool.EnableImageUnderstanding,
				EnableVideoUnderstanding: tool.EnableVideoUnderstanding,
			})
		}
	}
	return out, nil
}

func toolSearchProxyChatTool() apicompat.ChatTool {
	return apicompat.ChatTool{
		Type: "function",
		Function: &apicompat.ChatFunction{
			Name:        toolSearchProxyName,
			Description: "Search and load Codex tools, plugins, connectors, and MCP namespaces for the current task.",
			Parameters:  json.RawMessage(toolSearchProxySchema),
		},
	}
}

func namespaceChildrenToChatTools(
	tool apicompat.ResponsesTool,
	topLevel map[string]bool,
	flatOwner map[string]apicompat.NamespacedToolName,
) ([]apicompat.ChatTool, error) {
	if tool.Name == "" {
		return nil, nil
	}
	children := tool.Tools
	if len(children) == 0 {
		children = tool.Children
	}
	var out []apicompat.ChatTool
	for _, child := range children {
		if child.Type != "function" || child.Name == "" {
			continue
		}
		flat := apicompat.FlattenResponsesNamespaceToolName(tool.Name, child.Name)
		identity := apicompat.NamespacedToolName{Namespace: tool.Name, Name: child.Name}
		if topLevel[flat] {
			return nil, fmt.Errorf("namespace tool %q/%q flattens to %q which conflicts with a top-level tool of the same name; this upstream cannot disambiguate them, rename one of the tools", tool.Name, child.Name, flat)
		}
		if previous, ok := flatOwner[flat]; ok {
			if previous == identity {
				continue
			}
			return nil, fmt.Errorf("namespace tools %q/%q and %q/%q both flatten to %q; this upstream cannot disambiguate them, rename one of the tools", previous.Namespace, previous.Name, tool.Name, child.Name, flat)
		}
		flatOwner[flat] = identity
		out = append(out, apicompat.ChatTool{
			Type: "function",
			Function: &apicompat.ChatFunction{
				Name:        flat,
				Description: child.Description,
				Parameters:  child.Parameters,
				Strict:      child.Strict,
			},
		})
	}
	return out, nil
}

func responsesToolChoiceToChatToolChoice(raw json.RawMessage, declared map[string]bool) json.RawMessage {
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(raw, &choice); err != nil {
		return raw
	}
	var name string
	switch rawString(choice["type"]) {
	case "x_search":
		if !declared["x_search"] {
			return nil
		}
		out, err := json.Marshal(map[string]any{"type": "x_search"})
		if err != nil {
			return raw
		}
		return out
	case "tool_search":
		name = toolSearchProxyName
	case "function", "custom":
		name = rawString(choice["name"])
		if name == "" {
			name = rawNestedString(choice["function"], "name")
		}
		if name == "" {
			return raw
		}
		if namespace := rawString(choice["namespace"]); namespace != "" {
			name = apicompat.FlattenResponsesNamespaceToolName(namespace, name)
		}
	default:
		return nil
	}
	if !declared[name] {
		return nil
	}
	out, err := json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]string{
			"name": name,
		},
	})
	if err != nil {
		return raw
	}
	return out
}

func extractCustomToolCallInput(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return ""
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &object); err != nil {
		return trimmed
	}
	if raw, ok := object["input"]; ok {
		var input string
		if err := json.Unmarshal(raw, &input); err == nil {
			return input
		}
		return trimmed
	}
	if len(object) == 0 {
		return ""
	}
	return trimmed
}

// ChatCompletionsResponseToResponses converts a non-streaming Chat Completions
// response into a Responses API response.
func ChatCompletionsResponseToResponses(
	resp *apicompat.ChatCompletionsResponse,
	model string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
) *apicompat.ResponsesResponse {
	id := ""
	if resp != nil {
		id = resp.ID
	}
	if id == "" {
		id = generateResponsesID()
	}

	out := &apicompat.ResponsesResponse{
		ID:     id,
		Object: "response",
		Model:  model,
		Status: "completed",
	}
	if resp == nil {
		out.Output = []apicompat.ResponsesOutput{emptyResponsesMessageOutput()}
		return out
	}
	if out.Model == "" {
		out.Model = resp.Model
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		out.Output = chatMessageToResponsesOutput(choice.Message, customTools, toolSearch, namespaceTools)
		if choice.FinishReason == "length" {
			out.Status = "incomplete"
			out.IncompleteDetails = &apicompat.ResponsesIncompleteDetails{Reason: "max_output_tokens"}
		}
	}
	if len(out.Output) == 0 {
		out.Output = []apicompat.ResponsesOutput{emptyResponsesMessageOutput()}
	}
	if resp.Usage != nil {
		out.Usage = ChatUsageToResponsesUsage(resp.Usage)
	}
	return out
}

func chatMessageToResponsesOutput(
	message apicompat.ChatMessage,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
) []apicompat.ResponsesOutput {
	var outputs []apicompat.ResponsesOutput
	if message.ReasoningContent != "" {
		outputs = append(outputs, apicompat.ResponsesOutput{
			Type: "reasoning",
			ID:   generateItemID(),
			Summary: []apicompat.ResponsesSummary{{
				Type: "summary_text",
				Text: message.ReasoningContent,
			}},
		})
	}

	text := chatMessageContentText(message.Content)
	if text == "" && strings.TrimSpace(message.ReasoningContent) != "" && len(message.ToolCalls) == 0 {
		text = message.ReasoningContent
	}
	if text != "" || len(message.ToolCalls) == 0 {
		outputs = append(outputs, apicompat.ResponsesOutput{
			Type: "message",
			ID:   generateItemID(),
			Role: "assistant",
			Content: []apicompat.ResponsesContentPart{{
				Type: "output_text",
				Text: text,
			}},
			Status: "completed",
		})
	}

	for _, toolCall := range message.ToolCalls {
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		if customTools[toolCall.Function.Name] {
			outputs = append(outputs, apicompat.ResponsesOutput{
				Type:   "custom_tool_call",
				ID:     generateItemID(),
				CallID: toolCall.ID,
				Name:   toolCall.Function.Name,
				Input:  extractCustomToolCallInput(arguments),
				Status: "completed",
			})
			continue
		}
		if toolSearch && toolCall.Function.Name == toolSearchProxyName {
			outputs = append(outputs, apicompat.ResponsesOutput{
				Type:      "tool_search_call",
				ID:        generateItemID(),
				CallID:    toolCall.ID,
				Arguments: arguments,
				Status:    "completed",
			})
			continue
		}
		if namespace, ok := namespaceTools[toolCall.Function.Name]; ok {
			outputs = append(outputs, apicompat.ResponsesOutput{
				Type:      "function_call",
				ID:        generateItemID(),
				CallID:    toolCall.ID,
				Name:      namespace.Name,
				Namespace: namespace.Namespace,
				Arguments: arguments,
				Status:    "completed",
			})
			continue
		}
		outputs = append(outputs, apicompat.ResponsesOutput{
			Type:      "function_call",
			ID:        generateItemID(),
			CallID:    toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: arguments,
			Status:    "completed",
		})
	}

	return outputs
}

func emptyResponsesMessageOutput() apicompat.ResponsesOutput {
	return apicompat.ResponsesOutput{
		Type:    "message",
		ID:      generateItemID(),
		Role:    "assistant",
		Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: ""}},
		Status:  "completed",
	}
}

func chatMessageContentText(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []apicompat.ChatContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		return strings.Join(texts, "\n\n")
	}
	return ""
}

// ChatUsageToResponsesUsage converts Chat Completions token usage to Responses
// usage shape.
func ChatUsageToResponsesUsage(usage *apicompat.ChatUsage) *apicompat.ResponsesUsage {
	if usage == nil {
		return nil
	}
	out := &apicompat.ResponsesUsage{
		InputTokens:              usage.PromptTokens,
		OutputTokens:             usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CacheCreationInputTokens: chatCacheCreationTokens(usage.PromptTokensDetails),
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	if usage.PromptTokensDetails != nil &&
		(usage.PromptTokensDetails.CachedTokens > 0 || out.CacheCreationInputTokens > 0) {
		out.InputTokensDetails = &apicompat.ResponsesInputTokensDetails{
			CachedTokens:        usage.PromptTokensDetails.CachedTokens,
			CacheCreationTokens: out.CacheCreationInputTokens,
		}
	}
	return out
}

// ChatCompletionsToResponsesStreamState tracks state while converting Chat
// Completions SSE chunks into Responses SSE events.
type ChatCompletionsToResponsesStreamState struct {
	ResponseID     string
	Model          string
	Created        int64
	SequenceNumber int
	CreatedSent    bool
	CompletedSent  bool

	// nextOutputIndex assigns sequential output_index values to items as they
	// are opened (reasoning, message, tool calls), so the streamed indices match
	// the order of items in the final response.output array.
	nextOutputIndex int

	// Reasoning item lifecycle. DeepSeek-style upstreams stream all
	// reasoning_content before any content, so reasoning is modeled as its own
	// "reasoning" output item that must be opened (output_item.added) before any
	// reasoning delta and closed before the message/tool items open.
	ReasoningItemID string
	ReasoningIndex  int
	ReasoningOpen   bool
	ReasoningDone   bool

	// Message item + output_text content-part lifecycle.
	MessageItemID string
	MessageIndex  int
	TextPartOpen  bool

	Text      strings.Builder
	Reasoning strings.Builder

	// Tool-call lifecycle, keyed by the upstream tool_call index.
	ToolCalls       map[int]*apicompat.ChatToolCall
	ToolItemIDs     map[int]string
	ToolOutputIndex map[int]int

	CustomTools        map[string]bool
	ToolSearchDeclared bool
	NamespaceTools     map[string]apicompat.NamespacedToolName
	toolIsCustom       map[int]bool
	toolIsToolSearch   map[int]bool
	toolNamespace      map[int]apicompat.NamespacedToolName
	toolAnnounced      map[int]bool

	FinishReason string
	Usage        *apicompat.ResponsesUsage
}

// NewChatCompletionsToResponsesStreamState returns an initialized stream state.
func NewChatCompletionsToResponsesStreamState(model string) *ChatCompletionsToResponsesStreamState {
	return &ChatCompletionsToResponsesStreamState{
		ResponseID:       generateResponsesID(),
		Model:            model,
		Created:          time.Now().Unix(),
		ToolCalls:        make(map[int]*apicompat.ChatToolCall),
		ToolItemIDs:      make(map[int]string),
		ToolOutputIndex:  make(map[int]int),
		toolIsCustom:     make(map[int]bool),
		toolIsToolSearch: make(map[int]bool),
		toolNamespace:    make(map[int]apicompat.NamespacedToolName),
		toolAnnounced:    make(map[int]bool),
	}
}

func (state *ChatCompletionsToResponsesStreamState) allocOutputIndex() int {
	idx := state.nextOutputIndex
	state.nextOutputIndex++
	return idx
}

// ChatCompletionsChunkToResponsesEvents converts one Chat Completions stream
// chunk into zero or more Responses stream events.
func ChatCompletionsChunkToResponsesEvents(
	chunk *apicompat.ChatCompletionsChunk,
	state *ChatCompletionsToResponsesStreamState,
) []apicompat.ResponsesStreamEvent {
	if chunk == nil || state == nil {
		return nil
	}
	if chunk.ID != "" {
		state.ResponseID = chunk.ID
	}
	if state.Model == "" && chunk.Model != "" {
		state.Model = chunk.Model
	}
	if chunk.Usage != nil {
		state.Usage = ChatUsageToResponsesUsage(chunk.Usage)
	}

	var events []apicompat.ResponsesStreamEvent
	events = append(events, ensureChatToResponsesCreated(state)...)

	for _, choice := range chunk.Choices {
		// Reasoning is emitted as its own output item and must be opened
		// (output_item.added + reasoning_summary_part.added) before the first
		// delta, otherwise a strict client discards the delta. The leading
		// empty-string reasoning delta upstreams send is filtered out.
		if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			events = append(events, ensureChatReasoningItem(state)...)
			_, _ = state.Reasoning.WriteString(*choice.Delta.ReasoningContent)
			events = append(events, chatToResponsesEvent(state, "response.reasoning_summary_text.delta", &apicompat.ResponsesStreamEvent{
				OutputIndex:  state.ReasoningIndex,
				SummaryIndex: 0,
				Delta:        *choice.Delta.ReasoningContent,
				ItemID:       state.ReasoningItemID,
			}))
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			// First real content closes the reasoning item, then opens the
			// message item and its output_text content part.
			events = append(events, closeChatReasoningItem(state)...)
			events = append(events, ensureChatToResponsesMessageItem(state)...)
			events = append(events, ensureChatToResponsesTextPart(state)...)
			_, _ = state.Text.WriteString(*choice.Delta.Content)
			events = append(events, chatToResponsesEvent(state, "response.output_text.delta", &apicompat.ResponsesStreamEvent{
				OutputIndex:  state.MessageIndex,
				ContentIndex: 0,
				Delta:        *choice.Delta.Content,
				ItemID:       state.MessageItemID,
			}))
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			idx := 0
			if toolCall.Index != nil {
				idx = *toolCall.Index
			}
			stored, ok := state.ToolCalls[idx]
			if !ok {
				// A tool call closes any open reasoning item first.
				events = append(events, closeChatReasoningItem(state)...)
				copyCall := toolCall
				if copyCall.ID == "" {
					copyCall.ID = generateItemID()
				}
				copyCall.Type = "function"
				// Arguments are accumulated by the shared block below so the
				// emitted delta and the stored value stay in sync. Some upstreams
				// (e.g. GLM/Zhipu) pack id+name+arguments into the first tool_call
				// chunk; without this reset the first chunk's arguments would be
				// counted twice (once from this copy, once from the += below),
				// producing a doubled, invalid JSON like {"a":1}{"a":1}.
				copyCall.Function.Arguments = ""
				state.ToolCalls[idx] = &copyCall
				stored = &copyCall
				state.ToolItemIDs[idx] = generateItemID()
				state.ToolOutputIndex[idx] = state.allocOutputIndex()
			} else {
				if toolCall.ID != "" {
					stored.ID = toolCall.ID
				}
				if toolCall.Function.Name != "" {
					stored.Function.Name = toolCall.Function.Name
				}
			}
			events = append(events, announceChatToolItem(state, idx, stored, false)...)
			if toolCall.Function.Arguments != "" {
				stored.Function.Arguments += toolCall.Function.Arguments
				if state.toolAnnounced[idx] && !state.toolIsCustom[idx] && !state.toolIsToolSearch[idx] {
					events = append(events, chatToResponsesEvent(state, "response.function_call_arguments.delta", &apicompat.ResponsesStreamEvent{
						OutputIndex: state.ToolOutputIndex[idx],
						ItemID:      state.ToolItemIDs[idx],
						Delta:       toolCall.Function.Arguments,
						CallID:      stored.ID,
						Name:        stored.Function.Name,
					}))
				}
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			state.FinishReason = *choice.FinishReason
		}
	}

	return events
}

// FinalizeChatCompletionsResponsesStream emits terminal Responses events.
func FinalizeChatCompletionsResponsesStream(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if state == nil || state.CompletedSent {
		return nil
	}
	var events []apicompat.ResponsesStreamEvent
	events = append(events, ensureChatToResponsesCreated(state)...)

	// Close a reasoning item that never transitioned to content (reasoning-only
	// or empty completion).
	events = append(events, closeChatReasoningItem(state)...)
	events = append(events, synthesizeChatReasoningFallbackMessage(state)...)

	if state.MessageItemID != "" {
		if state.TextPartOpen {
			events = append(events, chatToResponsesEvent(state, "response.output_text.done", &apicompat.ResponsesStreamEvent{
				OutputIndex:  state.MessageIndex,
				ContentIndex: 0,
				Text:         state.Text.String(),
				ItemID:       state.MessageItemID,
			}))
			events = append(events, chatToResponsesEvent(state, "response.content_part.done", &apicompat.ResponsesStreamEvent{
				OutputIndex:  state.MessageIndex,
				ContentIndex: 0,
				ItemID:       state.MessageItemID,
				Part:         &apicompat.ResponsesContentPart{Type: "output_text", Text: state.Text.String()},
			}))
		}
		events = append(events, chatToResponsesEvent(state, "response.output_item.done", &apicompat.ResponsesStreamEvent{
			OutputIndex: state.MessageIndex,
			Item: &apicompat.ResponsesOutput{
				Type:    "message",
				ID:      state.MessageItemID,
				Role:    "assistant",
				Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: state.Text.String()}},
				Status:  "completed",
			},
		}))
	}

	// Close every function_call item opened during the stream. Codex finalizes a
	// tool call only after function_call_arguments.done + output_item.done for
	// that item; without them the call never completes and the session wedges.
	// Mirrors cc-switch's finalize_tools.
	events = append(events, closeChatToolItems(state)...)

	status := "completed"
	var incompleteDetails *apicompat.ResponsesIncompleteDetails
	if state.FinishReason == "length" {
		status = "incomplete"
		incompleteDetails = &apicompat.ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}

	state.CompletedSent = true
	events = append(events, chatToResponsesEvent(state, "response.completed", &apicompat.ResponsesStreamEvent{
		Response: &apicompat.ResponsesResponse{
			ID:                state.ResponseID,
			Object:            "response",
			Model:             state.Model,
			Status:            status,
			Output:            state.chatOutput(),
			Usage:             state.Usage,
			IncompleteDetails: incompleteDetails,
		},
	}))
	return events
}

func ensureChatToResponsesCreated(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if state.CreatedSent {
		return nil
	}
	state.CreatedSent = true
	return []apicompat.ResponsesStreamEvent{chatToResponsesEvent(state, "response.created", &apicompat.ResponsesStreamEvent{
		Response: &apicompat.ResponsesResponse{
			ID:     state.ResponseID,
			Object: "response",
			Model:  state.Model,
			Status: "in_progress",
			Output: []apicompat.ResponsesOutput{},
		},
	})}
}

// ensureChatReasoningItem opens the reasoning output item (output_item.added +
// reasoning_summary_part.added) before the first reasoning delta. Codex renders
// streaming reasoning only when this summary-part lifecycle is present.
func ensureChatReasoningItem(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if state.ReasoningOpen || state.ReasoningDone {
		return nil
	}
	state.ReasoningOpen = true
	state.ReasoningItemID = generateItemID()
	state.ReasoningIndex = state.allocOutputIndex()
	return []apicompat.ResponsesStreamEvent{
		chatToResponsesEvent(state, "response.output_item.added", &apicompat.ResponsesStreamEvent{
			OutputIndex: state.ReasoningIndex,
			Item:        &apicompat.ResponsesOutput{Type: "reasoning", ID: state.ReasoningItemID, Status: "in_progress"},
		}),
		chatToResponsesEvent(state, "response.reasoning_summary_part.added", &apicompat.ResponsesStreamEvent{
			OutputIndex:  state.ReasoningIndex,
			SummaryIndex: 0,
			ItemID:       state.ReasoningItemID,
			Part:         &apicompat.ResponsesContentPart{Type: "summary_text"},
		}),
	}
}

// closeChatReasoningItem emits the reasoning item's terminal events
// (reasoning_summary_text.done + reasoning_summary_part.done + output_item.done).
func closeChatReasoningItem(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if !state.ReasoningOpen {
		return nil
	}
	state.ReasoningOpen = false
	state.ReasoningDone = true
	reasoning := state.Reasoning.String()
	return []apicompat.ResponsesStreamEvent{
		chatToResponsesEvent(state, "response.reasoning_summary_text.done", &apicompat.ResponsesStreamEvent{
			OutputIndex:  state.ReasoningIndex,
			SummaryIndex: 0,
			Text:         reasoning,
			ItemID:       state.ReasoningItemID,
		}),
		chatToResponsesEvent(state, "response.reasoning_summary_part.done", &apicompat.ResponsesStreamEvent{
			OutputIndex:  state.ReasoningIndex,
			SummaryIndex: 0,
			ItemID:       state.ReasoningItemID,
			Part:         &apicompat.ResponsesContentPart{Type: "summary_text", Text: reasoning},
		}),
		chatToResponsesEvent(state, "response.output_item.done", &apicompat.ResponsesStreamEvent{
			OutputIndex: state.ReasoningIndex,
			Item: &apicompat.ResponsesOutput{
				Type:    "reasoning",
				ID:      state.ReasoningItemID,
				Status:  "completed",
				Summary: []apicompat.ResponsesSummary{{Type: "summary_text", Text: reasoning}},
			},
		}),
	}
}

func synthesizeChatReasoningFallbackMessage(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if state == nil ||
		state.MessageItemID != "" ||
		state.Text.Len() > 0 ||
		state.Reasoning.Len() == 0 ||
		len(state.ToolCalls) > 0 {
		return nil
	}

	text := state.Reasoning.String()
	if strings.TrimSpace(text) == "" {
		return nil
	}

	var events []apicompat.ResponsesStreamEvent
	events = append(events, ensureChatToResponsesMessageItem(state)...)
	events = append(events, ensureChatToResponsesTextPart(state)...)
	_, _ = state.Text.WriteString(text)
	events = append(events, chatToResponsesEvent(state, "response.output_text.delta", &apicompat.ResponsesStreamEvent{
		OutputIndex:  state.MessageIndex,
		ContentIndex: 0,
		Delta:        text,
		ItemID:       state.MessageItemID,
	}))
	return events
}

func ensureChatToResponsesMessageItem(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if state.MessageItemID != "" {
		return nil
	}
	state.MessageItemID = generateItemID()
	state.MessageIndex = state.allocOutputIndex()
	return []apicompat.ResponsesStreamEvent{chatToResponsesEvent(state, "response.output_item.added", &apicompat.ResponsesStreamEvent{
		OutputIndex: state.MessageIndex,
		Item: &apicompat.ResponsesOutput{
			Type:    "message",
			ID:      state.MessageItemID,
			Role:    "assistant",
			Status:  "in_progress",
			Content: []apicompat.ResponsesContentPart{{Type: "output_text"}},
		},
	})}
}

func ensureChatToResponsesTextPart(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if state.TextPartOpen {
		return nil
	}
	state.TextPartOpen = true
	return []apicompat.ResponsesStreamEvent{chatToResponsesEvent(state, "response.content_part.added", &apicompat.ResponsesStreamEvent{
		OutputIndex:  state.MessageIndex,
		ContentIndex: 0,
		ItemID:       state.MessageItemID,
		Part:         &apicompat.ResponsesContentPart{Type: "output_text", Text: ""},
	})}
}

func announceChatToolItem(
	state *ChatCompletionsToResponsesStreamState,
	index int,
	toolCall *apicompat.ChatToolCall,
	force bool,
) []apicompat.ResponsesStreamEvent {
	if state.toolAnnounced[index] {
		return nil
	}
	if !force && toolCall.Function.Name == "" &&
		(len(state.CustomTools) > 0 || state.ToolSearchDeclared || len(state.NamespaceTools) > 0) {
		return nil
	}

	state.toolAnnounced[index] = true
	isCustom := state.CustomTools[toolCall.Function.Name]
	isToolSearch := !isCustom && state.ToolSearchDeclared && toolCall.Function.Name == toolSearchProxyName
	state.toolIsCustom[index] = isCustom
	state.toolIsToolSearch[index] = isToolSearch

	itemType := "function_call"
	if isCustom {
		itemType = "custom_tool_call"
	} else if isToolSearch {
		itemType = "tool_search_call"
	}
	name, namespace := toolCall.Function.Name, ""
	if identity, ok := state.NamespaceTools[toolCall.Function.Name]; ok && !isCustom && !isToolSearch {
		state.toolNamespace[index] = identity
		name, namespace = identity.Name, identity.Namespace
	}

	events := []apicompat.ResponsesStreamEvent{chatToResponsesEvent(state, "response.output_item.added", &apicompat.ResponsesStreamEvent{
		OutputIndex: state.ToolOutputIndex[index],
		Item: &apicompat.ResponsesOutput{
			Type:      itemType,
			ID:        state.ToolItemIDs[index],
			CallID:    toolCall.ID,
			Name:      name,
			Namespace: namespace,
			Status:    "in_progress",
		},
	})}
	if !isCustom && !isToolSearch && toolCall.Function.Arguments != "" {
		events = append(events, chatToResponsesEvent(state, "response.function_call_arguments.delta", &apicompat.ResponsesStreamEvent{
			OutputIndex: state.ToolOutputIndex[index],
			ItemID:      state.ToolItemIDs[index],
			Delta:       toolCall.Function.Arguments,
			CallID:      toolCall.ID,
			Name:        toolCall.Function.Name,
		}))
	}
	return events
}

// closeChatToolItems emits function_call_arguments.done + output_item.done for
// every tool call opened during the stream, carrying the full call_id/name/
// arguments so codex can deserialize and execute the call. Mirrors cc-switch's
// finalize_tools.
func closeChatToolItems(state *ChatCompletionsToResponsesStreamState) []apicompat.ResponsesStreamEvent {
	if len(state.ToolCalls) == 0 {
		return nil
	}
	var events []apicompat.ResponsesStreamEvent
	for i := 0; i < len(state.ToolCalls); i++ {
		toolCall, ok := state.ToolCalls[i]
		if !ok || toolCall == nil {
			continue
		}
		itemID, opened := state.ToolItemIDs[i]
		if !opened {
			continue
		}
		events = append(events, announceChatToolItem(state, i, toolCall, true)...)
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		outputIndex := state.ToolOutputIndex[i]
		if state.toolIsCustom[i] {
			input := extractCustomToolCallInput(arguments)
			if input != "" {
				events = append(events, chatToResponsesEvent(state, "response.custom_tool_call_input.delta", &apicompat.ResponsesStreamEvent{
					OutputIndex: outputIndex,
					ItemID:      itemID,
					Delta:       input,
				}))
			}
			events = append(events,
				chatToResponsesEvent(state, "response.custom_tool_call_input.done", &apicompat.ResponsesStreamEvent{
					OutputIndex: outputIndex,
					ItemID:      itemID,
					CallID:      toolCall.ID,
					Name:        toolCall.Function.Name,
					Input:       input,
				}),
				chatToResponsesEvent(state, "response.output_item.done", &apicompat.ResponsesStreamEvent{
					OutputIndex: outputIndex,
					Item: &apicompat.ResponsesOutput{
						Type:   "custom_tool_call",
						ID:     itemID,
						CallID: toolCall.ID,
						Name:   toolCall.Function.Name,
						Input:  input,
						Status: "completed",
					},
				}),
			)
			continue
		}
		if state.toolIsToolSearch[i] {
			events = append(events, chatToResponsesEvent(state, "response.output_item.done", &apicompat.ResponsesStreamEvent{
				OutputIndex: outputIndex,
				Item: &apicompat.ResponsesOutput{
					Type:      "tool_search_call",
					ID:        itemID,
					CallID:    toolCall.ID,
					Arguments: arguments,
					Status:    "completed",
				},
			}))
			continue
		}
		name, namespace := toolCall.Function.Name, ""
		if identity, ok := state.toolNamespace[i]; ok {
			name, namespace = identity.Name, identity.Namespace
		}
		events = append(events,
			chatToResponsesEvent(state, "response.function_call_arguments.done", &apicompat.ResponsesStreamEvent{
				OutputIndex: outputIndex,
				ItemID:      itemID,
				CallID:      toolCall.ID,
				Name:        name,
				Arguments:   arguments,
			}),
			chatToResponsesEvent(state, "response.output_item.done", &apicompat.ResponsesStreamEvent{
				OutputIndex: outputIndex,
				Item: &apicompat.ResponsesOutput{
					Type:      "function_call",
					ID:        itemID,
					CallID:    toolCall.ID,
					Name:      name,
					Namespace: namespace,
					Arguments: arguments,
					Status:    "completed",
				},
			}),
		)
	}
	return events
}

func (state *ChatCompletionsToResponsesStreamState) chatOutput() []apicompat.ResponsesOutput {
	var outputs []apicompat.ResponsesOutput
	if state.Reasoning.Len() > 0 {
		outputs = append(outputs, apicompat.ResponsesOutput{
			Type: "reasoning",
			ID:   generateItemID(),
			Summary: []apicompat.ResponsesSummary{{
				Type: "summary_text",
				Text: state.Reasoning.String(),
			}},
		})
	}
	if state.MessageItemID != "" || len(state.ToolCalls) == 0 {
		outputs = append(outputs, apicompat.ResponsesOutput{
			Type: "message",
			ID:   nonEmpty(state.MessageItemID, generateItemID()),
			Role: "assistant",
			Content: []apicompat.ResponsesContentPart{{
				Type: "output_text",
				Text: state.Text.String(),
			}},
			Status: "completed",
		})
	}
	for i := 0; i < len(state.ToolCalls); i++ {
		toolCall, ok := state.ToolCalls[i]
		if !ok || toolCall == nil {
			continue
		}
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		if state.toolIsCustom[i] {
			outputs = append(outputs, apicompat.ResponsesOutput{
				Type:   "custom_tool_call",
				ID:     generateItemID(),
				CallID: toolCall.ID,
				Name:   toolCall.Function.Name,
				Input:  extractCustomToolCallInput(arguments),
				Status: "completed",
			})
			continue
		}
		if state.toolIsToolSearch[i] {
			outputs = append(outputs, apicompat.ResponsesOutput{
				Type:      "tool_search_call",
				ID:        generateItemID(),
				CallID:    toolCall.ID,
				Arguments: arguments,
				Status:    "completed",
			})
			continue
		}
		name, namespace := toolCall.Function.Name, ""
		if identity, ok := state.toolNamespace[i]; ok {
			name, namespace = identity.Name, identity.Namespace
		}
		outputs = append(outputs, apicompat.ResponsesOutput{
			Type:      "function_call",
			ID:        generateItemID(),
			CallID:    toolCall.ID,
			Name:      name,
			Namespace: namespace,
			Arguments: arguments,
			Status:    "completed",
		})
	}
	return outputs
}

func chatToResponsesEvent(
	state *ChatCompletionsToResponsesStreamState,
	eventType string,
	template *apicompat.ResponsesStreamEvent,
) apicompat.ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++
	evt := *template
	evt.Type = eventType
	evt.SequenceNumber = seq
	return evt
}

func rawString(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func rawNestedString(raw json.RawMessage, key string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return rawString(obj[key])
}

func bytesTrimSpace(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func generateResponsesID() string {
	buffer := make([]byte, 12)
	_, _ = rand.Read(buffer)
	return "resp_" + hex.EncodeToString(buffer)
}

func generateItemID() string {
	buffer := make([]byte, 12)
	_, _ = rand.Read(buffer)
	return "item_" + hex.EncodeToString(buffer)
}

func chatCacheCreationTokens(details *apicompat.ChatTokenDetails) int {
	if details == nil {
		return 0
	}
	if details.CacheWriteTokens > 0 {
		return details.CacheWriteTokens
	}
	return details.CacheCreationTokens
}
