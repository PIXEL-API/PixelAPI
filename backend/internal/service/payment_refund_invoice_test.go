package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateRefundRequestRejectsActiveInvoiceRequest(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "user-active")
	insertPaymentRefundInvoiceItem(t, ctx, client, order.ID, true)

	svc := &PaymentService{entClient: client}

	_, err := svc.validateRefundRequest(ctx, order.ID, order.UserID)
	require.Error(t, err)
	require.Equal(t, "PAYMENT_ORDER_HAS_ACTIVE_INVOICE", infraerrors.Reason(err))
}

func TestValidateRefundRequestIgnoresInactiveInvoiceRequest(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "user-inactive")
	insertPaymentRefundInvoiceItem(t, ctx, client, order.ID, false)

	svc := &PaymentService{entClient: client}

	_, err := svc.validateRefundRequest(ctx, order.ID, order.UserID)
	require.Error(t, err)
	require.Equal(t, "USER_REFUND_DISABLED", infraerrors.Reason(err))
}

func TestPrepareRefundRejectsActiveInvoiceRequest(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "admin-active")
	insertPaymentRefundInvoiceItem(t, ctx, client, order.ID, true)

	svc := &PaymentService{entClient: client}

	plan, result, err := svc.PrepareRefund(ctx, order.ID, 0, "", false, false)
	require.Nil(t, plan)
	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, "PAYMENT_ORDER_HAS_ACTIVE_INVOICE", infraerrors.Reason(err))
}

func createPaymentRefundInvoiceTestOrder(t *testing.T, ctx context.Context, client *dbent.Client, suffix string) *dbent.PaymentOrder {
	t.Helper()

	user, err := client.User.Create().
		SetEmail("refund-invoice-" + suffix + "@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-invoice-" + suffix).
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("REFUND-INVOICE-" + suffix).
		SetOutTradeNo("sub2_refund_invoice_" + suffix).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-refund-invoice-" + suffix).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	return order
}

func insertPaymentRefundInvoiceItem(t *testing.T, ctx context.Context, client *dbent.Client, orderID int64, active bool) {
	t.Helper()

	_, err := client.ExecContext(ctx, `
INSERT INTO invoice_request_items (source_type, source_id, active)
VALUES ($1, $2, $3)`, InvoiceSourceTypePaymentOrder, orderID, active)
	require.NoError(t, err)
}
