package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Refund Flow ---

const (
	// markRefundOkWriteTimeout 落退款终态那条 UPDATE 的独立超时。
	markRefundOkWriteTimeout = 10 * time.Second
	// markRefundOkWriteAttempts 落终态的尝试次数，用于扛住瞬时连接池抖动。
	markRefundOkWriteAttempts = 3
)

var ErrPaymentOrderHasActiveInvoice = infraerrors.Conflict("PAYMENT_ORDER_HAS_ACTIVE_INVOICE", "payment order has an active invoice request")

func (s *PaymentService) ensureOrderRefundableByInvoice(ctx context.Context, orderID int64) error {
	rows, err := s.entClient.QueryContext(ctx, `
SELECT 1
FROM invoice_request_items
WHERE source_type = $1
	AND source_id = $2
	AND active = TRUE
LIMIT 1`, InvoiceSourceTypePaymentOrder, orderID)
	if err != nil {
		return fmt.Errorf("check active invoice request for refund: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		return ErrPaymentOrderHasActiveInvoice
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate active invoice request check: %w", err)
	}
	return nil
}

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	if refundedOrder, err := s.entClient.PaymentOrder.Get(ctx, oid); err == nil {
		s.notifyPaymentOrder(ctx, "refund_requested", refundedOrder)
	} else {
		slog.Warn("payment.system_notice_refund_requested_reload_failed", "order_id", oid, "error", err)
	}
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	if err := s.ensureOrderRefundableByInvoice(ctx, o.ID); err != nil {
		return nil, err
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if !psSliceContains(refundInitiableStatuses(), o.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}
	if err := s.ensureOrderRefundableByInvoice(ctx, o.ID); err != nil {
		return nil, nil, err
	}
	// Check provider instance allows admin refund
	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		// Legacy order without provider_instance_id — block refund
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt <= 0 {
		amt = o.Amount
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt-o.Amount > paymentAmountToleranceForCurrency(orderCurrency) {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	ga := calculateGatewayRefundAmount(o.Amount, o.PayAmount, amt, orderCurrency)
	rr := strings.TrimSpace(reason)
	if rr == "" && o.RefundRequestReason != nil {
		rr = *o.RefundRequestReason
	}
	if rr == "" {
		rr = fmt.Sprintf("refund order:%d", o.ID)
	}
	p := &RefundPlan{OrderID: oid, Order: o, RefundAmount: amt, GatewayAmount: ga, Reason: rr, Force: force, DeductBalance: deduct, DeductionType: payment.DeductionTypeNone}
	if deduct {
		if er := s.prepDeduct(ctx, o, p, force); er != nil {
			return nil, er, nil
		}
	}
	return p, nil, nil
}

func (s *PaymentService) prepDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if o.OrderType == payment.OrderTypeSubscription {
		p.DeductionType = payment.DeductionTypeSubscription
		if o.SubscriptionGroupID != nil && o.SubscriptionDays != nil {
			p.SubDaysToDeduct = *o.SubscriptionDays
			sub, err := s.subscriptionSvc.GetActiveSubscription(ctx, o.UserID, *o.SubscriptionGroupID)
			if err == nil && sub != nil {
				p.SubscriptionID = sub.ID
			} else if !force {
				return &RefundResult{Success: false, Warning: "cannot find active subscription for deduction, use force", RequireForce: true}
			}
		}
		return nil
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		if !force {
			return &RefundResult{Success: false, Warning: "cannot fetch user balance, use force", RequireForce: true}
		}
		return nil
	}
	p.DeductionType = payment.DeductionTypeBalance
	p.BalanceToDeduct = math.Min(p.RefundAmount, u.Balance)
	return nil
}

// ExecuteRefund 执行退款。
//
// 执行顺序是 gateway-first：先调网关，**只有网关确认终态成功后才扣回**用户余额
// 或订阅时长。这与改造前「先扣款再调网关、失败再回滚」的顺序相反，原因是网关会
// 返回 pending —— Stripe / 微信 / 支付宝在「受理成功但尚未结算」时返回
// status=pending 且 error 为 nil（stripe.go:217 / wxpay.go:487 / alipay.go:387）。
//
// pending 的终态确认必然发生在**另一个请求**里（管理员点回查），那时内存中的
// RefundPlan 早已不存在，补偿回滚无从谈起。gateway-first 把「未确认成功就一分钱
// 不扣」变成不变式，于是：
//   - 补偿状态完全不需要持久化（没扣过就不用回滚）
//   - RevokeSubscription 的硬删除不再有不可逆窗口（只在确认成功后才执行）
//   - REFUND_ROLLBACK_FAILED 那个永久粘滞位（受 migration 131 的
//     UNIQUE(order_id, action) 保护、无任何清除路径）不再会被写出
func (s *PaymentService) ExecuteRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed)).SetStatus(OrderStatusRefunding).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if c == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	resp, err := s.gwRefund(ctx, p)
	if err != nil {
		return s.handleGwFail(ctx, p, err)
	}

	switch refundResponseStatus(resp) {
	case payment.ProviderStatusPending:
		return s.markRefundPending(ctx, p, resp)
	case payment.ProviderStatusFailed:
		return s.handleGwFail(ctx, p, fmt.Errorf("gateway reported refund failed"))
	default:
		return s.settleRefundSuccess(ctx, p)
	}
}

