package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"golang.org/x/net/http2"
	"golang.org/x/sync/singleflight"
)

// chatgptCodexModelsURL is variable so protocol tests can point it at a stub.
var chatgptCodexModelsURL = "https://chatgpt.com/backend-api/codex/models"

const (
	codexModelsManifestCacheBodyLimit  = 1 << 20
	codexModelsManifestCacheMaxEntries = 64
	codexModelsManifestCacheTTL        = 30 * time.Second
	codexModelsManifestCacheStaleTTL   = 5 * time.Minute
	codexModelsManifestRequestTimeout  = 15 * time.Second
)

// CodexModelsManifest contains the schema-agnostic upstream payload and cache
// metadata needed by the HTTP handler.
type CodexModelsManifest struct {
	Body        []byte
	ETag        string
	NotModified bool
	// API-key manifests may be converted before delivery. Revalidation must
	// use the upstream validator, not the converted representation's ETag.
	upstreamETag string
}

type codexModelsManifestUpstreamError struct {
	err        error
	retryable  bool
	statusCode int
	headers    http.Header
	body       []byte
}

func (e *codexModelsManifestUpstreamError) Error() string { return e.err.Error() }

func (e *codexModelsManifestUpstreamError) Unwrap() error { return e.err }

// IsRetryableCodexModelsManifestError reports whether selecting another
// account may succeed without changing the request.
func IsRetryableCodexModelsManifestError(err error) bool {
	var upstreamErr *codexModelsManifestUpstreamError
	return errors.As(err, &upstreamErr) && upstreamErr.retryable
}

func isRetryableCodexModelsManifestTransportError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var goAwayErr http2.GoAwayError
	if errors.As(err, &goAwayErr) {
		return true
	}
	var streamErr http2.StreamError
	if errors.As(err, &streamErr) {
		return true
	}
	var connectionErr http2.ConnectionError
	if errors.As(err, &connectionErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// net/http exposes some HTTP/2 transport failures only through strings.
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "http2:") &&
		(strings.Contains(message, "goaway") ||
			strings.Contains(message, "refused_stream") ||
			strings.Contains(message, "frame too large")) {
		return true
	}
	if strings.Contains(message, "stream error: stream id ") {
		return true
	}
	for _, code := range []http2.ErrCode{
		http2.ErrCodeNo,
		http2.ErrCodeProtocol,
		http2.ErrCodeInternal,
		http2.ErrCodeFlowControl,
		http2.ErrCodeSettingsTimeout,
		http2.ErrCodeStreamClosed,
		http2.ErrCodeFrameSize,
		http2.ErrCodeRefusedStream,
		http2.ErrCodeCancel,
		http2.ErrCodeCompression,
		http2.ErrCodeConnect,
		http2.ErrCodeEnhanceYourCalm,
		http2.ErrCodeInadequateSecurity,
		http2.ErrCodeHTTP11Required,
	} {
		if strings.Contains(message, "connection error: "+strings.ToLower(code.String())) {
			return true
		}
	}
	return false
}

type codexModelsManifestRequest struct {
	url                string
	headers            http.Header
	proxyURL           string
	accountID          int64
	account            *Account
	accountConcurrency int
	useAPIKeyUpstream  bool
	// Search capability is derived from the account's persisted Responses
	// probe. It participates in the cache identity so a probe state change
	// cannot reuse a body generated under a different tool contract.
	advertiseSearchTool bool
}

type codexModelsManifestCacheEntry struct {
	manifest   *CodexModelsManifest
	order      uint64
	expiresAt  time.Time
	staleUntil time.Time
}

type codexModelsManifestCacheState uint8

const (
	codexModelsManifestCacheMiss codexModelsManifestCacheState = iota
	codexModelsManifestCacheFresh
	codexModelsManifestCacheStale
)

type codexModelsManifestCache struct {
	mu        sync.Mutex
	entries   map[string]codexModelsManifestCacheEntry
	nextOrder uint64
	refresh   singleflight.Group
}

