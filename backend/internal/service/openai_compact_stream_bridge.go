package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const openAICompactClientStreamKey = "openai_compact_client_stream"

// MarkOpenAICompactClientStream records the body-signal client's original stream intent.
func MarkOpenAICompactClientStream(c *gin.Context) {
	if c != nil {
		c.Set(openAICompactClientStreamKey, true)
	}
}

func OpenAICompactClientStreamKeyForTest() string {
	return openAICompactClientStreamKey
}

func openAICompactClientWantsStream(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(openAICompactClientStreamKey)
	if !ok {
		return false
	}
	wants, _ := value.(bool)
	return wants
}

// writeOpenAICompactSSEBridge converts a unary compact response back to Responses SSE.
func writeOpenAICompactSSEBridge(c *gin.Context, statusCode int, finalResponse []byte) bool {
	if c == nil || !openAICompactClientWantsStream(c) {
		return false
	}
	committed := StopOpenAICompactSSEKeepaliveCommitted(c)
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		if committed {
			writeOpenAICompactSSEFailure(c, statusCode, finalResponse)
			return true
		}
		return false
	}
	payload, ok := buildOpenAICompactSSEPayload(finalResponse)
	if !ok {
		if committed {
			writeOpenAICompactSSEFailure(c, http.StatusBadGateway, finalResponse)
			return true
		}
		return false
	}
	if !committed {
		header := c.Writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(statusCode)
	}
	_, _ = c.Writer.Write(payload)
	c.Writer.Flush()
	return true
}

func writeOpenAICompactSSEFailure(c *gin.Context, statusCode int, errorBody []byte) {
	message := ""
	if len(errorBody) > 0 {
		message = sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(errorBody)))
	}
	if message == "" {
		message = "Upstream compact request failed with HTTP " + strconv.Itoa(statusCode)
	}
	writeOpenAICompactSSEFailureMessage(c, statusCode, "upstream_error", message)
}

func writeOpenAICompactSSEFailureMessage(c *gin.Context, statusCode int, errType, message string) {
	if c == nil {
		return
	}
	MarkOpsStreamError(c, errType, message, statusCode)
	payload, err := json.Marshal(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id":     "resp_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"object": "response",
			"status": "failed",
			"output": []any{},
			"error": map[string]any{
				"code":    errType,
				"message": message,
			},
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

// WriteOpenAICompactSSEFailureForHandler lets the handler terminate a committed compact stream.
func WriteOpenAICompactSSEFailureForHandler(c *gin.Context, statusCode int, errType, message string) {
	writeOpenAICompactSSEFailureMessage(c, statusCode, errType, message)
}

func writeOpenAICompactAwareJSONError(c *gin.Context, statusCode int, errType, message string) {
	if StopOpenAICompactSSEKeepaliveCommitted(c) {
		writeOpenAICompactSSEFailureMessage(c, statusCode, errType, message)
		return
	}
	c.JSON(statusCode, gin.H{"error": gin.H{"type": errType, "message": message}})
}

func buildOpenAICompactSSEPayload(finalResponse []byte) ([]byte, bool) {
	if len(finalResponse) == 0 || !gjson.ValidBytes(finalResponse) || !gjson.ParseBytes(finalResponse).IsObject() {
		return nil, false
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, finalResponse); err != nil {
		return nil, false
	}
	response := compacted.Bytes()
	if strings.TrimSpace(gjson.GetBytes(response, "id").String()) == "" {
		next, err := sjson.SetBytes(response, "id", "resp_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
		if err != nil {
			return nil, false
		}
		response = next
	}
	if usage := gjson.GetBytes(response, "usage"); usage.Exists() && !openAICompactUsageParsableByCodex(usage) {
		next, err := sjson.DeleteBytes(response, "usage")
		if err != nil {
			return nil, false
		}
		response = next
	}

	var buf bytes.Buffer
	appendEvent := func(eventType string, data []byte) {
		_, _ = buf.WriteString("event: ")
		_, _ = buf.WriteString(eventType)
		_, _ = buf.WriteString("\ndata: ")
		_, _ = buf.Write(data)
		_, _ = buf.WriteString("\n\n")
	}
	outputIndex := 0
	for _, item := range gjson.GetBytes(response, "output").Array() {
		if !item.IsObject() {
			continue
		}
		event, err := sjson.SetBytes([]byte(`{"type":"response.output_item.done"}`), "output_index", outputIndex)
		if err != nil {
			return nil, false
		}
		event, err = sjson.SetRawBytes(event, "item", []byte(item.Raw))
		if err != nil {
			return nil, false
		}
		appendEvent("response.output_item.done", event)
		outputIndex++
	}
	completed, err := sjson.SetRawBytes([]byte(`{"type":"response.completed"}`), "response", response)
	if err != nil {
		return nil, false
	}
	appendEvent("response.completed", completed)
	return buf.Bytes(), true
}

func openAICompactUsageParsableByCodex(usage gjson.Result) bool {
	if !usage.IsObject() {
		return false
	}
	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if usage.Get(field).Type != gjson.Number {
			return false
		}
	}
	return true
}
