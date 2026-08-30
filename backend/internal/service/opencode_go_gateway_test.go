//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type opencodeGoGatewayNoCallUpstream struct {
	calls int
}

func (u *opencodeGoGatewayNoCallUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return nil, errors.New("unexpected OpenCode Go upstream request")
}

func (u *opencodeGoGatewayNoCallUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	return nil, errors.New("unexpected OpenCode Go TLS upstream request")
}

type opencodeGoGatewayCaptureUpstream struct {
	calls               int
	doCalls             int
	tlsCalls            int
	tlsProfile          *tlsfingerprint.Profile
	req                 *http.Request
	body                []byte
	statusCode          int
	responseContentType string
	responseBody        string
}

func (u *opencodeGoGatewayCaptureUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.doCalls++
	return u.record(req)
}

func (u *opencodeGoGatewayCaptureUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.tlsCalls++
	u.tlsProfile = profile
	return u.record(req)
}

func (u *opencodeGoGatewayCaptureUpstream) record(req *http.Request) (*http.Response, error) {
	u.calls++
	u.req = req
	if req != nil && req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		u.body = append([]byte(nil), body...)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	contentType := u.responseContentType
	if contentType == "" {
		contentType = "application/json"
	}
	responseBody := u.responseBody
	if responseBody == "" {
		responseBody = `{
			"id":"resp_opencode_go",
			"object":"response",
			"model":"grok-4.6",
			"status":"completed",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
			"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
		}`
	}
	statusCode := u.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{contentType},
			"x-request-id": []string{"req-opencode-go-native-responses"},
		},
		Body:    io.NopCloser(strings.NewReader(responseBody)),
		Request: req,
	}, nil
}

func newOpencodeGoGatewayTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	service := &OpenAIGatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		httpUpstream: upstream,
	}
	service.SetTLSFingerprintProfileService(&TLSFingerprintProfileService{})
	return service
}

func newOpencodeGoGatewayTestAccount(modelMapping map[string]any) *Account {
	credentials := map[string]any{"api_key": "opencode-go-test-key"}
	if modelMapping != nil {
		credentials["model_mapping"] = modelMapping
	}
	return &Account{
		ID:          88001,
		Name:        "opencode-go-gateway-test",
		Platform:    PlatformOpencode,
		Type:        AccountTypeAPIKey,
		Credentials: credentials,
		Extra:       map[string]any{},
		Concurrency: 1,
	}
}

func newOpencodeGoGatewayContext(method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	return ginContext, recorder
}

func requireOpencodeGoCapabilityMismatch(
	t *testing.T,
	err error,
	inbound OpencodeGoProtocol,
	required OpencodeGoProtocol,
	wantDetail string,
) {
	t.Helper()
	require.Error(t, err)
	var routingErr *opencodeGoRoutingError
	require.ErrorAs(t, err, &routingErr)
	require.Equal(t, "capability_mismatch", routingErr.kind)
	require.Equal(t, inbound, routingErr.inbound)
	require.Equal(t, required, routingErr.required)
	require.Contains(t, routingErr.detail, wantDetail)
}

func TestOpencodeGoTransportSelectionLeavesOrdinaryOpenAIOnDo(t *testing.T) {
	upstream := &opencodeGoGatewayCaptureUpstream{}
	service := newOpencodeGoGatewayTestService(upstream)
	request := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	account := &Account{
		ID:          88002,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
	}

	response, err := service.doOpenAIAccountUpstream(request, "", account)

	require.NoError(t, err)
	require.NotNil(t, response)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, 1, upstream.doCalls, "ordinary OpenAI transport must remain on HTTPUpstream.Do")
	require.Zero(t, upstream.tlsCalls)
	require.Nil(t, upstream.tlsProfile)
}

func TestOpencodeGoResolveForwardModelUsesFinalMappedModelBeforeProtocolLookup(t *testing.T) {
	tests := []struct {
		name               string
		requestedModel     string
		modelMapping       map[string]any
		defaultMappedModel string
		wantRequested      string
		wantBilling        string
		wantUpstream       string
		wantProtocol       OpencodeGoProtocol
	}{
		{
			name:           "account mapping wins over dispatch default and normalizes chat model",
			requestedModel: "  client-chat  ",
			modelMapping: map[string]any{
				"client-chat": "opencode-go/deepseek-v4-flash[1m]",
			},
			defaultMappedModel: "opencode/qwen3.8-flash[1m]",
			wantRequested:      "client-chat",
			wantBilling:        "opencode-go/deepseek-v4-flash[1m]",
			wantUpstream:       "deepseek-v4-flash",
			wantProtocol:       OpencodeGoProtocolChat,
		},
		{
			name:               "dispatch default is used after an unmatched alias and normalizes messages model",
			requestedModel:     "client-messages",
			defaultMappedModel: " opencode/qwen3.8-flash[1m] ",
			wantRequested:      "client-messages",
			wantBilling:        "opencode/qwen3.8-flash[1m]",
			wantUpstream:       "qwen3.8-flash",
			wantProtocol:       OpencodeGoProtocolMessages,
		},
		{
			name:           "account mapping normalizes responses model before catalog lookup",
			requestedModel: "client-responses",
			modelMapping: map[string]any{
				"client-responses": " opencode/grok-4.6[1m] ",
			},
			wantRequested: "client-responses",
			wantBilling:   "opencode/grok-4.6[1m]",
			wantUpstream:  "grok-4.6",
			wantProtocol:  OpencodeGoProtocolResponses,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := resolveOpencodeGoForwardModel(
				newOpencodeGoGatewayTestAccount(tt.modelMapping),
				tt.requestedModel,
				tt.defaultMappedModel,
			)

			require.NoError(t, err)
			require.Equal(t, tt.wantRequested, resolved.RequestedModel)
			require.Equal(t, tt.wantBilling, resolved.BillingModel)
			require.Equal(t, tt.wantUpstream, resolved.UpstreamModel)
			require.Equal(t, tt.wantUpstream, resolved.Spec.ID)
			require.Equal(t, tt.wantProtocol, resolved.Spec.Protocol)
		})
	}
}

func TestOpencodeGoResolveForwardModelRejectsUnknownMappedModel(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"future-alias": "opencode-go/future-model[1m]",
	})

	resolved, err := resolveOpencodeGoForwardModel(account, "future-alias", "deepseek-v4-flash")

	require.Error(t, err)
	require.Equal(t, OpencodeGoResolvedModel{}, resolved)
	var routingErr *opencodeGoRoutingError
	require.ErrorAs(t, err, &routingErr)
	require.Equal(t, "unknown_model", routingErr.kind)
	require.Equal(t, "future-alias", routingErr.model)
	require.Equal(t, "future-model", routingErr.resolvedModel)
	require.Contains(t, err.Error(), `resolves to "future-model"`)
	require.Contains(t, err.Error(), "not present in the audited model catalog")
}

