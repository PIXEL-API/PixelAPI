package server

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func clusterGatewayAdmission(runtime *service.ClusterRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if runtime == nil || runtime.AcceptingGateway() || !isGatewayRequestPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "node_draining",
				"message": "This application node is draining and is not accepting new gateway requests",
			},
		})
	}
}

func isGatewayRequestPath(path string) bool {
	if path == "/v1" || strings.HasPrefix(path, "/v1/") ||
		path == "/v1beta" || strings.HasPrefix(path, "/v1beta/") ||
		path == "/backend-api/codex" || strings.HasPrefix(path, "/backend-api/codex/") ||
		path == "/antigravity" || strings.HasPrefix(path, "/antigravity/") {
		return true
	}
	switch path {
	case "/responses",
		"/alpha/search",
		"/models",
		"/chat/completions",
		"/images/generations",
		"/images/edits",
		"/videos/generations",
		"/videos/edits",
		"/videos/extensions":
		return true
	}
	return strings.HasPrefix(path, "/responses/") ||
		strings.HasPrefix(path, "/videos/")
}
