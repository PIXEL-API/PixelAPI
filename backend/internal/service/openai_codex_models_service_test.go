package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

type codexModelsHTTPUpstreamStub struct {
	do func(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error)
}

func (s *codexModelsHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return s.do(req, proxyURL, accountID, accountConcurrency)
}

func (s *codexModelsHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func newCodexModelsAPIKeyTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
		httpUpstream: upstream,
	}
}

func newCodexModelsAPIKeyTestAccount(baseURL string) *Account {
	credentials := map[string]any{"api_key": "sk-upstream"}
	if baseURL != "" {
		credentials["base_url"] = baseURL
	}
	return &Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: credentials,
		Concurrency: 3,
	}
}

func newCodexModelsOAuthTestAccount() *Account {
	return &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acc-123",
		},
	}
}

func newCodexModelsAgentIdentityTestAccount(t *testing.T, id int64, taskID string) *Account {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode":          OpenAIAuthModeAgentIdentity,
			"agent_runtime_id":   "runtime-models-test",
			"agent_private_key":  base64.StdEncoding.EncodeToString(der),
			"task_id":            taskID,
			"chatgpt_account_id": "acc-agent",
		},
	}
}

func decodeCodexModelsAssertionTask(t *testing.T, authorization string) string {
	t.Helper()
	const prefix = "AgentAssertion "
	require.True(t, strings.HasPrefix(authorization, prefix))
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(authorization, prefix))
	require.NoError(t, err)
	var envelope struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(payload, &envelope))
	return envelope.TaskID
}

func TestIsRetryableCodexModelsManifestTransportError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "nil"},
		{name: "configuration error", err: errors.New("invalid proxy URL")},
		{name: "canceled request", err: context.Canceled},
		{name: "redirect policy", err: &url.Error{Op: "Get", URL: "https://example.test", Err: errors.New("stopped after 10 redirects")}},
		{name: "deadline", err: context.DeadlineExceeded, retryable: true},
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF, retryable: true},
		{name: "closed connection", err: net.ErrClosed, retryable: true},
		{name: "network operation", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}, retryable: true},
		{name: "DNS", err: &net.DNSError{Err: "temporary failure", Name: "upstream.example"}, retryable: true},
		{name: "typed HTTP2 GOAWAY", err: http2.GoAwayError{ErrCode: http2.ErrCodeNo}, retryable: true},
		{name: "stdlib HTTP2 GOAWAY", err: errors.New("http2: server sent GOAWAY and closed the connection"), retryable: true},
		{name: "stdlib HTTP2 refused stream", err: errors.New("stream error: stream ID 3; REFUSED_STREAM"), retryable: true},
		{name: "stdlib HTTP2 connection error", err: errors.New("connection error: PROTOCOL_ERROR"), retryable: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.retryable, isRetryableCodexModelsManifestTransportError(test.err))
		})
	}
}

func TestFetchCodexModelsManifestOAuthPassthrough(t *testing.T) {
	manifestBody := `{"models":[{"slug":"gpt-5.6"}]}`
	var gotAuth, gotAccountID, gotOriginator, gotVersion, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("Originator")
		gotVersion = r.URL.Query().Get("client_version")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("ETag", `W/"manifest"`)
		_, _ = w.Write([]byte(manifestBody))
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	t.Cleanup(func() { chatgptCodexModelsURL = original })

	manifest, err := (&OpenAIGatewayService{}).FetchCodexModelsManifest(context.Background(), newCodexModelsOAuthTestAccount(), "0.144.0", "")
	require.NoError(t, err)
	require.Equal(t, manifestBody, string(manifest.Body))
	require.Equal(t, `W/"manifest"`, manifest.ETag)
	require.Equal(t, "Bearer test-access-token", gotAuth)
	require.Equal(t, "acc-123", gotAccountID)
	require.Equal(t, "codex_cli_rs", gotOriginator)
	require.Equal(t, "0.144.0", gotVersion)
	require.Equal(t, codexCLIUserAgent, gotUserAgent)
}

