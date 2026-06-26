package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type InvoiceHandler struct {
	invoiceService *service.InvoiceService
}

func NewInvoiceHandler(invoiceService *service.InvoiceService) *InvoiceHandler {
	return &InvoiceHandler{invoiceService: invoiceService}
}

type rejectInvoiceRequest struct {
	Reason    string `json:"reason"`
	AdminNote string `json:"admin_note"`
}

func (h *InvoiceHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	var userID int64
	if raw := c.Query("user_id"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			userID = parsed
		}
	}
	items, total, err := h.invoiceService.AdminList(c.Request.Context(), service.InvoiceRequestListParams{
		Page:     page,
		PageSize: pageSize,
		UserID:   userID,
		Status:   c.Query("status"),
		Keyword:  c.Query("keyword"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *InvoiceHandler) Get(c *gin.Context) {
	id, err := parseAdminInvoiceID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid id")
		return
	}
	out, err := h.invoiceService.AdminGet(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *InvoiceHandler) Issue(c *gin.Context) {
	id, err := parseAdminInvoiceID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid id")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req service.InvoiceIssueInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.invoiceService.AdminIssue(c.Request.Context(), id, subject.UserID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *InvoiceHandler) Reject(c *gin.Context) {
	id, err := parseAdminInvoiceID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid id")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req rejectInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.invoiceService.AdminReject(c.Request.Context(), id, subject.UserID, req.Reason, req.AdminNote)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func parseAdminInvoiceID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}
