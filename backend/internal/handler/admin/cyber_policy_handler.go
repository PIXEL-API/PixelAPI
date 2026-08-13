package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type cyberPolicyRestrictionService interface {
	GetCyberPolicyRestriction(ctx context.Context, userID, effectiveGroupID int64) (service.CyberPolicyBlockState, error)
	ClearCyberPolicyRestriction(ctx context.Context, userID, effectiveGroupID int64) (bool, error)
}

type cyberPolicyRequestService interface {
	ListCyberPolicyRequests(ctx context.Context, filter service.CyberPolicyRequestFilter) (*service.CyberPolicyRequestList, error)
	GetCyberPolicyRequestByID(ctx context.Context, id int64) (*service.CyberPolicyRequestDetail, error)
	ExportCyberPolicyRequests(ctx context.Context, filter service.CyberPolicyRequestFilter) ([]*service.CyberPolicyRequestDetail, bool, error)
}

type CyberPolicyHandler struct {
	service    cyberPolicyRestrictionService
	opsService cyberPolicyRequestService
}

func NewCyberPolicyHandler(svc *service.OpenAIGatewayService, opsService *service.OpsService) *CyberPolicyHandler {
	return &CyberPolicyHandler{service: svc, opsService: opsService}
}

type cyberPolicyRestrictionResponse struct {
	UserID            int64                         `json:"user_id"`
	GroupID           int64                         `json:"group_id"`
	Blocked           bool                          `json:"blocked"`
	Scope             service.CyberPolicyBlockScope `json:"scope"`
	BlockedUntil      *time.Time                    `json:"blocked_until"`
	RetryAfterSeconds int64                         `json:"retry_after_seconds"`
}

type clearCyberPolicyRestrictionResponse struct {
	UserID  int64 `json:"user_id"`
	GroupID int64 `json:"group_id"`
	Removed bool  `json:"removed"`
}

func parseCyberPolicyRestrictionIDs(c *gin.Context) (int64, int64, bool) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return 0, 0, false
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(c.Param("group_id")), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "Invalid group_id")
		return 0, 0, false
	}
	return userID, groupID, true
}

