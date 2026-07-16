package service

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeOpenAIResponsesRequest_MetadataContentAndFunctionOutput(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.1",
		"stream":true,
		"prompt_cache_key":" client-cache ",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"old user text"}]},
			{"type":"message","role":"assistant","content":"assistant text"},
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"latest user text"},
				{"type":"input_image","image_url":"https://example.com/image.png"}
			]},
			{"type":"function_call_output","call_id":"call_1","output":"ok"},
			{"type":"item_reference","id":"call_1"}
		]
	}`)

	analysis, err := AnalyzeOpenAIResponsesRequest(body)
	require.NoError(t, err)
	require.True(t, analysis.ModelExists)
	require.Equal(t, "gpt-5.1", analysis.Model)
	require.True(t, analysis.Stream)
	require.Equal(t, "client-cache", analysis.PromptCacheKey)
	moderationInput := analysis.ContentModerationInputCopy()
	require.Equal(t, "latest user text", moderationInput.Text)
	require.Equal(t, []string{"https://example.com/image.png"}, moderationInput.Images)
	require.True(t, analysis.FunctionCallOutputValidation.HasFunctionCallOutput)
	require.True(t, analysis.FunctionCallOutputValidation.HasItemReferenceForAllCallIDs)
}

func TestAnalyzeOpenAIResponsesRequest_InvalidFieldTypes(t *testing.T) {
	_, err := AnalyzeOpenAIResponsesRequest([]byte(`{"model":"gpt-5.1","stream":"true"}`))
	require.True(t, errors.Is(err, ErrOpenAIResponsesInvalidStreamFieldType))

	_, err = AnalyzeOpenAIResponsesRequest([]byte(`{"model":123}`))
	require.True(t, errors.Is(err, ErrOpenAIResponsesInvalidModelFieldType))
}

func TestAnalyzeOpenAIResponsesRequest_PreservesFirstDuplicateTopLevelField(t *testing.T) {
	analysis, err := AnalyzeOpenAIResponsesRequest([]byte(`{
		"model":"first-model",
		"model":"second-model",
		"stream":false,
		"stream":true,
		"input":"first input",
		"input":"second input"
	}`))
	require.NoError(t, err)
	require.Equal(t, "first-model", analysis.Model)
	require.False(t, analysis.Stream)
	require.Equal(t, "first input", analysis.ContentModerationInputCopy().Text)
}

func TestAnalyzeOpenAIResponsesRequest_CyberAndStandardInputsKeepDistinctSemantics(t *testing.T) {
	analysis, err := AnalyzeOpenAIResponsesRequest([]byte(`{
		"model":"gpt-5.1",
		"instructions":"system instruction",
		"input":[
			{"type":"message","role":"user","content":"old user input"},
			{"type":"message","role":"developer","content":"developer input"},
			{"type":"message","role":"user","content":"latest user input"},
			{"type":"input_text","text":"unscoped input"}
		]
	}`))
	require.NoError(t, err)

	standard := analysis.ContentModerationInputCopy()
	require.Equal(t, "unscoped input", standard.Text)

	cyber := analysis.CyberPreflightInputCopy()
	require.Contains(t, cyber.Text, "system instruction")
	require.Contains(t, cyber.Text, "old user input")
	require.Contains(t, cyber.Text, "developer input")
	require.Contains(t, cyber.Text, "latest user input")
	require.NotContains(t, cyber.Text, "unscoped input")
}

func TestAnalyzeOpenAIResponsesRequest_FunctionOutputToolContextShortCircuitsReferenceRequirement(t *testing.T) {
	analysis, err := AnalyzeOpenAIResponsesRequest([]byte(`{
		"model":"gpt-5.1",
		"input":[
			{"type":"function_call_output","output":"ok"},
			{"type":"function_call","call_id":"call_1"}
		]
	}`))
	require.NoError(t, err)
	require.Equal(t, FunctionCallOutputValidation{
		HasFunctionCallOutput: true,
		HasToolCallContext:    true,
	}, analysis.FunctionCallOutputValidation)
}

func TestAnalyzeOpenAIResponsesRequest_BoundsModerationTextWhileKeepingImage(t *testing.T) {
	longText := strings.Repeat("word ", maxModerationInputRunes)
	body := []byte(`{
		"model":"gpt-5.1",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"` + longText + `"},
				{"type":"input_image","image_url":"https://example.com/after-limit.png"}
			]
		}]
	}`)

	analysis, err := AnalyzeOpenAIResponsesRequest(body)
	require.NoError(t, err)
	content := analysis.ContentModerationInputCopy()
	require.Equal(t, maxModerationInputRunes, utf8.RuneCountInString(content.Text))
	require.Equal(t, []string{"https://example.com/after-limit.png"}, content.Images)
}

func TestAnalyzeOpenAIResponsesRequest_BoundedImagesKeepStableFullInputHash(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.1",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"inspect images"},
				{"type":"input_image","image_url":"https://example.com/first.png"},
				{"type":"input_image","image_url":"https://example.com/second.png"},
				{"type":"input_image","image_url":"https://example.com/first.png"}
			]
		}]
	}`)
	want := ContentModerationInput{
		Text: "inspect images",
		Images: []string{
			"https://example.com/first.png",
			"https://example.com/second.png",
		},
	}
	want.Normalize()

	for range 32 {
		analysis, err := AnalyzeOpenAIResponsesRequest(body)
		require.NoError(t, err)
		content := analysis.ContentModerationInputCopy()
		require.Len(t, content.Images, 1)
		require.Contains(t, want.Images, content.Images[0])
		require.Equal(t, want.Hash(), content.Hash())
	}
}

func TestAnalyzeOpenAIResponsesRequest_BoundedDataURIKeepsLegacyDedupHash(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.1",
		"input":[{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"inspect data image"},
				{"type":"input_image","source":{"media_type":"image/png","data":"QUJD"}},
				{"type":"input_image","source":{"media_type":"image/png","data":"QUJD"}}
			]
		}]
	}`)
	want := ContentModerationInput{
		Text:   "inspect data image",
		Images: []string{"data:image/png;base64,QUJD"},
	}
	want.Normalize()

	analysis, err := AnalyzeOpenAIResponsesRequest(body)
	require.NoError(t, err)
	content := analysis.ContentModerationInputCopy()
	require.Equal(t, want.Images, content.Images)
	require.Equal(t, want.Hash(), content.Hash())
}
