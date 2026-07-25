package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	grokConversationIDHeader         = "X-Grok-Conv-Id"
	claudeCodeSessionHeader          = "X-Claude-Code-Session-Id"
	grokClientToolCacheOptInHeader   = "X-Sub2API-Grok-Client-Tool-Cache"
	grokFreeCacheNativeToolsJSON     = `[{"type":"web_search"},{"type":"x_search"}]`
	grokFreeCacheDisabledToolChoice  = "none"
	grokClientToolCacheOptInExtraKey = "grok_client_tool_cache_enabled"
	grokFreeRolling24hTokenLimit     = int64(2_000_000)
	grokBillingSnapshotCacheExtraKey = "grok_billing_snapshot"
)

// Claude Code metadata.user_id often ends with _session_<uuid>.
var claudeCodeSessionSuffixPattern = regexp.MustCompile(`_session_([a-f0-9-]+)$`)

// extractClaudeCodeSessionID resolves the Claude Code conversation id from
// headers or Anthropic/OpenAI-compatible payload metadata.
func extractClaudeCodeSessionID(c *gin.Context, body []byte) string {
	if c != nil {
		if seed := strings.TrimSpace(c.GetHeader(claudeCodeSessionHeader)); seed != "" {
			return seed
		}
	}
	return extractClaudeCodeSessionIDFromPayload(body)
}

func extractClaudeCodeSessionIDFromPayload(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	userID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if userID == "" {
		return ""
	}
	if matches := claudeCodeSessionSuffixPattern.FindStringSubmatch(userID); len(matches) >= 2 {
		return matches[1]
	}
	if len(userID) > 0 && userID[0] == '{' {
		if sessionID := strings.TrimSpace(gjson.Get(userID, "session_id").String()); sessionID != "" {
			return sessionID
		}
	}
	return ""
}

// resolveGrokCacheIdentity derives a stable, tenant-isolated xAI prompt-cache
// identity. Raw downstream session identifiers are never exposed upstream.
func resolveGrokCacheIdentity(c *gin.Context, body []byte, explicitKey, upstreamModel string) string {
	apiKeyID := getAPIKeyIDFromContext(c)
	if apiKeyID <= 0 || isOpenAIResponsesCompactPath(c) {
		return ""
	}

	model := strings.ToLower(strings.TrimSpace(upstreamModel))
	if model == "" {
		return ""
	}

	seed := explicitGrokCacheSeed(c, body, explicitKey)
	if seed == "" {
		seed = deriveOpenAIStablePrefixSessionSeed(body)
		if seed == "" {
			seed = deriveOpenAIAnchoredContentSessionSeed(body)
		}
	}
	if seed == "" {
		return ""
	}

	isolatedSeed := fmt.Sprintf("grok-prompt-cache:v1:%d:%s:%s", apiKeyID, model, seed)
	return generateSessionUUID(isolatedSeed)
}

func explicitGrokCacheSeed(c *gin.Context, body []byte, explicitKey string) string {
	seed := ""
	if c != nil {
		seed = extractClaudeCodeSessionID(c, body)
		if seed == "" {
			seed = strings.TrimSpace(c.GetHeader("session_id"))
		}
		if seed == "" {
			seed = strings.TrimSpace(c.GetHeader("conversation_id"))
		}
		if seed == "" {
			seed = strings.TrimSpace(c.GetHeader(grokConversationIDHeader))
		}
	}
	if seed == "" && len(body) > 0 {
		seed = extractClaudeCodeSessionIDFromPayload(body)
	}
	if seed == "" && len(body) > 0 {
		seed = strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	}
	if seed == "" {
		seed = strings.TrimSpace(explicitKey)
	}
	return seed
}

func isGrokRequestContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	v, exists := c.Get("api_key")
	if !exists {
		return false
	}
	apiKey, ok := v.(*APIKey)
	return ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == PlatformGrok
}

