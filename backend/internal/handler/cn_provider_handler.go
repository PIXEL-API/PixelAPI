package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// QueryCNProviderQuota returns a live Coding Plan window snapshot for an owned account.
func (h *UserAccountHandler) QueryCNProviderQuota(c *gin.Context) {
	account, ok := h.resolveOwnedAccount(c)
	if !ok {
		return
	}
	if h.cnQuotaService == nil {
		response.Error(c, http.StatusServiceUnavailable, "cn provider quota service is not enabled")
		return
	}
	result, err := h.cnQuotaService.QueryUsageForAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// QueryCNProviderBalance returns a live pay-as-you-go balance snapshot for an owned account.
func (h *UserAccountHandler) QueryCNProviderBalance(c *gin.Context) {
	account, ok := h.resolveOwnedAccount(c)
	if !ok {
		return
	}
	if h.cnBalanceService == nil {
		response.Error(c, http.StatusServiceUnavailable, "cn provider balance service is not enabled")
		return
	}
	result, err := h.cnBalanceService.QueryBalanceForAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
