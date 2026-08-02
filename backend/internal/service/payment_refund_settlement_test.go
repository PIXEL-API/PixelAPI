//go:build unit

package service

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// REFUND_PENDING 必须被履约闸门认作退款态。漏掉会让 RetryFulfillment /
// AdminManualFulfillOrder 对一笔退款在途的订单二次充值或二次发货，是真实资损。
func TestRefundPendingCountsAsRefundStatus(t *testing.T) {
	require.True(t, psIsRefundStatus(OrderStatusRefundPending))

	// 回归保护：其余退款态一个都不能掉。
	for _, status := range []string{
		OrderStatusRefundRequested,
		OrderStatusRefunding,
		OrderStatusPartiallyRefunded,
		OrderStatusRefunded,
		OrderStatusRefundFailed,
	} {
		require.Truef(t, psIsRefundStatus(status), "status %s must count as refund status", status)
	}
	require.False(t, psIsRefundStatus(OrderStatusCompleted))
}

// REFUND_PENDING 不得出现在「允许发起退款」的白名单里：网关侧已经有一笔在途退款，
// 再发起一次就是重复退款。它只能经 QueryAndFinalizeRefund 收敛。
func TestRefundPendingIsNotInitiable(t *testing.T) {
	require.NotContains(t, refundInitiableStatuses(), OrderStatusRefundPending)
	require.Contains(t, refundInitiableStatuses(), OrderStatusCompleted)
	require.Contains(t, refundInitiableStatuses(), OrderStatusRefundRequested)
	require.Contains(t, refundInitiableStatuses(), OrderStatusRefundFailed)
}

func TestRefundResponseStatusNormalization(t *testing.T) {
	tests := []struct {
		name string
		resp *payment.RefundResponse
		want string
	}{
		// nil 表示压根没调网关（订单无交易号），沿用改造前「err==nil 即成功」的语义。
		{name: "nil response is success", resp: nil, want: payment.ProviderStatusSuccess},
		{name: "pending", resp: &payment.RefundResponse{Status: payment.ProviderStatusPending}, want: payment.ProviderStatusPending},
		{name: "failed", resp: &payment.RefundResponse{Status: payment.ProviderStatusFailed}, want: payment.ProviderStatusFailed},
		{name: "success", resp: &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, want: payment.ProviderStatusSuccess},
		{name: "case insensitive pending", resp: &payment.RefundResponse{Status: "PENDING"}, want: payment.ProviderStatusPending},
		{name: "padded pending", resp: &payment.RefundResponse{Status: "  pending  "}, want: payment.ProviderStatusPending},
		// 空串与未知值按成功处理：easypay 等 provider 不填状态，改造前 err==nil 就是成功，
		// 归一化不能反过来把既有的成功退款改判。
		{name: "empty status is success", resp: &payment.RefundResponse{Status: ""}, want: payment.ProviderStatusSuccess},
		{name: "unknown status is success", resp: &payment.RefundResponse{Status: "refunded"}, want: payment.ProviderStatusSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, refundResponseStatus(tt.resp))
		})
	}
}

func TestRefundTerminalStatus(t *testing.T) {
	full := &RefundPlan{RefundAmount: 100, Order: &dbent.PaymentOrder{Amount: 100}}
	require.Equal(t, OrderStatusRefunded, refundTerminalStatus(full))

	partial := &RefundPlan{RefundAmount: 40, Order: &dbent.PaymentOrder{Amount: 100}}
	require.Equal(t, OrderStatusPartiallyRefunded, refundTerminalStatus(partial))

	// Order 为 nil 时不应 panic，按全额退款处理。
	require.Equal(t, OrderStatusRefunded, refundTerminalStatus(&RefundPlan{RefundAmount: 10}))
}

// markRefundPending 的三条硬约束里最容易被改坏的一条：绝不写 refund_at。
// 营收报表按 refund_at 落桶且不看 status，且全仓没有清空 refund_at 的路径，
// 一旦在未落地阶段写入就会永久虚减营收。
func TestMarkRefundPendingPersistsSettlementAndLeavesRefundAtUnset(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "pending-settlement")

	// 进入 REFUNDING —— markRefundPending 的 CAS 前提。
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefunding).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	plan := &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount,
		Reason:        "gateway accepted",
		DeductBalance: true,
	}

	result, err := svc.markRefundPending(ctx, plan, &payment.RefundResponse{
		RefundID: "re_test_123",
		Status:   payment.ProviderStatusPending,
	})
	require.NoError(t, err)
	require.True(t, result.Success)
	require.NotEmpty(t, result.Warning)

	stored, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, stored.Status)
	require.Equal(t, "re_test_123", stored.RefundTradeNo)
	require.True(t, stored.RefundDeductOnSettle)
	require.Nil(t, stored.RefundAt, "refund_at must stay unset until the refund actually settles")

	settlement := refundSettlementOf(stored)
	require.Equal(t, "re_test_123", settlement.RefundTradeNo)
	require.True(t, settlement.DeductOnSettle)
}

// CAS 保护：订单不在 REFUNDING 时 markRefundPending 必须冲突返回，不得覆盖状态。
func TestMarkRefundPendingRejectsWrongStatus(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "pending-cas")

	svc := &PaymentService{entClient: client}
	_, err := svc.markRefundPending(ctx, &RefundPlan{
		OrderID:      order.ID,
		Order:        order,
		RefundAmount: order.Amount,
	}, &payment.RefundResponse{RefundID: "re_x", Status: payment.ProviderStatusPending})

	require.Error(t, err)
	require.Equal(t, "CONFLICT", infraerrors.Reason(err))
}

// 没有退款单号就无法回查，必须明确拒绝而不是静默把订单推进到某个终态。
func TestQueryAndFinalizeRefundRequiresRefundTradeNo(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "finalize-no-id")
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundPending).Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.Error(t, err)
	require.Equal(t, "REFUND_ID_MISSING", infraerrors.Reason(err))

	// 订单必须原地不动，不能被推进也不能被卡进 REFUNDING。
	stored, getErr := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusRefundPending, stored.Status)
}

// 非 pending 订单不得进入终态化流程。
func TestQueryAndFinalizeRefundRejectsNonPendingOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "finalize-wrong-status")

	svc := &PaymentService{entClient: client}
	_, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.Error(t, err)
	require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
}

// finalizeRefundFailed 把订单落到 REFUND_FAILED。gateway-first 下此前没有扣过任何款，
// 因此这里不应有任何补偿动作。
func TestFinalizeRefundFailedMarksOrderFailed(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentRefundInvoiceTestOrder(t, ctx, client, "finalize-failed")
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundPending).Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	res, err := svc.finalizeRefundFailed(ctx, order.ID, "gateway reported refund failed")
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, res.OrderStatus)
	require.Equal(t, payment.ProviderStatusFailed, res.RefundStatus)

	stored, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, stored.Status)
	require.NotNil(t, stored.FailedReason)
}
