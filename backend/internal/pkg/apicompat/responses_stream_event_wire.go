package apicompat

import "encoding/json"

// MarshalJSON renders strict Responses SSE wire objects. Index fields remain
// present at zero and tool/message items keep protocol-required empty values.
func (e ResponsesStreamEvent) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case "response.output_text.delta", "response.output_text.done":
		wire := e.wireBase()
		e.putItemID(wire)
		wire["output_index"] = e.OutputIndex
		wire["content_index"] = e.ContentIndex
		if e.Type == "response.output_text.done" {
			wire["text"] = e.Text
		} else {
			wire["delta"] = e.Delta
		}
		return json.Marshal(wire)

	case "response.content_part.added", "response.content_part.done":
		wire := e.wireBase()
		e.putItemID(wire)
		wire["output_index"] = e.OutputIndex
		wire["content_index"] = e.ContentIndex
		wire["part"] = outputTextPartWire(e.Part)
		return json.Marshal(wire)

	case "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done":
		wire := e.wireBase()
		e.putItemID(wire)
		wire["output_index"] = e.OutputIndex
		wire["summary_index"] = e.SummaryIndex
		if e.Type == "response.reasoning_summary_text.done" {
			wire["text"] = e.Text
		} else {
			wire["delta"] = e.Delta
		}
		return json.Marshal(wire)

	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		wire := e.wireBase()
		e.putItemID(wire)
		wire["output_index"] = e.OutputIndex
		wire["summary_index"] = e.SummaryIndex
		wire["part"] = summaryTextPartWire(e.Part)
		return json.Marshal(wire)

	case "response.output_item.added", "response.output_item.done":
		wire := e.wireBase()
		wire["output_index"] = e.OutputIndex
		wire["item"] = responsesItemWire(e.Item)
		return json.Marshal(wire)

	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		wire := e.wireBase()
		e.putItemID(wire)
		wire["output_index"] = e.OutputIndex
		if e.CallID != "" {
			wire["call_id"] = e.CallID
		}
		if e.Name != "" {
			wire["name"] = e.Name
		}
		if e.Type == "response.function_call_arguments.done" {
			wire["arguments"] = e.Arguments
		} else {
			wire["delta"] = e.Delta
		}
		return json.Marshal(wire)

	case "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
		wire := e.wireBase()
		e.putItemID(wire)
		wire["output_index"] = e.OutputIndex
		if e.CallID != "" {
			wire["call_id"] = e.CallID
		}
		if e.Name != "" {
			wire["name"] = e.Name
		}
		if e.Type == "response.custom_tool_call_input.done" {
			wire["input"] = e.Input
		} else {
			wire["delta"] = e.Delta
		}
		return json.Marshal(wire)

	default:
		type alias ResponsesStreamEvent
		return json.Marshal(alias(e))
	}
}

func (e ResponsesStreamEvent) wireBase() map[string]any {
	return map[string]any{
		"type":            e.Type,
		"sequence_number": e.SequenceNumber,
	}
}

func (e ResponsesStreamEvent) putItemID(wire map[string]any) {
	if e.ItemID != "" {
		wire["item_id"] = e.ItemID
	}
}

func outputTextPartWire(part *ResponsesContentPart) map[string]any {
	text := ""
	if part != nil {
		text = part.Text
	}
	return map[string]any{
		"type":        "output_text",
		"text":        text,
		"annotations": []any{},
		"logprobs":    []any{},
	}
}

func summaryTextPartWire(part *ResponsesContentPart) map[string]any {
	text := ""
	if part != nil {
		text = part.Text
	}
	return map[string]any{"type": "summary_text", "text": text}
}

func responsesItemWire(item *ResponsesOutput) map[string]any {
	if item == nil {
		return map[string]any{}
	}
	wire := map[string]any{"type": item.Type, "id": item.ID}
	if item.Status != "" {
		wire["status"] = item.Status
	}
	switch item.Type {
	case "message":
		role := item.Role
		if role == "" {
			role = "assistant"
		}
		wire["role"] = role
		wire["content"] = messageContentWire(item.Content)
	case "reasoning":
		wire["summary"] = reasoningSummaryWire(item.Summary)
		if item.EncryptedContent != "" {
			wire["encrypted_content"] = item.EncryptedContent
		}
	case "function_call":
		wire["call_id"] = item.CallID
		wire["name"] = item.Name
		wire["arguments"] = item.Arguments
		if item.Namespace != "" {
			wire["namespace"] = item.Namespace
		}
	case "custom_tool_call":
		wire["call_id"] = item.CallID
		wire["name"] = item.Name
		wire["input"] = item.Input
	case "tool_search_call":
		wire["call_id"] = item.CallID
		wire["execution"] = "client"
		wire["arguments"] = ToolSearchCallArgumentsJSON(item.Arguments)
	}
	return wire
}

func messageContentWire(parts []ResponsesContentPart) []map[string]any {
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		typeName := part.Type
		if typeName == "" {
			typeName = "output_text"
		}
		out = append(out, map[string]any{"type": typeName, "text": part.Text})
	}
	return out
}

func reasoningSummaryWire(summary []ResponsesSummary) []map[string]any {
	out := make([]map[string]any, 0, len(summary))
	for _, part := range summary {
		typeName := part.Type
		if typeName == "" {
			typeName = "summary_text"
		}
		out = append(out, map[string]any{"type": typeName, "text": part.Text})
	}
	return out
}
