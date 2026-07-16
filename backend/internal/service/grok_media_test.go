package service

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGrokVideoMutationEndpointsAreBillableGenerationRequests(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []GrokMediaEndpoint{
		GrokMediaEndpointVideosGenerations,
		GrokMediaEndpointVideosEdits,
		GrokMediaEndpointVideosExtensions,
	} {
		require.True(t, endpoint.IsGenerationRequest(), endpoint)
		require.True(t, endpoint.IsVideoMutationRequest(), endpoint)
	}
	require.False(t, GrokMediaEndpointVideoStatus.IsGenerationRequest())
	require.False(t, GrokMediaEndpointVideoStatus.IsVideoMutationRequest())
}

func TestParseGrokMediaJSONVideoBillingMetadata(t *testing.T) {
	t.Parallel()

	info := ParseGrokMediaRequest("application/json", []byte(`{
		"model":" grok-imagine-video-1.5 ",
		"prompt":"extend the scene",
		"resolution":"HD",
		"duration":20
	}`))

	require.Equal(t, "grok-imagine-video-1.5", info.Model)
	require.Equal(t, VideoBillingResolution720P, info.Resolution)
	require.Equal(t, VideoBillingMaxDurationSeconds, info.DurationSeconds)

	defaults := ParseGrokMediaRequest("application/json", []byte(`{"model":"grok-imagine-video"}`))
	require.Equal(t, VideoBillingResolution480P, defaults.Resolution)
	require.Equal(t, VideoBillingDefaultDurationSeconds, defaults.DurationSeconds)
}

func TestParseGrokMediaMultipartVideoBillingMetadata(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "grok-imagine-video-1.5"))
	require.NoError(t, writer.WriteField("resolution", "1080"))
	require.NoError(t, writer.WriteField("duration", "12"))
	require.NoError(t, writer.Close())

	info := ParseGrokMediaRequest(writer.FormDataContentType(), body.Bytes())

	require.Equal(t, VideoBillingResolution1080P, info.Resolution)
	require.Equal(t, 12, info.DurationSeconds)
}

