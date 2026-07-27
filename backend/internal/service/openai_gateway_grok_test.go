package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type grokConditionalStateRepoStub struct {
	AccountRepository
	updated      bool
	calls        int
	lastUntil    time.Time
	lastSnapshot GrokCredentialMutationSnapshot
}

func (r *grokConditionalStateRepoStub) SetGrokCredentialTempUnschedulableIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	until time.Time,
	_ string,
) (bool, error) {
	r.calls++
	r.lastSnapshot = snapshot
	r.lastUntil = until
	return r.updated, nil
}

func (r *grokConditionalStateRepoStub) SetGrokCredentialErrorIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	_ string,
) (bool, error) {
	r.calls++
	r.lastSnapshot = snapshot
	return r.updated, nil
}

func TestPatchGrokResponsesBodySanitizesComposerReasoningParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		upstreamModel string
		wantReasoning bool
	}{
		{name: "composer fast", upstreamModel: "grok-composer-2.5-fast"},
		{name: "composer shorthand", upstreamModel: "grok-composer"},
		{name: "composer legacy alias", upstreamModel: "composer-2.5"},
		{name: "provider-prefixed composer", upstreamModel: "xai/grok-composer-2.5-fast"},
		{name: "grok 4.5", upstreamModel: "grok-4.5", wantReasoning: true},
	}

	body := []byte(`{
		"model": "grok",
		"input": "hello",
		"reasoning": {"effort": "medium", "summary": "auto"},
		"reasoning_effort": "medium",
		"reasoningEffort": "medium"
	}`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patched, err := patchGrokResponsesBody(body, tt.upstreamModel)
			require.NoError(t, err)
			require.True(t, json.Valid(patched))
			require.Equal(t, tt.upstreamModel, gjson.GetBytes(patched, "model").String())

			if tt.wantReasoning {
				require.Equal(t, "medium", gjson.GetBytes(patched, "reasoning.effort").String())
				require.Equal(t, "medium", gjson.GetBytes(patched, "reasoning_effort").String())
				require.Equal(t, "medium", gjson.GetBytes(patched, "reasoningEffort").String())
				return
			}

			require.False(t, gjson.GetBytes(patched, "reasoning").Exists())
			require.False(t, gjson.GetBytes(patched, "reasoning_effort").Exists())
			require.False(t, gjson.GetBytes(patched, "reasoningEffort").Exists())
		})
	}
}

func TestApplyGrokCLIHeadersSetsInteractiveClientMode(t *testing.T) {
	headers := http.Header{}

	applyGrokCLIHeaders(headers)

	require.Equal(t, "interactive", headers.Get("X-Grok-Client-Mode"))
}

func TestGrokUpstreamErrorFailoverPolicy(t *testing.T) {
	svc := &OpenAIGatewayService{}

	tests := []struct {
		name       string
		statusCode int
		body       []byte
		wantPolicy bool
		wantFail   bool
	}{
		{
			name:       "content policy 403 does not fail over",
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"code":"content_policy_violation","message":"prompt violates content policy"}}`),
			wantPolicy: true,
			wantFail:   false,
		},
		{
			name:       "ordinary 403 fails over",
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"code":"permission_denied","message":"subscription required"}}`),
			wantPolicy: false,
			wantFail:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantPolicy, isGrokContentPolicyRejection(tt.statusCode, tt.body))
			require.Equal(t, tt.wantFail, svc.shouldFailoverGrokUpstreamError(tt.statusCode, tt.body))
		})
	}
}

func TestHandleGrokAccountUpstreamErrorPoolMode502Scheduling(t *testing.T) {
	tests := []struct {
		name                string
		poolMode            bool
		wantTempUnschedable bool
	}{
		{name: "pool account remains schedulable", poolMode: true, wantTempUnschedable: false},
		{name: "non-pool account is temporarily unschedulable", poolMode: false, wantTempUnschedable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				ID:       901,
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"pool_mode": tt.poolMode,
				},
			}
			svc := &OpenAIGatewayService{}

			svc.handleGrokAccountUpstreamError(
				context.Background(),
				account,
				http.StatusBadGateway,
				nil,
				[]byte(`{"error":{"message":"temporary upstream failure"}}`),
			)

			require.Equal(t, tt.wantTempUnschedable, svc.isOpenAIAccountRuntimeBlocked(account))
		})
	}
}

