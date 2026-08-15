package handler

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractGrokWebSearchSourcesRequiresNativeSourceWhitelist(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"output":[
			{"type":"web_search_call","action":{"sources":[
				{"url":"HTTPS://Example.COM/path?q=1#native","title":"Native title","snippet":"Native snippet"}
			]}},
			{"type":"message","content":[{"type":"output_text","text":"{\"results\":[{\"url\":\"https://example.com/path?q=1#model\",\"title\":\"\",\"snippet\":\"\"},{\"url\":\"https://hallucinated.example/post\",\"title\":\"Fake\",\"snippet\":\"Fake\"}]}"}]}
		]
	}`)

	results := extractGrokWebSearchSources(body, 5)
	require.Len(t, results, 1)
	require.Equal(t, "https://example.com/path?q=1", results[0].URL)
	require.Equal(t, "Native title", results[0].Title)
	require.Equal(t, "Native snippet", results[0].Snippet)
}

func TestExtractGrokWebSearchSourcesSupportsWebAndXSourcesAndDeduplicates(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"output":[
			{"type":"web_search_call","action":{"sources":[
				{"url":"https://EXAMPLE.com/item#web","title":"Example"},
				{"url":"ftp://example.com/rejected"},
				{"url":"https:///missing-host"}
			]}},
			{"type":"x_search_call","action":{"sources":[
				{"url":"https://example.com/item#x","snippet":"duplicate"},
				{"url":"https://x.com/xai/status/1","title":"X post"}
			]}}
		]
	}`)

	results := extractGrokWebSearchSources(body, 20)
	require.Len(t, results, 2)
	require.Equal(t, "https://example.com/item", results[0].URL)
	require.Equal(t, "Example", results[0].Title)
	require.Equal(t, "duplicate", results[0].Snippet)
	require.Equal(t, "https://x.com/xai/status/1", results[1].URL)
}

func TestExtractGrokWebSearchSourcesSupportsAnnotationsAndNestedResponse(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"response":{"output":[
			{"type":"message","content":[{"type":"output_text","text":"no structured result","annotations":[
				{"type":"url_citation","url":"https://docs.example/a#section","title":"Docs"},
				{"type":"web","url":"https://news.example/b","title":"News","snippet":"Summary"},
				{"type":"other","url":"https://ignored.example/c"}
			]}]}
		]},
		"output":[{"type":"web_search_call","action":{"sources":[{"url":"https://duplicate-container.example"}]}}]
	}`)

	results := extractGrokWebSearchSources(body, 5)
	require.Len(t, results, 2)
	require.Equal(t, "https://docs.example/a", results[0].URL)
	require.Equal(t, "Docs", results[0].Title)
	require.Equal(t, "https://news.example/b", results[1].URL)
	require.Equal(t, "Summary", results[1].Snippet)
}

func TestExtractGrokWebSearchSourcesHonorsMaxResults(t *testing.T) {
	t.Parallel()
	sources := make([]map[string]string, 0, maxGrokWebSearchResults+5)
	for index := 0; index < maxGrokWebSearchResults+5; index++ {
		sources = append(sources, map[string]string{"url": fmt.Sprintf("https://example.com/%d", index)})
	}
	body, err := json.Marshal(map[string]any{
		"output": []any{map[string]any{
			"type":   "web_search_call",
			"action": map[string]any{"sources": sources},
		}},
	})
	require.NoError(t, err)

	require.Len(t, extractGrokWebSearchSources(body, 2), 2)
	require.Len(t, extractGrokWebSearchSources(body, maxGrokWebSearchResults+100), maxGrokWebSearchResults)
}

func TestExtractGrokWebSearchSourcesRejectsInvalidPayloads(t *testing.T) {
	t.Parallel()
	require.Nil(t, extractGrokWebSearchSources(nil, 5))
	require.Nil(t, extractGrokWebSearchSources([]byte(`not-json`), 5))
	require.Empty(t, extractGrokWebSearchSources([]byte(`{"output":[]}`), 5))
}

func TestNormalizeGrokWebSearchMaxResults(t *testing.T) {
	t.Parallel()
	require.Equal(t, defaultGrokWebSearchResults, normalizeGrokWebSearchMaxResults(0))
	require.Equal(t, defaultGrokWebSearchResults, normalizeGrokWebSearchMaxResults(-1))
	require.Equal(t, 3, normalizeGrokWebSearchMaxResults(3))
	require.Equal(t, maxGrokWebSearchResults, normalizeGrokWebSearchMaxResults(maxGrokWebSearchResults+1))
}