func TestFetchCodexModelsManifestOAuthDefaultsAndNotModified(t *testing.T) {
	var gotVersion, gotIfNoneMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.URL.Query().Get("client_version")
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", `W/"manifest"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	t.Cleanup(func() { chatgptCodexModelsURL = original })

	manifest, err := (&OpenAIGatewayService{}).FetchCodexModelsManifest(
		context.Background(),
		newCodexModelsOAuthTestAccount(),
		"",
		`W/"manifest"`,
	)
	require.NoError(t, err)
	require.True(t, manifest.NotModified)
	require.Equal(t, openAICodexProbeVersion, gotVersion)
	require.Equal(t, `W/"manifest"`, gotIfNoneMatch)

	missingToken := newCodexModelsOAuthTestAccount()
	delete(missingToken.Credentials, "access_token")
	_, err = (&OpenAIGatewayService{}).FetchCodexModelsManifest(context.Background(), missingToken, "0.144.0", "")
	require.ErrorContains(t, err, "access token")
}

func TestFetchCodexModelsManifestAgentIdentityRecoversOnceAndRedacts(t *testing.T) {
	account := newCodexModelsAgentIdentityTestAccount(t, 0, "task-old")
	var modelsCalls atomic.Int32
	var registerCalls atomic.Int32
	var assertions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/task/register") {
			registerCalls.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"task-new"}`))
			return
		}
		modelsCalls.Add(1)
		assertions = append(assertions, r.Header.Get("Authorization"))
		if modelsCalls.Load() == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"invalid_task_id"}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"runtime-models-test task-new AgentAssertion secret"}`))
	}))
	defer server.Close()

	originalModelsURL := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	t.Cleanup(func() { chatgptCodexModelsURL = originalModelsURL })
	originalAuthURL := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = originalAuthURL })

	_, err := (&OpenAIGatewayService{}).FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	require.Error(t, err)
	require.Equal(t, int32(2), modelsCalls.Load())
	require.Equal(t, int32(1), registerCalls.Load())
	require.Len(t, assertions, 2)
	require.Equal(t, "task-old", decodeCodexModelsAssertionTask(t, assertions[0]))
	require.Equal(t, "task-new", decodeCodexModelsAssertionTask(t, assertions[1]))
	require.NotContains(t, err.Error(), "runtime-models-test")
	require.NotContains(t, err.Error(), "task-new")
	require.NotContains(t, err.Error(), "AgentAssertion secret")
	require.Contains(t, err.Error(), "[redacted]")
}

func TestFetchCodexModelsManifestAPIKeyCustomUpstreamAndCache(t *testing.T) {
	manifestBody := `{"models":[{"slug":"gpt-5.6"}]}`
	var calls atomic.Int32
	var gotRequest *http.Request
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
		calls.Add(1)
		gotRequest = req.Clone(req.Context())
		header := make(http.Header)
		header.Set("ETag", `W/"api-key-manifest"`)
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(manifestBody))}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1?tenant=one")

	manifest, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	require.NoError(t, err)
	require.Equal(t, manifestBody, string(manifest.Body))
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "https://upstream.example/v1/models?client_version=0.144.0&tenant=one", gotRequest.URL.String())
	require.Equal(t, "Bearer sk-upstream", gotRequest.Header.Get("Authorization"))
	require.Equal(t, "codex_cli_rs", gotRequest.Header.Get("Originator"))
	require.Equal(t, codexCLIUserAgent, gotRequest.Header.Get("User-Agent"))
	require.Empty(t, gotRequest.Header.Get("chatgpt-account-id"))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(gotRequest.Context()))

	cached, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", `"api-key-manifest"`)
	require.NoError(t, err)
	require.True(t, cached.NotModified)
	require.Equal(t, `W/"api-key-manifest"`, cached.ETag)
	require.Equal(t, int32(1), calls.Load(), "fresh cache must avoid another upstream request")
}

