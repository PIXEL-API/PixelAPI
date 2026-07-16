package handler

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type OIDCProviderHandler struct {
	service *service.OIDCProviderService
}

func NewOIDCProviderHandler(providerService *service.OIDCProviderService) *OIDCProviderHandler {
	return &OIDCProviderHandler{service: providerService}
}

func (h *OIDCProviderHandler) Discovery(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.service.Discovery())
}

func (h *OIDCProviderHandler) JWKS(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.service.JWKS())
}

func (h *OIDCProviderHandler) Authorize(c *gin.Context) {
	frontendURL, protocolErr := h.service.BeginAuthorization(c.Request.Context(), service.OIDCProviderAuthorizeInput{
		ClientID:            c.Query("client_id"),
		RedirectURI:         c.Query("redirect_uri"),
		ResponseType:        c.Query("response_type"),
		Scope:               c.Query("scope"),
		State:               c.Query("state"),
		Nonce:               c.Query("nonce"),
		CodeChallenge:       c.Query("code_challenge"),
		CodeChallengeMethod: c.Query("code_challenge_method"),
	})
	if protocolErr != nil {
		if protocolErr.RedirectURI != "" {
			redirectOIDCProviderError(c, protocolErr)
			return
		}
		writeOIDCProviderError(c, protocolErr)
		return
	}
	setOIDCProviderNoStore(c)
	c.Redirect(http.StatusFound, frontendURL)
}

type completeOIDCProviderAuthorizationRequest struct {
	RequestID string `json:"request_id" binding:"required"`
}

func (h *OIDCProviderHandler) CompleteAuthorization(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request completeOIDCProviderAuthorizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOIDCProviderError(c, &service.OIDCProviderError{
			ErrorCode: "invalid_request", Description: "request_id is required", HTTPStatus: http.StatusBadRequest,
		})
		return
	}
	subject, exists := servermiddleware.GetAuthSubjectFromContext(c)
	if !exists || subject.UserID <= 0 {
		writeOIDCProviderError(c, &service.OIDCProviderError{
			ErrorCode: "access_denied", Description: "authenticated user is required", HTTPStatus: http.StatusUnauthorized,
		})
		return
	}
	authTime := time.Now().UTC()
	if subject.AuthTimeUnix > 0 {
		authTime = time.Unix(subject.AuthTimeUnix, 0).UTC()
	}
	redirectURL, protocolErr := h.service.CompleteAuthorization(c.Request.Context(), request.RequestID, subject.UserID, authTime)
	if protocolErr != nil {
		writeOIDCProviderError(c, protocolErr)
		return
	}
	setOIDCProviderNoStore(c)
	response.Success(c, gin.H{"redirect_url": redirectURL})
}

func (h *OIDCProviderHandler) Token(c *gin.Context) {
	if protocolErr := requireOIDCProviderForm(c); protocolErr != nil {
		writeOIDCProviderError(c, protocolErr)
		return
	}
	clientID, clientSecret, protocolErr := oidcProviderClientCredentials(c)
	if protocolErr != nil {
		writeOIDCProviderClientError(c, protocolErr)
		return
	}
	if protocolErr = h.service.AuthenticateClient(clientID, clientSecret); protocolErr != nil {
		writeOIDCProviderClientError(c, protocolErr)
		return
	}
	if c.PostForm("grant_type") != "authorization_code" {
		writeOIDCProviderError(c, &service.OIDCProviderError{
			ErrorCode: "unsupported_grant_type", Description: "grant_type must be authorization_code", HTTPStatus: http.StatusBadRequest,
		})
		return
	}
	result, protocolErr := h.service.ExchangeCode(c.Request.Context(), service.OIDCProviderTokenInput{
		ClientID: clientID, Code: c.PostForm("code"), RedirectURI: c.PostForm("redirect_uri"),
		CodeVerifier: c.PostForm("code_verifier"),
	})
	if protocolErr != nil {
		writeOIDCProviderError(c, protocolErr)
		return
	}
	setOIDCProviderNoStore(c)
	c.JSON(http.StatusOK, result)
}

func (h *OIDCProviderHandler) UserInfo(c *gin.Context) {
	accessToken, protocolErr := oidcProviderBearerToken(c)
	if protocolErr != nil {
		writeOIDCProviderBearerError(c, protocolErr)
		return
	}
	result, protocolErr := h.service.UserInfo(c.Request.Context(), accessToken)
	if protocolErr != nil {
		if protocolErr.ErrorCode == "invalid_token" {
			writeOIDCProviderBearerError(c, protocolErr)
			return
		}
		writeOIDCProviderError(c, protocolErr)
		return
	}
	setOIDCProviderNoStore(c)
	c.JSON(http.StatusOK, result)
}

