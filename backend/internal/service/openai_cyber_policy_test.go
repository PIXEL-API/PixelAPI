package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDetectOpenAICyberPolicy(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantHit     bool
		wantMessage string
	}{
		{
			name:        "top level error code",
			payload:     `{"error":{"code":"cyber_policy","message":"blocked by policy"}}`,
			wantHit:     true,
			wantMessage: "blocked by policy",
		},
		{
			name:        "websocket response error code",
			payload:     `{"type":"response.failed","response":{"error":{"code":"CYBER_POLICY","message":"blocked websocket turn"}}}`,
			wantHit:     true,
			wantMessage: "blocked websocket turn",
		},
		{
			name:    "ordinary upstream error",
			payload: `{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`,
			wantHit: false,
		},
		{
			name:    "cyber policy text without structured code",
			payload: `{"error":{"code":"invalid_request_error","message":"cyber_policy"}}`,
			wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, code, message := detectOpenAICyberPolicy([]byte(tt.payload))
			require.Equal(t, tt.wantHit, hit)
			if !tt.wantHit {
				require.Empty(t, code)
				require.Empty(t, message)
				return
			}
			require.Equal(t, "cyber_policy", code)
			require.Equal(t, tt.wantMessage, message)
		})
	}
}

func TestCyberPolicyMark_IsScopedToUpstreamAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	BeginOpenAIUpstreamAttempt(c, "attempt-1", true)
	MarkOpsCyberPolicy(c, CyberPolicyMark{Message: "first", UpstreamStatus: 400})

	first := GetOpsCyberPolicyForAttempt(c, "attempt-1")
	require.NotNil(t, first)
	require.Equal(t, "first", first.Message)
	require.Nil(t, GetOpsCyberPolicyForAttempt(c, "attempt-2"))

	BeginOpenAIUpstreamAttempt(c, "attempt-2", true)
	MarkOpsCyberPolicy(c, CyberPolicyMark{Message: "second", UpstreamStatus: 403})

	require.Nil(t, GetOpsCyberPolicyForAttempt(c, "attempt-1"))
	second := GetOpsCyberPolicyForAttempt(c, "attempt-2")
	require.NotNil(t, second)
	require.Equal(t, "second", second.Message)
	require.Equal(t, 403, second.UpstreamStatus)
}

func TestCyberPolicyMark_FirstMarkWinsWithinSameAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	BeginOpenAIUpstreamAttempt(c, "attempt-1", true)
	MarkOpsCyberPolicy(c, CyberPolicyMark{Message: "first", Body: "first body"})
	MarkOpsCyberPolicy(c, CyberPolicyMark{Message: "duplicate", Body: "duplicate body"})

	mark := GetOpsCyberPolicyForAttempt(c, "attempt-1")
	require.NotNil(t, mark)
	require.Equal(t, "first", mark.Message)
	require.Equal(t, "first body", mark.Body)
}

func TestCyberPolicyMark_RequiresEnforcedCurrentAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	payload := []byte(`{"error":{"code":"cyber_policy","message":"blocked"}}`)

	BeginOpenAIUpstreamAttempt(c, "attempt-unselected", false)
	require.False(t, markOpsCyberPolicyPayload(c, payload, 400, 0, 0))
	require.Nil(t, GetOpsCyberPolicy(c))

	BeginOpenAIUpstreamAttempt(c, "attempt-selected", true)
	require.True(t, markOpsCyberPolicyPayload(c, payload, 400, 0, 0))
	mark := GetOpsCyberPolicyForAttempt(c, "attempt-selected")
	require.NotNil(t, mark)
	require.Equal(t, "blocked", mark.Message)

	BeginOpenAIUpstreamAttempt(c, "attempt-next-unselected", false)
	require.Nil(t, GetOpsCyberPolicy(c), "a new attempt must not inherit the previous selected route mark")
}

