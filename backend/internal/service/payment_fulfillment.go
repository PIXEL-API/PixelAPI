package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ErrOrderNotFound is returned by HandlePaymentNotification when the webhook
// references an out_trade_no that does not exist in our DB. Callers (webhook
// handlers) should treat this as a terminal, non-retryable condition and still
// respond with a 2xx success to the provider — otherwise the provider will keep
// retrying forever (e.g. when a foreign environment's webhook endpoint is
// misconfigured to point at us, or when our orders table has been wiped).
var ErrOrderNotFound = errors.New("payment order not found")

const paymentFulfillmentLeaseDuration = 5 * time.Minute

type paymentFulfillmentLease struct {
	version time.Time
}

// --- Payment Notification & Fulfillment ---

func (s *PaymentService) HandlePaymentNotification(ctx context.Context, n *payment.PaymentNotification, pk string) error {
	if n.Status != payment.NotificationStatusSuccess {
		return nil
	}
	// Look up order by out_trade_no (the external order ID we sent to the provider)
	order, err := s.entClient.PaymentOrder.Query().Where(paymentorder.OutTradeNo(n.OrderID)).Only(ctx)
	if err != nil {
		// Fallback only for true legacy "sub2_N" DB-ID payloads when the
		// current out_trade_no lookup genuinely did not find an order.
		if oid, ok := parseLegacyPaymentOrderID(n.OrderID, err); ok {
			return s.confirmPayment(ctx, oid, n.TradeNo, n.Amount, pk, n.Metadata)
		}
		if dbent.IsNotFound(err) {
			return fmt.Errorf("%w: out_trade_no=%s", ErrOrderNotFound, n.OrderID)
		}
		return fmt.Errorf("lookup order failed for out_trade_no %s: %w", n.OrderID, err)
	}
	return s.confirmPayment(ctx, order.ID, n.TradeNo, n.Amount, pk, n.Metadata)
}

func parseLegacyPaymentOrderID(orderID string, lookupErr error) (int64, bool) {
	if !dbent.IsNotFound(lookupErr) {
		return 0, false
	}
	orderID = strings.TrimSpace(orderID)
	if !strings.HasPrefix(orderID, orderIDPrefix) {
		return 0, false
	}
	trimmed := strings.TrimPrefix(orderID, orderIDPrefix)
	if trimmed == "" || trimmed == orderID {
		return 0, false
	}
	oid, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || oid <= 0 {
		return 0, false
	}
	return oid, true
}

func (s *PaymentService) confirmPayment(ctx context.Context, oid int64, tradeNo string, paid float64, pk string, metadata map[string]string) error {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		slog.Error("order not found", "orderID", oid)
		return nil
	}
	instanceProviderKey := ""
	if inst, instErr := s.getOrderProviderInstance(ctx, o); instErr == nil && inst != nil {
		instanceProviderKey = inst.ProviderKey
	}
	expectedProviderKey := expectedNotificationProviderKeyForOrder(s.registry, o, instanceProviderKey)
	if expectedProviderKey != "" && strings.TrimSpace(pk) != "" && !strings.EqualFold(expectedProviderKey, strings.TrimSpace(pk)) {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_PROVIDER_MISMATCH", pk, map[string]any{
			"expectedProvider": expectedProviderKey,
			"actualProvider":   pk,
			"tradeNo":          tradeNo,
		})
		return fmt.Errorf("provider mismatch: expected %s, got %s", expectedProviderKey, pk)
	}
	if err := validateProviderNotificationMetadata(o, pk, metadata); err != nil {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_PROVIDER_METADATA_MISMATCH", pk, map[string]any{
			"detail":  err.Error(),
			"tradeNo": tradeNo,
		})
		return err
	}
	if !isValidProviderAmount(paid) {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_INVALID_AMOUNT", pk, map[string]any{
			"expected": o.PayAmount,
			"paid":     paid,
			"tradeNo":  tradeNo,
		})
		return fmt.Errorf("invalid paid amount from provider: %v", paid)
	}
	if math.Abs(paid-o.PayAmount) > paymentAmountToleranceForCurrency(PaymentOrderCurrency(o)) {
		s.writeAuditLog(ctx, o.ID, "PAYMENT_AMOUNT_MISMATCH", pk, map[string]any{"expected": o.PayAmount, "paid": paid, "tradeNo": tradeNo})
		return fmt.Errorf("amount mismatch: expected %s, got %s", strconv.FormatFloat(o.PayAmount, 'f', -1, 64), strconv.FormatFloat(paid, 'f', -1, 64))
	}
	return s.toPaid(ctx, o, tradeNo, paid, pk)
}

