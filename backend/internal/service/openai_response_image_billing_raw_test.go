package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOpenAIResponseImageBillingConfigFromRawBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		endpoint       string
		requestedModel string
		body           string
		want           openAIResponseImageBillingConfig
	}{
		{
			name:           "empty body keeps fallback model without intent",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "gpt-5.4",
			want: openAIResponseImageBillingConfig{
				Model: "gpt-5.4",
				Size:  ImageBillingSize2K,
			},
		},
		{
			name:           "requested image model establishes intent",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: " gpt-image-2 ",
			body:           `{}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-2",
				Size:   ImageBillingSize2K,
			},
		},
		{
			name:           "native tool model and size win",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "gpt-5.4",
			body:           `{"model":"mapped-text-model","tools":[{"type":"image_generation","model":" gpt-image-2 ","size":" 1536x1024 "}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-2",
				Size:   ImageBillingSize2K,
			},
		},
		{
			name:           "native tool without model uses image default instead of text model",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "requested-text-model",
			body:           `{"model":"mapped-text-model","tools":[{"type":"image_generation","size":"1024x1024"}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-2",
				Size:   ImageBillingSize1K,
			},
		},
		{
			name:           "image body model fills missing native tool model",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "requested-text-model",
			body:           `{"model":"mapped-image-model","tools":[{"type":"image_generation","size":"1024x1024"}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "mapped-image-model",
				Size:   ImageBillingSize1K,
			},
		},
		{
			name:           "top level size fills empty tool size",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "gpt-5.4",
			body:           `{"size":"3840x2160","tools":[{"type":"image_generation","model":"gpt-image-2","size":"   "}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-2",
				Size:   ImageBillingSize4K,
			},
		},
		{
			name:           "first native image tool wins",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "gpt-5.4",
			body:           `{"tools":[{"type":"function","name":"noop"},{"type":"image_generation","model":"gpt-image-1","size":"1024x1024"},{"type":"image_generation","model":"gpt-image-2","size":"3840x2160"}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-1",
				Size:   ImageBillingSize1K,
			},
		},
		{
			name:           "namespace tool affects intent but not native tool billing selection",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "requested-model",
			body:           `{"model":"mapped-text-model","tools":[{"type":"namespace","name":"image_gen"}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "mapped-text-model",
				Size:   ImageBillingSize2K,
			},
		},
		{
			name:           "additional tools affect intent but billing retains body model",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "requested-model",
			body:           `{"model":"mapped-text-model","input":[{"type":"additional_tools","tools":[{"type":"image_generation","model":"ignored-image-model","size":"3840x2160"}]}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "mapped-text-model",
				Size:   ImageBillingSize2K,
			},
		},
		{
			name:           "tool choice affects intent",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "requested-model",
			body:           `{"tool_choice":{"type":"image_generation"}}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "requested-model",
				Size:   ImageBillingSize2K,
			},
		},
		{
			name:           "non string billing fields are ignored",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "fallback-model",
			body:           `{"model":42,"size":1024,"tools":[{"type":"image_generation","model":true,"size":{"width":1024}}]}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-2",
				Size:   ImageBillingSize2K,
			},
		},
		{
			name:           "invalid json behaves like an absent request map",
			endpoint:       openAIResponsesEndpoint,
			requestedModel: "fallback-model",
			body:           `{"model":"gpt-image-2"`,
			want: openAIResponseImageBillingConfig{
				Model: "fallback-model",
				Size:  ImageBillingSize2K,
			},
		},
		{
			name:           "dedicated image endpoint establishes intent",
			endpoint:       "/v1/images/generations",
			requestedModel: "",
			body:           `{}`,
			want: openAIResponseImageBillingConfig{
				Intent: true,
				Model:  "gpt-image-2",
				Size:   ImageBillingSize2K,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveOpenAIResponseImageBillingConfigFromRawBody(tt.endpoint, tt.requestedModel, []byte(tt.body))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveOpenAIResponseImageBillingConfigFromRawBodyIgnoresLargeUnrelatedInput(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":"2048x1152"}],"input":"` + strings.Repeat("x", 4<<20) + `"}`)
	got := resolveOpenAIResponseImageBillingConfigFromRawBody(openAIResponsesEndpoint, "fallback-model", body)
	require.Equal(t, openAIResponseImageBillingConfig{
		Intent: true,
		Model:  "gpt-image-2",
		Size:   ImageBillingSize2K,
	}, got)
}

func BenchmarkResolveOpenAIResponseImageBillingConfigFromRawBodyLargeInput(b *testing.B) {
	body := []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation","model":"gpt-image-2","size":"2048x1152"}],"input":"` + strings.Repeat("x", 4<<20) + `"}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for range b.N {
		_ = resolveOpenAIResponseImageBillingConfigFromRawBody(openAIResponsesEndpoint, "fallback-model", body)
	}
}
