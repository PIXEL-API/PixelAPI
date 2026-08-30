package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

const opencodeResponsesRawEndpoint = "/v1/responses"

// OpencodeGoResolvedModel is the single routing decision shared by all three
// OpenCode Go ingress protocols. Protocol selection must use UpstreamModel,
// after channel/account mapping and provider-prefix normalization.
type OpencodeGoResolvedModel struct {
	RequestedModel string
	BillingModel   string
	UpstreamModel  string
	Spec           OpencodeGoModelSpec
}

type opencodeGoRoutingError struct {
	kind          string
	model         string
	resolvedModel string
	inbound       OpencodeGoProtocol
	required      OpencodeGoProtocol
	detail        string
}

func (e *opencodeGoRoutingError) Error() string {
	if e == nil {
		return "OpenCode Go routing failed"
	}
	switch e.kind {
	case "missing_model":
		return "model is required"
	case "unknown_model":
		if e.resolvedModel != "" && e.resolvedModel != e.model {
			return fmt.Sprintf("OpenCode Go model %q resolves to %q, which is not present in the audited model catalog", e.model, e.resolvedModel)
		}
		return fmt.Sprintf("OpenCode Go model %q is not present in the audited model catalog", e.model)
	case "protocol_mismatch":
		message := fmt.Sprintf(
			"OpenCode Go model %q requires the %s protocol; the current %s request cannot be converted losslessly",
			e.resolvedModel,
			e.required,
			e.inbound,
		)
		if endpoint := opencodeGoIngressEndpoint(e.required); endpoint != "" {
			message += "; use " + endpoint
		}
		return message
	case "capability_mismatch":
		message := fmt.Sprintf(
			"OpenCode Go model %q requires the %s protocol; this %s compatibility request is not lossless",
			e.resolvedModel,
			e.required,
			e.inbound,
		)
		if e.detail != "" {
			message += ": " + e.detail
		}
		if endpoint := opencodeGoIngressEndpoint(e.required); endpoint != "" {
			message += "; use " + endpoint
		}
		return message
	case "unsupported_path":
		message := fmt.Sprintf("OpenCode Go native %s requests do not support request path %q", e.required, e.detail)
		if endpoint := opencodeGoIngressEndpoint(e.required); endpoint != "" {
			message += "; use " + endpoint
		}
		return message
	default:
		return "OpenCode Go routing failed"
	}
}

func opencodeGoIngressEndpoint(protocol OpencodeGoProtocol) string {
	switch protocol {
	case OpencodeGoProtocolChat:
		return openAIChatRawEndpoint
	case OpencodeGoProtocolMessages:
		return opencodeMessagesRawEndpoint
	case OpencodeGoProtocolResponses:
		return opencodeResponsesRawEndpoint
	default:
		return ""
	}
}

// resolveOpencodeGoForwardModel applies the normal forwarding mapping before
// consulting the audited Go catalog. Unknown models deliberately have no
// default protocol.
func resolveOpencodeGoForwardModel(account *Account, requestedModel, defaultMappedModel string) (OpencodeGoResolvedModel, error) {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return OpencodeGoResolvedModel{}, &opencodeGoRoutingError{kind: "missing_model"}
	}

	billingModel := strings.TrimSpace(resolveOpenAIForwardModel(account, requestedModel, defaultMappedModel))
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	// normalizeOpenAIModelForUpstream already applies the one allowed OpenCode
	// provider-prefix normalization. Lookup must be exact here; calling the
	// public normalizing resolver again would accept double-prefixed mappings
	// while the forwarding path only strips one prefix.
	spec, ok := opencodeGoModelByID[upstreamModel]
	if !ok {
		return OpencodeGoResolvedModel{}, &opencodeGoRoutingError{
			kind:          "unknown_model",
			model:         requestedModel,
			resolvedModel: upstreamModel,
		}
	}

	return OpencodeGoResolvedModel{
		RequestedModel: requestedModel,
		BillingModel:   billingModel,
		UpstreamModel:  spec.ID,
		Spec:           spec,
	}, nil
}

func rejectOpencodeGoRoutingError(c *gin.Context, inbound OpencodeGoProtocol, routeErr error) (*OpenAIForwardResult, error) {
	statusCode := http.StatusUnprocessableEntity
	errorType := "model_protocol_capability_error"
	errorParam := "model"
	var routingErr *opencodeGoRoutingError
	if errors.As(routeErr, &routingErr) {
		switch routingErr.kind {
		case "missing_model":
			statusCode = http.StatusBadRequest
			errorType = "invalid_request_error"
		case "unsupported_path":
			errorParam = "path"
		}
	}

	message := routeErr.Error()
	setOpsUpstreamError(c, statusCode, message, "")
	if inbound == OpencodeGoProtocolMessages {
		writeAnthropicError(c, statusCode, "invalid_request_error", message)
	} else {
		c.JSON(statusCode, gin.H{
			"error": gin.H{
				"type":    errorType,
				"message": message,
				"param":   errorParam,
			},
		})
	}
	return nil, routeErr
}

func newOpencodeGoProtocolMismatch(inbound OpencodeGoProtocol, resolved OpencodeGoResolvedModel) error {
	return &opencodeGoRoutingError{
		kind:          "protocol_mismatch",
		model:         resolved.RequestedModel,
		resolvedModel: resolved.UpstreamModel,
		inbound:       inbound,
		required:      resolved.Spec.Protocol,
	}
}

