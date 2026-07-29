package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
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
	require.True(t, GrokMediaEndpointVideoStatus.IsVideoLookupRequest())
	require.True(t, GrokMediaEndpointVideoContent.IsVideoLookupRequest())
	require.False(t, GrokMediaEndpointVideoContent.RequiresRequestBody())
}

func TestForwardGrokMediaGenerationBillingGateFailureLeavesResponseBodyEmpty(t *testing.T) {
	requestBody := []byte(`{"model":"grok-imagine-image-quality","prompt":"draw a cat","n":1}`)
	upstream := &grokMediaContentUpstreamStub{
		responses: []*http.Response{{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"grok-generation-request"},
			},
			Body: io.NopCloser(bytes.NewReader([]byte(
				`{"data":[{"url":"https://images.example/secret-image.png"}]}`,
			))),
		}},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, recorder := grokMediaContentTestContext(
		http.MethodPost,
		"https://api.example/v1/images/generations",
		map[string]string{"Content-Type": "application/json"},
	)
	billingErr := errors.New("billing gate rejected generation")
	gateCalls := 0
	ctx := WithOpenAIForwardResultBillingGate(
		context.Background(),
		NewOpenAIForwardResultBillingGate(func(result *OpenAIForwardResult) error {
			gateCalls++
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, "grok-imagine-image-quality", result.BillingModel)
			return billingErr
		}),
	)

	result, err := svc.ForwardGrokMedia(
		ctx,
		c,
		grokMediaContentTestAccount(),
		GrokMediaEndpointImagesGenerations,
		"",
		requestBody,
		"application/json",
	)

	require.ErrorIs(t, err, billingErr)
	require.NotNil(t, result)
	require.Equal(t, 1, gateCalls)
	require.Empty(t, recorder.Body.String(), "upstream success body must not be exposed before billing commits")
	require.Empty(t, recorder.Header().Get("Content-Type"))
	require.False(t, IsResponseCommitted(c))
	require.Len(t, upstream.requests, 1)
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
}

func TestGrokMediaUsageRejectsSuccessfulResponseWithoutTerminalOutput(t *testing.T) {
	requestInfo := ParseGrokMediaRequest(
		"application/json",
		[]byte(`{"model":"grok-imagine-image-quality","n":3,"resolution":"720p","duration":9}`),
	)
	tests := []struct {
		name      string
		endpoints []GrokMediaEndpoint
		bodies    [][]byte
		wantError string
	}{
		{
			name: "images",
			endpoints: []GrokMediaEndpoint{
				GrokMediaEndpointImagesGenerations,
				GrokMediaEndpointImagesEdits,
			},
			bodies: [][]byte{
				nil,
				[]byte("not-json"),
				[]byte(`{}`),
				[]byte(`{"data":[]}`),
			},
			wantError: "without image output",
		},
		{
			name: "videos",
			endpoints: []GrokMediaEndpoint{
				GrokMediaEndpointVideosGenerations,
				GrokMediaEndpointVideosEdits,
				GrokMediaEndpointVideosExtensions,
			},
			bodies: [][]byte{
				nil,
				[]byte("not-json"),
				[]byte(`{}`),
				[]byte(`{"status":"queued"}`),
			},
			wantError: "without a video request id",
		},
	}

	for _, tt := range tests {
		for _, endpoint := range tt.endpoints {
			for bodyIndex, body := range tt.bodies {
				t.Run(fmt.Sprintf("%s/%s/body_%d", tt.name, endpoint, bodyIndex), func(t *testing.T) {
					metadata, err := grokMediaUsageFromResponse(endpoint, requestInfo, body)

					require.ErrorContains(t, err, tt.wantError)
					require.Zero(t, metadata.ImageCount)
					require.Zero(t, metadata.VideoCount)
					require.Empty(t, metadata.ResponseID)
				})
			}
		}
	}
}

func TestGrokMediaUsageAcceptsRecognizedTerminalOutputs(t *testing.T) {
	requestInfo := ParseGrokMediaRequest(
		"application/json",
		[]byte(`{"model":"grok-imagine-image-quality","n":3,"size":"1024x1024"}`),
	)
	imageMetadata, err := grokMediaUsageFromResponse(
		GrokMediaEndpointImagesGenerations,
		requestInfo,
		[]byte(`{"data":[{"url":"https://images.example/one.png"},{"b64_json":"aW1hZ2U="}]}`),
	)
	require.NoError(t, err)
	require.Equal(t, 2, imageMetadata.ImageCount)

	videoMetadata, err := grokMediaUsageFromResponse(
		GrokMediaEndpointVideosGenerations,
		requestInfo,
		[]byte(`{"data":{"request_id":"video_req_123"}}`),
	)
	require.NoError(t, err)
	require.Equal(t, "video_req_123", videoMetadata.ResponseID)
	require.Equal(t, 1, videoMetadata.VideoCount)
}

