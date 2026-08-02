package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetPanelRateLimitSettings 读取面板 API 限流配置。
// GET /api/v1/admin/settings/panel-rate-limit
func (h *SettingHandler) GetPanelRateLimitSettings(c *gin.Context) {
	if h.settingService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Setting service not available")
		return
	}
	settings, err := h.settingService.GetPanelRateLimitSettings(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to get panel rate limit settings")
		return
	}
	response.Success(c, settings)
}

// UpdatePanelRateLimitSettings 保存面板 API 限流配置。
// PUT /api/v1/admin/settings/panel-rate-limit
//
// 保存后立即刷新当前节点的进程内缓存；多节点部署最迟 60s 内全部生效。
func (h *SettingHandler) UpdatePanelRateLimitSettings(c *gin.Context) {
	if h.settingService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Setting service not available")
		return
	}

	var req service.PanelRateLimitSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.settingService.SetPanelRateLimitSettings(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	settings, err := h.settingService.GetPanelRateLimitSettings(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to reload panel rate limit settings")
		return
	}
	response.Success(c, settings)
}