func newOpencodeGoCapabilityMismatch(inbound OpencodeGoProtocol, resolved OpencodeGoResolvedModel, detail string) error {
	return &opencodeGoRoutingError{
		kind:          "capability_mismatch",
		model:         resolved.RequestedModel,
		resolvedModel: resolved.UpstreamModel,
		inbound:       inbound,
		required:      resolved.Spec.Protocol,
		detail:        strings.TrimSpace(detail),
	}
}

func newOpencodeGoUnsupportedPath(resolved OpencodeGoResolvedModel, path string) error {
	return &opencodeGoRoutingError{
		kind:          "unsupported_path",
		model:         resolved.RequestedModel,
		resolvedModel: resolved.UpstreamModel,
		inbound:       OpencodeGoProtocolResponses,
		required:      OpencodeGoProtocolResponses,
		detail:        strings.TrimSpace(path),
	}
}

// validateOpencodeMessagesToChatBridge prevents the existing compatibility
// bridge from silently dropping Anthropic-only semantics.
func validateOpencodeMessagesToChatBridge(body []byte, resolved OpencodeGoResolvedModel) error {
	return validateOpencodeMessagesBridge(body, resolved, OpencodeGoProtocolChat)
}

func validateOpencodeChatToResponsesBridge(body []byte, resolved OpencodeGoResolvedModel) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil // The normal parser owns malformed JSON reporting.
	}

	_, hasMessages := payload["messages"]
	rawInput, hasInput := payload["input"]
	if hasInput && !hasMessages {
		// Some clients send a Responses-shaped body to /chat/completions. The
		// established bridge forwards that body directly to /responses, so only
		// reject fields it would actively strip and input/output shapes that the
		// eventual Chat response cannot represent.
		for _, field := range cursorResponsesUnsupportedFields {
			if raw, exists := payload[field]; exists && !isNullJSON(raw) {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Responses field %q would be removed by the Chat compatibility path", field))
			}
		}
		for _, field := range []string{"include", "background", "text"} {
			if raw, exists := payload[field]; exists && !isNullJSON(raw) {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Responses field %q requests output that cannot be returned losslessly through Chat Completions", field))
			}
		}
		if err := validateOpencodeResponsesToolsForChat(body, resolved, OpencodeGoProtocolChat); err != nil {
			return err
		}
		if err := validateOpencodeResponsesToolChoiceForChat(payload["tool_choice"], resolved, OpencodeGoProtocolChat); err != nil {
			return err
		}
		return validateOpencodeResponsesInputForChat(rawInput, resolved, OpencodeGoProtocolChat)
	}

	allowedTopLevel := jsonFieldSet(
		"model", "messages", "instructions",
		"max_tokens", "max_completion_tokens",
		"temperature", "top_p", "stream", "stream_options",
		"tools", "parallel_tool_calls", "tool_choice",
		"reasoning_effort", "service_tier",
		"prompt_cache_key", "prompt_cache_options",
		"functions", "function_call",
	)
	if field := firstUnsupportedJSONField(payload, allowedTopLevel); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat Completions field %q is not preserved by the Responses compatibility bridge", field))
	}
	if err := validateOpencodeOutputTokenFloor(
		payload,
		OpencodeGoProtocolChat,
		resolved,
		"Responses",
		"max_tokens",
		"max_completion_tokens",
	); err != nil {
		return err
	}
	if err := validateOpencodeChatToolsForResponses(payload["tools"], payload["functions"], resolved); err != nil {
		return err
	}
	if err := validateOpencodeChatToolChoiceForResponses(payload["tool_choice"], resolved); err != nil {
		return err
	}
	if err := validateOpencodeChatFunctionCallForResponses(payload["function_call"], resolved); err != nil {
		return err
	}
	if err := validateOpencodeJSONOptions(payload["stream_options"], jsonFieldSet("include_usage"), OpencodeGoProtocolChat, resolved, "Chat stream_options"); err != nil {
		return err
	}
	if err := validateOpencodeJSONOptions(payload["prompt_cache_options"], jsonFieldSet("mode", "ttl"), OpencodeGoProtocolChat, resolved, "Chat prompt_cache_options"); err != nil {
		return err
	}
	return validateOpencodeChatMessagesForResponses(payload["messages"], resolved)
}

func validateOpencodeChatMessagesForResponses(rawMessages json.RawMessage, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawMessages) {
		return nil
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return nil // The normal Chat request parser owns malformed JSON reporting.
	}
	for index, rawMessage := range messages {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(rawMessage, &message); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat message %d is not an object and cannot be represented losslessly by Responses", index))
		}
		role := strings.ToLower(strings.TrimSpace(rawJSONString(message["role"])))
		var allowedFields map[string]struct{}
		switch role {
		case "system", "user":
			allowedFields = jsonFieldSet("role", "content")
		case "assistant":
			allowedFields = jsonFieldSet("role", "content", "tool_calls")
		case "tool":
			allowedFields = jsonFieldSet("role", "content", "tool_call_id")
		case "function":
			allowedFields = jsonFieldSet("role", "content", "name")
		default:
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat message role %q is not supported by the Responses compatibility bridge", role))
		}
		if field := firstUnsupportedJSONField(message, allowedFields); field != "" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat message field %q for role %q is not preserved by the Responses compatibility bridge", field, role))
		}

		if err := validateOpencodeChatContentForResponses(message["content"], role, resolved); err != nil {
			return err
		}
		if role == "assistant" {
			if err := validateOpencodeChatToolCallsForResponses(message["tool_calls"], resolved); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOpencodeChatContentForResponses(rawContent json.RawMessage, role string, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawContent) {
		return nil
	}
	var text string
	if err := json.Unmarshal(rawContent, &text); err == nil {
		return nil
	}

	var parts []json.RawMessage
	if err := json.Unmarshal(rawContent, &parts); err != nil {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat content for role %q must be a string or an array of supported parts", role))
	}
	for index, rawPart := range parts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(rawPart, &part); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat content part %d for role %q is not an object", index, role))
		}
		partType := strings.ToLower(strings.TrimSpace(rawJSONString(part["type"])))
		switch partType {
		case "text":
			allowed := jsonFieldSet("type", "text")
			if role == "system" || role == "user" {
				allowed["prompt_cache_breakpoint"] = struct{}{}
			}
			if field := firstUnsupportedJSONField(part, allowed); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat content field %q in a %q part for role %q is not preserved by Responses", field, partType, role))
			}
		case "image_url":
			if role != "system" && role != "user" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat image content for role %q cannot be represented losslessly by Responses", role))
			}
			if field := firstUnsupportedJSONField(part, jsonFieldSet("type", "image_url", "prompt_cache_breakpoint")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat image content field %q is not preserved by Responses", field))
			}
			if err := validateOpencodeChatImageURL(part["image_url"], resolved); err != nil {
				return err
			}
		default:
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat content part type %q for role %q is not supported by the Responses compatibility bridge", partType, role))
		}
	}
	return nil
}