func TestFetchCodexModelsManifestAPIKeyServesStaleWhileRefreshing(t *testing.T) {
	var calls atomic.Int32
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		call := calls.Add(1)
		header := make(http.Header)
		if call == 1 {
			header.Set("ETag", `"first"`)
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`{"models":[],"version":1}`))}, nil
		}
		require.Equal(t, `"first"`, req.Header.Get("If-None-Match"))
		if call == 2 {
			close(refreshStarted)
			<-releaseRefresh
		}
		header.Set("ETag", `"second"`)
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`{"models":[],"version":2}`))}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")

	first, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[],"version":1}`, string(first.Body))

	s.codexModelsManifestCache.mu.Lock()
	for key, entry := range s.codexModelsManifestCache.entries {
		entry.expiresAt = time.Now().Add(-time.Second)
		entry.staleUntil = time.Now().Add(time.Minute)
		s.codexModelsManifestCache.entries[key] = entry
	}
	s.codexModelsManifestCache.mu.Unlock()

	stale, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[],"version":1}`, string(stale.Body))
	<-refreshStarted
	close(releaseRefresh)

	require.Eventually(t, func() bool {
		refreshed, fetchErr := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
		return fetchErr == nil && string(refreshed.Body) == `{"models":[],"version":2}`
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(2), calls.Load())
}

func TestFetchCodexModelsManifestCacheIsolationAndBodyLimits(t *testing.T) {
	var calls atomic.Int32
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		call := calls.Add(1)
		body := `{"models":[],"call":` + strconv.Itoa(int(call)) + `}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	first := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
	second := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
	second.ID = first.ID + 1

	_, err := s.FetchCodexModelsManifest(context.Background(), first, "0.144.0", "")
	require.NoError(t, err)
	_, err = s.FetchCodexModelsManifest(context.Background(), second, "0.144.0", "")
	require.NoError(t, err)
	_, err = s.FetchCodexModelsManifest(context.Background(), first, "0.145.0", "")
	require.NoError(t, err)
	require.Equal(t, int32(3), calls.Load(), "account and client version must isolate cache entries")

	tooLarge := strings.Repeat("x", int(codexModelsManifestBodyLimit+1))
	largeUpstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tooLarge))}, nil
	}}
	_, err = newCodexModelsAPIKeyTestService(largeUpstream).FetchCodexModelsManifest(context.Background(), first, "0.146.0", "")
	require.ErrorContains(t, err, ErrUpstreamResponseBodyTooLarge.Error())
	require.False(t, IsRetryableCodexModelsManifestError(err), "oversized successful bodies must not trigger failover downloads")
}

func TestFetchCodexModelsManifestCacheKeyIsolatesRequestIdentity(t *testing.T) {
	var calls atomic.Int32
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"models":[]}`)),
		}, nil
	}}
	gatewayService := newCodexModelsAPIKeyTestService(upstream)
	fetchTwice := func(t *testing.T, account *Account, clientVersion string) {
		t.Helper()
		for range 2 {
			_, err := gatewayService.FetchCodexModelsManifest(context.Background(), account, clientVersion, "")
			require.NoError(t, err)
		}
	}

	baseAccount := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
	fetchTwice(t, baseAccount, "0.144.0")
	require.Equal(t, int32(1), calls.Load(), "identical request identity must reuse the fresh cache entry")

	tests := []struct {
		name          string
		clientVersion string
		account       func() *Account
	}{
		{
			name:          "api key token",
			clientVersion: "0.144.0",
			account: func() *Account {
				account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
				account.Credentials["api_key"] = "sk-other"
				return account
			},
		},
		{
			name:          "base URL",
			clientVersion: "0.144.0",
			account: func() *Account {
				return newCodexModelsAPIKeyTestAccount("https://other-upstream.example/v1")
			},
		},
		{
			name:          "client version",
			clientVersion: "0.145.0",
			account: func() *Account {
				return newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
			},
		},
		{
			name:          "header override",
			clientVersion: "0.144.0",
			account: func() *Account {
				account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
				account.Credentials[credKeyHeaderOverrideEnabled] = true
				account.Credentials[credKeyHeaderOverrides] = map[string]any{"x-tenant": "other"}
				return account
			},
		},
		{
			name:          "proxy",
			clientVersion: "0.144.0",
			account: func() *Account {
				account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
				proxyID := int64(9)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080}
				return account
			},
		},
	}

	expectedCalls := int32(1)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetchTwice(t, test.account(), test.clientVersion)
			expectedCalls++
			require.Equal(t, expectedCalls, calls.Load(), "identity variant must use its own cache entry")
		})
	}
}

