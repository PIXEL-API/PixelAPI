package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ConvertChatCompletionsToResponses converts an OpenAI Chat Completions request to a Responses request.
func ConvertChatCompletionsToResponses(req map[string]any) (map[string]any, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}

	model := strings.TrimSpace(getString(req["model"]))
	if model == "" {
		return nil, errors.New("model is required")
	}

	messagesRaw, ok := req["messages"]
	if !ok {
		return nil, errors.New("messages is required")
	}
	messages, ok := messagesRaw.([]any)
	if !ok {
		return nil, errors.New("messages must be an array")
	}

	input, err := convertChatMessagesToResponsesInput(messages)
	if err != nil {
		return nil, err
	}

	out := make(map[string]any, len(req)+1)
	for key, value := range req {
		switch key {
		case "messages", "max_tokens", "max_completion_tokens", "stream_options", "functions", "function_call":
			continue
		default:
			out[key] = value
		}
	}

	out["model"] = model
	out["input"] = input

	if _, ok := out["max_output_tokens"]; !ok {
		if v, ok := req["max_tokens"]; ok {
			out["max_output_tokens"] = v
		} else if v, ok := req["max_completion_tokens"]; ok {
			out["max_output_tokens"] = v
		}
	}

	if _, ok := out["tools"]; !ok {
		if functions, ok := req["functions"].([]any); ok && len(functions) > 0 {
			tools := make([]any, 0, len(functions))
			for _, fn := range functions {
				if fnMap, ok := fn.(map[string]any); ok {
					tools = append(tools, map[string]any{
						"type":     "function",
						"function": fnMap,
					})
				}
			}
			if len(tools) > 0 {
				out["tools"] = tools
			}
		}
	}

	if _, ok := out["tool_choice"]; !ok {
		if functionCall, ok := req["function_call"]; ok {
			out["tool_choice"] = functionCall
		}
	}

	return out, nil
}

// ConvertResponsesToChatCompletion converts an OpenAI Responses response body to Chat Completions format.
func ConvertResponsesToChatCompletion(body []byte) ([]byte, error) {
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	id := strings.TrimSpace(getString(resp["id"]))
	if id == "" {
		id = "chatcmpl-" + safeRandomHex(12)
	}
	model := strings.TrimSpace(getString(resp["model"]))

	created := getInt64(resp["created_at"])
	if created == 0 {
		created = getInt64(resp["created"])
	}
	if created == 0 {
		created = time.Now().Unix()
	}

	text, toolCalls := extractResponseTextAndToolCalls(resp)
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	message := map[string]any{
		"role":    "assistant",
		"content": text,
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	chatResp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
	}

	if usage := extractResponseUsage(resp); usage != nil {
		chatResp["usage"] = usage
	}
	if fingerprint := strings.TrimSpace(getString(resp["system_fingerprint"])); fingerprint != "" {
		chatResp["system_fingerprint"] = fingerprint
	}

	return json.Marshal(chatResp)
}

func convertChatMessagesToResponsesInput(messages []any) ([]any, error) {
	input := make([]any, 0, len(messages))
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			return nil, errors.New("message must be an object")
		}
		role := strings.TrimSpace(getString(msgMap["role"]))
		if role == "" {
			return nil, errors.New("message role is required")
		}

		switch role {
		case "tool":
			callID := strings.TrimSpace(getString(msgMap["tool_call_id"]))
			if callID == "" {
				callID = strings.TrimSpace(getString(msgMap["id"]))
			}
			output := extractMessageContentText(msgMap["content"])
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			})
		case "function":
			callID := strings.TrimSpace(getString(msgMap["name"]))
			output := extractMessageContentText(msgMap["content"])
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			})
		default:
			convertedContent := convertChatContent(msgMap["content"])
			toolCalls := []any(nil)
			if role == "assistant" {
				toolCalls = extractToolCallsFromMessage(msgMap)
			}
			skipAssistantMessage := role == "assistant" && len(toolCalls) > 0 && isEmptyContent(convertedContent)
			if !skipAssistantMessage {
				msgItem := map[string]any{
					"role":    role,
					"content": convertedContent,
				}
				if name := strings.TrimSpace(getString(msgMap["name"])); name != "" {
					msgItem["name"] = name
				}
				input = append(input, msgItem)
			}
			if role == "assistant" && len(toolCalls) > 0 {
				input = append(input, toolCalls...)
			}
		}
	}
	return input, nil
}

func convertChatContent(content any) any {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		converted := make([]any, 0, len(v))
		for _, part := range v {
			partMap, ok := part.(map[string]any)
			if !ok {
				converted = append(converted, part)
				continue
			}
			partType := strings.TrimSpace(getString(partMap["type"]))
			switch partType {
			case "text":
				text := getString(partMap["text"])
				if text != "" {
					converted = append(converted, map[string]any{
						"type": "input_text",
						"text": text,
					})
					continue
				}
			case "image_url":
				imageURL := ""
				if imageObj, ok := partMap["image_url"].(map[string]any); ok {
					imageURL = getString(imageObj["url"])
				} else {
					imageURL = getString(partMap["image_url"])
				}
				if imageURL != "" {
					converted = append(converted, map[string]any{
						"type":      "input_image",
						"image_url": imageURL,
					})
					continue
				}
			case "input_text", "input_image":
				converted = append(converted, partMap)
				continue
			}
			converted = append(converted, partMap)
		}
		return converted
	default:
		return v
	}
}

