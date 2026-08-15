package openaiusage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectEnvelopePrecedenceAndShapeRules(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      bool
		wantIndex int
		wantInput int64
	}{
		{
			name:      "top level wins",
			body:      `{"usage":{"input_tokens":1},"response":{"usage":{"input_tokens":2}}}`,
			want:      true,
			wantIndex: 0,
			wantInput: 1,
		},
		{
			name:      "empty object blocks lower priority",
			body:      `{"usage":{},"response":{"usage":{"input_tokens":2}}}`,
			want:      true,
			wantIndex: 0,
			wantInput: 0,
		},
		{
			name:      "invalid shape is skipped",
			body:      `{"usage":"invalid","data":{"usage":{"input_tokens":3}}}`,
			want:      true,
			wantIndex: 2,
			wantInput: 3,
		},
		{
			name: "no valid object",
			body: `{"usage":"invalid","response":{"usage":null}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope, ok := SelectEnvelope([]byte(tt.body))
			require.Equal(t, tt.want, ok)
			if !tt.want {
				return
			}
			require.Equal(t, tt.wantIndex, envelope.Index)
			require.Equal(t, tt.wantInput, envelope.Usage.Get("input_tokens").Int())
		})
	}
}

func TestFirstPresentUsageReturnsInvalidValueForDiagnostics(t *testing.T) {
	usage, ok := FirstPresentUsage([]byte(`{"usage":"invalid","response":{"usage":{"input_tokens":2}}}`))
	require.True(t, ok)
	require.Equal(t, "invalid", usage.String())
}

func TestSelectEnvelopeReturnsCompanionsFromWinningContainer(t *testing.T) {
	envelope, ok := SelectEnvelope([]byte(`{
		"response":{"id":"resp_lower","model":"model-lower","usage":{"input_tokens":2}},
		"data":{"response":{
			"id":"resp_winner",
			"model":"model-winner",
			"usage":{"input_tokens":3},
			"output":[{"id":"image_winner"}],
			"tool_usage":{"image_gen":{"output_tokens":4}},
			"service_tier":"priority"
		}}
	}`))
	require.True(t, ok)
	require.Equal(t, 1, envelope.Index)
	require.Equal(t, "resp_lower", envelope.Container.Get("id").String())
	require.Equal(t, "model-lower", envelope.Container.Get("model").String())
	require.Empty(t, envelope.Container.Get("output").Array())
	require.False(t, envelope.ImageGen.Exists())
	require.Empty(t, envelope.ServiceTier)

	envelope, ok = SelectEnvelope([]byte(`{
		"response":{"usage":null},
		"data":{"response":{
			"id":"resp_winner",
			"model":"model-winner",
			"usage":{"input_tokens":3},
			"output":[{"id":"image_winner"}],
			"tool_usage":{"image_gen":{"output_tokens":4}},
			"service_tier":"priority"
		}}
	}`))
	require.True(t, ok)
	require.Equal(t, 3, envelope.Index)
	require.Equal(t, "resp_winner", envelope.Container.Get("id").String())
	require.Equal(t, "model-winner", envelope.Container.Get("model").String())
	require.Equal(t, "image_winner", envelope.Container.Get("output.0.id").String())
	require.Equal(t, int64(4), envelope.ImageGen.Get("output_tokens").Int())
	require.Equal(t, "priority", envelope.ServiceTier)
}

func TestParseHostedImageGenTokensUsesDetailsAsMinimumTotals(t *testing.T) {
	envelope, ok := SelectEnvelope([]byte(`{
		"usage":{},
		"tool_usage":{"image_gen":{
			"input_tokens":2,
			"input_tokens_details":{"image_tokens":3,"text_tokens":4},
			"output_tokens_details":{"image_tokens":5,"text_tokens":6}
		}}
	}`))
	require.True(t, ok)
	usage := ParseHostedImageGenTokens(envelope.ImageGen)
	require.Equal(t, 7, usage.InputTokens)
	require.Equal(t, 3, usage.ImageInputTokens)
	require.Equal(t, 4, usage.TextInputTokens)
	require.Equal(t, 11, usage.OutputTokens)
	require.Equal(t, 5, usage.ImageOutputTokens)
	require.Equal(t, 6, usage.TextOutputTokens)
}
