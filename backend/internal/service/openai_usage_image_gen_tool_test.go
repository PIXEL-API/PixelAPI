//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real-world shaped payload: usage.total_tokens == input+output, proving the
// hosted image_generation tool tokens are reported OUTSIDE `usage` and must be
// added on top of it rather than reclassified out of it.
const hostedImageGenResponseFragment = `"usage": {
		"input_tokens": 43792,
		"output_tokens": 1005,
		"total_tokens": 44797
	},
	"tool_usage": {
		"image_gen": {
			"input_tokens": 7918,
			"input_tokens_details": {"image_tokens": 7620, "text_tokens": 298},
			"output_tokens": 186,
			"output_tokens_details": {"image_tokens": 186, "text_tokens": 0},
			"total_tokens": 8104
		}
	}`

func TestExtractOpenAIUsageFromJSONBytesAddsHostedImageGenToolUsage(t *testing.T) {
	body := []byte(`{"id":"resp_abc123","object":"response",` + hostedImageGenResponseFragment + `}`)

	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)
	require.Equal(t, 43792+7918, usage.InputTokens, "image_gen input tokens must be added to input_tokens")
	require.Equal(t, 7620, usage.ImageInputTokens)
	require.Equal(t, 1005+186, usage.OutputTokens, "image_gen output tokens must be added to output_tokens")
	require.Equal(t, 186, usage.ImageOutputTokens)
}

func TestExtractOpenAIUsageFromJSONBytesAddsHostedImageGenFromUnwrappedResponse(t *testing.T) {
	// extractCodexFinalResponse hands the unwrapped `response` object to the
	// SSE -> JSON path; tool_usage then sits at the document root.
	body := []byte(`{` + hostedImageGenResponseFragment + `}`)

	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)
	require.Equal(t, 51710, usage.InputTokens)
	require.Equal(t, 1191, usage.OutputTokens)
	require.Equal(t, 186, usage.ImageOutputTokens)
	require.Equal(t, 7620, usage.ImageInputTokens)
}

func TestExtractOpenAIUsageFromJSONBytesMatchesHostedImageGenEnvelope(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "response envelope",
			body: `{"response":{"usage":{"input_tokens":10,"output_tokens":5},"tool_usage":{"image_gen":{"input_tokens":7,"output_tokens":3,"input_tokens_details":{"image_tokens":7},"output_tokens_details":{"image_tokens":3}}}}}`,
		},
		{
			name: "data envelope",
			body: `{"data":{"usage":{"input_tokens":10,"output_tokens":5},"tool_usage":{"image_gen":{"input_tokens":7,"output_tokens":3,"input_tokens_details":{"image_tokens":7},"output_tokens_details":{"image_tokens":3}}}}}`,
		},
		{
			name: "data response envelope",
			body: `{"data":{"response":{"usage":{"input_tokens":10,"output_tokens":5},"tool_usage":{"image_gen":{"input_tokens":7,"output_tokens":3,"input_tokens_details":{"image_tokens":7},"output_tokens_details":{"image_tokens":3}}}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, ok := extractOpenAIUsageFromJSONBytes([]byte(tt.body))

			require.True(t, ok)
			require.Equal(t, 17, usage.InputTokens)
			require.Equal(t, 8, usage.OutputTokens)
			require.Equal(t, 7, usage.ImageInputTokens)
			require.Equal(t, 3, usage.ImageOutputTokens)
		})
	}
}

func TestExtractOpenAIUsageFromJSONBytesDoesNotMergeLowerPriorityImageUsage(t *testing.T) {
	body := []byte(`{
		"usage":{"input_tokens":10,"output_tokens":5},
		"data":{
			"usage":{"input_tokens":20,"output_tokens":10},
			"tool_usage":{"image_gen":{"input_tokens":7,"output_tokens":3}}
		}
	}`)

	usage, ok := extractOpenAIUsageFromJSONBytes(body)

	require.True(t, ok)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.Zero(t, usage.ImageInputTokens)
	require.Zero(t, usage.ImageOutputTokens)
}

func TestExtractOpenAIUsageFromJSONBytesWithoutToolUsageUnchanged(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":100,"output_tokens":50}}`)

	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)
	require.Equal(t, 100, usage.InputTokens)
	require.Equal(t, 50, usage.OutputTokens)
	require.Zero(t, usage.ImageInputTokens)
	require.Zero(t, usage.ImageOutputTokens)
}

func TestParseSSEUsageBytesAddsHostedImageGenToolUsage(t *testing.T) {
	svc := &OpenAIGatewayService{}
	data := []byte(`{"type":"response.completed","response":{` + hostedImageGenResponseFragment + `}}`)

	usage := &OpenAIUsage{}
	svc.parseSSEUsageBytes(data, usage)

	require.Equal(t, 51710, usage.InputTokens)
	require.Equal(t, 7620, usage.ImageInputTokens)
	require.Equal(t, 1191, usage.OutputTokens)
	require.Equal(t, 186, usage.ImageOutputTokens)

	// A second terminal event must not double count: the parser assigns a
	// freshly merged snapshot rather than accumulating.
	svc.parseSSEUsageBytes(data, usage)
	require.Equal(t, 51710, usage.InputTokens)
	require.Equal(t, 1191, usage.OutputTokens)
	require.Equal(t, 186, usage.ImageOutputTokens)
}

