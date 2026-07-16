package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type userContentModerationTestRequest struct {
	Prompt string   `json:"prompt"`
	Images []string `json:"images"`
}

type userContentModerationTestResponse struct {
	Flagged         bool   `json:"flagged"`
	HighestCategory string `json:"highest_category"`
}

type userContentModerationLogResponse struct {
	ID              int64     `json:"id"`
	RequestID       string    `json:"request_id"`
	AccountID       int64     `json:"account_id"`
	Endpoint        string    `json:"endpoint"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Mode            string    `json:"mode"`
	Action          string    `json:"action"`
	Flagged         bool      `json:"flagged"`
	HighestCategory string    `json:"highest_category"`
	Sampled         bool      `json:"sampled"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"created_at"`
}

func (h *UserAccountHandler) GetModerationConfig(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, ok := parseUserModerationAccountID(c)
	if !ok {
		return
	}
	cfg, err := h.userModerationService.GetConfig(c.Request.Context(), subject.UserID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *UserAccountHandler) UpdateModerationConfig(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, ok := parseUserModerationAccountID(c)
	if !ok {
		return
	}
	var req service.UpdateUserContentModerationConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.userModerationService.UpdateConfig(c.Request.Context(), subject.UserID, accountID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *UserAccountHandler) TestModeration(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, ok := parseUserModerationAccountID(c)
	if !ok {
		return
	}
	var req userContentModerationTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.userModerationService.Test(c.Request.Context(), subject.UserID, accountID, req.Prompt, req.Images)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sanitizeUserContentModerationTestResult(result))
}

func (h *UserAccountHandler) ListModerationLogs(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	accountID, ok := parseUserModerationAccountID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.userModerationService.ListLogs(c.Request.Context(), subject.UserID, accountID, service.UserContentModerationLogFilter{
		Pagination: pagination.PaginationParams{
			Page:     page,
			PageSize: pageSize,
		},
		Result: c.Query("result"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, sanitizeUserContentModerationLogs(items), result.Total, result.Page, result.PageSize)
}

func parseUserModerationAccountID(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}

func sanitizeUserContentModerationTestResult(result *service.ContentModerationTestAuditResult) userContentModerationTestResponse {
	if result == nil {
		return userContentModerationTestResponse{}
	}
	return userContentModerationTestResponse{
		Flagged:         result.Flagged,
		HighestCategory: result.HighestCategory,
	}
}

func sanitizeUserContentModerationLogs(items []service.UserContentModerationLog) []userContentModerationLogResponse {
	out := make([]userContentModerationLogResponse, 0, len(items))
	for _, item := range items {
		out = append(out, userContentModerationLogResponse{
			ID:              item.ID,
			RequestID:       item.RequestID,
			AccountID:       item.AccountID,
			Endpoint:        item.Endpoint,
			Provider:        item.Provider,
			Model:           item.Model,
			Mode:            item.Mode,
			Action:          item.Action,
			Flagged:         item.Flagged,
			HighestCategory: item.HighestCategory,
			Sampled:         item.Sampled,
			Error:           item.Error,
			CreatedAt:       item.CreatedAt,
		})
	}
	return out
}
