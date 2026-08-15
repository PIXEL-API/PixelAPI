package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestShouldRecordGrokMediaUsage(t *testing.T) {
	tests := []struct {
		name     string
		endpoint service.GrokMediaEndpoint
		model    string
		result   *service.OpenAIForwardResult
		want     bool
	}{
		{
			name:     "image generation records usage",
			endpoint: service.GrokMediaEndpointImagesGenerations,
			model:    "grok-imagine",
			result:   &service.OpenAIForwardResult{ImageCount: 1},
			want:     true,
		},
		{
			name:     "image edit records usage",
			endpoint: service.GrokMediaEndpointImagesEdits,
			model:    "grok-imagine-edit",
			result:   &service.OpenAIForwardResult{ImageCount: 1},
			want:     true,
		},
		{
			name:     "video generation defers usage",
			endpoint: service.GrokMediaEndpointVideosGenerations,
			model:    "grok-imagine-video-1.5",
			result:   &service.OpenAIForwardResult{VideoCount: 1},
			want:     false,
		},
		{
			name:     "video edit defers usage",
			endpoint: service.GrokMediaEndpointVideosEdits,
			model:    "grok-imagine-video-1.5",
			result:   &service.OpenAIForwardResult{VideoCount: 1},
			want:     false,
		},
		{
			name:     "video extension defers usage",
			endpoint: service.GrokMediaEndpointVideosExtensions,
			model:    "grok-imagine-video-1.5",
			result:   &service.OpenAIForwardResult{VideoCount: 1},
			want:     false,
		},
		{
			name:     "video status skips empty model usage",
			endpoint: service.GrokMediaEndpointVideoStatus,
			model:    "",
			result:   &service.OpenAIForwardResult{VideoCount: 1},
			want:     false,
		},
		{
			name:     "generation skips usage without model",
			endpoint: service.GrokMediaEndpointImagesGenerations,
			model:    " ",
			result:   &service.OpenAIForwardResult{ImageCount: 1},
			want:     false,
		},
		{
			name:     "successful image response without output skips usage",
			endpoint: service.GrokMediaEndpointImagesGenerations,
			model:    "grok-imagine",
			result:   &service.OpenAIForwardResult{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldRecordGrokMediaUsage(tt.endpoint, tt.model, tt.result))
		})
	}
}
