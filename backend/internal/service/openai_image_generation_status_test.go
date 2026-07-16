package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCompletedImageGenerationStatus(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantChanged bool
	}{
		{
			name:        "output item done generating with result",
			input:       `{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"generating","result":"image-data"}}`,
			want:        `{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"completed","result":"image-data"}}`,
			wantChanged: true,
		},
		{
			name:        "terminal response only normalizes eligible image results",
			input:       `{"type":"response.completed","response":{"output":[{"type":"image_generation_call","status":"in_progress","result":"image-data"},{"type":"image_generation_call","status":"failed","result":"partial-data"},{"type":"message","status":"in_progress","result":"text"}]}}`,
			want:        `{"type":"response.completed","response":{"output":[{"type":"image_generation_call","status":"completed","result":"image-data"},{"type":"image_generation_call","status":"failed","result":"partial-data"},{"type":"message","status":"in_progress","result":"text"}]}}`,
			wantChanged: true,
		},
		{
			name:        "response done is supported",
			input:       `{"type":"response.done","response":{"output":[{"type":"image_generation_call","status":"generating","result":"image-data"}]}}`,
			want:        `{"type":"response.done","response":{"output":[{"type":"image_generation_call","status":"completed","result":"image-data"}]}}`,
			wantChanged: true,
		},
		{
			name:  "missing result stays generating",
			input: `{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"generating"}}`,
			want:  `{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"generating"}}`,
		},
		{
			name:  "blank result stays in progress",
			input: `{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"in_progress","result":"  "}}`,
			want:  `{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"in_progress","result":"  "}}`,
		},
		{
			name:  "non terminal event remains unchanged",
			input: `{"type":"response.output_item.added","item":{"type":"image_generation_call","status":"generating","result":"image-data"}}`,
			want:  `{"type":"response.output_item.added","item":{"type":"image_generation_call","status":"generating","result":"image-data"}}`,
		},
		{
			name:  "invalid JSON remains unchanged",
			input: `{"type":"response.output_item.done"`,
			want:  `{"type":"response.output_item.done"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed := normalizeCompletedImageGenerationStatus([]byte(test.input))
			require.Equal(t, test.wantChanged, changed)
			if gjson.Valid(test.want) {
				require.JSONEq(t, test.want, string(got))
			} else {
				require.Equal(t, test.want, string(got))
			}
		})
	}
}

func TestResponsesStreamingNormalizesCompletedImageGenerationStatus(t *testing.T) {
	tests := []struct {
		name        string
		passthrough bool
		status      string
	}{
		{name: "standard streaming", status: "generating"},
		{name: "passthrough streaming", passthrough: true, status: "in_progress"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := newImageGenerationStatusTestService()
			c, recorder := newImageGenerationStatusTestContext()
			streamBody := strings.Join([]string{
				`data: {"type":"response.output_item.done","item":{"id":"ig_stream","type":"image_generation_call","status":"` + test.status + `","result":"final-image"}}`,
				``,
				`data: {"type":"response.completed","response":{"id":"resp_image_stream","model":"mapped-model","output":[{"id":"ig_stream","type":"image_generation_call","status":"` + test.status + `","result":"final-image"}],"usage":{"input_tokens":11,"output_tokens":5,"output_tokens_details":{"image_tokens":4}}}}`,
				``,
			}, "\n")
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(streamBody)),
			}

			if test.passthrough {
				_, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "client-model", "mapped-model")
				require.NoError(t, err)
			} else {
				_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "client-model", "mapped-model")
				require.NoError(t, err)
			}

			body := recorder.Body.String()
			require.NotContains(t, body, `"status":"`+test.status+`"`)
			require.Equal(t, 2, strings.Count(body, `"status":"completed"`))
			require.NotContains(t, body, `"model":"mapped-model"`)
			require.Contains(t, body, `"model":"client-model"`)
		})
	}
}

func TestResponsesSSEExtractionNormalizesCompletedImageGenerationStatus(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"ig_extract","type":"image_generation_call","status":"generating","result":"final-image"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_extract","output":[{"id":"ig_extract","type":"image_generation_call","status":"in_progress","result":"final-image"}]}}`,
	}, "\n")

	finalResponse, ok := extractCodexFinalResponse(body)
	require.True(t, ok)
	require.Equal(t, "completed", gjson.GetBytes(finalResponse, "output.0.status").String())

	outputItems, ok := collectRawResponsesOutputItemsFromSSE(body)
	require.True(t, ok)
	require.Equal(t, "completed", gjson.GetBytes(outputItems, "0.status").String())
}

func newImageGenerationStatusTestService() *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg:           &config.Config{},
		toolCorrector: NewCodexToolCorrector(),
	}
}

func newImageGenerationStatusTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	return c, recorder
}