func paymentAmountToleranceForCurrency(currency string) float64 {
	minorUnit := payment.CurrencyMinorUnit(currency)
	if minorUnit <= 2 {
		return amountToleranceCNY
	}
	return math.Pow10(-minorUnit) / 2
}

func isValidProviderAmount(amount float64) bool {
	return amount > 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0)
}

func validateProviderNotificationMetadata(order *dbent.PaymentOrder, providerKey string, metadata map[string]string) error {
	return validateProviderSnapshotMetadata(order, providerKey, metadata)
}

func expectedNotificationProviderKey(registry *payment.Registry, orderPaymentType string, orderProviderKey string, instanceProviderKey string) string {
	if key := strings.TrimSpace(instanceProviderKey); key != "" {
		return key
	}
	if key := strings.TrimSpace(orderProviderKey); key != "" {
		return key
	}
	if registry != nil {
		if key := strings.TrimSpace(registry.GetProviderKey(payment.PaymentType(orderPaymentType))); key != "" {
			return key
		}
	}
	return strings.TrimSpace(orderPaymentType)
}

func (s *PaymentService) toPaid(ctx context.Context, o *dbent.PaymentOrder, tradeNo string, paid float64, pk string) error {
	previousStatus := o.Status
	now := time.Now()
	grace := now.Add(-paymentGraceMinutes * time.Minute)
	c, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(o.ID),
		paymentorder.Or(
			paymentorder.StatusEQ(OrderStatusPending),
			paymentorder.StatusEQ(OrderStatusCancelled),
			paymentorder.And(
				paymentorder.StatusEQ(OrderStatusFailed),
				paymentorder.ExpiresAtGTE(grace),
			),
			paymentorder.And(
				paymentorder.StatusEQ(OrderStatusExpired),
				paymentorder.UpdatedAtGTE(grace),
			),
		),
	).SetStatus(OrderStatusPaid).SetPayAmount(paid).SetPaymentTradeNo(tradeNo).SetPaidAt(now).ClearFailedAt().ClearFailedReason().Save(ctx)
	if err != nil {
		return fmt.Errorf("update to PAID: %w", err)
	}
	if c == 0 {
		return s.alreadyProcessed(ctx, o)
	}
	if previousStatus == OrderStatusCancelled || previousStatus == OrderStatusExpired {
		slog.Info("order recovered from webhook payment success",
			"orderID", o.ID,
			"previousStatus", previousStatus,
			"tradeNo", tradeNo,
			"provider", pk,
		)
		s.writeAuditLog(ctx, o.ID, "ORDER_RECOVERED", pk, map[string]any{
			"previous_status": previousStatus,
			"tradeNo":         tradeNo,
			"paidAmount":      paid,
			"reason":          "webhook payment success received after order " + previousStatus,
		})
	}
	s.writeAuditLog(ctx, o.ID, "ORDER_PAID", pk, map[string]any{"tradeNo": tradeNo, "paidAmount": paid})
	if paidOrder, err := s.entClient.PaymentOrder.Get(ctx, o.ID); err == nil {
		s.notifyPaymentOrder(ctx, "paid", paidOrder)
	} else {
		slog.Warn("payment.system_notice_paid_reload_failed", "order_id", o.ID, "error", err)
	}
	return s.executeFulfillment(ctx, o.ID)
}

func (s *PaymentService) alreadyProcessed(ctx context.Context, o *dbent.PaymentOrder) error {
	if o == nil {
		return errors.New("already processed check requires payment order")
	}
	cur, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	if err != nil {
		return fmt.Errorf("reload already processed order %d: %w", o.ID, err)
	}
	switch cur.Status {
	case OrderStatusCompleted, OrderStatusRefunded:
		return nil
	case OrderStatusFailed:
		if cur.PaidAt == nil {
			s.writeAuditLog(ctx, o.ID, "PAYMENT_AFTER_FAILED_UNPAID", "system", map[string]any{
				"status":    cur.Status,
				"updatedAt": cur.UpdatedAt,
				"reason":    "payment arrived after failed unpaid order could no longer be recovered",
			})
			return nil
		}
		return s.executeFulfillment(ctx, o.ID)
	case OrderStatusPaid, OrderStatusRecharging:
		return s.executeFulfillment(ctx, o.ID)
	case OrderStatusExpired:
		slog.Warn("webhook payment success for expired order beyond grace period",
			"orderID", o.ID,
			"status", cur.Status,
			"updatedAt", cur.UpdatedAt,
		)
		s.writeAuditLog(ctx, o.ID, "PAYMENT_AFTER_EXPIRY", "system", map[string]any{
			"status":    cur.Status,
			"updatedAt": cur.UpdatedAt,
			"reason":    "payment arrived after expiry grace period",
		})
		return nil
	default:
		return infraerrors.Conflict("ORDER_STATUS_CONFLICT", "order cannot be treated as processed in status "+cur.Status)
	}
}

