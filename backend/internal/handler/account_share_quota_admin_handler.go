package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountShareModeHandler) GetGlobalQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return
	}
	policy, err := h.service.GetAccountShareGlobalQuotaForAdmin(
		c.Request.Context(),
		actorUserID,
		actorIsAdmin,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

func (h *AccountShareModeHandler) UpdateGlobalQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return
	}
	var input service.UpdateAccountShareGlobalQuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid account share global quota request")
		return
	}
	executeUserRequiredIdempotentJSON(
		c,
		"account-share-admin-quota-global-update",
		input,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context, _ string) (any, error) {
			return h.service.UpdateAccountShareGlobalQuotaForAdmin(
				ctx,
				actorUserID,
				actorIsAdmin,
				input,
			)
		},
		nil,
	)
}

func (h *AccountShareModeHandler) GetOwnerQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return
	}
	ownerUserID, err := parseInt64Param(c, "owner_id")
	if err != nil {
		response.BadRequest(c, "Invalid owner user ID")
		return
	}
	state, err := h.service.GetAccountShareOwnerQuotaForAdmin(
		c.Request.Context(),
		actorUserID,
		actorIsAdmin,
		ownerUserID,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

func (h *AccountShareModeHandler) UpsertOwnerQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ownerUserID, ok := accountShareQuotaAdminOwner(c)
	if !ok {
		return
	}
	var input service.UpsertAccountShareOwnerQuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid account share owner quota request")
		return
	}
	executeUserRequiredIdempotentJSON(
		c,
		"account-share-admin-quota-owner-upsert",
		struct {
			OwnerUserID int64                                     `json:"owner_user_id"`
			Input       service.UpsertAccountShareOwnerQuotaInput `json:"input"`
		}{OwnerUserID: ownerUserID, Input: input},
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context, _ string) (any, error) {
			return h.service.UpsertAccountShareOwnerQuotaForAdmin(
				ctx,
				actorUserID,
				actorIsAdmin,
				ownerUserID,
				input,
			)
		},
		nil,
	)
}

func (h *AccountShareModeHandler) GrandfatherOwnerQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ownerUserID, ok := accountShareQuotaAdminOwner(c)
	if !ok {
		return
	}
	var input service.GrandfatherAccountShareOwnerQuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid account share grandfather quota request")
		return
	}
	executeUserRequiredIdempotentJSON(
		c,
		"account-share-admin-quota-owner-grandfather",
		struct {
			OwnerUserID int64                                          `json:"owner_user_id"`
			Input       service.GrandfatherAccountShareOwnerQuotaInput `json:"input"`
		}{OwnerUserID: ownerUserID, Input: input},
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context, _ string) (any, error) {
			return h.service.GrandfatherAccountShareOwnerQuotaForAdmin(
				ctx,
				actorUserID,
				actorIsAdmin,
				ownerUserID,
				input,
			)
		},
		nil,
	)
}

func (h *AccountShareModeHandler) RevokeOwnerQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ownerUserID, ok := accountShareQuotaAdminOwner(c)
	if !ok {
		return
	}
	var input service.RevokeAccountShareOwnerQuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid account share quota revoke request")
		return
	}
	executeUserRequiredIdempotentJSON(
		c,
		"account-share-admin-quota-owner-revoke",
		struct {
			OwnerUserID int64                                     `json:"owner_user_id"`
			Input       service.RevokeAccountShareOwnerQuotaInput `json:"input"`
		}{OwnerUserID: ownerUserID, Input: input},
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context, _ string) (any, error) {
			return h.service.RevokeAccountShareOwnerQuotaForAdmin(
				ctx,
				actorUserID,
				actorIsAdmin,
				ownerUserID,
				input,
			)
		},
		nil,
	)
}

func (h *AccountShareModeHandler) ListQuotaAuditForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return
	}
	scopeType := strings.ToLower(strings.TrimSpace(c.DefaultQuery(
		"scope_type",
		service.AccountShareQuotaScopeGlobal,
	)))
	var ownerUserID *int64
	if rawOwnerID := strings.TrimSpace(c.Query("owner_id")); rawOwnerID != "" {
		parsed, err := strconv.ParseInt(rawOwnerID, 10, 64)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid owner user ID")
			return
		}
		ownerUserID = &parsed
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListAccountShareQuotaAuditForAdmin(
		c.Request.Context(),
		actorUserID,
		actorIsAdmin,
		scopeType,
		ownerUserID,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func (h *AccountShareModeHandler) ListGrandfatherCandidatesForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListAccountShareGrandfatherCandidatesForAdmin(
		c.Request.Context(),
		actorUserID,
		actorIsAdmin,
		pagination.PaginationParams{Page: page, PageSize: pageSize},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func (h *AccountShareModeHandler) BatchGrandfatherQuotaForAdmin(c *gin.Context) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return
	}
	var input service.BatchGrandfatherAccountShareQuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid account share grandfather batch request")
		return
	}
	executeUserRequiredIdempotentJSON(
		c,
		"account-share-admin-quota-grandfather-batch",
		input,
		service.DefaultWriteIdempotencyTTL(),
		func(ctx context.Context, _ string) (any, error) {
			return h.service.BatchGrandfatherAccountShareQuotaForAdmin(
				ctx,
				actorUserID,
				actorIsAdmin,
				input,
			)
		},
		nil,
	)
}

func accountShareQuotaAdminActor(c *gin.Context) (int64, bool, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, false, false
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	return subject.UserID, role == service.RoleAdmin, true
}

func accountShareQuotaAdminOwner(c *gin.Context) (int64, bool, int64, bool) {
	actorUserID, actorIsAdmin, ok := accountShareQuotaAdminActor(c)
	if !ok {
		return 0, false, 0, false
	}
	ownerUserID, err := parseInt64Param(c, "owner_id")
	if err != nil {
		response.BadRequest(c, "Invalid owner user ID")
		return 0, false, 0, false
	}
	return actorUserID, actorIsAdmin, ownerUserID, true
}
