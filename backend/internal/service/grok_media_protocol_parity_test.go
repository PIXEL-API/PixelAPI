package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestParseGrokMediaRequestIncludesReferenceImages(t *testing.T) {
	t.Parallel()
	info := ParseGrokMediaRequest("application/json", []byte(`{
		"model":"grok-imagine-video-1.5-preview",
		"image":{"image_url":"https://example.test/a.png"},
		"reference_images":[
			{"url":"https://example.test/b.png","type":"image_url"},
			{"image_url":{"url":"https://example.test/c.png"}},
			"https://example.test/d.png"
		]
	}`))
	require.Equal(t, []string{
		"https://example.test/a.png",
		"https://example.test/b.png",
		"https://example.test/c.png",
		"https://example.test/d.png",
	}, info.InputImageURLs)
}

func TestNormalizeGrokVideoReferenceImagesCanonicalizesLegacyImageURL(t *testing.T) {
	t.Parallel()
	body, contentType, err := normalizeGrokMediaForwardBody(
		GrokMediaEndpointVideosGenerations,
		[]byte(`{
			"model":"grok-imagine-video-1.5-preview",
			"reference_images":[{"image_url":"https://example.test/reference.png","type":"image_url"}]
		}`),
		"application/json",
	)
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "https://example.test/reference.png", gjson.GetBytes(body, "reference_images.0.url").String())
	require.False(t, gjson.GetBytes(body, "reference_images.0.image_url").Exists())
	require.Equal(t, "image_url", gjson.GetBytes(body, "reference_images.0.type").String())
}

func TestPrepareGrokImageEditUsesOfficialImageObjectsAndEnforcesLimit(t *testing.T) {
	t.Parallel()
	body, _, err := prepareGrokMediaForwardBody(
		GrokMediaEndpointImagesEdits,
		[]byte(`{
			"model":"grok-imagine-edit",
			"image":["https://example.test/a.png",{"image_url":"https://example.test/b.png"}],
			"mask":{"url":"https://example.test/mask.png"}
		}`),
		"application/json",
	)
	require.NoError(t, err)
	require.Equal(t, "https://example.test/a.png", gjson.GetBytes(body, "image.0.url").String())
	require.Equal(t, "image_url", gjson.GetBytes(body, "image.0.type").String())
	require.Equal(t, "https://example.test/b.png", gjson.GetBytes(body, "image.1.url").String())
	require.Equal(t, "https://example.test/mask.png", gjson.GetBytes(body, "mask.url").String())

	_, _, err = prepareGrokMediaForwardBody(
		GrokMediaEndpointImagesEdits,
		[]byte(`{"image":["a","b","c","d"]}`),
		"application/json",
	)
	require.ErrorContains(t, err, "maximum of 3")
}

func TestExtractGrokMediaVideoRequestIDSupportsTaskID(t *testing.T) {
	t.Parallel()
	require.Equal(t, "task-top", extractGrokMediaVideoRequestID([]byte(`{"task_id":"task-top"}`)))
	require.Equal(t, "task-data", extractGrokMediaVideoRequestID([]byte(`{"data":{"task_id":"task-data"}}`)))
	require.Equal(t, "task-video", extractGrokMediaVideoRequestID([]byte(`{"video":{"task_id":"task-video"}}`)))
}
