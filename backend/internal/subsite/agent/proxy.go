package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxBufferedUpstreamBody = 64 << 20
const subsiteCodexCLIUserAgent = "codex_cli_rs/0.125.0"

type proxyResult struct {
	Usage            service.UsageIngestItem
	UpstreamEndpoint string
}

type responseCaptureMetrics struct {
	firstTokenMs *int
}

type upstreamError struct {
	statusCode  int
	contentType string
	body        []byte
	message     string
}

func (e *upstreamError) Error() string {
	if e == nil {
		return "upstream error"
	}
	if strings.TrimSpace(e.message) != "" {
		return e.message
	}
	if e.statusCode > 0 {
		return fmt.Sprintf("upstream returned %d", e.statusCode)
	}
	return "upstream error"
}

func (s *Server) proxyAuthorizedRequest(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, body []byte) (*proxyResult, error) {
	if shouldProxyOpenAIChatCompletionsThroughResponses(c.Request.URL.Path, authorization) {
		return s.proxyOpenAIChatCompletionsViaResponses(c, authorization, body)
	}
	upstreamReq, _, upstreamEndpoint, err := buildUpstreamRequest(c, authorization, body)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("call upstream: %w", err)
	}
	defer resp.Body.Close()

	responseBody, wroteResponse, metrics, err := streamOrBufferResponse(c, resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamError(resp.StatusCode, resp.Header.Get("Content-Type"), responseBody)
	}
	usage := buildUsageIngestItem(c, authorization, body, responseBody, upstreamEndpoint, time.Since(start), metrics)
	if err := s.queue.Enqueue(c.Request.Context(), usage); err != nil {
		return nil, fmt.Errorf("enqueue usage before response: %w", err)
	}
	if !wroteResponse {
		writeBufferedResponse(c, resp, responseBody)
	}
	return &proxyResult{UpstreamEndpoint: upstreamEndpoint}, nil
}

func (s *Server) proxyOpenAIChatCompletionsViaResponses(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, body []byte) (*proxyResult, error) {
	account := accountFromAuthorization(authorization)
	originalRequestBody := append([]byte(nil), body...)
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}

	originalModel := strings.TrimSpace(chatReq.Model)
	responsesReq, err := apicompat.ChatCompletionsToResponses(&chatReq)
	if err != nil {
		return nil, fmt.Errorf("convert chat completions to responses: %w", err)
	}
	upstreamModel := strings.TrimSpace(authorization.MappedModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(authorization.RequestedModel)
	}
	if upstreamModel != "" {
		responsesReq.Model = upstreamModel
	}
	if account.Type == service.AccountTypeOAuth {
		reqBody := map[string]any{}
		encoded, err := json.Marshal(responsesReq)
		if err != nil {
			return nil, fmt.Errorf("marshal responses request: %w", err)
		}
		if err := json.Unmarshal(encoded, &reqBody); err != nil {
			return nil, fmt.Errorf("unmarshal responses request: %w", err)
		}
		applySubsiteCodexOAuthTransform(reqBody, false)
		encoded, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("remarshal oauth responses request: %w", err)
		}
		body = encoded
	} else {
		body, err = json.Marshal(responsesReq)
		if err != nil {
			return nil, fmt.Errorf("marshal responses request: %w", err)
		}
	}

	requestBody := body
	upstreamReq, err := buildOpenAIResponsesRequest(c, authorization, account, requestBody)
	if err != nil {
		return nil, fmt.Errorf("build responses upstream request: %w", err)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("call upstream: %w", err)
	}
	defer resp.Body.Close()

	responseBody, wroteResponse, metrics, err := streamOrBufferResponse(c, resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamError(resp.StatusCode, resp.Header.Get("Content-Type"), responseBody)
	}

	convertedBody, convertedContentType, convertErr := convertResponsesBodyToChatCompletions(c, responseBody, originalModel, chatReq.Stream)
	if convertErr != nil {
		return nil, convertErr
	}
	if !wroteResponse {
		c.Data(resp.StatusCode, convertedContentType, convertedBody)
	}

	usage := buildUsageIngestItem(c, authorization, originalRequestBody, responseBody, "/v1/responses", time.Since(start), metrics)
	if err := s.queue.Enqueue(c.Request.Context(), usage); err != nil {
		return nil, fmt.Errorf("enqueue usage before response: %w", err)
	}
	return &proxyResult{UpstreamEndpoint: "/v1/responses"}, nil
}