func extractToolCallsFromMessage(msg map[string]any) []any {
	var out []any
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, call := range toolCalls {
			callMap, ok := call.(map[string]any)
			if !ok {
				continue
			}
			callID := strings.TrimSpace(getString(callMap["id"]))
			if callID == "" {
				callID = strings.TrimSpace(getString(callMap["call_id"]))
			}
			name := ""
			args := ""
			if fn, ok := callMap["function"].(map[string]any); ok {
				name = strings.TrimSpace(getString(fn["name"]))
				args = getString(fn["arguments"])
			}
			if name == "" && args == "" {
				continue
			}
			item := map[string]any{
				"type": "tool_call",
			}
			if callID != "" {
				item["call_id"] = callID
			}
			if name != "" {
				item["name"] = name
			}
			if args != "" {
				item["arguments"] = args
			}
			out = append(out, item)
		}
	}

	if fnCall, ok := msg["function_call"].(map[string]any); ok {
		name := strings.TrimSpace(getString(fnCall["name"]))
		args := getString(fnCall["arguments"])
		if name != "" || args != "" {
			callID := strings.TrimSpace(getString(msg["tool_call_id"]))
			if callID == "" {
				callID = name
			}
			item := map[string]any{
				"type": "function_call",
			}
			if callID != "" {
				item["call_id"] = callID
			}
			if name != "" {
				item["name"] = name
			}
			if args != "" {
				item["arguments"] = args
			}
			out = append(out, item)
		}
	}

	return out
}

func extractMessageContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			partType := strings.TrimSpace(getString(partMap["type"]))
			if partType == "" || partType == "text" || partType == "output_text" || partType == "input_text" {
				text := getString(partMap["text"])
				if text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func isEmptyContent(content any) bool {
	switch v := content.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

func extractResponseTextAndToolCalls(resp map[string]any) (string, []any) {
	output, ok := resp["output"].([]any)
	if !ok {
		if text, ok := resp["output_text"].(string); ok {
			return text, nil
		}
		return "", nil
	}

	textParts := make([]string, 0)
	toolCalls := make([]any, 0)

	for _, item := range output {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType := strings.TrimSpace(getString(itemMap["type"]))

		if itemType == "tool_call" || itemType == "function_call" {
			if tc := responseItemToChatToolCall(itemMap); tc != nil {
				toolCalls = append(toolCalls, tc)
			}
			continue
		}

		content := itemMap["content"]
		switch v := content.(type) {
		case string:
			if v != "" {
				textParts = append(textParts, v)
			}
		case []any:
			for _, part := range v {
				partMap, ok := part.(map[string]any)
				if !ok {
					continue
				}
				partType := strings.TrimSpace(getString(partMap["type"]))
				switch partType {
				case "output_text", "text", "input_text":
					text := getString(partMap["text"])
					if text != "" {
						textParts = append(textParts, text)
					}
				case "tool_call", "function_call":
					if tc := responseItemToChatToolCall(partMap); tc != nil {
						toolCalls = append(toolCalls, tc)
					}
				}
			}
		}
	}

	return strings.Join(textParts, ""), toolCalls
}

func responseItemToChatToolCall(item map[string]any) map[string]any {
	callID := strings.TrimSpace(getString(item["call_id"]))
	if callID == "" {
		callID = strings.TrimSpace(getString(item["id"]))
	}
	name := strings.TrimSpace(getString(item["name"]))
	arguments := getString(item["arguments"])
	if fn, ok := item["function"].(map[string]any); ok {
		if name == "" {
			name = strings.TrimSpace(getString(fn["name"]))
		}
		if arguments == "" {
			arguments = getString(fn["arguments"])
		}
	}

	if name == "" && arguments == "" && callID == "" {
		return nil
	}

	if callID == "" {
		callID = "call_" + safeRandomHex(6)
	}

	return map[string]any{
		"id":   callID,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}
}

func extractResponseUsage(resp map[string]any) map[string]any {
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		return nil
	}
	promptTokens := int(getNumber(usage["input_tokens"]))
	completionTokens := int(getNumber(usage["output_tokens"]))
	if promptTokens == 0 && completionTokens == 0 {
		return nil
	}

	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      promptTokens + completionTokens,
	}
}

func getString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func getNumber(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func getInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		i, _ := v.Int64()
		return i
	default:
		return 0
	}
}

func safeRandomHex(byteLength int) string {
	value, err := randomHexString(byteLength)
	if err != nil || value == "" {
		return "000000"
	}
	return value
}