func TestFetchCodexModelsManifestDoesNotCacheBodiesOverOneMiB(t *testing.T) {
	var calls atomic.Int32
	body := `{"models":[],"padding":"` + strings.Repeat("x", codexModelsManifestCacheBodyLimit+1) + `"}`
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
	for range 2 {
		manifest, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
		require.NoError(t, err)
		require.Len(t, manifest.Body, len(body))
	}
	require.Equal(t, int32(2), calls.Load())
}

func TestFetchCodexModelsManifestRejectsInvalidEnvelopeWithoutCaching(t *testing.T) {
	var calls atomic.Int32
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"object":"unexpected"}`)),
		}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")

	for range 2 {
		_, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
		require.Error(t, err)
		require.True(t, IsRetryableCodexModelsManifestError(err))
		require.ErrorContains(t, err, "OPENAI_CODEX_MODELS_UPSTREAM_INVALID_MANIFEST")
	}
	require.Equal(t, int32(2), calls.Load(), "invalid manifests must not enter the cache")
}

func TestConvertOpenAIModelListToCodexManifest(t *testing.T) {
	converted := convertOpenAIModelListToCodexManifest([]byte(`{"object":"list","data":[{"id":"gpt-5.6"},{"id":" "},{"id":"gpt-image-2"}]}`))
	require.JSONEq(t, `{"models":[{"slug":"gpt-5.6"},{"slug":"gpt-image-2"}]}`, string(converted))
	require.NoError(t, validateCodexModelsManifestEnvelope(converted))
}

func TestFetchCodexModelsManifestOAuth401IsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"token_revoked","message":"revoked"}}`))
	}))
	defer server.Close()
	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	t.Cleanup(func() { chatgptCodexModelsURL = original })

	_, err := (&OpenAIGatewayService{}).FetchCodexModelsManifest(context.Background(), newCodexModelsOAuthTestAccount(), "0.144.0", "")
	require.Error(t, err)
	require.True(t, IsRetryableCodexModelsManifestError(err))
}

type codexModelsCanceledStateRepo struct {
	AccountRepository
	updateCredentialsCalls  int
	tempUnschedulableCalls  int
	updateCredentialsCtxErr error
	tempUnschedulableCtxErr error
}

func (r *codexModelsCanceledStateRepo) UpdateCredentials(ctx context.Context, _ int64, _ map[string]any) error {
	r.updateCredentialsCalls++
	r.updateCredentialsCtxErr = ctx.Err()
	return nil
}

func (r *codexModelsCanceledStateRepo) SetTempUnschedulable(ctx context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedulableCalls++
	r.tempUnschedulableCtxErr = ctx.Err()
	return nil
}

func TestCodexModelsOAuth401PersistsStateAfterCallerCancellation(t *testing.T) {
	repo := &codexModelsCanceledStateRepo{}
	rateLimitService := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc := &OpenAIGatewayService{rateLimitService: rateLimitService}
	account := newCodexModelsOAuthTestAccount()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.handleCodexModelsManifestAccountAuthError(
		ctx,
		account,
		codexModelsManifestRequest{},
		&codexModelsManifestUpstreamError{
			err:        errors.New("models unauthorized"),
			statusCode: http.StatusUnauthorized,
			headers:    http.Header{},
			body:       []byte(`{"error":{"message":"expired access token"}}`),
		},
	)

	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.NoError(t, repo.updateCredentialsCtxErr)
	require.Equal(t, 1, repo.tempUnschedulableCalls)
	require.NoError(t, repo.tempUnschedulableCtxErr)
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

func TestFetchCodexModelsManifestSharedRefreshSurvivesCallerCancellation(t *testing.T) {
	var calls atomic.Int32
	readStarted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls.Add(1)
		bodyReader, bodyWriter := io.Pipe()
		go func() {
			once.Do(func() { close(readStarted) })
			<-release
			_, _ = bodyWriter.Write([]byte(`{"models":[]}`))
			_ = bodyWriter.Close()
		}()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: bodyReader}, nil
	}}
	s := newCodexModelsAPIKeyTestService(upstream)
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example/v1")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := s.FetchCodexModelsManifest(ctx, account, "0.144.0", "")
		result <- err
	}()
	<-readStarted
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	close(release)

	require.Eventually(t, func() bool {
		manifest, err := s.FetchCodexModelsManifest(context.Background(), account, "0.144.0", "")
		return err == nil && string(manifest.Body) == `{"models":[]}`
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(1), calls.Load())
}

