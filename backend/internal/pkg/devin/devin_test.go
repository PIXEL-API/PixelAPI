//go:build unit

package devin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"connectrpc.com/connect"
)

func TestDeriveSessionIDs(t *testing.T) {
	base := &ChatRequest{
		Messages: []ChatMessage{{Role: "user", Text: "hello"}},
	}

	// SessionKey 做种：同 key 稳定，异 key 不同。
	withKey := &ChatRequest{SessionKey: "sess-abc", Messages: base.Messages}
	t1, c1 := DeriveSessionIDs(withKey)
	t2, c2 := DeriveSessionIDs(&ChatRequest{SessionKey: "sess-abc", Messages: base.Messages})
	if t1 != t2 || c1 != c2 {
		t.Fatal("same session key must derive identical ids")
	}
	t3, c3 := DeriveSessionIDs(&ChatRequest{SessionKey: "sess-xyz", Messages: base.Messages})
	if t1 == t3 || c1 == c3 {
		t.Fatal("different session keys must derive different ids")
	}
	if c1 == t1 {
		t.Fatal("trajectory id and cascade id must differ")
	}

	// 无 SessionKey 时由内容派生：同前缀内容稳定。
	a1, a2 := DeriveSessionIDs(base)
	b1, b2 := DeriveSessionIDs(&ChatRequest{Messages: []ChatMessage{{Role: "user", Text: "hello"}}})
	if a1 != b1 || a2 != b2 {
		t.Fatal("content-derived ids must be stable for identical prefixes")
	}
	d1, _ := DeriveSessionIDs(&ChatRequest{Messages: []ChatMessage{{Role: "user", Text: "other"}}})
	if d1 == a1 {
		t.Fatal("different content must derive different trajectory ids")
	}
}

func TestClassifyConnectErrors(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		wantStatus      int
		wantRetryable   bool
		wantCredential  bool
		wantClientFault bool
	}{
		{"unauthenticated", connect.NewError(connect.CodeUnauthenticated, errors.New("bad token")), 401, false, true, false},
		{"permission denied", connect.NewError(connect.CodePermissionDenied, errors.New("denied")), 403, false, true, false},
		{"invalid argument", connect.NewError(connect.CodeInvalidArgument, errors.New("bad field")), 400, false, false, true},
		{"resource exhausted", connect.NewError(connect.CodeResourceExhausted, errors.New("rate limited")), 429, true, false, false},
		{"unavailable", connect.NewError(connect.CodeUnavailable, errors.New("upstream down")), 503, true, false, false},
		{"internal", connect.NewError(connect.CodeInternal, errors.New("boom")), 502, true, false, false},
		{"not found", connect.NewError(connect.CodeNotFound, errors.New("missing")), 404, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Classify(tt.err)
			if f == nil {
				t.Fatal("expected classified failure")
			}
			if f.StatusCode != tt.wantStatus || f.Retryable != tt.wantRetryable ||
				f.CredentialFailed != tt.wantCredential || f.ClientFault != tt.wantClientFault {
				t.Fatalf("failure = %+v", f)
			}
		})
	}
}

func TestClassifyPassthroughAndNil(t *testing.T) {
	if Classify(nil) != nil {
		t.Fatal("nil error must classify to nil")
	}
	sentinel := &Failure{StatusCode: 418, Code: "custom"}
	if Classify(sentinel) != sentinel {
		t.Fatal("existing *Failure must pass through unchanged")
	}
	if f := Classify(context.Canceled); f == nil || f.StatusCode != 499 {
		t.Fatalf("canceled = %+v", f)
	}
	if f := Classify(context.DeadlineExceeded); f == nil || f.StatusCode != 504 || !f.Retryable {
		t.Fatalf("deadline = %+v", f)
	}
	// EOF 落到未分类兜底：502 upstream_error（建流重试由 IsTransientConnectError
	// 判定，流中段失败由网关层按 NextAccountAction 默认换号）。
	if f := Classify(io.EOF); f == nil || f.StatusCode != 502 || f.Code != "upstream_error" {
		t.Fatalf("eof = %+v", f)
	}
}

