package service

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

const cnNativeAnthropicMaxBodyBytes = 256 * 1024

func adaptResponsesClientToolsForAnthropic(body []byte) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, err
	}
	mapping, changed, err := apicompat.AdaptResponsesClientTools(request)
	if err != nil || !changed {
		return body, mapping, err
	}
	adapted, err := json.Marshal(request)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, err
	}
	return adapted, mapping, nil
}

// parseSSEUsagePassthrough extracts usage fields from a native Anthropic SSE
// payload. It mirrors the existing Anthropic passthrough parser without
// coupling the OpenAI gateway to GatewayService.
func parseSSEUsagePassthrough(data string, usage *ClaudeUsage) {
	if usage == nil || strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
		return
	}
	parsed := gjson.Parse(data)
	for _, path := range []string{"message.usage", "usage"} {
		node := parsed.Get(path)
		if !node.Exists() || !node.IsObject() {
			continue
		}
		if v := node.Get("input_tokens").Int(); v > 0 {
			usage.InputTokens = int(v)
		}
		if v := node.Get("output_tokens").Int(); v > 0 {
			usage.OutputTokens = int(v)
		}
		if v := node.Get("cache_read_input_tokens").Int(); v > 0 {
			usage.CacheReadInputTokens = int(v)
		}
		if v := node.Get("cache_creation_input_tokens").Int(); v > 0 {
			usage.CacheCreationInputTokens = int(v)
		}
		if v := node.Get("cached_tokens").Int(); v > 0 && usage.CacheReadInputTokens == 0 {
			usage.CacheReadInputTokens = int(v)
		}
	}
}