// refundResponseStatus 把 provider 返回的退款状态归一到
// success / pending / failed 三态。
//
// resp == nil 表示 gwRefund 压根没调网关（订单没有交易号，直接跳过），
// 按成功处理——与本次改造前的行为一致。状态串为空或无法识别时同样按成功处理，
// 因为改造前的语义就是「error == nil 即成功」，不能因为归一化反而把
// 既有 provider（如 easypay 恒返回 success）的成功退款改判成别的状态。
func refundResponseStatus(resp *payment.RefundResponse) string {
	if resp == nil {
		return payment.ProviderStatusSuccess
	}
	switch strings.ToLower(strings.TrimSpace(resp.Status)) {
	case payment.ProviderStatusPending:
		return payment.ProviderStatusPending
	case payment.ProviderStatusFailed:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusSuccess
	}
}

// settleRefundSuccess 在网关确认终态成功后扣款并落终态。
func (s *PaymentService) settleRefundSuccess(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	warning := s.applyRefundDeductions(ctx, p)
	result, err := s.markRefundOk(ctx, p)
	if err != nil {
		return nil, err
	}
	if warning != "" {
		result.Warning = warning
	}
	return result, nil
}

// applyRefundDeductions 在退款已确认落地后扣回用户余额或订阅时长。
//
// 走到这里时钱已经从商户账户退给用户，是既成事实，因此扣减失败**不回滚、
// 也不改判订单状态**——订单终态必须如实反映网关结果。失败只写一条高噪声审计
// （REFUND_DEDUCTION_FAILED）并把告警文案带回管理端，交人工补账。
// 返回空串表示无需告警。
func (s *PaymentService) applyRefundDeductions(ctx context.Context, p *RefundPlan) string {
	if !p.DeductBalance {
		return ""
	}
	switch p.DeductionType {
	case payment.DeductionTypeBalance:
		if p.BalanceToDeduct <= 0 {
			return s.reportRefundDeductionShortfall(ctx, p, "balance_zero",
				"balance deduction was skipped because the user's balance is already 0")
		}
		if err := s.userRepo.DeductBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			return s.reportRefundDeductionFailure(ctx, p, "balance", err)
		}
		// prepDeduct 用 min(退款额, 当时余额) 夹取。gateway-first 把扣款推迟到了网关
		// 确认之后，pending 期间用户可能已经把余额花掉，扣到的钱会少于退款额。
		// 差额不是错误，但绝不能静默——否则平台单方面亏损且无人知晓。
		tolerance := paymentAmountToleranceForCurrency(PaymentOrderCurrency(p.Order))
		if shortfall := p.RefundAmount - p.BalanceToDeduct; shortfall > tolerance {
			return s.reportRefundDeductionShortfall(ctx, p, "balance_partial",
				fmt.Sprintf("deducted %.2f of %.2f; user balance was insufficient", p.BalanceToDeduct, p.RefundAmount))
		}
	case payment.DeductionTypeSubscription:
		if p.SubDaysToDeduct <= 0 || p.SubscriptionID <= 0 {
			return s.reportRefundDeductionShortfall(ctx, p, "subscription_missing",
				"subscription deduction was skipped: no active subscription to deduct from")
		}
		if _, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct); err != nil {
			if !errors.Is(err, ErrAdjustWouldExpire) {
				return s.reportRefundDeductionFailure(ctx, p, "subscription", err)
			}
			// 扣减会把订阅扣成过期 —— 直接整单撤销。
			// 此处的硬删除是安全的：只有在网关已确认退款成功后才会走到这里。
			slog.Info("subscription deduction would expire, revoking", "orderID", p.OrderID, "subID", p.SubscriptionID, "days", p.SubDaysToDeduct)
			if revokeErr := s.subscriptionSvc.RevokeSubscription(ctx, p.SubscriptionID); revokeErr != nil {
				return s.reportRefundDeductionFailure(ctx, p, "subscription_revoke", revokeErr)
			}
		}
	default:
		// DeductBalance=true 却没定出扣减方式：prepDeduct 在 force 模式下取用户
		// 或订阅失败时会直接返回而不设置 DeductionType（payment_refund.go 的
		// prepDeduct）。终态化路径固定用 force=true 调它，所以这条分支真的可达。
		// 静默跳过等于平台白退一笔钱，必须留痕。
		return s.reportRefundDeductionShortfall(ctx, p, "deduction_type_unresolved",
			"refund was marked deductible but no deduction method could be resolved")
	}
	return ""
}

