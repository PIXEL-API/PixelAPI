package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

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
		value.Get("prompt_cache_hit_tokens"),
	)
}

// mergeOpenAIChatUsage updates only explicitly reported token fields. A trailing
// cost-only frame must not erase usage, and an explicit cached_tokens:0 must
// still replace an earlier nonzero count. Stream usage values are snapshots,
// not deltas, so neither addition nor taking the maximum is correct.
func mergeOpenAIChatUsage(usage *OpenAIUsage, payload string) bool {
	value := gjson.Get(payload, "usage")
	parsed, ok := openAIUsageFromGJSON(value)
	if !ok {
		return false
	}
	updated := false
	assign := func(target *int, tokens int, paths ...string) bool {
		for _, path := range paths {
			if value.Get(path).Exists() {
				*target = tokens
				updated = true
				return true
			}
		}
		return false
	}
	// A new aggregate replaces its old modality breakdown. Keeping an omitted
	// child bucket can resurrect a stale cache hit or bill more than the total.
	previousInputTokens := usage.InputTokens
	if assign(&usage.InputTokens, parsed.InputTokens, "input_tokens", "prompt_tokens") {
		usage.TextInputTokens, usage.ImageInputTokens = 0, 0
		// A downward correction starts a new input snapshot. Cache counts from
		// the larger snapshot cannot be carried into the corrected total.
		if usage.InputTokens == 0 || usage.InputTokens < previousInputTokens {
			usage.CacheReadInputTokens, usage.CacheCreationInputTokens = 0, 0
			usage.TextCacheReadInputTokens, usage.ImageCacheReadInputTokens = 0, 0
		}
	}
	if assign(&usage.OutputTokens, parsed.OutputTokens, "output_tokens", "completion_tokens") {
		usage.TextOutputTokens, usage.ImageOutputTokens = 0, 0
	}
	assign(&usage.TextInputTokens, parsed.TextInputTokens, "input_tokens_details.text_tokens", "prompt_tokens_details.text_tokens")
	assign(&usage.ImageInputTokens, parsed.ImageInputTokens, "input_tokens_details.image_tokens", "prompt_tokens_details.image_tokens")
	assign(&usage.TextOutputTokens, parsed.TextOutputTokens, "output_tokens_details.text_tokens", "completion_tokens_details.text_tokens")
	assign(&usage.ImageOutputTokens, parsed.ImageOutputTokens, "output_tokens_details.image_tokens", "completion_tokens_details.image_tokens")
	if assign(&usage.CacheReadInputTokens, parsed.CacheReadInputTokens,
		"input_tokens_details.cached_tokens", "prompt_tokens_details.cached_tokens",
		"cache_read_input_tokens", "cache_read_tokens", "cached_tokens", "prompt_cache_hit_tokens") {
		usage.TextCacheReadInputTokens, usage.ImageCacheReadInputTokens = 0, 0
	}
	assign(&usage.CacheCreationInputTokens, parsed.CacheCreationInputTokens,
		"input_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_tokens",
		"input_tokens_details.cache_creation_tokens", "prompt_tokens_details.cache_creation_tokens",
		"cache_write_tokens", "cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens")
	assign(&usage.TextCacheReadInputTokens, parsed.TextCacheReadInputTokens, "input_tokens_details.cached_text_tokens", "prompt_tokens_details.cached_text_tokens")
	assign(&usage.ImageCacheReadInputTokens, parsed.ImageCacheReadInputTokens, "input_tokens_details.cached_image_tokens", "prompt_tokens_details.cached_image_tokens")
	return updated
}

// normalizedChatUsage keeps the client-facing protocol bridge consistent with
// the usage used for billing, including provider-specific cache aliases.
func normalizedChatUsage(original *apicompat.ChatUsage, usage OpenAIUsage) *apicompat.ChatUsage {
	result := apicompat.ChatUsage{}
	if original != nil {
		result = *original
	}
	result.PromptTokens = usage.InputTokens
	result.CompletionTokens = usage.OutputTokens
	result.TotalTokens = usage.InputTokens + usage.OutputTokens
	details := apicompat.ChatTokenDetails{}
	if result.PromptTokensDetails != nil {
		details = *result.PromptTokensDetails
	}
	details.CachedTokens = usage.CacheReadInputTokens
	details.CacheCreationTokens = usage.CacheCreationInputTokens
	details.CacheWriteTokens = usage.CacheCreationInputTokens
	result.PromptTokensDetails = &details
	return &result
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