func validateOpencodeChatImageURL(rawImageURL json.RawMessage, resolved OpencodeGoResolvedModel) error {
	var image map[string]json.RawMessage
	if err := json.Unmarshal(rawImageURL, &image); err != nil {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, "Chat image_url must be an object with a non-empty URL")
	}
	if field := firstUnsupportedJSONField(image, jsonFieldSet("url")); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat image_url field %q is not preserved by Responses", field))
	}
	url := strings.TrimSpace(rawJSONString(image["url"]))
	if url == "" || isOpencodeEmptyBase64DataURL(url) {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, "Chat image_url must contain a non-empty URL")
	}
	return nil
}

func validateOpencodeChatToolCallsForResponses(rawToolCalls json.RawMessage, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawToolCalls) {
		return nil
	}
	var calls []json.RawMessage
	if err := json.Unmarshal(rawToolCalls, &calls); err != nil {
		return nil // The normal Chat request parser owns malformed JSON reporting.
	}
	for index, rawCall := range calls {
		var call map[string]json.RawMessage
		if err := json.Unmarshal(rawCall, &call); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat tool call %d is not an object", index))
		}
		if field := firstUnsupportedJSONField(call, jsonFieldSet("id", "type", "function")); field != "" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat tool call field %q is not preserved by Responses", field))
		}
		if strings.TrimSpace(rawJSONString(call["type"])) != "function" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, "Chat assistant tool calls must use type \"function\"")
		}
		var function map[string]json.RawMessage
		if err := json.Unmarshal(call["function"], &function); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, "Chat function tool call must contain a function object")
		}
		if field := firstUnsupportedJSONField(function, jsonFieldSet("name", "arguments")); field != "" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat function call field %q is not preserved by Responses", field))
		}
	}
	return nil
}

func validateOpencodeChatToolsForResponses(rawTools, rawFunctions json.RawMessage, resolved OpencodeGoResolvedModel) error {
	if err := validateOpencodeChatFunctionDefinitions(rawTools, true, resolved); err != nil {
		return err
	}
	return validateOpencodeChatFunctionDefinitions(rawFunctions, false, resolved)
}

func validateOpencodeChatFunctionDefinitions(rawDefinitions json.RawMessage, wrapped bool, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawDefinitions) {
		return nil
	}
	var definitions []json.RawMessage
	if err := json.Unmarshal(rawDefinitions, &definitions); err != nil {
		return nil // The normal Chat request parser owns malformed JSON reporting.
	}
	for index, rawDefinition := range definitions {
		var definition map[string]json.RawMessage
		if err := json.Unmarshal(rawDefinition, &definition); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat function definition %d is not an object", index))
		}
		if wrapped {
			if field := firstUnsupportedJSONField(definition, jsonFieldSet("type", "function")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat tool field %q is not preserved by Responses", field))
			}
			if strings.TrimSpace(rawJSONString(definition["type"])) != "function" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat tool type %q cannot be represented losslessly by Responses", rawJSONString(definition["type"])))
			}
			var function map[string]json.RawMessage
			if err := json.Unmarshal(definition["function"], &function); err != nil {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, "Chat function tool must contain a function object")
			}
			definition = function
		}
		if field := firstUnsupportedJSONField(definition, jsonFieldSet("name", "description", "parameters", "strict")); field != "" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat function definition field %q is not preserved by Responses", field))
		}
	}
	return nil
}

func validateOpencodeChatToolChoiceForResponses(rawChoice json.RawMessage, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawChoice) {
		return nil
	}
	var choice string
	if err := json.Unmarshal(rawChoice, &choice); err == nil {
		switch choice {
		case "auto", "none", "required":
			return nil
		default:
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat tool_choice %q is not supported losslessly by Responses", choice))
		}
	}
	return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, "Chat object-form tool_choice is not normalized by the Responses compatibility bridge; use function_call or a string tool_choice")
}

func validateOpencodeChatFunctionCallForResponses(rawCall json.RawMessage, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawCall) {
		return nil
	}
	var mode string
	if err := json.Unmarshal(rawCall, &mode); err == nil {
		if mode == "auto" || mode == "none" {
			return nil
		}
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat function_call %q is not supported losslessly by Responses", mode))
	}
	var call map[string]json.RawMessage
	if err := json.Unmarshal(rawCall, &call); err != nil {
		return nil // The normal Chat request parser owns malformed JSON reporting.
	}
	if field := firstUnsupportedJSONField(call, jsonFieldSet("name")); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolChat, resolved, fmt.Sprintf("Chat function_call field %q is not preserved by Responses", field))
	}
	return nil
}

