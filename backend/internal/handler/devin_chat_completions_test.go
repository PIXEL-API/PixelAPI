package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	devinpkg "github.com/Wei-Shaw/sub2api/internal/pkg/devin"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestParseDevinChatRequest(t *testing.T) {
	body := []byte(`{
		"model": "swe-2-max",
		"stream": true,
		"max_tokens": 256,
		"temperature": 0.3,
		"stop": ["\nDONE", "STOP"],
		"messages": [
			{"role": "system", "content": "be terse"},
			{"role": "user", "content": [{"type": "text", "text": "hi"}]},
			{"role": "assistant", "content": "ok", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "f", "arguments": "{\"a\":1}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "done"}
		],
		"tools": [{"type": "function", "function": {"name": "f", "description": "d", "parameters": {"type": "object"}}}],
		"tool_choice": "auto",
		"parallel_tool_calls": false
	}`)
	req, model, stream, err := parseDevinChatRequest(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if model != "swe-2-max" || !stream {
		t.Fatalf("model=%q stream=%v", model, stream)
	}
	if req.SystemPrompt != "be terse" {
		t.Fatalf("system = %q", req.SystemPrompt)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d", len(req.Messages))
	}
	assistant := req.Messages[1]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_1" || assistant.ToolCalls[0].Name != "f" {
		t.Fatalf("tool calls = %+v", assistant.ToolCalls)
	}
	if req.Messages[2].ToolCallID != "call_1" {
		t.Fatalf("tool message = %+v", req.Messages[2])
	}
	if req.MaxTokens == nil || *req.MaxTokens != 256 {
		t.Fatalf("max tokens = %v", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.3 {
		t.Fatalf("temperature = %v", req.Temperature)
	}
	if len(req.StopSequences) != 2 {
		t.Fatalf("stop = %v", req.StopSequences)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "f" {
		t.Fatalf("tools = %+v", req.Tools)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != devinpkg.ToolChoiceAuto {
		t.Fatalf("tool choice = %+v", req.ToolChoice)
	}
	if !req.DisableParallelToolCalls {
		t.Fatal("parallel_tool_calls=false must map to DisableParallelToolCalls")
	}
}

func TestParseDevinChatRequestToolChoiceVariants(t *testing.T) {
	base := `{"model":"m","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"f"}}],"tool_choice":%s}`
	for _, tc := range []struct {
		raw  string
		mode string
		name string
	}{
		{`"none"`, devinpkg.ToolChoiceNone, ""},
		{`"required"`, devinpkg.ToolChoiceRequired, ""},
		{`{"type":"function","function":{"name":"f"}}`, devinpkg.ToolChoiceNamed, "f"},
	} {
		req, _, _, err := parseDevinChatRequest([]byte(fmt.Sprintf(base, tc.raw)))
		if err != nil {
			t.Fatalf("tool_choice %s: %v", tc.raw, err)
		}
		if req.ToolChoice == nil || req.ToolChoice.Mode != tc.mode || req.ToolChoice.ToolName != tc.name {
			t.Fatalf("tool_choice %s = %+v", tc.raw, req.ToolChoice)
		}
	}
	// 不支持的取值要 fail fast。
	if _, _, _, err := parseDevinChatRequest([]byte(fmt.Sprintf(base, `"bogus"`))); err == nil {
		t.Fatal("unsupported tool_choice must be rejected")
	}
}

func TestParseDevinChatRequestErrors(t *testing.T) {
	cases := map[string]string{
		"no messages":   `{"model":"m"}`,
		"bad role":      `{"model":"m","messages":[{"role":"function","content":"x"}]}`,
		"bad tool type": `{"model":"m","messages":[{"role":"user","content":"x"}],"tools":[{"type":"retrieval","function":{"name":"f"}}]}`,
		"remote image":  `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`,
	}
	for name, body := range cases {
		if _, _, _, err := parseDevinChatRequest([]byte(body)); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestParseDevinChatRequestMaxCompletionTokensPriority(t *testing.T) {
	req, _, _, err := parseDevinChatRequest([]byte(`{
		"model":"m","messages":[{"role":"user","content":"x"}],
		"max_tokens":100,"max_completion_tokens":42}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 42 {
		t.Fatalf("max_completion_tokens must win, got %v", req.MaxTokens)
	}
}

func TestFillDevinUsage(t *testing.T) {
	result := &service.OpenAIForwardResult{}
	fillDevinUsage(result, devinpkg.Usage{
		InputTokens:       100,
		OutputTokens:      20,
		CacheReadTokens:   30,
		CacheWriteTokens:  10,
		BillingModelUID:   "billing-uid",
		ResponseID:        "resp-1",
		UpstreamRequestID: "req-1",
		ResponseModel:     "actual-uid",
	})
	if result.Usage.InputTokens != 140 {
		t.Fatalf("input tokens = %d, want 140 (input+cache_read+cache_write)", result.Usage.InputTokens)
	}
	if result.Usage.CacheReadInputTokens != 30 || result.Usage.CacheCreationInputTokens != 10 {
		t.Fatalf("cache usage = %+v", result.Usage)
	}
	if result.BillingModel != "billing-uid" || result.ResponseID != "resp-1" ||
		result.RequestID != "req-1" || result.UpstreamResponseModel != "actual-uid" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDevinUsageJSON(t *testing.T) {
	usage := devinUsageJSON(&devinpkg.Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 10})
	if usage["prompt_tokens"] != int64(140) && usage["prompt_tokens"] != 140 {
		t.Fatalf("prompt_tokens = %v", usage["prompt_tokens"])
	}
	details, ok := usage["prompt_tokens_details"].(map[string]any)
	if !ok || details["cached_tokens"] != int64(30) && details["cached_tokens"] != 30 {
		t.Fatalf("cached tokens = %v", usage)
	}
	empty := devinUsageJSON(&devinpkg.Usage{InputTokens: 5, OutputTokens: 1})
	if _, ok := empty["prompt_tokens_details"]; ok {
		t.Fatal("zero cache must not emit prompt_tokens_details")
	}
}

func TestDevinFailoverErrorClassification(t *testing.T) {
	h := &OpenAIGatewayHandler{}

	// 凭证失效 → 换账号 + 标记账号。
	err := h.devinFailoverError(connect.NewError(connect.CodeUnauthenticated, errors.New("bad token")), false)
	var failover *service.UpstreamFailoverError
	if !errors.As(err, &failover) {
		t.Fatalf("expected UpstreamFailoverError, got %v", err)
	}
	if failover.Stage != service.GatewayFailureStageAccountAuth || failover.Scope != service.GatewayFailureScopeAccount {
		t.Fatalf("credential failure = %+v", failover)
	}

	// 请求形状错误 → 透传客户端，不换账号。
	err = h.devinFailoverError(connect.NewError(connect.CodeInvalidArgument, errors.New("bad field")), false)
	if !errors.As(err, &failover) {
		t.Fatalf("expected UpstreamFailoverError, got %v", err)
	}
	if failover.NextAccountAction != service.NextAccountStop || failover.ClientStatusCode != http.StatusBadRequest {
		t.Fatalf("client fault = %+v", failover)
	}

	// 流已开始 → 禁止换号。
	err = h.devinFailoverError(connect.NewError(connect.CodeUnavailable, errors.New("stream reset")), true)
	if !errors.As(err, &failover) {
		t.Fatalf("expected UpstreamFailoverError, got %v", err)
	}
	if failover.NextAccountAction != service.NextAccountStop {
		t.Fatalf("mid-stream failure must not switch accounts: %+v", failover)
	}
}