func buildOpenAIResponsesRequest(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, account *service.Account, body []byte) (*http.Request, error) {
	if c == nil || c.Request == nil {
		return nil, errors.New("request context is required")
	}
	if authorization == nil {
		return nil, errors.New("authorization is required")
	}
	if account == nil {
		account = accountFromAuthorization(authorization)
	}

	targetURL, err := buildOpenAIResponsesURL(c, account)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	copyForwardHeaders(req.Header, c.Request.Header)
	req.Header.Set("Content-Type", "application/json")
	applyUpstreamAuth(req.Header, account)

	if account != nil && account.Platform == service.PlatformOpenAI && account.Type == service.AccountTypeOAuth {
		req.Host = "chatgpt.com"
		req.Header.Del("conversation_id")
		req.Header.Del("session_id")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", resolveSubsiteOpenAIOriginator(c))
		req.Header.Set("accept", "text/event-stream")

		if authorization.APIKeyID > 0 {
			if sessionID := firstNonEmptyHeader(c, "OpenAI-Session-ID", "session_id"); sessionID != "" {
				req.Header.Set("session_id", isolateSubsiteSessionID(authorization.APIKeyID, sessionID))
			}
			if conversationID := firstNonEmptyHeader(c, "OpenAI-Conversation-ID", "conversation_id"); conversationID != "" {
				req.Header.Set("conversation_id", isolateSubsiteSessionID(authorization.APIKeyID, conversationID))
			}
		}
		if !openai.IsCodexOfficialClientByHeaders(req.Header.Get("User-Agent"), req.Header.Get("originator")) {
			req.Header.Set("User-Agent", subsiteCodexCLIUserAgent)
		}
	}

	return req, nil
}

func buildUpstreamRequest(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, body []byte) (*http.Request, []byte, string, error) {
	if authorization == nil {
		return nil, nil, "", errors.New("authorization is required")
	}
	account := accountFromAuthorization(authorization)
	requestBody, upstreamPath, contentTypeOverride, err := buildUpstreamRequestBodyAndPath(c, authorization, account, body)
	if err != nil {
		return nil, nil, "", err
	}
	targetURL, err := buildUpstreamURL(c, account, upstreamPath)
	if err != nil {
		return nil, nil, "", err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, nil, "", err
	}
	copyForwardHeaders(req.Header, c.Request.Header)
	if contentTypeOverride != "" {
		req.Header.Set("Content-Type", contentTypeOverride)
	}
	applyUpstreamAuth(req.Header, account)
	if req.Header.Get("Content-Type") == "" && len(requestBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, requestBody, upstreamPath, nil
}

func accountFromAuthorization(authorization *service.AuthorizeSubsiteResponse) *service.Account {
	return &service.Account{
		ID:           authorization.AccountID,
		Platform:     authorization.Platform,
		Type:         authorization.Credential.AccountType,
		AccountLevel: authorization.Credential.AccountLevel,
		Credentials:  cloneMap(authorization.Credential.Credentials),
		Extra:        cloneMap(authorization.Credential.Extra),
		Status:       service.StatusActive,
		Schedulable:  true,
	}
}

func buildUpstreamRequestBodyAndPath(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, account *service.Account, body []byte) ([]byte, string, string, error) {
	upstreamPath := upstreamPathForInbound(c.Request.URL.Path)
	if account != nil && account.Platform == service.PlatformOpenAI && account.Type == service.AccountTypeOAuth && strings.Contains(upstreamPath, "/responses") {
		reqBody := map[string]any{}
		if err := json.Unmarshal(body, &reqBody); err == nil {
			applySubsiteCodexOAuthTransform(reqBody, false)
			encoded, marshalErr := json.Marshal(reqBody)
			if marshalErr != nil {
				return nil, "", "", fmt.Errorf("marshal oauth passthrough body: %w", marshalErr)
			}
			body = encoded
		}
		normalizedBody, err := normalizeSubsiteOpenAIPassthroughOAuthBody(body, false)
		if err != nil {
			return nil, "", "", err
		}
		body = normalizedBody
	}
	if strings.TrimSpace(authorization.MappedModel) == "" || strings.TrimSpace(authorization.MappedModel) == strings.TrimSpace(authorization.RequestedModel) {
		return body, upstreamPath, "", nil
	}
	if isMultipartRequest(c) {
		updated, contentType, err := replaceMultipartModel(c.Request.Header.Get("Content-Type"), body, authorization.MappedModel)
		return updated, upstreamPath, contentType, err
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body, upstreamPath, "", nil
	}
	updated, err := sjson.SetBytes(body, "model", authorization.MappedModel)
	if err != nil {
		return nil, "", "", fmt.Errorf("map request model: %w", err)
	}
	_ = account
	return updated, upstreamPath, "", nil
}

func buildUpstreamURL(c *gin.Context, account *service.Account, upstreamPath string) (string, error) {
	baseURL := upstreamBaseURLForRequest(c, account, upstreamPath)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid upstream base_url")
	}
	parsed.Path = joinURLPath(parsed.Path, upstreamPath)
	parsed.RawQuery = c.Request.URL.RawQuery
	return parsed.String(), nil
}

