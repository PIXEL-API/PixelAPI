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
	errorCalls   int
	tempCalls    int
	lastUntil    time.Time
	lastReason   string
	lastSnapshot GrokCredentialMutationSnapshot
}

func (r *grokConditionalStateRepoStub) SetGrokCredentialTempUnschedulableIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	until time.Time,
	reason string,
) (bool, error) {
	r.calls++
	r.tempCalls++
	r.lastSnapshot = snapshot
	r.lastUntil = until
	r.lastReason = reason
	return r.updated, nil
}

func (r *grokConditionalStateRepoStub) SetGrokCredentialErrorIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	reason string,
) (bool, error) {
	r.calls++
	r.errorCalls++
	r.lastSnapshot = snapshot
	r.lastReason = reason
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
	require.Equal(t, 1, repo.tempCalls)
	require.Zero(t, repo.errorCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.JSONEq(t, `{"access_token":"stale-access","refresh_token":"stale-refresh","_token_version":100}`, repo.lastSnapshot.CredentialsJSON)
}

func TestHandleGrokAccountUpstreamErrorPaymentRequiredMarksAccountError(t *testing.T) {
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

	svc.handleGrokAccountUpstreamError(
		context.Background(),
		account,
		http.StatusPaymentRequired,
		nil,
		[]byte(`{"error":{"message":"payment required"}}`),
	)

	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, repo.errorCalls)
	require.Zero(t, repo.tempCalls)
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.True(t, repo.lastUntil.IsZero())
	require.Equal(t, grokPaymentRequiredErrorMessage, repo.lastReason)
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastSnapshot)
}

func TestHandleGrokPaymentRequiredDoesNotBlockAfterCredentialCASMiss(t *testing.T) {
	account := &Account{
		ID:       905,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":   "stale-access",
			"refresh_token":  "stale-refresh",
			"_token_version": int64(101),
		},
	}
	repo := &grokConditionalStateRepoStub{updated: false}
	svc := &OpenAIGatewayService{accountRepo: repo}

	svc.handleGrokAccountUpstreamError(
		context.Background(),
		account,
		http.StatusPaymentRequired,
		nil,
		[]byte(`{"error":{"message":"payment required"}}`),
	)

	require.Equal(t, 1, repo.errorCalls)
	require.Zero(t, repo.tempCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastSnapshot)
}

func TestHandleGrokAPIKeyPaymentRequiredMarksAccountError(t *testing.T) {
	account := &Account{
		ID:       904,
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "xai-test-key",
		},
	}
	repo := &grokConditionalStateRepoStub{updated: true}
	svc := &OpenAIGatewayService{accountRepo: repo}

	svc.handleGrokAccountUpstreamError(
		context.Background(),
		account,
		http.StatusPaymentRequired,
		nil,
		[]byte(`{"error":{"message":"payment required"}}`),
	)

	require.Equal(t, 1, repo.errorCalls)
	require.Zero(t, repo.tempCalls)
	require.Equal(t, grokPaymentRequiredErrorMessage, repo.lastReason)
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastSnapshot)
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

func TestAccountTestServiceGrokOAuthPaymentRequiredMarksAccountError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":"personal-team-blocked:spending-limit"}`)),
	}}
	account := &Account{
		ID:          56,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
	repo := &grokConditionalStateRepoStub{updated: true}
	svc := &AccountTestService{
		accountRepo:       repo,
		grokTokenProvider: NewGrokTokenProvider(repo, nil),
		httpUpstream:      upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/56/test", nil)

	err := svc.testGrokAccountConnection(c, account, "grok", "hi")

	require.Error(t, err)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, repo.errorCalls)
	require.Zero(t, repo.tempCalls)
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastSnapshot)
	require.True(t, repo.lastUntil.IsZero())
	require.Equal(t, grokPaymentRequiredErrorMessage, repo.lastReason)
	require.Contains(t, recorder.Body.String(), "Grok API returned 402")
}

