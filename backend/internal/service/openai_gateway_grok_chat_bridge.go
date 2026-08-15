package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	grokChatResponsesEndpoint = "/v1/responses"
	grokChatRawEndpoint       = "/v1/chat/completions"
	grokMissingUsageErrorCode = "grok_missing_usage"
	grokMissingUsageMessage   = "xAI upstream returned a successful chat completion without billable usage"
)

var grokChatResponsesBridgeTopLevelFields = map[string]struct{}{
	"model":                 {},
	"messages":              {},
	"instructions":          {},
	"stream":                {},
	"stream_options":        {},
	"max_tokens":            {},
	"max_completion_tokens": {},
	"temperature":           {},
	"top_p":                 {},
	"stop":                  {},
	"reasoning_effort":      {},
	"prompt_cache_key":      {},
	"tools":                 {},
	"tool_choice":           {},
	"functions":             {},
	"function_call":         {},
	"parallel_tool_calls":   {},
	"response_format":       {},
	"service_tier":          {},
}

// grokChatResponsesBridgeEligibility accepts only Chat Completions request
// shapes whose semantics survive the existing Chat-to-Responses converter.
// Everything else must use native Grok Chat rather than losing fields.
func grokChatResponsesBridgeEligibility(body []byte) (bool, string) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return false, "invalid_json"
	}

	for _, field := range []string{"stop", "reasoning_effort"} {
		if raw, exists := root[field]; exists && !grokChatJSONNull(raw) {
			return false, "unsupported_" + field
		}
	}
	if raw, exists := root["instructions"]; exists {
		var instructions string
		if !grokChatJSONNull(raw) && json.Unmarshal(raw, &instructions) != nil {
			return false, "invalid_instructions"
		}
	}
	if raw, exists := root["response_format"]; exists {
		var responseFormat map[string]json.RawMessage
		if !grokChatJSONNull(raw) && (json.Unmarshal(raw, &responseFormat) != nil || responseFormat == nil) {
			return false, "invalid_response_format"
		}
	}
	if raw, exists := root["service_tier"]; exists {
		var serviceTier string
		if !grokChatJSONNull(raw) && json.Unmarshal(raw, &serviceTier) != nil {
			return false, "invalid_service_tier"
		}
	}
	if raw, exists := root["tools"]; exists {
		if ok, reason := grokChatFunctionDeclarationsBridgeable(raw); !ok {
			return false, reason
		}
	}
	if raw, exists := root["functions"]; exists && !grokChatNullOrEmptyArray(raw) {
		return false, "unsupported_functions"
	}
	if raw, exists := root["tool_choice"]; exists {
		if ok, reason := grokChatToolChoiceBridgeable(raw); !ok {
			return false, reason
		}
		var choice string
		if json.Unmarshal(raw, &choice) == nil && choice == "required" && !grokChatHasFunctionDeclarations(root) {
			return false, "required_tool_choice_without_tools"
		}
	}
	if raw, exists := root["function_call"]; exists && !grokChatNullOrNone(raw) {
		return false, "unsupported_function_call"
	}
	for field := range root {
		if _, supported := grokChatResponsesBridgeTopLevelFields[field]; !supported {
			return false, "unknown_field_" + field
		}
	}

	var model string
	if raw, ok := root["model"]; !ok || json.Unmarshal(raw, &model) != nil || strings.TrimSpace(model) == "" {
		return false, "invalid_model"
	}
	if raw, ok := root["stream"]; ok {
		var stream *bool
		if json.Unmarshal(raw, &stream) != nil || stream == nil {
			return false, "invalid_stream"
		}
	}
	if raw, ok := root["parallel_tool_calls"]; ok {
		var parallelToolCalls *bool
		if json.Unmarshal(raw, &parallelToolCalls) != nil || parallelToolCalls == nil {
			return false, "invalid_parallel_tool_calls"
		}
	}
	if raw, ok := root["stream_options"]; ok {
		var options map[string]json.RawMessage
		if json.Unmarshal(raw, &options) != nil || options == nil {
			return false, "invalid_stream_options"
		}
		for field, value := range options {
			if field != "include_usage" {
				return false, "unknown_stream_option_" + field
			}
			var includeUsage *bool
			if json.Unmarshal(value, &includeUsage) != nil || includeUsage == nil {
				return false, "invalid_stream_include_usage"
			}
		}
	}
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		if raw, ok := root[field]; ok {
			var value *int
			if json.Unmarshal(raw, &value) != nil || value == nil || *value < 128 {
				return false, "unsafe_" + field
			}
		}
	}
	if _, hasMaxTokens := root["max_tokens"]; hasMaxTokens {
		if _, hasMaxCompletionTokens := root["max_completion_tokens"]; hasMaxCompletionTokens {
			return false, "conflicting_max_tokens"
		}
	}
	for _, field := range []string{"temperature", "top_p"} {
		if raw, ok := root[field]; ok {
			var value *float64
			if json.Unmarshal(raw, &value) != nil || value == nil {
				return false, "invalid_" + field
			}
		}
	}
	if raw, ok := root["prompt_cache_key"]; ok {
		var key string
		if json.Unmarshal(raw, &key) != nil {
			return false, "invalid_prompt_cache_key"
		}
	}

	var messages []map[string]json.RawMessage
	rawMessages, ok := root["messages"]
	if !ok || json.Unmarshal(rawMessages, &messages) != nil || len(messages) == 0 {
		return false, "invalid_messages"
	}
	for _, message := range messages {
		var role string
		if raw, exists := message["role"]; !exists || json.Unmarshal(raw, &role) != nil {
			return false, "invalid_message_role"
		}
		switch role {
		case "system", "user":
			if ok, reason := grokChatMessageFieldsBridgeable(message, "role", "content"); !ok {
				return false, reason
			}
			raw, exists := message["content"]
			if !exists {
				return false, "non_text_message_content"
			}
			if ok, reason := grokChatRequiredMessageContentBridgeable(raw); !ok {
				return false, reason
			}
		case "assistant":
			if ok, reason := grokChatMessageFieldsBridgeable(message, "role", "content", "reasoning_content", "tool_calls"); !ok {
				return false, reason
			}
			reasoningContent := ""
			if raw, exists := message["reasoning_content"]; exists {
				if !grokChatJSONNull(raw) && json.Unmarshal(raw, &reasoningContent) != nil {
					return false, "invalid_reasoning_content"
				}
			}
			hasReasoningContent := strings.TrimSpace(reasoningContent) != ""
			toolCallCount := 0
			if raw, exists := message["tool_calls"]; exists {
				var reason string
				toolCallCount, reason = grokChatAssistantToolCallsBridgeable(raw)
				if reason != "" {
					return false, reason
				}
			}
			raw, hasContent := message["content"]
			if !hasContent || grokChatJSONNull(raw) {
				if toolCallCount == 0 && !hasReasoningContent {
					return false, "non_text_message_content"
				}
				continue
			}
			var content string
			if json.Unmarshal(raw, &content) == nil {
				if strings.TrimSpace(content) == "" && toolCallCount == 0 && !hasReasoningContent {
					return false, "empty_message_content"
				}
				continue
			}
			if ok, reason := grokChatStructuredContentBridgeable(raw); !ok {
				if !hasReasoningContent || reason != "empty_message_content" {
					return false, reason
				}
			}
		case "tool":
			if ok, reason := grokChatMessageFieldsBridgeable(message, "role", "content", "tool_call_id"); !ok {
				return false, reason
			}
			var callID string
			if raw, exists := message["tool_call_id"]; !exists || json.Unmarshal(raw, &callID) != nil || strings.TrimSpace(callID) == "" {
				return false, "invalid_tool_call_id"
			}
			var output string
			if raw, exists := message["content"]; !exists || json.Unmarshal(raw, &output) != nil || output == "" {
				return false, "invalid_tool_message_content"
			}
		default:
			return false, "unsupported_message_role_" + role
		}
	}
	return true, ""
}

