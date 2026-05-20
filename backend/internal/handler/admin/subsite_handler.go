package admin

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SubsiteHandler struct {
	subsiteService *service.SubsiteService
	leaseService   *service.AccountLeaseService
}

func NewSubsiteHandler(subsiteService *service.SubsiteService, leaseService *service.AccountLeaseService) *SubsiteHandler {
	return &SubsiteHandler{subsiteService: subsiteService, leaseService: leaseService}
}

func (h *SubsiteHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.subsiteService.List(c.Request.Context(), pagination.PaginationParams{
		Page:     page,
		PageSize: pageSize,
	}, service.ListSubsitesFilter{
		Status: strings.TrimSpace(c.Query("status")),
		Search: strings.TrimSpace(c.Query("search")),
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.PaginatedWithResult(c, items, &response.PaginationResult{
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
		Pages:    result.Pages,
	})
}

func (h *SubsiteHandler) Create(c *gin.Context) {
	var input service.CreateSubsiteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.subsiteService.Create(c.Request.Context(), input)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, result)
}

func (h *SubsiteHandler) Update(c *gin.Context) {
	var input service.UpdateSubsiteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.subsiteService.Update(c.Request.Context(), c.Param("id"), input)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *SubsiteHandler) Activate(c *gin.Context) {
	if response.ErrorFrom(c, h.subsiteService.Activate(c.Request.Context(), c.Param("id"))) {
		return
	}
	response.Success(c, gin.H{"status": service.SubsiteStatusActive})
}

func (h *SubsiteHandler) Pause(c *gin.Context) {
	if response.ErrorFrom(c, h.subsiteService.Pause(c.Request.Context(), c.Param("id"))) {
		return
	}
	response.Success(c, gin.H{"status": service.SubsiteStatusMaintenance})
}

func (h *SubsiteHandler) Resume(c *gin.Context) {
	if response.ErrorFrom(c, h.subsiteService.Resume(c.Request.Context(), c.Param("id"))) {
		return
	}
	response.Success(c, gin.H{"status": service.SubsiteStatusActive})
}

func (h *SubsiteHandler) ResetSecret(c *gin.Context) {
	result, err := h.subsiteService.ResetSecret(c.Request.Context(), c.Param("id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *SubsiteHandler) ListLeases(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	leases, result, err := h.leaseService.ListBySubsitePaginated(c.Request.Context(), c.Param("id"), pagination.PaginationParams{
		Page:     page,
		PageSize: pageSize,
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.PaginatedWithResult(c, leases, &response.PaginationResult{
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
		Pages:    result.Pages,
	})
}

func (h *SubsiteHandler) ListLeaseActiveAccountIDs(c *gin.Context) {
	accountIDs, err := h.leaseService.ListActiveAccountIDsBySubsite(c.Request.Context(), c.Param("id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"account_ids": accountIDs})
}

func (h *SubsiteHandler) CreateLease(c *gin.Context) {
	var input struct {
		GroupID        int64      `json:"group_id" binding:"required"`
		AccountID      int64      `json:"account_id" binding:"required"`
		MaxConcurrency int        `json:"max_concurrency"`
		MaxRequests    int        `json:"max_requests"`
		MaxTokens      int64      `json:"max_tokens"`
		ExpiresAt      *time.Time `json:"expires_at"`
		TTLSeconds     int        `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	expiresAt := input.ExpiresAt
	if expiresAt == nil && input.TTLSeconds > 0 {
		t := time.Now().Add(time.Duration(input.TTLSeconds) * time.Second)
		expiresAt = &t
	}
	lease, err := h.leaseService.Create(c.Request.Context(), service.CreateAccountLeaseInput{
		SubsiteID:      c.Param("id"),
		GroupID:        input.GroupID,
		AccountID:      input.AccountID,
		MaxConcurrency: input.MaxConcurrency,
		MaxRequests:    input.MaxRequests,
		MaxTokens:      input.MaxTokens,
		ExpiresAt:      expiresAt,
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, lease)
}

func (h *SubsiteHandler) DrainLease(c *gin.Context) {
	lease, err := h.leaseService.DrainForSubsite(c.Request.Context(), c.Param("id"), c.Param("lease_id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}

func (h *SubsiteHandler) ReleaseLease(c *gin.Context) {
	lease, err := h.leaseService.ReleaseForSubsite(c.Request.Context(), c.Param("id"), c.Param("lease_id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}

func (h *SubsiteHandler) RenewLease(c *gin.Context) {
	var input struct {
		ExpiresAt  *time.Time `json:"expires_at"`
		TTLSeconds int        `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var expiresAt time.Time
	if input.ExpiresAt != nil {
		expiresAt = *input.ExpiresAt
	} else if input.TTLSeconds > 0 {
		expiresAt = time.Now().Add(time.Duration(input.TTLSeconds) * time.Second)
	} else {
		response.BadRequest(c, "expires_at or ttl_seconds is required")
		return
	}
	lease, err := h.leaseService.Renew(c.Request.Context(), service.RenewAccountLeaseInput{
		SubsiteID: c.Param("id"),
		LeaseID:   c.Param("lease_id"),
		ExpiresAt: expiresAt,
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}

func (h *SubsiteHandler) UpdateLease(c *gin.Context) {
	var input struct {
		MaxConcurrency int   `json:"max_concurrency"`
		MaxRequests    int   `json:"max_requests"`
		MaxTokens      int64 `json:"max_tokens"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	lease, err := h.leaseService.UpdateLimitsForSubsite(
		c.Request.Context(),
		c.Param("id"),
		c.Param("lease_id"),
		input.MaxConcurrency,
		input.MaxRequests,
		input.MaxTokens,
	)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}

func (h *SubsiteHandler) DeleteLease(c *gin.Context) {
	lease, err := h.leaseService.DeleteForSubsite(c.Request.Context(), c.Param("id"), c.Param("lease_id"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}
