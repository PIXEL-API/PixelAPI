package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	defaultGrokWebSearchResults = 5
	maxGrokWebSearchResults     = 20
)

func (h *GatewayHandler) WebSearch(c *gin.Context) {
	isXSearch := c.GetBool("grok_x_search_endpoint")
	var req grokStandaloneSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type":    "invalid_request_error",
			"message": err.Error(),
		}})
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = strings.TrimSpace(req.Input)
	}
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type":    "invalid_request_error",
			"message": "query is required",
		}})
		return
	}
	req.Query = query
	maxResults := 0
	if req.MaxResults != nil {
		maxResults = *req.MaxResults
	}
	maxResults = normalizeGrokWebSearchMaxResults(maxResults)
	searchModel := resolveGrokStandaloneSearchModel()
	searchLabel := "web_search"
	if isXSearch {
		searchLabel = "x_search"
	}

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{
			"type":    "authentication_error",
			"message": "API key required",
		}})
		return
	}

	if apiKey.Group == nil || apiKey.Group.Platform != "grok" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type":    "invalid_request_error",
			"message": searchLabel + " is only supported for grok groups",
		}})
		return
	}
	searchPrice := apiKey.Group.GetSearchPricePer1k()
	if searchPrice == nil || math.IsNaN(*searchPrice) || math.IsInf(*searchPrice, 0) || *searchPrice < 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"type":    "billing_configuration_error",
			"message": "grok search_price_per_1k must be explicitly configured",
		}})
		return
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		c.JSON(status, gin.H{"error": gin.H{"type": code, "message": message}})
		return
	}

	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	reqLog := requestLogger(c, "handler.gateway.web_search")
	auditBody, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user", "content": req.Query,
		}},
	})
	if decision := h.checkCyberPreflight(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, searchModel, auditBody); decision != nil && decision.Blocked {
		c.JSON(contentModerationStatus(decision), gin.H{"error": gin.H{
			"type":    cyberPreflightErrorCode(decision),
			"message": decision.Message,
		}})
		return
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, searchModel, auditBody); decision != nil && decision.Blocked {
		c.JSON(contentModerationStatus(decision), gin.H{"error": gin.H{
			"type":    contentModerationErrorCode(decision),
			"message": decision.Message,
		}})
		return
	}

	groupID := apiKey.GroupID
	if groupID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type":    "invalid_request_error",
			"message": "group required",
		}})
		return
	}

	failedAccounts := make(map[int64]struct{})
	var account *service.Account
	var accountReleaseFunc func()
	var nativeResp *websearch.SearchResponse
	var providerName string
	var err error

	defer func() {
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
	}()

	// First attempt plus up to three failover accounts, matching the upstream contract.
	for attempt := 0; attempt < 4; attempt++ {
		selected, selectErr := h.gatewayService.SelectAccountWithLoadAwareness(
			c.Request.Context(), groupID, "", searchModel, failedAccounts, "", 0,
		)
		if selectErr != nil {
			if attempt == 0 {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
					"type":    "scheduling_error",
					"message": selectErr.Error(),
				}})
				return
			}
			break
		}
		if selected == nil || selected.Account == nil {
			if attempt == 0 {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
					"type":    "scheduling_error",
					"message": "No available accounts",
				}})
				return
			}
			break
		}

		release, acquireOK, acquireErr := h.acquireWebSearchAccountSlot(c, selected)
		if !acquireOK {
			if attempt == 0 && acquireErr != nil {
				h.handleConcurrencyError(c, acquireErr, "account", false)
				return
			}
			failedAccounts[selected.Account.ID] = struct{}{}
			continue
		}
		account = selected.Account
		accountReleaseFunc = release
		setOpsSelectedAccount(c, account.ID, account.Platform)
		if decision := h.checkUserContentModeration(c, reqLog, apiKey, subject, account, service.ContentModerationProtocolOpenAIChat, searchModel, auditBody); decision != nil && decision.Blocked {
			c.JSON(contentModerationStatus(decision), gin.H{"error": gin.H{
				"type":    contentModerationErrorCode(decision),
				"message": decision.Message,
			}})
			return
		}

		if isXSearch {
			nativeResp, providerName, err = h.doGrokNativeXSearch(c.Request.Context(), account, req, searchModel, maxResults)
		} else {
			nativeResp, providerName, err = h.doGrokNativeWebSearch(c.Request.Context(), account, req.Query, maxResults, searchModel)
		}
		if err == nil {
			break
		}
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(err, &failoverErr) || !failoverErr.ShouldRetryNextAccount() {
			break
		}
		failedAccounts[account.ID] = struct{}{}
		if accountReleaseFunc != nil {
			accountReleaseFunc()
			accountReleaseFunc = nil
		}
		account = nil
	}
	if err != nil {
		message := err.Error()
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "web_search_error", "message": message}})
		return
	}
	if account == nil || nativeResp == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"type":    "scheduling_error",
			"message": "No available accounts",
		}})
		return
	}

	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetSecurityClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	requestPayloadHash := service.HashUsageRequestPayload([]byte(req.Query))
	// Billing request IDs must be unique per invocation. Deriving this from query
	// content would incorrectly collapse intentional repeated searches.
	searchRequestID := searchLabel + ":" + uuid.NewString()
	if *searchPrice == 0 {
		logger.L().With(
			zap.String("component", "handler.gateway.web_search"),
			zap.Int64("group_id", apiKey.Group.ID),
		).Info("gateway.web_search.search_price_per_1k_explicit_free")
	}
	// The project's submitUsageRecordTask already falls back to synchronous
	// execution when its worker pool is absent or full, so billing work is not
	// silently dropped even though this standalone request has no token usage.
	h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if recordErr := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
			Result: &service.ForwardResult{
				RequestID:   searchRequestID,
				Model:       "grok-" + strings.ReplaceAll(searchLabel, "_", "-"),
				SearchCount: 1,
			},
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
		}); recordErr != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.web_search"),
				zap.Int64("user_id", apiKey.User.ID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Int64("account_id", account.ID),
			).Error("gateway.web_search.record_usage_failed", zap.Error(recordErr))
		}
	})

	c.JSON(http.StatusOK, gin.H{
		"query":       req.Query,
		"results":     nativeResp.Results,
		"provider":    providerName,
		"max_results": maxResults,
	})
}

