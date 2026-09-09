package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayService_StreamingReusesScannerBufferAndStillParsesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 0,
			MaxLineSize:               defaultMaxLineSize,
		},
	}

	svc := &GatewayService{
		cfg:              cfg,
		rateLimitService: &RateLimitService{},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	go func() {
		defer func() { _ = pw.Close() }()
		// Minimal SSE event to trigger parseSSEUsage
		_, _ = pw.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"))
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
	_ = pr.Close()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.usage)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 7, result.usage.OutputTokens)
}

func TestGatewayService_AnthropicDisconnectedStreamDrain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		name := "oauth"
		if passthrough {
			name = "api_key_passthrough"
		}
		t.Run(name, func(t *testing.T) {
			for _, scenario := range []struct {
				name         string
				disconnected bool
				cancelClient bool
				commentsOnly bool
				heartbeats   bool
				duration     time.Duration
				wantTimeout  bool
			}{
				{name: "heartbeats cannot extend disconnected drain", disconnected: true, heartbeats: true, duration: 1500 * time.Millisecond, wantTimeout: true},
				{name: "canceled client with only comments and empty events", cancelClient: true, commentsOnly: true, heartbeats: true, duration: 1500 * time.Millisecond, wantTimeout: true},
				{name: "usage still arrives after disconnect", disconnected: true, heartbeats: true, duration: 100 * time.Millisecond},
				{name: "silent disconnected upstream closes", disconnected: true, duration: 1500 * time.Millisecond, wantTimeout: true},
				{name: "live client may stream beyond drain limit", heartbeats: true, duration: 1250 * time.Millisecond},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					svc := &GatewayService{
						cfg: &config.Config{Gateway: config.GatewayConfig{
							StreamDataIntervalTimeout: 1,
							MaxLineSize:               defaultMaxLineSize,
						}},
						rateLimitService: &RateLimitService{},
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
					if scenario.cancelClient {
						clientCtx, cancelClient := context.WithCancel(c.Request.Context())
						c.Request = c.Request.WithContext(clientCtx)
						cancelClient()
					}
					if scenario.disconnected {
						c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
					}
					pr, pw := io.Pipe()
					defer func() { _ = pr.Close() }()
					producerDone := make(chan struct{})
					go func() {
						defer close(producerDone)
						defer func() { _ = pw.Close() }()
						if !scenario.commentsOnly {
							if _, err := io.WriteString(pw, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n"); err != nil {
								return
							}
							// The output usage follows the first failed downstream write.
							if _, err := io.WriteString(pw, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"); err != nil {
								return
							}
						}
						timer := time.NewTimer(scenario.duration)
						defer timer.Stop()
						ticker := time.NewTicker(25 * time.Millisecond)
						defer ticker.Stop()
						for {
							select {
							case <-timer.C:
								_, _ = io.WriteString(pw, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
								return
							case <-ticker.C:
								if scenario.heartbeats {
									heartbeat := "event: ping\ndata: {\"type\":\"ping\"}\n\n\n"
									if scenario.commentsOnly {
										heartbeat = ": keep-alive\n\n\n"
									}
									if _, err := io.WriteString(pw, heartbeat); err != nil {
										return
									}
								}
							}
						}
					}()
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}
					var result *streamingResult
					var err error
					if passthrough {
						result, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
					} else {
						result, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
					}
					_ = pr.Close()
					<-producerDone
					if scenario.wantTimeout {
						require.Error(t, err)
						require.Contains(t, err.Error(), "stream usage incomplete")
					} else {
						require.NoError(t, err)
					}
					require.NotNil(t, result)
					require.Equal(t, scenario.disconnected || scenario.cancelClient, result.clientDisconnect)
					if scenario.commentsOnly {
						require.Zero(t, result.usage.InputTokens)
						require.Zero(t, result.usage.OutputTokens)
					} else {
						require.Equal(t, 3, result.usage.InputTokens)
						require.Equal(t, 7, result.usage.OutputTokens)
					}
				})
			}
		})
	}
}
