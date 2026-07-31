package service

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func isOpenAIContextWindowError(upstreamMsg string, upstreamBody []byte) bool {
	match := func(text string) bool {
		lower := strings.ToLower(strings.TrimSpace(text))
		if lower == "" {
			return false
		}
		if strings.Contains(lower, "context_too_large") || strings.Contains(lower, "context_length_exceeded") {
			return true
		}
		if strings.Contains(lower, "maximum context length") || strings.Contains(lower, "max context length") {
			return true
		}
		hasExceeded := strings.Contains(lower, "exceed") || strings.Contains(lower, "too large") || strings.Contains(lower, "too long")
		if strings.Contains(lower, "context window") && hasExceeded {
			return true
		}
		if strings.Contains(lower, "context length") && hasExceeded {
			return true
		}
		return strings.Contains(lower, "token limit") && strings.Contains(lower, "context") && hasExceeded
	}

	if match(upstreamMsg) {
		return true
	}
	if len(upstreamBody) == 0 {
		return false
	}
	for _, path := range []string{
		"error.message",
		"response.error.message",
		"message",
		"error.code",
		"response.error.code",
		"code",
	} {
		if match(gjson.GetBytes(upstreamBody, path).String()) {
			return true
		}
	}
	return match(string(upstreamBody))
}

func openAIStreamFailedEventSemanticStatus(payload []byte, message string) int {
	if isOpenAIContextWindowError(message, payload) {
		return http.StatusBadRequest
	}
	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "response.error.code").String()))
	if code == "" {
		code = strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.code").String()))
	}
	errType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "response.error.type").String()))
	if errType == "" {
		errType = strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.type").String()))
	}
	combined := strings.TrimSpace(errType + " " + code + " " + strings.ToLower(strings.TrimSpace(message)))
	switch {
	case strings.Contains(errType, "invalid_request"):
		return http.StatusBadRequest
	case strings.Contains(combined, "rate_limit"):
		return http.StatusTooManyRequests
	case strings.Contains(combined, "authentication"), strings.Contains(combined, "unauthorized"), strings.Contains(combined, "invalid_api_key"):
		return http.StatusUnauthorized
	case strings.Contains(combined, "permission"), strings.Contains(combined, "forbidden"), strings.Contains(combined, "access denied"):
		return http.StatusForbidden
	case code == "server_is_overloaded", code == "slow_down":
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func openAIStreamFailedEventPassthroughBody(payload []byte, failedMessage string) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}
	if gjson.GetBytes(payload, "error").Exists() {
		return payload
	}
	responseError := gjson.GetBytes(payload, "response.error")
	if !responseError.Exists() {
		if strings.TrimSpace(failedMessage) == "" {
			return payload
		}
		body, err := marshalOpenAIUpstreamJSON(gin.H{"error": gin.H{"message": failedMessage}})
		if err != nil {
			return payload
		}
		return body
	}
	errorPayload := gin.H{}
	for _, field := range []string{"type", "code", "param"} {
		if value := strings.TrimSpace(gjson.Get(responseError.Raw, field).String()); value != "" {
			errorPayload[field] = value
		}
	}
	message := strings.TrimSpace(gjson.Get(responseError.Raw, "message").String())
	if message == "" {
		message = strings.TrimSpace(failedMessage)
	}
	if message != "" {
		errorPayload["message"] = message
	}
	if len(errorPayload) == 0 {
		return payload
	}
	body, err := marshalOpenAIUpstreamJSON(gin.H{"error": errorPayload})
	if err != nil {
		return payload
	}
	return body
}

// sanitizeOpenAIResponseFailedEventForClient removes verbose request and response
// fields before a response.failed event is relayed to the client. Once output has
// started, a context-window failure can no longer be converted to an HTTP error,
// so normalize its error metadata in-place while preserving the Responses SSE
// protocol.
func sanitizeOpenAIResponseFailedEventForClient(payload []byte, eventType string, clientOutputStarted bool) ([]byte, bool) {
	if eventType != "response.failed" || len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload, false
	}

	updated := payload
	if clientOutputStarted && isOpenAIContextWindowError(extractOpenAISSEErrorMessage(payload), payload) {
		errorPath := ""
		switch {
		case gjson.GetBytes(updated, "response.error").Exists():
			errorPath = "response.error"
		case gjson.GetBytes(updated, "error").Exists():
			errorPath = "error"
		}
		if errorPath != "" {
			next, err := sjson.SetBytes(updated, errorPath+".type", "invalid_request_error")
			if err != nil {
				return payload, false
			}
			updated = next
			next, err = sjson.SetBytes(updated, errorPath+".code", "context_length_exceeded")
			if err != nil {
				return payload, false
			}
			updated = next
		}
	}

	if !gjson.GetBytes(updated, "response").Exists() {
		return updated, !bytes.Equal(updated, payload)
	}
	for _, path := range []string{
		"response.instructions",
		"response.output",
		"response.usage",
		"response.metadata",
		"response.reasoning",
		"response.tools",
		"response.tool_choice",
		"response.parallel_tool_calls",
		"response.text",
		"response.truncation",
		"response.max_output_tokens",
		"response.incomplete_details",
	} {
		next, err := sjson.DeleteBytes(updated, path)
		if err != nil {
			return payload, false
		}
		updated = next
	}
	return updated, !bytes.Equal(updated, payload)
}

func applyOpenAIStreamFailedErrorPassthroughRule(c *gin.Context, platform string, payload []byte, failedMessage string) (status int, errType, errMsg string, matched bool) {
	return applyErrorPassthroughRule(
		c,
		platform,
		openAIStreamFailedEventSemanticStatus(payload, failedMessage),
		openAIStreamFailedEventPassthroughBody(payload, failedMessage),
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	)
}

func (s *OpenAIGatewayService) recordOpenAIStreamUpstreamError(c *gin.Context, account *Account, passthrough bool, upstreamRequestID, kind string, payload []byte, message string) string {
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if message == "" {
		message = "OpenAI upstream response failed"
	}
	detail := ""
	if len(payload) > 0 && s != nil && s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		detail = truncateString(string(payload), maxBytes)
	}
	semanticStatus := openAIStreamFailedEventSemanticStatus(payload, message)
	if c != nil {
		setOpsUpstreamError(c, semanticStatus, message, detail)
		event := OpsUpstreamErrorEvent{
			Platform:           PlatformOpenAI,
			UpstreamStatusCode: semanticStatus,
			UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
			Passthrough:        passthrough,
			Kind:               kind,
			Message:            message,
			Detail:             detail,
		}
		if account != nil {
			event.Platform = account.Platform
			event.AccountID = account.ID
			event.AccountName = account.Name
		}
		appendOpsUpstreamError(c, event)
	}
	return message
}