// acquireWebSearchAccountSlot resolves an immediately acquired slot or waits
// according to the scheduler's WaitPlan. A full wait queue can fail over.
func (h *GatewayHandler) acquireWebSearchAccountSlot(
	c *gin.Context,
	selected *service.AccountSelectionResult,
) (release func(), ok bool, acquireErr error) {
	if selected == nil || selected.Account == nil {
		return nil, false, nil
	}
	if selected.Acquired {
		return selected.ReleaseFunc, true, nil
	}
	if selected.WaitPlan == nil || h.concurrencyHelper == nil {
		return nil, false, nil
	}
	account := selected.Account
	accountWaitCounted := false
	canWait, waitErr := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selected.WaitPlan.MaxWaiting)
	if waitErr != nil {
		logger.L().Warn("gateway.web_search.account_wait_counter_increment_failed",
			zap.Int64("account_id", account.ID),
			zap.Error(waitErr),
		)
	} else if !canWait {
		return nil, false, nil
	} else {
		accountWaitCounted = true
	}
	releaseWait := func() {
		if accountWaitCounted {
			h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
			accountWaitCounted = false
		}
	}
	streamStarted := false
	slotRelease, err := h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
		c,
		account.ID,
		selected.WaitPlan.MaxConcurrency,
		selected.WaitPlan.Timeout,
		false,
		&streamStarted,
	)
	releaseWait()
	if err != nil {
		return nil, false, err
	}
	return slotRelease, true, nil
}

func (h *GatewayHandler) doGrokNativeWebSearch(ctx context.Context, account *service.Account, query string, maxResults int, model string) (*websearch.SearchResponse, string, error) {
	maxResults = normalizeGrokWebSearchMaxResults(maxResults)
	searchBody := map[string]any{
		"model":   xai.ResolveGrokTextResponsesModelID(model),
		"input":   buildGrokWebSearchPrompt(query, maxResults),
		"tools":   []map[string]any{{"type": "web_search"}},
		"include": []string{"web_search_call.action.sources"},
		"store":   false,
		"stream":  false,
	}
	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return nil, "", fmt.Errorf("encode grok web search request: %w", err)
	}

	respBytes, err := h.gatewayService.DoGrokNativeResponsesJSON(ctx, account, bodyBytes)
	if err != nil {
		return nil, "", err
	}
	return &websearch.SearchResponse{
		Results: extractGrokWebSearchSources(respBytes, maxResults),
		Query:   query,
	}, "grok-native", nil
}

func (h *GatewayHandler) doGrokNativeXSearch(ctx context.Context, account *service.Account, req grokStandaloneSearchRequest, model string, maxResults int) (*websearch.SearchResponse, string, error) {
	maxResults = normalizeGrokWebSearchMaxResults(maxResults)
	bodyBytes, err := buildGrokXSearchResponsesBody(req, model)
	if err != nil {
		return nil, "", err
	}
	respBytes, err := h.gatewayService.DoGrokNativeResponsesJSON(ctx, account, bodyBytes)
	if err != nil {
		return nil, "", err
	}
	return &websearch.SearchResponse{
		Results: extractGrokWebSearchSources(respBytes, maxResults),
		Query:   req.Query,
	}, "grok-native", nil
}