func TestOpencodeGoResolveForwardModelNormalizesExactlyOnce(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"double-prefix-alias": "opencode/opencode-go/deepseek-v4-flash",
	})

	resolved, err := resolveOpencodeGoForwardModel(account, "double-prefix-alias", "")

	require.Error(t, err)
	require.Equal(t, OpencodeGoResolvedModel{}, resolved)
	var routingErr *opencodeGoRoutingError
	require.ErrorAs(t, err, &routingErr)
	require.Equal(t, "unknown_model", routingErr.kind)
	require.Equal(t, "opencode-go/deepseek-v4-flash", routingErr.resolvedModel)
}

func TestOpencodeGoResolveForwardModelAcceptsExplicitDoublePrefixRequestAlias(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"opencode-go/opencode-go/deepseek-v4-flash": "deepseek-v4-flash",
	})

	resolved, err := resolveOpencodeGoForwardModel(
		account,
		"opencode-go/opencode-go/deepseek-v4-flash",
		"",
	)

	require.NoError(t, err)
	require.Equal(t, "opencode-go/opencode-go/deepseek-v4-flash", resolved.RequestedModel)
	require.Equal(t, "deepseek-v4-flash", resolved.BillingModel)
	require.Equal(t, "deepseek-v4-flash", resolved.UpstreamModel)
	require.Equal(t, OpencodeGoProtocolChat, resolved.Spec.Protocol)
}

func TestOpencodeGoResolveOpenAIAPIKeyResponsesURL(t *testing.T) {
	service := newOpencodeGoGatewayTestService(nil)
	tests := []struct {
		name    string
		account *Account
		want    string
	}{
		{
			name: "OpenCode ignores credential base URL and uses fixed Go Responses endpoint",
			account: &Account{
				Platform: PlatformOpencode,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":  "opencode-go-test-key",
					"base_url": "https://must-not-be-used.example/v1",
				},
			},
			want: "https://opencode.ai/zen/go/v1/responses",
		},
		{
			name: "ordinary OpenAI custom v1 base keeps existing URL behavior",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":  "openai-test-key",
					"base_url": "https://relay.example/openai/v1",
				},
			},
			want: "https://relay.example/openai/v1/responses",
		},
		{
			name: "ordinary OpenAI Responses URL is not duplicated",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":  "openai-test-key",
					"base_url": "https://relay.example/openai/v1/responses",
				},
			},
			want: "https://relay.example/openai/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.resolveOpenAIAPIKeyResponsesURL(tt.account)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOpencodeGoIngressDecisionMatrixRejectsBeforeUpstream(t *testing.T) {
	tests := []struct {
		name               string
		path               string
		body               []byte
		forward            func(*OpenAIGatewayService, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)
		wantMessageParts   []string
		forbidMessageParts []string
	}{
		{
			name: "chat ingress rejects messages model",
			path: "/v1/chat/completions",
			body: []byte(`{
				"model":"qwen3.8-flash",
				"messages":[{"role":"user","content":"hello"}],
				"stream":false
			}`),
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			},
			wantMessageParts: []string{`model "qwen3.8-flash"`, "requires the messages protocol", "current chat request", "use /v1/messages"},
		},
		{
			name: "responses ingress rejects messages model",
			path: "/v1/responses",
			body: []byte(`{
				"model":"qwen3.8-flash",
				"input":"hello",
				"stream":false
			}`),
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.Forward(context.Background(), c, account, body)
			},
			wantMessageParts: []string{`model "qwen3.8-flash"`, "requires the messages protocol", "current responses request", "use /v1/messages"},
		},
		{
			name: "unknown model has no guessed protocol",
			path: "/v1/chat/completions",
			body: []byte(`{
				"model":"future-model",
				"messages":[{"role":"user","content":"hello"}],
				"stream":false
			}`),
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			},
			wantMessageParts:   []string{`model "future-model"`, "not present in the audited model catalog"},
			forbidMessageParts: []string{"use /v1/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			body := append([]byte(nil), tt.body...)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, tt.path, body)

			result, err := tt.forward(service, c, newOpencodeGoGatewayTestAccount(nil), body)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			require.Equal(t, "model_protocol_capability_error", gjson.Get(recorder.Body.String(), "error.type").String())
			require.Equal(t, "model", gjson.Get(recorder.Body.String(), "error.param").String())
			message := gjson.Get(recorder.Body.String(), "error.message").String()
			for _, part := range tt.wantMessageParts {
				require.Contains(t, message, part)
			}
			for _, part := range tt.forbidMessageParts {
				require.NotContains(t, message, part)
			}
			require.Zero(t, upstream.calls, "routing rejection must happen before any upstream request")
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}
}

func TestOpencodeGoMessagesToChatCapabilityChecks(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(nil)
	resolved, err := resolveOpencodeGoForwardModel(account, "deepseek-v4-flash", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolChat, resolved.Spec.Protocol)

	rejected := []struct {
		name       string
		body       []byte
		wantDetail string
	}{
		{
			name: "server web search tool",
			body: []byte(`{
				"model":"deepseek-v4-flash",
				"max_tokens":64,
				"messages":[{"role":"user","content":"hello"}],
				"tools":[{"type":"web_search_20250305","name":"web_search"}]
			}`),
			wantDetail: "server-side web search tools",
		},
		{
			name: "thinking configuration",
			body: []byte(`{
				"model":"deepseek-v4-flash",
				"max_tokens":64,
				"thinking":{"type":"enabled","budget_tokens":1024},
				"messages":[{"role":"user","content":"hello"}]
			}`),
			wantDetail: "thinking configuration",
		},
		{
			name: "nested cache control",
			body: []byte(`{
				"model":"deepseek-v4-flash",
				"max_tokens":64,
				"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]
			}`),
			wantDetail: "cache_control",
		},
		{
			name: "document content block",
			body: []byte(`{
				"model":"deepseek-v4-flash",
				"max_tokens":64,
				"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AA=="}}]}]
			}`),
			wantDetail: `content block type "document"`,
		},
		{
			name: "stop sequences",
			body: []byte(`{
				"model":"deepseek-v4-flash",
				"max_tokens":64,
				"stop_sequences":["END"],
				"messages":[{"role":"user","content":"hello"}]
			}`),
			wantDetail: "stop_sequences",
		},
		{
			name: "historical thinking block",
			body: []byte(`{
				"model":"deepseek-v4-flash",
				"max_tokens":64,
				"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"private"}]},{"role":"user","content":"continue"}]
			}`),
			wantDetail: `content block type "thinking"`,
		},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			body := append([]byte(nil), tt.body...)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/messages", body)

			result, forwardErr := service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

			require.Error(t, forwardErr)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			require.Equal(t, "error", gjson.Get(recorder.Body.String(), "type").String())
			require.Equal(t, "invalid_request_error", gjson.Get(recorder.Body.String(), "error.type").String())
			message := gjson.Get(recorder.Body.String(), "error.message").String()
			require.Contains(t, message, tt.wantDetail)
			require.Contains(t, message, "use /v1/chat/completions")
			require.Zero(t, upstream.calls, "capability rejection must be local")
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}

	standardFunctionBody := []byte(`{
		"model":"deepseek-v4-flash",
		"max_tokens":128,
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"lookup","description":"look up a value","input_schema":{"type":"object","properties":{"id":{"type":"string"},"cache_control":{"type":"string"}}}}]
	}`)
	require.NoError(t, validateOpencodeMessagesToChatBridge(standardFunctionBody, resolved), "standard Anthropic function tools and business schema fields named cache_control must remain bridgeable")
}

