//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSoraDirectClient_DoRequestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{BaseURL: server.URL},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)

	body, _, err := client.doRequest(context.Background(), &Account{ID: 1}, http.MethodGet, server.URL, http.Header{}, nil, false)
	require.NoError(t, err)
	require.Contains(t, string(body), "ok")
}

func TestSoraDirectClient_BuildBaseHeaders(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				Headers: map[string]string{
					"X-Test":                "yes",
					"Authorization":         "should-ignore",
					"openai-sentinel-token": "skip",
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)

	headers := client.buildBaseHeaders("token-123", "UA")
	require.Equal(t, "Bearer token-123", headers.Get("Authorization"))
	require.Equal(t, "UA", headers.Get("User-Agent"))
	require.Equal(t, "yes", headers.Get("X-Test"))
	require.Empty(t, headers.Get("openai-sentinel-token"))
}

func TestSoraDirectClient_GetImageTaskFallbackLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		switch limit {
		case "1":
			_, _ = w.Write([]byte(`{"task_responses":[]}`))
		case "2":
			_, _ = w.Write([]byte(`{"task_responses":[{"id":"task-1","status":"completed","progress_pct":1,"generations":[{"url":"https://example.com/a.png"}]}]}`))
		default:
			_, _ = w.Write([]byte(`{"task_responses":[]}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL:            server.URL,
				RecentTaskLimit:    1,
				RecentTaskLimitMax: 2,
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{Credentials: map[string]any{"access_token": "token"}}

	status, err := client.GetImageTask(context.Background(), account, "task-1")
	require.NoError(t, err)
	require.Equal(t, "completed", status.Status)
	require.Equal(t, []string{"https://example.com/a.png"}, status.URLs)
}

func TestNormalizeSoraBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty",
			raw:  "",
			want: "",
		},
		{
			name: "append_backend_for_sora_host",
			raw:  "https://sora.chatgpt.com",
			want: "https://sora.chatgpt.com/backend",
		},
		{
			name: "convert_backend_api_to_backend",
			raw:  "https://sora.chatgpt.com/backend-api",
			want: "https://sora.chatgpt.com/backend",
		},
		{
			name: "keep_backend",
			raw:  "https://sora.chatgpt.com/backend",
			want: "https://sora.chatgpt.com/backend",
		},
		{
			name: "keep_custom_host",
			raw:  "https://example.com/custom-path",
			want: "https://example.com/custom-path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSoraBaseURL(tt.raw)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSoraDirectClient_BuildURL_UsesNormalizedBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com",
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	require.Equal(t, "https://sora.chatgpt.com/backend/video_gen", client.buildURL("/video_gen"))
}

func TestSoraDirectClient_BuildUpstreamError_NotFoundHint(t *testing.T) {
	t.Parallel()
	client := NewSoraDirectClient(&config.Config{}, nil, nil)

	err := client.buildUpstreamError(http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"Not found"}}`), "https://sora.chatgpt.com/video_gen")
	var upstreamErr *SoraUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Contains(t, upstreamErr.Message, "请检查 sora.client.base_url")

	errNoHint := client.buildUpstreamError(http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"Not found"}}`), "https://sora.chatgpt.com/backend/video_gen")
	require.ErrorAs(t, errNoHint, &upstreamErr)
	require.NotContains(t, upstreamErr.Message, "请检查 sora.client.base_url")
}

func TestFormatSoraHeaders_RedactsSensitive(t *testing.T) {
	t.Parallel()
	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret-token")
	headers.Set("openai-sentinel-token", "sentinel-secret")
	headers.Set("X-Test", "ok")

	out := formatSoraHeaders(headers)
	require.Contains(t, out, `"Authorization":"***"`)
	require.Contains(t, out, `Sentinel-Token":"***"`)
	require.Contains(t, out, `"X-Test":"ok"`)
	require.NotContains(t, out, "secret-token")
	require.NotContains(t, out, "sentinel-secret")
}

func TestSummarizeSoraResponseBody_RedactsJSON(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":{"message":"bad"},"access_token":"abc123"}`)
	out := summarizeSoraResponseBody(body, 512)
	require.Contains(t, out, `"access_token":"***"`)
	require.NotContains(t, out, "abc123")
}

func TestSummarizeSoraResponseBody_Truncates(t *testing.T) {
	t.Parallel()
	body := []byte(strings.Repeat("x", 100))
	out := summarizeSoraResponseBody(body, 10)
	require.Contains(t, out, "(truncated)")
}

