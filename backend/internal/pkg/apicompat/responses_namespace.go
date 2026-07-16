package apicompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ResponsesNamespaceName aliases the chat bridge namespace identity so native
// and bridged Responses paths use the same flatten/restore contract.
type ResponsesNamespaceName = NamespacedToolName

func FlattenResponsesNamespaces(req map[string]any) (map[string]ResponsesNamespaceName, bool, error) {
	return FlattenResponsesNamespacesExcept(req, nil)
}

// FlattenResponsesNamespacesExcept converts namespace child functions into
// public Responses function tools while preserving service-owned namespaces.
func FlattenResponsesNamespacesExcept(req map[string]any, preserved map[string]bool) (map[string]ResponsesNamespaceName, bool, error) {
	if req == nil {
		return nil, false, nil
	}
	tools, ok := req["tools"].([]any)
	if !ok || len(tools) == 0 {
		return nil, false, nil
	}

	topLevel := make(map[string]bool)
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.TrimSpace(responsesNamespaceString(tool["type"]))
		name := strings.TrimSpace(responsesNamespaceString(tool["name"]))
		if (typ == "function" || typ == "custom") && name != "" {
			topLevel[name] = true
		}
	}

	names := make(map[string]ResponsesNamespaceName)
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(responsesNamespaceString(tool["type"])) != "namespace" {
			continue
		}
		namespace := strings.TrimSpace(responsesNamespaceString(tool["name"]))
		if namespace == "" || preserved[namespace] {
			continue
		}
		for _, rawChild := range responsesNamespaceChildren(tool) {
			child, ok := rawChild.(map[string]any)
			if !ok || strings.TrimSpace(responsesNamespaceString(child["type"])) != "function" {
				continue
			}
			name := strings.TrimSpace(responsesNamespaceString(child["name"]))
			if name == "" {
				continue
			}
			flat := FlattenResponsesNamespaceToolName(namespace, name)
			entry := ResponsesNamespaceName{Namespace: namespace, Name: name}
			if topLevel[flat] {
				return nil, false, fmt.Errorf("namespace tool %q/%q flattens to %q which conflicts with a top-level tool of the same name; this upstream cannot disambiguate them, rename one of the tools", namespace, name, flat)
			}
			if previous, exists := names[flat]; exists && previous != entry {
				return nil, false, fmt.Errorf("namespace tools %q/%q and %q/%q both flatten to %q; this upstream cannot disambiguate them, rename one of the tools", previous.Namespace, previous.Name, namespace, name, flat)
			}
			names[flat] = entry
		}
	}
	if len(names) == 0 {
		return nil, false, nil
	}

	flattened := make([]any, 0, len(tools)+len(names))
	seen := make(map[string]bool)
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(responsesNamespaceString(tool["type"])) != "namespace" {
			flattened = append(flattened, raw)
			continue
		}
		namespace := strings.TrimSpace(responsesNamespaceString(tool["name"]))
		if preserved[namespace] {
			flattened = append(flattened, raw)
			continue
		}
		for _, rawChild := range responsesNamespaceChildren(tool) {
			child, ok := rawChild.(map[string]any)
			if !ok || strings.TrimSpace(responsesNamespaceString(child["type"])) != "function" {
				continue
			}
			name := strings.TrimSpace(responsesNamespaceString(child["name"]))
			flat := FlattenResponsesNamespaceToolName(namespace, name)
			if name == "" || seen[flat] {
				continue
			}
			seen[flat] = true
			flatChild := make(map[string]any, len(child))
			for key, value := range child {
				flatChild[key] = value
			}
			flatChild["name"] = flat
			flattened = append(flattened, flatChild)
		}
	}
	req["tools"] = flattened
	rewriteResponsesNamespaceQualifiedCalls(req["input"], names)
	if choice, ok := req["tool_choice"].(map[string]any); ok {
		choiceNamespace := strings.TrimSpace(responsesNamespaceString(choice["name"]))
		if strings.TrimSpace(responsesNamespaceString(choice["type"])) == "namespace" && !preserved[choiceNamespace] {
			req["tool_choice"] = "auto"
		} else {
			rewriteResponsesNamespaceQualifiedCall(choice, names)
		}
	}
	return names, true, nil
}

// RestoreResponsesNamespaceCalls restores flattened function calls in a JSON
// payload to the namespace/name identity expected by Codex clients.
func RestoreResponsesNamespaceCalls(payload []byte, names map[string]ResponsesNamespaceName) ([]byte, bool, error) {
	if len(payload) == 0 || len(names) == 0 {
		return payload, false, nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return payload, false, err
	}
	if !restoreResponsesNamespaceValue(value, names) {
		return payload, false, nil
	}
	var rebuilt bytes.Buffer
	encoder := json.NewEncoder(&rebuilt)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return payload, false, err
	}
	return bytes.TrimSuffix(rebuilt.Bytes(), []byte("\n")), true, nil
}

func responsesNamespaceChildren(tool map[string]any) []any {
	if children, ok := tool["tools"].([]any); ok && len(children) > 0 {
		return children
	}
	children, _ := tool["children"].([]any)
	return children
}

func rewriteResponsesNamespaceQualifiedCalls(value any, names map[string]ResponsesNamespaceName) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			rewriteResponsesNamespaceQualifiedCalls(item, names)
		}
	case map[string]any:
		if strings.TrimSpace(responsesNamespaceString(typed["type"])) == "function_call" {
			rewriteResponsesNamespaceQualifiedCall(typed, names)
		}
		for _, child := range typed {
			rewriteResponsesNamespaceQualifiedCalls(child, names)
		}
	}
}

func rewriteResponsesNamespaceQualifiedCall(item map[string]any, names map[string]ResponsesNamespaceName) bool {
	namespace := strings.TrimSpace(responsesNamespaceString(item["namespace"]))
	name := strings.TrimSpace(responsesNamespaceString(item["name"]))
	if namespace == "" || name == "" {
		return false
	}
	flat := FlattenResponsesNamespaceToolName(namespace, name)
	entry, ok := names[flat]
	if !ok || entry.Namespace != namespace || entry.Name != name {
		return false
	}
	item["name"] = flat
	delete(item, "namespace")
	return true
}

func restoreResponsesNamespaceValue(value any, names map[string]ResponsesNamespaceName) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = restoreResponsesNamespaceValue(item, names) || changed
		}
	case map[string]any:
		if strings.TrimSpace(responsesNamespaceString(typed["type"])) == "function_call" {
			if entry, ok := names[strings.TrimSpace(responsesNamespaceString(typed["name"]))]; ok {
				typed["name"] = entry.Name
				typed["namespace"] = entry.Namespace
				changed = true
			}
		}
		for _, child := range typed {
			changed = restoreResponsesNamespaceValue(child, names) || changed
		}
	}
	return changed
}

func responsesNamespaceString(value any) string {
	text, _ := value.(string)
	return text
}
