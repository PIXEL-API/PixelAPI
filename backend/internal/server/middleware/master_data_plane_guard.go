package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// MasterDataPlaneGuard blocks direct model gateway execution on the master when
// traffic must enter through subsite agents.
func MasterDataPlaneGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || settingService.IsMasterDataPlaneEnabled(c.Request.Context()) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "master_data_plane_disabled",
				"code":    "MASTER_DATA_PLANE_DISABLED",
				"message": "master data plane is disabled; send model requests through a subsite endpoint or enable master data plane failover",
			},
		})
	}
}
