package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type accountShareRoomLifecycleRequest struct {
	ExpectedVersion int64  `json:"expected_version" binding:"required"`
	Reason          string `json:"reason"`
	Confirmed       bool   `json:"confirmed"`
}

type accountShareRoomDeleteIntentRequest struct {
	ExpectedVersion int64  `json:"expected_version" binding:"required"`
	Reason          string `json:"reason"`
}

type accountShareRoomDeleteRequest struct {
	ExpectedVersion int64  `json:"expected_version" binding:"required"`
	RoomName        string `json:"room_name" binding:"required"`
	Token           string `json:"token" binding:"required"`
	Reason          string `json:"reason"`
	Confirmed       bool   `json:"confirmed" binding:"required"`
}

func (h *AccountShareModeHandler) GetRoomManagementState(c *gin.Context) {
	subject, actorIsAdmin, listingID, ok := accountShareLifecycleActor(c)
	if !ok {
		return
	}
	state, err := h.service.GetRoomManagementState(
		c.Request.Context(),
		subject.UserID,
		actorIsAdmin,
		listingID,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

func (h *AccountShareModeHandler) DrainRoom(c *gin.Context) {
	subject, actorIsAdmin, listingID, ok := accountShareLifecycleActor(c)
	if !ok {
		return
	}
	var request accountShareRoomLifecycleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAccountShareLifecycleIdempotent(
		c,
		"account_share_room_drain",
		map[string]any{"listing_id": listingID, "body": request},
		func(ctx context.Context, _ string) (any, error) {
			return h.service.DrainRoom(ctx, subject.UserID, actorIsAdmin, listingID, service.AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: request.ExpectedVersion,
				Reason:          request.Reason,
				Confirmed:       request.Confirmed,
			})
		},
	)
}

func (h *AccountShareModeHandler) ActivateRoom(c *gin.Context) {
	subject, actorIsAdmin, listingID, ok := accountShareLifecycleActor(c)
	if !ok {
		return
	}
	var request accountShareRoomLifecycleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAccountShareLifecycleIdempotent(
		c,
		"account_share_room_activate",
		map[string]any{"listing_id": listingID, "body": request},
		func(ctx context.Context, _ string) (any, error) {
			return h.service.ActivateRoom(ctx, subject.UserID, actorIsAdmin, listingID, service.AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: request.ExpectedVersion,
				Reason:          request.Reason,
				Confirmed:       request.Confirmed,
			})
		},
	)
}

func (h *AccountShareModeHandler) SuspendRoom(c *gin.Context) {
	subject, actorIsAdmin, listingID, ok := accountShareLifecycleActor(c)
	if !ok {
		return
	}
	var request accountShareRoomLifecycleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAccountShareLifecycleIdempotent(
		c,
		"account_share_room_suspend",
		map[string]any{"listing_id": listingID, "body": request},
		func(ctx context.Context, _ string) (any, error) {
			return h.service.SuspendRoom(ctx, subject.UserID, actorIsAdmin, listingID, service.AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: request.ExpectedVersion,
				Reason:          request.Reason,
				Confirmed:       request.Confirmed,
			})
		},
	)
}

func (h *AccountShareModeHandler) CreateRoomDeleteIntent(c *gin.Context) {
	subject, actorIsAdmin, listingID, ok := accountShareLifecycleActor(c)
	if !ok {
		return
	}
	var request accountShareRoomDeleteIntentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	intent, err := h.service.CreateRoomDeleteIntent(
		c.Request.Context(),
		subject.UserID,
		actorIsAdmin,
		listingID,
		service.AccountShareRoomDeleteIntentInput{
			ExpectedVersion: request.ExpectedVersion,
			Reason:          request.Reason,
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, intent)
}

func (h *AccountShareModeHandler) DeleteRoom(c *gin.Context) {
	subject, actorIsAdmin, listingID, ok := accountShareLifecycleActor(c)
	if !ok {
		return
	}
	var request accountShareRoomDeleteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAccountShareLifecycleIdempotent(
		c,
		"account_share_room_delete",
		map[string]any{"listing_id": listingID, "body": request},
		func(ctx context.Context, idempotencyKey string) (any, error) {
			return h.service.DeleteRoom(ctx, subject.UserID, actorIsAdmin, listingID, service.AccountShareRoomDeleteInput{
				ExpectedVersion: request.ExpectedVersion,
				RoomName:        request.RoomName,
				Token:           request.Token,
				Reason:          request.Reason,
				Confirmed:       request.Confirmed,
				RequestID:       idempotencyKey,
			})
		},
	)
}

func (h *AccountShareModeHandler) GetRoomOperation(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	operationID := strings.TrimSpace(c.Param("operation_id"))
	if operationID == "" {
		response.BadRequest(c, "Invalid operation ID")
		return
	}
	operation, err := h.service.GetRoomOperation(
		c.Request.Context(),
		subject.UserID,
		role == service.RoleAdmin,
		operationID,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, operation)
}

func accountShareLifecycleActor(c *gin.Context) (middleware2.AuthSubject, bool, int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return middleware2.AuthSubject{}, false, 0, false
	}
	listingID, err := parseInt64Param(c, "id")
	if err != nil {
		response.BadRequest(c, "Invalid listing ID")
		return middleware2.AuthSubject{}, false, 0, false
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	return subject, role == service.RoleAdmin, listingID, true
}

func executeAccountShareLifecycleIdempotent(
	c *gin.Context,
	scope string,
	payload any,
	execute func(context.Context, string) (any, error),
) {
	executeUserRequiredIdempotentJSON(
		c,
		scope,
		payload,
		service.DefaultSystemOperationIdempotencyTTL(),
		execute,
		func(c *gin.Context, data any) {
			if accountShareOperationStillPending(data) {
				response.Accepted(c, data)
				return
			}
			response.Success(c, data)
		},
	)
}

func accountShareOperationStillPending(data any) bool {
	switch value := data.(type) {
	case *service.AccountShareRoomOperation:
		return value != nil && value.Status != "succeeded" && value.Status != "failed" && value.Status != "cancelled"
	case service.AccountShareRoomOperation:
		return value.Status != "succeeded" && value.Status != "failed" && value.Status != "cancelled"
	case *service.AccountShareRoomManagementState:
		return value != nil && strings.TrimSpace(value.PendingOperationID) != ""
	case service.AccountShareRoomManagementState:
		return strings.TrimSpace(value.PendingOperationID) != ""
	case map[string]any:
		status, _ := value["status"].(string)
		if status != "" {
			return status != "succeeded" && status != "failed" && status != "cancelled"
		}
		operationID, _ := value["pending_operation_id"].(string)
		return strings.TrimSpace(operationID) != ""
	default:
		return false
	}
}