func TestOpencodeGoChatToResponsesCapabilityChecks(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(nil)
	resolved, err := resolveOpencodeGoForwardModel(account, "grok-4.6", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolResponses, resolved.Spec.Protocol)

	rejected := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "custom tool",
			body:       `{"model":"grok-4.6","input":"hello","stream":false,"tools":[{"type":"custom","name":"exec"}]}`,
			wantDetail: `tool type "custom"`,
		},
		{
			name:       "namespace tool",
			body:       `{"model":"grok-4.6","input":"hello","stream":false,"tools":[{"type":"namespace","name":"team","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}]}`,
			wantDetail: `tool type "namespace"`,
		},
		{
			name:       "tool search",
			body:       `{"model":"grok-4.6","input":"hello","stream":false,"tools":[{"type":"tool_search"}]}`,
			wantDetail: `tool type "tool_search"`,
		},
		{
			name:       "chat stop sequences",
			body:       `{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"stop":"END","stream":false}`,
			wantDetail: `field "stop"`,
		},
		{
			name:       "unsupported chat response format",
			body:       `{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_object"},"stream":false}`,
			wantDetail: `field "response_format"`,
		},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/chat/completions", body)

			result, forwardErr := service.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

			require.Error(t, forwardErr)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			require.Equal(t, "model_protocol_capability_error", gjson.Get(recorder.Body.String(), "error.type").String())
			message := gjson.Get(recorder.Body.String(), "error.message").String()
			require.Contains(t, message, tt.wantDetail)
			require.Contains(t, message, "use /v1/responses")
			require.Zero(t, upstream.calls, "capability rejection must happen before the Responses bridge sends a request")
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}

	standardChatFunctionBody := []byte(`{
		"model":"grok-4.6",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"id":{"type":"string"}}}}]
	}`)
	require.NoError(t, validateOpencodeChatToResponsesBridge(standardChatFunctionBody, resolved), "standard Chat function tools must remain bridgeable")

	responsesShapedFunctionBody := []byte(`{
		"model":"grok-4.6",
		"input":"hello",
		"stream":false,
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`)
	require.NoError(t, validateOpencodeChatToResponsesBridge(responsesShapedFunctionBody, resolved), "standard Responses function tools sent through the Chat endpoint must remain bridgeable")
}

func TestOpencodeGoResponsesToChatCapabilityChecks(t *testing.T) {
	rejected := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "unsupported input item",
			body:       `{"model":"deepseek-v4-flash","input":[{"type":"local_shell_call","id":"shell_1","action":{"command":"pwd"}}],"stream":false}`,
			wantDetail: `input item type "local_shell_call"`,
		},
		{
			name:       "unsupported content part",
			body:       `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":[{"type":"input_audio","audio":{"data":"AA=="}}]}],"stream":false}`,
			wantDetail: `content part type "input_audio"`,
		},
		{
			name:       "responses include field",
			body:       `{"model":"deepseek-v4-flash","input":"hello","include":["reasoning.encrypted_content"],"stream":false}`,
			wantDetail: `field "include"`,
		},
		{
			name:       "custom tool",
			body:       `{"model":"deepseek-v4-flash","input":"hello","tools":[{"type":"custom","name":"exec"}],"stream":false}`,
			wantDetail: `tool type "custom"`,
		},
		{
			name:       "namespace tool",
			body:       `{"model":"deepseek-v4-flash","input":"hello","tools":[{"type":"namespace","name":"team","tools":[{"type":"function","name":"send"}]}],"stream":false}`,
			wantDetail: `tool type "namespace"`,
		},
		{
			name:       "tool search",
			body:       `{"model":"deepseek-v4-flash","input":"hello","tools":[{"type":"tool_search"}],"stream":false}`,
			wantDetail: `tool type "tool_search"`,
		},
		{
			name:       "reasoning input item",
			body:       `{"model":"deepseek-v4-flash","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"private"}]}],"stream":false}`,
			wantDetail: "reasoning input items",
		},
		{
			name:       "assistant image content",
			body:       `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}],"stream":false}`,
			wantDetail: `image content for role "assistant"`,
		},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			body := []byte(tt.body)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses", body)

			result, err := service.Forward(context.Background(), c, newOpencodeGoGatewayTestAccount(nil), body)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			require.Equal(t, "model_protocol_capability_error", gjson.Get(recorder.Body.String(), "error.type").String())
			message := gjson.Get(recorder.Body.String(), "error.message").String()
			require.Contains(t, message, tt.wantDetail)
			require.Contains(t, message, "use /v1/chat/completions")
			require.Zero(t, upstream.calls, "capability rejection must happen before the Chat bridge sends a request")
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}
}

func TestOpencodeGoMessagesToResponsesCapabilityChecks(t *testing.T) {
	rejected := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "thinking configuration",
			body:       `{"model":"grok-4.6","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`,
			wantDetail: "thinking configuration",
		},
		{
			name:       "document content block",
			body:       `{"model":"grok-4.6","max_tokens":64,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AA=="}}]}]}`,
			wantDetail: `content block type "document"`,
		},
		{
			name:       "stop sequences",
			body:       `{"model":"grok-4.6","max_tokens":64,"stop_sequences":["END"],"messages":[{"role":"user","content":"hello"}]}`,
			wantDetail: "stop_sequences",
		},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			body := []byte(tt.body)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/messages", body)

			result, err := service.ForwardAsAnthropic(context.Background(), c, newOpencodeGoGatewayTestAccount(nil), body, "", "")

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			require.Equal(t, "error", gjson.Get(recorder.Body.String(), "type").String())
			require.Equal(t, "invalid_request_error", gjson.Get(recorder.Body.String(), "error.type").String())
			message := gjson.Get(recorder.Body.String(), "error.message").String()
			require.Contains(t, message, tt.wantDetail)
			require.Contains(t, message, "use /v1/responses")
			require.Zero(t, upstream.calls, "capability rejection must happen before the Responses bridge sends a request")
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}
}

func TestOpencodeGoMessagesToChatUsesResolvedAliasAndBearer(t *testing.T) {
	upstream := &opencodeGoGatewayCaptureUpstream{
		responseBody: `{
			"id":"chatcmpl_opencode_bridge",
			"object":"chat.completion",
			"model":"deepseek-v4-flash",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`,
	}
	service := newOpencodeGoGatewayTestService(upstream)
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"gpt-client-high": "opencode-go/deepseek-v4-flash[1m]",
	})
	body := []byte(`{
		"model":"gpt-client-high",
		"max_tokens":128,
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"cache_control":{"type":"string"}}}}],
		"stream":false
	}`)
	c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/messages", body)

	result, err := service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, "/zen/go/v1/chat/completions", upstream.req.URL.Path)
	require.Equal(t, "Bearer opencode-go-test-key", upstream.req.Header.Get("Authorization"))
	require.Empty(t, upstream.req.Header.Get("x-api-key"))
	require.Equal(t, "deepseek-v4-flash", gjson.GetBytes(upstream.body, "model").String())
	require.False(t, gjson.GetBytes(upstream.body, "reasoning_effort").Exists(), "unspecified output_config.effort must not inject reasoning_effort")
	require.False(t, gjson.GetBytes(upstream.body, "parallel_tool_calls").Exists(), "unspecified Anthropic options must not inject parallel_tool_calls")
	require.Equal(t, "gpt-client-high", result.Model)
	require.Equal(t, "opencode-go/deepseek-v4-flash[1m]", result.BillingModel)
	require.Equal(t, "deepseek-v4-flash", result.UpstreamModel)
	require.Nil(t, result.ReasoningEffort, "usage metadata must match the request actually sent upstream")
	require.Equal(t, openAIChatRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
}

