//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpencodeTestErrorRetryableWithOtherModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{"below 400 never retryable", http.StatusOK, `{"choices":[]}`, false},
		{"403 region error", http.StatusForbidden, `{"type":"error","error":{"type":"RegionError","message":"only available hosted in China"}}`, true},
		{"503 server_error", http.StatusServiceUnavailable, `{"error":{"type":"server_error","message":"Error from provider: Endpoint is unavailable."}}`, true},
		{"404 model not found", http.StatusNotFound, `{"error":{"message":"model not found"}}`, true},
		{"401 auth error", http.StatusUnauthorized, `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`, false},
		{"401 credits error", http.StatusUnauthorized, `{"type":"error","error":{"type":"CreditsError","message":"Insufficient balance."}}`, false},
		{"429 usage limit", http.StatusTooManyRequests, `{"type":"error","error":{"type":"GoUsageLimitError","message":"Weekly usage limit reached."}}`, false},
		{"403 non-model error not retryable", http.StatusForbidden, `{"error":{"message":"Forbidden"}}`, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, opencodeTestErrorRetryableWithOtherModel(tt.statusCode, tt.body))
		})
	}
}

func TestOpencodeProbeResponseIsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		protocol OpencodeGoProtocol
		body     string
		want     bool
	}{
		{name: "chat", protocol: OpencodeGoProtocolChat, body: `{"choices":[{"message":{"content":"OK"}}]}`, want: true},
		{name: "chat empty", protocol: OpencodeGoProtocolChat, body: `{"choices":[]}`},
		{name: "messages", protocol: OpencodeGoProtocolMessages, body: `{"type":"message","content":[{"type":"text","text":"OK"}]}`, want: true},
		{name: "messages wrong type", protocol: OpencodeGoProtocolMessages, body: `{"type":"error","content":[{"type":"text","text":"OK"}]}`},
		{name: "responses", protocol: OpencodeGoProtocolResponses, body: `{"object":"response","output":[{"type":"message"}]}`, want: true},
		{name: "responses empty", protocol: OpencodeGoProtocolResponses, body: `{"object":"response","output":[]}`},
		{name: "malformed", protocol: OpencodeGoProtocolChat, body: `not-json`},
		{name: "unknown protocol", protocol: OpencodeGoProtocol("unknown"), body: `{}`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, opencodeProbeResponseIsValid(tt.protocol, []byte(tt.body)))
		})
	}
}

type opencodeTestRequest struct {
	model   string
	method  string
	path    string
	header  http.Header
	payload map[string]any
}

// opencodeTestUpstream 是 HTTPUpstream 的测试替身，按请求体里的 model 返回预设响应并记录请求。
type opencodeTestUpstream struct {
	responses map[string]*http.Response
	calls     []string
	requests  []opencodeTestRequest
}

func (u *opencodeTestUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *opencodeTestUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	bodyBytes, _ := io.ReadAll(req.Body)
	var parsed map[string]any
	_ = json.Unmarshal(bodyBytes, &parsed)
	model, _ := parsed["model"].(string)
	u.calls = append(u.calls, model)
	u.requests = append(u.requests, opencodeTestRequest{
		model:   model,
		method:  req.Method,
		path:    req.URL.Path,
		header:  req.Header.Clone(),
		payload: parsed,
	})
	if resp, ok := u.responses[model]; ok {
		return resp, nil
	}
	return nil, fmt.Errorf("unexpected OpenCode test request for model %q", model)
}

func newOpencodeChatOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"OK"}}]}`)),
	}
}

func newOpencodeMessagesOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"type":"message","content":[{"type":"text","text":"OK"}]}`)),
	}
}

func newOpencodeResponsesOKResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`)),
	}
}

func newOpencodeErrorResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newOpencodeTestService(upstream HTTPUpstream) *AccountTestService {
	return &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
}

func opencodeTestGinContext(t *testing.T) *gin.Context {
	t.Helper()
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginCtx.Request = (&http.Request{}).WithContext(context.Background())
	return ginCtx
}

func TestOpencodeTestModelsAreInAuditedCatalog(t *testing.T) {
	t.Parallel()
	models := append([]string{defaultOpencodeTestModel}, opencodeTestModelFallbacks...)
	for _, model := range models {
		spec, ok := ResolveOpencodeGoModelSpec(model)
		require.Truef(t, ok, "OpenCode test model %q must be present in the audited catalog", model)
		require.Equal(t, model, spec.ID)
		require.NotEmpty(t, spec.Protocol)
		require.Falsef(t, spec.Deprecated, "OpenCode connection test must not depend on deprecated model %q", model)
	}
}

func TestOpencodeAccountConnectionRoutesByFinalModelProtocol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		requestedModel     string
		modelMapping       map[string]any
		upstreamModel      string
		protocol           OpencodeGoProtocol
		path               string
		response           *http.Response
		wantBearer         bool
		wantMessagesFields bool
	}{
		{
			name:               "chat model with provider prefix",
			requestedModel:     "opencode-go/deepseek-v4-flash[1m]",
			upstreamModel:      "deepseek-v4-flash",
			protocol:           OpencodeGoProtocolChat,
			path:               "/zen/go/v1/chat/completions",
			response:           newOpencodeChatOKResponse(),
			wantBearer:         true,
			wantMessagesFields: true,
		},
		{
			name:               "messages model",
			requestedModel:     "qwen3.8-flash",
			upstreamModel:      "qwen3.8-flash",
			protocol:           OpencodeGoProtocolMessages,
			path:               "/zen/go/v1/messages",
			response:           newOpencodeMessagesOKResponse(),
			wantMessagesFields: true,
		},
		{
			name:           "mapped responses model",
			requestedModel: "client-model",
			modelMapping: map[string]any{
				"client-model": "opencode/grok-4.6",
			},
			upstreamModel: "grok-4.6",
			protocol:      OpencodeGoProtocolResponses,
			path:          "/zen/go/v1/responses",
			response:      newOpencodeResponsesOKResponse(),
			wantBearer:    true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			upstream := &opencodeTestUpstream{responses: map[string]*http.Response{
				tt.upstreamModel: tt.response,
			}}
			credentials := map[string]any{"api_key": "opencode-secret"}
			if tt.modelMapping != nil {
				credentials["model_mapping"] = tt.modelMapping
			}
			account := &Account{
				Platform:    PlatformOpencode,
				Type:        AccountTypeAPIKey,
				Credentials: credentials,
			}

			err := newOpencodeTestService(upstream).testOpencodeAccountConnection(opencodeTestGinContext(t), account, tt.requestedModel)
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			request := upstream.requests[0]
			require.Equal(t, http.MethodPost, request.method)
			require.Equal(t, tt.path, request.path)
			require.Equal(t, tt.upstreamModel, request.model)
			require.Equal(t, false, request.payload["stream"])

			if tt.wantBearer {
				require.Equal(t, "Bearer opencode-secret", request.header.Get("Authorization"))
				require.Empty(t, request.header.Get("x-api-key"))
				require.Empty(t, request.header.Get("anthropic-version"))
			} else {
				require.Empty(t, request.header.Get("Authorization"))
				require.Equal(t, "opencode-secret", request.header.Get("x-api-key"))
				require.Equal(t, "2023-06-01", request.header.Get("anthropic-version"))
			}

			_, hasMessages := request.payload["messages"]
			require.Equal(t, tt.wantMessagesFields, hasMessages)
			_, hasInput := request.payload["input"]
			require.Equal(t, tt.protocol == OpencodeGoProtocolResponses, hasInput)
		})
	}
}

func TestOpencodeAccountConnectionRejectsUnknownModelBeforeRequest(t *testing.T) {
	t.Parallel()
	upstream := &opencodeTestUpstream{responses: map[string]*http.Response{}}
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
		},
	}

	err := newOpencodeTestService(upstream).testOpencodeAccountConnection(opencodeTestGinContext(t), account, "future-model")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown upstream model "future-model"`)
	require.Empty(t, upstream.requests)
}

func TestOpencodeAccountConnectionRejectsDoubleProviderPrefixBeforeRequest(t *testing.T) {
	t.Parallel()
	upstream := &opencodeTestUpstream{responses: map[string]*http.Response{}}
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
			"model_mapping": map[string]any{
				"client-model": "opencode-go/opencode-go/deepseek-v4-flash",
			},
		},
	}

	err := newOpencodeTestService(upstream).testOpencodeAccountConnection(opencodeTestGinContext(t), account, "client-model")
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown upstream model "opencode-go/deepseek-v4-flash"`)
	require.Empty(t, upstream.requests)
}

func TestOpencodeAccountConnectionFallsBackOnRegionError(t *testing.T) {
	t.Parallel()
	upstream := &opencodeTestUpstream{responses: map[string]*http.Response{
		"deepseek-v4-flash": newOpencodeErrorResponse(http.StatusForbidden, `{"type":"error","error":{"type":"RegionError","message":"only available hosted in China"}}`),
		"gpt-5.6-luna":      newOpencodeResponsesOKResponse(),
	}}
	svc := newOpencodeTestService(upstream)
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
		},
	}

	err := svc.testOpencodeAccountConnection(opencodeTestGinContext(t), account, "")
	require.NoError(t, err)
	require.Equal(t, []string{"deepseek-v4-flash", "gpt-5.6-luna"}, upstream.calls,
		"should fall back from deepseek-v4-flash to gpt-5.6-luna on RegionError")
	require.Equal(t, "/zen/go/v1/chat/completions", upstream.requests[0].path)
	require.Equal(t, "/zen/go/v1/responses", upstream.requests[1].path)
}

func TestOpencodeAccountConnectionNoFallbackOnAuthError(t *testing.T) {
	t.Parallel()
	upstream := &opencodeTestUpstream{responses: map[string]*http.Response{
		"deepseek-v4-flash": newOpencodeErrorResponse(http.StatusUnauthorized, `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`),
	}}
	svc := newOpencodeTestService(upstream)
	account := &Account{
		Platform: PlatformOpencode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-secret",
		},
	}

	err := svc.testOpencodeAccountConnection(opencodeTestGinContext(t), account, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Equal(t, []string{"deepseek-v4-flash"}, upstream.calls,
		"auth error is account-level and must not trigger model fallback")
}
