package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func newCompactBodySignalTestContext(t *testing.T, path string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestNormalizeOpenAIResponsesCompactRequestRemoteV2StaysOnResponses(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"stream":true,
		"store":true,
		"prompt_cache_key":"pck-signal-1",
		"reasoning":{"effort":"max","context":"all_turns"},
		"input":[
			{"type":"message","role":"user","content":"hello"},
			{"type":"compaction_trigger"}
		]
	}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses", body)
	c.Request.Header.Set("x-codex-beta-features", "responses_websockets_v2, remote_compaction_v2, another_feature")

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses", c.Request.URL.Path)
	require.False(t, isOpenAIRemoteCompactPath(c))
	require.Equal(t, body, normalized)
	require.True(t, gjson.GetBytes(normalized, "stream").Bool())
	require.True(t, gjson.GetBytes(normalized, "store").Bool())
	require.Equal(t, "pck-signal-1", gjson.GetBytes(normalized, "prompt_cache_key").String())
	require.Equal(t, "max", gjson.GetBytes(normalized, "reasoning.effort").String())
	_, seedExists := c.Get(service.OpenAICompactSessionSeedKeyForTest())
	require.False(t, seedExists)
	_, streamMarkerExists := c.Get(service.OpenAICompactClientStreamKeyForTest())
	require.False(t, streamMarkerExists)
}

func TestNormalizeOpenAIResponsesCompactRequestRemoteV2AliasesStayOnResponses(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`)
	for _, path := range []string{"/v1/responses/", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			c := newCompactBodySignalTestContext(t, path, body)
			c.Request.Header.Set("x-codex-beta-features", "remote_compaction_v2")
			normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
			require.True(t, ok)
			require.Equal(t, path, c.Request.URL.Path)
			require.Equal(t, body, normalized)
		})
	}
}

func TestNormalizeOpenAIResponsesCompactRequestNonRemoteV2PromotesLegacyWire(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	tests := []struct {
		name       string
		body       []byte
		betaHeader string
		wantMarked bool
	}{
		{name: "no_header", body: []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"compaction_trigger"}]}`), wantMarked: true},
		{name: "unrelated_header", body: []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"compaction_trigger"}]}`), betaHeader: "responses_websockets_v2", wantMarked: true},
		{name: "wrong_case_feature", body: []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"compaction_trigger"}]}`), betaHeader: "REMOTE_COMPACTION_V2", wantMarked: true},
		{name: "stream_false", body: []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"compaction_trigger"}]}`), betaHeader: "remote_compaction_v2"},
		{name: "stream_absent", body: []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`), betaHeader: "remote_compaction_v2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newCompactBodySignalTestContext(t, "/v1/responses", test.body)
			if test.betaHeader != "" {
				c.Request.Header.Set("x-codex-beta-features", test.betaHeader)
			}
			normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), test.body)
			require.True(t, ok)
			require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
			require.False(t, gjson.GetBytes(normalized, "stream").Exists())
			marked, exists := c.Get(service.OpenAICompactClientStreamKeyForTest())
			require.Equal(t, test.wantMarked, exists)
			if test.wantMarked {
				require.Equal(t, true, marked)
			}
		})
	}
}

func TestNormalizeOpenAIResponsesCompactRequestSubpathAndNoTriggerUntouched(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	tests := []struct {
		path string
		body []byte
	}{
		{path: "/v1/responses/resp_123/cancel", body: []byte(`{"model":"gpt-5.5","input":[{"type":"compaction_trigger"}]}`)},
		{path: "/v1/responses", body: []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)},
	}
	for _, test := range tests {
		c := newCompactBodySignalTestContext(t, test.path, test.body)
		normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), test.body)
		require.True(t, ok)
		require.Equal(t, test.path, c.Request.URL.Path)
		require.Equal(t, test.body, normalized)
	}
}

func TestNormalizeOpenAIResponsesCompactRequestPathBasedDoesNotDoubleSuffix(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	body := []byte(`{"model":"gpt-5.5","stream":true,"store":true,"input":[{"type":"message","role":"user","content":"hello"}]}`)
	c := newCompactBodySignalTestContext(t, "/v1/responses/compact", body)
	c.Request.Header.Set("x-codex-beta-features", "remote_compaction_v2")

	normalized, ok := h.normalizeOpenAIResponsesCompactRequest(c, zap.NewNop(), body)
	require.True(t, ok)
	require.Equal(t, "/v1/responses/compact", c.Request.URL.Path)
	require.False(t, gjson.GetBytes(normalized, "stream").Exists())
	require.False(t, gjson.GetBytes(normalized, "store").Exists())
	_, streamMarkerExists := c.Get(service.OpenAICompactClientStreamKeyForTest())
	require.False(t, streamMarkerExists)
}
