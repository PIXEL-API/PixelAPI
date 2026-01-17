package service

import "strings"

// NeedsToolContinuation 判定请求是否需要工具调用续链处理。
// 满足以下任一信号即视为续链：previous_response_id、input 内包含 function_call_output/item_reference、
// 或显式声明 tools/tool_choice。
func NeedsToolContinuation(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	if hasNonEmptyString(reqBody["previous_response_id"]) {
		return true
	}
	if hasToolsSignal(reqBody) {
		return true
	}
	if hasToolChoiceSignal(reqBody) {
		return true
	}
	if inputHasType(reqBody, "function_call_output") {
		return true
	}
	if inputHasType(reqBody, "item_reference") {
		return true
	}
	return false
}

// HasFunctionCallOutput 判断 input 是否包含 function_call_output，用于触发续链校验。
func HasFunctionCallOutput(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	return inputHasType(reqBody, "function_call_output")
}

// HasToolCallContext 判断 input 是否包含带 call_id 的 tool_call/function_call，
// 用于判断 function_call_output 是否具备可关联的上下文。
func HasToolCallContext(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "tool_call" && itemType != "function_call" {
			continue
		}
		if callID, ok := itemMap["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			return true
		}
	}
	return false
}

// FunctionCallOutputCallIDs 提取 input 中 function_call_output 的 call_id 集合。
// 仅返回非空 call_id，用于与 item_reference.id 做匹配校验。
func FunctionCallOutputCallIDs(reqBody map[string]any) []string {
	if reqBody == nil {
		return nil
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return nil
	}
	ids := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "function_call_output" {
			continue
		}
		if callID, ok := itemMap["call_id"].(string); ok && strings.TrimSpace(callID) != "" {
			ids[callID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	return result
}

// HasFunctionCallOutputMissingCallID 判断是否存在缺少 call_id 的 function_call_output。
func HasFunctionCallOutputMissingCallID(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "function_call_output" {
			continue
		}
		callID, _ := itemMap["call_id"].(string)
		if strings.TrimSpace(callID) == "" {
			return true
		}
	}
	return false
}

// HasItemReferenceForCallIDs 判断 item_reference.id 是否覆盖所有 call_id。
// 用于仅依赖引用项完成续链场景的校验。
func HasItemReferenceForCallIDs(reqBody map[string]any, callIDs []string) bool {
	if reqBody == nil || len(callIDs) == 0 {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	referenceIDs := make(map[string]struct{})
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "item_reference" {
			continue
		}
		idValue, _ := itemMap["id"].(string)
		idValue = strings.TrimSpace(idValue)
		if idValue == "" {
			continue
		}
		referenceIDs[idValue] = struct{}{}
	}
	if len(referenceIDs) == 0 {
		return false
	}
	for _, callID := range callIDs {
		if _, ok := referenceIDs[callID]; !ok {
			return false
		}
	}
	return true
}

// inputHasType 判断 input 中是否存在指定类型的 item。
func inputHasType(reqBody map[string]any, want string) bool {
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType == want {
			return true
		}
	}
	return false
}

// hasNonEmptyString 判断字段是否为非空字符串。
func hasNonEmptyString(value any) bool {
	stringValue, ok := value.(string)
	return ok && strings.TrimSpace(stringValue) != ""
}

// hasToolsSignal 判断 tools 字段是否显式声明（存在且不为空）。
func hasToolsSignal(reqBody map[string]any) bool {
	raw, exists := reqBody["tools"]
	if !exists || raw == nil {
		return false
	}
	if tools, ok := raw.([]any); ok {
		return len(tools) > 0
	}
	return false
}

// hasToolChoiceSignal 判断 tool_choice 是否显式声明（非空或非 nil）。
func hasToolChoiceSignal(reqBody map[string]any) bool {
	raw, exists := reqBody["tool_choice"]
	if !exists || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case map[string]any:
		return len(value) > 0
	default:
		return false
	}
}
