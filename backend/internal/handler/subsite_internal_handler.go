package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SubsiteInternalHandler struct {
	authService      *service.SubsiteAuthService
	subsiteService   *service.SubsiteService
	authorizeService *service.RequestAuthorizeService
	usageService     *service.UsageIngestService
	leaseService     *service.AccountLeaseService
}

func NewSubsiteInternalHandler(
	authService *service.SubsiteAuthService,
	subsiteService *service.SubsiteService,
	authorizeService *service.RequestAuthorizeService,
	usageService *service.UsageIngestService,
	leaseService *service.AccountLeaseService,
) *SubsiteInternalHandler {
	return &SubsiteInternalHandler{
		authService:      authService,
		subsiteService:   subsiteService,
		authorizeService: authorizeService,
		usageService:     usageService,
		leaseService:     leaseService,
	}
}

func (h *SubsiteInternalHandler) Heartbeat(c *gin.Context) {
	subsiteID, ok := h.verify(c)
	if !ok {
		return
	}
	var input service.SubsiteHeartbeatInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if strings.TrimSpace(input.SubsiteID) == "" {
		input.SubsiteID = subsiteID
	}
	input.RemoteIP = c.ClientIP()
	result, err := h.subsiteService.RecordHeartbeat(c.Request.Context(), input)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *SubsiteInternalHandler) Config(c *gin.Context) {
	subsiteID, ok := h.verify(c)
	if !ok {
		return
	}
	subsite, err := h.subsiteService.Get(c.Request.Context(), subsiteID)
	if response.ErrorFrom(c, err) {
		return
	}
	leases, err := h.leaseService.ListBySubsite(c.Request.Context(), subsiteID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{
		"subsite": subsite,
		"leases":  leases,
	})
}

func (h *SubsiteInternalHandler) Authorize(c *gin.Context) {
	subsiteID, ok := h.verify(c)
	if !ok {
		return
	}
	var input service.AuthorizeSubsiteRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if strings.TrimSpace(input.SubsiteID) == "" {
		input.SubsiteID = subsiteID
	}
	result, err := h.authorizeService.Authorize(c.Request.Context(), input)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *SubsiteInternalHandler) UsageBatch(c *gin.Context) {
	subsiteID, ok := h.verify(c)
	if !ok {
		return
	}
	var input service.UsageIngestBatch
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if strings.TrimSpace(input.SubsiteID) == "" {
		input.SubsiteID = subsiteID
	}
	result, err := h.usageService.Ingest(c.Request.Context(), input)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *SubsiteInternalHandler) CancelRequest(c *gin.Context) {
	_, ok := h.verify(c)
	if !ok {
		return
	}
	var input struct {
		RequestID string `json:"request_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if response.ErrorFrom(c, h.authorizeService.Cancel(c.Request.Context(), input.RequestID)) {
		return
	}
	response.Success(c, gin.H{"status": service.QuotaReservationStatusCanceled})
}

func (h *SubsiteInternalHandler) RenewLease(c *gin.Context) {
	_, ok := h.verify(c)
	if !ok {
		return
	}
	var input struct {
		LeaseID    string     `json:"lease_id" binding:"required"`
		ExpiresAt  *time.Time `json:"expires_at"`
		TTLSeconds int        `json:"ttl_seconds"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var expiresAt time.Time
	if input.ExpiresAt != nil {
		expiresAt = *input.ExpiresAt
	} else if input.TTLSeconds > 0 {
		expiresAt = time.Now().Add(time.Duration(input.TTLSeconds) * time.Second)
	} else {
		response.BadRequest(c, "expires_at or ttl_seconds is required")
		return
	}
	lease, err := h.leaseService.Renew(c.Request.Context(), service.RenewAccountLeaseInput{
		LeaseID:   input.LeaseID,
		ExpiresAt: expiresAt,
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}

func (h *SubsiteInternalHandler) ReleaseLease(c *gin.Context) {
	_, ok := h.verify(c)
	if !ok {
		return
	}
	var input struct {
		LeaseID string `json:"lease_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	lease, err := h.leaseService.Release(c.Request.Context(), input.LeaseID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, lease)
}

func (h *SubsiteInternalHandler) verify(c *gin.Context) (string, bool) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.BadRequest(c, err.Error())
		return "", false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	hash := sha256.Sum256(body)
	req := service.SubsiteSignedRequest{
		SubsiteID:  c.GetHeader(service.SubsiteAuthHeaderID),
		Method:     c.Request.Method,
		Path:       c.Request.URL.EscapedPath(),
		Timestamp:  c.GetHeader(service.SubsiteAuthHeaderTimestamp),
		Nonce:      c.GetHeader(service.SubsiteAuthHeaderNonce),
		BodySHA256: c.GetHeader(service.SubsiteAuthHeaderBodySHA),
		Signature:  c.GetHeader(service.SubsiteAuthHeaderSignature),
		Body:       body,
	}
	if strings.TrimSpace(req.BodySHA256) == "" {
		req.BodySHA256 = hex.EncodeToString(hash[:])
	}
	if response.ErrorFrom(c, h.authService.Verify(c.Request.Context(), req)) {
		return "", false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return strings.TrimSpace(req.SubsiteID), true
}