func validateOpencodeResponsesToChatBridge(body []byte, resolved OpencodeGoResolvedModel) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil // The normal parser owns malformed JSON reporting.
	}
	allowedTopLevel := jsonFieldSet(
		"model", "instructions", "input", "max_output_tokens",
		"temperature", "top_p", "stream", "tools",
		"parallel_tool_calls", "reasoning", "tool_choice",
		"service_tier", "prompt_cache_key", "prompt_cache_options",
	)
	if field := firstUnsupportedJSONField(payload, allowedTopLevel); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolResponses, resolved, fmt.Sprintf("Responses field %q is not preserved by the Chat Completions compatibility bridge", field))
	}
	if err := validateOpencodeResponsesReasoningForChat(payload["reasoning"], resolved); err != nil {
		return err
	}
	if err := validateOpencodeResponsesToolsForChat(body, resolved, OpencodeGoProtocolResponses); err != nil {
		return err
	}
	if err := validateOpencodeResponsesToolChoiceForChat(payload["tool_choice"], resolved, OpencodeGoProtocolResponses); err != nil {
		return err
	}
	if err := validateOpencodeJSONOptions(payload["prompt_cache_options"], jsonFieldSet("mode", "ttl"), OpencodeGoProtocolResponses, resolved, "Responses prompt_cache_options"); err != nil {
		return err
	}
	return validateOpencodeResponsesInputForChat(payload["input"], resolved, OpencodeGoProtocolResponses)
}

func validateOpencodeResponsesReasoningForChat(rawReasoning json.RawMessage, resolved OpencodeGoResolvedModel) error {
	if isNullJSON(rawReasoning) {
		return nil
	}
	var reasoning map[string]json.RawMessage
	if err := json.Unmarshal(rawReasoning, &reasoning); err != nil {
		return nil // The normal Responses request parser owns malformed JSON reporting.
	}
	if rawSummary, exists := reasoning["summary"]; exists && !isNullJSON(rawSummary) {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolResponses, resolved, "Responses reasoning.summary cannot be represented losslessly by Chat Completions")
	}
	if field := firstUnsupportedJSONField(reasoning, jsonFieldSet("effort", "summary")); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolResponses, resolved, fmt.Sprintf("Responses reasoning field %q is not preserved by Chat Completions", field))
	}
	return nil
}

func validateOpencodeResponsesToolsForChat(body []byte, resolved OpencodeGoResolvedModel, inbound OpencodeGoProtocol) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil // The normal parser owns malformed JSON reporting.
	}
	if rawTools := payload["tools"]; !isNullJSON(rawTools) {
		var tools []json.RawMessage
		if err := json.Unmarshal(rawTools, &tools); err != nil {
			return nil // The normal parser owns malformed JSON reporting.
		}
		for index, rawTool := range tools {
			var tool map[string]json.RawMessage
			if err := json.Unmarshal(rawTool, &tool); err != nil {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses tool %d is not an object", index))
			}
			toolType := strings.TrimSpace(rawJSONString(tool["type"]))
			if toolType != "function" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses tool type %q cannot be represented losslessly by Chat Completions", toolType))
			}
			if field := firstUnsupportedJSONField(tool, jsonFieldSet("type", "name", "description", "parameters", "strict")); field != "" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses function tool field %q is not preserved by Chat Completions", field))
			}
		}
	}

	var request apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil // The normal parser owns malformed JSON reporting.
	}
	tools, err := apicompat.EffectiveResponsesTools(&request)
	if err != nil {
		return nil // The normal conversion path emits the precise validation error.
	}
	for _, tool := range tools {
		if strings.TrimSpace(tool.Type) != "function" {
			return newOpencodeGoCapabilityMismatch(
				inbound,
				resolved,
				fmt.Sprintf("Responses tool type %q cannot be represented losslessly by Chat Completions", tool.Type),
			)
		}
	}
	return nil
}

