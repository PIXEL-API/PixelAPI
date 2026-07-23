package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesRejectedFieldRetryStateRejectsDuplicateBodyAndCap(t *testing.T) {
	initialBody := []byte(`{"model":"gpt-5.5"}`)
	state := newOpenAIResponsesRejectedFieldRetryState(initialBody)

	require.False(t, state.Allow(initialBody))
	for attempt := 0; attempt < maxOpenAIResponsesRejectedFieldRetries; attempt++ {
		nextBody := []byte(fmt.Sprintf(`{"model":"gpt-5.5","variant":%d}`, attempt))
		require.True(t, state.Allow(nextBody))
		require.False(t, state.Allow(nextBody))
	}
	require.False(t, state.Allow([]byte(`{"model":"gpt-5.5","variant":"overflow"}`)))
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRejectsAmbiguousErrors(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		responseBody []byte
	}{
		{
			name:         "namespace belongs to message",
			body:         []byte(`{"input":[{"type":"message","namespace":"keep"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].namespace'.","param":"input[0].namespace"}}`),
		},
		{
			name:         "parameter is invalid but not unsupported",
			body:         []byte(`{"max_output_tokens":4096}`),
			responseBody: []byte(`{"error":{"code":"invalid_request_error","message":"max_output_tokens must be positive","param":"max_output_tokens"}}`),
		},
		{
			name:         "structured param overrides message",
			body:         []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: input[0].namespace","param":"tools"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, tt.responseBody)
			require.NoError(t, err)
			require.False(t, changed)
			require.Nil(t, retryBody)
		})
	}
}

func TestOpenAIGatewayServiceRetriesExplicitlyRejectedResponsesFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"max_output_tokens":2048,"input":[{"type":"function_call","name":"first","namespace":"keep","arguments":"{}"},{"type":"custom_tool_call","name":"second","namespace":"remove","input":"{}"}]}`)
	upstream := &openAIRejectedFieldUpstream{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[1].namespace'.","param":"input[1].namespace"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens","param":"max_output_tokens"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 3)
	require.True(t, gjson.GetBytes(upstream.bodies[0], "input.1.namespace").Exists())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "input.1.namespace").Exists())
	require.Equal(t, int64(2048), gjson.GetBytes(upstream.bodies[1], "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.bodies[2], "input.1.namespace").Exists())
	require.False(t, gjson.GetBytes(upstream.bodies[2], "max_output_tokens").Exists())
}

func newOpenAIRejectedFieldTestService(upstream *openAIRejectedFieldUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
		httpUpstream: upstream,
	}
}

type openAIRejectedFieldUpstream struct {
	responses []*http.Response
	bodies    [][]byte
}

func (u *openAIRejectedFieldUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	if len(u.responses) == 0 {
		return nil, fmt.Errorf("unexpected upstream request %d", len(u.bodies))
	}
	response := u.responses[0]
	u.responses = u.responses[1:]
	return response, nil
}

func (u *openAIRejectedFieldUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func newOpenAIRejectedFieldTestContext(body []byte) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "curl/8.0")
	return c
}

func newOpenAIRejectedFieldTestAccount() *Account {
	return &Account{
		ID:          5107,
		Name:        "responses-compatible",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat.example",
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newOpenAIRejectedFieldTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