func (c *codexModelsManifestCache) get(key string, now time.Time) (*CodexModelsManifest, codexModelsManifestCacheState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, codexModelsManifestCacheMiss
	}
	if !now.Before(entry.staleUntil) {
		delete(c.entries, key)
		return nil, codexModelsManifestCacheMiss
	}
	if now.Before(entry.expiresAt) {
		return entry.manifest, codexModelsManifestCacheFresh
	}
	return entry.manifest, codexModelsManifestCacheStale
}

func (c *codexModelsManifestCache) set(key string, manifest *CodexModelsManifest, now time.Time) {
	if manifest == nil || len(manifest.Body) > codexModelsManifestCacheBodyLimit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]codexModelsManifestCacheEntry)
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= codexModelsManifestCacheMaxEntries {
		oldestKey := ""
		var oldestOrder uint64
		for candidateKey, entry := range c.entries {
			if !now.Before(entry.staleUntil) {
				delete(c.entries, candidateKey)
				continue
			}
			if oldestKey == "" || entry.order < oldestOrder {
				oldestKey = candidateKey
				oldestOrder = entry.order
			}
		}
		if len(c.entries) >= codexModelsManifestCacheMaxEntries && oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.nextOrder++
	c.entries[key] = codexModelsManifestCacheEntry{
		manifest:   manifest,
		order:      c.nextOrder,
		expiresAt:  now.Add(codexModelsManifestCacheTTL),
		staleUntil: now.Add(codexModelsManifestCacheStaleTTL),
	}
}

