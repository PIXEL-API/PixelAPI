package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ChatCompletionsToResponses tests
// ---------------------------------------------------------------------------

func TestChatCompletionsToResponses_BasicText(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.True(t, resp.Stream) // always forced true
	assert.False(t, *resp.Store)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)
	assert.Equal(t, "user", items[0].Role)
}

func TestChatCompletionsToResponses_SystemMessage(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are helpful."`)},
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 2)
	assert.Equal(t, "system", items[0].Role)
	assert.Equal(t, "user", items[1].Role)
}

func TestChatCompletionsToResponses_ToolCalls(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Call the function"`)},
			{
				Role: "assistant",
				ToolCalls: []ChatToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: ChatFunctionCall{
							Name:      "ping",
							Arguments: `{"host":"example.com"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_1",
				Content:    json.RawMessage(`"pong"`),
			},
		},
		Tools: []ChatTool{
			{
				Type: "function",
				Function: &ChatFunction{
					Name:        "ping",
					Description: "Ping a host",
					Parameters:  json.RawMessage(`{"type":"object"}`),
				},
			},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	// user + function_call + function_call_output = 3
	// (assistant message with empty content + tool_calls → only function_call items emitted)
	require.Len(t, items, 3)

	// Check function_call item
	assert.Equal(t, "function_call", items[1].Type)
	assert.Equal(t, "call_1", items[1].CallID)
	assert.Equal(t, "ping", items[1].Name)

	// Check function_call_output item
	assert.Equal(t, "function_call_output", items[2].Type)
	assert.Equal(t, "call_1", items[2].CallID)
	assert.Equal(t, "pong", items[2].Output)

	// Check tools
	require.Len(t, resp.Tools, 1)
	assert.Equal(t, "function", resp.Tools[0].Type)
	assert.Equal(t, "ping", resp.Tools[0].Name)
}

func TestChatCompletionsToResponses_MaxTokens(t *testing.T) {
	t.Run("max_tokens", func(t *testing.T) {
		maxTokens := 100
		req := &ChatCompletionsRequest{
			Model:     "gpt-4o",
			MaxTokens: &maxTokens,
			Messages:  []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		}
		resp, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		require.NotNil(t, resp.MaxOutputTokens)
		// Below minMaxOutputTokens (128), should be clamped
		assert.Equal(t, minMaxOutputTokens, *resp.MaxOutputTokens)
	})

	t.Run("max_completion_tokens_preferred", func(t *testing.T) {
		maxTokens := 100
		maxCompletion := 500
		req := &ChatCompletionsRequest{
			Model:               "gpt-4o",
			MaxTokens:           &maxTokens,
			MaxCompletionTokens: &maxCompletion,
			Messages:            []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
		}
		resp, err := ChatCompletionsToResponses(req)
		require.NoError(t, err)
		require.NotNil(t, resp.MaxOutputTokens)
		assert.Equal(t, 500, *resp.MaxOutputTokens)
	})
}

func TestChatCompletionsToResponses_ReasoningEffort(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model:           "gpt-4o",
		ReasoningEffort: "high",
		Messages:        []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	require.NotNil(t, resp.Reasoning)
	assert.Equal(t, "high", resp.Reasoning.Effort)
	assert.Equal(t, "auto", resp.Reasoning.Summary)
}

func TestChatCompletionsToResponses_ImageURL(t *testing.T) {
	content := `[{"type":"text","text":"Describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc123"}}]`
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(content)},
		},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 1)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &parts))
	require.Len(t, parts, 2)
	assert.Equal(t, "input_text", parts[0].Type)
	assert.Equal(t, "Describe this", parts[0].Text)
	assert.Equal(t, "input_image", parts[1].Type)
	assert.Equal(t, "data:image/png;base64,abc123", parts[1].ImageURL)
}

func TestChatCompletionsToResponses_LegacyFunctions(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Hi"`)},
		},
		Functions: []ChatFunction{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
		FunctionCall: json.RawMessage(`{"name":"get_weather"}`),
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	require.Len(t, resp.Tools, 1)
	assert.Equal(t, "function", resp.Tools[0].Type)
	assert.Equal(t, "get_weather", resp.Tools[0].Name)

	// tool_choice should be converted
	require.NotNil(t, resp.ToolChoice)
	var tc map[string]any
	require.NoError(t, json.Unmarshal(resp.ToolChoice, &tc))
	assert.Equal(t, "function", tc["type"])
}

