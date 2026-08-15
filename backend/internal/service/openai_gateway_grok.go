package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	grokUpstreamUserAgent        = "sub2api-grok/1.0"
	grokCLIVersion               = xai.CLIClientVersion
	grokDefaultResponsesModel    = "grok-4.5"
	grok45DefaultReasoningEffort = "high"
)

func (s *OpenAIGatewayService) forwardGrokResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	reqStream bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	if account.Type != AccountTypeOAuth && account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("grok account type %s is not supported by Responses forwarding", account.Type)
	}

	upstreamModel := account.GetMappedModel(originalModel)
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = grokDefaultResponsesModel
	}
	ctx = withGrokRequestedModel(ctx, upstreamModel)
	if isGrokImageGenerationModel(upstreamModel) {
		return nil, fmt.Errorf("model %s is an image model and is not available on the Responses endpoint; use /v1/images/generations instead", upstreamModel)
	}
	patchedBody, clientToolMapping, err := patchGrokResponsesBodyWithClientTools(body, upstreamModel)
	if err != nil {
		setOpsUpstreamError(c, http.StatusBadRequest, err.Error(), "")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request_error", "message": err.Error(), "param": "tools",
		}})
		return nil, err
	}
	setGrokResponsesClientToolMapping(c, clientToolMapping)
	if isOpenAIResponsesCompactPath(c) {
		patchedBody, err = buildGrokCompactRequestBody(patchedBody)
		if err != nil {
			return nil, err
		}
	}
	// Resolve against the body xAI will receive so promoted Codex Lite tools
	// participate in the stable cache prefix.
	cacheIdentity := resolveGrokCacheIdentity(c, patchedBody, "", upstreamModel)
	mixedCacheIntentBody := append([]byte(nil), patchedBody...)
	patchedBody, err = s.restoreGrokReasoningItems(ctx, account, patchedBody)
	if err != nil {
		writeGrokReasoningCompatibilityError(c, err)
		return nil, err
	}
	patchedBody, err = sanitizeGrokReasoningNullContent(patchedBody)
	if err != nil {
		return nil, err
	}
	patchedBody, err = applyGrokResponsesCacheIdentity(patchedBody, body, cacheIdentity, account.IsGrokOAuth())
	if err != nil {
		return nil, fmt.Errorf("apply grok prompt cache identity: %w", err)
	}
	patchedBody, err = applyGrokFreeRequestToolCacheRoute(
		c,
		patchedBody,
		mixedCacheIntentBody,
		account,
		cacheIdentity,
	)
	if err != nil {
		return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", err)
	}
	setOpsUpstreamRequestBody(c, patchedBody)

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	upstreamStart := time.Now()
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		upstreamReq, buildErr := s.buildGrokResponsesRequest(upstreamCtx, c, account, patchedBody, token, cacheIdentity)
		if buildErr != nil {
			return nil, buildErr
		}
		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		if err != nil {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
		}
		if attempt > 0 || resp.StatusCode != http.StatusBadRequest {
			break
		}

		respBody := s.readUpstreamErrorBody(resp)
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		if !isGrokInvalidEncryptedContentResponse(resp.StatusCode, respBody) {
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			break
		}
		retryBody, changed, trimErr := trimGrokInvalidEncryptedContentRetryBody(patchedBody)
		if trimErr != nil {
			return nil, fmt.Errorf("prepare Grok invalid encrypted_content retry: %w", trimErr)
		}
		if !changed {
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			break
		}
		patchedBody = retryBody
		setOpsUpstreamRequestBody(c, patchedBody)
		slog.Info("grok_invalid_encrypted_content_retry", "account_id", account.ID, "cache_identity_present", cacheIdentity != "")
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				resp,
				c,
				account,
				false,
				writeResponsesError,
			)
		}
		respBody := s.readUpstreamErrorBody(resp)
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(respBody))
		if upstreamMsg == "" {
			upstreamMsg = fmt.Sprintf("xAI upstream returned status %d", resp.StatusCode)
		}
		kind := "http_error"
		if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
			kind = "failover"
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
			Kind:               kind,
			Message:            upstreamMsg,
		})
		s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleErrorResponse(ctx, resp, c, account, patchedBody, upstreamModel)
	}

	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)

	var usage *OpenAIUsage
	var firstTokenMs *int
	responseID := ""
	searchCount := 0
	if reqStream {
		maxLineSize := defaultMaxLineSize
		if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
			maxLineSize = s.cfg.Gateway.MaxLineSize
		}
		resp.Body = newGrokResponsesBillingPingFilterBody(resp.Body, account, maxLineSize)
		if hasGrokResponsesClientToolMapping(clientToolMapping) {
			resp.Body = newGrokResponsesClientToolStreamBody(resp.Body, clientToolMapping, maxLineSize)
		}
		streamResult, err := s.handleStreamingResponse(ctx, resp, c, account, startTime, originalModel, upstreamModel)
		if err != nil {
			return nil, err
		}
		usage = streamResult.usage
		firstTokenMs = streamResult.firstTokenMs
		responseID = strings.TrimSpace(streamResult.responseID)
		searchCount = streamResult.searchCount
	} else {
		nonStreamResult, err := s.handleNonStreamingResponse(ctx, resp, c, account, originalModel, upstreamModel)
		if err != nil {
			return nil, err
		}
		usage = nonStreamResult.usage
		responseID = strings.TrimSpace(nonStreamResult.responseID)
		searchCount = nonStreamResult.searchCount
	}

	if usage == nil {
		usage = &OpenAIUsage{}
	}
	return &OpenAIForwardResult{
		RequestID:       firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
		ResponseID:      responseID,
		Usage:           *usage,
		Model:           originalModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: extractOpenAIReasoningEffortFromBody(patchedBody, upstreamModel, originalModel),
		Stream:          reqStream,
		OpenAIWSMode:    false,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(startTime),
		FirstTokenMs:    firstTokenMs,
		SearchCount:     searchCount,
	}, nil
}

func isGrokInvalidEncryptedContentResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	code := gjson.GetBytes(body, "code")
	message := gjson.GetBytes(body, "error")
	if code.Type != gjson.String || message.Type != gjson.String ||
		!strings.EqualFold(strings.TrimSpace(code.String()), "invalid-argument") {
		return false
	}
	normalizedMessage := strings.ToLower(message.String())
	return strings.Contains(normalizedMessage, "decrypt") &&
		strings.Contains(normalizedMessage, "encrypted_content")
}

func trimGrokInvalidEncryptedContentRetryBody(body []byte) ([]byte, bool, error) {
	input := gjson.GetBytes(body, "input")
	items := input.Array()
	if input.IsObject() {
		items = []gjson.Result{input}
	}
	hasEncryptedReasoning := false
	for _, item := range items {
		if strings.TrimSpace(item.Get("type").String()) == "reasoning" &&
			item.Get("encrypted_content").Exists() {
			hasEncryptedReasoning = true
			break
		}
	}
	if !hasEncryptedReasoning {
		return body, false, nil
	}

	var requestBody map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&requestBody); err != nil {
		return nil, false, err
	}
	if !trimOpenAIEncryptedReasoningItems(requestBody) {
		return body, false, nil
	}
	retryBody, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return nil, false, err
	}
	return retryBody, true, nil
}

