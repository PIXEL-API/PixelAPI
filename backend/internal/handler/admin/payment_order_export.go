package admin

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type adminOrderFilters struct {
	UserID      int64
	Status      string
	OrderType   string
	PaymentType string
	Keyword     string
}

func parseAdminOrderFilters(c *gin.Context) (adminOrderFilters, bool) {
	filters := adminOrderFilters{
		Status:      c.Query("status"),
		OrderType:   c.Query("order_type"),
		PaymentType: c.Query("payment_type"),
		Keyword:     c.Query("keyword"),
	}

	rawUserID := c.Query("user_id")
	if rawUserID == "" {
		return filters, true
	}
	normalizedUserID := strings.TrimSpace(rawUserID)
	userID, err := strconv.ParseInt(normalizedUserID, 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return adminOrderFilters{}, false
	}
	filters.UserID = userID
	return filters, true
}

func (f adminOrderFilters) orderListParams() service.OrderListParams {
	return service.OrderListParams{
		Status:      f.Status,
		OrderType:   f.OrderType,
		PaymentType: f.PaymentType,
		Keyword:     f.Keyword,
	}
}

// ExportOrders writes all orders matching the current admin filters as CSV.
// GET /api/v1/admin/payment/orders/export
func (h *PaymentHandler) ExportOrders(c *gin.Context) {
	filters, ok := parseAdminOrderFilters(c)
	if !ok {
		return
	}

	tempFile, err := os.CreateTemp("", "sub2api-orders-*.csv")
	if err != nil {
		response.ErrorFrom(c, fmt.Errorf("create payment order export temp file: %w", err))
		return
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	if err := h.paymentService.WriteAdminOrdersCSV(c.Request.Context(), filters.UserID, filters.orderListParams(), tempFile); err != nil {
		_ = tempFile.Close()
		response.ErrorFrom(c, err)
		return
	}
	if _, err := tempFile.Seek(0, 0); err != nil {
		_ = tempFile.Close()
		response.ErrorFrom(c, fmt.Errorf("rewind payment order export file: %w", err))
		return
	}
	fileInfo, err := tempFile.Stat()
	if err != nil {
		_ = tempFile.Close()
		response.ErrorFrom(c, fmt.Errorf("stat payment order export file: %w", err))
		return
	}

	filename := fmt.Sprintf("orders-%s.csv", time.Now().UTC().Format("20060102-150405"))
	c.DataFromReader(http.StatusOK, fileInfo.Size(), "text/csv; charset=utf-8", tempFile, map[string]string{
		"Content-Disposition":    fmt.Sprintf("attachment; filename=%q", filename),
		"X-Export-Filename":      filename,
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
	})
}
