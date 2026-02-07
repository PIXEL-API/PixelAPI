package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStripSignatureSensitiveBlocksFromClaudeRequest(t *testing.T) {
	req := &antigravity.ClaudeRequest{
		Model: "claude-sonnet-4-5",
		Thinking: &antigravity.ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 1024,
		},
		Messages: []antigravity.ClaudeMessage{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"secret plan","signature":""},
					{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}
				]`),
			},
			{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"tool_result","tool_use_id":"t1","content":"ok","is_error":false},
					{"type":"redacted_thinking","data":"..."}
				]`),
			},
		},
	}

	changed, err := stripSignatureSensitiveBlocksFromClaudeRequest(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Nil(t, req.Thinking)

	require.Len(t, req.Messages, 2)

	var blocks0 []map[string]any
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &blocks0))
	require.Len(t, blocks0, 2)
	require.Equal(t, "text", blocks0[0]["type"])
	require.Equal(t, "secret plan", blocks0[0]["text"])
	require.Equal(t, "text", blocks0[1]["type"])

	var blocks1 []map[string]any
	require.NoError(t, json.Unmarshal(req.Messages[1].Content, &blocks1))
	require.Len(t, blocks1, 1)
	require.Equal(t, "text", blocks1[0]["type"])
	require.NotEmpty(t, blocks1[0]["text"])
}

func TestStripThinkingFromClaudeRequest_DoesNotDowngradeTools(t *testing.T) {
	req := &antigravity.ClaudeRequest{
		Model: "claude-sonnet-4-5",
		Thinking: &antigravity.ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 1024,
		},
		Messages: []antigravity.ClaudeMessage{
			{
				Role:    "assistant",
				Content: json.RawMessage(`[{"type":"thinking","thinking":"secret plan"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]`),
			},
		},
	}

	changed, err := stripThinkingFromClaudeRequest(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Nil(t, req.Thinking)

	var blocks []map[string]any
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &blocks))
	require.Len(t, blocks, 2)
	require.Equal(t, "text", blocks[0]["type"])
	require.Equal(t, "secret plan", blocks[0]["text"])
	require.Equal(t, "tool_use", blocks[1]["type"])
}

func TestIsPromptTooLongError(t *testing.T) {
	require.True(t, isPromptTooLongError([]byte(`{"error":{"message":"Prompt is too long"}}`)))
	require.True(t, isPromptTooLongError([]byte(`{"message":"Prompt is too long"}`)))
	require.False(t, isPromptTooLongError([]byte(`{"error":{"message":"other"}}`)))
}

type httpUpstreamStub struct {
	resp *http.Response
	err  error
}

func (s *httpUpstreamStub) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return s.resp, s.err
}

func (s *httpUpstreamStub) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ bool) (*http.Response, error) {
	return s.resp, s.err
}

func TestAntigravityGatewayService_Forward_PromptTooLong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model": "claude-opus-4-6",
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
		"stream":     false,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	respBody := []byte(`{"error":{"message":"Prompt is too long"}}`)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"X-Request-Id": []string{"req-1"}},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	svc := &AntigravityGatewayService{
		tokenProvider: &AntigravityTokenProvider{},
		httpUpstream:  &httpUpstreamStub{resp: resp},
	}

	account := &Account{
		ID:          1,
		Name:        "acc-1",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "token",
		},
	}

	result, err := svc.Forward(context.Background(), c, account, body, false)
	require.Nil(t, result)

	var promptErr *PromptTooLongError
	require.ErrorAs(t, err, &promptErr)
	require.Equal(t, http.StatusBadRequest, promptErr.StatusCode)
	require.Equal(t, "req-1", promptErr.RequestID)
	require.NotEmpty(t, promptErr.Body)

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "prompt_too_long", events[0].Kind)
}

// TestAntigravityGatewayService_Forward_ModelRateLimitTriggersFailover
// 验证：当账号存在模型限流且剩余时间 >= antigravityRateLimitThreshold 时，
// Forward 方法应返回 UpstreamFailoverError，触发 Handler 切换账号
func TestAntigravityGatewayService_Forward_ModelRateLimitTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model": "claude-opus-4-6",
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
		"stream":     false,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	// 不需要真正调用上游，因为预检查会直接返回切换信号
	svc := &AntigravityGatewayService{
		tokenProvider: &AntigravityTokenProvider{},
		httpUpstream:  &httpUpstreamStub{resp: nil, err: nil},
	}

	// 设置模型限流：剩余时间 30 秒（> antigravityRateLimitThreshold 7s）
	futureResetAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	account := &Account{
		ID:          1,
		Name:        "acc-rate-limited",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "token",
		},
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"claude-opus-4-6-thinking": map[string]any{
					"rate_limit_reset_at": futureResetAt,
				},
			},
		},
	}

	result, err := svc.Forward(context.Background(), c, account, body, false)
	require.Nil(t, result, "Forward should not return result when model rate limited")
	require.NotNil(t, err, "Forward should return error")

	// 核心验证：错误应该是 UpstreamFailoverError，而不是普通 502 错误
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "error should be UpstreamFailoverError to trigger account switch")
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	// 非粘性会话请求，ForceCacheBilling 应为 false
	require.False(t, failoverErr.ForceCacheBilling, "ForceCacheBilling should be false for non-sticky session")
}