func (h *CyberPolicyHandler) GetRestriction(c *gin.Context) {
	userID, groupID, ok := parseCyberPolicyRestrictionIDs(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		response.InternalError(c, "Cyber policy restriction service unavailable")
		return
	}
	state, err := h.service.GetCyberPolicyRestriction(c.Request.Context(), userID, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	result := cyberPolicyRestrictionResponse{
		UserID:  userID,
		GroupID: groupID,
		Blocked: state.Blocked,
		Scope:   state.Scope,
	}
	if state.Blocked {
		if !state.BlockedUntil.IsZero() {
			blockedUntil := state.BlockedUntil
			result.BlockedUntil = &blockedUntil
		}
		if state.RetryAfter > 0 {
			result.RetryAfterSeconds = int64((state.RetryAfter + time.Second - 1) / time.Second)
		}
	}
	response.Success(c, result)
}

func (h *CyberPolicyHandler) ClearRestriction(c *gin.Context) {
	userID, groupID, ok := parseCyberPolicyRestrictionIDs(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		response.InternalError(c, "Cyber policy restriction service unavailable")
		return
	}
	removed, err := h.service.ClearCyberPolicyRestriction(c.Request.Context(), userID, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	adminSubject, _ := middleware2.GetAuthSubjectFromContext(c)
	logger.L().Info(
		"admin.cyber_policy_restriction_clear",
		zap.Int64("admin_user_id", adminSubject.UserID),
		zap.Int64("target_user_id", userID),
		zap.Int64("effective_group_id", groupID),
		zap.Bool("removed", removed),
	)
	response.Success(c, clearCyberPolicyRestrictionResponse{
		UserID:  userID,
		GroupID: groupID,
		Removed: removed,
	})
}

const cyberPolicyRequestMaxWindow = 31 * 24 * time.Hour

func (h *CyberPolicyHandler) ListRequests(c *gin.Context) {
	if h == nil || h.opsService == nil {
		response.InternalError(c, "Cyber Policy request service unavailable")
		return
	}
	filter, ok := parseCyberPolicyRequestFilter(c)
	if !ok {
		return
	}
	result, err := h.opsService.ListCyberPolicyRequests(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, result.Items, result.Total, result.Page, result.PageSize)
}

func (h *CyberPolicyHandler) GetRequest(c *gin.Context) {
	if h == nil || h.opsService == nil {
		response.InternalError(c, "Cyber Policy request service unavailable")
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid Cyber Policy request id")
		return
	}
	detail, err := h.opsService.GetCyberPolicyRequestByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, detail)
}

func (h *CyberPolicyHandler) ExportRequests(c *gin.Context) {
	if h == nil || h.opsService == nil {
		response.InternalError(c, "Cyber Policy request service unavailable")
		return
	}
	filter, ok := parseCyberPolicyRequestFilter(c)
	if !ok {
		return
	}
	items, truncated, err := h.opsService.ExportCyberPolicyRequests(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	var buffer bytes.Buffer
	_, _ = buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	header := []string{
		"时间", "请求 ID", "分组名称", "分组 ID", "用户名", "邮箱", "用户 ID",
		"API Key 名称", "API Key ID", "账号名称", "账号 ID", "请求模型", "上游模型",
		"入口端点", "上游端点", "HTTP 状态", "上游状态", "请求内容是否截断", "请求原始字节数",
		"上游错误消息", "请求内容（已脱敏，可能截断）",
	}
	if err := writer.Write(header); err != nil {
		response.ErrorFrom(c, fmt.Errorf("write Cyber Policy export header: %w", err))
		return
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		row := []string{
			item.CreatedAt.UTC().Format(time.RFC3339),
			sanitizeCyberPolicyCSVText(item.RequestID),
			sanitizeCyberPolicyCSVText(item.GroupName),
			formatCyberPolicyInt64Pointer(item.GroupID),
			sanitizeCyberPolicyCSVText(item.UserName),
			sanitizeCyberPolicyCSVText(item.UserEmail),
			formatCyberPolicyInt64Pointer(item.UserID),
			sanitizeCyberPolicyCSVText(item.APIKeyName),
			formatCyberPolicyInt64Pointer(item.APIKeyID),
			sanitizeCyberPolicyCSVText(item.AccountName),
			formatCyberPolicyInt64Pointer(item.AccountID),
			sanitizeCyberPolicyCSVText(item.RequestedModel),
			sanitizeCyberPolicyCSVText(item.UpstreamModel),
			sanitizeCyberPolicyCSVText(item.InboundEndpoint),
			sanitizeCyberPolicyCSVText(item.UpstreamEndpoint),
			strconv.Itoa(item.StatusCode),
			formatCyberPolicyIntPointer(item.UpstreamStatusCode),
			strconv.FormatBool(item.RequestContentTruncated),
			formatCyberPolicyIntPointer(item.RequestContentBytes),
			sanitizeCyberPolicyCSVText(item.UpstreamErrorMessage),
			sanitizeCyberPolicyCSVText(item.RequestContent),
		}
		if err := writer.Write(row); err != nil {
			response.ErrorFrom(c, fmt.Errorf("write Cyber Policy export row: %w", err))
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		response.ErrorFrom(c, fmt.Errorf("flush Cyber Policy export: %w", err))
		return
	}

	filename := fmt.Sprintf("cyber-policy-requests-%s.csv", time.Now().UTC().Format("20060102-150405"))
	adminSubject, _ := middleware2.GetAuthSubjectFromContext(c)
	logger.L().Info(
		"admin.cyber_policy_requests_export",
		zap.Int64("admin_user_id", adminSubject.UserID),
		zap.Int("exported_rows", len(items)),
		zap.Bool("truncated", truncated),
		zap.String("group_query", filter.GroupQuery),
		zap.String("user_query", filter.UserQuery),
		zap.Time("start_time", *filter.StartTime),
		zap.Time("end_time", *filter.EndTime),
	)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("X-Export-Filename", filename)
	c.Header("X-Export-Limit", strconv.Itoa(service.CyberPolicyRequestExportMaxRows))
	c.Header("X-Export-Truncated", strconv.FormatBool(truncated))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buffer.Bytes())
}

func parseCyberPolicyRequestFilter(c *gin.Context) (service.CyberPolicyRequestFilter, bool) {
	page, pageSize := response.ParsePagination(c)
	now := time.Now().UTC()
	end := now
	start := now.Add(-24 * time.Hour)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		parsed, _, err := parseContentModerationDate(raw)
		if err != nil {
			response.BadRequest(c, "Invalid from")
			return service.CyberPolicyRequestFilter{}, false
		}
		start = parsed
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		parsed, dateOnly, err := parseContentModerationDate(raw)
		if err != nil {
			response.BadRequest(c, "Invalid to")
			return service.CyberPolicyRequestFilter{}, false
		}
		if dateOnly {
			parsed = parsed.Add(24 * time.Hour)
		}
		end = parsed
	}
	if !start.Before(end) || end.Sub(start) > cyberPolicyRequestMaxWindow {
		response.BadRequest(c, "Cyber Policy request time range must be within 31 days")
		return service.CyberPolicyRequestFilter{}, false
	}
	return service.CyberPolicyRequestFilter{
		StartTime:  &start,
		EndTime:    &end,
		GroupQuery: truncateCyberPolicyQuery(c.Query("group_query"), 100),
		UserQuery:  truncateCyberPolicyQuery(c.Query("user_query"), 100),
		Model:      truncateCyberPolicyQuery(c.Query("model"), 255),
		Endpoint:   truncateCyberPolicyQuery(c.Query("endpoint"), 128),
		Page:       page,
		PageSize:   pageSize,
	}, true
}

func truncateCyberPolicyQuery(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}

func sanitizeCyberPolicyCSVText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func formatCyberPolicyInt64Pointer(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func formatCyberPolicyIntPointer(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}