func validateOpencodeResponsesToolChoiceForChat(rawChoice json.RawMessage, resolved OpencodeGoResolvedModel, inbound OpencodeGoProtocol) error {
	if isNullJSON(rawChoice) {
		return nil
	}
	var choice string
	if err := json.Unmarshal(rawChoice, &choice); err == nil {
		switch choice {
		case "auto", "none", "required":
			return nil
		default:
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses tool_choice %q cannot be represented losslessly by Chat Completions", choice))
		}
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(rawChoice, &object); err != nil {
		return nil // The normal parser owns malformed JSON reporting.
	}
	if field := firstUnsupportedJSONField(object, jsonFieldSet("type", "name", "function")); field != "" {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses tool_choice field %q is not preserved by Chat Completions", field))
	}
	if strings.TrimSpace(rawJSONString(object["type"])) != "function" {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses object-form tool_choice type %q cannot be represented losslessly by Chat Completions", rawJSONString(object["type"])))
	}
	if rawFunction := object["function"]; !isNullJSON(rawFunction) {
		var function map[string]json.RawMessage
		if err := json.Unmarshal(rawFunction, &function); err != nil {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses tool_choice.function must be an object")
		}
		if field := firstUnsupportedJSONField(function, jsonFieldSet("name")); field != "" {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses tool_choice.function field %q is not preserved by Chat Completions", field))
		}
	}
	return nil
}

func validateOpencodeResponsesInputForChat(rawInput json.RawMessage, resolved OpencodeGoResolvedModel, inbound OpencodeGoProtocol) error {
	if isNullJSON(rawInput) {
		return nil
	}
	var inputText string
	if err := json.Unmarshal(rawInput, &inputText); err == nil {
		return nil // String input is losslessly represented as one user message.
	}
	var items []json.RawMessage
	if err := json.Unmarshal(rawInput, &items); err != nil {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses input must be a string or an array of supported input items")
	}
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses input array contains a non-object item that cannot be represented losslessly by Chat Completions")
		}
		itemType := strings.ToLower(strings.TrimSpace(rawJSONString(item["type"])))
		role := strings.ToLower(strings.TrimSpace(rawJSONString(item["role"])))
		switch itemType {
		case "", "message":
			if role != "system" && role != "user" && role != "assistant" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses message role %q is not supported by the Chat Completions bridge", role))
			}
			if field := firstUnsupportedJSONField(item, jsonFieldSet("type", "role", "content")); field != "" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses message field %q for role %q is not preserved by Chat Completions", field, role))
			}
			if err := validateOpencodeResponsesContentForChat(item["content"], role, resolved, inbound); err != nil {
				return err
			}
		case "function_call":
			if field := firstUnsupportedJSONField(item, jsonFieldSet("type", "call_id", "name", "arguments")); field != "" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses function_call field %q is not preserved by Chat Completions", field))
			}
		case "function_call_output":
			if field := firstUnsupportedJSONField(item, jsonFieldSet("type", "call_id", "output")); field != "" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses function_call_output field %q is not preserved by Chat Completions", field))
			}
		case "input_text", "text":
			if field := firstUnsupportedJSONField(item, jsonFieldSet("type", "text")); field != "" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses %s field %q is not preserved by Chat Completions", itemType, field))
			}
		case "input_image":
			if field := firstUnsupportedJSONField(item, jsonFieldSet("type", "image_url")); field != "" {
				return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses input_image field %q is not preserved by Chat Completions", field))
			}
			if err := validateOpencodeResponsesImageURL(item["image_url"], resolved, inbound); err != nil {
				return err
			}
		case "reasoning":
			return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses reasoning input items cannot be preserved losslessly by Chat Completions")
		case "tool_search_call", "tool_search_output", "custom_tool_call", "custom_tool_call_output":
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses input item type %q is not lossless through Chat Completions", itemType))
		default:
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses input item type %q is not supported by the Chat Completions bridge", itemType))
		}
	}
	return nil
}

func validateOpencodeResponsesContentForChat(rawContent json.RawMessage, role string, resolved OpencodeGoResolvedModel, inbound OpencodeGoProtocol) error {
	if isNullJSON(rawContent) {
		return nil
	}
	var text string
	if err := json.Unmarshal(rawContent, &text); err == nil {
		return nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(rawContent, &parts); err == nil {
		for _, rawPart := range parts {
			if err := validateOpencodeResponsesContentPartForChat(rawPart, role, resolved, inbound); err != nil {
				return err
			}
		}
		return nil
	}
	var single map[string]json.RawMessage
	if err := json.Unmarshal(rawContent, &single); err == nil {
		return validateOpencodeResponsesContentPartForChat(rawContent, role, resolved, inbound)
	}
	return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses content for role %q must be a string, object, or array of supported parts", role))
}

func validateOpencodeResponsesContentPartForChat(rawPart json.RawMessage, role string, resolved OpencodeGoResolvedModel, inbound OpencodeGoProtocol) error {
	var part map[string]json.RawMessage
	if err := json.Unmarshal(rawPart, &part); err != nil {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses content contains a non-object part that cannot be represented losslessly by Chat Completions")
	}
	partType := strings.ToLower(strings.TrimSpace(rawJSONString(part["type"])))
	switch partType {
	case "", "input_text", "output_text", "text":
		if field := firstUnsupportedJSONField(part, jsonFieldSet("type", "text")); field != "" {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses text content field %q is not preserved by Chat Completions", field))
		}
	case "input_image", "image_url":
		if role != "user" {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses image content for role %q cannot be represented losslessly by Chat Completions", role))
		}
		if field := firstUnsupportedJSONField(part, jsonFieldSet("type", "image_url")); field != "" {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses image content field %q is not preserved by Chat Completions", field))
		}
		if err := validateOpencodeResponsesImageURL(part["image_url"], resolved, inbound); err != nil {
			return err
		}
	default:
		return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses content part type %q is not supported by the Chat Completions bridge", partType))
	}
	return nil
}

func validateOpencodeResponsesImageURL(rawImageURL json.RawMessage, resolved OpencodeGoResolvedModel, inbound OpencodeGoProtocol) error {
	if url := strings.TrimSpace(rawJSONString(rawImageURL)); url != "" {
		if isOpencodeEmptyBase64DataURL(url) {
			return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses input_image must contain a non-empty image URL")
		}
		return nil
	}
	var image map[string]json.RawMessage
	if err := json.Unmarshal(rawImageURL, &image); err != nil {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses input_image must contain a non-empty image URL")
	}
	if field := firstUnsupportedJSONField(image, jsonFieldSet("url")); field != "" {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("Responses image_url field %q is not preserved by Chat Completions", field))
	}
	url := strings.TrimSpace(rawJSONString(image["url"]))
	if url == "" || isOpencodeEmptyBase64DataURL(url) {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, "Responses input_image must contain a non-empty image URL")
	}
	return nil
}