func normalizeGrokWebSearchMaxResults(maxResults int) int {
	if maxResults <= 0 {
		return defaultGrokWebSearchResults
	}
	if maxResults > maxGrokWebSearchResults {
		return maxGrokWebSearchResults
	}
	return maxResults
}

func buildGrokWebSearchPrompt(query string, maxResults int) string {
	return fmt.Sprintf(`Search the web for the user query below. Return ONLY valid JSON with this exact shape: {"results":[{"url":"https://...","title":"page title","snippet":"concise factual summary"}]}. Return at most %d unique results. Every URL must be an actual web_search source. Populate a non-empty title and snippet for every result. Do not wrap the JSON in markdown.

User query:
%s`, normalizeGrokWebSearchMaxResults(maxResults), query)
}

// extractGrokWebSearchSources only accepts model-enriched results whose
// normalized URL is present in native search sources. Raw native sources are
// returned as the fallback so model-produced URLs never become trusted input.
func extractGrokWebSearchSources(body []byte, maxResults int) []websearch.SearchResult {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return nil
	}
	maxResults = normalizeGrokWebSearchMaxResults(maxResults)

	sources := make(map[string]websearch.SearchResult)
	sourceOrder := make([]string, 0)
	addSource := func(rawURL, title, snippet string) {
		key, ok := normalizeGrokWebSearchURL(rawURL)
		if !ok {
			return
		}
		result, exists := sources[key]
		if !exists {
			result.URL = key
			sourceOrder = append(sourceOrder, key)
		}
		if result.Title == "" {
			result.Title = usableGrokWebSearchTitle(title, result.URL)
		}
		if result.Snippet == "" {
			result.Snippet = strings.TrimSpace(snippet)
		}
		sources[key] = result
	}

	output := gjson.GetBytes(body, "response.output")
	if !output.IsArray() {
		output = gjson.GetBytes(body, "output")
	}
	output.ForEach(func(_, item gjson.Result) bool {
		callType := item.Get("type").String()
		if callType == "web_search_call" || callType == "x_search_call" {
			callSources := item.Get("action.sources")
			if callSources.IsArray() {
				callSources.ForEach(func(_, source gjson.Result) bool {
					addSource(source.Get("url").String(), source.Get("title").String(), source.Get("snippet").String())
					return true
				})
			}
		}
		if callType == "message" {
			item.Get("content").ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() != "output_text" {
					return true
				}
				part.Get("annotations").ForEach(func(_, annotation gjson.Result) bool {
					annotationType := annotation.Get("type").String()
					if annotationType == "url_citation" || annotationType == "web" {
						addSource(annotation.Get("url").String(), annotation.Get("title").String(), annotation.Get("snippet").String())
					}
					return true
				})
				return true
			})
		}
		return true
	})

	out := make([]websearch.SearchResult, 0, min(maxResults, len(sources)))
	seen := make(map[string]bool)
	output.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "message" {
			return true
		}
		item.Get("content").ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() != "output_text" || len(out) >= maxResults {
				return true
			}
			for _, result := range parseGrokWebSearchStructuredResults(part.Get("text").String()) {
				key, ok := normalizeGrokWebSearchURL(result.URL)
				if !ok || seen[key] {
					continue
				}
				source, allowed := sources[key]
				if !allowed {
					continue
				}
				seen[key] = true
				result.URL = source.URL
				result.Title = usableGrokWebSearchTitle(result.Title, result.URL)
				if result.Title == "" {
					result.Title = source.Title
				}
				result.Snippet = strings.TrimSpace(result.Snippet)
				if result.Snippet == "" {
					result.Snippet = source.Snippet
				}
				out = append(out, result)
				if len(out) >= maxResults {
					break
				}
			}
			return true
		})
		return len(out) < maxResults
	})

	for _, key := range sourceOrder {
		if len(out) >= maxResults {
			break
		}
		if seen[key] {
			continue
		}
		result := sources[key]
		if result.Title == "" {
			result.Title = grokWebSearchTitleFromURL(result.URL)
		}
		seen[key] = true
		out = append(out, result)
	}
	return out
}

func parseGrokWebSearchStructuredResults(text string) []websearch.SearchResult {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return nil
	}
	var payload struct {
		Results []websearch.SearchResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &payload); err != nil {
		return nil
	}
	return payload.Results
}

func normalizeGrokWebSearchURL(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), true
}

func usableGrokWebSearchTitle(title, rawURL string) string {
	title = strings.TrimSpace(title)
	if title == "" || title == rawURL {
		return ""
	}
	if _, err := strconv.Atoi(title); err == nil {
		return ""
	}
	return title
}

func grokWebSearchTitleFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}
