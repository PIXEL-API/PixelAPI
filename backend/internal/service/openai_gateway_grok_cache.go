package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	grokConversationIDHeader         = "X-Grok-Conv-Id"
	grokFreeCacheNativeToolsJSON     = `[{"type":"web_search"},{"type":"x_search"}]`
	grokFreeCacheDisabledToolChoice  = "none"
	grokFreeRolling24hTokenLimit     = int64(2_000_000)
	grokBillingSnapshotCacheExtraKey = "grok_billing_snapshot"
)

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
		seed = strings.TrimSpace(c.GetHeader("session_id"))
		if seed == "" {
			seed = strings.TrimSpace(c.GetHeader("conversation_id"))
		}
		if seed == "" {
			seed = strings.TrimSpace(c.GetHeader(grokConversationIDHeader))
		}
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
	if gjson.GetBytes(intentSourceBody, "tools").Exists() || gjson.GetBytes(intentSourceBody, "tool_choice").Exists() {
		return out, nil
	}
	out, err = sjson.SetRawBytes(out, "tools", []byte(grokFreeCacheNativeToolsJSON))
	if err != nil {
		return nil, err
	}
	return sjson.SetBytes(out, "tool_choice", grokFreeCacheDisabledToolChoice)
}

// applyGrokFreeMessagesFunctionToolCacheRoute enables the mixed native/function
// tool route only for known Free OAuth accounts on the Messages bridge.
func applyGrokFreeMessagesFunctionToolCacheRoute(body, intentSourceBody []byte, account *Account, cacheIdentity string) ([]byte, error) {
	if strings.TrimSpace(cacheIdentity) == "" || !isKnownGrokFreeAccount(account) {
		return body, nil
	}
	if !isGrokFreeCacheFunctionToolIntent(
		gjson.GetBytes(intentSourceBody, "tools"),
		gjson.GetBytes(intentSourceBody, "tool_choice"),
	) {
		return body, nil
	}
	return appendMissingGrokFreeCacheNativeTools(body)
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
	if !tools.IsArray() || len(tools.Array()) == 0 {
		return false
	}
	for _, tool := range tools.Array() {
		if !tool.IsObject() || strings.TrimSpace(tool.Get("type").String()) != "function" ||
			strings.TrimSpace(tool.Get("name").String()) == "" || tool.Get("function").Exists() {
			return false
		}
	}
	if !toolChoice.Exists() {
		return true
	}
	return toolChoice.Type == gjson.String && strings.TrimSpace(toolChoice.String()) == "auto"
}

func appendMissingGrokFreeCacheNativeTools(body []byte) ([]byte, error) {
	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() || len(tools.Array()) == 0 {
		return body, nil
	}

	items := tools.Array()
	merged := make([]json.RawMessage, 0, len(items)+2)
	present := make(map[string]bool, 2)
	hasFunction := false
	for _, tool := range items {
		toolType := strings.TrimSpace(tool.Get("type").String())
		switch toolType {
		case "function":
			if !tool.IsObject() || strings.TrimSpace(tool.Get("name").String()) == "" || tool.Get("function").Exists() {
				return body, nil
			}
			hasFunction = true
		case "web_search", "x_search":
		default:
			return body, nil
		}
		merged = append(merged, json.RawMessage(tool.Raw))
		present[toolType] = true
	}
	if !hasFunction {
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
