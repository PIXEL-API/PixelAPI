package service

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// openAIRequestView keeps the raw request body as the canonical representation.
// Callers can inspect the small set of routing fields without decoding the full
// Responses payload and can accumulate simple object-field rewrites as raw JSON
// patches. Complex mutations must call Decode and DisablePatches.
// Scalar metadata owns its strings because routing and billing may retain it
// after the request body and its raw JSON view are no longer needed.
type openAIRequestView struct {
	body               []byte
	root               gjson.Result
	Model              string
	Stream             bool
	PromptCacheKey     string
	PreviousResponseID string
	ServiceTier        string
	ReasoningEffort    string
	patches            []openAIRequestPatch
	patchesDisabled    bool
}

type openAIRequestPatch struct {
	path   string
	delete bool
	value  any
}

func newOpenAIRequestView(body []byte) openAIRequestView {
	if len(body) == 0 {
		return openAIRequestView{}
	}

	const (
		modelField uint8 = 1 << iota
		streamField
		promptCacheKeyField
		previousResponseIDField
		serviceTierField
		reasoningField
		allRequestViewFields = modelField | streamField | promptCacheKeyField |
			previousResponseIDField | serviceTierField | reasoningField
	)

	root := parseRawJSONView(body)
	view := openAIRequestView{body: body, root: root}
	var seen uint8
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			if seen&modelField == 0 {
				view.Model = strings.Clone(strings.TrimSpace(value.String()))
				seen |= modelField
			}
		case "stream":
			if seen&streamField == 0 {
				view.Stream = value.Bool()
				seen |= streamField
			}
		case "prompt_cache_key":
			if seen&promptCacheKeyField == 0 {
				view.PromptCacheKey = strings.Clone(strings.TrimSpace(value.String()))
				seen |= promptCacheKeyField
			}
		case "previous_response_id":
			if seen&previousResponseIDField == 0 {
				view.PreviousResponseID = strings.Clone(strings.TrimSpace(value.String()))
				seen |= previousResponseIDField
			}
		case "service_tier":
			if seen&serviceTierField == 0 {
				view.ServiceTier = strings.Clone(strings.TrimSpace(value.String()))
				seen |= serviceTierField
			}
		case "reasoning":
			if seen&reasoningField == 0 {
				view.ReasoningEffort = strings.Clone(strings.TrimSpace(value.Get("effort").String()))
				seen |= reasoningField
			}
		}
		return seen != allRequestViewFields
	})
	return view
}

func (v openAIRequestView) Get(path string) gjson.Result {
	if !v.root.Exists() {
		return gjson.Result{}
	}
	return v.root.Get(path)
}

// parseRawJSONView is a synchronous, read-only view over raw JSON. The caller
// must keep raw alive while using the result. Avoiding gjson.ParseBytes here is
// important because ParseBytes copies the complete payload into a string.
func parseRawJSONView(raw []byte) gjson.Result {
	if len(raw) == 0 {
		return gjson.Result{}
	}
	return gjson.Parse(unsafe.String(unsafe.SliceData(raw), len(raw)))
}

func openAIRequestBodyHasImageGenerationDeclaration(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	root := parseRawJSONView(body)
	return openAIJSONToolsContainImageGeneration(root.Get("tools")) ||
		openAIJSONInputContainsImageGenTool(root.Get("input")) ||
		openAIJSONToolChoiceSelectsImageGeneration(root.Get("tool_choice"))
}

// Decode deliberately delegates to the existing decoder so complex branches
// keep this fork's current json.Unmarshal number semantics.
func (v openAIRequestView) Decode(c *gin.Context) (map[string]any, error) {
	return getOpenAIRequestBodyMap(c, v.body)
}

func (v *openAIRequestView) MarkPatchSet(path string, value any) {
	if v == nil || v.patchesDisabled {
		return
	}
	path = strings.TrimSpace(path)
	if !isSimpleOpenAIRequestPatchPath(path) {
		v.DisablePatches()
		return
	}
	v.patches = append(v.patches, openAIRequestPatch{path: path, value: value})
}

func (v *openAIRequestView) MarkPatchDelete(path string) {
	if v == nil || v.patchesDisabled {
		return
	}
	path = strings.TrimSpace(path)
	if !isSimpleOpenAIRequestPatchPath(path) {
		v.DisablePatches()
		return
	}
	v.patches = append(v.patches, openAIRequestPatch{path: path, delete: true})
}

func isSimpleOpenAIRequestPatchPath(path string) bool {
	if path == "" || strings.ContainsRune(path, '\\') {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func (v *openAIRequestView) DisablePatches() {
	if v == nil {
		return
	}
	v.patchesDisabled = true
	v.patches = nil
}

func (v openAIRequestView) HasPatches() bool {
	return !v.patchesDisabled && len(v.patches) > 0
}

func (v openAIRequestView) ApplyPatches() ([]byte, error) {
	if v.patchesDisabled || len(v.patches) == 0 {
		return nil, errors.New("openai request patches disabled")
	}

	body := v.body
	for _, patch := range v.patches {
		var err error
		if patch.delete {
			body, err = sjson.DeleteBytes(body, patch.path)
		} else {
			body, err = sjson.SetBytes(body, patch.path, patch.value)
		}
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

func setOpenAIRequestMapPath(reqBody map[string]any, path string, value any) {
	path = strings.TrimSpace(path)
	if reqBody == nil || path == "" {
		return
	}

	parts := strings.Split(path, ".")
	current := reqBody
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		next, _ := current[part].(map[string]any)
		if next == nil {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last != "" {
		current[last] = value
	}
}

func deleteOpenAIRequestMapPath(reqBody map[string]any, path string) {
	path = strings.TrimSpace(path)
	if reqBody == nil || path == "" {
		return
	}

	parts := strings.Split(path, ".")
	current := reqBody
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		next, _ := current[part].(map[string]any)
		if next == nil {
			return
		}
		current = next
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last != "" {
		delete(current, last)
	}
}