func (s *PaymentService) executeFulfillment(ctx context.Context, oid int64) error {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if o.OrderType == payment.OrderTypeSubscription {
		return s.ExecuteSubscriptionFulfillment(ctx, oid)
	}
	if o.OrderType == payment.OrderTypeShop {
		if s.shopFulfillment == nil {
			return infraerrors.ServiceUnavailable("SHOP_FULFILLMENT_NOT_CONFIGURED", "shop fulfillment service is not configured")
		}
		if err := s.shopFulfillment.ConfirmPaidAndDeliver(ctx, oid); err != nil {
			return err
		}
		if completedOrder, err := s.entClient.PaymentOrder.Get(ctx, oid); err == nil {
			if completedOrder.Status == OrderStatusCompleted {
				s.notifyPaymentOrder(ctx, "completed", completedOrder)
			}
		} else {
			slog.Warn("payment.system_notice_shop_completed_reload_failed", "order_id", oid, "error", err)
		}
		return nil
	}
	return s.ExecuteBalanceFulfillment(ctx, oid)
}

func (s *PaymentService) ExecuteBalanceFulfillment(ctx context.Context, oid int64) error {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status == OrderStatusCompleted {
		return nil
	}
	if psIsRefundStatus(o.Status) {
		return infraerrors.BadRequest("INVALID_STATUS", "refund-related order cannot fulfill")
	}
	if o.Status != OrderStatusPaid && o.Status != OrderStatusFailed && o.Status != OrderStatusRecharging {
		return infraerrors.BadRequest("INVALID_STATUS", "order cannot fulfill in status "+o.Status)
	}
	lease, err := s.acquirePaymentFulfillmentLease(ctx, o)
	if err != nil {
		return err
	}
	if lease == nil {
		return nil
	}
	if err := s.doBalance(ctx, o, lease); err != nil {
		return s.reconcileBalanceFulfillmentFailure(ctx, o, lease, err)
	}
	return nil
}

func (s *PaymentService) acquirePaymentFulfillmentLease(ctx context.Context, o *dbent.PaymentOrder) (*paymentFulfillmentLease, error) {
	if o == nil {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "nil payment order")
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	staleBefore := now.Add(-paymentFulfillmentLeaseDuration)
	updated, err := s.entClient.PaymentOrder.Update().
		Where(
			paymentorder.IDEQ(o.ID),
			paymentorder.Or(
				paymentorder.StatusIn(OrderStatusPaid, OrderStatusFailed),
				paymentorder.And(
					paymentorder.StatusEQ(OrderStatusRecharging),
					paymentorder.UpdatedAtLTE(staleBefore),
				),
			),
		).
		SetStatus(OrderStatusRecharging).
		SetUpdatedAt(now).
		ClearFailedAt().
		ClearFailedReason().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire fulfillment lease: %w", err)
	}
	if updated == 0 {
		current, getErr := s.entClient.PaymentOrder.Get(ctx, o.ID)
		if getErr != nil {
			return nil, fmt.Errorf("reload fulfillment lease: %w", getErr)
		}
		if current.Status == OrderStatusCompleted {
			return nil, nil
		}
		if current.Status == OrderStatusRecharging {
			return nil, infraerrors.Conflict("CONFLICT", "order is being processed")
		}
		return nil, infraerrors.Conflict("CONFLICT", "order status changed while acquiring fulfillment lease")
	}

	claimed, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	if err != nil {
		return nil, fmt.Errorf("reload acquired fulfillment lease: %w", err)
	}
	if claimed.Status != OrderStatusRecharging {
		return nil, infraerrors.Conflict("CONFLICT", "fulfillment lease was lost")
	}
	return &paymentFulfillmentLease{version: claimed.UpdatedAt}, nil
}

// redeemAction represents the idempotency decision for balance fulfillment.
type redeemAction int