func validateOpencodeMessagesToResponsesBridge(body []byte, resolved OpencodeGoResolvedModel) error {
	return validateOpencodeMessagesBridge(body, resolved, OpencodeGoProtocolResponses)
}

func validateOpencodeMessagesBridge(body []byte, resolved OpencodeGoResolvedModel, target OpencodeGoProtocol) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil // The normal Messages request parser owns malformed JSON reporting.
	}
	targetName := "Responses"
	if target == OpencodeGoProtocolChat {
		targetName = "Chat Completions"
	}

	if rawThinking, exists := payload["thinking"]; exists && !isNullJSON(rawThinking) {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic thinking configuration cannot be represented losslessly by %s", targetName))
	}
	if hasOpencodeAnthropicCacheControl(payload) {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic cache_control cannot be preserved by %s", targetName))
	}
	if rawStop, exists := payload["stop_sequences"]; exists && !isNullJSON(rawStop) && !isEmptyRawJSONArray(rawStop) {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic stop_sequences cannot be represented losslessly by %s", targetName))
	}
	allowedTopLevel := jsonFieldSet(
		"model", "max_tokens", "system", "messages", "tools", "stream",
		"temperature", "top_p", "stop_sequences", "tool_choice", "output_config",
	)
	if field := firstUnsupportedJSONField(payload, allowedTopLevel); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic field %q is not preserved by the %s compatibility bridge", field, targetName))
	}
	if err := validateOpencodeAnthropicOutputConfig(payload["output_config"], resolved, targetName); err != nil {
		return err
	}
	if err := validateOpencodeAnthropicSystem(payload["system"], resolved, targetName); err != nil {
		return err
	}
	if err := validateOpencodeAnthropicTools(payload["tools"], resolved, target, targetName); err != nil {
		return err
	}
	if err := validateOpencodeAnthropicToolChoice(payload["tool_choice"], resolved, targetName); err != nil {
		return err
	}
	if err := validateOpencodeAnthropicMessages(payload["messages"], resolved, targetName); err != nil {
		return err
	}
	return validateOpencodeOutputTokenFloor(
		payload,
		OpencodeGoProtocolMessages,
		resolved,
		targetName,
		"max_tokens",
	)
}

func validateOpencodeOutputTokenFloor(
	payload map[string]json.RawMessage,
	inbound OpencodeGoProtocol,
	resolved OpencodeGoResolvedModel,
	targetName string,
	fields ...string,
) error {
	for _, field := range fields {
		rawValue, exists := payload[field]
		if !exists || isNullJSON(rawValue) {
			continue
		}
		var value int
		if err := json.Unmarshal(rawValue, &value); err != nil {
			continue // The normal protocol parser owns malformed numeric values.
		}
		if value > 0 && value < minMaxOutputTokens {
			return newOpencodeGoCapabilityMismatch(
				inbound,
				resolved,
				fmt.Sprintf(
					"%s field %q value %d would be increased to %d by the %s compatibility bridge",
					inbound,
					field,
					value,
					minMaxOutputTokens,
					targetName,
				),
			)
		}
	}
	return nil
}

func validateOpencodeAnthropicOutputConfig(rawConfig json.RawMessage, resolved OpencodeGoResolvedModel, targetName string) error {
	if isNullJSON(rawConfig) {
		return nil
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil // The normal Messages request parser owns malformed JSON reporting.
	}
	if field := firstUnsupportedJSONField(config, jsonFieldSet("effort")); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic output_config field %q is not preserved by the %s compatibility bridge", field, targetName))
	}
	return nil
}

func validateOpencodeAnthropicSystem(rawSystem json.RawMessage, resolved OpencodeGoResolvedModel, targetName string) error {
	if isNullJSON(rawSystem) {
		return nil
	}
	var text string
	if err := json.Unmarshal(rawSystem, &text); err == nil {
		return nil
	}
	return validateOpencodeAnthropicBlocks(rawSystem, "system", resolved, targetName)
}

func validateOpencodeAnthropicMessages(rawMessages json.RawMessage, resolved OpencodeGoResolvedModel, targetName string) error {
	if isNullJSON(rawMessages) {
		return nil
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return nil // The normal Messages request parser owns malformed JSON reporting.
	}
	for index, rawMessage := range messages {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(rawMessage, &message); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic message %d is not an object", index))
		}
		if field := firstUnsupportedJSONField(message, jsonFieldSet("role", "content")); field != "" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic message field %q is not preserved by the %s compatibility bridge", field, targetName))
		}
		role := strings.ToLower(strings.TrimSpace(rawJSONString(message["role"])))
		if role != "user" && role != "assistant" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic message role %q is not supported by the %s compatibility bridge", role, targetName))
		}
		if err := validateOpencodeAnthropicContent(message["content"], role, resolved, targetName); err != nil {
			return err
		}
	}
	return nil
}

func validateOpencodeAnthropicContent(rawContent json.RawMessage, role string, resolved OpencodeGoResolvedModel, targetName string) error {
	if isNullJSON(rawContent) {
		return nil
	}
	var text string
	if err := json.Unmarshal(rawContent, &text); err == nil {
		return nil
	}
	return validateOpencodeAnthropicBlocks(rawContent, role, resolved, targetName)
}

