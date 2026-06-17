package admin

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountShareModePolicyHandler struct {
	service *service.AccountShareModeService
}

func NewAccountShareModePolicyHandler(svc *service.AccountShareModeService) *AccountShareModePolicyHandler {
	return &AccountShareModePolicyHandler{service: svc}
}

type updateAccountShareModePolicyRequest struct {
	Platform           *string  `json:"platform"`
	PlatformShareRatio *float64 `json:"platform_share_ratio" binding:"omitempty,gte=0,lte=1"`
	OwnerShareRatio    *float64 `json:"owner_share_ratio" binding:"omitempty,gte=0,lte=1"`
	Enabled            *bool    `json:"enabled"`
}

func (h *AccountShareModePolicyHandler) Get(c *gin.Context) {
	platform := strings.TrimSpace(c.DefaultQuery("platform", service.PlatformOpenAI))
	policy, err := h.service.GetPolicy(c.Request.Context(), platform)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

func (h *AccountShareModePolicyHandler) Update(c *gin.Context) {
	var req updateAccountShareModePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	platform := service.PlatformOpenAI
	if req.Platform != nil {
		platform = strings.TrimSpace(*req.Platform)
	}
	policy, err := h.service.UpdatePolicy(c.Request.Context(), service.UpdateAccountShareModePolicyInput{
		Platform:           platform,
		PlatformShareRatio: req.PlatformShareRatio,
		OwnerShareRatio:    req.OwnerShareRatio,
		Enabled:            req.Enabled,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}