func TestCodexModelsManifestCacheEvictsOldestEntry(t *testing.T) {
	var cache codexModelsManifestCache
	now := time.Now()
	for index := range codexModelsManifestCacheMaxEntries + 1 {
		cache.set(strconv.Itoa(index), &CodexModelsManifest{Body: []byte(`{}`)}, now)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	require.Len(t, cache.entries, codexModelsManifestCacheMaxEntries)
	_, containsOldest := cache.entries["0"]
	require.False(t, containsOldest)
}

func TestCodexModelsManifestRetryableStatusClassification(t *testing.T) {
	tests := []struct {
		status    int
		retryable bool
	}{
		{status: http.StatusBadRequest},
		{status: http.StatusUnauthorized},
		{status: http.StatusTooManyRequests, retryable: true},
		{status: http.StatusInternalServerError, retryable: true},
		{status: http.StatusServiceUnavailable, retryable: true},
		{status: 600},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Status: http.StatusText(test.status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"failed"}`))}, nil
			}}
			_, err := newCodexModelsAPIKeyTestService(upstream).FetchCodexModelsManifest(context.Background(), newCodexModelsAPIKeyTestAccount("https://upstream.example/v1"), "0.144.0", "")
			require.Error(t, err)
			require.Equal(t, test.retryable, IsRetryableCodexModelsManifestError(err))
		})
	}
}

func TestFetchCodexModelsManifestRejectsUnsafeOrUnsupportedAPIKeyUpstreams(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "missing custom upstream"},
		{name: "official OpenAI", baseURL: "https://API.OPENAI.COM.:443/v1"},
		{name: "fragment", baseURL: "https://upstream.example/v1#fragment"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				t.Fatal("rejected upstream must not be contacted")
				return nil, nil
			}}
			_, err := newCodexModelsAPIKeyTestService(upstream).FetchCodexModelsManifest(
				context.Background(),
				newCodexModelsAPIKeyTestAccount(test.baseURL),
				"0.144.0",
				"",
			)
			require.Error(t, err)
		})
	}
}

func TestBuildCodexModelsManifestURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "root", endpoint: "https://upstream.example", want: "https://upstream.example/v1/models?client_version=0.144.0"},
		{name: "v1", endpoint: "https://upstream.example/v1", want: "https://upstream.example/v1/models?client_version=0.144.0"},
		{name: "models", endpoint: "https://upstream.example/models", want: "https://upstream.example/models?client_version=0.144.0"},
		{name: "v1 models", endpoint: "https://upstream.example/v1/models", want: "https://upstream.example/v1/models?client_version=0.144.0"},
		{name: "preview", endpoint: "https://upstream.example/v1preview", want: "https://upstream.example/v1preview/models?client_version=0.144.0"},
		{name: "query", endpoint: "https://upstream.example/v1?tenant=one", want: "https://upstream.example/v1/models?client_version=0.144.0&tenant=one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := buildCodexModelsManifestURL(test.endpoint, true, "0.144.0")
			require.NoError(t, err)
			require.Equal(t, test.want, got.String())
		})
	}
	_, err := buildCodexModelsManifestURL("https://upstream.example/v1#fragment", true, "0.144.0")
	require.ErrorContains(t, err, "fragments are not supported")
}

func TestCodexModelsManifestETagMatches(t *testing.T) {
	require.True(t, codexModelsManifestETagMatches(`W/"other", "manifest"`, `W/"manifest"`))
	require.True(t, codexModelsManifestETagMatches("*", `"manifest"`))
	require.False(t, codexModelsManifestETagMatches(`"other"`, `"manifest"`))
	require.False(t, codexModelsManifestETagMatches(`"manifest"`, ""))
}
