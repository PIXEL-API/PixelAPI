package service

import "github.com/tidwall/gjson"

// hostedImageGenToolUsage locates the hosted `image_generation` tool usage block
// inside a /v1/responses payload. The block lives beside `usage` (not inside it),
// either at the document root (non-streaming body, or the unwrapped `response`
// object produced by extractCodexFinalResponse) or under `response` (raw SSE /
// WebSocket `response.completed` events).
func hostedImageGenToolUsage(body []byte) gjson.Result {
	if imageGen := gjson.GetBytes(body, "tool_usage.image_gen"); imageGen.Exists() && imageGen.IsObject() {
		return imageGen
	}
	return gjson.GetBytes(body, "response.tool_usage.image_gen")
}

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

	imageInput := nonNegativeOpenAITokenCount(int(imageGen.Get("input_tokens_details.image_tokens").Int()))
	textInput := nonNegativeOpenAITokenCount(int(imageGen.Get("input_tokens_details.text_tokens").Int()))
	totalInput := hostedImageGenTotalTokens(int(imageGen.Get("input_tokens").Int()), imageInput, textInput)

	imageOutput := nonNegativeOpenAITokenCount(int(imageGen.Get("output_tokens_details.image_tokens").Int()))
	textOutput := nonNegativeOpenAITokenCount(int(imageGen.Get("output_tokens_details.text_tokens").Int()))
	totalOutput := hostedImageGenTotalTokens(int(imageGen.Get("output_tokens").Int()), imageOutput, textOutput)

	if totalInput > 0 {
		usage.InputTokens = nonNegativeOpenAITokenCount(usage.InputTokens) + totalInput
	}
	if imageInput > 0 {
		usage.ImageInputTokens = nonNegativeOpenAITokenCount(usage.ImageInputTokens) + imageInput
	}
	// Only extend the text classification when the base usage already carries
	// one; otherwise the remainder stays unclassified input, which bills at the
	// same rate and avoids inventing a partial classification.
	if textInput > 0 && usage.TextInputTokens > 0 {
		usage.TextInputTokens += textInput
	}

	if totalOutput > 0 {
		usage.OutputTokens = nonNegativeOpenAITokenCount(usage.OutputTokens) + totalOutput
	}
	if imageOutput > 0 {
		usage.ImageOutputTokens = nonNegativeOpenAITokenCount(usage.ImageOutputTokens) + imageOutput
	}
	if textOutput > 0 && usage.TextOutputTokens > 0 {
		usage.TextOutputTokens += textOutput
	}
}

// hostedImageGenTotalTokens trusts the reported total but never lets it fall
// below the sum of its known details, so a truncated/absent total cannot drop
// billable tokens.
func hostedImageGenTotalTokens(reported, image, text int) int {
	total := nonNegativeOpenAITokenCount(reported)
	if sum := image + text; sum > total {
		total = sum
	}
	return total
}
