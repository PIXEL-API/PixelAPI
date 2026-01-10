package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	codexReleaseAPIURL     = "https://api.github.com/repos/openai/codex/releases/latest"
	codexReleaseHTMLURL    = "https://github.com/openai/codex/releases/latest"
	codexPromptURLFmt      = "https://raw.githubusercontent.com/openai/codex/%s/codex-rs/core/%s"
	opencodeCodexURL       = "https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/opencode/src/session/prompt/codex.txt"
	opencodeCodexHeaderURL = "https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/opencode/src/session/prompt/codex_header.txt"
	codexCacheTTL          = 15 * time.Minute
)

type codexModelFamily string

const (
	codexFamilyGpt52Codex codexModelFamily = "gpt-5.2-codex"
	codexFamilyCodexMax   codexModelFamily = "codex-max"
	codexFamilyCodex      codexModelFamily = "codex"
	codexFamilyGpt52      codexModelFamily = "gpt-5.2"
	codexFamilyGpt51      codexModelFamily = "gpt-5.1"
)

var codexPromptFiles = map[codexModelFamily]string{
	codexFamilyGpt52Codex: "gpt-5.2-codex_prompt.md",
	codexFamilyCodexMax:   "gpt-5.1-codex-max_prompt.md",
	codexFamilyCodex:      "gpt_5_codex_prompt.md",
	codexFamilyGpt52:      "gpt_5_2_prompt.md",
	codexFamilyGpt51:      "gpt_5_1_prompt.md",
}

var codexCacheFiles = map[codexModelFamily]string{
	codexFamilyGpt52Codex: "gpt-5.2-codex-instructions.md",
	codexFamilyCodexMax:   "codex-max-instructions.md",
	codexFamilyCodex:      "codex-instructions.md",
	codexFamilyGpt52:      "gpt-5.2-instructions.md",
	codexFamilyGpt51:      "gpt-5.1-instructions.md",
}

var codexModelMap = map[string]string{
	"gpt-5.1-codex":             "gpt-5.1-codex",
	"gpt-5.1-codex-low":         "gpt-5.1-codex",
	"gpt-5.1-codex-medium":      "gpt-5.1-codex",
	"gpt-5.1-codex-high":        "gpt-5.1-codex",
	"gpt-5.1-codex-max":         "gpt-5.1-codex-max",
	"gpt-5.1-codex-max-low":     "gpt-5.1-codex-max",
	"gpt-5.1-codex-max-medium":  "gpt-5.1-codex-max",
	"gpt-5.1-codex-max-high":    "gpt-5.1-codex-max",
	"gpt-5.1-codex-max-xhigh":   "gpt-5.1-codex-max",
	"gpt-5.2":                   "gpt-5.2",
	"gpt-5.2-none":              "gpt-5.2",
	"gpt-5.2-low":               "gpt-5.2",
	"gpt-5.2-medium":            "gpt-5.2",
	"gpt-5.2-high":              "gpt-5.2",
	"gpt-5.2-xhigh":             "gpt-5.2",
	"gpt-5.2-codex":             "gpt-5.2-codex",
	"gpt-5.2-codex-low":         "gpt-5.2-codex",
	"gpt-5.2-codex-medium":      "gpt-5.2-codex",
	"gpt-5.2-codex-high":        "gpt-5.2-codex",
	"gpt-5.2-codex-xhigh":       "gpt-5.2-codex",
	"gpt-5.1-codex-mini":        "gpt-5.1-codex-mini",
	"gpt-5.1-codex-mini-medium": "gpt-5.1-codex-mini",
	"gpt-5.1-codex-mini-high":   "gpt-5.1-codex-mini",
	"gpt-5.1":                   "gpt-5.1",
	"gpt-5.1-none":              "gpt-5.1",
	"gpt-5.1-low":               "gpt-5.1",
	"gpt-5.1-medium":            "gpt-5.1",
	"gpt-5.1-high":              "gpt-5.1",
	"gpt-5.1-chat-latest":       "gpt-5.1",
	"gpt-5-codex":               "gpt-5.1-codex",
	"codex-mini-latest":         "gpt-5.1-codex-mini",
	"gpt-5-codex-mini":          "gpt-5.1-codex-mini",
	"gpt-5-codex-mini-medium":   "gpt-5.1-codex-mini",
	"gpt-5-codex-mini-high":     "gpt-5.1-codex-mini",
	"gpt-5":                     "gpt-5.1",
	"gpt-5-mini":                "gpt-5.1",
	"gpt-5-nano":                "gpt-5.1",
}