func TestAccountTestServiceGrokAPIKeyPaymentRequiredMarksAccountError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":"payment-required"}`)),
	}}
	account := &Account{
		ID:          57,
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "xai-test-key",
		},
	}
	repo := &grokConditionalStateRepoStub{updated: true}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/57/test", nil)

	err := svc.testGrokAccountConnection(c, account, "grok", "hi")

	require.Error(t, err)
	require.Equal(t, 1, repo.errorCalls)
	require.Zero(t, repo.tempCalls)
	require.Equal(t, grokPaymentRequiredErrorMessage, repo.lastReason)
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastSnapshot)
	require.Contains(t, recorder.Body.String(), "Grok API returned 402")
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

func TestBuildGrokCompactRequestBodyUsesResponsesCompactionTurn(t *testing.T) {
	body := []byte(`{"model":"grok-4.5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"tools":[{"type":"function","name":"shell"}],"stream":true}`)
	patched, err := buildGrokCompactRequestBody(body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(patched, "stream").Bool())
	require.False(t, gjson.GetBytes(patched, "store").Bool())
	require.Equal(t, "none", gjson.GetBytes(patched, "tool_choice").String())
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(patched, "include.0").String())
	require.Equal(t, "hello", gjson.GetBytes(patched, "input.0.content.0.text").String())
	prompt := gjson.GetBytes(patched, "input.1.content.0.text").String()
	require.Contains(t, prompt, "1. Primary Request and Intent")
	require.Contains(t, prompt, "9. Optional Next Step")
}

func TestConvertGrokResponseToOpenAICompact(t *testing.T) {
	body := []byte(`{
		"id":"resp_grok_1",
		"object":"response",
		"status":"completed",
		"model":"grok-4.5",
		"output":[
			{"id":"rs_1","type":"reasoning","summary":[],"encrypted_content":"grok-encrypted-state"},
			{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"summary text"}]}
		],
		"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}
	}`)
	converted, err := convertGrokResponseToOpenAICompact(body)
	require.NoError(t, err)
	require.Equal(t, "resp_grok_1", gjson.GetBytes(converted, "id").String())
	require.Len(t, gjson.GetBytes(converted, "output").Array(), 1)
	require.Equal(t, "compaction", gjson.GetBytes(converted, "output.0.type").String())
	require.Equal(t, "grok-encrypted-state", gjson.GetBytes(converted, "output.0.encrypted_content").String())
	require.Equal(t, "summary text", gjson.GetBytes(converted, "output.0.summary.0.text").String())
	require.Equal(t, int64(14), gjson.GetBytes(converted, "usage.total_tokens").Int())
}

func TestForwardGrokResponsesCompactConvertsJSONAndSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	jsonResponse := `{
		"id":"resp_grok_compact",
		"object":"response",
		"status":"completed",
		"model":"grok-4.5",
		"output":[
			{"id":"rs_compact","type":"reasoning","status":"completed","summary":[],"encrypted_content":"grok-compact-state"},
			{"id":"msg_compact","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"compact summary"}]}
		],
		"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17}
	}`
	sseResponse := strings.Join([]string{
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"rs_compact","type":"reasoning","status":"completed","summary":[],"encrypted_content":"grok-compact-state"}}`,
		"",
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"id":"msg_compact","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"compact summary"}]}}`,
		"",
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_grok_compact","object":"response","status":"completed","model":"grok-4.5","output":[],"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "JSON", contentType: "application/json", body: jsonResponse},
		{name: "SSE", contentType: "text/event-stream", body: sseResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestBody := []byte(`{"model":"grok","input":"compact this conversation","stream":false}`)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(requestBody))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}}
			svc := &OpenAIGatewayService{
				httpUpstream: upstream,
				cache:        &stubGatewayCache{},
				cfg:          &config.Config{},
			}
			account := &Account{
				ID:          58,
				Name:        "grok-compact",
				Platform:    PlatformGrok,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key":  "xai-compact-key",
					"base_url": "https://api.x.ai/v1",
				},
			}

			result, err := svc.forwardGrokResponses(
				context.Background(),
				c,
				account,
				requestBody,
				"grok",
				false,
				time.Now(),
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.Stream)
			require.Equal(t, "resp_grok_compact", result.ResponseID)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 5, result.Usage.OutputTokens)
			require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.1.content.0.text").String(), "1. Primary Request and Intent")

			response := recorder.Body.Bytes()
			require.True(t, json.Valid(response))
			require.Len(t, gjson.GetBytes(response, "output").Array(), 1)
			require.Equal(t, "compaction", gjson.GetBytes(response, "output.0.type").String())
			require.Equal(t, "grok-compact-state", gjson.GetBytes(response, "output.0.encrypted_content").String())
			require.Equal(t, "compact summary", gjson.GetBytes(response, "output.0.summary.0.text").String())
		})
	}
}

