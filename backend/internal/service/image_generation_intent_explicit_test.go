package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsExplicitImageGenerationIntent(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		requestedModel string
		body           string
		want           bool
	}{
		{
			name: "passive namespace declaration",
			body: `{"model":"gpt-5.5","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}],"tool_choice":"auto","input":"write code"}`,
		},
		{
			name: "passive Responses Lite declaration",
			body: `{"model":"gpt-5.5","input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]},{"type":"message","role":"user","content":"write code"}],"tool_choice":"auto"}`,
		},
		{
			name: "passive flattened function declaration",
			body: `{"model":"gpt-5.5","tools":[{"type":"function","name":"image_gen.imagegen"}],"tool_choice":"auto","input":"write code"}`,
		},
		{
			name: "native image tool declaration",
			body: `{"model":"gpt-5.5","tools":[{"type":"image_generation","model":"gpt-image-2"}],"tool_choice":"auto"}`,
			want: true,
		},
		{
			name:           "requested image model",
			requestedModel: "gpt-image-2",
			want:           true,
		},
		{
			name:     "image endpoint",
			endpoint: "/v1/images/generations",
			want:     true,
		},
		{
			name: "namespace tool choice",
			body: `{"model":"gpt-5.5","tools":[{"type":"namespace","name":"image_gen"}],"tool_choice":{"type":"namespace","name":"image_gen"}}`,
			want: true,
		},
		{
			name: "flattened function tool choice",
			body: `{"model":"gpt-5.5","tools":[{"type":"function","name":"image_gen.imagegen"}],"tool_choice":{"type":"function","name":"image_gen.imagegen"}}`,
			want: true,
		},
		{
			name: "wrapped function tool choice",
			body: `{"model":"gpt-5.5","tool_choice":{"tool":{"type":"function","name":"image_gen__imagegen"}}}`,
			want: true,
		},
		{
			name: "function object tool choice",
			body: `{"model":"gpt-5.5","tool_choice":{"type":"function","function":{"namespace":"image_gen","name":"imagegen"}}}`,
			want: true,
		},
		{
			name: "image call history is not current intent",
			body: `{"model":"gpt-5.5","input":[{"type":"function_call","namespace":"image_gen","name":"imagegen","arguments":"{}"},{"type":"image_generation_call","id":"ig_1"}]}`,
		},
		{
			name: "malformed body",
			body: `{"model":`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint := test.endpoint
			if endpoint == "" {
				endpoint = openAIResponsesEndpoint
			}
			require.Equal(t, test.want, IsExplicitImageGenerationIntent(
				endpoint,
				test.requestedModel,
				[]byte(test.body),
			))
		})
	}
}

func TestIsExplicitImageGenerationIntentKeepsGeneralDeclarationDetectionSeparate(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","tools":[{"type":"namespace","name":"image_gen"}],"tool_choice":"auto"}`)

	require.True(t, IsImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.5", body))
	require.False(t, IsExplicitImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.5", body))
}
