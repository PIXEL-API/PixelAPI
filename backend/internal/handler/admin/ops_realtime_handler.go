package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetConcurrencyStats returns real-time concurrency usage aggregated by platform/group/account.
// GET /api/v1/admin/ops/concurrency
func (h *OpsHandler) GetConcurrencyStats(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if !h.opsService.IsRealtimeMonitoringEnabled(c.Request.Context()) {
		response.Success(c, gin.H{
			"enabled":   false,
			"platform":  map[string]*service.PlatformConcurrencyInfo{},
			"group":     map[int64]*service.GroupConcurrencyInfo{},
			"account":   map[int64]*service.AccountConcurrencyInfo{},
			"timestamp": time.Now().UTC(),
		})
		return
	}

	platformFilter := strings.TrimSpace(c.Query("platform"))
	var groupID *int64
	if v := strings.TrimSpace(c.Query("group_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		groupID = &id
	}

	platform, group, account, collectedAt, err := h.opsService.GetConcurrencyStats(c.Request.Context(), platformFilter, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	payload := gin.H{
		"enabled":  true,
		"platform": platform,
		"group":    group,
		"account":  account,
	}
	if collectedAt != nil {
		payload["timestamp"] = collectedAt.UTC()
	}
	response.Success(c, payload)
}

// GetAccountAvailability returns account availability statistics.
// GET /api/v1/admin/ops/account-availability
//
// Query params:
// - platform: optional
// - group_id: optional
func (h *OpsHandler) GetAccountAvailability(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if !h.opsService.IsRealtimeMonitoringEnabled(c.Request.Context()) {
		response.Success(c, gin.H{
			"enabled":   false,
			"platform":  map[string]*service.PlatformAvailability{},
			"group":     map[int64]*service.GroupAvailability{},
			"account":   map[int64]*service.AccountAvailability{},
			"timestamp": time.Now().UTC(),
		})
		return
	}

	platform := strings.TrimSpace(c.Query("platform"))
	var groupID *int64
	if v := strings.TrimSpace(c.Query("group_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		groupID = &id
	}

	platformStats, groupStats, accountStats, collectedAt, err := h.opsService.GetAccountAvailabilityStats(c.Request.Context(), platform, groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	payload := gin.H{
		"enabled":  true,
		"platform": platformStats,
		"group":    groupStats,
		"account":  accountStats,
	}
	if collectedAt != nil {
		payload["timestamp"] = collectedAt.UTC()
	}
	response.Success(c, payload)
}