func TestPatchGrokResponsesBodyRestoresCompactInput(t *testing.T) {
	body := []byte(`{
		"model":"grok-4.5",
		"input":[
			{"id":"cmp_1","type":"compaction","status":"completed","encrypted_content":"grok-encrypted-state","summary":[{"type":"summary_text","text":"summary text"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`)
	patched, err := patchGrokResponsesBody(body, "grok-4.5")
	require.NoError(t, err)
	require.Equal(t, "reasoning", gjson.GetBytes(patched, "input.0.type").String())
	require.Equal(t, "grok-encrypted-state", gjson.GetBytes(patched, "input.0.encrypted_content").String())
	require.Contains(t, gjson.GetBytes(patched, "input.1.content.0.text").String(), "summary text")
	require.Equal(t, "continue", gjson.GetBytes(patched, "input.2.content.0.text").String())
}

func TestSanitizeGrokResponsesToolsDropsOrphanedToolChoice(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantTools      bool
		wantToolChoice bool
	}{
		{name: "missing tools", body: `{"input":"hello","tool_choice":"auto"}`},
		{name: "all unsupported", body: `{"input":"hello","tools":[{"type":"namespace","name":"client_tools"}],"tool_choice":"auto"}`},
		{name: "supported", body: `{"input":"hello","tools":[{"type":"function","name":"lookup"}],"tool_choice":"auto"}`, wantTools: true, wantToolChoice: true},
		{name: "malformed tools", body: `{"input":"hello","tools":{"type":"function","name":"lookup"},"tool_choice":"auto"}`, wantTools: true, wantToolChoice: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			patched, err := sanitizeGrokResponsesTools([]byte(test.body))
			require.NoError(t, err)
			require.True(t, json.Valid(patched))
			require.Equal(t, test.wantTools, gjson.GetBytes(patched, "tools").Exists())
			require.Equal(t, test.wantToolChoice, gjson.GetBytes(patched, "tool_choice").Exists())
		})
	}
}

func TestTrimGrokInvalidEncryptedContentRetryBody(t *testing.T) {
	body := []byte(`{"input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"keep"}],"encrypted_content":"cipher"},{"type":"message","role":"user","content":"hi"}],"metadata":{"large_id":9007199254740993}}`)
	trimmed, changed, err := trimGrokInvalidEncryptedContentRetryBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(trimmed, "input.0.encrypted_content").Exists())
	require.Equal(t, "keep", gjson.GetBytes(trimmed, "input.0.summary.0.text").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(trimmed, "metadata.large_id").Raw)
}

func TestExplicitGrokCacheSeedPreservesIDEConversationHeaders(t *testing.T) {
	headers := []struct {
		name  string
		value string
	}{
		{name: openCodeSessionAffinityHeader, value: "opencode-affinity"},
		{name: openCodeSessionIDHeader, value: "opencode-session"},
		{name: openCodeNativeSessionHeader, value: "opencode-native"},
		{name: codeBuddyConversationHeader, value: "codebuddy-conversation"},
	}
	for _, header := range headers {
		t.Run(header.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set(header.name, header.value)
			require.Equal(t, header.value, explicitGrokCacheSeed(c, []byte(`{"prompt_cache_key":"body-key"}`), "fallback"))
		})
	}
}

func TestExtractGrokUpstreamModelIDsPrefersProtocolIdentifiers(t *testing.T) {
	models, err := extractGrokUpstreamModelIDs([]byte(`{
		"models":[
			{"name":"Display label","modelId":"grok-4.5"},
			{"name":"Other label","_meta":{"model_id":"grok-code-fast-1"}}
		]
	}`))
	require.NoError(t, err)
	require.Equal(t, []string{"grok-4.5", "grok-code-fast-1"}, models)
}