func TestSoraDirectClient_GetAccessToken_SoraDefaultUseCredentials(t *testing.T) {
	t.Parallel()
	cache := newOpenAITokenCacheStub()
	provider := NewOpenAITokenProvider(nil, cache, nil)
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, provider)
	account := &Account{
		ID:       1,
		Platform: PlatformSora,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "sora-credential-token",
		},
	}

	token, err := client.getAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "sora-credential-token", token)
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.getCalled))
}

func TestSoraDirectClient_GetAccessToken_SoraCanEnableProvider(t *testing.T) {
	t.Parallel()
	cache := newOpenAITokenCacheStub()
	account := &Account{
		ID:       2,
		Platform: PlatformSora,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "sora-credential-token",
		},
	}
	cache.tokens[OpenAITokenCacheKey(account)] = "provider-token"
	provider := NewOpenAITokenProvider(nil, cache, nil)
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL:                "https://sora.chatgpt.com/backend",
				UseOpenAITokenProvider: true,
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, provider)

	token, err := client.getAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "provider-token", token)
	require.Greater(t, atomic.LoadInt32(&cache.getCalled), int32(0))
}

func TestSoraDirectClient_GetAccessToken_FromSessionToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=session-token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken": "session-access-token",
			"expires":     "2099-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	origin := soraSessionAuthURL
	soraSessionAuthURL = server.URL
	defer func() { soraSessionAuthURL = origin }()

	client := NewSoraDirectClient(&config.Config{}, nil, nil)
	account := &Account{
		ID:       10,
		Platform: PlatformSora,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"session_token": "session-token",
		},
	}

	token, err := client.getAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "session-access-token", token)
	require.Equal(t, "session-access-token", account.GetCredential("access_token"))
}

func TestSoraDirectClient_GetAccessToken_FromRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/oauth/token", r.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.FormValue("grant_type"))
		require.Equal(t, "refresh-token-old", r.FormValue("refresh_token"))
		require.NotEmpty(t, r.FormValue("client_id"))
		require.Equal(t, "com.openai.chat://auth0.openai.com/ios/com.openai.chat/callback", r.FormValue("redirect_uri"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refresh-access-token",
			"refresh_token": "refresh-token-new",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	origin := soraOAuthTokenURL
	soraOAuthTokenURL = server.URL + "/oauth/token"
	defer func() { soraOAuthTokenURL = origin }()

	client := NewSoraDirectClient(&config.Config{}, nil, nil)
	account := &Account{
		ID:       11,
		Platform: PlatformSora,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "refresh-token-old",
		},
	}

	token, err := client.getAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "refresh-access-token", token)
	require.Equal(t, "refresh-token-new", account.GetCredential("refresh_token"))
	require.NotNil(t, account.GetCredentialAsTime("expires_at"))
}

func TestSoraDirectClient_PreflightCheck_VideoQuotaExceeded(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/nf/check", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rate_limit_and_credit_balance": map[string]any{
				"estimated_num_videos_remaining": 0,
				"rate_limit_reached":             true,
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: server.URL,
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{
		ID:       12,
		Platform: PlatformSora,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "ok",
			"expires_at":   time.Now().Add(2 * time.Hour).Format(time.RFC3339),
		},
	}
	err := client.PreflightCheck(context.Background(), account, "sora2-landscape-10s", SoraModelConfig{Type: "video"})
	require.Error(t, err)
	var upstreamErr *SoraUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusTooManyRequests, upstreamErr.StatusCode)
}

func TestShouldAttemptSoraTokenRecover(t *testing.T) {
	t.Parallel()

	require.True(t, shouldAttemptSoraTokenRecover(http.StatusUnauthorized, "https://sora.chatgpt.com/backend/video_gen"))
	require.True(t, shouldAttemptSoraTokenRecover(http.StatusForbidden, "https://chatgpt.com/backend/video_gen"))
	require.False(t, shouldAttemptSoraTokenRecover(http.StatusUnauthorized, "https://sora.chatgpt.com/api/auth/session"))
	require.False(t, shouldAttemptSoraTokenRecover(http.StatusUnauthorized, "https://auth.openai.com/oauth/token"))
	require.False(t, shouldAttemptSoraTokenRecover(http.StatusTooManyRequests, "https://sora.chatgpt.com/backend/video_gen"))
}

type soraClientRequestCall struct {
	Path      string
	UserAgent string
	ProxyURL  string
}