func grokChatFunctionDeclarationsBridgeable(raw json.RawMessage) (bool, string) {
	if grokChatJSONNull(raw) {
		return true, ""
	}
	var declarations []json.RawMessage
	if json.Unmarshal(raw, &declarations) != nil {
		return false, "invalid_tools"
	}
	for _, declaration := range declarations {
		var tool map[string]json.RawMessage
		if json.Unmarshal(declaration, &tool) != nil || tool == nil {
			return false, "invalid_tool"
		}
		for field := range tool {
			if field != "type" && field != "function" {
				return false, "unsafe_tool_field_" + field
			}
		}
		var toolType string
		if rawType, exists := tool["type"]; !exists || json.Unmarshal(rawType, &toolType) != nil || toolType != "function" {
			return false, "unsupported_tool_type"
		}
		functionRaw, exists := tool["function"]
		if !exists {
			return false, "invalid_tool_function"
		}
		var function map[string]json.RawMessage
		if json.Unmarshal(functionRaw, &function) != nil || function == nil {
			return false, "invalid_tool_function"
		}
		for field := range function {
			switch field {
			case "name", "description", "parameters", "strict":
			default:
				return false, "unsafe_tool_function_field_" + field
			}
		}
		var name string
		if rawName, exists := function["name"]; !exists || json.Unmarshal(rawName, &name) != nil || strings.TrimSpace(name) == "" {
			return false, "invalid_tool_function_name"
		}
		if rawDescription, exists := function["description"]; exists {
			var description string
			if json.Unmarshal(rawDescription, &description) != nil {
				return false, "invalid_tool_function_description"
			}
		}
		var parameters map[string]json.RawMessage
		if rawParameters, exists := function["parameters"]; !exists || json.Unmarshal(rawParameters, &parameters) != nil || parameters == nil {
			return false, "invalid_tool_function_parameters"
		}
		if rawStrict, exists := function["strict"]; exists {
			var strict bool
			if json.Unmarshal(rawStrict, &strict) != nil {
				return false, "invalid_tool_function_strict"
			}
		}
	}
	return true, ""
}

