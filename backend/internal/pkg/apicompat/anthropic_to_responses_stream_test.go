package apicompat

import "testing"

func TestAnthropicEventToResponsesTextEmitsOrderedContentPartEvents(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	state.Model = "claude-sonnet-4-5"

	var events []ResponsesStreamEvent
	feed := func(event *AnthropicStreamEvent) {
		events = append(events, AnthropicEventToResponsesEvents(event, state)...)
	}

	index := 0
	feed(&AnthropicStreamEvent{Type: "message_start", Message: &AnthropicResponse{ID: "msg_1", Model: state.Model}})
	feed(&AnthropicStreamEvent{Type: "content_block_start", Index: &index, ContentBlock: &AnthropicContentBlock{Type: "text"}})
	feed(&AnthropicStreamEvent{Type: "content_block_delta", Index: &index, Delta: &AnthropicDelta{Type: "text_delta", Text: "Hel"}})
	feed(&AnthropicStreamEvent{Type: "content_block_delta", Index: &index, Delta: &AnthropicDelta{Type: "text_delta", Text: "lo"}})
	feed(&AnthropicStreamEvent{Type: "content_block_stop", Index: &index})
	feed(&AnthropicStreamEvent{Type: "message_stop"})

	position := func(eventType string) int {
		for i := range events {
			if events[i].Type == eventType {
				return i
			}
		}
		return -1
	}
	partAdded := position("response.content_part.added")
	firstDelta := position("response.output_text.delta")
	textDone := position("response.output_text.done")
	partDone := position("response.content_part.done")
	if partAdded < 0 || firstDelta < 0 || textDone < 0 || partDone < 0 {
		t.Fatalf("missing required content events: %+v", eventTypes(events))
	}
	if partAdded >= firstDelta || firstDelta >= textDone || textDone >= partDone {
		t.Fatalf("invalid content event order: %+v", eventTypes(events))
	}
	if events[textDone].Text != "Hello" {
		t.Fatalf("output_text.done text = %q, want Hello", events[textDone].Text)
	}
	if events[partDone].Part == nil || events[partDone].Part.Text != "Hello" {
		t.Fatalf("content_part.done part = %+v, want full text", events[partDone].Part)
	}
}

func TestAnthropicEventToResponsesMultipleTextPartsUseDistinctIndexes(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	var events []ResponsesStreamEvent
	feed := func(event *AnthropicStreamEvent) {
		events = append(events, AnthropicEventToResponsesEvents(event, state)...)
	}

	feed(&AnthropicStreamEvent{Type: "message_start", Message: &AnthropicResponse{ID: "msg_multi"}})
	for index, text := range []string{"first", "second"} {
		blockIndex := index
		feed(&AnthropicStreamEvent{Type: "content_block_start", Index: &blockIndex, ContentBlock: &AnthropicContentBlock{Type: "text"}})
		feed(&AnthropicStreamEvent{Type: "content_block_delta", Index: &blockIndex, Delta: &AnthropicDelta{Type: "text_delta", Text: text}})
		feed(&AnthropicStreamEvent{Type: "content_block_stop", Index: &blockIndex})
	}
	feed(&AnthropicStreamEvent{Type: "message_stop"})

	var addedIndexes []int
	var completed *ResponsesStreamEvent
	for i := range events {
		switch events[i].Type {
		case "response.content_part.added":
			addedIndexes = append(addedIndexes, events[i].ContentIndex)
		case "response.completed":
			completed = &events[i]
		}
	}
	if len(addedIndexes) != 2 || addedIndexes[0] != 0 || addedIndexes[1] != 1 {
		t.Fatalf("content_part.added indexes = %v, want [0 1]", addedIndexes)
	}
	if completed == nil || completed.Response == nil || len(completed.Response.Output) != 1 {
		t.Fatalf("terminal output missing: %+v", completed)
	}
	content := completed.Response.Output[0].Content
	if len(content) != 2 || content[0].Text != "first" || content[1].Text != "second" {
		t.Fatalf("terminal message content = %+v", content)
	}
}