type soraClientRecordingUpstream struct {
	calls []soraClientRequestCall
}

func (u *soraClientRecordingUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, errors.New("unexpected Do call")
}

func (u *soraClientRecordingUpstream) DoWithTLS(req *http.Request, proxyURL string, _ int64, _ int, _ bool) (*http.Response, error) {
	u.calls = append(u.calls, soraClientRequestCall{
		Path:      req.URL.Path,
		UserAgent: req.Header.Get("User-Agent"),
		ProxyURL:  proxyURL,
	})
	switch req.URL.Path {
	case "/backend-api/sentinel/req":
		return newSoraClientMockResponse(http.StatusOK, `{"token":"sentinel-token","turnstile":{"dx":"ok"}}`), nil
	case "/backend/nf/create":
		return newSoraClientMockResponse(http.StatusOK, `{"id":"task-123"}`), nil
	case "/backend/nf/create/storyboard":
		return newSoraClientMockResponse(http.StatusOK, `{"id":"storyboard-123"}`), nil
	case "/backend/uploads":
		return newSoraClientMockResponse(http.StatusOK, `{"id":"upload-123"}`), nil
	case "/backend/nf/check":
		return newSoraClientMockResponse(http.StatusOK, `{"rate_limit_and_credit_balance":{"estimated_num_videos_remaining":1,"rate_limit_reached":false}}`), nil
	case "/backend/characters/upload":
		return newSoraClientMockResponse(http.StatusOK, `{"id":"cameo-123"}`), nil
	case "/backend/project_y/cameos/in_progress/cameo-123":
		return newSoraClientMockResponse(http.StatusOK, `{"status":"finalized","status_message":"Completed","username_hint":"foo.bar","display_name_hint":"Bar","profile_asset_url":"https://example.com/avatar.webp"}`), nil
	case "/backend/project_y/file/upload":
		return newSoraClientMockResponse(http.StatusOK, `{"asset_pointer":"asset-123"}`), nil
	case "/backend/characters/finalize":
		return newSoraClientMockResponse(http.StatusOK, `{"character":{"character_id":"character-123"}}`), nil
	case "/backend/project_y/post":
		return newSoraClientMockResponse(http.StatusOK, `{"post":{"id":"s_post"}}`), nil
	default:
		return newSoraClientMockResponse(http.StatusOK, `{"ok":true}`), nil
	}
}

func newSoraClientMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSoraDirectClient_TaskUserAgent_DefaultMobileFallback(t *testing.T) {
	client := NewSoraDirectClient(&config.Config{}, nil, nil)
	ua := client.taskUserAgent()
	require.NotEmpty(t, ua)
	allowed := append([]string{}, soraMobileUserAgents...)
	allowed = append(allowed, soraDesktopUserAgents...)
	require.Contains(t, allowed, ua)
}

