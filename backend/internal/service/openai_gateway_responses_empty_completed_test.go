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

func TestOpenAIStreamingBareDoneReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		name := "streaming"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
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
					`data: {"type":"response.created","response":{"id":"resp_empty_done","status":"in_progress"}}`,
					"",
					`data: {"type":"response.done"}`,
					"",
				}, "\n"))),
				Header: http.Header{"X-Request-Id": []string{"rid-empty-done"}},
			}

			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Name: "acc"}, time.Now(), "model", "model")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Name: "acc"}, time.Now(), "model", "model")
			}

			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
			require.Contains(t, string(failoverErr.ResponseBody), openAIResponsesEmptyCompletedMessage)
		})
	}
}

func TestOpenAIStreamingUsageEnvelopeTerminalSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type streamHandler struct {
		name string
		run  func(*OpenAIGatewayService, *gin.Context, *http.Response) (*OpenAIUsage, error)
	}
	handlers := []streamHandler{
		{
			name: "streaming",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response) (*OpenAIUsage, error) {
				result, err := svc.handleStreamingResponse(
					c.Request.Context(),
					resp,
					c,
					&Account{ID: 1, Platform: PlatformOpenAI, Name: "acc"},
					time.Now(),
					"model",
					"model",
				)
				if result == nil {
					return nil, err
				}
				return result.usage, err
			},
		},
		{
			name: "passthrough",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response) (*OpenAIUsage, error) {
				result, err := svc.handleStreamingResponsePassthrough(
					c.Request.Context(),
					resp,
					c,
					&Account{ID: 1, Platform: PlatformOpenAI, Name: "acc"},
					time.Now(),
					"model",
					"model",
				)
				if result == nil {
					return nil, err
				}
				return result.usage, err
			},
		},
	}
	terminals := []string{"response.completed", "response.done"}
	usageEnvelopes := []struct {
		name       string
		json       string
		wantInput  int
		wantOutput int
	}{
		{name: "top level usage", json: `"usage":{"input_tokens":7,"output_tokens":3}`, wantInput: 7, wantOutput: 3},
		{name: "response usage", json: `"response":{"usage":{"input_tokens":8,"output_tokens":4}}`, wantInput: 8, wantOutput: 4},
		{name: "data usage", json: `"data":{"usage":{"input_tokens":9,"output_tokens":5}}`, wantInput: 9, wantOutput: 5},
		{name: "data response usage", json: `"data":{"response":{"usage":{"input_tokens":10,"output_tokens":6}}}`, wantInput: 10, wantOutput: 6},
		{name: "zero data usage", json: `"data":{"usage":{"input_tokens":0,"output_tokens":0}}`},
		{name: "zero data response usage", json: `"data":{"response":{"usage":{"input_tokens":0,"output_tokens":0}}}`},
	}

	for _, handler := range handlers {
		for _, terminal := range terminals {
			for _, envelope := range usageEnvelopes {
				name := handler.name + "/" + terminal + "/" + envelope.name
				t.Run(name, func(t *testing.T) {
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
					terminalPayload := `{"type":"` + terminal + `",` + envelope.json + `}`
					resp := &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader(strings.Join([]string{
							`data: {"type":"response.created","response":{"id":"resp_nested","status":"in_progress"}}`,
							"",
							"data: " + terminalPayload,
							"",
						}, "\n"))),
						Header: http.Header{"X-Request-Id": []string{"rid-nested-usage"}},
					}

					usage, err := handler.run(svc, c, resp)

					require.NoError(t, err)
					require.NotNil(t, usage)
					require.Equal(t, envelope.wantInput, usage.InputTokens)
					require.Equal(t, envelope.wantOutput, usage.OutputTokens)
					require.Contains(t, rec.Body.String(), terminalPayload)
				})
			}
		}
	}
}

func TestOpenAIStreamingEarlierZeroUsagePreventsEmptyTerminalFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		name := "streaming"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			svc := &OpenAIGatewayService{
				cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(strings.Join([]string{
					`data: {"type":"response.in_progress","data":{"usage":{"input_tokens":0,"output_tokens":0}}}`,
					"",
					`data: {"type":"response.completed"}`,
					"",
				}, "\n"))),
				Header: http.Header{"X-Request-Id": []string{"rid-earlier-zero-usage"}},
			}

			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 1}, time.Now(), "model", "model")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1}, time.Now(), "model", "model")
			}

			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), "response.completed")
		})
	}
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
			name: "completed with empty top level usage object",
			data: `{"type":"response.completed","usage":{}}`,
			want: false,
		},
		{
			name: "completed with zero data usage",
			data: `{"type":"response.completed","data":{"usage":{"input_tokens":0,"output_tokens":0}}}`,
			want: false,
		},
		{
			name: "completed with empty data response usage",
			data: `{"type":"response.completed","data":{"response":{"usage":{}}}}`,
			want: false,
		},
		{
			name: "done with empty data usage",
			data: `{"type":"response.done","data":{"usage":{}}}`,
			want: false,
		},
		{
			name: "done with zero data response usage",
			data: `{"type":"response.done","data":{"response":{"usage":{"input_tokens":0,"output_tokens":0}}}}`,
			want: false,
		},
		{
			name: "null usage is not valid usage",
			data: `{"type":"response.completed","usage":null}`,
			want: true,
		},
		{
			name: "string response usage is not valid usage",
			data: `{"type":"response.completed","response":{"usage":"invalid"}}`,
			want: true,
		},
		{
			name: "array data usage is not valid usage",
			data: `{"type":"response.done","data":{"usage":[]}}`,
			want: true,
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
			require.Equal(t, tt.want, openAIResponsesCompletedEventIsEmpty([]byte(tt.data), tt.usage, false))
		})
	}
}