func validateOpencodeAnthropicBlocks(rawBlocks json.RawMessage, role string, resolved OpencodeGoResolvedModel, targetName string) error {
	var blocks []json.RawMessage
	if err := json.Unmarshal(rawBlocks, &blocks); err != nil {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic content for role %q must be a string or an array of supported blocks", role))
	}
	for index, rawBlock := range blocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic content block %d for role %q is not an object", index, role))
		}
		blockType := strings.ToLower(strings.TrimSpace(rawJSONString(block["type"])))
		switch blockType {
		case "text":
			if field := firstUnsupportedJSONField(block, jsonFieldSet("type", "text")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic text block field %q is not preserved by the %s compatibility bridge", field, targetName))
			}
		case "image":
			if role != "user" && role != "tool_result" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic image blocks for role %q cannot be represented losslessly by %s", role, targetName))
			}
			if field := firstUnsupportedJSONField(block, jsonFieldSet("type", "source")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic image block field %q is not preserved by the %s compatibility bridge", field, targetName))
			}
			if err := validateOpencodeAnthropicImageSource(block["source"], resolved, targetName); err != nil {
				return err
			}
		case "tool_use":
			if role != "assistant" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_use blocks for role %q cannot be represented losslessly by %s", role, targetName))
			}
			if field := firstUnsupportedJSONField(block, jsonFieldSet("type", "id", "name", "input")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_use field %q is not preserved by the %s compatibility bridge", field, targetName))
			}
		case "tool_result":
			if role != "user" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_result blocks for role %q cannot be represented losslessly by %s", role, targetName))
			}
			if field := firstUnsupportedJSONField(block, jsonFieldSet("type", "tool_use_id", "content", "is_error")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_result field %q is not preserved by the %s compatibility bridge", field, targetName))
			}
			if rawIsError := block["is_error"]; !isNullJSON(rawIsError) && rawJSONBool(rawIsError) {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_result is_error semantics cannot be preserved by %s", targetName))
			}
			if err := validateOpencodeAnthropicToolResultContent(block["content"], resolved, targetName); err != nil {
				return err
			}
		case "thinking", "redacted_thinking":
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic content block type %q cannot be replayed through %s", blockType, targetName))
		default:
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic content block type %q is not supported by the %s compatibility bridge", blockType, targetName))
		}
	}
	return nil
}

func validateOpencodeAnthropicToolResultContent(rawContent json.RawMessage, resolved OpencodeGoResolvedModel, targetName string) error {
	if isNullJSON(rawContent) {
		return nil
	}
	var text string
	if err := json.Unmarshal(rawContent, &text); err == nil {
		return nil
	}
	return validateOpencodeAnthropicBlocks(rawContent, "tool_result", resolved, targetName)
}

func validateOpencodeAnthropicImageSource(rawSource json.RawMessage, resolved OpencodeGoResolvedModel, targetName string) error {
	var source map[string]json.RawMessage
	if err := json.Unmarshal(rawSource, &source); err != nil {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, "Anthropic image source must be a base64 object")
	}
	if field := firstUnsupportedJSONField(source, jsonFieldSet("type", "media_type", "data")); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic image source field %q is not preserved by the %s compatibility bridge", field, targetName))
	}
	if strings.TrimSpace(rawJSONString(source["type"])) != "base64" || strings.TrimSpace(rawJSONString(source["data"])) == "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic image source cannot be represented losslessly by %s unless it contains base64 data", targetName))
	}
	return nil
}

func validateOpencodeAnthropicTools(rawTools json.RawMessage, resolved OpencodeGoResolvedModel, target OpencodeGoProtocol, targetName string) error {
	if isNullJSON(rawTools) {
		return nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil // The normal Messages request parser owns malformed JSON reporting.
	}
	for index, rawTool := range tools {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool %d is not an object", index))
		}
		toolType := strings.ToLower(strings.TrimSpace(rawJSONString(tool["type"])))
		if strings.HasPrefix(toolType, "web_search") {
			if target == OpencodeGoProtocolChat {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, "Anthropic server-side web search tools cannot be represented by Chat Completions")
			}
			if field := firstUnsupportedJSONField(tool, jsonFieldSet("type", "name")); field != "" {
				return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic web search field %q is not preserved by the Responses compatibility bridge", field))
			}
			continue
		}
		if toolType != "" && toolType != "custom" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool type %q is not supported by the %s compatibility bridge", toolType, targetName))
		}
		if field := firstUnsupportedJSONField(tool, jsonFieldSet("type", "name", "description", "input_schema")); field != "" {
			return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic function tool field %q is not preserved by the %s compatibility bridge", field, targetName))
		}
	}
	return nil
}

func validateOpencodeAnthropicToolChoice(rawChoice json.RawMessage, resolved OpencodeGoResolvedModel, targetName string) error {
	if isNullJSON(rawChoice) {
		return nil
	}
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(rawChoice, &choice); err != nil {
		return nil // The normal Messages request parser owns malformed JSON reporting.
	}
	if field := firstUnsupportedJSONField(choice, jsonFieldSet("type", "name")); field != "" {
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_choice field %q is not preserved by the %s compatibility bridge", field, targetName))
	}
	switch strings.TrimSpace(rawJSONString(choice["type"])) {
	case "auto", "any", "none", "tool":
		return nil
	default:
		return newOpencodeGoCapabilityMismatch(OpencodeGoProtocolMessages, resolved, fmt.Sprintf("Anthropic tool_choice type %q is not supported by the %s compatibility bridge", rawJSONString(choice["type"]), targetName))
	}
}

