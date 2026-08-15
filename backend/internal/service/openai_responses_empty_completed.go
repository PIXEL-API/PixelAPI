package service

import "github.com/tidwall/gjson"

const openAIResponsesEmptyCompletedMessage = "OpenAI upstream returned an empty response.completed stream with no output and no usage"

// openAIResponsesCompletedEventIsEmpty reports whether a response.completed /
// response.done SSE payload carries no usage, no error and no output items.
// The accumulated usage is consulted too, because OpenAI may deliver usage on
// an earlier event. An empty terminal event after a stream with no semantic
// output is treated as a silent upstream refusal.
func openAIResponsesCompletedEventIsEmpty(data []byte, usage *OpenAIUsage, usageFieldsComplete bool) bool {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return false
	}
	if usageFieldsComplete {
		return false
	}
	if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0 ||
		usage.ImageInputTokens > 0 || usage.ImageOutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0 ||
		usage.TextInputTokens > 0 || usage.TextOutputTokens > 0 ||
		usage.TextCacheReadInputTokens > 0 || usage.ImageCacheReadInputTokens > 0) {
		return false
	}
	if _, ok := extractOpenAIUsageFromJSONBytes(data); ok {
		return false
	}
	if gjson.GetBytes(data, "error").Exists() || gjson.GetBytes(data, "response.error").Exists() {
		return false
	}
	if output := gjson.GetBytes(data, "response.output"); output.Exists() && output.IsArray() && len(output.Array()) > 0 {
		return false
	}
	return true
}