// applyGrokResponsesCacheIdentity writes the isolated identity to the request.
// Free OAuth requests without explicit tool intent receive xAI native tools in
// disabled mode so the request is routed through the cache-capable tier.
func applyGrokResponsesCacheIdentity(body, intentSourceBody []byte, identity string, injectFreeTierTools bool) ([]byte, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		if gjson.GetBytes(body, "prompt_cache_key").Exists() {
			return sjson.DeleteBytes(body, "prompt_cache_key")
		}
		return body, nil
	}

	out, err := sjson.SetBytes(body, "prompt_cache_key", identity)
	if err != nil {
		return nil, err
	}
	if !injectFreeTierTools {
		return out, nil
	}
	// Inspect the pre-sanitization source. An additional_tools carrier still
	// represents explicit client tool intent even after patching moves or drops it.
	if hasGrokResponsesToolIntent(intentSourceBody) {
		return out, nil
	}
	out, err = sjson.SetRawBytes(out, "tools", []byte(grokFreeCacheNativeToolsJSON))
	if err != nil {
		return nil, err
	}
	return sjson.SetBytes(out, "tool_choice", grokFreeCacheDisabledToolChoice)
}

func hasGrokResponsesToolIntent(body []byte) bool {
	if gjson.GetBytes(body, "tools").Exists() || gjson.GetBytes(body, "tool_choice").Exists() {
		return true
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	for _, item := range input.Array() {
		if strings.TrimSpace(item.Get("type").String()) != "additional_tools" {
			continue
		}
		tools := item.Get("tools")
		if !tools.Exists() || !tools.IsArray() || len(tools.Array()) > 0 {
			return true
		}
	}
	return false
}

// applyGrokFreeMessagesFunctionToolCacheRoute enables xAI's cache-capable
// mixed-tools route only for known Free accounts. Pure client tools default to
// the cache route; operators can explicitly disable this per account.
func applyGrokFreeMessagesFunctionToolCacheRoute(body, intentSourceBody []byte, account *Account, cacheIdentity string) ([]byte, error) {
	allowPureClientTools, _ := grokClientToolCacheAccountPolicy(account)
	return applyGrokFreeToolCacheRoute(body, intentSourceBody, account, cacheIdentity, allowPureClientTools, true)
}

// applyGrokFreeRequestToolCacheRoute also accepts a request-scoped override.
// The header is consumed locally and is not forwarded to xAI.
func applyGrokFreeRequestToolCacheRoute(c *gin.Context, body, intentSourceBody []byte, account *Account, cacheIdentity string) ([]byte, error) {
	allowPureClientTools, accountPolicyExplicit := grokClientToolCacheAccountPolicy(account)
	requestOptOut := false
	if c != nil {
		switch strings.ToLower(strings.TrimSpace(c.GetHeader(grokClientToolCacheOptInHeader))) {
		case "1", "true", "yes", "on", "prefer-cache":
			allowPureClientTools = true
		case "0", "false", "no", "off":
			allowPureClientTools = false
			requestOptOut = true
		}
	}
	if !allowPureClientTools && !accountPolicyExplicit && !requestOptOut && isGrokClaudeDesktopResponsesCacheRequest(c) {
		allowPureClientTools = true
	}
	return applyGrokFreeToolCacheRoute(
		body,
		intentSourceBody,
		account,
		cacheIdentity,
		allowPureClientTools,
		allowPureClientTools,
	)
}

// grokClientToolCacheAccountPolicy defaults on only for positively identified
// Grok Free OAuth accounts. An invalid configured value fails closed.
func grokClientToolCacheAccountPolicy(account *Account) (enabled, explicit bool) {
	if !isKnownGrokFreeAccount(account) {
		return false, false
	}
	if account.Extra == nil {
		return true, false
	}
	value, exists := account.Extra[grokClientToolCacheOptInExtraKey]
	if !exists {
		return true, false
	}
	enabled, valid := value.(bool)
	if !valid {
		return false, true
	}
	return enabled, true
}

// isGrokClaudeDesktopResponsesCacheRequest recognizes the strict wire
// fingerprint emitted by Claude Desktop through an OpenAI Responses bridge.
func isGrokClaudeDesktopResponsesCacheRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil || isOpenAIResponsesCompactPath(c) {
		return false
	}
	path := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	if !strings.HasSuffix(path, "/responses") {
		return false
	}
	if !claudeCodeUAPattern.MatchString(strings.TrimSpace(c.GetHeader("User-Agent"))) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(c.GetHeader("X-App"))) {
	case "cli", "cli-bg":
	default:
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(c.GetHeader("anthropic-client-platform")), "desktop_app") {
		return false
	}
	return strings.TrimSpace(c.GetHeader(claudeCodeSessionHeader)) != ""
}

