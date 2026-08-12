//go:build unit

package service

import (
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

func TestOpenAIStreamingEmptyCompletedReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				StreamDataIntervalTimeout: 0,
				StreamKeepaliveInterval:   0,
				MaxLineSize:               defaultMaxLineSize,
			},
		},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_empty","status":"in_progress"}}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_empty","status":"completed"}}`,
			"",
		}, "\n"))),
		Header: http.Header{"X-Request-Id": []string{"rid-empty-completed"}},
	}

	_, err := svc.handleStreamingResponse(
		c.Request.Context(),
		resp,
		c,
		&Account{ID: 1, Platform: PlatformOpenAI, Name: "acc"},
		time.Now(),
		"model",
		"model",
	)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
	require.Contains(t, string(failoverErr.ResponseBody), openAIResponsesEmptyCompletedMessage)
}

func TestOpenAIStreamingPassthroughEmptyCompletedReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				StreamDataIntervalTimeout: 0,
				StreamKeepaliveInterval:   0,
				MaxLineSize:               defaultMaxLineSize,
			},
		},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_empty_pt","status":"in_progress"}}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_empty_pt","status":"completed"}}`,
			"",
		}, "\n"))),
		Header: http.Header{"X-Request-Id": []string{"rid-empty-completed-pt"}},
	}

	_, err := svc.handleStreamingResponsePassthrough(
		c.Request.Context(),
		resp,
		c,
		&Account{ID: 1, Platform: PlatformOpenAI, Name: "acc"},
		time.Now(),
		"model",
		"model",
	)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
	require.Contains(t, string(failoverErr.ResponseBody), openAIResponsesEmptyCompletedMessage)
}

func TestOpenAIResponsesCompletedEventIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		usage *OpenAIUsage
		want  bool
	}{
		{
			name: "bare completed",
			data: `{"type":"response.completed"}`,
			want: true,
		},
		{
			name: "completed with empty output array",
			data: `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`,
			want: true,
		},
		{
			name: "completed with response usage",
			data: `{"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
			want: false,
		},
		{
			name: "completed with top level usage",
			data: `{"type":"response.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
			want: false,
		},
		{
			name: "completed with error",
			data: `{"type":"response.completed","response":{"id":"r1","status":"completed","error":{"code":"x"}}}`,
			want: false,
		},
		{
			name: "completed with output item",
			data: `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"message","id":"msg_1"}]}}`,
			want: false,
		},
		{
			name: "accumulated usage",
			data: `{"type":"response.completed"}`,
			usage: &OpenAIUsage{
				InputTokens:  7,
				OutputTokens: 2,
			},
			want: false,
		},
		{
			name: "invalid json",
			data: `{"type":`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, openAIResponsesCompletedEventIsEmpty([]byte(tt.data), tt.usage))
		})
	}
}