const (
	// redeemActionCreate: code does not exist — create it, then redeem.
	redeemActionCreate redeemAction = iota
	// redeemActionRedeem: code exists but is unused — skip creation, redeem only.
	redeemActionRedeem
	// redeemActionSkipCompleted: code exists and is already used — skip to mark completed.
	redeemActionSkipCompleted
	// redeemActionAbort: lookup did not clearly establish whether the code exists.
	redeemActionAbort
)

// resolveRedeemAction decides the idempotency action based on an existing redeem code lookup.
// existing is the result of GetByCode; lookupErr is the error from that call.
func resolveRedeemAction(existing *RedeemCode, lookupErr error) redeemAction {
	if errors.Is(lookupErr, ErrRedeemCodeNotFound) {
		return redeemActionCreate
	}
	if lookupErr != nil || existing == nil {
		return redeemActionAbort
	}
	if existing.IsUsed() {
		return redeemActionSkipCompleted
	}
	return redeemActionRedeem
}

type balanceFulfillmentCommitment int

const (
	balanceFulfillmentDefinitelyNotCommitted balanceFulfillmentCommitment = iota
	balanceFulfillmentCommitted
	balanceFulfillmentVerificationUnavailable
)

func classifyBalanceFulfillmentCommitment(o *dbent.PaymentOrder, code *RedeemCode, lookupErr error) (balanceFulfillmentCommitment, error) {
	if o == nil {
		return balanceFulfillmentVerificationUnavailable, errors.New("payment order is required to verify balance fulfillment")
	}
	if lookupErr != nil {
		if errors.Is(lookupErr, ErrRedeemCodeNotFound) {
			return balanceFulfillmentDefinitelyNotCommitted, nil
		}
		return balanceFulfillmentVerificationUnavailable, fmt.Errorf("lookup payment redeem code: %w", lookupErr)
	}
	if code == nil {
		return balanceFulfillmentVerificationUnavailable, errors.New("payment redeem code lookup returned no code and no error")
	}
	if code.Status != StatusUsed {
		return balanceFulfillmentDefinitelyNotCommitted, nil
	}
	if code.Code != o.RechargeCode || code.Type != RedeemTypeBalance || code.Value != o.Amount || code.UsedBy == nil || *code.UsedBy != o.UserID {
		return balanceFulfillmentDefinitelyNotCommitted, nil
	}
	return balanceFulfillmentCommitted, nil
}

func validateExistingBalanceRedeemCode(o *dbent.PaymentOrder, code *RedeemCode) error {
	if o == nil || code == nil {
		return errors.New("payment order and redeem code are required")
	}
	if code.Code != o.RechargeCode {
		return fmt.Errorf("redeem code does not match payment order %d", o.ID)
	}
	if code.Type != RedeemTypeBalance {
		return fmt.Errorf("redeem code type %q does not match balance payment order %d", code.Type, o.ID)
	}
	if code.Value != o.Amount {
		return fmt.Errorf("redeem code value does not match payment order %d", o.ID)
	}
	return nil
}

func (s *PaymentService) doBalance(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease) error {
	// Idempotency: check if redeem code already exists (from a previous partial run)
	existing, lookupErr := s.redeemService.GetByCode(ctx, o.RechargeCode)
	action := resolveRedeemAction(existing, lookupErr)

	switch action {
	case redeemActionAbort:
		if lookupErr != nil {
			return fmt.Errorf("lookup payment redeem code: %w", lookupErr)
		}
		return errors.New("lookup payment redeem code returned no code and no error")
	case redeemActionSkipCompleted:
		commitment, verifyErr := classifyBalanceFulfillmentCommitment(o, existing, lookupErr)
		if verifyErr != nil {
			return fmt.Errorf("verify existing payment redeem code: %w", verifyErr)
		}
		if commitment != balanceFulfillmentCommitted {
			return fmt.Errorf("used redeem code does not belong to payment order %d", o.ID)
		}
		return s.markCompleted(ctx, o, lease, "RECHARGE_SUCCESS")
	case redeemActionCreate:
		rc := &RedeemCode{Code: o.RechargeCode, Type: RedeemTypeBalance, Value: o.Amount, Status: StatusUnused}
		if err := s.redeemService.CreateCode(ctx, rc); err != nil {
			return fmt.Errorf("create redeem code: %w", err)
		}
	case redeemActionRedeem:
		if err := validateExistingBalanceRedeemCode(o, existing); err != nil {
			return err
		}
	}
	redeemed, err := s.redeemService.redeemWithTransactionGuard(ctx, o.UserID, o.RechargeCode, func(txCtx context.Context) error {
		return s.lockPaymentFulfillmentLease(txCtx, o.ID, lease)
	})
	if err != nil {
		return fmt.Errorf("redeem balance: %w", err)
	}
	commitment, verifyErr := classifyBalanceFulfillmentCommitment(o, redeemed, nil)
	if verifyErr != nil {
		return fmt.Errorf("verify redeemed payment code: %w", verifyErr)
	}
	if commitment != balanceFulfillmentCommitted {
		return fmt.Errorf("redeemed code does not prove fulfillment for payment order %d", o.ID)
	}
	return s.markCompleted(ctx, o, lease, "RECHARGE_SUCCESS")
}

