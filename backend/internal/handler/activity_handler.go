package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type ActivityHandler struct {
	activityService *service.ActivityService
}

func NewActivityHandler(activityService *service.ActivityService) *ActivityHandler {
	return &ActivityHandler{activityService: activityService}
}

func (h *ActivityHandler) ListWelfareActivities(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	items, err := h.activityService.UserListWelfareActivities(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *ActivityHandler) ListMyWinners(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	items, err := h.activityService.UserListWinners(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *ActivityHandler) ListPublicWinners(c *gin.Context) {
	if _, ok := requireAuth(c); !ok {
		return
	}
	campaignID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || campaignID <= 0 {
		response.BadRequest(c, "Invalid activity id")
		return
	}
	page, pageSize := response.ParsePagination(c)
	if pageSize > 50 {
		pageSize = 50
	}
	items, total, err := h.activityService.UserListPublicWinners(c.Request.Context(), campaignID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *ActivityHandler) JoinDraw(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	campaignID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || campaignID <= 0 {
		response.BadRequest(c, "Invalid activity id")
		return
	}
	item, err := h.activityService.UserJoinDraw(c.Request.Context(), subject.UserID, campaignID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *ActivityHandler) SubmitWinnerClaim(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	winnerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || winnerID <= 0 {
		response.BadRequest(c, "Invalid winner id")
		return
	}
	var req struct {
		ClaimInfo map[string]any `json:"claim_info" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.activityService.UserSubmitWinnerClaim(c.Request.Context(), subject.UserID, winnerID, req.ClaimInfo)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
