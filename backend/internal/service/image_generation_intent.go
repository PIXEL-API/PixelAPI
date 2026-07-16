package service

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	openAIResponsesEndpoint          = "/v1/responses"
	openAIResponsesCompactEndpoint   = "/v1/responses/compact"
	responsesLiteHeader              = "X-OpenAI-Internal-Codex-Responses-Lite"
	responsesLiteHeaderKey           = "x-openai-internal-codex-responses-lite"
	responsesLiteWSMetadataKey       = "ws_request_header_x_openai_internal_codex_responses_lite"
	imageGenerationPermissionMessage = "Image generation is not enabled for this group"
)

func isOpenAIResponsesLiteHeader(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func isOpenAIResponsesLiteWebSocketPayload(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	return isOpenAIResponsesLiteHeader(gjson.GetBytes(body, "client_metadata."+responsesLiteWSMetadataKey).String())
}

// ImageGenerationPermissionMessage returns the stable end-user error text for disabled groups.
func ImageGenerationPermissionMessage() string {
	return imageGenerationPermissionMessage
}

// GroupAllowsImageGeneration preserves ungrouped-key behavior and enforces the flag when a group is present.
func GroupAllowsImageGeneration(group *Group) bool {
	return group == nil || group.AllowImageGeneration
}

// IsImageGenerationIntent classifies requests that can produce generated images.
func IsImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
	if IsImageGenerationEndpoint(endpoint) {
		return true
	}
	if isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	if model := strings.TrimSpace(gjson.GetBytes(body, "model").String()); isOpenAIImageGenerationModel(model) {
		return true
	}
	if openAIJSONToolsContainImageGeneration(gjson.GetBytes(body, "tools")) {
		return true
	}
	if openAIJSONInputContainsImageGenTool(gjson.GetBytes(body, "input")) {
		return true
	}
	return openAIJSONToolChoiceSelectsImageGeneration(gjson.GetBytes(body, "tool_choice"))
}

// IsImageGenerationIntentMap is the map-backed variant used after service-side request mutation.
func IsImageGenerationIntentMap(endpoint string, requestedModel string, reqBody map[string]any) bool {
	if IsImageGenerationEndpoint(endpoint) {
		return true
	}
	if isOpenAIImageGenerationModel(requestedModel) {
		return true
	}
	if reqBody == nil {
		return false
	}
	if isOpenAIImageGenerationModel(firstNonEmptyString(reqBody["model"])) {
		return true
	}
	if hasOpenAIImageGenerationTool(reqBody) {
		return true
	}
	return openAIAnyToolChoiceSelectsImageGeneration(reqBody["tool_choice"])
}

// IsImageGenerationEndpoint identifies dedicated generated-image endpoints.
func IsImageGenerationEndpoint(endpoint string) bool {
	switch normalizeImageGenerationEndpoint(endpoint) {
	case "/v1/images/generations", "/v1/images/edits", "/images/generations", "/images/edits":
		return true
	default:
		return false
	}
}

func normalizeImageGenerationEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(strings.ToLower(endpoint))
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimPrefix(endpoint, "https://api.openai.com")
	if idx := strings.IndexByte(endpoint, '?'); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return strings.TrimRight(endpoint, "/")
}

