package admin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	tzpkg "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type RevenueHandler struct {
	revenueService *service.RevenueService
}

type createRevenueShareSettlementExportRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Timezone  string `json:"timezone"`
	Status    string `json:"status"`
	Search    string `json:"search"`
}

func NewRevenueHandler(revenueService *service.RevenueService) *RevenueHandler {
	return &RevenueHandler{revenueService: revenueService}
}

// GetSummary returns the read-only revenue management dashboard data.
// GET /api/v1/admin/revenue/summary
func (h *RevenueHandler) GetSummary(c *gin.Context) {
	params, ok := parseRevenueQueryParams(c, true)
	if !ok {
		return
	}
	stats, err := h.revenueService.GetSummary(c.Request.Context(), params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
}

// GetBreakdowns returns the expensive TopN revenue breakdown sections.
// GET /api/v1/admin/revenue/breakdowns
func (h *RevenueHandler) GetBreakdowns(c *gin.Context) {
	params, ok := parseRevenueQueryParams(c, false)
	if !ok {
		return
	}
	breakdowns, err := h.revenueService.GetBreakdowns(c.Request.Context(), params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, breakdowns)
}

func parseRevenueQueryParams(c *gin.Context, allowIncludeBreakdowns bool) (service.RevenueQueryParams, bool) {
	startTime, endTime := parseRevenueTimeRange(c)
	granularity := strings.ToLower(strings.TrimSpace(c.DefaultQuery("granularity", service.RevenueGranularityDay)))
	if granularity != service.RevenueGranularityDay && granularity != service.RevenueGranularityHour {
		response.BadRequest(c, "granularity must be day or hour")
		return service.RevenueQueryParams{}, false
	}
	if !endTime.After(startTime) {
		response.BadRequest(c, "end_date must be after start_date")
		return service.RevenueQueryParams{}, false
	}

	topLimit := 10
	if rawLimit := strings.TrimSpace(c.Query("top_limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "top_limit must be a positive integer")
			return service.RevenueQueryParams{}, false
		}
		topLimit = parsed
	}

	var userID *int64
	if rawUserID := strings.TrimSpace(c.Query("user_id")); rawUserID != "" {
		parsed, err := strconv.ParseInt(rawUserID, 10, 64)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "user_id must be a positive integer")
			return service.RevenueQueryParams{}, false
		}
		userID = &parsed
	}

	skipBreakdowns := false
	if allowIncludeBreakdowns {
		if rawInclude := strings.TrimSpace(c.Query("include_breakdowns")); rawInclude != "" {
			includeBreakdowns, err := strconv.ParseBool(rawInclude)
			if err != nil {
				response.BadRequest(c, "include_breakdowns must be true or false")
				return service.RevenueQueryParams{}, false
			}
			skipBreakdowns = !includeBreakdowns
		}
	}

	return service.RevenueQueryParams{
		StartTime:      startTime,
		EndTime:        endTime,
		Granularity:    granularity,
		Timezone:       normalizeRevenueTimezone(c.Query("timezone")),
		TopLimit:       topLimit,
		UserID:         userID,
		SkipBreakdowns: skipBreakdowns,
	}, true
}

