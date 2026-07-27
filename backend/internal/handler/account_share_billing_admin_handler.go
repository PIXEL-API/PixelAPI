package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountShareModeHandler) ListBillingIntentsNeedingAttention(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListBillingIntentsNeedingAttention(
		c.Request.Context(),
		subject.UserID,
		role == service.RoleAdmin,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func (h *AccountShareModeHandler) GetBillingIntentForAdmin(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	intentID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid billing intent ID")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	intent, err := h.service.GetBillingIntentForAdmin(
		c.Request.Context(),
		subject.UserID,
		role == service.RoleAdmin,
		intentID,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, intent)
}

func (h *AccountShareModeHandler) WaiveBillingIntentForAdmin(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	intentID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid billing intent ID")
		return
	}
	var input service.WaiveAccountShareBillingIntentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid billing intent waiver request")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	result, err := h.service.WaiveBillingIntentForAdmin(
		c.Request.Context(),
		subject.UserID,
		role == service.RoleAdmin,
		intentID,
		input,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