func TestOpencodeGoResponsesModelIgnoresGenericOpenAIForceChatFlags(t *testing.T) {
	tests := []struct {
		name    string
		inbound OpencodeGoProtocol
		extra   map[string]any
	}{
		{
			name:    "chat ignores negative capability probe",
			inbound: OpencodeGoProtocolChat,
			extra:   map[string]any{openai_compat.ExtraKeyResponsesSupported: false},
		},
		{
			name:    "chat ignores force chat mode",
			inbound: OpencodeGoProtocolChat,
			extra:   map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)},
		},
		{
			name:    "messages ignores negative capability probe",
			inbound: OpencodeGoProtocolMessages,
			extra:   map[string]any{openai_compat.ExtraKeyResponsesSupported: false},
		},
		{
			name:    "messages ignores force chat mode",
			inbound: OpencodeGoProtocolMessages,
			extra:   map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)},
		},
	}

	const upstreamSSE = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_opencode_bridge\",\"object\":\"response\",\"model\":\"grok-4.6\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\ndata: [DONE]\n\n"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayCaptureUpstream{
				responseContentType: "text/event-stream",
				responseBody:        upstreamSSE,
			}
			service := newOpencodeGoGatewayTestService(upstream)
			account := newOpencodeGoGatewayTestAccount(map[string]any{
				"gpt-client-high": "opencode-go/grok-4.6[1m]",
			})
			account.Extra = tt.extra

			var (
				body []byte
				path string
			)
			switch tt.inbound {
			case OpencodeGoProtocolChat:
				path = "/v1/chat/completions"
				body = []byte(`{"model":"gpt-client-high","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			case OpencodeGoProtocolMessages:
				path = "/v1/messages"
				body = []byte(`{"model":"gpt-client-high","messages":[{"role":"user","content":"hello"}],"max_tokens":128,"stream":false}`)
			default:
				t.Fatalf("unexpected inbound protocol %q", tt.inbound)
			}
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, path, body)

			var (
				result *OpenAIForwardResult
				err    error
			)
			if tt.inbound == OpencodeGoProtocolChat {
				result, err = service.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			} else {
				result, err = service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, 1, upstream.calls)
			require.Zero(t, upstream.doCalls, "OpenCode Responses bridge must not bypass TLS fingerprint transport")
			require.Equal(t, 1, upstream.tlsCalls)
			require.NotNil(t, upstream.tlsProfile)
			require.NotNil(t, upstream.req)
			require.Equal(t, "/zen/go/v1/responses", upstream.req.URL.Path)
			require.Equal(t, "Bearer opencode-go-test-key", upstream.req.Header.Get("Authorization"))
			require.Empty(t, upstream.req.Header.Get("x-api-key"))
			require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.body, "model").String())
			require.Equal(t, opencodeResponsesRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
			require.Equal(t, "gpt-client-high", result.Model)
			require.Equal(t, "opencode-go/grok-4.6[1m]", result.BillingModel)
			require.Equal(t, "grok-4.6", result.UpstreamModel)
		})
	}
}

func TestOpencodeGoNativeResponsesUsesFixedEndpointBearerAndFinalMappedSlug(t *testing.T) {
	upstream := &opencodeGoGatewayCaptureUpstream{}
	service := newOpencodeGoGatewayTestService(upstream)
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-responses-alias": "opencode-go/grok-4.6[1m]",
	})
	body := []byte(`{
		"model":"client-responses-alias",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"hello"},
				{"type":"input_image","image_url":"data:image/png;base64,"}
			]
		}],
		"stream":false,
		"max_output_tokens":32,
		"metadata":{"trace":"preserve-me"}
	}`)
	c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses", body)

	result, err := service.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, upstream.calls)
	require.Zero(t, upstream.doCalls, "OpenCode native Responses must not bypass TLS fingerprint transport")
	require.Equal(t, 1, upstream.tlsCalls)
	require.NotNil(t, upstream.tlsProfile)
	require.NotNil(t, upstream.req)
	require.Equal(t, http.MethodPost, upstream.req.Method)
	require.Equal(t, "https://opencode.ai/zen/go/v1/responses", upstream.req.URL.String())
	require.Equal(t, "/zen/go/v1/responses", upstream.req.URL.Path)
	require.Equal(t, "Bearer opencode-go-test-key", upstream.req.Header.Get("Authorization"))
	require.Empty(t, upstream.req.Header.Get("x-api-key"))
	require.Equal(t, "application/json", upstream.req.Header.Get("Content-Type"))
	require.JSONEq(t, `{
		"model":"grok-4.6",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"hello"},
				{"type":"input_image","image_url":"data:image/png;base64,"}
			]
		}],
		"stream":false,
		"max_output_tokens":32,
		"metadata":{"trace":"preserve-me"}
	}`, string(upstream.body), "native Responses forwarding may only replace the model with the final audited slug")
	require.Equal(t, int64(2), gjson.GetBytes(upstream.body, "input.0.content.#").Int(), "native Responses forwarding must not drop empty-base64 input parts")
	require.Equal(t, "data:image/png;base64,", gjson.GetBytes(upstream.body, "input.0.content.1.image_url").String(), "OpenCode Go native Responses must bypass generic passthrough image cleanup")
	require.Equal(t, "/v1/responses", GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, "client-responses-alias", result.Model)
	require.Equal(t, "opencode-go/grok-4.6[1m]", result.BillingModel)
	require.Equal(t, "grok-4.6", result.UpstreamModel)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, "client-responses-alias", gjson.Get(recorder.Body.String(), "model").String(), "client response must retain the requested alias")

	var forwarded map[string]any
	require.NoError(t, json.Unmarshal(upstream.body, &forwarded))
	require.Len(t, forwarded, 5, "native forwarding must not inject extra request fields")
}

func TestOpencodeGoNativeResponsesAppliesFastPolicy(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-responses-alias": "opencode-go/grok-4.6[1m]",
	})
	passSettings := &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		ServiceTier: OpenAIFastTierPriority,
		Action:      BetaPolicyActionPass,
		Scope:       BetaPolicyScopeAll,
	}}}
	blockSettings := &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		ServiceTier:    OpenAIFastTierPriority,
		Action:         BetaPolicyActionBlock,
		Scope:          BetaPolicyScopeAll,
		ErrorMessage:   "fast mode is blocked for OpenCode Responses",
		ModelWhitelist: []string{"grok-4.6"},
		FallbackAction: BetaPolicyActionPass,
	}}}

	tests := []struct {
		name         string
		settings     *OpenAIFastPolicySettings
		wantTier     string
		wantBlocked  bool
		wantUpstream bool
	}{
		{
			name:         "filter removes priority alias",
			settings:     DefaultOpenAIFastPolicySettings(),
			wantUpstream: true,
		},
		{
			name:         "pass normalizes fast alias",
			settings:     passSettings,
			wantTier:     OpenAIFastTierPriority,
			wantUpstream: true,
		},
		{
			name:        "block rejects before upstream",
			settings:    blockSettings,
			wantBlocked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayCaptureUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			service.settingService = newOpenAIGatewayServiceWithSettings(t, tt.settings).settingService
			body := []byte(`{
				"model":"client-responses-alias",
				"input":"hello",
				"stream":false,
				"service_tier":"fast",
				"metadata":{"trace":"preserve-me"}
			}`)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses", body)

			result, err := service.Forward(context.Background(), c, account, body)

			if tt.wantBlocked {
				require.Error(t, err)
				require.Nil(t, result)
				require.Zero(t, upstream.calls, "blocked fast requests must not reach OpenCode")
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Equal(t, "permission_error", gjson.Get(recorder.Body.String(), "error.type").String())
				require.Contains(t, gjson.Get(recorder.Body.String(), "error.message").String(), "fast mode is blocked")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantUpstream, upstream.calls == 1)
			require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.body, "model").String())
			require.Equal(t, "preserve-me", gjson.GetBytes(upstream.body, "metadata.trace").String())
			if tt.wantTier == "" {
				require.False(t, gjson.GetBytes(upstream.body, "service_tier").Exists(), "filter policy must remove service_tier")
			} else {
				require.Equal(t, tt.wantTier, gjson.GetBytes(upstream.body, "service_tier").String())
			}
		})
	}
}

func TestOpencodeGoNativeResponsesAuthorizationFailuresTriggerFailoverBeforeCommit(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-responses-alias": "opencode-go/grok-4.6[1m]",
	})
	body := []byte(`{"model":"client-responses-alias","input":"hello","stream":false}`)

	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden} {
		t.Run(fmt.Sprintf("status_%d", statusCode), func(t *testing.T) {
			upstream := &opencodeGoGatewayCaptureUpstream{
				statusCode:   statusCode,
				responseBody: `{"error":{"message":"OpenCode account is unavailable"}}`,
			}
			service := newOpencodeGoGatewayTestService(upstream)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses", body)

			result, err := service.Forward(context.Background(), c, account, body)

			require.Error(t, err)
			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, statusCode, failoverErr.StatusCode)
			require.Equal(t, 1, upstream.calls)
			require.False(t, IsResponseCommitted(c), "failover must be selected before writing a client response")
			require.Empty(t, recorder.Body.String(), "failover attempts must not leak the failed account response")
		})
	}
}

func TestOpencodeGoMessagesToChatOnlyForwardsExplicitGenerationOptions(t *testing.T) {
	request := &apicompat.AnthropicRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: minMaxOutputTokens,
		Messages: []apicompat.AnthropicMessage{{
			Role:    "user",
			Content: json.RawMessage(`"hello"`),
		}},
	}

	implicit, err := AnthropicToChatCompletionsRequest(request)
	require.NoError(t, err)
	require.Empty(t, implicit.ReasoningEffort)
	require.Nil(t, implicit.ParallelToolCalls)

	request.OutputConfig = &apicompat.AnthropicOutputConfig{Effort: "max"}
	explicit, err := AnthropicToChatCompletionsRequest(request)
	require.NoError(t, err)
	require.Equal(t, "xhigh", explicit.ReasoningEffort)
	require.Nil(t, explicit.ParallelToolCalls, "Anthropic Messages has no explicit parallel_tool_calls request field")
}

func TestOpencodeGoNativeChatUsesResolvedAliasBearerAndFixedEndpoint(t *testing.T) {
	upstream := &opencodeGoGatewayCaptureUpstream{
		responseBody: `{
			"id":"chatcmpl_opencode_native",
			"object":"chat.completion",
			"model":"deepseek-v4-flash",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`,
	}
	service := newOpencodeGoGatewayTestService(upstream)
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-chat-alias": "opencode-go/deepseek-v4-flash[1m]",
	})
	body := []byte(`{
		"model":"client-chat-alias",
		"messages":[{"role":"user","content":"hello"}],
		"stream":false
	}`)
	c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/chat/completions", body)

	result, err := service.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, upstream.calls)
	require.Zero(t, upstream.doCalls)
	require.Equal(t, 1, upstream.tlsCalls)
	require.NotNil(t, upstream.tlsProfile)
	require.NotNil(t, upstream.req)
	require.Equal(t, http.MethodPost, upstream.req.Method)
	require.Equal(t, "https://opencode.ai/zen/go/v1/chat/completions", upstream.req.URL.String())
	require.Equal(t, "/zen/go/v1/chat/completions", upstream.req.URL.Path)
	require.Equal(t, "Bearer opencode-go-test-key", upstream.req.Header.Get("Authorization"))
	require.Empty(t, upstream.req.Header.Get("x-api-key"))
	require.Empty(t, upstream.req.Header.Get("anthropic-version"))
	require.Equal(t, "application/json", upstream.req.Header.Get("Content-Type"))
	require.Equal(t, "deepseek-v4-flash", gjson.GetBytes(upstream.body, "model").String())
	require.Equal(t, "client-chat-alias", result.Model)
	require.Equal(t, "opencode-go/deepseek-v4-flash[1m]", result.BillingModel)
	require.Equal(t, "deepseek-v4-flash", result.UpstreamModel)
	require.Equal(t, openAIChatRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
}

func TestOpencodeGoNativeMessagesUsesResolvedAliasAPIKeyAndFixedEndpoint(t *testing.T) {
	upstream := &opencodeGoGatewayCaptureUpstream{
		responseBody: `{
			"id":"msg_opencode_native",
			"type":"message",
			"role":"assistant",
			"model":"qwen3.8-flash",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"stop_sequence":null,
			"usage":{"input_tokens":3,"output_tokens":2}
		}`,
	}
	service := newOpencodeGoGatewayTestService(upstream)
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-messages-alias": "opencode-go/qwen3.8-flash[1m]",
	})
	body := []byte(`{
		"model":"client-messages-alias",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hello"}],
		"stream":false
	}`)
	c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/messages", body)

	result, err := service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, upstream.calls)
	require.Zero(t, upstream.doCalls)
	require.Equal(t, 1, upstream.tlsCalls)
	require.NotNil(t, upstream.tlsProfile)
	require.NotNil(t, upstream.req)
	require.Equal(t, http.MethodPost, upstream.req.Method)
	require.Equal(t, "https://opencode.ai/zen/go/v1/messages", upstream.req.URL.String())
	require.Equal(t, "/zen/go/v1/messages", upstream.req.URL.Path)
	require.Empty(t, upstream.req.Header.Get("Authorization"))
	require.Equal(t, "opencode-go-test-key", upstream.req.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", upstream.req.Header.Get("anthropic-version"))
	require.Equal(t, "application/json", upstream.req.Header.Get("Content-Type"))
	require.Equal(t, "qwen3.8-flash", gjson.GetBytes(upstream.body, "model").String())
	require.Equal(t, "client-messages-alias", result.Model)
	require.Equal(t, "opencode-go/qwen3.8-flash[1m]", result.BillingModel)
	require.Equal(t, "qwen3.8-flash", result.UpstreamModel)
	require.Equal(t, opencodeMessagesRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
}

func TestOpencodeGoResponsesToChatUsesResolvedAliasBearerAndFixedEndpoint(t *testing.T) {
	upstream := &opencodeGoGatewayCaptureUpstream{
		responseBody: `{
			"id":"chatcmpl_opencode_responses_bridge",
			"object":"chat.completion",
			"model":"deepseek-v4-flash",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`,
	}
	service := newOpencodeGoGatewayTestService(upstream)
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-responses-chat-alias": "opencode-go/deepseek-v4-flash[1m]",
	})
	body := []byte(`{
		"model":"client-responses-chat-alias",
		"input":"hello",
		"stream":false,
		"max_output_tokens":32
	}`)
	c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses", body)

	result, err := service.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, upstream.calls)
	require.NotNil(t, upstream.req)
	require.Equal(t, http.MethodPost, upstream.req.Method)
	require.Equal(t, "https://opencode.ai/zen/go/v1/chat/completions", upstream.req.URL.String())
	require.Equal(t, "/zen/go/v1/chat/completions", upstream.req.URL.Path)
	require.Equal(t, "Bearer opencode-go-test-key", upstream.req.Header.Get("Authorization"))
	require.Empty(t, upstream.req.Header.Get("x-api-key"))
	require.Empty(t, upstream.req.Header.Get("anthropic-version"))
	require.Equal(t, "application/json", upstream.req.Header.Get("Content-Type"))
	require.Equal(t, "deepseek-v4-flash", gjson.GetBytes(upstream.body, "model").String())
	require.Equal(t, "client-responses-chat-alias", result.Model)
	require.Equal(t, "opencode-go/deepseek-v4-flash[1m]", result.BillingModel)
	require.Equal(t, "deepseek-v4-flash", result.UpstreamModel)
	require.Equal(t, openAIChatRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, "client-responses-chat-alias", gjson.Get(recorder.Body.String(), "model").String(), "client response must retain the requested alias")
}

func TestOpencodeGoUnknownModelRejectedAcrossMessagesAndResponsesBeforeUpstream(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		body        []byte
		forward     func(*OpenAIGatewayService, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)
		assertError func(*testing.T, string)
	}{
		{
			name: "messages ingress",
			path: "/v1/messages",
			body: []byte(`{
				"model":"future-model",
				"max_tokens":32,
				"messages":[{"role":"user","content":"hello"}],
				"stream":false
			}`),
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			},
			assertError: func(t *testing.T, responseBody string) {
				t.Helper()
				require.Equal(t, "error", gjson.Get(responseBody, "type").String())
				require.Equal(t, "invalid_request_error", gjson.Get(responseBody, "error.type").String())
			},
		},
		{
			name: "responses ingress",
			path: "/v1/responses",
			body: []byte(`{
				"model":"future-model",
				"input":"hello",
				"stream":false
			}`),
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.Forward(context.Background(), c, account, body)
			},
			assertError: func(t *testing.T, responseBody string) {
				t.Helper()
				require.Equal(t, "model_protocol_capability_error", gjson.Get(responseBody, "error.type").String())
				require.Equal(t, "model", gjson.Get(responseBody, "error.param").String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			body := append([]byte(nil), tt.body...)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, tt.path, body)

			result, err := tt.forward(service, c, newOpencodeGoGatewayTestAccount(nil), body)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			responseBody := recorder.Body.String()
			tt.assertError(t, responseBody)
			message := gjson.Get(responseBody, "error.message").String()
			require.Contains(t, message, `model "future-model"`)
			require.Contains(t, message, "not present in the audited model catalog")
			require.Zero(t, upstream.calls, "unknown models must be rejected before any upstream request")
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}
}

func TestOpencodeGoChatToResponsesStrictCapabilityValidator(t *testing.T) {
	resolved, err := resolveOpencodeGoForwardModel(newOpencodeGoGatewayTestAccount(nil), "grok-4.6", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolResponses, resolved.Spec.Protocol)

	rejected := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "unknown message role",
			body:       `{"model":"grok-4.6","messages":[{"role":"developer","content":"hello"}]}`,
			wantDetail: `message role "developer"`,
		},
		{
			name:       "assistant image part",
			body:       `{"model":"grok-4.6","messages":[{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`,
			wantDetail: `image content for role "assistant"`,
		},
		{
			name:       "message non preserved field",
			body:       `{"model":"grok-4.6","messages":[{"role":"user","content":"hello","name":"client-user"}]}`,
			wantDetail: `message field "name"`,
		},
		{
			name:       "non function tool",
			body:       `{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"custom","function":{"name":"exec","parameters":{"type":"object"}}}]}`,
			wantDetail: `tool type "custom"`,
		},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpencodeChatToResponsesBridge([]byte(tt.body), resolved)

			requireOpencodeGoCapabilityMismatch(t, err, OpencodeGoProtocolChat, OpencodeGoProtocolResponses, tt.wantDetail)
		})
	}

	standardFunctionBody := []byte(`{
		"model":"grok-4.6",
		"messages":[
			{"role":"user","content":"look up this value"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"id\":\"42\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"found"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","description":"look up a value","parameters":{"type":"object","properties":{"id":{"type":"string"}}},"strict":true}}],
		"tool_choice":"required"
	}`)
	t.Run("standard function remains allowed", func(t *testing.T) {
		require.NoError(t, validateOpencodeChatToResponsesBridge(standardFunctionBody, resolved), "standard Chat function definitions, calls, and results must remain bridgeable")
	})
}

func TestOpencodeGoResponsesToChatStrictCapabilityValidator(t *testing.T) {
	resolved, err := resolveOpencodeGoForwardModel(newOpencodeGoGatewayTestAccount(nil), "deepseek-v4-flash", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolChat, resolved.Spec.Protocol)

	rejected := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "reasoning summary",
			body:       `{"model":"deepseek-v4-flash","input":"hello","reasoning":{"effort":"low","summary":"auto"}}`,
			wantDetail: "reasoning.summary",
		},
		{
			name:       "reasoning unknown field",
			body:       `{"model":"deepseek-v4-flash","input":"hello","reasoning":{"effort":"low","trace":"private"}}`,
			wantDetail: `reasoning field "trace"`,
		},
		{
			name:       "single object content unknown part",
			body:       `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":{"type":"input_audio","audio":{"data":"AA=="}}}]}`,
			wantDetail: `content part type "input_audio"`,
		},
		{
			name:       "empty image url",
			body:       `{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":""}]}]}`,
			wantDetail: "input_image must contain a non-empty image URL",
		},
		{
			name:       "function tool non preserved field",
			body:       `{"model":"deepseek-v4-flash","input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"},"defer_loading":true}]}`,
			wantDetail: `function tool field "defer_loading"`,
		},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpencodeResponsesToChatBridge([]byte(tt.body), resolved)

			requireOpencodeGoCapabilityMismatch(t, err, OpencodeGoProtocolResponses, OpencodeGoProtocolChat, tt.wantDetail)
		})
	}

	standardFunctionAndUserMediaBody := []byte(`{
		"model":"deepseek-v4-flash",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"describe this image"},
				{"type":"input_image","image_url":"https://example.invalid/image.png"}
			]
		}],
		"tools":[{"type":"function","name":"lookup","description":"look up a value","parameters":{"type":"object","properties":{"id":{"type":"string"}}},"strict":true}],
		"tool_choice":"auto",
		"reasoning":{"effort":"low"}
	}`)
	t.Run("standard function and user media remain allowed", func(t *testing.T) {
		require.NoError(t, validateOpencodeResponsesToChatBridge(standardFunctionAndUserMediaBody, resolved), "standard Responses functions plus user text and image content must remain bridgeable")
	})
}