func applyGrokFreeToolCacheRoute(
	body,
	intentSourceBody []byte,
	account *Account,
	cacheIdentity string,
	allowPureClientTools,
	allowFunctionSearch bool,
) ([]byte, error) {
	if strings.TrimSpace(cacheIdentity) == "" || !isKnownGrokFreeAccount(account) {
		return body, nil
	}
	intentTools := gjson.GetBytes(intentSourceBody, "tools")
	intentToolChoice := gjson.GetBytes(intentSourceBody, "tool_choice")
	if !isGrokFreeCacheFunctionToolIntent(intentTools, intentToolChoice) {
		return body, nil
	}
	if intentToolChoice.Type == gjson.String &&
		strings.TrimSpace(intentToolChoice.String()) == grokFreeCacheDisabledToolChoice {
		return appendGrokFreeCacheNativeToolsWithPolicy(body, true, false)
	}
	return appendGrokFreeCacheNativeToolsWithPolicy(body, allowPureClientTools, allowFunctionSearch)
}

func isKnownGrokFreeAccount(account *Account) bool {
	if account == nil || !account.IsGrokOAuth() {
		return false
	}

	freeSignal, paidSignal, inferredFreeSignal := grokCacheBillingSignals(account.Extra)
	if snapshot, err := grokQuotaSnapshotFromExtra(account.Extra); err == nil && snapshot != nil {
		if tier := strings.TrimSpace(snapshot.SubscriptionTier); tier != "" {
			if isGrokFreeSubscriptionTier(tier) {
				freeSignal = true
			} else if !isGrokUnknownSubscriptionTier(tier) {
				paidSignal = true
			}
		}
		if snapshot.Tokens != nil && snapshot.Tokens.Limit != nil && *snapshot.Tokens.Limit == grokFreeRolling24hTokenLimit {
			inferredFreeSignal = true
		}
	}
	if tier := strings.TrimSpace(account.GetCredential("subscription_tier")); tier != "" {
		if isGrokFreeSubscriptionTier(tier) {
			freeSignal = true
		} else if !isGrokUnknownSubscriptionTier(tier) {
			paidSignal = true
		}
	}
	return !paidSignal && (freeSignal || inferredFreeSignal)
}

// grokCacheBillingSignals reads only the stable JSON contract stored in Extra.
// This keeps cache routing decoupled from the billing probe implementation.
func grokCacheBillingSignals(extra map[string]any) (free, paid, inferredFree bool) {
	if extra == nil || extra[grokBillingSnapshotCacheExtraKey] == nil {
		return false, false, false
	}
	raw, err := json.Marshal(extra[grokBillingSnapshotCacheExtraKey])
	if err != nil || !json.Valid(raw) {
		return false, false, false
	}
	billing := gjson.ParseBytes(raw)
	if tier := strings.TrimSpace(billing.Get("plan").String()); tier != "" {
		if isGrokFreeSubscriptionTier(tier) {
			free = true
		} else if !isGrokUnknownSubscriptionTier(tier) {
			paid = true
		}
	}
	if value := billing.Get("usage_percent"); value.Exists() && value.Type != gjson.Null {
		paid = true
	}
	if value := billing.Get("used_percent"); value.Exists() && value.Type != gjson.Null {
		paid = true
	}
	if billing.Get("monthly_limit_cents").Float() > 0 {
		paid = true
	}
	statusCode := billing.Get("status_code").Int()
	failedWindows := billing.Get("failed_windows")
	if strings.TrimSpace(billing.Get("monthly_updated_at").String()) != "" ||
		(statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices &&
			!billing.Get("partial").Bool() && (!failedWindows.Exists() || len(failedWindows.Array()) == 0)) {
		inferredFree = true
	}
	return free, paid, inferredFree
}

func isGrokFreeSubscriptionTier(tier string) bool {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "free", "grok-free", "grok_free", "free-tier", "free_tier", "basic", "grok-basic", "grok_basic":
		return true
	default:
		return false
	}
}

func isGrokUnknownSubscriptionTier(tier string) bool {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", "unknown", "n/a", "none":
		return true
	default:
		return false
	}
}

