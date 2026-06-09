package handler

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
	Subject            string `json:"subject" binding:"required"`
	Content            string `json:"content" binding:"required"`
	Priority           string `json:"priority"`
	Type               string `json:"type"`
	ReferencedNoticeID *int64 `json:"referenced_notice_id"`
}

type AddConversationMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

type MarkConversationReadRequest struct {
	ReadUntilMessageID *int64 `json:"read_until_message_id"`
}

func (h *ConversationHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	items, pageInfo, err := h.conversationService.ListForUser(
		c.Request.Context(),
		subject.UserID,
		pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortBy:    c.DefaultQuery("sort_by", "last_message_at"),
			SortOrder: c.DefaultQuery("sort_order", "desc"),
		},
		service.ConversationListFilters{
			Status:     c.Query("status"),
			Kind:       c.Query("kind"),
			Type:       c.Query("type"),
			Search:     c.Query("search"),
			UnreadOnly: parseBoolQuery(c.Query("unread_only")),
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, dto.UserConversationsFromService(items), pageInfo.Total, page, pageSize)
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

	out, err := h.conversationService.CreateByUser(c.Request.Context(), &service.CreateConversationInput{
		UserID:             subject.UserID,
		Subject:            req.Subject,
		Content:            req.Content,
		ReferencedNoticeID: req.ReferencedNoticeID,
		Priority:           req.Priority,
		Type:               req.Type,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.UserConversationFromService(out))
}

func (h *ConversationHandler) Get(c *gin.Context) {
	userID, id, ok := h.userAndConversationID(c)
	if !ok {
		return
	}
	out, err := h.conversationService.GetForUser(c.Request.Context(), userID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserConversationFromService(out))
}

func (h *ConversationHandler) ListMessages(c *gin.Context) {
	userID, id, ok := h.userAndConversationID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageInfo, err := h.conversationService.ListMessagesForUser(
		c.Request.Context(),
		userID,
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
	response.Paginated(c, dto.UserConversationMessagesFromService(items), pageInfo.Total, page, pageSize)
}

func (h *ConversationHandler) AddMessage(c *gin.Context) {
	userID, id, ok := h.userAndConversationID(c)
	if !ok {
		return
	}
	var req AddConversationMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.conversationService.AddUserMessage(c.Request.Context(), &service.AddConversationMessageInput{
		ConversationID: id,
		ActorID:        userID,
		Content:        req.Content,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.UserConversationFromService(out))
}

func (h *ConversationHandler) MarkRead(c *gin.Context) {
	userID, id, ok := h.userAndConversationID(c)
	if !ok {
		return
	}
	req, ok := bindOptionalMarkReadRequest(c)
	if !ok {
		return
	}
	out, err := h.conversationService.MarkReadForUser(c.Request.Context(), userID, id, req.ReadUntilMessageID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserConversationFromService(out))
}

func (h *ConversationHandler) Close(c *gin.Context) {
	userID, id, ok := h.userAndConversationID(c)
	if !ok {
		return
	}
	out, err := h.conversationService.CloseForUser(c.Request.Context(), userID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserConversationFromService(out))
}

func (h *ConversationHandler) UnreadCount(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	count, err := h.conversationService.CountUnreadForUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

func (h *ConversationHandler) userAndConversationID(c *gin.Context) (int64, int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid conversation ID")
		return 0, 0, false
	}
	return subject.UserID, id, true
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