func (s *PaymentService) reconcileBalanceFulfillmentFailure(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease, cause error) error {
	if cause == nil {
		return errors.New("balance fulfillment reconciliation requires failure cause")
	}
	if s.redeemService == nil {
		return errors.Join(cause, errors.New("verify balance fulfillment after failure: redeem service is unavailable"))
	}

	code, lookupErr := s.redeemService.GetByCode(ctx, o.RechargeCode)
	commitment, verifyErr := classifyBalanceFulfillmentCommitment(o, code, lookupErr)
	switch commitment {
	case balanceFulfillmentCommitted:
		if err := s.markCompleted(ctx, o, lease, "RECHARGE_SUCCESS"); err != nil {
			return errors.Join(cause, fmt.Errorf("complete verified balance fulfillment: %w", err))
		}
		return nil
	case balanceFulfillmentVerificationUnavailable:
		if verifyErr == nil {
			verifyErr = errors.New("unknown balance fulfillment verification failure")
		}
		return errors.Join(cause, fmt.Errorf("verify balance fulfillment after failure: %w", verifyErr))
	default:
		s.markFailed(ctx, o.ID, lease, cause)
		return cause
	}
}

func (s *PaymentService) markCompleted(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease, auditAction string) error {
	if lease == nil {
		return errors.New("missing payment fulfillment lease")
	}
	now := time.Now()
	updated, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(o.ID),
		paymentorder.StatusEQ(OrderStatusRecharging),
		paymentorder.UpdatedAtEQ(lease.version),
	).SetStatus(OrderStatusCompleted).SetCompletedAt(now).Save(ctx)
	if err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}
	if updated == 0 {
		current, getErr := s.entClient.PaymentOrder.Get(ctx, o.ID)
		if getErr == nil && current.Status == OrderStatusCompleted {
			return nil
		}
		return infraerrors.Conflict("CONFLICT", "fulfillment lease was lost before completion")
	}
	shouldNotify := !s.hasAuditLog(ctx, o.ID, auditAction)
	if shouldNotify {
		s.writeAuditLog(ctx, o.ID, auditAction, "system", map[string]any{
			"rechargeCode":   o.RechargeCode,
			"creditedAmount": o.Amount,
			"payAmount":      o.PayAmount,
		})
	}
	if completedOrder, err := s.entClient.PaymentOrder.Get(ctx, o.ID); err == nil && shouldNotify {
		s.notifyPaymentOrder(ctx, "completed", completedOrder)
	} else if err != nil {
		slog.Warn("payment.system_notice_completed_reload_failed", "order_id", o.ID, "error", err)
	}
	return nil
}

func (s *PaymentService) ExecuteSubscriptionFulfillment(ctx context.Context, oid int64) error {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status == OrderStatusCompleted {
		return nil
	}
	if psIsRefundStatus(o.Status) {
		return infraerrors.BadRequest("INVALID_STATUS", "refund-related order cannot fulfill")
	}
	if o.Status != OrderStatusPaid && o.Status != OrderStatusFailed && o.Status != OrderStatusRecharging {
		return infraerrors.BadRequest("INVALID_STATUS", "order cannot fulfill in status "+o.Status)
	}
	if o.SubscriptionGroupID == nil || o.SubscriptionDays == nil {
		return infraerrors.BadRequest("INVALID_STATUS", "missing subscription info")
	}
	lease, err := s.acquirePaymentFulfillmentLease(ctx, o)
	if err != nil {
		return err
	}
	if lease == nil {
		return nil
	}
	if err := s.doSub(ctx, o, lease); err != nil {
		return s.reconcileSubscriptionFulfillmentFailure(ctx, o, lease, err)
	}
	return nil
}

