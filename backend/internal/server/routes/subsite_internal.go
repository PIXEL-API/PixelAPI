package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterSubsiteInternalRoutes(r *gin.Engine, h *handler.Handlers) {
	internal := r.Group("/api/internal")
	{
		internal.POST("/subsites/heartbeat", h.SubsiteInternal.Heartbeat)
		internal.GET("/subsites/config", h.SubsiteInternal.Config)
		internal.POST("/requests/authorize", h.SubsiteInternal.Authorize)
		internal.POST("/requests/cancel", h.SubsiteInternal.CancelRequest)
		internal.POST("/usage/batch", h.SubsiteInternal.UsageBatch)
		internal.POST("/leases/renew", h.SubsiteInternal.RenewLease)
		internal.POST("/leases/release", h.SubsiteInternal.ReleaseLease)
	}
}
