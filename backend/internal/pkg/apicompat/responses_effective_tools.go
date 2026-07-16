package apicompat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const responsesChatToolNameMaxBytes = 64

// EffectiveResponsesTools merges top-level tools with Codex additional_tools
// input items. It intentionally inspects the discriminator before decoding the
// tools field so malformed tools on ordinary message items are ignored.
func EffectiveResponsesTools(req *ResponsesRequest) ([]ResponsesTool, error) {
	if req == nil {
		return nil, nil
	}
	tools := append([]ResponsesTool(nil), req.Tools...)
	input := trimResponsesRaw(req.Input)
	if len(input) == 0 || string(input) == "null" || input[0] != '[' {
		return tools, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("parse responses input for additional tools: %w", err)
	}
	for _, raw := range items {
		raw = trimResponsesRaw(raw)
		if len(raw) == 0 || raw[0] != '{' {
			continue
		}
		var discriminator struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &discriminator); err != nil {
			return nil, fmt.Errorf("parse responses additional tools item: %w", err)
		}
		if discriminator.Type != "additional_tools" {
			continue
		}
		var item struct {
			Tools []ResponsesTool `json:"tools"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("parse responses additional tools item: %w", err)
		}
		tools = append(tools, item.Tools...)
	}
	return tools, nil
}

// CustomToolNames records custom/freeform tools for response identity restore.
func CustomToolNames(tools []ResponsesTool) map[string]bool {
	var names map[string]bool
	for _, tool := range tools {
		if tool.Type != "custom" || tool.Name == "" {
			continue
		}
		if names == nil {
			names = make(map[string]bool)
		}
		names[tool.Name] = true
	}
	return names
}

// NamespacedToolName identifies a function child in a Responses namespace.
type NamespacedToolName struct {
	Namespace string
	Name      string
}

// NamespaceToolNames maps flattened Chat function names back to their original
// Responses namespace and child name.
func NamespaceToolNames(tools []ResponsesTool) map[string]NamespacedToolName {
	var names map[string]NamespacedToolName
	for _, tool := range tools {
		if tool.Type != "namespace" || tool.Name == "" {
			continue
		}
		children := tool.Tools
		if len(children) == 0 {
			children = tool.Children
		}
		for _, child := range children {
			if child.Type != "function" || child.Name == "" {
				continue
			}
			if names == nil {
				names = make(map[string]NamespacedToolName)
			}
			names[FlattenResponsesNamespaceToolName(tool.Name, child.Name)] = NamespacedToolName{
				Namespace: tool.Name,
				Name:      child.Name,
			}
		}
	}
	return names
}

// HasToolSearchTool reports whether the client declared the built-in tool search.
func HasToolSearchTool(tools []ResponsesTool) bool {
	for _, tool := range tools {
		if tool.Type == "tool_search" {
			return true
		}
	}
	return false
}

// FlattenResponsesNamespaceToolName produces a Chat-compatible function name.
// Names over 64 UTF-8 bytes are truncated on rune boundaries and receive an
// eight-hex SHA-256 suffix to preserve identity.
func FlattenResponsesNamespaceToolName(namespace, name string) string {
	full := namespace + "__" + name
	if len(full) <= responsesChatToolNameMaxBytes {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	suffix := "__" + hex.EncodeToString(sum[:4])
	prefixLimit := responsesChatToolNameMaxBytes - len(suffix)
	var prefix strings.Builder
	for _, r := range full {
		if prefix.Len()+len(string(r)) > prefixLimit {
			break
		}
		_, _ = prefix.WriteRune(r)
	}
	return prefix.String() + suffix
}

// ToolSearchCallArgumentsJSON restores the JSON value expected on the wire.
// Invalid model output remains a JSON string so the client can reject it and
// trigger the normal model retry behavior.
func ToolSearchCallArgumentsJSON(arguments string) json.RawMessage {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	fallback, _ := json.Marshal(arguments)
	return fallback
}

func trimResponsesRaw(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}