func hasOpencodeAnthropicCacheControl(payload map[string]json.RawMessage) bool {
	if hasCacheControlOnRawBlocks(payload["system"]) {
		return true
	}
	if rawTools := payload["tools"]; !isNullJSON(rawTools) {
		var tools []json.RawMessage
		if json.Unmarshal(rawTools, &tools) == nil {
			for _, rawTool := range tools {
				var tool map[string]json.RawMessage
				if json.Unmarshal(rawTool, &tool) == nil {
					if raw, exists := tool["cache_control"]; exists && !isNullJSON(raw) {
						return true
					}
				}
			}
		}
	}
	if rawMessages := payload["messages"]; !isNullJSON(rawMessages) {
		var messages []json.RawMessage
		if json.Unmarshal(rawMessages, &messages) == nil {
			for _, rawMessage := range messages {
				var message map[string]json.RawMessage
				if json.Unmarshal(rawMessage, &message) == nil && hasCacheControlOnRawBlocks(message["content"]) {
					return true
				}
			}
		}
	}
	return false
}

func hasCacheControlOnRawBlocks(rawBlocks json.RawMessage) bool {
	if isNullJSON(rawBlocks) {
		return false
	}
	var blocks []json.RawMessage
	if json.Unmarshal(rawBlocks, &blocks) != nil {
		return false
	}
	for _, rawBlock := range blocks {
		var block map[string]json.RawMessage
		if json.Unmarshal(rawBlock, &block) != nil {
			continue
		}
		if raw, exists := block["cache_control"]; exists && !isNullJSON(raw) {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(rawJSONString(block["type"])), "tool_result") && hasCacheControlOnRawBlocks(block["content"]) {
			return true
		}
	}
	return false
}

func isEmptyRawJSONArray(raw json.RawMessage) bool {
	var items []json.RawMessage
	return json.Unmarshal(raw, &items) == nil && len(items) == 0
}

func isNullJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func jsonFieldSet(fields ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}

func firstUnsupportedJSONField(object map[string]json.RawMessage, allowed map[string]struct{}) string {
	unsupported := make([]string, 0)
	for field, raw := range object {
		if _, ok := allowed[field]; ok || isNullJSON(raw) {
			continue
		}
		unsupported = append(unsupported, field)
	}
	sort.Strings(unsupported)
	if len(unsupported) == 0 {
		return ""
	}
	return unsupported[0]
}

func rawJSONString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func rawJSONBool(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func validateOpencodeJSONOptions(rawOptions json.RawMessage, allowed map[string]struct{}, inbound OpencodeGoProtocol, resolved OpencodeGoResolvedModel, label string) error {
	if isNullJSON(rawOptions) {
		return nil
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(rawOptions, &options); err != nil {
		return nil // The normal request parser owns malformed JSON reporting.
	}
	if field := firstUnsupportedJSONField(options, allowed); field != "" {
		return newOpencodeGoCapabilityMismatch(inbound, resolved, fmt.Sprintf("%s field %q is not preserved by the compatibility bridge", label, field))
	}
	return nil
}

func isOpencodeEmptyBase64DataURL(raw string) bool {
	if !strings.HasPrefix(raw, "data:") {
		return false
	}
	separator := strings.Index(raw, ";")
	if separator < 0 {
		return false
	}
	payload := raw[separator+1:]
	if !strings.HasPrefix(payload, "base64,") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(payload, "base64,")) == ""
}

func (s *OpenAIGatewayService) forwardOpencodeRawResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	resolved OpencodeGoResolvedModel,
	reqStream bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	if suffix := strings.TrimSpace(rawOpenAIResponsesRequestPathSuffix(c)); suffix != "" {
		requestPath := suffix
		if c != nil && c.Request != nil && c.Request.URL != nil && strings.TrimSpace(c.Request.URL.Path) != "" {
			requestPath = c.Request.URL.Path
		}
		return rejectOpencodeGoRoutingError(c, OpencodeGoProtocolResponses, newOpencodeGoUnsupportedPath(resolved, requestPath))
	}
	upstreamBody, err := sjson.SetBytes(body, "model", resolved.UpstreamModel)
	if err != nil {
		return nil, fmt.Errorf("rewrite OpenCode Go Responses model: %w", err)
	}
	SetActualOpenAIUpstreamEndpoint(c, opencodeResponsesRawEndpoint)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(
		upstreamBody,
		resolved.UpstreamModel,
		resolved.BillingModel,
		resolved.RequestedModel,
	)
	result, forwardErr := s.forwardOpenAIPassthroughWithOptions(
		ctx,
		c,
		account,
		upstreamBody,
		resolved.RequestedModel,
		reasoningEffort,
		reqStream,
		startTime,
		openAIPassthroughOptions{
			preserveRequestBody:             true,
			useOpenAIUpstreamFailoverPolicy: true,
		},
	)
	applyOpencodeGoResolvedModelToResult(result, resolved)
	return result, forwardErr
}

func applyOpencodeGoResolvedModelToResult(result *OpenAIForwardResult, resolved OpencodeGoResolvedModel) {
	if result == nil {
		return
	}
	result.Model = resolved.RequestedModel
	result.BillingModel = resolved.BillingModel
	result.UpstreamModel = resolved.UpstreamModel
}

func (s *OpenAIGatewayService) resolveOpenAIAPIKeyResponsesURL(account *Account) (string, error) {
	baseURL := account.GetOpenAIBaseURL()
	if account.IsOpencode() {
		baseURL = account.GetOpencodeBaseURL()
	}
	if strings.TrimSpace(baseURL) == "" {
		return openaiPlatformAPIURL, nil
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	return buildOpenAIResponsesURL(validatedURL), nil
}