func TestSoraDirectClient_CreateVideoTask_UsesSameUserAgentAndProxyForSentinelAndCreate(t *testing.T) {
	originPowTokenGenerator := soraPowTokenGenerator
	soraPowTokenGenerator = func(_ string) string { return "gAAAAACmock" }
	defer func() {
		soraPowTokenGenerator = originPowTokenGenerator
	}()

	upstream := &soraClientRecordingUpstream{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	proxyID := int64(9)
	account := &Account{
		ID:          21,
		Platform:    PlatformSora,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		ProxyID:     &proxyID,
		Proxy: &Proxy{
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
		},
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}

	taskID, err := client.CreateVideoTask(context.Background(), account, SoraVideoRequest{Prompt: "test"})
	require.NoError(t, err)
	require.Equal(t, "task-123", taskID)
	require.Len(t, upstream.calls, 2)

	sentinelCall := upstream.calls[0]
	createCall := upstream.calls[1]
	require.Equal(t, "/backend-api/sentinel/req", sentinelCall.Path)
	require.Equal(t, "/backend/nf/create", createCall.Path)
	require.Equal(t, "http://127.0.0.1:8080", sentinelCall.ProxyURL)
	require.Equal(t, sentinelCall.ProxyURL, createCall.ProxyURL)
	require.NotEmpty(t, sentinelCall.UserAgent)
	require.Equal(t, sentinelCall.UserAgent, createCall.UserAgent)
}

func TestSoraDirectClient_UploadImage_UsesTaskUserAgentAndProxy(t *testing.T) {
	upstream := &soraClientRecordingUpstream{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	proxyID := int64(3)
	account := &Account{
		ID:      31,
		ProxyID: &proxyID,
		Proxy: &Proxy{
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
		},
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}

	uploadID, err := client.UploadImage(context.Background(), account, []byte("mock-image"), "a.png")
	require.NoError(t, err)
	require.Equal(t, "upload-123", uploadID)
	require.Len(t, upstream.calls, 1)
	require.Equal(t, "/backend/uploads", upstream.calls[0].Path)
	require.Equal(t, "http://127.0.0.1:8080", upstream.calls[0].ProxyURL)
	require.NotEmpty(t, upstream.calls[0].UserAgent)
}

func TestSoraDirectClient_PreflightCheck_UsesTaskUserAgentAndProxy(t *testing.T) {
	upstream := &soraClientRecordingUpstream{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	proxyID := int64(7)
	account := &Account{
		ID:      41,
		ProxyID: &proxyID,
		Proxy: &Proxy{
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
		},
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}

	err := client.PreflightCheck(context.Background(), account, "sora2", SoraModelConfig{Type: "video"})
	require.NoError(t, err)
	require.Len(t, upstream.calls, 1)
	require.Equal(t, "/backend/nf/check", upstream.calls[0].Path)
	require.Equal(t, "http://127.0.0.1:8080", upstream.calls[0].ProxyURL)
	require.NotEmpty(t, upstream.calls[0].UserAgent)
}

func TestSoraDirectClient_CreateStoryboardTask(t *testing.T) {
	originPowTokenGenerator := soraPowTokenGenerator
	soraPowTokenGenerator = func(_ string) string { return "gAAAAACmock" }
	defer func() { soraPowTokenGenerator = originPowTokenGenerator }()

	upstream := &soraClientRecordingUpstream{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	account := &Account{
		ID: 51,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}

	taskID, err := client.CreateStoryboardTask(context.Background(), account, SoraStoryboardRequest{
		Prompt: "Shot 1:\nduration: 5sec\nScene: cat",
	})
	require.NoError(t, err)
	require.Equal(t, "storyboard-123", taskID)
	require.Len(t, upstream.calls, 2)
	require.Equal(t, "/backend-api/sentinel/req", upstream.calls[0].Path)
	require.Equal(t, "/backend/nf/create/storyboard", upstream.calls[1].Path)
}

func TestSoraDirectClient_GetVideoTask_ReturnsGenerationID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/nf/pending/v2":
			_, _ = w.Write([]byte(`[]`))
		case "/project_y/profile/drafts":
			_, _ = w.Write([]byte(`{"items":[{"id":"gen_1","task_id":"task-1","kind":"video","downloadable_url":"https://example.com/v.mp4"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: server.URL,
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{Credentials: map[string]any{"access_token": "token"}}

	status, err := client.GetVideoTask(context.Background(), account, "task-1")
	require.NoError(t, err)
	require.Equal(t, "completed", status.Status)
	require.Equal(t, "gen_1", status.GenerationID)
	require.Equal(t, []string{"https://example.com/v.mp4"}, status.URLs)
}

func TestSoraDirectClient_PostVideoForWatermarkFree(t *testing.T) {
	originPowTokenGenerator := soraPowTokenGenerator
	soraPowTokenGenerator = func(_ string) string { return "gAAAAACmock" }
	defer func() { soraPowTokenGenerator = originPowTokenGenerator }()

	upstream := &soraClientRecordingUpstream{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	account := &Account{
		ID: 52,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}

	postID, err := client.PostVideoForWatermarkFree(context.Background(), account, "gen_1")
	require.NoError(t, err)
	require.Equal(t, "s_post", postID)
	require.Len(t, upstream.calls, 2)
	require.Equal(t, "/backend-api/sentinel/req", upstream.calls[0].Path)
	require.Equal(t, "/backend/project_y/post", upstream.calls[1].Path)
}

type soraClientFallbackUpstream struct {
	doWithTLSCalls int32
	respBody       string
	respStatusCode int
	err            error
}

func (u *soraClientFallbackUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, errors.New("unexpected Do call")
}

func (u *soraClientFallbackUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ bool) (*http.Response, error) {
	atomic.AddInt32(&u.doWithTLSCalls, 1)
	if u.err != nil {
		return nil, u.err
	}
	statusCode := u.respStatusCode
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	body := u.respBody
	if body == "" {
		body = `{"ok":true}`
	}
	return newSoraClientMockResponse(statusCode, body), nil
}

func TestSoraDirectClient_DoHTTP_UsesCurlCFFISidecarWhenEnabled(t *testing.T) {
	var captured soraCurlCFFISidecarRequest
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/request", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &captured))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": http.StatusOK,
			"headers": map[string]any{
				"Content-Type": "application/json",
				"X-Sidecar":    []string{"yes"},
			},
			"body_base64": base64.StdEncoding.EncodeToString([]byte(`{"ok":true}`)),
		})
	}))
	defer sidecar.Close()

	upstream := &soraClientFallbackUpstream{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled:             true,
					BaseURL:             sidecar.URL,
					Impersonate:         "chrome131",
					TimeoutSeconds:      15,
					SessionReuseEnabled: true,
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	req, err := http.NewRequest(http.MethodPost, "https://sora.chatgpt.com/backend/me", strings.NewReader("hello-sidecar"))
	require.NoError(t, err)
	req.Header.Set("User-Agent", "test-ua")

	resp, err := client.doHTTP(req, "http://127.0.0.1:18080", &Account{ID: 1})
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.JSONEq(t, `{"ok":true}`, string(body))
	require.Equal(t, int32(0), atomic.LoadInt32(&upstream.doWithTLSCalls))
	require.Equal(t, "http://127.0.0.1:18080", captured.ProxyURL)
	require.NotEmpty(t, captured.SessionKey)
	require.Equal(t, "chrome131", captured.Impersonate)
	require.Equal(t, "https://sora.chatgpt.com/backend/me", captured.URL)
	decodedReqBody, err := base64.StdEncoding.DecodeString(captured.BodyBase64)
	require.NoError(t, err)
	require.Equal(t, "hello-sidecar", string(decodedReqBody))
}

func TestSoraDirectClient_DoHTTP_CurlCFFISidecarFailureReturnsError(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer sidecar.Close()

	upstream := &soraClientFallbackUpstream{respBody: `{"fallback":true}`}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled: true,
					BaseURL: sidecar.URL,
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	req, err := http.NewRequest(http.MethodGet, "https://sora.chatgpt.com/backend/me", nil)
	require.NoError(t, err)

	_, err = client.doHTTP(req, "", &Account{ID: 2})
	require.Error(t, err)
	require.Contains(t, err.Error(), "sora curl_cffi sidecar")
	require.Equal(t, int32(0), atomic.LoadInt32(&upstream.doWithTLSCalls))
}

func TestSoraDirectClient_DoHTTP_CurlCFFISidecarDisabledUsesLegacyStack(t *testing.T) {
	upstream := &soraClientFallbackUpstream{respBody: `{"legacy":true}`}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled: false,
					BaseURL: "http://127.0.0.1:18080",
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, upstream, nil)
	req, err := http.NewRequest(http.MethodGet, "https://sora.chatgpt.com/backend/me", nil)
	require.NoError(t, err)

	resp, err := client.doHTTP(req, "", &Account{ID: 3})
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"legacy":true}`, string(body))
	require.Equal(t, int32(1), atomic.LoadInt32(&upstream.doWithTLSCalls))
}

func TestConvertSidecarHeaderValue_NilAndSlice(t *testing.T) {
	require.Nil(t, convertSidecarHeaderValue(nil))
	require.Equal(t, []string{"a", "b"}, convertSidecarHeaderValue([]any{"a", " ", "b"}))
}

func TestSoraDirectClient_DoHTTP_SidecarSessionKeyStableForSameAccountProxy(t *testing.T) {
	var captured []soraCurlCFFISidecarRequest
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var reqPayload soraCurlCFFISidecarRequest
		require.NoError(t, json.Unmarshal(raw, &reqPayload))
		captured = append(captured, reqPayload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": http.StatusOK,
			"headers": map[string]any{
				"Content-Type": "application/json",
			},
			"body": `{"ok":true}`,
		})
	}))
	defer sidecar.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL: "https://sora.chatgpt.com/backend",
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled:             true,
					BaseURL:             sidecar.URL,
					SessionReuseEnabled: true,
					SessionTTLSeconds:   3600,
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{ID: 1001}

	req1, err := http.NewRequest(http.MethodGet, "https://sora.chatgpt.com/backend/me", nil)
	require.NoError(t, err)
	_, err = client.doHTTP(req1, "http://127.0.0.1:18080", account)
	require.NoError(t, err)

	req2, err := http.NewRequest(http.MethodGet, "https://sora.chatgpt.com/backend/me", nil)
	require.NoError(t, err)
	_, err = client.doHTTP(req2, "http://127.0.0.1:18080", account)
	require.NoError(t, err)

	require.Len(t, captured, 2)
	require.NotEmpty(t, captured[0].SessionKey)
	require.Equal(t, captured[0].SessionKey, captured[1].SessionKey)
}