// FetchCodexModelsManifest fetches a live Codex model manifest from either the
// ChatGPT backend for OAuth accounts or a custom upstream for API key accounts.
func (s *OpenAIGatewayService) FetchCodexModelsManifest(ctx context.Context, account *Account, clientVersion, ifNoneMatch string) (*CodexModelsManifest, error) {
	if account == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_ACCOUNT_REQUIRED", "account is required")
	}

	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = openAICodexProbeVersion
	}

	requestEndpoint := chatgptCodexModelsURL
	authToken := ""
	useAPIKeyUpstream := false
	appendModelsPath := false
	switch {
	case account.IsOpenAIOAuth():
		authToken = strings.TrimSpace(account.GetOpenAIAccessToken())
		if authToken == "" && !account.IsOpenAIAgentIdentity() {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_TOKEN_MISSING", "account has no Codex backend access token")
		}
	case account.IsOpenAIApiKey():
		baseURL := strings.TrimSpace(account.GetCredential("base_url"))
		if baseURL == "" || isOfficialOpenAIModelsBaseURL(baseURL) {
			return nil, infraerrors.New(
				http.StatusBadGateway,
				"OPENAI_CODEX_MODELS_API_KEY_UPSTREAM_UNSUPPORTED",
				"Codex models manifest requires a custom API key upstream base URL",
			)
		}
		authToken = strings.TrimSpace(account.GetOpenAIApiKey())
		if authToken == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_MISSING", "account has no API key for the Codex models upstream")
		}
		normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_UPSTREAM_INVALID", "invalid Codex models upstream base URL: %v", err)
		}
		requestEndpoint = normalizedBaseURL
		useAPIKeyUpstream = true
		appendModelsPath = true
	default:
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_ACCOUNT_TYPE_UNSUPPORTED", "account type %q cannot fetch the Codex models manifest", account.Type)
	}

	requestURL, err := buildCodexModelsManifestURL(requestEndpoint, appendModelsPath, clientVersion)
	if err != nil {
		if useAPIKeyUpstream {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_UPSTREAM_INVALID", "invalid Codex models upstream base URL: %v", err)
		}
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "parse codex models request URL: %v", err)
	}

	headers := make(http.Header)
	if useAPIKeyUpstream {
		headers.Set("Authorization", "Bearer "+authToken)
		account.ApplyHeaderOverrides(headers)
	} else {
		authHeaders, authErr := s.buildOpenAIAuthenticationHeaders(ctx, account, authToken)
		if authErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_AUTH_FAILED", "build Codex models authentication: %v", authErr)
		}
		for key, values := range authHeaders {
			for _, value := range values {
				headers.Add(key, value)
			}
		}
		setOpenAIChatGPTAccountHeaders(headers, account)
	}
	headers.Set("Accept", "application/json")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("Version", clientVersion)
	headers.Set("User-Agent", codexCLIUserAgent)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	request := codexModelsManifestRequest{
		url:                 requestURL.String(),
		headers:             headers,
		proxyURL:            proxyURL,
		accountID:           account.ID,
		account:             account,
		accountConcurrency:  account.Concurrency,
		useAPIKeyUpstream:   useAPIKeyUpstream,
		advertiseSearchTool: shouldAdvertiseCodexSearchTool(account),
	}
	if useAPIKeyUpstream {
		return s.fetchCachedAPIKeyCodexModelsManifest(ctx, request, ifNoneMatch)
	}

	manifest, fetchErr := s.fetchCodexModelsManifestUpstream(ctx, request, ifNoneMatch)
	if !account.IsOpenAIAgentIdentity() || !isAgentIdentityTaskInvalidCodexModelsError(fetchErr) {
		s.handleCodexModelsManifestAccountAuthError(ctx, account, request, fetchErr)
		return manifest, fetchErr
	}
	expectedTaskID := strings.TrimSpace(account.GetCredential("task_id"))
	if recoverErr := s.recoverAgentIdentityTask(ctx, account, expectedTaskID); recoverErr != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_AUTH_FAILED", "agent identity task recovery failed: %v", recoverErr)
	}
	ctx = withAgentIdentitySensitiveValues(ctx, expectedTaskID)
	authHeaders, authErr := s.buildOpenAIAuthenticationHeaders(ctx, account, "")
	if authErr != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_AUTH_FAILED", "build Codex models authentication after task recovery: %v", authErr)
	}
	request.headers.Del("Authorization")
	request.headers.Del("ChatGPT-Account-ID")
	for key, values := range authHeaders {
		for _, value := range values {
			request.headers.Add(key, value)
		}
	}
	setOpenAIChatGPTAccountHeaders(request.headers, account)
	return s.fetchCodexModelsManifestUpstream(ctx, request, ifNoneMatch)
}

func isAgentIdentityTaskInvalidCodexModelsError(err error) bool {
	var upstreamErr *codexModelsManifestUpstreamError
	return errors.As(err, &upstreamErr) &&
		isAgentIdentityTaskInvalidHTTPResponse(upstreamErr.statusCode, upstreamErr.body)
}

func (s *OpenAIGatewayService) handleCodexModelsManifestAccountAuthError(ctx context.Context, account *Account, request codexModelsManifestRequest, err error) {
	if s == nil || account == nil || err == nil || request.useAPIKeyUpstream || !account.IsOpenAIOAuth() || account.IsOpenAIAgentIdentity() {
		return
	}
	var upstreamErr *codexModelsManifestUpstreamError
	if !errors.As(err, &upstreamErr) || upstreamErr.statusCode != http.StatusUnauthorized {
		return
	}
	headers := upstreamErr.headers
	if headers == nil {
		headers = http.Header{}
	}
	s.handleOpenAIAccountUpstreamErrorForModel(ctx, account, "", upstreamErr.statusCode, headers, upstreamErr.body)
}