// ListShareSettlements returns auditable account-share settlement entries.
// GET /api/v1/admin/revenue/share-settlements
func (h *RevenueHandler) ListShareSettlements(c *gin.Context) {
	startTime, endTime := parseRevenueTimeRange(c)
	if !endTime.After(startTime) {
		response.BadRequest(c, "end_date must be after start_date")
		return
	}
	page, pageSize := response.ParsePagination(c)
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := h.revenueService.ListShareSettlements(c.Request.Context(), service.RevenueShareSettlementQueryParams{
		StartTime: startTime,
		EndTime:   endTime,
		Page:      page,
		PageSize:  pageSize,
		Search:    c.Query("search"),
		Status:    c.Query("status"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

// CreateShareSettlementExport creates an async export task for the current filter.
// POST /api/v1/admin/revenue/share-settlements/exports
func (h *RevenueHandler) CreateShareSettlementExport(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	var req createRevenueShareSettlementExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	startTime, endTime, ok := parseRevenueExportTimeRange(c, req.StartDate, req.EndDate, req.Timezone)
	if !ok {
		return
	}
	task, err := h.revenueService.CreateShareSettlementExport(c.Request.Context(), service.RevenueShareSettlementExportParams{
		StartTime: startTime,
		EndTime:   endTime,
		Timezone:  normalizeRevenueTimezone(req.Timezone),
		Status:    req.Status,
		Search:    req.Search,
	}, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

// GetShareSettlementExport returns export progress.
// GET /api/v1/admin/revenue/share-settlements/exports/:id
func (h *RevenueHandler) GetShareSettlementExport(c *gin.Context) {
	taskID, ok := parseRevenueExportTaskID(c)
	if !ok {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	task, err := h.revenueService.GetShareSettlementExportTask(c.Request.Context(), taskID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

// CancelShareSettlementExport cancels a pending or running export task.
// POST /api/v1/admin/revenue/share-settlements/exports/:id/cancel
func (h *RevenueHandler) CancelShareSettlementExport(c *gin.Context) {
	taskID, ok := parseRevenueExportTaskID(c)
	if !ok {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	task, err := h.revenueService.CancelShareSettlementExport(c.Request.Context(), taskID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

// DownloadShareSettlementExport streams a completed export file.
// GET /api/v1/admin/revenue/share-settlements/exports/:id/download
func (h *RevenueHandler) DownloadShareSettlementExport(c *gin.Context) {
	taskID, ok := parseRevenueExportTaskID(c)
	if !ok {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	download, err := h.revenueService.GetShareSettlementExportDownload(c.Request.Context(), taskID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	file, err := os.Open(download.Path)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = file.Close() }()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeRevenueExportFilename(download.FileName)))
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(200, download.Size, download.ContentType, file, nil)
}

func parseRevenueTimeRange(c *gin.Context) (time.Time, time.Time) {
	userTZ := c.Query("timezone")
	now := tzpkg.NowInUserLocation(userTZ)
	startTime := tzpkg.StartOfDayInUserLocation(now, userTZ)
	endTime := tzpkg.StartOfDayInUserLocation(now.AddDate(0, 0, 1), userTZ)

	if startDate := strings.TrimSpace(c.Query("start_date")); startDate != "" {
		if parsed, err := tzpkg.ParseInUserLocation("2006-01-02", startDate, userTZ); err == nil {
			startTime = parsed
		}
	}
	if endDate := strings.TrimSpace(c.Query("end_date")); endDate != "" {
		if parsed, err := tzpkg.ParseInUserLocation("2006-01-02", endDate, userTZ); err == nil {
			endTime = parsed.Add(24 * time.Hour)
		}
	}

	return startTime, endTime
}

func parseRevenueExportTimeRange(c *gin.Context, startDate, endDate, userTZ string) (time.Time, time.Time, bool) {
	startDate = strings.TrimSpace(startDate)
	endDate = strings.TrimSpace(endDate)
	if startDate == "" || endDate == "" {
		response.BadRequest(c, "start_date and end_date are required")
		return time.Time{}, time.Time{}, false
	}
	startTime, err := tzpkg.ParseInUserLocation("2006-01-02", startDate, userTZ)
	if err != nil {
		response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
		return time.Time{}, time.Time{}, false
	}
	endTime, err := tzpkg.ParseInUserLocation("2006-01-02", endDate, userTZ)
	if err != nil {
		response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
		return time.Time{}, time.Time{}, false
	}
	return startTime, endTime.Add(24 * time.Hour), true
}

func parseRevenueExportTaskID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid export task id")
		return 0, false
	}
	return id, true
}

func safeRevenueExportFilename(filename string) string {
	name := strings.TrimSpace(filepath.Base(filename))
	if name == "" || name == "." || strings.Contains(name, "\x00") {
		return "share-settlements-export.csv.gz"
	}
	return strings.NewReplacer("/", "_", "\\", "_", "\r", "_", "\n", "_").Replace(name)
}

func normalizeRevenueTimezone(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if _, err := time.LoadLocation(raw); err == nil {
			return raw
		}
	}
	name := tzpkg.Name()
	if name != "" && name != "Local" {
		if _, err := time.LoadLocation(name); err == nil {
			return name
		}
	}
	return "UTC"
}