var opencodePromptSignatures = []string{
	"you are a coding agent running in the opencode",
	"you are opencode, an agent",
	"you are opencode, an interactive cli agent",
	"you are opencode, an interactive cli tool",
	"you are opencode, the best coding agent on the planet",
}

var opencodeContextMarkers = []string{
	"here is some useful information about the environment you are running in:",
	"<env>",
	"instructions from:",
	"<instructions>",
}

type codexTransformResult struct {
	Modified        bool
	NormalizedModel string
	PromptCacheKey  string
}

type codexCacheMetadata struct {
	ETag        string `json:"etag"`
	Tag         string `json:"tag"`
	LastChecked int64  `json:"lastChecked"`
	URL         string `json:"url"`
}

type opencodeCacheMetadata struct {
	ETag        string `json:"etag"`
	LastFetch   string `json:"lastFetch,omitempty"`
	LastChecked int64  `json:"lastChecked"`
}

func codexModeEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CODEX_MODE"))
	if value == "" {
		return true
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return true
	}
}

func applyCodexOAuthTransform(reqBody map[string]any) codexTransformResult {
	result := codexTransformResult{}

	model := ""
	if v, ok := reqBody["model"].(string); ok {
		model = v
	}
	normalizedModel := normalizeCodexModel(model)
	if normalizedModel != "" {
		if model != normalizedModel {
			reqBody["model"] = normalizedModel
			result.Modified = true
		}
		result.NormalizedModel = normalizedModel
	}

	if v, ok := reqBody["store"].(bool); !ok || v {
		reqBody["store"] = false
		result.Modified = true
	}
	if v, ok := reqBody["stream"].(bool); !ok || !v {
		reqBody["stream"] = true
		result.Modified = true
	}

	if _, ok := reqBody["max_output_tokens"]; ok {
		delete(reqBody, "max_output_tokens")
		result.Modified = true
	}
	if _, ok := reqBody["max_completion_tokens"]; ok {
		delete(reqBody, "max_completion_tokens")
		result.Modified = true
	}

	if normalizeCodexTools(reqBody) {
		result.Modified = true
	}

	if v, ok := reqBody["prompt_cache_key"].(string); ok {
		result.PromptCacheKey = strings.TrimSpace(v)
	}

	instructions := strings.TrimSpace(getOpenCodeCodexHeader())
	existingInstructions, _ := reqBody["instructions"].(string)
	existingInstructions = strings.TrimSpace(existingInstructions)

	if instructions != "" {
		if existingInstructions != "" && existingInstructions != instructions {
			if input, ok := reqBody["input"].([]any); ok {
				reqBody["input"] = prependSystemInstruction(input, existingInstructions)
				result.Modified = true
			}
		}
		if existingInstructions != instructions {
			reqBody["instructions"] = instructions
			result.Modified = true
		}
	}

	if input, ok := reqBody["input"].([]any); ok {
		input = filterCodexInput(input)
		input = normalizeOrphanedToolOutputs(input)
		reqBody["input"] = input
		result.Modified = true
	}

	return result
}