func (s *OpenAIGatewayService) fetchCachedAPIKeyCodexModelsManifest(ctx context.Context, request codexModelsManifestRequest, ifNoneMatch string) (*CodexModelsManifest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cacheKey := buildCodexModelsManifestCacheKey(request)
	manifest, state := s.codexModelsManifestCache.get(cacheKey, time.Now())
	if state == codexModelsManifestCacheFresh {
		return codexModelsManifestForClient(manifest, ifNoneMatch), nil
	}
	resultCh := s.refreshCachedAPIKeyCodexModelsManifest(cacheKey, request)
	if state == codexModelsManifestCacheStale {
		return codexModelsManifestForClient(manifest, ifNoneMatch), nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			return nil, result.Err
		}
		manifest, ok := result.Val.(*CodexModelsManifest)
		if !ok || manifest == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "invalid shared Codex models manifest result")
		}
		return codexModelsManifestForClient(manifest, ifNoneMatch), nil
	}
}

func (s *OpenAIGatewayService) refreshCachedAPIKeyCodexModelsManifest(cacheKey string, request codexModelsManifestRequest) <-chan singleflight.Result {
	return s.codexModelsManifestCache.refresh.DoChan(cacheKey, func() (any, error) {
		cached, _ := s.codexModelsManifestCache.get(cacheKey, time.Now())
		ifNoneMatch := ""
		if cached != nil {
			ifNoneMatch = cached.upstreamETag
		}
		manifest, err := s.fetchCodexModelsManifestUpstream(context.Background(), request, ifNoneMatch)
		if err != nil {
			return nil, err
		}
		if manifest.NotModified && cached != nil {
			s.codexModelsManifestCache.set(cacheKey, cached, time.Now())
			return cached, nil
		}
		if !manifest.NotModified {
			s.codexModelsManifestCache.set(cacheKey, manifest, time.Now())
		}
		return manifest, nil
	})
}

func (s *OpenAIGatewayService) fetchCodexModelsManifestUpstream(ctx context.Context, request codexModelsManifestRequest, ifNoneMatch string) (*CodexModelsManifest, error) {
	reqCtx, cancel := context.WithTimeout(ctx, codexModelsManifestRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, request.url, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "create codex models request: %v", err)
	}
	req.Header = request.headers.Clone()
	if ifNoneMatch = strings.TrimSpace(ifNoneMatch); ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	var resp *http.Response
	if request.useAPIKeyUpstream {
		if s.httpUpstream == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_UPSTREAM_NOT_CONFIGURED", "Codex models upstream HTTP client is not configured")
		}
		req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
		resp, err = s.httpUpstream.Do(req, request.proxyURL, request.accountID, request.accountConcurrency)
	} else {
		client, clientErr := httpclient.GetClient(httpclient.Options{
			ProxyURL:              request.proxyURL,
			Timeout:               codexModelsManifestRequestTimeout,
			ResponseHeaderTimeout: 10 * time.Second,
		})
		if clientErr != nil {
			return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_PROXY_INVALID", "invalid proxy configuration: %v", clientErr)
		}
		resp, err = client.Do(req)
	}
	if err != nil {
		return nil, &codexModelsManifestUpstreamError{
			err:       infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest request failed: %v", err),
			retryable: isRetryableCodexModelsManifestTransportError(err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return &CodexModelsManifest{ETag: resp.Header.Get("ETag"), NotModified: true}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		body = s.redactAgentIdentitySensitiveBody(reqCtx, request.account, body)
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, &codexModelsManifestUpstreamError{
			err:        infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest upstream error %d: %s", resp.StatusCode, message),
			statusCode: resp.StatusCode,
			headers:    resp.Header.Clone(),
			body:       body,
			retryable: (resp.StatusCode == http.StatusUnauthorized && !request.useAPIKeyUpstream) ||
				resp.StatusCode == http.StatusTooManyRequests ||
				(resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < 600),
		}
	}

	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveModelsListReadLimit(s.cfg))
	if err != nil {
		return nil, &codexModelsManifestUpstreamError{
			err:       infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "read codex models manifest response: %v", err),
			retryable: !errors.Is(err, ErrUpstreamResponseBodyTooLarge) && isRetryableCodexModelsManifestTransportError(err),
		}
	}
	clientETag := resp.Header.Get("ETag")
	if request.useAPIKeyUpstream {
		converted, convertErr := convertOpenAIModelListToCodexManifest(body, request.advertiseSearchTool)
		if convertErr != nil {
			return nil, &codexModelsManifestUpstreamError{
				err:       infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_INVALID_MANIFEST", "convert OpenAI model list to Codex manifest: %v", convertErr),
				retryable: true,
			}
		}
		if !bytes.Equal(body, converted) {
			clientETag = fmt.Sprintf(`"%x"`, sha256.Sum256(converted))
		}
		body = converted
	}
	if err := validateCodexModelsManifestEnvelope(body); err != nil {
		return nil, &codexModelsManifestUpstreamError{
			err: infraerrors.Newf(
				http.StatusBadGateway,
				"OPENAI_CODEX_MODELS_UPSTREAM_INVALID_MANIFEST",
				"codex models manifest upstream returned an invalid envelope: %v",
				err,
			),
			retryable: true,
		}
	}
	return &CodexModelsManifest{Body: body, ETag: clientETag, upstreamETag: resp.Header.Get("ETag")}, nil
}

