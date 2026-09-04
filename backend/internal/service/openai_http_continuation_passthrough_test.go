package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIHTTPContinuation_PassthroughSuccessBindsResponseOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		stream      bool
		contentType string
		responseID  string
		body        string
	}{
		{
			name:        "non-streaming JSON",
			contentType: "application/json",
			responseID:  "resp_http_passthrough_json",
			body:        `{"id":"resp_http_passthrough_json","usage":{"input_tokens":1,"output_tokens":2}}`,
		},
		{
			name:        "non-streaming SSE converted to JSON",
			contentType: "text/event-stream",
			responseID:  "resp_http_passthrough_sse_json",
			body: strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp_http_passthrough_sse_json"}}`,
				`data: {"type":"response.completed","response":{"id":"resp_http_passthrough_sse_json","usage":{"input_tokens":1,"output_tokens":2}}}`,
				`data: [DONE]`,
			}, "\n\n"),
		},
		{
			name:        "streaming SSE",
			stream:      true,
			contentType: "text/event-stream",
			responseID:  "resp_http_passthrough_stream",
			body: strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp_http_passthrough_stream"}}`,
				`data: {"type":"response.completed","response":{"id":"resp_http_passthrough_stream","usage":{"input_tokens":1,"output_tokens":2}}}`,
				`data: [DONE]`,
			}, "\n\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groupID := int64(73)
			userID := int64(501)
			apiKeyID := int64(601)
			accountID := int64(701)
			cache := &stubGatewayCache{isolateByGroup: true}
			stateStore := NewOpenAIWSStateStore(cache)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{test.contentType}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}}
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.MaxLineSize = defaultMaxLineSize
			svc := &OpenAIGatewayService{
				cfg:                cfg,
				cache:              cache,
				httpUpstream:       upstream,
				openaiWSStateStore: stateStore,
			}
			account := &Account{
				ID:          accountID,
				Name:        "openai-api-key",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key":  "sk-test",
					"base_url": "http://upstream.test",
				},
			}

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
			setOpenAIEffectiveGroupID(c, &groupID)
			SetOpenAIHTTPResponseOwner(c, userID, apiKeyID)
			requestBody := []byte(`{"model":"gpt-5.1","input":"hello","stream":` + boolString(test.stream) + `}`)

			result, err := svc.forwardOpenAIPassthroughWithOptions(
				context.Background(),
				c,
				account,
				requestBody,
				"gpt-5.1",
				nil,
				test.stream,
				time.Now(),
				openAIPassthroughOptions{},
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, test.responseID, result.ResponseID)

			owner, found, ownerErr := stateStore.GetHTTPResponseOwnerStrict(context.Background(), groupID, test.responseID)
			require.NoError(t, ownerErr)
			require.True(t, found)
			require.Equal(t, OpenAIHTTPResponseOwner{
				Version:   openAIHTTPResponseOwnerVersion,
				UserID:    userID,
				APIKeyID:  apiKeyID,
				GroupID:   groupID,
				AccountID: accountID,
			}, owner)

			boundAccountID, bindingErr := stateStore.GetResponseAccountStrict(context.Background(), groupID, test.responseID)
			require.NoError(t, bindingErr)
			require.Equal(t, accountID, boundAccountID)
		})
	}
}