func isGrokFreeCacheFunctionToolIntent(tools, toolChoice gjson.Result) bool {
	if !tools.IsArray() {
		return false
	}
	items := tools.Array()
	if len(items) == 0 {
		return false
	}
	for _, tool := range items {
		if !tool.IsObject() {
			return false
		}
		toolType := strings.TrimSpace(tool.Get("type").String())
		if _, ok := grokResponsesSupportedToolTypes[toolType]; !ok {
			return false
		}
		if toolType == "function" &&
			(strings.TrimSpace(tool.Get("name").String()) == "" || tool.Get("function").Exists()) {
			return false
		}
	}
	if !toolChoice.Exists() {
		return true
	}
	if toolChoice.Type != gjson.String {
		return false
	}
	switch strings.TrimSpace(toolChoice.String()) {
	case "auto", grokFreeCacheDisabledToolChoice:
		return true
	default:
		return false
	}
}

func appendMissingGrokFreeCacheNativeTools(body []byte) ([]byte, error) {
	return appendGrokFreeCacheNativeTools(body, false)
}

func appendGrokFreeCacheNativeTools(body []byte, allowPureClientTools bool) ([]byte, error) {
	return appendGrokFreeCacheNativeToolsWithPolicy(body, allowPureClientTools, true)
}

func appendGrokFreeCacheNativeToolsWithPolicy(
	body []byte,
	allowPureClientTools,
	allowFunctionSearch bool,
) ([]byte, error) {
	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return body, nil
	}

	items := tools.Array()
	if len(items) == 0 {
		return body, nil
	}
	hasNativeSearch := false
	for _, tool := range items {
		switch strings.TrimSpace(tool.Get("type").String()) {
		case "web_search", "x_search":
			hasNativeSearch = true
		}
	}
	if !allowPureClientTools && !allowFunctionSearch && !hasNativeSearch {
		return body, nil
	}

	merged := make([]json.RawMessage, 0, len(items)+2)
	present := make(map[string]bool, 2)
	hasCompanionTool := false
	for _, tool := range items {
		toolType := strings.TrimSpace(tool.Get("type").String())
		switch toolType {
		case "function":
			name := strings.TrimSpace(tool.Get("name").String())
			if !tool.IsObject() || name == "" || tool.Get("function").Exists() {
				return body, nil
			}
			if (name == "web_search" || name == "x_search") && allowFunctionSearch {
				if present[name] {
					continue
				}
				raw, err := json.Marshal(map[string]string{"type": name})
				if err != nil {
					return nil, err
				}
				merged = append(merged, raw)
				present[name] = true
				if allowPureClientTools {
					hasCompanionTool = true
				}
				continue
			}
			if name == "web_search" || name == "x_search" {
				present[name] = true
			}
			hasCompanionTool = true
			merged = append(merged, json.RawMessage(tool.Raw))
		case "web_search", "x_search":
			if present[toolType] {
				continue
			}
			merged = append(merged, json.RawMessage(tool.Raw))
			present[toolType] = true
		default:
			if _, ok := grokResponsesSupportedToolTypes[toolType]; !ok {
				return body, nil
			}
			hasCompanionTool = true
			merged = append(merged, json.RawMessage(tool.Raw))
		}
	}
	if !hasCompanionTool {
		return body, nil
	}
	if !allowPureClientTools && !present["web_search"] && !present["x_search"] {
		return body, nil
	}
	for _, toolType := range []string{"web_search", "x_search"} {
		if present[toolType] {
			continue
		}
		raw, err := json.Marshal(map[string]string{"type": toolType})
		if err != nil {
			return nil, err
		}
		merged = append(merged, raw)
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(body, "tools", encoded)
}

func applyGrokCacheHeaders(headers http.Header, identity string) {
	if headers == nil {
		return
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		headers.Del(grokConversationIDHeader)
		return
	}
	headers.Set(grokConversationIDHeader, identity)
}

func stripGrokChatPromptCacheKey(body []byte) ([]byte, error) {
	if !gjson.GetBytes(body, "prompt_cache_key").Exists() {
		return body, nil
	}
	return sjson.DeleteBytes(body, "prompt_cache_key")
}