func openAIJSONToolsContainImageGeneration(tools gjson.Result) bool {
	if !tools.IsArray() {
		return false
	}
	found := false
	tools.ForEach(func(_, item gjson.Result) bool {
		if isOpenAIImageGenerationType(item.Get("type").String()) || isImageGenNamespaceTool(item) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isOpenAIImageGenerationType(value string) bool {
	return strings.TrimSpace(value) == "image_generation"
}

func isOpenAIImageGenNamespaceName(value string) bool {
	return strings.TrimSpace(value) == "image_gen"
}

func isImageGenNamespaceTool(tool gjson.Result) bool {
	return strings.TrimSpace(tool.Get("type").String()) == "namespace" &&
		isOpenAIImageGenNamespaceName(tool.Get("name").String())
}

func openAIJSONInputContainsImageGenTool(input gjson.Result) bool {
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if strings.TrimSpace(item.Get("type").String()) != "additional_tools" {
			return true
		}
		if openAIJSONToolsContainImageGeneration(item.Get("tools")) {
			found = true
			return false
		}
		return true
	})
	return found
}

func openAIJSONToolChoiceSelectsImageGeneration(choice gjson.Result) bool {
	if !choice.Exists() {
		return false
	}
	if choice.Type == gjson.String {
		return strings.TrimSpace(choice.String()) == "image_generation"
	}
	if !choice.IsObject() {
		return false
	}
	if strings.TrimSpace(choice.Get("type").String()) == "image_generation" {
		return true
	}
	if strings.TrimSpace(choice.Get("type").String()) == "namespace" &&
		(isOpenAIImageGenNamespaceName(choice.Get("name").String()) ||
			isOpenAIImageGenNamespaceName(choice.Get("namespace").String())) {
		return true
	}
	if strings.TrimSpace(choice.Get("tool.type").String()) == "image_generation" {
		return true
	}
	if strings.TrimSpace(choice.Get("function.name").String()) == "image_generation" {
		return true
	}
	return false
}

func openAIAnyToolChoiceSelectsImageGeneration(choice any) bool {
	switch v := choice.(type) {
	case string:
		return strings.TrimSpace(v) == "image_generation"
	case map[string]any:
		choiceType := strings.TrimSpace(firstNonEmptyString(v["type"]))
		if isOpenAIImageGenerationType(choiceType) {
			return true
		}
		if choiceType == "namespace" &&
			(isOpenAIImageGenNamespaceName(firstNonEmptyString(v["name"])) ||
				isOpenAIImageGenNamespaceName(firstNonEmptyString(v["namespace"]))) {
			return true
		}
		if tool, ok := v["tool"].(map[string]any); ok && openAIAnyToolChoiceSelectsImageGeneration(tool) {
			return true
		}
		if fn, ok := v["function"].(map[string]any); ok && isOpenAIImageGenerationType(firstNonEmptyString(fn["name"])) {
			return true
		}
	}
	return false
}

func getAPIKeyFromContext(c interface{ Get(string) (any, bool) }) *APIKey {
	if c == nil {
		return nil
	}
	v, exists := c.Get("api_key")
	if !exists {
		return nil
	}
	apiKey, _ := v.(*APIKey)
	return apiKey
}

func apiKeyGroup(apiKey *APIKey) *Group {
	if apiKey == nil {
		return nil
	}
	return apiKey.Group
}

func cloneRequestMapForImageIntent(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func resolveOpenAIResponsesImageBillingConfig(reqBody map[string]any, fallbackModel string) (string, string, error) {
	imageModel := ""
	imageSize := ""
	hasImageTool := false
	if reqBody != nil {
		rawTools, _ := reqBody["tools"].([]any)
		for _, rawTool := range rawTools {
			toolMap, ok := rawTool.(map[string]any)
			if !ok || strings.TrimSpace(firstNonEmptyString(toolMap["type"])) != "image_generation" {
				continue
			}
			hasImageTool = true
			imageModel = strings.TrimSpace(firstNonEmptyString(toolMap["model"]))
			imageSize = strings.TrimSpace(firstNonEmptyString(toolMap["size"]))
			break
		}
		if imageSize == "" {
			imageSize = strings.TrimSpace(firstNonEmptyString(reqBody["size"]))
		}
	}
	if imageModel == "" && reqBody != nil {
		bodyModel := strings.TrimSpace(firstNonEmptyString(reqBody["model"]))
		if isOpenAIImageBillingModelAlias(bodyModel) || !hasImageTool {
			imageModel = bodyModel
		}
	}
	if imageModel == "" && hasImageTool {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = strings.TrimSpace(fallbackModel)
	}
	sizeTier := normalizeOpenAIImageSizeTier(imageSize)
	return imageModel, sizeTier, nil
}

func resolveOpenAIResponsesImageBillingConfigFromBody(body []byte, fallbackModel string) (string, string, error) {
	reqBody := cloneRequestMapForImageIntent(body)
	return resolveOpenAIResponsesImageBillingConfig(reqBody, fallbackModel)
}

func isOpenAIImageBillingModelAlias(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return false
	}
	return isOpenAIImageGenerationModel(normalized) || strings.Contains(normalized, "image")
}