func TestAnthropicEventToResponsesCompletedCarriesFullTextOutput(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	state.Model = "claude-sonnet-4-5"
	var events []ResponsesStreamEvent
	feed := func(event *AnthropicStreamEvent) {
		events = append(events, AnthropicEventToResponsesEvents(event, state)...)
	}

	index := 0
	feed(&AnthropicStreamEvent{Type: "message_start", Message: &AnthropicResponse{ID: "msg_1"}})
	feed(&AnthropicStreamEvent{Type: "content_block_start", Index: &index, ContentBlock: &AnthropicContentBlock{Type: "text"}})
	feed(&AnthropicStreamEvent{Type: "content_block_delta", Index: &index, Delta: &AnthropicDelta{Type: "text_delta", Text: "4826"}})
	feed(&AnthropicStreamEvent{Type: "content_block_stop", Index: &index})
	feed(&AnthropicStreamEvent{Type: "message_stop"})

	completed := terminalResponseEvent(events)
	if completed == nil || completed.Response == nil || len(completed.Response.Output) != 1 {
		t.Fatalf("response.completed carries no output")
	}
	message := completed.Response.Output[0]
	if message.Type != "message" || message.Role != "assistant" || len(message.Content) != 1 {
		t.Fatalf("terminal output = %+v, want completed assistant message", message)
	}
	if message.Content[0].Text != "4826" {
		t.Fatalf("terminal output text = %q, want 4826", message.Content[0].Text)
	}
}

func TestAnthropicEventToResponsesToolCallCarriesArgumentsInDoneAndCompleted(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	var events []ResponsesStreamEvent
	feed := func(event *AnthropicStreamEvent) {
		events = append(events, AnthropicEventToResponsesEvents(event, state)...)
	}

	index := 0
	feed(&AnthropicStreamEvent{Type: "message_start", Message: &AnthropicResponse{ID: "msg_tool"}})
	feed(&AnthropicStreamEvent{Type: "content_block_start", Index: &index, ContentBlock: &AnthropicContentBlock{
		Type: "tool_use", ID: "toolu_1", Name: "get_weather",
	}})
	feed(&AnthropicStreamEvent{Type: "content_block_delta", Index: &index, Delta: &AnthropicDelta{Type: "input_json_delta", PartialJSON: `{"city":`}})
	feed(&AnthropicStreamEvent{Type: "content_block_delta", Index: &index, Delta: &AnthropicDelta{Type: "input_json_delta", PartialJSON: `"SH"}`}})
	feed(&AnthropicStreamEvent{Type: "content_block_stop", Index: &index})
	feed(&AnthropicStreamEvent{Type: "message_stop"})

	var doneItem *ResponsesOutput
	for i := range events {
		if events[i].Type == "response.output_item.done" {
			doneItem = events[i].Item
		}
	}
	completed := terminalResponseEvent(events)
	if doneItem == nil || completed == nil || completed.Response == nil || len(completed.Response.Output) != 1 {
		t.Fatalf("tool call output missing from done or terminal event")
	}
	for label, call := range map[string]ResponsesOutput{
		"output_item.done":   *doneItem,
		"response.completed": completed.Response.Output[0],
	} {
		if call.Type != "function_call" || call.Name != "get_weather" || call.Arguments != `{"city":"SH"}` {
			t.Fatalf("%s tool call = %+v", label, call)
		}
	}
}

func TestFinalizeAnthropicResponsesStreamCarriesAccumulatedOutput(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	index := 0
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_start", Message: &AnthropicResponse{ID: "msg_truncated"}}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_start", Index: &index, ContentBlock: &AnthropicContentBlock{Type: "text"}}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_delta", Index: &index, Delta: &AnthropicDelta{Type: "text_delta", Text: "partial"}}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop", Index: &index}, state)

	events := FinalizeAnthropicResponsesStream(state)
	completed := terminalResponseEvent(events)
	if completed == nil || completed.Response == nil || len(completed.Response.Output) != 1 {
		t.Fatalf("synthetic terminal event carries no output: %+v", events)
	}
	if got := completed.Response.Output[0].Content[0].Text; got != "partial" {
		t.Fatalf("synthetic terminal output text = %q, want partial", got)
	}
}

func eventTypes(events []ResponsesStreamEvent) []string {
	types := make([]string, 0, len(events))
	for i := range events {
		types = append(types, events[i].Type)
	}
	return types
}

func terminalResponseEvent(events []ResponsesStreamEvent) *ResponsesStreamEvent {
	for i := range events {
		if events[i].Type == "response.completed" || events[i].Type == "response.incomplete" {
			return &events[i]
		}
	}
	return nil
}
