package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIRequestViewExtractsScalars(t *testing.T) {
	view := newOpenAIRequestView([]byte(`{"model":" gpt-5 ","stream":true,"prompt_cache_key":" cache-1 ","previous_response_id":" resp-1 ","service_tier":" fast ","reasoning":{"effort":" medium "}}`))

	require.Equal(t, "gpt-5", view.Model)
	require.True(t, view.Stream)
	require.Equal(t, "cache-1", view.PromptCacheKey)
	require.Equal(t, "resp-1", view.PreviousResponseID)
	require.Equal(t, "fast", view.ServiceTier)
	require.Equal(t, "medium", view.ReasoningEffort)
}

func TestOpenAIRequestViewKeepsFirstDuplicateTopLevelField(t *testing.T) {
	view := newOpenAIRequestView([]byte(`{"model":"first","model":"second","stream":false,"stream":true,"reasoning":{"effort":"low"},"reasoning":{"effort":"high"}}`))

	require.Equal(t, "first", view.Model)
	require.False(t, view.Stream)
	require.Equal(t, "low", view.ReasoningEffort)
}

func TestOpenAIRequestViewDecodeKeepsExistingNumberSemantics(t *testing.T) {
	view := newOpenAIRequestView([]byte(`{"model":"gpt-5","max_output_tokens":256}`))

	reqBody, err := view.Decode(nil)
	require.NoError(t, err)
	require.IsType(t, float64(0), reqBody["max_output_tokens"])
	require.Equal(t, float64(256), reqBody["max_output_tokens"])
}

func TestOpenAIRequestViewAppliesMultipleRawPatchesInOrder(t *testing.T) {
	view := newOpenAIRequestView([]byte(`{"model":"gpt-5","previous_response_id":"resp-1","reasoning":{"effort":"minimal"},"opaque":9007199254740993}`))
	view.MarkPatchSet("model", "gpt-5.1")
	view.MarkPatchDelete("previous_response_id")
	view.MarkPatchSet("reasoning.effort", "none")

	patched, err := view.ApplyPatches()
	require.NoError(t, err)
	require.Equal(t, "gpt-5.1", gjson.GetBytes(patched, "model").String())
	require.False(t, gjson.GetBytes(patched, "previous_response_id").Exists())
	require.Equal(t, "none", gjson.GetBytes(patched, "reasoning.effort").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(patched, "opaque").Raw)
}

func TestOpenAIRequestViewAppliesRepeatedPatchToSamePath(t *testing.T) {
	view := newOpenAIRequestView([]byte(`{"model":"gpt-5"}`))
	view.MarkPatchSet("model", "gpt-5.1")
	view.MarkPatchSet("model", "gpt-5.2")

	patched, err := view.ApplyPatches()
	require.NoError(t, err)
	require.Equal(t, "gpt-5.2", gjson.GetBytes(patched, "model").String())
}

func TestOpenAIRequestViewComplexPathDisablesRawPatches(t *testing.T) {
	tests := []string{
		"",
		"reasoning..effort",
		`metadata.user\.id`,
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			view := newOpenAIRequestView([]byte(`{"model":"gpt-5"}`))
			view.MarkPatchSet("model", "gpt-5.1")
			view.MarkPatchSet(path, "value")

			require.False(t, view.HasPatches())
			_, err := view.ApplyPatches()
			require.Error(t, err)
		})
	}
}

func TestOpenAIRequestViewDisablePatchesClearsQueuedOperations(t *testing.T) {
	view := newOpenAIRequestView([]byte(`{"model":"gpt-5"}`))
	view.MarkPatchSet("model", "gpt-5.1")
	require.True(t, view.HasPatches())

	view.DisablePatches()
	require.False(t, view.HasPatches())
	_, err := view.ApplyPatches()
	require.Error(t, err)
}

func TestOpenAIRequestBodyHasImageGenerationDeclaration(t *testing.T) {
	require.True(t, openAIRequestBodyHasImageGenerationDeclaration([]byte(`{"tools":[{"type":"image_generation"}]}`)))
	require.True(t, openAIRequestBodyHasImageGenerationDeclaration([]byte(`{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen"}]}]}`)))
	require.True(t, openAIRequestBodyHasImageGenerationDeclaration([]byte(`{"tool_choice":{"type":"image_generation"}}`)))
	require.False(t, openAIRequestBodyHasImageGenerationDeclaration([]byte(`{"tools":[{"type":"web_search"}]}`)))
}

func TestOpenAIRequestMapPathHelpers(t *testing.T) {
	reqBody := map[string]any{"reasoning": map[string]any{"effort": "low"}}

	setOpenAIRequestMapPath(reqBody, "reasoning.effort", "high")
	setOpenAIRequestMapPath(reqBody, "text.verbosity", "medium")
	require.Equal(t, "high", reqBody["reasoning"].(map[string]any)["effort"])
	require.Equal(t, "medium", reqBody["text"].(map[string]any)["verbosity"])

	deleteOpenAIRequestMapPath(reqBody, "reasoning.effort")
	deleteOpenAIRequestMapPath(reqBody, "missing.child")
	require.NotContains(t, reqBody["reasoning"].(map[string]any), "effort")
}
