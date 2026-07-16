package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/shoporder"
)

const paymentOrderExportBatchSize = 2000

type paymentOrderExportCursor struct {
	createdAt time.Time
	id        int64
}

type paymentOrderExportIterator struct {
	service         *PaymentService
	userID          int64
	params          OrderListParams
	exportStartedAt time.Time
	cursor          *paymentOrderExportCursor
	items           []PaymentOrderListItem
	nextIndex       int
	exhausted       bool
}

type directShopOrderExportIterator struct {
	service         *PaymentService
	userID          int64
	params          OrderListParams
	exportStartedAt time.Time
	cursor          *paymentOrderExportCursor
	items           []PaymentOrderListItem
	nextIndex       int
	exhausted       bool
}

// WriteAdminOrdersCSV writes all orders matching the admin list filters as a
// bounded-memory CSV stream. Orders created after this export starts are
// excluded so concurrent inserts cannot move the keyset while it is read.
func (s *PaymentService) WriteAdminOrdersCSV(ctx context.Context, userID int64, params OrderListParams, dst io.Writer) error {
	if s == nil || s.entClient == nil {
		return fmt.Errorf("payment service is unavailable")
	}
	if dst == nil {
		return fmt.Errorf("payment order export writer is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	exportStartedAt := time.Now().UTC()
	if err := copyWithBOM(dst); err != nil {
		return fmt.Errorf("write payment order export BOM: %w", err)
	}

	writer := csv.NewWriter(dst)
	if err := writePaymentOrderExportCSVHeader(writer); err != nil {
		return err
	}

	paymentIterator := &paymentOrderExportIterator{
		service:         s,
		userID:          userID,
		params:          params,
		exportStartedAt: exportStartedAt,
	}
	paymentItem, hasPaymentItem, err := paymentIterator.next(ctx)
	if err != nil {
		return err
	}

	var (
		shopIterator *directShopOrderExportIterator
		shopItem     PaymentOrderListItem
		hasShopItem  bool
		exportedRows int
	)
	if shouldIncludeDirectShopOrders(params) {
		shopIterator = &directShopOrderExportIterator{
			service:         s,
			userID:          userID,
			params:          params,
			exportStartedAt: exportStartedAt,
		}
		shopItem, hasShopItem, err = shopIterator.next(ctx)
		if err != nil {
			return err
		}
	}

	for hasPaymentItem || hasShopItem {
		if err := ctx.Err(); err != nil {
			return err
		}

		if hasPaymentItem && (!hasShopItem || paymentOrderListItemBefore(paymentItem, shopItem)) {
			if err := writePaymentOrderExportCSVRow(writer, paymentItem); err != nil {
				return err
			}
			exportedRows++
			paymentItem, hasPaymentItem, err = paymentIterator.next(ctx)
		} else {
			if err := writePaymentOrderExportCSVRow(writer, shopItem); err != nil {
				return err
			}
			exportedRows++
			shopItem, hasShopItem, err = shopIterator.next(ctx)
		}
		if err != nil {
			return err
		}
		if exportedRows%paymentOrderExportBatchSize == 0 {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return fmt.Errorf("flush payment order export CSV: %w", err)
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush payment order export CSV: %w", err)
	}
	return nil
}

func (it *paymentOrderExportIterator) next(ctx context.Context) (PaymentOrderListItem, bool, error) {
	if it.nextIndex < len(it.items) {
		item := it.items[it.nextIndex]
		it.nextIndex++
		return item, true, nil
	}
	if it.exhausted {
		return PaymentOrderListItem{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return PaymentOrderListItem{}, false, err
	}

	query := it.service.adminPaymentOrderQuery(it.userID, it.params).
		Where(paymentorder.CreatedAtLTE(it.exportStartedAt))
	if it.cursor != nil {
		query = query.Where(paymentorder.Or(
			paymentorder.CreatedAtLT(it.cursor.createdAt),
			paymentorder.And(
				paymentorder.CreatedAtEQ(it.cursor.createdAt),
				paymentorder.IDLT(it.cursor.id),
			),
		))
	}
	orders, err := query.
		Order(
			dbent.Desc(paymentorder.FieldCreatedAt),
			dbent.Desc(paymentorder.FieldID),
		).
		Limit(paymentOrderExportBatchSize).
		All(ctx)
	if err != nil {
		return PaymentOrderListItem{}, false, fmt.Errorf("query payment order export batch: %w", err)
	}
	if len(orders) == 0 {
		it.exhausted = true
		return PaymentOrderListItem{}, false, nil
	}

	it.items = make([]PaymentOrderListItem, 0, len(orders))
	for _, order := range orders {
		it.items = append(it.items, mapPaymentOrderListItem(order))
	}
	last := orders[len(orders)-1]
	it.cursor = &paymentOrderExportCursor{createdAt: last.CreatedAt, id: last.ID}
	it.nextIndex = 1
	if len(orders) < paymentOrderExportBatchSize {
		it.exhausted = true
	}
	return it.items[0], true, nil
}

func (it *directShopOrderExportIterator) next(ctx context.Context) (PaymentOrderListItem, bool, error) {
	if it.nextIndex < len(it.items) {
		item := it.items[it.nextIndex]
		it.nextIndex++
		return item, true, nil
	}
	if it.exhausted {
		return PaymentOrderListItem{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return PaymentOrderListItem{}, false, err
	}

	query := it.service.directShopOrderQuery(it.userID, it.params).
		Where(shoporder.CreatedAtLTE(it.exportStartedAt)).
		WithUser()
	if it.cursor != nil {
		query = query.Where(shoporder.Or(
			shoporder.CreatedAtLT(it.cursor.createdAt),
			shoporder.And(
				shoporder.CreatedAtEQ(it.cursor.createdAt),
				shoporder.IDLT(it.cursor.id),
			),
		))
	}
	orders, err := query.
		Order(
			dbent.Desc(shoporder.FieldCreatedAt),
			dbent.Desc(shoporder.FieldID),
		).
		Limit(paymentOrderExportBatchSize).
		All(ctx)
	if err != nil {
		return PaymentOrderListItem{}, false, fmt.Errorf("query direct shop order export batch: %w", err)
	}
	if len(orders) == 0 {
		it.exhausted = true
		return PaymentOrderListItem{}, false, nil
	}

	it.items = make([]PaymentOrderListItem, 0, len(orders))
	for _, order := range orders {
		it.items = append(it.items, mapDirectShopOrderListItem(order))
	}
	last := orders[len(orders)-1]
	it.cursor = &paymentOrderExportCursor{createdAt: last.CreatedAt, id: last.ID}
	it.nextIndex = 1
	if len(orders) < paymentOrderExportBatchSize {
		it.exhausted = true
	}
	return it.items[0], true, nil
}

func writePaymentOrderExportCSVHeader(writer *csv.Writer) error {
	if err := writer.Write([]string{
		"source",
		"id",
		"out_trade_no",
		"user_id",
		"user_email",
		"user_name",
		"amount",
		"pay_amount",
		"fee_rate",
		"payment_type",
		"order_type",
		"plan_id",
		"subscription_group_id",
		"subscription_days",
		"shop_order_id",
		"status",
		"refund_amount",
		"refund_reason",
		"refund_at",
		"refund_requested_at",
		"refund_request_reason",
		"refund_requested_by",
		"expires_at",
		"paid_at",
		"completed_at",
		"failed_at",
		"failed_reason",
		"created_at",
		"updated_at",
	}); err != nil {
		return fmt.Errorf("write payment order export CSV header: %w", err)
	}
	return nil
}

func writePaymentOrderExportCSVRow(writer *csv.Writer, item PaymentOrderListItem) error {
	if err := writer.Write([]string{
		sanitizePaymentOrderExportText(item.Source),
		strconv.FormatInt(item.ID, 10),
		sanitizePaymentOrderExportText(item.OutTradeNo),
		strconv.FormatInt(item.UserID, 10),
		sanitizePaymentOrderExportText(item.UserEmail),
		sanitizePaymentOrderExportText(item.UserName),
		strconv.FormatFloat(item.Amount, 'f', -1, 64),
		strconv.FormatFloat(item.PayAmount, 'f', -1, 64),
		strconv.FormatFloat(item.FeeRate, 'f', -1, 64),
		sanitizePaymentOrderExportText(item.PaymentType),
		sanitizePaymentOrderExportText(item.OrderType),
		formatPaymentOrderExportInt64Pointer(item.PlanID),
		formatPaymentOrderExportInt64Pointer(item.SubscriptionGroupID),
		formatPaymentOrderExportIntPointer(item.SubscriptionDays),
		formatPaymentOrderExportInt64Pointer(item.ShopOrderID),
		sanitizePaymentOrderExportText(item.Status),
		strconv.FormatFloat(item.RefundAmount, 'f', -1, 64),
		formatPaymentOrderExportStringPointer(item.RefundReason),
		formatPaymentOrderExportTimePointer(item.RefundAt),
		formatPaymentOrderExportTimePointer(item.RefundRequestedAt),
		formatPaymentOrderExportStringPointer(item.RefundRequestReason),
		formatPaymentOrderExportStringPointer(item.RefundRequestedBy),
		formatPaymentOrderExportTimePointer(item.ExpiresAt),
		formatPaymentOrderExportTimePointer(item.PaidAt),
		formatPaymentOrderExportTimePointer(item.CompletedAt),
		formatPaymentOrderExportTimePointer(item.FailedAt),
		formatPaymentOrderExportStringPointer(item.FailedReason),
		formatPaymentOrderExportTime(item.CreatedAt),
		formatPaymentOrderExportTime(item.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("write payment order export CSV row: %w", err)
	}
	return nil
}

func sanitizePaymentOrderExportText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func formatPaymentOrderExportInt64Pointer(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func formatPaymentOrderExportIntPointer(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func formatPaymentOrderExportStringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return sanitizePaymentOrderExportText(*value)
}

func formatPaymentOrderExportTimePointer(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatPaymentOrderExportTime(*value)
}

func formatPaymentOrderExportTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
