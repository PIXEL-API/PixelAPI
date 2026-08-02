package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// noopMiddleware 是一个什么都不做的中间件占位。
// 用于「限流器可能为 nil」的场景下按路由挂载可选中间件，
// 避免为此把每条路由都写成 if/else 两份注册。
func noopMiddleware(c *gin.Context) { c.Next() }

// RegisterBrandAssetRoutes 注册品牌图片端点。
//
// 挂在引擎根上而不是 /api/v1 下是刻意的：生产边缘对 /api/ 前缀统一
// X-Cache-Status: BYPASS，非 /api 的路径才会被缓存。该路径必须同时出现在
// web.shouldBypassEmbeddedFrontend 的白名单里，否则会被 SPA 兜底吞成 index.html。
func RegisterBrandAssetRoutes(r *gin.Engine, h *handler.Handlers) {
	r.GET(service.BrandAssetPath, h.Setting.ServeSiteLogo)
}

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine, clusterRuntime *service.ClusterRuntime) {
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "live"})
	})
	r.GET("/health/ready", func(c *gin.Context) {
		readiness := clusterRuntime.Readiness()
		status := http.StatusOK
		if !readiness.Ready {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, readiness)
	})

	// Claude Code 遥测日志（忽略，直接返回200）
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode)
	// This is used by the frontend to detect when the service has restarted after setup
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}