// TestAntigravityGatewayService_ForwardGemini_ModelRateLimitTriggersFailover
// 验证：ForwardGemini 方法同样能正确将 AntigravityAccountSwitchError 转换为 UpstreamFailoverError
func TestAntigravityGatewayService_ForwardGemini_ModelRateLimitTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "hi"}}},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", bytes.NewReader(body))
	c.Request = req

	// 不需要真正调用上游，因为预检查会直接返回切换信号
	svc := &AntigravityGatewayService{
		tokenProvider: &AntigravityTokenProvider{},
		httpUpstream:  &httpUpstreamStub{resp: nil, err: nil},
	}

	// 设置模型限流：剩余时间 30 秒（> antigravityRateLimitThreshold 7s）
	futureResetAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	account := &Account{
		ID:          2,
		Name:        "acc-gemini-rate-limited",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "token",
		},
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"gemini-2.5-flash": map[string]any{
					"rate_limit_reset_at": futureResetAt,
				},
			},
		},
	}

	result, err := svc.ForwardGemini(context.Background(), c, account, "gemini-2.5-flash", "generateContent", false, body, false)
	require.Nil(t, result, "ForwardGemini should not return result when model rate limited")
	require.NotNil(t, err, "ForwardGemini should return error")

	// 核心验证：错误应该是 UpstreamFailoverError，而不是普通 502 错误
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "error should be UpstreamFailoverError to trigger account switch")
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	// 非粘性会话请求，ForceCacheBilling 应为 false
	require.False(t, failoverErr.ForceCacheBilling, "ForceCacheBilling should be false for non-sticky session")
}

// TestAntigravityGatewayService_Forward_StickySessionForceCacheBilling
// 验证：粘性会话切换时，UpstreamFailoverError.ForceCacheBilling 应为 true
func TestAntigravityGatewayService_Forward_StickySessionForceCacheBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model":    "claude-opus-4-6",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	svc := &AntigravityGatewayService{
		tokenProvider: &AntigravityTokenProvider{},
		httpUpstream:  &httpUpstreamStub{resp: nil, err: nil},
	}

	// 设置模型限流：剩余时间 30 秒（> antigravityRateLimitThreshold 7s）
	futureResetAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	account := &Account{
		ID:          3,
		Name:        "acc-sticky-rate-limited",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "token",
		},
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"claude-opus-4-6-thinking": map[string]any{
					"rate_limit_reset_at": futureResetAt,
				},
			},
		},
	}

	// 传入 isStickySession = true
	result, err := svc.Forward(context.Background(), c, account, body, true)
	require.Nil(t, result, "Forward should not return result when model rate limited")
	require.NotNil(t, err, "Forward should return error")

	// 核心验证：粘性会话切换时，ForceCacheBilling 应为 true
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "error should be UpstreamFailoverError to trigger account switch")
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.True(t, failoverErr.ForceCacheBilling, "ForceCacheBilling should be true for sticky session switch")
}

// TestAntigravityGatewayService_ForwardGemini_StickySessionForceCacheBilling
// 验证：ForwardGemini 粘性会话切换时，UpstreamFailoverError.ForceCacheBilling 应为 true
func TestAntigravityGatewayService_ForwardGemini_StickySessionForceCacheBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": "hi"}}},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", bytes.NewReader(body))
	c.Request = req

	svc := &AntigravityGatewayService{
		tokenProvider: &AntigravityTokenProvider{},
		httpUpstream:  &httpUpstreamStub{resp: nil, err: nil},
	}

	// 设置模型限流：剩余时间 30 秒（> antigravityRateLimitThreshold 7s）
	futureResetAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	account := &Account{
		ID:          4,
		Name:        "acc-gemini-sticky-rate-limited",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "token",
		},
		Extra: map[string]any{
			modelRateLimitsKey: map[string]any{
				"gemini-2.5-flash": map[string]any{
					"rate_limit_reset_at": futureResetAt,
				},
			},
		},
	}

	// 传入 isStickySession = true
	result, err := svc.ForwardGemini(context.Background(), c, account, "gemini-2.5-flash", "generateContent", false, body, true)
	require.Nil(t, result, "ForwardGemini should not return result when model rate limited")
	require.NotNil(t, err, "ForwardGemini should return error")

	// 核心验证：粘性会话切换时，ForceCacheBilling 应为 true
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "error should be UpstreamFailoverError to trigger account switch")
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.True(t, failoverErr.ForceCacheBilling, "ForceCacheBilling should be true for sticky session switch")
}