func TestIsTransientConnectError(t *testing.T) {
	if IsTransientConnectError(context.Canceled) {
		t.Fatal("canceled is not transient")
	}
	if IsTransientConnectError(context.DeadlineExceeded) {
		t.Fatal("deadline is not transient")
	}
	if IsTransientConnectError(&Failure{StatusCode: 502}) {
		t.Fatal("classified failure is not transient")
	}
	if !IsTransientConnectError(io.EOF) || !IsTransientConnectError(io.ErrUnexpectedEOF) {
		t.Fatal("EOF must be transient")
	}
	if !IsTransientConnectError(connect.NewError(connect.CodeUnavailable, errors.New("http2: stream error: received RST_STREAM"))) {
		t.Fatal("http2 transport failure must be transient")
	}
	if IsTransientConnectError(connect.NewError(connect.CodeUnauthenticated, errors.New("bad token"))) {
		t.Fatal("semantic connect error is not transient")
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatal("empty token must fail")
	}
	client, err := NewClient(ClientConfig{Token: "devin-session-token$abc"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if client.cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("base url = %q, want %q", client.cfg.BaseURL, DefaultBaseURL)
	}
	client2, err := NewClient(ClientConfig{Token: "t", BaseURL: "https://example.com/"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client2.Close()
	if client2.cfg.BaseURL != "https://example.com" {
		t.Fatalf("trailing slash not trimmed: %q", client2.cfg.BaseURL)
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{Token: "devin-session-token$test"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestBuildRequestBasics(t *testing.T) {
	client := newTestClient(t)
	maxTokens := int64(512)
	temp := 0.5
	req, err := client.BuildRequest(&ChatRequest{
		Model:         "model-uid-1",
		SystemPrompt:  "be helpful",
		Messages:      []ChatMessage{{Role: "user", Text: "hi"}},
		MaxTokens:     &maxTokens,
		Temperature:   &temp,
		StopSequences: []string{"\n\nEND"},
		SessionKey:    "s1",
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.GetChatModelUid() != "model-uid-1" {
		t.Fatalf("model uid = %q", req.GetChatModelUid())
	}
	if req.GetPrompt() != "be helpful" {
		t.Fatalf("prompt = %q", req.GetPrompt())
	}
	if req.GetConfiguration().GetMaxTokens() != 512 {
		t.Fatalf("max tokens = %d", req.GetConfiguration().GetMaxTokens())
	}
	if got := req.GetConfiguration().GetTemperature(); got != 0.5 {
		t.Fatalf("temperature = %v", got)
	}
	if pats := req.GetConfiguration().GetStopPatterns(); len(pats) != 1 || pats[0] != "\n\nEND" {
		t.Fatalf("stop patterns = %v", pats)
	}
	if len(req.GetChatMessagePrompts()) == 0 {
		t.Fatal("expected chat message prompts")
	}
	if req.GetCascadeId() == "" || req.GetTrajectoryReference().GetTrajectoryId() == "" {
		t.Fatal("trajectory/cascade ids must be populated")
	}
	if req.GetMetadata().GetApiKey() != "devin-session-token$test" {
		t.Fatal("metadata api key must carry the session token")
	}
}

func TestBuildRequestToolChoice(t *testing.T) {
	client := newTestClient(t)
	tool := ToolDefinition{
		Name:       "get_weather",
		Parameters: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","description":"城市名"}}}`),
	}

	// tool_choice=none：工具声明与描述注入都必须不进 wire。
	req, err := client.BuildRequest(&ChatRequest{
		Model:      "m",
		Messages:   []ChatMessage{{Role: "user", Text: "hi"}},
		Tools:      []ToolDefinition{tool},
		ToolChoice: &ToolChoice{Mode: ToolChoiceNone},
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if len(req.GetTools()) != 0 {
		t.Fatalf("tool_choice=none must drop tool declarations, got %d", len(req.GetTools()))
	}
	if strings.Contains(req.GetPrompt(), "get_weather") {
		t.Fatal("tool descriptions must not be injected when tools are disabled")
	}

	// tool_choice=named 命中工具。
	req, err = client.BuildRequest(&ChatRequest{
		Model:      "m",
		Messages:   []ChatMessage{{Role: "user", Text: "hi"}},
		Tools:      []ToolDefinition{tool},
		ToolChoice: &ToolChoice{Mode: ToolChoiceNamed, ToolName: "get_weather"},
	})
	if err != nil {
		t.Fatalf("BuildRequest named: %v", err)
	}
	if got := req.GetToolChoice().GetToolName(); got != "get_weather" {
		t.Fatalf("tool choice = %q", got)
	}
	if len(req.GetTools()) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.GetTools()))
	}

	// named 未命中 → client fault。
	_, err = client.BuildRequest(&ChatRequest{
		Model:      "m",
		Messages:   []ChatMessage{{Role: "user", Text: "hi"}},
		Tools:      []ToolDefinition{tool},
		ToolChoice: &ToolChoice{Mode: ToolChoiceNamed, ToolName: "missing"},
	})
	failure := Classify(err)
	if failure == nil || !failure.ClientFault {
		t.Fatalf("named tool_choice miss must be a client fault, got %v", err)
	}
}

func TestSanitizeToolSchemaStripsAnnotations(t *testing.T) {
	out, err := sanitizeToolSchema(json.RawMessage(`{
		"type": "object",
		"title": "Params",
		"$comment": "generated",
		"properties": {
			"city": {"type": "string", "description": "城市"},
			"days": {"type": "integer"}
		},
		"required": ["city"]
	}`))
	if err != nil {
		t.Fatalf("sanitizeToolSchema: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("sanitized schema is not valid json: %v", err)
	}
	if _, ok := decoded["title"]; ok {
		t.Fatal("title annotation must be stripped")
	}
	if _, ok := decoded["$comment"]; ok {
		t.Fatal("$comment annotation must be stripped")
	}
	props, ok := decoded["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties must be preserved")
	}
	city, ok := props["city"].(map[string]any)
	if !ok {
		t.Fatal("city property must be preserved")
	}
	if _, ok := city["description"]; ok {
		t.Fatal("nested description must be stripped")
	}
	if city["type"] != "string" {
		t.Fatal("literal fields must be preserved")
	}
}

func TestWithToolDescriptions(t *testing.T) {
	injected, err := withToolDescriptions("sys", []ToolDefinition{{
		Name:        "get_weather",
		Description: "获取天气",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
	}})
	if err != nil {
		t.Fatalf("withToolDescriptions: %v", err)
	}
	if !strings.HasPrefix(injected, "sys") {
		t.Fatalf("system prompt must be preserved, got %q", injected)
	}
	if !strings.Contains(injected, "get_weather") {
		t.Fatal("tool name must be injected")
	}
}

func TestPairToolCallsWithResults(t *testing.T) {
	client := newTestClient(t)
	// 客户端历史常是「全部调用→全部结果」分组，上游要求 call→result 紧邻。
	req, err := client.BuildRequest(&ChatRequest{
		Model: "m",
		Messages: []ChatMessage{
			{Role: "user", Text: "call tools"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "c1", Name: "f1", Arguments: "{}"},
				{ID: "c2", Name: "f2", Arguments: "{}"},
			}},
			{Role: "tool", ToolCallID: "c1", Text: "r1"},
			{Role: "tool", ToolCallID: "c2", Text: "r2"},
			{Role: "user", Text: "continue"},
		},
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if len(req.GetChatMessagePrompts()) == 0 {
		t.Fatal("expected prompts")
	}
	// 至少验证配对重排没有丢消息：prompt 数 ≥ 消息轮数对应的最小 prompt 数。
	if len(req.GetChatMessagePrompts()) < 4 {
		t.Fatalf("expected paired call/result prompts, got %d", len(req.GetChatMessagePrompts()))
	}
}