func (s *PaymentService) reconcileSubscriptionFulfillmentFailure(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease, cause error) error {
	if cause == nil {
		return errors.New("subscription fulfillment reconciliation requires failure cause")
	}

	committed, verifyErr := hasPaymentSubscriptionAssignmentAudit(ctx, s.entClient, o.ID)
	if verifyErr != nil {
		return errors.Join(cause, fmt.Errorf("verify subscription fulfillment after failure: %w", verifyErr))
	}
	if committed {
		if err := s.markCompleted(ctx, o, lease, "SUBSCRIPTION_SUCCESS"); err != nil {
			return errors.Join(cause, fmt.Errorf("complete verified subscription fulfillment: %w", err))
		}
		return nil
	}

	s.markFailed(ctx, o.ID, lease, cause)
	return cause
}

func (s *PaymentService) doSub(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease) error {
	gid := *o.SubscriptionGroupID
	days := *o.SubscriptionDays
	g, err := s.groupRepo.GetByID(ctx, gid)
	if err != nil || g.Status != payment.EntityStatusActive {
		return fmt.Errorf("group %d no longer exists or inactive", gid)
	}
	if err := s.ensurePaymentSubscriptionAssigned(ctx, o, lease, gid, days); err != nil {
		return err
	}
	return s.markCompleted(ctx, o, lease, "SUBSCRIPTION_SUCCESS")
}

func (s *PaymentService) lockPaymentFulfillmentLease(ctx context.Context, orderID int64, lease *paymentFulfillmentLease) error {
	if lease == nil {
		return errors.New("missing payment fulfillment lease")
	}
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return errors.New("payment fulfillment lease guard requires transaction context")
	}
	updated, err := tx.Client().PaymentOrder.Update().
		Where(
			paymentorder.IDEQ(orderID),
			paymentorder.StatusEQ(OrderStatusRecharging),
			paymentorder.UpdatedAtEQ(lease.version),
		).
		// Explicitly setting the current lease version suppresses Ent's
		// UpdateDefaultUpdatedAt hook and emits a no-op UPDATE. The write still
		// locks the matching row until the surrounding transaction commits.
		SetUpdatedAt(lease.version).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("lock payment fulfillment lease: %w", err)
	}
	if updated != 1 {
		return infraerrors.Conflict("CONFLICT", "payment fulfillment lease was lost")
	}
	return nil
}