func TestSoraDirectClient_DoRequestWithProxy_CloudflareChallengeSetsCooldownAndSkipsRetry(t *testing.T) {
	var sidecarCalls int32
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sidecarCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": http.StatusForbidden,
			"headers": map[string]any{
				"cf-ray":       "9d05d73dec4d8c8e-GRU",
				"content-type": "text/html",
			},
			"body": `<!DOCTYPE html><html><head><title>Just a moment...</title></head><body><script>window._cf_chl_opt={};</script></body></html>`,
		})
	}))
	defer sidecar.Close()

	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				BaseURL:                            "https://sora.chatgpt.com/backend",
				MaxRetries:                         3,
				CloudflareChallengeCooldownSeconds: 60,
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled:     true,
					BaseURL:     sidecar.URL,
					Impersonate: "chrome131",
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	headers := http.Header{}

	_, _, err := client.doRequestWithProxy(
		context.Background(),
		&Account{ID: 99},
		"http://127.0.0.1:18080",
		http.MethodGet,
		"https://sora.chatgpt.com/backend/me",
		headers,
		nil,
		true,
	)
	require.Error(t, err)
	var upstreamErr *SoraUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusForbidden, upstreamErr.StatusCode)
	require.Equal(t, int32(1), atomic.LoadInt32(&sidecarCalls), "challenge should not trigger retry loop")

	_, _, err = client.doRequestWithProxy(
		context.Background(),
		&Account{ID: 99},
		"http://127.0.0.1:18080",
		http.MethodGet,
		"https://sora.chatgpt.com/backend/me",
		headers,
		nil,
		true,
	)
	require.Error(t, err)
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusTooManyRequests, upstreamErr.StatusCode)
	require.Contains(t, upstreamErr.Message, "cooling down")
	require.Contains(t, upstreamErr.Message, "cf-ray")
	require.Equal(t, int32(1), atomic.LoadInt32(&sidecarCalls), "cooldown should block outbound request")
}

