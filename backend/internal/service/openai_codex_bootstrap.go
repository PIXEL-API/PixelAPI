package service

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
)

// OpenAICodexBootstrapKind identifies the narrowly validated Codex bootstrap
// shape that was converted from a call-less function_call_output to a user
// message. Empty means that the request was left unchanged.
type OpenAICodexBootstrapKind string

const (
	OpenAICodexBootstrapAutomation OpenAICodexBootstrapKind = "automation"
	OpenAICodexBootstrapDelegation OpenAICodexBootstrapKind = "delegation"
)

// IsOpenAIResponsesCreatePath reports whether path is one of the public
// Responses create endpoints. Compatibility rewrites must never run on
// subresources such as /responses/compact or /responses/{id}/cancel.
func IsOpenAIResponsesCreatePath(path string) bool {
	normalizedPath := strings.TrimRight(strings.TrimSpace(path), "/")
	switch normalizedPath {
	case "/v1/responses", "/openai/v1/responses", "/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

// NormalizeOpenAICodexBootstrap applies the same strict bootstrap compatibility
// rule to HTTP ingress and Ops retries. Ordinary or ambiguous call outputs are
// returned byte-for-byte unchanged so the normal Responses validation remains
// authoritative.
func NormalizeOpenAICodexBootstrap(body []byte) ([]byte, OpenAICodexBootstrapKind, bool) {
	if normalized, changed := NormalizeOpenAICodexAutomationBootstrap(body); changed {
		return normalized, OpenAICodexBootstrapAutomation, true
	}
	if normalized, changed := NormalizeOpenAICodexDelegationBootstrap(body); changed {
		return normalized, OpenAICodexBootstrapDelegation, true
	}
	return body, "", false
}

func NormalizeOpenAICodexDelegationBootstrap(body []byte) ([]byte, bool) {
	return normalizeOpenAICodexCallOutputBootstrap(body, isOpenAICodexDelegationCandidate)
}

func NormalizeOpenAICodexAutomationBootstrap(body []byte) ([]byte, bool) {
	return normalizeOpenAICodexCallOutputBootstrap(body, isOpenAICodexAutomationCandidate)
}

func normalizeOpenAICodexCallOutputBootstrap(body []byte, isCandidate func(map[string]any) bool) ([]byte, bool) {
	if !hasUniqueOpenAIJSONMembers(body) {
		return body, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var request map[string]any
	if err := decoder.Decode(&request); err != nil {
		return body, false
	}
	if previousResponseID, exists := request["previous_response_id"]; exists {
		value, ok := previousResponseID.(string)
		if !ok || strings.TrimSpace(value) != "" {
			return body, false
		}
	}
	input, ok := request["input"].([]any)
	if !ok {
		return body, false
	}

	// Any call/reference anchor makes a call-less output ambiguous. Responses
	// built-ins follow the *_call / *_call_output naming convention, so classify
	// by the wire type shape instead of maintaining an incomplete allowlist.
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := openAIStringField(item, "type")
		if typ == "item_reference" || strings.HasSuffix(typ, "_call") {
			return body, false
		}
		if isOpenAIResponsesCallOutputType(typ) {
			callIDValue, exists := item["call_id"]
			callID, isString := callIDValue.(string)
			if exists && (!isString || strings.TrimSpace(callID) != "") {
				return body, false
			}
			if !isCandidate(item) {
				return body, false
			}
		}
	}

	changed := false
	for i, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || !isCandidate(item) {
			continue
		}
		output, ok := item["output"].(string)
		if !ok {
			continue
		}
		input[i] = map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{map[string]any{
				"type": "input_text",
				"text": output,
			}},
		}
		changed = true
	}
	if !changed {
		return body, false
	}
	normalized, err := json.Marshal(request)
	if err != nil {
		return body, false
	}
	return normalized, true
}

