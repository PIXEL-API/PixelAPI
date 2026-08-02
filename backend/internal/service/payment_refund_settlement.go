package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Refund Pending Finalization ---
//
// 网关受理了退款但尚未结算时（Stripe / 微信 / 支付宝会返回 status=pending 且
// error 为 nil），订单落到 REFUND_PENDING，由管理员回查推进到终态。
// 本文件是 REFUND_PENDING 的全部进出口。

// RefundQueryResult 是管理端「回查退款状态」的返回体。
type RefundQueryResult struct {
	OrderID         int64   `json:"order_id"`
	OrderStatus     string  `json:"order_status"`
	RefundStatus    string  `json:"refund_status"`
	BalanceDeducted float64 `json:"balance_deducted,omitempty"`
	SubDaysDeducted int     `json:"sub_days_deducted,omitempty"`
	Warning         string  `json:"warning,omitempty"`
}

// refundSettlement 是一笔 pending 退款终态化时需要的持久化上下文。
type refundSettlement struct {
	// RefundTradeNo 网关侧退款单号，回查的唯一凭据。
	RefundTradeNo string
	// DeductOnSettle 管理员发起退款时是否选择了扣回余额/订阅。
	DeductOnSettle bool
}

// markRefundPending 把订单落到 REFUND_PENDING 并持久化终态化所需的上下文。
//
// 三条硬约束：
//  1. **绝不写 refund_at**。营收报表（revenue_service.go:502/530/545）完全按
//     refund_at 落桶且不看 status，全仓也没有清空 refund_at 的路径。未落地的
//     退款一旦写入 refund_at 就会立刻计入当期营收，且永远撤不回来。
//  2. **绝不扣款**。gateway-first 的核心不变式：未确认成功不动用户资产。
//  3. refund_trade_no 必须落库，否则回查无凭据，订单会永久卡在 pending。
func (s *PaymentService) markRefundPending(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	refundTradeNo := ""
	if resp != nil {
		refundTradeNo = strings.TrimSpace(resp.RefundID)
	}

	// **先留痕，再落库。** 走到这里网关已经受理了退款，而 refundTradeNo 此刻只存在于
	// 内存里。若下面的 UPDATE 失败，函数返回后这个单号就永久丢失，运维再也无法向网关
	// 回查这笔在途退款（对微信尤其致命：它只能按 out_refund_no 查）。
	// writeAuditLog 失败只 slog 不阻断，不会引入新的失败路径。
	s.writeAuditLog(ctx, p.OrderID, "REFUND_PENDING", "admin", map[string]any{
		"refundAmount":   p.RefundAmount,
		"reason":         p.Reason,
		"refundTradeNo":  refundTradeNo,
		"deductOnSettle": p.DeductBalance,
		"force":          p.Force,
	})

	// CAS 落库：只有仍处于 REFUNDING 的订单才会被推进到 REFUND_PENDING。
	// 落库脱离请求 ctx：客户端断连不该让一笔网关已受理的退款卡在 REFUNDING
	// （该状态没有任何出口，也没有后台清理任务）。
	ctx = context.WithoutCancel(ctx)
	affected, err := s.entClient.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefunding)).
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(p.RefundAmount).
		SetRefundReason(p.Reason).
		SetForceRefund(p.Force).
		SetRefundTradeNo(refundTradeNo).
		SetRefundDeductOnSettle(p.DeductBalance).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund pending: %w", err)
	}
	if affected == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	if pendingOrder, err := s.entClient.PaymentOrder.Get(ctx, p.OrderID); err == nil {
		s.notifyPaymentOrder(ctx, "refund_pending", pendingOrder)
	} else {
		slog.Warn("payment.system_notice_refund_pending_reload_failed", "order_id", p.OrderID, "error", err)
	}

	warning := "refund accepted by gateway but not settled yet; finalize it once the provider confirms"
	if refundTradeNo == "" {
		warning = "refund accepted by gateway but no refund id was returned; automatic finalization is unavailable, verify manually at the provider console"
	}
	return &RefundResult{Success: true, Warning: warning}, nil
}

// refundSettlementOf 读取一笔 pending 退款的终态化上下文（迁移 264 的两个列）。
func refundSettlementOf(o *dbent.PaymentOrder) *refundSettlement {
	if o == nil {
		return &refundSettlement{}
	}
	return &refundSettlement{
		RefundTradeNo:  strings.TrimSpace(o.RefundTradeNo),
		DeductOnSettle: o.RefundDeductOnSettle,
	}
}