func buildOpenAIResponsesURL(c *gin.Context, account *service.Account) (string, error) {
	baseURL := upstreamBaseURLForRequest(c, account, "/v1/responses")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid upstream base_url")
	}
	parsed.Path = joinURLPath(parsed.Path, "/v1/responses")
	parsed.RawQuery = c.Request.URL.RawQuery
	return parsed.String(), nil
}

func upstreamBaseURLForRequest(c *gin.Context, account *service.Account, upstreamPath string) string {
	if account != nil && account.Platform == service.PlatformOpenAI && account.Type == service.AccountTypeOAuth && strings.Contains(upstreamPath, "/responses") {
		return "https://chatgpt.com/backend-api/codex/responses"
	}
	return upstreamBaseURL(account)
}

func upstreamBaseURL(account *service.Account) string {
	switch account.Platform {
	case service.PlatformOpenAI:
		return account.GetOpenAIBaseURL()
	case service.PlatformGemini:
		return account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	default:
		if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
			if account.Platform == service.PlatformAntigravity && account.Type == service.AccountTypeAPIKey {
				return strings.TrimRight(baseURL, "/") + "/antigravity"
			}
			return baseURL
		}
		return "https://api.anthropic.com"
	}
}

func upstreamPathForInbound(inboundPath string) string {
	switch {
	case strings.HasPrefix(inboundPath, "/v1beta/"):
		return inboundPath
	case strings.Contains(inboundPath, "/chat/completions"):
		return "/v1/chat/completions"
	case strings.Contains(inboundPath, "/images/generations"):
		return "/v1/images/generations"
	case strings.Contains(inboundPath, "/images/edits"):
		return "/v1/images/edits"
	case strings.Contains(inboundPath, "/responses"):
		idx := strings.LastIndex(inboundPath, "/responses")
		suffix := ""
		if idx >= 0 {
			suffix = inboundPath[idx+len("/responses"):]
		}
		return "/v1/responses" + suffix
	case strings.Contains(inboundPath, "/messages"):
		return "/v1/messages"
	default:
		return inboundPath
	}
}

func joinURLPath(basePath, appendPath string) string {
	basePath = strings.TrimRight(basePath, "/")
	appendPath = "/" + strings.TrimLeft(appendPath, "/")
	if basePath == "" || basePath == "/" {
		return appendPath
	}
	if strings.HasSuffix(basePath, "/responses") && strings.HasPrefix(appendPath, "/v1/responses") {
		return path.Clean(basePath + strings.TrimPrefix(appendPath, "/v1/responses"))
	}
	if strings.HasSuffix(basePath, "/v1") && strings.HasPrefix(appendPath, "/v1/") {
		appendPath = strings.TrimPrefix(appendPath, "/v1")
	}
	return path.Clean(basePath + appendPath)
}

func copyForwardHeaders(dst, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "" ||
			hopByHopHeader(lower) ||
			inboundAuthHeader(lower) ||
			lower == "content-length" ||
			lower == "accept-encoding" ||
			lower == "host" ||
			strings.HasPrefix(lower, "x-sub2api-") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func applyUpstreamAuth(header http.Header, account *service.Account) {
	header.Del("Authorization")
	header.Del("X-Api-Key")
	header.Del("X-Goog-Api-Key")
	switch account.Platform {
	case service.PlatformGemini:
		token := strings.TrimSpace(account.GetCredential("access_token"))
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
			return
		}
		if apiKey := strings.TrimSpace(account.GetCredential("api_key")); apiKey != "" {
			header.Set("X-Goog-Api-Key", apiKey)
		}
	case service.PlatformAnthropic, service.PlatformAntigravity:
		if apiKey := strings.TrimSpace(account.GetCredential("api_key")); apiKey != "" {
			header.Set("X-Api-Key", apiKey)
		} else if token := strings.TrimSpace(account.GetCredential("access_token")); token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
		if header.Get("Anthropic-Version") == "" {
			header.Set("Anthropic-Version", "2023-06-01")
		}
	default:
		token := strings.TrimSpace(account.GetCredential("access_token"))
		if token == "" {
			token = strings.TrimSpace(account.GetCredential("api_key"))
		}
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
		}
	}
	if account != nil && account.Platform == service.PlatformOpenAI && account.Type == service.AccountTypeOAuth {
		if chatgptAccountID := strings.TrimSpace(account.GetCredential("chatgpt_account_id")); chatgptAccountID != "" {
			header.Set("chatgpt-account-id", chatgptAccountID)
		}
		if header.Get("OpenAI-Beta") == "" {
			header.Set("OpenAI-Beta", "responses=experimental")
		}
		if header.Get("originator") == "" {
			header.Set("originator", "codex_cli_rs")
		}
		if strings.TrimSpace(header.Get("User-Agent")) == "" || !openai.IsCodexOfficialClientByHeaders(header.Get("User-Agent"), header.Get("originator")) {
			header.Set("User-Agent", subsiteCodexCLIUserAgent)
		}
	}
}

