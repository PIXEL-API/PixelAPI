package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const openAIUpstreamClientErrorFallbackType = "invalid_request_error"
const openAIUpstreamClientErrorFallbackMessage = "Upstream rejected the request"

// isOpenAIDeterministicClientError reports whether the upstream status means the
// request itself is invalid rather than the upstream account or network being
// temporarily unhealthy.
func isOpenAIDeterministicClientError(statusCode int) bool {
	return statusCode == http.StatusBadRequest
}

func writeOpenAIUpstreamClientError(c *gin.Context, statusCode int, body []byte, upstreamMsg string) {
	if c == nil {
		return
	}

	errorPayload := gin.H{
		"type": openAIUpstreamClientErrorFallbackType,
	}
	if errType := strings.TrimSpace(gjson.GetBytes(body, "error.type").String()); errType != "" {
		errorPayload["type"] = errType
	}
	if code := strings.TrimSpace(extractUpstreamErrorCode(body)); code != "" {
		errorPayload["code"] = code
	}
	if param := strings.TrimSpace(gjson.GetBytes(body, "error.param").String()); param != "" {
		errorPayload["param"] = param
	}

	message := strings.TrimSpace(upstreamMsg)
	if message == "" {
		message = openAIUpstreamClientErrorFallbackMessage
	}
	errorPayload["message"] = message

	if StopOpenAICompactSSEKeepaliveCommitted(c) {
		writeOpenAICompactSSEClientError(c, statusCode, errorPayload)
		return
	}
	c.JSON(statusCode, gin.H{"error": errorPayload})
}

func writeOpenAICompactSSEClientError(c *gin.Context, statusCode int, errorPayload gin.H) {
	if c == nil {
		return
	}

	errType, _ := errorPayload["type"].(string)
	errType = strings.TrimSpace(errType)
	if errType == "" {
		errType = openAIUpstreamClientErrorFallbackType
	}
	message, _ := errorPayload["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		message = openAIUpstreamClientErrorFallbackMessage
	}
	MarkOpsStreamError(c, errType, message, statusCode)

	payload, err := marshalOpenAIUpstreamJSON(gin.H{
		"type": "response.failed",
		"response": gin.H{
			"id":     "resp_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"object": "response",
			"status": "failed",
			"output": []any{},
			"error":  errorPayload,
		},
	})
	if err != nil {
		return
	}

	_, _ = c.Writer.Write([]byte("event: response.failed\ndata: "))
	_, _ = c.Writer.Write(payload)
	_, _ = c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}