func TestHandleGrokAccountUpstreamErrorDoesNotBlockAfterCredentialCASMiss(t *testing.T) {
	account := &Account{
		ID:       902,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":   "stale-access",
			"refresh_token":  "stale-refresh",
			"_token_version": int64(100),
		},
	}
	repo := &grokConditionalStateRepoStub{updated: false}
	svc := &OpenAIGatewayService{accountRepo: repo}

	svc.handleGrokAccountUpstreamError(
		context.Background(),
		account,
		http.StatusUnauthorized,
		nil,
		[]byte(`{"error":{"message":"unauthorized"}}`),
	)

	require.Equal(t, 1, repo.calls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.JSONEq(t, `{"access_token":"stale-access","refresh_token":"stale-refresh","_token_version":100}`, repo.lastSnapshot.CredentialsJSON)
}

func TestHandleGrokAccountUpstreamErrorPaymentRequiredUsesThirtyMinuteCooldown(t *testing.T) {
	account := &Account{
		ID:       903,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh",
		},
	}
	repo := &grokConditionalStateRepoStub{updated: true}
	svc := &OpenAIGatewayService{accountRepo: repo}
	startedAt := time.Now()

	svc.handleGrokAccountUpstreamError(
		context.Background(),
		account,
		http.StatusPaymentRequired,
		nil,
		[]byte(`{"error":{"message":"payment required"}}`),
	)

	require.Equal(t, 1, repo.calls)
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.WithinDuration(t, startedAt.Add(30*time.Minute), repo.lastUntil, 2*time.Second)
}

func TestForwardGrokResponsesAPIKeyUsesConfiguredXAIEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"grok","input":"hi","stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_grok_api_key","model":"grok-4.5","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`,
		)),
	}}
	account := &Account{
		ID:          53,
		Name:        "grok-api-key",
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{
			"api_key":  "xai-test-key",
			"base_url": "https://xai.example.com/v1",
		},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}

	result, err := svc.forwardGrokResponses(context.Background(), c, account, body, "grok", false, time.Now())

	require.NoError(t, err)
	require.Equal(t, "https://xai.example.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer xai-test-key", upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("X-Grok-Client-Version"), "API-key requests must not impersonate Grok CLI OAuth")
	require.Equal(t, "grok-4.5", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
}

func TestAccountTestServiceGrokAPIKeyUsesResponsesEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
		)),
	}}
	account := &Account{
		ID:          54,
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "xai-test-key",
			"base_url": "https://xai.example.com/v1",
		},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/54/test", nil)

	err := svc.testGrokAccountConnection(c, account, "grok", "hi")

	require.NoError(t, err)
	require.Equal(t, "https://xai.example.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer xai-test-key", upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	require.Contains(t, recorder.Body.String(), `"type":"test_complete"`)
}

func TestForwardAsAnthropicForGrokUsesResponsesCacheRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"grok","max_tokens":32,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Set("api_key", &APIKey{ID: 5401})

	upstreamBody := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_grok_messages","object":"response","model":"grok-4.5","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7,"input_tokens_details":{"cached_tokens":3}}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	account := &Account{
		ID:          55,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, grokCLIVersion, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	identity := gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String()
	require.NotEmpty(t, identity)
	require.Equal(t, identity, upstream.lastReq.Header.Get(grokConversationIDHeader))
	require.Equal(t, "web_search", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
	require.Equal(t, "x_search", gjson.GetBytes(upstream.lastBody, "tools.1.type").String())
	require.Equal(t, "none", gjson.GetBytes(upstream.lastBody, "tool_choice").String())
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, 3, result.Usage.CacheReadInputTokens)
	require.Contains(t, recorder.Body.String(), "ok")
}