func resolveSubsiteOpenAIOriginator(c *gin.Context) string {
	if c == nil {
		return "codex_cli_rs"
	}
	if originator := strings.TrimSpace(c.GetHeader("originator")); originator != "" {
		return originator
	}
	return "codex_cli_rs"
}

func firstNonEmptyHeader(c *gin.Context, keys ...string) string {
	if c == nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(c.GetHeader(key)); value != "" {
			return value
		}
	}
	return ""
}

func isolateSubsiteSessionID(apiKeyID int64, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if apiKeyID <= 0 {
		return raw
	}
	h := xxhash.New()
	_, _ = fmt.Fprintf(h, "k%d:", apiKeyID)
	_, _ = h.WriteString(raw)
	return fmt.Sprintf("%016x", h.Sum64())
}

func streamOrBufferResponse(c *gin.Context, resp *http.Response) ([]byte, bool, *responseCaptureMetrics, error) {
	copyResponseHeaders(c.Writer.Header(), resp.Header)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Status(resp.StatusCode)
	metrics := &responseCaptureMetrics{}
	body, err := readUpstreamBodyWithMetrics(resp.Body, contentType, metrics)
	if err != nil {
		return nil, false, nil, fmt.Errorf("read upstream response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, false, metrics, nil
	}
	return body, false, metrics, nil
}

func writeBufferedResponse(c *gin.Context, resp *http.Response, body []byte) {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Data(resp.StatusCode, contentType, body)
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "" || hopByHopHeader(lower) || lower == "content-length" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func buildUsageIngestItem(c *gin.Context, authorization *service.AuthorizeSubsiteResponse, requestBody, responseBody []byte, upstreamEndpoint string, duration time.Duration, metrics *responseCaptureMetrics) service.UsageIngestItem {
	usage := parseUsageForPath(c.Request.URL.Path, responseBody)
	requestType := service.RequestTypeSync
	if isStreamRequest(c, requestBody) || isSSEContentType(c.Writer.Header().Get("Content-Type")) {
		requestType = service.RequestTypeStream
	}
	imageCount, imageSize := parseImageRequest(c, requestBody)
	durationMs := durationMillisPtr(duration)
	var firstTokenMs *int
	if metrics != nil {
		firstTokenMs = metrics.firstTokenMs
	}
	return service.UsageIngestItem{
		RequestID:             authorization.RequestID,
		ReservationID:         authorization.ReservationID,
		APIKeyID:              authorization.APIKeyID,
		UserID:                authorization.UserID,
		AccountID:             authorization.AccountID,
		GroupID:               authorization.GroupID,
		SubscriptionID:        authorization.SubscriptionID,
		AccountType:           authorization.Credential.AccountType,
		Model:                 authorization.MappedModel,
		RequestedModel:        authorization.RequestedModel,
		ServiceTier:           strings.TrimSpace(gjson.GetBytes(requestBody, "service_tier").String()),
		ReasoningEffort:       extractReasoningEffort(requestBody),
		BillingType:           authorization.BillingType,
		RequestType:           int16(requestType),
		InputTokens:           usage.inputTokens,
		OutputTokens:          usage.outputTokens,
		CacheCreationTokens:   usage.cacheCreationTokens,
		CacheCreation5mTokens: usage.cacheCreation5mTokens,
		CacheCreation1hTokens: usage.cacheCreation1hTokens,
		CacheReadTokens:       usage.cacheReadTokens,
		ImageOutputTokens:     usage.imageOutputTokens,
		ImageCount:            imageCount,
		ImageSize:             imageSize,
		MediaType:             usage.mediaType,
		RequestFingerprint:    requestFingerprint(c.Request.Method, c.Request.URL.Path, requestBody),
		RequestPayloadHash:    hashBytes(requestBody),
		InboundEndpoint:       c.Request.URL.Path,
		UpstreamEndpoint:      upstreamEndpoint,
		UserAgent:             c.GetHeader("User-Agent"),
		IPAddress:             clientIP(c),
		DurationMs:            durationMs,
		FirstTokenMs:          firstTokenMs,
		OccurredAt:            time.Now().Add(-duration),
	}
}

type parsedUsage struct {
	inputTokens           int
	outputTokens          int
	cacheCreationTokens   int
	cacheCreation5mTokens int
	cacheCreation1hTokens int
	cacheReadTokens       int
	imageOutputTokens     int
	mediaType             string
}

func parseUsageForPath(inboundPath string, responseBody []byte) parsedUsage {
	switch {
	case strings.HasPrefix(inboundPath, "/v1beta/"):
		return parseGeminiUsage(responseBody)
	case strings.Contains(inboundPath, "/messages"):
		return parseClaudeUsage(responseBody)
	default:
		return parseOpenAIUsage(responseBody)
	}
}

func parseOpenAIUsage(body []byte) parsedUsage {
	if isLikelySSE(body) {
		return parseOpenAISSEUsage(string(body))
	}
	usage := findOpenAIUsageObject(body)
	return openAIUsageFromResult(usage)
}

func parseOpenAISSEUsage(body string) parsedUsage {
	var out parsedUsage
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		data, ok := extractSSEDataLine(scanner.Text())
		if !ok || data == "" || data == "[DONE]" || !gjson.Valid(data) {
			continue
		}
		current := openAIUsageFromResult(findOpenAIUsageObject([]byte(data)))
		mergeUsage(&out, current)
	}
	return out
}

func findOpenAIUsageObject(body []byte) gjson.Result {
	for _, candidate := range []string{"usage", "response.usage"} {
		if usage := gjson.GetBytes(body, candidate); usage.Exists() {
			return usage
		}
	}
	return gjson.Result{}
}

func openAIUsageFromResult(usage gjson.Result) parsedUsage {
	if !usage.Exists() {
		return parsedUsage{}
	}
	input := int(usage.Get("input_tokens").Int())
	output := int(usage.Get("output_tokens").Int())
	cached := int(usage.Get("input_tokens_details.cached_tokens").Int())
	imageOutput := int(usage.Get("output_tokens_details.image_tokens").Int())
	return parsedUsage{
		inputTokens:       input - cached,
		outputTokens:      output,
		cacheReadTokens:   cached,
		imageOutputTokens: imageOutput,
	}
}

func parseClaudeUsage(body []byte) parsedUsage {
	if isLikelySSE(body) {
		return parseClaudeSSEUsage(string(body))
	}
	usage := gjson.GetBytes(body, "usage")
	return claudeUsageFromResult(usage)
}

func parseClaudeSSEUsage(body string) parsedUsage {
	var out parsedUsage
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		data, ok := extractSSEDataLine(scanner.Text())
		if !ok || data == "" || data == "[DONE]" || !gjson.Valid(data) {
			continue
		}
		parsed := gjson.Parse(data)
		var usage gjson.Result
		switch parsed.Get("type").String() {
		case "message_start":
			usage = parsed.Get("message.usage")
		case "message_delta":
			usage = parsed.Get("usage")
		default:
			usage = parsed.Get("usage")
		}
		current := claudeUsageFromResult(usage)
		mergeUsage(&out, current)
	}
	return out
}

