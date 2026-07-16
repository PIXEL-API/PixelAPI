package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

func RegisterOIDCProviderRoutes(
	r *gin.Engine,
	v1 *gin.RouterGroup,
	h *handler.OIDCProviderHandler,
	jwtAuth servermiddleware.JWTAuthMiddleware,
	cfg *config.Config,
) {
	if cfg == nil || !cfg.OIDCProvider.Enabled || h == nil {
		return
	}
	r.GET("/.well-known/openid-configuration", h.Discovery)

	oidc := v1.Group("/oidc")
	{
		oidc.GET("/jwks", h.JWKS)
		oidc.GET("/authorize", h.Authorize)
		oidc.POST("/token", h.Token)
		oidc.GET("/userinfo", h.UserInfo)
		oidc.POST("/userinfo", h.UserInfo)
		oidc.POST("/revoke", h.Revoke)

		authenticated := oidc.Group("")
		authenticated.Use(gin.HandlerFunc(jwtAuth))
		authenticated.POST("/authorize/complete", h.CompleteAuthorization)
	}
}