func TestCyberPolicyMark_PersistsSearchableOpsContextForEnforcedAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	responseBody := `{"error":{"code":"cyber_policy","message":"blocked upstream"}}`

	BeginOpenAIUpstreamAttempt(c, "attempt-selected", true)
	MarkOpsCyberPolicy(c, CyberPolicyMark{
		Message:        "blocked upstream",
		Body:           responseBody,
		UpstreamStatus: http.StatusOK,
	})

	message, ok := c.Get(OpsUpstreamErrorMessageKey)
	require.True(t, ok)
	require.Equal(t, "cyber_policy: blocked upstream", message)
	detail, ok := c.Get(OpsUpstreamErrorDetailKey)
	require.True(t, ok)
	require.Equal(t, responseBody, detail)
	_, hasEvents := c.Get(OpsUpstreamErrorsKey)
	require.False(t, hasEvents, "the central mark must not duplicate protocol-specific upstream events")

	MarkOpsCyberPolicy(c, CyberPolicyMark{Message: "duplicate", Body: "duplicate"})
	message, _ = c.Get(OpsUpstreamErrorMessageKey)
	detail, _ = c.Get(OpsUpstreamErrorDetailKey)
	require.Equal(t, "cyber_policy: blocked upstream", message)
	require.Equal(t, responseBody, detail)
}

func TestCyberPolicyMark_DoesNotPersistOpsContextForUnselectedAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	BeginOpenAIUpstreamAttempt(c, "attempt-unselected", false)
	MarkOpsCyberPolicy(c, CyberPolicyMark{
		Message:        "blocked upstream",
		Body:           `{"error":{"code":"cyber_policy"}}`,
		UpstreamStatus: http.StatusOK,
	})

	require.Nil(t, GetOpsCyberPolicy(c))
	_, hasMessage := c.Get(OpsUpstreamErrorMessageKey)
	_, hasDetail := c.Get(OpsUpstreamErrorDetailKey)
	_, hasEvents := c.Get(OpsUpstreamErrorsKey)
	require.False(t, hasMessage)
	require.False(t, hasDetail)
	require.False(t, hasEvents)
}

func newCyberPolicyProtocolTestContext(t *testing.T, enforced bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	BeginOpenAIUpstreamAttempt(c, "attempt-protocol", enforced)
	return c, rec
}

func TestCyberPolicyHTTPJSONUsesAttemptGroupGate(t *testing.T) {
	payload := `{"error":{"code":"cyber_policy","message":"blocked by selected policy"}}`
	run := func(c *gin.Context) (string, error) {
		resp := &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(payload)),
		}
		var writtenType string
		_, err := (&OpenAIGatewayService{}).handleCompatErrorResponse(
			resp,
			c,
			&Account{ID: 1, Platform: PlatformOpenAI, Name: "account"},
			"gpt-5",
			func(_ *gin.Context, _ int, errType, _ string) { writtenType = errType },
		)
		return writtenType, err
	}
	baselineRecorder := httptest.NewRecorder()
	baselineContext, _ := gin.CreateTestContext(baselineRecorder)
	baselineContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	baselineType, baselineErr := run(baselineContext)
	require.Error(t, baselineErr)
	require.NotContains(t, baselineErr.Error(), "openai cyber_policy")

	for _, enforced := range []bool{false, true} {
		t.Run(map[bool]string{false: "unselected", true: "selected"}[enforced], func(t *testing.T) {
			c, _ := newCyberPolicyProtocolTestContext(t, enforced)
			writtenType, err := run(c)
			require.Error(t, err)
			if enforced {
				require.Contains(t, err.Error(), "openai cyber_policy")
				require.Equal(t, "invalid_request_error", writtenType)
				require.NotNil(t, GetOpsCyberPolicyForAttempt(c, "attempt-protocol"))
				return
			}
			require.Equal(t, baselineErr.Error(), err.Error(), "an unselected group must retain the ordinary error path")
			require.Equal(t, baselineType, writtenType)
			require.Nil(t, GetOpsCyberPolicy(c))
		})
	}
}