func TestParseOpenAIWSResponseUsageAddsHostedImageGenToolUsage(t *testing.T) {
	message := []byte(`{"type":"response.completed","response":{` + hostedImageGenResponseFragment + `}}`)

	usage := &OpenAIUsage{}
	parseOpenAIWSResponseUsageFromTerminalEvent(message, usage)

	require.Equal(t, 51710, usage.InputTokens)
	require.Equal(t, 7620, usage.ImageInputTokens)
	require.Equal(t, 1191, usage.OutputTokens)
	require.Equal(t, 186, usage.ImageOutputTokens)
}

func TestMergeHostedImageGenToolUsageKeepsBaseImageTokensAndAdds(t *testing.T) {
	// Base usage already reports image tokens inside output_tokens_details;
	// those are part of output_tokens, the tool block is extra on top.
	body := []byte(`{
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"output_tokens_details": {"image_tokens": 30}
		},
		"tool_usage": {
			"image_gen": {
				"input_tokens": 200,
				"input_tokens_details": {"image_tokens": 180, "text_tokens": 20},
				"output_tokens": 100,
				"output_tokens_details": {"image_tokens": 100, "text_tokens": 0}
			}
		}
	}`)

	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)
	require.Equal(t, 300, usage.InputTokens)
	require.Equal(t, 180, usage.ImageInputTokens)
	require.Equal(t, 150, usage.OutputTokens)
	require.Equal(t, 130, usage.ImageOutputTokens, "base 30 + hosted tool 100")
}

func TestMergeHostedImageGenToolUsageMissingTotalsFallBackToDetails(t *testing.T) {
	body := []byte(`{
		"usage": {"input_tokens": 10, "output_tokens": 5},
		"tool_usage": {
			"image_gen": {
				"input_tokens_details": {"image_tokens": 700, "text_tokens": 30},
				"output_tokens_details": {"image_tokens": 90}
			}
		}
	}`)

	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)
	require.Equal(t, 10+730, usage.InputTokens)
	require.Equal(t, 700, usage.ImageInputTokens)
	require.Equal(t, 5+90, usage.OutputTokens)
	require.Equal(t, 90, usage.ImageOutputTokens)
}

func TestMergeHostedImageGenToolUsageNoOpVariants(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"missing", `{}`},
		{"null", `{"image_gen":null}`},
		{"not object", `{"image_gen":42}`},
		{"images only", `{"image_gen":{"images":1}}`},
		{"zero tokens", `{"image_gen":{"input_tokens":0,"output_tokens":0}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usage := OpenAIUsage{InputTokens: 100, OutputTokens: 50}
			original := usage
			mergeHostedImageGenToolUsage(gjson.GetBytes([]byte(tc.json), "image_gen"), &usage)
			require.Equal(t, original, usage)
		})
	}
}

func TestMergeHostedImageGenToolUsageExtendsExistingTextClassification(t *testing.T) {
	usage := OpenAIUsage{
		InputTokens:      100,
		TextInputTokens:  60,
		OutputTokens:     50,
		TextOutputTokens: 50,
	}
	body := []byte(`{"image_gen":{
		"input_tokens": 40,
		"input_tokens_details": {"image_tokens": 25, "text_tokens": 15},
		"output_tokens": 20,
		"output_tokens_details": {"image_tokens": 18, "text_tokens": 2}
	}}`)

	mergeHostedImageGenToolUsage(gjson.GetBytes(body, "image_gen"), &usage)

	require.Equal(t, 140, usage.InputTokens)
	require.Equal(t, 75, usage.TextInputTokens)
	require.Equal(t, 25, usage.ImageInputTokens)
	require.Equal(t, 70, usage.OutputTokens)
	require.Equal(t, 52, usage.TextOutputTokens)
	require.Equal(t, 18, usage.ImageOutputTokens)
}

// Guards the actual billing consequence: the hosted image tokens must reach the
// billing token buckets without cannibalising the text buckets.
func TestOpenAIUsageTokensCarriesHostedImageGenTokens(t *testing.T) {
	body := []byte(`{` + hostedImageGenResponseFragment + `}`)
	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)

	tokens, _ := openAIUsageTokens(usage)
	require.Equal(t, 7620, tokens.ImageInputTokens)
	require.Equal(t, 44090, tokens.InputTokens, "43792 base + 298 image_gen text stay on the text input price")
	require.Equal(t, 1191, tokens.OutputTokens)
	require.Equal(t, 186, tokens.ImageOutputTokens)
	// Billing derives text output as OutputTokens - ImageOutputTokens; the base
	// 1005 text output tokens must survive intact.
	require.Equal(t, 1005, tokens.OutputTokens-tokens.ImageOutputTokens)
}