func TestOpencodeGoMessagesBridgeStrictCapabilityValidators(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(nil)
	chatResolved, err := resolveOpencodeGoForwardModel(account, "deepseek-v4-flash", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolChat, chatResolved.Spec.Protocol)
	responsesResolved, err := resolveOpencodeGoForwardModel(account, "grok-4.6", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolResponses, responsesResolved.Spec.Protocol)

	targets := []struct {
		name     string
		required OpencodeGoProtocol
		resolved OpencodeGoResolvedModel
		validate func([]byte, OpencodeGoResolvedModel) error
	}{
		{
			name:     "to chat",
			required: OpencodeGoProtocolChat,
			resolved: chatResolved,
			validate: validateOpencodeMessagesToChatBridge,
		},
		{
			name:     "to responses",
			required: OpencodeGoProtocolResponses,
			resolved: responsesResolved,
			validate: validateOpencodeMessagesToResponsesBridge,
		},
	}

	rejected := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "non empty metadata",
			body:       `{"model":"bridge-model","max_tokens":64,"metadata":{"user_id":"user-1"},"messages":[{"role":"user","content":"hello"}]}`,
			wantDetail: `field "metadata"`,
		},
		{
			name:       "non empty top k",
			body:       `{"model":"bridge-model","max_tokens":64,"top_k":40,"messages":[{"role":"user","content":"hello"}]}`,
			wantDetail: `field "top_k"`,
		},
		{
			name:       "unknown role",
			body:       `{"model":"bridge-model","max_tokens":64,"messages":[{"role":"developer","content":"hello"}]}`,
			wantDetail: `message role "developer"`,
		},
		{
			name:       "user tool use",
			body:       `{"model":"bridge-model","max_tokens":64,"messages":[{"role":"user","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]}]}`,
			wantDetail: `tool_use blocks for role "user"`,
		},
		{
			name:       "assistant tool result",
			body:       `{"model":"bridge-model","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"found"}]}]}`,
			wantDetail: `tool_result blocks for role "assistant"`,
		},
		{
			name:       "assistant image",
			body:       `{"model":"bridge-model","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`,
			wantDetail: `image blocks for role "assistant"`,
		},
		{
			name:       "tool result error semantics",
			body:       `{"model":"bridge-model","max_tokens":64,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"failed","is_error":true}]}]}`,
			wantDetail: "tool_result is_error semantics",
		},
		{
			name:       "output config unknown field",
			body:       `{"model":"bridge-model","max_tokens":64,"output_config":{"effort":"high","format":{"type":"json_schema"}},"messages":[{"role":"user","content":"hello"}]}`,
			wantDetail: `output_config field "format"`,
		},
	}

	standardBody := []byte(`{
		"model":"bridge-model",
		"max_tokens":128,
		"system":[{"type":"text","text":"You are helpful."}],
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"inspect this image"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}
			]},
			{"role":"assistant","content":[
				{"type":"text","text":"checking"},
				{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"id":"42"}}
			]},
			{"role":"user","content":[{
				"type":"tool_result",
				"tool_use_id":"toolu_1",
				"is_error":false,
				"content":[
					{"type":"text","text":"found"},
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AQ=="}}
				]
			}]}
		],
		"tools":[{
			"name":"lookup",
			"description":"look up a value",
			"input_schema":{"type":"object","properties":{"cache_control":{"type":"string"},"id":{"type":"string"}}}
		}],
		"tool_choice":{"type":"auto"},
		"output_config":{"effort":"high"}
	}`)

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			for _, tt := range rejected {
				t.Run(tt.name, func(t *testing.T) {
					err := target.validate([]byte(tt.body), target.resolved)

					requireOpencodeGoCapabilityMismatch(t, err, OpencodeGoProtocolMessages, target.required, tt.wantDetail)
				})
			}

			t.Run("standard content remains allowed", func(t *testing.T) {
				require.NoError(t, target.validate(standardBody, target.resolved), "standard Anthropic text, image, tool_use, tool_result, and function tools must remain bridgeable; input_schema business fields named cache_control are not protocol cache controls")
			})
		})
	}
}