func (s *PaymentService) ensurePaymentSubscriptionAssigned(ctx context.Context, o *dbent.PaymentOrder, lease *paymentFulfillmentLease, groupID int64, days int) error {
	if s.subscriptionSvc == nil {
		return errors.New("subscription service is unavailable")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin subscription fulfillment tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	txClient := tx.Client()
	if err := s.lockPaymentFulfillmentLease(txCtx, o.ID, lease); err != nil {
		return err
	}
	alreadyAssigned, err := hasPaymentSubscriptionAssignmentAudit(txCtx, txClient, o.ID)
	if err != nil {
		return fmt.Errorf("check subscription assignment audit: %w", err)
	}

	recoveredFromNote := false
	assignmentPerformed := false
	var assignedSub *UserSubscription
	var extended bool
	if !alreadyAssigned {
		orderNote := paymentSubscriptionOrderNote(o.ID)
		existing, lookupErr := s.subscriptionSvc.userSubRepo.GetByUserIDAndGroupID(txCtx, o.UserID, groupID)
		switch {
		case lookupErr == nil && existing != nil && hasPaymentSubscriptionOrderNote(existing.Notes, orderNote):
			recoveredFromNote = true
			assignedSub = existing
			extended = true
		case lookupErr != nil && !errors.Is(lookupErr, ErrSubscriptionNotFound):
			return fmt.Errorf("check existing subscription assignment: %w", lookupErr)
		default:
			assignedSub, extended, err = s.subscriptionSvc.assignOrExtendSubscription(txCtx, &AssignSubscriptionInput{
				UserID:       o.UserID,
				GroupID:      groupID,
				ValidityDays: days,
				AssignedBy:   0,
				Notes:        orderNote,
			}, true)
			if err != nil {
				return fmt.Errorf("assign subscription: %w", err)
			}
			assignmentPerformed = true
		}

		detail, marshalErr := json.Marshal(map[string]any{
			"groupID":           groupID,
			"validityDays":      days,
			"recoveredFromNote": recoveredFromNote,
		})
		if marshalErr != nil {
			return fmt.Errorf("marshal subscription assignment audit: %w", marshalErr)
		}
		if _, err := txClient.PaymentAuditLog.Create().
			SetOrderID(strconv.FormatInt(o.ID, 10)).
			SetAction("SUBSCRIPTION_ASSIGNED").
			SetDetail(string(detail)).
			SetOperator("system").
			Save(txCtx); err != nil {
			if dbent.IsConstraintError(err) {
				_ = tx.Rollback()
				claimed, checkErr := hasPaymentSubscriptionAssignmentAudit(ctx, s.entClient, o.ID)
				if checkErr == nil && claimed {
					s.invalidatePaymentSubscriptionCachesAfterCommit(o.ID, o.UserID, groupID)
					return nil
				}
			}
			return fmt.Errorf("record subscription assignment audit: %w", err)
		}
	} else {
		slog.Info("subscription already assigned for order, skipping", "orderID", o.ID, "groupID", groupID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subscription fulfillment tx: %w", err)
	}
	s.invalidatePaymentSubscriptionCachesAfterCommit(o.ID, o.UserID, groupID)
	if assignmentPerformed && assignedSub != nil {
		event := "created"
		if extended {
			event = "extended"
		}
		s.subscriptionSvc.notifySubscription(ctx, event, assignedSub, normalizeAssignValidityDays(days))
	}
	return nil
}

func (s *PaymentService) invalidatePaymentSubscriptionCachesAfterCommit(orderID, userID, groupID int64) {
	if s.subscriptionSvc == nil {
		return
	}
	if err := s.subscriptionSvc.invalidateSubscriptionCaches(userID, groupID); err != nil {
		slog.Warn("payment.subscription_cache_invalidation_failed",
			"order_id", orderID,
			"user_id", userID,
			"group_id", groupID,
			"error", err,
		)
	}
}

func hasPaymentSubscriptionAssignmentAudit(ctx context.Context, client *dbent.Client, orderID int64) (bool, error) {
	count, err := client.PaymentAuditLog.Query().
		Where(
			paymentauditlog.OrderIDEQ(strconv.FormatInt(orderID, 10)),
			paymentauditlog.ActionIn("SUBSCRIPTION_ASSIGNED", "SUBSCRIPTION_SUCCESS"),
		).
		Limit(1).
		Count(ctx)
	return count > 0, err
}

func paymentSubscriptionOrderNote(orderID int64) string {
	return fmt.Sprintf("payment order %d", orderID)
}

func hasPaymentSubscriptionOrderNote(notes string, orderNote string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == orderNote {
			return true
		}
	}
	return false
}

func (s *PaymentService) hasAuditLog(ctx context.Context, orderID int64, action string) bool {
	oid := strconv.FormatInt(orderID, 10)
	c, _ := s.entClient.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(oid), paymentauditlog.ActionEQ(action)).
		Limit(1).Count(ctx)
	return c > 0
}

func (s *PaymentService) markFailed(ctx context.Context, oid int64, lease *paymentFulfillmentLease, cause error) {
	if lease == nil {
		slog.Error("mark FAILED without fulfillment lease", "orderID", oid)
		return
	}
	now := time.Now()
	r := psErrMsg(cause)
	// The lease version prevents a stale worker from overwriting a newer owner.
	c, e := s.entClient.PaymentOrder.Update().
		Where(
			paymentorder.IDEQ(oid),
			paymentorder.StatusEQ(OrderStatusRecharging),
			paymentorder.UpdatedAtEQ(lease.version),
		).
		SetStatus(OrderStatusFailed).SetFailedAt(now).SetFailedReason(r).Save(ctx)
	if e != nil {
		slog.Error("mark FAILED", "orderID", oid, "error", e)
	}
	if c > 0 {
		s.writeAuditLog(ctx, oid, "FULFILLMENT_FAILED", "system", map[string]any{"reason": r})
		if failedOrder, err := s.entClient.PaymentOrder.Get(ctx, oid); err == nil {
			s.notifyPaymentOrder(ctx, "fulfillment_failed", failedOrder)
		} else {
			slog.Warn("payment.system_notice_failed_reload_failed", "order_id", oid, "error", err)
		}
	}
}

func (s *PaymentService) notifyPaymentOrder(ctx context.Context, event string, order *dbent.PaymentOrder) {
	if s == nil || s.systemNotice == nil {
		return
	}
	s.systemNotice.NotifyPaymentOrder(ctx, event, order)
}

