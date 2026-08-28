package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type IdeasHandler struct {
	ideasService *service.IdeasService
}

func NewIdeasHandler(ideasService *service.IdeasService) *IdeasHandler {
	return &IdeasHandler{ideasService: ideasService}
}

type ideaModerationRequest struct {
	Reason string `json:"reason"`
}

func (h *IdeasHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.ideasService.AdminList(c.Request.Context(), service.IdeaPostListParams{
		Page:     page,
		PageSize: pageSize,
		Sort:     c.Query("sort"),
		Keyword:  c.Query("keyword"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *IdeasHandler) Get(c *gin.Context) {
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.AdminGet(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) Approve(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.AdminApprove(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) Reject(c *gin.Context) {
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req ideaModerationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.ideasService.AdminReject(c.Request.Context(), id, subject.UserID, req.Reason)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) Hide(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.AdminHide(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) Restore(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.AdminRestore(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) RetryModeration(c *gin.Context) {
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.ModeratePost(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

// --- 举报 ---

func (h *IdeasHandler) ListReports(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.ideasService.AdminListReports(c.Request.Context(), c.Query("status"), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *IdeasHandler) ResolveReport(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.ideasService.AdminResolveReport(c.Request.Context(), id, subject.UserID, req.Resolution); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// --- 标签治理 ---

func (h *IdeasHandler) ListTags(c *gin.Context) {
	tags, err := h.ideasService.AdminListTags(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tags)
}

func (h *IdeasHandler) CreateTag(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tag, err := h.ideasService.AdminCreateTag(c.Request.Context(), req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, tag)
}

func (h *IdeasHandler) UpdateTag(c *gin.Context) {
	id, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tag, err := h.ideasService.AdminUpdateTag(c.Request.Context(), id, req.Name, req.Status)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tag)
}

func (h *IdeasHandler) MergeTags(c *gin.Context) {
	sourceID, ok := parseAdminIdeaID(c)
	if !ok {
		return
	}
	var req struct {
		TargetTagID int64 `json:"target_tag_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.TargetTagID <= 0 {
		response.BadRequest(c, "Invalid target_tag_id")
		return
	}
	if err := h.ideasService.AdminMergeTags(c.Request.Context(), sourceID, req.TargetTagID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

func parseAdminIdeaID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid id")
		return 0, false
	}
	return id, true
}