func normalizeCodexModel(model string) string {
	if model == "" {
		return "gpt-5.1"
	}

	modelID := model
	if strings.Contains(modelID, "/") {
		parts := strings.Split(modelID, "/")
		modelID = parts[len(parts)-1]
	}

	if mapped := getNormalizedCodexModel(modelID); mapped != "" {
		return mapped
	}

	normalized := strings.ToLower(modelID)

	if strings.Contains(normalized, "gpt-5.2-codex") || strings.Contains(normalized, "gpt 5.2 codex") {
		return "gpt-5.2-codex"
	}
	if strings.Contains(normalized, "gpt-5.2") || strings.Contains(normalized, "gpt 5.2") {
		return "gpt-5.2"
	}
	if strings.Contains(normalized, "gpt-5.1-codex-max") || strings.Contains(normalized, "gpt 5.1 codex max") {
		return "gpt-5.1-codex-max"
	}
	if strings.Contains(normalized, "gpt-5.1-codex-mini") || strings.Contains(normalized, "gpt 5.1 codex mini") {
		return "gpt-5.1-codex-mini"
	}
	if strings.Contains(normalized, "codex-mini-latest") ||
		strings.Contains(normalized, "gpt-5-codex-mini") ||
		strings.Contains(normalized, "gpt 5 codex mini") {
		return "codex-mini-latest"
	}
	if strings.Contains(normalized, "gpt-5.1-codex") || strings.Contains(normalized, "gpt 5.1 codex") {
		return "gpt-5.1-codex"
	}
	if strings.Contains(normalized, "gpt-5.1") || strings.Contains(normalized, "gpt 5.1") {
		return "gpt-5.1"
	}
	if strings.Contains(normalized, "codex") {
		return "gpt-5.1-codex"
	}
	if strings.Contains(normalized, "gpt-5") || strings.Contains(normalized, "gpt 5") {
		return "gpt-5.1"
	}

	return "gpt-5.1"
}

func getNormalizedCodexModel(modelID string) string {
	if modelID == "" {
		return ""
	}
	if mapped, ok := codexModelMap[modelID]; ok {
		return mapped
	}
	lower := strings.ToLower(modelID)
	for key, value := range codexModelMap {
		if strings.ToLower(key) == lower {
			return value
		}
	}
	return ""
}

func getCodexModelFamily(normalizedModel string) codexModelFamily {
	model := strings.ToLower(normalizedModel)
	if strings.Contains(model, "gpt-5.2-codex") || strings.Contains(model, "gpt 5.2 codex") {
		return codexFamilyGpt52Codex
	}
	if strings.Contains(model, "codex-max") {
		return codexFamilyCodexMax
	}
	if strings.Contains(model, "codex") || strings.HasPrefix(model, "codex-") {
		return codexFamilyCodex
	}
	if strings.Contains(model, "gpt-5.2") {
		return codexFamilyGpt52
	}
	return codexFamilyGpt51
}

func getCodexInstructions(normalizedModel string) string {
	if normalizedModel == "" {
		normalizedModel = "gpt-5.1-codex"
	}

	modelFamily := getCodexModelFamily(normalizedModel)
	promptFile := codexPromptFiles[modelFamily]
	cacheFile := codexCachePath(codexCacheFiles[modelFamily])
	metaFile := codexCachePath(strings.TrimSuffix(codexCacheFiles[modelFamily], ".md") + "-meta.json")

	var meta codexCacheMetadata
	if loadJSON(metaFile, &meta) && meta.LastChecked > 0 {
		if time.Since(time.UnixMilli(meta.LastChecked)) < codexCacheTTL {
			if cached, ok := readFile(cacheFile); ok {
				return cached
			}
		}
	}

	latestTag, err := getLatestCodexReleaseTag()
	if err != nil {
		if cached, ok := readFile(cacheFile); ok {
			return cached
		}
		return ""
	}

	if meta.Tag != latestTag {
		meta.ETag = ""
	}

	promptURL := fmt.Sprintf(codexPromptURLFmt, latestTag, promptFile)
	content, etag, status, err := fetchWithETag(promptURL, meta.ETag)
	if err == nil && status == http.StatusNotModified {
		if cached, ok := readFile(cacheFile); ok {
			return cached
		}
	}
	if err == nil && status >= 200 && status < 300 {
		if content != "" {
			if err := writeFile(cacheFile, content); err == nil {
				meta = codexCacheMetadata{
					ETag:        etag,
					Tag:         latestTag,
					LastChecked: time.Now().UnixMilli(),
					URL:         promptURL,
				}
				_ = writeJSON(metaFile, meta)
			}
			return content
		}
	}

	if cached, ok := readFile(cacheFile); ok {
		return cached
	}

	return ""
}

