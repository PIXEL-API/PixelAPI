package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type grokMediaContentUpstreamStub struct {
	requests  []*http.Request
	responses []*http.Response
}

func (s *grokMediaContentUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.requests = append(s.requests, req)
	if len(s.responses) == 0 {
		return nil, io.EOF
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, nil
}

func (s *grokMediaContentUpstreamStub) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func grokMediaContentTestAccount() *Account {
	return &Account{
		ID:       9,
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "upstream-key",
			"base_url": "https://relay.example/v1",
		},
	}
}

func grokMediaContentTestContext(method, target string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c, recorder
}

func grokMediaContentStatusResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestForwardGrokMediaContentFetchesSignedURLWithoutCredentials(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{
		responses: []*http.Response{
			grokMediaContentStatusResponse(`{"status":"done","video":{"url":"https://vidgen.x.ai/signed-token/task-1.mp4?signature=secret"}}`),
			{
				StatusCode: http.StatusPartialContent,
				Header: http.Header{
					"Content-Type":        []string{"video/mp4"},
					"Content-Length":      []string{"13"},
					"Content-Range":       []string{"bytes 0-12/100"},
					"Accept-Ranges":       []string{"bytes"},
					"Content-Disposition": []string{`attachment; filename="task-1.mp4"`},
				},
				Body: io.NopCloser(strings.NewReader("video-payload")),
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := grokMediaContentTestContext(
		http.MethodGet,
		"https://api.example/v1/videos/task-1/content",
		map[string]string{"Range": "bytes=0-12"},
	)

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, grokMediaContentTestAccount(),
		GrokMediaEndpointVideoContent, "task-1", nil, "",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusPartialContent, recorder.Code)
	require.Equal(t, "video-payload", recorder.Body.String())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://relay.example/v1/videos/task-1", upstream.requests[0].URL.String())
	require.Equal(t, "Bearer upstream-key", upstream.requests[0].Header.Get("Authorization"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
	require.Equal(t, "https://vidgen.x.ai/signed-token/task-1.mp4?signature=secret", upstream.requests[1].URL.String())
	require.Empty(t, upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, "bytes=0-12", upstream.requests[1].Header.Get("Range"))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[1].Context()))
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "bytes 0-12/100", recorder.Header().Get("Content-Range"))
	require.Equal(t, "bytes", recorder.Header().Get("Accept-Ranges"))
	require.Equal(t, `attachment; filename="task-1.mp4"`, recorder.Header().Get("Content-Disposition"))
	require.True(t, IsResponseCommitted(c))
}

func TestForwardGrokMediaContentFollowsAuthenticatedSub2APIChain(t *testing.T) {
	for _, statusURL := range []string{
		`/v1/videos/task-1/content`,
		`https://different-relay.example/v1/videos/task-1/content`,
	} {
		t.Run(statusURL, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{
				responses: []*http.Response{
					grokMediaContentStatusResponse(`{"status":"completed","video":{"url":"` + statusURL + `"}}`),
					{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"video/mp4"}},
						Body:       io.NopCloser(strings.NewReader("video-payload")),
					},
				},
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			c, recorder := grokMediaContentTestContext(
				http.MethodGet, "https://api.example/v1/videos/task-1/content", nil,
			)

			_, err := svc.ForwardGrokMedia(
				context.Background(), c, grokMediaContentTestAccount(),
				GrokMediaEndpointVideoContent, "task-1", nil, "",
			)

			require.NoError(t, err)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "video-payload", recorder.Body.String())
			require.Len(t, upstream.requests, 2)
			require.Equal(t, "https://relay.example/v1/videos/task-1/content", upstream.requests[1].URL.String())
			require.Equal(t, "Bearer upstream-key", upstream.requests[1].Header.Get("Authorization"))
		})
	}
}