func TestCyberPolicyCompatSSEUsesAttemptGroupGate(t *testing.T) {
	payload := `data: {"type":"response.failed","response":{"id":"resp_cyber","status":"failed","error":{"code":"cyber_policy","message":"blocked SSE"},"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"
	type runner func(*OpenAIGatewayService, context.Context, *http.Response, *gin.Context, *Account) (*OpenAIForwardResult, error)
	runners := map[string]runner{
		"chat": func(s *OpenAIGatewayService, ctx context.Context, resp *http.Response, c *gin.Context, account *Account) (*OpenAIForwardResult, error) {
			return s.handleChatStreamingResponse(ctx, resp, c, account, "gpt-5", "gpt-5", "gpt-5", false, time.Now())
		},
		"messages": func(s *OpenAIGatewayService, ctx context.Context, resp *http.Response, c *gin.Context, account *Account) (*OpenAIForwardResult, error) {
			return s.handleAnthropicStreamingResponse(ctx, resp, c, account, "claude", "gpt-5", "gpt-5", time.Now())
		},
	}
	for protocol, run := range runners {
		for _, enforced := range []bool{false, true} {
			t.Run(protocol+map[bool]string{false: "_unselected", true: "_selected"}[enforced], func(t *testing.T) {
				c, _ := newCyberPolicyProtocolTestContext(t, enforced)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(payload)),
				}
				_, err := run(&OpenAIGatewayService{}, c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Name: "account"})
				require.Error(t, err)
				if enforced {
					require.Contains(t, err.Error(), "openai cyber_policy")
					require.NotNil(t, GetOpsCyberPolicyForAttempt(c, "attempt-protocol"))
					return
				}
				require.NotContains(t, err.Error(), "openai cyber_policy")
				require.Nil(t, GetOpsCyberPolicy(c))
			})
		}
	}
}

func TestCyberPolicyResponsesSSEUsesAttemptGroupGate(t *testing.T) {
	payload := strings.Join([]string{
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_cyber","status":"failed","error":{"code":"cyber_policy","message":"blocked Responses"}}}`,
		"",
	}, "\n")
	for _, enforced := range []bool{false, true} {
		t.Run(map[bool]string{false: "unselected", true: "selected"}[enforced], func(t *testing.T) {
			c, _ := newCyberPolicyProtocolTestContext(t, enforced)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(payload)),
			}
			_, err := (&OpenAIGatewayService{}).handleStreamingResponsePassthrough(
				c.Request.Context(),
				resp,
				c,
				&Account{ID: 1, Platform: PlatformOpenAI, Name: "account"},
				time.Now(),
				"gpt-5",
				"gpt-5",
			)
			require.Error(t, err)
			if enforced {
				require.NotNil(t, GetOpsCyberPolicyForAttempt(c, "attempt-protocol"))
				return
			}
			require.Nil(t, GetOpsCyberPolicy(c))
		})
	}
}

type cyberPolicyFrameConnStub struct {
	payload []byte
}

func (c *cyberPolicyFrameConnStub) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	return coderws.MessageText, c.payload, nil
}

func (c *cyberPolicyFrameConnStub) WriteFrame(context.Context, coderws.MessageType, []byte) error {
	return nil
}

func (c *cyberPolicyFrameConnStub) Close() error { return nil }

func TestCyberPolicyWebSocketUsesAttemptGroupGate(t *testing.T) {
	payload := []byte(`{"type":"error","error":{"code":"cyber_policy","message":"blocked WS"}}`)
	for _, enforced := range []bool{false, true} {
		t.Run(map[bool]string{false: "unselected", true: "selected"}[enforced], func(t *testing.T) {
			c, _ := newCyberPolicyProtocolTestContext(t, enforced)
			conn := &openAIWSCyberDetectingFrameConn{
				inner: &cyberPolicyFrameConnStub{payload: payload},
				c:     c,
			}
			_, got, err := conn.ReadFrame(c.Request.Context())
			require.NoError(t, err)
			require.Equal(t, payload, got)
			if enforced {
				require.NotNil(t, GetOpsCyberPolicyForAttempt(c, "attempt-protocol"))
				return
			}
			require.Nil(t, GetOpsCyberPolicy(c))
		})
	}
}