func claudeUsageFromResult(usage gjson.Result) parsedUsage {
	if !usage.Exists() {
		return parsedUsage{}
	}
	cc5m := int(usage.Get("cache_creation.ephemeral_5m_input_tokens").Int())
	cc1h := int(usage.Get("cache_creation.ephemeral_1h_input_tokens").Int())
	cacheCreation := int(usage.Get("cache_creation_input_tokens").Int())
	if cacheCreation == 0 {
		cacheCreation = cc5m + cc1h
	}
	cacheRead := int(usage.Get("cache_read_input_tokens").Int())
	if cacheRead == 0 {
		cacheRead = int(usage.Get("cached_tokens").Int())
	}
	return parsedUsage{
		inputTokens:           int(usage.Get("input_tokens").Int()),
		outputTokens:          int(usage.Get("output_tokens").Int()),
		cacheCreationTokens:   cacheCreation,
		cacheCreation5mTokens: cc5m,
		cacheCreation1hTokens: cc1h,
		cacheReadTokens:       cacheRead,
	}
}

func parseGeminiUsage(body []byte) parsedUsage {
	if isLikelySSE(body) {
		return parseGeminiSSEUsage(string(body))
	}
	return geminiUsageFromResult(gjson.GetBytes(body, "usageMetadata"))
}

func newUpstreamError(statusCode int, contentType string, body []byte) error {
	message := strings.TrimSpace(extractUpstreamErrorMessage(body))
	if message == "" {
		message = fmt.Sprintf("upstream returned %d", statusCode)
	} else {
		message = fmt.Sprintf("upstream returned %d: %s", statusCode, message)
	}
	return &upstreamError{
		statusCode:  statusCode,
		contentType: strings.TrimSpace(contentType),
		body:        append([]byte(nil), body...),
		message:     message,
	}
}