// reportRefundDeductionShortfall 记录「扣款没失败、但也没足额扣到」的情况。
//
// 与 reportRefundDeductionFailure 的区别：那个是操作报错，这个是操作本身成功
// 但金额不足或压根没执行。两者对账面的后果相同（平台少收），都必须可见。
func (s *PaymentService) reportRefundDeductionShortfall(ctx context.Context, p *RefundPlan, kind, detail string) string {
	slog.Warn("refund settled at gateway but deduction was short",
		"orderID", p.OrderID, "userID", p.Order.UserID, "kind", kind,
		"refundAmount", p.RefundAmount, "balanceToDeduct", p.BalanceToDeduct,
		"subDaysToDeduct", p.SubDaysToDeduct, "detail", detail)
	s.writeAuditLog(ctx, p.OrderID, "REFUND_DEDUCTION_SHORTFALL", "admin", map[string]any{
		"kind":            kind,
		"detail":          detail,
		"refundAmount":    p.RefundAmount,
		"balanceToDeduct": p.BalanceToDeduct,
		"subDaysToDeduct": p.SubDaysToDeduct,
	})
	return "refund settled at gateway but the deduction was short (" + kind + "): " + detail + "; manual reconciliation may be required"
}

func (s *PaymentService) reportRefundDeductionFailure(ctx context.Context, p *RefundPlan, kind string, err error) string {
	slog.Error("[CRITICAL] refund settled at gateway but deduction failed",
		"orderID", p.OrderID, "userID", p.Order.UserID, "kind", kind,
		"balanceToDeduct", p.BalanceToDeduct, "subDaysToDeduct", p.SubDaysToDeduct, "error", err)
	s.writeAuditLog(ctx, p.OrderID, "REFUND_DEDUCTION_FAILED", "admin", map[string]any{
		"kind":            kind,
		"detail":          psErrMsg(err),
		"balanceToDeduct": p.BalanceToDeduct,
		"subDaysToDeduct": p.SubDaysToDeduct,
	})
	return "refund settled at gateway but deduction failed (" + kind + "): " + psErrMsg(err) + "; manual reconciliation required"
}

// gwRefund 向网关发起退款。
//
// 返回值即 provider 的原始响应，调用方据此区分 success / pending / failed。
// 改造前这里写的是 `_, err = prov.Refund(...)`，把响应连同 Status 和 RefundID
// 一起丢弃，于是「受理但未结算」被当成终态成功 —— 这正是 B-4 要修的根因。
//
// 返回 (nil, nil) 表示订单没有交易号、根本没调网关，调用方按成功处理。
func (s *PaymentService) gwRefund(ctx context.Context, p *RefundPlan) (*payment.RefundResponse, error) {
	if p.Order.PaymentTradeNo == "" {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_NO_TRADE_NO", "admin", map[string]any{"detail": "skipped"})
		return nil, nil
	}

	// Use the exact provider instance that created this order, not a random one
	// from the registry. Each instance has its own merchant credentials.
	prov, err := s.getRefundProvider(ctx, p.Order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{
			"detail": err.Error(),
		})
		return nil, err
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		TradeNo: p.Order.PaymentTradeNo,
		OrderID: p.Order.OutTradeNo,
		Amount:  strconv.FormatFloat(p.GatewayAmount, 'f', 2, 64),
		Reason:  p.Reason,
	})
	finishProviderCall()
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// getRefundProvider creates a provider using the order's original instance config.
// Delegates to getOrderProvider which handles instance lookup and fallback.
func (s *PaymentService) getRefundProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", o.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