func grokChatToolChoiceBridgeable(raw json.RawMessage) (bool, string) {
	if grokChatJSONNull(raw) {
		return true, ""
	}
	var choice string
	if json.Unmarshal(raw, &choice) != nil {
		return false, "unsupported_tool_choice"
	}
	switch choice {
	case "auto", "none", "required":
		return true, ""
	default:
		return false, "unsupported_tool_choice"
	}
}

func grokChatHasFunctionDeclarations(root map[string]json.RawMessage) bool {
	for _, field := range []string{"tools", "functions"} {
		raw, exists := root[field]
		if !exists {
			continue
		}
		var declarations []json.RawMessage
		if json.Unmarshal(raw, &declarations) == nil && len(declarations) > 0 {
			return true
		}
	}
	return false
}

func grokChatMessageFieldsBridgeable(message map[string]json.RawMessage, allowedFields ...string) (bool, string) {
	allowed := make(map[string]struct{}, len(allowedFields))
	for _, field := range allowedFields {
		allowed[field] = struct{}{}
	}
	for field := range message {
		if _, ok := allowed[field]; !ok {
			return false, "unsafe_message_field_" + field
		}
	}
	return true, ""
}

func grokChatRequiredMessageContentBridgeable(raw json.RawMessage) (bool, string) {
	var content string
	if json.Unmarshal(raw, &content) == nil {
		if strings.TrimSpace(content) == "" {
			return false, "empty_message_content"
		}
		return true, ""
	}
	return grokChatStructuredContentBridgeable(raw)
}

func grokChatAssistantToolCallsBridgeable(raw json.RawMessage) (int, string) {
	if grokChatJSONNull(raw) {
		return 0, ""
	}
	var calls []map[string]json.RawMessage
	if json.Unmarshal(raw, &calls) != nil {
		return 0, "invalid_tool_calls"
	}
	for _, call := range calls {
		if call == nil {
			return 0, "invalid_tool_call"
		}
		for field := range call {
			switch field {
			case "id", "type", "function", "index":
			default:
				return 0, "unsafe_tool_call_field_" + field
			}
		}
		if rawIndex, exists := call["index"]; exists {
			var index *int
			if json.Unmarshal(rawIndex, &index) != nil || (index != nil && *index < 0) {
				return 0, "invalid_tool_call_index"
			}
		}
		var callID string
		if rawID, exists := call["id"]; !exists || json.Unmarshal(rawID, &callID) != nil || strings.TrimSpace(callID) == "" {
			return 0, "invalid_tool_call_id"
		}
		var callType string
		if rawType, exists := call["type"]; !exists || json.Unmarshal(rawType, &callType) != nil || callType != "function" {
			return 0, "unsupported_tool_call_type"
		}
		var function map[string]json.RawMessage
		if rawFunction, exists := call["function"]; !exists || json.Unmarshal(rawFunction, &function) != nil || function == nil {
			return 0, "invalid_tool_call_function"
		}
		for field := range function {
			if field != "name" && field != "arguments" {
				return 0, "unsafe_tool_call_function_field_" + field
			}
		}
		var name string
		if rawName, exists := function["name"]; !exists || json.Unmarshal(rawName, &name) != nil || strings.TrimSpace(name) == "" {
			return 0, "invalid_tool_call_function_name"
		}
		var arguments string
		if rawArguments, exists := function["arguments"]; !exists || json.Unmarshal(rawArguments, &arguments) != nil || !json.Valid([]byte(arguments)) {
			return 0, "invalid_tool_call_arguments"
		}
	}
	return len(calls), ""
}