func TestSoraDirectClient_SidecarSessionKey_SkipsWhenAccountMissing(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled:             true,
					SessionReuseEnabled: true,
					SessionTTLSeconds:   3600,
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	require.Equal(t, "", client.sidecarSessionKey(nil, "http://127.0.0.1:18080"))
	require.Empty(t, client.sidecarSessions)
}

func TestSoraDirectClient_SidecarSessionKey_PrunesExpiredAndRecreates(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled:             true,
					SessionReuseEnabled: true,
					SessionTTLSeconds:   3600,
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{ID: 123}
	key := soraAccountProxyKey(account, "http://127.0.0.1:18080")
	client.sidecarSessions[key] = soraSidecarSessionEntry{
		SessionKey: "sora-expired",
		ExpiresAt:  time.Now().Add(-time.Minute),
		LastUsedAt: time.Now().Add(-2 * time.Minute),
	}

	sessionKey := client.sidecarSessionKey(account, "http://127.0.0.1:18080")
	require.NotEmpty(t, sessionKey)
	require.NotEqual(t, "sora-expired", sessionKey)
	require.Len(t, client.sidecarSessions, 1)
}

func TestSoraDirectClient_SidecarSessionKey_TTLZeroKeepsLongLivedSession(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				CurlCFFISidecar: config.SoraCurlCFFISidecarConfig{
					Enabled:             true,
					SessionReuseEnabled: true,
					SessionTTLSeconds:   0,
				},
			},
		},
	}
	client := NewSoraDirectClient(cfg, nil, nil)
	account := &Account{ID: 456}

	first := client.sidecarSessionKey(account, "http://127.0.0.1:18080")
	second := client.sidecarSessionKey(account, "http://127.0.0.1:18080")
	require.NotEmpty(t, first)
	require.Equal(t, first, second)

	key := soraAccountProxyKey(account, "http://127.0.0.1:18080")
	entry, ok := client.sidecarSessions[key]
	require.True(t, ok)
	require.True(t, entry.ExpiresAt.After(time.Now().Add(300*24*time.Hour)))
}