// handleGwFail 处理网关退款失败。
//
// gateway-first 顺序下，走到这里时**尚未扣过任何余额或订阅**，因此不存在需要
// 补偿回滚的动作，订单直接还原到发起退款前的状态供管理员重试。
// 这也是本次改造删掉 RollbackRefund 与 REFUND_ROLLBACK_FAILED 粘滞位的原因。
func (s *PaymentService) handleGwFail(ctx context.Context, p *RefundPlan, gErr error) (*RefundResult, error) {
	s.restoreStatus(ctx, p)
	s.writeAuditLog(ctx, p.OrderID, "REFUND_GATEWAY_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
	if failedOrder, err := s.entClient.PaymentOrder.Get(ctx, p.OrderID); err == nil {
		s.notifyPaymentOrder(ctx, "refund_failed", failedOrder)
	} else {
		slog.Warn("payment.system_notice_refund_failed_reload_failed", "order_id", p.OrderID, "error", err)
	}
	return &RefundResult{Success: false, Warning: "gateway failed: " + psErrMsg(gErr) + ", nothing was deducted"}, nil
}

// refundTerminalStatus 判定退款终态：部分退款与全额退款分属两个状态。
func refundTerminalStatus(p *RefundPlan) string {
	if p.Order != nil && p.RefundAmount < p.Order.Amount {
		return OrderStatusPartiallyRefunded
	}
	return OrderStatusRefunded
}

// markRefundOk 落退款终态。
//
// 调用它时网关已经确认退款成功、且 applyRefundDeductions 已经扣过款，
// 因此这条 UPDATE **不能被放弃**：一旦失败，订单会停在 REFUNDING —— 那个状态
// 没有任何出口，也没有后台清理任务，而钱已经退了、余额也已经扣了。
// 更糟的是此时不能靠"还原状态让管理员重试"来兜底：扣款没有幂等保护，
// 重试会二次扣款。所以这里脱离请求 ctx 并做有限次重试，尽量让它写成功。
func (s *PaymentService) markRefundOk(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	fs := refundTerminalStatus(p)
	now := time.Now()

	// 脱离请求 ctx：客户端断连/超时不该把一笔已经退成功并已扣款的订单钉死在 REFUNDING。
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markRefundOkWriteTimeout)
	defer cancel()

	var err error
	for attempt := 0; attempt < markRefundOkWriteAttempts; attempt++ {
		_, err = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(writeCtx)
		if err == nil {
			break
		}
		slog.Error("mark refund terminal status failed, retrying",
			"orderID", p.OrderID, "attempt", attempt+1, "error", err)
	}
	if err != nil {
		// 走到这里是最坏情况：钱退了、款扣了，订单卡在 REFUNDING。
		// 必须留下足以人工修复的痕迹。
		slog.Error("[CRITICAL] refund settled and deducted but order stuck in REFUNDING",
			"orderID", p.OrderID, "targetStatus", fs, "refundAmount", p.RefundAmount, "error", err)
		s.writeAuditLog(ctx, p.OrderID, "REFUND_TERMINAL_WRITE_FAILED", "admin", map[string]any{
			"targetStatus": fs,
			"refundAmount": p.RefundAmount,
			"detail":       psErrMsg(err),
		})
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	ctx = writeCtx
	s.writeAuditLog(ctx, p.OrderID, "REFUND_SUCCESS", "admin", map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "force": p.Force})
	if refundedOrder, err := s.entClient.PaymentOrder.Get(ctx, p.OrderID); err == nil {
		s.notifyPaymentOrder(ctx, "refunded", refundedOrder)
	} else {
		slog.Warn("payment.system_notice_refunded_reload_failed", "order_id", p.OrderID, "error", err)
	}
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

// restoreStatus 把订单还原到发起退款前的状态。
//
// 改造前这里硬编码只认 COMPLETED / REFUND_REQUESTED，会把从 REFUND_FAILED
// 重试的订单静默降级成 COMPLETED。现在按原状态还原，仅在原状态不在
// PrepareRefund 允许的白名单内时才回落 COMPLETED。
func (s *PaymentService) restoreStatus(ctx context.Context, p *RefundPlan) {
	rs := OrderStatusCompleted
	if p.Order != nil && psSliceContains(refundInitiableStatuses(), p.Order.Status) {
		rs = p.Order.Status
	}
	_, _ = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(rs).Save(ctx)
}

// refundInitiableStatuses 是允许**发起**退款的订单状态集合。
//
// 刻意不含 REFUND_PENDING：pending 订单已经在网关侧有一笔在途退款，
// 再发起一次会造成重复退款。它只能经 QueryAndFinalizeRefund 收敛到终态。
func refundInitiableStatuses() []string {
	return []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed}
}
