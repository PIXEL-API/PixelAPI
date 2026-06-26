package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type ActivityHandler struct {
	activityService *service.ActivityService
}

func NewActivityHandler(activityService *service.ActivityService) *ActivityHandler {
	return &ActivityHandler{activityService: activityService}
}

func (h *ActivityHandler) ListCampaigns(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.activityService.AdminListCampaigns(c.Request.Context(), service.ActivityListParams{
		Page:     page,
		PageSize: pageSize,
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *ActivityHandler) GetCampaign(c *gin.Context) {
	id, ok := parseAdminActivityID(c, "id")
	if !ok {
		return
	}
	item, err := h.activityService.AdminGetCampaign(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *ActivityHandler) GetCampaignStats(c *gin.Context) {
	id, ok := parseAdminActivityID(c, "id")
	if !ok {
		return
	}
	item, err := h.activityService.AdminGetCampaignStats(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *ActivityHandler) CreateCampaign(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req service.ActivityCampaignUpsertInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.activityService.AdminCreateCampaign(c.Request.Context(), req, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, item)
}

func (h *ActivityHandler) UpdateCampaign(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, idOK := parseAdminActivityID(c, "id")
	if !idOK {
		return
	}
	var req service.ActivityCampaignUpsertInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.activityService.AdminUpdateCampaign(c.Request.Context(), id, req, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *ActivityHandler) EndCampaign(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, idOK := parseAdminActivityID(c, "id")
	if !idOK {
		return
	}
	if err := h.activityService.AdminEndCampaign(c.Request.Context(), id, subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ended"})
}

func (h *ActivityHandler) RunDraw(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, idOK := parseAdminActivityID(c, "id")
	if !idOK {
		return
	}
	result, err := h.activityService.AdminRunDraw(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ActivityHandler) ListWinners(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	campaignID := int64(0)
	if raw := c.Query("campaign_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			response.BadRequest(c, "Invalid campaign_id")
			return
		}
		campaignID = parsed
	}
	items, total, err := h.activityService.AdminListWinners(c.Request.Context(), campaignID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *ActivityHandler) MarkWinnerDelivered(c *gin.Context) {
	id, ok := parseAdminActivityID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.activityService.AdminMarkWinnerDelivered(c.Request.Context(), id, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *ActivityHandler) RejectWinner(c *gin.Context) {
	id, ok := parseAdminActivityID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.activityService.AdminRejectWinner(c.Request.Context(), id, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func parseAdminActivityID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}
