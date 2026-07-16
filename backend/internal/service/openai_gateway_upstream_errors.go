package service

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// writeOpenAIPassthroughErrorHeaders applies the minimal response-header policy
// for sanitized upstream errors. Arbitrary upstream headers must not cross the
// trust boundary; only a valid Retry-After value is useful to clients.
func writeOpenAIPassthroughErrorHeaders(dst, src http.Header) {
	if dst == nil {
		return
	}
	dst.Set("Content-Type", "application/json; charset=utf-8")
	dst.Set("Cache-Control", "no-store")
	dst.Del("Retry-After")
	if src == nil {
		return
	}

	retryAfter := strings.TrimSpace(src.Get("Retry-After"))
	if validOpenAIPassthroughRetryAfter(retryAfter, time.Now()) {
		dst.Set("Retry-After", retryAfter)
	}
}

func validOpenAIPassthroughRetryAfter(raw string, now time.Time) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	allDigits := true
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		seconds, err := strconv.ParseUint(raw, 10, 64)
		return err == nil && seconds > 0
	}

	retryAt, err := http.ParseTime(raw)
	return err == nil && retryAt.After(now)
}

func writeSanitizedOpenAIPassthroughError(c *gin.Context, upstreamStatus int, upstreamHeaders http.Header) {
	downstreamStatus := upstreamStatus
	message := "Upstream request failed"
	switch upstreamStatus {
	case http.StatusUnauthorized:
		downstreamStatus = http.StatusBadGateway
		message = "Upstream authentication failed"
	case http.StatusForbidden:
		downstreamStatus = http.StatusBadGateway
		message = "Upstream access denied"
	default:
		if upstreamStatus >= http.StatusInternalServerError {
			message = "Upstream service temporarily unavailable"
		}
	}
	writeOpenAIPassthroughErrorEnvelope(c, downstreamStatus, upstreamHeaders, message)
}

// writeOpenAIPassthroughErrorEnvelope writes a locally owned JSON envelope.
// The caller supplies either a generic message or a separately sanitized,
// actionable context-window message.
func writeOpenAIPassthroughErrorEnvelope(c *gin.Context, status int, upstreamHeaders http.Header, message string) {
	if c == nil {
		return
	}
	body, _ := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "upstream_error",
			"message": message,
		},
	})
	if writeOpenAICompactSSEBridge(c, status, body) {
		return
	}
	writeOpenAIPassthroughErrorHeaders(c.Writer.Header(), upstreamHeaders)
	c.Data(status, "application/json; charset=utf-8", body)
}