func TestForwardGrokMediaInvalidGenerationSuccessDoesNotBillOrExposeBody(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    GrokMediaEndpoint
		requestBody []byte
		response    string
	}{
		{
			name:        "image response has no output despite requested count",
			endpoint:    GrokMediaEndpointImagesGenerations,
			requestBody: []byte(`{"model":"grok-imagine-image-quality","prompt":"draw a cat","n":3}`),
			response:    `{"data":[]}`,
		},
		{
			name:        "video response has no request id",
			endpoint:    GrokMediaEndpointVideosGenerations,
			requestBody: []byte(`{"model":"grok-imagine-video-1.5","prompt":"draw a cat","duration":9}`),
			response:    `{"status":"queued"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &grokMediaContentUpstreamStub{
				responses: []*http.Response{{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
						"X-Request-Id": []string{"invalid-generation-success"},
					},
					Body: io.NopCloser(strings.NewReader(tt.response)),
				}},
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			c, recorder := grokMediaContentTestContext(
				http.MethodPost,
				"https://api.example/v1/media/generations",
				map[string]string{"Content-Type": "application/json"},
			)
			gateCalls := 0
			ctx := WithOpenAIForwardResultBillingGate(
				context.Background(),
				NewOpenAIForwardResultBillingGate(func(*OpenAIForwardResult) error {
					gateCalls++
					return nil
				}),
			)

			result, err := svc.ForwardGrokMedia(
				ctx,
				c,
				grokMediaContentTestAccount(),
				tt.endpoint,
				"",
				tt.requestBody,
				"application/json",
			)

			require.Error(t, err)
			require.Nil(t, result)
			require.Zero(t, gateCalls)
			require.Equal(t, http.StatusBadGateway, recorder.Code)
			require.Equal(t, "Upstream request failed", gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
			require.NotContains(t, recorder.Body.String(), tt.response)
			require.Len(t, upstream.requests, 1)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
		})
	}
}

func TestGrokMediaVideoRequestSessionHashIsOwnerScoped(t *testing.T) {
	t.Parallel()

	base := GrokMediaVideoRequestSessionHash("request-1", 10, 20)
	require.NotEmpty(t, base)
	require.NotEqual(t, base, GrokMediaVideoRequestSessionHash("request-1", 11, 20))
	require.NotEqual(t, base, GrokMediaVideoRequestSessionHash("request-1", 10, 21))
	require.NotEqual(t, base, GrokMediaVideoRequestSessionHash("request-2", 10, 20))
	require.Empty(t, GrokMediaVideoRequestSessionHash("", 10, 20))
	require.Empty(t, GrokMediaVideoRequestSessionHash("request-1", 0, 20))
	require.Empty(t, GrokMediaVideoRequestSessionHash("request-1", 10, 0))
}

type grokOwnerBindingCache struct {
	GatewayCache
	bindings map[string]int64
	strings  map[string]string
	ttl      time.Duration
}

func (c *grokOwnerBindingCache) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (c *grokOwnerBindingCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	if accountID, ok := c.bindings[c.key(groupID, sessionHash)]; ok {
		return accountID, nil
	}
	return 0, ErrGatewaySessionStringNotFound
}

func (c *grokOwnerBindingCache) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if c.bindings == nil {
		c.bindings = make(map[string]int64)
	}
	c.bindings[c.key(groupID, sessionHash)] = accountID
	c.ttl = ttl
	return nil
}

func (c *grokOwnerBindingCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.bindings, c.key(groupID, sessionHash))
	return nil
}

func (c *grokOwnerBindingCache) GetSessionString(_ context.Context, groupID int64, sessionHash string) (string, error) {
	if value, ok := c.strings[c.key(groupID, sessionHash)]; ok {
		return value, nil
	}
	return "", ErrGatewaySessionStringNotFound
}

func (c *grokOwnerBindingCache) SetSessionString(_ context.Context, groupID int64, sessionHash, value string, ttl time.Duration) error {
	if c.strings == nil {
		c.strings = make(map[string]string)
	}
	c.strings[c.key(groupID, sessionHash)] = value
	c.ttl = ttl
	return nil
}

func TestGrokMediaVideoRequestBindingRejectsOtherOwnersAndGroups(t *testing.T) {
	t.Parallel()

	cache := &grokOwnerBindingCache{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 90
	svc := &OpenAIGatewayService{cache: cache, cfg: cfg}
	groupID := int64(7)
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(
		context.Background(), &groupID, "request-1", 10, 20, 30,
	))
	require.Equal(t, 90*time.Second, cache.ttl)

	accountID, err := svc.ResolveGrokMediaVideoRequestAccount(
		context.Background(), &groupID, "request-1", 10, 20,
	)
	require.NoError(t, err)
	require.Equal(t, int64(30), accountID)

	otherGroupID := int64(8)
	for _, lookup := range []struct {
		groupID  *int64
		userID   int64
		apiKeyID int64
	}{
		{groupID: &groupID, userID: 11, apiKeyID: 20},
		{groupID: &groupID, userID: 10, apiKeyID: 21},
		{groupID: &otherGroupID, userID: 10, apiKeyID: 20},
	} {
		accountID, err = svc.ResolveGrokMediaVideoRequestAccount(
			context.Background(), lookup.groupID, "request-1", lookup.userID, lookup.apiKeyID,
		)
		require.Error(t, err)
		require.Zero(t, accountID)
	}

	_, err = (&OpenAIGatewayService{}).ResolveGrokMediaVideoRequestAccount(
		context.Background(), &groupID, "request-1", 10, 20,
	)
	require.ErrorContains(t, err, "cache is unavailable")
}

func TestGrokMediaVideoOwnerBindingSurvivesRoutingEviction(t *testing.T) {
	t.Parallel()

	cache := &grokOwnerBindingCache{}
	svc := &OpenAIGatewayService{cache: cache}
	groupID := int64(7)
	const (
		requestID = "request-recover"
		userID    = int64(10)
		apiKeyID  = int64(20)
		accountID = int64(30)
	)
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(
		context.Background(), &groupID, requestID, userID, apiKeyID, accountID,
	))

	ownerKey := grokMediaVideoOwnerBindingKey(requestID, userID, apiKeyID)
	routingKey := svc.openAISessionCacheKey(GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID))
	require.NotEqual(t, ownerKey, routingKey)
	require.Equal(t, "30", cache.strings[cache.key(groupID, ownerKey)])
	require.Equal(t, accountID, cache.bindings[cache.key(groupID, routingKey)])

	// Scheduler eviction removes only the routing/sticky record when the
	// account is temporarily blocked. Ownership must remain authoritative.
	require.NoError(t, cache.DeleteSessionAccountID(context.Background(), groupID, routingKey))
	_, routingExists := cache.bindings[cache.key(groupID, routingKey)]
	require.False(t, routingExists)

	resolvedID, err := svc.ResolveGrokMediaVideoRequestAccount(
		context.Background(), &groupID, requestID, userID, apiKeyID,
	)
	require.NoError(t, err)
	require.Equal(t, accountID, resolvedID)
	require.Equal(t, accountID, cache.bindings[cache.key(groupID, routingKey)], "lookup should restore scheduler routing after cooldown")
}

func TestGrokMediaVideoOwnerBindingMigratesLegacyRoutingRecord(t *testing.T) {
	t.Parallel()

	cache := &grokOwnerBindingCache{}
	svc := &OpenAIGatewayService{cache: cache}
	groupID := int64(7)
	const (
		requestID = "request-legacy"
		userID    = int64(10)
		apiKeyID  = int64(20)
		accountID = int64(30)
	)
	routingKey := svc.openAISessionCacheKey(GrokMediaVideoRequestSessionHash(requestID, userID, apiKeyID))
	require.NoError(t, cache.SetSessionAccountID(context.Background(), groupID, routingKey, accountID, time.Minute))

	resolvedID, err := svc.ResolveGrokMediaVideoRequestAccount(
		context.Background(), &groupID, requestID, userID, apiKeyID,
	)
	require.NoError(t, err)
	require.Equal(t, accountID, resolvedID)
	ownerKey := grokMediaVideoOwnerBindingKey(requestID, userID, apiKeyID)
	require.Equal(t, "30", cache.strings[cache.key(groupID, ownerKey)])
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
		metadata, err := grokMediaUsageFromResponse(endpoint, requestInfo, []byte(`{"data":{"request_id":"video_req_123"}}`))

		require.NoError(t, err)
		require.Equal(t, "video_req_123", metadata.ResponseID, endpoint)
		require.Equal(t, 1, metadata.VideoCount, endpoint)
		require.Equal(t, VideoBillingResolution720P, metadata.VideoResolution, endpoint)
		require.Equal(t, 9, metadata.VideoDurationSeconds, endpoint)
		require.Equal(t, 1, metadata.ImageCount, endpoint)
		require.Empty(t, metadata.ImageSize, endpoint)
		require.Empty(t, metadata.ImageInputSize, endpoint)
	}
}

func TestBuildGrokMediaURLSupportsVideoMutationsAndHonorsOAuthRelay(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = false
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"relay.example.test"}
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": "https://relay.example.test/tenant/xai/v1",
		},
	}

	editURL, err := buildGrokMediaURL(context.Background(), account, cfg, nil, GrokMediaEndpointVideosEdits, "")
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/tenant/xai/v1/videos/edits", editURL)

	extensionURL, err := buildGrokMediaURL(context.Background(), account, cfg, nil, GrokMediaEndpointVideosExtensions, "")
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/tenant/xai/v1/videos/extensions", extensionURL)

	contentURL, err := buildGrokMediaURL(context.Background(), account, cfg, nil, GrokMediaEndpointVideoContent, "task/one")
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/tenant/xai/v1/videos/task%2Fone/content", contentURL)
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