func (h *OIDCProviderHandler) Revoke(c *gin.Context) {
	if protocolErr := requireOIDCProviderForm(c); protocolErr != nil {
		writeOIDCProviderError(c, protocolErr)
		return
	}
	clientID, clientSecret, protocolErr := oidcProviderClientCredentials(c)
	if protocolErr != nil {
		writeOIDCProviderClientError(c, protocolErr)
		return
	}
	if protocolErr = h.service.AuthenticateClient(clientID, clientSecret); protocolErr != nil {
		writeOIDCProviderClientError(c, protocolErr)
		return
	}
	if protocolErr = h.service.Revoke(c.Request.Context(), clientID, c.PostForm("token")); protocolErr != nil {
		writeOIDCProviderError(c, protocolErr)
		return
	}
	setOIDCProviderNoStore(c)
	c.Status(http.StatusOK)
}

func requireOIDCProviderForm(c *gin.Context) *service.OIDCProviderError {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return &service.OIDCProviderError{
			ErrorCode: "invalid_request", Description: "Content-Type must be application/x-www-form-urlencoded", HTTPStatus: http.StatusBadRequest,
		}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	if err := c.Request.ParseForm(); err != nil {
		return &service.OIDCProviderError{
			ErrorCode: "invalid_request", Description: "request form is invalid", HTTPStatus: http.StatusBadRequest,
		}
	}
	return nil
}

func oidcProviderClientCredentials(c *gin.Context) (string, string, *service.OIDCProviderError) {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	postClientID := c.PostForm("client_id")
	postClientSecret := c.PostForm("client_secret")
	if authorization != "" {
		clientID, clientSecret, ok := c.Request.BasicAuth()
		if !ok || postClientSecret != "" || (postClientID != "" && postClientID != clientID) {
			return "", "", invalidOIDCProviderClientError()
		}
		return clientID, clientSecret, nil
	}
	if postClientID == "" || postClientSecret == "" {
		return "", "", invalidOIDCProviderClientError()
	}
	return postClientID, postClientSecret, nil
}

func invalidOIDCProviderClientError() *service.OIDCProviderError {
	return &service.OIDCProviderError{
		ErrorCode: "invalid_client", Description: "client authentication failed", HTTPStatus: http.StatusUnauthorized,
	}
}

func oidcProviderBearerToken(c *gin.Context) (string, *service.OIDCProviderError) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", &service.OIDCProviderError{
			ErrorCode: "invalid_token", Description: "a Bearer access token is required", HTTPStatus: http.StatusUnauthorized,
		}
	}
	return strings.TrimSpace(parts[1]), nil
}

func redirectOIDCProviderError(c *gin.Context, protocolErr *service.OIDCProviderError) {
	redirectURL, err := url.Parse(protocolErr.RedirectURI)
	if err != nil {
		writeOIDCProviderError(c, &service.OIDCProviderError{
			ErrorCode: "server_error", Description: "registered redirect URI is invalid", HTTPStatus: http.StatusInternalServerError,
		})
		return
	}
	query := redirectURL.Query()
	query.Set("error", protocolErr.ErrorCode)
	query.Set("error_description", protocolErr.Description)
	if protocolErr.State != "" {
		query.Set("state", protocolErr.State)
	}
	redirectURL.RawQuery = query.Encode()
	setOIDCProviderNoStore(c)
	c.Redirect(http.StatusFound, redirectURL.String())
}

func writeOIDCProviderError(c *gin.Context, protocolErr *service.OIDCProviderError) {
	status := protocolErr.HTTPStatus
	if status == 0 {
		status = http.StatusBadRequest
	}
	setOIDCProviderNoStore(c)
	c.JSON(status, gin.H{"error": protocolErr.ErrorCode, "error_description": protocolErr.Description})
}

func writeOIDCProviderClientError(c *gin.Context, protocolErr *service.OIDCProviderError) {
	c.Header("WWW-Authenticate", `Basic realm="oidc-token"`)
	writeOIDCProviderError(c, protocolErr)
}

func writeOIDCProviderBearerError(c *gin.Context, protocolErr *service.OIDCProviderError) {
	description := strings.ReplaceAll(protocolErr.Description, `"`, `'`)
	c.Header("WWW-Authenticate", `Bearer error="`+protocolErr.ErrorCode+`", error_description="`+description+`"`)
	writeOIDCProviderError(c, protocolErr)
}

func setOIDCProviderNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}