// convertOpenAIModelListToCodexManifest converts a standard OpenAI
// model list to the Codex manifest shape without discarding metadata declared
// by the upstream. Known Codex capability fields are validated and malformed
// declarations fail closed; unknown fields remain untouched because they are
// part of the upstream model descriptor contract.
func convertOpenAIModelListToCodexManifest(body []byte, advertiseSearch bool) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode upstream model list: %w", err)
	}
	if envelope == nil {
		return nil, errors.New("upstream model list must be an object")
	}
	if _, ok := envelope["models"]; ok {
		return body, nil
	}
	data, ok := envelope["data"]
	if !ok {
		return nil, errors.New("missing data array")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode data array: %w", err)
	}
	models := make([]map[string]json.RawMessage, 0, len(entries))
	for _, rawEntry := range entries {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(rawEntry, &entry); err != nil || entry == nil {
			continue
		}
		var rawID string
		if err := json.Unmarshal(entry["id"], &rawID); err != nil {
			continue
		}
		id := strings.TrimSpace(rawID)
		if id != "" {
			delete(entry, "id")
			entry["slug"], _ = json.Marshal(id)
			_, searchDeclared := entry["supports_search_tool"]
			if err := validateCodexModelDescriptor(entry); err != nil {
				return nil, fmt.Errorf("model %q: %w", id, err)
			}
			if advertiseSearch && !searchDeclared {
				entry["supports_search_tool"] = json.RawMessage("true")
			}
			models = append(models, entry)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("data array contains no valid model entries")
	}
	converted, err := json.Marshal(map[string][]map[string]json.RawMessage{"models": models})
	if err != nil {
		return nil, fmt.Errorf("encode models array: %w", err)
	}
	return converted, nil
}

var codexModelDescriptorStringFields = map[string]struct{}{
	"apply_patch_tool_type":        {},
	"multi_agent_reasoning_effort": {},
	"default_reasoning_level":      {},
}

var codexModelDescriptorBoolFields = map[string]struct{}{
	"supports_search_tool": {},
	"use_responses_lite":   {},
}

// validateCodexModelDescriptor checks fields with stable upstream types. Fields
// such as comp_hash, tool_mode and multi_agent_version are opaque in the Codex
// schema and must keep their original JSON types.
func validateCodexModelDescriptor(entry map[string]json.RawMessage) error {
	for field := range codexModelDescriptorStringFields {
		if value, ok := entry[field]; ok && !validCodexModelDescriptorField(field, value) {
			return fmt.Errorf("malformed %s", field)
		}
	}
	for field := range codexModelDescriptorBoolFields {
		if value, ok := entry[field]; ok && !validCodexModelDescriptorField(field, value) {
			return fmt.Errorf("malformed %s", field)
		}
	}
	for _, field := range []string{"supported_reasoning_levels", "input_modalities"} {
		if value, ok := entry[field]; ok && !validCodexModelDescriptorField(field, value) {
			return fmt.Errorf("malformed %s", field)
		}
	}
	return nil
}