func grokChatStructuredContentBridgeable(raw json.RawMessage) (bool, string) {
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return false, "non_text_message_content"
	}
	if len(parts) == 0 {
		return false, "empty_message_content"
	}
	hasContent := false
	for _, part := range parts {
		var partType string
		rawType, ok := part["type"]
		if !ok || json.Unmarshal(rawType, &partType) != nil {
			return false, "non_text_message_content"
		}
		switch strings.TrimSpace(partType) {
		case "text":
			var text string
			if rawText, exists := part["text"]; exists && json.Unmarshal(rawText, &text) == nil && strings.TrimSpace(text) != "" {
				hasContent = true
			}
		case "image_url", "input_image":
			hasContent = true
		default:
			return false, "unsupported_content_part_" + strings.TrimSpace(partType)
		}
	}
	if !hasContent {
		return false, "empty_message_content"
	}
	return true, ""
}

func grokChatNullOrNone(raw json.RawMessage) bool {
	if grokChatJSONNull(raw) {
		return true
	}
	var value string
	return json.Unmarshal(raw, &value) == nil && strings.EqualFold(strings.TrimSpace(value), "none")
}

func grokChatJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func grokChatNullOrEmptyArray(raw json.RawMessage) bool {
	if grokChatJSONNull(raw) {
		return true
	}
	var values []json.RawMessage
	return json.Unmarshal(raw, &values) == nil && len(values) == 0
}

// hasBillableGrokChatUsage stays aligned with the aggregate token buckets used
// to settle Chat Completions usage. Detail-only fields do not make a response
// safe to bill.
func hasBillableGrokChatUsage(usage OpenAIUsage) bool {
	return usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 ||
		usage.CacheReadInputTokens > 0
}

// requiresBillableGrokChatUsage recognizes Grok by both account platform and
// model identity so the integrity gate remains correct for compatible account
// types that may expose Grok model names.
func requiresBillableGrokChatUsage(account *Account, models ...string) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	for _, model := range models {
		normalized := strings.ToLower(strings.TrimSpace(model))
		if separator := strings.LastIndex(normalized, "/"); separator >= 0 {
			normalized = strings.TrimSpace(normalized[separator+1:])
		}
		if normalized == "grok" || strings.HasPrefix(normalized, "grok-") {
			return true
		}
	}
	return false
}

func newGrokMissingUsageFailoverError(c *gin.Context, account *Account, upstreamRequestID string) *UpstreamFailoverError {
	accountID := int64(0)
	accountName := ""
	if account != nil {
		accountID = account.ID
		accountName = account.Name
	}

	setOpsUpstreamError(c, http.StatusBadGateway, grokMissingUsageMessage, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           PlatformGrok,
		AccountID:          accountID,
		AccountName:        accountName,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               "failover",
		Message:            grokMissingUsageMessage,
	})

	body, _ := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "upstream_error",
			"code":    grokMissingUsageErrorCode,
			"message": grokMissingUsageMessage,
		},
	})
	headers := http.Header{}
	if requestID := strings.TrimSpace(upstreamRequestID); requestID != "" {
		headers.Set("x-request-id", requestID)
	}
	return &UpstreamFailoverError{
		StatusCode:      http.StatusBadGateway,
		ResponseBody:    body,
		ResponseHeaders: headers,
	}
}