func (s *PaymentService) RetryFulfillment(ctx context.Context, oid int64) error {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.PaidAt == nil {
		return infraerrors.BadRequest("INVALID_STATUS", "order is not paid")
	}
	if psIsRefundStatus(o.Status) {
		return infraerrors.BadRequest("INVALID_STATUS", "refund-related order cannot retry")
	}
	if o.Status == OrderStatusCompleted {
		return infraerrors.BadRequest("INVALID_STATUS", "order already completed")
	}
	if o.Status != OrderStatusFailed && o.Status != OrderStatusPaid && o.Status != OrderStatusRecharging {
		return infraerrors.BadRequest("INVALID_STATUS", "only paid, failed, and recoverable recharging orders can retry")
	}
	s.writeAuditLog(ctx, oid, "RECHARGE_RETRY", "admin", map[string]any{"detail": "admin manual retry"})
	return s.executeFulfillment(ctx, oid)
}

func (s *PaymentService) AdminManualFulfillOrder(ctx context.Context, oid int64, req AdminManualFulfillmentRequest) error {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return infraerrors.BadRequest("INVALID_REASON", "manual fulfillment reason is required")
	}
	if len([]rune(reason)) > 500 {
		return infraerrors.BadRequest("INVALID_REASON", "manual fulfillment reason is too long")
	}

	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status == OrderStatusCompleted {
		return infraerrors.BadRequest("INVALID_STATUS", "order already completed")
	}
	if psIsRefundStatus(o.Status) {
		return infraerrors.BadRequest("INVALID_STATUS", "refund-related order cannot fulfill")
	}

	now := time.Now()
	if o.Status == OrderStatusRecharging {
		if o.UpdatedAt.After(now.Add(-paymentFulfillmentLeaseDuration)) {
			return infraerrors.Conflict("ORDER_PROCESSING", "order is still being fulfilled")
		}
	}

	switch o.Status {
	case OrderStatusPending, OrderStatusExpired, OrderStatusCancelled, OrderStatusFailed, OrderStatusPaid, OrderStatusRecharging:
	default:
		return infraerrors.BadRequest("INVALID_STATUS", "order cannot be manually fulfilled in status "+o.Status)
	}

	paidAmount := o.PayAmount
	if req.PaidAmount != nil {
		if err := validateManualPaidAmount(*req.PaidAmount); err != nil {
			return err
		}
		paidAmount = normalizeManualPaidAmount(*req.PaidAmount)
	}
	tradeNo := strings.TrimSpace(req.TradeNo)
	if len(tradeNo) > 128 {
		return infraerrors.BadRequest("INVALID_TRADE_NO", "trade no is too long")
	}

	if o.Status != OrderStatusPaid {
		up := s.entClient.PaymentOrder.Update().
			Where(paymentorder.IDEQ(oid), paymentorder.StatusEQ(o.Status)).
			SetStatus(OrderStatusPaid).
			SetPayAmount(paidAmount).
			ClearFailedAt().
			ClearFailedReason()
		if o.PaidAt == nil {
			up.SetPaidAt(now)
		}
		if tradeNo != "" {
			up.SetPaymentTradeNo(tradeNo)
		}
		updated, err := up.Save(ctx)
		if err != nil {
			return fmt.Errorf("mark order paid manually: %w", err)
		}
		if updated == 0 {
			return infraerrors.Conflict("CONFLICT", "order status has changed")
		}
	} else if tradeNo != "" || math.Abs(paidAmount-o.PayAmount) > amountToleranceCNY || o.PaidAt == nil {
		up := s.entClient.PaymentOrder.UpdateOneID(oid).
			SetPayAmount(paidAmount)
		if o.PaidAt == nil {
			up.SetPaidAt(now)
		}
		if tradeNo != "" {
			up.SetPaymentTradeNo(tradeNo)
		}
		if _, err := up.Save(ctx); err != nil {
			return fmt.Errorf("update paid order manual details: %w", err)
		}
	}

	s.writeAuditLog(ctx, oid, "ORDER_MANUAL_FULFILL", "admin", map[string]any{
		"reason":            reason,
		"previous_status":   o.Status,
		"expected_pay":      o.PayAmount,
		"manual_paid":       paidAmount,
		"trade_no":          tradeNo,
		"amount_mismatched": math.Abs(paidAmount-o.PayAmount) > amountToleranceCNY,
	})
	return s.executeFulfillment(ctx, oid)
}

func validateManualPaidAmount(amount float64) error {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return infraerrors.BadRequest("INVALID_PAID_AMOUNT", "paid amount is invalid")
	}
	return nil
}

func normalizeManualPaidAmount(amount float64) float64 {
	return math.Round(amount*100) / 100
}
