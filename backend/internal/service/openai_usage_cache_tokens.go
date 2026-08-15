package service

import "github.com/tidwall/gjson"

func openAIUsageFromGJSON(value gjson.Result) (OpenAIUsage, bool) {
	if !value.Exists() || !value.IsObject() {
		return OpenAIUsage{}, false
	}
	inputTokens := openAIUsageIntAlias(value, "input_tokens", "prompt_tokens")
	outputTokens := openAIUsageIntAlias(value, "output_tokens", "completion_tokens")
	imageOutputTokens := openAIUsageIntAlias(value, "output_tokens_details.image_tokens", "completion_tokens_details.image_tokens")
	return OpenAIUsage{
		InputTokens:               int(inputTokens),
		TextInputTokens:           int(openAIUsageIntAlias(value, "input_tokens_details.text_tokens", "prompt_tokens_details.text_tokens")),
		ImageInputTokens:          int(openAIUsageIntAlias(value, "input_tokens_details.image_tokens", "prompt_tokens_details.image_tokens")),
		OutputTokens:              int(outputTokens),
		TextOutputTokens:          int(openAIUsageIntAlias(value, "output_tokens_details.text_tokens", "completion_tokens_details.text_tokens")),
		CacheCreationInputTokens:  openAICacheCreationTokensFromUsage(value),
		CacheReadInputTokens:      openAICacheReadTokensFromUsage(value),
		TextCacheReadInputTokens:  int(openAIUsageIntAlias(value, "input_tokens_details.cached_text_tokens", "prompt_tokens_details.cached_text_tokens")),
		ImageCacheReadInputTokens: int(openAIUsageIntAlias(value, "input_tokens_details.cached_image_tokens", "prompt_tokens_details.cached_image_tokens")),
		ImageOutputTokens:         int(imageOutputTokens),
	}, true
}

// openAIUsageIntAlias uses field presence, not a non-zero heuristic. An
// explicitly reported canonical zero is authoritative and must not be replaced
// by a stale compatibility alias from the same Usage object.
func openAIUsageIntAlias(value gjson.Result, canonicalPath, aliasPath string) int64 {
	canonical := value.Get(canonicalPath)
	if canonical.Exists() {
		return canonical.Int()
	}
	return value.Get(aliasPath).Int()
}

func openAICacheReadTokensFromUsage(value gjson.Result) int {
	for _, nested := range []gjson.Result{
		value.Get("input_tokens_details.cached_tokens"),
		value.Get("prompt_tokens_details.cached_tokens"),
	} {
		if nested.Exists() {
			return max(int(nested.Int()), 0)
		}
	}
	return firstPositiveGJSONInt(
		value.Get("cache_read_input_tokens"),
		value.Get("cache_read_tokens"),
		value.Get("cached_tokens"),
	)
}

func openAICacheCreationTokensFromUsage(value gjson.Result) int {
	for _, nested := range []gjson.Result{
		value.Get("input_tokens_details.cache_write_tokens"),
		value.Get("prompt_tokens_details.cache_write_tokens"),
		value.Get("input_tokens_details.cache_creation_tokens"),
		value.Get("prompt_tokens_details.cache_creation_tokens"),
	} {
		if nested.Exists() {
			return max(int(nested.Int()), 0)
		}
	}
	return firstPositiveGJSONInt(
		value.Get("cache_write_tokens"),
		value.Get("cache_creation_input_tokens"),
		value.Get("cache_write_input_tokens"),
		value.Get("cache_creation_tokens"),
	)
}

func firstPositiveGJSONInt(values ...gjson.Result) int {
	for _, value := range values {
		if tokens := int(value.Int()); tokens > 0 {
			return tokens
		}
	}
	return 0
}
