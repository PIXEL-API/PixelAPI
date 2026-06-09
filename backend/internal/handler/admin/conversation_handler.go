package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type ConversationHandler struct {
	conversationService *service.ConversationService
}

func NewConversationHandler(conversationService *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{conversationService: conversationService}
}

type CreateConversationRequest struct {
	UserID             int64  `json:"user_id" binding:"required"`
	Subject            string `json:"subject" binding:"required"`
	Content            string `json:"content" binding:"required"`
	Kind               string `json:"kind"`
	Priority           string `json:"priority"`
	Type               string `json:"type"`
	Source             string `json:"source"`
	SourceID           string `json:"source_id"`
	ReferencedNoticeID *int64 `json:"referenced_notice_id"`
	ContentFormat      string `json:"content_format"`
}

type AddConversationMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

type MarkConversationReadRequest struct {
	ReadUntilMessageID *int64 `json:"read_until_message_id"`
}

type UpdateConversationStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

type UpdateConversationAssigneeRequest struct {
	AssignedAdminID *int64 `json:"assigned_admin_id"`
}

func (h *ConversationHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	userID, _ := strconv.ParseInt(c.Query("user_id"), 10, 64)
	assignedAdminID, _ := strconv.ParseInt(c.Query("assigned_admin_id"), 10, 64)

	items, pageInfo, err := h.conversationService.ListAdmin(
		c.Request.Context(),
		pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortBy:    c.DefaultQuery("sort_by", "last_message_at"),
			SortOrder: c.DefaultQuery("sort_order", "desc"),
		},
		service.ConversationListFilters{
			UserID:          userID,
			Status:          c.Query("status"),
			Kind:            c.Query("kind"),
			Priority:        c.Query("priority"),
			Type:            c.Query("type"),
			AssignedAdminID: assignedAdminID,
			Search:          c.Query("search"),
			UnreadOnly:      parseBoolQuery(c.Query("unread_only")),
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, dto.ConversationsFromService(items), pageInfo.Total, page, pageSize)
}

func (h *ConversationHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req CreateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	input := &service.CreateConversationInput{
		UserID:             req.UserID,
		Subject:            req.Subject,
		Content:            req.Content,
		Kind:               req.Kind,
		Priority:           req.Priority,
		Type:               req.Type,
		Source:             req.Source,
		SourceID:           req.SourceID,
		ReferencedNoticeID: req.ReferencedNoticeID,
		ContentFormat:      req.ContentFormat,
		ActorID:            subject.UserID,
	}
	var out *service.Conversation
	var err error
	if req.Kind == service.ConversationKindSystemNotice {
		out, err = h.conversationService.CreateSystemNotice(c.Request.Context(), input)
	} else {
		out, err = h.conversationService.CreateByAdmin(c.Request.Context(), input)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.ConversationFromService(out))
}

func (h *ConversationHandler) Get(c *gin.Context) {
	id, ok := conversationID(c)
	if !ok {
		return
	}
	out, err := h.conversationService.GetAdmin(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ConversationFromService(out))
}

func (h *ConversationHandler) ListMessages(c *gin.Context) {
	id, ok := conversationID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageInfo, err := h.conversationService.ListMessagesAdmin(
		c.Request.Context(),
		id,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
		service.ConversationMessageListFilters{
			BeforeID: parseInt64Query(c.Query("before_id")),
			Latest:   parseBoolQuery(c.Query("latest")),
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, dto.ConversationMessagesFromService(items), pageInfo.Total, page, pageSize)
}

func (h *ConversationHandler) AddMessage(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := conversationID(c)
	if !ok {
		return
	}

	var req AddConversationMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	out, err := h.conversationService.AddAdminMessage(c.Request.Context(), &service.AddConversationMessageInput{
		ConversationID: id,
		ActorID:        subject.UserID,
		Content:        req.Content,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.ConversationFromService(out))
}

func (h *ConversationHandler) MarkRead(c *gin.Context) {
	id, ok := conversationID(c)
	if !ok {
		return
	}
	req, ok := bindOptionalMarkReadRequest(c)
	if !ok {
		return
	}
	out, err := h.conversationService.MarkReadAdmin(c.Request.Context(), id, req.ReadUntilMessageID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ConversationFromService(out))
}

func (h *ConversationHandler) UpdateStatus(c *gin.Context) {
	id, ok := conversationID(c)
	if !ok {
		return
	}
	var req UpdateConversationStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.conversationService.UpdateStatusAdmin(c.Request.Context(), id, req.Status)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ConversationFromService(out))
}

func (h *ConversationHandler) UpdateAssignee(c *gin.Context) {
	id, ok := conversationID(c)
	if !ok {
		return
	}
	var req UpdateConversationAssigneeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.conversationService.UpdateAssigneeAdmin(c.Request.Context(), id, req.AssignedAdminID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ConversationFromService(out))
}

func (h *ConversationHandler) UnreadCount(c *gin.Context) {
	count, err := h.conversationService.CountUnreadForAdmin(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

func conversationID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid conversation ID")
		return 0, false
	}
	return id, true
}

func parseBoolQuery(v string) bool {
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

func bindOptionalMarkReadRequest(c *gin.Context) (MarkConversationReadRequest, bool) {
	var req MarkConversationReadRequest
	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return req, false
		}
	}
	return req, true
}

func parseInt64Query(v string) int64 {
	if v == "" {
		return 0
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return -1
	}
	return id
}