func getLatestCodexReleaseTag() (string, error) {
	body, _, status, err := fetchWithETag(codexReleaseAPIURL, "")
	if err == nil && status >= 200 && status < 300 && body != "" {
		var data struct {
			TagName string `json:"tag_name"`
		}
		if json.Unmarshal([]byte(body), &data) == nil && data.TagName != "" {
			return data.TagName, nil
		}
	}

	resp, err := http.Get(codexReleaseHTMLURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if finalURL != "" {
		if tag := parseReleaseTagFromURL(finalURL); tag != "" {
			return tag, nil
		}
	}

	html, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return parseReleaseTagFromHTML(string(html))
}

func parseReleaseTagFromURL(url string) string {
	parts := strings.Split(url, "/tag/")
	if len(parts) < 2 {
		return ""
	}
	tag := parts[len(parts)-1]
	if tag == "" || strings.Contains(tag, "/") {
		return ""
	}
	return tag
}

func parseReleaseTagFromHTML(html string) (string, error) {
	const marker = "/openai/codex/releases/tag/"
	idx := strings.Index(html, marker)
	if idx == -1 {
		return "", fmt.Errorf("release tag not found")
	}
	rest := html[idx+len(marker):]
	for i, r := range rest {
		if r == '"' || r == '\'' {
			return rest[:i], nil
		}
	}
	return "", fmt.Errorf("release tag not found")
}

func getOpenCodeCachedPrompt(url, cacheFileName, metaFileName string) string {
	cacheDir := codexCachePath("")
	if cacheDir == "" {
		return ""
	}
	cacheFile := filepath.Join(cacheDir, cacheFileName)
	metaFile := filepath.Join(cacheDir, metaFileName)

	var cachedContent string
	if content, ok := readFile(cacheFile); ok {
		cachedContent = content
	}

	var meta opencodeCacheMetadata
	if loadJSON(metaFile, &meta) && meta.LastChecked > 0 && cachedContent != "" {
		if time.Since(time.UnixMilli(meta.LastChecked)) < codexCacheTTL {
			return cachedContent
		}
	}

	content, etag, status, err := fetchWithETag(url, meta.ETag)
	if err == nil && status == http.StatusNotModified && cachedContent != "" {
		return cachedContent
	}
	if err == nil && status >= 200 && status < 300 && content != "" {
		_ = writeFile(cacheFile, content)
		meta = opencodeCacheMetadata{
			ETag:        etag,
			LastFetch:   time.Now().UTC().Format(time.RFC3339),
			LastChecked: time.Now().UnixMilli(),
		}
		_ = writeJSON(metaFile, meta)
		return content
	}

	return cachedContent
}

func getOpenCodeCodexPrompt() string {
	return getOpenCodeCachedPrompt(opencodeCodexURL, "opencode-codex.txt", "opencode-codex-meta.json")
}

func getOpenCodeCodexHeader() string {
	return getOpenCodeCachedPrompt(opencodeCodexHeaderURL, "opencode-codex-header.txt", "opencode-codex-header-meta.json")
}

func filterCodexInput(input []any) []any {
	filtered := make([]any, 0, len(input))
	for _, item := range input {
		m, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if typ, ok := m["type"].(string); ok && typ == "item_reference" {
			continue
		}
		if _, ok := m["id"]; ok {
			delete(m, "id")
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func prependSystemInstruction(input []any, instructions string) []any {
	message := map[string]any{
		"role": "system",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": instructions,
			},
		},
	}
	return append([]any{message}, input...)
}

func filterOpenCodeSystemPromptsWithCachedPrompt(input []any, cachedPrompt string) []any {
	if len(input) == 0 {
		return input
	}
	cachedPrompt = strings.TrimSpace(cachedPrompt)

	result := make([]any, 0, len(input))
	for _, item := range input {
		m, ok := item.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}
		role, _ := m["role"].(string)
		if role == "user" {
			result = append(result, item)
			continue
		}
		if !isOpenCodeSystemPrompt(m, cachedPrompt) {
			result = append(result, item)
			continue
		}
		contentText := getContentText(m)
		if contentText == "" {
			continue
		}
		if preserved := extractOpenCodeContext(contentText); preserved != "" {
			result = append(result, replaceContentText(m, preserved))
		}
	}
	return result
}

func isOpenCodeSystemPrompt(item map[string]any, cachedPrompt string) bool {
	role, _ := item["role"].(string)
	if role != "developer" && role != "system" {
		return false
	}

	contentText := getContentText(item)
	if contentText == "" {
		return false
	}

	if cachedPrompt != "" {
		contentTrimmed := strings.TrimSpace(contentText)
		cachedTrimmed := strings.TrimSpace(cachedPrompt)
		if contentTrimmed == cachedTrimmed {
			return true
		}
		if strings.HasPrefix(contentTrimmed, cachedTrimmed) {
			return true
		}
		contentPrefix := contentTrimmed
		if len(contentPrefix) > 200 {
			contentPrefix = contentPrefix[:200]
		}
		cachedPrefix := cachedTrimmed
		if len(cachedPrefix) > 200 {
			cachedPrefix = cachedPrefix[:200]
		}
		if contentPrefix == cachedPrefix {
			return true
		}
	}

	normalized := strings.ToLower(strings.TrimLeftFunc(contentText, unicode.IsSpace))
	for _, signature := range opencodePromptSignatures {
		if strings.HasPrefix(normalized, signature) {
			return true
		}
	}
	return false
}

func getContentText(item map[string]any) string {
	content := item["content"]
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, part := range v {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := partMap["type"].(string)
			if typ != "input_text" {
				continue
			}
			if text, ok := partMap["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func replaceContentText(item map[string]any, contentText string) map[string]any {
	content := item["content"]
	switch content.(type) {
	case string:
		item["content"] = contentText
	case []any:
		item["content"] = []any{map[string]any{
			"type": "input_text",
			"text": contentText,
		}}
	default:
		item["content"] = contentText
	}
	return item
}

func extractOpenCodeContext(contentText string) string {
	lower := strings.ToLower(contentText)
	earliest := -1
	for _, marker := range opencodeContextMarkers {
		idx := strings.Index(lower, marker)
		if idx >= 0 && (earliest == -1 || idx < earliest) {
			earliest = idx
		}
	}
	if earliest == -1 {
		return ""
	}
	return strings.TrimLeftFunc(contentText[earliest:], unicode.IsSpace)
}

func addCodexBridgeMessage(input []any) []any {
	message := map[string]any{
		"type": "message",
		"role": "developer",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": codexOpenCodeBridge,
			},
		},
	}
	return append([]any{message}, input...)
}

func addToolRemapMessage(input []any) []any {
	message := map[string]any{
		"type": "message",
		"role": "developer",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": codexToolRemapMessage,
			},
		},
	}
	return append([]any{message}, input...)
}

