//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func grokSearchBillingTestAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "grok-search-billing",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "grok-search-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
}

func grokSearchBillingUpstreamResponse(searchOutput string) *http.Response {
	body := "data: {\"type\":\"response.output_item.done\",\"item\":" + searchOutput + "}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_search\",\"object\":\"response\",\"model\":\"grok-4.5\",\"status\":\"completed\",\"output\":[" + searchOutput + "," +
		"{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]," +
		"\"usage\":{\"input_tokens\":5,\"output_tokens\":2,\"total_tokens\":7}}}\n\n" +
		"data: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_grok_search"},
		},
		Body: io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestForwardGrokChatPropagatesDeduplicatedSearchCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestBody := []byte(`{"model":"grok","messages":[{"role":"user","content":"search"}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))
	searchOutput := `{"type":"x_search_call","id":"xs1","call_id":"search_1","status":"completed"}`
	service := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{
		resp: grokSearchBillingUpstreamResponse(searchOutput),
	}}

	result, err := service.forwardGrokChatCompletions(
		context.Background(), c, grokSearchBillingTestAccount(9101), requestBody,
		"grok", "grok", "grok-4.5", false, false, time.Now(),
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.SearchCount, "item.done and response.completed must charge one search")
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "ok")
}

func TestForwardGrokMessagesBufferedPropagatesSearchCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestBody := []byte(`{"model":"grok","max_tokens":32,"stream":false,"messages":[{"role":"user","content":"search"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(requestBody))
	searchOutput := `{"type":"web_search_call","id":"ws1","call_id":"search_1","status":"completed"}`
	service := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{
		resp: grokSearchBillingUpstreamResponse(searchOutput),
	}}

	result, err := service.ForwardAsAnthropic(
		context.Background(), c, grokSearchBillingTestAccount(9102), requestBody, "", "",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.SearchCount)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Contains(t, recorder.Body.String(), "ok")
}

func TestForwardGrokMessagesStreamingDeduplicatesSearchCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestBody := []byte(`{"model":"grok","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"search"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(requestBody))
	searchOutput := `{"type":"tool_search_call","id":"ts1","call_id":"search_1","status":"completed"}`
	service := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{
		resp: grokSearchBillingUpstreamResponse(searchOutput),
	}}

	result, err := service.ForwardAsAnthropic(
		context.Background(), c, grokSearchBillingTestAccount(9103), requestBody, "", "",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.SearchCount)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Contains(t, recorder.Body.String(), "message_stop")
}

func TestPatchGrokResponsesPreservesNativeSearchToolsAndFilters(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"model":"grok",
		"input":"search",
		"tools":[
			{"type":"web_search"},
			{"type":"x_search","allowed_x_handles":["xai"],"excluded_x_handles":["spam"],"from_date":"2026-08-01","to_date":"2026-08-14","enable_image_understanding":true,"enable_video_understanding":false}
		],
		"tool_choice":{"type":"x_search"}
	}`)

	patched, err := patchGrokResponsesBody(body, "grok-4.5")

	require.NoError(t, err)
	require.Equal(t, "web_search", gjson.GetBytes(patched, "tools.0.type").String())
	require.Equal(t, "x_search", gjson.GetBytes(patched, "tools.1.type").String())
	require.Equal(t, "xai", gjson.GetBytes(patched, "tools.1.allowed_x_handles.0").String())
	require.Equal(t, "spam", gjson.GetBytes(patched, "tools.1.excluded_x_handles.0").String())
	require.Equal(t, "2026-08-01", gjson.GetBytes(patched, "tools.1.from_date").String())
	require.Equal(t, "2026-08-14", gjson.GetBytes(patched, "tools.1.to_date").String())
	require.True(t, gjson.GetBytes(patched, "tools.1.enable_image_understanding").Bool())
	require.False(t, gjson.GetBytes(patched, "tools.1.enable_video_understanding").Bool())
	require.Equal(t, "x_search", gjson.GetBytes(patched, "tool_choice.type").String())
}