func TestChatCompletionsToResponses_ServiceTier(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model:       "gpt-4o",
		ServiceTier: "flex",
		Messages:    []ChatMessage{{Role: "user", Content: json.RawMessage(`"Hi"`)}},
	}
	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)
	assert.Equal(t, "flex", resp.ServiceTier)
}

func TestChatCompletionsToResponses_AssistantWithTextAndToolCalls(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Do something"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`"Let me call a function."`),
				ToolCalls: []ChatToolCall{
					{
						ID:   "call_abc",
						Type: "function",
						Function: ChatFunctionCall{
							Name:      "do_thing",
							Arguments: `{}`,
						},
					},
				},
			},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	// user + assistant message (with text) + function_call
	require.Len(t, items, 3)
	assert.Equal(t, "user", items[0].Role)
	assert.Equal(t, "assistant", items[1].Role)
	assert.Equal(t, "function_call", items[2].Type)
}

// ---------------------------------------------------------------------------
// ResponsesToChatCompletions tests
// ---------------------------------------------------------------------------

func TestResponsesToChatCompletions_BasicText(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_123",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "Hello, world!"},
				},
			},
		},
		Usage: &ResponsesUsage{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	assert.Equal(t, "chat.completion", chat.Object)
	assert.Equal(t, "gpt-4o", chat.Model)
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "stop", chat.Choices[0].FinishReason)

	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	assert.Equal(t, "Hello, world!", content)

	require.NotNil(t, chat.Usage)
	assert.Equal(t, 10, chat.Usage.PromptTokens)
	assert.Equal(t, 5, chat.Usage.CompletionTokens)
	assert.Equal(t, 15, chat.Usage.TotalTokens)
}