func TestOpenAIHTTPContinuation_PassthroughFailureDoesNotBindResponseOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const responseID = "resp_http_passthrough_failed"
	groupID := int64(73)
	cache := &stubGatewayCache{isolateByGroup: true}
	stateStore := NewOpenAIWSStateStore(cache)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_http_passthrough_failed"}}`,
			`data: {"type":"response.failed","response":{"id":"resp_http_passthrough_failed","error":{"message":"failed"}}}`,
		}, "\n\n"))),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxLineSize = defaultMaxLineSize
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		cache:              cache,
		httpUpstream:       upstream,
		openaiWSStateStore: stateStore,
	}
	account := &Account{
		ID:          701,
		Name:        "openai-api-key",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.test",
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	setOpenAIEffectiveGroupID(c, &groupID)
	SetOpenAIHTTPResponseOwner(c, 501, 601)

	result, err := svc.forwardOpenAIPassthroughWithOptions(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"gpt-5.1","input":"hello","stream":true}`),
		"gpt-5.1",
		nil,
		true,
		time.Now(),
		openAIPassthroughOptions{},
	)
	require.Error(t, err)
	require.Nil(t, result)

	_, found, ownerErr := stateStore.GetHTTPResponseOwnerStrict(context.Background(), groupID, responseID)
	require.NoError(t, ownerErr)
	require.False(t, found)
	boundAccountID, bindingErr := stateStore.GetResponseAccountStrict(context.Background(), groupID, responseID)
	require.NoError(t, bindingErr)
	require.Zero(t, boundAccountID)
}

func TestOpenAIHTTPContinuation_PassthroughFailedJSONDoesNotBindResponseOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const responseID = "resp_http_passthrough_failed_json"
	groupID := int64(73)
	cache := &stubGatewayCache{isolateByGroup: true}
	stateStore := NewOpenAIWSStateStore(cache)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_http_passthrough_failed_json","status":"failed","error":{"message":"failed"}}`,
		)),
	}}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxLineSize = defaultMaxLineSize
	svc := &OpenAIGatewayService{
		cfg:                cfg,
		cache:              cache,
		httpUpstream:       upstream,
		openaiWSStateStore: stateStore,
	}
	account := &Account{
		ID:          701,
		Name:        "openai-api-key",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.test",
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	setOpenAIEffectiveGroupID(c, &groupID)
	SetOpenAIHTTPResponseOwner(c, 501, 601)

	result, err := svc.forwardOpenAIPassthroughWithOptions(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"gpt-5.1","input":"hello","stream":false}`),
		"gpt-5.1",
		nil,
		false,
		time.Now(),
		openAIPassthroughOptions{},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.ResponseID)

	_, found, ownerErr := stateStore.GetHTTPResponseOwnerStrict(context.Background(), groupID, responseID)
	require.NoError(t, ownerErr)
	require.False(t, found)
	boundAccountID, bindingErr := stateStore.GetResponseAccountStrict(context.Background(), groupID, responseID)
	require.NoError(t, bindingErr)
	require.Zero(t, boundAccountID)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestExtractOpenAIResponseIDFromJSONBytesPrefersResponseOverEventID(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "created event prefers nested response id",
			body: `{"type":"response.created","id":"evt_2","response":{"id":"resp_2"}}`,
			want: "resp_2",
		},
		{
			name: "non terminal event id is not a response id",
			body: `{"type":"response.delta","id":"evt_3"}`,
		},
		{
			name: "terminal compatibility envelope permits top level response id",
			body: `{"type":"response.completed","id":"resp_4"}`,
			want: "resp_4",
		},
		{
			name: "bare response object uses top level id",
			body: `{"id":"resp_5","status":"completed"}`,
			want: "resp_5",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, extractOpenAIResponseIDFromJSONBytes([]byte(test.body)))
		})
	}
}

func TestExtractOpenAIHTTPContinuationResponseIDRejectsFailedResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "completed", body: `{"id":"resp_completed","status":"completed"}`, want: "resp_completed"},
		{name: "incomplete remains bindable", body: `{"id":"resp_incomplete","status":"incomplete"}`, want: "resp_incomplete"},
		{name: "top level failed", body: `{"id":"resp_failed","status":"failed"}`},
		{name: "nested failed", body: `{"response":{"id":"resp_failed_nested","status":"failed"}}`},
		{name: "failed event", body: `{"type":"response.failed","response":{"id":"resp_failed_event"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, extractOpenAIHTTPContinuationResponseIDFromJSONBytes([]byte(test.body)))
		})
	}
}
