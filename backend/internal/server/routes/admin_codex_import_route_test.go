package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterAccountRoutesIncludesUpstreamCodexSessionImportPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &adminhandler.AccountHandler{},
		OAuth:   &adminhandler.OAuthHandler{},
	}}

	registerAccountRoutes(router.Group("/api/v1/admin"), handlers)

	found := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/v1/admin/accounts/import/codex-session" {
			found = true
			break
		}
	}
	require.True(t, found, "the upstream-compatible Codex import route must remain registered")
}
