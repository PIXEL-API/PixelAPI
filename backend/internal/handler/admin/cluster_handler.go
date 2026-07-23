package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ClusterHandler struct {
	clusterService *service.ClusterService
}

type clusterNodeOperationRequest struct {
	Reason string `json:"reason"`
}

type clusterCacheRefreshRequest struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

func NewClusterHandler(clusterService *service.ClusterService) *ClusterHandler {
	return &ClusterHandler{clusterService: clusterService}
}

// GetSummary handles GET /api/v1/admin/ops/cluster/summary.
func (h *ClusterHandler) GetSummary(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	result, err := h.clusterService.GetSummary(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListInstances handles GET /api/v1/admin/ops/cluster/instances.
func (h *ClusterHandler) ListInstances(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	result, err := h.clusterService.ListInstances(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetInstance handles GET /api/v1/admin/ops/cluster/instances/:node_id.
func (h *ClusterHandler) GetInstance(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	result, err := h.clusterService.GetInstance(c.Request.Context(), c.Param("node_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListTasks handles GET /api/v1/admin/ops/cluster/tasks.
func (h *ClusterHandler) ListTasks(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	result, err := h.clusterService.ListTasks(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ListOperations handles GET /api/v1/admin/ops/cluster/operations.
func (h *ClusterHandler) ListOperations(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			response.BadRequest(c, "limit must be between 1 and 200")
			return
		}
		limit = value
	}
	result, err := h.clusterService.ListOperations(c.Request.Context(), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// DrainInstance handles POST /api/v1/admin/ops/cluster/instances/:node_id/drain.
func (h *ClusterHandler) DrainInstance(c *gin.Context) {
	actor, ok := h.requireInteractiveAdmin(c)
	if !ok {
		return
	}
	var request clusterNodeOperationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := h.clusterService.Drain(c.Request.Context(), service.ClusterNodeOperationRequest{
		NodeID:         c.Param("node_id"),
		Reason:         request.Reason,
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
		Actor:          actor,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, result)
}

// ResumeInstance handles POST /api/v1/admin/ops/cluster/instances/:node_id/resume.
func (h *ClusterHandler) ResumeInstance(c *gin.Context) {
	actor, ok := h.requireInteractiveAdmin(c)
	if !ok {
		return
	}
	var request clusterNodeOperationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := h.clusterService.Resume(c.Request.Context(), service.ClusterNodeOperationRequest{
		NodeID:         c.Param("node_id"),
		Reason:         request.Reason,
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
		Actor:          actor,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, result)
}

// RefreshCache handles POST /api/v1/admin/ops/cluster/cache-refresh.
func (h *ClusterHandler) RefreshCache(c *gin.Context) {
	actor, ok := h.requireInteractiveAdmin(c)
	if !ok {
		return
	}
	var request clusterCacheRefreshRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := h.clusterService.RefreshCache(c.Request.Context(), service.ClusterCacheRefreshRequest{
		Scope:          request.Scope,
		Reason:         request.Reason,
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
		Actor:          actor,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, result)
}

func (h *ClusterHandler) requireService(c *gin.Context) bool {
	if h == nil || h.clusterService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Cluster service not available")
		return false
	}
	return true
}

func (h *ClusterHandler) requireInteractiveAdmin(c *gin.Context) (service.ClusterOperationActor, bool) {
	if !h.requireService(c) {
		return service.ClusterOperationActor{}, false
	}
	authMethod, exists := c.Get("auth_method")
	if !exists || authMethod != "jwt" {
		response.Forbidden(c, "Interactive administrator JWT required")
		return service.ClusterOperationActor{}, false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Authenticated administrator required")
		return service.ClusterOperationActor{}, false
	}
	return service.ClusterOperationActor{UserID: subject.UserID}, true
}