func hasTools(reqBody map[string]any) bool {
	tools, ok := reqBody["tools"]
	if !ok || tools == nil {
		return false
	}
	if list, ok := tools.([]any); ok {
		return len(list) > 0
	}
	return true
}

func normalizeCodexTools(reqBody map[string]any) bool {
	rawTools, ok := reqBody["tools"]
	if !ok || rawTools == nil {
		return false
	}
	tools, ok := rawTools.([]any)
	if !ok {
		return false
	}

	modified := false
	for idx, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}

		toolType, _ := toolMap["type"].(string)
		if strings.TrimSpace(toolType) != "function" {
			continue
		}

		function, ok := toolMap["function"].(map[string]any)
		if !ok {
			continue
		}

		if _, ok := toolMap["name"]; !ok {
			if name, ok := function["name"].(string); ok && strings.TrimSpace(name) != "" {
				toolMap["name"] = name
				modified = true
			}
		}
		if _, ok := toolMap["description"]; !ok {
			if desc, ok := function["description"].(string); ok && strings.TrimSpace(desc) != "" {
				toolMap["description"] = desc
				modified = true
			}
		}
		if _, ok := toolMap["parameters"]; !ok {
			if params, ok := function["parameters"]; ok {
				toolMap["parameters"] = params
				modified = true
			}
		}
		if _, ok := toolMap["strict"]; !ok {
			if strict, ok := function["strict"]; ok {
				toolMap["strict"] = strict
				modified = true
			}
		}

		tools[idx] = toolMap
	}

	if modified {
		reqBody["tools"] = tools
	}

	return modified
}

