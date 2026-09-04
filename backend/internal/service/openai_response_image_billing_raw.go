package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// resolveOpenAIResponseImageBillingConfigFromRawBody resolves the image intent
// and billing dimensions directly from the request bytes. It deliberately
// avoids materializing the full Responses payload as map[string]any because
// input can contain large conversation and image data unrelated to billing.
func resolveOpenAIResponseImageBillingConfigFromRawBody(endpoint, requestedModel string, body []byte) openAIResponseImageBillingConfig {
	intent := IsImageGenerationIntent(endpoint, requestedModel, body)
	imageModel, imageSize := resolveOpenAIResponsesImageBillingFieldsFromRawBody(body, requestedModel)
	if intent && imageModel == "" {
		imageModel = "gpt-image-2"
	}
	return openAIResponseImageBillingConfig{
		Intent: intent,
		Model:  imageModel,
		Size:   imageSize,
	}
}

func resolveOpenAIResponsesImageBillingFieldsFromRawBody(body []byte, fallbackModel string) (string, string) {
	imageModel := ""
	imageSize := ""
	hasImageTool := false
	if len(body) > 0 && gjson.ValidBytes(body) {
		root := parseRawJSONView(body)
		tools := root.Get("tools")
		if tools.IsArray() {
			tools.ForEach(func(_, item gjson.Result) bool {
				if openAIImageBillingJSONString(item.Get("type")) != "image_generation" {
					return true
				}
				hasImageTool = true
				imageModel = openAIImageBillingJSONString(item.Get("model"))
				imageSize = openAIImageBillingJSONString(item.Get("size"))
				return false
			})
		}
		if imageSize == "" {
			imageSize = openAIImageBillingJSONString(root.Get("size"))
		}
		if imageModel == "" {
			bodyModel := openAIImageBillingJSONString(root.Get("model"))
			if isOpenAIImageBillingModelAlias(bodyModel) || !hasImageTool {
				imageModel = bodyModel
			}
		}
	}
	if imageModel == "" && hasImageTool {
		imageModel = "gpt-image-2"
	}
	if imageModel == "" {
		imageModel = strings.TrimSpace(fallbackModel)
	}
	// Image accounting survives the request view and can enter the usage queue.
	// Detach its model from the raw request's backing allocation.
	return strings.Clone(strings.TrimSpace(imageModel)), normalizeOpenAIImageSizeTier(imageSize)
}

func openAIImageBillingJSONString(value gjson.Result) string {
	if value.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(value.String())
}