func TestForwardGrokMediaContentPreservesRangeNotSatisfiable(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{
		responses: []*http.Response{
			grokMediaContentStatusResponse(`{"status":"completed"}`),
			{
				StatusCode: http.StatusRequestedRangeNotSatisfiable,
				Header: http.Header{
					"Content-Type":  []string{"text/plain"},
					"Content-Range": []string{"bytes */100"},
					"Accept-Ranges": []string{"bytes"},
				},
				Body: io.NopCloser(strings.NewReader("bad-range")),
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := grokMediaContentTestContext(
		http.MethodGet,
		"https://api.example/v1/videos/task-1/content",
		map[string]string{"Range": "bytes=500-600"},
	)

	_, err := svc.ForwardGrokMedia(
		context.Background(), c, grokMediaContentTestAccount(),
		GrokMediaEndpointVideoContent, "task-1", nil, "",
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, recorder.Code)
	require.Equal(t, "bad-range", recorder.Body.String())
	require.Equal(t, "bytes */100", recorder.Header().Get("Content-Range"))
	require.Equal(t, "bytes", recorder.Header().Get("Accept-Ranges"))
}

func TestForwardGrokMediaContentRejectsRedirectResponses(t *testing.T) {
	for _, tt := range []struct {
		name      string
		responses []*http.Response
		wantCalls int
	}{
		{
			name: "status redirect",
			responses: []*http.Response{{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://attacker.invalid/status"}},
				Body:       http.NoBody,
			}},
			wantCalls: 1,
		},
		{
			name: "content redirect",
			responses: []*http.Response{
				grokMediaContentStatusResponse(`{"status":"done","video":{"url":"https://vidgen.x.ai/task-1.mp4"}}`),
				{
					StatusCode: http.StatusTemporaryRedirect,
					Header:     http.Header{"Location": []string{"https://attacker.invalid/content"}},
					Body:       http.NoBody,
				},
			},
			wantCalls: 2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{responses: tt.responses}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			c, _ := grokMediaContentTestContext(
				http.MethodGet, "https://api.example/v1/videos/task-1/content", nil,
			)

			_, err := svc.ForwardGrokMedia(
				context.Background(), c, grokMediaContentTestAccount(),
				GrokMediaEndpointVideoContent, "task-1", nil, "",
			)

			require.ErrorContains(t, err, "redirect is not allowed")
			require.Len(t, upstream.requests, tt.wantCalls)
		})
	}
}

func TestGrokMediaSignedVideoContentURLValidation(t *testing.T) {
	valid, err := grokMediaSignedVideoContentURL(
		[]byte(`{"video":{"url":"https://vidgen.x.ai/video.mp4?signature=secret"}}`),
		"task-1",
	)
	require.NoError(t, err)
	require.Equal(t, "https://vidgen.x.ai/video.mp4?signature=secret", valid)

	relay, err := grokMediaSignedVideoContentURL(
		[]byte(`{"video":{"url":"/v1/videos/task-1/content"}}`),
		"task-1",
	)
	require.NoError(t, err)
	require.Empty(t, relay)

	for _, rawURL := range []string{
		"http://vidgen.x.ai/video.mp4",
		"https://vidgen.x.ai.attacker.invalid/video.mp4",
		"https://vidgen.x.ai@attacker.invalid/video.mp4",
		"https://vidgen.x.ai:444/video.mp4",
		"/v1/videos/task-2/content",
	} {
		t.Run(rawURL, func(t *testing.T) {
			_, err := grokMediaSignedVideoContentURL(
				[]byte(`{"video":{"url":"`+rawURL+`"}}`),
				"task-1",
			)
			require.ErrorContains(t, err, "unsupported video content URL")
		})
	}
}

func TestForwardGrokVideoStatusRewritesProtectedURLsToSameOrigin(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{
		responses: []*http.Response{
			grokMediaContentStatusResponse(`{"id":"task/one","status":"completed","video":{"url":"https://vidgen.x.ai/signed.mp4"},"nested":[{"url":"https://relay.example/v1/videos/task%2Fone/content"},{"url":"https://relay.example/v1/videos/other/content"}],"counter":9007199254740993}`),
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := grokMediaContentTestContext(
		http.MethodGet,
		"https://api.example/v1/videos/task%2Fone",
		map[string]string{"X-Forwarded-Host": "malicious.invalid"},
	)

	_, err := svc.ForwardGrokMedia(
		context.Background(), c, grokMediaContentTestAccount(),
		GrokMediaEndpointVideoStatus, "task/one", nil, "",
	)

	require.NoError(t, err)
	require.Equal(t, "/v1/videos/task%2Fone/content", gjson.GetBytes(recorder.Body.Bytes(), "video.url").String())
	require.Equal(t, "/v1/videos/task%2Fone/content", gjson.GetBytes(recorder.Body.Bytes(), "nested.0.url").String())
	require.Equal(t, "https://relay.example/v1/videos/other/content", gjson.GetBytes(recorder.Body.Bytes(), "nested.1.url").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(recorder.Body.Bytes(), "counter").String())
	require.NotContains(t, recorder.Body.String(), "malicious.invalid")
}