func normalizeOrphanedToolOutputs(input []any) []any {
	functionCallIDs := map[string]bool{}
	localShellCallIDs := map[string]bool{}
	customToolCallIDs := map[string]bool{}

	for _, item := range input {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		callID := getCallID(m)
		if callID == "" {
			continue
		}
		switch m["type"] {
		case "function_call":
			functionCallIDs[callID] = true
		case "local_shell_call":
			localShellCallIDs[callID] = true
		case "custom_tool_call":
			customToolCallIDs[callID] = true
		}
	}

	output := make([]any, 0, len(input))
	for _, item := range input {
		m, ok := item.(map[string]any)
		if !ok {
			output = append(output, item)
			continue
		}
		switch m["type"] {
		case "function_call_output":
			callID := getCallID(m)
			if callID == "" || !(functionCallIDs[callID] || localShellCallIDs[callID]) {
				output = append(output, convertOrphanedOutputToMessage(m, callID))
				continue
			}
		case "custom_tool_call_output":
			callID := getCallID(m)
			if callID == "" || !customToolCallIDs[callID] {
				output = append(output, convertOrphanedOutputToMessage(m, callID))
				continue
			}
		case "local_shell_call_output":
			callID := getCallID(m)
			if callID == "" || !localShellCallIDs[callID] {
				output = append(output, convertOrphanedOutputToMessage(m, callID))
				continue
			}
		}
		output = append(output, m)
	}
	return output
}

func getCallID(item map[string]any) string {
	raw, ok := item["call_id"]
	if !ok {
		return ""
	}
	callID, ok := raw.(string)
	if !ok {
		return ""
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return ""
	}
	return callID
}

func convertOrphanedOutputToMessage(item map[string]any, callID string) map[string]any {
	toolName := "tool"
	if name, ok := item["name"].(string); ok && name != "" {
		toolName = name
	}
	labelID := callID
	if labelID == "" {
		labelID = "unknown"
	}
	text := stringifyOutput(item["output"])
	if len(text) > 16000 {
		text = text[:16000] + "\n...[truncated]"
	}
	return map[string]any{
		"type":    "message",
		"role":    "assistant",
		"content": fmt.Sprintf("[Previous %s result; call_id=%s]: %s", toolName, labelID, text),
	}
}

func stringifyOutput(output any) string {
	switch v := output.(type) {
	case string:
		return v
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprintf("%v", v)
	}
}

func resolveCodexReasoning(reqBody map[string]any, modelName string) (string, string) {
	existingEffort := getReasoningValue(reqBody, "effort", "reasoningEffort")
	existingSummary := getReasoningValue(reqBody, "summary", "reasoningSummary")
	return getReasoningConfig(modelName, existingEffort, existingSummary)
}