// QueryAndFinalizeRefund 向网关回查一笔 pending 退款并把订单推进到终态。
//
// 这是 REFUND_PENDING 的**唯一**出口。并发保护靠 REFUND_PENDING → REFUNDING
// 的 CAS：管理员双击或多节点同时回查时只有一个请求能拿到锁，其余直接 409，
// 因此不会重复扣款。
func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundQueryResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status != OrderStatusRefundPending {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only pending refunds can be finalized")
	}

	settlement := refundSettlementOf(o)
	if settlement.RefundTradeNo == "" {
		return nil, infraerrors.BadRequest("REFUND_ID_MISSING",
			"this refund has no gateway refund id recorded; verify manually at the provider console")
	}

	locked, err := s.entClient.PaymentOrder.Update().
		Where(paymentorder.IDEQ(oid), paymentorder.StatusEQ(OrderStatusRefundPending)).
		SetStatus(OrderStatusRefunding).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock pending refund: %w", err)
	}
	if locked == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	// 拿到锁之后的每一条提前返回都必须还原成 REFUND_PENDING，
	// 否则订单会永久卡在 REFUNDING —— 那个状态没有任何出口。
	restore := func() {
		if _, rerr := s.entClient.PaymentOrder.Update().
			Where(paymentorder.IDEQ(oid), paymentorder.StatusEQ(OrderStatusRefunding)).
			SetStatus(OrderStatusRefundPending).Save(ctx); rerr != nil {
			slog.Error("[CRITICAL] failed to restore pending refund status", "orderID", oid, "error", rerr)
		}
	}

	prov, err := s.getRefundProvider(ctx, o)
	if err != nil {
		restore()
		return nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", psErrMsg(err))
	}
	queryProv, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		restore()
		return nil, infraerrors.BadRequest("REFUND_QUERY_UNSUPPORTED",
			"this payment provider does not support refund status query; please verify manually")
	}

	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProv.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:  o.PaymentTradeNo,
		OrderID:  o.OutTradeNo,
		RefundID: settlement.RefundTradeNo,
		// 与 gwRefund 同口径：网关金额而非账面退款额，两者在 fee_rate≠0 时不等。
		Amount: strconv.FormatFloat(
			calculateGatewayRefundAmount(o.Amount, o.PayAmount, o.RefundAmount, PaymentOrderCurrency(o)),
			'f', 2, 64),
	})
	finishProviderCall()
	if err != nil {
		// 「查不到」不等于「退款失败」。留在 pending 等人工重试，绝不擅自改判终态。
		restore()
		return nil, infraerrors.InternalServer("REFUND_QUERY_FAILED", psErrMsg(err))
	}

	switch refundResponseStatus(resp) {
	case payment.ProviderStatusPending:
		restore()
		return &RefundQueryResult{
			OrderID:      oid,
			OrderStatus:  OrderStatusRefundPending,
			RefundStatus: payment.ProviderStatusPending,
		}, nil
	case payment.ProviderStatusFailed:
		res, ferr := s.finalizeRefundFailed(ctx, oid, "gateway reported refund failed")
		if ferr != nil {
			// 落终态失败 —— 必须还原回 pending，否则订单卡死在 REFUNDING（无出口）。
			// 此路径尚未扣过任何款，还原是安全的。
			restore()
			return nil, ferr
		}
		return res, nil
	}

	// 终态成功：此刻才扣款。扣减计划按**当前**订单与用户状态重新推导，
	// 不依赖发起退款时的内存快照（那份快照早已随请求结束消失）。
	p, err := s.buildSettlementPlan(ctx, o, settlement)
	if err != nil {
		restore()
		return nil, err
	}
	warning := s.applyRefundDeductions(ctx, p)
	if _, err := s.markRefundOk(ctx, p); err != nil {
		return nil, err
	}
	return &RefundQueryResult{
		OrderID:         oid,
		OrderStatus:     refundTerminalStatus(p),
		RefundStatus:    payment.ProviderStatusSuccess,
		BalanceDeducted: p.BalanceToDeduct,
		SubDaysDeducted: p.SubDaysToDeduct,
		Warning:         warning,
	}, nil
}

// buildSettlementPlan 用订单当前状态重建终态化所需的扣减计划。
func (s *PaymentService) buildSettlementPlan(ctx context.Context, o *dbent.PaymentOrder, settlement *refundSettlement) (*RefundPlan, error) {
	reason := ""
	if o.RefundReason != nil {
		reason = *o.RefundReason
	}
	p := &RefundPlan{
		OrderID:       o.ID,
		Order:         o,
		RefundAmount:  o.RefundAmount,
		GatewayAmount: calculateGatewayRefundAmount(o.Amount, o.PayAmount, o.RefundAmount, PaymentOrderCurrency(o)),
		Reason:        reason,
		Force:         o.ForceRefund,
		DeductBalance: settlement.DeductOnSettle,
		DeductionType: payment.DeductionTypeNone,
	}
	if !settlement.DeductOnSettle {
		return p, nil
	}
	// force=true：退款已在网关落地，此刻不能再因为「找不到活跃订阅」把流程卡住，
	// 否则订单无法收敛到终态。找不到就记 0，由 applyRefundDeductions 跳过并留痕。
	if r := s.prepDeduct(ctx, o, p, true); r != nil {
		return nil, infraerrors.InternalServer("REFUND_DEDUCT_PREPARE_FAILED", r.Warning)
	}
	return p, nil
}

// finalizeRefundFailed 把一笔回查确认失败的 pending 退款落到 REFUND_FAILED。
// 此前没有扣过任何款，因此无需补偿。
func (s *PaymentService) finalizeRefundFailed(ctx context.Context, oid int64, reason string) (*RefundQueryResult, error) {
	now := time.Now()
	if _, err := s.entClient.PaymentOrder.UpdateOneID(oid).
		SetStatus(OrderStatusRefundFailed).
		SetFailedAt(now).
		SetFailedReason(reason).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("finalize refund failed: %w", err)
	}
	s.writeAuditLog(ctx, oid, "REFUND_FAILED", "admin", map[string]any{"detail": reason})
	if failedOrder, err := s.entClient.PaymentOrder.Get(ctx, oid); err == nil {
		s.notifyPaymentOrder(ctx, "refund_failed", failedOrder)
	} else {
		slog.Warn("payment.system_notice_refund_failed_reload_failed", "order_id", oid, "error", err)
	}
	return &RefundQueryResult{
		OrderID:      oid,
		OrderStatus:  OrderStatusRefundFailed,
		RefundStatus: payment.ProviderStatusFailed,
	}, nil
}