func (s *OpenAIGatewayService) forwardGrokChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	billingModel string,
	upstreamModel string,
	clientStream bool,
	includeUsage bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	if account.Type != AccountTypeOAuth && account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("grok account type %s is not supported by chat completions forwarding", account.Type)
	}

	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = account.GetMappedModel(originalModel)
	}
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = grokDefaultResponsesModel
	}
	ctx = withGrokRequestedModel(ctx, upstreamModel)

	patchedBody, err := patchGrokResponsesBody(body, upstreamModel)
	if err != nil {
		return nil, err
	}
	cacheIdentity := resolveGrokCacheIdentity(c, patchedBody, "", upstreamModel)
	mixedCacheIntentBody := append([]byte(nil), patchedBody...)
	patchedBody, err = applyGrokResponsesCacheIdentity(patchedBody, body, cacheIdentity, account.IsGrokOAuth())
	if err != nil {
		return nil, fmt.Errorf("apply grok prompt cache identity: %w", err)
	}
	patchedBody, err = applyGrokFreeRequestToolCacheRoute(
		c,
		patchedBody,
		mixedCacheIntentBody,
		account,
		cacheIdentity,
	)
	if err != nil {
		return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", err)
	}
	setOpsUpstreamRequestBody(c, patchedBody)

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := s.buildGrokResponsesRequest(upstreamCtx, c, account, patchedBody, token, cacheIdentity)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	SetActualOpenAIUpstreamEndpoint(c, grokChatResponsesEndpoint)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				resp,
				c,
				account,
				false,
				writeChatCompletionsError,
			)
		}
		respBody := s.readUpstreamErrorBody(resp)
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(respBody))
		if upstreamMsg == "" {
			upstreamMsg = fmt.Sprintf("xAI upstream returned status %d", resp.StatusCode)
		}
		kind := "http_error"
		if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
			kind = "failover"
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
			Kind:               kind,
			Message:            upstreamMsg,
		})
		s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		if s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody) {
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleChatCompletionsErrorResponse(resp, c, account, originalModel)
	}

	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	configuredIdleSeconds := 0
	if s.cfg != nil {
		configuredIdleSeconds = s.cfg.Gateway.StreamDataIntervalTimeout
	}
	streamIdle := resolveGrokStreamIdleTimeout(configuredIdleSeconds)
	resp.Body = newGrokStreamIdleReadCloser(resp.Body, streamIdle, func() {
		s.tempUnscheduleGrok(ctx, account, grokStreamIdleCooldown, "grok stream idle timeout")
	})
	resp.Body = newGrokResponsesBillingPingFilterBody(resp.Body, account, maxLineSize)
	searchCounter := newGrokSearchCountingReadCloser(resp.Body)
	resp.Body = searchCounter

	var result *OpenAIForwardResult
	var handleErr error
	if clientStream {
		result, handleErr = s.handleChatStreamingResponse(ctx, resp, c, account, originalModel, billingModel, upstreamModel, includeUsage, startTime)
	} else {
		result, handleErr = s.handleChatBufferedStreamingResponse(ctx, resp, c, account, originalModel, billingModel, upstreamModel, startTime)
	}
	if handleErr == nil && result != nil {
		result.UpstreamEndpoint = grokChatResponsesEndpoint
		result.ServiceTier = extractOpenAIServiceTierFromBody(patchedBody)
		result.ReasoningEffort = extractOpenAIReasoningEffortFromBody(patchedBody, originalModel)
		result.SearchCount = searchCounter.Count()
	}
	return result, handleErr
}

func patchGrokResponsesBody(body []byte, upstreamModel string) ([]byte, error) {
	return patchGrokResponsesBodyBase(body, upstreamModel)
}

func patchGrokResponsesBodyWithClientTools(body []byte, upstreamModel string) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	if !json.Valid(body) {
		return nil, apicompat.ResponsesClientToolMapping{}, fmt.Errorf("invalid json request body")
	}
	promoted, err := sanitizeGrokResponsesInput(body)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, err
	}
	adapted, mapping, err := adaptGrokResponsesClientTools(promoted)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, err
	}
	patched, err := patchGrokResponsesBodyBase(adapted, upstreamModel)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, err
	}
	return patched, mapping, nil
}