func extractUpstreamErrorMessage(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 || !gjson.ValidBytes(body) {
		return strings.TrimSpace(string(body))
	}
	for _, path := range []string{
		"error.message",
		"response.error.message",
		"detail.message",
		"detail",
		"message",
	} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return strings.TrimSpace(string(body))
}

func shouldFailoverForUpstreamError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var upstreamErr *upstreamError
	if errors.As(err, &upstreamErr) && upstreamErr != nil {
		bodyLower := strings.ToLower(string(upstreamErr.body))
		messageLower := strings.ToLower(strings.TrimSpace(upstreamErr.message))
		switch upstreamErr.statusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests:
			return true
		case http.StatusBadRequest:
			if strings.Contains(bodyLower, "api.responses.write") || strings.Contains(messageLower, "api.responses.write") {
				return true
			}
			if strings.Contains(bodyLower, "insufficient permissions") || strings.Contains(messageLower, "insufficient permissions") {
				return true
			}
			if strings.Contains(bodyLower, "refresh_token_reused") || strings.Contains(messageLower, "refresh_token_reused") {
				return true
			}
		}
		if upstreamErr.statusCode >= http.StatusInternalServerError {
			return true
		}
	}
	return isRetryableUpstreamTransportError(err)
}

func isRetryableUpstreamTransportError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
		type temporary interface {
			Temporary() bool
		}
		if tempErr, ok := any(netErr).(temporary); ok && tempErr.Temporary() {
			return true
		}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	messageLower := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, fragment := range []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"tls handshake timeout",
		"client connection lost",
		"no such host",
	} {
		if strings.Contains(messageLower, fragment) {
			return true
		}
	}
	return false
}

func parseGeminiSSEUsage(body string) parsedUsage {
	var out parsedUsage
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		data, ok := extractSSEDataLine(scanner.Text())
		if !ok || data == "" || data == "[DONE]" || !gjson.Valid(data) {
			continue
		}
		current := geminiUsageFromResult(gjson.Get(data, "usageMetadata"))
		mergeUsage(&out, current)
	}
	return out
}

func geminiUsageFromResult(usage gjson.Result) parsedUsage {
	if !usage.Exists() {
		return parsedUsage{}
	}
	prompt := int(usage.Get("promptTokenCount").Int())
	cached := int(usage.Get("cachedContentTokenCount").Int())
	imageTokens := 0
	usage.Get("candidatesTokensDetails").ForEach(func(_, detail gjson.Result) bool {
		if strings.EqualFold(detail.Get("modality").String(), "IMAGE") {
			imageTokens = int(detail.Get("tokenCount").Int())
			return false
		}
		return true
	})
	return parsedUsage{
		inputTokens:       prompt - cached,
		outputTokens:      int(usage.Get("candidatesTokenCount").Int()) + int(usage.Get("thoughtsTokenCount").Int()),
		cacheReadTokens:   cached,
		imageOutputTokens: imageTokens,
	}
}

func mergeUsage(dst *parsedUsage, current parsedUsage) {
	if current.inputTokens > 0 {
		dst.inputTokens = current.inputTokens
	}
	if current.outputTokens > 0 {
		dst.outputTokens = current.outputTokens
	}
	if current.cacheCreationTokens > 0 {
		dst.cacheCreationTokens = current.cacheCreationTokens
	}
	if current.cacheCreation5mTokens > 0 {
		dst.cacheCreation5mTokens = current.cacheCreation5mTokens
	}
	if current.cacheCreation1hTokens > 0 {
		dst.cacheCreation1hTokens = current.cacheCreation1hTokens
	}
	if current.cacheReadTokens > 0 {
		dst.cacheReadTokens = current.cacheReadTokens
	}
	if current.imageOutputTokens > 0 {
		dst.imageOutputTokens = current.imageOutputTokens
	}
	if current.mediaType != "" {
		dst.mediaType = current.mediaType
	}
}

func parseImageRequest(c *gin.Context, body []byte) (int, string) {
	if !isImageEndpoint(c.Request.URL.Path) {
		return 0, ""
	}
	count := int(gjson.GetBytes(body, "n").Int())
	size := strings.TrimSpace(gjson.GetBytes(body, "size").String())
	if isMultipartRequest(c) {
		if rawN := strings.TrimSpace(extractMultipartField(c.GetHeader("Content-Type"), body, "n")); rawN != "" {
			if parsed, err := strconv.Atoi(rawN); err == nil {
				count = parsed
			}
		}
		size = strings.TrimSpace(extractMultipartField(c.GetHeader("Content-Type"), body, "size"))
	}
	if count <= 0 {
		count = 1
	}
	return count, size
}

