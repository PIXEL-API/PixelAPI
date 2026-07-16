package service

import "github.com/tidwall/gjson"

func openAIUsageFromGJSON(value gjson.Result) (OpenAIUsage, bool) {
	if !value.Exists() || !value.IsObject() {
		return OpenAIUsage{}, false
	}
	inputTokens := value.Get("input_tokens").Int()
	if inputTokens == 0 {
		inputTokens = value.Get("prompt_tokens").Int()
	}
	outputTokens := value.Get("output_tokens").Int()
	if outputTokens == 0 {
		outputTokens = value.Get("completion_tokens").Int()
	}
	imageOutputTokens := value.Get("output_tokens_details.image_tokens").Int()
	if imageOutputTokens == 0 {
		imageOutputTokens = value.Get("completion_tokens_details.image_tokens").Int()
	}
	return OpenAIUsage{
		InputTokens:               int(inputTokens),
		TextInputTokens:           int(value.Get("input_tokens_details.text_tokens").Int()),
		ImageInputTokens:          int(value.Get("input_tokens_details.image_tokens").Int()),
		OutputTokens:              int(outputTokens),
		TextOutputTokens:          int(value.Get("output_tokens_details.text_tokens").Int()),
		CacheCreationInputTokens:  openAICacheCreationTokensFromUsage(value),
		CacheReadInputTokens:      openAICacheReadTokensFromUsage(value),
		TextCacheReadInputTokens:  int(value.Get("input_tokens_details.cached_text_tokens").Int()),
		ImageCacheReadInputTokens: int(value.Get("input_tokens_details.cached_image_tokens").Int()),
		ImageOutputTokens:         int(imageOutputTokens),
	}, true
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
