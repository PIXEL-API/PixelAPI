package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizedOpenAIUpstreamEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		path     string
		fallback string
		want     string
	}{
		{
			name:     "responses root maps to responses upstream",
			path:     "/v1/responses",
			fallback: openAIUpstreamEndpointResponses,
			want:     "/v1/responses",
		},
		{
			name:     "responses compact keeps compact suffix",
			path:     "/openai/v1/responses/compact",
			fallback: openAIUpstreamEndpointResponses,
			want:     "/v1/responses/compact",
		},
		{
			name:     "responses nested suffix preserved",
			path:     "/openai/v1/responses/compact/detail",
			fallback: openAIUpstreamEndpointResponses,
			want:     "/v1/responses/compact/detail",
		},
		{
			name:     "non responses path uses fallback",
			path:     "/v1/messages",
			fallback: openAIUpstreamEndpointResponses,
			want:     "/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)

			got := normalizedOpenAIUpstreamEndpoint(c, tt.fallback)
			require.Equal(t, tt.want, got)
		})
	}
}