func replaceMultipartModel(contentType string, body []byte, mappedModel string) ([]byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", err
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, "", errors.New("multipart boundary is required")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		header := textproto.MIMEHeader{}
		for key, values := range part.Header {
			for _, value := range values {
				header.Add(key, value)
			}
		}
		dst, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if part.FormName() == "model" {
			_, err = dst.Write([]byte(mappedModel))
		} else {
			_, err = io.Copy(dst, part)
		}
		if err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

func extractMultipartField(contentType string, body []byte, fieldName string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary := params["boundary"]
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return ""
		}
		if err != nil {
			return ""
		}
		if part.FormName() != fieldName {
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, 64<<10))
		if err != nil {
			return ""
		}
		return string(value)
	}
}

func isMultipartRequest(c *gin.Context) bool {
	return strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/")
}

func isSSEContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func isLikelySSE(body []byte) bool {
	return bytes.Contains(body, []byte("\ndata:")) || bytes.HasPrefix(bytes.TrimSpace(body), []byte("data:"))
}

func extractSSEDataLine(line string) (string, bool) {
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
}

func readUpstreamBodyWithMetrics(reader io.Reader, contentType string, metrics *responseCaptureMetrics) ([]byte, error) {
	startedAt := time.Now()
	var out bytes.Buffer
	chunk := make([]byte, 32*1024)
	sseContentType := isSSEContentType(contentType)
	var pendingSSE []byte
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			if out.Len()+n > maxBufferedUpstreamBody {
				return nil, fmt.Errorf("upstream response exceeds buffer limit")
			}
			part := chunk[:n]
			_, _ = out.Write(part)
			if sseContentType && metrics != nil && metrics.firstTokenMs == nil {
				pendingSSE = append(pendingSSE, part...)
				pendingSSE = observeSSEFirstToken(pendingSSE, startedAt, metrics)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	if sseContentType && metrics != nil && metrics.firstTokenMs == nil && len(pendingSSE) > 0 {
		_ = observeSSELine(bytes.TrimRight(pendingSSE, "\r"), startedAt, metrics)
	}
	return out.Bytes(), nil
}

func observeSSEFirstToken(pending []byte, startedAt time.Time, metrics *responseCaptureMetrics) []byte {
	for {
		idx := bytes.IndexByte(pending, '\n')
		if idx < 0 {
			return pending
		}
		line := bytes.TrimRight(pending[:idx], "\r")
		_ = observeSSELine(line, startedAt, metrics)
		pending = pending[idx+1:]
	}
}

func observeSSELine(line []byte, startedAt time.Time, metrics *responseCaptureMetrics) bool {
	if metrics == nil || metrics.firstTokenMs != nil {
		return false
	}
	data, ok := extractSSEDataLine(string(line))
	if !ok || data == "" || data == "[DONE]" || !gjson.Valid(data) {
		return false
	}
	if !ssePayloadHasToken([]byte(data)) {
		return false
	}
	value := int(time.Since(startedAt).Milliseconds())
	metrics.firstTokenMs = &value
	return true
}

func ssePayloadHasToken(payload []byte) bool {
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if eventType != "" {
		switch eventType {
		case "response.created", "response.in_progress", "response.output_item.added", "response.output_item.done":
			return false
		}
		if strings.Contains(eventType, ".delta") {
			return true
		}
		if strings.HasPrefix(eventType, "response.output_text") || strings.HasPrefix(eventType, "response.output") {
			return true
		}
		if eventType == "response.completed" || eventType == "response.done" {
			return strings.TrimSpace(gjson.GetBytes(payload, "response.output.0.content.0.text").String()) != ""
		}
	}
	if strings.TrimSpace(gjson.GetBytes(payload, "choices.0.delta.content").String()) != "" {
		return true
	}
	if strings.TrimSpace(gjson.GetBytes(payload, "delta.text").String()) != "" {
		return true
	}
	if strings.TrimSpace(gjson.GetBytes(payload, "message.delta.text").String()) != "" {
		return true
	}
	if strings.TrimSpace(gjson.GetBytes(payload, "candidates.0.content.parts.0.text").String()) != "" {
		return true
	}
	return false
}

func durationMillisPtr(duration time.Duration) *int {
	if duration < 0 {
		duration = 0
	}
	value := int(duration.Milliseconds())
	return &value
}

func hopByHopHeader(lower string) bool {
	switch lower {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func inboundAuthHeader(lower string) bool {
	switch lower {
	case "authorization", "x-api-key", "x-goog-api-key":
		return true
	default:
		return false
	}
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func shouldProxyOpenAIChatCompletionsThroughResponses(inboundPath string, authorization *service.AuthorizeSubsiteResponse) bool {
	if authorization == nil {
		return false
	}
	return strings.Contains(inboundPath, "/chat/completions") &&
		strings.EqualFold(strings.TrimSpace(authorization.Platform), service.PlatformOpenAI)
}

func normalizeSubsiteOpenAIPassthroughOAuthBody(body []byte, compact bool) ([]byte, error) {
	normalized := body
	for _, field := range []string{"user", "metadata", "prompt_cache_retention", "safety_identifier", "stream_options"} {
		if gjson.GetBytes(normalized, field).Exists() {
			next, err := sjson.DeleteBytes(normalized, field)
			if err != nil {
				return body, fmt.Errorf("normalize oauth body delete %s: %w", field, err)
			}
			normalized = next
		}
	}
	if compact {
		if gjson.GetBytes(normalized, "store").Exists() {
			next, err := sjson.DeleteBytes(normalized, "store")
			if err != nil {
				return body, fmt.Errorf("normalize oauth body delete store: %w", err)
			}
			normalized = next
		}
		if gjson.GetBytes(normalized, "stream").Exists() {
			next, err := sjson.DeleteBytes(normalized, "stream")
			if err != nil {
				return body, fmt.Errorf("normalize oauth body delete stream: %w", err)
			}
			normalized = next
		}
		return normalized, nil
	}
	if store := gjson.GetBytes(normalized, "store"); !store.Exists() || store.Type != gjson.False {
		next, err := sjson.SetBytes(normalized, "store", false)
		if err != nil {
			return body, fmt.Errorf("normalize oauth body store=false: %w", err)
		}
		normalized = next
	}
	if stream := gjson.GetBytes(normalized, "stream"); !stream.Exists() || stream.Type != gjson.True {
		next, err := sjson.SetBytes(normalized, "stream", true)
		if err != nil {
			return body, fmt.Errorf("normalize oauth body stream=true: %w", err)
		}
		normalized = next
	}
	return normalized, nil
}

func applySubsiteCodexOAuthTransform(reqBody map[string]any, compact bool) {
	if reqBody == nil {
		return
	}
	if compact {
		delete(reqBody, "store")
		delete(reqBody, "stream")
	} else {
		reqBody["store"] = false
		reqBody["stream"] = true
	}
	for _, key := range []string{
		"user", "metadata", "prompt_cache_retention", "safety_identifier", "stream_options",
		"max_output_tokens", "max_completion_tokens", "temperature", "top_p", "frequency_penalty", "presence_penalty",
	} {
		delete(reqBody, key)
	}
	if inputStr, ok := reqBody["input"].(string); ok {
		trimmed := strings.TrimSpace(inputStr)
		if trimmed != "" {
			reqBody["input"] = []any{
				map[string]any{
					"type":    "message",
					"role":    "user",
					"content": inputStr,
				},
			}
		} else {
			reqBody["input"] = []any{}
		}
	}
	if instructions, ok := reqBody["instructions"].(string); !ok || strings.TrimSpace(instructions) == "" {
		reqBody["instructions"] = "You are a helpful coding assistant."
	}
}

func convertResponsesBodyToChatCompletions(c *gin.Context, responseBody []byte, originalModel string, stream bool) ([]byte, string, error) {
	if stream || isSSEContentType(c.Writer.Header().Get("Content-Type")) || isLikelySSE(responseBody) {
		converted, err := convertResponsesSSEToChatCompletionsSSE(responseBody, originalModel)
		if err != nil {
			return nil, "", err
		}
		return converted, "text/event-stream", nil
	}
	var responsesResp apicompat.ResponsesResponse
	if err := json.Unmarshal(responseBody, &responsesResp); err != nil {
		return nil, "", fmt.Errorf("parse responses body: %w", err)
	}
	chatResp := apicompat.ResponsesToChatCompletions(&responsesResp, originalModel)
	converted, err := json.Marshal(chatResp)
	if err != nil {
		return nil, "", fmt.Errorf("marshal chat completions body: %w", err)
	}
	return converted, "application/json", nil
}

func convertResponsesSSEToChatCompletionsSSE(body []byte, originalModel string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	state := apicompat.NewResponsesEventToChatState()
	state.Model = originalModel
	var out bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if line == "data: [DONE]" {
			continue
		}
		payload := line[6:]
		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("parse responses sse event: %w", err)
		}
		for _, chunk := range apicompat.ResponsesEventToChatChunks(&event, state) {
			sse, err := apicompat.ChatChunkToSSE(chunk)
			if err != nil {
				return nil, fmt.Errorf("marshal chat chunk: %w", err)
			}
			out.WriteString(sse)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan responses sse: %w", err)
	}
	for _, chunk := range apicompat.FinalizeResponsesChatStream(state) {
		sse, err := apicompat.ChatChunkToSSE(chunk)
		if err != nil {
			return nil, fmt.Errorf("finalize chat chunk: %w", err)
		}
		out.WriteString(sse)
	}
	out.WriteString("data: [DONE]\n\n")
	return out.Bytes(), nil
}
