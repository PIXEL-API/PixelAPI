package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// FilterAccountShareCodexModelsManifest limits a complete upstream manifest to
// concrete model IDs available to the current account-share key. Unknown fields
// are preserved, and the client ETag describes the filtered representation.
func FilterAccountShareCodexModelsManifest(manifest *CodexModelsManifest, allowedModels []string, ifNoneMatch string) (*CodexModelsManifest, error) {
	if manifest == nil || manifest.NotModified || len(manifest.Body) == 0 {
		return nil, fmt.Errorf("filter account share Codex models manifest: complete upstream body is required")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(manifest.Body, &envelope); err != nil {
		return nil, fmt.Errorf("decode account share Codex models manifest: %w", err)
	}
	modelsJSON := bytes.TrimSpace(envelope["models"])
	if len(modelsJSON) == 0 || modelsJSON[0] != '[' {
		return nil, fmt.Errorf("account share Codex models manifest must contain a models array")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(modelsJSON, &entries); err != nil {
		return nil, fmt.Errorf("decode account share Codex models array: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedModels))
	for _, model := range allowedModels {
		allowed[model] = struct{}{}
	}
	filtered := make([]json.RawMessage, 0, len(entries))
	for index, entry := range entries {
		var model struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(entry, &model); err != nil {
			return nil, fmt.Errorf("decode account share Codex model at index %d: %w", index, err)
		}
		if strings.TrimSpace(model.Slug) == "" {
			return nil, fmt.Errorf("account share Codex model at index %d must contain a nonempty slug", index)
		}
		if _, ok := allowed[model.Slug]; ok {
			filtered = append(filtered, entry)
		}
	}
	var err error
	envelope["models"], err = json.Marshal(filtered)
	if err != nil {
		return nil, fmt.Errorf("encode filtered account share Codex models: %w", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode filtered account share Codex models manifest: %w", err)
	}
	filteredManifest := &CodexModelsManifest{
		Body: body,
		ETag: fmt.Sprintf(`"%x"`, sha256.Sum256(body)),
	}
	return codexModelsManifestForClient(filteredManifest, ifNoneMatch), nil
}