func hasUniqueOpenAIJSONMembers(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if !consumeUniqueOpenAIJSONValue(decoder) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func consumeUniqueOpenAIJSONValue(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return true
	}

	switch delim {
	case '{':
		members := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false
			}
			key, ok := keyToken.(string)
			if !ok {
				return false
			}
			if _, duplicate := members[key]; duplicate {
				return false
			}
			members[key] = struct{}{}
			if !consumeUniqueOpenAIJSONValue(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !consumeUniqueOpenAIJSONValue(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func isOpenAIResponsesCallOutputType(typ string) bool {
	return strings.HasSuffix(typ, "_call_output") || typ == "tool_search_output"
}

func isOpenAICodexDelegationCandidate(item map[string]any) bool {
	if openAIStringField(item, "type") != "function_call_output" ||
		!isOpenAICodexDelegationTool(openAIStringField(item, "namespace"), openAIStringField(item, "name")) {
		return false
	}
	output, ok := item["output"].(string)
	return ok && validOpenAICodexDelegationEnvelope(output)
}

func isOpenAICodexAutomationCandidate(item map[string]any) bool {
	if openAIStringField(item, "type") != "function_call_output" ||
		openAIStringField(item, "namespace") != "codex_app" ||
		openAIStringField(item, "name") != "automation_update" {
		return false
	}
	output, ok := item["output"].(string)
	return ok && validOpenAICodexAutomationBootstrap(output)
}

func openAIStringField(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func isOpenAICodexDelegationTool(namespace, name string) bool {
	return (namespace == "codex_app" || namespace == "codex_tui") &&
		(name == "create_thread" || name == "send_message_to_thread")
}

func validOpenAICodexAutomationBootstrap(value string) bool {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	if strings.ContainsRune(normalized, '\r') {
		return false
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) < 6 {
		return false
	}
	if _, ok := openAICodexAutomationHeaderValue(lines[0], "Automation: "); !ok {
		return false
	}
	automationID, ok := openAICodexAutomationHeaderValue(lines[1], "Automation ID: ")
	if !ok || !validOpenAICodexAutomationID(automationID) {
		return false
	}
	expectedMemory := "Automation memory: $CODEX_HOME/automations/" + automationID + "/memory.md"
	if lines[2] != expectedMemory {
		return false
	}
	lastRun, ok := openAICodexAutomationHeaderValue(lines[3], "Last run: ")
	if !ok || !validOpenAICodexAutomationLastRun(lastRun) || lines[4] != "" {
		return false
	}
	return strings.TrimSpace(strings.Join(lines[5:], "\n")) != ""
}

func openAICodexAutomationHeaderValue(line, prefix string) (string, bool) {
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(line, prefix)
	return value, value != "" && strings.TrimSpace(value) == value
}

func validOpenAICodexAutomationID(value string) bool {
	if len(value) == 0 || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func validOpenAICodexAutomationLastRun(value string) bool {
	if value == "never" {
		return true
	}
	separator := strings.LastIndex(value, " (")
	if separator <= 0 || !strings.HasSuffix(value, ")") {
		return false
	}
	runAt, err := time.Parse(time.RFC3339Nano, value[:separator])
	if err != nil {
		return false
	}
	epochMillis, err := strconv.ParseInt(value[separator+2:len(value)-1], 10, 64)
	return err == nil && runAt.UnixMilli() == epochMillis
}

func validOpenAICodexDelegationEnvelope(value string) bool {
	decoder := xml.NewDecoder(strings.NewReader(value))
	var rootSeen, sourceSeen, inputSeen bool
	var childName string
	var childText bytes.Buffer
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return rootSeen && depth == 0 && sourceSeen && inputSeen
		}
		if err != nil {
			return false
		}
		switch current := token.(type) {
		case xml.StartElement:
			depth++
			if current.Name.Space != "" || len(current.Attr) != 0 || (depth == 1 && current.Name.Local != "codex_delegation") || depth > 2 {
				return false
			}
			if depth == 1 {
				if rootSeen {
					return false
				}
				rootSeen = true
				continue
			}
			if current.Name.Local != "source_thread_id" && current.Name.Local != "input" {
				return false
			}
			childName = current.Name.Local
			childText.Reset()
		case xml.EndElement:
			if current.Name.Space != "" {
				return false
			}
			if depth == 2 {
				if current.Name.Local != childName || strings.TrimSpace(childText.String()) == "" {
					return false
				}
				if childName == "source_thread_id" {
					if sourceSeen {
						return false
					}
					sourceSeen = true
				} else {
					if inputSeen {
						return false
					}
					inputSeen = true
				}
				childName = ""
			}
			depth--
			if depth < 0 {
				return false
			}
		case xml.CharData:
			if depth == 2 {
				_, _ = childText.Write(current)
			} else if len(bytes.TrimSpace(current)) != 0 {
				return false
			}
		case xml.Comment:
			return false
		case xml.ProcInst, xml.Directive:
			return false
		}
	}
}