func patchGrokResponsesBodyBase(body []byte, upstreamModel string) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid json request body")
	}
	out, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(upstreamModel, "grok-4.5") {
		for _, unsupportedField := range []string{"presence_penalty", "presencePenalty", "frequency_penalty", "frequencyPenalty", "stop"} {
			if !gjson.GetBytes(out, unsupportedField).Exists() {
				continue
			}
			out, err = sjson.DeleteBytes(out, unsupportedField)
			if err != nil {
				return nil, err
			}
		}
	}
	out, err = sanitizeGrokResponsesModelCapabilities(out, upstreamModel)
	if err != nil {
		return nil, err
	}
	for _, unsupportedField := range []string{"prompt_cache_retention", "safety_identifier"} {
		if gjson.GetBytes(out, unsupportedField).Exists() {
			out, err = sjson.DeleteBytes(out, unsupportedField)
			if err != nil {
				return nil, err
			}
		}
	}
	out, err = sanitizeGrokResponsesUnsupportedFields(out)
	if err != nil {
		return nil, err
	}
	out, err = convertOpenAICompactInputsForGrok(out)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokResponsesInput(out)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokReasoningNullContent(out)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokResponsesTools(out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func sanitizeGrokResponsesModelCapabilities(body []byte, upstreamModel string) ([]byte, error) {
	if !grokModelRejectsReasoningEffort(upstreamModel) {
		if isGrok45Model(upstreamModel) {
			return applyGrok45ReasoningEffortPolicy(body)
		}
		// Grok 4.6 accepts the caller's native reasoning configuration. Do not
		// apply the Grok 4.5 default/capping policy to the newer model family.
		if isGrok46Model(upstreamModel) {
			return body, nil
		}
		return body, nil
	}

	out := body
	for _, field := range []string{"reasoning", "reasoning_effort", "reasoningEffort"} {
		if !gjson.GetBytes(out, field).Exists() {
			continue
		}
		var err error
		out, err = sjson.DeleteBytes(out, field)
		if err != nil {
			return nil, fmt.Errorf("remove unsupported Grok Composer %s: %w", field, err)
		}
	}
	return out, nil
}

func applyGrok45ReasoningEffortPolicy(body []byte) ([]byte, error) {
	reasoning := gjson.GetBytes(body, "reasoning")
	if reasoning.Exists() && !reasoning.IsObject() {
		// Preserve malformed explicit input so xAI can return its schema error;
		// do not silently replace the client's value with a valid object.
		return body, nil
	}

	for _, path := range []string{"reasoning.effort", "reasoning_effort", "reasoningEffort"} {
		effort := gjson.GetBytes(body, path)
		if !effort.Exists() {
			continue
		}
		if effort.Type != gjson.String {
			return body, nil
		}
		normalized := normalizeGrok45ReasoningEffort(effort.String())
		if normalized == "" {
			return body, nil
		}
		if path == "reasoningEffort" {
			// Preserve the compatibility field while also adding the official
			// Responses shape used by xAI and the usage recorder.
			return sjson.SetBytes(body, "reasoning.effort", normalized)
		}
		return sjson.SetBytes(body, path, normalized)
	}

	return sjson.SetBytes(body, "reasoning.effort", grok45DefaultReasoningEffort)
}

func normalizeGrok45ReasoningEffort(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
	switch value {
	case "low", "medium", "high":
		return value
	case "xhigh", "extrahigh", "max":
		// Grok 4.5 exposes high as its maximum supported effort. Codex clients
		// commonly send xhigh/max, so cap those explicit maxima deterministically.
		return "high"
	default:
		return ""
	}
}

func isGrok45Model(model string) bool {
	switch normalizeGrokModelID(model) {
	case "grok", "grok-latest", "grok-4.5", "grok-4.5-latest":
		return true
	default:
		return false
	}
}

func isGrok46Model(model string) bool {
	switch normalizeGrokModelID(model) {
	case "grok-4.6", "grok-4.6-latest":
		return true
	default:
		return false
	}
}

func grokModelRejectsReasoningEffort(model string) bool {
	switch normalizeGrokModelID(model) {
	case "grok-composer", "grok-composer-2.5-fast", "composer-2.5":
		return true
	default:
		return false
	}
}

func normalizeGrokModelID(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = strings.TrimSpace(model[slash+1:])
	}
	return model
}