// grokRawChatNumSourcesUsed reads xAI's authoritative native Chat
// Completions search usage. It intentionally rejects coercible strings,
// fractions and negative values instead of inferring cost from citations.
func grokRawChatNumSourcesUsed(payload []byte) (int, bool) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return 0, false
	}
	value := gjson.GetBytes(payload, "usage.num_sources_used")
	if !value.Exists() || value.Type != gjson.Number {
		return 0, false
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value.Raw), 10, 64)
	if err != nil || parsed < 0 || uint64(parsed) > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(parsed), true
}

// grokRawChatSearchCountingReadCloser observes native Chat Completions SSE
// usage without changing the bytes consumed by the established raw stream
// forwarder. xAI may repeat or progressively update usage, so Count returns
// the greatest valid num_sources_used value observed in the stream.
type grokRawChatSearchCountingReadCloser struct {
	source io.ReadCloser

	mu        sync.Mutex
	pending   []byte
	frameData [][]byte
	count     int
	closed    bool
}

func newGrokRawChatSearchCountingReadCloser(source io.ReadCloser) *grokRawChatSearchCountingReadCloser {
	return &grokRawChatSearchCountingReadCloser{source: source}
}

func (r *grokRawChatSearchCountingReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.source == nil {
		return 0, io.EOF
	}
	n, err := r.source.Read(p)
	r.mu.Lock()
	if n > 0 {
		r.observeLocked(p[:n], false)
	}
	if err == io.EOF {
		r.observeLocked(nil, true)
	}
	r.mu.Unlock()
	return n, err
}

func (r *grokRawChatSearchCountingReadCloser) Close() error {
	if r == nil || r.source == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	return r.source.Close()
}

func (r *grokRawChatSearchCountingReadCloser) Count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func (r *grokRawChatSearchCountingReadCloser) observeLocked(chunk []byte, atEOF bool) {
	if len(chunk) > 0 {
		r.pending = append(r.pending, chunk...)
	}
	for len(r.pending) > 0 {
		advance, line, _ := scanSSELinesPreservingEndings(r.pending, atEOF)
		if advance == 0 {
			return
		}
		r.observeLineLocked(line)
		r.pending = r.pending[advance:]
	}
	if atEOF {
		r.flushFrameLocked()
		r.pending = nil
	}
}

func (r *grokRawChatSearchCountingReadCloser) observeLineLocked(rawLine []byte) {
	line := trimSSELineEnding(rawLine)
	if len(line) == 0 {
		r.flushFrameLocked()
		return
	}
	data, ok := extractOpenAISSEDataLine(string(line))
	if !ok {
		return
	}
	r.frameData = append(r.frameData, []byte(data))
}

func (r *grokRawChatSearchCountingReadCloser) flushFrameLocked() {
	if len(r.frameData) == 0 {
		return
	}
	payload := bytes.Join(r.frameData, []byte("\n"))
	if count, ok := grokRawChatNumSourcesUsed(payload); ok && count > r.count {
		r.count = count
	}
	for index := range r.frameData {
		r.frameData[index] = nil
	}
	r.frameData = r.frameData[:0]
}