func TestOpencodeGoCrossProtocolOutputTokenFloorValidation(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(nil)
	chatResolved, err := resolveOpencodeGoForwardModel(account, "deepseek-v4-flash", "")
	require.NoError(t, err)
	responsesResolved, err := resolveOpencodeGoForwardModel(account, "grok-4.6", "")
	require.NoError(t, err)

	targets := []struct {
		name     string
		field    string
		body     string
		inbound  OpencodeGoProtocol
		required OpencodeGoProtocol
		resolved OpencodeGoResolvedModel
		validate func([]byte, OpencodeGoResolvedModel) error
	}{
		{
			name:     "chat max tokens to responses",
			field:    "max_tokens",
			body:     `{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":%d}`,
			inbound:  OpencodeGoProtocolChat,
			required: OpencodeGoProtocolResponses,
			resolved: responsesResolved,
			validate: validateOpencodeChatToResponsesBridge,
		},
		{
			name:     "chat max completion tokens to responses",
			field:    "max_completion_tokens",
			body:     `{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":%d}`,
			inbound:  OpencodeGoProtocolChat,
			required: OpencodeGoProtocolResponses,
			resolved: responsesResolved,
			validate: validateOpencodeChatToResponsesBridge,
		},
		{
			name:     "messages max tokens to chat",
			field:    "max_tokens",
			body:     `{"model":"deepseek-v4-flash","max_tokens":%d,"messages":[{"role":"user","content":"hello"}]}`,
			inbound:  OpencodeGoProtocolMessages,
			required: OpencodeGoProtocolChat,
			resolved: chatResolved,
			validate: validateOpencodeMessagesToChatBridge,
		},
		{
			name:     "messages max tokens to responses",
			field:    "max_tokens",
			body:     `{"model":"grok-4.6","max_tokens":%d,"messages":[{"role":"user","content":"hello"}]}`,
			inbound:  OpencodeGoProtocolMessages,
			required: OpencodeGoProtocolResponses,
			resolved: responsesResolved,
			validate: validateOpencodeMessagesToResponsesBridge,
		},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			for _, limit := range []int{1, minMaxOutputTokens - 1, minMaxOutputTokens, minMaxOutputTokens + 1} {
				t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
					err := target.validate([]byte(fmt.Sprintf(target.body, limit)), target.resolved)
					if limit >= minMaxOutputTokens {
						require.NoError(t, err, "the exact supported boundary must remain bridgeable")
						return
					}

					requireOpencodeGoCapabilityMismatch(t, err, target.inbound, target.required, fmt.Sprintf("would be increased to %d", minMaxOutputTokens))
					var routingErr *opencodeGoRoutingError
					require.ErrorAs(t, err, &routingErr)
					require.Contains(t, routingErr.detail, fmt.Sprintf("field %q value %d", target.field, limit))
				})
			}
		})
	}
}