func TestGrokVideoMutationJSONBodyIsPreserved(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"grok-imagine-video-1.5","video":{"id":"video_123"},"custom":{"keep":true}}`)
	for _, endpoint := range []GrokMediaEndpoint{
		GrokMediaEndpointVideosEdits,
		GrokMediaEndpointVideosExtensions,
	} {
		prepared, contentType, err := prepareGrokMediaForwardBody(endpoint, body, "application/json")
		require.NoError(t, err)
		require.Equal(t, body, prepared, endpoint)

		normalized, contentType, err := normalizeGrokMediaForwardBody(endpoint, prepared, contentType)
		require.NoError(t, err)
		require.Equal(t, body, normalized, endpoint)

		sanitized, _, err := sanitizeGrokMediaForwardBody(endpoint, normalized, contentType)
		require.NoError(t, err)
		require.Equal(t, body, sanitized, endpoint)
	}
}

func TestGrokVideoMutationUsageIncludesPerSecondBillingMetadata(t *testing.T) {
	t.Parallel()

	requestInfo := ParseGrokMediaRequest("application/json", []byte(`{
		"model":"grok-imagine-video-1.5",
		"resolution":"720p",
		"duration":9
	}`))

	for _, endpoint := range []GrokMediaEndpoint{
		GrokMediaEndpointVideosGenerations,
		GrokMediaEndpointVideosEdits,
		GrokMediaEndpointVideosExtensions,
	} {
		metadata := grokMediaUsageFromResponse(endpoint, requestInfo, []byte(`{"data":{"request_id":"video_req_123"}}`))

		require.Equal(t, "video_req_123", metadata.ResponseID, endpoint)
		require.Equal(t, 1, metadata.VideoCount, endpoint)
		require.Equal(t, VideoBillingResolution720P, metadata.VideoResolution, endpoint)
		require.Equal(t, 9, metadata.VideoDurationSeconds, endpoint)
		require.Equal(t, 1, metadata.ImageCount, endpoint)
		require.Empty(t, metadata.ImageSize, endpoint)
		require.Empty(t, metadata.ImageInputSize, endpoint)
	}
}

func TestBuildGrokMediaURLSupportsVideoMutationsAndKeepsOAuthOnOfficialAPI(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": "https://untrusted.example.invalid",
		},
	}

	editURL, err := buildGrokMediaURL(context.Background(), account, nil, nil, GrokMediaEndpointVideosEdits, "")
	require.NoError(t, err)
	require.Equal(t, "https://api.x.ai/v1/videos/edits", editURL)

	extensionURL, err := buildGrokMediaURL(context.Background(), account, nil, nil, GrokMediaEndpointVideosExtensions, "")
	require.NoError(t, err)
	require.Equal(t, "https://api.x.ai/v1/videos/extensions", extensionURL)
}

type grokURLSettingRepo struct {
	SettingRepository
	value string
}

func (r *grokURLSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyUpstreamURLAllowlistExtraHosts {
		return "", ErrSettingNotFound
	}
	return r.value, nil
}

func TestBuildGrokURLsApplyDynamicAllowlist(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"api.x.ai", "cli-chat-proxy.grok.com"}
	settings := &SettingService{
		cfg: cfg,
		settingRepo: &grokURLSettingRepo{
			value: `["grok-proxy.example.com"]`,
		},
	}
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "secret",
			"base_url": "https://grok-proxy.example.com/v1",
		},
	}

	responsesURL, err := buildGrokResponsesURL(context.Background(), account, cfg, settings)
	require.NoError(t, err)
	require.Equal(t, "https://grok-proxy.example.com/v1/responses", responsesURL)

	chatURL, err := buildGrokChatCompletionsURL(context.Background(), account, cfg, settings)
	require.NoError(t, err)
	require.Equal(t, "https://grok-proxy.example.com/v1/chat/completions", chatURL)

	mediaURL, err := buildGrokMediaURL(context.Background(), account, cfg, settings, GrokMediaEndpointImagesGenerations, "")
	require.NoError(t, err)
	require.Equal(t, "https://grok-proxy.example.com/v1/images/generations", mediaURL)

	account.Credentials["base_url"] = "https://not-allowed.example.com/v1"
	_, err = buildGrokResponsesURL(context.Background(), account, cfg, settings)
	require.ErrorContains(t, err, "URL security policy")
}

func TestBuildGrokAPIKeyURLFailsClosedWithoutSecurityConfig(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "secret", "base_url": "https://api.x.ai/v1"},
	}
	_, err := buildGrokResponsesURL(context.Background(), account, nil, nil)
	require.ErrorContains(t, err, "security configuration is required")
}

func TestAppendGrokMediaRequestQueryKeepsTargetAuthorityAndPath(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/request-1?expand=media&host=evil.example&path=/override", nil)

	target, err := appendGrokMediaRequestQuery("https://api.x.ai/v1/videos/request-1", c)
	require.NoError(t, err)
	parsed, err := url.Parse(target)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "api.x.ai", parsed.Host)
	require.Equal(t, "/v1/videos/request-1", parsed.Path)
	require.Equal(t, "media", parsed.Query().Get("expand"))
	require.Equal(t, "evil.example", parsed.Query().Get("host"))
	require.Equal(t, "/override", parsed.Query().Get("path"))
}

func TestAppendGrokMediaRequestQueryRejectsCredentialParameters(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/request-1?api_key=must-not-forward", nil)

	target, err := appendGrokMediaRequestQuery("https://api.x.ai/v1/videos/request-1", c)
	require.Error(t, err)
	require.Empty(t, target)
	require.NotContains(t, err.Error(), "must-not-forward")
}

type grokMediaStateRepo struct {
	AccountRepository
	tempUnschedCalls int
	reason           string
}

func (r *grokMediaStateRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.tempUnschedCalls++
	r.reason = reason
	return nil
}

func TestHandleGrokMediaErrorUpdatesAccountBeforeEarlyReturn(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*gin.Context, *Account)
	}{
		{
			name: "passthrough rule",
			prepare: func(c *gin.Context, _ *Account) {
				responseCode := http.StatusTooManyRequests
				message := "rate limited"
				rules := &ErrorPassthroughService{}
				rules.setLocalCache([]*model.ErrorPassthroughRule{{
					ID:              1,
					Name:            "grok 429",
					Enabled:         true,
					Priority:        1,
					ErrorCodes:      []int{http.StatusTooManyRequests},
					MatchMode:       model.MatchModeAll,
					PassthroughCode: false,
					ResponseCode:    &responseCode,
					PassthroughBody: false,
					CustomMessage:   &message,
				}})
				BindErrorPassthroughService(c, rules)
			},
		},
		{
			name: "non-custom error code",
			prepare: func(_ *gin.Context, account *Account) {
				account.Type = AccountTypeAPIKey
				account.Credentials["custom_error_codes_enabled"] = true
				account.Credentials["custom_error_codes"] = []any{float64(http.StatusInternalServerError)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			account := &Account{ID: 42, Name: "grok", Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{}}
			tt.prepare(c, account)
			repo := &grokMediaStateRepo{}
			svc := &OpenAIGatewayService{accountRepo: repo, cfg: &config.Config{}}
			resp := &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"30"}},
				Body:       http.NoBody,
			}

			_, err := svc.handleGrokMediaErrorResponse(context.Background(), resp, c, account, "req-1", "grok-4")
			require.Error(t, err)
			require.Equal(t, 1, repo.tempUnschedCalls)
			require.Equal(t, "grok rate limited", repo.reason)
		})
	}
}
