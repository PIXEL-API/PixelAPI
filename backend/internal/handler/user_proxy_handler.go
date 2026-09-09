package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type createOwnedProxyRequest struct {
	Name        string `json:"name" binding:"required,max=100"`
	Protocol    string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host        string `json:"host" binding:"required,max=255"`
	Port        int    `json:"port" binding:"required,min=1,max=65535"`
	Username    string `json:"username" binding:"max=100"`
	Password    string `json:"password" binding:"max=100"`
	MaxAccounts int    `json:"max_accounts" binding:"min=0"`
}

type updateOwnedProxyRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=100"`
	Protocol    *string `json:"protocol" binding:"omitempty,oneof=http https socks5 socks5h"`
	Host        *string `json:"host" binding:"omitempty,min=1,max=255"`
	Port        *int    `json:"port" binding:"omitempty,min=1,max=65535"`
	Username    *string `json:"username" binding:"omitempty,max=100"`
	Password    *string `json:"password" binding:"omitempty,max=100"`
	Status      *string `json:"status" binding:"omitempty,oneof=active inactive"`
	MaxAccounts *int    `json:"max_accounts" binding:"omitempty,min=0"`
}

func (h *AccountShareModeHandler) ListOwnedProxies(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	proxies, err := h.service.ListOwnedProxies(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.ProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyWithAccountCountFromService(&proxies[i]))
	}
	response.Success(c, out)
}

func (h *AccountShareModeHandler) CreateOwnedProxy(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req createOwnedProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxy, err := h.service.CreateOwnedProxy(c.Request.Context(), subject.UserID, service.CreateOwnedProxyInput{
		Name: req.Name, Protocol: req.Protocol, Host: req.Host, Port: req.Port,
		Username: req.Username, Password: req.Password, MaxAccounts: req.MaxAccounts,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyFromService(proxy))
}

func (h *AccountShareModeHandler) UpdateOwnedProxy(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}
	var req updateOwnedProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	proxy, err := h.service.UpdateOwnedProxy(c.Request.Context(), subject.UserID, id, service.UpdateOwnedProxyInput{
		Name: req.Name, Protocol: req.Protocol, Host: req.Host, Port: req.Port,
		Username: req.Username, Password: req.Password, Status: req.Status, MaxAccounts: req.MaxAccounts,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.ProxyFromService(proxy))
}

func (h *AccountShareModeHandler) DeleteOwnedProxy(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}
	if err := h.service.DeleteOwnedProxy(c.Request.Context(), subject.UserID, id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy deleted successfully"})
}