func TestOpencodeGoLowOutputLimitsRejectBeforeUpstream(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		body      []byte
		wantField string
		forward   func(*OpenAIGatewayService, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)
	}{
		{
			name:      "chat to responses",
			path:      "/v1/chat/completions",
			body:      []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":127}`),
			wantField: "max_completion_tokens",
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			},
		},
		{
			name:      "messages to chat",
			path:      "/v1/messages",
			body:      []byte(`{"model":"deepseek-v4-flash","max_tokens":127,"messages":[{"role":"user","content":"hello"}]}`),
			wantField: "max_tokens",
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			},
		},
		{
			name:      "messages to responses",
			path:      "/v1/messages",
			body:      []byte(`{"model":"grok-4.6","max_tokens":1,"messages":[{"role":"user","content":"hello"}]}`),
			wantField: "max_tokens",
			forward: func(service *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return service.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &opencodeGoGatewayNoCallUpstream{}
			service := newOpencodeGoGatewayTestService(upstream)
			body := append([]byte(nil), tt.body...)
			c, recorder := newOpencodeGoGatewayContext(http.MethodPost, tt.path, body)

			result, err := tt.forward(service, c, newOpencodeGoGatewayTestAccount(nil), body)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			message := gjson.Get(recorder.Body.String(), "error.message").String()
			require.Contains(t, message, fmt.Sprintf("field %q", tt.wantField))
			require.Contains(t, message, fmt.Sprintf("would be increased to %d", minMaxOutputTokens))
			require.Zero(t, upstream.calls)
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
		})
	}
}