// forwardGrokRawChatCompletions preserves the original Chat Completions body
// for Grok accounts. It performs only routing/billing necessities and reuses
// the established raw Chat response translators.
func (s *OpenAIGatewayService) forwardGrokRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if account == nil || !account.IsGrok() {
		return nil, errors.New("grok account is required for raw chat forwarding")
	}
	if !json.Valid(body) {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Request body must be valid JSON")
		return nil, errors.New("invalid Grok Chat Completions JSON body")
	}
	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in Grok Chat Completions request")
	}
	startTime := time.Now()
	clientStream := gjson.GetBytes(body, "stream").Bool()
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	ctx = withGrokRequestedModel(ctx, upstreamModel)
	cacheIdentity := resolveGrokCacheIdentity(c, body, "", upstreamModel)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)

	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	var err error
	upstreamBody, err = s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, err
	}
	serviceTier := extractOpenAIServiceTierFromBody(upstreamBody)
	forwardResult := &OpenAIForwardResult{
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          clientStream,
	}
	ctx = withOpenAIForwardResultBillingState(ctx, c, forwardResult, startTime, openAIResponseImageBillingConfig{})
	if clientStream {
		upstreamBody, err = ensureOpenAIChatStreamUsage(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("enable Grok raw stream usage: %w", err)
		}
	}
	upstreamBody, err = stripGrokChatPromptCacheKey(upstreamBody)
	if err != nil {
		return nil, fmt.Errorf("remove Responses-only Grok prompt cache key: %w", err)
	}
	setOpsUpstreamRequestBody(c, upstreamBody)

	token, tokenKind, err := s.GetRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("account %d returned an empty %s credential", account.ID, tokenKind)
	}
	targetURL, err := buildGrokChatCompletionsURL(ctx, account, s.cfg, s.settingService)
	if err != nil {
		return nil, fmt.Errorf("build Grok Chat Completions URL: %w", err)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		http.MethodPost,
		targetURL,
		bytes.NewReader(upstreamBody),
	)
	if err != nil {
		return nil, fmt.Errorf("build Grok raw Chat request: %w", err)
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Content-Type", "application/json")
	if clientStream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(targetURL) {
		applyGrokCLIHeaders(upstreamReq.Header)
	}
	applyGrokCacheHeaders(upstreamReq.Header, cacheIdentity)
	if c != nil {
		if beta := strings.TrimSpace(c.GetHeader("OpenAI-Beta")); beta != "" {
			upstreamReq.Header.Set("OpenAI-Beta", beta)
		}
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	proxyURL, err := grokAccountProxyURL(account)
	if err != nil {
		return nil, err
	}
	SetActualOpenAIUpstreamEndpoint(c, grokChatRawEndpoint)
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)

	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(resp, c, account, false, writeChatCompletionsError)
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
				ResponseHeaders:        resp.Header.Clone(),
				RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleChatCompletionsErrorResponse(resp, c, account, originalModel)
	}

	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
	searchCount := 0
	var streamSearchCounter *grokRawChatSearchCountingReadCloser
	if !clientStream {
		readCtx, cancelRead := s.detachedNonStreamingReadContext(ctx)
		defer cancelRead()
		respBody, readErr := ReadUpstreamResponseBodyWithContext(readCtx, resp.Body, s.cfg, c, openAITooLargeError)
		if readErr != nil {
			if !errors.Is(readErr, ErrUpstreamResponseBodyTooLarge) {
				writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
			}
			return nil, fmt.Errorf("read Grok raw upstream body: %w", readErr)
		}
		var usage OpenAIUsage
		if parsedUsage, ok := extractOpenAIUsageFromJSONBytes(respBody); ok {
			usage = parsedUsage
		}
		responseModel := strings.TrimSpace(gjson.GetBytes(respBody, "model").String())
		if requiresBillableGrokChatUsage(account, billingModel, upstreamModel, responseModel) && !hasBillableGrokChatUsage(usage) {
			upstreamRequestID := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
			return nil, newGrokMissingUsageFailoverError(c, account, upstreamRequestID)
		}
		if count, ok := grokRawChatNumSourcesUsed(respBody); ok {
			searchCount = count
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	} else {
		streamSearchCounter = newGrokRawChatSearchCountingReadCloser(resp.Body)
		resp.Body = streamSearchCounter
	}
	var result *OpenAIForwardResult
	if clientStream {
		result, err = s.streamRawChatCompletions(ctx, c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	} else {
		result, err = s.bufferRawChatCompletions(ctx, c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	if result != nil {
		if streamSearchCounter != nil {
			searchCount = streamSearchCounter.Count()
		}
		result.SearchCount = searchCount
		if strings.TrimSpace(result.RequestID) == "" {
			result.RequestID = firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
		}
		result.ResponseHeaders = resp.Header.Clone()
		result.UpstreamEndpoint = grokChatRawEndpoint
		if clientStream && err == nil && requiresBillableGrokChatUsage(account, billingModel, upstreamModel, result.UpstreamResponseModel) && !hasBillableGrokChatUsage(result.Usage) {
			upstreamRequestID := firstNonEmpty(result.RequestID, resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
			setOpsUpstreamError(c, http.StatusBadGateway, grokMissingUsageMessage, "")
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           PlatformGrok,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: http.StatusBadGateway,
				UpstreamRequestID:  upstreamRequestID,
				Kind:               "http_error",
				Message:            grokMissingUsageMessage,
			})
			err = fmt.Errorf("%s after streaming response was committed (request_id=%s)", grokMissingUsageMessage, upstreamRequestID)
		}
	}
	return result, err
}