func TestResponsesToChatCompletions_ToolCalls(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_456",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type:      "function_call",
				CallID:    "call_xyz",
				Name:      "get_weather",
				Arguments: `{"city":"NYC"}`,
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "tool_calls", chat.Choices[0].FinishReason)

	msg := chat.Choices[0].Message
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "call_xyz", msg.ToolCalls[0].ID)
	assert.Equal(t, "function", msg.ToolCalls[0].Type)
	assert.Equal(t, "get_weather", msg.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"city":"NYC"}`, msg.ToolCalls[0].Function.Arguments)
}

func TestResponsesToChatCompletions_Reasoning(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_789",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type: "reasoning",
				Summary: []ResponsesSummary{
					{Type: "summary_text", Text: "I thought about it."},
				},
			},
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "The answer is 42."},
				},
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)

	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	// Reasoning summary is prepended to text
	assert.Equal(t, "I thought about it.The answer is 42.", content)
}

func TestResponsesToChatCompletions_Incomplete(t *testing.T) {
	resp := &ResponsesResponse{
		ID:                "resp_inc",
		Status:            "incomplete",
		IncompleteDetails: &ResponsesIncompleteDetails{Reason: "max_output_tokens"},
		Output: []ResponsesOutput{
			{
				Type: "message",
				Content: []ResponsesContentPart{
					{Type: "output_text", Text: "partial..."},
				},
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "length", chat.Choices[0].FinishReason)
}

func TestResponsesToChatCompletions_CachedTokens(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_cache",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type:    "message",
				Content: []ResponsesContentPart{{Type: "output_text", Text: "cached"}},
			},
		},
		Usage: &ResponsesUsage{
			InputTokens:  100,
			OutputTokens: 10,
			TotalTokens:  110,
			InputTokensDetails: &ResponsesInputTokensDetails{
				CachedTokens: 80,
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.NotNil(t, chat.Usage)
	require.NotNil(t, chat.Usage.PromptTokensDetails)
	assert.Equal(t, 80, chat.Usage.PromptTokensDetails.CachedTokens)
}

func TestResponsesToChatCompletions_WebSearch(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_ws",
		Status: "completed",
		Output: []ResponsesOutput{
			{
				Type:   "web_search_call",
				Action: &WebSearchAction{Type: "search", Query: "test"},
			},
			{
				Type:    "message",
				Content: []ResponsesContentPart{{Type: "output_text", Text: "search results"}},
			},
		},
	}

	chat := ResponsesToChatCompletions(resp, "gpt-4o")
	require.Len(t, chat.Choices, 1)
	assert.Equal(t, "stop", chat.Choices[0].FinishReason)

	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	assert.Equal(t, "search results", content)
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesEventToChatChunks tests
// ---------------------------------------------------------------------------

func TestResponsesEventToChatChunks_TextDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"

	// response.created → role chunk
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.created",
		Response: &ResponsesResponse{
			ID: "resp_stream",
		},
	}, state)
	require.Len(t, chunks, 1)
	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)
	assert.True(t, state.SentRole)

	// response.output_text.delta → content chunk
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.output_text.delta",
		Delta: "Hello",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.Content)
	assert.Equal(t, "Hello", *chunks[0].Choices[0].Delta.Content)
}

func TestResponsesEventToChatChunks_ToolCallDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SentRole = true

	// response.output_item.added (function_call) — output_index=1 (e.g. after a message item at 0)
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 1,
		Item: &ResponsesOutput{
			Type:   "function_call",
			CallID: "call_1",
			Name:   "get_weather",
		},
	}, state)
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Choices[0].Delta.ToolCalls, 1)
	tc := chunks[0].Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "get_weather", tc.Function.Name)
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index)

	// response.function_call_arguments.delta — uses output_index (NOT call_id) to find tool
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 1, // matches the output_index from output_item.added above
		Delta:       `{"city":`,
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index, "argument delta must use same index as the tool call")
	assert.Equal(t, `{"city":`, tc.Function.Arguments)

	// Add a second function call at output_index=2
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 2,
		Item: &ResponsesOutput{
			Type:   "function_call",
			CallID: "call_2",
			Name:   "get_time",
		},
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 1, *tc.Index, "second tool call should get index 1")

	// Argument delta for second tool call
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 2,
		Delta:       `{"tz":"UTC"}`,
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 1, *tc.Index, "second tool arg delta must use index 1")

	// Argument delta for first tool call (interleaved)
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 1,
		Delta:       `"Tokyo"}`,
	}, state)
	require.Len(t, chunks, 1)
	tc = chunks[0].Choices[0].Delta.ToolCalls[0]
	require.NotNil(t, tc.Index)
	assert.Equal(t, 0, *tc.Index, "first tool arg delta must still use index 0")
}

func TestResponsesEventToChatChunks_Completed(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:  50,
				OutputTokens: 20,
				TotalTokens:  70,
				InputTokensDetails: &ResponsesInputTokensDetails{
					CachedTokens: 30,
				},
			},
		},
	}, state)
	// finish chunk + usage chunk
	require.Len(t, chunks, 2)

	// First chunk: finish_reason
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[0].Choices[0].FinishReason)

	// Second chunk: usage
	require.NotNil(t, chunks[1].Usage)
	assert.Equal(t, 50, chunks[1].Usage.PromptTokens)
	assert.Equal(t, 20, chunks[1].Usage.CompletionTokens)
	assert.Equal(t, 70, chunks[1].Usage.TotalTokens)
	require.NotNil(t, chunks[1].Usage.PromptTokensDetails)
	assert.Equal(t, 30, chunks[1].Usage.PromptTokensDetails.CachedTokens)
}

func TestResponsesEventToChatChunks_CompletedWithToolCalls(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SawToolCall = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
		},
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "tool_calls", *chunks[0].Choices[0].FinishReason)
}

func TestResponsesEventToChatChunks_ReasoningDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.SentRole = true

	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.reasoning_summary_text.delta",
		Delta: "Thinking...",
	}, state)
	require.Len(t, chunks, 1)
	require.NotNil(t, chunks[0].Choices[0].Delta.Content)
	assert.Equal(t, "Thinking...", *chunks[0].Choices[0].Delta.Content)
}

func TestFinalizeResponsesChatStream(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true
	state.Usage = &ChatUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	chunks := FinalizeResponsesChatStream(state)
	require.Len(t, chunks, 2)

	// Finish chunk
	require.NotNil(t, chunks[0].Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunks[0].Choices[0].FinishReason)

	// Usage chunk
	require.NotNil(t, chunks[1].Usage)
	assert.Equal(t, 100, chunks[1].Usage.PromptTokens)

	// Idempotent: second call returns nil
	assert.Nil(t, FinalizeResponsesChatStream(state))
}

func TestFinalizeResponsesChatStream_AfterCompleted(t *testing.T) {
	// If response.completed already emitted the finish chunk, FinalizeResponsesChatStream
	// must be a no-op (prevents double finish_reason being sent to the client).
	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	// Simulate response.completed
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
		},
	}, state)
	require.NotEmpty(t, chunks) // finish + usage chunks

	// Now FinalizeResponsesChatStream should return nil — already finalized.
	assert.Nil(t, FinalizeResponsesChatStream(state))
}

func TestChatChunkToSSE(t *testing.T) {
	chunk := ChatCompletionsChunk{
		ID:      "chatcmpl-test",
		Object:  "chat.completion.chunk",
		Created: 1700000000,
		Model:   "gpt-4o",
		Choices: []ChatChunkChoice{
			{
				Index:        0,
				Delta:        ChatDelta{Role: "assistant"},
				FinishReason: nil,
			},
		},
	}

	sse, err := ChatChunkToSSE(chunk)
	require.NoError(t, err)
	assert.Contains(t, sse, "data: ")
	assert.Contains(t, sse, "chatcmpl-test")
	assert.Contains(t, sse, "assistant")
	assert.True(t, len(sse) > 10)
}

// ---------------------------------------------------------------------------
// Stream round-trip test
// ---------------------------------------------------------------------------

func TestChatCompletionsStreamRoundTrip(t *testing.T) {
	// Simulate: client sends chat completions request, upstream returns Responses SSE events.
	// Verify that the streaming state machine produces correct chat completions chunks.

	state := NewResponsesEventToChatState()
	state.Model = "gpt-4o"
	state.IncludeUsage = true

	var allChunks []ChatCompletionsChunk

	// 1. response.created
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_rt"},
	}, state)
	allChunks = append(allChunks, chunks...)

	// 2. text deltas
	for _, text := range []string{"Hello", ", ", "world", "!"} {
		chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
			Type:  "response.output_text.delta",
			Delta: text,
		}, state)
		allChunks = append(allChunks, chunks...)
	}

	// 3. response.completed
	chunks = ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:  10,
				OutputTokens: 4,
				TotalTokens:  14,
			},
		},
	}, state)
	allChunks = append(allChunks, chunks...)

	// Verify: role chunk + 4 text chunks + finish chunk + usage chunk = 7
	require.Len(t, allChunks, 7)

	// First chunk has role
	assert.Equal(t, "assistant", allChunks[0].Choices[0].Delta.Role)

	// Text chunks
	var fullText string
	for i := 1; i <= 4; i++ {
		require.NotNil(t, allChunks[i].Choices[0].Delta.Content)
		fullText += *allChunks[i].Choices[0].Delta.Content
	}
	assert.Equal(t, "Hello, world!", fullText)

	// Finish chunk
	require.NotNil(t, allChunks[5].Choices[0].FinishReason)
	assert.Equal(t, "stop", *allChunks[5].Choices[0].FinishReason)

	// Usage chunk
	require.NotNil(t, allChunks[6].Usage)
	assert.Equal(t, 10, allChunks[6].Usage.PromptTokens)
	assert.Equal(t, 4, allChunks[6].Usage.CompletionTokens)

	// All chunks share the same ID
	for _, c := range allChunks {
		assert.Equal(t, "resp_rt", c.ID)
	}
}