func TestOpencodeGoResponsesCompactRejectedBeforeUpstreamWhileBarePathSucceeds(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(map[string]any{
		"client-responses-alias": "opencode-go/grok-4.6[1m]",
	})
	body := []byte(`{
		"model":"client-responses-alias",
		"input":"hello",
		"stream":false
	}`)

	t.Run("compact path is rejected locally", func(t *testing.T) {
		upstream := &opencodeGoGatewayNoCallUpstream{}
		service := newOpencodeGoGatewayTestService(upstream)
		c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses/compact", body)

		result, err := service.Forward(context.Background(), c, account, body)

		require.Error(t, err)
		require.Nil(t, result)
		require.GreaterOrEqual(t, recorder.Code, http.StatusBadRequest)
		require.Less(t, recorder.Code, http.StatusInternalServerError)
		require.NotEmpty(t, gjson.Get(recorder.Body.String(), "error.type").String(), "compact rejection must use an explicit client-facing error envelope")
		require.Equal(t, "path", gjson.Get(recorder.Body.String(), "error.param").String())
		message := gjson.Get(recorder.Body.String(), "error.message").String()
		lowerMessage := strings.ToLower(message)
		require.Contains(t, message, `request path "/v1/responses/compact"`)
		require.Contains(t, lowerMessage, "use /v1/responses")
		require.Contains(t, lowerMessage, "opencode go")
		require.Zero(t, upstream.calls, "unsupported OpenCode Go compact requests must be rejected before the HTTP upstream")
		require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
	})

	t.Run("bare responses path still succeeds", func(t *testing.T) {
		upstream := &opencodeGoGatewayCaptureUpstream{}
		service := newOpencodeGoGatewayTestService(upstream)
		c, recorder := newOpencodeGoGatewayContext(http.MethodPost, "/v1/responses", body)

		result, err := service.Forward(context.Background(), c, account, body)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, 1, upstream.calls)
		require.NotNil(t, upstream.req)
		require.Equal(t, "/zen/go/v1/responses", upstream.req.URL.Path)
		require.Equal(t, "grok-4.6", gjson.GetBytes(upstream.body, "model").String())
		require.Equal(t, opencodeResponsesRawEndpoint, GetActualOpenAIUpstreamEndpoint(c))
	})
}

func TestOpencodeGoUnsupportedTopLevelFieldSelectionIsDeterministic(t *testing.T) {
	account := newOpencodeGoGatewayTestAccount(nil)
	responsesResolved, err := resolveOpencodeGoForwardModel(account, "grok-4.6", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolResponses, responsesResolved.Spec.Protocol)
	chatResolved, err := resolveOpencodeGoForwardModel(account, "deepseek-v4-flash", "")
	require.NoError(t, err)
	require.Equal(t, OpencodeGoProtocolChat, chatResolved.Spec.Protocol)

	tests := []struct {
		name       string
		body       []byte
		inbound    OpencodeGoProtocol
		required   OpencodeGoProtocol
		resolved   OpencodeGoResolvedModel
		validate   func([]byte, OpencodeGoResolvedModel) error
		wantDetail string
	}{
		{
			name:       "chat to responses",
			body:       []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"user":"client-1","store":true,"frequency_penalty":0.5}`),
			inbound:    OpencodeGoProtocolChat,
			required:   OpencodeGoProtocolResponses,
			resolved:   responsesResolved,
			validate:   validateOpencodeChatToResponsesBridge,
			wantDetail: `Chat Completions field "frequency_penalty" is not preserved by the Responses compatibility bridge`,
		},
		{
			name:       "responses to chat",
			body:       []byte(`{"model":"deepseek-v4-flash","input":"hello","store":true,"metadata":{"trace":"keep"},"background":true}`),
			inbound:    OpencodeGoProtocolResponses,
			required:   OpencodeGoProtocolChat,
			resolved:   chatResolved,
			validate:   validateOpencodeResponsesToChatBridge,
			wantDetail: `Responses field "background" is not preserved by the Chat Completions compatibility bridge`,
		},
	}

	const repetitions = 128
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observedDetails := make(map[string]int)
			for iteration := 0; iteration < repetitions; iteration++ {
				err := tt.validate(tt.body, tt.resolved)
				requireOpencodeGoCapabilityMismatch(t, err, tt.inbound, tt.required, tt.wantDetail)

				var routingErr *opencodeGoRoutingError
				require.ErrorAs(t, err, &routingErr)
				require.Equal(t, tt.wantDetail, routingErr.detail, "unsupported top-level field selection must use the lexicographically first field on every invocation")
				observedDetails[routingErr.detail]++
			}
			require.Equal(t, map[string]int{tt.wantDetail: repetitions}, observedDetails)
		})
	}
}
