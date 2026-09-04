package handler

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const openAIResponsesBodyBudgetErrorCode = "gateway_memory_budget_exhausted"

var errOpenAIResponsesBodyReadTimeout = errors.New("openai responses request body read timed out")
var errOpenAIResponsesBodyReadDeadlineUnavailable = errors.New("openai responses request body read deadline unavailable")

func (h *OpenAIGatewayHandler) acquireResponsesBodyBudget(
	c *gin.Context,
	reqLog *zap.Logger,
) (*service.OpenAIResponsesBodyBudgetLease, bool) {
	if h == nil || h.responsesBodyBudget == nil {
		if h != nil && h.responsesBodyBudgetInitErr != nil {
			if reqLog != nil {
				reqLog.Error("openai.responses_body_budget_init_failed", zap.Error(h.responsesBodyBudgetInitErr))
			}
			h.errorResponse(c, http.StatusInternalServerError, "api_error", "Request body protection is unavailable")
			return nil, false
		}
		return nil, true
	}

	reservation, err := openAIResponsesBodyReservation(c.Request, h.cfg.Gateway.MaxBodySize)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, false
		}
		if reqLog != nil {
			reqLog.Error("openai.responses_body_budget_reservation_failed", zap.Error(err))
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body framing")
		return nil, false
	}

	lease, err := h.responsesBodyBudget.Acquire(c.Request.Context(), reservation)
	if err == nil {
		return lease, true
	}
	if c.Request.Context().Err() != nil {
		return nil, false
	}

	snapshot := h.responsesBodyBudget.Snapshot()
	if reqLog != nil {
		reqLog.Warn("openai.responses_body_budget_rejected",
			zap.Int64("reservation_bytes", reservation),
			zap.Int64("capacity_bytes", snapshot.CapacityBytes),
			zap.Int64("in_use_bytes", snapshot.InUseBytes),
			zap.Int64("waiters", snapshot.Waiters),
			zap.String("content_encoding", normalizedOpenAIRequestContentEncoding(c.Request)),
			zap.Error(err),
		)
	}
	retryAfter := h.responsesBodyBudget.RetryAfterSeconds()
	if retryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": gin.H{
			"type":    "server_error",
			"code":    openAIResponsesBodyBudgetErrorCode,
			"message": "Server request-body capacity is temporarily exhausted; retry later",
		},
	})
	return nil, false
}

func openAIResponsesBodyReservation(req *http.Request, maxBodyBytes int64) (int64, error) {
	if req == nil {
		return 0, errors.New("request is nil")
	}
	if maxBodyBytes <= 0 {
		return 0, errors.New("maximum body size is not positive")
	}
	if req.ContentLength > maxBodyBytes {
		return 0, &http.MaxBytesError{Limit: maxBodyBytes}
	}
	encoding := normalizedOpenAIRequestContentEncoding(req)
	switch encoding {
	case "zstd":
		return pkghttputil.MaxZstdRequestBodyMemoryReservation, nil
	case "gzip", "x-gzip", "deflate":
		return pkghttputil.MaxCompressedRequestBodyMemoryReservation, nil
	default:
		if encoding != "" && encoding != "identity" {
			// The decoder will reject unsupported encodings. Reserve the worst
			// supported footprint until that validation occurs.
			return pkghttputil.MaxZstdRequestBodyMemoryReservation, nil
		}
	}
	if req.ContentLength > 0 && len(req.TransferEncoding) == 0 {
		return req.ContentLength, nil
	}
	// Unknown, chunked, or zero-length framing is reserved pessimistically.
	// The bounded reader can retain its previous half-sized allocation while
	// growing the final buffer, so reserve that complete live-slice bound.
	return pkghttputil.BoundedRequestBodyMemoryReservation(maxBodyBytes), nil
}

func normalizedOpenAIRequestContentEncoding(req *http.Request) string {
	if req == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
}

func (h *OpenAIGatewayHandler) readResponsesRequestBody(writer http.ResponseWriter, req *http.Request) ([]byte, error) {
	readBody := func() ([]byte, error) {
		if h != nil && h.cfg != nil && h.cfg.Gateway.MaxBodySize > 0 {
			return pkghttputil.ReadRequestBodyWithLimit(req, h.cfg.Gateway.MaxBodySize)
		}
		return pkghttputil.ReadRequestBodyWithPrealloc(req)
	}
	if h == nil || h.responsesBodyBudget == nil || h.responsesBodyBudget.ReadTimeout() <= 0 {
		body, err := readBody()
		if err != nil && writer != nil {
			// Even with the admission gate disabled, fail a partially consumed
			// request closed. This is best-effort because deadline support is only
			// mandatory when the configured body-protection feature is enabled.
			_ = http.NewResponseController(writer).SetReadDeadline(time.Now().Add(-time.Second))
		}
		return body, err
	}
	if req == nil || req.Body == nil {
		return nil, nil
	}

	controller := http.NewResponseController(writer)
	deadline := time.Now().Add(h.responsesBodyBudget.ReadTimeout())
	if err := controller.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("%w: %v", errOpenAIResponsesBodyReadDeadlineUnavailable, err)
	}

	body, err := readBody()
	if err != nil {
		// Any decode/read failure can leave unread client bytes behind. Expire the
		// deadline immediately so net/http cannot wait for the original timeout
		// while closing or draining the request after the handler returns.
		if deadlineErr := controller.SetReadDeadline(time.Now().Add(-time.Second)); deadlineErr != nil {
			return nil, fmt.Errorf("%w: expire deadline after body read failure: %v (read error: %v)", errOpenAIResponsesBodyReadDeadlineUnavailable, deadlineErr, err)
		}
		if isOpenAIResponsesBodyReadTimeout(err) {
			return nil, fmt.Errorf("%w: %v", errOpenAIResponsesBodyReadTimeout, err)
		}
		return nil, err
	}
	if clearErr := controller.SetReadDeadline(time.Time{}); clearErr != nil {
		return nil, fmt.Errorf("%w: clear deadline: %v", errOpenAIResponsesBodyReadDeadlineUnavailable, clearErr)
	}
	return body, nil
}

func markOpenAIResponsesRequestConnectionUnusable(writer http.ResponseWriter, req *http.Request) {
	if req == nil {
		return
	}
	req.Close = true
	// Connection is a hop-by-hop HTTP/1 header. Emitting it on HTTP/2 can ask
	// the server to drain the entire multiplexed connection instead of only
	// terminating this failed request stream.
	if req.ProtoMajor == 1 && writer != nil {
		writer.Header().Set("Connection", "close")
	}
}

func isOpenAIResponsesBodyReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
