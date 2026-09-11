package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// QueryCNProviderQuota probes a Coding Plan account and returns its rolling windows.
func (h *AccountHandler) QueryCNProviderQuota(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.cnQuotaService == nil {
		response.Error(c, http.StatusServiceUnavailable, "cn provider quota service is not enabled")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.cnQuotaService.QueryUsageForAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// QueryCNProviderBalance probes a pay-as-you-go account and returns its balances.
func (h *AccountHandler) QueryCNProviderBalance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.cnBalanceService == nil {
		response.Error(c, http.StatusServiceUnavailable, "cn provider balance service is not enabled")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.cnBalanceService.QueryBalanceForAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
