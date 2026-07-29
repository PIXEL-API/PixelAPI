package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountTestTrackingBody struct {
	reader     io.Reader
	readCalled bool
}

func (b *accountTestTrackingBody) Read(p []byte) (int, error) {
	b.readCalled = true
	if b.reader == nil {
		return 0, io.EOF
	}
	return b.reader.Read(p)
}

func (b *accountTestTrackingBody) Close() error {
	return nil
}

func TestAccountTestService_OpenAIImageOAuthHandlesOutputItemDoneFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000006,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:       53,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	err := svc.testOpenAIImageOAuth(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
	require.Equal(t, codexCLIUserAgent, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "codex_cli_rs", upstream.lastReq.Header.Get("Originator"))
	require.Contains(t, rec.Body.String(), "Calling Codex /responses image tool")
	require.Contains(t, rec.Body.String(), "data:image/png;base64,aGVsbG8=")
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

func TestAccountTestService_OpenAIImageAPIKeyUsesConfiguredV1BaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
		},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:       54,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	err := svc.testOpenAIImageAPIKey(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
	require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.NotContains(t, string(upstream.lastBody), "response_format")
	require.NotContains(t, string(upstream.lastBody), `"n"`)
	require.Contains(t, rec.Body.String(), "data:image/png;base64,aGVsbG8=")
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

func TestAccountTestService_OpenAIImageAPIKeyRejectsUnexpectedStatusBeforeReadingBody(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusContinue,
		http.StatusFound,
		http.StatusNotModified,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

			body := &accountTestTrackingBody{
				reader: strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="}]}`),
			}
			upstream := &httpUpstreamRecorder{
				resp: &http.Response{
					StatusCode: statusCode,
					Header:     make(http.Header),
					Body:       body,
				},
			}
			svc := &AccountTestService{
				httpUpstream: upstream,
				cfg:          &config.Config{},
			}
			account := &Account{
				ID:       55,
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key": "test-api-key",
				},
			}

			err := svc.testOpenAIImageAPIKey(c, context.Background(), account, "gpt-image-2", "draw a cat")

			require.Error(t, err)
			require.NotNil(t, upstream.lastReq)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
			require.False(t, body.readCalled)
			require.NotContains(t, rec.Body.String(), "\"success\":true")
			require.NotContains(t, rec.Body.String(), "data:image/png")
		})
	}
}

func TestAccountTestService_OpenAIImageOAuthRejectsUnexpectedStatusBeforeReadingBody(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusContinue,
		http.StatusFound,
		http.StatusNotModified,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

			body := &accountTestTrackingBody{
				reader: strings.NewReader(
					"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"output_format\":\"png\"}}\n\n" +
						"data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n" +
						"data: [DONE]\n\n",
				),
			}
			upstream := &httpUpstreamRecorder{
				resp: &http.Response{
					StatusCode: statusCode,
					Header:     make(http.Header),
					Body:       body,
				},
			}
			svc := &AccountTestService{httpUpstream: upstream}
			account := &Account{
				ID:       56,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "token-123",
				},
			}

			err := svc.testOpenAIImageOAuth(c, context.Background(), account, "gpt-image-2", "draw a cat")

			require.Error(t, err)
			require.NotNil(t, upstream.lastReq)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
			require.False(t, body.readCalled)
			require.NotContains(t, rec.Body.String(), "\"success\":true")
			require.NotContains(t, rec.Body.String(), "data:image/png")
		})
	}
}