func getReasoningValue(reqBody map[string]any, field, providerField string) string {
	if reasoning, ok := reqBody["reasoning"].(map[string]any); ok {
		if value, ok := reasoning[field].(string); ok && value != "" {
			return value
		}
	}
	if provider := getProviderOpenAI(reqBody); provider != nil {
		if value, ok := provider[providerField].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func resolveTextVerbosity(reqBody map[string]any) string {
	if text, ok := reqBody["text"].(map[string]any); ok {
		if value, ok := text["verbosity"].(string); ok && value != "" {
			return value
		}
	}
	if provider := getProviderOpenAI(reqBody); provider != nil {
		if value, ok := provider["textVerbosity"].(string); ok && value != "" {
			return value
		}
	}
	return "medium"
}

func resolveInclude(reqBody map[string]any) []any {
	include := toStringSlice(reqBody["include"])
	if len(include) == 0 {
		if provider := getProviderOpenAI(reqBody); provider != nil {
			include = toStringSlice(provider["include"])
		}
	}
	if len(include) == 0 {
		include = []string{"reasoning.encrypted_content"}
	}

	unique := make(map[string]struct{}, len(include)+1)
	for _, value := range include {
		if value == "" {
			continue
		}
		unique[value] = struct{}{}
	}
	if _, ok := unique["reasoning.encrypted_content"]; !ok {
		include = append(include, "reasoning.encrypted_content")
		unique["reasoning.encrypted_content"] = struct{}{}
	}

	final := make([]any, 0, len(unique))
	for _, value := range include {
		if value == "" {
			continue
		}
		if _, ok := unique[value]; ok {
			final = append(final, value)
			delete(unique, value)
		}
	}
	for value := range unique {
		final = append(final, value)
	}
	return final
}

func getReasoningConfig(modelName, effortOverride, summaryOverride string) (string, string) {
	normalized := strings.ToLower(modelName)

	isGpt52Codex := strings.Contains(normalized, "gpt-5.2-codex") || strings.Contains(normalized, "gpt 5.2 codex")
	isGpt52General := (strings.Contains(normalized, "gpt-5.2") || strings.Contains(normalized, "gpt 5.2")) && !isGpt52Codex
	isCodexMax := strings.Contains(normalized, "codex-max") || strings.Contains(normalized, "codex max")
	isCodexMini := strings.Contains(normalized, "codex-mini") ||
		strings.Contains(normalized, "codex mini") ||
		strings.Contains(normalized, "codex_mini") ||
		strings.Contains(normalized, "codex-mini-latest")
	isCodex := strings.Contains(normalized, "codex") && !isCodexMini
	isLightweight := !isCodexMini && (strings.Contains(normalized, "nano") || strings.Contains(normalized, "mini"))
	isGpt51General := (strings.Contains(normalized, "gpt-5.1") || strings.Contains(normalized, "gpt 5.1")) &&
		!isCodex && !isCodexMax && !isCodexMini

	supportsXhigh := isGpt52General || isGpt52Codex || isCodexMax
	supportsNone := isGpt52General || isGpt51General

	defaultEffort := "medium"
	if isCodexMini {
		defaultEffort = "medium"
	} else if supportsXhigh {
		defaultEffort = "high"
	} else if isLightweight {
		defaultEffort = "minimal"
	}

	effort := effortOverride
	if effort == "" {
		effort = defaultEffort
	}

	if isCodexMini {
		if effort == "minimal" || effort == "low" || effort == "none" {
			effort = "medium"
		}
		if effort == "xhigh" {
			effort = "high"
		}
		if effort != "high" && effort != "medium" {
			effort = "medium"
		}
	}

	if !supportsXhigh && effort == "xhigh" {
		effort = "high"
	}
	if !supportsNone && effort == "none" {
		effort = "low"
	}
	if effort == "minimal" {
		effort = "low"
	}

	summary := summaryOverride
	if summary == "" {
		summary = "auto"
	}

	return effort, summary
}

func getProviderOpenAI(reqBody map[string]any) map[string]any {
	providerOptions, ok := reqBody["providerOptions"].(map[string]any)
	if !ok || providerOptions == nil {
		return nil
	}
	openaiOptions, ok := providerOptions["openai"].(map[string]any)
	if !ok || openaiOptions == nil {
		return nil
	}
	return openaiOptions
}

func ensureMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func toStringSlice(value any) []string {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func codexCachePath(filename string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cacheDir := filepath.Join(home, ".opencode", "cache")
	if filename == "" {
		return cacheDir
	}
	return filepath.Join(cacheDir, filename)
}

func readFile(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func writeFile(path, content string) error {
	if path == "" {
		return fmt.Errorf("empty cache path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func loadJSON(path string, target any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false
	}
	return true
}

func writeJSON(path string, value any) error {
	if path == "" {
		return fmt.Errorf("empty json path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func fetchWithETag(url, etag string) (string, string, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("User-Agent", "sub2api-codex")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", resp.StatusCode, err
	}
	return string(body), resp.Header.Get("etag"), resp.StatusCode, nil
}