// sanitizeGrokReasoningNullContent removes content:null from reasoning input
// items. xAI's untagged enum rejects that field with HTTP 422.
func sanitizeGrokReasoningNullContent(body []byte) ([]byte, error) {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body, nil
	}

	items := input.Array()
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			continue
		}
		content := item.Get("content")
		if !content.Exists() || content.Type != gjson.Null {
			continue
		}
		var err error
		body, err = sjson.DeleteBytes(body, fmt.Sprintf("input.%d.content", index))
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

var grokResponsesUnsupportedRecursiveFields = map[string]struct{}{
	"external_web_access": {},
}

func sanitizeGrokResponsesUnsupportedFields(body []byte) ([]byte, error) {
	if !bytes.Contains(body, []byte(`"external_web_access"`)) {
		return body, nil
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if !deleteJSONFields(payload, grokResponsesUnsupportedRecursiveFields) {
		return body, nil
	}
	return json.Marshal(payload)
}

func deleteJSONFields(value any, fields map[string]struct{}) bool {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for field := range fields {
			if _, ok := typed[field]; ok {
				delete(typed, field)
				changed = true
			}
		}
		for _, child := range typed {
			if deleteJSONFields(child, fields) {
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, child := range typed {
			if deleteJSONFields(child, fields) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

// additional_tools is a Codex/Responses Lite private input carrier. xAI
// rejects the carrier itself but accepts supported tools at the top level.
func sanitizeGrokResponsesInput(body []byte) ([]byte, error) {
	if !bytes.Contains(body, []byte(`"additional_tools"`)) {
		return body, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body, nil
	}

	rawItems := input.Array()
	filtered := make([]json.RawMessage, 0, len(rawItems))
	topLevelTools := gjson.GetBytes(body, "tools")
	mergedTools := make([]json.RawMessage, 0)
	seenTools := make(map[string]struct{})
	appendTool := func(tool gjson.Result) bool {
		key := grokResponsesToolDedupKey(tool)
		if _, exists := seenTools[key]; exists {
			return false
		}
		seenTools[key] = struct{}{}
		mergedTools = append(mergedTools, json.RawMessage(tool.Raw))
		return true
	}
	if topLevelTools.IsArray() {
		for _, tool := range topLevelTools.Array() {
			seenTools[grokResponsesToolDedupKey(tool)] = struct{}{}
			mergedTools = append(mergedTools, json.RawMessage(tool.Raw))
		}
	}

	promoted := false
	for _, item := range rawItems {
		if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
			tools := item.Get("tools")
			if tools.IsArray() {
				for _, tool := range tools.Array() {
					if appendTool(tool) {
						promoted = true
					}
				}
			}
			continue
		}
		filtered = append(filtered, json.RawMessage(item.Raw))
	}
	if len(filtered) == len(rawItems) {
		return body, nil
	}

	encodedInput, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	body, err = sjson.SetRawBytes(body, "input", encodedInput)
	if err != nil || !promoted {
		return body, err
	}
	encodedTools, err := json.Marshal(mergedTools)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(body, "tools", encodedTools)
}

func grokResponsesToolDedupKey(tool gjson.Result) string {
	toolType := strings.TrimSpace(tool.Get("type").String())
	if toolType != "" {
		if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
			return "type:" + toolType + "\x00name:" + name
		}
		if toolType == "mcp" {
			if label := strings.TrimSpace(tool.Get("server_label").String()); label != "" {
				return "type:mcp\x00server_label:" + label
			}
		}
	}
	return "json:" + normalizeCompatSeedJSON(json.RawMessage(tool.Raw))
}

var grokResponsesSupportedToolTypes = map[string]struct{}{
	"code_execution":     {},
	"code_interpreter":   {},
	"collections_search": {},
	"file_search":        {},
	"function":           {},
	"mcp":                {},
	"shell":              {},
	"web_search":         {},
	"x_search":           {},
}

func sanitizeGrokResponsesTools(body []byte) ([]byte, error) {
	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() {
		if gjson.GetBytes(body, "tool_choice").Exists() {
			return sjson.DeleteBytes(body, "tool_choice")
		}
		return body, nil
	}
	if !tools.IsArray() {
		return body, nil
	}

	rawTools := tools.Array()
	filteredTools := make([]json.RawMessage, 0, len(rawTools))
	for _, tool := range rawTools {
		toolType := strings.TrimSpace(tool.Get("type").String())
		if _, ok := grokResponsesSupportedToolTypes[toolType]; ok {
			filteredTools = append(filteredTools, json.RawMessage(tool.Raw))
		}
	}

	var err error
	if len(filteredTools) != len(rawTools) {
		if len(filteredTools) == 0 {
			body, err = sjson.DeleteBytes(body, "tools")
		} else {
			var encoded []byte
			encoded, err = json.Marshal(filteredTools)
			if err != nil {
				return nil, err
			}
			body, err = sjson.SetRawBytes(body, "tools", encoded)
		}
		if err != nil {
			return nil, err
		}
	}

	toolChoice := gjson.GetBytes(body, "tool_choice")
	if !toolChoice.Exists() {
		return body, nil
	}
	if shouldDropGrokToolChoice(toolChoice, filteredTools) {
		body, err = sjson.DeleteBytes(body, "tool_choice")
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

func shouldDropGrokToolChoice(toolChoice gjson.Result, tools []json.RawMessage) bool {
	if len(tools) == 0 {
		return true
	}
	if !toolChoice.IsObject() {
		return false
	}
	choiceType := strings.TrimSpace(toolChoice.Get("type").String())
	if choiceType == "" {
		return false
	}
	if _, ok := grokResponsesSupportedToolTypes[choiceType]; !ok {
		return true
	}
	if choiceType == "function" {
		choiceName := strings.TrimSpace(toolChoice.Get("name").String())
		if choiceName == "" {
			choiceName = strings.TrimSpace(toolChoice.Get("function.name").String())
		}
		if choiceName == "" {
			return false
		}
		for _, tool := range tools {
			var item struct {
				Type     string `json:"type"`
				Name     string `json:"name"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(tool, &item); err != nil {
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = strings.TrimSpace(item.Function.Name)
			}
			if strings.TrimSpace(item.Type) == "function" && name == choiceName {
				return false
			}
		}
		return true
	}
	return false
}

func (s *OpenAIGatewayService) buildGrokResponsesRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, token, cacheIdentity string) (*http.Request, error) {
	targetURL, err := buildGrokResponsesURL(ctx, account, s.cfg, s.settingService)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(ctx),
		http.MethodPost,
		targetURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if account.IsGrokOAuth() {
		applyGrokCLIHeaders(req.Header)
	}
	applyGrokCacheHeaders(req.Header, cacheIdentity)
	if c != nil {
		if v := c.GetHeader("OpenAI-Beta"); strings.TrimSpace(v) != "" {
			req.Header.Set("OpenAI-Beta", v)
		}
	}
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

// applyGrokCLIHeaders identifies subscription traffic as a supported Grok CLI
// version. The CLI gateway rejects otherwise valid OAuth requests without it.
func applyGrokCLIHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	version := xai.ResolveCLIVersion()
	headers.Set("User-Agent", xai.CLIUserAgentForVersion(version))
	headers.Set("X-Grok-Client-Version", version)
	headers.Set("x-grok-client-version", version)
	headers.Set("x-grok-client-identifier", xai.CLIClientIdentifier)
	headers.Set("X-Grok-Client-Mode", "interactive")
}

func (s *OpenAIGatewayService) updateGrokUsageSnapshot(ctx context.Context, account *Account, snapshot *xai.QuotaSnapshot) {
	if s == nil || account == nil || account.ID <= 0 || snapshot == nil {
		return
	}
	accountID := account.ID
	now := time.Now()
	resetAt, hasActiveLimit := grokRateLimitResetAtForAccount(account, snapshot, now)
	if hasActiveLimit {
		normalizeGrokExhaustedWindowResets(snapshot, resetAt, now)
	}
	recovery := isSuccessfulGrokRateLimitRecovery(account, snapshot)
	critical := snapshot.StatusCode == http.StatusTooManyRequests || hasActiveLimit || recovery
	if s.codexSnapshotThrottle != nil {
		allowed := s.codexSnapshotThrottle.Allow(accountID, now)
		if !critical && !allowed {
			return
		}
	}

	stateCtx := ctx
	if hasActiveLimit {
		var cancel context.CancelFunc
		stateCtx, cancel = openAIAccountStateContext(ctx)
		defer cancel()
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[grokQuotaSnapshotExtraKey] = snapshot
	if s.accountRepo != nil {
		_ = s.accountRepo.UpdateExtra(stateCtx, accountID, map[string]any{
			grokQuotaSnapshotExtraKey: snapshot,
		})
	}
	// Pool-mode API keys retain the neutral snapshot for observability, while
	// their upstream pool remains authoritative for credential health.
	if hasActiveLimit && !account.IsPoolMode() {
		s.rateLimitGrok(stateCtx, account, resetAt)
	} else if recovery {
		clearGrokRateLimitAfterRecovery(stateCtx, s.accountRepo, account)
	}
}

func (s *OpenAIGatewayService) updateGrokUsageFromResponse(ctx context.Context, account *Account, headers http.Header, statusCode int) {
	snapshot := xai.ParseQuotaHeaders(headers, statusCode)
	if snapshot == nil && statusCode == http.StatusTooManyRequests {
		snapshot = &xai.QuotaSnapshot{
			StatusCode: statusCode,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		}
	}
	if snapshot != nil {
		s.updateGrokUsageSnapshot(ctx, account, snapshot)
		return
	}
	// A successful response without optional xAI quota headers is still valid
	// recovery evidence. CAS-clearing prevents this old observation from
	// deleting a newer cooldown written concurrently.
	recoverySnapshot := &xai.QuotaSnapshot{StatusCode: statusCode}
	if isSuccessfulGrokRateLimitRecovery(account, recoverySnapshot) {
		clearGrokRateLimitAfterRecovery(ctx, s.accountRepo, account)
	}
}

func (s *OpenAIGatewayService) rateLimitGrok(ctx context.Context, account *Account, resetAt time.Time) {
	if s == nil || account == nil {
		return
	}
	now := time.Now()
	resetAt = normalizeGrokRateLimitResetAt(account, resetAt, now)
	runtimeUntil := resetAt
	if account.TempUnschedulableUntil != nil && account.TempUnschedulableUntil.After(runtimeUntil) {
		runtimeUntil = *account.TempUnschedulableUntil
	}
	s.BlockAccountScheduling(account, runtimeUntil, "429")
	if model := grokRequestedModelFromContext(ctx); model != "" {
		markGrokTeamModelRateLimit(account, model, resolveGrokTeamRateLimitUntil(resetAt, now))
	}
	persistGrokRateLimit(ctx, s.accountRepo, account, resetAt)
}

func (s *OpenAIGatewayService) handleGrokAccountUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte) {
	if s == nil || account == nil {
		return
	}
	if isGrokContentPolicyRejection(statusCode, responseBody) {
		return
	}
	s.updateGrokUsageFromResponse(ctx, account, headers, statusCode)
	decision := classifyGrokUpstreamFailure(statusCode, responseBody, grokRequestedModelFromContext(ctx))
	if decision.ShouldCooldown && decision.Class != GrokFailureNone && decision.Class != GrokFailureRateLimit && !account.IsPoolMode() {
		if decision.Class == GrokFailureFreeUsage {
			now := time.Now()
			if snapshot := xai.ParseQuotaHeaders(headers, statusCode); snapshot != nil {
				if resetAt, limited := grokRateLimitResetAtForAccount(account, snapshot, now); limited && resetAt.After(now) {
					if decision.Model != "" && isGrokModelSpecificFreeUsage(strings.ToLower(decision.Reason), decision.Model) {
						markGrokModelQuotaBlock(account.ID, decision.Model, resetAt)
						return
					}
					s.rateLimitGrok(ctx, account, resetAt)
					return
				}
			}
		}
		if s.applyGrokUpstreamFailureDecision(ctx, account, decision) {
			return
		}
	}
	if statusCode == http.StatusForbidden && s.applyGrokForbiddenPolicy(ctx, account, responseBody) {
		return
	}
	if account.IsPoolMode() {
		slog.Info("grok_pool_mode_error_state_skipped", "account_id", account.ID, "status_code", statusCode)
		return
	}
	switch statusCode {
	case http.StatusUnauthorized:
		s.tempUnscheduleGrokCredentialIfMatch(ctx, account, 10*time.Minute, "grok oauth token unauthorized")
	case http.StatusPaymentRequired:
		updated, err := setGrokPaymentRequiredErrorIfMatch(ctx, s.accountRepo, account)
		if err != nil {
			slog.Error("grok_payment_required_account_error_update_failed", "account_id", account.ID, "error", err)
			return
		}
		if updated {
			s.BlockAccountScheduling(account, time.Time{}, grokPaymentRequiredErrorMessage)
		}
	case http.StatusForbidden:
		if isGrokSpendingLimitError(responseBody) {
			s.rateLimitGrok(ctx, account, grokSpendingLimitResetAt(account, time.Now()))
			return
		}
		s.tempUnscheduleGrokCredentialIfMatch(ctx, account, 30*time.Minute, "grok entitlement or subscription tier denied")
	case http.StatusTooManyRequests:
		// updateGrokUsageFromResponse installs the durable and runtime limit.
	default:
		if statusCode >= 500 {
			s.tempUnscheduleGrok(ctx, account, 2*time.Minute, "grok upstream temporary error")
		}
	}
	_ = responseBody
}

func (s *OpenAIGatewayService) tempUnscheduleGrokCredentialIfMatch(ctx context.Context, account *Account, cooldown time.Duration, reason string) {
	if s == nil || account == nil {
		return
	}
	if !account.IsGrokOAuth() {
		s.tempUnscheduleGrok(ctx, account, cooldown, reason)
		return
	}
	repo, ok := s.accountRepo.(grokCredentialConditionalStateRepository)
	if !ok {
		slog.Error("grok_credential_temp_unsched_repository_unavailable", "account_id", account.ID, "reason", reason)
		return
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	until := time.Now().Add(cooldown)
	updated, err := repo.SetGrokCredentialTempUnschedulableIfMatch(
		stateCtx,
		account.ID,
		grokCredentialMutationSnapshot(account),
		until,
		reason,
	)
	if err != nil {
		slog.Warn("grok_credential_temp_unsched_failed", "account_id", account.ID, "reason", reason, "error", err)
		return
	}
	if updated {
		s.BlockAccountScheduling(account, until, reason)
	}
}

func (s *OpenAIGatewayService) tempUnscheduleGrok(ctx context.Context, account *Account, cooldown time.Duration, reason string) {
	if s == nil || account == nil {
		return
	}
	until := time.Now().Add(cooldown)
	if account.TempUnschedulableUntil != nil && account.TempUnschedulableUntil.After(until) {
		until = *account.TempUnschedulableUntil
	}
	s.BlockAccountScheduling(account, until, reason)
	if s.accountRepo != nil {
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		_ = s.accountRepo.SetTempUnschedulable(stateCtx, account.ID, until, reason)
	}
}