func validCodexModelDescriptorField(field string, value json.RawMessage) bool {
	if _, ok := codexModelDescriptorStringFields[field]; ok {
		return jsonNullOrString(value)
	}
	if _, ok := codexModelDescriptorBoolFields[field]; ok {
		return jsonNullOrBool(value)
	}
	if field == "supported_reasoning_levels" || field == "input_modalities" {
		return jsonArray(value)
	}
	return true
}

func jsonNullOrString(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	if bytes.Equal(value, []byte("null")) {
		return true
	}
	var text string
	return json.Unmarshal(value, &text) == nil
}

func jsonNullOrBool(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	return bytes.Equal(value, []byte("null")) || bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false"))
}

func jsonArray(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	return len(value) > 0 && value[0] == '[' && json.Valid(value)
}

func shouldAdvertiseCodexSearchTool(account *Account) bool {
	return account != nil && account.IsOpenAI() && account.Type == AccountTypeAPIKey &&
		openai_compat.ResolveResponsesSupport(account.Extra) == openai_compat.ResponsesSupportNo
}

func validateCodexModelsManifestEnvelope(body []byte) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode JSON object: %w", err)
	}
	if envelope == nil {
		return errors.New("expected a JSON object")
	}
	models, ok := envelope["models"]
	if !ok {
		return errors.New("missing top-level models array")
	}
	models = bytes.TrimSpace(models)
	if len(models) == 0 || models[0] != '[' {
		return errors.New("top-level models field is not an array")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(models, &entries); err != nil {
		return fmt.Errorf("decode top-level models array: %w", err)
	}
	return nil
}

func buildCodexModelsManifestCacheKey(request codexModelsManifestRequest) string {
	hasher := sha256.New()
	_, _ = fmt.Fprintf(hasher, "%d\n%t\n%s\n%s\n", request.accountID, request.advertiseSearchTool, request.proxyURL, request.url)
	headerNames := make([]string, 0, len(request.headers))
	for name := range request.headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		_, _ = fmt.Fprintf(hasher, "%s\n", strings.ToLower(name))
		for _, value := range request.headers[name] {
			_, _ = fmt.Fprintf(hasher, "%s\n", value)
		}
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func codexModelsManifestForClient(manifest *CodexModelsManifest, ifNoneMatch string) *CodexModelsManifest {
	if manifest == nil {
		return nil
	}
	if codexModelsManifestETagMatches(ifNoneMatch, manifest.ETag) {
		return &CodexModelsManifest{ETag: manifest.ETag, NotModified: true}
	}
	return manifest
}

func codexModelsManifestETagMatches(ifNoneMatch, etag string) bool {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return false
	}
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		if len(value) >= 2 && strings.EqualFold(value[:2], "W/") {
			value = strings.TrimSpace(value[2:])
		}
		return value
	}
	want := normalize(etag)
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || normalize(candidate) == want {
			return true
		}
	}
	return false
}

func isOfficialOpenAIModelsBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	hostname := strings.TrimSuffix(parsed.Hostname(), ".")
	return strings.EqualFold(hostname, "api.openai.com")
}

func buildCodexModelsManifestURL(endpoint string, appendModelsPath bool, clientVersion string) (*url.URL, error) {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if requestURL.Fragment != "" {
		return nil, fmt.Errorf("URL fragments are not supported")
	}

	query := requestURL.Query()
	requestURL.RawQuery = ""
	requestURL.ForceQuery = false
	if appendModelsPath {
		requestURL, err = url.Parse(buildOpenAIEndpointURL(requestURL.String(), "/v1/models"))
		if err != nil {
			return nil, err
		}
	}
	query.Set("client_version", clientVersion)
	requestURL.RawQuery = query.Encode()
	return requestURL, nil
}
