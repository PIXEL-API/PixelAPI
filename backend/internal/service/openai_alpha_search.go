package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	chatgptCodexAlphaSearchURL   = "https://chatgpt.com/backend-api/codex/alpha/search"
	openAIPlatformAlphaSearchURL = "https://api.openai.com/v1/alpha/search"
)

// ForwardAlphaSearch proxies the standalone Codex web-search endpoint without
// binding the endpoint to a versioned request/response schema.
//
// A non-nil result is returned only for a strict 2xx upstream response. Any
// non-failover response (including 1xx/3xx) is rewritten as a locally owned,
// sanitized error and returns an error, so callers can finalize durable billing
// state without treating the request as a successful search.
func (s *OpenAIGatewayService) ForwardAlphaSearch(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	if s == nil || c == nil || account == nil {
		return nil, fmt.Errorf("service, context, and account are required")
	}
	modelResult := gjson.GetBytes(body, "model")
	requestedModel := strings.TrimSpace(modelResult.String())
	if modelResult.Type != gjson.String || requestedModel == "" {
		return nil, fmt.Errorf("model is required")
	}

	upstreamModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(requestedModel))
	if upstreamModel != "" && upstreamModel != requestedModel {
		body = ReplaceModelInBody(body, upstreamModel)
	}

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	clientBeta := strings.TrimSpace(c.GetHeader("OpenAI-Beta"))
	req, err := s.buildUpstreamRequestOpenAIPassthrough(
		WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI),
		c,
		account,
		body,
		token,
	)
	if err != nil {
		return nil, err
	}

	targetURL, err := s.openAIAlphaSearchURL(account)
	if err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse alpha search URL: %w", err)
	}
	if c.Request != nil && c.Request.URL != nil {
		query := parsedURL.Query()
		for key, values := range c.Request.URL.Query() {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		parsedURL.RawQuery = query.Encode()
	}
	req.URL = parsedURL
	req.Header.Set("Accept", "application/json")
	if clientBeta == "" {
		req.Header.Del("OpenAI-Beta")
	}
	if version := strings.TrimSpace(c.GetHeader("Version")); version != "" {
		req.Header.Set("Version", version)
	} else if account.Type == AccountTypeOAuth {
		req.Header.Set("Version", codexCLIVersion)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) && !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
		MarkResponseCommitted(c)
		writeOpenAIPassthroughErrorEnvelope(c, http.StatusBadGateway, resp.Header, "Upstream request failed")
		return nil, fmt.Errorf("alpha search upstream returned non-success status %d", resp.StatusCode)
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read alpha search response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		upstreamMessage := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMessage, respBody) {
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			s.handleFailoverSideEffectsForModel(ctx, resp, account, requestedModel)
			return nil, newOpenAIUpstreamFailoverError(
				resp.StatusCode,
				resp.Header,
				respBody,
				upstreamMessage,
				account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			)
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		MarkResponseCommitted(c)
		writeSanitizedOpenAIPassthroughError(c, resp.StatusCode, resp.Header)
		return nil, fmt.Errorf("alpha search upstream returned non-success status %d", resp.StatusCode)
	}

	s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	result := &OpenAIForwardResult{
		RequestID:       strings.TrimSpace(resp.Header.Get("x-request-id")),
		Model:           requestedModel,
		UpstreamModel:   upstreamModel,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(upstreamStart),
		WebSearchCalls:  1,
	}
	c.Data(resp.StatusCode, contentType, respBody)
	return result, nil
}

func (s *OpenAIGatewayService) openAIAlphaSearchURL(account *Account) (string, error) {
	if account == nil {
		return "", fmt.Errorf("account is required")
	}
	switch account.Type {
	case AccountTypeOAuth:
		return chatgptCodexAlphaSearchURL, nil
	case AccountTypeAPIKey:
		baseURL := account.GetOpenAIBaseURL()
		if baseURL == "" {
			return openAIPlatformAlphaSearchURL, nil
		}
		validatedURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return "", err
		}
		return buildOpenAIEndpointURL(validatedURL, "/v1/alpha/search"), nil
	default:
		return "", fmt.Errorf("unsupported OpenAI account type: %s", account.Type)
	}
}
