//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveGrokStreamIdleTimeout(t *testing.T) {
	require.Equal(t, 90*time.Second, resolveGrokStreamIdleTimeout(90))
	require.Equal(t, defaultGrokStreamIdleTimeout, resolveGrokStreamIdleTimeout(0))
	require.Equal(t, defaultGrokStreamIdleTimeout, resolveGrokStreamIdleTimeout(-1))
}

func TestGrokResponsesStreamIdleBeforeOutputReturnsFailoverWithoutCommitting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader, writer := io.Pipe()
	defer writer.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 1,
			MaxLineSize:               defaultMaxLineSize,
		}},
		accountRepo: repo,
	}
	account := &Account{ID: 9301, Platform: PlatformGrok, Type: AccountTypeAPIKey}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}

	_, err := svc.handleStreamingResponse(context.Background(), response, c, account, time.Now(), "grok", "grok-4.5")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Contains(t, string(failoverErr.ResponseBody), "empty_upstream")
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok stream idle timeout", repo.lastTempUnschedReason)
	require.NoError(t, reader.Close())
}

func TestGrokChatAndMessagesStreamIdleBeforeOutputReturnFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"chat", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer writer.Close()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil)
			account := &Account{ID: 9302, Platform: PlatformGrok, Type: AccountTypeAPIKey}
			repo := &grokQuotaAccountRepo{}
			svc := &OpenAIGatewayService{
				cfg: &config.Config{Gateway: config.GatewayConfig{
					StreamDataIntervalTimeout: 1,
					MaxLineSize:               defaultMaxLineSize,
				}},
				accountRepo: repo,
			}
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
			response.Body = newGrokStreamIdleReadCloser(response.Body, time.Second, func() {
				svc.tempUnscheduleGrok(context.Background(), account, grokStreamIdleCooldown, "grok stream idle timeout")
			})

			var err error
			if protocol == "chat" {
				_, err = svc.handleChatBufferedStreamingResponse(
					context.Background(), response, c, account,
					"grok", "grok", "grok-4.5", time.Now(),
				)
			} else {
				_, err = svc.handleAnthropicBufferedStreamingResponse(
					context.Background(), response, c, account,
					"grok", "grok", "grok-4.5", time.Now(),
				)
			}

			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.False(t, c.Writer.Written())
			require.Empty(t, recorder.Body.String())
			require.Equal(t, 1, repo.tempUnschedCalls)
			require.NoError(t, reader.Close())
		})
	}
}

func TestGrokStreamIdleFailoverError(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth}
	err := grokStreamIdleFailoverError(account, 180*time.Second)
	require.Equal(t, 502, err.StatusCode)
	require.True(t, err.SafeToFailoverAfterWrite)
	require.Contains(t, string(err.ResponseBody), "empty_upstream")
}

func TestGrokStreamIdleReadCloserPreservesDataAndCallsIdleOnce(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	var idleCalls atomic.Int32
	body := newGrokStreamIdleReadCloser(reader, 50*time.Millisecond, func() {
		idleCalls.Add(1)
	})
	defer body.Close()

	go func() {
		_, _ = writer.Write([]byte("abcdef"))
	}()
	buffer := make([]byte, 3)
	n, err := body.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "abc", string(buffer[:n]))
	n, err = body.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "def", string(buffer[:n]))

	_, err = body.Read(buffer)
	require.ErrorIs(t, err, errGrokStreamIdleTimeout)
	_, err = body.Read(buffer)
	require.Error(t, err)
	require.EqualValues(t, 1, idleCalls.Load())
}

func TestGrokChatStreamIdleAfterOutputWritesProtocolErrorWithoutFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader, writer := io.Pipe()
	defer writer.Close()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	account := &Account{ID: 9303, Platform: PlatformGrok, Type: AccountTypeAPIKey}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 1,
			MaxLineSize:               defaultMaxLineSize,
		}},
		accountRepo: repo,
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	response.Body = newGrokStreamIdleReadCloser(response.Body, time.Second, func() {
		svc.tempUnscheduleGrok(context.Background(), account, grokStreamIdleCooldown, "grok stream idle timeout")
	})
	go func() {
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
	}()

	_, err := svc.handleChatStreamingResponse(
		context.Background(), response, c, account,
		"grok", "grok", "grok-4.5", false, time.Now(),
	)

	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), "partial")
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), "Grok upstream stream timed out")
	require.True(t, strings.HasSuffix(recorder.Body.String(), "data: [DONE]\n\n"))
	require.Equal(t, 1, repo.tempUnschedCalls)
}

func TestGrokMessagesStreamIdleAfterOutputWritesProtocolErrorWithoutFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader, writer := io.Pipe()
	defer writer.Close()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{ID: 9304, Platform: PlatformGrok, Type: AccountTypeAPIKey}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 1,
			MaxLineSize:               defaultMaxLineSize,
		}},
		accountRepo: repo,
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	response.Body = newGrokStreamIdleReadCloser(response.Body, time.Second, func() {
		svc.tempUnscheduleGrok(context.Background(), account, grokStreamIdleCooldown, "grok stream idle timeout")
	})
	go func() {
		_, _ = writer.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_partial\",\"model\":\"grok-4.5\"}}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
	}()

	_, err := svc.handleAnthropicStreamingResponse(
		context.Background(), response, c, account,
		"grok", "grok", "grok-4.5", time.Now(),
	)

	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), "partial")
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), "Grok upstream stream timed out")
	require.NotContains(t, recorder.Body.String(), "[DONE]")
	require.Equal(t, 1, repo.tempUnschedCalls)
}

func TestGrokResponsesStreamIdleAfterOutputWritesProtocolErrorWithoutFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader, writer := io.Pipe()
	defer writer.Close()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	account := &Account{ID: 9305, Platform: PlatformGrok, Type: AccountTypeAPIKey}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 1,
			MaxLineSize:               defaultMaxLineSize,
		}},
		accountRepo: repo,
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	go func() {
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
	}()

	_, err := svc.handleStreamingResponse(context.Background(), response, c, account, time.Now(), "grok", "grok-4.5")

	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), "partial")
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.Contains(t, recorder.Body.String(), "stream_timeout")
	require.Equal(t, 1, repo.tempUnschedCalls)
}
