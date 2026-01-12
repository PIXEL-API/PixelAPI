package admin

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type OpsHandler struct {
	opsService *service.OpsService
}

func NewOpsHandler(opsService *service.OpsService) *OpsHandler {
	return &OpsHandler{opsService: opsService}
}

// GetErrorLogs lists ops error logs.
// GET /api/v1/admin/ops/errors
func (h *OpsHandler) GetErrorLogs(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	page, pageSize := response.ParsePagination(c)
	// Ops list can be larger than standard admin tables.
	if pageSize > 500 {
		pageSize = 500
	}

	startTime, endTime, err := parseOpsTimeRange(c, "1h")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	filter := &service.OpsErrorLogFilter{
		Page:     page,
		PageSize: pageSize,
	}
	if !startTime.IsZero() {
		filter.StartTime = &startTime
	}
	if !endTime.IsZero() {
		filter.EndTime = &endTime
	}

	if platform := strings.TrimSpace(c.Query("platform")); platform != "" {
		filter.Platform = platform
	}
	if v := strings.TrimSpace(c.Query("group_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filter.GroupID = &id
	}
	if v := strings.TrimSpace(c.Query("account_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid account_id")
			return
		}
		filter.AccountID = &id
	}
	if phase := strings.TrimSpace(c.Query("phase")); phase != "" {
		filter.Phase = phase
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		filter.Query = q
	}
	if statusCodesStr := strings.TrimSpace(c.Query("status_codes")); statusCodesStr != "" {
		parts := strings.Split(statusCodesStr, ",")
		out := make([]int, 0, len(parts))
		for _, part := range parts {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 {
				response.BadRequest(c, "Invalid status_codes")
				return
			}
			out = append(out, n)
		}
		filter.StatusCodes = out
	}

	result, err := h.opsService.GetErrorLogs(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Paginated(c, result.Errors, int64(result.Total), result.Page, result.PageSize)
}

// GetErrorLogByID returns a single error log detail.
// GET /api/v1/admin/ops/errors/:id
func (h *OpsHandler) GetErrorLogByID(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	idStr := strings.TrimSpace(c.Param("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid error id")
		return
	}

	detail, err := h.opsService.GetErrorLogByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, detail)
}

// ListRequestDetails returns a request-level list (success + error) for drill-down.
// GET /api/v1/admin/ops/requests
func (h *OpsHandler) ListRequestDetails(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	page, pageSize := response.ParsePagination(c)
	if pageSize > 100 {
		pageSize = 100
	}

	startTime, endTime, err := parseOpsTimeRange(c, "1h")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	filter := &service.OpsRequestDetailFilter{
		Page:      page,
		PageSize:  pageSize,
		StartTime: &startTime,
		EndTime:   &endTime,
	}

	filter.Kind = strings.TrimSpace(c.Query("kind"))
	filter.Platform = strings.TrimSpace(c.Query("platform"))
	filter.Model = strings.TrimSpace(c.Query("model"))
	filter.RequestID = strings.TrimSpace(c.Query("request_id"))
	filter.Query = strings.TrimSpace(c.Query("q"))
	filter.Sort = strings.TrimSpace(c.Query("sort"))

	if v := strings.TrimSpace(c.Query("user_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		filter.UserID = &id
	}
	if v := strings.TrimSpace(c.Query("api_key_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		filter.APIKeyID = &id
	}
	if v := strings.TrimSpace(c.Query("account_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid account_id")
			return
		}
		filter.AccountID = &id
	}
	if v := strings.TrimSpace(c.Query("group_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filter.GroupID = &id
	}

	if v := strings.TrimSpace(c.Query("min_duration_ms")); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			response.BadRequest(c, "Invalid min_duration_ms")
			return
		}
		filter.MinDurationMs = &parsed
	}
	if v := strings.TrimSpace(c.Query("max_duration_ms")); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			response.BadRequest(c, "Invalid max_duration_ms")
			return
		}
		filter.MaxDurationMs = &parsed
	}

	out, err := h.opsService.ListRequestDetails(c.Request.Context(), filter)
	if err != nil {
		// Invalid sort/kind/platform etc should be a bad request; keep it simple.
		if strings.Contains(strings.ToLower(err.Error()), "invalid") {
			response.BadRequest(c, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "Failed to list request details")
		return
	}

	response.Paginated(c, out.Items, out.Total, out.Page, out.PageSize)
}

type opsRetryRequest struct {
	Mode            string `json:"mode"`
	PinnedAccountID *int64 `json:"pinned_account_id"`
}

// RetryErrorRequest retries a failed request using stored request_body.
// POST /api/v1/admin/ops/errors/:id/retry
func (h *OpsHandler) RetryErrorRequest(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := strings.TrimSpace(c.Param("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid error id")
		return
	}

	req := opsRetryRequest{Mode: service.OpsRetryModeClient}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = service.OpsRetryModeClient
	}

	result, err := h.opsService.RetryError(c.Request.Context(), subject.UserID, id, req.Mode, req.PinnedAccountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

func parseOpsTimeRange(c *gin.Context, defaultRange string) (time.Time, time.Time, error) {
	startStr := strings.TrimSpace(c.Query("start_time"))
	endStr := strings.TrimSpace(c.Query("end_time"))

	parseTS := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, nil
		}
		return time.Parse(time.RFC3339, s)
	}

	start, err := parseTS(startStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseTS(endStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	// start/end explicitly provided (even partially)
	if startStr != "" || endStr != "" {
		if end.IsZero() {
			end = time.Now()
		}
		if start.IsZero() {
			dur, _ := parseOpsDuration(defaultRange)
			start = end.Add(-dur)
		}
		if start.After(end) {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: start_time must be <= end_time")
		}
		if end.Sub(start) > 30*24*time.Hour {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: max window is 30 days")
		}
		return start, end, nil
	}

	// time_range fallback
	tr := strings.TrimSpace(c.Query("time_range"))
	if tr == "" {
		tr = defaultRange
	}
	dur, ok := parseOpsDuration(tr)
	if !ok {
		dur, _ = parseOpsDuration(defaultRange)
	}

	end = time.Now()
	start = end.Add(-dur)
	if end.Sub(start) > 30*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: max window is 30 days")
	}
	return start, end, nil
}

func parseOpsDuration(v string) (time.Duration, bool) {
	switch strings.TrimSpace(v) {
	case "5m":
		return 5 * time.Minute, true
	case "30m":
		return 30 * time.Minute, true
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "24h":
		return 24 * time.Hour, true
	default:
		return 0, false
	}
}
