package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/openaiusage"
	"github.com/tidwall/gjson"
)

// mergeHostedImageGenToolUsage folds the hosted image_generation tool usage into
// the base usage parsed from `usage` / `response.usage`.
//
// OpenAI reports the hosted tool's tokens in a SEPARATE accounting block: the
// sample observed upstream has usage.total_tokens == input_tokens+output_tokens,
// i.e. tool_usage.image_gen tokens are NOT already included in `usage`. They are
// therefore ADDED here rather than assigned.
//
// This is the one place where we deliberately diverge from upstream sub2api,
// which merely assigns image dims when they are zero. Assignment is wrong for
// this fork because billing treats ImageInputTokens/ImageOutputTokens as
// *classifications* of InputTokens/OutputTokens (see openAIUsageTokens and
// BillingService.calculateCostBreakdown: textOutput = OutputTokens -
// ImageOutputTokens). Assigning would silently move already-billed text tokens
// into the image bucket instead of billing the extra image tokens, so the
// undercount would persist and text tokens would be misclassified on top.
func mergeHostedImageGenToolUsage(imageGen gjson.Result, usage *OpenAIUsage) {
	if usage == nil || !imageGen.Exists() || !imageGen.IsObject() {
		return
	}

	toolUsage := openaiusage.ParseHostedImageGenTokens(imageGen)

	if toolUsage.InputTokens > 0 {
		usage.InputTokens = nonNegativeOpenAITokenCount(usage.InputTokens) + toolUsage.InputTokens
	}
	if toolUsage.ImageInputTokens > 0 {
		usage.ImageInputTokens = nonNegativeOpenAITokenCount(usage.ImageInputTokens) + toolUsage.ImageInputTokens
	}
	// Only extend the text classification when the base usage already carries
	// one; otherwise the remainder stays unclassified input, which bills at the
	// same rate and avoids inventing a partial classification.
	if toolUsage.TextInputTokens > 0 && usage.TextInputTokens > 0 {
		usage.TextInputTokens += toolUsage.TextInputTokens
	}

	if toolUsage.OutputTokens > 0 {
		usage.OutputTokens = nonNegativeOpenAITokenCount(usage.OutputTokens) + toolUsage.OutputTokens
	}
	if toolUsage.ImageOutputTokens > 0 {
		usage.ImageOutputTokens = nonNegativeOpenAITokenCount(usage.ImageOutputTokens) + toolUsage.ImageOutputTokens
	}
	if toolUsage.TextOutputTokens > 0 && usage.TextOutputTokens > 0 {
		usage.TextOutputTokens += toolUsage.TextOutputTokens
	}
}
